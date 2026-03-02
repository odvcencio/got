package repo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stashTestRepo creates a temp repo with an initial committed file.
// Uses initRepoWithCommit from fetch_test.go which already exists.
func stashTestRepo(t *testing.T, name string, content []byte) *Repo {
	t.Helper()
	r, _ := initRepoWithCommit(t, name, content, "initial commit")
	return r
}

// Test 1: Stash saves changes, working tree reverts to HEAD.
func TestStashSavesAndReverts(t *testing.T) {
	r := stashTestRepo(t, "hello.txt", []byte("original"))

	// Modify the file.
	modPath := filepath.Join(r.RootDir, "hello.txt")
	if err := os.WriteFile(modPath, []byte("modified"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	entry, err := r.Stash("test-author")
	if err != nil {
		t.Fatalf("Stash: %v", err)
	}
	if entry == nil {
		t.Fatal("Stash returned nil entry")
	}
	if entry.CommitHash == "" {
		t.Fatal("Stash entry has empty commit hash")
	}

	// Working tree should now match HEAD (original content).
	data, err := os.ReadFile(modPath)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(data) != "original" {
		t.Errorf("working tree content = %q, want %q", string(data), "original")
	}
}

// Test 2: Pop applies the stash and removes entry.
func TestStashPopRestoresChanges(t *testing.T) {
	r := stashTestRepo(t, "hello.txt", []byte("original"))

	// Modify and stash.
	modPath := filepath.Join(r.RootDir, "hello.txt")
	if err := os.WriteFile(modPath, []byte("modified"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	if _, err := r.Stash("test-author"); err != nil {
		t.Fatalf("Stash: %v", err)
	}

	// Pop the stash.
	if err := r.StashPop(0); err != nil {
		t.Fatalf("StashPop: %v", err)
	}

	// File should have modified content.
	data, err := os.ReadFile(modPath)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(data) != "modified" {
		t.Errorf("after pop content = %q, want %q", string(data), "modified")
	}

	// Stash list should be empty.
	list, err := r.StashList()
	if err != nil {
		t.Fatalf("StashList: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("stash list has %d entries after pop, want 0", len(list))
	}
}

// Test 3: Apply restores but keeps entry in list.
func TestStashApplyWithoutRemoval(t *testing.T) {
	r := stashTestRepo(t, "hello.txt", []byte("original"))

	// Modify and stash.
	modPath := filepath.Join(r.RootDir, "hello.txt")
	if err := os.WriteFile(modPath, []byte("modified"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	if _, err := r.Stash("test-author"); err != nil {
		t.Fatalf("Stash: %v", err)
	}

	// Apply the stash.
	if err := r.StashApply(0); err != nil {
		t.Fatalf("StashApply: %v", err)
	}

	// File should have modified content.
	data, err := os.ReadFile(modPath)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(data) != "modified" {
		t.Errorf("after apply content = %q, want %q", string(data), "modified")
	}

	// Stash list should still have one entry.
	list, err := r.StashList()
	if err != nil {
		t.Fatalf("StashList: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("stash list has %d entries after apply, want 1", len(list))
	}
}

// Test 4: Multiple stashes show in newest-first order.
func TestStashListShowsEntries(t *testing.T) {
	r := stashTestRepo(t, "hello.txt", []byte("original"))

	modPath := filepath.Join(r.RootDir, "hello.txt")

	// Create first stash.
	if err := os.WriteFile(modPath, []byte("change1"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	entry1, err := r.Stash("test-author")
	if err != nil {
		t.Fatalf("Stash 1: %v", err)
	}

	// Create second stash.
	if err := os.WriteFile(modPath, []byte("change2"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	entry2, err := r.Stash("test-author")
	if err != nil {
		t.Fatalf("Stash 2: %v", err)
	}

	// List should have 2 entries, newest first.
	list, err := r.StashList()
	if err != nil {
		t.Fatalf("StashList: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("stash list has %d entries, want 2", len(list))
	}
	if list[0].CommitHash != entry2.CommitHash {
		t.Errorf("list[0].CommitHash = %q, want %q (newest)", list[0].CommitHash, entry2.CommitHash)
	}
	if list[1].CommitHash != entry1.CommitHash {
		t.Errorf("list[1].CommitHash = %q, want %q (oldest)", list[1].CommitHash, entry1.CommitHash)
	}
}

// Test 5: Drop removes specific entry by index.
func TestStashDropRemovesEntry(t *testing.T) {
	r := stashTestRepo(t, "hello.txt", []byte("original"))

	modPath := filepath.Join(r.RootDir, "hello.txt")

	// Create two stashes.
	if err := os.WriteFile(modPath, []byte("change1"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	entry1, err := r.Stash("test-author")
	if err != nil {
		t.Fatalf("Stash 1: %v", err)
	}

	if err := os.WriteFile(modPath, []byte("change2"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := r.Stash("test-author"); err != nil {
		t.Fatalf("Stash 2: %v", err)
	}

	// Drop index 0 (newest).
	if err := r.StashDrop(0); err != nil {
		t.Fatalf("StashDrop: %v", err)
	}

	list, err := r.StashList()
	if err != nil {
		t.Fatalf("StashList: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("stash list has %d entries after drop, want 1", len(list))
	}
	if list[0].CommitHash != entry1.CommitHash {
		t.Errorf("remaining entry = %q, want %q (the older stash)", list[0].CommitHash, entry1.CommitHash)
	}
}

// Test 6: Error when nothing to stash.
func TestStashOnCleanTreeReturnsError(t *testing.T) {
	r := stashTestRepo(t, "hello.txt", []byte("original"))

	// No modifications — stash should fail.
	_, err := r.Stash("test-author")
	if err == nil {
		t.Fatal("Stash on clean tree should return error, got nil")
	}
}

// Test 7: LIFO ordering works correctly with multiple stashes and pops.
func TestStashMultipleAndPopOrder(t *testing.T) {
	r := stashTestRepo(t, "hello.txt", []byte("original"))

	modPath := filepath.Join(r.RootDir, "hello.txt")

	// Stash "first".
	if err := os.WriteFile(modPath, []byte("first"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := r.Stash("test-author"); err != nil {
		t.Fatalf("Stash first: %v", err)
	}

	// Stash "second".
	if err := os.WriteFile(modPath, []byte("second"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := r.Stash("test-author"); err != nil {
		t.Fatalf("Stash second: %v", err)
	}

	// Stash "third".
	if err := os.WriteFile(modPath, []byte("third"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := r.Stash("test-author"); err != nil {
		t.Fatalf("Stash third: %v", err)
	}

	// Pop should restore "third" (most recent).
	if err := r.StashPop(0); err != nil {
		t.Fatalf("StashPop 0: %v", err)
	}
	data, err := os.ReadFile(modPath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data) != "third" {
		t.Errorf("first pop content = %q, want %q", string(data), "third")
	}

	// Revert back to clean state for next pop.
	if err := r.revertToHEAD(); err != nil {
		t.Fatalf("revertToHEAD: %v", err)
	}

	// Pop should restore "second".
	if err := r.StashPop(0); err != nil {
		t.Fatalf("StashPop 0: %v", err)
	}
	data, err = os.ReadFile(modPath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data) != "second" {
		t.Errorf("second pop content = %q, want %q", string(data), "second")
	}

	// Revert back to clean state for next pop.
	if err := r.revertToHEAD(); err != nil {
		t.Fatalf("revertToHEAD: %v", err)
	}

	// Pop should restore "first".
	if err := r.StashPop(0); err != nil {
		t.Fatalf("StashPop 0: %v", err)
	}
	data, err = os.ReadFile(modPath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data) != "first" {
		t.Errorf("third pop content = %q, want %q", string(data), "first")
	}

	// Stack should be empty.
	list, err := r.StashList()
	if err != nil {
		t.Fatalf("StashList: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("stash list has %d entries, want 0", len(list))
	}
}

// Test 8: StashShow displays correct changed files (modified).
func TestStashShowModified(t *testing.T) {
	r := stashTestRepo(t, "hello.txt", []byte("original"))

	// Modify the file.
	modPath := filepath.Join(r.RootDir, "hello.txt")
	if err := os.WriteFile(modPath, []byte("modified"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	if _, err := r.Stash("test-author"); err != nil {
		t.Fatalf("Stash: %v", err)
	}

	entries, err := r.StashShow(0)
	if err != nil {
		t.Fatalf("StashShow: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("StashShow returned %d entries, want 1", len(entries))
	}
	if entries[0].Path != "hello.txt" {
		t.Errorf("entry path = %q, want %q", entries[0].Path, "hello.txt")
	}
	if entries[0].ChangeType != "modified" {
		t.Errorf("entry change type = %q, want %q", entries[0].ChangeType, "modified")
	}
}

// Test 9: StashShow shows added files.
func TestStashShowAdded(t *testing.T) {
	r := stashTestRepo(t, "hello.txt", []byte("original"))

	// Add a new file.
	newPath := filepath.Join(r.RootDir, "new.txt")
	if err := os.WriteFile(newPath, []byte("new content"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	if _, err := r.Stash("test-author"); err != nil {
		t.Fatalf("Stash: %v", err)
	}

	entries, err := r.StashShow(0)
	if err != nil {
		t.Fatalf("StashShow: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("StashShow returned %d entries, want 1", len(entries))
	}
	if entries[0].Path != "new.txt" {
		t.Errorf("entry path = %q, want %q", entries[0].Path, "new.txt")
	}
	if entries[0].ChangeType != "added" {
		t.Errorf("entry change type = %q, want %q", entries[0].ChangeType, "added")
	}
}

// Test 10: StashShow shows deleted files.
func TestStashShowDeleted(t *testing.T) {
	r := stashTestRepo(t, "hello.txt", []byte("original"))

	// Delete the file.
	modPath := filepath.Join(r.RootDir, "hello.txt")
	if err := os.Remove(modPath); err != nil {
		t.Fatalf("remove: %v", err)
	}

	if _, err := r.Stash("test-author"); err != nil {
		t.Fatalf("Stash: %v", err)
	}

	entries, err := r.StashShow(0)
	if err != nil {
		t.Fatalf("StashShow: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("StashShow returned %d entries, want 1", len(entries))
	}
	if entries[0].Path != "hello.txt" {
		t.Errorf("entry path = %q, want %q", entries[0].Path, "hello.txt")
	}
	if entries[0].ChangeType != "deleted" {
		t.Errorf("entry change type = %q, want %q", entries[0].ChangeType, "deleted")
	}
}

// Test 11: StashShow with multiple changes.
func TestStashShowMultipleChanges(t *testing.T) {
	r := stashTestRepo(t, "a.txt", []byte("original a"))

	// Also add another file before the initial stash context.
	bPath := filepath.Join(r.RootDir, "b.txt")
	if err := os.WriteFile(bPath, []byte("original b"), 0o644); err != nil {
		t.Fatalf("write b: %v", err)
	}
	if err := r.Add([]string{"b.txt"}); err != nil {
		t.Fatalf("Add(b): %v", err)
	}
	if _, err := r.Commit("add b", "test-author"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// Modify a.txt, delete b.txt, add c.txt.
	if err := os.WriteFile(filepath.Join(r.RootDir, "a.txt"), []byte("modified a"), 0o644); err != nil {
		t.Fatalf("write a: %v", err)
	}
	if err := os.Remove(bPath); err != nil {
		t.Fatalf("remove b: %v", err)
	}
	if err := os.WriteFile(filepath.Join(r.RootDir, "c.txt"), []byte("new c"), 0o644); err != nil {
		t.Fatalf("write c: %v", err)
	}

	if _, err := r.Stash("test-author"); err != nil {
		t.Fatalf("Stash: %v", err)
	}

	entries, err := r.StashShow(0)
	if err != nil {
		t.Fatalf("StashShow: %v", err)
	}

	if len(entries) != 3 {
		t.Fatalf("StashShow returned %d entries, want 3", len(entries))
	}

	// Entries should be sorted by path.
	want := map[string]string{
		"a.txt": "modified",
		"b.txt": "deleted",
		"c.txt": "added",
	}
	for _, e := range entries {
		expected, ok := want[e.Path]
		if !ok {
			t.Errorf("unexpected entry: %q", e.Path)
			continue
		}
		if e.ChangeType != expected {
			t.Errorf("entry %q: change type = %q, want %q", e.Path, e.ChangeType, expected)
		}
	}
}

// Test 12: StashApply with merge applies cleanly when no conflicts.
func TestStashApplyMerge_CleanApply(t *testing.T) {
	r := stashTestRepo(t, "a.txt", []byte("line 1\nline 2\nline 3\n"))

	// Modify a.txt and stash.
	modPath := filepath.Join(r.RootDir, "a.txt")
	if err := os.WriteFile(modPath, []byte("line 1\nmodified by stash\nline 3\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := r.Stash("test-author"); err != nil {
		t.Fatalf("Stash: %v", err)
	}

	// Now apply the stash (working tree is clean, same as when stash was made).
	result, err := r.StashApplyMerge(0)
	if err != nil {
		t.Fatalf("StashApplyMerge: %v", err)
	}
	if !result.Clean {
		t.Errorf("expected clean apply, got conflicts: %v", result.ConflictPaths)
	}

	// Verify file content.
	data, err := os.ReadFile(modPath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data) != "line 1\nmodified by stash\nline 3\n" {
		t.Errorf("content = %q, want %q", string(data), "line 1\nmodified by stash\nline 3\n")
	}
}

// Test 13: StashApply with merge where working tree changed independently.
func TestStashApplyMerge_IndependentChanges(t *testing.T) {
	// Create repo with two files.
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Create and commit two files.
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("file a\n"), 0o644); err != nil {
		t.Fatalf("write a: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("file b\n"), 0o644); err != nil {
		t.Fatalf("write b: %v", err)
	}
	if err := r.Add([]string{"a.txt", "b.txt"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := r.Commit("initial", "test-author"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// Modify a.txt and stash.
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("modified a\n"), 0o644); err != nil {
		t.Fatalf("write a: %v", err)
	}
	if _, err := r.Stash("test-author"); err != nil {
		t.Fatalf("Stash: %v", err)
	}

	// Modify b.txt and commit (independent change).
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("modified b\n"), 0o644); err != nil {
		t.Fatalf("write b: %v", err)
	}
	if err := r.Add([]string{"b.txt"}); err != nil {
		t.Fatalf("Add b: %v", err)
	}
	if _, err := r.Commit("modify b", "test-author"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// Apply the stash. The stash modified a.txt, HEAD modified b.txt.
	// These are independent changes so it should apply cleanly.
	result, err := r.StashApplyMerge(0)
	if err != nil {
		t.Fatalf("StashApplyMerge: %v", err)
	}
	if !result.Clean {
		t.Errorf("expected clean apply, got conflicts: %v", result.ConflictPaths)
	}

	// Verify both changes are present.
	dataA, err := os.ReadFile(filepath.Join(dir, "a.txt"))
	if err != nil {
		t.Fatalf("read a: %v", err)
	}
	if string(dataA) != "modified a\n" {
		t.Errorf("a.txt = %q, want %q", string(dataA), "modified a\n")
	}

	dataB, err := os.ReadFile(filepath.Join(dir, "b.txt"))
	if err != nil {
		t.Fatalf("read b: %v", err)
	}
	if string(dataB) != "modified b\n" {
		t.Errorf("b.txt = %q, want %q", string(dataB), "modified b\n")
	}
}

// Test 14: StashApply with merge handles conflicts properly.
func TestStashApplyMerge_Conflicts(t *testing.T) {
	r := stashTestRepo(t, "file.txt", []byte("original content\n"))

	// Modify file and stash.
	modPath := filepath.Join(r.RootDir, "file.txt")
	if err := os.WriteFile(modPath, []byte("stash version\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := r.Stash("test-author"); err != nil {
		t.Fatalf("Stash: %v", err)
	}

	// Make a conflicting change and commit.
	if err := os.WriteFile(modPath, []byte("HEAD version\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := r.Add([]string{"file.txt"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := r.Commit("conflicting commit", "test-author"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// Apply the stash -- should have conflict.
	result, err := r.StashApplyMerge(0)
	if err != nil {
		t.Fatalf("StashApplyMerge: %v", err)
	}
	if result.Clean {
		t.Fatal("expected conflicts, got clean apply")
	}
	if len(result.ConflictPaths) != 1 {
		t.Fatalf("expected 1 conflict, got %d: %v", len(result.ConflictPaths), result.ConflictPaths)
	}
	if result.ConflictPaths[0] != "file.txt" {
		t.Errorf("conflict path = %q, want %q", result.ConflictPaths[0], "file.txt")
	}

	// The file on disk should have conflict markers.
	data, err := os.ReadFile(modPath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "<<<<<<<") || !strings.Contains(content, ">>>>>>>") {
		t.Errorf("file should contain conflict markers, got:\n%s", content)
	}
}

// Test 15: StashApply (non-merge) returns error on conflict.
func TestStashApply_ReturnsErrorOnConflict(t *testing.T) {
	r := stashTestRepo(t, "file.txt", []byte("original\n"))

	modPath := filepath.Join(r.RootDir, "file.txt")
	if err := os.WriteFile(modPath, []byte("stash version\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := r.Stash("test-author"); err != nil {
		t.Fatalf("Stash: %v", err)
	}

	if err := os.WriteFile(modPath, []byte("HEAD version\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := r.Add([]string{"file.txt"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := r.Commit("conflicting", "test-author"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	err := r.StashApply(0)
	if err == nil {
		t.Fatal("expected error from StashApply on conflict, got nil")
	}
	if !strings.Contains(err.Error(), "conflict") {
		t.Errorf("error should mention conflict, got: %v", err)
	}
}

// Test 16: StashShow index out of range returns error.
func TestStashShow_OutOfRange(t *testing.T) {
	r := stashTestRepo(t, "hello.txt", []byte("original"))

	_, err := r.StashShow(0)
	if err == nil {
		t.Fatal("expected error for empty stash, got nil")
	}
	if !strings.Contains(err.Error(), "out of range") {
		t.Errorf("error should mention out of range, got: %v", err)
	}
}

// Test 17: StashShowDiff returns unified diff content.
func TestStashShowDiff(t *testing.T) {
	r := stashTestRepo(t, "hello.txt", []byte("original\n"))

	modPath := filepath.Join(r.RootDir, "hello.txt")
	if err := os.WriteFile(modPath, []byte("modified\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	if _, err := r.Stash("test-author"); err != nil {
		t.Fatalf("Stash: %v", err)
	}

	diff, err := r.StashShowDiff(0)
	if err != nil {
		t.Fatalf("StashShowDiff: %v", err)
	}

	content := string(diff)
	if !strings.Contains(content, "--- a/hello.txt") {
		t.Errorf("diff should contain --- a/hello.txt, got:\n%s", content)
	}
	if !strings.Contains(content, "+++ b/hello.txt") {
		t.Errorf("diff should contain +++ b/hello.txt, got:\n%s", content)
	}
	if !strings.Contains(content, "-original") {
		t.Errorf("diff should contain -original, got:\n%s", content)
	}
	if !strings.Contains(content, "+modified") {
		t.Errorf("diff should contain +modified, got:\n%s", content)
	}
}
