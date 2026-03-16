package repo

import (
	"context"
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

// --- New hooks engine tests ---

// writeScript creates an executable shell script in the given directory and
// returns its path.
func writeScript(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o755); err != nil {
		t.Fatalf("write script %s: %v", name, err)
	}
	return p
}

func TestRunHookEntry_StdinJSON(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("hook tests require unix shell scripts")
	}

	dir := t.TempDir()
	outFile := filepath.Join(dir, "out.txt")

	// Script reads stdin and writes it to a file.
	script := "#!/bin/sh\ncat > " + outFile + "\n"
	scriptPath := writeScript(t, dir, "hook.sh", script)

	entry := HookEntry{
		Name:  "reader",
		Point: "pre-commit",
		Run:   scriptPath,
	}

	payload := []byte(`{"hook":"pre-commit","repo":"/tmp"}`)
	err := RunHookEntry(context.Background(), dir, entry, payload)
	if err != nil {
		t.Fatalf("RunHookEntry: %v", err)
	}

	got, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if string(got) != string(payload) {
		t.Errorf("stdin = %q, want %q", got, payload)
	}
}

func TestRunHookEntry_NonZeroExitReturnsError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("hook tests require unix shell scripts")
	}

	dir := t.TempDir()
	scriptPath := writeScript(t, dir, "fail.sh", "#!/bin/sh\nexit 1\n")

	entry := HookEntry{
		Name:  "fail",
		Point: "pre-commit",
		Run:   scriptPath,
	}

	err := RunHookEntry(context.Background(), dir, entry, nil)
	if err == nil {
		t.Fatal("expected error from non-zero exit")
	}
	if !strings.Contains(err.Error(), "pre-commit.fail") {
		t.Errorf("error should mention hook name, got: %v", err)
	}
}

func TestRunHookEntry_TimeoutKillsProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("hook tests require unix shell scripts")
	}

	dir := t.TempDir()
	scriptPath := writeScript(t, dir, "slow.sh", "#!/bin/sh\nsleep 60\n")

	entry := HookEntry{
		Name:    "slow",
		Point:   "pre-commit",
		Run:     scriptPath,
		Timeout: "100ms",
	}

	err := RunHookEntry(context.Background(), dir, entry, nil)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("error should mention timeout, got: %v", err)
	}
}

func TestRunHooksForPoint_PreHooksAbortOnFirstFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("hook tests require unix shell scripts")
	}

	dir := t.TempDir()
	marker := filepath.Join(dir, "second-ran")

	failPath := writeScript(t, dir, "fail.sh", "#!/bin/sh\nexit 1\n")
	secondPath := writeScript(t, dir, "second.sh", "#!/bin/sh\ntouch "+marker+"\n")

	hooks := []HookEntry{
		{Name: "first", Point: "pre-commit", Run: failPath},
		{Name: "second", Point: "pre-commit", Run: secondPath},
	}

	err := RunHooksForPoint(context.Background(), dir, hooks, nil, true)
	if err == nil {
		t.Fatal("expected error from first hook")
	}

	if _, statErr := os.Stat(marker); statErr == nil {
		t.Error("second hook should not have run after first failed (canAbort=true)")
	}
}

func TestRunHooksForPoint_PostHooksContinueOnFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("hook tests require unix shell scripts")
	}

	dir := t.TempDir()
	marker := filepath.Join(dir, "second-ran")

	failPath := writeScript(t, dir, "fail.sh", "#!/bin/sh\nexit 1\n")
	secondPath := writeScript(t, dir, "second.sh", "#!/bin/sh\ntouch "+marker+"\n")

	hooks := []HookEntry{
		{Name: "first", Point: "post-commit", Run: failPath},
		{Name: "second", Point: "post-commit", Run: secondPath},
	}

	err := RunHooksForPoint(context.Background(), dir, hooks, nil, false)
	if err != nil {
		t.Fatalf("RunHooksForPoint should not return error for post hooks, got: %v", err)
	}

	if _, statErr := os.Stat(marker); statErr != nil {
		t.Error("second hook should have run despite first failing (canAbort=false)")
	}
}
