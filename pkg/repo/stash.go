package repo

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/odvcencio/graft/pkg/diff3"
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
	return filepath.Join(r.GraftDir, "stash")
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
	tmp, err := os.CreateTemp(r.GraftDir, ".stash-tmp-*")
	if err != nil {
		return fmt.Errorf("stash: tmpfile: %w", err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("stash: write: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("stash: sync: %w", err)
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
	if err := r.WriteStaging(stg); err != nil {
		return nil, fmt.Errorf("stash: write staging: %w", err)
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
		Message:   "WIP on stash",
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

	r.GitShadowStash("push")

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
	if err := r.StashDrop(index); err != nil {
		return err
	}
	r.GitShadowStash("pop")
	return nil
}

// StashApply applies the stash at index using a 3-way merge:
//   - Base = the parent of the stash commit (state when stash was created)
//   - Ours = the current working tree / HEAD
//   - Theirs = the stash commit's tree
//
// This handles the case where the working tree has changed since the stash was
// created, properly detecting and reporting conflicts instead of silently
// clobbering changes. Returns nil error on success (including when there are
// conflicts). Use StashApplyMerge to get a detailed result.
func (r *Repo) StashApply(index int) error {
	result, err := r.StashApplyMerge(index)
	if err != nil {
		return err
	}
	if !result.Clean {
		return fmt.Errorf("stash apply: %d conflict(s) in: %s",
			len(result.ConflictPaths),
			joinPaths(result.ConflictPaths))
	}
	return nil
}

// joinPaths joins a slice of paths with ", ".
func joinPaths(paths []string) string {
	return strings.Join(paths, ", ")
}

// StashApplyMerge applies the stash at index using a 3-way merge and returns
// a detailed result. Unlike StashApply, this method does not return an error
// for conflicts -- instead it reports them in StashApplyResult.
func (r *Repo) StashApplyMerge(index int) (*StashApplyResult, error) {
	stack, err := r.readStashStack()
	if err != nil {
		return nil, err
	}

	if index < 0 || index >= len(stack) {
		return nil, fmt.Errorf("stash: index %d out of range (stack has %d entries)", index, len(stack))
	}

	entry := stack[index]

	// Read the stash commit.
	stashCommit, err := r.Store.ReadCommit(entry.CommitHash)
	if err != nil {
		return nil, fmt.Errorf("stash: read commit %s: %w", entry.CommitHash, err)
	}

	// Flatten the stash commit's tree (theirs).
	theirsFiles, err := r.FlattenTree(stashCommit.TreeHash)
	if err != nil {
		return nil, fmt.Errorf("stash: flatten stash tree: %w", err)
	}
	theirsMap := indexByPath(theirsFiles)

	// Get the base: the parent of the stash commit.
	var baseMap map[string]TreeFileEntry
	if len(stashCommit.Parents) > 0 && stashCommit.Parents[0] != "" {
		parentCommit, err := r.Store.ReadCommit(stashCommit.Parents[0])
		if err != nil {
			return nil, fmt.Errorf("stash: read parent commit: %w", err)
		}
		baseFiles, err := r.FlattenTree(parentCommit.TreeHash)
		if err != nil {
			return nil, fmt.Errorf("stash: flatten base tree: %w", err)
		}
		baseMap = indexByPath(baseFiles)
	} else {
		baseMap = make(map[string]TreeFileEntry)
	}

	// Get ours: the current HEAD tree.
	var oursMap map[string]TreeFileEntry
	headHash, err := r.ResolveRef("HEAD")
	if err == nil && headHash != "" {
		headCommit, err := r.Store.ReadCommit(headHash)
		if err != nil {
			return nil, fmt.Errorf("stash: read HEAD commit: %w", err)
		}
		oursFiles, err := r.FlattenTree(headCommit.TreeHash)
		if err != nil {
			return nil, fmt.Errorf("stash: flatten HEAD tree: %w", err)
		}
		oursMap = indexByPath(oursFiles)
	} else {
		oursMap = make(map[string]TreeFileEntry)
	}

	// Use the shared three-way merge helper.
	mergeResult, err := r.threeWayTreeMerge(baseMap, oursMap, theirsMap)
	if err != nil {
		return nil, fmt.Errorf("stash: %w", err)
	}

	// Apply results to the working directory.
	if err := r.applyThreeWayResult(mergeResult); err != nil {
		return nil, fmt.Errorf("stash: %w", err)
	}

	// Stage all written/added/conflicted files.
	var pathsToStage []string
	for _, f := range mergeResult.Files {
		if f.Status != "unchanged" && f.Status != "deleted" {
			pathsToStage = append(pathsToStage, f.Path)
		}
	}
	if len(pathsToStage) > 0 {
		if err := r.Add(pathsToStage); err != nil {
			return nil, fmt.Errorf("stash: stage: %w", err)
		}
	}

	// Remove deleted files from staging.
	if len(mergeResult.DeletedPaths) > 0 {
		stg, err := r.ReadStaging()
		if err != nil {
			return nil, fmt.Errorf("stash: read staging: %w", err)
		}
		for _, p := range mergeResult.DeletedPaths {
			delete(stg.Entries, p)
		}
		if err := r.WriteStaging(stg); err != nil {
			return nil, fmt.Errorf("stash: write staging: %w", err)
		}
	}

	return &StashApplyResult{
		Clean:         !mergeResult.HasConflicts,
		ConflictPaths: mergeResult.ConflictDetails,
	}, nil
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
	if err := r.writeStashStack(stack); err != nil {
		return err
	}
	r.GitShadowStash("drop")
	return nil
}

// StashShowEntry describes a single file changed in a stash entry.
type StashShowEntry struct {
	Path       string // file path
	ChangeType string // "added", "modified", "deleted"
}

// StashShow returns the list of files changed in the stash at the given index
// by comparing the stash commit's tree against its parent's tree. If the stash
// has no parent (created on an empty repo), all files are reported as "added".
func (r *Repo) StashShow(index int) ([]StashShowEntry, error) {
	stack, err := r.readStashStack()
	if err != nil {
		return nil, err
	}

	if index < 0 || index >= len(stack) {
		return nil, fmt.Errorf("stash: index %d out of range (stack has %d entries)", index, len(stack))
	}

	entry := stack[index]
	commit, err := r.Store.ReadCommit(entry.CommitHash)
	if err != nil {
		return nil, fmt.Errorf("stash: read commit %s: %w", entry.CommitHash, err)
	}

	stashFiles, err := r.FlattenTree(commit.TreeHash)
	if err != nil {
		return nil, fmt.Errorf("stash: flatten stash tree: %w", err)
	}
	stashMap := indexByPath(stashFiles)

	// If the stash commit has a parent, compare against the parent's tree.
	var parentMap map[string]TreeFileEntry
	if len(commit.Parents) > 0 && commit.Parents[0] != "" {
		parentCommit, err := r.Store.ReadCommit(commit.Parents[0])
		if err != nil {
			return nil, fmt.Errorf("stash: read parent commit %s: %w", commit.Parents[0], err)
		}
		parentFiles, err := r.FlattenTree(parentCommit.TreeHash)
		if err != nil {
			return nil, fmt.Errorf("stash: flatten parent tree: %w", err)
		}
		parentMap = indexByPath(parentFiles)
	} else {
		parentMap = make(map[string]TreeFileEntry)
	}

	// Collect all paths from both trees.
	allPaths := make(map[string]bool)
	for p := range stashMap {
		allPaths[p] = true
	}
	for p := range parentMap {
		allPaths[p] = true
	}

	sortedPaths := make([]string, 0, len(allPaths))
	for p := range allPaths {
		sortedPaths = append(sortedPaths, p)
	}
	sort.Strings(sortedPaths)

	var result []StashShowEntry
	for _, p := range sortedPaths {
		_, inStash := stashMap[p]
		_, inParent := parentMap[p]

		switch {
		case inStash && !inParent:
			result = append(result, StashShowEntry{Path: p, ChangeType: "added"})
		case !inStash && inParent:
			result = append(result, StashShowEntry{Path: p, ChangeType: "deleted"})
		case inStash && inParent:
			if stashMap[p].BlobHash != parentMap[p].BlobHash {
				result = append(result, StashShowEntry{Path: p, ChangeType: "modified"})
			}
		}
	}

	return result, nil
}

// StashShowDiff returns the full diff (patch) content for a stash entry by
// comparing the stash commit's tree against its parent's tree.
func (r *Repo) StashShowDiff(index int) ([]byte, error) {
	stack, err := r.readStashStack()
	if err != nil {
		return nil, err
	}

	if index < 0 || index >= len(stack) {
		return nil, fmt.Errorf("stash: index %d out of range (stack has %d entries)", index, len(stack))
	}

	entry := stack[index]
	commit, err := r.Store.ReadCommit(entry.CommitHash)
	if err != nil {
		return nil, fmt.Errorf("stash: read commit %s: %w", entry.CommitHash, err)
	}

	stashFiles, err := r.FlattenTree(commit.TreeHash)
	if err != nil {
		return nil, fmt.Errorf("stash: flatten stash tree: %w", err)
	}
	stashMap := indexByPath(stashFiles)

	var parentMap map[string]TreeFileEntry
	if len(commit.Parents) > 0 && commit.Parents[0] != "" {
		parentCommit, err := r.Store.ReadCommit(commit.Parents[0])
		if err != nil {
			return nil, fmt.Errorf("stash: read parent commit %s: %w", commit.Parents[0], err)
		}
		parentFiles, err := r.FlattenTree(parentCommit.TreeHash)
		if err != nil {
			return nil, fmt.Errorf("stash: flatten parent tree: %w", err)
		}
		parentMap = indexByPath(parentFiles)
	} else {
		parentMap = make(map[string]TreeFileEntry)
	}

	allPaths := make(map[string]bool)
	for p := range stashMap {
		allPaths[p] = true
	}
	for p := range parentMap {
		allPaths[p] = true
	}
	sortedPaths := make([]string, 0, len(allPaths))
	for p := range allPaths {
		sortedPaths = append(sortedPaths, p)
	}
	sort.Strings(sortedPaths)

	var buf []byte
	for _, p := range sortedPaths {
		sEntry, inStash := stashMap[p]
		pEntry, inParent := parentMap[p]

		var oldData, newData []byte

		switch {
		case inStash && !inParent:
			newBlob, err := r.Store.ReadBlob(sEntry.BlobHash)
			if err != nil {
				return nil, fmt.Errorf("stash: read blob %s: %w", sEntry.BlobHash, err)
			}
			newData = newBlob.Data
		case !inStash && inParent:
			oldBlob, err := r.Store.ReadBlob(pEntry.BlobHash)
			if err != nil {
				return nil, fmt.Errorf("stash: read blob %s: %w", pEntry.BlobHash, err)
			}
			oldData = oldBlob.Data
		case inStash && inParent:
			if sEntry.BlobHash == pEntry.BlobHash {
				continue // unchanged
			}
			oldBlob, err := r.Store.ReadBlob(pEntry.BlobHash)
			if err != nil {
				return nil, fmt.Errorf("stash: read blob %s: %w", pEntry.BlobHash, err)
			}
			oldData = oldBlob.Data
			newBlob, err := r.Store.ReadBlob(sEntry.BlobHash)
			if err != nil {
				return nil, fmt.Errorf("stash: read blob %s: %w", sEntry.BlobHash, err)
			}
			newData = newBlob.Data
		default:
			continue
		}

		buf = append(buf, formatUnifiedDiff(p, oldData, newData)...)
	}

	return buf, nil
}

// formatUnifiedDiff produces a unified-diff output for a single file
// using the Myers line-level diff algorithm.
func formatUnifiedDiff(path string, oldData, newData []byte) []byte {
	var buf []byte

	aPath := "a/" + path
	bPath := "b/" + path
	if oldData == nil {
		aPath = "/dev/null"
	}
	if newData == nil {
		bPath = "/dev/null"
	}

	buf = append(buf, []byte(fmt.Sprintf("--- %s\n+++ %s\n", aPath, bPath))...)

	oldLines := splitLines(oldData)
	newLines := splitLines(newData)

	if len(oldLines) > 0 || len(newLines) > 0 {
		buf = append(buf, []byte(fmt.Sprintf("@@ -1,%d +1,%d @@\n", len(oldLines), len(newLines)))...)

		diffs := diff3.LineDiff(oldData, newData)
		for _, d := range diffs {
			switch d.Type {
			case diff3.Delete:
				buf = append(buf, '-')
				buf = append(buf, []byte(d.Content)...)
				buf = append(buf, '\n')
			case diff3.Insert:
				buf = append(buf, '+')
				buf = append(buf, []byte(d.Content)...)
				buf = append(buf, '\n')
			case diff3.Equal:
				buf = append(buf, ' ')
				buf = append(buf, []byte(d.Content)...)
				buf = append(buf, '\n')
			}
		}
	}

	return buf
}

// splitLines splits data into lines, stripping the trailing newline.
func splitLines(data []byte) [][]byte {
	if len(data) == 0 {
		return nil
	}
	// Remove trailing newline to avoid a spurious empty line.
	if data[len(data)-1] == '\n' {
		data = data[:len(data)-1]
	}
	if len(data) == 0 {
		return nil
	}
	var lines [][]byte
	start := 0
	for i, b := range data {
		if b == '\n' {
			lines = append(lines, data[start:i])
			start = i + 1
		}
	}
	if start < len(data) {
		lines = append(lines, data[start:])
	}
	return lines
}

// StashApplyResult holds the outcome of a stash apply with 3-way merge.
type StashApplyResult struct {
	// Clean is true if the apply completed without conflicts.
	Clean bool
	// ConflictPaths lists files that have merge conflicts.
	ConflictPaths []string
}
