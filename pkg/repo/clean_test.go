package repo

import (
	"os"
	"path/filepath"
	"testing"
)

// TestClean_RemovesUntrackedFiles creates a repo with tracked and untracked
// files, runs Clean with Force, and verifies that only untracked files are
// removed.
func TestClean_RemovesUntrackedFiles(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Create and stage a tracked file.
	if err := os.WriteFile(filepath.Join(dir, "tracked.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write tracked.go: %v", err)
	}
	if err := r.Add([]string{"tracked.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Create an untracked file.
	if err := os.WriteFile(filepath.Join(dir, "untracked.txt"), []byte("junk\n"), 0o644); err != nil {
		t.Fatalf("write untracked.txt: %v", err)
	}

	removed, err := r.Clean(CleanOptions{Force: true})
	if err != nil {
		t.Fatalf("Clean: %v", err)
	}

	if len(removed) != 1 {
		t.Fatalf("expected 1 removed path, got %d: %v", len(removed), removed)
	}
	if removed[0] != "untracked.txt" {
		t.Fatalf("expected removed path 'untracked.txt', got %q", removed[0])
	}

	// Verify the file is gone from disk.
	if _, err := os.Stat(filepath.Join(dir, "untracked.txt")); !os.IsNotExist(err) {
		t.Fatalf("expected untracked.txt to be removed from disk, stat err=%v", err)
	}

	// Verify tracked file is still present.
	if _, err := os.Stat(filepath.Join(dir, "tracked.go")); err != nil {
		t.Fatalf("tracked.go should still exist: %v", err)
	}
}

// TestClean_RequiresForce verifies that Clean without Force returns an error
// and does not remove any files.
func TestClean_RequiresForce(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Create an untracked file.
	untrackedPath := filepath.Join(dir, "junk.txt")
	if err := os.WriteFile(untrackedPath, []byte("data\n"), 0o644); err != nil {
		t.Fatalf("write junk.txt: %v", err)
	}

	_, err = r.Clean(CleanOptions{Force: false})
	if err == nil {
		t.Fatal("expected error when Force is false, got nil")
	}

	// File should still exist.
	if _, statErr := os.Stat(untrackedPath); statErr != nil {
		t.Fatalf("junk.txt should still exist after failed clean: %v", statErr)
	}
}

// TestClean_DryRun verifies that CleanDryRun returns the list of files that
// would be removed without actually removing them.
func TestClean_DryRun(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Create and stage a tracked file.
	if err := os.WriteFile(filepath.Join(dir, "tracked.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write tracked.go: %v", err)
	}
	if err := r.Add([]string{"tracked.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Create untracked files.
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatalf("write a.txt: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b\n"), 0o644); err != nil {
		t.Fatalf("write b.txt: %v", err)
	}

	wouldRemove, err := r.CleanDryRun(CleanOptions{Force: false})
	if err != nil {
		t.Fatalf("CleanDryRun: %v", err)
	}

	if len(wouldRemove) != 2 {
		t.Fatalf("expected 2 would-remove paths, got %d: %v", len(wouldRemove), wouldRemove)
	}

	// Files should still exist on disk.
	for _, name := range []string{"a.txt", "b.txt"} {
		if _, statErr := os.Stat(filepath.Join(dir, name)); statErr != nil {
			t.Fatalf("%s should still exist after dry run: %v", name, statErr)
		}
	}
}

// TestClean_PreservesTrackedFiles verifies that tracked files are never
// included in the removal list.
func TestClean_PreservesTrackedFiles(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Create and stage several tracked files.
	files := []string{"a.go", "b.go", "c.txt"}
	for _, name := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("content\n"), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	if err := r.Add(files); err != nil {
		t.Fatalf("Add: %v", err)
	}

	removed, err := r.Clean(CleanOptions{Force: true})
	if err != nil {
		t.Fatalf("Clean: %v", err)
	}

	if len(removed) != 0 {
		t.Fatalf("expected 0 removed paths, got %d: %v", len(removed), removed)
	}

	// All tracked files should still exist.
	for _, name := range files {
		if _, statErr := os.Stat(filepath.Join(dir, name)); statErr != nil {
			t.Fatalf("tracked file %s should exist: %v", name, statErr)
		}
	}
}

// TestClean_Directories verifies that with Directories=true, empty untracked
// directories are removed.
func TestClean_Directories(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Create and stage a tracked file.
	if err := os.WriteFile(filepath.Join(dir, "tracked.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write tracked.go: %v", err)
	}
	if err := r.Add([]string{"tracked.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Create an empty untracked directory.
	emptyDir := filepath.Join(dir, "empty-dir")
	if err := os.MkdirAll(emptyDir, 0o755); err != nil {
		t.Fatalf("mkdir empty-dir: %v", err)
	}

	removed, err := r.Clean(CleanOptions{Force: true, Directories: true})
	if err != nil {
		t.Fatalf("Clean: %v", err)
	}

	found := false
	for _, p := range removed {
		if p == "empty-dir" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected 'empty-dir' in removed list, got %v", removed)
	}

	// Verify the directory is gone.
	if _, statErr := os.Stat(emptyDir); !os.IsNotExist(statErr) {
		t.Fatalf("expected empty-dir to be removed, stat err=%v", statErr)
	}
}

// TestClean_IgnoredOnly verifies that -x removes only ignored files.
func TestClean_IgnoredOnly(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Create a .graftignore that ignores *.log files.
	if err := os.WriteFile(filepath.Join(dir, ".graftignore"), []byte("*.log\n"), 0o644); err != nil {
		t.Fatalf("write .graftignore: %v", err)
	}

	// Create an ignored file and an untracked non-ignored file.
	if err := os.WriteFile(filepath.Join(dir, "debug.log"), []byte("log\n"), 0o644); err != nil {
		t.Fatalf("write debug.log: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("notes\n"), 0o644); err != nil {
		t.Fatalf("write notes.txt: %v", err)
	}

	removed, err := r.Clean(CleanOptions{Force: true, IgnoredOnly: true})
	if err != nil {
		t.Fatalf("Clean: %v", err)
	}

	if len(removed) != 1 {
		t.Fatalf("expected 1 removed path, got %d: %v", len(removed), removed)
	}
	if removed[0] != "debug.log" {
		t.Fatalf("expected 'debug.log', got %q", removed[0])
	}

	// notes.txt should still exist.
	if _, statErr := os.Stat(filepath.Join(dir, "notes.txt")); statErr != nil {
		t.Fatalf("notes.txt should still exist: %v", statErr)
	}
}

// TestClean_IgnoredToo verifies that -X removes both untracked and ignored files.
func TestClean_IgnoredToo(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Create a .graftignore that ignores *.log files.
	if err := os.WriteFile(filepath.Join(dir, ".graftignore"), []byte("*.log\n"), 0o644); err != nil {
		t.Fatalf("write .graftignore: %v", err)
	}

	// Stage tracked files (including .graftignore so it is not cleaned).
	if err := os.WriteFile(filepath.Join(dir, "tracked.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write tracked.go: %v", err)
	}
	if err := r.Add([]string{"tracked.go", ".graftignore"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Create an ignored file and an untracked non-ignored file.
	if err := os.WriteFile(filepath.Join(dir, "debug.log"), []byte("log\n"), 0o644); err != nil {
		t.Fatalf("write debug.log: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("notes\n"), 0o644); err != nil {
		t.Fatalf("write notes.txt: %v", err)
	}

	removed, err := r.Clean(CleanOptions{Force: true, IgnoredToo: true})
	if err != nil {
		t.Fatalf("Clean: %v", err)
	}

	if len(removed) != 2 {
		t.Fatalf("expected 2 removed paths, got %d: %v", len(removed), removed)
	}

	// Both files should be gone.
	for _, name := range []string{"debug.log", "notes.txt"} {
		if _, statErr := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(statErr) {
			t.Fatalf("expected %s to be removed, stat err=%v", name, statErr)
		}
	}

	// Tracked file should remain.
	if _, statErr := os.Stat(filepath.Join(dir, "tracked.go")); statErr != nil {
		t.Fatalf("tracked.go should still exist: %v", statErr)
	}
}
