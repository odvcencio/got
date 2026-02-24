package repo

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/odvcencio/got/pkg/object"
)

func commitMainGo(t *testing.T, r *Repo, dir, content, message string) object.Hash {
	t.Helper()

	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(content), 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add main.go: %v", err)
	}
	h, err := r.Commit(message, "test-author")
	if err != nil {
		t.Fatalf("Commit %q: %v", message, err)
	}
	return h
}

func TestMergeBaseGenerationNumbersFollowAncestry(t *testing.T) {
	r, dir := setupMergeRepo(t)

	commitA, err := r.ResolveRef("HEAD")
	if err != nil {
		t.Fatalf("ResolveRef(HEAD): %v", err)
	}

	commitB := commitMainGo(t, r, dir, `package main

func A() { println("a") }

func B() { println("b-main") }
`, "main adds B")

	if err := r.Checkout("feature"); err != nil {
		t.Fatalf("Checkout(feature): %v", err)
	}
	commitC := commitMainGo(t, r, dir, `package main

func A() { println("a") }

func C() { println("c-feature") }
`, "feature adds C")

	if err := r.Checkout("main"); err != nil {
		t.Fatalf("Checkout(main): %v", err)
	}
	report, err := r.Merge("feature")
	if err != nil {
		t.Fatalf("Merge(feature): %v", err)
	}
	if report.HasConflicts {
		t.Fatalf("expected clean merge, got conflicts")
	}
	if report.MergeCommit == "" {
		t.Fatalf("expected merge commit hash")
	}
	commitM := report.MergeCommit

	state := r.getMergeTraversalState()

	genA, err := state.generation(r, commitA)
	if err != nil {
		t.Fatalf("generation(A): %v", err)
	}
	genB, err := state.generation(r, commitB)
	if err != nil {
		t.Fatalf("generation(B): %v", err)
	}
	genC, err := state.generation(r, commitC)
	if err != nil {
		t.Fatalf("generation(C): %v", err)
	}
	genM, err := state.generation(r, commitM)
	if err != nil {
		t.Fatalf("generation(M): %v", err)
	}

	if genA == 0 {
		t.Fatalf("generation(A) should be >= 1, got 0")
	}
	if genB <= genA {
		t.Fatalf("generation(B) = %d, want > generation(A) = %d", genB, genA)
	}
	if genC <= genA {
		t.Fatalf("generation(C) = %d, want > generation(A) = %d", genC, genA)
	}
	if genM <= genB || genM <= genC {
		t.Fatalf("generation(M) = %d, want > max(generation(B)=%d, generation(C)=%d)", genM, genB, genC)
	}

	if state.generationCacheSize() < 4 {
		t.Fatalf("expected generation cache to contain at least 4 commits, got %d", state.generationCacheSize())
	}
}

func TestFindMergeBase_UsesCanonicalPairCache(t *testing.T) {
	r, dir := setupMergeRepo(t)

	commitA, err := r.ResolveRef("HEAD")
	if err != nil {
		t.Fatalf("ResolveRef(HEAD): %v", err)
	}

	mainTip := commitMainGo(t, r, dir, `package main

func A() { println("a") }

func MainOnly() { println("main") }
`, "main only change")

	if err := r.Checkout("feature"); err != nil {
		t.Fatalf("Checkout(feature): %v", err)
	}
	featureTip := commitMainGo(t, r, dir, `package main

func A() { println("a") }

func FeatureOnly() { println("feature") }
`, "feature only change")

	state := r.getMergeTraversalState()
	if got := state.mergeBaseCacheSize(); got != 0 {
		t.Fatalf("merge-base cache size before query = %d, want 0", got)
	}

	base1, err := r.FindMergeBase(mainTip, featureTip)
	if err != nil {
		t.Fatalf("FindMergeBase(main, feature): %v", err)
	}
	if base1 != commitA {
		t.Fatalf("FindMergeBase(main, feature) = %q, want %q", base1, commitA)
	}
	if got := state.mergeBaseCacheSize(); got != 1 {
		t.Fatalf("merge-base cache size after first query = %d, want 1", got)
	}

	base2, err := r.FindMergeBase(featureTip, mainTip)
	if err != nil {
		t.Fatalf("FindMergeBase(feature, main): %v", err)
	}
	if base2 != base1 {
		t.Fatalf("symmetric query returned %q, want %q", base2, base1)
	}
	if got := state.mergeBaseCacheSize(); got != 1 {
		t.Fatalf("merge-base cache size after symmetric query = %d, want 1", got)
	}
}

func TestFindMergeBase_CachesNoCommonAncestor(t *testing.T) {
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
		Message:   "orphan A",
	})
	if err != nil {
		t.Fatalf("WriteCommit(orphan A): %v", err)
	}
	commitB, err := r.Store.WriteCommit(&object.CommitObj{
		TreeHash:  treeHash,
		Author:    "test-author",
		Timestamp: time.Now().Unix(),
		Message:   "orphan B",
	})
	if err != nil {
		t.Fatalf("WriteCommit(orphan B): %v", err)
	}

	state := r.getMergeTraversalState()

	base1, err := r.FindMergeBase(commitA, commitB)
	if err != nil {
		t.Fatalf("FindMergeBase(orphanA, orphanB): %v", err)
	}
	if base1 != "" {
		t.Fatalf("FindMergeBase(orphanA, orphanB) = %q, want empty", base1)
	}
	if got := state.mergeBaseCacheSize(); got != 1 {
		t.Fatalf("merge-base cache size after first no-base query = %d, want 1", got)
	}

	base2, err := r.FindMergeBase(commitB, commitA)
	if err != nil {
		t.Fatalf("FindMergeBase(orphanB, orphanA): %v", err)
	}
	if base2 != "" {
		t.Fatalf("FindMergeBase(orphanB, orphanA) = %q, want empty", base2)
	}
	if got := state.mergeBaseCacheSize(); got != 1 {
		t.Fatalf("merge-base cache size after symmetric no-base query = %d, want 1", got)
	}

	cached, ok := state.loadMergeBase(commitA, commitB)
	if !ok {
		t.Fatalf("expected no-base result to be cached")
	}
	if cached.found {
		t.Fatalf("cached no-base entry incorrectly marked found=true")
	}
}

func TestFindMergeBase_MergeParentFastPath(t *testing.T) {
	r, dir := setupMergeRepo(t)

	_ = commitMainGo(t, r, dir, `package main

func A() { println("a") }

func MainOnly() { println("main") }
`, "main side change")

	if err := r.Checkout("feature"); err != nil {
		t.Fatalf("Checkout(feature): %v", err)
	}
	featureTip := commitMainGo(t, r, dir, `package main

func A() { println("a") }

func FeatureOnly() { println("feature") }
`, "feature side change")

	if err := r.Checkout("main"); err != nil {
		t.Fatalf("Checkout(main): %v", err)
	}
	report, err := r.Merge("feature")
	if err != nil {
		t.Fatalf("Merge(feature): %v", err)
	}
	if report.HasConflicts {
		t.Fatalf("expected clean merge, got conflicts")
	}
	if report.MergeCommit == "" {
		t.Fatalf("expected merge commit hash")
	}

	base, err := r.FindMergeBase(report.MergeCommit, featureTip)
	if err != nil {
		t.Fatalf("FindMergeBase(merge, featureTip): %v", err)
	}
	if base != featureTip {
		t.Fatalf("FindMergeBase(merge, featureTip) = %q, want %q", base, featureTip)
	}
}
