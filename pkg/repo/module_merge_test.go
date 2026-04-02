package repo

import (
	"testing"
	"time"

	"github.com/odvcencio/graft/pkg/object"
)

// makeCommitChain creates a chain of n commits (each parented on the previous)
// and returns their hashes in order [oldest … newest]. This gives commit i
// generation number i+1.
func makeCommitChain(t *testing.T, store *object.Store, n int) []object.Hash {
	t.Helper()

	// Write a minimal tree for commits to reference.
	tree := &object.TreeObj{}
	treeHash, err := store.WriteTree(tree)
	if err != nil {
		t.Fatalf("write tree: %v", err)
	}

	hashes := make([]object.Hash, 0, n)
	for i := 0; i < n; i++ {
		c := &object.CommitObj{
			TreeHash:  treeHash,
			Author:    "test",
			Timestamp: time.Now().Unix() + int64(i),
			Message:   "commit",
		}
		if i > 0 {
			c.Parents = []object.Hash{hashes[i-1]}
		}
		h, err := store.WriteCommit(c)
		if err != nil {
			t.Fatalf("write commit %d: %v", i, err)
		}
		hashes = append(hashes, h)
	}
	return hashes
}

func moduleMap(entries ...TreeModuleEntry) map[string]TreeModuleEntry {
	m := make(map[string]TreeModuleEntry, len(entries))
	for _, e := range entries {
		m[e.Path] = e
	}
	return m
}

// TestMergeModules_OneSideChanged: base=A, ours=B, theirs=A -> resolved to B.
func TestMergeModules_OneSideChanged(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	hashes := makeCommitChain(t, r.Store, 2)
	hashA := hashes[0]
	hashB := hashes[1]

	baseMap := moduleMap(TreeModuleEntry{Path: "libs/foo", BlobHash: hashA})
	oursMap := moduleMap(TreeModuleEntry{Path: "libs/foo", BlobHash: hashB})
	theirsMap := moduleMap(TreeModuleEntry{Path: "libs/foo", BlobHash: hashA})

	result, err := r.mergeModuleEntries(baseMap, oursMap, theirsMap)
	if err != nil {
		t.Fatalf("mergeModuleEntries: %v", err)
	}

	if result.HasConflicts {
		t.Errorf("expected no conflicts, got %d", len(result.Conflicts))
	}
	if got, ok := result.Resolved["libs/foo"]; !ok {
		t.Fatal("libs/foo not in resolved map")
	} else if got != hashB {
		t.Errorf("resolved = %s, want %s (ours)", got, hashB)
	}
	if len(result.Removed) != 0 {
		t.Errorf("unexpected removals: %v", result.Removed)
	}
}

// TestMergeModules_BothChangedNewerWins: both sides change from base to
// different commits. The one with the higher generation number wins.
func TestMergeModules_BothChangedNewerWins(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Create a chain: c0 -> c1 -> c2 -> c3
	// c0 = base, c1 = ours (gen 2), c3 = theirs (gen 4). Theirs is newer.
	hashes := makeCommitChain(t, r.Store, 4)
	hashBase := hashes[0]
	hashOurs := hashes[1]   // generation 2
	hashTheirs := hashes[3] // generation 4

	baseMap := moduleMap(TreeModuleEntry{Path: "libs/bar", BlobHash: hashBase})
	oursMap := moduleMap(TreeModuleEntry{Path: "libs/bar", BlobHash: hashOurs})
	theirsMap := moduleMap(TreeModuleEntry{Path: "libs/bar", BlobHash: hashTheirs})

	result, err := r.mergeModuleEntries(baseMap, oursMap, theirsMap)
	if err != nil {
		t.Fatalf("mergeModuleEntries: %v", err)
	}

	if result.HasConflicts {
		t.Errorf("expected no conflicts, got %d", len(result.Conflicts))
	}
	if got := result.Resolved["libs/bar"]; got != hashTheirs {
		t.Errorf("resolved = %s, want %s (theirs, newer)", got, hashTheirs)
	}
}

// TestMergeModules_BothDeleted: base has it, ours and theirs don't -> removed.
func TestMergeModules_BothDeleted(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	hashes := makeCommitChain(t, r.Store, 1)
	hashA := hashes[0]

	baseMap := moduleMap(TreeModuleEntry{Path: "libs/gone", BlobHash: hashA})
	oursMap := moduleMap()
	theirsMap := moduleMap()

	result, err := r.mergeModuleEntries(baseMap, oursMap, theirsMap)
	if err != nil {
		t.Fatalf("mergeModuleEntries: %v", err)
	}

	if result.HasConflicts {
		t.Errorf("expected no conflicts")
	}
	if _, ok := result.Resolved["libs/gone"]; ok {
		t.Error("libs/gone should not be in resolved")
	}
	if len(result.Removed) != 1 || result.Removed[0] != "libs/gone" {
		t.Errorf("Removed = %v, want [libs/gone]", result.Removed)
	}
}

// TestMergeModules_AddedInOneOnly: not in base, only in theirs -> resolved to theirs.
func TestMergeModules_AddedInOneOnly(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	hashes := makeCommitChain(t, r.Store, 1)
	hashTheirs := hashes[0]

	baseMap := moduleMap()
	oursMap := moduleMap()
	theirsMap := moduleMap(TreeModuleEntry{Path: "libs/new", BlobHash: hashTheirs})

	result, err := r.mergeModuleEntries(baseMap, oursMap, theirsMap)
	if err != nil {
		t.Fatalf("mergeModuleEntries: %v", err)
	}

	if result.HasConflicts {
		t.Errorf("expected no conflicts")
	}
	if got := result.Resolved["libs/new"]; got != hashTheirs {
		t.Errorf("resolved = %s, want %s", got, hashTheirs)
	}
	if len(result.Removed) != 0 {
		t.Errorf("unexpected removals: %v", result.Removed)
	}
}

// TestMergeModules_Unchanged: all three sides have the same hash -> resolved to that hash.
func TestMergeModules_Unchanged(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	hashes := makeCommitChain(t, r.Store, 1)
	hashA := hashes[0]

	baseMap := moduleMap(TreeModuleEntry{Path: "libs/stable", BlobHash: hashA})
	oursMap := moduleMap(TreeModuleEntry{Path: "libs/stable", BlobHash: hashA})
	theirsMap := moduleMap(TreeModuleEntry{Path: "libs/stable", BlobHash: hashA})

	result, err := r.mergeModuleEntries(baseMap, oursMap, theirsMap)
	if err != nil {
		t.Fatalf("mergeModuleEntries: %v", err)
	}

	if result.HasConflicts {
		t.Errorf("expected no conflicts")
	}
	if got := result.Resolved["libs/stable"]; got != hashA {
		t.Errorf("resolved = %s, want %s", got, hashA)
	}
	if len(result.Removed) != 0 {
		t.Errorf("unexpected removals: %v", result.Removed)
	}
}

// TestMergeModules_DeletedByTheirsOursChanged: theirs deletes, ours updates -> keep ours.
func TestMergeModules_DeletedByTheirsOursChanged(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	hashes := makeCommitChain(t, r.Store, 2)
	hashBase := hashes[0]
	hashOurs := hashes[1]

	baseMap := moduleMap(TreeModuleEntry{Path: "libs/kept", BlobHash: hashBase})
	oursMap := moduleMap(TreeModuleEntry{Path: "libs/kept", BlobHash: hashOurs})
	theirsMap := moduleMap()

	result, err := r.mergeModuleEntries(baseMap, oursMap, theirsMap)
	if err != nil {
		t.Fatalf("mergeModuleEntries: %v", err)
	}

	if result.HasConflicts {
		t.Errorf("expected no conflicts")
	}
	if got := result.Resolved["libs/kept"]; got != hashOurs {
		t.Errorf("resolved = %s, want %s (ours changed)", got, hashOurs)
	}
	if len(result.Removed) != 0 {
		t.Errorf("unexpected removals: %v", result.Removed)
	}
}

// TestMergeModules_DeletedByOursTheirsChanged: ours deletes, theirs updates -> keep theirs.
func TestMergeModules_DeletedByOursTheirsChanged(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	hashes := makeCommitChain(t, r.Store, 2)
	hashBase := hashes[0]
	hashTheirs := hashes[1]

	baseMap := moduleMap(TreeModuleEntry{Path: "libs/kept", BlobHash: hashBase})
	oursMap := moduleMap()
	theirsMap := moduleMap(TreeModuleEntry{Path: "libs/kept", BlobHash: hashTheirs})

	result, err := r.mergeModuleEntries(baseMap, oursMap, theirsMap)
	if err != nil {
		t.Fatalf("mergeModuleEntries: %v", err)
	}

	if result.HasConflicts {
		t.Errorf("expected no conflicts")
	}
	if got := result.Resolved["libs/kept"]; got != hashTheirs {
		t.Errorf("resolved = %s, want %s (theirs changed)", got, hashTheirs)
	}
	if len(result.Removed) != 0 {
		t.Errorf("unexpected removals: %v", result.Removed)
	}
}

// TestMergeModules_AddedBothSame: both add with same hash -> resolved.
func TestMergeModules_AddedBothSame(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	hashes := makeCommitChain(t, r.Store, 1)
	h := hashes[0]

	baseMap := moduleMap()
	oursMap := moduleMap(TreeModuleEntry{Path: "libs/shared", BlobHash: h})
	theirsMap := moduleMap(TreeModuleEntry{Path: "libs/shared", BlobHash: h})

	result, err := r.mergeModuleEntries(baseMap, oursMap, theirsMap)
	if err != nil {
		t.Fatalf("mergeModuleEntries: %v", err)
	}

	if result.HasConflicts {
		t.Errorf("expected no conflicts")
	}
	if got := result.Resolved["libs/shared"]; got != h {
		t.Errorf("resolved = %s, want %s", got, h)
	}
}

// TestMergeModules_AddedBothDifferentNewerWins: both add with different hashes.
func TestMergeModules_AddedBothDifferentNewerWins(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	hashes := makeCommitChain(t, r.Store, 3)
	hashOurs := hashes[0]   // generation 1
	hashTheirs := hashes[2] // generation 3

	baseMap := moduleMap()
	oursMap := moduleMap(TreeModuleEntry{Path: "libs/both", BlobHash: hashOurs})
	theirsMap := moduleMap(TreeModuleEntry{Path: "libs/both", BlobHash: hashTheirs})

	result, err := r.mergeModuleEntries(baseMap, oursMap, theirsMap)
	if err != nil {
		t.Fatalf("mergeModuleEntries: %v", err)
	}

	if result.HasConflicts {
		t.Errorf("expected no conflicts, got %v", result.Conflicts)
	}
	if got := result.Resolved["libs/both"]; got != hashTheirs {
		t.Errorf("resolved = %s, want %s (theirs, newer)", got, hashTheirs)
	}
}

// TestCollectModulePaths verifies sorted, deduplicated path collection.
func TestCollectModulePaths(t *testing.T) {
	m1 := map[string]TreeModuleEntry{
		"b/mod": {Path: "b/mod"},
		"a/mod": {Path: "a/mod"},
	}
	m2 := map[string]TreeModuleEntry{
		"c/mod": {Path: "c/mod"},
		"a/mod": {Path: "a/mod"},
	}

	paths := collectModulePaths(m1, m2)
	want := []string{"a/mod", "b/mod", "c/mod"}

	if len(paths) != len(want) {
		t.Fatalf("paths = %v, want %v", paths, want)
	}
	for i := range want {
		if paths[i] != want[i] {
			t.Errorf("paths[%d] = %q, want %q", i, paths[i], want[i])
		}
	}
}
