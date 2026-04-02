package repo

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// --- test helpers ---

// initGitGraftRepo creates a temp dir, runs git init, configures user.name/email,
// calls Init(dir), and returns the *Repo.
func initGitGraftRepo(t *testing.T) *Repo {
	t.Helper()
	dir := t.TempDir()

	// git init
	cmd := exec.Command("git", "init", dir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("git init: %v", err)
	}

	// configure user so commits work
	for _, kv := range [][2]string{
		{"user.name", "test"},
		{"user.email", "test@test.com"},
	} {
		cmd = exec.Command("git", "-C", dir, "config", kv[0], kv[1])
		if err := cmd.Run(); err != nil {
			t.Fatalf("git config %s: %v", kv[0], err)
		}
	}

	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	return r
}

// initGitGraftRepoWithCommit creates a repo with a committed file so that
// HEAD exists and branch operations work.
func initGitGraftRepoWithCommit(t *testing.T) *Repo {
	t.Helper()
	r := initGitGraftRepo(t)
	shadowWriteFile(t, r.RootDir, "initial.txt", "hello\n")
	r.GitShadowStage([]string{"initial.txt"})
	r.GitShadowCommit("initial commit", "test <test@test.com>", false)
	return r
}

// shadowWriteFile creates a file with the given content under dir.
func shadowWriteFile(t *testing.T, dir, name, content string) {
	t.Helper()
	p := filepath.Join(dir, name)
	parent := filepath.Dir(p)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", parent, err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", p, err)
	}
}

// gitOutput runs a git command in dir and returns stdout as a trimmed string.
func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	var stdout bytes.Buffer
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Stdout = &stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(stdout.String())
}

// --- tests ---

func TestHasGitDir_Present(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	r := &Repo{RootDir: dir}
	if !r.HasGitDir() {
		t.Error("HasGitDir() = false, want true when .git/ directory exists")
	}
}

func TestHasGitDir_Absent(t *testing.T) {
	dir := t.TempDir()
	r := &Repo{RootDir: dir}
	if r.HasGitDir() {
		t.Error("HasGitDir() = true, want false when .git/ does not exist")
	}
}

func TestHasGitDir_LinkedWorktree(t *testing.T) {
	dir := t.TempDir()
	// Write .git as a file (linked worktree style)
	if err := os.WriteFile(filepath.Join(dir, ".git"), []byte("gitdir: /some/path\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := &Repo{RootDir: dir}
	if !r.HasGitDir() {
		t.Error("HasGitDir() = false, want true when .git is a file (linked worktree)")
	}
}

func TestGitShadowStageFiles(t *testing.T) {
	r := initGitGraftRepo(t)
	shadowWriteFile(t, r.RootDir, "hello.txt", "world\n")

	r.GitShadowStage([]string{"hello.txt"})

	out := gitOutput(t, r.RootDir, "diff", "--cached", "--name-only")
	if !strings.Contains(out, "hello.txt") {
		t.Errorf("staged files = %q, want to contain %q", out, "hello.txt")
	}
}

func TestGitShadowCommit(t *testing.T) {
	r := initGitGraftRepo(t)
	shadowWriteFile(t, r.RootDir, "hello.txt", "world\n")
	r.GitShadowStage([]string{"hello.txt"})

	r.GitShadowCommit("test commit message", "Alice <alice@example.com>", false)

	out := gitOutput(t, r.RootDir, "log", "--oneline")
	if !strings.Contains(out, "test commit message") {
		t.Errorf("git log = %q, want to contain %q", out, "test commit message")
	}
}

func TestGitShadowBranch(t *testing.T) {
	r := initGitGraftRepoWithCommit(t)

	r.GitShadowCreateBranch("feature-x")

	out := gitOutput(t, r.RootDir, "branch", "--list")
	if !strings.Contains(out, "feature-x") {
		t.Errorf("branch list = %q, want to contain %q", out, "feature-x")
	}
}

func TestGitShadowDeleteBranch(t *testing.T) {
	r := initGitGraftRepoWithCommit(t)
	r.GitShadowCreateBranch("feature-y")

	r.GitShadowDeleteBranch("feature-y")

	out := gitOutput(t, r.RootDir, "branch", "--list")
	if strings.Contains(out, "feature-y") {
		t.Errorf("branch list = %q, should not contain deleted branch %q", out, "feature-y")
	}
}

func TestGitShadowTag(t *testing.T) {
	r := initGitGraftRepoWithCommit(t)

	r.GitShadowCreateTag("v1.0.0")

	out := gitOutput(t, r.RootDir, "tag", "--list")
	if !strings.Contains(out, "v1.0.0") {
		t.Errorf("tag list = %q, want to contain %q", out, "v1.0.0")
	}
}

func TestGitShadowDeleteTag(t *testing.T) {
	r := initGitGraftRepoWithCommit(t)
	r.GitShadowCreateTag("v2.0.0")

	r.GitShadowDeleteTag("v2.0.0")

	out := gitOutput(t, r.RootDir, "tag", "--list")
	if strings.Contains(out, "v2.0.0") {
		t.Errorf("tag list = %q, should not contain deleted tag %q", out, "v2.0.0")
	}
}

func TestGitShadowCheckout(t *testing.T) {
	r := initGitGraftRepoWithCommit(t)
	r.GitShadowCreateBranch("dev")

	r.GitShadowCheckout("dev")

	out := gitOutput(t, r.RootDir, "symbolic-ref", "--short", "HEAD")
	if out != "dev" {
		t.Errorf("HEAD = %q, want %q", out, "dev")
	}
}

func TestGitShadowFailureLogged(t *testing.T) {
	r := initGitGraftRepo(t)
	// Ensure GraftDir is set (Init sets it)
	if r.GraftDir == "" {
		t.Fatal("GraftDir is empty")
	}

	// Checkout a nonexistent branch — should fail and log
	r.GitShadowCheckout("nonexistent-branch-abc123")

	if !r.HasShadowFailures() {
		t.Error("HasShadowFailures() = false after a failed shadow operation, want true")
	}

	// Verify we can clear failures
	r.ClearShadowFailures()
	if r.HasShadowFailures() {
		t.Error("HasShadowFailures() = true after ClearShadowFailures(), want false")
	}
}
