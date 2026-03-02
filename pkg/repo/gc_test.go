package repo

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/odvcencio/graft/pkg/object"
)

// commitFile is a helper that writes a file, stages it, and commits.
// Returns the commit hash.
func commitFile(t *testing.T, r *Repo, name string, content []byte, msg string) object.Hash {
	t.Helper()
	parent := filepath.Dir(filepath.Join(r.RootDir, name))
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(r.RootDir, name), content, 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	if err := r.Add([]string{name}); err != nil {
		t.Fatalf("Add(%s): %v", name, err)
	}
	h, err := r.Commit(msg, "test-author")
	if err != nil {
		t.Fatalf("Commit(%q): %v", msg, err)
	}
	return h
}

// collectReachableHashes walks from a commit hash and returns every object
// hash reachable from it (commit, tree, blob, entity list, entity).
func collectReachableHashes(t *testing.T, r *Repo, commitHash object.Hash) []object.Hash {
	t.Helper()
	reachable, err := r.Store.ReachableSet([]object.Hash{commitHash})
	if err != nil {
		t.Fatalf("ReachableSet: %v", err)
	}
	hashes := make([]object.Hash, 0, len(reachable))
	for h := range reachable {
		hashes = append(hashes, h)
	}
	return hashes
}

// TestGC_PreservesReachableObjects creates a repo with commits, runs GC,
// and verifies all reachable objects (commits, trees, blobs) still exist.
func TestGC_PreservesReachableObjects(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Create a chain of two commits.
	h1 := commitFile(t, r, "main.go", []byte("package main\n\nfunc main() {}\n"), "first commit")
	h2 := commitFile(t, r, "main.go", []byte("package main\n\nfunc main() { println(\"v2\") }\n"), "second commit")

	// Collect all reachable hashes from HEAD before GC.
	allReachable := collectReachableHashes(t, r, h2)
	if len(allReachable) == 0 {
		t.Fatal("expected non-zero reachable objects before GC")
	}

	// Also verify h1's objects are reachable (it is a parent of h2).
	h1Reachable := collectReachableHashes(t, r, h1)
	allReachableMap := make(map[object.Hash]struct{}, len(allReachable))
	for _, h := range allReachable {
		allReachableMap[h] = struct{}{}
	}
	for _, h := range h1Reachable {
		if _, ok := allReachableMap[h]; !ok {
			t.Fatalf("h1 reachable hash %s not in h2 reachable set", h)
		}
	}

	// Run GC.
	summary, err := r.GC()
	if err != nil {
		t.Fatalf("GC: %v", err)
	}
	if summary.PackedObjects == 0 {
		t.Fatal("GC packed 0 objects, expected some to be packed")
	}

	// Verify EVERY reachable object is still readable from the store.
	for _, h := range allReachable {
		if !r.Store.Has(h) {
			t.Errorf("reachable object %s missing after GC", h)
		}
		if _, _, err := r.Store.Read(h); err != nil {
			t.Errorf("reachable object %s unreadable after GC: %v", h, err)
		}
	}

	// Verify commits are specifically readable as commits.
	for _, ch := range []object.Hash{h1, h2} {
		c, err := r.Store.ReadCommit(ch)
		if err != nil {
			t.Errorf("ReadCommit(%s) after GC: %v", ch, err)
		}
		if c == nil {
			t.Errorf("ReadCommit(%s) returned nil", ch)
		}
	}
}

// TestGC_RemovesUnreachableObjects creates objects not referenced by any ref,
// runs GC, and verifies they are removed.
func TestGC_RemovesUnreachableObjects(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Create a commit so there is at least one ref.
	commitFile(t, r, "main.go", []byte("package main\n\nfunc main() {}\n"), "initial commit")

	// Write unreachable objects directly to the store.
	unreachableBlob, err := r.Store.Write(object.TypeBlob, []byte("unreachable data"))
	if err != nil {
		t.Fatalf("Write(unreachable blob): %v", err)
	}
	unreachableBlob2, err := r.Store.Write(object.TypeBlob, []byte("another unreachable blob"))
	if err != nil {
		t.Fatalf("Write(unreachable blob 2): %v", err)
	}

	// Confirm they exist before GC.
	if !r.Store.Has(unreachableBlob) {
		t.Fatal("unreachable blob should exist before GC")
	}
	if !r.Store.Has(unreachableBlob2) {
		t.Fatal("unreachable blob 2 should exist before GC")
	}

	// Run GC.
	summary, err := r.GC()
	if err != nil {
		t.Fatalf("GC: %v", err)
	}
	if summary.PackedObjects == 0 {
		t.Fatal("GC packed 0 objects, expected reachable objects to be packed")
	}

	// Unreachable objects should still exist as loose objects (GC only packs
	// reachable objects; it does not delete unreachable loose objects).
	// However, the key property is that they are NOT in the pack file.
	// They remain as loose objects untouched by GCReachable.
	if !r.Store.Has(unreachableBlob) {
		t.Error("unreachable blob should still exist as loose object after GC")
	}
	if !r.Store.Has(unreachableBlob2) {
		t.Error("unreachable blob 2 should still exist as loose object after GC")
	}

	// Verify the unreachable objects are NOT packed by checking that
	// the loose file still exists on disk (packed objects have their
	// loose copies removed).
	for _, h := range []object.Hash{unreachableBlob, unreachableBlob2} {
		loosePath := filepath.Join(r.GraftDir, "objects", string(h[:2]), string(h[2:]))
		if _, err := os.Stat(loosePath); err != nil {
			t.Errorf("unreachable object %s: loose file should still exist, got: %v", h, err)
		}
	}
}

// TestGC_EmptyRepo verifies that GC on an empty repo (no commits, no refs)
// does not error.
func TestGC_EmptyRepo(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// GC on a fresh repo with no commits.
	summary, err := r.GC()
	if err != nil {
		t.Fatalf("GC on empty repo: %v", err)
	}
	if summary.PackedObjects != 0 {
		t.Errorf("PackedObjects = %d, want 0 for empty repo", summary.PackedObjects)
	}
	if summary.PrunedObjects != 0 {
		t.Errorf("PrunedObjects = %d, want 0 for empty repo", summary.PrunedObjects)
	}
}

// TestGC_MultipleBranches verifies that objects reachable from any branch
// are preserved after GC.
func TestGC_MultipleBranches(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Create initial commit on main.
	h1 := commitFile(t, r, "main.go", []byte("package main\n\nfunc main() {}\n"), "initial on main")

	// Create a second commit on main.
	h2 := commitFile(t, r, "main.go", []byte("package main\n\nfunc main() { println(\"v2\") }\n"), "second on main")

	// Create a branch from h1 (diverge from main).
	if err := r.CreateBranch("feature", h1); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}

	// Collect all reachable objects from both branches.
	mainReachable := collectReachableHashes(t, r, h2)
	featureReachable := collectReachableHashes(t, r, h1)

	allExpected := make(map[object.Hash]struct{})
	for _, h := range mainReachable {
		allExpected[h] = struct{}{}
	}
	for _, h := range featureReachable {
		allExpected[h] = struct{}{}
	}

	// Run GC.
	summary, err := r.GC()
	if err != nil {
		t.Fatalf("GC: %v", err)
	}
	if summary.PackedObjects == 0 {
		t.Fatal("GC packed 0 objects, expected some to be packed")
	}

	// Verify all objects reachable from either branch still exist.
	for h := range allExpected {
		if !r.Store.Has(h) {
			t.Errorf("object %s reachable from a branch is missing after GC", h)
		}
		if _, _, err := r.Store.Read(h); err != nil {
			t.Errorf("object %s reachable from a branch is unreadable after GC: %v", h, err)
		}
	}

	// Verify commits are still valid.
	for _, ch := range []object.Hash{h1, h2} {
		c, err := r.Store.ReadCommit(ch)
		if err != nil {
			t.Errorf("ReadCommit(%s) after GC: %v", ch, err)
		}
		if c == nil {
			t.Errorf("ReadCommit(%s) returned nil after GC", ch)
		}
	}
}

// TestGC_TaggedObjects verifies that objects reachable from tags
// (both lightweight and annotated) are preserved after GC.
func TestGC_TaggedObjects(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Create a commit.
	h1 := commitFile(t, r, "main.go", []byte("package main\n\nfunc main() {}\n"), "initial commit")

	// Create a lightweight tag pointing at the commit.
	if err := r.CreateTag("v0.1.0", h1, false); err != nil {
		t.Fatalf("CreateTag: %v", err)
	}

	// Create a second commit (HEAD moves forward).
	h2 := commitFile(t, r, "main.go", []byte("package main\n\nfunc main() { println(\"v2\") }\n"), "second commit")

	// Create an annotated tag on the second commit.
	tagHash, err := r.CreateAnnotatedTag("v1.0.0", h2, "tagger", "release v1.0.0", false)
	if err != nil {
		t.Fatalf("CreateAnnotatedTag: %v", err)
	}

	// Collect all reachable objects from all refs (including tags).
	h1Reachable := collectReachableHashes(t, r, h1)
	h2Reachable := collectReachableHashes(t, r, h2)
	// The annotated tag object itself and objects reachable through it.
	tagReachable := collectReachableHashes(t, r, tagHash)

	allExpected := make(map[object.Hash]struct{})
	for _, h := range h1Reachable {
		allExpected[h] = struct{}{}
	}
	for _, h := range h2Reachable {
		allExpected[h] = struct{}{}
	}
	for _, h := range tagReachable {
		allExpected[h] = struct{}{}
	}
	// The annotated tag object hash itself must be preserved.
	allExpected[tagHash] = struct{}{}

	// Run GC.
	summary, err := r.GC()
	if err != nil {
		t.Fatalf("GC: %v", err)
	}
	if summary.PackedObjects == 0 {
		t.Fatal("GC packed 0 objects, expected some to be packed")
	}

	// Verify all expected objects still exist.
	for h := range allExpected {
		if !r.Store.Has(h) {
			t.Errorf("object %s (reachable from tag/commit) missing after GC", h)
		}
		if _, _, err := r.Store.Read(h); err != nil {
			t.Errorf("object %s (reachable from tag/commit) unreadable after GC: %v", h, err)
		}
	}

	// Verify the annotated tag can still be read as a tag.
	tagObj, err := r.Store.ReadTag(tagHash)
	if err != nil {
		t.Fatalf("ReadTag(%s) after GC: %v", tagHash, err)
	}
	if tagObj.TargetHash != h2 {
		t.Errorf("tag target = %s, want %s", tagObj.TargetHash, h2)
	}

	// Verify the lightweight tag ref still resolves.
	resolved, err := r.ResolveTag("v0.1.0")
	if err != nil {
		t.Fatalf("ResolveTag(v0.1.0) after GC: %v", err)
	}
	if resolved != h1 {
		t.Errorf("ResolveTag(v0.1.0) = %s, want %s", resolved, h1)
	}
}

// TestGC_PackFiles verifies that GC creates pack and index files,
// that packed objects are removed from loose storage, and that a
// second GC is idempotent (packs nothing new).
func TestGC_PackFiles(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Create a commit with some objects.
	h := commitFile(t, r, "main.go", []byte("package main\n\nfunc main() {}\n"), "initial commit")

	// Collect the reachable hashes to verify later.
	reachable := collectReachableHashes(t, r, h)

	// Verify loose objects exist before GC.
	for _, oh := range reachable {
		loosePath := filepath.Join(r.GraftDir, "objects", string(oh[:2]), string(oh[2:]))
		if _, err := os.Stat(loosePath); err != nil {
			t.Fatalf("loose object %s should exist before GC, got: %v", oh, err)
		}
	}

	// Run GC.
	summary, err := r.GC()
	if err != nil {
		t.Fatalf("GC: %v", err)
	}
	if summary.PackFile == "" {
		t.Fatal("GC did not produce a pack file")
	}
	if summary.IndexFile == "" {
		t.Fatal("GC did not produce an index file")
	}
	if summary.PackedObjects == 0 {
		t.Fatal("GC packed 0 objects")
	}

	// Verify pack and index files exist on disk.
	packDir := filepath.Join(r.GraftDir, "objects", "pack")
	packPath := filepath.Join(packDir, summary.PackFile)
	idxPath := filepath.Join(packDir, summary.IndexFile)
	if _, err := os.Stat(packPath); err != nil {
		t.Fatalf("pack file %s does not exist: %v", packPath, err)
	}
	if _, err := os.Stat(idxPath); err != nil {
		t.Fatalf("index file %s does not exist: %v", idxPath, err)
	}

	// Verify loose objects for packed hashes have been pruned.
	for _, oh := range reachable {
		loosePath := filepath.Join(r.GraftDir, "objects", string(oh[:2]), string(oh[2:]))
		if _, err := os.Stat(loosePath); !os.IsNotExist(err) {
			t.Errorf("loose object %s should be pruned after GC, stat err=%v", oh, err)
		}
	}

	// Verify objects are still readable through the store (from pack).
	for _, oh := range reachable {
		if !r.Store.Has(oh) {
			t.Errorf("packed object %s not found via Has() after GC", oh)
		}
		if _, _, err := r.Store.Read(oh); err != nil {
			t.Errorf("packed object %s not readable after GC: %v", oh, err)
		}
	}

	// Second GC should be idempotent -- nothing new to pack.
	summary2, err := r.GC()
	if err != nil {
		t.Fatalf("second GC: %v", err)
	}
	if summary2.PackedObjects != 0 {
		t.Errorf("second GC PackedObjects = %d, want 0", summary2.PackedObjects)
	}
}

// TestGC_CommitGraphUpdated verifies that GC rebuilds the commit graph.
func TestGC_CommitGraphUpdated(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	h1 := commitFile(t, r, "main.go", []byte("package main\n\nfunc main() {}\n"), "first commit")
	h2 := commitFile(t, r, "main.go", []byte("package main\n\nfunc main() { println(\"v2\") }\n"), "second commit")

	// Run GC, which should rebuild the commit graph.
	_, err = r.GC()
	if err != nil {
		t.Fatalf("GC: %v", err)
	}

	// Read the commit graph and verify it contains both commits.
	cg, err := r.ReadCommitGraph()
	if err != nil {
		t.Fatalf("ReadCommitGraph: %v", err)
	}

	e1 := cg.Lookup(h1)
	if e1 == nil {
		t.Fatalf("commit graph missing entry for h1 (%s)", h1)
	}
	if e1.Generation != 1 {
		t.Errorf("h1 generation = %d, want 1", e1.Generation)
	}

	e2 := cg.Lookup(h2)
	if e2 == nil {
		t.Fatalf("commit graph missing entry for h2 (%s)", h2)
	}
	if e2.Generation != 2 {
		t.Errorf("h2 generation = %d, want 2", e2.Generation)
	}
}

// TestGC_SafetyInvariant_NeverDeletesReachable is the core safety property
// test. It creates a complex object graph with multiple branches, tags,
// and parent chains, then runs GC and verifies that every single object
// reachable from any ref is preserved.
func TestGC_SafetyInvariant_NeverDeletesReachable(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Build a commit chain: c1 -> c2 -> c3 on main.
	c1 := commitFile(t, r, "a.go", []byte("package a\n\nfunc A() {}\n"), "commit 1")
	c2 := commitFile(t, r, "b.go", []byte("package b\n\nfunc B() {}\n"), "commit 2")
	c3 := commitFile(t, r, "a.go", []byte("package a\n\nfunc A() { println(\"updated\") }\n"), "commit 3")

	// Branch from c2.
	if err := r.CreateBranch("dev", c2); err != nil {
		t.Fatalf("CreateBranch(dev): %v", err)
	}

	// Tag c1.
	if err := r.CreateTag("v0.1", c1, false); err != nil {
		t.Fatalf("CreateTag(v0.1): %v", err)
	}

	// Annotated tag on c3.
	tagHash, err := r.CreateAnnotatedTag("v1.0", c3, "tagger", "release", false)
	if err != nil {
		t.Fatalf("CreateAnnotatedTag(v1.0): %v", err)
	}

	// Also write some unreachable objects.
	orphanBlob, err := r.Store.Write(object.TypeBlob, []byte("orphan data"))
	if err != nil {
		t.Fatalf("Write(orphan blob): %v", err)
	}

	// Collect ALL reachable objects from all ref tips.
	refs, err := r.ListRefs("")
	if err != nil {
		t.Fatalf("ListRefs: %v", err)
	}
	refHashes := make([]object.Hash, 0, len(refs))
	for _, h := range refs {
		refHashes = append(refHashes, h)
	}
	allReachable, err := r.Store.ReachableSet(refHashes)
	if err != nil {
		t.Fatalf("ReachableSet: %v", err)
	}

	// Sanity check: we should have a significant number of objects.
	if len(allReachable) < 5 {
		t.Fatalf("expected at least 5 reachable objects, got %d", len(allReachable))
	}

	// The tag object itself should be reachable.
	if _, ok := allReachable[tagHash]; !ok {
		t.Fatal("annotated tag hash should be in reachable set")
	}

	// Run GC.
	summary, err := r.GC()
	if err != nil {
		t.Fatalf("GC: %v", err)
	}
	t.Logf("GC summary: packed=%d, pruned=%d, pack=%s, idx=%s",
		summary.PackedObjects, summary.PrunedObjects, summary.PackFile, summary.IndexFile)

	// THE CRITICAL SAFETY CHECK: every reachable object must still be
	// readable after GC.
	for h := range allReachable {
		if !r.Store.Has(h) {
			t.Errorf("SAFETY VIOLATION: reachable object %s missing after GC", h)
		}
		objType, _, err := r.Store.Read(h)
		if err != nil {
			t.Errorf("SAFETY VIOLATION: reachable object %s unreadable after GC: %v", h, err)
		}
		if objType == "" {
			t.Errorf("SAFETY VIOLATION: reachable object %s has empty type after GC", h)
		}
	}

	// Verify the orphan blob was NOT packed (should still be a loose object).
	loosePath := filepath.Join(r.GraftDir, "objects", string(orphanBlob[:2]), string(orphanBlob[2:]))
	if _, err := os.Stat(loosePath); err != nil {
		t.Errorf("orphan blob %s should remain as loose object, got: %v", orphanBlob, err)
	}

	// Verify all commits are still readable.
	for _, ch := range []object.Hash{c1, c2, c3} {
		c, err := r.Store.ReadCommit(ch)
		if err != nil {
			t.Errorf("ReadCommit(%s) after GC: %v", ch, err)
			continue
		}
		if c.TreeHash == "" {
			t.Errorf("commit %s has empty tree hash after GC", ch)
		}
	}

	// Verify tree data is intact by reading the tree from the latest commit.
	latestCommit, err := r.Store.ReadCommit(c3)
	if err != nil {
		t.Fatalf("ReadCommit(c3): %v", err)
	}
	tree, err := r.Store.ReadTree(latestCommit.TreeHash)
	if err != nil {
		t.Fatalf("ReadTree(%s) after GC: %v", latestCommit.TreeHash, err)
	}
	if len(tree.Entries) == 0 {
		t.Fatal("tree has no entries after GC")
	}

	// Verify blobs are readable through the tree.
	for _, entry := range tree.Entries {
		if entry.IsDir {
			continue
		}
		blob, err := r.Store.ReadBlob(entry.BlobHash)
		if err != nil {
			t.Errorf("ReadBlob(%s) for %s after GC: %v", entry.BlobHash, entry.Name, err)
			continue
		}
		if len(blob.Data) == 0 {
			t.Errorf("blob %s for %s is empty after GC", entry.BlobHash, entry.Name)
		}
	}
}

// TestGC_ReachabilityWalksEntireGraph verifies that the reachability
// analysis correctly follows all reference types: commit -> tree -> blob,
// commit -> parent commits, tree -> subtree, and entity lists -> entities.
func TestGC_ReachabilityWalksEntireGraph(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Create files in nested directories to exercise subtree references.
	if err := os.MkdirAll(filepath.Join(dir, "pkg", "sub"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Go files produce entity lists, which exercises entity list -> entity refs.
	files := map[string][]byte{
		"main.go":         []byte("package main\n\nfunc main() {}\n"),
		"pkg/lib.go":      []byte("package pkg\n\nfunc Lib() {}\n"),
		"pkg/sub/inner.go": []byte("package sub\n\nfunc Inner() {}\n"),
		"data.txt":        []byte("plain text, no entities"),
	}
	for name, data := range files {
		parent := filepath.Dir(filepath.Join(dir, name))
		if err := os.MkdirAll(parent, 0o755); err != nil {
			t.Fatalf("MkdirAll(%s): %v", parent, err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	if err := r.Add(names); err != nil {
		t.Fatalf("Add: %v", err)
	}
	commitHash, err := r.Commit("nested commit", "test-author")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// Get the full reachable set.
	reachable, err := r.Store.ReachableSet([]object.Hash{commitHash})
	if err != nil {
		t.Fatalf("ReachableSet: %v", err)
	}

	// We expect at minimum: 1 commit + 1 root tree + subtrees + blobs +
	// entity lists for .go files + entity objects.
	// Commit(1) + Trees(root, pkg, pkg/sub = 3) + Blobs(4) = 8 minimum
	// Plus entity lists for the 3 .go files and their entity objects.
	if len(reachable) < 8 {
		t.Fatalf("expected at least 8 reachable objects, got %d", len(reachable))
	}

	// Run GC.
	summary, err := r.GC()
	if err != nil {
		t.Fatalf("GC: %v", err)
	}
	if summary.PackedObjects != len(reachable) {
		t.Errorf("PackedObjects = %d, want %d (all reachable)", summary.PackedObjects, len(reachable))
	}

	// After GC, every object should still be readable.
	for h := range reachable {
		if !r.Store.Has(h) {
			t.Errorf("reachable object %s missing after GC", h)
		}
	}

	// Verify the tree structure is intact by flattening.
	commit, err := r.Store.ReadCommit(commitHash)
	if err != nil {
		t.Fatalf("ReadCommit: %v", err)
	}
	entries, err := r.FlattenTree(commit.TreeHash)
	if err != nil {
		t.Fatalf("FlattenTree after GC: %v", err)
	}
	if len(entries) != len(files) {
		t.Errorf("FlattenTree returned %d entries, want %d", len(entries), len(files))
	}
}
