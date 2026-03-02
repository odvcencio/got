package repo

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/odvcencio/graft/pkg/object"
)

// StashEntry represents a single entry in the stash stack.
type StashEntry struct {
	CommitHash object.Hash `json:"commit_hash"`
	Message    string      `json:"message"`
	Timestamp  int64       `json:"timestamp"`
}

// stashPath returns the filesystem path to the stash file.
func (r *Repo) stashPath() string {
	return filepath.Join(r.GotDir, "stash")
}

// readStashStack loads the stash stack from .graft/stash. If the file does
// not exist, an empty slice is returned (no error).
func (r *Repo) readStashStack() ([]StashEntry, error) {
	data, err := os.ReadFile(r.stashPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("stash: read stack: %w", err)
	}

	var entries []StashEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("stash: unmarshal stack: %w", err)
	}
	return entries, nil
}

// writeStashStack atomically writes the stash stack to .graft/stash.
func (r *Repo) writeStashStack(entries []StashEntry) error {
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("stash: marshal stack: %w", err)
	}

	// Atomic write via temp file + rename.
	tmp, err := os.CreateTemp(r.GotDir, ".stash-tmp-*")
	if err != nil {
		return fmt.Errorf("stash: tmpfile: %w", err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("stash: write: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("stash: close: %w", err)
	}

	if err := os.Rename(tmpName, r.stashPath()); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("stash: rename: %w", err)
	}
	return nil
}

// Stash saves the current staging and working tree state as a commit with
// HEAD as parent, then reverts the working tree and staging to match HEAD.
// Returns an error if there are no changes to stash.
func (r *Repo) Stash(author string) (*StashEntry, error) {
	// 1. Check that there are changes to stash.
	statusEntries, err := r.Status()
	if err != nil {
		return nil, fmt.Errorf("stash: %w", err)
	}
	hasChanges := false
	for _, e := range statusEntries {
		if e.IndexStatus != StatusClean || e.WorkStatus != StatusClean {
			hasChanges = true
			break
		}
	}
	if !hasChanges {
		return nil, fmt.Errorf("stash: no changes to stash")
	}

	// 2. Stage all dirty working tree files so the stash commit captures
	//    everything (including unstaged modifications and new untracked files).
	var toStage []string
	for _, e := range statusEntries {
		if e.WorkStatus == StatusDirty || e.WorkStatus == StatusUntracked {
			toStage = append(toStage, e.Path)
		}
	}
	if len(toStage) > 0 {
		if err := r.Add(toStage); err != nil {
			return nil, fmt.Errorf("stash: stage dirty files: %w", err)
		}
	}

	// Handle working tree deletions: remove from staging any file that was
	// deleted from disk so the stash commit reflects that deletion.
	stg, err := r.ReadStaging()
	if err != nil {
		return nil, fmt.Errorf("stash: %w", err)
	}
	for _, e := range statusEntries {
		if e.WorkStatus == StatusDeleted {
			delete(stg.Entries, e.Path)
		}
	}

	// 3. Build tree from current staging.
	treeHash, err := r.BuildTree(stg)
	if err != nil {
		return nil, fmt.Errorf("stash: build tree: %w", err)
	}

	// 4. Resolve HEAD for parent.
	var parents []object.Hash
	parentHash, err := r.ResolveRef("HEAD")
	if err == nil && parentHash != "" {
		parents = append(parents, parentHash)
	}

	// 5. Create stash commit.
	now := time.Now()
	commitObj := &object.CommitObj{
		TreeHash:  treeHash,
		Parents:   parents,
		Author:    author,
		Timestamp: now.Unix(),
		Message:   fmt.Sprintf("WIP on stash"),
	}
	commitHash, err := r.Store.WriteCommit(commitObj)
	if err != nil {
		return nil, fmt.Errorf("stash: write commit: %w", err)
	}

	// 6. Push entry onto stash stack (newest first).
	stack, err := r.readStashStack()
	if err != nil {
		return nil, err
	}
	entry := StashEntry{
		CommitHash: commitHash,
		Message:    commitObj.Message,
		Timestamp:  now.Unix(),
	}
	stack = append([]StashEntry{entry}, stack...)
	if err := r.writeStashStack(stack); err != nil {
		return nil, err
	}

	// 7. Revert working tree and staging to HEAD.
	if err := r.revertToHEAD(); err != nil {
		return nil, fmt.Errorf("stash: revert: %w", err)
	}

	return &entry, nil
}

// revertToHEAD resets the working tree and staging to match the HEAD commit's
// tree. If HEAD has no commits yet, it clears all tracked files and staging.
func (r *Repo) revertToHEAD() error {
	// Gather all currently tracked file paths for removal.
	currentFiles := r.trackedFiles()

	headHash, err := r.ResolveRef("HEAD")
	if err != nil {
		// No commits — clear everything.
		for path := range currentFiles {
			absPath := filepath.Join(r.RootDir, filepath.FromSlash(path))
			os.Remove(absPath)
			r.removeEmptyParents(filepath.Dir(absPath))
		}
		if err := r.WriteStaging(&Staging{Entries: make(map[string]*StagingEntry)}); err != nil {
			return fmt.Errorf("write staging: %w", err)
		}
		return nil
	}

	commit, err := r.Store.ReadCommit(headHash)
	if err != nil {
		return fmt.Errorf("read HEAD commit: %w", err)
	}

	targetFiles, err := r.FlattenTree(commit.TreeHash)
	if err != nil {
		return fmt.Errorf("flatten HEAD tree: %w", err)
	}

	targetMap := make(map[string]TreeFileEntry, len(targetFiles))
	for _, f := range targetFiles {
		targetMap[f.Path] = f
	}

	// Remove all currently tracked files from disk.
	for path := range currentFiles {
		absPath := filepath.Join(r.RootDir, filepath.FromSlash(path))
		if err := os.Remove(absPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove %q: %w", path, err)
		}
		r.removeEmptyParents(filepath.Dir(absPath))
	}

	// Write all files from HEAD tree.
	for _, f := range targetFiles {
		absPath := filepath.Join(r.RootDir, filepath.FromSlash(f.Path))

		dir := filepath.Dir(absPath)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("mkdir %q: %w", dir, err)
		}

		blob, err := r.Store.ReadBlob(f.BlobHash)
		if err != nil {
			return fmt.Errorf("read blob for %q: %w", f.Path, err)
		}

		if err := os.WriteFile(absPath, blob.Data, filePermFromMode(f.Mode)); err != nil {
			return fmt.Errorf("write %q: %w", f.Path, err)
		}
	}

	// Rebuild staging to match HEAD tree.
	stg := &Staging{Entries: make(map[string]*StagingEntry, len(targetFiles))}
	for _, f := range targetFiles {
		absPath := filepath.Join(r.RootDir, filepath.FromSlash(f.Path))
		info, err := os.Stat(absPath)
		if err != nil {
			return fmt.Errorf("stat %q: %w", f.Path, err)
		}

		entry := &StagingEntry{
			Path:           f.Path,
			BlobHash:       f.BlobHash,
			EntityListHash: f.EntityListHash,
		}
		setStagingEntryStat(entry, info, normalizeFileMode(f.Mode))
		stg.Entries[f.Path] = entry
	}
	if err := r.WriteStaging(stg); err != nil {
		return fmt.Errorf("write staging: %w", err)
	}

	return nil
}

// StashPop applies the stash at index then drops it from the stack.
func (r *Repo) StashPop(index int) error {
	if err := r.StashApply(index); err != nil {
		return err
	}
	return r.StashDrop(index)
}

// StashApply applies the stash at index (restores files and staging) without
// removing the entry from the stack.
func (r *Repo) StashApply(index int) error {
	stack, err := r.readStashStack()
	if err != nil {
		return err
	}

	if index < 0 || index >= len(stack) {
		return fmt.Errorf("stash: index %d out of range (stack has %d entries)", index, len(stack))
	}

	entry := stack[index]

	// Read the stash commit.
	commit, err := r.Store.ReadCommit(entry.CommitHash)
	if err != nil {
		return fmt.Errorf("stash: read commit %s: %w", entry.CommitHash, err)
	}

	// Flatten the stash commit's tree.
	stashFiles, err := r.FlattenTree(commit.TreeHash)
	if err != nil {
		return fmt.Errorf("stash: flatten tree: %w", err)
	}

	// Remove all currently tracked files from disk.
	currentFiles := r.trackedFiles()
	for path := range currentFiles {
		absPath := filepath.Join(r.RootDir, filepath.FromSlash(path))
		if err := os.Remove(absPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("stash: remove %q: %w", path, err)
		}
		r.removeEmptyParents(filepath.Dir(absPath))
	}

	// Write all files from the stash tree.
	for _, f := range stashFiles {
		absPath := filepath.Join(r.RootDir, filepath.FromSlash(f.Path))

		dir := filepath.Dir(absPath)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("stash: mkdir %q: %w", dir, err)
		}

		blob, err := r.Store.ReadBlob(f.BlobHash)
		if err != nil {
			return fmt.Errorf("stash: read blob for %q: %w", f.Path, err)
		}

		if err := os.WriteFile(absPath, blob.Data, filePermFromMode(f.Mode)); err != nil {
			return fmt.Errorf("stash: write %q: %w", f.Path, err)
		}
	}

	// Rebuild staging to match stash tree.
	stg := &Staging{Entries: make(map[string]*StagingEntry, len(stashFiles))}
	for _, f := range stashFiles {
		absPath := filepath.Join(r.RootDir, filepath.FromSlash(f.Path))
		info, err := os.Stat(absPath)
		if err != nil {
			return fmt.Errorf("stash: stat %q: %w", f.Path, err)
		}

		se := &StagingEntry{
			Path:           f.Path,
			BlobHash:       f.BlobHash,
			EntityListHash: f.EntityListHash,
		}
		setStagingEntryStat(se, info, normalizeFileMode(f.Mode))
		stg.Entries[f.Path] = se
	}
	if err := r.WriteStaging(stg); err != nil {
		return fmt.Errorf("stash: %w", err)
	}

	return nil
}

// StashList returns all stash entries, newest first.
func (r *Repo) StashList() ([]StashEntry, error) {
	stack, err := r.readStashStack()
	if err != nil {
		return nil, err
	}
	return stack, nil
}

// StashDrop removes the stash entry at the given index.
func (r *Repo) StashDrop(index int) error {
	stack, err := r.readStashStack()
	if err != nil {
		return err
	}

	if index < 0 || index >= len(stack) {
		return fmt.Errorf("stash: index %d out of range (stack has %d entries)", index, len(stack))
	}

	stack = append(stack[:index], stack[index+1:]...)
	return r.writeStashStack(stack)
}
