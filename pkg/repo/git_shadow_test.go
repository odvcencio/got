package repo

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func initGitGraftRepo(t *testing.T) *Repo {
	t.Helper()
	dir := t.TempDir()

	// Initialize git repo first.
	cmd := exec.Command("git", "init", "-b", "main")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %s: %v", out, err)
	}

	// Configure git user for commits.
	for _, args := range [][]string{
		{"config", "user.name", "Test"},
		{"config", "user.email", "test@test.local"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s: %v", args, out, err)
		}
	}

	// Create initial git commit so the branch exists.
	placeholder := filepath.Join(dir, ".gitkeep")
	if err := os.WriteFile(placeholder, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"add", ".gitkeep"},
		{"commit", "-m", "initial"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s: %v", args, out, err)
		}
	}

	// Initialize graft repo alongside.
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init graft: %v", err)
	}
	return r
}

func TestHasGitDir(t *testing.T) {
	t.Run("with git", func(t *testing.T) {
		r := initGitGraftRepo(t)
		if !r.HasGitDir() {
			t.Error("HasGitDir() = false, want true")
		}
	})

	t.Run("without git", func(t *testing.T) {
		dir := t.TempDir()
		r, err := Init(dir)
		if err != nil {
			t.Fatal(err)
		}
		if r.HasGitDir() {
			t.Error("HasGitDir() = true, want false")
		}
	})
}

func TestGitShadowSyncSnapshot(t *testing.T) {
	r := initGitGraftRepo(t)

	// Write a file and make a graft commit.
	if err := os.WriteFile(filepath.Join(r.RootDir, "hello.txt"), []byte("hello world\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := r.Add([]string{"hello.txt"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	h, err := r.Commit("add hello", "test-author")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// Sync snapshot to git.
	short := string(h)
	if len(short) > 12 {
		short = short[:12]
	}
	msg := "graft resync: match graft HEAD " + short
	r.GitShadowSyncSnapshot(msg, "test-author")

	// Verify git log contains our commit.
	cmd := exec.Command("git", "log", "--oneline", "-1")
	cmd.Dir = r.RootDir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git log: %v", err)
	}
	if !strings.Contains(string(out), "graft resync") {
		t.Errorf("git log output %q does not contain resync commit", strings.TrimSpace(string(out)))
	}
}

func TestClearShadowFailures(t *testing.T) {
	r := initGitGraftRepo(t)

	// Create a shadow-failures.log.
	logPath := filepath.Join(r.GraftDir, "shadow-failures.log")
	if err := os.WriteFile(logPath, []byte("some failure\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	r.ClearShadowFailures()

	if _, err := os.Stat(logPath); !os.IsNotExist(err) {
		t.Error("shadow-failures.log should have been removed")
	}
}

func TestGitShadowCheckout(t *testing.T) {
	r := initGitGraftRepo(t)

	// Checkout a new branch via shadow.
	r.GitShadowCheckout("feature-x")

	// Verify git is on the branch.
	cmd := exec.Command("git", "symbolic-ref", "--short", "HEAD")
	cmd.Dir = r.RootDir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git symbolic-ref: %v", err)
	}
	if got := strings.TrimSpace(string(out)); got != "feature-x" {
		t.Errorf("git branch = %q, want %q", got, "feature-x")
	}
}

// TestResyncGitFlow exercises the full resync flow that the
// "graft repair resync-git" command performs.
func TestResyncGitFlow(t *testing.T) {
	r := initGitGraftRepo(t)

	// 1. Make a graft commit with a file.
	if err := os.WriteFile(filepath.Join(r.RootDir, "data.txt"), []byte("graft data\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := r.Add([]string{"data.txt"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	head, err := r.Commit("add data", "test <test@local>")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// 2. Artificially create shadow-failures.log.
	logPath := filepath.Join(r.GraftDir, "shadow-failures.log")
	if err := os.WriteFile(logPath, []byte("checkout main: error\nadd -A: error\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 3. Run the resync flow (same logic as the command).
	branch, _ := r.CurrentBranch()
	if branch != "" {
		r.GitShadowCheckout(branch)
	}

	author := r.ResolveAuthor()
	short := string(head)
	if len(short) > 12 {
		short = short[:12]
	}
	msg := "graft resync: match graft HEAD " + short
	r.GitShadowSyncSnapshot(msg, author)
	r.ClearShadowFailures()

	// 4. Verify git log shows the resync commit.
	cmd := exec.Command("git", "log", "--oneline", "-1")
	cmd.Dir = r.RootDir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git log: %v", err)
	}
	if !strings.Contains(string(out), "graft resync") {
		t.Errorf("git log %q does not contain resync commit", strings.TrimSpace(string(out)))
	}

	// 5. Verify shadow-failures.log is cleared.
	if _, err := os.Stat(logPath); !os.IsNotExist(err) {
		t.Error("shadow-failures.log should have been removed after resync")
	}

	// 6. Verify the file is tracked by git.
	cmd = exec.Command("git", "show", "HEAD:data.txt")
	cmd.Dir = r.RootDir
	content, err := cmd.Output()
	if err != nil {
		t.Fatalf("git show HEAD:data.txt: %v", err)
	}
	if string(content) != "graft data\n" {
		t.Errorf("git tracked content = %q, want %q", content, "graft data\n")
	}
}
