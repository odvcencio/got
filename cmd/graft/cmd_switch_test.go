package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIntegration_SwitchToExistingBranch(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dir := initRepo(t)

	// Create an initial commit on main.
	commitFile(t, dir, "base.txt", "base content\n", "initial commit")

	// Create a feature branch and switch to it with 'switch'.
	mustRunGraft(t, dir, "branch", "feature")
	out := mustRunGraft(t, dir, "switch", "feature")

	if !strings.Contains(out, "switched to branch 'feature'") {
		t.Fatalf("unexpected switch output: %s", out)
	}

	// Verify we are on the feature branch by committing a file and checking
	// it disappears when switching back to main.
	commitFile(t, dir, "feature.txt", "feature content\n", "feature commit")

	mustRunGraft(t, dir, "switch", "main")
	if _, err := os.Stat(filepath.Join(dir, "feature.txt")); err == nil {
		t.Fatal("feature.txt should not exist on main")
	}
}

func TestIntegration_SwitchCreateNewBranch(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dir := initRepo(t)

	// Create an initial commit.
	commitFile(t, dir, "base.txt", "base content\n", "initial commit")

	// Use switch -c to create and switch to a new branch.
	out := mustRunGraft(t, dir, "switch", "-c", "new-feature", "new-feature")

	if !strings.Contains(out, "switched to new branch 'new-feature'") {
		t.Fatalf("unexpected switch -c output: %s", out)
	}

	// Verify the branch exists.
	branchOut := mustRunGraft(t, dir, "branch")
	if !strings.Contains(branchOut, "new-feature") {
		t.Fatalf("branch list should contain new-feature: %s", branchOut)
	}
	if !strings.Contains(branchOut, "* new-feature") {
		t.Fatalf("new-feature should be the current branch: %s", branchOut)
	}
}

func TestIntegration_SwitchNonExistentBranch(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dir := initRepo(t)

	// Create an initial commit so the repo isn't empty.
	commitFile(t, dir, "base.txt", "base content\n", "initial commit")

	// Switching to a non-existent branch should fail.
	_, err := runGraft(t, dir, "switch", "does-not-exist")
	if err == nil {
		t.Fatal("expected error when switching to non-existent branch")
	}
}

func TestIntegration_SwitchDashNotSupported(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dir := initRepo(t)

	commitFile(t, dir, "base.txt", "base content\n", "initial commit")

	// Switch with "-" should report not yet supported.
	out, err := runGraft(t, dir, "switch", "-")
	if err == nil {
		t.Fatal("expected error when switching with -")
	}
	if !strings.Contains(out, "not yet supported") {
		t.Fatalf("expected 'not yet supported' message, got: %s", out)
	}
}
