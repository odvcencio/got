package repo

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/odvcencio/graft/pkg/object"
)

// CherryPickResult captures the outcome of a commit-level cherry-pick.
type CherryPickResult struct {
	TargetCommit object.Hash
	CommitHash   object.Hash
	Message      string
}

// CherryPick applies the changes introduced by the given commit onto HEAD
// using a three-way structural merge.
//
// Algorithm:
//  1. Read the target commit and its first parent
//  2. Three-way merge: base=parent's tree, ours=HEAD's tree, theirs=target's tree
//  3. If clean: auto-commit with the original commit's message and author, HEAD as parent
//  4. If conflicts: return error describing the conflicts
func (r *Repo) CherryPick(targetHash object.Hash) (*CherryPickResult, error) {
	targetHash = object.Hash(strings.TrimSpace(string(targetHash)))
	if targetHash == "" {
		return nil, fmt.Errorf("cherry-pick: target commit is required")
	}

	// Read the target commit.
	targetCommit, err := r.Store.ReadCommit(targetHash)
	if err != nil {
		return nil, fmt.Errorf("cherry-pick: read target commit %s: %w", targetHash, err)
	}

	// Target must have at least one parent (no root commits).
	if len(targetCommit.Parents) == 0 {
		return nil, fmt.Errorf("cherry-pick: commit %s has no parent; cannot cherry-pick a root commit", targetHash)
	}
	parentHash := targetCommit.Parents[0]

	// Read the parent commit.
	parentCommit, err := r.Store.ReadCommit(parentHash)
	if err != nil {
		return nil, fmt.Errorf("cherry-pick: read parent commit %s: %w", parentHash, err)
	}

	// Resolve HEAD.
	headHash, err := r.ResolveRef("HEAD")
	if err != nil {
		return nil, fmt.Errorf("cherry-pick: resolve HEAD: %w", err)
	}
	headCommit, err := r.Store.ReadCommit(headHash)
	if err != nil {
		return nil, fmt.Errorf("cherry-pick: read HEAD commit %s: %w", headHash, err)
	}

	// Flatten all three trees: base (parent), ours (HEAD), theirs (target).
	baseFiles, err := r.FlattenTree(parentCommit.TreeHash)
	if err != nil {
		return nil, fmt.Errorf("cherry-pick: flatten base tree: %w", err)
	}
	oursFiles, err := r.FlattenTree(headCommit.TreeHash)
	if err != nil {
		return nil, fmt.Errorf("cherry-pick: flatten ours tree: %w", err)
	}
	theirsFiles, err := r.FlattenTree(targetCommit.TreeHash)
	if err != nil {
		return nil, fmt.Errorf("cherry-pick: flatten theirs tree: %w", err)
	}

	// Index files by path.
	baseMap := indexByPath(baseFiles)
	oursMap := indexByPath(oursFiles)
	theirsMap := indexByPath(theirsFiles)

	// Collect all file paths across all three trees.
	allPaths := collectAllPaths(baseMap, oursMap, theirsMap)

	// Process each file with three-way merge logic.
	report := &MergeReport{}
	type mergedFile struct {
		path    string
		content []byte
		mode    string
	}
	var mergedFiles []mergedFile
	var deletedPaths []string

	for _, path := range allPaths {
		_, inBase := baseMap[path]
		_, inOurs := oursMap[path]
		_, inTheirs := theirsMap[path]

		switch {
		case inBase && inOurs && inTheirs:
			// In all three: three-way merge.
			fr, content, err := r.mergeThreeWay(path, baseMap[path], oursMap[path], theirsMap[path])
			if err != nil {
				return nil, fmt.Errorf("cherry-pick: merge file %q: %w", path, err)
			}
			report.Files = append(report.Files, fr)
			if fr.Status == "conflict" {
				report.HasConflicts = true
				report.TotalConflicts += fr.ConflictCount
			}
			mergedFiles = append(mergedFiles, mergedFile{
				path:    path,
				content: content,
				mode:    normalizeFileMode(oursMap[path].Mode),
			})

		case !inBase && inOurs && inTheirs:
			// New in both ours and theirs (not in base).
			if oursMap[path].BlobHash == theirsMap[path].BlobHash {
				content, err := r.readBlobData(oursMap[path].BlobHash)
				if err != nil {
					return nil, fmt.Errorf("cherry-pick: read %q: %w", path, err)
				}
				report.Files = append(report.Files, FileMergeReport{Path: path, Status: "clean"})
				mergedFiles = append(mergedFiles, mergedFile{
					path:    path,
					content: content,
					mode:    normalizeFileMode(oursMap[path].Mode),
				})
			} else {
				oursData, err := r.readBlobData(oursMap[path].BlobHash)
				if err != nil {
					return nil, fmt.Errorf("cherry-pick: read ours %q: %w", path, err)
				}
				theirsData, err := r.readBlobData(theirsMap[path].BlobHash)
				if err != nil {
					return nil, fmt.Errorf("cherry-pick: read theirs %q: %w", path, err)
				}
				fr, content, err := r.mergeFileContents(path, nil, oursData, theirsData)
				if err != nil {
					return nil, fmt.Errorf("cherry-pick: merge file %q: %w", path, err)
				}
				report.Files = append(report.Files, fr)
				if fr.Status == "conflict" {
					report.HasConflicts = true
					report.TotalConflicts += fr.ConflictCount
				}
				mergedFiles = append(mergedFiles, mergedFile{
					path:    path,
					content: content,
					mode:    normalizeFileMode(oursMap[path].Mode),
				})
			}

		case inBase && inOurs && !inTheirs:
			// Deleted by theirs (the cherry-picked commit deleted this file).
			if oursMap[path].BlobHash == baseMap[path].BlobHash {
				// Ours unchanged from base: clean delete.
				report.Files = append(report.Files, FileMergeReport{Path: path, Status: "deleted"})
				deletedPaths = append(deletedPaths, path)
			} else {
				// Ours modified, theirs deleted: conflict.
				oursData, err := r.readBlobData(oursMap[path].BlobHash)
				if err != nil {
					return nil, fmt.Errorf("cherry-pick: read ours %q: %w", path, err)
				}
				content := renderFileConflict(oursData, nil)
				report.Files = append(report.Files, FileMergeReport{
					Path:          path,
					Status:        "conflict",
					ConflictCount: 1,
				})
				report.HasConflicts = true
				report.TotalConflicts++
				mergedFiles = append(mergedFiles, mergedFile{
					path:    path,
					content: content,
					mode:    normalizeFileMode(oursMap[path].Mode),
				})
			}

		case inBase && !inOurs && inTheirs:
			// Deleted by ours, present in theirs.
			if theirsMap[path].BlobHash == baseMap[path].BlobHash {
				// Theirs unchanged from base: ours' deletion wins.
				report.Files = append(report.Files, FileMergeReport{Path: path, Status: "deleted"})
				deletedPaths = append(deletedPaths, path)
			} else {
				// Theirs modified, ours deleted: conflict.
				theirsData, err := r.readBlobData(theirsMap[path].BlobHash)
				if err != nil {
					return nil, fmt.Errorf("cherry-pick: read theirs %q: %w", path, err)
				}
				content := renderFileConflict(nil, theirsData)
				report.Files = append(report.Files, FileMergeReport{
					Path:          path,
					Status:        "conflict",
					ConflictCount: 1,
				})
				report.HasConflicts = true
				report.TotalConflicts++
				mergedFiles = append(mergedFiles, mergedFile{
					path:    path,
					content: content,
					mode:    normalizeFileMode(theirsMap[path].Mode),
				})
			}

		case !inBase && inOurs && !inTheirs:
			// Only in ours: keep as-is (cherry-pick doesn't touch this file).
			content, err := r.readBlobData(oursMap[path].BlobHash)
			if err != nil {
				return nil, fmt.Errorf("cherry-pick: read %q: %w", path, err)
			}
			report.Files = append(report.Files, FileMergeReport{Path: path, Status: "added"})
			mergedFiles = append(mergedFiles, mergedFile{
				path:    path,
				content: content,
				mode:    normalizeFileMode(oursMap[path].Mode),
			})

		case !inBase && !inOurs && inTheirs:
			// New in theirs only (added by cherry-picked commit): add.
			content, err := r.readBlobData(theirsMap[path].BlobHash)
			if err != nil {
				return nil, fmt.Errorf("cherry-pick: read %q: %w", path, err)
			}
			report.Files = append(report.Files, FileMergeReport{Path: path, Status: "added"})
			mergedFiles = append(mergedFiles, mergedFile{
				path:    path,
				content: content,
				mode:    normalizeFileMode(theirsMap[path].Mode),
			})

		case inBase && !inOurs && !inTheirs:
			// Both deleted: already gone.
			report.Files = append(report.Files, FileMergeReport{Path: path, Status: "deleted"})
			deletedPaths = append(deletedPaths, path)
		}
	}

	// If there are conflicts, return error without committing.
	if report.HasConflicts {
		var conflictPaths []string
		for _, f := range report.Files {
			if f.Status == "conflict" {
				conflictPaths = append(conflictPaths, f.Path)
			}
		}
		return nil, fmt.Errorf("cherry-pick: conflicts in %s", strings.Join(conflictPaths, ", "))
	}

	// Write merged files to working directory.
	for _, mf := range mergedFiles {
		absPath := filepath.Join(r.RootDir, filepath.FromSlash(mf.path))
		dir := filepath.Dir(absPath)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("cherry-pick: mkdir %q: %w", dir, err)
		}
		if err := os.WriteFile(absPath, mf.content, filePermFromMode(mf.mode)); err != nil {
			return nil, fmt.Errorf("cherry-pick: write %q: %w", mf.path, err)
		}
	}

	// Remove deleted files.
	for _, path := range deletedPaths {
		absPath := filepath.Join(r.RootDir, filepath.FromSlash(path))
		if err := os.Remove(absPath); err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("cherry-pick: remove %q: %w", path, err)
		}
		r.removeEmptyParents(filepath.Dir(absPath))
	}

	// Stage all merged files.
	var pathsToAdd []string
	for _, mf := range mergedFiles {
		pathsToAdd = append(pathsToAdd, mf.path)
	}
	if len(pathsToAdd) > 0 {
		if err := r.Add(pathsToAdd); err != nil {
			return nil, fmt.Errorf("cherry-pick: stage: %w", err)
		}
	}

	// Remove deleted files from staging.
	if len(deletedPaths) > 0 {
		stg, err := r.ReadStaging()
		if err != nil {
			return nil, fmt.Errorf("cherry-pick: read staging: %w", err)
		}
		for _, p := range deletedPaths {
			delete(stg.Entries, p)
		}
		if err := r.WriteStaging(stg); err != nil {
			return nil, fmt.Errorf("cherry-pick: write staging: %w", err)
		}
	}

	// Commit with the original commit's message and author, HEAD as parent.
	author := strings.TrimSpace(targetCommit.Author)
	if author == "" {
		author = "graft-cherry-pick"
	}
	message := targetCommit.Message

	stg, err := r.ReadStaging()
	if err != nil {
		return nil, fmt.Errorf("cherry-pick: read staging: %w", err)
	}
	if len(stg.Entries) == 0 {
		return nil, fmt.Errorf("cherry-pick: nothing staged")
	}

	treeHash, err := r.BuildTree(stg)
	if err != nil {
		return nil, fmt.Errorf("cherry-pick: build tree: %w", err)
	}

	commitObj := &object.CommitObj{
		TreeHash:  treeHash,
		Parents:   []object.Hash{headHash},
		Author:    author,
		Timestamp: time.Now().Unix(),
		Message:   message,
	}

	commitHash, err := r.Store.WriteCommit(commitObj)
	if err != nil {
		return nil, fmt.Errorf("cherry-pick: write commit: %w", err)
	}

	// Update current branch ref.
	head, err := r.Head()
	if err != nil {
		return nil, fmt.Errorf("cherry-pick: read HEAD: %w", err)
	}
	if strings.HasPrefix(head, "refs/") {
		if err := r.UpdateRefCAS(head, commitHash, headHash); err != nil {
			return nil, fmt.Errorf("cherry-pick: update ref %q: %w", head, err)
		}
	} else {
		if err := r.UpdateRefCAS("HEAD", commitHash, headHash); err != nil {
			return nil, fmt.Errorf("cherry-pick: update detached HEAD: %w", err)
		}
	}

	r.invalidateStatusCache()

	return &CherryPickResult{
		TargetCommit: targetHash,
		CommitHash:   commitHash,
		Message:      message,
	}, nil
}
