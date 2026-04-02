package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/odvcencio/graft/pkg/coordd"
	"github.com/odvcencio/graft/pkg/repo"
)

func initGitBackedGraftRepo(t *testing.T) *repo.Repo {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	runGitMainTestCommand(t, dir, "init", "-b", "main")
	runGitMainTestCommand(t, dir, "config", "user.name", "Test User")
	runGitMainTestCommand(t, dir, "config", "user.email", "test@example.com")
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(".graft/\n"), 0o644); err != nil {
		t.Fatalf("WriteFile .gitignore: %v", err)
	}
	if err := coordd.SaveGuardConfig(r.GraftDir, &coordd.GuardConfig{
		Mode:             "advisory",
		PreferredBackend: "host-direct",
	}); err != nil {
		t.Fatalf("SaveGuardConfig: %v", err)
	}
	return r
}

func runGitMainTestCommand(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
	}
	return string(out)
}

func TestGitIgnoreExplanation_RecordsGovernedProcessMetadata(t *testing.T) {
	r := initGitBackedGraftRepo(t)
	if err := os.WriteFile(filepath.Join(r.RootDir, ".gitignore"), []byte("ignored.txt\n"), 0o644); err != nil {
		t.Fatalf("WriteFile .gitignore: %v", err)
	}
	if err := os.WriteFile(filepath.Join(r.RootDir, "ignored.txt"), []byte("ignored\n"), 0o644); err != nil {
		t.Fatalf("WriteFile ignored.txt: %v", err)
	}

	explanation, err := gitIgnoreExplanation(r.RootDir, "ignored.txt")
	if err != nil {
		t.Fatalf("gitIgnoreExplanation: %v", err)
	}
	if !explanation.Ignored || explanation.Final == nil {
		t.Fatalf("expected ignored explanation with final match, got %#v", explanation)
	}

	events, err := coordd.ListEvents(r.GraftDir, 0)
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	found := false
	for _, event := range events {
		if event.Type != "action_exec_started" {
			continue
		}
		if event.Data["label"] == "git-check-ignore:verbose" && event.Data["origin"] == "git_check_ignore" && event.Data["point"] == "verbose" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected governed git-check-ignore event, got %#v", events)
	}
}

func TestExtractGitArchive_RecordsGovernedProcessMetadata(t *testing.T) {
	r := initGitBackedGraftRepo(t)
	if err := os.WriteFile(filepath.Join(r.RootDir, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("WriteFile README.md: %v", err)
	}
	runGitMainTestCommand(t, r.RootDir, "add", "README.md")
	runGitMainTestCommand(t, r.RootDir, "commit", "-m", "init")

	dest := t.TempDir()
	if err := extractGitArchive(context.Background(), r.RootDir, "HEAD", dest); err != nil {
		t.Fatalf("extractGitArchive: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "README.md")); err != nil {
		t.Fatalf("Stat README.md: %v", err)
	}

	events, err := coordd.ListEvents(r.GraftDir, 0)
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	found := false
	for _, event := range events {
		if event.Type != "action_exec_started" {
			continue
		}
		if event.Data["label"] == "git-repair:archive" && event.Data["origin"] == "git_repair" && event.Data["point"] == "archive" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected governed git-repair archive event, got %#v", events)
	}
}
