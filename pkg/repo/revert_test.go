package repo

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRevert_CleanRevert verifies that reverting a commit cleanly undoes its
// changes and creates a new commit with the expected revert message.
func TestRevert_CleanRevert(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Create base commit with a file.
	writeTestFile(t, filepath.Join(dir, "a.txt"), []byte("line1\n"))
	if err := r.Add([]string{"a.txt"}); err != nil {
		t.Fatalf("Add(base): %v", err)
	}
	_, err = r.Commit("base commit", "alice")
	if err != nil {
		t.Fatalf("Commit(base): %v", err)
	}

	// Create a second commit that adds a line.
	writeTestFile(t, filepath.Join(dir, "a.txt"), []byte("line1\nline2\n"))
	if err := r.Add([]string{"a.txt"}); err != nil {
		t.Fatalf("Add(second): %v", err)
	}
	secondHash, err := r.Commit("add line2", "bob")
	if err != nil {
		t.Fatalf("Commit(second): %v", err)
	}

	// Revert the second commit (should remove line2).
	result, err := r.Revert(secondHash)
	if err != nil {
		t.Fatalf("Revert: %v", err)
	}

	// Verify the result.
	if result.TargetCommit != secondHash {
		t.Errorf("TargetCommit = %s, want %s", result.TargetCommit, secondHash)
	}
	if !strings.Contains(result.Message, "Revert") {
		t.Errorf("Message = %q, want to contain %q", result.Message, "Revert")
	}
	if !strings.Contains(result.Message, "add line2") {
		t.Errorf("Message = %q, want to contain original message %q", result.Message, "add line2")
	}

	// Verify a.txt was reverted to just line1.
	content, err := os.ReadFile(filepath.Join(dir, "a.txt"))
	if err != nil {
		t.Fatalf("ReadFile(a.txt): %v", err)
	}
	if string(content) != "line1\n" {
		t.Errorf("a.txt = %q, want %q", string(content), "line1\n")
	}

	// Verify HEAD was updated.
	headHash, err := r.ResolveRef("HEAD")
	if err != nil {
		t.Fatalf("ResolveRef(HEAD): %v", err)
	}
	if headHash != result.CommitHash {
		t.Errorf("HEAD = %s, want %s", headHash, result.CommitHash)
	}

	// Verify parent is the second commit (the one we reverted).
	newCommit, err := r.Store.ReadCommit(result.CommitHash)
	if err != nil {
		t.Fatalf("ReadCommit: %v", err)
	}
	if len(newCommit.Parents) != 1 || newCommit.Parents[0] != secondHash {
		t.Errorf("parents = %v, want [%s]", newCommit.Parents, secondHash)
	}
}

// TestRevert_ConflictSavesState verifies that a revert with conflicts saves
// sequencer state and returns an ErrRevertConflict.
func TestRevert_ConflictSavesState(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Create base commit.
	writeTestFile(t, filepath.Join(dir, "file.txt"), []byte("original\n"))
	if err := r.Add([]string{"file.txt"}); err != nil {
		t.Fatalf("Add(base): %v", err)
	}
	_, err = r.Commit("base", "alice")
	if err != nil {
		t.Fatalf("Commit(base): %v", err)
	}

	// Second commit changes the file.
	writeTestFile(t, filepath.Join(dir, "file.txt"), []byte("changed\n"))
	if err := r.Add([]string{"file.txt"}); err != nil {
		t.Fatalf("Add(second): %v", err)
	}
	secondHash, err := r.Commit("change file", "bob")
	if err != nil {
		t.Fatalf("Commit(second): %v", err)
	}

	// Third commit further modifies the file in a conflicting way.
	writeTestFile(t, filepath.Join(dir, "file.txt"), []byte("further changed\n"))
	if err := r.Add([]string{"file.txt"}); err != nil {
		t.Fatalf("Add(third): %v", err)
	}
	_, err = r.Commit("further change", "carol")
	if err != nil {
		t.Fatalf("Commit(third): %v", err)
	}

	// Revert the second commit; this should conflict because HEAD has diverged.
	_, err = r.Revert(secondHash)
	if err == nil {
		t.Fatal("Revert should fail on conflict, got nil error")
	}

	var revertErr *ErrRevertConflict
	if !errors.As(err, &revertErr) {
		t.Fatalf("error type = %T, want *ErrRevertConflict; error = %v", err, err)
	}

	// Verify sequencer state was saved.
	if !r.IsRevertInProgress() {
		t.Fatal("IsRevertInProgress should be true after conflict")
	}

	targetHashStr, err := r.revertSeq().ReadFile("target-hash")
	if err != nil {
		t.Fatalf("ReadFile(target-hash): %v", err)
	}
	if targetHashStr != string(secondHash) {
		t.Errorf("saved target-hash = %q, want %q", targetHashStr, secondHash)
	}
}

// TestRevert_ContinueAfterConflict verifies that after resolving conflicts,
// RevertContinue creates the revert commit and cleans up state.
func TestRevert_ContinueAfterConflict(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Create base commit.
	writeTestFile(t, filepath.Join(dir, "file.txt"), []byte("original\n"))
	if err := r.Add([]string{"file.txt"}); err != nil {
		t.Fatalf("Add(base): %v", err)
	}
	_, err = r.Commit("base", "alice")
	if err != nil {
		t.Fatalf("Commit(base): %v", err)
	}

	// Second commit changes the file.
	writeTestFile(t, filepath.Join(dir, "file.txt"), []byte("changed\n"))
	if err := r.Add([]string{"file.txt"}); err != nil {
		t.Fatalf("Add(second): %v", err)
	}
	secondHash, err := r.Commit("change file", "bob")
	if err != nil {
		t.Fatalf("Commit(second): %v", err)
	}

	// Third commit further modifies the file in a conflicting way.
	writeTestFile(t, filepath.Join(dir, "file.txt"), []byte("further changed\n"))
	if err := r.Add([]string{"file.txt"}); err != nil {
		t.Fatalf("Add(third): %v", err)
	}
	_, err = r.Commit("further change", "carol")
	if err != nil {
		t.Fatalf("Commit(third): %v", err)
	}

	// Revert should fail with conflict.
	_, err = r.Revert(secondHash)
	if err == nil {
		t.Fatal("Revert should fail on conflict")
	}

	// Simulate resolving the conflict: write the resolved content and stage it.
	writeTestFile(t, filepath.Join(dir, "file.txt"), []byte("resolved\n"))
	if err := r.Add([]string{"file.txt"}); err != nil {
		t.Fatalf("Add(resolved): %v", err)
	}

	// Continue the revert.
	result, err := r.RevertContinue()
	if err != nil {
		t.Fatalf("RevertContinue: %v", err)
	}

	// Verify the result.
	if result.TargetCommit != secondHash {
		t.Errorf("TargetCommit = %s, want %s", result.TargetCommit, secondHash)
	}
	if !strings.Contains(result.Message, "Revert") {
		t.Errorf("Message = %q, want to contain %q", result.Message, "Revert")
	}

	// Verify sequencer state was cleaned up.
	if r.IsRevertInProgress() {
		t.Fatal("IsRevertInProgress should be false after continue")
	}

	// Verify HEAD was updated.
	headHash, err := r.ResolveRef("HEAD")
	if err != nil {
		t.Fatalf("ResolveRef(HEAD): %v", err)
	}
	if headHash != result.CommitHash {
		t.Errorf("HEAD = %s, want %s", headHash, result.CommitHash)
	}

	// Verify working tree has the resolved content.
	content, err := os.ReadFile(filepath.Join(dir, "file.txt"))
	if err != nil {
		t.Fatalf("ReadFile(file.txt): %v", err)
	}
	if string(content) != "resolved\n" {
		t.Errorf("file.txt = %q, want %q", string(content), "resolved\n")
	}
}

// TestRevert_Abort verifies that aborting a revert restores the original state.
func TestRevert_Abort(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Create base commit.
	writeTestFile(t, filepath.Join(dir, "file.txt"), []byte("original\n"))
	if err := r.Add([]string{"file.txt"}); err != nil {
		t.Fatalf("Add(base): %v", err)
	}
	_, err = r.Commit("base", "alice")
	if err != nil {
		t.Fatalf("Commit(base): %v", err)
	}

	// Second commit changes the file.
	writeTestFile(t, filepath.Join(dir, "file.txt"), []byte("changed\n"))
	if err := r.Add([]string{"file.txt"}); err != nil {
		t.Fatalf("Add(second): %v", err)
	}
	secondHash, err := r.Commit("change file", "bob")
	if err != nil {
		t.Fatalf("Commit(second): %v", err)
	}

	// Third commit further modifies the file.
	writeTestFile(t, filepath.Join(dir, "file.txt"), []byte("further changed\n"))
	if err := r.Add([]string{"file.txt"}); err != nil {
		t.Fatalf("Add(third): %v", err)
	}
	thirdHash, err := r.Commit("further change", "carol")
	if err != nil {
		t.Fatalf("Commit(third): %v", err)
	}

	// Revert should fail with conflict.
	_, err = r.Revert(secondHash)
	if err == nil {
		t.Fatal("Revert should fail on conflict")
	}

	// Abort the revert.
	if err := r.RevertAbort(); err != nil {
		t.Fatalf("RevertAbort: %v", err)
	}

	// Verify sequencer state was cleaned up.
	if r.IsRevertInProgress() {
		t.Fatal("IsRevertInProgress should be false after abort")
	}

	// Verify HEAD was restored.
	headHash, err := r.ResolveRef("HEAD")
	if err != nil {
		t.Fatalf("ResolveRef(HEAD): %v", err)
	}
	if headHash != thirdHash {
		t.Errorf("HEAD = %s, want %s", headHash, thirdHash)
	}

	// Verify working tree was restored.
	content, err := os.ReadFile(filepath.Join(dir, "file.txt"))
	if err != nil {
		t.Fatalf("ReadFile(file.txt): %v", err)
	}
	if string(content) != "further changed\n" {
		t.Errorf("file.txt = %q, want %q", string(content), "further changed\n")
	}
}

// TestRevert_RootCommit verifies that reverting a root commit (no parent) errors.
func TestRevert_RootCommit(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Create a root commit.
	writeTestFile(t, filepath.Join(dir, "a.txt"), []byte("content\n"))
	if err := r.Add([]string{"a.txt"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	rootHash, err := r.Commit("root commit", "alice")
	if err != nil {
		t.Fatalf("Commit(root): %v", err)
	}

	// Create a second commit so we have a valid HEAD.
	writeTestFile(t, filepath.Join(dir, "b.txt"), []byte("other\n"))
	if err := r.Add([]string{"b.txt"}); err != nil {
		t.Fatalf("Add(second): %v", err)
	}
	if _, err := r.Commit("second commit", "alice"); err != nil {
		t.Fatalf("Commit(second): %v", err)
	}

	// Try to revert the root commit.
	_, err = r.Revert(rootHash)
	if err == nil {
		t.Fatal("Revert of root commit should fail, got nil error")
	}
	if !strings.Contains(err.Error(), "no parent") {
		t.Errorf("error = %q, want to contain %q", err.Error(), "no parent")
	}
}

// TestRevert_FileAddedByCommit verifies that reverting a commit which added a
// file results in that file being deleted.
func TestRevert_FileAddedByCommit(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Create base commit.
	writeTestFile(t, filepath.Join(dir, "a.txt"), []byte("base\n"))
	if err := r.Add([]string{"a.txt"}); err != nil {
		t.Fatalf("Add(base): %v", err)
	}
	_, err = r.Commit("base", "alice")
	if err != nil {
		t.Fatalf("Commit(base): %v", err)
	}

	// Add a new file.
	writeTestFile(t, filepath.Join(dir, "new.txt"), []byte("new content\n"))
	if err := r.Add([]string{"new.txt"}); err != nil {
		t.Fatalf("Add(new): %v", err)
	}
	addHash, err := r.Commit("add new.txt", "bob")
	if err != nil {
		t.Fatalf("Commit(add): %v", err)
	}

	// Revert the add commit (should delete new.txt).
	_, err = r.Revert(addHash)
	if err != nil {
		t.Fatalf("Revert: %v", err)
	}

	// Verify new.txt is gone.
	if _, err := os.Stat(filepath.Join(dir, "new.txt")); !os.IsNotExist(err) {
		t.Errorf("new.txt should not exist after reverting its creation")
	}

	// Verify a.txt is untouched.
	content, err := os.ReadFile(filepath.Join(dir, "a.txt"))
	if err != nil {
		t.Fatalf("ReadFile(a.txt): %v", err)
	}
	if string(content) != "base\n" {
		t.Errorf("a.txt = %q, want %q", string(content), "base\n")
	}
}

// TestRevert_DeletedFileIsRestored verifies that reverting a commit which
// deleted a file restores that file in the working directory.
func TestRevert_DeletedFileIsRestored(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Create base commit with two files: the one that will be deleted and a
	// companion that keeps staging non-empty after the deletion.
	writeTestFile(t, filepath.Join(dir, "keep.txt"), []byte("stay\n"))
	writeTestFile(t, filepath.Join(dir, "removed.txt"), []byte("important content\n"))
	if err := r.Add([]string{"keep.txt", "removed.txt"}); err != nil {
		t.Fatalf("Add(base): %v", err)
	}
	_, err = r.Commit("add files", "alice")
	if err != nil {
		t.Fatalf("Commit(base): %v", err)
	}

	// Delete removed.txt and commit the deletion.
	if err := r.Remove([]string{"removed.txt"}, false); err != nil {
		t.Fatalf("Remove(staging): %v", err)
	}
	deleteHash, err := r.Commit("delete removed.txt", "bob")
	if err != nil {
		t.Fatalf("Commit(delete): %v", err)
	}

	// Verify the file is gone before reverting.
	if _, err := os.Stat(filepath.Join(dir, "removed.txt")); !os.IsNotExist(err) {
		t.Fatal("removed.txt should not exist after deletion commit")
	}

	// Revert the deletion commit (should restore removed.txt).
	result, err := r.Revert(deleteHash)
	if err != nil {
		t.Fatalf("Revert: %v", err)
	}

	// Verify removed.txt is restored.
	content, err := os.ReadFile(filepath.Join(dir, "removed.txt"))
	if err != nil {
		t.Fatalf("ReadFile(removed.txt): %v", err)
	}
	if string(content) != "important content\n" {
		t.Errorf("removed.txt = %q, want %q", string(content), "important content\n")
	}

	// Verify keep.txt is untouched.
	keepContent, err := os.ReadFile(filepath.Join(dir, "keep.txt"))
	if err != nil {
		t.Fatalf("ReadFile(keep.txt): %v", err)
	}
	if string(keepContent) != "stay\n" {
		t.Errorf("keep.txt = %q, want %q", string(keepContent), "stay\n")
	}

	// Verify the revert message references the deletion commit.
	if !strings.Contains(result.Message, "Revert") {
		t.Errorf("Message = %q, want to contain %q", result.Message, "Revert")
	}
	if !strings.Contains(result.Message, "delete removed.txt") {
		t.Errorf("Message = %q, want to contain original message %q", result.Message, "delete removed.txt")
	}
}

// TestRevert_UsesResolveAuthor verifies that the revert commit uses
// ResolveAuthor (current user) rather than the HEAD commit's author.
func TestRevert_UsesResolveAuthor(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Set repo-level user config so ResolveAuthor returns a known identity.
	cfg := &Config{
		User: &UserConfig{Name: "Reverter", Email: "reverter@example.com"},
	}
	if err := r.WriteConfig(cfg); err != nil {
		t.Fatalf("WriteConfig: %v", err)
	}

	// Create base commit with a different author.
	writeTestFile(t, filepath.Join(dir, "a.txt"), []byte("line1\n"))
	if err := r.Add([]string{"a.txt"}); err != nil {
		t.Fatalf("Add(base): %v", err)
	}
	_, err = r.Commit("base commit", "original-author")
	if err != nil {
		t.Fatalf("Commit(base): %v", err)
	}

	// Create a second commit by a different author.
	writeTestFile(t, filepath.Join(dir, "a.txt"), []byte("line1\nline2\n"))
	if err := r.Add([]string{"a.txt"}); err != nil {
		t.Fatalf("Add(second): %v", err)
	}
	secondHash, err := r.Commit("add line2", "other-author")
	if err != nil {
		t.Fatalf("Commit(second): %v", err)
	}

	// Revert the second commit.
	result, err := r.Revert(secondHash)
	if err != nil {
		t.Fatalf("Revert: %v", err)
	}

	// The revert commit should be attributed to the configured user, not
	// the original commit's author.
	revertCommit, err := r.Store.ReadCommit(result.CommitHash)
	if err != nil {
		t.Fatalf("ReadCommit: %v", err)
	}
	wantAuthor := "Reverter <reverter@example.com>"
	if revertCommit.Author != wantAuthor {
		t.Errorf("revert commit Author = %q, want %q", revertCommit.Author, wantAuthor)
	}
}

// TestRevert_ContinueUsesResolveAuthor verifies that RevertContinue also uses
// ResolveAuthor for the commit author.
func TestRevert_ContinueUsesResolveAuthor(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Set repo-level user config.
	cfg := &Config{
		User: &UserConfig{Name: "Reverter", Email: "reverter@example.com"},
	}
	if err := r.WriteConfig(cfg); err != nil {
		t.Fatalf("WriteConfig: %v", err)
	}

	// Create base commit.
	writeTestFile(t, filepath.Join(dir, "file.txt"), []byte("original\n"))
	if err := r.Add([]string{"file.txt"}); err != nil {
		t.Fatalf("Add(base): %v", err)
	}
	_, err = r.Commit("base", "alice")
	if err != nil {
		t.Fatalf("Commit(base): %v", err)
	}

	// Second commit changes the file.
	writeTestFile(t, filepath.Join(dir, "file.txt"), []byte("changed\n"))
	if err := r.Add([]string{"file.txt"}); err != nil {
		t.Fatalf("Add(second): %v", err)
	}
	secondHash, err := r.Commit("change file", "bob")
	if err != nil {
		t.Fatalf("Commit(second): %v", err)
	}

	// Third commit further modifies the file in a conflicting way.
	writeTestFile(t, filepath.Join(dir, "file.txt"), []byte("further changed\n"))
	if err := r.Add([]string{"file.txt"}); err != nil {
		t.Fatalf("Add(third): %v", err)
	}
	_, err = r.Commit("further change", "carol")
	if err != nil {
		t.Fatalf("Commit(third): %v", err)
	}

	// Revert should fail with conflict.
	_, err = r.Revert(secondHash)
	if err == nil {
		t.Fatal("Revert should fail on conflict")
	}

	// Resolve the conflict and stage.
	writeTestFile(t, filepath.Join(dir, "file.txt"), []byte("resolved\n"))
	if err := r.Add([]string{"file.txt"}); err != nil {
		t.Fatalf("Add(resolved): %v", err)
	}

	// Continue the revert.
	result, err := r.RevertContinue()
	if err != nil {
		t.Fatalf("RevertContinue: %v", err)
	}

	// The revert commit should be attributed to the configured user.
	revertCommit, err := r.Store.ReadCommit(result.CommitHash)
	if err != nil {
		t.Fatalf("ReadCommit: %v", err)
	}
	wantAuthor := "Reverter <reverter@example.com>"
	if revertCommit.Author != wantAuthor {
		t.Errorf("revert continue commit Author = %q, want %q", revertCommit.Author, wantAuthor)
	}
}

// TestRevertContinue_RejectsMovedHEAD verifies that RevertContinue fails if
// HEAD has been moved to a different branch since the revert started.
func TestRevertContinue_RejectsMovedHEAD(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Create base commit.
	writeTestFile(t, filepath.Join(dir, "file.txt"), []byte("original\n"))
	if err := r.Add([]string{"file.txt"}); err != nil {
		t.Fatalf("Add(base): %v", err)
	}
	baseHash, err := r.Commit("base", "alice")
	if err != nil {
		t.Fatalf("Commit(base): %v", err)
	}

	// Second commit changes the file.
	writeTestFile(t, filepath.Join(dir, "file.txt"), []byte("changed\n"))
	if err := r.Add([]string{"file.txt"}); err != nil {
		t.Fatalf("Add(second): %v", err)
	}
	secondHash, err := r.Commit("change file", "bob")
	if err != nil {
		t.Fatalf("Commit(second): %v", err)
	}

	// Third commit further modifies the file.
	writeTestFile(t, filepath.Join(dir, "file.txt"), []byte("further changed\n"))
	if err := r.Add([]string{"file.txt"}); err != nil {
		t.Fatalf("Add(third): %v", err)
	}
	_, err = r.Commit("further change", "carol")
	if err != nil {
		t.Fatalf("Commit(third): %v", err)
	}

	// Revert should fail with conflict (revert was started on "main").
	_, err = r.Revert(secondHash)
	if err == nil {
		t.Fatal("Revert should fail on conflict")
	}
	if !r.IsRevertInProgress() {
		t.Fatal("expected revert in progress")
	}

	// Simulate switching to a different branch while revert is paused by
	// directly writing the HEAD file (Checkout refuses a dirty working tree).
	if err := r.CreateBranch("other", baseHash); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}
	headPath := filepath.Join(r.GraftDir, "HEAD")
	if err := os.WriteFile(headPath, []byte("ref: refs/heads/other\n"), 0o644); err != nil {
		t.Fatalf("write HEAD: %v", err)
	}

	// Stage something so continue doesn't fail on "nothing staged".
	writeTestFile(t, filepath.Join(dir, "file.txt"), []byte("resolved\n"))
	if err := r.Add([]string{"file.txt"}); err != nil {
		t.Fatalf("Add(resolved): %v", err)
	}

	// RevertContinue should reject because HEAD moved.
	_, err = r.RevertContinue()
	if err == nil {
		t.Fatal("RevertContinue should fail when HEAD has moved")
	}
	if !strings.Contains(err.Error(), "HEAD has moved") {
		t.Errorf("error = %q, want to contain %q", err.Error(), "HEAD has moved")
	}
}

// writeTestFile is a test helper that creates parent directories and writes content.
func writeTestFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", path, err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}
