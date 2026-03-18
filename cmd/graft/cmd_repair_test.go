package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/odvcencio/graft/pkg/gitbridge"
	"github.com/odvcencio/graft/pkg/repo"
)

func TestRepairReseedPreservesTrackedIgnoredFiles(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.name", "Test User")
	runGit(t, dir, "config", "user.email", "test@example.com")

	writeVerifyCmdFile(t, filepath.Join(dir, ".graftignore"), []byte("orchard\n"))
	writeVerifyCmdFile(t, filepath.Join(dir, "README.md"), []byte("hello\n"))
	writeVerifyCmdFile(t, filepath.Join(dir, "cmd", "orchard", "main.go"), []byte("package main\n"))
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "initial")

	existing, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	if err := existing.WriteConfig(&repo.Config{Remotes: map[string]string{"origin": "https://example.com/graft/repo"}}); err != nil {
		t.Fatalf("WriteConfig: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	var out bytes.Buffer
	cmd := newRepairCmd()
	cmd.SilenceUsage = true
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"reseed", "--yes"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v\noutput:\n%s", err, out.String())
	}

	backups, err := filepath.Glob(dir + ".graft-backup-*")
	if err != nil {
		t.Fatalf("Glob backups: %v", err)
	}
	if len(backups) != 1 {
		t.Fatalf("len(backups) = %d, want 1", len(backups))
	}

	reseeded, err := repo.Open(dir)
	if err != nil {
		t.Fatalf("repo.Open: %v", err)
	}
	headHash, err := reseeded.ResolveRef("HEAD")
	if err != nil {
		t.Fatalf("ResolveRef(HEAD): %v", err)
	}
	commitObj, err := reseeded.Store.ReadCommit(headHash)
	if err != nil {
		t.Fatalf("ReadCommit: %v", err)
	}
	files, err := reseeded.FlattenTree(commitObj.TreeHash)
	if err != nil {
		t.Fatalf("FlattenTree: %v", err)
	}

	got := make(map[string]struct{}, len(files))
	for _, file := range files {
		got[file.Path] = struct{}{}
	}
	for _, want := range []string{".graftignore", "README.md", "cmd/orchard/main.go"} {
		if _, ok := got[want]; !ok {
			t.Fatalf("missing %s from reseeded commit", want)
		}
	}

	cfg, err := reseeded.ReadConfig()
	if err != nil {
		t.Fatalf("ReadConfig: %v", err)
	}
	if cfg.Remotes["origin"] != "https://example.com/graft/repo" {
		t.Fatalf("origin = %q, want preserved remote", cfg.Remotes["origin"])
	}

	statusEntries, err := reseeded.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(statusEntries) != 0 {
		t.Fatalf("len(statusEntries) = %d, want clean worktree", len(statusEntries))
	}

	bridge, err := gitbridge.OpenBridge(dir)
	if err != nil {
		t.Fatalf("OpenBridge: %v", err)
	}
	defer bridge.Close()

	if _, err := os.Stat(filepath.Join(dir, ".graft", "hashmap")); err != nil {
		t.Fatalf("hashmap missing: %v", err)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, string(output))
	}
}
