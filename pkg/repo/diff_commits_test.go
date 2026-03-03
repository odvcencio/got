package repo

import (
	"os"
	"path/filepath"
	"testing"
)

// TestDiffCommits_AddedModifiedDeleted creates two commits where a file is
// modified, a new file is added, and one is deleted between them, and
// verifies that DiffCommits returns the correct file-level changes.
func TestDiffCommits_AddedModifiedDeleted(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// First commit: two files.
	if err := os.WriteFile(filepath.Join(dir, "keep.txt"), []byte("original"), 0o644); err != nil {
		t.Fatalf("write keep.txt: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "remove.txt"), []byte("will be removed"), 0o644); err != nil {
		t.Fatalf("write remove.txt: %v", err)
	}
	if err := r.Add([]string{"keep.txt", "remove.txt"}); err != nil {
		t.Fatalf("Add v1: %v", err)
	}
	h1, err := r.Commit("initial", "tester")
	if err != nil {
		t.Fatalf("Commit(initial): %v", err)
	}

	// Second commit: modify keep.txt, delete remove.txt, add new.txt.
	if err := os.WriteFile(filepath.Join(dir, "keep.txt"), []byte("modified"), 0o644); err != nil {
		t.Fatalf("write keep.txt v2: %v", err)
	}
	if err := os.Remove(filepath.Join(dir, "remove.txt")); err != nil {
		t.Fatalf("remove remove.txt: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("brand new"), 0o644); err != nil {
		t.Fatalf("write new.txt: %v", err)
	}
	if err := r.Remove([]string{"remove.txt"}, false); err != nil {
		t.Fatalf("Remove(remove.txt): %v", err)
	}
	if err := r.Add([]string{"keep.txt", "new.txt"}); err != nil {
		t.Fatalf("Add v2: %v", err)
	}
	h2, err := r.Commit("update", "tester")
	if err != nil {
		t.Fatalf("Commit(update): %v", err)
	}

	report, err := r.DiffCommits(h1, h2)
	if err != nil {
		t.Fatalf("DiffCommits: %v", err)
	}

	if report.OldCommit != h1 {
		t.Errorf("OldCommit = %q, want %q", report.OldCommit, h1)
	}
	if report.NewCommit != h2 {
		t.Errorf("NewCommit = %q, want %q", report.NewCommit, h2)
	}

	// Build a status map: path -> status.
	statusMap := make(map[string]string)
	for _, f := range report.Files {
		statusMap[f.Path] = f.Status
	}

	if s, ok := statusMap["keep.txt"]; !ok || s != "modified" {
		t.Errorf("keep.txt: expected 'modified', got %q (found=%v)", s, ok)
	}
	if s, ok := statusMap["remove.txt"]; !ok || s != "deleted" {
		t.Errorf("remove.txt: expected 'deleted', got %q (found=%v)", s, ok)
	}
	if s, ok := statusMap["new.txt"]; !ok || s != "added" {
		t.Errorf("new.txt: expected 'added', got %q (found=%v)", s, ok)
	}

	// Verify blob hashes are set correctly.
	for _, f := range report.Files {
		switch f.Status {
		case "added":
			if f.OldBlobHash != "" {
				t.Errorf("%s: added file should have empty OldBlobHash, got %q", f.Path, f.OldBlobHash)
			}
			if f.NewBlobHash == "" {
				t.Errorf("%s: added file should have non-empty NewBlobHash", f.Path)
			}
		case "deleted":
			if f.OldBlobHash == "" {
				t.Errorf("%s: deleted file should have non-empty OldBlobHash", f.Path)
			}
			if f.NewBlobHash != "" {
				t.Errorf("%s: deleted file should have empty NewBlobHash, got %q", f.Path, f.NewBlobHash)
			}
		case "modified":
			if f.OldBlobHash == "" {
				t.Errorf("%s: modified file should have non-empty OldBlobHash", f.Path)
			}
			if f.NewBlobHash == "" {
				t.Errorf("%s: modified file should have non-empty NewBlobHash", f.Path)
			}
			if f.OldBlobHash == f.NewBlobHash {
				t.Errorf("%s: modified file should have different blob hashes", f.Path)
			}
		}
	}
}

// TestDiffCommits_EntityChanges verifies that entity-level changes are
// included in the report when Go source files are involved.
func TestDiffCommits_EntityChanges(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	src1 := "package main\n\nfunc Alpha() int { return 1 }\n\nfunc Beta() int { return 2 }\n"
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(src1), 0o644); err != nil {
		t.Fatalf("write main.go v1: %v", err)
	}
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add v1: %v", err)
	}
	h1, err := r.Commit("initial", "tester")
	if err != nil {
		t.Fatalf("Commit(initial): %v", err)
	}

	src2 := "package main\n\nfunc Alpha() int { return 99 }\n\nfunc Gamma() int { return 3 }\n"
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(src2), 0o644); err != nil {
		t.Fatalf("write main.go v2: %v", err)
	}
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add v2: %v", err)
	}
	h2, err := r.Commit("modify", "tester")
	if err != nil {
		t.Fatalf("Commit(modify): %v", err)
	}

	report, err := r.DiffCommits(h1, h2)
	if err != nil {
		t.Fatalf("DiffCommits: %v", err)
	}

	if len(report.EntityChanges) == 0 {
		t.Fatal("expected entity changes, got none")
	}

	changeMap := make(map[string]string)
	for _, c := range report.EntityChanges {
		changeMap[c.Path+":"+c.EntityKey] = c.ChangeType
	}

	if ct, ok := changeMap["main.go:declaration:Alpha"]; !ok || ct != "modify" {
		t.Errorf("Alpha: expected 'modify', got %q (found=%v)", ct, ok)
	}
	if ct, ok := changeMap["main.go:declaration:Beta"]; !ok || ct != "delete" {
		t.Errorf("Beta: expected 'delete', got %q (found=%v)", ct, ok)
	}
	if ct, ok := changeMap["main.go:declaration:Gamma"]; !ok || ct != "create" {
		t.Errorf("Gamma: expected 'create', got %q (found=%v)", ct, ok)
	}
}

// TestDiffCommits_IdenticalCommits verifies that diffing a commit against
// itself returns no changes.
func TestDiffCommits_IdenticalCommits(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("content"), 0o644); err != nil {
		t.Fatalf("write file.txt: %v", err)
	}
	if err := r.Add([]string{"file.txt"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	h, err := r.Commit("single", "tester")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	report, err := r.DiffCommits(h, h)
	if err != nil {
		t.Fatalf("DiffCommits: %v", err)
	}

	if len(report.Files) != 0 {
		t.Errorf("expected no file changes for identical commits, got %d: %+v", len(report.Files), report.Files)
	}
}

// TestDiffRefs_Resolution verifies that DiffRefs resolves branch names and
// produces the same result as DiffCommits with the resolved hashes.
func TestDiffRefs_Resolution(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Create first commit on main.
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("write a.txt: %v", err)
	}
	if err := r.Add([]string{"a.txt"}); err != nil {
		t.Fatalf("Add a.txt: %v", err)
	}
	h1, err := r.Commit("first", "tester")
	if err != nil {
		t.Fatalf("Commit(first): %v", err)
	}

	// Create a branch "feature" at current HEAD.
	if err := r.CreateBranch("feature", h1); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}

	// Second commit on main.
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("world"), 0o644); err != nil {
		t.Fatalf("write b.txt: %v", err)
	}
	if err := r.Add([]string{"b.txt"}); err != nil {
		t.Fatalf("Add b.txt: %v", err)
	}
	h2, err := r.Commit("second", "tester")
	if err != nil {
		t.Fatalf("Commit(second): %v", err)
	}

	// DiffRefs feature..main should show b.txt as added.
	report, err := r.DiffRefs("feature", "main")
	if err != nil {
		t.Fatalf("DiffRefs: %v", err)
	}

	if report.OldCommit != h1 {
		t.Errorf("OldCommit = %q, want %q", report.OldCommit, h1)
	}
	if report.NewCommit != h2 {
		t.Errorf("NewCommit = %q, want %q", report.NewCommit, h2)
	}

	if len(report.Files) != 1 {
		t.Fatalf("expected 1 file change, got %d: %+v", len(report.Files), report.Files)
	}
	if report.Files[0].Path != "b.txt" || report.Files[0].Status != "added" {
		t.Errorf("expected b.txt added, got %+v", report.Files[0])
	}
}

// TestDiffRefs_BadRef verifies that DiffRefs returns an error for
// unresolvable ref names.
func TestDiffRefs_BadRef(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Create a commit so at least HEAD resolves.
	if err := os.WriteFile(filepath.Join(dir, "x.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := r.Add([]string{"x.txt"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := r.Commit("init", "tester"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	_, err = r.DiffRefs("nonexistent", "main")
	if err == nil {
		t.Fatal("expected error for bad ref, got nil")
	}

	_, err = r.DiffRefs("main", "nonexistent")
	if err == nil {
		t.Fatal("expected error for bad ref, got nil")
	}
}
