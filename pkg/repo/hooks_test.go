package repo

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// installHook creates a hook script in the repo's hooks directory.
func installHook(t *testing.T, r *Repo, name HookName, script string, executable bool) {
	t.Helper()
	hooksDir := filepath.Join(r.GraftDir, "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatalf("MkdirAll hooks: %v", err)
	}
	hookPath := filepath.Join(hooksDir, string(name))
	perm := os.FileMode(0o644)
	if executable {
		perm = 0o755
	}
	if err := os.WriteFile(hookPath, []byte(script), perm); err != nil {
		t.Fatalf("write hook %s: %v", name, err)
	}
}

func TestPreCommitHookBlocksCommit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("hook tests require unix shell scripts")
	}

	r := initRepoWithFile(t, "main.go", []byte("package main\n\nfunc main() {}\n"))

	installHook(t, r, HookPreCommit, "#!/bin/sh\nexit 1\n", true)

	_, err := r.Commit("should be blocked", "test-author")
	if err == nil {
		t.Fatal("expected commit to fail due to pre-commit hook, but it succeeded")
	}
	if !strings.Contains(err.Error(), "pre-commit") {
		t.Errorf("error should mention pre-commit, got: %v", err)
	}
}

func TestPreCommitHookAllowsCommit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("hook tests require unix shell scripts")
	}

	r := initRepoWithFile(t, "main.go", []byte("package main\n\nfunc main() {}\n"))

	installHook(t, r, HookPreCommit, "#!/bin/sh\nexit 0\n", true)

	h, err := r.Commit("should succeed", "test-author")
	if err != nil {
		t.Fatalf("Commit failed: %v", err)
	}

	c, err := r.Store.ReadCommit(h)
	if err != nil {
		t.Fatalf("ReadCommit: %v", err)
	}
	if c.Message != "should succeed" {
		t.Errorf("Message = %q, want %q", c.Message, "should succeed")
	}
}

func TestCommitMsgHookCanModifyMessage(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("hook tests require unix shell scripts")
	}

	r := initRepoWithFile(t, "main.go", []byte("package main\n\nfunc main() {}\n"))

	// This hook appends a "Signed-off-by" line to the commit message.
	script := `#!/bin/sh
echo "" >> "$1"
echo "Signed-off-by: Hook" >> "$1"
`
	installHook(t, r, HookCommitMsg, script, true)

	h, err := r.Commit("original message", "test-author")
	if err != nil {
		t.Fatalf("Commit failed: %v", err)
	}

	c, err := r.Store.ReadCommit(h)
	if err != nil {
		t.Fatalf("ReadCommit: %v", err)
	}
	if !strings.Contains(c.Message, "Signed-off-by: Hook") {
		t.Errorf("expected message to contain Signed-off-by, got: %q", c.Message)
	}
	if !strings.Contains(c.Message, "original message") {
		t.Errorf("expected message to still contain original text, got: %q", c.Message)
	}
}

func TestNoHookDirIsOk(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("hook tests require unix shell scripts")
	}

	r := initRepoWithFile(t, "main.go", []byte("package main\n\nfunc main() {}\n"))

	// Ensure no hooks directory exists.
	hooksDir := filepath.Join(r.GraftDir, "hooks")
	os.RemoveAll(hooksDir)

	h, err := r.Commit("no hooks", "test-author")
	if err != nil {
		t.Fatalf("Commit failed: %v", err)
	}

	c, err := r.Store.ReadCommit(h)
	if err != nil {
		t.Fatalf("ReadCommit: %v", err)
	}
	if c.Message != "no hooks" {
		t.Errorf("Message = %q, want %q", c.Message, "no hooks")
	}
}

func TestNonExecutableHookIsSkipped(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("hook tests require unix shell scripts")
	}

	r := initRepoWithFile(t, "main.go", []byte("package main\n\nfunc main() {}\n"))

	// Install a hook that would fail if executed, but make it non-executable.
	installHook(t, r, HookPreCommit, "#!/bin/sh\nexit 1\n", false)

	h, err := r.Commit("should succeed anyway", "test-author")
	if err != nil {
		t.Fatalf("Commit failed: %v", err)
	}

	c, err := r.Store.ReadCommit(h)
	if err != nil {
		t.Fatalf("ReadCommit: %v", err)
	}
	if c.Message != "should succeed anyway" {
		t.Errorf("Message = %q, want %q", c.Message, "should succeed anyway")
	}
}
