package gitbridge

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectGitChanges(t *testing.T) {
	dir := t.TempDir()

	// Init git repo with a commit
	runGit(t, dir, "init")
	os.WriteFile(filepath.Join(dir, "a.go"), []byte("package main\n"), 0644)
	runGit(t, dir, "add", "a.go")
	runGit(t, dir, "-c", "user.email=t@t", "-c", "user.name=T", "commit", "-m", "init")

	// Init bridge
	b, err := InitBridge(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()

	// Save current git HEAD as known state
	knownHead, err := b.GitHEAD()
	if err != nil {
		t.Fatal(err)
	}

	// No changes yet
	changed, err := b.GitRefsChanged(knownHead)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Error("expected no changes immediately after init")
	}

	// Make a git commit
	os.WriteFile(filepath.Join(dir, "b.go"), []byte("package main\n"), 0644)
	runGit(t, dir, "add", "b.go")
	runGit(t, dir, "-c", "user.email=t@t", "-c", "user.name=T", "commit", "-m", "second")

	// Now should detect changes
	changed, err = b.GitRefsChanged(knownHead)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Error("expected changes after git commit")
	}
}
