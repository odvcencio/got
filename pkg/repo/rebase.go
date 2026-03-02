package repo

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/odvcencio/graft/pkg/merge"
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

// rebaseMergeDir returns the path to the sequencer state directory.
func (r *Repo) rebaseMergeDir() string {
	return filepath.Join(r.GraftDir, "rebase-merge")
}

// isRebaseInProgress checks if a rebase is currently active.
func (r *Repo) isRebaseInProgress() bool {
	_, err := os.Stat(r.rebaseMergeDir())
	return err == nil
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
	if r.isRebaseInProgress() {
		return ErrRebaseInProgress
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
		return nil // no-op
	}

	// 5. Collect commits from merge-base..HEAD (oldest first).
	commits, err := r.collectCommits(mergeBase, headHash)
	if err != nil {
		return fmt.Errorf("rebase: %w", err)
	}
	if len(commits) == 0 {
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
	if r.isRebaseInProgress() {
		return ErrRebaseInProgress
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
		os.RemoveAll(r.rebaseMergeDir())
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

	stoppedSHA, err := r.readSequencerFile("stopped-sha")
	if err != nil {
		return fmt.Errorf("rebase continue: no stopped commit found: %w", err)
	}
	stoppedSHA = strings.TrimSpace(stoppedSHA)

	// Read the original commit to preserve its message and author.
	origCommit, err := r.Store.ReadCommit(object.Hash(stoppedSHA))
	if err != nil {
		return fmt.Errorf("rebase continue: read original commit %s: %w", stoppedSHA, err)
	}

	// Create a commit with the original message.
	commitHash, err := r.Commit(origCommit.Message, origCommit.Author)
	if err != nil {
		return fmt.Errorf("rebase continue: commit: %w", err)
	}
	_ = commitHash

	// Move stopped-sha to done.
	done, _ := r.readSequencerFile("done")
	done = strings.TrimRight(done, "\n")
	if done != "" {
		done += "\n"
	}
	done += stoppedSHA + "\n"
	if err := r.writeSequencerFileAtomic("done", done); err != nil {
		return fmt.Errorf("rebase continue: update done: %w", err)
	}

	// Remove stopped-sha.
	os.Remove(filepath.Join(r.rebaseMergeDir(), "stopped-sha"))

	// Continue replaying remaining commits.
	return r.replayCommits()
}

// RebaseAbort cancels the current rebase and restores the original state.
func (r *Repo) RebaseAbort() error {
	if !r.isRebaseInProgress() {
		return ErrNoRebaseInProgress
	}

	origHead, err := r.readSequencerFile("orig-head")
	if err != nil {
		return fmt.Errorf("rebase abort: read orig-head: %w", err)
	}
	origHead = strings.TrimSpace(origHead)

	headName, err := r.readSequencerFile("head-name")
	if err != nil {
		return fmt.Errorf("rebase abort: read head-name: %w", err)
	}
	headName = strings.TrimSpace(headName)

	// Update the branch ref back to orig-head.
	if strings.HasPrefix(headName, "refs/heads/") {
		if err := r.UpdateRef(headName, object.Hash(origHead)); err != nil {
			return fmt.Errorf("rebase abort: restore branch ref: %w", err)
		}
	}

	// Reattach HEAD to the branch.
	headPath := filepath.Join(r.GraftDir, "HEAD")
	if strings.HasPrefix(headName, "refs/") {
		if err := os.WriteFile(headPath, []byte("ref: "+headName+"\n"), 0o644); err != nil {
			return fmt.Errorf("rebase abort: reattach HEAD: %w", err)
		}
	} else {
		if err := os.WriteFile(headPath, []byte(origHead+"\n"), 0o644); err != nil {
			return fmt.Errorf("rebase abort: set HEAD: %w", err)
		}
	}

	// Checkout the original state.
	origCommit, err := r.Store.ReadCommit(object.Hash(origHead))
	if err != nil {
		return fmt.Errorf("rebase abort: read orig commit: %w", err)
	}
	if err := r.checkoutTree(origCommit); err != nil {
		return fmt.Errorf("rebase abort: checkout: %w", err)
	}

	// Clean up sequencer state.
	if err := os.RemoveAll(r.rebaseMergeDir()); err != nil {
		return fmt.Errorf("rebase abort: cleanup: %w", err)
	}

	return nil
}

// RebaseSkip drops the current stopped commit and continues with the next.
func (r *Repo) RebaseSkip() error {
	if !r.isRebaseInProgress() {
		return ErrNoRebaseInProgress
	}

	stoppedSHA, err := r.readSequencerFile("stopped-sha")
	if err != nil {
		return fmt.Errorf("rebase skip: no stopped commit found: %w", err)
	}
	stoppedSHA = strings.TrimSpace(stoppedSHA)

	// Move stopped-sha to done (skipped).
	done, _ := r.readSequencerFile("done")
	done = strings.TrimRight(done, "\n")
	if done != "" {
		done += "\n"
	}
	done += stoppedSHA + "\n"
	if err := r.writeSequencerFileAtomic("done", done); err != nil {
		return fmt.Errorf("rebase skip: update done: %w", err)
	}

	// Remove stopped-sha.
	os.Remove(filepath.Join(r.rebaseMergeDir(), "stopped-sha"))

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
	// Try as raw hash — verify it exists.
	_, err = r.Store.ReadCommit(object.Hash(target))
	if err == nil {
		return object.Hash(target), nil
	}
	return "", fmt.Errorf("cannot resolve %q to a commit", target)
}

// collectCommits walks from tip backwards (following first parents) until it
// reaches stop (exclusive). Returns the commits in oldest-first order.
func (r *Repo) collectCommits(stop, tip object.Hash) ([]object.Hash, error) {
	var commits []object.Hash
	current := tip
	for current != stop && current != "" {
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
	todoStr, err := r.readSequencerFile("todo")
	if err != nil {
		// No todo means nothing left — finish up.
		return r.finishRebase()
	}
	todoStr = strings.TrimSpace(todoStr)
	if todoStr == "" {
		return r.finishRebase()
	}

	todo := strings.Split(todoStr, "\n")
	var done []string

	// Read existing done list.
	doneStr, _ := r.readSequencerFile("done")
	doneStr = strings.TrimRight(doneStr, "\n")
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
			if err2 := r.writeSequencerFileAtomic("todo", todoContent); err2 != nil {
				return fmt.Errorf("rebase: save todo: %w", err2)
			}
			if err2 := r.writeSequencerFileAtomic("stopped-sha", string(commitHash)+"\n"); err2 != nil {
				return fmt.Errorf("rebase: save stopped-sha: %w", err2)
			}
			// Write done.
			doneContent := strings.Join(done, "\n")
			if doneContent != "" {
				doneContent += "\n"
			}
			if err2 := r.writeSequencerFileAtomic("done", doneContent); err2 != nil {
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
	if err := r.writeSequencerFileAtomic("done", doneContent); err != nil {
		return fmt.Errorf("rebase: save done: %w", err)
	}
	if err := r.writeSequencerFileAtomic("todo", ""); err != nil {
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

	allPaths := collectAllPaths(baseMap, oursMap, theirsMap)

	hasConflicts := false
	var conflictDetails []string

	type fileWrite struct {
		path    string
		content []byte
		mode    string
	}
	var writes []fileWrite
	var deletes []string

	for _, path := range allPaths {
		_, inBase := baseMap[path]
		_, inOurs := oursMap[path]
		_, inTheirs := theirsMap[path]

		switch {
		case inBase && inOurs && inTheirs:
			// Three-way merge.
			if oursMap[path].BlobHash == theirsMap[path].BlobHash {
				// Same content, no merge needed.
				continue
			}
			if oursMap[path].BlobHash == baseMap[path].BlobHash {
				// Only theirs changed — take theirs.
				content, err := r.readBlobData(theirsMap[path].BlobHash)
				if err != nil {
					return err
				}
				writes = append(writes, fileWrite{path, content, normalizeFileMode(theirsMap[path].Mode)})
				continue
			}
			if theirsMap[path].BlobHash == baseMap[path].BlobHash {
				// Only ours changed — keep ours.
				continue
			}
			// Both changed.
			baseData, err := r.readBlobData(baseMap[path].BlobHash)
			if err != nil {
				return err
			}
			oursData, err := r.readBlobData(oursMap[path].BlobHash)
			if err != nil {
				return err
			}
			theirsData, err := r.readBlobData(theirsMap[path].BlobHash)
			if err != nil {
				return err
			}
			result, err := merge.MergeFiles(path, baseData, oursData, theirsData)
			if err != nil {
				return fmt.Errorf("merge %q: %w", path, err)
			}
			if result.HasConflicts {
				hasConflicts = true
				conflictDetails = append(conflictDetails, path)
			}
			writes = append(writes, fileWrite{path, result.Merged, normalizeFileMode(oursMap[path].Mode)})

		case !inBase && !inOurs && inTheirs:
			// New in theirs (the commit being replayed).
			content, err := r.readBlobData(theirsMap[path].BlobHash)
			if err != nil {
				return err
			}
			writes = append(writes, fileWrite{path, content, normalizeFileMode(theirsMap[path].Mode)})

		case !inBase && inOurs && inTheirs:
			// Added in both.
			if oursMap[path].BlobHash == theirsMap[path].BlobHash {
				continue
			}
			oursData, err := r.readBlobData(oursMap[path].BlobHash)
			if err != nil {
				return err
			}
			theirsData, err := r.readBlobData(theirsMap[path].BlobHash)
			if err != nil {
				return err
			}
			result, err := merge.MergeFiles(path, nil, oursData, theirsData)
			if err != nil {
				return fmt.Errorf("merge %q: %w", path, err)
			}
			if result.HasConflicts {
				hasConflicts = true
				conflictDetails = append(conflictDetails, path)
			}
			writes = append(writes, fileWrite{path, result.Merged, normalizeFileMode(oursMap[path].Mode)})

		case inBase && inOurs && !inTheirs:
			// Deleted by theirs (the commit being replayed).
			if oursMap[path].BlobHash == baseMap[path].BlobHash {
				// Ours unchanged — clean delete.
				deletes = append(deletes, path)
			} else {
				// Ours modified — conflict.
				hasConflicts = true
				conflictDetails = append(conflictDetails, path)
				oursData, err := r.readBlobData(oursMap[path].BlobHash)
				if err != nil {
					return err
				}
				content := renderFileConflict(oursData, nil)
				writes = append(writes, fileWrite{path, content, normalizeFileMode(oursMap[path].Mode)})
			}

		case inBase && !inOurs && inTheirs:
			// Deleted by ours, modified by theirs.
			if theirsMap[path].BlobHash == baseMap[path].BlobHash {
				continue // theirs unchanged, keep deletion
			}
			hasConflicts = true
			conflictDetails = append(conflictDetails, path)
			theirsData, err := r.readBlobData(theirsMap[path].BlobHash)
			if err != nil {
				return err
			}
			content := renderFileConflict(nil, theirsData)
			writes = append(writes, fileWrite{path, content, normalizeFileMode(theirsMap[path].Mode)})

		case !inBase && inOurs && !inTheirs:
			// Only in ours, not involved in cherry-pick.
			continue

		case inBase && !inOurs && !inTheirs:
			// Deleted in both — already gone.
			continue
		}
	}

	// Write files.
	for _, w := range writes {
		absPath := filepath.Join(r.RootDir, filepath.FromSlash(w.path))
		if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
			return fmt.Errorf("mkdir: %w", err)
		}
		if err := os.WriteFile(absPath, w.content, filePermFromMode(w.mode)); err != nil {
			return fmt.Errorf("write %q: %w", w.path, err)
		}
	}

	// Delete files.
	for _, path := range deletes {
		absPath := filepath.Join(r.RootDir, filepath.FromSlash(path))
		if err := os.Remove(absPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove %q: %w", path, err)
		}
		r.removeEmptyParents(filepath.Dir(absPath))
	}

	if hasConflicts {
		// Stage the conflict state for the working tree.
		var pathsToStage []string
		for _, w := range writes {
			pathsToStage = append(pathsToStage, w.path)
		}
		if len(pathsToStage) > 0 {
			if err := r.Add(pathsToStage); err != nil {
				return fmt.Errorf("stage conflicts: %w", err)
			}
		}
		return &ErrRebaseConflict{
			CommitHash: commitHash,
			Message:    origCommit.Message,
			Details:    fmt.Sprintf("conflict in: %s", strings.Join(conflictDetails, ", ")),
		}
	}

	// Stage all changes and commit.
	var pathsToStage []string
	for _, w := range writes {
		pathsToStage = append(pathsToStage, w.path)
	}
	if len(pathsToStage) > 0 {
		if err := r.Add(pathsToStage); err != nil {
			return fmt.Errorf("stage: %w", err)
		}
	}

	// Handle deleted files in staging.
	if len(deletes) > 0 {
		stg, err := r.ReadStaging()
		if err != nil {
			return fmt.Errorf("read staging: %w", err)
		}
		for _, p := range deletes {
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
	headPath := filepath.Join(r.GraftDir, "HEAD")
	if err := os.WriteFile(headPath, []byte(string(newHash)+"\n"), 0o644); err != nil {
		return fmt.Errorf("update HEAD: %w", err)
	}

	r.invalidateStatusCache()
	return nil
}

// finishRebase updates the original branch ref and reattaches HEAD.
func (r *Repo) finishRebase() error {
	headName, err := r.readSequencerFile("head-name")
	if err != nil {
		return fmt.Errorf("rebase finish: read head-name: %w", err)
	}
	headName = strings.TrimSpace(headName)

	// Get the current detached HEAD hash (the new tip).
	newTip, err := r.ResolveRef("HEAD")
	if err != nil {
		return fmt.Errorf("rebase finish: resolve HEAD: %w", err)
	}

	// Update the branch ref to point to the new tip.
	if strings.HasPrefix(headName, "refs/heads/") {
		if err := r.UpdateRef(headName, newTip); err != nil {
			return fmt.Errorf("rebase finish: update branch ref: %w", err)
		}
	}

	// Reattach HEAD to the branch.
	headPath := filepath.Join(r.GraftDir, "HEAD")
	if strings.HasPrefix(headName, "refs/") {
		if err := os.WriteFile(headPath, []byte("ref: "+headName+"\n"), 0o644); err != nil {
			return fmt.Errorf("rebase finish: reattach HEAD: %w", err)
		}
	}

	// Clean up sequencer state.
	if err := os.RemoveAll(r.rebaseMergeDir()); err != nil {
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

	headPath := filepath.Join(r.GraftDir, "HEAD")
	if err := os.WriteFile(headPath, []byte(string(hash)+"\n"), 0o644); err != nil {
		return fmt.Errorf("write HEAD: %w", err)
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
	dir := r.rebaseMergeDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %q: %w", dir, err)
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

	for name, content := range files {
		if err := r.writeSequencerFileAtomic(name, content); err != nil {
			return fmt.Errorf("write %q: %w", name, err)
		}
	}

	return nil
}

// readSequencerFile reads a file from the rebase-merge directory.
func (r *Repo) readSequencerFile(name string) (string, error) {
	data, err := os.ReadFile(filepath.Join(r.rebaseMergeDir(), name))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// writeSequencerFileAtomic writes a file to the rebase-merge directory using
// temp file + rename for atomicity.
func (r *Repo) writeSequencerFileAtomic(name, content string) error {
	dir := r.rebaseMergeDir()
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
