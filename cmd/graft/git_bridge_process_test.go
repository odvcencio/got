package main

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/odvcencio/graft/pkg/coordd"
	"github.com/odvcencio/graft/pkg/repo"
)

func TestRunGitStreaming_BlockedByCoorddGuard(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	cmd := exec.Command("git", "-C", dir, "init")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, string(out))
	}
	if err := coordd.SaveGuardConfig(r.GraftDir, &coordd.GuardConfig{
		Mode:             "enforce",
		PreferredBackend: "host-direct",
	}); err != nil {
		t.Fatalf("SaveGuardConfig: %v", err)
	}

	err = runGitStreaming(context.Background(), dir, io.Discard, io.Discard, "commit", "--allow-empty", "-m", "sync")
	if err == nil {
		t.Fatal("expected git bridge commit to be blocked")
	}
	var exitCoder interface{ ExitCode() int }
	if !errors.As(err, &exitCoder) {
		t.Fatalf("expected exit-coded error, got %T: %v", err, err)
	}
	if exitCoder.ExitCode() != 126 {
		t.Fatalf("ExitCode = %d, want 126", exitCoder.ExitCode())
	}
}

func TestSyncGitSnapshotFromWorktree_IgnoresGraftMetadata(t *testing.T) {
	r := initGitBackedGraftRepo(t)

	if err := os.WriteFile(filepath.Join(r.RootDir, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("WriteFile README.md: %v", err)
	}
	runGitMainTestCommand(t, r.RootDir, "add", "README.md")
	runGitMainTestCommand(t, r.RootDir, "commit", "-m", "init")

	if err := os.WriteFile(filepath.Join(r.RootDir, "README.md"), []byte("updated\n"), 0o644); err != nil {
		t.Fatalf("WriteFile README.md update: %v", err)
	}
	if err := os.WriteFile(filepath.Join(r.GraftDir, "scratch"), []byte("metadata\n"), 0o644); err != nil {
		t.Fatalf("WriteFile graft scratch: %v", err)
	}

	if err := syncGitSnapshotFromWorktree(context.Background(), r); err != nil {
		t.Fatalf("syncGitSnapshotFromWorktree: %v", err)
	}

	status := runGitMainTestCommand(t, r.RootDir, "status", "--short")
	if status != "" {
		t.Fatalf("git status not clean after sync:\n%s", status)
	}

	tracked := runGitMainTestCommand(t, r.RootDir, "ls-files", "--", ".graft")
	if tracked != "" {
		t.Fatalf(".graft should not be tracked, got:\n%s", tracked)
	}
}
