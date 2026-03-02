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

// cherryPickDir returns the path to the cherry-pick sequencer state directory.
func (r *Repo) cherryPickDir() string {
	return filepath.Join(r.GraftDir, "cherry-pick")
}

// IsCherryPickInProgress returns true if a cherry-pick is currently paused
// (waiting for conflict resolution).
func (r *Repo) IsCherryPickInProgress() bool {
	_, err := os.Stat(r.cherryPickDir())
	return err == nil
}

// readCherryPickFile reads a file from the cherry-pick sequencer directory.
func (r *Repo) readCherryPickFile(name string) (string, error) {
	data, err := os.ReadFile(filepath.Join(r.cherryPickDir(), name))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// writeCherryPickFileAtomic writes a file to the cherry-pick directory using
// temp file + rename for atomicity.
func (r *Repo) writeCherryPickFileAtomic(name, content string) error {
	dir := r.cherryPickDir()
	target := filepath.Join(dir, name)

	tmp, err := os.CreateTemp(dir, name+".tmp.*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()

	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return os.Rename(tmpPath, target)
}

// saveCherryPickState saves sequencer state so the user can resolve conflicts
// and then run --continue, --abort, or --skip.
func (r *Repo) saveCherryPickState(targetHash object.Hash, origHead object.Hash, headName string) error {
	dir := r.cherryPickDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("cherry-pick: mkdir %q: %w", dir, err)
	}

	files := map[string]string{
		"target-hash": string(targetHash) + "\n",
		"orig-head":   string(origHead) + "\n",
		"head-name":   headName + "\n",
	}

	for name, content := range files {
		if err := r.writeCherryPickFileAtomic(name, content); err != nil {
			return fmt.Errorf("cherry-pick: write %q: %w", name, err)
		}
	}
	return nil
}

// cleanCherryPickState removes the cherry-pick sequencer directory.
func (r *Repo) cleanCherryPickState() error {
	return os.RemoveAll(r.cherryPickDir())
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

// CherryPickContinue resumes a paused cherry-pick after the user has resolved
// conflicts and staged the resolution. It commits with the original commit's
// message and author.
func (r *Repo) CherryPickContinue() (*CherryPickResult, error) {
	if !r.IsCherryPickInProgress() {
		return nil, ErrNoCherryPickInProgress
	}

	// Read the target hash from sequencer state.
	targetHashStr, err := r.readCherryPickFile("target-hash")
	if err != nil {
		return nil, fmt.Errorf("cherry-pick continue: read target-hash: %w", err)
	}
	targetHash := object.Hash(strings.TrimSpace(targetHashStr))

	// Read the original commit for message and author.
	targetCommit, err := r.Store.ReadCommit(targetHash)
	if err != nil {
		return nil, fmt.Errorf("cherry-pick continue: read target commit %s: %w", targetHash, err)
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

	// Build the tree from staging.
	treeHash, err := r.BuildTree(stg)
	if err != nil {
		return nil, fmt.Errorf("cherry-pick continue: build tree: %w", err)
	}

	// Commit with the original commit's message and author.
	author := strings.TrimSpace(targetCommit.Author)
	if author == "" {
		author = "graft-cherry-pick"
	}
	message := targetCommit.Message

	commitObj := &object.CommitObj{
		TreeHash:  treeHash,
		Parents:   []object.Hash{headHash},
		Author:    author,
		Timestamp: time.Now().Unix(),
		Message:   message,
	}

	commitHash, err := r.Store.WriteCommit(commitObj)
	if err != nil {
		return nil, fmt.Errorf("cherry-pick continue: write commit: %w", err)
	}

	// Update current branch ref.
	head, err := r.Head()
	if err != nil {
		return nil, fmt.Errorf("cherry-pick continue: read HEAD: %w", err)
	}
	if strings.HasPrefix(head, "refs/") {
		if err := r.UpdateRefCAS(head, commitHash, headHash); err != nil {
			return nil, fmt.Errorf("cherry-pick continue: update ref %q: %w", head, err)
		}
	} else {
		if err := r.UpdateRefCAS("HEAD", commitHash, headHash); err != nil {
			return nil, fmt.Errorf("cherry-pick continue: update detached HEAD: %w", err)
		}
	}

	r.invalidateStatusCache()

	// Clean up sequencer state.
	if err := r.cleanCherryPickState(); err != nil {
		return nil, fmt.Errorf("cherry-pick continue: cleanup: %w", err)
	}

	return &CherryPickResult{
		TargetCommit: targetHash,
		CommitHash:   commitHash,
		Message:      message,
	}, nil
}

// CherryPickAbort cancels the in-progress cherry-pick and restores the
// working tree to the state before the cherry-pick started.
func (r *Repo) CherryPickAbort() error {
	if !r.IsCherryPickInProgress() {
		return ErrNoCherryPickInProgress
	}

	// Read original HEAD.
	origHeadStr, err := r.readCherryPickFile("orig-head")
	if err != nil {
		return fmt.Errorf("cherry-pick abort: read orig-head: %w", err)
	}
	origHead := object.Hash(strings.TrimSpace(origHeadStr))

	// Read head-name.
	headName, err := r.readCherryPickFile("head-name")
	if err != nil {
		return fmt.Errorf("cherry-pick abort: read head-name: %w", err)
	}
	headName = strings.TrimSpace(headName)

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
