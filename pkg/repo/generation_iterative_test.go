package repo

import (
	"fmt"
	"strings"
	"testing"

	"github.com/odvcencio/graft/pkg/object"
)

// writeTestCommit is a helper that writes a commit to the object store with
// the given parents and returns its hash.
func writeTestCommit(t *testing.T, r *Repo, treeHash object.Hash, parents []object.Hash, msg string) object.Hash {
	t.Helper()
	h, err := r.Store.WriteCommit(&object.CommitObj{
		TreeHash:  treeHash,
		Parents:   parents,
		Author:    "test-author",
		Timestamp: 1_700_000_000,
		Message:   msg,
	})
	if err != nil {
		t.Fatalf("WriteCommit(%q): %v", msg, err)
	}
	return h
}

// TestGeneration_DeepLinearHistory builds a chain of 2000 commits and verifies
// that the iterative algorithm computes generation numbers correctly without
// stack overflow.
func TestGeneration_DeepLinearHistory(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	treeHash, err := r.Store.WriteTree(&object.TreeObj{})
	if err != nil {
		t.Fatalf("WriteTree: %v", err)
	}

	const depth = 2000
	hashes := make([]object.Hash, depth)

	// Root commit (no parents).
	hashes[0] = writeTestCommit(t, r, treeHash, nil, "commit-0")

	// Build a linear chain.
	for i := 1; i < depth; i++ {
		hashes[i] = writeTestCommit(t, r, treeHash, []object.Hash{hashes[i-1]}, fmt.Sprintf("commit-%d", i))
	}

	state := r.getMergeTraversalState()

	// Request generation for the tip (deepest commit). This exercises
	// the full iterative traversal from tip to root.
	gen, err := state.generation(r, hashes[depth-1])
	if err != nil {
		t.Fatalf("generation(tip): %v", err)
	}

	// Root has generation 1, each child adds 1.
	expectedGen := uint64(depth)
	if gen != expectedGen {
		t.Fatalf("generation(tip) = %d, want %d", gen, expectedGen)
	}

	// Verify root generation.
	genRoot, err := state.generation(r, hashes[0])
	if err != nil {
		t.Fatalf("generation(root): %v", err)
	}
	if genRoot != 1 {
		t.Fatalf("generation(root) = %d, want 1", genRoot)
	}

	// Verify a middle commit.
	mid := depth / 2
	genMid, err := state.generation(r, hashes[mid])
	if err != nil {
		t.Fatalf("generation(mid): %v", err)
	}
	expectedMid := uint64(mid + 1)
	if genMid != expectedMid {
		t.Fatalf("generation(mid=%d) = %d, want %d", mid, genMid, expectedMid)
	}

	// Verify all commits were cached.
	if state.generationCacheSize() < depth {
		t.Fatalf("generation cache size = %d, want >= %d", state.generationCacheSize(), depth)
	}
}

// TestGeneration_DiamondMergePattern builds a diamond merge graph:
//
//	    A
//	   / \
//	  B   C
//	   \ /
//	    D
//
// and verifies generation numbers are computed correctly.
func TestGeneration_DiamondMergePattern(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	treeHash, err := r.Store.WriteTree(&object.TreeObj{})
	if err != nil {
		t.Fatalf("WriteTree: %v", err)
	}

	a := writeTestCommit(t, r, treeHash, nil, "A")
	b := writeTestCommit(t, r, treeHash, []object.Hash{a}, "B")
	c := writeTestCommit(t, r, treeHash, []object.Hash{a}, "C")
	d := writeTestCommit(t, r, treeHash, []object.Hash{b, c}, "D")

	state := r.getMergeTraversalState()

	genA, err := state.generation(r, a)
	if err != nil {
		t.Fatalf("generation(A): %v", err)
	}
	genB, err := state.generation(r, b)
	if err != nil {
		t.Fatalf("generation(B): %v", err)
	}
	genC, err := state.generation(r, c)
	if err != nil {
		t.Fatalf("generation(C): %v", err)
	}
	genD, err := state.generation(r, d)
	if err != nil {
		t.Fatalf("generation(D): %v", err)
	}

	// A is root: generation 1.
	if genA != 1 {
		t.Fatalf("generation(A) = %d, want 1", genA)
	}
	// B and C are children of A: generation 2.
	if genB != 2 {
		t.Fatalf("generation(B) = %d, want 2", genB)
	}
	if genC != 2 {
		t.Fatalf("generation(C) = %d, want 2", genC)
	}
	// D merges B and C: max(2,2) + 1 = 3.
	if genD != 3 {
		t.Fatalf("generation(D) = %d, want 3", genD)
	}
}

// TestGeneration_AsymmetricMerge verifies that the generation of a merge
// commit is max(parent generations) + 1 when parents have different depths.
//
//	A - B - C - D
//	         \
//	          M  (merge of D and B)
//	         /
//	    B---+
//
// Actually:
//
//	A -> B -> C -> D
//	          |
//	          M (parents: D, B)
func TestGeneration_AsymmetricMerge(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	treeHash, err := r.Store.WriteTree(&object.TreeObj{})
	if err != nil {
		t.Fatalf("WriteTree: %v", err)
	}

	a := writeTestCommit(t, r, treeHash, nil, "A")
	b := writeTestCommit(t, r, treeHash, []object.Hash{a}, "B")
	c := writeTestCommit(t, r, treeHash, []object.Hash{b}, "C")
	d := writeTestCommit(t, r, treeHash, []object.Hash{c}, "D")
	// Merge commit M with parents D (gen 4) and B (gen 2).
	m := writeTestCommit(t, r, treeHash, []object.Hash{d, b}, "M")

	state := r.getMergeTraversalState()

	genM, err := state.generation(r, m)
	if err != nil {
		t.Fatalf("generation(M): %v", err)
	}

	// D has generation 4, B has generation 2. M = max(4,2)+1 = 5.
	if genM != 5 {
		t.Fatalf("generation(M) = %d, want 5", genM)
	}
}

// TestGeneration_EmptyHash verifies that empty hash returns generation 0.
func TestGeneration_EmptyHash(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	state := r.getMergeTraversalState()

	gen, err := state.generation(r, "")
	if err != nil {
		t.Fatalf("generation(empty): %v", err)
	}
	if gen != 0 {
		t.Fatalf("generation(empty) = %d, want 0", gen)
	}
}

// TestGeneration_CycleDetection verifies that cycles in the commit graph
// are detected and reported as errors by the iterative algorithm.
func TestGeneration_CycleDetection(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	treeHash, err := r.Store.WriteTree(&object.TreeObj{})
	if err != nil {
		t.Fatalf("WriteTree: %v", err)
	}

	// Create A -> B, then corrupt A to point back to B, forming A <-> B cycle.
	commitA := writeTestCommit(t, r, treeHash, nil, "A")
	commitB := writeTestCommit(t, r, treeHash, []object.Hash{commitA}, "B")

	// Corrupt A to have B as parent (creating cycle).
	corruptA, err := r.Store.ReadCommit(commitA)
	if err != nil {
		t.Fatalf("ReadCommit(A): %v", err)
	}
	corruptA.Parents = []object.Hash{commitB}
	writeCorruptCommitAtHash(t, r, commitA, corruptA)

	state := r.getMergeTraversalState()

	_, err = state.generation(r, commitB)
	if err == nil {
		t.Fatal("expected cycle error, got nil")
	}
	if !strings.Contains(err.Error(), "cycle detected") {
		t.Fatalf("generation cycle error = %q, want to contain %q", err, "cycle detected")
	}
}

// TestGeneration_CachingPreservation verifies that once a generation number
// is computed, subsequent calls return the cached value without re-traversal.
func TestGeneration_CachingPreservation(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	treeHash, err := r.Store.WriteTree(&object.TreeObj{})
	if err != nil {
		t.Fatalf("WriteTree: %v", err)
	}

	a := writeTestCommit(t, r, treeHash, nil, "A")
	b := writeTestCommit(t, r, treeHash, []object.Hash{a}, "B")
	c := writeTestCommit(t, r, treeHash, []object.Hash{b}, "C")

	state := r.getMergeTraversalState()

	// Compute generation for C (should also cache A and B).
	gen1, err := state.generation(r, c)
	if err != nil {
		t.Fatalf("generation(C) first call: %v", err)
	}

	// Verify all three are cached.
	if state.generationCacheSize() != 3 {
		t.Fatalf("cache size = %d, want 3", state.generationCacheSize())
	}

	// Second call should return same value from cache.
	gen2, err := state.generation(r, c)
	if err != nil {
		t.Fatalf("generation(C) second call: %v", err)
	}
	if gen1 != gen2 {
		t.Fatalf("generation(C) first=%d second=%d, want equal", gen1, gen2)
	}

	// Verify individual cached values.
	genA, _ := state.loadGeneration(a)
	genB, _ := state.loadGeneration(b)
	if genA != 1 {
		t.Fatalf("cached generation(A) = %d, want 1", genA)
	}
	if genB != 2 {
		t.Fatalf("cached generation(B) = %d, want 2", genB)
	}
}

// TestGeneration_MultipleDiamonds builds a chain of diamond merges and
// verifies generation numbers are correct throughout.
//
//	A -> B -> D (merge B,C) -> E -> G (merge E,F) -> ...
//	     |                     |
//	     C --------------------F
func TestGeneration_MultipleDiamonds(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	treeHash, err := r.Store.WriteTree(&object.TreeObj{})
	if err != nil {
		t.Fatalf("WriteTree: %v", err)
	}

	root := writeTestCommit(t, r, treeHash, nil, "root")

	state := r.getMergeTraversalState()

	// Build 50 diamond patterns chained together.
	const diamonds = 50
	tip := root
	for i := 0; i < diamonds; i++ {
		left := writeTestCommit(t, r, treeHash, []object.Hash{tip}, fmt.Sprintf("left-%d", i))
		right := writeTestCommit(t, r, treeHash, []object.Hash{tip}, fmt.Sprintf("right-%d", i))
		tip = writeTestCommit(t, r, treeHash, []object.Hash{left, right}, fmt.Sprintf("merge-%d", i))
	}

	gen, err := state.generation(r, tip)
	if err != nil {
		t.Fatalf("generation(tip): %v", err)
	}

	// root=1, then each diamond adds 2 (fork + merge).
	// After diamond i: fork children = gen(tip)+1, merge = gen(tip)+2
	expectedGen := uint64(1 + diamonds*2)
	if gen != expectedGen {
		t.Fatalf("generation(tip after %d diamonds) = %d, want %d", diamonds, gen, expectedGen)
	}
}

// TestGeneration_RootCommitIsOne verifies that a root commit (no parents)
// has generation 1.
func TestGeneration_RootCommitIsOne(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	treeHash, err := r.Store.WriteTree(&object.TreeObj{})
	if err != nil {
		t.Fatalf("WriteTree: %v", err)
	}

	root := writeTestCommit(t, r, treeHash, nil, "root")
	state := r.getMergeTraversalState()

	gen, err := state.generation(r, root)
	if err != nil {
		t.Fatalf("generation(root): %v", err)
	}
	if gen != 1 {
		t.Fatalf("generation(root) = %d, want 1", gen)
	}
}
