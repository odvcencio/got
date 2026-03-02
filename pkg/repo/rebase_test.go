package repo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// rebaseWriteFile is a test helper that writes content to a file.
func rebaseWriteFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", path, err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}

// rebaseCommitFile is a test helper that writes a file, adds it, and commits.
func rebaseCommitFile(t *testing.T, r *Repo, filename string, content []byte, message, author string) {
	t.Helper()
	rebaseWriteFile(t, filepath.Join(r.RootDir, filename), content)
	if err := r.Add([]string{filename}); err != nil {
		t.Fatalf("Add(%s): %v", filename, err)
	}
	if _, err := r.Commit(message, author); err != nil {
		t.Fatalf("Commit(%s): %v", message, err)
	}
}

// TestRebase_LinearReplay creates main with 1 commit, a branch with 3 commits,
// advances main, then rebases the branch onto main. Verifies all 3 commits
// replayed in order and the branch points to the new tip.
func TestRebase_LinearReplay(t *testing.T) {
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

	// Create feature branch from base.
	if err := r.CreateBranch("feature", baseHash); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}
	if err := r.Checkout("feature"); err != nil {
		t.Fatalf("Checkout(feature): %v", err)
	}

	// Make 3 commits on feature.
	rebaseCommitFile(t, r, "feat1.txt", []byte("feature 1\n"), "feat commit 1", "bob")
	rebaseCommitFile(t, r, "feat2.txt", []byte("feature 2\n"), "feat commit 2", "bob")
	rebaseCommitFile(t, r, "feat3.txt", []byte("feature 3\n"), "feat commit 3", "bob")

	// Switch to main and advance it.
	if err := r.Checkout("main"); err != nil {
		t.Fatalf("Checkout(main): %v", err)
	}
	rebaseCommitFile(t, r, "main_advance.txt", []byte("main advance\n"), "advance main", "alice")

	mainHash, err := r.ResolveRef("refs/heads/main")
	if err != nil {
		t.Fatalf("ResolveRef(main): %v", err)
	}

	// Switch to feature and rebase onto main.
	if err := r.Checkout("feature"); err != nil {
		t.Fatalf("Checkout(feature): %v", err)
	}

	if err := r.Rebase("main"); err != nil {
		t.Fatalf("Rebase(main): %v", err)
	}

	// Verify HEAD is on feature branch.
	branch, err := r.CurrentBranch()
	if err != nil {
		t.Fatalf("CurrentBranch: %v", err)
	}
	if branch != "feature" {
		t.Errorf("CurrentBranch = %q, want %q", branch, "feature")
	}

	// Verify feature branch tip is different from original.
	newTip, err := r.ResolveRef("refs/heads/feature")
	if err != nil {
		t.Fatalf("ResolveRef(feature): %v", err)
	}

	// Walk the commit history and verify.
	commits, err := r.Log(newTip, 10)
	if err != nil {
		t.Fatalf("Log: %v", err)
	}

	// Should have: 3 feature commits + advance main + initial = 5 commits.
	if len(commits) < 5 {
		t.Fatalf("expected at least 5 commits in log, got %d", len(commits))
	}

	// The most recent 3 should be the feature commits (newest first in log).
	if commits[0].Message != "feat commit 3" {
		t.Errorf("commits[0].Message = %q, want %q", commits[0].Message, "feat commit 3")
	}
	if commits[1].Message != "feat commit 2" {
		t.Errorf("commits[1].Message = %q, want %q", commits[1].Message, "feat commit 2")
	}
	if commits[2].Message != "feat commit 1" {
		t.Errorf("commits[2].Message = %q, want %q", commits[2].Message, "feat commit 1")
	}

	// The parent of the oldest feature commit should be mainHash.
	if len(commits[2].Parents) == 0 || commits[2].Parents[0] != mainHash {
		t.Errorf("oldest rebased commit's parent = %v, want [%s]", commits[2].Parents, mainHash)
	}

	// Verify all files exist.
	for _, f := range []string{"base.txt", "main_advance.txt", "feat1.txt", "feat2.txt", "feat3.txt"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("file %q missing after rebase: %v", f, err)
		}
	}

	// Verify sequencer state is cleaned up.
	if _, err := os.Stat(filepath.Join(dir, ".graft", "rebase-merge")); !os.IsNotExist(err) {
		t.Errorf("rebase-merge directory should be cleaned up after successful rebase")
	}
}

// TestRebase_ConflictStopsAndContinues creates conflicting changes, rebases,
// resolves the conflict, and continues.
func TestRebase_ConflictStopsAndContinues(t *testing.T) {
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

	// Switch to main and make a conflicting change.
	if err := r.Checkout("main"); err != nil {
		t.Fatalf("Checkout(main): %v", err)
	}
	rebaseCommitFile(t, r, "file.txt", []byte("main change\n"), "main edit", "alice")

	// Switch to feature and rebase onto main.
	if err := r.Checkout("feature"); err != nil {
		t.Fatalf("Checkout(feature): %v", err)
	}

	err = r.Rebase("main")
	if err == nil {
		t.Fatal("expected conflict error, got nil")
	}

	// Should be an ErrRebaseConflict.
	conflictErr, ok := err.(*ErrRebaseConflict)
	if !ok {
		t.Fatalf("expected *ErrRebaseConflict, got %T: %v", err, err)
	}
	_ = conflictErr

	// Verify sequencer state exists.
	if !r.isRebaseInProgress() {
		t.Fatal("expected rebase to be in progress")
	}

	// Resolve the conflict by writing the resolved content.
	rebaseWriteFile(t, filepath.Join(dir, "file.txt"), []byte("resolved content\n"))
	if err := r.Add([]string{"file.txt"}); err != nil {
		t.Fatalf("Add(resolved): %v", err)
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

	// Verify the resolved content is in the working tree.
	data, err := os.ReadFile(filepath.Join(dir, "file.txt"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "resolved content\n" {
		t.Errorf("file.txt = %q, want %q", string(data), "resolved content\n")
	}
}

// TestRebase_Abort starts a rebase that will conflict, then aborts.
// Verifies that the original state is restored.
func TestRebase_Abort(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Initial commit on main.
	rebaseCommitFile(t, r, "file.txt", []byte("original\n"), "initial", "alice")

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

	// Modify file on feature.
	rebaseCommitFile(t, r, "file.txt", []byte("feature version\n"), "feature change", "bob")

	origFeatureHash, err := r.ResolveRef("refs/heads/feature")
	if err != nil {
		t.Fatalf("ResolveRef(feature): %v", err)
	}

	// Switch to main and make conflicting change.
	if err := r.Checkout("main"); err != nil {
		t.Fatalf("Checkout(main): %v", err)
	}
	rebaseCommitFile(t, r, "file.txt", []byte("main version\n"), "main change", "alice")

	// Switch to feature and rebase.
	if err := r.Checkout("feature"); err != nil {
		t.Fatalf("Checkout(feature): %v", err)
	}

	err = r.Rebase("main")
	if err == nil {
		t.Fatal("expected conflict error")
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

	// Verify HEAD points to feature.
	branch, err := r.CurrentBranch()
	if err != nil {
		t.Fatalf("CurrentBranch: %v", err)
	}
	if branch != "feature" {
		t.Errorf("CurrentBranch = %q, want %q", branch, "feature")
	}

	// Verify working tree has original content.
	data, err := os.ReadFile(filepath.Join(dir, "file.txt"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "feature version\n" {
		t.Errorf("file.txt = %q, want %q", string(data), "feature version\n")
	}
}

// TestRebase_Skip starts a rebase that conflicts, skips the conflicting commit,
// and verifies remaining commits are applied.
func TestRebase_Skip(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Initial commit on main.
	rebaseCommitFile(t, r, "file.txt", []byte("original\n"), "initial", "alice")

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

	// First feature commit: conflicting change.
	rebaseCommitFile(t, r, "file.txt", []byte("feature conflict\n"), "conflicting commit", "bob")

	// Second feature commit: add a new file (no conflict).
	rebaseCommitFile(t, r, "feat_extra.txt", []byte("extra file\n"), "add extra", "bob")

	// Switch to main and make conflicting change.
	if err := r.Checkout("main"); err != nil {
		t.Fatalf("Checkout(main): %v", err)
	}
	rebaseCommitFile(t, r, "file.txt", []byte("main conflict\n"), "main conflict change", "alice")

	// Switch to feature and rebase.
	if err := r.Checkout("feature"); err != nil {
		t.Fatalf("Checkout(feature): %v", err)
	}

	err = r.Rebase("main")
	if err == nil {
		t.Fatal("expected conflict error")
	}

	// Skip the conflicting commit.
	if err := r.RebaseSkip(); err != nil {
		t.Fatalf("RebaseSkip: %v", err)
	}

	// Verify rebase completed.
	if r.isRebaseInProgress() {
		t.Fatal("rebase should be finished after skip")
	}

	// Verify feature branch is on a new tip.
	branch, err := r.CurrentBranch()
	if err != nil {
		t.Fatalf("CurrentBranch: %v", err)
	}
	if branch != "feature" {
		t.Errorf("CurrentBranch = %q, want %q", branch, "feature")
	}

	// The extra file from the second commit should be present.
	if _, err := os.Stat(filepath.Join(dir, "feat_extra.txt")); err != nil {
		t.Errorf("feat_extra.txt should exist after skip: %v", err)
	}

	// file.txt should have main's content (since we skipped the conflict).
	data, err := os.ReadFile(filepath.Join(dir, "file.txt"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "main conflict\n" {
		t.Errorf("file.txt = %q, want %q", string(data), "main conflict\n")
	}
}

// TestRebase_AlreadyUpToDate verifies that rebase is a no-op when HEAD is
// already a descendant of upstream.
func TestRebase_AlreadyUpToDate(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Initial commit.
	rebaseCommitFile(t, r, "file.txt", []byte("content\n"), "initial", "alice")

	baseHash, err := r.ResolveRef("HEAD")
	if err != nil {
		t.Fatalf("ResolveRef(HEAD): %v", err)
	}

	// Create feature branch and add a commit.
	if err := r.CreateBranch("feature", baseHash); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}
	if err := r.Checkout("feature"); err != nil {
		t.Fatalf("Checkout(feature): %v", err)
	}

	rebaseCommitFile(t, r, "feat.txt", []byte("feature\n"), "feature commit", "bob")
	featureHashBefore, err := r.ResolveRef("refs/heads/feature")
	if err != nil {
		t.Fatalf("ResolveRef(feature): %v", err)
	}

	// Rebase onto main — feature is already ahead of main, merge base is main.
	// This should be a no-op since HEAD is descendant of upstream.
	if err := r.Rebase("main"); err != nil {
		t.Fatalf("Rebase: %v", err)
	}

	// Verify nothing changed.
	featureHashAfter, err := r.ResolveRef("refs/heads/feature")
	if err != nil {
		t.Fatalf("ResolveRef(feature): %v", err)
	}
	if featureHashAfter != featureHashBefore {
		t.Errorf("feature hash changed: %s -> %s", featureHashBefore, featureHashAfter)
	}

	// Verify no sequencer state.
	if r.isRebaseInProgress() {
		t.Error("no rebase should be in progress")
	}
}

// TestRebase_Onto verifies RebaseOnto with explicit newbase.
func TestRebase_Onto(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Initial commit on main.
	rebaseCommitFile(t, r, "base.txt", []byte("base\n"), "initial", "alice")

	baseHash, err := r.ResolveRef("HEAD")
	if err != nil {
		t.Fatalf("ResolveRef(HEAD): %v", err)
	}

	// Advance main to create a "newbase" point.
	rebaseCommitFile(t, r, "main2.txt", []byte("main 2\n"), "main advance", "alice")
	newbaseHash, err := r.ResolveRef("HEAD")
	if err != nil {
		t.Fatalf("ResolveRef(HEAD) for newbase: %v", err)
	}

	// Create feature branch from baseHash.
	if err := r.CreateBranch("feature", baseHash); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}
	if err := r.Checkout("feature"); err != nil {
		t.Fatalf("Checkout(feature): %v", err)
	}

	// Make 2 commits on feature.
	rebaseCommitFile(t, r, "feat1.txt", []byte("feat 1\n"), "feat 1", "bob")
	rebaseCommitFile(t, r, "feat2.txt", []byte("feat 2\n"), "feat 2", "bob")

	// RebaseOnto: replay commits from base..HEAD onto newbase.
	// This means: take commits after baseHash up to HEAD, and replay them onto newbaseHash.
	if err := r.RebaseOnto(string(newbaseHash), string(baseHash)); err != nil {
		t.Fatalf("RebaseOnto: %v", err)
	}

	// Verify HEAD is on feature.
	branch, err := r.CurrentBranch()
	if err != nil {
		t.Fatalf("CurrentBranch: %v", err)
	}
	if branch != "feature" {
		t.Errorf("CurrentBranch = %q, want %q", branch, "feature")
	}

	// Verify commit history.
	tip, err := r.ResolveRef("HEAD")
	if err != nil {
		t.Fatalf("ResolveRef(HEAD): %v", err)
	}
	commits, err := r.Log(tip, 10)
	if err != nil {
		t.Fatalf("Log: %v", err)
	}

	// Should have: feat 2, feat 1, main advance, initial = 4 commits.
	if len(commits) < 4 {
		var msgs []string
		for _, c := range commits {
			msgs = append(msgs, c.Message)
		}
		t.Fatalf("expected at least 4 commits, got %d: %v", len(commits), msgs)
	}

	if commits[0].Message != "feat 2" {
		t.Errorf("commits[0].Message = %q, want %q", commits[0].Message, "feat 2")
	}
	if commits[1].Message != "feat 1" {
		t.Errorf("commits[1].Message = %q, want %q", commits[1].Message, "feat 1")
	}

	// Parent of oldest replayed commit should be newbaseHash.
	if len(commits[1].Parents) == 0 || commits[1].Parents[0] != newbaseHash {
		t.Errorf("oldest replayed commit parent = %v, want [%s]", commits[1].Parents, newbaseHash)
	}

	// Verify all files exist.
	for _, f := range []string{"base.txt", "main2.txt", "feat1.txt", "feat2.txt"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("file %q missing after rebase onto: %v", f, err)
		}
	}

	// Verify sequencer cleaned up.
	if r.isRebaseInProgress() {
		t.Error("rebase should not be in progress")
	}
}

// TestRebase_NoRebaseInProgressErrors verifies error handling for continue/abort/skip
// when no rebase is in progress.
func TestRebase_NoRebaseInProgressErrors(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	rebaseCommitFile(t, r, "file.txt", []byte("content\n"), "initial", "alice")

	if err := r.RebaseContinue(); err == nil {
		t.Error("RebaseContinue should fail with no rebase in progress")
	} else if !strings.Contains(err.Error(), "no rebase in progress") {
		t.Errorf("unexpected error: %v", err)
	}

	if err := r.RebaseAbort(); err == nil {
		t.Error("RebaseAbort should fail with no rebase in progress")
	} else if !strings.Contains(err.Error(), "no rebase in progress") {
		t.Errorf("unexpected error: %v", err)
	}

	if err := r.RebaseSkip(); err == nil {
		t.Error("RebaseSkip should fail with no rebase in progress")
	} else if !strings.Contains(err.Error(), "no rebase in progress") {
		t.Errorf("unexpected error: %v", err)
	}
}
