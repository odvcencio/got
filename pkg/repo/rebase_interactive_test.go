package repo

import (
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
