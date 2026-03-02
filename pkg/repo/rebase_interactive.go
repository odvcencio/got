package repo

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/odvcencio/graft/pkg/merge"
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
)

// TodoItem represents a single entry in an interactive rebase todo list.
type TodoItem struct {
	Action  TodoAction
	Hash    object.Hash // empty for exec
	Message string      // original commit message summary, or command for exec
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

	// Detach HEAD to upstream.
	if err := r.detachHead(upstreamHash); err != nil {
		os.RemoveAll(r.rebaseMergeDir())
		return fmt.Errorf("rebase: detach HEAD: %w", err)
	}

	// Execute the todo list.
	if err := r.executeTodoList(items); err != nil {
		// On error, clean up sequencer state.
		os.RemoveAll(r.rebaseMergeDir())
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
		case TodoPick, TodoReword, TodoSquash, TodoFixup, TodoDrop:
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

// executeTodoList runs each action in the todo list.
func (r *Repo) executeTodoList(items []TodoItem) error {
	for i := 0; i < len(items); i++ {
		item := items[i]

		switch item.Action {
		case TodoPick:
			hash, err := r.resolveShortHash(item.Hash)
			if err != nil {
				return fmt.Errorf("rebase interactive: resolve %s: %w", item.Hash, err)
			}
			if err := r.replaySingleCommit(hash); err != nil {
				return err
			}

		case TodoReword:
			hash, err := r.resolveShortHash(item.Hash)
			if err != nil {
				return fmt.Errorf("rebase interactive: resolve %s: %w", item.Hash, err)
			}
			if err := r.replaySingleCommit(hash); err != nil {
				return err
			}
			// Amend the just-created commit with a new message from the editor.
			if err := r.rewordLastCommit(); err != nil {
				return fmt.Errorf("rebase interactive: reword: %w", err)
			}

		case TodoSquash:
			hash, err := r.resolveShortHash(item.Hash)
			if err != nil {
				return fmt.Errorf("rebase interactive: resolve %s: %w", item.Hash, err)
			}
			if err := r.squashCommit(hash, true); err != nil {
				return fmt.Errorf("rebase interactive: squash: %w", err)
			}

		case TodoFixup:
			hash, err := r.resolveShortHash(item.Hash)
			if err != nil {
				return fmt.Errorf("rebase interactive: resolve %s: %w", item.Hash, err)
			}
			if err := r.squashCommit(hash, false); err != nil {
				return fmt.Errorf("rebase interactive: fixup: %w", err)
			}

		case TodoDrop:
			// Simply skip this commit.
			continue

		case TodoExec:
			if err := r.execCommand(item.Message); err != nil {
				return fmt.Errorf("rebase interactive: exec %q: %w", item.Message, err)
			}
		}
	}

	return nil
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

	allPaths := collectAllPaths(baseMap, oursMap, theirsMap)

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
			if oursMap[path].BlobHash == theirsMap[path].BlobHash {
				continue
			}
			if oursMap[path].BlobHash == baseMap[path].BlobHash {
				content, err := r.readBlobData(theirsMap[path].BlobHash)
				if err != nil {
					return err
				}
				writes = append(writes, fileWrite{path, content, normalizeFileMode(theirsMap[path].Mode)})
				continue
			}
			if theirsMap[path].BlobHash == baseMap[path].BlobHash {
				continue
			}
			// Both changed - do merge.
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
			result, err := mergeFilesForRebase(path, baseData, oursData, theirsData)
			if err != nil {
				return err
			}
			writes = append(writes, fileWrite{path, result, normalizeFileMode(oursMap[path].Mode)})

		case !inBase && !inOurs && inTheirs:
			content, err := r.readBlobData(theirsMap[path].BlobHash)
			if err != nil {
				return err
			}
			writes = append(writes, fileWrite{path, content, normalizeFileMode(theirsMap[path].Mode)})

		case !inBase && inOurs && inTheirs:
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
			result, err := mergeFilesForRebase(path, nil, oursData, theirsData)
			if err != nil {
				return err
			}
			writes = append(writes, fileWrite{path, result, normalizeFileMode(oursMap[path].Mode)})

		case inBase && inOurs && !inTheirs:
			if oursMap[path].BlobHash == baseMap[path].BlobHash {
				deletes = append(deletes, path)
			}
			// If ours modified, keep ours (conflict scenarios not handled for simplicity in squash).

		case inBase && !inOurs && inTheirs:
			if theirsMap[path].BlobHash == baseMap[path].BlobHash {
				continue
			}
			content, err := r.readBlobData(theirsMap[path].BlobHash)
			if err != nil {
				return err
			}
			writes = append(writes, fileWrite{path, content, normalizeFileMode(theirsMap[path].Mode)})

		case !inBase && inOurs && !inTheirs:
			continue

		case inBase && !inOurs && !inTheirs:
			continue
		}
	}

	// Apply file changes to working directory.
	for _, w := range writes {
		absPath := filepath.Join(r.RootDir, filepath.FromSlash(w.path))
		if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
			return fmt.Errorf("mkdir: %w", err)
		}
		if err := os.WriteFile(absPath, w.content, filePermFromMode(w.mode)); err != nil {
			return fmt.Errorf("write %q: %w", w.path, err)
		}
	}
	for _, path := range deletes {
		absPath := filepath.Join(r.RootDir, filepath.FromSlash(path))
		if err := os.Remove(absPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove %q: %w", path, err)
		}
		r.removeEmptyParents(filepath.Dir(absPath))
	}

	// Stage everything.
	var pathsToStage []string
	for _, w := range writes {
		pathsToStage = append(pathsToStage, w.path)
	}
	if len(pathsToStage) > 0 {
		if err := r.Add(pathsToStage); err != nil {
			return fmt.Errorf("stage: %w", err)
		}
	}
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


// mergeFilesForRebase is a helper that performs a three-way merge and returns
// the merged content. Unlike the full replaySingleCommit, it does not track
// conflicts separately (used for squash/fixup where conflicts are less expected).
func mergeFilesForRebase(path string, base, ours, theirs []byte) ([]byte, error) {
	result, err := merge.MergeFiles(path, base, ours, theirs)
	if err != nil {
		return nil, fmt.Errorf("merge %q: %w", path, err)
	}
	return result.Merged, nil
}
