package repo

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/odvcencio/graft/pkg/object"
)

// TestCommitGraph_WriteAndRead writes a commit graph file and reads it back,
// verifying that all entries round-trip correctly.
func TestCommitGraph_WriteAndRead(t *testing.T) {
	r := initRepoWithFile(t, "main.go", []byte("package main\n\nfunc main() {}\n"))

	h1, err := r.Commit("first commit", "test-author")
	if err != nil {
		t.Fatalf("first Commit: %v", err)
	}

	// Modify and commit again.
	if err := os.WriteFile(filepath.Join(r.RootDir, "main.go"),
		[]byte("package main\n\nfunc main() { println(\"v2\") }\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	h2, err := r.Commit("second commit", "test-author")
	if err != nil {
		t.Fatalf("second Commit: %v", err)
	}

	// Write the commit graph.
	if err := r.WriteCommitGraph(); err != nil {
		t.Fatalf("WriteCommitGraph: %v", err)
	}

	// Verify the file exists on disk.
	graphPath := filepath.Join(r.GraftDir, "objects", "info", "commit-graph")
	if _, err := os.Stat(graphPath); err != nil {
		t.Fatalf("commit-graph file missing: %v", err)
	}

	// Read it back.
	graph, err := r.ReadCommitGraph()
	if err != nil {
		t.Fatalf("ReadCommitGraph: %v", err)
	}

	// Verify both commits are present.
	e1 := graph.Lookup(h1)
	if e1 == nil {
		t.Fatalf("entry for h1 (%s) not found", h1)
	}
	e2 := graph.Lookup(h2)
	if e2 == nil {
		t.Fatalf("entry for h2 (%s) not found", h2)
	}

	// Verify entry fields for h1 (root commit).
	c1, err := r.Store.ReadCommit(h1)
	if err != nil {
		t.Fatalf("ReadCommit(h1): %v", err)
	}
	if e1.TreeHash != c1.TreeHash {
		t.Errorf("h1 TreeHash = %q, want %q", e1.TreeHash, c1.TreeHash)
	}
	if len(e1.Parents) != 0 {
		t.Errorf("h1 Parents = %v, want empty", e1.Parents)
	}
	if e1.Timestamp != c1.Timestamp {
		t.Errorf("h1 Timestamp = %d, want %d", e1.Timestamp, c1.Timestamp)
	}

	// Verify entry fields for h2.
	c2, err := r.Store.ReadCommit(h2)
	if err != nil {
		t.Fatalf("ReadCommit(h2): %v", err)
	}
	if e2.TreeHash != c2.TreeHash {
		t.Errorf("h2 TreeHash = %q, want %q", e2.TreeHash, c2.TreeHash)
	}
	if len(e2.Parents) != 1 || e2.Parents[0] != h1 {
		t.Errorf("h2 Parents = %v, want [%s]", e2.Parents, h1)
	}
	if e2.Timestamp != c2.Timestamp {
		t.Errorf("h2 Timestamp = %d, want %d", e2.Timestamp, c2.Timestamp)
	}
}

// TestCommitGraph_GenerationNumbers verifies generation numbers for a linear
// chain: root=1, child=2, grandchild=3.
func TestCommitGraph_GenerationNumbers(t *testing.T) {
	r := initRepoWithFile(t, "main.go", []byte("package main\n\nfunc main() {}\n"))

	h1, err := r.Commit("first", "test-author")
	if err != nil {
		t.Fatalf("Commit 1: %v", err)
	}

	if err := os.WriteFile(filepath.Join(r.RootDir, "main.go"),
		[]byte("package main\n\nfunc main() { _ = 2 }\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	h2, err := r.Commit("second", "test-author")
	if err != nil {
		t.Fatalf("Commit 2: %v", err)
	}

	if err := os.WriteFile(filepath.Join(r.RootDir, "main.go"),
		[]byte("package main\n\nfunc main() { _ = 3 }\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	h3, err := r.Commit("third", "test-author")
	if err != nil {
		t.Fatalf("Commit 3: %v", err)
	}

	if err := r.WriteCommitGraph(); err != nil {
		t.Fatalf("WriteCommitGraph: %v", err)
	}

	graph, err := r.ReadCommitGraph()
	if err != nil {
		t.Fatalf("ReadCommitGraph: %v", err)
	}

	// Root commit: generation 1.
	if g := graph.Generation(h1); g != 1 {
		t.Errorf("generation(h1) = %d, want 1", g)
	}
	// Child: generation 2.
	if g := graph.Generation(h2); g != 2 {
		t.Errorf("generation(h2) = %d, want 2", g)
	}
	// Grandchild: generation 3.
	if g := graph.Generation(h3); g != 3 {
		t.Errorf("generation(h3) = %d, want 3", g)
	}
}

// TestCommitGraph_MergeCommitGeneration verifies that a merge commit's
// generation equals max(parent generations) + 1.
func TestCommitGraph_MergeCommitGeneration(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Create a file and initial commit.
	if err := os.WriteFile(filepath.Join(dir, "main.go"),
		[]byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	rootHash, err := r.Commit("root", "test-author")
	if err != nil {
		t.Fatalf("Commit root: %v", err)
	}

	// Read the root commit to get tree hash.
	rootCommit, err := r.Store.ReadCommit(rootHash)
	if err != nil {
		t.Fatalf("ReadCommit root: %v", err)
	}

	// Create two child commits directly via the store (to simulate
	// two branches diverging from root).
	childA := &object.CommitObj{
		TreeHash:  rootCommit.TreeHash,
		Parents:   []object.Hash{rootHash},
		Author:    "test-author",
		Timestamp: time.Now().Unix(),
		Message:   "child A",
	}
	hashA, err := r.Store.WriteCommit(childA)
	if err != nil {
		t.Fatalf("WriteCommit childA: %v", err)
	}

	// childB has childA as parent so it is at generation 3.
	childB := &object.CommitObj{
		TreeHash:  rootCommit.TreeHash,
		Parents:   []object.Hash{hashA},
		Author:    "test-author",
		Timestamp: time.Now().Unix(),
		Message:   "child B",
	}
	hashB, err := r.Store.WriteCommit(childB)
	if err != nil {
		t.Fatalf("WriteCommit childB: %v", err)
	}

	// Create a second branch off root for the merge.
	childC := &object.CommitObj{
		TreeHash:  rootCommit.TreeHash,
		Parents:   []object.Hash{rootHash},
		Author:    "test-author",
		Timestamp: time.Now().Unix(),
		Message:   "child C",
	}
	hashC, err := r.Store.WriteCommit(childC)
	if err != nil {
		t.Fatalf("WriteCommit childC: %v", err)
	}

	// Merge commit with parents B (gen=3) and C (gen=2).
	// Expected generation = max(3, 2) + 1 = 4.
	mergeCommit := &object.CommitObj{
		TreeHash:  rootCommit.TreeHash,
		Parents:   []object.Hash{hashB, hashC},
		Author:    "test-author",
		Timestamp: time.Now().Unix(),
		Message:   "merge B and C",
	}
	mergeHash, err := r.Store.WriteCommit(mergeCommit)
	if err != nil {
		t.Fatalf("WriteCommit merge: %v", err)
	}

	// Point a ref at the merge commit so WriteCommitGraph can find it.
	if err := r.UpdateRef("refs/heads/main", mergeHash); err != nil {
		t.Fatalf("UpdateRef: %v", err)
	}

	if err := r.WriteCommitGraph(); err != nil {
		t.Fatalf("WriteCommitGraph: %v", err)
	}

	graph, err := r.ReadCommitGraph()
	if err != nil {
		t.Fatalf("ReadCommitGraph: %v", err)
	}

	// root=1, A=2, B=3, C=2, merge=4
	if g := graph.Generation(rootHash); g != 1 {
		t.Errorf("generation(root) = %d, want 1", g)
	}
	if g := graph.Generation(hashA); g != 2 {
		t.Errorf("generation(A) = %d, want 2", g)
	}
	if g := graph.Generation(hashB); g != 3 {
		t.Errorf("generation(B) = %d, want 3", g)
	}
	if g := graph.Generation(hashC); g != 2 {
		t.Errorf("generation(C) = %d, want 2", g)
	}
	if g := graph.Generation(mergeHash); g != 4 {
		t.Errorf("generation(merge) = %d, want 4", g)
	}
}

// TestCommitGraph_Lookup verifies that Lookup returns the correct entry for
// known hashes and nil for unknown hashes.
func TestCommitGraph_Lookup(t *testing.T) {
	r := initRepoWithFile(t, "main.go", []byte("package main\n\nfunc main() {}\n"))

	h, err := r.Commit("initial commit", "test-author")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	if err := r.WriteCommitGraph(); err != nil {
		t.Fatalf("WriteCommitGraph: %v", err)
	}

	graph, err := r.ReadCommitGraph()
	if err != nil {
		t.Fatalf("ReadCommitGraph: %v", err)
	}

	// Existing hash should return non-nil.
	entry := graph.Lookup(h)
	if entry == nil {
		t.Fatalf("Lookup(%s) = nil, want non-nil", h)
	}

	// Verify the entry has reasonable data.
	if entry.TreeHash == "" {
		t.Error("entry.TreeHash is empty")
	}
	if entry.Generation != 1 {
		t.Errorf("entry.Generation = %d, want 1", entry.Generation)
	}

	// Non-existent hash should return nil.
	fakeHash := object.Hash("0000000000000000000000000000000000000000000000000000000000000000")
	if got := graph.Lookup(fakeHash); got != nil {
		t.Errorf("Lookup(fake) = %v, want nil", got)
	}

	// Lookup on nil graph should return nil (no panic).
	var nilGraph *CommitGraph
	if got := nilGraph.Lookup(h); got != nil {
		t.Errorf("nil graph Lookup = %v, want nil", got)
	}

	// Generation on missing hash should return 0.
	if g := graph.Generation(fakeHash); g != 0 {
		t.Errorf("Generation(fake) = %d, want 0", g)
	}
}

// TestCommitGraph_EmptyRepo verifies that writing and reading a commit graph
// in a repository with no commits produces an empty graph.
func TestCommitGraph_EmptyRepo(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// WriteCommitGraph on empty repo should succeed.
	if err := r.WriteCommitGraph(); err != nil {
		t.Fatalf("WriteCommitGraph: %v", err)
	}

	graph, err := r.ReadCommitGraph()
	if err != nil {
		t.Fatalf("ReadCommitGraph: %v", err)
	}

	if len(graph.Entries) != 0 {
		t.Errorf("expected empty graph, got %d entries", len(graph.Entries))
	}

	// Reading without writing should also return empty graph.
	dir2 := t.TempDir()
	r2, err := Init(dir2)
	if err != nil {
		t.Fatalf("Init r2: %v", err)
	}
	graph2, err := r2.ReadCommitGraph()
	if err != nil {
		t.Fatalf("ReadCommitGraph r2: %v", err)
	}
	if len(graph2.Entries) != 0 {
		t.Errorf("expected empty graph for r2, got %d entries", len(graph2.Entries))
	}
}
