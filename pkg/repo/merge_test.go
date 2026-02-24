package repo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setupMergeRepo creates a test repo with an initial commit on "main",
// creates a "feature" branch from that commit, and returns the repo and
// temp directory. The initial commit contains main.go with function A.
func setupMergeRepo(t *testing.T) (*Repo, string) {
	t.Helper()

	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	base := `package main

func A() { println("a") }
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(base), 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add main.go: %v", err)
	}

	_, err = r.Commit("initial commit", "test-author")
	if err != nil {
		t.Fatalf("initial Commit: %v", err)
	}

	headHash, err := r.ResolveRef("HEAD")
	if err != nil {
		t.Fatalf("ResolveRef(HEAD): %v", err)
	}

	// Create feature branch at the same commit.
	if err := r.CreateBranch("feature", headHash); err != nil {
		t.Fatalf("CreateBranch(feature): %v", err)
	}

	return r, dir
}

// TestMerge_CleanNonOverlapping verifies that merging two branches with
// non-overlapping additions (main adds func C, feature adds func B)
// produces a clean merge containing all three functions.
func TestMerge_CleanNonOverlapping(t *testing.T) {
	r, dir := setupMergeRepo(t)

	// On main: add func C.
	oursContent := `package main

func A() { println("a") }

func C() { println("c") }
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(oursContent), 0o644); err != nil {
		t.Fatalf("write main.go (ours): %v", err)
	}
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add main.go (ours): %v", err)
	}
	_, err := r.Commit("add func C on main", "test-author")
	if err != nil {
		t.Fatalf("Commit (ours): %v", err)
	}

	// Switch to feature branch.
	if err := r.Checkout("feature"); err != nil {
		t.Fatalf("Checkout(feature): %v", err)
	}

	// On feature: add func B.
	theirsContent := `package main

func A() { println("a") }

func B() { println("b") }
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(theirsContent), 0o644); err != nil {
		t.Fatalf("write main.go (theirs): %v", err)
	}
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add main.go (theirs): %v", err)
	}
	_, err = r.Commit("add func B on feature", "test-author")
	if err != nil {
		t.Fatalf("Commit (theirs): %v", err)
	}

	// Switch back to main.
	if err := r.Checkout("main"); err != nil {
		t.Fatalf("Checkout(main): %v", err)
	}

	// Merge feature into main.
	report, err := r.Merge("feature")
	if err != nil {
		t.Fatalf("Merge(feature): %v", err)
	}

	if report.HasConflicts {
		t.Fatalf("expected clean merge, got conflicts: %+v", report)
	}

	// Verify merged file contains all three functions.
	merged, err := os.ReadFile(filepath.Join(dir, "main.go"))
	if err != nil {
		t.Fatalf("read merged main.go: %v", err)
	}
	mergedStr := string(merged)
	if !strings.Contains(mergedStr, "func A()") {
		t.Errorf("merged file missing func A: %s", mergedStr)
	}
	if !strings.Contains(mergedStr, "func B()") {
		t.Errorf("merged file missing func B: %s", mergedStr)
	}
	if !strings.Contains(mergedStr, "func C()") {
		t.Errorf("merged file missing func C: %s", mergedStr)
	}
}

// TestMerge_ConflictReported verifies that both sides modifying the same
// function produces a conflict with HasConflicts=true and conflict markers
// in the file on disk.
func TestMerge_ConflictReported(t *testing.T) {
	r, dir := setupMergeRepo(t)

	// On main: modify func A.
	oursContent := `package main

func A() { println("ours") }
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(oursContent), 0o644); err != nil {
		t.Fatalf("write main.go (ours): %v", err)
	}
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add main.go (ours): %v", err)
	}
	_, err := r.Commit("modify A on main", "test-author")
	if err != nil {
		t.Fatalf("Commit (ours): %v", err)
	}

	// Switch to feature branch.
	if err := r.Checkout("feature"); err != nil {
		t.Fatalf("Checkout(feature): %v", err)
	}

	// On feature: modify func A differently.
	theirsContent := `package main

func A() { println("theirs") }
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(theirsContent), 0o644); err != nil {
		t.Fatalf("write main.go (theirs): %v", err)
	}
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add main.go (theirs): %v", err)
	}
	_, err = r.Commit("modify A on feature", "test-author")
	if err != nil {
		t.Fatalf("Commit (theirs): %v", err)
	}

	// Switch back to main.
	if err := r.Checkout("main"); err != nil {
		t.Fatalf("Checkout(main): %v", err)
	}

	// Merge feature into main.
	report, err := r.Merge("feature")
	if err != nil {
		t.Fatalf("Merge(feature): %v", err)
	}

	if !report.HasConflicts {
		t.Fatal("expected conflicts, got clean merge")
	}
	if report.TotalConflicts == 0 {
		t.Error("TotalConflicts should be > 0")
	}
	if report.MergeCommit != "" {
		t.Error("MergeCommit should be empty for conflicted merge")
	}

	// Verify conflict markers in the file on disk.
	merged, err := os.ReadFile(filepath.Join(dir, "main.go"))
	if err != nil {
		t.Fatalf("read merged main.go: %v", err)
	}
	mergedStr := string(merged)
	if !strings.Contains(mergedStr, "<<<<<<<") {
		t.Errorf("expected conflict markers in file, got:\n%s", mergedStr)
	}
	if !strings.Contains(mergedStr, ">>>>>>>") {
		t.Errorf("expected conflict markers in file, got:\n%s", mergedStr)
	}

	stg, err := r.ReadStaging()
	if err != nil {
		t.Fatalf("ReadStaging: %v", err)
	}
	entry := stg.Entries["main.go"]
	if entry == nil {
		t.Fatalf("expected main.go in staging after conflicted merge")
	}
	if !entry.Conflict {
		t.Fatalf("expected main.go conflict flag in staging")
	}
	if entry.BaseBlobHash == "" || entry.OursBlobHash == "" || entry.TheirsBlobHash == "" {
		t.Fatalf("expected conflict blob hashes populated, got base=%q ours=%q theirs=%q", entry.BaseBlobHash, entry.OursBlobHash, entry.TheirsBlobHash)
	}

	statusEntries, err := r.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	foundConflict := false
	for _, e := range statusEntries {
		if e.Path == "main.go" && (e.IndexStatus == StatusConflict || e.WorkStatus == StatusConflict) {
			foundConflict = true
			break
		}
	}
	if !foundConflict {
		t.Fatalf("expected status to expose conflict state for main.go")
	}
}

// TestMerge_DeleteVsModifyFileConflict verifies repository-level safety for
// file delete-vs-modify: the merge must report a conflict and keep conflict
// markers instead of silently dropping the modified side.
func TestMerge_DeleteVsModifyFileConflict(t *testing.T) {
	r, dir := setupMergeRepo(t)

	// On main: modify main.go.
	oursContent := `package main

func A() { println("ours-change") }
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(oursContent), 0o644); err != nil {
		t.Fatalf("write main.go (ours): %v", err)
	}
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add main.go (ours): %v", err)
	}
	if _, err := r.Commit("modify main.go on main", "test-author"); err != nil {
		t.Fatalf("Commit (ours): %v", err)
	}

	// Switch to feature and delete main.go while adding another tracked file
	// so the delete commit is non-empty.
	if err := r.Checkout("feature"); err != nil {
		t.Fatalf("Checkout(feature): %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "keep.txt"), []byte("keep\n"), 0o644); err != nil {
		t.Fatalf("write keep.txt: %v", err)
	}
	if err := r.Add([]string{"keep.txt"}); err != nil {
		t.Fatalf("Add keep.txt: %v", err)
	}
	if err := r.Remove([]string{"main.go"}, false); err != nil {
		t.Fatalf("Remove main.go: %v", err)
	}
	if _, err := r.Commit("delete main.go on feature", "test-author"); err != nil {
		t.Fatalf("Commit (delete): %v", err)
	}

	// Merge feature into main.
	if err := r.Checkout("main"); err != nil {
		t.Fatalf("Checkout(main): %v", err)
	}
	report, err := r.Merge("feature")
	if err != nil {
		t.Fatalf("Merge(feature): %v", err)
	}

	if !report.HasConflicts {
		t.Fatalf("expected conflict for delete-vs-modify, got clean merge: %+v", report)
	}
	if report.TotalConflicts == 0 {
		t.Fatalf("expected conflict count > 0, got 0")
	}

	merged, err := os.ReadFile(filepath.Join(dir, "main.go"))
	if err != nil {
		t.Fatalf("read conflicted main.go: %v", err)
	}
	mergedStr := string(merged)
	if !strings.Contains(mergedStr, "<<<<<<< ours") || !strings.Contains(mergedStr, ">>>>>>> theirs") {
		t.Fatalf("expected conflict markers in main.go, got:\n%s", mergedStr)
	}
	if !strings.Contains(mergedStr, "ours-change") {
		t.Fatalf("expected ours modification to be preserved in conflict body, got:\n%s", mergedStr)
	}

	stg, err := r.ReadStaging()
	if err != nil {
		t.Fatalf("ReadStaging: %v", err)
	}
	entry := stg.Entries["main.go"]
	if entry == nil {
		t.Fatalf("expected conflicted main.go in staging")
	}
	if !entry.Conflict {
		t.Fatalf("expected staging conflict flag for main.go")
	}
	if entry.OursBlobHash == "" {
		t.Fatalf("expected ours blob hash to be recorded")
	}
	if entry.TheirsBlobHash != "" {
		t.Fatalf("expected empty theirs blob hash for deleted side, got %q", entry.TheirsBlobHash)
	}
}

// TestMerge_CommitWithTwoParents verifies that a clean merge creates a
// commit with exactly two parent hashes.
func TestMerge_CommitWithTwoParents(t *testing.T) {
	r, dir := setupMergeRepo(t)

	// On main: add func C.
	oursContent := `package main

func A() { println("a") }

func C() { println("c") }
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(oursContent), 0o644); err != nil {
		t.Fatalf("write main.go (ours): %v", err)
	}
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add main.go (ours): %v", err)
	}
	mainCommit, err := r.Commit("add func C on main", "test-author")
	if err != nil {
		t.Fatalf("Commit (ours): %v", err)
	}

	// Switch to feature branch.
	if err := r.Checkout("feature"); err != nil {
		t.Fatalf("Checkout(feature): %v", err)
	}

	// On feature: add func B.
	theirsContent := `package main

func A() { println("a") }

func B() { println("b") }
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(theirsContent), 0o644); err != nil {
		t.Fatalf("write main.go (theirs): %v", err)
	}
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add main.go (theirs): %v", err)
	}
	featureCommit, err := r.Commit("add func B on feature", "test-author")
	if err != nil {
		t.Fatalf("Commit (theirs): %v", err)
	}

	// Switch back to main.
	if err := r.Checkout("main"); err != nil {
		t.Fatalf("Checkout(main): %v", err)
	}

	// Merge feature into main.
	report, err := r.Merge("feature")
	if err != nil {
		t.Fatalf("Merge(feature): %v", err)
	}

	if report.HasConflicts {
		t.Fatalf("expected clean merge, got conflicts")
	}
	if report.MergeCommit == "" {
		t.Fatal("expected merge commit hash, got empty")
	}

	// Read the merge commit and verify two parents.
	commit, err := r.Store.ReadCommit(report.MergeCommit)
	if err != nil {
		t.Fatalf("ReadCommit(%s): %v", report.MergeCommit, err)
	}
	if len(commit.Parents) != 2 {
		t.Fatalf("merge commit parents = %d, want 2", len(commit.Parents))
	}
	if commit.Parents[0] != mainCommit {
		t.Errorf("parent[0] = %q, want %q (main)", commit.Parents[0], mainCommit)
	}
	if commit.Parents[1] != featureCommit {
		t.Errorf("parent[1] = %q, want %q (feature)", commit.Parents[1], featureCommit)
	}

	// Verify commit message.
	if !strings.Contains(commit.Message, "Merge branch 'feature'") {
		t.Errorf("commit message = %q, want to contain %q", commit.Message, "Merge branch 'feature'")
	}
}

// TestFindMergeBase_LinearHistory verifies that FindMergeBase finds the
// correct common ancestor in a linear commit chain A -> B -> C.
// The merge base of B and C should be B.
func TestFindMergeBase_LinearHistory(t *testing.T) {
	r, dir := setupMergeRepo(t)

	// At this point we have commit A (initial).
	commitA, err := r.ResolveRef("HEAD")
	if err != nil {
		t.Fatalf("ResolveRef(HEAD): %v", err)
	}

	// Create commit B.
	contentB := `package main

func A() { println("a") }

func B() { println("b") }
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(contentB), 0o644); err != nil {
		t.Fatalf("write main.go (B): %v", err)
	}
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add (B): %v", err)
	}
	commitB, err := r.Commit("commit B", "test-author")
	if err != nil {
		t.Fatalf("Commit B: %v", err)
	}

	// Create commit C.
	contentC := `package main

func A() { println("a") }

func B() { println("b") }

func C() { println("c") }
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(contentC), 0o644); err != nil {
		t.Fatalf("write main.go (C): %v", err)
	}
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add (C): %v", err)
	}
	commitC, err := r.Commit("commit C", "test-author")
	if err != nil {
		t.Fatalf("Commit C: %v", err)
	}

	// Merge base of B and C should be B (since C is a child of B).
	base, err := r.FindMergeBase(commitB, commitC)
	if err != nil {
		t.Fatalf("FindMergeBase(B, C): %v", err)
	}
	if base != commitB {
		t.Errorf("FindMergeBase(B, C) = %q, want %q (commitB)", base, commitB)
	}

	// Merge base of A and C should be A.
	base, err = r.FindMergeBase(commitA, commitC)
	if err != nil {
		t.Fatalf("FindMergeBase(A, C): %v", err)
	}
	if base != commitA {
		t.Errorf("FindMergeBase(A, C) = %q, want %q (commitA)", base, commitA)
	}

	// Merge base of a commit with itself should be itself.
	base, err = r.FindMergeBase(commitB, commitB)
	if err != nil {
		t.Fatalf("FindMergeBase(B, B): %v", err)
	}
	if base != commitB {
		t.Errorf("FindMergeBase(B, B) = %q, want %q (commitB)", base, commitB)
	}
}
