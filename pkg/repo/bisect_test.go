package repo

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/odvcencio/graft/pkg/object"
)

// bisectTestRepo creates a temp repo with n linear commits (c0 .. c{n-1}).
// Returns the repo and the ordered slice of commit hashes (oldest first).
func bisectTestRepo(t *testing.T, n int) (*Repo, []object.Hash) {
	t.Helper()
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	hashes := make([]object.Hash, n)
	for i := 0; i < n; i++ {
		content := fmt.Sprintf("package main\n\nvar v = %d\n", i)
		if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(content), 0o644); err != nil {
			t.Fatalf("write main.go: %v", err)
		}
		if err := r.Add([]string{"main.go"}); err != nil {
			t.Fatalf("Add: %v", err)
		}
		h, err := r.Commit(fmt.Sprintf("commit %d", i), "test-author")
		if err != nil {
			t.Fatalf("Commit %d: %v", i, err)
		}
		hashes[i] = h
	}
	return r, hashes
}

// TestBisect_LinearHistory creates 10 linear commits and verifies that bisect
// finds the first bad commit in approximately log2(n) steps.
func TestBisect_LinearHistory(t *testing.T) {
	r, hashes := bisectTestRepo(t, 10)

	// Pretend commit 6 (0-indexed) introduced the bug.
	// good = commit 0, bad = commit 9 (the latest).
	good := hashes[0]
	bad := hashes[9]
	bugIntroducedAt := 6

	res, err := r.BisectStart(bad, good)
	if err != nil {
		t.Fatalf("BisectStart: %v", err)
	}
	if res.Done {
		t.Fatal("BisectStart should not be done immediately")
	}

	steps := 0
	maxSteps := 15 // generous upper bound

	for !res.Done {
		steps++
		if steps > maxSteps {
			t.Fatalf("bisect took more than %d steps, something is wrong", maxSteps)
		}

		// Determine if the current commit is good or bad.
		// Find its index in the hash list.
		idx := -1
		for i, h := range hashes {
			if h == res.Current {
				idx = i
				break
			}
		}
		if idx == -1 {
			t.Fatalf("current commit %s not found in hash list", res.Current)
		}

		if idx >= bugIntroducedAt {
			res, err = r.BisectBad()
		} else {
			res, err = r.BisectGood()
		}
		if err != nil {
			t.Fatalf("bisect step %d: %v", steps, err)
		}
	}

	if res.FirstBad != hashes[bugIntroducedAt] {
		t.Errorf("FirstBad = %s, want %s (commit %d)", res.FirstBad, hashes[bugIntroducedAt], bugIntroducedAt)
	}

	// With 10 commits, bisect should find the answer in roughly log2(10) ~ 3-4 steps.
	expectedMax := int(math.Ceil(math.Log2(10))) + 1
	if steps > expectedMax {
		t.Errorf("bisect took %d steps, expected at most ~%d", steps, expectedMax)
	}

	t.Logf("bisect found commit %d in %d steps", bugIntroducedAt, steps)
}

// TestBisect_StartSavesState verifies that BisectStart creates the expected
// state files in .graft/bisect/.
func TestBisect_StartSavesState(t *testing.T) {
	r, hashes := bisectTestRepo(t, 5)

	good := hashes[0]
	bad := hashes[4]

	_, err := r.BisectStart(bad, good)
	if err != nil {
		t.Fatalf("BisectStart: %v", err)
	}

	bisectDir := filepath.Join(r.GraftDir, "bisect")

	// Check that the bisect directory exists.
	info, err := os.Stat(bisectDir)
	if err != nil {
		t.Fatalf("bisect dir missing: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("bisect path is not a directory")
	}

	// Check bad file.
	badData, err := os.ReadFile(filepath.Join(bisectDir, "bad"))
	if err != nil {
		t.Fatalf("read bad: %v", err)
	}
	if strings.TrimSpace(string(badData)) != string(bad) {
		t.Errorf("bad file = %q, want %q", strings.TrimSpace(string(badData)), bad)
	}

	// Check good file.
	goodData, err := os.ReadFile(filepath.Join(bisectDir, "good"))
	if err != nil {
		t.Fatalf("read good: %v", err)
	}
	if strings.TrimSpace(string(goodData)) != string(good) {
		t.Errorf("good file = %q, want %q", strings.TrimSpace(string(goodData)), good)
	}

	// Check start-ref file.
	startRefData, err := os.ReadFile(filepath.Join(bisectDir, "start-ref"))
	if err != nil {
		t.Fatalf("read start-ref: %v", err)
	}
	startRef := strings.TrimSpace(string(startRefData))
	if startRef != "refs/heads/main" {
		t.Errorf("start-ref = %q, want %q", startRef, "refs/heads/main")
	}

	// Check log file exists and has content.
	logData, err := os.ReadFile(filepath.Join(bisectDir, "log"))
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	logContent := string(logData)
	if !strings.Contains(logContent, "# bad:") {
		t.Error("log does not contain '# bad:' entry")
	}
	if !strings.Contains(logContent, "# good:") {
		t.Error("log does not contain '# good:' entry")
	}

	// Check expected-steps file.
	stepsData, err := os.ReadFile(filepath.Join(bisectDir, "expected-steps"))
	if err != nil {
		t.Fatalf("read expected-steps: %v", err)
	}
	stepsStr := strings.TrimSpace(string(stepsData))
	if stepsStr == "" {
		t.Error("expected-steps file is empty")
	}
}

// TestBisect_ResetRestoresRef verifies that BisectReset restores the original
// branch and cleans up the bisect directory.
func TestBisect_ResetRestoresRef(t *testing.T) {
	r, hashes := bisectTestRepo(t, 5)

	// Verify we start on main.
	branch, err := r.CurrentBranch()
	if err != nil {
		t.Fatalf("CurrentBranch: %v", err)
	}
	if branch != "main" {
		t.Fatalf("expected to start on main, got %q", branch)
	}

	good := hashes[0]
	bad := hashes[4]

	_, err = r.BisectStart(bad, good)
	if err != nil {
		t.Fatalf("BisectStart: %v", err)
	}

	// After start, HEAD should be detached (on the midpoint).
	branch, err = r.CurrentBranch()
	if err != nil {
		t.Fatalf("CurrentBranch after start: %v", err)
	}
	if branch != "" {
		t.Errorf("expected detached HEAD during bisect, got branch %q", branch)
	}

	// Now reset.
	if err := r.BisectReset(); err != nil {
		t.Fatalf("BisectReset: %v", err)
	}

	// HEAD should be back on main.
	branch, err = r.CurrentBranch()
	if err != nil {
		t.Fatalf("CurrentBranch after reset: %v", err)
	}
	if branch != "main" {
		t.Errorf("after reset, branch = %q, want %q", branch, "main")
	}

	// HEAD should resolve to the last commit.
	headHash, err := r.ResolveRef("HEAD")
	if err != nil {
		t.Fatalf("ResolveRef(HEAD) after reset: %v", err)
	}
	if headHash != hashes[4] {
		t.Errorf("after reset, HEAD = %s, want %s", headHash, hashes[4])
	}

	// Bisect directory should be gone.
	if r.IsBisecting() {
		t.Error("IsBisecting should be false after reset")
	}
}

// TestBisect_AlreadyBisecting verifies that BisectStart returns an error if a
// bisect session is already in progress.
func TestBisect_AlreadyBisecting(t *testing.T) {
	r, hashes := bisectTestRepo(t, 5)

	good := hashes[0]
	bad := hashes[4]

	_, err := r.BisectStart(bad, good)
	if err != nil {
		t.Fatalf("first BisectStart: %v", err)
	}

	// Second start should fail.
	_, err = r.BisectStart(bad, good)
	if err == nil {
		t.Fatal("expected error on second BisectStart, got nil")
	}
	if !strings.Contains(err.Error(), "already bisecting") {
		t.Errorf("error = %q, want it to contain 'already bisecting'", err.Error())
	}
}

// TestBisect_Skip verifies that BisectSkip advances to a different candidate.
func TestBisect_Skip(t *testing.T) {
	r, hashes := bisectTestRepo(t, 10)

	good := hashes[0]
	bad := hashes[9]

	res, err := r.BisectStart(bad, good)
	if err != nil {
		t.Fatalf("BisectStart: %v", err)
	}

	firstMidpoint := res.Current

	// Skip the current commit.
	res, err = r.BisectSkip()
	if err != nil {
		t.Fatalf("BisectSkip: %v", err)
	}

	// The new current should be different from the first midpoint.
	if res.Current == firstMidpoint {
		t.Errorf("BisectSkip returned same commit %s as before skip", res.Current)
	}

	// The current commit should be in our hash list (i.e., a valid commit).
	found := false
	for _, h := range hashes {
		if h == res.Current {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("BisectSkip returned unknown commit %s", res.Current)
	}

	// Check that the log contains a skip entry.
	logLines, err := r.BisectLog()
	if err != nil {
		t.Fatalf("BisectLog: %v", err)
	}
	hasSkip := false
	for _, line := range logLines {
		if strings.HasPrefix(line, "# skip:") {
			hasSkip = true
			break
		}
	}
	if !hasSkip {
		t.Error("bisect log does not contain a skip entry")
	}
}

// TestBisect_MidpointSelection verifies that the midpoint is roughly in the
// middle of the range between good and bad.
func TestBisect_MidpointSelection(t *testing.T) {
	r, hashes := bisectTestRepo(t, 20)

	good := hashes[0]
	bad := hashes[19]

	res, err := r.BisectStart(bad, good)
	if err != nil {
		t.Fatalf("BisectStart: %v", err)
	}

	// Find the index of the midpoint in the hash list.
	midIdx := -1
	for i, h := range hashes {
		if h == res.Current {
			midIdx = i
			break
		}
	}
	if midIdx == -1 {
		t.Fatalf("midpoint %s not found in hash list", res.Current)
	}

	// The candidates are commits 1..19 (commit 0 is good and excluded).
	// That's 19 candidates. The midpoint should be roughly at index 19/2 ~ 9-10
	// from the good side. We allow a range for the midpoint.
	//
	// The actual midpoint in the candidate list depends on BFS distance ordering,
	// but for a linear history it should be approximately in the middle.
	lower := 5  // at least past the first quarter
	upper := 15 // at most before the last quarter

	if midIdx < lower || midIdx > upper {
		t.Errorf("midpoint index = %d, expected roughly between %d and %d", midIdx, lower, upper)
	}

	t.Logf("midpoint selected at index %d out of 20 commits", midIdx)
}

// TestBisect_IsBisecting verifies IsBisecting returns correct state.
func TestBisect_IsBisecting(t *testing.T) {
	r, hashes := bisectTestRepo(t, 3)

	if r.IsBisecting() {
		t.Error("IsBisecting should be false before starting")
	}

	_, err := r.BisectStart(hashes[2], hashes[0])
	if err != nil {
		t.Fatalf("BisectStart: %v", err)
	}

	if !r.IsBisecting() {
		t.Error("IsBisecting should be true after starting")
	}

	if err := r.BisectReset(); err != nil {
		t.Fatalf("BisectReset: %v", err)
	}

	if r.IsBisecting() {
		t.Error("IsBisecting should be false after reset")
	}
}

// TestBisect_BisectLog verifies that the log accumulates entries.
func TestBisect_BisectLog(t *testing.T) {
	r, hashes := bisectTestRepo(t, 8)

	_, err := r.BisectStart(hashes[7], hashes[0])
	if err != nil {
		t.Fatalf("BisectStart: %v", err)
	}

	logLines, err := r.BisectLog()
	if err != nil {
		t.Fatalf("BisectLog: %v", err)
	}

	// After start, should have at least 2 entries (bad + good).
	if len(logLines) < 2 {
		t.Fatalf("expected at least 2 log lines, got %d", len(logLines))
	}

	// Mark good and check log grows.
	_, err = r.BisectGood()
	if err != nil {
		t.Fatalf("BisectGood: %v", err)
	}

	logLines2, err := r.BisectLog()
	if err != nil {
		t.Fatalf("BisectLog after good: %v", err)
	}

	if len(logLines2) <= len(logLines) {
		t.Errorf("log did not grow after BisectGood: before=%d after=%d", len(logLines), len(logLines2))
	}
}

// TestBisect_ResultMessage verifies that BisectResult.Message is populated.
func TestBisect_ResultMessage(t *testing.T) {
	r, hashes := bisectTestRepo(t, 5)

	res, err := r.BisectStart(hashes[4], hashes[0])
	if err != nil {
		t.Fatalf("BisectStart: %v", err)
	}

	if res.Message == "" {
		t.Error("BisectResult.Message should not be empty")
	}

	// The message should be one of our commit messages.
	if !strings.HasPrefix(res.Message, "commit ") {
		t.Errorf("Message = %q, expected it to start with 'commit '", res.Message)
	}
}

// TestBisect_StepsEstimate verifies the Steps field is reasonable.
func TestBisect_StepsEstimate(t *testing.T) {
	r, hashes := bisectTestRepo(t, 16)

	res, err := r.BisectStart(hashes[15], hashes[0])
	if err != nil {
		t.Fatalf("BisectStart: %v", err)
	}

	// 15 candidates (commits 1..15), log2(15) ~ 4
	if res.Steps < 2 || res.Steps > 5 {
		t.Errorf("Steps = %d, expected between 2 and 5 for 15 candidates", res.Steps)
	}
	t.Logf("remaining=%d steps=%d", res.Remaining, res.Steps)
}

// TestBisect_InvalidCommits verifies error handling for invalid hashes.
func TestBisect_InvalidCommits(t *testing.T) {
	r, hashes := bisectTestRepo(t, 3)

	_, err := r.BisectStart(object.Hash("deadbeef"), hashes[0])
	if err == nil {
		t.Fatal("expected error for invalid bad commit")
	}

	_, err = r.BisectStart(hashes[2], object.Hash("deadbeef"))
	if err == nil {
		t.Fatal("expected error for invalid good commit")
	}
}

// TestBisect_NotBisecting verifies errors when calling methods outside a session.
func TestBisect_NotBisecting(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	_, err = r.BisectGood()
	if err == nil {
		t.Error("expected error from BisectGood when not bisecting")
	}

	_, err = r.BisectBad()
	if err == nil {
		t.Error("expected error from BisectBad when not bisecting")
	}

	_, err = r.BisectSkip()
	if err == nil {
		t.Error("expected error from BisectSkip when not bisecting")
	}

	err = r.BisectReset()
	if err == nil {
		t.Error("expected error from BisectReset when not bisecting")
	}

	_, err = r.BisectLog()
	if err == nil {
		t.Error("expected error from BisectLog when not bisecting")
	}
}
