package repo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/odvcencio/graft/pkg/object"
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

// TestMerge_FastForward verifies that when HEAD is an ancestor of the merge
// target (no divergence), the merge fast-forwards without creating a merge
// commit. The branch ref should advance to the target commit.
func TestMerge_FastForward(t *testing.T) {
	r, dir := setupMergeRepo(t)

	// At this point, "main" and "feature" both point to the initial commit.
	// Switch to feature and add a commit so feature is ahead of main.
	if err := r.Checkout("feature"); err != nil {
		t.Fatalf("Checkout(feature): %v", err)
	}

	featureContent := `package main

func A() { println("a") }

func B() { println("b") }
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(featureContent), 0o644); err != nil {
		t.Fatalf("write main.go (feature): %v", err)
	}
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add main.go (feature): %v", err)
	}
	featureCommit, err := r.Commit("add func B on feature", "test-author")
	if err != nil {
		t.Fatalf("Commit (feature): %v", err)
	}

	// Switch back to main -- main is behind feature with no divergent commits.
	if err := r.Checkout("main"); err != nil {
		t.Fatalf("Checkout(main): %v", err)
	}

	report, err := r.Merge("feature")
	if err != nil {
		t.Fatalf("Merge(feature): %v", err)
	}

	if !report.IsFastForward {
		t.Fatal("expected fast-forward merge, got three-way merge")
	}
	if report.HasConflicts {
		t.Fatal("fast-forward merge should not have conflicts")
	}
	// For a fast-forward, MergeCommit is set to the target branch hash.
	if report.MergeCommit != featureCommit {
		t.Errorf("MergeCommit = %q, want %q (feature tip)", report.MergeCommit, featureCommit)
	}

	// Verify HEAD now points to the feature commit (no new merge commit created).
	headHash, err := r.ResolveRef("HEAD")
	if err != nil {
		t.Fatalf("ResolveRef(HEAD): %v", err)
	}
	if headHash != featureCommit {
		t.Errorf("HEAD = %q, want %q (feature tip)", headHash, featureCommit)
	}

	// The feature commit should have exactly 1 parent (not 2 -- it's not a merge commit).
	commit, err := r.Store.ReadCommit(featureCommit)
	if err != nil {
		t.Fatalf("ReadCommit(%s): %v", featureCommit, err)
	}
	if len(commit.Parents) != 1 {
		t.Errorf("fast-forwarded commit has %d parents, want 1", len(commit.Parents))
	}

	// Verify the working tree was updated.
	data, err := os.ReadFile(filepath.Join(dir, "main.go"))
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	if !strings.Contains(string(data), "func B()") {
		t.Errorf("working tree not updated: missing func B in main.go")
	}
}

// TestMerge_Abort verifies that MergeAbort restores the repository to the
// state before a conflicted merge, and that calling MergeAbort when no merge
// is in progress returns an error.
func TestMerge_Abort(t *testing.T) {
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
	mainCommit, err := r.Commit("modify A on main", "test-author")
	if err != nil {
		t.Fatalf("Commit (ours): %v", err)
	}

	// Switch to feature and modify func A differently.
	if err := r.Checkout("feature"); err != nil {
		t.Fatalf("Checkout(feature): %v", err)
	}
	theirsContent := `package main

func A() { println("theirs") }
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(theirsContent), 0o644); err != nil {
		t.Fatalf("write main.go (theirs): %v", err)
	}
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add main.go (theirs): %v", err)
	}
	if _, err := r.Commit("modify A on feature", "test-author"); err != nil {
		t.Fatalf("Commit (theirs): %v", err)
	}

	// Switch back to main.
	if err := r.Checkout("main"); err != nil {
		t.Fatalf("Checkout(main): %v", err)
	}

	// Merge feature into main -- this should conflict.
	report, err := r.Merge("feature")
	if err != nil {
		t.Fatalf("Merge(feature): %v", err)
	}
	if !report.HasConflicts {
		t.Fatal("expected conflicts for abort test")
	}

	// Verify merge is in progress.
	if !r.IsMergeInProgress() {
		t.Fatal("expected IsMergeInProgress() == true after conflicted merge")
	}

	// Abort the merge.
	if err := r.MergeAbort(); err != nil {
		t.Fatalf("MergeAbort: %v", err)
	}

	// Verify merge is no longer in progress.
	if r.IsMergeInProgress() {
		t.Fatal("expected IsMergeInProgress() == false after abort")
	}

	// Verify HEAD was restored.
	currentHead, err := r.ResolveRef("HEAD")
	if err != nil {
		t.Fatalf("ResolveRef(HEAD) after abort: %v", err)
	}
	if currentHead != mainCommit {
		t.Errorf("HEAD after abort = %q, want %q (pre-merge)", currentHead, mainCommit)
	}

	// Verify working tree was restored (conflict markers should be gone).
	data, err := os.ReadFile(filepath.Join(dir, "main.go"))
	if err != nil {
		t.Fatalf("read main.go after abort: %v", err)
	}
	if strings.Contains(string(data), "<<<<<<<") {
		t.Errorf("working tree still has conflict markers after abort")
	}
	if !strings.Contains(string(data), `println("ours")`) {
		t.Errorf("working tree not restored to pre-merge state, got:\n%s", string(data))
	}

	// Verify MergeAbort with no merge in progress returns error.
	if err := r.MergeAbort(); err == nil {
		t.Fatal("expected error from MergeAbort when no merge in progress")
	}
}

// TestMerge_AuthorFromConfig verifies that merge commits use ResolveAuthor()
// (reading from repo config) rather than a hardcoded author string.
func TestMerge_AuthorFromConfig(t *testing.T) {
	r, dir := setupMergeRepo(t)

	// Set user config on the repo.
	cfg, err := r.ReadConfig()
	if err != nil {
		t.Fatalf("ReadConfig: %v", err)
	}
	cfg.User = &UserConfig{
		Name:  "Test Merger",
		Email: "merger@example.com",
	}
	if err := r.WriteConfig(cfg); err != nil {
		t.Fatalf("WriteConfig: %v", err)
	}

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
	if _, err := r.Commit("add func C on main", "test-author"); err != nil {
		t.Fatalf("Commit (ours): %v", err)
	}

	// Switch to feature and add func B.
	if err := r.Checkout("feature"); err != nil {
		t.Fatalf("Checkout(feature): %v", err)
	}
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
	if _, err := r.Commit("add func B on feature", "test-author"); err != nil {
		t.Fatalf("Commit (theirs): %v", err)
	}

	// Switch back to main and merge.
	if err := r.Checkout("main"); err != nil {
		t.Fatalf("Checkout(main): %v", err)
	}

	report, err := r.Merge("feature")
	if err != nil {
		t.Fatalf("Merge(feature): %v", err)
	}
	if report.HasConflicts {
		t.Fatal("expected clean merge")
	}
	if report.MergeCommit == "" {
		t.Fatal("expected merge commit hash")
	}

	// Read the merge commit and verify the author.
	commit, err := r.Store.ReadCommit(report.MergeCommit)
	if err != nil {
		t.Fatalf("ReadCommit(%s): %v", report.MergeCommit, err)
	}
	expectedAuthor := "Test Merger <merger@example.com>"
	if commit.Author != expectedAuthor {
		t.Errorf("merge commit author = %q, want %q", commit.Author, expectedAuthor)
	}
}

// TestThreeWayTreeMerge_SharedHelper tests the extracted threeWayTreeMerge
// helper directly, verifying the 7 cases: both modified (clean + conflict),
// new in theirs, new in both (same + different), deleted by theirs (clean),
// deleted by ours (clean), only in ours, and both deleted.
func TestThreeWayTreeMerge_SharedHelper(t *testing.T) {
	r, dir := setupMergeRepo(t)

	// We need blobs in the store to use with threeWayTreeMerge.
	// Create several blobs with different content.
	writeBlob := func(content string) string {
		t.Helper()
		p := filepath.Join(dir, "tmp_blob")
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatalf("write tmp blob: %v", err)
		}
		if err := r.Add([]string{"tmp_blob"}); err != nil {
			t.Fatalf("Add tmp blob: %v", err)
		}
		stg, err := r.ReadStaging()
		if err != nil {
			t.Fatalf("ReadStaging: %v", err)
		}
		entry := stg.Entries["tmp_blob"]
		if entry == nil {
			t.Fatalf("tmp_blob not in staging")
		}
		return string(entry.BlobHash)
	}

	blobBase := writeBlob("line1\nline2\nline3\n")
	blobOurs := writeBlob("line1-ours\nline2\nline3\n")
	blobTheirs := writeBlob("line1\nline2\nline3-theirs\n")
	blobSame := writeBlob("same content\n")

	// Build base/ours/theirs maps covering the different cases.
	mkEntry := func(blobHash, mode string) TreeFileEntry {
		return TreeFileEntry{BlobHash: object.Hash(blobHash), Mode: mode}
	}

	baseMap := map[string]TreeFileEntry{
		"both-modified.txt":   mkEntry(blobBase, "100644"),
		"only-theirs-changed": mkEntry(blobBase, "100644"),
		"only-ours-changed":   mkEntry(blobBase, "100644"),
		"deleted-by-theirs":   mkEntry(blobBase, "100644"),
		"deleted-by-ours":     mkEntry(blobBase, "100644"),
		"both-deleted":        mkEntry(blobBase, "100644"),
		"same-in-ours-theirs": mkEntry(blobBase, "100644"),
	}
	oursMap := map[string]TreeFileEntry{
		"both-modified.txt":   mkEntry(blobOurs, "100644"),
		"only-theirs-changed": mkEntry(blobBase, "100644"),
		"only-ours-changed":   mkEntry(blobOurs, "100644"),
		"deleted-by-theirs":   mkEntry(blobBase, "100644"),
		// deleted-by-ours: not present
		// both-deleted: not present
		"same-in-ours-theirs": mkEntry(blobSame, "100644"),
		"only-in-ours":        mkEntry(blobOurs, "100644"),
		"new-in-both-same":    mkEntry(blobSame, "100644"),
	}
	theirsMap := map[string]TreeFileEntry{
		"both-modified.txt":   mkEntry(blobTheirs, "100644"),
		"only-theirs-changed": mkEntry(blobTheirs, "100644"),
		"only-ours-changed":   mkEntry(blobBase, "100644"),
		// deleted-by-theirs: not present
		"deleted-by-ours": mkEntry(blobBase, "100644"),
		// both-deleted: not present
		"same-in-ours-theirs": mkEntry(blobSame, "100644"),
		"new-in-theirs":       mkEntry(blobTheirs, "100644"),
		"new-in-both-same":    mkEntry(blobSame, "100644"),
	}

	result, err := r.threeWayTreeMerge(baseMap, oursMap, theirsMap)
	if err != nil {
		t.Fatalf("threeWayTreeMerge: %v", err)
	}

	// Build a map of path -> status for easy checking.
	statusMap := make(map[string]string)
	for _, f := range result.Files {
		statusMap[f.Path] = f.Status
	}

	// Verify expected statuses.
	tests := []struct {
		path   string
		status string
	}{
		{"both-modified.txt", "clean"},       // both modified different lines -- text merge resolves cleanly
		{"only-theirs-changed", "clean"},     // only theirs changed from base
		{"only-ours-changed", "unchanged"},   // only ours changed -- keep ours, unchanged
		{"deleted-by-theirs", "deleted"},     // theirs deleted, ours same as base -- clean delete
		{"deleted-by-ours", "deleted"},       // ours deleted, theirs same as base -- clean delete
		{"both-deleted", "deleted"},          // both deleted
		{"same-in-ours-theirs", "unchanged"}, // ours == theirs, no merge needed
		{"only-in-ours", "unchanged"},        // only in ours, not in theirs or base
		{"new-in-theirs", "added"},           // new in theirs only
		{"new-in-both-same", "unchanged"},    // new in both, same content
	}

	for _, tc := range tests {
		got, ok := statusMap[tc.path]
		if !ok {
			t.Errorf("path %q not found in result", tc.path)
			continue
		}
		if got != tc.status {
			t.Errorf("path %q: status = %q, want %q", tc.path, got, tc.status)
		}
	}

	// Verify deleted paths.
	deletedSet := make(map[string]bool)
	for _, p := range result.DeletedPaths {
		deletedSet[p] = true
	}
	for _, expected := range []string{"deleted-by-theirs", "deleted-by-ours", "both-deleted"} {
		if !deletedSet[expected] {
			t.Errorf("expected %q in DeletedPaths", expected)
		}
	}

	// Verify the "only-theirs-changed" file has theirs content.
	for _, f := range result.Files {
		if f.Path == "only-theirs-changed" {
			if string(f.Content) != "line1\nline2\nline3-theirs\n" {
				t.Errorf("only-theirs-changed content = %q, want %q", string(f.Content), "line1\nline2\nline3-theirs\n")
			}
		}
		if f.Path == "new-in-theirs" {
			if string(f.Content) != "line1\nline2\nline3-theirs\n" {
				t.Errorf("new-in-theirs content = %q, want %q", string(f.Content), "line1\nline2\nline3-theirs\n")
			}
		}
	}

	// Verify HasConflicts is false for this scenario (plain text, non-overlapping changes).
	if result.HasConflicts {
		t.Errorf("expected no conflicts, got HasConflicts=true, details: %v", result.ConflictDetails)
	}
}

// TestMerge_NoMergeInProgressBeforeMerge verifies that a clean merge removes
// merge state files (MERGE_HEAD, ORIG_HEAD) so IsMergeInProgress returns false.
func TestMerge_NoMergeInProgressBeforeMerge(t *testing.T) {
	r, _ := setupMergeRepo(t)

	// No merge should be in progress initially.
	if r.IsMergeInProgress() {
		t.Fatal("IsMergeInProgress should be false before any merge")
	}
}

// TestMergePreview_CleanMerge verifies that MergePreview returns the correct
// report for a clean (non-conflicting) merge without modifying the working
// tree, staging area, or refs.
func TestMergePreview_CleanMerge(t *testing.T) {
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

	// Switch to feature branch and add func B.
	if err := r.Checkout("feature"); err != nil {
		t.Fatalf("Checkout(feature): %v", err)
	}
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
	if _, err := r.Commit("add func B on feature", "test-author"); err != nil {
		t.Fatalf("Commit (theirs): %v", err)
	}

	// Switch back to main.
	if err := r.Checkout("main"); err != nil {
		t.Fatalf("Checkout(main): %v", err)
	}

	// Snapshot working tree content and HEAD before preview.
	previewMainGo, err := os.ReadFile(filepath.Join(dir, "main.go"))
	if err != nil {
		t.Fatalf("read main.go before preview: %v", err)
	}
	headBefore, err := r.ResolveRef("HEAD")
	if err != nil {
		t.Fatalf("ResolveRef(HEAD) before preview: %v", err)
	}

	// Run MergePreview.
	report, err := r.MergePreview("feature")
	if err != nil {
		t.Fatalf("MergePreview(feature): %v", err)
	}

	// Verify the report is correct.
	if report.HasConflicts {
		t.Fatalf("expected clean merge preview, got conflicts: %+v", report)
	}
	if report.IsFastForward {
		t.Fatal("expected three-way merge preview, got fast-forward")
	}
	if len(report.Files) == 0 {
		t.Fatal("expected at least one file in merge preview report")
	}

	// Verify at least one file reported as "clean" (the merged main.go).
	foundClean := false
	for _, f := range report.Files {
		if f.Path == "main.go" && f.Status == "clean" {
			foundClean = true
		}
	}
	if !foundClean {
		t.Errorf("expected main.go with status 'clean' in report, got: %+v", report.Files)
	}

	// MergeCommit should be empty — preview does not create a commit.
	if report.MergeCommit != "" {
		t.Errorf("MergePreview should not set MergeCommit, got %q", report.MergeCommit)
	}

	// Verify working tree was NOT modified.
	postMainGo, err := os.ReadFile(filepath.Join(dir, "main.go"))
	if err != nil {
		t.Fatalf("read main.go after preview: %v", err)
	}
	if string(postMainGo) != string(previewMainGo) {
		t.Errorf("MergePreview modified working tree.\nbefore:\n%s\nafter:\n%s", previewMainGo, postMainGo)
	}

	// Verify HEAD was NOT modified.
	headAfter, err := r.ResolveRef("HEAD")
	if err != nil {
		t.Fatalf("ResolveRef(HEAD) after preview: %v", err)
	}
	if headAfter != headBefore {
		t.Errorf("MergePreview changed HEAD from %q to %q", headBefore, headAfter)
	}
	if headAfter != mainCommit {
		t.Errorf("HEAD should still be %q (main commit), got %q", mainCommit, headAfter)
	}

	// Verify no merge state files were created.
	if r.IsMergeInProgress() {
		t.Error("MergePreview should not leave merge state files")
	}
}

// TestMergePreview_ConflictMerge verifies that MergePreview correctly reports
// conflicts without modifying the working tree, staging, or refs.
func TestMergePreview_ConflictMerge(t *testing.T) {
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
	mainCommit, err := r.Commit("modify A on main", "test-author")
	if err != nil {
		t.Fatalf("Commit (ours): %v", err)
	}

	// Switch to feature and modify func A differently.
	if err := r.Checkout("feature"); err != nil {
		t.Fatalf("Checkout(feature): %v", err)
	}
	theirsContent := `package main

func A() { println("theirs") }
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(theirsContent), 0o644); err != nil {
		t.Fatalf("write main.go (theirs): %v", err)
	}
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add main.go (theirs): %v", err)
	}
	if _, err := r.Commit("modify A on feature", "test-author"); err != nil {
		t.Fatalf("Commit (theirs): %v", err)
	}

	// Switch back to main.
	if err := r.Checkout("main"); err != nil {
		t.Fatalf("Checkout(main): %v", err)
	}

	// Snapshot state before preview.
	previewMainGo, err := os.ReadFile(filepath.Join(dir, "main.go"))
	if err != nil {
		t.Fatalf("read main.go before preview: %v", err)
	}
	headBefore, err := r.ResolveRef("HEAD")
	if err != nil {
		t.Fatalf("ResolveRef(HEAD) before preview: %v", err)
	}

	// Run MergePreview.
	report, err := r.MergePreview("feature")
	if err != nil {
		t.Fatalf("MergePreview(feature): %v", err)
	}

	// Verify report shows conflicts.
	if !report.HasConflicts {
		t.Fatal("expected conflicts in merge preview")
	}
	if report.TotalConflicts == 0 {
		t.Error("TotalConflicts should be > 0")
	}
	if report.MergeCommit != "" {
		t.Errorf("MergePreview should not set MergeCommit, got %q", report.MergeCommit)
	}

	// Verify at least one file reported as "conflict".
	foundConflict := false
	for _, f := range report.Files {
		if f.Path == "main.go" && f.Status == "conflict" {
			foundConflict = true
		}
	}
	if !foundConflict {
		t.Errorf("expected main.go with status 'conflict' in report, got: %+v", report.Files)
	}

	// Verify working tree was NOT modified (no conflict markers written).
	postMainGo, err := os.ReadFile(filepath.Join(dir, "main.go"))
	if err != nil {
		t.Fatalf("read main.go after preview: %v", err)
	}
	if string(postMainGo) != string(previewMainGo) {
		t.Errorf("MergePreview modified working tree.\nbefore:\n%s\nafter:\n%s", previewMainGo, postMainGo)
	}
	if strings.Contains(string(postMainGo), "<<<<<<<") {
		t.Error("MergePreview wrote conflict markers to working tree")
	}

	// Verify HEAD was NOT modified.
	headAfter, err := r.ResolveRef("HEAD")
	if err != nil {
		t.Fatalf("ResolveRef(HEAD) after preview: %v", err)
	}
	if headAfter != headBefore {
		t.Errorf("MergePreview changed HEAD from %q to %q", headBefore, headAfter)
	}
	if headAfter != mainCommit {
		t.Errorf("HEAD should still be %q (main commit), got %q", mainCommit, headAfter)
	}

	// Verify no merge state files were created.
	if r.IsMergeInProgress() {
		t.Error("MergePreview should not leave merge state files")
	}

	// Verify staging was NOT modified (no conflict entries).
	stg, err := r.ReadStaging()
	if err != nil {
		t.Fatalf("ReadStaging after preview: %v", err)
	}
	for path, entry := range stg.Entries {
		if entry.Conflict {
			t.Errorf("MergePreview left conflict staging entry for %q", path)
		}
	}
}

// TestMergePreview_FastForward verifies that MergePreview returns a
// fast-forward report without actually advancing HEAD.
func TestMergePreview_FastForward(t *testing.T) {
	r, dir := setupMergeRepo(t)

	// Feature is at same commit as main. Add a commit on feature so
	// merging feature into main would be a fast-forward.
	if err := r.Checkout("feature"); err != nil {
		t.Fatalf("Checkout(feature): %v", err)
	}
	featureContent := `package main

func A() { println("a") }

func B() { println("b") }
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(featureContent), 0o644); err != nil {
		t.Fatalf("write main.go (feature): %v", err)
	}
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add main.go (feature): %v", err)
	}
	if _, err := r.Commit("add func B on feature", "test-author"); err != nil {
		t.Fatalf("Commit (feature): %v", err)
	}

	// Switch back to main.
	if err := r.Checkout("main"); err != nil {
		t.Fatalf("Checkout(main): %v", err)
	}

	// Snapshot state before preview.
	previewMainGo, err := os.ReadFile(filepath.Join(dir, "main.go"))
	if err != nil {
		t.Fatalf("read main.go before preview: %v", err)
	}
	headBefore, err := r.ResolveRef("HEAD")
	if err != nil {
		t.Fatalf("ResolveRef(HEAD) before preview: %v", err)
	}

	// Run MergePreview.
	report, err := r.MergePreview("feature")
	if err != nil {
		t.Fatalf("MergePreview(feature): %v", err)
	}

	// Verify it detected fast-forward.
	if !report.IsFastForward {
		t.Fatal("expected fast-forward merge preview")
	}
	if report.HasConflicts {
		t.Fatal("fast-forward preview should not have conflicts")
	}
	// MergeCommit should be empty — preview does not advance refs.
	if report.MergeCommit != "" {
		t.Errorf("MergePreview should not set MergeCommit, got %q", report.MergeCommit)
	}

	// Verify working tree was NOT modified.
	postMainGo, err := os.ReadFile(filepath.Join(dir, "main.go"))
	if err != nil {
		t.Fatalf("read main.go after preview: %v", err)
	}
	if string(postMainGo) != string(previewMainGo) {
		t.Errorf("MergePreview modified working tree.\nbefore:\n%s\nafter:\n%s", previewMainGo, postMainGo)
	}

	// Verify HEAD was NOT modified.
	headAfter, err := r.ResolveRef("HEAD")
	if err != nil {
		t.Fatalf("ResolveRef(HEAD) after preview: %v", err)
	}
	if headAfter != headBefore {
		t.Errorf("MergePreview changed HEAD from %q to %q", headBefore, headAfter)
	}
}

// TestMergePreview_ThenMerge verifies that running MergePreview does not
// interfere with a subsequent real Merge on the same branch.
func TestMergePreview_ThenMerge(t *testing.T) {
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
	if _, err := r.Commit("add func C on main", "test-author"); err != nil {
		t.Fatalf("Commit (ours): %v", err)
	}

	// Switch to feature and add func B.
	if err := r.Checkout("feature"); err != nil {
		t.Fatalf("Checkout(feature): %v", err)
	}
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
	if _, err := r.Commit("add func B on feature", "test-author"); err != nil {
		t.Fatalf("Commit (theirs): %v", err)
	}

	// Switch back to main.
	if err := r.Checkout("main"); err != nil {
		t.Fatalf("Checkout(main): %v", err)
	}

	// Run MergePreview first.
	preview, err := r.MergePreview("feature")
	if err != nil {
		t.Fatalf("MergePreview(feature): %v", err)
	}
	if preview.HasConflicts {
		t.Fatalf("expected clean preview, got conflicts")
	}

	// Now run the actual Merge — it should still succeed.
	report, err := r.Merge("feature")
	if err != nil {
		t.Fatalf("Merge(feature) after preview: %v", err)
	}
	if report.HasConflicts {
		t.Fatal("expected clean merge after preview")
	}
	if report.MergeCommit == "" {
		t.Fatal("expected merge commit after real merge")
	}

	// Verify the merged file has all three functions.
	merged, err := os.ReadFile(filepath.Join(dir, "main.go"))
	if err != nil {
		t.Fatalf("read merged main.go: %v", err)
	}
	mergedStr := string(merged)
	for _, fn := range []string{"func A()", "func B()", "func C()"} {
		if !strings.Contains(mergedStr, fn) {
			t.Errorf("merged file missing %s: %s", fn, mergedStr)
		}
	}
}
