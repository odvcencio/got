package gitbridge

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/odvcencio/graft/pkg/repo"
)

func TestDetectGitRepo(t *testing.T) {
	dir := t.TempDir()

	if DetectGitRepo(dir) {
		t.Error("expected false for non-git directory")
	}

	// Create .git directory structure
	os.MkdirAll(filepath.Join(dir, ".git", "objects"), 0755)
	os.MkdirAll(filepath.Join(dir, ".git", "refs"), 0755)
	os.WriteFile(filepath.Join(dir, ".git", "HEAD"), []byte("ref: refs/heads/main\n"), 0644)

	if !DetectGitRepo(dir) {
		t.Error("expected true for directory with .git")
	}
}

func TestInitBridge(t *testing.T) {
	dir := t.TempDir()

	cmd := exec.Command("git", "init", dir)
	if err := cmd.Run(); err != nil {
		t.Skipf("git not available: %v", err)
	}

	testFile := filepath.Join(dir, "main.go")
	os.WriteFile(testFile, []byte("package main\n\nfunc main() {}\n"), 0644)
	runGit(t, dir, "add", "main.go")
	runGit(t, dir, "-c", "user.email=test@test.com", "-c", "user.name=Test", "commit", "-m", "initial")

	b, err := InitBridge(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()

	if _, err := os.Stat(filepath.Join(dir, ".graft")); err != nil {
		t.Error(".graft directory not created")
	}

	if _, err := os.Stat(filepath.Join(dir, ".graft", "objects")); err != nil {
		t.Error(".graft/objects not created")
	}

	if _, err := os.Stat(filepath.Join(dir, ".graft", "refs", "heads")); err != nil {
		t.Error(".graft/refs/heads not created")
	}

	if b.hashMap.Len() == 0 {
		t.Error("expected hash map to have entries after init")
	}
}

func TestInitBridge_ExternalProcessGuardCanBlockImportHEAD(t *testing.T) {
	dir := t.TempDir()

	cmd := exec.Command("git", "init", dir)
	if err := cmd.Run(); err != nil {
		t.Skipf("git not available: %v", err)
	}

	testFile := filepath.Join(dir, "main.go")
	os.WriteFile(testFile, []byte("package main\n\nfunc main() {}\n"), 0644)
	runGit(t, dir, "add", "main.go")
	runGit(t, dir, "-c", "user.email=test@test.com", "-c", "user.name=Test", "commit", "-m", "initial")

	prev := repo.SetExternalProcessGuard(func(spec repo.ExternalProcessSpec) error {
		if spec.Label == "gitbridge:ls-files" {
			return errors.New("blocked git ls-files")
		}
		return nil
	})
	t.Cleanup(func() {
		repo.SetExternalProcessGuard(prev)
	})

	if _, err := InitBridge(dir); err == nil || !strings.Contains(err.Error(), "blocked git ls-files") {
		t.Fatalf("InitBridge error = %v, want blocked git ls-files", err)
	}
}

func TestGitHEAD_ExternalProcessGuardCanBlock(t *testing.T) {
	dir := t.TempDir()

	cmd := exec.Command("git", "init", dir)
	if err := cmd.Run(); err != nil {
		t.Skipf("git not available: %v", err)
	}

	testFile := filepath.Join(dir, "main.go")
	os.WriteFile(testFile, []byte("package main\n"), 0644)
	runGit(t, dir, "add", "main.go")
	runGit(t, dir, "-c", "user.email=test@test.com", "-c", "user.name=Test", "commit", "-m", "initial")

	prev := repo.SetExternalProcessGuard(func(spec repo.ExternalProcessSpec) error {
		if spec.Label == "gitbridge:rev-parse" {
			return errors.New("blocked git rev-parse")
		}
		return nil
	})
	t.Cleanup(func() {
		repo.SetExternalProcessGuard(prev)
	})

	b := &Bridge{rootDir: dir}
	if _, err := b.GitHEAD(); err == nil || !strings.Contains(err.Error(), "blocked git rev-parse") {
		t.Fatalf("GitHEAD error = %v, want blocked git rev-parse", err)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
}
