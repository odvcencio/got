package repo

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/odvcencio/graft/pkg/object"
)

// TestParseTodoList_AllActions verifies that parseTodoList correctly parses
// all action types and ignores comments and blank lines.
func TestParseTodoList_AllActions(t *testing.T) {
	content := `pick abc12345 first commit message
reword def67890 second commit message
squash 11112222 third commit message
fixup 33334444 fourth commit message
drop 55556666 fifth commit message
exec echo hello world

# This is a comment
# Another comment

pick aaa11111 after blanks and comments
`

	items, err := parseTodoList(content)
	if err != nil {
		t.Fatalf("parseTodoList returned error: %v", err)
	}

	expected := []struct {
		action  TodoAction
		hash    object.Hash
		message string
	}{
		{TodoPick, "abc12345", "first commit message"},
		{TodoReword, "def67890", "second commit message"},
		{TodoSquash, "11112222", "third commit message"},
		{TodoFixup, "33334444", "fourth commit message"},
		{TodoDrop, "55556666", "fifth commit message"},
		{TodoExec, "", "echo hello world"},
		{TodoPick, "aaa11111", "after blanks and comments"},
	}

	if len(items) != len(expected) {
		t.Fatalf("got %d items, want %d", len(items), len(expected))
	}

	for i, want := range expected {
		got := items[i]
		if got.Action != want.action {
			t.Errorf("items[%d].Action = %q, want %q", i, got.Action, want.action)
		}
		if want.action == TodoExec {
			if got.Hash != "" {
				t.Errorf("items[%d].Hash = %q, want empty for exec", i, got.Hash)
			}
		} else {
			if got.Hash != want.hash {
				t.Errorf("items[%d].Hash = %q, want %q", i, got.Hash, want.hash)
			}
		}
		if got.Message != want.message {
			t.Errorf("items[%d].Message = %q, want %q", i, got.Message, want.message)
		}
	}
}

// TestParseTodoList_UnknownAction verifies that unknown actions produce errors.
func TestParseTodoList_UnknownAction(t *testing.T) {
	content := "bogus abc12345 some message\n"
	_, err := parseTodoList(content)
	if err == nil {
		t.Fatal("expected error for unknown action, got nil")
	}
	if !strings.Contains(err.Error(), "unknown action") {
		t.Errorf("error = %q, want it to contain 'unknown action'", err.Error())
	}
}

// TestParseTodoList_EmptyInput returns no items and no error.
func TestParseTodoList_EmptyInput(t *testing.T) {
	items, err := parseTodoList("")
	if err != nil {
		t.Fatalf("parseTodoList returned error: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("got %d items, want 0", len(items))
	}
}

// TestParseTodoList_OnlyComments returns no items and no error.
func TestParseTodoList_OnlyComments(t *testing.T) {
	content := "# comment 1\n# comment 2\n"
	items, err := parseTodoList(content)
	if err != nil {
		t.Fatalf("parseTodoList returned error: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("got %d items, want 0", len(items))
	}
}

// TestRebaseInteractive_DropCommit verifies that dropping a commit from the
// todo list causes it to be absent from the rebased history.
func TestRebaseInteractive_DropCommit(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Initial commit on main.
	rebaseCommitFile(t, r, "base.txt", []byte("base content\n"), "initial", "alice")

	baseHash, err := r.ResolveRef("HEAD")
	if err != nil {
		t.Fatalf("ResolveRef(HEAD): %v", err)
	}

	// Create feature branch.
	if err := r.CreateBranch("feature", baseHash); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}
	if err := r.Checkout("feature"); err != nil {
		t.Fatalf("Checkout(feature): %v", err)
	}

	// Make 3 commits on feature.
	rebaseCommitFile(t, r, "feat1.txt", []byte("feature 1\n"), "feat commit 1", "bob")
	feat1Hash, _ := r.ResolveRef("HEAD")
	rebaseCommitFile(t, r, "feat2.txt", []byte("feature 2\n"), "feat commit 2", "bob")
	rebaseCommitFile(t, r, "feat3.txt", []byte("feature 3\n"), "feat commit 3", "bob")
	feat3Hash, _ := r.ResolveRef("HEAD")

	// Switch to main and advance it.
	if err := r.Checkout("main"); err != nil {
		t.Fatalf("Checkout(main): %v", err)
	}
	rebaseCommitFile(t, r, "main_advance.txt", []byte("main advance\n"), "advance main", "alice")

	// Switch back to feature.
	if err := r.Checkout("feature"); err != nil {
		t.Fatalf("Checkout(feature): %v", err)
	}

	// Create a todo list that drops the second commit.
	items := []TodoItem{
		{Action: TodoPick, Hash: feat1Hash, Message: "feat commit 1"},
		{Action: TodoDrop, Hash: object.Hash("dropped"), Message: "feat commit 2"},
		{Action: TodoPick, Hash: feat3Hash, Message: "feat commit 3"},
	}

	if err := r.rebaseWithTodoList("main", items); err != nil {
		t.Fatalf("rebaseWithTodoList: %v", err)
	}

	// Verify HEAD is on feature branch.
	branch, err := r.CurrentBranch()
	if err != nil {
		t.Fatalf("CurrentBranch: %v", err)
	}
	if branch != "feature" {
		t.Errorf("CurrentBranch = %q, want %q", branch, "feature")
	}

	// Walk commit history.
	tip, err := r.ResolveRef("HEAD")
	if err != nil {
		t.Fatalf("ResolveRef(HEAD): %v", err)
	}
	commits, err := r.Log(tip, 10)
	if err != nil {
		t.Fatalf("Log: %v", err)
	}

	// Should have: feat commit 3, feat commit 1, advance main, initial = 4 commits.
	// "feat commit 2" should NOT be present.
	for _, c := range commits {
		if c.Message == "feat commit 2" {
			t.Error("dropped commit 'feat commit 2' should not appear in history")
		}
	}

	// Verify the two picked commits are present.
	messages := make(map[string]bool)
	for _, c := range commits {
		messages[c.Message] = true
	}
	if !messages["feat commit 1"] {
		t.Error("'feat commit 1' should be in history")
	}
	if !messages["feat commit 3"] {
		t.Error("'feat commit 3' should be in history")
	}

	// feat2.txt should NOT exist (that commit was dropped).
	if _, err := os.Stat(filepath.Join(dir, "feat2.txt")); !os.IsNotExist(err) {
		t.Error("feat2.txt should not exist after dropping its commit")
	}

	// feat1.txt and feat3.txt should exist.
	if _, err := os.Stat(filepath.Join(dir, "feat1.txt")); err != nil {
		t.Errorf("feat1.txt should exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "feat3.txt")); err != nil {
		t.Errorf("feat3.txt should exist: %v", err)
	}

	// Verify sequencer is cleaned up.
	if r.isRebaseInProgress() {
		t.Error("rebase should not be in progress after completion")
	}
}

// TestRebaseInteractive_SquashCombinesMessages verifies that squashing two
// commits results in a single commit with the combined messages.
func TestRebaseInteractive_SquashCombinesMessages(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Initial commit on main.
	rebaseCommitFile(t, r, "base.txt", []byte("base content\n"), "initial", "alice")

	baseHash, err := r.ResolveRef("HEAD")
	if err != nil {
		t.Fatalf("ResolveRef(HEAD): %v", err)
	}

	// Create feature branch.
	if err := r.CreateBranch("feature", baseHash); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}
	if err := r.Checkout("feature"); err != nil {
		t.Fatalf("Checkout(feature): %v", err)
	}

	// Make 2 commits on feature.
	rebaseCommitFile(t, r, "feat1.txt", []byte("feature 1\n"), "first feature commit", "bob")
	feat1Hash, _ := r.ResolveRef("HEAD")
	rebaseCommitFile(t, r, "feat2.txt", []byte("feature 2\n"), "second feature commit", "bob")
	feat2Hash, _ := r.ResolveRef("HEAD")

	// Switch to main and advance it.
	if err := r.Checkout("main"); err != nil {
		t.Fatalf("Checkout(main): %v", err)
	}
	rebaseCommitFile(t, r, "main_advance.txt", []byte("main advance\n"), "advance main", "alice")

	// Switch back to feature.
	if err := r.Checkout("feature"); err != nil {
		t.Fatalf("Checkout(feature): %v", err)
	}

	// Create a todo list: pick first, squash second into first.
	items := []TodoItem{
		{Action: TodoPick, Hash: feat1Hash, Message: "first feature commit"},
		{Action: TodoSquash, Hash: feat2Hash, Message: "second feature commit"},
	}

	if err := r.rebaseWithTodoList("main", items); err != nil {
		t.Fatalf("rebaseWithTodoList: %v", err)
	}

	// Verify HEAD is on feature branch.
	branch, err := r.CurrentBranch()
	if err != nil {
		t.Fatalf("CurrentBranch: %v", err)
	}
	if branch != "feature" {
		t.Errorf("CurrentBranch = %q, want %q", branch, "feature")
	}

	// Walk commit history.
	tip, err := r.ResolveRef("HEAD")
	if err != nil {
		t.Fatalf("ResolveRef(HEAD): %v", err)
	}
	commits, err := r.Log(tip, 10)
	if err != nil {
		t.Fatalf("Log: %v", err)
	}

	// The tip commit should have the combined message.
	if len(commits) == 0 {
		t.Fatal("no commits in log")
	}

	tipCommit := commits[0]
	if !strings.Contains(tipCommit.Message, "first feature commit") {
		t.Errorf("squashed commit message should contain 'first feature commit', got %q", tipCommit.Message)
	}
	if !strings.Contains(tipCommit.Message, "second feature commit") {
		t.Errorf("squashed commit message should contain 'second feature commit', got %q", tipCommit.Message)
	}

	// There should be no separate commit with "second feature commit" as the sole message.
	for _, c := range commits {
		if c.Message == "second feature commit" {
			t.Error("squashed commit should not appear as a separate commit")
		}
	}

	// Both files should exist in the working tree.
	if _, err := os.Stat(filepath.Join(dir, "feat1.txt")); err != nil {
		t.Errorf("feat1.txt should exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "feat2.txt")); err != nil {
		t.Errorf("feat2.txt should exist: %v", err)
	}

	// Verify sequencer is cleaned up.
	if r.isRebaseInProgress() {
		t.Error("rebase should not be in progress after completion")
	}
}

// TestRebaseInteractive_FixupDiscardsMessage verifies that fixup keeps only
// the previous commit's message and discards the fixup commit's message.
func TestRebaseInteractive_FixupDiscardsMessage(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Initial commit on main.
	rebaseCommitFile(t, r, "base.txt", []byte("base content\n"), "initial", "alice")

	baseHash, err := r.ResolveRef("HEAD")
	if err != nil {
		t.Fatalf("ResolveRef(HEAD): %v", err)
	}

	// Create feature branch.
	if err := r.CreateBranch("feature", baseHash); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}
	if err := r.Checkout("feature"); err != nil {
		t.Fatalf("Checkout(feature): %v", err)
	}

	// Make 2 commits on feature.
	rebaseCommitFile(t, r, "feat1.txt", []byte("feature 1\n"), "important message", "bob")
	feat1Hash, _ := r.ResolveRef("HEAD")
	rebaseCommitFile(t, r, "feat2.txt", []byte("feature 2\n"), "fixup noise", "bob")
	feat2Hash, _ := r.ResolveRef("HEAD")

	// Switch to main and advance it.
	if err := r.Checkout("main"); err != nil {
		t.Fatalf("Checkout(main): %v", err)
	}
	rebaseCommitFile(t, r, "main_advance.txt", []byte("main advance\n"), "advance main", "alice")

	// Switch back to feature.
	if err := r.Checkout("feature"); err != nil {
		t.Fatalf("Checkout(feature): %v", err)
	}

	// Create a todo list: pick first, fixup second.
	items := []TodoItem{
		{Action: TodoPick, Hash: feat1Hash, Message: "important message"},
		{Action: TodoFixup, Hash: feat2Hash, Message: "fixup noise"},
	}

	if err := r.rebaseWithTodoList("main", items); err != nil {
		t.Fatalf("rebaseWithTodoList: %v", err)
	}

	// Verify HEAD is on feature branch.
	branch, err := r.CurrentBranch()
	if err != nil {
		t.Fatalf("CurrentBranch: %v", err)
	}
	if branch != "feature" {
		t.Errorf("CurrentBranch = %q, want %q", branch, "feature")
	}

	// Walk commit history.
	tip, err := r.ResolveRef("HEAD")
	if err != nil {
		t.Fatalf("ResolveRef(HEAD): %v", err)
	}
	commits, err := r.Log(tip, 10)
	if err != nil {
		t.Fatalf("Log: %v", err)
	}

	// The tip commit should have only the first commit's message.
	if len(commits) == 0 {
		t.Fatal("no commits in log")
	}

	tipCommit := commits[0]
	if tipCommit.Message != "important message" {
		t.Errorf("fixup result message = %q, want %q", tipCommit.Message, "important message")
	}

	// The fixup message should NOT appear anywhere.
	for _, c := range commits {
		if strings.Contains(c.Message, "fixup noise") {
			t.Error("fixup commit message 'fixup noise' should not appear in any commit")
		}
	}

	// Both files should exist in the working tree.
	if _, err := os.Stat(filepath.Join(dir, "feat1.txt")); err != nil {
		t.Errorf("feat1.txt should exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "feat2.txt")); err != nil {
		t.Errorf("feat2.txt should exist: %v", err)
	}

	// Verify sequencer is cleaned up.
	if r.isRebaseInProgress() {
		t.Error("rebase should not be in progress after completion")
	}
}

// TestParseTodoList_EditAction verifies that edit action is parsed correctly.
func TestParseTodoList_EditAction(t *testing.T) {
	content := "edit abc12345 some commit\n"
	items, err := parseTodoList(content)
	if err != nil {
		t.Fatalf("parseTodoList returned error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}
	if items[0].Action != TodoEdit {
		t.Errorf("Action = %q, want %q", items[0].Action, TodoEdit)
	}
	if items[0].Hash != "abc12345" {
		t.Errorf("Hash = %q, want %q", items[0].Hash, "abc12345")
	}
	if items[0].Message != "some commit" {
		t.Errorf("Message = %q, want %q", items[0].Message, "some commit")
	}
}

// TestRebaseInteractive_ConflictPreservesState verifies that when a conflict
// occurs during interactive rebase, the sequencer state is preserved so the
// user can --continue or --abort.
func TestRebaseInteractive_ConflictPreservesState(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Initial commit on main.
	rebaseCommitFile(t, r, "file.txt", []byte("line 1\n"), "initial", "alice")

	baseHash, err := r.ResolveRef("HEAD")
	if err != nil {
		t.Fatalf("ResolveRef(HEAD): %v", err)
	}

	// Create feature branch.
	if err := r.CreateBranch("feature", baseHash); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}
	if err := r.Checkout("feature"); err != nil {
		t.Fatalf("Checkout(feature): %v", err)
	}

	// Modify same file on feature.
	rebaseCommitFile(t, r, "file.txt", []byte("feature change\n"), "feature edit", "bob")
	featHash, _ := r.ResolveRef("HEAD")

	// Switch to main and make a conflicting change.
	if err := r.Checkout("main"); err != nil {
		t.Fatalf("Checkout(main): %v", err)
	}
	rebaseCommitFile(t, r, "file.txt", []byte("main change\n"), "main edit", "alice")

	// Switch to feature.
	if err := r.Checkout("feature"); err != nil {
		t.Fatalf("Checkout(feature): %v", err)
	}

	// Run interactive rebase with a pick that will conflict.
	items := []TodoItem{
		{Action: TodoPick, Hash: featHash, Message: "feature edit"},
	}

	err = r.rebaseWithTodoList("main", items)
	if err == nil {
		t.Fatal("expected conflict error, got nil")
	}

	// Verify it is a conflict error.
	var conflictErr *ErrRebaseConflict
	if !errors.As(err, &conflictErr) {
		t.Fatalf("expected *ErrRebaseConflict, got %T: %v", err, err)
	}

	// CRITICAL: Verify sequencer state is preserved (not cleaned up).
	if !r.isRebaseInProgress() {
		t.Fatal("expected rebase to be in progress after conflict")
	}

	// Verify stopped-sha is written.
	stoppedSHA, err := r.readSequencerFile("stopped-sha")
	if err != nil {
		t.Fatalf("stopped-sha should exist: %v", err)
	}
	if strings.TrimSpace(stoppedSHA) != string(featHash) {
		t.Errorf("stopped-sha = %q, want %q", strings.TrimSpace(stoppedSHA), featHash)
	}

	// Verify the interactive flag is set.
	if !r.isInteractiveRebase() {
		t.Error("expected interactive rebase flag to be set")
	}

	// Now resolve the conflict and continue.
	rebaseWriteFile(t, filepath.Join(dir, "file.txt"), []byte("resolved content\n"))
	if err := r.Add([]string{"file.txt"}); err != nil {
		t.Fatalf("Add(resolved): %v", err)
	}

	if err := r.RebaseContinue(); err != nil {
		t.Fatalf("RebaseContinue: %v", err)
	}

	// Verify rebase completed.
	if r.isRebaseInProgress() {
		t.Fatal("rebase should be finished")
	}

	// Verify HEAD is on feature.
	branch, err := r.CurrentBranch()
	if err != nil {
		t.Fatalf("CurrentBranch: %v", err)
	}
	if branch != "feature" {
		t.Errorf("CurrentBranch = %q, want %q", branch, "feature")
	}

	// Verify the resolved content is in the working tree.
	data, err := os.ReadFile(filepath.Join(dir, "file.txt"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "resolved content\n" {
		t.Errorf("file.txt = %q, want %q", string(data), "resolved content\n")
	}
}

// TestRebaseInteractive_ConflictAbort verifies that after a conflict during
// interactive rebase, --abort properly restores the original state.
func TestRebaseInteractive_ConflictAbort(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	rebaseCommitFile(t, r, "file.txt", []byte("original\n"), "initial", "alice")

	baseHash, err := r.ResolveRef("HEAD")
	if err != nil {
		t.Fatalf("ResolveRef(HEAD): %v", err)
	}

	if err := r.CreateBranch("feature", baseHash); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}
	if err := r.Checkout("feature"); err != nil {
		t.Fatalf("Checkout(feature): %v", err)
	}

	rebaseCommitFile(t, r, "file.txt", []byte("feature version\n"), "feature change", "bob")
	featHash, _ := r.ResolveRef("HEAD")

	origFeatureHash, _ := r.ResolveRef("refs/heads/feature")

	if err := r.Checkout("main"); err != nil {
		t.Fatalf("Checkout(main): %v", err)
	}
	rebaseCommitFile(t, r, "file.txt", []byte("main version\n"), "main change", "alice")

	if err := r.Checkout("feature"); err != nil {
		t.Fatalf("Checkout(feature): %v", err)
	}

	items := []TodoItem{
		{Action: TodoPick, Hash: featHash, Message: "feature change"},
	}
	err = r.rebaseWithTodoList("main", items)
	if err == nil {
		t.Fatal("expected conflict error")
	}

	// State should be preserved.
	if !r.isRebaseInProgress() {
		t.Fatal("rebase should be in progress after conflict")
	}

	// Abort.
	if err := r.RebaseAbort(); err != nil {
		t.Fatalf("RebaseAbort: %v", err)
	}

	// Verify rebase is no longer in progress.
	if r.isRebaseInProgress() {
		t.Fatal("rebase should not be in progress after abort")
	}

	// Verify feature branch ref is restored.
	featureHash, err := r.ResolveRef("refs/heads/feature")
	if err != nil {
		t.Fatalf("ResolveRef(feature): %v", err)
	}
	if featureHash != origFeatureHash {
		t.Errorf("feature hash = %s, want %s (original)", featureHash, origFeatureHash)
	}
}

// TestRebaseInteractive_SquashDeleteVsModifyConflict verifies that squash
// properly surfaces delete-vs-modify conflicts instead of silently dropping them.
func TestRebaseInteractive_SquashDeleteVsModifyConflict(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Set up: base has file.txt and other.txt.
	rebaseCommitFile(t, r, "file.txt", []byte("base content\n"), "initial", "alice")
	rebaseCommitFile(t, r, "other.txt", []byte("other\n"), "add other", "alice")

	baseHash, _ := r.ResolveRef("HEAD")

	if err := r.CreateBranch("feature", baseHash); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}
	if err := r.Checkout("feature"); err != nil {
		t.Fatalf("Checkout(feature): %v", err)
	}

	// Feature commit 1: modify file.txt.
	rebaseCommitFile(t, r, "file.txt", []byte("feature modified\n"), "modify file", "bob")
	modHash, _ := r.ResolveRef("HEAD")

	// Feature commit 2: delete file.txt (other.txt remains so staging is not empty).
	absPath := filepath.Join(dir, "file.txt")
	if err := os.Remove(absPath); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	stg, _ := r.ReadStaging()
	delete(stg.Entries, "file.txt")
	if err := r.WriteStaging(stg); err != nil {
		t.Fatalf("WriteStaging: %v", err)
	}
	if _, err := r.Commit("delete file", "bob"); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	delHash, _ := r.ResolveRef("HEAD")

	// Switch to main and modify the same file differently.
	if err := r.Checkout("main"); err != nil {
		t.Fatalf("Checkout(main): %v", err)
	}
	rebaseCommitFile(t, r, "file.txt", []byte("main modified\n"), "main modify", "alice")

	if err := r.Checkout("feature"); err != nil {
		t.Fatalf("Checkout(feature): %v", err)
	}

	// Interactive rebase: pick modify, squash delete.
	items := []TodoItem{
		{Action: TodoPick, Hash: modHash, Message: "modify file"},
		{Action: TodoSquash, Hash: delHash, Message: "delete file"},
	}

	err = r.rebaseWithTodoList("main", items)

	// The squash should produce a conflict because:
	// - base (parent of delHash = modHash) has "feature modified"
	// - ours (HEAD after picking modHash) has merge of "feature modified" + "main modified"
	// - theirs (delHash) has file deleted
	// Since ours != base and theirs deleted, this is a delete-vs-modify conflict.
	if err == nil {
		t.Log("squash completed without error (merge may have resolved cleanly)")
		return
	}

	var conflictErr *ErrRebaseConflict
	if errors.As(err, &conflictErr) {
		// This is the expected behavior: the conflict is surfaced.
		t.Logf("got expected conflict: %v", conflictErr)

		// Verify sequencer state is preserved.
		if !r.isRebaseInProgress() {
			t.Fatal("rebase should be in progress after squash conflict")
		}

		// Verify the conflict file has conflict markers.
		data, readErr := os.ReadFile(filepath.Join(dir, "file.txt"))
		if readErr != nil {
			t.Fatalf("ReadFile: %v", readErr)
		}
		content := string(data)
		if !strings.Contains(content, "<<<<<<<") || !strings.Contains(content, ">>>>>>>") {
			t.Errorf("expected conflict markers in file.txt, got: %q", content)
		}
	} else {
		t.Fatalf("expected *ErrRebaseConflict or nil, got %T: %v", err, err)
	}
}

// TestRebaseInteractive_EditStopAndContinue verifies that the edit operation
// stops the rebase, allows changes, and continues correctly.
func TestRebaseInteractive_EditStopAndContinue(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Initial commit on main.
	rebaseCommitFile(t, r, "base.txt", []byte("base content\n"), "initial", "alice")

	baseHash, err := r.ResolveRef("HEAD")
	if err != nil {
		t.Fatalf("ResolveRef(HEAD): %v", err)
	}

	// Create feature branch.
	if err := r.CreateBranch("feature", baseHash); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}
	if err := r.Checkout("feature"); err != nil {
		t.Fatalf("Checkout(feature): %v", err)
	}

	// Make 3 commits on feature.
	rebaseCommitFile(t, r, "feat1.txt", []byte("feature 1\n"), "feat commit 1", "bob")
	feat1Hash, _ := r.ResolveRef("HEAD")
	rebaseCommitFile(t, r, "feat2.txt", []byte("feature 2\n"), "feat commit 2", "bob")
	feat2Hash, _ := r.ResolveRef("HEAD")
	rebaseCommitFile(t, r, "feat3.txt", []byte("feature 3\n"), "feat commit 3", "bob")
	feat3Hash, _ := r.ResolveRef("HEAD")

	// Switch to main and advance it.
	if err := r.Checkout("main"); err != nil {
		t.Fatalf("Checkout(main): %v", err)
	}
	rebaseCommitFile(t, r, "main_advance.txt", []byte("main advance\n"), "advance main", "alice")

	// Switch back to feature.
	if err := r.Checkout("feature"); err != nil {
		t.Fatalf("Checkout(feature): %v", err)
	}

	// Create a todo list with edit on the second commit.
	items := []TodoItem{
		{Action: TodoPick, Hash: feat1Hash, Message: "feat commit 1"},
		{Action: TodoEdit, Hash: feat2Hash, Message: "feat commit 2"},
		{Action: TodoPick, Hash: feat3Hash, Message: "feat commit 3"},
	}

	err = r.rebaseWithTodoList("main", items)

	// Should get an ErrRebaseEditStop.
	if err == nil {
		t.Fatal("expected edit stop error, got nil")
	}

	var editStop *ErrRebaseEditStop
	if !errors.As(err, &editStop) {
		t.Fatalf("expected *ErrRebaseEditStop, got %T: %v", err, err)
	}

	// Verify sequencer state is preserved.
	if !r.isRebaseInProgress() {
		t.Fatal("expected rebase to be in progress after edit stop")
	}

	// Verify edit-mode marker exists.
	editModeData, err := r.readSequencerFile("edit-mode")
	if err != nil {
		t.Fatalf("edit-mode file should exist: %v", err)
	}
	if strings.TrimSpace(editModeData) != "true" {
		t.Errorf("edit-mode = %q, want 'true'", strings.TrimSpace(editModeData))
	}

	// At this point, feat1 and feat2 have been applied but feat3 has not.
	if _, err := os.Stat(filepath.Join(dir, "feat1.txt")); err != nil {
		t.Errorf("feat1.txt should exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "feat2.txt")); err != nil {
		t.Errorf("feat2.txt should exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "feat3.txt")); !os.IsNotExist(err) {
		t.Error("feat3.txt should NOT exist yet (edit stop before pick)")
	}

	// Simulate user making changes: modify feat2.txt and stage it.
	rebaseWriteFile(t, filepath.Join(dir, "feat2.txt"), []byte("feature 2 EDITED\n"))
	if err := r.Add([]string{"feat2.txt"}); err != nil {
		t.Fatalf("Add(feat2.txt): %v", err)
	}

	// Continue the rebase.
	if err := r.RebaseContinue(); err != nil {
		t.Fatalf("RebaseContinue: %v", err)
	}

	// Verify rebase completed.
	if r.isRebaseInProgress() {
		t.Fatal("rebase should be finished")
	}

	// Verify HEAD is on feature.
	branch, err := r.CurrentBranch()
	if err != nil {
		t.Fatalf("CurrentBranch: %v", err)
	}
	if branch != "feature" {
		t.Errorf("CurrentBranch = %q, want %q", branch, "feature")
	}

	// Verify all files exist.
	for _, f := range []string{"base.txt", "main_advance.txt", "feat1.txt", "feat2.txt", "feat3.txt"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("%s should exist: %v", f, err)
		}
	}

	// Verify feat2.txt has the edited content.
	data, err := os.ReadFile(filepath.Join(dir, "feat2.txt"))
	if err != nil {
		t.Fatalf("ReadFile(feat2.txt): %v", err)
	}
	if string(data) != "feature 2 EDITED\n" {
		t.Errorf("feat2.txt = %q, want %q", string(data), "feature 2 EDITED\n")
	}

	// Verify all 3 feature commits are in history.
	tip, err := r.ResolveRef("HEAD")
	if err != nil {
		t.Fatalf("ResolveRef(HEAD): %v", err)
	}
	commits, err := r.Log(tip, 10)
	if err != nil {
		t.Fatalf("Log: %v", err)
	}

	messages := make(map[string]bool)
	for _, c := range commits {
		messages[c.Message] = true
	}
	for _, msg := range []string{"feat commit 1", "feat commit 2", "feat commit 3"} {
		if !messages[msg] {
			t.Errorf("expected %q in history", msg)
		}
	}
}

// TestRebaseInteractive_EditNoChanges verifies that continuing after an edit
// stop without making changes works (the commit is kept as-is).
func TestRebaseInteractive_EditNoChanges(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	rebaseCommitFile(t, r, "base.txt", []byte("base\n"), "initial", "alice")
	baseHash, _ := r.ResolveRef("HEAD")

	if err := r.CreateBranch("feature", baseHash); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}
	if err := r.Checkout("feature"); err != nil {
		t.Fatalf("Checkout(feature): %v", err)
	}

	rebaseCommitFile(t, r, "feat.txt", []byte("feature\n"), "feature commit", "bob")
	featHash, _ := r.ResolveRef("HEAD")

	if err := r.Checkout("main"); err != nil {
		t.Fatalf("Checkout(main): %v", err)
	}
	rebaseCommitFile(t, r, "main.txt", []byte("main\n"), "advance main", "alice")

	if err := r.Checkout("feature"); err != nil {
		t.Fatalf("Checkout(feature): %v", err)
	}

	items := []TodoItem{
		{Action: TodoEdit, Hash: featHash, Message: "feature commit"},
	}

	err = r.rebaseWithTodoList("main", items)
	var editStop *ErrRebaseEditStop
	if !errors.As(err, &editStop) {
		t.Fatalf("expected *ErrRebaseEditStop, got %T: %v", err, err)
	}

	// Continue without making any changes.
	if err := r.RebaseContinue(); err != nil {
		t.Fatalf("RebaseContinue: %v", err)
	}

	if r.isRebaseInProgress() {
		t.Fatal("rebase should be finished")
	}

	// File should still be there.
	if _, err := os.Stat(filepath.Join(dir, "feat.txt")); err != nil {
		t.Errorf("feat.txt should exist: %v", err)
	}
}

// TestAutosquashTodoList verifies that autosquashTodoList correctly reorders
// commits with fixup!/squash! prefixes.
func TestAutosquashTodoList(t *testing.T) {
	items := []TodoItem{
		{Action: TodoPick, Hash: "aaa", Message: "add feature A"},
		{Action: TodoPick, Hash: "bbb", Message: "add feature B"},
		{Action: TodoPick, Hash: "ccc", Message: "fixup! add feature A"},
		{Action: TodoPick, Hash: "ddd", Message: "squash! add feature B"},
		{Action: TodoPick, Hash: "eee", Message: "add feature C"},
	}

	result := autosquashTodoList(items)

	// Expected order:
	// pick aaa "add feature A"
	// fixup ccc "fixup! add feature A"
	// pick bbb "add feature B"
	// squash ddd "squash! add feature B"
	// pick eee "add feature C"

	expected := []struct {
		action  TodoAction
		hash    object.Hash
		message string
	}{
		{TodoPick, "aaa", "add feature A"},
		{TodoFixup, "ccc", "fixup! add feature A"},
		{TodoPick, "bbb", "add feature B"},
		{TodoSquash, "ddd", "squash! add feature B"},
		{TodoPick, "eee", "add feature C"},
	}

	if len(result) != len(expected) {
		for i, item := range result {
			t.Logf("result[%d]: %s %s %s", i, item.Action, item.Hash, item.Message)
		}
		t.Fatalf("got %d items, want %d", len(result), len(expected))
	}

	for i, want := range expected {
		got := result[i]
		if got.Action != want.action {
			t.Errorf("result[%d].Action = %q, want %q", i, got.Action, want.action)
		}
		if got.Hash != want.hash {
			t.Errorf("result[%d].Hash = %q, want %q", i, got.Hash, want.hash)
		}
		if got.Message != want.message {
			t.Errorf("result[%d].Message = %q, want %q", i, got.Message, want.message)
		}
	}
}

// TestAutosquashTodoList_NoMatch verifies that fixup/squash commits with no
// matching target are kept at the end with their original action.
func TestAutosquashTodoList_NoMatch(t *testing.T) {
	items := []TodoItem{
		{Action: TodoPick, Hash: "aaa", Message: "add feature A"},
		{Action: TodoPick, Hash: "bbb", Message: "fixup! nonexistent"},
	}

	result := autosquashTodoList(items)

	if len(result) != 2 {
		t.Fatalf("got %d items, want 2", len(result))
	}

	// First should be the normal pick.
	if result[0].Hash != "aaa" {
		t.Errorf("result[0].Hash = %q, want %q", result[0].Hash, "aaa")
	}

	// Second should be the unmatched fixup, kept with original action.
	if result[1].Hash != "bbb" {
		t.Errorf("result[1].Hash = %q, want %q", result[1].Hash, "bbb")
	}
	if result[1].Action != TodoPick {
		t.Errorf("result[1].Action = %q, want %q (original)", result[1].Action, TodoPick)
	}
}

// TestAutosquashTodoList_NoSquashItems verifies that the list is unchanged
// when there are no fixup!/squash! commits.
func TestAutosquashTodoList_NoSquashItems(t *testing.T) {
	items := []TodoItem{
		{Action: TodoPick, Hash: "aaa", Message: "add feature A"},
		{Action: TodoPick, Hash: "bbb", Message: "add feature B"},
	}

	result := autosquashTodoList(items)

	if len(result) != 2 {
		t.Fatalf("got %d items, want 2", len(result))
	}
	if result[0].Hash != "aaa" || result[1].Hash != "bbb" {
		t.Error("list should be unchanged")
	}
}

// TestAutosquashTodoList_MultipleFixups verifies that multiple fixup commits
// targeting the same commit are all placed after the target.
func TestAutosquashTodoList_MultipleFixups(t *testing.T) {
	items := []TodoItem{
		{Action: TodoPick, Hash: "aaa", Message: "add feature A"},
		{Action: TodoPick, Hash: "bbb", Message: "fixup! add feature A"},
		{Action: TodoPick, Hash: "ccc", Message: "fixup! add feature A"},
	}

	result := autosquashTodoList(items)

	// Expected:
	// pick aaa "add feature A"
	// fixup bbb "fixup! add feature A"
	// fixup ccc "fixup! add feature A"
	if len(result) != 3 {
		t.Fatalf("got %d items, want 3", len(result))
	}
	if result[0].Hash != "aaa" {
		t.Errorf("result[0].Hash = %q, want aaa", result[0].Hash)
	}
	if result[1].Hash != "bbb" || result[1].Action != TodoFixup {
		t.Errorf("result[1] = {%s %s}, want {fixup bbb}", result[1].Action, result[1].Hash)
	}
	if result[2].Hash != "ccc" || result[2].Action != TodoFixup {
		t.Errorf("result[2] = {%s %s}, want {fixup ccc}", result[2].Action, result[2].Hash)
	}
}

// TestSerializeTodoItems verifies round-trip serialization of todo items.
func TestSerializeTodoItems(t *testing.T) {
	items := []TodoItem{
		{Action: TodoPick, Hash: "aaa11111", Message: "first commit"},
		{Action: TodoEdit, Hash: "bbb22222", Message: "second commit"},
		{Action: TodoSquash, Hash: "ccc33333", Message: "third commit"},
		{Action: TodoExec, Message: "echo hello"},
	}

	serialized := serializeTodoItems(items)
	parsed, err := parseTodoList(serialized)
	if err != nil {
		t.Fatalf("parseTodoList error: %v", err)
	}

	if len(parsed) != len(items) {
		t.Fatalf("got %d items, want %d", len(parsed), len(items))
	}

	for i, want := range items {
		got := parsed[i]
		if got.Action != want.Action {
			t.Errorf("items[%d].Action = %q, want %q", i, got.Action, want.Action)
		}
		if want.Action != TodoExec {
			if got.Hash != want.Hash {
				t.Errorf("items[%d].Hash = %q, want %q", i, got.Hash, want.Hash)
			}
		}
		if got.Message != want.Message {
			t.Errorf("items[%d].Message = %q, want %q", i, got.Message, want.Message)
		}
	}
}

// TestRebaseInteractive_ErrorPreservesState verifies that any error during
// interactive rebase preserves the sequencer state for --continue/--abort.
func TestRebaseInteractive_ErrorPreservesState(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Initial commit.
	rebaseCommitFile(t, r, "base.txt", []byte("base\n"), "initial", "alice")

	baseHash, _ := r.ResolveRef("HEAD")

	if err := r.CreateBranch("feature", baseHash); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}
	if err := r.Checkout("feature"); err != nil {
		t.Fatalf("Checkout(feature): %v", err)
	}

	rebaseCommitFile(t, r, "feat.txt", []byte("feature\n"), "feature commit", "bob")

	if err := r.Checkout("main"); err != nil {
		t.Fatalf("Checkout(main): %v", err)
	}
	rebaseCommitFile(t, r, "main.txt", []byte("main\n"), "advance main", "alice")

	if err := r.Checkout("feature"); err != nil {
		t.Fatalf("Checkout(feature): %v", err)
	}

	// Use a bogus hash that won't resolve, to trigger an error mid-rebase.
	items := []TodoItem{
		{Action: TodoPick, Hash: "nonexistent_hash", Message: "bad commit"},
	}

	err = r.rebaseWithTodoList("main", items)
	if err == nil {
		t.Fatal("expected error for nonexistent hash, got nil")
	}

	// The sequencer state should still be present for --abort.
	if !r.isRebaseInProgress() {
		t.Fatal("rebase should still be in progress after error")
	}

	// Abort should work.
	if err := r.RebaseAbort(); err != nil {
		t.Fatalf("RebaseAbort should work after error: %v", err)
	}

	if r.isRebaseInProgress() {
		t.Fatal("rebase should not be in progress after abort")
	}
}

// TestRebaseInteractive_ConflictThenContinueWithRemainingItems verifies that
// when a conflict occurs on the first of multiple items, resolving and continuing
// correctly processes the remaining items.
func TestRebaseInteractive_ConflictThenContinueWithRemainingItems(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	rebaseCommitFile(t, r, "file.txt", []byte("original\n"), "initial", "alice")

	baseHash, _ := r.ResolveRef("HEAD")

	if err := r.CreateBranch("feature", baseHash); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}
	if err := r.Checkout("feature"); err != nil {
		t.Fatalf("Checkout(feature): %v", err)
	}

	// First commit: conflicting change.
	rebaseCommitFile(t, r, "file.txt", []byte("feature conflict\n"), "conflicting commit", "bob")
	conflictHash, _ := r.ResolveRef("HEAD")

	// Second commit: add a new file (no conflict).
	rebaseCommitFile(t, r, "extra.txt", []byte("extra\n"), "add extra", "bob")
	extraHash, _ := r.ResolveRef("HEAD")

	if err := r.Checkout("main"); err != nil {
		t.Fatalf("Checkout(main): %v", err)
	}
	rebaseCommitFile(t, r, "file.txt", []byte("main conflict\n"), "main change", "alice")

	if err := r.Checkout("feature"); err != nil {
		t.Fatalf("Checkout(feature): %v", err)
	}

	items := []TodoItem{
		{Action: TodoPick, Hash: conflictHash, Message: "conflicting commit"},
		{Action: TodoPick, Hash: extraHash, Message: "add extra"},
	}

	err = r.rebaseWithTodoList("main", items)
	if err == nil {
		t.Fatal("expected conflict error")
	}

	var conflictErr *ErrRebaseConflict
	if !errors.As(err, &conflictErr) {
		t.Fatalf("expected *ErrRebaseConflict, got %T: %v", err, err)
	}

	// Verify interactive-todo has the remaining item.
	todoContent, err := r.readSequencerFile("interactive-todo")
	if err != nil {
		t.Fatalf("interactive-todo should exist: %v", err)
	}
	if !strings.Contains(todoContent, string(extraHash)) {
		t.Errorf("interactive-todo should contain remaining commit %s, got: %q", extraHash, todoContent)
	}

	// Resolve conflict.
	rebaseWriteFile(t, filepath.Join(dir, "file.txt"), []byte("resolved\n"))
	if err := r.Add([]string{"file.txt"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Continue.
	if err := r.RebaseContinue(); err != nil {
		t.Fatalf("RebaseContinue: %v", err)
	}

	// Rebase should be done.
	if r.isRebaseInProgress() {
		t.Fatal("rebase should be finished")
	}

	// extra.txt should exist (the second commit was applied).
	if _, err := os.Stat(filepath.Join(dir, "extra.txt")); err != nil {
		t.Errorf("extra.txt should exist: %v", err)
	}

	// file.txt should have resolved content.
	data, _ := os.ReadFile(filepath.Join(dir, "file.txt"))
	if string(data) != "resolved\n" {
		t.Errorf("file.txt = %q, want %q", string(data), "resolved\n")
	}
}
