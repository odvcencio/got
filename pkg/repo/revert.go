package repo

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/odvcencio/graft/pkg/object"
)

// RevertResult captures the outcome of a revert operation.
type RevertResult struct {
	TargetCommit object.Hash
	CommitHash   object.Hash
	Message      string
}

// ErrRevertConflict is returned when a revert produces merge conflicts.
type ErrRevertConflict struct {
	TargetHash object.Hash
	Details    string
}

func (e *ErrRevertConflict) Error() string {
	return fmt.Sprintf("revert: conflicts reverting %s: %s\nfix conflicts and run 'graft revert --continue'", shortHash(e.TargetHash), e.Details)
}

// IsRevertInProgress returns true if a revert operation is paused due to conflicts.
func (r *Repo) IsRevertInProgress() bool {
	return r.revertSeq().IsActive()
}

// Revert creates a new commit that undoes the changes introduced by the target
// commit. This is the inverse of cherry-pick: instead of applying the diff
// parent->target onto HEAD, it applies the diff target->parent onto HEAD.
//
// In three-way merge terms:
//   - Base   = target commit's tree (the state we're reverting FROM)
//   - Ours   = HEAD's tree (current state)
//   - Theirs = target's parent tree (the state we're reverting TO)
func (r *Repo) Revert(targetHash object.Hash) (*RevertResult, error) {
	targetHash = object.Hash(strings.TrimSpace(string(targetHash)))
	if targetHash == "" {
		return nil, fmt.Errorf("revert: target commit is required")
	}

	// Read the target commit.
	targetCommit, err := r.Store.ReadCommit(targetHash)
	if err != nil {
		return nil, fmt.Errorf("revert: read target commit %s: %w", targetHash, err)
	}

	// Target must have at least one parent (cannot revert a root commit).
	if len(targetCommit.Parents) == 0 {
		return nil, fmt.Errorf("revert: commit %s has no parent; cannot revert a root commit", targetHash)
	}
	parentHash := targetCommit.Parents[0]

	// Read the parent commit.
	parentCommit, err := r.Store.ReadCommit(parentHash)
	if err != nil {
		return nil, fmt.Errorf("revert: read parent commit %s: %w", parentHash, err)
	}

	// Resolve HEAD.
	headHash, err := r.ResolveRef("HEAD")
	if err != nil {
		return nil, fmt.Errorf("revert: resolve HEAD: %w", err)
	}
	headCommit, err := r.Store.ReadCommit(headHash)
	if err != nil {
		return nil, fmt.Errorf("revert: read HEAD commit %s: %w", headHash, err)
	}

	// Flatten all three trees.
	// Revert swaps base and theirs compared to cherry-pick:
	//   base=target's tree, ours=HEAD's tree, theirs=parent's tree
	baseFiles, err := r.FlattenTree(targetCommit.TreeHash)
	if err != nil {
		return nil, fmt.Errorf("revert: flatten base tree: %w", err)
	}
	oursFiles, err := r.FlattenTree(headCommit.TreeHash)
	if err != nil {
		return nil, fmt.Errorf("revert: flatten ours tree: %w", err)
	}
	theirsFiles, err := r.FlattenTree(parentCommit.TreeHash)
	if err != nil {
		return nil, fmt.Errorf("revert: flatten theirs tree: %w", err)
	}

	// Index files by path.
	baseMap := indexByPath(baseFiles)
	oursMap := indexByPath(oursFiles)
	theirsMap := indexByPath(theirsFiles)

	// Use the shared three-way merge helper.
	mergeResult, err := r.threeWayTreeMerge(baseMap, oursMap, theirsMap)
	if err != nil {
		return nil, fmt.Errorf("revert: %w", err)
	}

	// Apply results to the working directory.
	if err := r.applyThreeWayResult(mergeResult); err != nil {
		return nil, fmt.Errorf("revert: %w", err)
	}

	// If there are conflicts, save sequencer state and return error.
	if mergeResult.HasConflicts {
		// Stage the written files for the conflict state.
		var pathsToStage []string
		for _, f := range mergeResult.Files {
			if f.Status != "unchanged" && f.Status != "deleted" {
				pathsToStage = append(pathsToStage, f.Path)
			}
		}
		if len(pathsToStage) > 0 {
			if stageErr := r.Add(pathsToStage); stageErr != nil {
				return nil, fmt.Errorf("revert: stage conflicts: %w", stageErr)
			}
		}

		if err := r.writeRevertState(targetHash, headHash); err != nil {
			return nil, fmt.Errorf("revert: save state: %w", err)
		}

		return nil, &ErrRevertConflict{
			TargetHash: targetHash,
			Details:    fmt.Sprintf("conflict in: %s", mergeResult.conflictDetailsString()),
		}
	}

	// Stage merge results and remove deleted paths.
	if err := r.stageMergeResult(mergeResult); err != nil {
		return nil, fmt.Errorf("revert: %w", err)
	}

	message := fmt.Sprintf("Revert %q", commitTitle(targetCommit.Message))
	author := r.ResolveAuthor()

	commitHash, err := r.commitFromStaging(commitStagingParams{
		Message:  message,
		Author:   author,
		Parents:  []object.Hash{headHash},
		HeadHash: headHash,
	})
	if err != nil {
		return nil, fmt.Errorf("revert: %w", err)
	}

	return &RevertResult{
		TargetCommit: targetHash,
		CommitHash:   commitHash,
		Message:      message,
	}, nil
}

// RevertContinue resumes a paused revert after the user has resolved conflicts
// and staged the resolution.
func (r *Repo) RevertContinue() (*RevertResult, error) {
	if !r.IsRevertInProgress() {
		return nil, fmt.Errorf("revert: no revert in progress")
	}

	seq := r.revertSeq()

	targetHashStr, err := seq.ReadFile("target-hash")
	if err != nil {
		return nil, fmt.Errorf("revert continue: read target-hash: %w", err)
	}
	targetHash := object.Hash(targetHashStr)

	// Read the target commit to build the revert message.
	targetCommit, err := r.Store.ReadCommit(targetHash)
	if err != nil {
		return nil, fmt.Errorf("revert continue: read target commit %s: %w", targetHash, err)
	}

	// Read saved head-name and verify HEAD hasn't moved to a different branch.
	headName, _ := seq.ReadFile("head-name")
	currentHead, err := r.Head()
	if err == nil && strings.TrimSpace(currentHead) != headName {
		return nil, fmt.Errorf("revert continue: HEAD has moved since revert started (expected %s, got %s); abort and retry", headName, currentHead)
	}

	headHash, err := r.ResolveRef("HEAD")
	if err != nil {
		return nil, fmt.Errorf("revert continue: resolve HEAD: %w", err)
	}

	message := fmt.Sprintf("Revert %q", commitTitle(targetCommit.Message))
	author := r.ResolveAuthor()

	commitHash, err := r.commitFromStaging(commitStagingParams{
		Message:  message,
		Author:   author,
		Parents:  []object.Hash{headHash},
		HeadName: headName,
		HeadHash: headHash,
	})
	if err != nil {
		return nil, fmt.Errorf("revert continue: %w", err)
	}

	// Clean up sequencer state.
	if err := seq.Clean(); err != nil {
		return nil, fmt.Errorf("revert continue: cleanup: %w", err)
	}

	return &RevertResult{
		TargetCommit: targetHash,
		CommitHash:   commitHash,
		Message:      message,
	}, nil
}

// RevertAbort cancels the current revert and restores the original state.
func (r *Repo) RevertAbort() error {
	if !r.IsRevertInProgress() {
		return fmt.Errorf("revert: no revert in progress")
	}

	seq := r.revertSeq()

	origHeadStr, err := seq.ReadFile("orig-head")
	if err != nil {
		return fmt.Errorf("revert abort: read orig-head: %w", err)
	}
	origHead := object.Hash(origHeadStr)

	headName, err := seq.ReadFile("head-name")
	if err != nil {
		return fmt.Errorf("revert abort: read head-name: %w", err)
	}

	// Restore the branch ref to orig-head.
	if strings.HasPrefix(headName, "refs/heads/") {
		if err := r.UpdateRef(headName, origHead); err != nil {
			return fmt.Errorf("revert abort: restore branch ref: %w", err)
		}
	}

	// Restore HEAD.
	headPath := filepath.Join(r.GraftDir, "HEAD")
	if strings.HasPrefix(headName, "refs/") {
		if err := os.WriteFile(headPath, []byte("ref: "+headName+"\n"), 0o644); err != nil {
			return fmt.Errorf("revert abort: reattach HEAD: %w", err)
		}
	} else {
		if err := os.WriteFile(headPath, []byte(string(origHead)+"\n"), 0o644); err != nil {
			return fmt.Errorf("revert abort: set HEAD: %w", err)
		}
	}

	// Checkout the original tree.
	origCommit, err := r.Store.ReadCommit(origHead)
	if err != nil {
		return fmt.Errorf("revert abort: read orig commit: %w", err)
	}
	if err := r.checkoutTree(origCommit); err != nil {
		return fmt.Errorf("revert abort: checkout: %w", err)
	}

	// Clean up sequencer state.
	if err := seq.Clean(); err != nil {
		return fmt.Errorf("revert abort: cleanup: %w", err)
	}

	return nil
}

// writeRevertState saves the sequencer state for a paused revert.
func (r *Repo) writeRevertState(targetHash, origHead object.Hash) error {
	seq := r.revertSeq()
	if err := seq.Init(); err != nil {
		return fmt.Errorf("mkdir %q: %w", seq.Dir(), err)
	}

	headName := "HEAD"
	head, err := r.Head()
	if err == nil && strings.HasPrefix(head, "refs/") {
		headName = head
	}

	files := map[string]string{
		"target-hash": string(targetHash) + "\n",
		"orig-head":   string(origHead) + "\n",
		"head-name":   headName + "\n",
	}

	return seq.WriteFiles(files)
}
