package repo

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResetUnstagesToHead(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	file := filepath.Join(r.RootDir, "main.go")
	if err := os.WriteFile(file, []byte("package main\n\nfunc A() {}\n"), 0o644); err != nil {
		t.Fatalf("write initial file: %v", err)
	}
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("add initial file: %v", err)
	}
	if _, err := r.Commit("alice", "initial"); err != nil {
		t.Fatalf("commit initial: %v", err)
	}

	if err := os.WriteFile(file, []byte("package main\n\nfunc A() {}\nfunc B() {}\n"), 0o644); err != nil {
		t.Fatalf("write modified file: %v", err)
	}
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("add modified file: %v", err)
	}

	before, err := r.Status()
	if err != nil {
		t.Fatalf("status before reset: %v", err)
	}
	if len(before) == 0 {
		t.Fatal("expected non-empty status before reset")
	}

	if err := r.Reset([]string{"main.go"}); err != nil {
		t.Fatalf("reset: %v", err)
	}

	after, err := r.Status()
	if err != nil {
		t.Fatalf("status after reset: %v", err)
	}
	entry := findStatusEntry(after, "main.go")
	if entry == nil {
		t.Fatalf("expected status entry for main.go after reset, got %+v", after)
	}
	if entry.IndexStatus != StatusClean {
		t.Fatalf("IndexStatus = %v, want %v", entry.IndexStatus, StatusClean)
	}
	if entry.WorkStatus != StatusDirty {
		t.Fatalf("WorkStatus = %v, want %v", entry.WorkStatus, StatusDirty)
	}
}

func TestResetRemovesStagedNewFile(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	file := filepath.Join(r.RootDir, "new.txt")
	if err := os.WriteFile(file, []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write new file: %v", err)
	}
	if err := r.Add([]string{"new.txt"}); err != nil {
		t.Fatalf("add new file: %v", err)
	}

	if err := r.Reset([]string{"new.txt"}); err != nil {
		t.Fatalf("reset new file: %v", err)
	}

	stg, err := r.ReadStaging()
	if err != nil {
		t.Fatalf("read staging: %v", err)
	}
	if _, ok := stg.Entries["new.txt"]; ok {
		t.Fatalf("expected new.txt to be unstaged, got staging entry %+v", stg.Entries["new.txt"])
	}
}

// TestResetSoft_OnlyMovesHEAD verifies that reset --soft moves HEAD but
// leaves staging and working tree untouched.
func TestResetSoft_OnlyMovesHEAD(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	file := filepath.Join(r.RootDir, "main.go")
	if err := os.WriteFile(file, []byte("package main\n\nfunc A() {}\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	h1, err := r.Commit("first commit", "test-author")
	if err != nil {
		t.Fatalf("commit first: %v", err)
	}

	// Make a second commit with modified content.
	if err := os.WriteFile(file, []byte("package main\n\nfunc B() {}\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	_, err = r.Commit("second commit", "test-author")
	if err != nil {
		t.Fatalf("commit second: %v", err)
	}

	// Save staging before reset.
	stgBefore, err := r.ReadStaging()
	if err != nil {
		t.Fatalf("read staging: %v", err)
	}
	stagingHashBefore := stgBefore.Entries["main.go"].BlobHash

	// Reset --soft to h1.
	if err := r.ResetToCommit(h1, ResetSoft); err != nil {
		t.Fatalf("ResetToCommit soft: %v", err)
	}

	// HEAD should now point to h1.
	headHash, err := r.ResolveRef("HEAD")
	if err != nil {
		t.Fatalf("ResolveRef(HEAD): %v", err)
	}
	if headHash != h1 {
		t.Errorf("HEAD = %q, want %q", headHash, h1)
	}

	// Staging should be unchanged (still have the second commit's blob).
	stgAfter, err := r.ReadStaging()
	if err != nil {
		t.Fatalf("read staging after: %v", err)
	}
	if stgAfter.Entries["main.go"].BlobHash != stagingHashBefore {
		t.Errorf("staging blob changed: got %q, want %q",
			stgAfter.Entries["main.go"].BlobHash, stagingHashBefore)
	}

	// Working tree should be unchanged.
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(data) != "package main\n\nfunc B() {}\n" {
		t.Errorf("working tree changed: %q", string(data))
	}
}

// TestResetMixed_ResetsStaging verifies that reset --mixed (default) moves
// HEAD and resets staging, but preserves the working tree.
func TestResetMixed_ResetsStaging(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	file := filepath.Join(r.RootDir, "main.go")
	if err := os.WriteFile(file, []byte("package main\n\nfunc A() {}\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	h1, err := r.Commit("first commit", "test-author")
	if err != nil {
		t.Fatalf("commit first: %v", err)
	}

	// Capture the first commit's tree for later comparison.
	c1, err := r.Store.ReadCommit(h1)
	if err != nil {
		t.Fatalf("read commit: %v", err)
	}
	c1Entries, err := r.FlattenTree(c1.TreeHash)
	if err != nil {
		t.Fatalf("flatten tree: %v", err)
	}
	c1BlobHash := c1Entries[0].BlobHash

	// Make a second commit with modified content.
	if err := os.WriteFile(file, []byte("package main\n\nfunc B() {}\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	_, err = r.Commit("second commit", "test-author")
	if err != nil {
		t.Fatalf("commit second: %v", err)
	}

	// Reset --mixed to h1.
	if err := r.ResetToCommit(h1, ResetMixed); err != nil {
		t.Fatalf("ResetToCommit mixed: %v", err)
	}

	// HEAD should point to h1.
	headHash, err := r.ResolveRef("HEAD")
	if err != nil {
		t.Fatalf("ResolveRef(HEAD): %v", err)
	}
	if headHash != h1 {
		t.Errorf("HEAD = %q, want %q", headHash, h1)
	}

	// Staging should match h1's tree (blob hash from first commit).
	stgAfter, err := r.ReadStaging()
	if err != nil {
		t.Fatalf("read staging after: %v", err)
	}
	if stgAfter.Entries["main.go"].BlobHash != c1BlobHash {
		t.Errorf("staging blob = %q, want %q (from first commit)",
			stgAfter.Entries["main.go"].BlobHash, c1BlobHash)
	}

	// Working tree should still have the second commit's content.
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(data) != "package main\n\nfunc B() {}\n" {
		t.Errorf("working tree should be preserved, got %q", string(data))
	}
}

// TestResetHard_RestoresWorkingTreeAndStaging verifies that reset --hard moves
// HEAD, resets staging, and restores the working tree.
func TestResetHard_RestoresWorkingTreeAndStaging(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	file := filepath.Join(r.RootDir, "main.go")
	if err := os.WriteFile(file, []byte("package main\n\nfunc A() {}\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	h1, err := r.Commit("first commit", "test-author")
	if err != nil {
		t.Fatalf("commit first: %v", err)
	}

	// Make a second commit that adds a new file and modifies main.go.
	if err := os.WriteFile(file, []byte("package main\n\nfunc B() {}\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	newFile := filepath.Join(r.RootDir, "extra.txt")
	if err := os.WriteFile(newFile, []byte("extra\n"), 0o644); err != nil {
		t.Fatalf("write extra: %v", err)
	}
	if err := r.Add([]string{"main.go", "extra.txt"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	_, err = r.Commit("second commit", "test-author")
	if err != nil {
		t.Fatalf("commit second: %v", err)
	}

	// Verify extra.txt exists before reset.
	if _, err := os.Stat(newFile); err != nil {
		t.Fatalf("extra.txt should exist before reset: %v", err)
	}

	// Reset --hard to h1.
	if err := r.ResetToCommit(h1, ResetHard); err != nil {
		t.Fatalf("ResetToCommit hard: %v", err)
	}

	// HEAD should point to h1.
	headHash, err := r.ResolveRef("HEAD")
	if err != nil {
		t.Fatalf("ResolveRef(HEAD): %v", err)
	}
	if headHash != h1 {
		t.Errorf("HEAD = %q, want %q", headHash, h1)
	}

	// Working tree: main.go should be restored to first commit's content.
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	if string(data) != "package main\n\nfunc A() {}\n" {
		t.Errorf("main.go content = %q, want first commit's content", string(data))
	}

	// Working tree: extra.txt should be removed.
	if _, err := os.Stat(newFile); !os.IsNotExist(err) {
		t.Errorf("extra.txt should not exist after reset --hard, err = %v", err)
	}

	// Staging should match h1's tree.
	stg, err := r.ReadStaging()
	if err != nil {
		t.Fatalf("read staging: %v", err)
	}
	if _, ok := stg.Entries["extra.txt"]; ok {
		t.Error("staging should not contain extra.txt after reset --hard")
	}
	if _, ok := stg.Entries["main.go"]; !ok {
		t.Error("staging should contain main.go after reset --hard")
	}
}

func findStatusEntry(entries []StatusEntry, path string) *StatusEntry {
	for i := range entries {
		if entries[i].Path == path {
			return &entries[i]
		}
	}
	return nil
}
