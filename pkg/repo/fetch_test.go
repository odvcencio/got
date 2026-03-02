package repo

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/odvcencio/graft/pkg/object"
)

// initRepoWithCommit creates a temp repo, writes a file, stages it, and
// commits. Returns the repo and the commit hash.
func initRepoWithCommit(t *testing.T, name string, content []byte, msg string) (*Repo, object.Hash) {
	t.Helper()
	r := initRepoWithFile(t, name, content)
	h, err := r.Commit(msg, "test-author")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	return r, h
}

// setupRemotePair creates a "remote" repo with a commit and a "local" repo
// configured with the remote as "origin". Returns (local, remote, commitHash).
func setupRemotePair(t *testing.T) (*Repo, *Repo, object.Hash) {
	t.Helper()

	// Create the "remote" repo with a commit.
	remoteRepo, commitHash := initRepoWithCommit(t,
		"hello.go",
		[]byte("package main\n\nfunc hello() {}\n"),
		"initial commit",
	)

	// Create the "local" repo.
	localDir := t.TempDir()
	localRepo, err := Init(localDir)
	if err != nil {
		t.Fatalf("Init local: %v", err)
	}

	// Configure the local repo to point at the remote.
	if err := localRepo.SetRemote("origin", remoteRepo.RootDir); err != nil {
		t.Fatalf("SetRemote: %v", err)
	}

	return localRepo, remoteRepo, commitHash
}

// TestFetch_UpdatesTrackingRefs verifies that Fetch creates remote-tracking
// refs under refs/remotes/<name>/ for every branch and tag in the remote.
func TestFetch_UpdatesTrackingRefs(t *testing.T) {
	local, _, commitHash := setupRemotePair(t)

	result, err := local.Fetch("origin")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	// The remote has refs/heads/main pointing at commitHash.
	trackingRef := "refs/remotes/origin/heads/main"
	got, err := local.ResolveRef(trackingRef)
	if err != nil {
		t.Fatalf("ResolveRef(%q): %v", trackingRef, err)
	}
	if got != commitHash {
		t.Errorf("tracking ref = %q, want %q", got, commitHash)
	}

	// Verify result metadata.
	if result.RemoteName != "origin" {
		t.Errorf("RemoteName = %q, want %q", result.RemoteName, "origin")
	}
	if result.ObjectCount == 0 {
		t.Error("ObjectCount should be > 0 after first fetch")
	}
	if len(result.UpdatedRefs) == 0 {
		t.Error("UpdatedRefs should not be empty after first fetch")
	}

	// Verify the specific ref update entry.
	found := false
	for _, ru := range result.UpdatedRefs {
		if ru.Name == trackingRef {
			found = true
			if ru.NewHash != commitHash {
				t.Errorf("RefUpdate.NewHash = %q, want %q", ru.NewHash, commitHash)
			}
			if ru.OldHash != "" {
				t.Errorf("RefUpdate.OldHash = %q, want empty (new ref)", ru.OldHash)
			}
		}
	}
	if !found {
		t.Errorf("no RefUpdate for %q", trackingRef)
	}
}

// TestFetch_DoesNotTouchWorkingTree verifies that Fetch does not modify the
// local working directory or HEAD.
func TestFetch_DoesNotTouchWorkingTree(t *testing.T) {
	local, _, _ := setupRemotePair(t)

	// Record working tree state before fetch.
	entriesBefore, err := os.ReadDir(local.RootDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	headBefore, _ := local.Head()

	if _, err := local.Fetch("origin"); err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	// HEAD should be unchanged (still unborn or whatever it was).
	headAfter, _ := local.Head()
	if headAfter != headBefore {
		t.Errorf("HEAD changed: %q -> %q", headBefore, headAfter)
	}

	// No new files should appear in the working tree (only .graft/ allowed).
	entriesAfter, err := os.ReadDir(local.RootDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	beforeSet := make(map[string]bool)
	for _, e := range entriesBefore {
		beforeSet[e.Name()] = true
	}
	for _, e := range entriesAfter {
		if !beforeSet[e.Name()] && e.Name() != ".graft" {
			t.Errorf("unexpected file in working tree after fetch: %q", e.Name())
		}
	}

	// The local branch refs/heads/main should NOT be created by fetch.
	_, err = local.ResolveRef("refs/heads/main")
	if err == nil {
		t.Error("Fetch should NOT create local branch refs/heads/main")
	}
}

// TestFetch_AlreadyUpToDate verifies that fetching an already-up-to-date repo
// is a no-op (no new refs updated, zero objects written).
func TestFetch_AlreadyUpToDate(t *testing.T) {
	local, _, _ := setupRemotePair(t)

	// First fetch.
	result1, err := local.Fetch("origin")
	if err != nil {
		t.Fatalf("first Fetch: %v", err)
	}
	if len(result1.UpdatedRefs) == 0 {
		t.Fatal("first fetch should update refs")
	}

	// Second fetch: should be a no-op.
	result2, err := local.Fetch("origin")
	if err != nil {
		t.Fatalf("second Fetch: %v", err)
	}
	if len(result2.UpdatedRefs) != 0 {
		t.Errorf("second fetch should update 0 refs, got %d", len(result2.UpdatedRefs))
	}
	// Object count may be 0 since nothing new to copy.
	if result2.ObjectCount != 0 {
		t.Errorf("second fetch ObjectCount = %d, want 0", result2.ObjectCount)
	}
}

// TestFetch_NoRemoteConfigured verifies that Fetch returns a clear error
// when the named remote is not configured.
func TestFetch_NoRemoteConfigured(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	_, err = r.Fetch("origin")
	if err == nil {
		t.Fatal("Fetch should fail when remote is not configured")
	}
	// Error should mention the remote name.
	if got := err.Error(); !contains(got, "origin") {
		t.Errorf("error should mention remote name, got: %s", got)
	}
}

// TestFetch_CopiesFullObjectGraph verifies that after fetch the local store
// contains the full object graph (commit, tree, blobs) from the remote.
func TestFetch_CopiesFullObjectGraph(t *testing.T) {
	local, remote, commitHash := setupRemotePair(t)

	if _, err := local.Fetch("origin"); err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	// Read the commit from local store.
	commit, err := local.Store.ReadCommit(commitHash)
	if err != nil {
		t.Fatalf("ReadCommit from local: %v", err)
	}

	// Verify tree is present.
	if !local.Store.Has(commit.TreeHash) {
		t.Errorf("local store missing tree %s", commit.TreeHash)
	}

	// Walk the tree and verify all blobs are present.
	tree, err := local.Store.ReadTree(commit.TreeHash)
	if err != nil {
		t.Fatalf("ReadTree: %v", err)
	}
	for _, entry := range tree.Entries {
		if entry.IsDir {
			if !local.Store.Has(entry.SubtreeHash) {
				t.Errorf("local store missing subtree %s", entry.SubtreeHash)
			}
		} else {
			if !local.Store.Has(entry.BlobHash) {
				t.Errorf("local store missing blob %s for %s", entry.BlobHash, entry.Name)
			}
		}
	}

	// Verify the remote commit is readable too.
	remoteCommit, err := remote.Store.ReadCommit(commitHash)
	if err != nil {
		t.Fatalf("ReadCommit from remote: %v", err)
	}
	if commit.Message != remoteCommit.Message {
		t.Errorf("commit message mismatch: local=%q, remote=%q", commit.Message, remoteCommit.Message)
	}
}

// TestFetch_MultipleCommits verifies fetch after the remote advances with
// additional commits.
func TestFetch_MultipleCommits(t *testing.T) {
	local, remoteRepo, _ := setupRemotePair(t)

	// First fetch.
	if _, err := local.Fetch("origin"); err != nil {
		t.Fatalf("first Fetch: %v", err)
	}

	// Advance the remote with a second commit.
	if err := os.WriteFile(
		filepath.Join(remoteRepo.RootDir, "hello.go"),
		[]byte("package main\n\nfunc hello() { println(\"v2\") }\n"),
		0o644,
	); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := remoteRepo.Add([]string{"hello.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	newHash, err := remoteRepo.Commit("second commit", "test-author")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// Second fetch should pick up the new commit.
	result, err := local.Fetch("origin")
	if err != nil {
		t.Fatalf("second Fetch: %v", err)
	}
	if result.ObjectCount == 0 {
		t.Error("expected new objects from second fetch")
	}
	if len(result.UpdatedRefs) == 0 {
		t.Error("expected updated refs from second fetch")
	}

	// Tracking ref should now point at the new commit.
	got, err := local.ResolveRef("refs/remotes/origin/heads/main")
	if err != nil {
		t.Fatalf("ResolveRef: %v", err)
	}
	if got != newHash {
		t.Errorf("tracking ref = %q, want %q", got, newHash)
	}

	// New commit should be readable from local store.
	c, err := local.Store.ReadCommit(newHash)
	if err != nil {
		t.Fatalf("ReadCommit: %v", err)
	}
	if c.Message != "second commit" {
		t.Errorf("commit message = %q, want %q", c.Message, "second commit")
	}
}

// TestFetch_DefaultRemoteName verifies that passing "" defaults to "origin".
func TestFetch_DefaultRemoteName(t *testing.T) {
	local, _, _ := setupRemotePair(t)

	result, err := local.Fetch("")
	if err != nil {
		t.Fatalf("Fetch with empty name: %v", err)
	}
	if result.RemoteName != "origin" {
		t.Errorf("RemoteName = %q, want %q", result.RemoteName, "origin")
	}
}

// TestFetch_RemoteTags verifies that remote tags are fetched under
// refs/remotes/<name>/tags/<tag>.
func TestFetch_RemoteTags(t *testing.T) {
	local, remoteRepo, commitHash := setupRemotePair(t)

	// Create a tag in the remote repo.
	if err := remoteRepo.UpdateRef("refs/tags/v1.0", commitHash); err != nil {
		t.Fatalf("create remote tag: %v", err)
	}

	result, err := local.Fetch("origin")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	// Verify the tag tracking ref exists.
	trackingRef := "refs/remotes/origin/tags/v1.0"
	got, err := local.ResolveRef(trackingRef)
	if err != nil {
		t.Fatalf("ResolveRef(%q): %v", trackingRef, err)
	}
	if got != commitHash {
		t.Errorf("tag tracking ref = %q, want %q", got, commitHash)
	}

	// Verify it appears in the result.
	found := false
	for _, ru := range result.UpdatedRefs {
		if ru.Name == trackingRef {
			found = true
		}
	}
	if !found {
		t.Errorf("tag tracking ref %q not in UpdatedRefs", trackingRef)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && containsImpl(s, sub)
}

func containsImpl(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
