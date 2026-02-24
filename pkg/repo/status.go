package repo

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/odvcencio/got/pkg/object"
)

// FileStatus represents the state of a file in the working tree or index.
type FileStatus int

const (
	StatusClean     FileStatus = iota // file matches between compared areas
	StatusNew                         // in staging, not in HEAD tree
	StatusModified                    // in staging, different from HEAD
	StatusRenamed                     // same content, path changed
	StatusConflict                    // file has unresolved merge conflicts in index
	StatusDeleted                     // in HEAD but not in staging (or on disk but not in staging)
	StatusUntracked                   // in working dir but not in staging
	StatusDirty                       // staged but working copy differs from staged
)

// StatusEntry records the status of a single file.
type StatusEntry struct {
	Path        string     // repo-relative path
	RenamedFrom string     // non-empty when IndexStatus or WorkStatus is StatusRenamed
	IndexStatus FileStatus // staging vs HEAD comparison
	WorkStatus  FileStatus // working tree vs staging comparison
}

type headTreeState struct {
	BlobHash object.Hash
	Mode     string
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
	workRenamedNewToOld, workRenamedOldToNew, err := r.detectWorktreeRenames(stg, workFiles)
	if err != nil {
		return nil, fmt.Errorf("status: detect worktree renames: %w", err)
	}
	refreshStaging := false

	// --- Working tree vs staging comparison ---

	// For each file on disk:
	for path := range workFiles {
		se, inStaging := stg.Entries[path]
		if !inStaging {
			if oldPath, renamed := workRenamedNewToOld[path]; renamed {
				result[path] = &StatusEntry{
					Path:        path,
					RenamedFrom: oldPath,
					IndexStatus: StatusUntracked,
					WorkStatus:  StatusRenamed,
				}
				continue
			}

			// File exists on disk but not in staging → untracked.
			result[path] = &StatusEntry{
				Path:        path,
				IndexStatus: StatusUntracked,
				WorkStatus:  StatusUntracked,
			}
			continue
		}

		if se.Conflict {
			result[path] = &StatusEntry{
				Path:       path,
				WorkStatus: StatusConflict,
			}
			continue
		}

		// File is in staging — compare metadata first, then content hash if needed.
		absPath := filepath.Join(r.RootDir, filepath.FromSlash(path))
		info, err := os.Stat(absPath)
		if err != nil {
			return nil, fmt.Errorf("status: stat %q: %w", path, err)
		}
		workMode := modeFromFileInfo(info)
		workStatus := StatusClean
		if !stagingStatMatchesWorktree(se, info, workMode) {
			content, err := os.ReadFile(absPath)
			if err != nil {
				return nil, fmt.Errorf("status: read %q: %w", path, err)
			}
			workHash := object.HashObject(object.TypeBlob, content)
			if workHash != se.BlobHash || workMode != normalizeFileMode(se.Mode) {
				workStatus = StatusDirty
			} else if refreshStagingEntryStat(se, info, workMode) {
				refreshStaging = true
			}
		}

		entry := &StatusEntry{
			Path:       path,
			WorkStatus: workStatus,
		}

		result[path] = entry
	}

	// For each staged entry not on disk → deleted from working tree.
	for path, se := range stg.Entries {
		if _, onDisk := workFiles[path]; !onDisk {
			if _, renamed := workRenamedOldToNew[path]; renamed {
				continue
			}
			entry, exists := result[path]
			if !exists {
				entry = &StatusEntry{Path: path}
				result[path] = entry
			}
			if se.Conflict {
				entry.WorkStatus = StatusConflict
			} else {
				entry.WorkStatus = StatusDeleted
			}
		}
	}

	// --- Staging vs HEAD comparison ---
	// Try to get HEAD tree entries. For now (FlattenTree may not exist),
	// we treat HEAD as empty if there are no commits yet or if we cannot
	// resolve the tree.
	headEntries := r.headTreeEntries()
	indexRenamedNewToOld, indexRenamedOldToNew := detectIndexRenames(stg, headEntries)

	for path, se := range stg.Entries {
		entry, exists := result[path]
		if !exists {
			entry = &StatusEntry{Path: path}
			result[path] = entry
		}

		headState, inHead := headEntries[path]
		if se.Conflict {
			entry.IndexStatus = StatusConflict
		} else if !inHead {
			if oldPath, renamed := indexRenamedNewToOld[path]; renamed {
				entry.IndexStatus = StatusRenamed
				entry.RenamedFrom = oldPath
			} else {
				entry.IndexStatus = StatusNew
			}
		} else if se.BlobHash != headState.BlobHash || normalizeFileMode(se.Mode) != normalizeFileMode(headState.Mode) {
			entry.IndexStatus = StatusModified
		} else {
			entry.IndexStatus = StatusClean
		}
	}

	// For each HEAD entry not in staging → deleted from index.
	for path := range headEntries {
		if _, inStaging := stg.Entries[path]; !inStaging {
			if _, renamed := indexRenamedOldToNew[path]; renamed {
				continue
			}
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

	if refreshStaging {
		if err := r.WriteStaging(stg); err != nil {
			return nil, fmt.Errorf("status: refresh staging: %w", err)
		}
	}

	return entries, nil
}

// headTreeEntries attempts to read the HEAD commit's tree and flatten it
// into a map of path → BlobHash. If there are no commits yet (fresh repo)
// or if tree reading fails, an empty map is returned.
func (r *Repo) headTreeEntries() map[string]headTreeState {
	result := make(map[string]headTreeState)

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
func (r *Repo) flattenTree(treeHash object.Hash, prefix string, entries map[string]headTreeState) {
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
			entries[path] = headTreeState{
				BlobHash: te.BlobHash,
				Mode:     normalizeFileMode(te.Mode),
			}
		}
	}
}

const statusStatCacheNanoThreshold int64 = 1_000_000_000_000
const statusRacyCleanWindow = 2 * time.Second

func stagingStatMatchesWorktree(se *StagingEntry, info os.FileInfo, workMode string) bool {
	if se == nil {
		return false
	}
	if normalizeFileMode(se.Mode) != normalizeFileMode(workMode) {
		return false
	}
	if se.Size != info.Size() {
		return false
	}
	// Old index entries may use second resolution; hash those once and refresh.
	if se.ModTime < statusStatCacheNanoThreshold {
		return false
	}
	if isRacyCleanModTime(info.ModTime()) {
		return false
	}
	// Some filesystems expose coarse (second-level) mtimes. When nanoseconds are
	// zero, same-size edits inside a second can evade stat-only detection.
	if info.ModTime().Nanosecond() == 0 {
		return false
	}
	return se.ModTime == info.ModTime().UnixNano()
}

func refreshStagingEntryStat(se *StagingEntry, info os.FileInfo, workMode string) bool {
	if se == nil {
		return false
	}
	nextMode := normalizeFileMode(workMode)
	nextModTime := info.ModTime().UnixNano()
	nextSize := info.Size()
	if se.ModTime == nextModTime && se.Size == nextSize && normalizeFileMode(se.Mode) == nextMode {
		return false
	}
	se.Mode = nextMode
	se.ModTime = nextModTime
	se.Size = nextSize
	return true
}

func isRacyCleanModTime(modTime time.Time) bool {
	now := time.Now()
	if modTime.After(now) {
		return true
	}
	return now.Sub(modTime) < statusRacyCleanWindow
}

func detectIndexRenames(stg *Staging, headEntries map[string]headTreeState) (map[string]string, map[string]string) {
	newByKey := make(map[string][]string)
	oldByKey := make(map[string][]string)

	for path, se := range stg.Entries {
		if _, inHead := headEntries[path]; inHead {
			continue
		}
		key := renameMatchKey(se.BlobHash, se.Mode)
		newByKey[key] = append(newByKey[key], path)
	}
	for path, hs := range headEntries {
		if _, inStaging := stg.Entries[path]; inStaging {
			continue
		}
		key := renameMatchKey(hs.BlobHash, hs.Mode)
		oldByKey[key] = append(oldByKey[key], path)
	}

	return pairRenameCandidates(newByKey, oldByKey)
}

func (r *Repo) detectWorktreeRenames(stg *Staging, workFiles map[string]bool) (map[string]string, map[string]string, error) {
	oldByKey := make(map[string][]string)
	newByKey := make(map[string][]string)

	for path, se := range stg.Entries {
		if workFiles[path] {
			continue
		}
		key := renameMatchKey(se.BlobHash, se.Mode)
		oldByKey[key] = append(oldByKey[key], path)
	}

	for path := range workFiles {
		if _, inStaging := stg.Entries[path]; inStaging {
			continue
		}
		absPath := filepath.Join(r.RootDir, filepath.FromSlash(path))
		info, err := os.Stat(absPath)
		if err != nil {
			return nil, nil, err
		}
		data, err := os.ReadFile(absPath)
		if err != nil {
			return nil, nil, err
		}
		key := renameMatchKey(object.HashObject(object.TypeBlob, data), modeFromFileInfo(info))
		newByKey[key] = append(newByKey[key], path)
	}

	newToOld, oldToNew := pairRenameCandidates(newByKey, oldByKey)
	return newToOld, oldToNew, nil
}

func pairRenameCandidates(newByKey, oldByKey map[string][]string) (map[string]string, map[string]string) {
	newToOld := make(map[string]string)
	oldToNew := make(map[string]string)

	for key, newPaths := range newByKey {
		oldPaths := oldByKey[key]
		if len(oldPaths) == 0 {
			continue
		}

		sort.Strings(newPaths)
		sort.Strings(oldPaths)

		n := len(newPaths)
		if len(oldPaths) < n {
			n = len(oldPaths)
		}

		for i := 0; i < n; i++ {
			newPath := newPaths[i]
			oldPath := oldPaths[i]
			newToOld[newPath] = oldPath
			oldToNew[oldPath] = newPath
		}
	}

	return newToOld, oldToNew
}

func renameMatchKey(blobHash object.Hash, mode string) string {
	return string(blobHash) + "|" + normalizeFileMode(strings.TrimSpace(mode))
}
