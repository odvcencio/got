package repo

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/odvcencio/graft/pkg/object"
)

// ErrRebaseConflict is returned when a cherry-pick during rebase produces a conflict.
type ErrRebaseConflict struct {
	CommitHash object.Hash
	Message    string
	Details    string
}

func (e *ErrRebaseConflict) Error() string {
	return fmt.Sprintf("rebase: conflict applying %s: %s", shortHash(e.CommitHash), e.Details)
}

// ErrRebaseInProgress is returned when starting a rebase while one is already active.
var ErrRebaseInProgress = fmt.Errorf("rebase: a rebase is already in progress")

// ErrNoRebaseInProgress is returned when continue/abort/skip is called with no active rebase.
var ErrNoRebaseInProgress = fmt.Errorf("rebase: no rebase in progress")

// RebaseOptions holds optional configuration for a rebase operation.
type RebaseOptions struct {
	// Autostash automatically stashes uncommitted changes before rebase
	// and restores them after completion (or abort).
	Autostash bool
}

// rebaseMergeDir returns the path to the sequencer state directory.
// Used by rebase_interactive.go; delegates to rebaseSeq().
func (r *Repo) rebaseMergeDir() string {
	return r.rebaseSeq().Dir()
}

// isRebaseInProgress checks if a rebase is currently active.
func (r *Repo) isRebaseInProgress() bool {
	return r.rebaseSeq().IsActive()
}

// Rebase replays commits from the current branch onto the given upstream.
//
// Algorithm:
//  1. Resolve upstream to a commit hash
//  2. Resolve HEAD to a commit hash
//  3. Find merge base between HEAD and upstream
//  4. If HEAD equals upstream or merge base equals HEAD: no-op (already up to date)
//  5. Collect commits from merge-base..HEAD (oldest first)
//  6. Save sequencer state
//  7. Detach HEAD to upstream
//  8. Cherry-pick each commit; on conflict, save position and return error
//  9. Update original branch ref and reattach HEAD
//  10. Clean up sequencer state
func (r *Repo) Rebase(upstream string) error {
	return r.RebaseWithOptions(upstream, RebaseOptions{})
}

// RebaseWithOptions replays commits from the current branch onto the given
// upstream, using the provided options.
func (r *Repo) RebaseWithOptions(upstream string, opts RebaseOptions) error {
	if r.isRebaseInProgress() {
		return ErrRebaseInProgress
	}

	// Handle autostash: stash dirty changes before starting.
	if opts.Autostash {
		if err := r.autostashSave(); err != nil {
			return err
		}
	}

	// 1. Resolve upstream.
	upstreamHash, err := r.resolveToHash(upstream)
	if err != nil {
		return fmt.Errorf("rebase: resolve upstream %q: %w", upstream, err)
	}

	// 2. Resolve HEAD.
	headHash, err := r.ResolveRef("HEAD")
	if err != nil {
		return fmt.Errorf("rebase: resolve HEAD: %w", err)
	}

	// 3. Find merge base.
	mergeBase, err := r.FindMergeBase(headHash, upstreamHash)
	if err != nil {
		return fmt.Errorf("rebase: %w", err)
	}

	// 4. Already up to date?
	if headHash == upstreamHash || mergeBase == headHash {
		// Pop the autostash if we saved one, even though we're a no-op.
		r.autostashPop()
		return nil // no-op
	}

	// 5. Collect commits from merge-base..HEAD (oldest first).
	commits, err := r.collectCommits(mergeBase, headHash)
	if err != nil {
		return fmt.Errorf("rebase: %w", err)
	}
	if len(commits) == 0 {
		r.autostashPop()
		return nil // nothing to replay
	}

	// Determine the branch name to save.
	branchName, err := r.Head()
	if err != nil {
		return fmt.Errorf("rebase: read HEAD: %w", err)
	}

	return r.doRebase(branchName, headHash, upstreamHash, commits)
}

// RebaseOnto replays commits from upstream..HEAD onto newbase.
func (r *Repo) RebaseOnto(newbase, upstream string) error {
	return r.RebaseOntoWithOptions(newbase, upstream, RebaseOptions{})
}

// RebaseOntoWithOptions replays commits from upstream..HEAD onto newbase,
// using the provided options.
func (r *Repo) RebaseOntoWithOptions(newbase, upstream string, opts RebaseOptions) error {
	if r.isRebaseInProgress() {
		return ErrRebaseInProgress
	}

	// Handle autostash: stash dirty changes before starting.
	if opts.Autostash {
		if err := r.autostashSave(); err != nil {
			return err
		}
	}

	// Resolve newbase.
	newbaseHash, err := r.resolveToHash(newbase)
	if err != nil {
		return fmt.Errorf("rebase: resolve newbase %q: %w", newbase, err)
	}

	// Resolve upstream.
	upstreamHash, err := r.resolveToHash(upstream)
	if err != nil {
		return fmt.Errorf("rebase: resolve upstream %q: %w", upstream, err)
	}

	// Resolve HEAD.
	headHash, err := r.ResolveRef("HEAD")
	if err != nil {
		return fmt.Errorf("rebase: resolve HEAD: %w", err)
	}

	// Collect commits from upstream..HEAD (oldest first).
	commits, err := r.collectCommits(upstreamHash, headHash)
	if err != nil {
		return fmt.Errorf("rebase: %w", err)
	}
	if len(commits) == 0 {
		r.autostashPop()
		return nil
	}

	branchName, err := r.Head()
	if err != nil {
		return fmt.Errorf("rebase: read HEAD: %w", err)
	}

	return r.doRebase(branchName, headHash, newbaseHash, commits)
}

// doRebase is the shared implementation for Rebase and RebaseOnto.
func (r *Repo) doRebase(branchName string, origHead, onto object.Hash, commits []object.Hash) error {
	// 6. Save sequencer state.
	if err := r.writeSequencerState(branchName, origHead, onto, commits, nil); err != nil {
		return fmt.Errorf("rebase: save state: %w", err)
	}

	// 7. Detach HEAD to onto.
	if err := r.detachHead(onto); err != nil {
		// Clean up on failure.
		r.rebaseSeq().Clean() //nolint:errcheck
		return fmt.Errorf("rebase: detach HEAD: %w", err)
	}

	// 8. Replay commits.
	return r.replayCommits()
}

// RebaseContinue resumes a paused rebase after the user has resolved conflicts
// and staged the resolution.
func (r *Repo) RebaseContinue() error {
	if !r.isRebaseInProgress() {
		return ErrNoRebaseInProgress
	}

	// Dispatch to the interactive continue path if this is an interactive rebase.
	if r.isInteractiveRebase() {
		return r.RebaseInteractiveContinue()
	}

	seq := r.rebaseSeq()

	stoppedSHA, err := seq.ReadHash("stopped-sha")
	if err != nil {
		return fmt.Errorf("rebase continue: no stopped commit found: %w", err)
	}

	// Read the original commit to preserve its message and author.
	origCommit, err := r.Store.ReadCommit(stoppedSHA)
	if err != nil {
		return fmt.Errorf("rebase continue: read original commit %s: %w", stoppedSHA, err)
	}

	// Resolve current HEAD for CAS.
	headHash, err := r.ResolveRef("HEAD")
	if err != nil {
		return fmt.Errorf("rebase continue: resolve HEAD: %w", err)
	}

	// Stage any remaining changes from conflict resolution.
	stg, err := r.ReadStaging()
	if err != nil {
		return fmt.Errorf("rebase continue: read staging: %w", err)
	}
	if len(stg.Entries) == 0 {
		return fmt.Errorf("rebase continue: nothing staged")
	}
	for _, entry := range stg.Entries {
		if entry.Conflict {
			return fmt.Errorf("rebase continue: unresolved conflicts remain; resolve them and stage the files")
		}
	}

	commitHash, err := r.commitFromStaging(commitStagingParams{
		Message:  origCommit.Message,
		Author:   origCommit.Author,
		Parents:  []object.Hash{headHash},
		HeadHash: headHash,
	})
	if err != nil {
		return fmt.Errorf("rebase continue: %w", err)
	}
	_ = commitHash

	// Move stopped-sha to done.
	done, _ := seq.ReadFile("done")
	if done != "" {
		done += "\n"
	}
	done += string(stoppedSHA) + "\n"
	if err := seq.WriteFile("done", done); err != nil {
		return fmt.Errorf("rebase continue: update done: %w", err)
	}

	// Remove stopped-sha.
	seq.RemoveFile("stopped-sha")

	// Continue replaying remaining commits.
	return r.replayCommits()
}

// RebaseAbort cancels the current rebase and restores the original state.
func (r *Repo) RebaseAbort() error {
	if !r.isRebaseInProgress() {
		return ErrNoRebaseInProgress
	}

	seq := r.rebaseSeq()

	// Check for autostash before reading other state, since we need to
	// restore it after aborting.
	hadAutostash := r.hasAutostash()

	origHead, err := seq.ReadHash("orig-head")
	if err != nil {
		return fmt.Errorf("rebase abort: read orig-head: %w", err)
	}

	headName, err := seq.ReadFile("head-name")
	if err != nil {
		return fmt.Errorf("rebase abort: read head-name: %w", err)
	}

	// Update the branch ref back to orig-head.
	if strings.HasPrefix(headName, "refs/heads/") {
		currentRef, _ := r.ResolveRef(headName)
		if err := r.UpdateRefCAS(headName, origHead, currentRef); err != nil {
			return fmt.Errorf("rebase abort: restore branch ref: %w", err)
		}
	}

	// Reattach HEAD to the branch.
	if strings.HasPrefix(headName, "refs/") {
		if err := r.setHeadSymbolic(headName); err != nil {
			return fmt.Errorf("rebase abort: reattach HEAD: %w", err)
		}
	} else {
		if err := r.setHeadDetached(origHead); err != nil {
			return fmt.Errorf("rebase abort: set HEAD: %w", err)
		}
	}

	// Checkout the original state.
	origCommit, err := r.Store.ReadCommit(origHead)
	if err != nil {
		return fmt.Errorf("rebase abort: read orig commit: %w", err)
	}
	if err := r.checkoutTree(origCommit); err != nil {
		return fmt.Errorf("rebase abort: checkout: %w", err)
	}

	// Restore autostash before cleaning up sequencer state.
	if hadAutostash {
		r.autostashPop()
	}

	// Clean up sequencer state.
	if err := seq.Clean(); err != nil {
		return fmt.Errorf("rebase abort: cleanup: %w", err)
	}

	return nil
}

// RebaseSkip drops the current stopped commit and continues with the next.
func (r *Repo) RebaseSkip() error {
	if !r.isRebaseInProgress() {
		return ErrNoRebaseInProgress
	}

	seq := r.rebaseSeq()

	stoppedSHA, err := seq.ReadHash("stopped-sha")
	if err != nil {
		return fmt.Errorf("rebase skip: no stopped commit found: %w", err)
	}

	// Move stopped-sha to done (skipped).
	done, _ := seq.ReadFile("done")
	if done != "" {
		done += "\n"
	}
	done += string(stoppedSHA) + "\n"
	if err := seq.WriteFile("done", done); err != nil {
		return fmt.Errorf("rebase skip: update done: %w", err)
	}

	// Remove stopped-sha.
	seq.RemoveFile("stopped-sha")

	// Reset working tree to current HEAD so we have a clean state.
	headHash, err := r.ResolveRef("HEAD")
	if err != nil {
		return fmt.Errorf("rebase skip: resolve HEAD: %w", err)
	}
	headCommit, err := r.Store.ReadCommit(headHash)
	if err != nil {
		return fmt.Errorf("rebase skip: read HEAD commit: %w", err)
	}
	if err := r.checkoutTree(headCommit); err != nil {
		return fmt.Errorf("rebase skip: reset tree: %w", err)
	}

	// Continue replaying.
	return r.replayCommits()
}

// resolveToHash resolves a target string (branch name or hash) to a commit hash.
func (r *Repo) resolveToHash(target string) (object.Hash, error) {
	// Try as branch ref first.
	h, err := r.ResolveRef("refs/heads/" + target)
	if err == nil {
		return h, nil
	}
	// Try as full ref path.
	h, err = r.ResolveRef(target)
	if err == nil {
		return h, nil
	}
	// Try as raw hash — validate format and verify it exists.
	if object.ValidateHash(target) == nil {
		if _, err = r.Store.ReadCommit(object.Hash(target)); err == nil {
			return object.Hash(target), nil
		}
	}
	return "", fmt.Errorf("cannot resolve %q to a commit", target)
}

// collectCommits walks first-parent links from tip backward to stop (exclusive),
// returning commits in oldest-first order. This follows first-parent only,
// matching Git's rebase behavior of linearizing history.
func (r *Repo) collectCommits(stop, tip object.Hash) ([]object.Hash, error) {
	var commits []object.Hash
	current := tip
	seen := make(map[object.Hash]bool)
	for current != stop && current != "" {
		if seen[current] {
			return nil, fmt.Errorf("cycle detected in commit graph at %s", shortHash(current))
		}
		seen[current] = true
		commits = append(commits, current)
		c, err := r.Store.ReadCommit(current)
		if err != nil {
			return nil, fmt.Errorf("collect commits: read %s: %w", current, err)
		}
		if len(c.Parents) == 0 {
			break
		}
		current = c.Parents[0]
	}
	// Reverse to get oldest first.
	for i, j := 0, len(commits)-1; i < j; i, j = i+1, j-1 {
		commits[i], commits[j] = commits[j], commits[i]
	}
	return commits, nil
}

// replayCommits reads the todo list and replays each remaining commit.
func (r *Repo) replayCommits() error {
	seq := r.rebaseSeq()

	todoStr, err := seq.ReadFile("todo")
	if err != nil {
		// No todo means nothing left — finish up.
		return r.finishRebase()
	}
	if todoStr == "" {
		return r.finishRebase()
	}

	todo := strings.Split(todoStr, "\n")
	var done []string

	// Read existing done list.
	doneStr, _ := seq.ReadFile("done")
	if doneStr != "" {
		done = strings.Split(doneStr, "\n")
	}

	for len(todo) > 0 {
		commitHash := object.Hash(strings.TrimSpace(todo[0]))
		todo = todo[1:]

		err := r.replaySingleCommit(commitHash)
		if err != nil {
			// Save position: write remaining todo, stopped-sha.
			todoContent := strings.Join(todo, "\n")
			if todoContent != "" {
				todoContent += "\n"
			}
			if err2 := seq.WriteFile("todo", todoContent); err2 != nil {
				return fmt.Errorf("rebase: save todo: %w", err2)
			}
			if err2 := seq.WriteFile("stopped-sha", string(commitHash)+"\n"); err2 != nil {
				return fmt.Errorf("rebase: save stopped-sha: %w", err2)
			}
			// Write done.
			doneContent := strings.Join(done, "\n")
			if doneContent != "" {
				doneContent += "\n"
			}
			if err2 := seq.WriteFile("done", doneContent); err2 != nil {
				return fmt.Errorf("rebase: save done: %w", err2)
			}
			return err
		}

		done = append(done, string(commitHash))
	}

	// Update done and clear todo.
	doneContent := strings.Join(done, "\n")
	if doneContent != "" {
		doneContent += "\n"
	}
	if err := seq.WriteFile("done", doneContent); err != nil {
		return fmt.Errorf("rebase: save done: %w", err)
	}
	if err := seq.WriteFile("todo", ""); err != nil {
		return fmt.Errorf("rebase: clear todo: %w", err)
	}

	return r.finishRebase()
}

// replaySingleCommit cherry-picks a single commit onto the current HEAD.
func (r *Repo) replaySingleCommit(commitHash object.Hash) error {
	origCommit, err := r.Store.ReadCommit(commitHash)
	if err != nil {
		return fmt.Errorf("read commit %s: %w", shortHash(commitHash), err)
	}

	if len(origCommit.Parents) == 0 {
		return fmt.Errorf("commit %s has no parents", shortHash(commitHash))
	}

	parentHash := origCommit.Parents[0]
	headHash, err := r.ResolveRef("HEAD")
	if err != nil {
		return fmt.Errorf("resolve HEAD: %w", err)
	}

	// Flatten all three trees.
	parentCommit, err := r.Store.ReadCommit(parentHash)
	if err != nil {
		return fmt.Errorf("read parent %s: %w", shortHash(parentHash), err)
	}
	headCommit, err := r.Store.ReadCommit(headHash)
	if err != nil {
		return fmt.Errorf("read HEAD commit: %w", err)
	}

	baseFiles, err := r.FlattenTree(parentCommit.TreeHash)
	if err != nil {
		return fmt.Errorf("flatten base tree: %w", err)
	}
	oursFiles, err := r.FlattenTree(headCommit.TreeHash)
	if err != nil {
		return fmt.Errorf("flatten ours tree: %w", err)
	}
	theirsFiles, err := r.FlattenTree(origCommit.TreeHash)
	if err != nil {
		return fmt.Errorf("flatten theirs tree: %w", err)
	}

	baseMap := indexByPath(baseFiles)
	oursMap := indexByPath(oursFiles)
	theirsMap := indexByPath(theirsFiles)

	// Use the shared three-way merge helper.
	mergeResult, err := r.threeWayTreeMerge(baseMap, oursMap, theirsMap)
	if err != nil {
		return err
	}

	// Apply results to the working directory.
	if err := r.applyThreeWayResult(mergeResult); err != nil {
		return err
	}

	if mergeResult.HasConflicts {
		// Stage the written files for the conflict state.
		var pathsToStage []string
		for _, f := range mergeResult.Files {
			if f.Status != "unchanged" && f.Status != "deleted" {
				pathsToStage = append(pathsToStage, f.Path)
			}
		}
		if len(pathsToStage) > 0 {
			if err := r.Add(pathsToStage); err != nil {
				return fmt.Errorf("stage conflicts: %w", err)
			}
		}
		return &ErrRebaseConflict{
			CommitHash: commitHash,
			Message:    origCommit.Message,
			Details:    fmt.Sprintf("conflict in: %s", mergeResult.conflictDetailsString()),
		}
	}

	// Stage all changes and commit.
	var pathsToStage []string
	for _, f := range mergeResult.Files {
		if f.Status != "unchanged" && f.Status != "deleted" {
			pathsToStage = append(pathsToStage, f.Path)
		}
	}
	if len(pathsToStage) > 0 {
		if err := r.Add(pathsToStage); err != nil {
			return fmt.Errorf("stage: %w", err)
		}
	}

	// Handle deleted files in staging.
	if len(mergeResult.DeletedPaths) > 0 {
		stg, err := r.ReadStaging()
		if err != nil {
			return fmt.Errorf("read staging: %w", err)
		}
		for _, p := range mergeResult.DeletedPaths {
			delete(stg.Entries, p)
		}
		if err := r.WriteStaging(stg); err != nil {
			return fmt.Errorf("write staging: %w", err)
		}
	}

	// Create the replayed commit.
	author := origCommit.Author
	if author == "" {
		author = "graft-rebase"
	}

	stg, err := r.ReadStaging()
	if err != nil {
		return fmt.Errorf("read staging: %w", err)
	}
	if len(stg.Entries) == 0 {
		return fmt.Errorf("nothing staged after cherry-pick of %s", shortHash(commitHash))
	}

	treeHash, err := r.BuildTree(stg)
	if err != nil {
		return fmt.Errorf("build tree: %w", err)
	}

	newCommit := &object.CommitObj{
		TreeHash:  treeHash,
		Parents:   []object.Hash{headHash},
		Author:    author,
		Timestamp: time.Now().Unix(),
		Message:   origCommit.Message,
	}

	newHash, err := r.Store.WriteCommit(newCommit)
	if err != nil {
		return fmt.Errorf("write commit: %w", err)
	}

	// Update detached HEAD.
	if err := r.setHeadDetached(newHash); err != nil {
		return fmt.Errorf("update HEAD: %w", err)
	}

	r.invalidateStatusCache()
	return nil
}

// finishRebase updates the original branch ref and reattaches HEAD.
func (r *Repo) finishRebase() error {
	seq := r.rebaseSeq()

	headName, err := seq.ReadFile("head-name")
	if err != nil {
		return fmt.Errorf("rebase finish: read head-name: %w", err)
	}

	// Get the current detached HEAD hash (the new tip).
	newTip, err := r.ResolveRef("HEAD")
	if err != nil {
		return fmt.Errorf("rebase finish: resolve HEAD: %w", err)
	}

	// Update the branch ref to point to the new tip.
	if strings.HasPrefix(headName, "refs/heads/") {
		currentRef, _ := r.ResolveRef(headName)
		if err := r.UpdateRefCAS(headName, newTip, currentRef); err != nil {
			return fmt.Errorf("rebase finish: update branch ref: %w", err)
		}
	}

	// Reattach HEAD to the branch.
	if strings.HasPrefix(headName, "refs/") {
		if err := r.setHeadSymbolic(headName); err != nil {
			return fmt.Errorf("rebase finish: reattach HEAD: %w", err)
		}
	}

	// Restore autostash before cleaning up the sequencer directory
	// (which contains the autostash marker file).
	r.autostashPop()

	// Clean up sequencer state.
	if err := seq.Clean(); err != nil {
		return fmt.Errorf("rebase finish: cleanup: %w", err)
	}

	return nil
}

// detachHead sets HEAD to a raw commit hash and checks out the tree.
func (r *Repo) detachHead(hash object.Hash) error {
	commit, err := r.Store.ReadCommit(hash)
	if err != nil {
		return fmt.Errorf("read commit %s: %w", shortHash(hash), err)
	}

	if err := r.checkoutTree(commit); err != nil {
		return fmt.Errorf("checkout tree: %w", err)
	}

	if err := r.setHeadDetached(hash); err != nil {
		return err
	}

	return nil
}

// checkoutTree writes the tree of a commit to the working directory and updates staging.
func (r *Repo) checkoutTree(commit *object.CommitObj) error {
	targetFiles, err := r.FlattenTree(commit.TreeHash)
	if err != nil {
		return fmt.Errorf("flatten tree: %w", err)
	}

	// Remove all currently tracked files.
	currentFiles := r.trackedFiles()
	for path := range currentFiles {
		absPath := filepath.Join(r.RootDir, filepath.FromSlash(path))
		if err := os.Remove(absPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove %q: %w", path, err)
		}
		r.removeEmptyParents(filepath.Dir(absPath))
	}

	// Write all files from target tree.
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

	// Update staging.
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

// writeSequencerState creates the rebase-merge directory and writes all state files.
func (r *Repo) writeSequencerState(headName string, origHead, onto object.Hash, todo []object.Hash, done []object.Hash) error {
	seq := r.rebaseSeq()
	if err := seq.Init(); err != nil {
		return fmt.Errorf("mkdir %q: %w", seq.Dir(), err)
	}

	files := map[string]string{
		"head-name": headName + "\n",
		"orig-head": string(origHead) + "\n",
		"onto":      string(onto) + "\n",
	}

	// Build todo.
	var todoLines []string
	for _, h := range todo {
		todoLines = append(todoLines, string(h))
	}
	todoContent := strings.Join(todoLines, "\n")
	if todoContent != "" {
		todoContent += "\n"
	}
	files["todo"] = todoContent

	// Build done.
	var doneLines []string
	for _, h := range done {
		doneLines = append(doneLines, string(h))
	}
	doneContent := strings.Join(doneLines, "\n")
	if len(doneLines) > 0 {
		doneContent += "\n"
	}
	files["done"] = doneContent

	return seq.WriteFiles(files)
}

// readSequencerFile reads a file from the rebase-merge directory.
// Returns trimmed content. Used by rebase_interactive.go.
func (r *Repo) readSequencerFile(name string) (string, error) {
	return r.rebaseSeq().ReadFile(name)
}

// readSequencerHash reads a hash from the rebase-merge directory with validation.
// Used by rebase_interactive.go.
func (r *Repo) readSequencerHash(name string) (object.Hash, error) {
	return r.rebaseSeq().ReadHash(name)
}

// hasAutostash returns true if an autostash file exists in the sequencer directory.
func (r *Repo) hasAutostash() bool {
	_, err := r.rebaseSeq().ReadFile("autostash")
	return err == nil
}

// autostashSave checks for dirty working tree state and, if dirty, creates a
// stash and records the stash commit hash in the autostash file. Must be called
// BEFORE the rebase-merge directory is created (writeSequencerState creates it).
func (r *Repo) autostashSave() error {
	// Check if working tree is dirty.
	statusEntries, err := r.Status()
	if err != nil {
		return fmt.Errorf("rebase: autostash: %w", err)
	}
	dirty := false
	for _, e := range statusEntries {
		if e.IndexStatus != StatusClean || e.WorkStatus != StatusClean {
			dirty = true
			break
		}
	}
	if !dirty {
		return nil // nothing to stash
	}

	// Create the stash.
	entry, err := r.Stash("rebase autostash")
	if err != nil {
		return fmt.Errorf("rebase: autostash: %w", err)
	}

	// Ensure the rebase-merge directory exists for storing the autostash marker.
	seq := r.rebaseSeq()
	if err := seq.Init(); err != nil {
		return fmt.Errorf("rebase: autostash: mkdir: %w", err)
	}

	// Write the stash commit hash to the autostash file.
	if err := seq.WriteFile("autostash", string(entry.CommitHash)+"\n"); err != nil {
		return fmt.Errorf("rebase: autostash: write marker: %w", err)
	}

	return nil
}

// autostashPop restores the autostash if one exists. It drops the stash entry
// on success. If popping the stash causes conflicts, a warning is printed to
// stderr but the error is not propagated -- the rebase itself is considered
// successful.
func (r *Repo) autostashPop() {
	if !r.hasAutostash() {
		return
	}
	// Read the expected stash hash.
	seq := r.rebaseSeq()
	expectedHash, _ := seq.ReadFile("autostash")

	// Find the stash index matching our autostash hash.
	stack, err := r.readStashStack()
	if err != nil || len(stack) == 0 {
		fmt.Fprintf(os.Stderr, "warning: autostash pop: could not read stash stack: %v\n", err)
		return
	}

	idx := -1
	for i, entry := range stack {
		if string(entry.CommitHash) == expectedHash {
			idx = i
			break
		}
	}
	if idx < 0 {
		fmt.Fprintf(os.Stderr, "warning: autostash pop: stash entry not found (may have been manually popped)\n")
		return
	}

	err = r.StashPop(idx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not restore autostash: %v\nYour changes are still in the stash.\n", err)
	}
}

// writeSequencerFileAtomic writes a file to the rebase-merge directory using
// temp file + rename for atomicity. Used by rebase_interactive.go.
func (r *Repo) writeSequencerFileAtomic(name, content string) error {
	return r.rebaseSeq().WriteFile(name, content)
}
