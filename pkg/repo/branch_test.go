package repo

import (
	"os"
	"path/filepath"
	"testing"
)

// Test 1: Create, list, and delete branches.
func TestBranch_CreateListDelete(t *testing.T) {
	r := initRepoWithFile(t, "main.go", []byte("package main\n\nfunc main() {}\n"))

	// Initial commit so HEAD resolves and "main" ref exists.
	_, err := r.Commit("initial commit", "test-author")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	headHash, err := r.ResolveRef("HEAD")
	if err != nil {
		t.Fatalf("ResolveRef(HEAD): %v", err)
	}

	// Create "feature" branch pointing at HEAD.
	if err := r.CreateBranch("feature", headHash); err != nil {
		t.Fatalf("CreateBranch(feature): %v", err)
	}

	// List should return ["feature", "main"] (sorted).
	branches, err := r.ListBranches()
	if err != nil {
		t.Fatalf("ListBranches: %v", err)
	}
	if len(branches) != 2 {
		t.Fatalf("ListBranches: got %d branches, want 2", len(branches))
	}
	if branches[0] != "feature" {
		t.Errorf("branches[0] = %q, want %q", branches[0], "feature")
	}
	if branches[1] != "main" {
		t.Errorf("branches[1] = %q, want %q", branches[1], "main")
	}

	// Delete "feature".
	if err := r.DeleteBranch("feature"); err != nil {
		t.Fatalf("DeleteBranch(feature): %v", err)
	}

	// List should now return only ["main"].
	branches, err = r.ListBranches()
	if err != nil {
		t.Fatalf("ListBranches after delete: %v", err)
	}
	if len(branches) != 1 {
		t.Fatalf("ListBranches after delete: got %d branches, want 1", len(branches))
	}
	if branches[0] != "main" {
		t.Errorf("branches[0] = %q, want %q", branches[0], "main")
	}
}

// Test 2: CurrentBranch returns "main" initially.
func TestBranch_CurrentBranch(t *testing.T) {
	r := initRepoWithFile(t, "main.go", []byte("package main\n\nfunc main() {}\n"))

	branch, err := r.CurrentBranch()
	if err != nil {
		t.Fatalf("CurrentBranch: %v", err)
	}
	if branch != "main" {
		t.Errorf("CurrentBranch = %q, want %q", branch, "main")
	}
}

// Test 3: Delete current branch returns error.
func TestBranch_DeleteCurrentBranch_Error(t *testing.T) {
	r := initRepoWithFile(t, "main.go", []byte("package main\n\nfunc main() {}\n"))

	_, err := r.Commit("initial commit", "test-author")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	err = r.DeleteBranch("main")
	if err == nil {
		t.Fatal("DeleteBranch(main) should have failed for current branch")
	}
}

// Test 4: CreateBranch fails if branch already exists.
func TestBranch_CreateDuplicate_Error(t *testing.T) {
	r := initRepoWithFile(t, "main.go", []byte("package main\n\nfunc main() {}\n"))

	h, err := r.Commit("initial commit", "test-author")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	if err := r.CreateBranch("feature", h); err != nil {
		t.Fatalf("CreateBranch(feature): %v", err)
	}

	// Creating again should fail.
	err = r.CreateBranch("feature", h)
	if err == nil {
		t.Fatal("CreateBranch(feature) should fail on duplicate")
	}
}

// Test 5: DeleteBranch for non-existent branch returns error.
func TestBranch_DeleteNonExistent_Error(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	err = r.DeleteBranch("ghost")
	if err == nil {
		t.Fatal("DeleteBranch(ghost) should have failed for non-existent branch")
	}
}

// Test 6: ListBranches on fresh repo with no ref files returns empty.
func TestBranch_ListEmpty(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// No commits yet, refs/heads/ exists but has no files.
	branches, err := r.ListBranches()
	if err != nil {
		t.Fatalf("ListBranches: %v", err)
	}
	if len(branches) != 0 {
		t.Errorf("ListBranches: got %d branches, want 0", len(branches))
	}
}

// Test 7: CreateBranch writes the correct hash to the ref file.
func TestBranch_CreateWritesCorrectHash(t *testing.T) {
	r := initRepoWithFile(t, "main.go", []byte("package main\n\nfunc main() {}\n"))

	h, err := r.Commit("initial commit", "test-author")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	if err := r.CreateBranch("feature", h); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}

	// Read the ref file directly to verify content.
	refPath := filepath.Join(r.GotDir, "refs", "heads", "feature")
	data, err := os.ReadFile(refPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	got := string(data)
	want := string(h) + "\n"
	if got != want {
		t.Errorf("ref file content = %q, want %q", got, want)
	}
}
