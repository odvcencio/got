package repo

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/odvcencio/graft/pkg/object"
	"github.com/odvcencio/graft/pkg/remote"
)

func TestShallowState_ReadsShallowFile(t *testing.T) {
	dir := t.TempDir()
	graftDir := filepath.Join(dir, ".graft")
	if err := os.MkdirAll(filepath.Join(graftDir, "objects"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(graftDir, "refs", "heads"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(graftDir, "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	hash1 := object.Hash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	hash2 := object.Hash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")

	state := remote.NewShallowState()
	state.Add(hash1)
	state.Add(hash2)
	if err := remote.WriteShallowFile(graftDir, state); err != nil {
		t.Fatal(err)
	}

	r, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}

	got, err := r.ShallowState()
	if err != nil {
		t.Fatalf("ShallowState(): %v", err)
	}
	if got.Len() != 2 {
		t.Fatalf("Len() = %d, want 2", got.Len())
	}
	if !got.IsShallow(hash1) {
		t.Errorf("expected %s to be shallow", hash1)
	}
	if !got.IsShallow(hash2) {
		t.Errorf("expected %s to be shallow", hash2)
	}

	if !r.IsShallowRepository() {
		t.Error("expected IsShallowRepository to return true")
	}
}

func TestShallowState_EmptyWhenNoFile(t *testing.T) {
	dir := t.TempDir()
	graftDir := filepath.Join(dir, ".graft")
	if err := os.MkdirAll(filepath.Join(graftDir, "objects"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(graftDir, "refs", "heads"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(graftDir, "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	r, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}

	got, err := r.ShallowState()
	if err != nil {
		t.Fatalf("ShallowState(): %v", err)
	}
	if got.Len() != 0 {
		t.Fatalf("expected empty shallow state, got %d entries", got.Len())
	}

	if r.IsShallowRepository() {
		t.Error("expected IsShallowRepository to return false")
	}
}

func TestLogStopsAtShallowBoundary(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatal(err)
	}

	// Create a chain of commits: A -> B -> C
	blobHash, err := r.Store.WriteBlob(&object.Blob{Data: []byte("hello\n")})
	if err != nil {
		t.Fatal(err)
	}
	treeHash, err := r.Store.WriteTree(&object.TreeObj{Entries: []object.TreeEntry{{Name: "file.txt", BlobHash: blobHash}}})
	if err != nil {
		t.Fatal(err)
	}

	commitA, err := r.Store.WriteCommit(&object.CommitObj{
		TreeHash:  treeHash,
		Author:    "Alice",
		Timestamp: 1700000000,
		Message:   "commit A",
	})
	if err != nil {
		t.Fatal(err)
	}

	commitB, err := r.Store.WriteCommit(&object.CommitObj{
		TreeHash:  treeHash,
		Parents:   []object.Hash{commitA},
		Author:    "Alice",
		Timestamp: 1700000001,
		Message:   "commit B",
	})
	if err != nil {
		t.Fatal(err)
	}

	commitC, err := r.Store.WriteCommit(&object.CommitObj{
		TreeHash:  treeHash,
		Parents:   []object.Hash{commitB},
		Author:    "Alice",
		Timestamp: 1700000002,
		Message:   "commit C",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Mark commit A as a shallow boundary — log should stop before reaching it.
	state := remote.NewShallowState()
	state.Add(commitA)
	if err := remote.WriteShallowFile(r.GraftDir, state); err != nil {
		t.Fatal(err)
	}

	// Re-open the repo so the cached shallow state is fresh.
	r, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}

	commits, err := r.Log(commitC, 100)
	if err != nil {
		t.Fatalf("Log: %v", err)
	}

	// Should get C and B, but not A (shallow boundary).
	if len(commits) != 2 {
		t.Fatalf("expected 2 commits in log, got %d", len(commits))
	}
	if commits[0].Message != "commit C" {
		t.Errorf("first commit message = %q, want 'commit C'", commits[0].Message)
	}
	if commits[1].Message != "commit B" {
		t.Errorf("second commit message = %q, want 'commit B'", commits[1].Message)
	}
}

func TestShortlogStopsAtShallowBoundary(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatal(err)
	}

	blobHash, err := r.Store.WriteBlob(&object.Blob{Data: []byte("hello\n")})
	if err != nil {
		t.Fatal(err)
	}
	treeHash, err := r.Store.WriteTree(&object.TreeObj{Entries: []object.TreeEntry{{Name: "file.txt", BlobHash: blobHash}}})
	if err != nil {
		t.Fatal(err)
	}

	commitA, err := r.Store.WriteCommit(&object.CommitObj{
		TreeHash:  treeHash,
		Author:    "Alice",
		Timestamp: 1700000000,
		Message:   "commit A",
	})
	if err != nil {
		t.Fatal(err)
	}

	commitB, err := r.Store.WriteCommit(&object.CommitObj{
		TreeHash:  treeHash,
		Parents:   []object.Hash{commitA},
		Author:    "Bob",
		Timestamp: 1700000001,
		Message:   "commit B",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Point HEAD at commit B.
	if err := r.UpdateRef("refs/heads/main", commitB); err != nil {
		t.Fatal(err)
	}

	// Mark commit A as a shallow boundary.
	state := remote.NewShallowState()
	state.Add(commitA)
	if err := remote.WriteShallowFile(r.GraftDir, state); err != nil {
		t.Fatal(err)
	}

	r, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}

	entries, err := r.Shortlog(ShortlogOptions{})
	if err != nil {
		t.Fatalf("Shortlog: %v", err)
	}

	// Should only see Bob's commit (B), not Alice's (A is a shallow boundary).
	if len(entries) != 1 {
		t.Fatalf("expected 1 shortlog entry, got %d", len(entries))
	}
	if entries[0].Author != "Bob" {
		t.Errorf("expected author 'Bob', got %q", entries[0].Author)
	}
	if entries[0].Count != 1 {
		t.Errorf("expected count 1, got %d", entries[0].Count)
	}
}

func TestFindMergeBaseWithShallowBoundary(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatal(err)
	}

	blobHash, err := r.Store.WriteBlob(&object.Blob{Data: []byte("hello\n")})
	if err != nil {
		t.Fatal(err)
	}
	treeHash, err := r.Store.WriteTree(&object.TreeObj{Entries: []object.TreeEntry{{Name: "file.txt", BlobHash: blobHash}}})
	if err != nil {
		t.Fatal(err)
	}

	// A -> B (branch1) and A -> C (branch2), but A is a shallow boundary.
	commitA, err := r.Store.WriteCommit(&object.CommitObj{
		TreeHash:  treeHash,
		Author:    "Alice",
		Timestamp: 1700000000,
		Message:   "commit A",
	})
	if err != nil {
		t.Fatal(err)
	}

	commitB, err := r.Store.WriteCommit(&object.CommitObj{
		TreeHash:  treeHash,
		Parents:   []object.Hash{commitA},
		Author:    "Alice",
		Timestamp: 1700000001,
		Message:   "commit B",
	})
	if err != nil {
		t.Fatal(err)
	}

	commitC, err := r.Store.WriteCommit(&object.CommitObj{
		TreeHash:  treeHash,
		Parents:   []object.Hash{commitA},
		Author:    "Alice",
		Timestamp: 1700000002,
		Message:   "commit C",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Without shallow boundary, merge base should be A.
	base, err := r.FindMergeBase(commitB, commitC)
	if err != nil {
		t.Fatalf("FindMergeBase without shallow: %v", err)
	}
	if base != commitA {
		t.Fatalf("expected merge base %s, got %s", commitA, base)
	}

	// Now delete commit A from the store and mark it as shallow boundary.
	// This simulates a shallow clone where A is not available.
	objDir := filepath.Join(r.GraftDir, "objects")
	hashStr := string(commitA)
	objPath := filepath.Join(objDir, hashStr[:2], hashStr[2:])
	if err := os.Remove(objPath); err != nil {
		t.Fatal(err)
	}

	state := remote.NewShallowState()
	state.Add(commitA)
	if err := remote.WriteShallowFile(r.GraftDir, state); err != nil {
		t.Fatal(err)
	}

	// Re-open repo so shallow state is fresh.
	r, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}

	// With A removed and marked shallow, FindMergeBase should not error.
	// It may or may not find A as the base (the traversal sets may still
	// intersect at A from the parent lists of B and C), but the key
	// requirement is that it does not produce a hard error.
	_, err = r.FindMergeBase(commitB, commitC)
	if err != nil {
		t.Fatalf("FindMergeBase with shallow boundary: %v", err)
	}
}
