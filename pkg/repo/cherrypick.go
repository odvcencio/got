package repo

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/odvcencio/graft/pkg/object"
)

// NOTE: cherry-pick sequencer state is managed via r.cherryPickSeq() defined
// in sequencer.go. The helpers below delegate to that shared abstraction.

// CherryPickResult captures the outcome of a commit-level cherry-pick.
type CherryPickResult struct {
	TargetCommit object.Hash
	CommitHash   object.Hash
	Message      string
}

// ErrCherryPickConflict is returned when cherry-pick encounters merge conflicts.
// The sequencer state has been saved so the user can resolve and --continue.
type ErrCherryPickConflict struct {
	TargetHash object.Hash
	Details    string
}

func (e *ErrCherryPickConflict) Error() string {
	return fmt.Sprintf("cherry-pick: conflicts applying %s: %s\nfix conflicts and run 'graft cherry-pick --continue'", shortHash(e.TargetHash), e.Details)
}

// ErrNoCherryPickInProgress is returned when --continue/--abort/--skip is
// called with no active cherry-pick.
var ErrNoCherryPickInProgress = fmt.Errorf("cherry-pick: no cherry-pick in progress")

// IsCherryPickInProgress returns true if a cherry-pick is currently paused
// (waiting for conflict resolution).
func (r *Repo) IsCherryPickInProgress() bool {
	return r.cherryPickSeq().IsActive()
}

// saveCherryPickState saves sequencer state so the user can resolve conflicts
// and then run --continue, --abort, or --skip.
func (r *Repo) saveCherryPickState(targetHash object.Hash, origHead object.Hash, headName string) error {
	seq := r.cherryPickSeq()
	if err := seq.Init(); err != nil {
		return fmt.Errorf("cherry-pick: mkdir %q: %w", seq.Dir(), err)
	}

	files := map[string]string{
		"target-hash": string(targetHash) + "\n",
		"orig-head":   string(origHead) + "\n",
		"head-name":   headName + "\n",
	}

	if err := seq.WriteFiles(files); err != nil {
		return fmt.Errorf("cherry-pick: %w", err)
	}
	return nil
}

// cleanCherryPickState removes the cherry-pick sequencer directory.
func (r *Repo) cleanCherryPickState() error {
	return r.cherryPickSeq().Clean()
}

// CherryPick applies the changes introduced by the given commit onto HEAD
// using a three-way structural merge.
//
// Algorithm:
//  1. Read the target commit and its first parent
//  2. Three-way merge: base=parent's tree, ours=HEAD's tree, theirs=target's tree
//  3. If clean: auto-commit with the original commit's message and author, HEAD as parent
//  4. If conflicts: save sequencer state and return *ErrCherryPickConflict
func (r *Repo) CherryPick(targetHash object.Hash) (*CherryPickResult, error) {
	targetHash = object.Hash(strings.TrimSpace(string(targetHash)))
	if targetHash == "" {
		return nil, fmt.Errorf("cherry-pick: target commit is required")
	}

	if r.IsCherryPickInProgress() {
		return nil, fmt.Errorf("cherry-pick: a cherry-pick is already in progress; use --continue, --abort, or --skip")
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

	// If there are conflicts, save state and return typed error.
	if mergeResult.HasConflicts {
		// Stage the conflicted files so the user can see them and resolve.
		var pathsToStage []string
		for _, f := range mergeResult.Files {
			if f.Status != "unchanged" && f.Status != "deleted" {
				pathsToStage = append(pathsToStage, f.Path)
			}
		}
		if len(pathsToStage) > 0 {
			if err := r.Add(pathsToStage); err != nil {
				return nil, fmt.Errorf("cherry-pick: stage conflicts: %w", err)
			}
		}

		// Save sequencer state.
		headName, err := r.Head()
		if err != nil {
			headName = "HEAD"
		}
		if err := r.saveCherryPickState(targetHash, headHash, headName); err != nil {
			return nil, err
		}

		return nil, &ErrCherryPickConflict{
			TargetHash: targetHash,
			Details:    mergeResult.conflictDetailsString(),
		}
	}

	// Stage merge results and remove deleted paths.
	if err := r.stageMergeResult(mergeResult); err != nil {
		return nil, fmt.Errorf("cherry-pick: %w", err)
	}

	// Commit with the original commit's message and author.
	author := strings.TrimSpace(targetCommit.Author)
	if author == "" {
		author = "graft-cherry-pick"
	}

	commitHash, err := r.commitFromStaging(commitStagingParams{
		Message:  targetCommit.Message,
		Author:   author,
		Parents:  []object.Hash{headHash},
		HeadHash: headHash,
	})
	if err != nil {
		return nil, fmt.Errorf("cherry-pick: %w", err)
	}

	return &CherryPickResult{
		TargetCommit: targetHash,
		CommitHash:   commitHash,
		Message:      targetCommit.Message,
	}, nil
}

// CherryPickContinue resumes a paused cherry-pick after the user has resolved
// conflicts and staged the resolution. It commits with the original commit's
// message and author.
func (r *Repo) CherryPickContinue() (*CherryPickResult, error) {
	if !r.IsCherryPickInProgress() {
		return nil, ErrNoCherryPickInProgress
	}

	seq := r.cherryPickSeq()

	// Read the target hash from sequencer state.
	targetHashStr, err := seq.ReadFile("target-hash")
	if err != nil {
		return nil, fmt.Errorf("cherry-pick continue: read target-hash: %w", err)
	}
	targetHash := object.Hash(targetHashStr)

	// Read the original commit for message and author.
	targetCommit, err := r.Store.ReadCommit(targetHash)
	if err != nil {
		return nil, fmt.Errorf("cherry-pick continue: read target commit %s: %w", targetHash, err)
	}

	// Read saved head-name and verify HEAD hasn't moved to a different branch.
	headName, _ := seq.ReadFile("head-name")
	currentHead, err := r.Head()
	if err == nil && strings.TrimSpace(currentHead) != headName {
		return nil, fmt.Errorf("cherry-pick continue: HEAD has moved since cherry-pick started (expected %s, got %s); abort and retry", headName, currentHead)
	}

	// Resolve current HEAD.
	headHash, err := r.ResolveRef("HEAD")
	if err != nil {
		return nil, fmt.Errorf("cherry-pick continue: resolve HEAD: %w", err)
	}

	// Read current staging (user should have resolved conflicts and staged).
	stg, err := r.ReadStaging()
	if err != nil {
		return nil, fmt.Errorf("cherry-pick continue: read staging: %w", err)
	}
	if len(stg.Entries) == 0 {
		return nil, fmt.Errorf("cherry-pick continue: nothing staged")
	}

	// Check for unresolved conflicts in staging.
	for _, entry := range stg.Entries {
		if entry.Conflict {
			return nil, fmt.Errorf("cherry-pick continue: unresolved conflicts remain; resolve them and stage the files")
		}
	}

	// Commit with the original commit's message and author.
	author := strings.TrimSpace(targetCommit.Author)
	if author == "" {
		author = "graft-cherry-pick"
	}

	commitHash, err := r.commitFromStaging(commitStagingParams{
		Message:  targetCommit.Message,
		Author:   author,
		Parents:  []object.Hash{headHash},
		HeadName: headName,
		HeadHash: headHash,
	})
	if err != nil {
		return nil, fmt.Errorf("cherry-pick continue: %w", err)
	}

	// Clean up sequencer state.
	if err := r.cleanCherryPickState(); err != nil {
		return nil, fmt.Errorf("cherry-pick continue: cleanup: %w", err)
	}

	return &CherryPickResult{
		TargetCommit: targetHash,
		CommitHash:   commitHash,
		Message:      targetCommit.Message,
	}, nil
}

// CherryPickAbort cancels the in-progress cherry-pick and restores the
// working tree to the state before the cherry-pick started.
func (r *Repo) CherryPickAbort() error {
	if !r.IsCherryPickInProgress() {
		return ErrNoCherryPickInProgress
	}

	seq := r.cherryPickSeq()

	// Read original HEAD.
	origHeadStr, err := seq.ReadFile("orig-head")
	if err != nil {
		return fmt.Errorf("cherry-pick abort: read orig-head: %w", err)
	}
	origHead := object.Hash(origHeadStr)

	// Read head-name.
	headName, err := seq.ReadFile("head-name")
	if err != nil {
		return fmt.Errorf("cherry-pick abort: read head-name: %w", err)
	}

	// Checkout the original commit's tree to restore working directory.
	origCommit, err := r.Store.ReadCommit(origHead)
	if err != nil {
		return fmt.Errorf("cherry-pick abort: read orig commit: %w", err)
	}
	if err := r.checkoutTree(origCommit); err != nil {
		return fmt.Errorf("cherry-pick abort: checkout: %w", err)
	}

	// Restore the branch ref to original HEAD.
	if strings.HasPrefix(headName, "refs/heads/") {
		if err := r.UpdateRef(headName, origHead); err != nil {
			return fmt.Errorf("cherry-pick abort: restore branch ref: %w", err)
		}
	}

	// Reattach HEAD to the branch (or set detached HEAD).
	headPath := filepath.Join(r.GraftDir, "HEAD")
	if strings.HasPrefix(headName, "refs/") {
		if err := os.WriteFile(headPath, []byte("ref: "+headName+"\n"), 0o644); err != nil {
			return fmt.Errorf("cherry-pick abort: reattach HEAD: %w", err)
		}
	} else {
		if err := os.WriteFile(headPath, []byte(string(origHead)+"\n"), 0o644); err != nil {
			return fmt.Errorf("cherry-pick abort: set HEAD: %w", err)
		}
	}

	r.invalidateStatusCache()

	// Clean up sequencer state.
	if err := r.cleanCherryPickState(); err != nil {
		return fmt.Errorf("cherry-pick abort: cleanup: %w", err)
	}

	return nil
}

// CherryPickSkip discards the current conflicted cherry-pick state and resets
// the working tree to the current HEAD.
func (r *Repo) CherryPickSkip() error {
	if !r.IsCherryPickInProgress() {
		return ErrNoCherryPickInProgress
	}

	// Reset working tree to current HEAD so we discard the conflict state.
	headHash, err := r.ResolveRef("HEAD")
	if err != nil {
		return fmt.Errorf("cherry-pick skip: resolve HEAD: %w", err)
	}
	headCommit, err := r.Store.ReadCommit(headHash)
	if err != nil {
		return fmt.Errorf("cherry-pick skip: read HEAD commit: %w", err)
	}
	if err := r.checkoutTree(headCommit); err != nil {
		return fmt.Errorf("cherry-pick skip: reset tree: %w", err)
	}

	r.invalidateStatusCache()

	// Clean up sequencer state.
	if err := r.cleanCherryPickState(); err != nil {
		return fmt.Errorf("cherry-pick skip: cleanup: %w", err)
	}

	return nil
}
