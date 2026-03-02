package repo

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

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

// revertDir returns the path to the revert sequencer state directory.
func (r *Repo) revertDir() string {
	return filepath.Join(r.GraftDir, "revert")
}

// IsRevertInProgress returns true if a revert operation is paused due to conflicts.
func (r *Repo) IsRevertInProgress() bool {
	_, err := os.Stat(r.revertDir())
	return err == nil
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

	// Stage all changed/added files.
	var pathsToAdd []string
	for _, f := range mergeResult.Files {
		if f.Status != "unchanged" && f.Status != "deleted" {
			pathsToAdd = append(pathsToAdd, f.Path)
		}
	}
	if len(pathsToAdd) > 0 {
		if err := r.Add(pathsToAdd); err != nil {
			return nil, fmt.Errorf("revert: stage: %w", err)
		}
	}

	// Remove deleted files from staging.
	if len(mergeResult.DeletedPaths) > 0 {
		stg, err := r.ReadStaging()
		if err != nil {
			return nil, fmt.Errorf("revert: read staging: %w", err)
		}
		for _, p := range mergeResult.DeletedPaths {
			delete(stg.Entries, p)
		}
		if err := r.WriteStaging(stg); err != nil {
			return nil, fmt.Errorf("revert: write staging: %w", err)
		}
	}

	// Build revert commit message.
	message := fmt.Sprintf("Revert %q", commitTitle(targetCommit.Message))

	// Commit with current user info; use HEAD's author as fallback.
	author := strings.TrimSpace(headCommit.Author)
	if author == "" {
		author = "graft-revert"
	}

	stg, err := r.ReadStaging()
	if err != nil {
		return nil, fmt.Errorf("revert: read staging: %w", err)
	}
	if len(stg.Entries) == 0 {
		return nil, fmt.Errorf("revert: nothing staged")
	}

	treeHash, err := r.BuildTree(stg)
	if err != nil {
		return nil, fmt.Errorf("revert: build tree: %w", err)
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
		return nil, fmt.Errorf("revert: write commit: %w", err)
	}

	// Update current branch ref.
	head, err := r.Head()
	if err != nil {
		return nil, fmt.Errorf("revert: read HEAD: %w", err)
	}
	if strings.HasPrefix(head, "refs/") {
		if err := r.UpdateRefCAS(head, commitHash, headHash); err != nil {
			return nil, fmt.Errorf("revert: update ref %q: %w", head, err)
		}
	} else {
		if err := r.UpdateRefCAS("HEAD", commitHash, headHash); err != nil {
			return nil, fmt.Errorf("revert: update detached HEAD: %w", err)
		}
	}

	r.invalidateStatusCache()

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

	targetHashStr, err := r.readRevertFile("target-hash")
	if err != nil {
		return nil, fmt.Errorf("revert continue: read target-hash: %w", err)
	}
	targetHash := object.Hash(strings.TrimSpace(targetHashStr))

	// Read the target commit to build the revert message.
	targetCommit, err := r.Store.ReadCommit(targetHash)
	if err != nil {
		return nil, fmt.Errorf("revert continue: read target commit %s: %w", targetHash, err)
	}

	headHash, err := r.ResolveRef("HEAD")
	if err != nil {
		return nil, fmt.Errorf("revert continue: resolve HEAD: %w", err)
	}
	headCommit, err := r.Store.ReadCommit(headHash)
	if err != nil {
		return nil, fmt.Errorf("revert continue: read HEAD commit: %w", err)
	}

	message := fmt.Sprintf("Revert %q", commitTitle(targetCommit.Message))

	author := strings.TrimSpace(headCommit.Author)
	if author == "" {
		author = "graft-revert"
	}

	stg, err := r.ReadStaging()
	if err != nil {
		return nil, fmt.Errorf("revert continue: read staging: %w", err)
	}
	if len(stg.Entries) == 0 {
		return nil, fmt.Errorf("revert continue: nothing staged")
	}

	treeHash, err := r.BuildTree(stg)
	if err != nil {
		return nil, fmt.Errorf("revert continue: build tree: %w", err)
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
		return nil, fmt.Errorf("revert continue: write commit: %w", err)
	}

	// Update current branch ref.
	headName, _ := r.readRevertFile("head-name")
	headName = strings.TrimSpace(headName)
	if strings.HasPrefix(headName, "refs/") {
		if err := r.UpdateRefCAS(headName, commitHash, headHash); err != nil {
			return nil, fmt.Errorf("revert continue: update ref %q: %w", headName, err)
		}
	} else {
		if err := r.UpdateRefCAS("HEAD", commitHash, headHash); err != nil {
			return nil, fmt.Errorf("revert continue: update detached HEAD: %w", err)
		}
	}

	r.invalidateStatusCache()

	// Clean up sequencer state.
	if err := os.RemoveAll(r.revertDir()); err != nil {
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

	origHeadStr, err := r.readRevertFile("orig-head")
	if err != nil {
		return fmt.Errorf("revert abort: read orig-head: %w", err)
	}
	origHead := object.Hash(strings.TrimSpace(origHeadStr))

	headName, err := r.readRevertFile("head-name")
	if err != nil {
		return fmt.Errorf("revert abort: read head-name: %w", err)
	}
	headName = strings.TrimSpace(headName)

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
	if err := os.RemoveAll(r.revertDir()); err != nil {
		return fmt.Errorf("revert abort: cleanup: %w", err)
	}

	return nil
}

// writeRevertState saves the sequencer state for a paused revert.
func (r *Repo) writeRevertState(targetHash, origHead object.Hash) error {
	dir := r.revertDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %q: %w", dir, err)
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

	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return fmt.Errorf("write %q: %w", name, err)
		}
	}

	return nil
}

// readRevertFile reads a file from the revert sequencer state directory.
func (r *Repo) readRevertFile(name string) (string, error) {
	data, err := os.ReadFile(filepath.Join(r.revertDir(), name))
	if err != nil {
		return "", err
	}
	return string(data), nil
}
