package repo

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func initGitBackedRepo(t *testing.T) *Repo {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	runGitTestCommand(t, dir, "init")
	runGitTestCommand(t, dir, "config", "user.name", "Test User")
	runGitTestCommand(t, dir, "config", "user.email", "test@example.com")
	return r
}

func runGitTestCommand(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
	}
	return string(out)
}

func TestGitStageFiles_ExternalProcessGuardCanBlock(t *testing.T) {
	r := initGitBackedRepo(t)
	if err := os.WriteFile(filepath.Join(r.RootDir, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	prev := SetExternalProcessGuard(func(spec ExternalProcessSpec) error {
		if spec.Label == "git-stage-files" {
			return errors.New("blocked git stage")
		}
		return nil
	})
	t.Cleanup(func() {
		SetExternalProcessGuard(prev)
	})

	r.gitStageFiles([]string{"main.go"})
	staged := runGitTestCommand(t, r.RootDir, "diff", "--cached", "--name-only")
	if staged != "" {
		t.Fatalf("staged output = %q, want empty", staged)
	}
}

func TestGitMirrorCommit_ExternalProcessGuardCanBlock(t *testing.T) {
	r := initGitBackedRepo(t)
	if err := os.WriteFile(filepath.Join(r.RootDir, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	runGitTestCommand(t, r.RootDir, "add", "main.go")

	prev := SetExternalProcessGuard(func(spec ExternalProcessSpec) error {
		if spec.Label == "git-mirror-commit" {
			return errors.New("blocked git mirror commit")
		}
		return nil
	})
	t.Cleanup(func() {
		SetExternalProcessGuard(prev)
	})

	r.gitMirrorCommit("mirror commit", "Test User <test@example.com>")
	cmd := exec.Command("git", "-C", r.RootDir, "rev-parse", "--verify", "HEAD")
	if err := cmd.Run(); err == nil {
		t.Fatal("expected no git HEAD commit after blocked mirror commit")
	}
}
