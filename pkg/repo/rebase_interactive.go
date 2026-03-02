package repo

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/odvcencio/graft/pkg/object"
)

// TodoAction represents an action in an interactive rebase todo list.
type TodoAction string

const (
	TodoPick   TodoAction = "pick"
	TodoReword TodoAction = "reword"
	TodoSquash TodoAction = "squash"
	TodoFixup  TodoAction = "fixup"
	TodoDrop   TodoAction = "drop"
	TodoExec   TodoAction = "exec"
	TodoEdit   TodoAction = "edit"
)

// TodoItem represents a single entry in an interactive rebase todo list.
type TodoItem struct {
	Action  TodoAction
	Hash    object.Hash // empty for exec
	Message string      // original commit message summary, or command for exec
}

// ErrRebaseEditStop is returned when an "edit" action pauses the rebase
// so the user can amend the commit before continuing.
type ErrRebaseEditStop struct {
	CommitHash object.Hash
	Message    string
}

func (e *ErrRebaseEditStop) Error() string {
	return fmt.Sprintf("rebase: stopped at %s (%s) -- amend the commit and run 'graft rebase --continue'",
		shortHash(e.CommitHash), commitTitle(e.Message))
}

// RebaseInteractive starts an interactive rebase, opening the user's editor
// to edit the todo list before executing it.
func (r *Repo) RebaseInteractive(upstream string) error {
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

	// 6. Generate the todo list content.
	todoContent, err := r.generateTodoList(commits)
	if err != nil {
		return fmt.Errorf("rebase: generate todo list: %w", err)
	}

	// 7. Write to temp file and open editor.
	tmpFile, err := os.CreateTemp("", "graft-rebase-todo-*.txt")
	if err != nil {
		return fmt.Errorf("rebase: create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if _, err := tmpFile.WriteString(todoContent); err != nil {
		tmpFile.Close()
		return fmt.Errorf("rebase: write todo file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("rebase: close todo file: %w", err)
	}

	// Open editor.
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "vi"
	}

	editorCmd := exec.Command(editor, tmpPath)
	editorCmd.Stdin = os.Stdin
	editorCmd.Stdout = os.Stdout
	editorCmd.Stderr = os.Stderr
	if err := editorCmd.Run(); err != nil {
		return fmt.Errorf("rebase: editor exited with error: %w", err)
	}

	// 8. Read back the edited todo list.
	editedContent, err := os.ReadFile(tmpPath)
	if err != nil {
		return fmt.Errorf("rebase: read edited todo file: %w", err)
	}

	// 9. Parse the todo list.
	items, err := parseTodoList(string(editedContent))
	if err != nil {
		return fmt.Errorf("rebase: %w", err)
	}

	// 10. If todo list is empty after editing, abort.
	if len(items) == 0 {
		return fmt.Errorf("rebase: nothing to do (empty todo list)")
	}

	// 11. Execute via the shared path.
	return r.rebaseWithTodoList(upstream, items)
}

// RebaseInteractiveWithAutosquash starts an interactive rebase with autosquash.
// Commits whose messages start with "fixup! <title>" or "squash! <title>" are
// automatically reordered to follow their target commit and their action is
// changed to fixup or squash respectively.
func (r *Repo) RebaseInteractiveWithAutosquash(upstream string) error {
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

	// 6. Build todo items from commits.
	items, err := r.buildTodoItems(commits)
	if err != nil {
		return fmt.Errorf("rebase: build todo items: %w", err)
	}

	// 7. Apply autosquash reordering.
	items = autosquashTodoList(items)

	// 8. Generate the todo list content for the editor.
	todoContent := formatTodoItems(items)

	// 9. Write to temp file and open editor.
	tmpFile, err := os.CreateTemp("", "graft-rebase-todo-*.txt")
	if err != nil {
		return fmt.Errorf("rebase: create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if _, err := tmpFile.WriteString(todoContent); err != nil {
		tmpFile.Close()
		return fmt.Errorf("rebase: write todo file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("rebase: close todo file: %w", err)
	}

	// Open editor.
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "vi"
	}

	editorCmd := exec.Command(editor, tmpPath)
	editorCmd.Stdin = os.Stdin
	editorCmd.Stdout = os.Stdout
	editorCmd.Stderr = os.Stderr
	if err := editorCmd.Run(); err != nil {
		return fmt.Errorf("rebase: editor exited with error: %w", err)
	}

	// 10. Read back the edited todo list.
	editedContent, err := os.ReadFile(tmpPath)
	if err != nil {
		return fmt.Errorf("rebase: read edited todo file: %w", err)
	}

	// 11. Parse the todo list.
	editedItems, err := parseTodoList(string(editedContent))
	if err != nil {
		return fmt.Errorf("rebase: %w", err)
	}

	// 12. If todo list is empty after editing, abort.
	if len(editedItems) == 0 {
		return fmt.Errorf("rebase: nothing to do (empty todo list)")
	}

	// 13. Execute via the shared path.
	return r.rebaseWithTodoList(upstream, editedItems)
}

// rebaseWithTodoList executes an interactive rebase with the given todo items.
// This is the shared execution path used by both RebaseInteractive (after
// the editor) and tests (which supply items directly).
func (r *Repo) rebaseWithTodoList(upstream string, items []TodoItem) error {
	if r.isRebaseInProgress() {
		return ErrRebaseInProgress
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

	// Determine the branch name to save.
	branchName, err := r.Head()
	if err != nil {
		return fmt.Errorf("rebase: read HEAD: %w", err)
	}

	// Save sequencer state (use an empty todo since we manage our own).
	if err := r.writeSequencerState(branchName, headHash, upstreamHash, nil, nil); err != nil {
		return fmt.Errorf("rebase: save state: %w", err)
	}

	// Mark this as an interactive rebase so RebaseContinue knows how to resume.
	if err := r.writeSequencerFileAtomic("interactive", "true\n"); err != nil {
		os.RemoveAll(r.rebaseMergeDir())
		return fmt.Errorf("rebase: save interactive flag: %w", err)
	}

	// Detach HEAD to upstream.
	if err := r.detachHead(upstreamHash); err != nil {
		os.RemoveAll(r.rebaseMergeDir())
		return fmt.Errorf("rebase: detach HEAD: %w", err)
	}

	// Execute the todo list.
	if err := r.executeTodoList(items); err != nil {
		// On error, preserve sequencer state so --continue or --abort can work.
		// The executeTodoList method already saved the remaining todo items
		// and stopped-sha before returning.
		return err
	}

	// Finish the rebase.
	return r.finishRebase()
}

// generateTodoList creates the todo file content from a list of commit hashes.
func (r *Repo) generateTodoList(commits []object.Hash) (string, error) {
	var b strings.Builder

	for _, h := range commits {
		c, err := r.Store.ReadCommit(h)
		if err != nil {
			return "", fmt.Errorf("read commit %s: %w", shortHash(h), err)
		}
		// Use first line of commit message as summary.
		summary := commitTitle(c.Message)
		hashStr := string(h)
		if len(hashStr) > 8 {
			hashStr = hashStr[:8]
		}
		fmt.Fprintf(&b, "pick %s %s\n", hashStr, summary)
	}

	b.WriteString("\n")
	b.WriteString("# Commands:\n")
	b.WriteString("# pick = use commit\n")
	b.WriteString("# reword = use commit but edit message\n")
	b.WriteString("# edit = use commit, stop for amending\n")
	b.WriteString("# squash = meld into previous commit, combine messages\n")
	b.WriteString("# fixup = meld into previous commit, discard this message\n")
	b.WriteString("# drop = remove commit\n")
	b.WriteString("# exec = run command\n")

	return b.String(), nil
}

// parseTodoList parses the content of an edited todo file into TodoItems.
// It ignores blank lines and comment lines (starting with #).
func parseTodoList(content string) ([]TodoItem, error) {
	var items []TodoItem
	lines := strings.Split(content, "\n")

	for lineNum, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, " ", 2)
		if len(parts) < 1 {
			continue
		}

		action := TodoAction(strings.ToLower(parts[0]))

		switch action {
		case TodoPick, TodoReword, TodoSquash, TodoFixup, TodoDrop, TodoEdit:
			if len(parts) < 2 {
				return nil, fmt.Errorf("parse todo: line %d: %s requires a commit hash", lineNum+1, action)
			}
			rest := parts[1]
			// Split rest into hash and message.
			hashAndMsg := strings.SplitN(rest, " ", 2)
			hash := hashAndMsg[0]
			msg := ""
			if len(hashAndMsg) > 1 {
				msg = hashAndMsg[1]
			}
			items = append(items, TodoItem{
				Action:  action,
				Hash:    object.Hash(hash),
				Message: msg,
			})

		case TodoExec:
			if len(parts) < 2 {
				return nil, fmt.Errorf("parse todo: line %d: exec requires a command", lineNum+1)
			}
			items = append(items, TodoItem{
				Action:  TodoExec,
				Message: parts[1],
			})

		default:
			return nil, fmt.Errorf("parse todo: line %d: unknown action %q", lineNum+1, parts[0])
		}
	}

	return items, nil
}

// serializeTodoItems converts a slice of TodoItems into the sequencer file format.
// Each line is: action hash message
func serializeTodoItems(items []TodoItem) string {
	var b strings.Builder
	for _, item := range items {
		if item.Action == TodoExec {
			fmt.Fprintf(&b, "exec %s\n", item.Message)
		} else {
			fmt.Fprintf(&b, "%s %s %s\n", item.Action, item.Hash, item.Message)
		}
	}
	return b.String()
}

// executeTodoList runs each action in the todo list. On error (conflict or
// edit stop), it saves the remaining items to the sequencer state so the
// rebase can be continued or aborted.
func (r *Repo) executeTodoList(items []TodoItem) error {
	for i := 0; i < len(items); i++ {
		item := items[i]

		switch item.Action {
		case TodoPick:
			hash, err := r.resolveShortHash(item.Hash)
			if err != nil {
				r.saveInteractiveTodoState(items[i+1:], hash)
				return fmt.Errorf("rebase interactive: resolve %s: %w", item.Hash, err)
			}
			if err := r.replaySingleCommit(hash); err != nil {
				r.saveInteractiveTodoState(items[i+1:], hash)
				return err
			}

		case TodoReword:
			hash, err := r.resolveShortHash(item.Hash)
			if err != nil {
				r.saveInteractiveTodoState(items[i+1:], hash)
				return fmt.Errorf("rebase interactive: resolve %s: %w", item.Hash, err)
			}
			if err := r.replaySingleCommit(hash); err != nil {
				r.saveInteractiveTodoState(items[i+1:], hash)
				return err
			}
			// Amend the just-created commit with a new message from the editor.
			if err := r.rewordLastCommit(); err != nil {
				r.saveInteractiveTodoState(items[i+1:], hash)
				return fmt.Errorf("rebase interactive: reword: %w", err)
			}

		case TodoEdit:
			hash, err := r.resolveShortHash(item.Hash)
			if err != nil {
				r.saveInteractiveTodoState(items[i+1:], hash)
				return fmt.Errorf("rebase interactive: resolve %s: %w", item.Hash, err)
			}
			if err := r.replaySingleCommit(hash); err != nil {
				r.saveInteractiveTodoState(items[i+1:], hash)
				return err
			}
			// Save the remaining items (after this one) and stop.
			r.saveInteractiveTodoState(items[i+1:], hash)
			// Write an edit-mode marker so RebaseContinue knows to amend.
			r.writeSequencerFileAtomic("edit-mode", "true\n") //nolint:errcheck
			origCommit, _ := r.Store.ReadCommit(hash)
			msg := ""
			if origCommit != nil {
				msg = origCommit.Message
			}
			return &ErrRebaseEditStop{
				CommitHash: hash,
				Message:    msg,
			}

		case TodoSquash:
			hash, err := r.resolveShortHash(item.Hash)
			if err != nil {
				r.saveInteractiveTodoState(items[i+1:], hash)
				return fmt.Errorf("rebase interactive: resolve %s: %w", item.Hash, err)
			}
			if err := r.squashCommit(hash, true); err != nil {
				r.saveInteractiveTodoState(items[i+1:], hash)
				return err
			}

		case TodoFixup:
			hash, err := r.resolveShortHash(item.Hash)
			if err != nil {
				r.saveInteractiveTodoState(items[i+1:], hash)
				return fmt.Errorf("rebase interactive: resolve %s: %w", item.Hash, err)
			}
			if err := r.squashCommit(hash, false); err != nil {
				r.saveInteractiveTodoState(items[i+1:], hash)
				return err
			}

		case TodoDrop:
			// Simply skip this commit.
			continue

		case TodoExec:
			if err := r.execCommand(item.Message); err != nil {
				r.saveInteractiveTodoState(items[i+1:], "")
				return fmt.Errorf("rebase interactive: exec %q: %w", item.Message, err)
			}
		}
	}

	return nil
}

// saveInteractiveTodoState writes the remaining interactive todo items and
// the stopped commit hash to sequencer state for --continue to resume.
func (r *Repo) saveInteractiveTodoState(remaining []TodoItem, stoppedHash object.Hash) {
	// Write the remaining interactive todo items.
	todoContent := serializeTodoItems(remaining)
	r.writeSequencerFileAtomic("interactive-todo", todoContent) //nolint:errcheck

	// Write stopped-sha so RebaseContinue knows which commit to finish.
	if stoppedHash != "" {
		r.writeSequencerFileAtomic("stopped-sha", string(stoppedHash)+"\n") //nolint:errcheck
	}
}

// RebaseInteractiveContinue resumes a paused interactive rebase after the user
// has resolved conflicts (or made edits for an edit stop) and staged changes.
func (r *Repo) RebaseInteractiveContinue() error {
	if !r.isRebaseInProgress() {
		return ErrNoRebaseInProgress
	}

	// Check for edit-mode: the user was stopped at an "edit" action
	// and should amend the current commit with their working tree changes.
	editMode := false
	if em, err := r.readSequencerFile("edit-mode"); err == nil && strings.TrimSpace(em) == "true" {
		editMode = true
		os.Remove(filepath.Join(r.rebaseMergeDir(), "edit-mode"))
	}

	stoppedSHA, err := r.readSequencerFile("stopped-sha")
	if err != nil {
		return fmt.Errorf("rebase continue: no stopped commit found: %w", err)
	}
	stoppedSHA = strings.TrimSpace(stoppedSHA)

	if editMode {
		// Amend the current HEAD commit with whatever the user has staged.
		if err := r.amendHeadWithStaged(); err != nil {
			return fmt.Errorf("rebase continue: amend edit: %w", err)
		}
	} else {
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
	}

	// Remove stopped-sha.
	os.Remove(filepath.Join(r.rebaseMergeDir(), "stopped-sha"))

	// Read remaining interactive todo items.
	todoContent, err := r.readSequencerFile("interactive-todo")
	if err != nil || strings.TrimSpace(todoContent) == "" {
		// No remaining items -- finish the rebase.
		return r.finishRebase()
	}

	// Parse remaining items.
	remaining, err := parseTodoList(todoContent)
	if err != nil {
		return fmt.Errorf("rebase continue: parse remaining todo: %w", err)
	}

	if len(remaining) == 0 {
		return r.finishRebase()
	}

	// Continue executing the remaining items.
	if err := r.executeTodoList(remaining); err != nil {
		return err
	}

	return r.finishRebase()
}

// amendHeadWithStaged amends the current HEAD commit with the currently
// staged files. Used by the edit action's --continue path.
func (r *Repo) amendHeadWithStaged() error {
	headHash, err := r.ResolveRef("HEAD")
	if err != nil {
		return fmt.Errorf("resolve HEAD: %w", err)
	}

	headCommit, err := r.Store.ReadCommit(headHash)
	if err != nil {
		return fmt.Errorf("read HEAD commit: %w", err)
	}

	// Build tree from current staging area.
	stg, err := r.ReadStaging()
	if err != nil {
		return fmt.Errorf("read staging: %w", err)
	}

	treeHash, err := r.BuildTree(stg)
	if err != nil {
		return fmt.Errorf("build tree: %w", err)
	}

	// If tree hasn't changed, nothing to amend.
	if treeHash == headCommit.TreeHash {
		return nil
	}

	// Create a new commit with the same metadata but the new tree.
	newCommit := &object.CommitObj{
		TreeHash:  treeHash,
		Parents:   headCommit.Parents,
		Author:    headCommit.Author,
		Timestamp: time.Now().Unix(),
		Message:   headCommit.Message,
	}

	newHash, err := r.Store.WriteCommit(newCommit)
	if err != nil {
		return fmt.Errorf("write amended commit: %w", err)
	}

	// Update HEAD.
	headPath := filepath.Join(r.GraftDir, "HEAD")
	if err := os.WriteFile(headPath, []byte(string(newHash)+"\n"), 0o644); err != nil {
		return fmt.Errorf("update HEAD: %w", err)
	}

	r.invalidateStatusCache()
	return nil
}

// isInteractiveRebase checks if the current in-progress rebase is interactive.
func (r *Repo) isInteractiveRebase() bool {
	data, err := r.readSequencerFile("interactive")
	if err != nil {
		return false
	}
	return strings.TrimSpace(data) == "true"
}

// resolveShortHash resolves a potentially abbreviated hash to the full hash
// by looking it up as a commit. If the hash is already full, it returns it directly.
func (r *Repo) resolveShortHash(h object.Hash) (object.Hash, error) {
	// Try reading directly first.
	if _, err := r.Store.ReadCommit(h); err == nil {
		return h, nil
	}

	// Walk recent commits to find a match by prefix.
	// Use the orig-head from sequencer state if available.
	origHeadStr, err := r.readSequencerFile("orig-head")
	if err != nil {
		return "", fmt.Errorf("cannot resolve short hash %s: no sequencer state", h)
	}
	origHead := object.Hash(strings.TrimSpace(origHeadStr))

	// Walk from orig-head backward.
	current := origHead
	prefix := string(h)
	for current != "" {
		if strings.HasPrefix(string(current), prefix) {
			return current, nil
		}
		c, err := r.Store.ReadCommit(current)
		if err != nil {
			break
		}
		if len(c.Parents) == 0 {
			break
		}
		current = c.Parents[0]
	}

	return "", fmt.Errorf("cannot resolve short hash %s to a commit", h)
}

// squashCommit cherry-picks the given commit's changes and amends them into
// the previous commit. If combineMessages is true (squash), the messages are
// concatenated. If false (fixup), the previous commit's message is kept.
func (r *Repo) squashCommit(commitHash object.Hash, combineMessages bool) error {
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

	// Read the current HEAD commit (the one we'll amend).
	headCommit, err := r.Store.ReadCommit(headHash)
	if err != nil {
		return fmt.Errorf("read HEAD commit: %w", err)
	}

	// Flatten trees for three-way merge.
	parentCommit, err := r.Store.ReadCommit(parentHash)
	if err != nil {
		return fmt.Errorf("read parent %s: %w", shortHash(parentHash), err)
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

	// Stage everything.
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

	if mergeResult.HasConflicts {
		return &ErrRebaseConflict{
			CommitHash: commitHash,
			Message:    origCommit.Message,
			Details:    fmt.Sprintf("conflict in: %s", mergeResult.conflictDetailsString()),
		}
	}

	// Build a new tree and amend the previous commit.
	stg, err := r.ReadStaging()
	if err != nil {
		return fmt.Errorf("read staging: %w", err)
	}
	treeHash, err := r.BuildTree(stg)
	if err != nil {
		return fmt.Errorf("build tree: %w", err)
	}

	// Determine the new commit message.
	var newMessage string
	if combineMessages {
		newMessage = headCommit.Message + "\n\n" + origCommit.Message
	} else {
		newMessage = headCommit.Message
	}

	// Amend: create a new commit with the same parent as the HEAD commit.
	var parents []object.Hash
	if len(headCommit.Parents) > 0 {
		parents = headCommit.Parents
	}

	author := headCommit.Author
	if author == "" {
		author = "graft-rebase"
	}

	newCommit := &object.CommitObj{
		TreeHash:  treeHash,
		Parents:   parents,
		Author:    author,
		Timestamp: time.Now().Unix(),
		Message:   newMessage,
	}

	newHash, err := r.Store.WriteCommit(newCommit)
	if err != nil {
		return fmt.Errorf("write commit: %w", err)
	}

	// Update detached HEAD to the amended commit.
	headPath := filepath.Join(r.GraftDir, "HEAD")
	if err := os.WriteFile(headPath, []byte(string(newHash)+"\n"), 0o644); err != nil {
		return fmt.Errorf("update HEAD: %w", err)
	}

	r.invalidateStatusCache()
	return nil
}

// rewordLastCommit opens the editor to let the user rewrite the message of
// the most recent commit (HEAD).
func (r *Repo) rewordLastCommit() error {
	headHash, err := r.ResolveRef("HEAD")
	if err != nil {
		return fmt.Errorf("resolve HEAD: %w", err)
	}

	headCommit, err := r.Store.ReadCommit(headHash)
	if err != nil {
		return fmt.Errorf("read HEAD commit: %w", err)
	}

	// Write message to temp file.
	tmpFile, err := os.CreateTemp("", "graft-reword-*.txt")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if _, err := tmpFile.WriteString(headCommit.Message); err != nil {
		tmpFile.Close()
		return err
	}
	if err := tmpFile.Close(); err != nil {
		return err
	}

	// Open editor.
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "vi"
	}

	editorCmd := exec.Command(editor, tmpPath)
	editorCmd.Stdin = os.Stdin
	editorCmd.Stdout = os.Stdout
	editorCmd.Stderr = os.Stderr
	if err := editorCmd.Run(); err != nil {
		return fmt.Errorf("editor exited with error: %w", err)
	}

	// Read back edited message.
	newMsgBytes, err := os.ReadFile(tmpPath)
	if err != nil {
		return fmt.Errorf("read edited message: %w", err)
	}
	newMessage := string(newMsgBytes)

	// Create an amended commit.
	newCommit := &object.CommitObj{
		TreeHash:  headCommit.TreeHash,
		Parents:   headCommit.Parents,
		Author:    headCommit.Author,
		Timestamp: headCommit.Timestamp,
		Message:   newMessage,
	}

	newHash, err := r.Store.WriteCommit(newCommit)
	if err != nil {
		return fmt.Errorf("write amended commit: %w", err)
	}

	// Update HEAD.
	headPath := filepath.Join(r.GraftDir, "HEAD")
	if err := os.WriteFile(headPath, []byte(string(newHash)+"\n"), 0o644); err != nil {
		return fmt.Errorf("update HEAD: %w", err)
	}

	r.invalidateStatusCache()
	return nil
}

// execCommand runs a shell command during interactive rebase.
func (r *Repo) execCommand(command string) error {
	cmd := exec.Command("sh", "-c", command)
	cmd.Dir = r.RootDir
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// buildTodoItems creates TodoItem entries from a list of commit hashes.
func (r *Repo) buildTodoItems(commits []object.Hash) ([]TodoItem, error) {
	var items []TodoItem
	for _, h := range commits {
		c, err := r.Store.ReadCommit(h)
		if err != nil {
			return nil, fmt.Errorf("read commit %s: %w", shortHash(h), err)
		}
		hashStr := string(h)
		if len(hashStr) > 8 {
			hashStr = hashStr[:8]
		}
		items = append(items, TodoItem{
			Action:  TodoPick,
			Hash:    object.Hash(hashStr),
			Message: commitTitle(c.Message),
		})
	}
	return items, nil
}

// formatTodoItems formats a list of TodoItems into the editor-friendly todo
// file content (with help comments appended).
func formatTodoItems(items []TodoItem) string {
	var b strings.Builder
	for _, item := range items {
		if item.Action == TodoExec {
			fmt.Fprintf(&b, "exec %s\n", item.Message)
		} else {
			fmt.Fprintf(&b, "%s %s %s\n", item.Action, item.Hash, item.Message)
		}
	}
	b.WriteString("\n")
	b.WriteString("# Commands:\n")
	b.WriteString("# pick = use commit\n")
	b.WriteString("# reword = use commit but edit message\n")
	b.WriteString("# edit = use commit, stop for amending\n")
	b.WriteString("# squash = meld into previous commit, combine messages\n")
	b.WriteString("# fixup = meld into previous commit, discard this message\n")
	b.WriteString("# drop = remove commit\n")
	b.WriteString("# exec = run command\n")
	return b.String()
}

// autosquashTodoList reorders a todo list so that commits whose messages start
// with "fixup! <title>" or "squash! <title>" are placed immediately after
// their target commit, with their action changed to fixup or squash.
func autosquashTodoList(items []TodoItem) []TodoItem {
	// Separate normal items from fixup/squash items.
	type squashEntry struct {
		item       TodoItem
		targetTitle string
		action     TodoAction
	}

	var normal []TodoItem
	var squashes []squashEntry

	for _, item := range items {
		msg := item.Message
		if strings.HasPrefix(msg, "fixup! ") {
			squashes = append(squashes, squashEntry{
				item:        item,
				targetTitle: strings.TrimPrefix(msg, "fixup! "),
				action:      TodoFixup,
			})
		} else if strings.HasPrefix(msg, "squash! ") {
			squashes = append(squashes, squashEntry{
				item:        item,
				targetTitle: strings.TrimPrefix(msg, "squash! "),
				action:      TodoSquash,
			})
		} else {
			normal = append(normal, item)
		}
	}

	if len(squashes) == 0 {
		return items
	}

	// For each squash entry, find its target in the normal list and insert after.
	// Process in reverse order so indices stay stable.
	var result []TodoItem
	result = append(result, normal...)

	for _, sq := range squashes {
		inserted := false
		for i := 0; i < len(result); i++ {
			if result[i].Message == sq.targetTitle {
				// Change the action to fixup or squash.
				entry := sq.item
				entry.Action = sq.action
				// Skip past any fixup/squash items already placed after this target
				// so that multiple fixups for the same target preserve their
				// original relative order.
				insertPos := i + 1
				for insertPos < len(result) && (result[insertPos].Action == TodoFixup || result[insertPos].Action == TodoSquash) {
					insertPos++
				}
				// Insert at insertPos.
				newResult := make([]TodoItem, 0, len(result)+1)
				newResult = append(newResult, result[:insertPos]...)
				newResult = append(newResult, entry)
				newResult = append(newResult, result[insertPos:]...)
				result = newResult
				inserted = true
				break
			}
		}
		if !inserted {
			// No target found -- keep the item at the end with its original action.
			result = append(result, sq.item)
		}
	}

	return result
}
