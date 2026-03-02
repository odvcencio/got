package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// graftBin holds the path to the built graft binary for integration tests.
var graftBin string

func TestMain(m *testing.M) {
	// Build the graft binary once into a temp directory.
	tmp, err := os.MkdirTemp("", "graft-integration-*")
	if err != nil {
		panic("create temp dir: " + err.Error())
	}
	defer os.RemoveAll(tmp)

	bin := filepath.Join(tmp, "graft")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/graft/")
	cmd.Dir = filepath.Clean(filepath.Join(mustGetwd(), "..", ".."))
	out, err := cmd.CombinedOutput()
	if err != nil {
		panic("build graft: " + err.Error() + "\n" + string(out))
	}
	graftBin = bin

	os.Exit(m.Run())
}

func mustGetwd() string {
	wd, err := os.Getwd()
	if err != nil {
		panic("getwd: " + err.Error())
	}
	return wd
}

// runGraft executes the graft binary with the given arguments in the specified
// directory. It returns the combined stdout/stderr output and any error.
func runGraft(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(graftBin, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "USER=TestUser")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// mustRunGraft is like runGraft but fails the test on error.
func mustRunGraft(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := runGraft(t, dir, args...)
	if err != nil {
		t.Fatalf("graft %v failed: %v\noutput: %s", args, err, out)
	}
	return out
}

// initRepo creates a fresh graft repo in a temp directory and returns the path.
func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	mustRunGraft(t, dir, "init")
	return dir
}

// writeFile creates a file with the given content relative to dir.
func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", name, err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// commitFile is a convenience that creates/overwrites a file, adds it, and
// commits with the given message. Returns the commit output.
func commitFile(t *testing.T, dir, name, content, message string) string {
	t.Helper()
	writeFile(t, dir, name, content)
	mustRunGraft(t, dir, "add", name)
	return mustRunGraft(t, dir, "commit", "-m", message, "--author", "Test User", "--no-sign")
}

// TestIntegration_InitAddCommitLog exercises the basic workflow:
// init -> create file -> add -> commit -> log.
func TestIntegration_InitAddCommitLog(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dir := initRepo(t)

	// Create a file and commit it.
	writeFile(t, dir, "hello.txt", "hello world\n")
	mustRunGraft(t, dir, "add", "hello.txt")
	commitOut := mustRunGraft(t, dir, "commit", "-m", "initial commit", "--author", "Test User", "--no-sign")

	// Commit output should contain the message and branch.
	if !strings.Contains(commitOut, "initial commit") {
		t.Errorf("commit output missing message: %s", commitOut)
	}
	if !strings.Contains(commitOut, "main") {
		t.Errorf("commit output missing branch name: %s", commitOut)
	}

	// Log should show the commit.
	logOut := mustRunGraft(t, dir, "log")
	if !strings.Contains(logOut, "initial commit") {
		t.Errorf("log output missing commit message: %s", logOut)
	}
	if !strings.Contains(logOut, "Test User") {
		t.Errorf("log output missing author: %s", logOut)
	}
	if !strings.Contains(logOut, "HEAD -> main") {
		t.Errorf("log output missing HEAD decoration: %s", logOut)
	}
}

// TestIntegration_BranchCheckoutMerge exercises branching and merging:
// init -> commit -> create branch -> checkout -> commit on branch -> checkout main -> merge.
func TestIntegration_BranchCheckoutMerge(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dir := initRepo(t)

	// Initial commit on main.
	commitFile(t, dir, "base.txt", "base content\n", "initial commit")

	// Create and switch to feature branch.
	mustRunGraft(t, dir, "branch", "feature")
	checkoutOut := mustRunGraft(t, dir, "checkout", "feature")
	if !strings.Contains(checkoutOut, "feature") {
		t.Errorf("checkout output missing branch name: %s", checkoutOut)
	}

	// Commit a new file on the feature branch.
	commitFile(t, dir, "feature.txt", "feature content\n", "add feature file")

	// Verify the feature file exists.
	if _, err := os.Stat(filepath.Join(dir, "feature.txt")); err != nil {
		t.Fatalf("feature.txt should exist on feature branch: %v", err)
	}

	// Switch back to main.
	mustRunGraft(t, dir, "checkout", "main")

	// On main, the feature file should not exist.
	if _, err := os.Stat(filepath.Join(dir, "feature.txt")); err == nil {
		t.Fatal("feature.txt should not exist on main before merge")
	}

	// Merge feature into main.
	mergeOut := mustRunGraft(t, dir, "merge", "feature")
	if !strings.Contains(mergeOut, "merge completed") && !strings.Contains(mergeOut, "fast-forward") {
		t.Errorf("merge output unexpected: %s", mergeOut)
	}

	// After merge, the feature file should exist on main.
	data, err := os.ReadFile(filepath.Join(dir, "feature.txt"))
	if err != nil {
		t.Fatalf("feature.txt should exist after merge: %v", err)
	}
	if string(data) != "feature content\n" {
		t.Errorf("feature.txt content mismatch: %q", string(data))
	}

	// Verify branch listing shows both branches.
	branchOut := mustRunGraft(t, dir, "branch")
	if !strings.Contains(branchOut, "main") {
		t.Errorf("branch list missing main: %s", branchOut)
	}
	if !strings.Contains(branchOut, "feature") {
		t.Errorf("branch list missing feature: %s", branchOut)
	}
	// The current branch should be marked with *.
	if !strings.Contains(branchOut, "* main") {
		t.Errorf("branch list should show main as current: %s", branchOut)
	}
}

// TestIntegration_StatusVariousStates exercises status output for different
// file states: staged, modified, and untracked.
func TestIntegration_StatusVariousStates(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dir := initRepo(t)

	// Status on fresh repo with no commits.
	statusOut := mustRunGraft(t, dir, "status")
	if !strings.Contains(statusOut, "no commits yet") {
		t.Errorf("status should report no commits yet: %s", statusOut)
	}

	// Commit a file so we have a base state.
	commitFile(t, dir, "tracked.txt", "original\n", "initial commit")

	// Clean status after commit.
	statusOut = mustRunGraft(t, dir, "status")
	if !strings.Contains(statusOut, "on main") {
		t.Errorf("status should show current branch: %s", statusOut)
	}
	// Should not have any staged/unstaged/untracked sections.
	if strings.Contains(statusOut, "staged:") ||
		strings.Contains(statusOut, "unstaged:") ||
		strings.Contains(statusOut, "untracked:") {
		t.Errorf("status should be clean after commit: %s", statusOut)
	}

	// Modify the tracked file -> should show as unstaged.
	writeFile(t, dir, "tracked.txt", "modified\n")
	statusOut = mustRunGraft(t, dir, "status")
	if !strings.Contains(statusOut, "unstaged:") {
		t.Errorf("status should show unstaged section: %s", statusOut)
	}
	if !strings.Contains(statusOut, "tracked.txt") {
		t.Errorf("status should list modified file: %s", statusOut)
	}

	// Create an untracked file -> should show as untracked.
	writeFile(t, dir, "newfile.txt", "new\n")
	statusOut = mustRunGraft(t, dir, "status")
	if !strings.Contains(statusOut, "untracked:") {
		t.Errorf("status should show untracked section: %s", statusOut)
	}
	if !strings.Contains(statusOut, "newfile.txt") {
		t.Errorf("status should list untracked file: %s", statusOut)
	}

	// Stage the files -> should show as staged.
	mustRunGraft(t, dir, "add", "tracked.txt", "newfile.txt")
	statusOut = mustRunGraft(t, dir, "status")
	if !strings.Contains(statusOut, "staged:") {
		t.Errorf("status should show staged section: %s", statusOut)
	}
	// Both files should appear in the staged section.
	if !strings.Contains(statusOut, "tracked.txt") {
		t.Errorf("status should show staged tracked.txt: %s", statusOut)
	}
	if !strings.Contains(statusOut, "newfile.txt") {
		t.Errorf("status should show staged newfile.txt: %s", statusOut)
	}
}

// TestIntegration_DiffBetweenCommits exercises diff output by modifying a file,
// staging the change, and checking diff --staged output.
func TestIntegration_DiffBetweenCommits(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dir := initRepo(t)

	// Create and commit a file.
	commitFile(t, dir, "content.txt", "line one\nline two\nline three\n", "first commit")

	// Modify the file.
	writeFile(t, dir, "content.txt", "line one\nline two modified\nline three\n")

	// Unstaged diff should show the change.
	diffOut := mustRunGraft(t, dir, "diff")
	if !strings.Contains(diffOut, "content.txt") {
		t.Errorf("diff output should reference the file: %s", diffOut)
	}
	if !strings.Contains(diffOut, "-line two") {
		t.Errorf("diff output should show removed line: %s", diffOut)
	}
	if !strings.Contains(diffOut, "+line two modified") {
		t.Errorf("diff output should show added line: %s", diffOut)
	}

	// Stage the change and check staged diff.
	mustRunGraft(t, dir, "add", "content.txt")
	stagedDiffOut := mustRunGraft(t, dir, "diff", "--staged")
	if !strings.Contains(stagedDiffOut, "content.txt") {
		t.Errorf("staged diff should reference the file: %s", stagedDiffOut)
	}
	if !strings.Contains(stagedDiffOut, "+line two modified") {
		t.Errorf("staged diff should show the change: %s", stagedDiffOut)
	}

	// After committing, staged diff should be empty.
	mustRunGraft(t, dir, "commit", "-m", "second commit", "--author", "Test User", "--no-sign")
	emptyDiff := mustRunGraft(t, dir, "diff", "--staged")
	if strings.TrimSpace(emptyDiff) != "" {
		t.Errorf("diff --staged should be empty after commit, got: %s", emptyDiff)
	}
}

// TestIntegration_TagCreateAndList exercises tag creation and listing.
func TestIntegration_TagCreateAndList(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dir := initRepo(t)

	// Need at least one commit to tag.
	commitFile(t, dir, "readme.txt", "hello\n", "initial commit")

	// Create a lightweight tag.
	mustRunGraft(t, dir, "tag", "v1.0")

	// List tags and verify v1.0 appears.
	tagOut := mustRunGraft(t, dir, "tag")
	if !strings.Contains(tagOut, "v1.0") {
		t.Errorf("tag list should contain v1.0: %s", tagOut)
	}

	// Create a second tag.
	mustRunGraft(t, dir, "tag", "v1.1")
	tagOut = mustRunGraft(t, dir, "tag")
	if !strings.Contains(tagOut, "v1.0") || !strings.Contains(tagOut, "v1.1") {
		t.Errorf("tag list should contain both tags: %s", tagOut)
	}

	// Delete a tag and verify it's gone.
	mustRunGraft(t, dir, "tag", "-d", "v1.0")
	tagOut = mustRunGraft(t, dir, "tag")
	if strings.Contains(tagOut, "v1.0") {
		t.Errorf("tag list should not contain deleted v1.0: %s", tagOut)
	}
	if !strings.Contains(tagOut, "v1.1") {
		t.Errorf("tag list should still contain v1.1: %s", tagOut)
	}
}
