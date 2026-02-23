package repo

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/odvcencio/got/pkg/object"
)

// FileStatus represents the state of a file in the working tree or index.
type FileStatus int

const (
	StatusClean     FileStatus = iota // file matches between compared areas
	StatusNew                         // in staging, not in HEAD tree
	StatusModified                    // in staging, different from HEAD
	StatusDeleted                     // in HEAD but not in staging (or on disk but not in staging)
	StatusUntracked                   // in working dir but not in staging
	StatusDirty                       // staged but working copy differs from staged
)

// StatusEntry records the status of a single file.
type StatusEntry struct {
	Path        string     // repo-relative path
	IndexStatus FileStatus // staging vs HEAD comparison
	WorkStatus  FileStatus // working tree vs staging comparison
}

// Status computes the working tree status for the repository.
//
// Algorithm:
//  1. Read staging index.
//  2. Walk the working directory (skipping .got/ and ignored paths).
//  3. Compare working tree files against staging entries.
//  4. Compare staging entries against HEAD tree (if available).
//  5. Return a sorted list of status entries.
func (r *Repo) Status() ([]StatusEntry, error) {
	stg, err := r.ReadStaging()
	if err != nil {
		return nil, fmt.Errorf("status: %w", err)
	}

	ic := NewIgnoreChecker(r.RootDir)

	// Collect all working-tree files (repo-relative paths).
	workFiles := make(map[string]bool)
	err = filepath.WalkDir(r.RootDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		rel, err := filepath.Rel(r.RootDir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)

		// Skip the root directory itself.
		if rel == "." {
			return nil
		}

		// Skip ignored directories entirely.
		if ic.IsIgnored(rel) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}

		// Only track regular files.
		if !d.IsDir() {
			workFiles[rel] = true
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("status: walk: %w", err)
	}

	// Build the result map keyed by path.
	result := make(map[string]*StatusEntry)

	// --- Working tree vs staging comparison ---

	// For each file on disk:
	for path := range workFiles {
		se, inStaging := stg.Entries[path]
		if !inStaging {
			// File exists on disk but not in staging → untracked.
			result[path] = &StatusEntry{
				Path:        path,
				IndexStatus: StatusUntracked,
				WorkStatus:  StatusUntracked,
			}
			continue
		}

		// File is in staging — compare content hash.
		absPath := filepath.Join(r.RootDir, filepath.FromSlash(path))
		content, err := os.ReadFile(absPath)
		if err != nil {
			return nil, fmt.Errorf("status: read %q: %w", path, err)
		}

		workHash := object.HashObject(object.TypeBlob, content)
		workStatus := StatusClean
		if workHash != se.BlobHash {
			workStatus = StatusDirty
		}

		entry := &StatusEntry{
			Path:       path,
			WorkStatus: workStatus,
		}

		result[path] = entry
	}

	// For each staged entry not on disk → deleted from working tree.
	for path := range stg.Entries {
		if _, onDisk := workFiles[path]; !onDisk {
			entry, exists := result[path]
			if !exists {
				entry = &StatusEntry{Path: path}
				result[path] = entry
			}
			entry.WorkStatus = StatusDeleted
		}
	}

	// --- Staging vs HEAD comparison ---
	// Try to get HEAD tree entries. For now (FlattenTree may not exist),
	// we treat HEAD as empty if there are no commits yet or if we cannot
	// resolve the tree.
	headEntries := r.headTreeEntries()

	for path, se := range stg.Entries {
		entry, exists := result[path]
		if !exists {
			entry = &StatusEntry{Path: path}
			result[path] = entry
		}

		headBlobHash, inHead := headEntries[path]
		if !inHead {
			entry.IndexStatus = StatusNew
		} else if se.BlobHash != headBlobHash {
			entry.IndexStatus = StatusModified
		} else {
			entry.IndexStatus = StatusClean
		}
	}

	// For each HEAD entry not in staging → deleted from index.
	for path := range headEntries {
		if _, inStaging := stg.Entries[path]; !inStaging {
			entry, exists := result[path]
			if !exists {
				entry = &StatusEntry{Path: path}
				result[path] = entry
			}
			entry.IndexStatus = StatusDeleted
		}
	}

	// Collect and sort.
	entries := make([]StatusEntry, 0, len(result))
	for _, e := range result {
		entries = append(entries, *e)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Path < entries[j].Path
	})

	return entries, nil
}

// headTreeEntries attempts to read the HEAD commit's tree and flatten it
// into a map of path → BlobHash. If there are no commits yet (fresh repo)
// or if tree reading fails, an empty map is returned.
func (r *Repo) headTreeEntries() map[string]object.Hash {
	result := make(map[string]object.Hash)

	headHash, err := r.ResolveRef("HEAD")
	if err != nil {
		// No commits yet — HEAD is empty.
		return result
	}

	commit, err := r.Store.ReadCommit(headHash)
	if err != nil {
		return result
	}

	// Recursively flatten the tree.
	r.flattenTree(commit.TreeHash, "", result)
	return result
}

// flattenTree recursively walks a tree object and populates entries with
// path → BlobHash mappings.
func (r *Repo) flattenTree(treeHash object.Hash, prefix string, entries map[string]object.Hash) {
	tree, err := r.Store.ReadTree(treeHash)
	if err != nil {
		return
	}

	for _, te := range tree.Entries {
		path := te.Name
		if prefix != "" {
			path = prefix + "/" + te.Name
		}

		if te.IsDir && te.SubtreeHash != "" {
			r.flattenTree(te.SubtreeHash, path, entries)
		} else if !te.IsDir {
			entries[path] = te.BlobHash
		}
	}
}
