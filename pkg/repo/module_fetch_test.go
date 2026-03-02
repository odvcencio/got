package repo

import (
	"testing"

	"github.com/odvcencio/graft/pkg/object"
)

func TestResolveModuleTarget_Track(t *testing.T) {
	m := &Module{
		ModuleEntry: ModuleEntry{
			Name:  "trackmod",
			URL:   "https://example.com/trackmod.git",
			Path:  "vendor/trackmod",
			Track: "main",
		},
	}

	refs := map[string]object.Hash{
		"refs/heads/main":    "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111",
		"refs/heads/develop": "bbbb2222bbbb2222bbbb2222bbbb2222bbbb2222bbbb2222bbbb2222bbbb2222",
		"refs/tags/v1.0.0":   "cccc3333cccc3333cccc3333cccc3333cccc3333cccc3333cccc3333cccc3333",
	}

	h, err := resolveModuleTarget(m, refs)
	if err != nil {
		t.Fatalf("resolveModuleTarget: %v", err)
	}
	if h != "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111" {
		t.Errorf("hash = %q, want main branch hash", h)
	}
}

func TestResolveModuleTarget_TrackMissing(t *testing.T) {
	m := &Module{
		ModuleEntry: ModuleEntry{
			Name:  "trackmod",
			URL:   "https://example.com/trackmod.git",
			Path:  "vendor/trackmod",
			Track: "nonexistent",
		},
	}

	refs := map[string]object.Hash{
		"refs/heads/main": "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111",
	}

	_, err := resolveModuleTarget(m, refs)
	if err == nil {
		t.Fatal("expected error for missing tracking branch, got nil")
	}
}

func TestResolveModuleTarget_PinTag(t *testing.T) {
	m := &Module{
		ModuleEntry: ModuleEntry{
			Name: "pinmod",
			URL:  "https://example.com/pinmod.git",
			Path: "vendor/pinmod",
			Pin:  "v1.0.0",
		},
	}

	refs := map[string]object.Hash{
		"refs/heads/main":  "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111",
		"refs/tags/v1.0.0": "cccc3333cccc3333cccc3333cccc3333cccc3333cccc3333cccc3333cccc3333",
	}

	h, err := resolveModuleTarget(m, refs)
	if err != nil {
		t.Fatalf("resolveModuleTarget: %v", err)
	}
	if h != "cccc3333cccc3333cccc3333cccc3333cccc3333cccc3333cccc3333cccc3333" {
		t.Errorf("hash = %q, want v1.0.0 tag hash", h)
	}
}

func TestResolveModuleTarget_PinCommitHash(t *testing.T) {
	commitHash := "dddd4444dddd4444dddd4444dddd4444dddd4444dddd4444dddd4444dddd4444"

	m := &Module{
		ModuleEntry: ModuleEntry{
			Name: "pinmod",
			URL:  "https://example.com/pinmod.git",
			Path: "vendor/pinmod",
			Pin:  commitHash,
		},
	}

	// No matching tag in refs -- should fall back to literal hash.
	refs := map[string]object.Hash{
		"refs/heads/main": "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111",
	}

	h, err := resolveModuleTarget(m, refs)
	if err != nil {
		t.Fatalf("resolveModuleTarget: %v", err)
	}
	if h != object.Hash(commitHash) {
		t.Errorf("hash = %q, want commit hash %q", h, commitHash)
	}
}

func TestResolveModuleTarget_PinShortHash(t *testing.T) {
	m := &Module{
		ModuleEntry: ModuleEntry{
			Name: "pinmod",
			URL:  "https://example.com/pinmod.git",
			Path: "vendor/pinmod",
			Pin:  "abc",
		},
	}

	refs := map[string]object.Hash{}

	_, err := resolveModuleTarget(m, refs)
	if err == nil {
		t.Fatal("expected error for too-short pin hash, got nil")
	}
}

func TestResolveModuleTarget_NeitherTrackNorPin(t *testing.T) {
	m := &Module{
		ModuleEntry: ModuleEntry{
			Name: "noconfig",
			URL:  "https://example.com/noconfig.git",
			Path: "vendor/noconfig",
		},
	}

	refs := map[string]object.Hash{}

	_, err := resolveModuleTarget(m, refs)
	if err == nil {
		t.Fatal("expected error for module with no track/pin, got nil")
	}
}

func TestModuleFetch_LockUpdateWithTrack(t *testing.T) {
	r := createTestRepo(t)

	entry := ModuleEntry{
		Name:  "fetchmod",
		URL:   "https://example.com/fetchmod.git",
		Path:  "vendor/fetchmod",
		Track: "main",
	}
	if err := r.AddModuleEntry(entry); err != nil {
		t.Fatalf("AddModuleEntry: %v", err)
	}

	// Simulate a previous lock state.
	oldCommit := object.Hash("1111111111111111111111111111111111111111111111111111111111111111")
	if err := r.UpdateModuleLock("fetchmod", oldCommit, "https://example.com/fetchmod.git"); err != nil {
		t.Fatalf("UpdateModuleLock (old): %v", err)
	}

	// Now update the lock to a new commit, simulating what ModuleFetchAndUpdate
	// does after fetching objects.
	newCommit := object.Hash("2222222222222222222222222222222222222222222222222222222222222222")
	resolvedURL := "https://resolved.example.com/fetchmod.git"
	if err := r.UpdateModuleLock("fetchmod", newCommit, resolvedURL); err != nil {
		t.Fatalf("UpdateModuleLock (new): %v", err)
	}

	// Read back and verify the lock was updated correctly.
	lock, err := r.ReadModuleLock()
	if err != nil {
		t.Fatalf("ReadModuleLock: %v", err)
	}
	if lock == nil {
		t.Fatal("lock is nil")
	}

	le, ok := lock.Modules["fetchmod"]
	if !ok {
		t.Fatal("lock does not contain fetchmod")
	}
	if le.Commit != newCommit {
		t.Errorf("Commit = %q, want %q", le.Commit, newCommit)
	}
	if le.URL != resolvedURL {
		t.Errorf("URL = %q, want %q", le.URL, resolvedURL)
	}
	if le.Track != "main" {
		t.Errorf("Track = %q, want %q", le.Track, "main")
	}
	if le.Pin != "" {
		t.Errorf("Pin = %q, want empty", le.Pin)
	}
}

func TestModuleFetch_LockUpdateWithPin(t *testing.T) {
	r := createTestRepo(t)

	entry := ModuleEntry{
		Name: "pinmod",
		URL:  "https://example.com/pinmod.git",
		Path: "vendor/pinmod",
		Pin:  "v2.0.0",
	}
	if err := r.AddModuleEntry(entry); err != nil {
		t.Fatalf("AddModuleEntry: %v", err)
	}

	commit := object.Hash("3333333333333333333333333333333333333333333333333333333333333333")
	resolvedURL := "https://resolved.example.com/pinmod.git"
	if err := r.UpdateModuleLock("pinmod", commit, resolvedURL); err != nil {
		t.Fatalf("UpdateModuleLock: %v", err)
	}

	// Read back and verify pin field is preserved.
	lock, err := r.ReadModuleLock()
	if err != nil {
		t.Fatalf("ReadModuleLock: %v", err)
	}
	if lock == nil {
		t.Fatal("lock is nil")
	}

	le, ok := lock.Modules["pinmod"]
	if !ok {
		t.Fatal("lock does not contain pinmod")
	}
	if le.Commit != commit {
		t.Errorf("Commit = %q, want %q", le.Commit, commit)
	}
	if le.URL != resolvedURL {
		t.Errorf("URL = %q, want %q", le.URL, resolvedURL)
	}
	if le.Pin != "v2.0.0" {
		t.Errorf("Pin = %q, want %q", le.Pin, "v2.0.0")
	}
	if le.Track != "" {
		t.Errorf("Track = %q, want empty", le.Track)
	}
}
