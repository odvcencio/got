package repo

import (
	"testing"
	"time"

	"github.com/odvcencio/graft/pkg/object"
)

func TestInvalidate_ClearsMergeBasesPreservesCommitsAndGenerations(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	treeHash, err := r.Store.WriteTree(&object.TreeObj{})
	if err != nil {
		t.Fatalf("WriteTree: %v", err)
	}

	commitA, err := r.Store.WriteCommit(&object.CommitObj{
		TreeHash:  treeHash,
		Author:    "test-author",
		Timestamp: time.Now().Unix(),
		Message:   "commit A",
	})
	if err != nil {
		t.Fatalf("WriteCommit(A): %v", err)
	}

	commitB, err := r.Store.WriteCommit(&object.CommitObj{
		TreeHash:  treeHash,
		Parents:   []object.Hash{commitA},
		Author:    "test-author",
		Timestamp: time.Now().Unix(),
		Message:   "commit B",
	})
	if err != nil {
		t.Fatalf("WriteCommit(B): %v", err)
	}

	// Populate all three caches: commits, generations, merge bases.
	state := r.getMergeTraversalState()

	_, err = state.readCommit(r, commitA)
	if err != nil {
		t.Fatalf("readCommit(A): %v", err)
	}
	_, err = state.readCommit(r, commitB)
	if err != nil {
		t.Fatalf("readCommit(B): %v", err)
	}

	_, err = state.generation(r, commitA)
	if err != nil {
		t.Fatalf("generation(A): %v", err)
	}
	_, err = state.generation(r, commitB)
	if err != nil {
		t.Fatalf("generation(B): %v", err)
	}

	// Populate merge base cache.
	_, err = r.FindMergeBase(commitA, commitB)
	if err != nil {
		t.Fatalf("FindMergeBase: %v", err)
	}

	if state.mergeBaseCacheSize() == 0 {
		t.Fatal("expected merge base cache to be non-empty before invalidation")
	}
	if state.generationCacheSize() == 0 {
		t.Fatal("expected generation cache to be non-empty before invalidation")
	}

	// Record sizes before invalidation.
	genSizeBefore := state.generationCacheSize()

	state.mu.RLock()
	commitSizeBefore := len(state.commits)
	state.mu.RUnlock()

	// Invalidate.
	state.invalidate()

	// Merge bases should be cleared.
	if got := state.mergeBaseCacheSize(); got != 0 {
		t.Fatalf("merge base cache size after invalidate = %d, want 0", got)
	}

	// Commits cache should be preserved.
	state.mu.RLock()
	commitSizeAfter := len(state.commits)
	state.mu.RUnlock()
	if commitSizeAfter != commitSizeBefore {
		t.Fatalf("commit cache size changed from %d to %d after invalidate", commitSizeBefore, commitSizeAfter)
	}

	// Generations cache should be preserved.
	if got := state.generationCacheSize(); got != genSizeBefore {
		t.Fatalf("generation cache size changed from %d to %d after invalidate", genSizeBefore, got)
	}
}

func TestInvalidateMergeBaseCache_NilStateIsNoOp(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Before any merge base operation, traversalState is nil.
	// InvalidateMergeBaseCache should not panic.
	r.InvalidateMergeBaseCache()
}

func TestCommit_InvalidatesMergeBaseCache(t *testing.T) {
	r, dir := setupMergeRepo(t)

	// Resolve the initial commit (the base).
	commitA, err := r.ResolveRef("HEAD")
	if err != nil {
		t.Fatalf("ResolveRef(HEAD): %v", err)
	}

	// Make a commit on main.
	mainTip := commitMainGo(t, r, dir, `package main

func A() { println("a") }

func MainOnly() { println("main") }
`, "main side change")

	// Switch to feature and make a commit.
	if err := r.Checkout("feature"); err != nil {
		t.Fatalf("Checkout(feature): %v", err)
	}
	featureTip := commitMainGo(t, r, dir, `package main

func A() { println("a") }

func FeatureOnly() { println("feature") }
`, "feature side change")

	// Compute merge base — should be commitA.
	base, err := r.FindMergeBase(mainTip, featureTip)
	if err != nil {
		t.Fatalf("FindMergeBase(main, feature): %v", err)
	}
	if base != commitA {
		t.Fatalf("FindMergeBase = %q, want %q", base, commitA)
	}

	state := r.getMergeTraversalState()
	if state.mergeBaseCacheSize() == 0 {
		t.Fatal("expected cache to be populated after FindMergeBase")
	}

	// Make another commit on feature — this should invalidate the cache.
	_ = commitMainGo(t, r, dir, `package main

func A() { println("a") }

func FeatureOnly() { println("feature-v2") }
`, "feature second change")

	// Cache should be empty after commit.
	if got := state.mergeBaseCacheSize(); got != 0 {
		t.Fatalf("merge base cache size after Commit = %d, want 0", got)
	}
}

func TestUpdateRefCAS_InvalidatesMergeBaseCache(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	treeHash, err := r.Store.WriteTree(&object.TreeObj{})
	if err != nil {
		t.Fatalf("WriteTree: %v", err)
	}

	commitA, err := r.Store.WriteCommit(&object.CommitObj{
		TreeHash:  treeHash,
		Author:    "test-author",
		Timestamp: time.Now().Unix(),
		Message:   "commit A",
	})
	if err != nil {
		t.Fatalf("WriteCommit(A): %v", err)
	}

	commitB, err := r.Store.WriteCommit(&object.CommitObj{
		TreeHash:  treeHash,
		Parents:   []object.Hash{commitA},
		Author:    "test-author",
		Timestamp: time.Now().Unix(),
		Message:   "commit B",
	})
	if err != nil {
		t.Fatalf("WriteCommit(B): %v", err)
	}

	// Populate the merge base cache.
	_, err = r.FindMergeBase(commitA, commitB)
	if err != nil {
		t.Fatalf("FindMergeBase: %v", err)
	}

	state := r.getMergeTraversalState()
	if state.mergeBaseCacheSize() == 0 {
		t.Fatal("expected cache to be populated")
	}

	// Set up a ref so we can move it.
	refName := "refs/heads/test-branch"
	if err := r.UpdateRef(refName, commitA); err != nil {
		t.Fatalf("UpdateRef(create): %v", err)
	}

	// Move the ref — this should invalidate the cache.
	if err := r.UpdateRefCAS(refName, commitB, commitA); err != nil {
		t.Fatalf("UpdateRefCAS(move): %v", err)
	}

	if got := state.mergeBaseCacheSize(); got != 0 {
		t.Fatalf("merge base cache size after UpdateRefCAS = %d, want 0", got)
	}
}

func TestInvalidation_MergeBaseRecomputedCorrectly(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	treeHash, err := r.Store.WriteTree(&object.TreeObj{})
	if err != nil {
		t.Fatalf("WriteTree: %v", err)
	}

	// Build a simple linear chain: A <- B <- C
	commitA, err := r.Store.WriteCommit(&object.CommitObj{
		TreeHash:  treeHash,
		Author:    "test-author",
		Timestamp: time.Now().Unix(),
		Message:   "A",
	})
	if err != nil {
		t.Fatalf("WriteCommit(A): %v", err)
	}

	commitB, err := r.Store.WriteCommit(&object.CommitObj{
		TreeHash:  treeHash,
		Parents:   []object.Hash{commitA},
		Author:    "test-author",
		Timestamp: time.Now().Unix(),
		Message:   "B",
	})
	if err != nil {
		t.Fatalf("WriteCommit(B): %v", err)
	}

	// Branch off A: A <- D (separate branch)
	commitD, err := r.Store.WriteCommit(&object.CommitObj{
		TreeHash:  treeHash,
		Parents:   []object.Hash{commitA},
		Author:    "test-author",
		Timestamp: time.Now().Unix(),
		Message:   "D",
	})
	if err != nil {
		t.Fatalf("WriteCommit(D): %v", err)
	}

	// Merge base of B and D should be A.
	base1, err := r.FindMergeBase(commitB, commitD)
	if err != nil {
		t.Fatalf("FindMergeBase(B, D): %v", err)
	}
	if base1 != commitA {
		t.Fatalf("FindMergeBase(B, D) = %q, want %q", base1, commitA)
	}

	state := r.getMergeTraversalState()
	if state.mergeBaseCacheSize() == 0 {
		t.Fatal("expected cache populated after FindMergeBase")
	}

	// Now add a new commit C on top of B.
	commitC, err := r.Store.WriteCommit(&object.CommitObj{
		TreeHash:  treeHash,
		Parents:   []object.Hash{commitB},
		Author:    "test-author",
		Timestamp: time.Now().Unix(),
		Message:   "C",
	})
	if err != nil {
		t.Fatalf("WriteCommit(C): %v", err)
	}

	// Manually invalidate (simulating what Commit/UpdateRef would do).
	r.InvalidateMergeBaseCache()

	if got := state.mergeBaseCacheSize(); got != 0 {
		t.Fatalf("cache size after invalidation = %d, want 0", got)
	}

	// Merge base of C and D should still be A (recomputed, not stale).
	base2, err := r.FindMergeBase(commitC, commitD)
	if err != nil {
		t.Fatalf("FindMergeBase(C, D): %v", err)
	}
	if base2 != commitA {
		t.Fatalf("FindMergeBase(C, D) = %q, want %q", base2, commitA)
	}

	// Verify the cache was re-populated.
	if state.mergeBaseCacheSize() == 0 {
		t.Fatal("expected cache to be re-populated after recomputation")
	}
}
