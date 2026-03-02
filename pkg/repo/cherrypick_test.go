package repo

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/odvcencio/graft/pkg/object"
)

// TestCherryPick_AppliesCleanly verifies that cherry-pick applies a commit's
// changes cleanly when there are no conflicts with HEAD.
func TestCherryPick_AppliesCleanly(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Create base commit with two files.
	cherryPickWriteFile(t, filepath.Join(dir, "a.txt"), []byte("line1\n"))
	cherryPickWriteFile(t, filepath.Join(dir, "b.txt"), []byte("hello\n"))
	if err := r.Add([]string{"a.txt", "b.txt"}); err != nil {
		t.Fatalf("Add(base): %v", err)
	}
	baseHash, err := r.Commit("base commit", "alice")
	if err != nil {
		t.Fatalf("Commit(base): %v", err)
	}

	// Create feature branch from base.
	if err := r.CreateBranch("feature", baseHash); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}
	if err := r.Checkout("feature"); err != nil {
		t.Fatalf("Checkout(feature): %v", err)
	}

	// Make a change on feature branch (modify b.txt).
	cherryPickWriteFile(t, filepath.Join(dir, "b.txt"), []byte("hello world\n"))
	if err := r.Add([]string{"b.txt"}); err != nil {
		t.Fatalf("Add(feature): %v", err)
	}
	featureHash, err := r.Commit("update b.txt", "bob")
	if err != nil {
		t.Fatalf("Commit(feature): %v", err)
	}

	// Switch back to main.
	if err := r.Checkout("main"); err != nil {
		t.Fatalf("Checkout(main): %v", err)
	}

	// Make a non-conflicting change on main (modify a.txt).
	cherryPickWriteFile(t, filepath.Join(dir, "a.txt"), []byte("line1\nline2\n"))
	if err := r.Add([]string{"a.txt"}); err != nil {
		t.Fatalf("Add(main): %v", err)
	}
	mainHash, err := r.Commit("update a.txt", "carol")
	if err != nil {
		t.Fatalf("Commit(main): %v", err)
	}

	// Cherry-pick the feature commit onto main.
	result, err := r.CherryPick(featureHash)
	if err != nil {
		t.Fatalf("CherryPick: %v", err)
	}

	// Verify the result.
	if result.TargetCommit != featureHash {
		t.Errorf("TargetCommit = %s, want %s", result.TargetCommit, featureHash)
	}

	// Verify b.txt was updated.
	bContent, err := os.ReadFile(filepath.Join(dir, "b.txt"))
	if err != nil {
		t.Fatalf("ReadFile(b.txt): %v", err)
	}
	if string(bContent) != "hello world\n" {
		t.Errorf("b.txt = %q, want %q", string(bContent), "hello world\n")
	}

	// Verify a.txt still has main's changes.
	aContent, err := os.ReadFile(filepath.Join(dir, "a.txt"))
	if err != nil {
		t.Fatalf("ReadFile(a.txt): %v", err)
	}
	if string(aContent) != "line1\nline2\n" {
		t.Errorf("a.txt = %q, want %q", string(aContent), "line1\nline2\n")
	}

	// Verify HEAD was updated.
	headHash, err := r.ResolveRef("HEAD")
	if err != nil {
		t.Fatalf("ResolveRef(HEAD): %v", err)
	}
	if headHash != result.CommitHash {
		t.Errorf("HEAD = %s, want %s", headHash, result.CommitHash)
	}

	// Verify parent is the previous main commit.
	newCommit, err := r.Store.ReadCommit(result.CommitHash)
	if err != nil {
		t.Fatalf("ReadCommit: %v", err)
	}
	if len(newCommit.Parents) != 1 || newCommit.Parents[0] != mainHash {
		t.Errorf("parents = %v, want [%s]", newCommit.Parents, mainHash)
	}
}

// TestCherryPick_ConflictReportsError verifies that cherry-pick on conflicting
// changes returns an error describing the conflict without committing.
func TestCherryPick_ConflictReportsError(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Create base commit.
	cherryPickWriteFile(t, filepath.Join(dir, "file.txt"), []byte("original\n"))
	if err := r.Add([]string{"file.txt"}); err != nil {
		t.Fatalf("Add(base): %v", err)
	}
	baseHash, err := r.Commit("base", "alice")
	if err != nil {
		t.Fatalf("Commit(base): %v", err)
	}

	// Create feature branch from base.
	if err := r.CreateBranch("feature", baseHash); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}
	if err := r.Checkout("feature"); err != nil {
		t.Fatalf("Checkout(feature): %v", err)
	}

	// Conflicting change on feature.
	cherryPickWriteFile(t, filepath.Join(dir, "file.txt"), []byte("feature change\n"))
	if err := r.Add([]string{"file.txt"}); err != nil {
		t.Fatalf("Add(feature): %v", err)
	}
	featureHash, err := r.Commit("feature change", "bob")
	if err != nil {
		t.Fatalf("Commit(feature): %v", err)
	}

	// Switch to main and make a conflicting change.
	if err := r.Checkout("main"); err != nil {
		t.Fatalf("Checkout(main): %v", err)
	}
	cherryPickWriteFile(t, filepath.Join(dir, "file.txt"), []byte("main change\n"))
	if err := r.Add([]string{"file.txt"}); err != nil {
		t.Fatalf("Add(main): %v", err)
	}
	mainHash, err := r.Commit("main change", "carol")
	if err != nil {
		t.Fatalf("Commit(main): %v", err)
	}

	// Cherry-pick should fail with conflict.
	_, err = r.CherryPick(featureHash)
	if err == nil {
		t.Fatal("CherryPick should fail on conflict, got nil error")
	}
	if !strings.Contains(err.Error(), "conflict") {
		t.Errorf("error = %q, want to contain %q", err.Error(), "conflict")
	}
	if !strings.Contains(err.Error(), "file.txt") {
		t.Errorf("error = %q, want to contain %q", err.Error(), "file.txt")
	}

	// HEAD should not have changed.
	headHash, err := r.ResolveRef("HEAD")
	if err != nil {
		t.Fatalf("ResolveRef(HEAD): %v", err)
	}
	if headHash != mainHash {
		t.Errorf("HEAD = %s, want %s (should not change on conflict)", headHash, mainHash)
	}
}

// TestCherryPick_PreservesOriginalCommitMessage verifies that the cherry-picked
// commit preserves the original commit's message and author.
func TestCherryPick_PreservesOriginalCommitMessage(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Create base commit.
	cherryPickWriteFile(t, filepath.Join(dir, "a.txt"), []byte("initial\n"))
	if err := r.Add([]string{"a.txt"}); err != nil {
		t.Fatalf("Add(base): %v", err)
	}
	baseHash, err := r.Commit("base", "alice")
	if err != nil {
		t.Fatalf("Commit(base): %v", err)
	}

	// Create feature branch.
	if err := r.CreateBranch("feature", baseHash); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}
	if err := r.Checkout("feature"); err != nil {
		t.Fatalf("Checkout(feature): %v", err)
	}

	// Make commit with distinctive message and author.
	cherryPickWriteFile(t, filepath.Join(dir, "a.txt"), []byte("modified\n"))
	if err := r.Add([]string{"a.txt"}); err != nil {
		t.Fatalf("Add(feature): %v", err)
	}
	originalMessage := "fix: critical bugfix in parser"
	originalAuthor := "bob-the-developer"
	featureHash, err := r.Commit(originalMessage, originalAuthor)
	if err != nil {
		t.Fatalf("Commit(feature): %v", err)
	}

	// Switch back to main.
	if err := r.Checkout("main"); err != nil {
		t.Fatalf("Checkout(main): %v", err)
	}

	// Cherry-pick.
	result, err := r.CherryPick(featureHash)
	if err != nil {
		t.Fatalf("CherryPick: %v", err)
	}

	// Verify message is preserved.
	if result.Message != originalMessage {
		t.Errorf("Message = %q, want %q", result.Message, originalMessage)
	}

	// Verify the commit object's message and author.
	newCommit, err := r.Store.ReadCommit(result.CommitHash)
	if err != nil {
		t.Fatalf("ReadCommit: %v", err)
	}
	if newCommit.Message != originalMessage {
		t.Errorf("commit.Message = %q, want %q", newCommit.Message, originalMessage)
	}
	if newCommit.Author != originalAuthor {
		t.Errorf("commit.Author = %q, want %q", newCommit.Author, originalAuthor)
	}
}

// TestCherryPick_RootCommitReturnsError verifies that cherry-picking a root
// commit (one with no parent) returns an error.
func TestCherryPick_RootCommitReturnsError(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Create a root commit (the first commit has no parent).
	cherryPickWriteFile(t, filepath.Join(dir, "a.txt"), []byte("content\n"))
	if err := r.Add([]string{"a.txt"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	rootHash, err := r.Commit("root commit", "alice")
	if err != nil {
		t.Fatalf("Commit(root): %v", err)
	}

	// Create a second commit so we have a valid HEAD to cherry-pick onto.
	cherryPickWriteFile(t, filepath.Join(dir, "b.txt"), []byte("other\n"))
	if err := r.Add([]string{"b.txt"}); err != nil {
		t.Fatalf("Add(second): %v", err)
	}
	if _, err := r.Commit("second commit", "alice"); err != nil {
		t.Fatalf("Commit(second): %v", err)
	}

	// Try to cherry-pick the root commit.
	_, err = r.CherryPick(rootHash)
	if err == nil {
		t.Fatal("CherryPick of root commit should fail, got nil error")
	}
	if !strings.Contains(err.Error(), "no parent") {
		t.Errorf("error = %q, want to contain %q", err.Error(), "no parent")
	}
}

// TestCherryPick_NewFileAddedByCommit verifies that cherry-pick correctly adds
// a new file that was introduced in the cherry-picked commit.
func TestCherryPick_NewFileAddedByCommit(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Create base commit.
	cherryPickWriteFile(t, filepath.Join(dir, "a.txt"), []byte("initial\n"))
	if err := r.Add([]string{"a.txt"}); err != nil {
		t.Fatalf("Add(base): %v", err)
	}
	baseHash, err := r.Commit("base", "alice")
	if err != nil {
		t.Fatalf("Commit(base): %v", err)
	}

	// Create feature branch.
	if err := r.CreateBranch("feature", baseHash); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}
	if err := r.Checkout("feature"); err != nil {
		t.Fatalf("Checkout(feature): %v", err)
	}

	// Add a new file on feature.
	cherryPickWriteFile(t, filepath.Join(dir, "new.txt"), []byte("new content\n"))
	if err := r.Add([]string{"new.txt"}); err != nil {
		t.Fatalf("Add(feature): %v", err)
	}
	featureHash, err := r.Commit("add new.txt", "bob")
	if err != nil {
		t.Fatalf("Commit(feature): %v", err)
	}

	// Switch back to main.
	if err := r.Checkout("main"); err != nil {
		t.Fatalf("Checkout(main): %v", err)
	}

	// Cherry-pick.
	_, err = r.CherryPick(featureHash)
	if err != nil {
		t.Fatalf("CherryPick: %v", err)
	}

	// Verify new file exists.
	content, err := os.ReadFile(filepath.Join(dir, "new.txt"))
	if err != nil {
		t.Fatalf("ReadFile(new.txt): %v", err)
	}
	if string(content) != "new content\n" {
		t.Errorf("new.txt = %q, want %q", string(content), "new content\n")
	}
}

// setupCherryPickConflict creates a repo with a conflicting cherry-pick scenario.
// Returns the repo, temp dir, feature commit hash, and main commit hash.
func setupCherryPickConflict(t *testing.T) (*Repo, string, object.Hash, object.Hash) {
	t.Helper()
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Create base commit.
	cherryPickWriteFile(t, filepath.Join(dir, "file.txt"), []byte("original\n"))
	if err := r.Add([]string{"file.txt"}); err != nil {
		t.Fatalf("Add(base): %v", err)
	}
	baseHash, err := r.Commit("base", "alice")
	if err != nil {
		t.Fatalf("Commit(base): %v", err)
	}

	// Create feature branch with conflicting change.
	if err := r.CreateBranch("feature", baseHash); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}
	if err := r.Checkout("feature"); err != nil {
		t.Fatalf("Checkout(feature): %v", err)
	}
	cherryPickWriteFile(t, filepath.Join(dir, "file.txt"), []byte("feature change\n"))
	if err := r.Add([]string{"file.txt"}); err != nil {
		t.Fatalf("Add(feature): %v", err)
	}
	featureHash, err := r.Commit("feature change", "bob")
	if err != nil {
		t.Fatalf("Commit(feature): %v", err)
	}

	// Switch to main and make a conflicting change.
	if err := r.Checkout("main"); err != nil {
		t.Fatalf("Checkout(main): %v", err)
	}
	cherryPickWriteFile(t, filepath.Join(dir, "file.txt"), []byte("main change\n"))
	if err := r.Add([]string{"file.txt"}); err != nil {
		t.Fatalf("Add(main): %v", err)
	}
	mainHash, err := r.Commit("main change", "carol")
	if err != nil {
		t.Fatalf("Commit(main): %v", err)
	}

	return r, dir, featureHash, mainHash
}

// TestCherryPickConflict_SavesState verifies that a conflicting cherry-pick
// saves sequencer state and returns an *ErrCherryPickConflict.
func TestCherryPickConflict_SavesState(t *testing.T) {
	r, _, featureHash, mainHash := setupCherryPickConflict(t)

	// Cherry-pick should fail with conflict and save state.
	_, err := r.CherryPick(featureHash)
	if err == nil {
		t.Fatal("CherryPick should fail on conflict, got nil error")
	}

	// Verify we get the typed error.
	var conflictErr *ErrCherryPickConflict
	if !errors.As(err, &conflictErr) {
		t.Fatalf("error type = %T, want *ErrCherryPickConflict; error = %v", err, err)
	}
	if conflictErr.TargetHash != featureHash {
		t.Errorf("TargetHash = %s, want %s", conflictErr.TargetHash, featureHash)
	}
	if !strings.Contains(conflictErr.Details, "file.txt") {
		t.Errorf("Details = %q, want to contain %q", conflictErr.Details, "file.txt")
	}

	// Verify sequencer state directory exists.
	if !r.IsCherryPickInProgress() {
		t.Fatal("IsCherryPickInProgress() = false, want true")
	}

	// Verify target-hash file.
	targetHashStr, err := r.readCherryPickFile("target-hash")
	if err != nil {
		t.Fatalf("readCherryPickFile(target-hash): %v", err)
	}
	if got := strings.TrimSpace(targetHashStr); got != string(featureHash) {
		t.Errorf("target-hash = %q, want %q", got, featureHash)
	}

	// Verify orig-head file.
	origHeadStr, err := r.readCherryPickFile("orig-head")
	if err != nil {
		t.Fatalf("readCherryPickFile(orig-head): %v", err)
	}
	if got := strings.TrimSpace(origHeadStr); got != string(mainHash) {
		t.Errorf("orig-head = %q, want %q", got, mainHash)
	}

	// Verify head-name file.
	headNameStr, err := r.readCherryPickFile("head-name")
	if err != nil {
		t.Fatalf("readCherryPickFile(head-name): %v", err)
	}
	if got := strings.TrimSpace(headNameStr); got != "refs/heads/main" {
		t.Errorf("head-name = %q, want %q", got, "refs/heads/main")
	}

	// HEAD should not have changed.
	headHash, err := r.ResolveRef("HEAD")
	if err != nil {
		t.Fatalf("ResolveRef(HEAD): %v", err)
	}
	if headHash != mainHash {
		t.Errorf("HEAD = %s, want %s (should not change on conflict)", headHash, mainHash)
	}
}

// TestCherryPickContinue_CommitsResolvedConflict verifies that resolving
// conflicts and running CherryPickContinue creates the commit.
func TestCherryPickContinue_CommitsResolvedConflict(t *testing.T) {
	r, dir, featureHash, mainHash := setupCherryPickConflict(t)

	// Cherry-pick to trigger conflict.
	_, err := r.CherryPick(featureHash)
	if err == nil {
		t.Fatal("CherryPick should fail on conflict")
	}
	var conflictErr *ErrCherryPickConflict
	if !errors.As(err, &conflictErr) {
		t.Fatalf("error type = %T, want *ErrCherryPickConflict", err)
	}

	// Simulate user resolving the conflict: write resolved content and stage it.
	resolvedContent := []byte("resolved content\n")
	cherryPickWriteFile(t, filepath.Join(dir, "file.txt"), resolvedContent)
	if err := r.Add([]string{"file.txt"}); err != nil {
		t.Fatalf("Add(resolved): %v", err)
	}

	// Continue the cherry-pick.
	result, err := r.CherryPickContinue()
	if err != nil {
		t.Fatalf("CherryPickContinue: %v", err)
	}

	// Verify the result.
	if result.TargetCommit != featureHash {
		t.Errorf("TargetCommit = %s, want %s", result.TargetCommit, featureHash)
	}
	if result.Message != "feature change" {
		t.Errorf("Message = %q, want %q", result.Message, "feature change")
	}

	// Verify HEAD was updated.
	headHash, err := r.ResolveRef("HEAD")
	if err != nil {
		t.Fatalf("ResolveRef(HEAD): %v", err)
	}
	if headHash != result.CommitHash {
		t.Errorf("HEAD = %s, want %s", headHash, result.CommitHash)
	}

	// Verify the commit has the correct parent (the main commit before cherry-pick).
	newCommit, err := r.Store.ReadCommit(result.CommitHash)
	if err != nil {
		t.Fatalf("ReadCommit: %v", err)
	}
	if len(newCommit.Parents) != 1 || newCommit.Parents[0] != mainHash {
		t.Errorf("parents = %v, want [%s]", newCommit.Parents, mainHash)
	}

	// Verify the commit preserves author from the original commit.
	if newCommit.Author != "bob" {
		t.Errorf("Author = %q, want %q", newCommit.Author, "bob")
	}

	// Verify file content.
	content, err := os.ReadFile(filepath.Join(dir, "file.txt"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(content) != "resolved content\n" {
		t.Errorf("file.txt = %q, want %q", string(content), "resolved content\n")
	}

	// Verify sequencer state is cleaned up.
	if r.IsCherryPickInProgress() {
		t.Error("IsCherryPickInProgress() = true after continue, want false")
	}
}

// TestCherryPickAbort_RestoresOriginalState verifies that CherryPickAbort
// restores HEAD, the branch ref, and the working tree to the pre-cherry-pick state.
func TestCherryPickAbort_RestoresOriginalState(t *testing.T) {
	r, dir, featureHash, mainHash := setupCherryPickConflict(t)

	// Cherry-pick to trigger conflict.
	_, err := r.CherryPick(featureHash)
	if err == nil {
		t.Fatal("CherryPick should fail on conflict")
	}

	// Abort the cherry-pick.
	if err := r.CherryPickAbort(); err != nil {
		t.Fatalf("CherryPickAbort: %v", err)
	}

	// Verify HEAD is restored.
	headHash, err := r.ResolveRef("HEAD")
	if err != nil {
		t.Fatalf("ResolveRef(HEAD): %v", err)
	}
	if headHash != mainHash {
		t.Errorf("HEAD = %s, want %s", headHash, mainHash)
	}

	// Verify HEAD is reattached to main branch.
	head, err := r.Head()
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	if head != "refs/heads/main" {
		t.Errorf("Head() = %q, want %q", head, "refs/heads/main")
	}

	// Verify the working tree is restored (file.txt should have main's content).
	content, err := os.ReadFile(filepath.Join(dir, "file.txt"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(content) != "main change\n" {
		t.Errorf("file.txt = %q, want %q", string(content), "main change\n")
	}

	// Verify sequencer state is cleaned up.
	if r.IsCherryPickInProgress() {
		t.Error("IsCherryPickInProgress() = true after abort, want false")
	}
}

// TestCherryPickSkip_DiscardsConflict verifies that CherryPickSkip resets the
// working tree to HEAD and cleans up sequencer state.
func TestCherryPickSkip_DiscardsConflict(t *testing.T) {
	r, dir, featureHash, mainHash := setupCherryPickConflict(t)

	// Cherry-pick to trigger conflict.
	_, err := r.CherryPick(featureHash)
	if err == nil {
		t.Fatal("CherryPick should fail on conflict")
	}

	// Skip the cherry-pick.
	if err := r.CherryPickSkip(); err != nil {
		t.Fatalf("CherryPickSkip: %v", err)
	}

	// Verify HEAD has not changed.
	headHash, err := r.ResolveRef("HEAD")
	if err != nil {
		t.Fatalf("ResolveRef(HEAD): %v", err)
	}
	if headHash != mainHash {
		t.Errorf("HEAD = %s, want %s", headHash, mainHash)
	}

	// Verify working tree is restored (file.txt should have main's content).
	content, err := os.ReadFile(filepath.Join(dir, "file.txt"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(content) != "main change\n" {
		t.Errorf("file.txt = %q, want %q", string(content), "main change\n")
	}

	// Verify sequencer state is cleaned up.
	if r.IsCherryPickInProgress() {
		t.Error("IsCherryPickInProgress() = true after skip, want false")
	}
}

// TestCherryPickContinue_NoInProgress verifies that CherryPickContinue fails
// when there is no cherry-pick in progress.
func TestCherryPickContinue_NoInProgress(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	_, err = r.CherryPickContinue()
	if !errors.Is(err, ErrNoCherryPickInProgress) {
		t.Errorf("error = %v, want %v", err, ErrNoCherryPickInProgress)
	}
}

// TestCherryPickAbort_NoInProgress verifies that CherryPickAbort fails
// when there is no cherry-pick in progress.
func TestCherryPickAbort_NoInProgress(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	err = r.CherryPickAbort()
	if !errors.Is(err, ErrNoCherryPickInProgress) {
		t.Errorf("error = %v, want %v", err, ErrNoCherryPickInProgress)
	}
}

// TestCherryPickSkip_NoInProgress verifies that CherryPickSkip fails
// when there is no cherry-pick in progress.
func TestCherryPickSkip_NoInProgress(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	err = r.CherryPickSkip()
	if !errors.Is(err, ErrNoCherryPickInProgress) {
		t.Errorf("error = %v, want %v", err, ErrNoCherryPickInProgress)
	}
}

// TestCherryPick_BlocksWhenInProgress verifies that starting a new cherry-pick
// while one is already in progress returns an error.
func TestCherryPick_BlocksWhenInProgress(t *testing.T) {
	r, _, featureHash, _ := setupCherryPickConflict(t)

	// Cherry-pick to trigger conflict.
	_, err := r.CherryPick(featureHash)
	if err == nil {
		t.Fatal("CherryPick should fail on conflict")
	}

	// Try to start another cherry-pick.
	_, err = r.CherryPick(featureHash)
	if err == nil {
		t.Fatal("second CherryPick should fail")
	}
	if !strings.Contains(err.Error(), "already in progress") {
		t.Errorf("error = %q, want to contain %q", err.Error(), "already in progress")
	}
}
