package repo

import (
	"os"
	"path/filepath"
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
