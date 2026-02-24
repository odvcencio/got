package repo

import (
	"os"
	"path/filepath"
	"testing"
)

// Test 1: Checkout restores files to the target branch's content.
func TestCheckout_RestoresFiles(t *testing.T) {
	r := initRepoWithFile(t, "main.go", []byte("package main\n\nfunc main() { v1() }\n"))

	// Initial commit on main.
	_, err := r.Commit("initial on main", "test-author")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	headHash, err := r.ResolveRef("HEAD")
	if err != nil {
		t.Fatalf("ResolveRef(HEAD): %v", err)
	}

	// Create "feature" branch at this commit.
	if err := r.CreateBranch("feature", headHash); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}

	// Modify file and commit again on main.
	mainPath := filepath.Join(r.RootDir, "main.go")
	if err := os.WriteFile(mainPath, []byte("package main\n\nfunc main() { v2() }\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	_, err = r.Commit("second on main", "test-author")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// Checkout "feature" — file should have original content.
	if err := r.Checkout("feature"); err != nil {
		t.Fatalf("Checkout(feature): %v", err)
	}

	data, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	want := "package main\n\nfunc main() { v1() }\n"
	if string(data) != want {
		t.Errorf("main.go content after checkout:\n  got:  %q\n  want: %q", string(data), want)
	}

	// HEAD should now point to feature branch.
	branch, err := r.CurrentBranch()
	if err != nil {
		t.Fatalf("CurrentBranch: %v", err)
	}
	if branch != "feature" {
		t.Errorf("CurrentBranch = %q, want %q", branch, "feature")
	}
}

// Test 2: Checkout removes files not in target tree.
func TestCheckout_RemovesExtraFiles(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Create two files and commit on main.
	for _, f := range []struct {
		name    string
		content []byte
	}{
		{"main.go", []byte("package main\n\nfunc main() {}\n")},
		{"extra.go", []byte("package main\n\nfunc extra() {}\n")},
	} {
		parent := filepath.Dir(filepath.Join(dir, f.name))
		if err := os.MkdirAll(parent, 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, f.name), f.content, 0o644); err != nil {
			t.Fatalf("write %s: %v", f.name, err)
		}
	}
	if err := r.Add([]string{"main.go", "extra.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	_, err = r.Commit("initial with both files", "test-author")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// Create "minimal" branch at current HEAD.
	headHash, err := r.ResolveRef("HEAD")
	if err != nil {
		t.Fatalf("ResolveRef: %v", err)
	}
	if err := r.CreateBranch("minimal", headHash); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}

	// On main: remove extra.go, commit.
	if err := os.Remove(filepath.Join(dir, "extra.go")); err != nil {
		t.Fatalf("Remove extra.go: %v", err)
	}
	// Re-add with only main.go staged.
	// We need to update staging to remove extra.go. Simplest: read staging,
	// remove the entry, write staging, then commit.
	stg, err := r.ReadStaging()
	if err != nil {
		t.Fatalf("ReadStaging: %v", err)
	}
	delete(stg.Entries, "extra.go")
	if err := r.WriteStaging(stg); err != nil {
		t.Fatalf("WriteStaging: %v", err)
	}
	_, err = r.Commit("remove extra.go on main", "test-author")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// Verify extra.go is NOT on disk now.
	if _, err := os.Stat(filepath.Join(dir, "extra.go")); err == nil {
		t.Fatal("extra.go should not exist on disk before checkout")
	}

	// Checkout "minimal" — which has both files.
	if err := r.Checkout("minimal"); err != nil {
		t.Fatalf("Checkout(minimal): %v", err)
	}

	// extra.go should now exist again.
	if _, err := os.Stat(filepath.Join(dir, "extra.go")); err != nil {
		t.Fatalf("extra.go should exist after checkout: %v", err)
	}

	// Now checkout back to main — extra.go should be removed.
	if err := r.Checkout("main"); err != nil {
		t.Fatalf("Checkout(main): %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "extra.go")); err == nil {
		t.Fatal("extra.go should have been removed after checkout to main")
	}
}

// Test 3: Dirty working tree refuses checkout.
func TestCheckout_DirtyWorkTree_Error(t *testing.T) {
	r := initRepoWithFile(t, "main.go", []byte("package main\n\nfunc main() {}\n"))

	_, err := r.Commit("initial commit", "test-author")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	headHash, err := r.ResolveRef("HEAD")
	if err != nil {
		t.Fatalf("ResolveRef: %v", err)
	}

	if err := r.CreateBranch("feature", headHash); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}

	// Modify the file WITHOUT staging (dirty working tree).
	mainPath := filepath.Join(r.RootDir, "main.go")
	if err := os.WriteFile(mainPath, []byte("package main\n\nfunc main() { dirty() }\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	err = r.Checkout("feature")
	if err == nil {
		t.Fatal("Checkout should fail with dirty working tree")
	}
}

// Test 4: Checkout with staged changes refuses.
func TestCheckout_StagedChanges_Error(t *testing.T) {
	r := initRepoWithFile(t, "main.go", []byte("package main\n\nfunc main() {}\n"))

	_, err := r.Commit("initial commit", "test-author")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	headHash, err := r.ResolveRef("HEAD")
	if err != nil {
		t.Fatalf("ResolveRef: %v", err)
	}

	if err := r.CreateBranch("feature", headHash); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}

	// Modify and stage (but don't commit).
	mainPath := filepath.Join(r.RootDir, "main.go")
	if err := os.WriteFile(mainPath, []byte("package main\n\nfunc main() { staged() }\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	err = r.Checkout("feature")
	if err == nil {
		t.Fatal("Checkout should fail with staged changes")
	}
}

// Test 5: Checkout detached (by raw hash) updates HEAD to non-symbolic.
func TestCheckout_DetachedHead(t *testing.T) {
	r := initRepoWithFile(t, "main.go", []byte("package main\n\nfunc main() {}\n"))

	h, err := r.Commit("initial commit", "test-author")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// Checkout by raw hash.
	if err := r.Checkout(string(h)); err != nil {
		t.Fatalf("Checkout(hash): %v", err)
	}

	// CurrentBranch should return "" (detached).
	branch, err := r.CurrentBranch()
	if err != nil {
		t.Fatalf("CurrentBranch: %v", err)
	}
	if branch != "" {
		t.Errorf("CurrentBranch = %q, want %q (detached)", branch, "")
	}

	// HEAD should resolve to the commit hash.
	resolved, err := r.ResolveRef("HEAD")
	if err != nil {
		t.Fatalf("ResolveRef(HEAD): %v", err)
	}
	if resolved != h {
		t.Errorf("HEAD = %q, want %q", resolved, h)
	}
}

// Test 6: Checkout handles subdirectories correctly.
func TestCheckout_Subdirectories(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Create files in subdirectories.
	files := map[string][]byte{
		"main.go":          []byte("package main\n\nfunc main() {}\n"),
		"pkg/util/util.go": []byte("package util\n\nfunc Util() {}\n"),
	}
	for name, content := range files {
		parent := filepath.Dir(filepath.Join(dir, name))
		if err := os.MkdirAll(parent, 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), content, 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	if err := r.Add([]string{"main.go", "pkg/util/util.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	_, err = r.Commit("initial with subdirs", "test-author")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	headHash, err := r.ResolveRef("HEAD")
	if err != nil {
		t.Fatalf("ResolveRef: %v", err)
	}

	// Create feature branch.
	if err := r.CreateBranch("feature", headHash); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}

	// Modify the subdirectory file and commit on main.
	if err := os.WriteFile(filepath.Join(dir, "pkg/util/util.go"),
		[]byte("package util\n\nfunc UtilV2() {}\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := r.Add([]string{"pkg/util/util.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	_, err = r.Commit("update util on main", "test-author")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// Checkout feature.
	if err := r.Checkout("feature"); err != nil {
		t.Fatalf("Checkout(feature): %v", err)
	}

	// util.go should have original content.
	data, err := os.ReadFile(filepath.Join(dir, "pkg/util/util.go"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	want := "package util\n\nfunc Util() {}\n"
	if string(data) != want {
		t.Errorf("util.go content:\n  got:  %q\n  want: %q", string(data), want)
	}
}

func TestCheckout_RestoresExecutableMode(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	script := filepath.Join(dir, "run.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho hi\n"), 0o755); err != nil {
		t.Fatalf("write run.sh: %v", err)
	}
	if err := r.Add([]string{"run.sh"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := r.Commit("add executable", "test-author"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	headHash, err := r.ResolveRef("HEAD")
	if err != nil {
		t.Fatalf("ResolveRef: %v", err)
	}
	if err := r.CreateBranch("exec", headHash); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}

	if err := os.Chmod(script, 0o644); err != nil {
		t.Fatalf("chmod run.sh 0644: %v", err)
	}
	if err := r.Add([]string{"run.sh"}); err != nil {
		t.Fatalf("Add non-executable: %v", err)
	}
	if _, err := r.Commit("drop executable bit", "test-author"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	if err := r.Checkout("exec"); err != nil {
		t.Fatalf("Checkout(exec): %v", err)
	}

	info, err := os.Stat(script)
	if err != nil {
		t.Fatalf("stat run.sh: %v", err)
	}
	if info.Mode()&0o111 == 0 {
		t.Fatalf("expected executable bit restored, mode=%#o", info.Mode().Perm())
	}
}
