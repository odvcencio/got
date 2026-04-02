package repo

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCheckout_RestoresSidecar(t *testing.T) {
	// 1. Init repo, add a file, write .gts/index.json, commit on main.
	r := initRepoWithFile(t, "main.go", []byte("package main\n"))

	gtsDir := filepath.Join(r.RootDir, ".gts")
	if err := os.MkdirAll(gtsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	gtsContent := []byte(`{"version":"0.2.0"}`)
	if err := os.WriteFile(filepath.Join(gtsDir, "index.json"), gtsContent, 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := r.Commit("initial with sidecar", "test-author")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	headHash, err := r.ResolveRef("HEAD")
	if err != nil {
		t.Fatal(err)
	}

	// 2. Create branch "other", checkout other.
	if err := r.CreateBranch("other", headHash); err != nil {
		t.Fatal(err)
	}
	if err := r.Checkout("other"); err != nil {
		t.Fatalf("Checkout(other): %v", err)
	}

	// 3. Remove .gts/ on disk, add a different file, commit on other (no .gts/).
	os.RemoveAll(gtsDir)

	otherFile := filepath.Join(r.RootDir, "other.go")
	if err := os.WriteFile(otherFile, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := r.Add([]string{"other.go"}); err != nil {
		t.Fatal(err)
	}
	_, err = r.Commit("other branch commit", "test-author")
	if err != nil {
		t.Fatalf("Commit on other: %v", err)
	}

	// Verify .gts/ does not exist on disk before checking out main.
	if _, err := os.Stat(filepath.Join(gtsDir, "index.json")); err == nil {
		t.Fatal(".gts/index.json should not exist on other branch")
	}

	// 4. Checkout back to main.
	if err := r.Checkout("main"); err != nil {
		t.Fatalf("Checkout(main): %v", err)
	}

	// 5. Verify .gts/index.json exists with correct content.
	data, err := os.ReadFile(filepath.Join(gtsDir, "index.json"))
	if err != nil {
		t.Fatalf(".gts/index.json not restored after checkout: %v", err)
	}
	if string(data) != string(gtsContent) {
		t.Errorf(".gts/index.json content = %q, want %q", string(data), string(gtsContent))
	}
}

func TestCheckout_CleansStaleSidecar(t *testing.T) {
	// 1. Init repo, add file, write .gts/old.json, commit on main.
	r := initRepoWithFile(t, "main.go", []byte("package main\n"))

	gtsDir := filepath.Join(r.RootDir, ".gts")
	if err := os.MkdirAll(gtsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	oldContent := []byte(`{"file":"old"}`)
	if err := os.WriteFile(filepath.Join(gtsDir, "old.json"), oldContent, 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := r.Commit("main with old.json", "test-author")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	headHash, err := r.ResolveRef("HEAD")
	if err != nil {
		t.Fatal(err)
	}

	// 2. Create branch "other", checkout other.
	if err := r.CreateBranch("other", headHash); err != nil {
		t.Fatal(err)
	}
	if err := r.Checkout("other"); err != nil {
		t.Fatalf("Checkout(other): %v", err)
	}

	// 3. Replace .gts/old.json with .gts/new.json, commit on other.
	os.RemoveAll(gtsDir)
	if err := os.MkdirAll(gtsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	newContent := []byte(`{"file":"new"}`)
	if err := os.WriteFile(filepath.Join(gtsDir, "new.json"), newContent, 0o644); err != nil {
		t.Fatal(err)
	}

	// Need a tracked change to commit.
	otherFile := filepath.Join(r.RootDir, "other.go")
	if err := os.WriteFile(otherFile, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := r.Add([]string{"other.go"}); err != nil {
		t.Fatal(err)
	}
	_, err = r.Commit("other with new.json", "test-author")
	if err != nil {
		t.Fatalf("Commit on other: %v", err)
	}

	// 4. Checkout main.
	if err := r.Checkout("main"); err != nil {
		t.Fatalf("Checkout(main): %v", err)
	}

	// 5. Verify .gts/old.json exists, .gts/new.json does NOT exist.
	data, err := os.ReadFile(filepath.Join(gtsDir, "old.json"))
	if err != nil {
		t.Fatalf(".gts/old.json not restored: %v", err)
	}
	if string(data) != string(oldContent) {
		t.Errorf(".gts/old.json content = %q, want %q", string(data), string(oldContent))
	}

	if _, err := os.Stat(filepath.Join(gtsDir, "new.json")); err == nil {
		t.Error(".gts/new.json should not exist after checkout to main (stale sidecar not cleaned)")
	}
}
