package repo

import (
	"fmt"
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

	// Use the shared three-way merge helper.
	mergeResult, err := r.threeWayTreeMerge(baseMap, oursMap, theirsMap)
	if err != nil {
		return nil, fmt.Errorf("cherry-pick: %w", err)
	}

	// Apply results to the working directory.
	if err := r.applyThreeWayResult(mergeResult); err != nil {
		return nil, fmt.Errorf("cherry-pick: %w", err)
	}

	// If there are conflicts, return error without committing.
	if mergeResult.HasConflicts {
		return nil, fmt.Errorf("cherry-pick: conflicts in %s", mergeResult.conflictDetailsString())
	}

	// Stage all changed/added files.
	var pathsToAdd []string
	for _, f := range mergeResult.Files {
		if f.Status != "unchanged" && f.Status != "deleted" {
			pathsToAdd = append(pathsToAdd, f.Path)
		}
	}
	if len(pathsToAdd) > 0 {
		if err := r.Add(pathsToAdd); err != nil {
			return nil, fmt.Errorf("cherry-pick: stage: %w", err)
		}
	}

	// Remove deleted files from staging.
	if len(mergeResult.DeletedPaths) > 0 {
		stg, err := r.ReadStaging()
		if err != nil {
			return nil, fmt.Errorf("cherry-pick: read staging: %w", err)
		}
		for _, p := range mergeResult.DeletedPaths {
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
