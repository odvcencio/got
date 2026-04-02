package repo

import (
	"context"
	"errors"
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

func TestRunHook_ExternalProcessGuardCanBlock(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("hook tests require unix shell scripts")
	}

	r := initRepoWithFile(t, "main.go", []byte("package main\n\nfunc main() {}\n"))
	marker := filepath.Join(r.RootDir, "hook-ran")
	installHook(t, r, HookPreCommit, "#!/bin/sh\ntouch "+marker+"\n", true)

	prev := SetExternalProcessGuard(func(spec ExternalProcessSpec) error {
		if spec.Label == "repo-hook:pre-commit" {
			return errors.New("blocked by external process guard")
		}
		return nil
	})
	t.Cleanup(func() {
		SetExternalProcessGuard(prev)
	})

	_, err := r.Commit("blocked by guard", "test-author")
	if err == nil {
		t.Fatal("expected commit to fail due to external process guard")
	}
	if !strings.Contains(err.Error(), "blocked by external process guard") {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Fatalf("hook marker should not exist, stat error=%v", statErr)
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

func TestCommit_RunsPreCommitAnalysisHook(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("hook tests require unix shell scripts")
	}

	r := initRepoWithFile(t, "main.go", []byte("package main\n"))
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatal(err)
	}

	// Install a pre-commit-analysis hook via hooks.toml.
	gtsDir := filepath.Join(r.RootDir, ".gts")
	marker := filepath.Join(gtsDir, "hook-ran")
	hookScript := writeScript(t, r.RootDir, "analysis-hook.sh",
		"#!/bin/sh\nmkdir -p "+gtsDir+" && touch "+marker+"\n")

	hooksToml := "[pre-commit-analysis.gts-refresh]\nrun = \"" + hookScript + "\"\non_fail = \"warn\"\n"
	if err := os.WriteFile(filepath.Join(r.RootDir, "hooks.toml"), []byte(hooksToml), 0o644); err != nil {
		t.Fatalf("write hooks.toml: %v", err)
	}

	_, err := r.Commit("test commit", "Test <test@test.com>")
	if err != nil {
		t.Fatalf("commit failed: %v", err)
	}

	if _, err := os.Stat(marker); os.IsNotExist(err) {
		t.Fatal("pre-commit-analysis hook did not run")
	}
}

func TestCommit_PreCommitAnalysisHookReceivesStagedPaths(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("hook tests require unix shell scripts")
	}

	r := initRepoWithFile(t, "main.go", []byte("package main\n"))
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatal(err)
	}

	// Hook that captures stdin (the staged paths payload) to a file.
	stdinCapture := filepath.Join(r.RootDir, "stdin-capture.txt")
	hookScript := writeScript(t, r.RootDir, "capture-hook.sh",
		"#!/bin/sh\ncat > "+stdinCapture+"\n")

	hooksToml := "[pre-commit-analysis.capture]\nrun = \"" + hookScript + "\"\n"
	if err := os.WriteFile(filepath.Join(r.RootDir, "hooks.toml"), []byte(hooksToml), 0o644); err != nil {
		t.Fatalf("write hooks.toml: %v", err)
	}

	_, err := r.Commit("test commit", "Test <test@test.com>")
	if err != nil {
		t.Fatalf("commit failed: %v", err)
	}

	data, err := os.ReadFile(stdinCapture)
	if err != nil {
		t.Fatalf("read stdin capture: %v", err)
	}
	got := strings.TrimSpace(string(data))
	if !strings.Contains(got, "main.go") {
		t.Errorf("expected staged paths to contain main.go, got: %q", got)
	}
}
