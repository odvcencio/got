package repo

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/odvcencio/got/pkg/object"
)

// Checkout switches the working directory to the state of the target.
// The target can be a branch name or a raw commit hash.
//
// Algorithm:
//  1. Check for uncommitted changes — refuse if any exist.
//  2. Resolve target: try as branch name first, then as raw hash.
//  3. Read the target commit, flatten its tree.
//  4. Remove all tracked files (files in current HEAD tree + staging).
//  5. Write all files from target tree to working directory.
//  6. Update staging to match the new tree.
//  7. Update HEAD (symbolic ref for branch, raw hash for detached).
func (r *Repo) Checkout(target string) error {
	// 1. Check for uncommitted changes.
	if err := r.ensureClean(); err != nil {
		return fmt.Errorf("checkout: %w", err)
	}

	// 2. Resolve target.
	isBranch := false
	var targetHash object.Hash

	// Try as branch name first.
	branchHash, err := r.ResolveRef("refs/heads/" + target)
	if err == nil {
		targetHash = branchHash
		isBranch = true
	} else {
		// Try as raw hash.
		targetHash = object.Hash(target)
	}

	// 3. Read the target commit and flatten its tree.
	commit, err := r.Store.ReadCommit(targetHash)
	if err != nil {
		return fmt.Errorf("checkout: cannot read commit %s: %w", targetHash, err)
	}

	targetFiles, err := r.FlattenTree(commit.TreeHash)
	if err != nil {
		return fmt.Errorf("checkout: flatten target tree: %w", err)
	}

	// Build a map for quick lookup.
	targetMap := make(map[string]TreeFileEntry, len(targetFiles))
	for _, f := range targetFiles {
		targetMap[f.Path] = f
	}

	// 4. Determine files to remove: files in current HEAD tree + staging that
	//    are NOT in the target tree.
	currentFiles := r.trackedFiles()

	for path := range currentFiles {
		absPath := filepath.Join(r.RootDir, filepath.FromSlash(path))
		if err := os.Remove(absPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("checkout: remove %q: %w", path, err)
		}
		// Clean up empty parent directories.
		r.removeEmptyParents(filepath.Dir(absPath))
	}

	// 5. Write all files from target tree.
	for _, f := range targetFiles {
		absPath := filepath.Join(r.RootDir, filepath.FromSlash(f.Path))

		// Create parent directories.
		dir := filepath.Dir(absPath)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("checkout: mkdir %q: %w", dir, err)
		}

		// Read blob from store and write to disk.
		blob, err := r.Store.ReadBlob(f.BlobHash)
		if err != nil {
			return fmt.Errorf("checkout: read blob for %q: %w", f.Path, err)
		}

		if err := os.WriteFile(absPath, blob.Data, filePermFromMode(f.Mode)); err != nil {
			return fmt.Errorf("checkout: write %q: %w", f.Path, err)
		}
	}

	// 6. Update staging to match the new tree.
	stg := &Staging{Entries: make(map[string]*StagingEntry, len(targetFiles))}
	for _, f := range targetFiles {
		absPath := filepath.Join(r.RootDir, filepath.FromSlash(f.Path))
		info, err := os.Stat(absPath)
		if err != nil {
			return fmt.Errorf("checkout: stat %q: %w", f.Path, err)
		}

		stg.Entries[f.Path] = &StagingEntry{
			Path:           f.Path,
			BlobHash:       f.BlobHash,
			EntityListHash: f.EntityListHash,
			Mode:           normalizeFileMode(f.Mode),
			ModTime:        info.ModTime().Unix(),
			Size:           info.Size(),
		}
	}
	if err := r.WriteStaging(stg); err != nil {
		return fmt.Errorf("checkout: %w", err)
	}

	// 7. Update HEAD.
	headPath := filepath.Join(r.GotDir, "HEAD")
	var headContent string
	if isBranch {
		headContent = "ref: refs/heads/" + target + "\n"
	} else {
		headContent = string(targetHash) + "\n"
	}
	if err := os.WriteFile(headPath, []byte(headContent), 0o644); err != nil {
		return fmt.Errorf("checkout: update HEAD: %w", err)
	}

	return nil
}

// ensureClean checks that the working tree has no uncommitted changes.
// It returns an error if there are any staged changes or dirty files.
func (r *Repo) ensureClean() error {
	entries, err := r.Status()
	if err != nil {
		return fmt.Errorf("check status: %w", err)
	}

	for _, e := range entries {
		if e.IndexStatus != StatusClean || e.WorkStatus != StatusClean {
			return fmt.Errorf("working tree is not clean (file %q has uncommitted changes)", e.Path)
		}
	}
	return nil
}

// trackedFiles returns a set of all currently tracked file paths. It merges
// paths from the HEAD tree and the staging index.
func (r *Repo) trackedFiles() map[string]bool {
	files := make(map[string]bool)

	// From HEAD tree.
	headEntries := r.headTreeEntries()
	for path := range headEntries {
		files[path] = true
	}

	// From staging.
	stg, err := r.ReadStaging()
	if err == nil {
		for path := range stg.Entries {
			files[path] = true
		}
	}

	return files
}

// removeEmptyParents removes empty directories up to (but not including)
// the repository root.
func (r *Repo) removeEmptyParents(dir string) {
	for {
		// Never remove the repo root itself.
		if dir == r.RootDir || !strings.HasPrefix(dir, r.RootDir) {
			return
		}

		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) > 0 {
			return
		}

		os.Remove(dir)
		dir = filepath.Dir(dir)
	}
}
