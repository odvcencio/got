package repo

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/odvcencio/graft/pkg/object"
)

func TestModuleStatus_UpToDate(t *testing.T) {
	r := createTestRepo(t)

	entry := ModuleEntry{
		Name:  "libs/core",
		URL:   "https://example.com/core.git",
		Path:  "vendor/core",
		Track: "main",
	}
	if err := r.AddModuleEntry(entry); err != nil {
		t.Fatalf("AddModuleEntry: %v", err)
	}

	commit := object.Hash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err := r.UpdateModuleLock("libs/core", commit, "https://example.com/core.git"); err != nil {
		t.Fatalf("UpdateModuleLock: %v", err)
	}

	// Simulate sync by writing HEAD in the module metadata directory.
	headPath := filepath.Join(r.ModuleMetadataDir("libs/core"), "HEAD")
	if err := os.WriteFile(headPath, []byte(string(commit)+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile HEAD: %v", err)
	}

	statuses, err := r.ModuleStatus()
	if err != nil {
		t.Fatalf("ModuleStatus: %v", err)
	}
	if len(statuses) != 1 {
		t.Fatalf("expected 1 status entry, got %d", len(statuses))
	}

	s := statuses[0]
	if s.Name != "libs/core" {
		t.Errorf("Name = %q, want %q", s.Name, "libs/core")
	}
	if s.Path != "vendor/core" {
		t.Errorf("Path = %q, want %q", s.Path, "vendor/core")
	}
	if s.Track != "main" {
		t.Errorf("Track = %q, want %q", s.Track, "main")
	}
	if s.LockedCommit != commit {
		t.Errorf("LockedCommit = %q, want %q", s.LockedCommit, commit)
	}
	if s.HeadCommit != commit {
		t.Errorf("HeadCommit = %q, want %q", s.HeadCommit, commit)
	}
	if !s.Synced {
		t.Error("Synced = false, want true")
	}
}

func TestModuleStatus_NotSynced(t *testing.T) {
	r := createTestRepo(t)

	entry := ModuleEntry{
		Name:  "libs/ui",
		URL:   "https://example.com/ui.git",
		Path:  "vendor/ui",
		Track: "develop",
	}
	if err := r.AddModuleEntry(entry); err != nil {
		t.Fatalf("AddModuleEntry: %v", err)
	}

	commit := object.Hash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	if err := r.UpdateModuleLock("libs/ui", commit, "https://example.com/ui.git"); err != nil {
		t.Fatalf("UpdateModuleLock: %v", err)
	}

	// Do NOT write a HEAD file — simulating a module that is locked but not synced.

	statuses, err := r.ModuleStatus()
	if err != nil {
		t.Fatalf("ModuleStatus: %v", err)
	}
	if len(statuses) != 1 {
		t.Fatalf("expected 1 status entry, got %d", len(statuses))
	}

	s := statuses[0]
	if s.Name != "libs/ui" {
		t.Errorf("Name = %q, want %q", s.Name, "libs/ui")
	}
	if s.LockedCommit != commit {
		t.Errorf("LockedCommit = %q, want %q", s.LockedCommit, commit)
	}
	if s.HeadCommit != "" {
		t.Errorf("HeadCommit = %q, want empty", s.HeadCommit)
	}
	if s.Synced {
		t.Error("Synced = true, want false")
	}
}

func TestModuleStatus_NoLock(t *testing.T) {
	r := createTestRepo(t)

	entry := ModuleEntry{
		Name: "libs/util",
		URL:  "https://example.com/util.git",
		Path: "vendor/util",
		Pin:  "v2.0.0",
	}
	if err := r.AddModuleEntry(entry); err != nil {
		t.Fatalf("AddModuleEntry: %v", err)
	}

	// Do NOT lock — no UpdateModuleLock call.

	statuses, err := r.ModuleStatus()
	if err != nil {
		t.Fatalf("ModuleStatus: %v", err)
	}
	if len(statuses) != 1 {
		t.Fatalf("expected 1 status entry, got %d", len(statuses))
	}

	s := statuses[0]
	if s.Name != "libs/util" {
		t.Errorf("Name = %q, want %q", s.Name, "libs/util")
	}
	if s.Path != "vendor/util" {
		t.Errorf("Path = %q, want %q", s.Path, "vendor/util")
	}
	if s.Pin != "v2.0.0" {
		t.Errorf("Pin = %q, want %q", s.Pin, "v2.0.0")
	}
	if s.LockedCommit != "" {
		t.Errorf("LockedCommit = %q, want empty", s.LockedCommit)
	}
	if s.HeadCommit != "" {
		t.Errorf("HeadCommit = %q, want empty", s.HeadCommit)
	}
	if s.Synced {
		t.Error("Synced = true, want false (no lock)")
	}
}
