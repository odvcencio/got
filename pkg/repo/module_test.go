package repo

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/odvcencio/graft/pkg/object"
)

func createTestRepo(t *testing.T) *Repo {
	t.Helper()
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init(%q): %v", dir, err)
	}
	return r
}

func TestRepo_ListModules_Empty(t *testing.T) {
	r := createTestRepo(t)

	modules, err := r.ListModules()
	if err != nil {
		t.Fatalf("ListModules: %v", err)
	}
	if modules != nil {
		t.Errorf("ListModules on repo without .graftmodules = %v, want nil", modules)
	}
}

func TestRepo_AddModule(t *testing.T) {
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

	// Verify .graftmodules was written.
	graftmodulesPath := filepath.Join(r.RootDir, ".graftmodules")
	if _, err := os.Stat(graftmodulesPath); err != nil {
		t.Fatalf(".graftmodules should exist: %v", err)
	}

	// Verify the file can be parsed back.
	entries, err := r.ReadGraftModulesFile()
	if err != nil {
		t.Fatalf("ReadGraftModulesFile: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Name != "libs/core" {
		t.Errorf("Name = %q, want %q", entries[0].Name, "libs/core")
	}
	if entries[0].URL != "https://example.com/core.git" {
		t.Errorf("URL = %q, want %q", entries[0].URL, "https://example.com/core.git")
	}
	if entries[0].Path != "vendor/core" {
		t.Errorf("Path = %q, want %q", entries[0].Path, "vendor/core")
	}
	if entries[0].Track != "main" {
		t.Errorf("Track = %q, want %q", entries[0].Track, "main")
	}

	// Verify metadata directory was created.
	metaDir := r.ModuleMetadataDir("libs/core")
	assertDir(t, metaDir)
	assertDir(t, filepath.Join(metaDir, "refs"))
}

func TestRepo_AddModule_DuplicateName(t *testing.T) {
	r := createTestRepo(t)

	entry := ModuleEntry{
		Name: "mymod",
		URL:  "https://example.com/a.git",
		Path: "vendor/a",
	}
	if err := r.AddModuleEntry(entry); err != nil {
		t.Fatalf("first AddModuleEntry: %v", err)
	}

	dup := ModuleEntry{
		Name: "mymod",
		URL:  "https://example.com/b.git",
		Path: "vendor/b",
	}
	err := r.AddModuleEntry(dup)
	if err == nil {
		t.Fatal("AddModuleEntry with duplicate name should fail, got nil error")
	}
}

func TestRepo_AddModule_DuplicatePath(t *testing.T) {
	r := createTestRepo(t)

	entry := ModuleEntry{
		Name: "mod-a",
		URL:  "https://example.com/a.git",
		Path: "vendor/shared",
	}
	if err := r.AddModuleEntry(entry); err != nil {
		t.Fatalf("first AddModuleEntry: %v", err)
	}

	dup := ModuleEntry{
		Name: "mod-b",
		URL:  "https://example.com/b.git",
		Path: "vendor/shared",
	}
	err := r.AddModuleEntry(dup)
	if err == nil {
		t.Fatal("AddModuleEntry with duplicate path should fail, got nil error")
	}
}

func TestRepo_RemoveModule(t *testing.T) {
	r := createTestRepo(t)

	entry := ModuleEntry{
		Name: "removeme",
		URL:  "https://example.com/rm.git",
		Path: "vendor/rm",
	}
	if err := r.AddModuleEntry(entry); err != nil {
		t.Fatalf("AddModuleEntry: %v", err)
	}

	// Also write a lock entry so we can verify it gets cleaned up.
	if err := r.UpdateModuleLock("removeme", object.Hash("abc123"), "https://resolved.example.com/rm.git"); err != nil {
		t.Fatalf("UpdateModuleLock: %v", err)
	}

	metaDir := r.ModuleMetadataDir("removeme")
	assertDir(t, metaDir)

	if err := r.RemoveModuleEntry("removeme"); err != nil {
		t.Fatalf("RemoveModuleEntry: %v", err)
	}

	// .graftmodules should have no entries.
	entries, err := r.ReadGraftModulesFile()
	if err != nil {
		t.Fatalf("ReadGraftModulesFile: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries after remove, got %d", len(entries))
	}

	// Lock file should not contain the removed module.
	lock, err := r.ReadModuleLock()
	if err != nil {
		t.Fatalf("ReadModuleLock: %v", err)
	}
	if lock != nil {
		if _, ok := lock.Modules["removeme"]; ok {
			t.Error("lock file still contains removed module")
		}
	}

	// Metadata directory should be gone.
	if _, err := os.Stat(metaDir); !os.IsNotExist(err) {
		t.Errorf("metadata dir should not exist after remove, stat err = %v", err)
	}
}

func TestRepo_RemoveModule_NotFound(t *testing.T) {
	r := createTestRepo(t)

	// Write an empty .graftmodules so the file exists but has no entries.
	if err := r.WriteGraftModulesFile(nil); err != nil {
		t.Fatalf("WriteGraftModulesFile: %v", err)
	}

	err := r.RemoveModuleEntry("nonexistent")
	if err == nil {
		t.Fatal("RemoveModuleEntry for nonexistent module should fail, got nil error")
	}
}

func TestRepo_UpdateModuleLock(t *testing.T) {
	r := createTestRepo(t)

	entry := ModuleEntry{
		Name:  "locktest",
		URL:   "https://example.com/lock.git",
		Path:  "vendor/lock",
		Track: "develop",
	}
	if err := r.AddModuleEntry(entry); err != nil {
		t.Fatalf("AddModuleEntry: %v", err)
	}

	commit := object.Hash("deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef")
	resolvedURL := "https://resolved.example.com/lock.git"

	if err := r.UpdateModuleLock("locktest", commit, resolvedURL); err != nil {
		t.Fatalf("UpdateModuleLock: %v", err)
	}

	// Read back and verify.
	lockPath := filepath.Join(r.RootDir, ".graftmodules.lock")
	data, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("ReadFile lock: %v", err)
	}

	var lock ModuleLock
	if err := json.Unmarshal(data, &lock); err != nil {
		t.Fatalf("Unmarshal lock: %v", err)
	}

	le, ok := lock.Modules["locktest"]
	if !ok {
		t.Fatal("lock file does not contain 'locktest' entry")
	}
	if le.Commit != commit {
		t.Errorf("Commit = %q, want %q", le.Commit, commit)
	}
	if le.URL != resolvedURL {
		t.Errorf("URL = %q, want %q", le.URL, resolvedURL)
	}
	if le.Track != "develop" {
		t.Errorf("Track = %q, want %q", le.Track, "develop")
	}
	if le.Pin != "" {
		t.Errorf("Pin = %q, want empty", le.Pin)
	}
}

func TestRepo_UpdateModuleLock_NotFound(t *testing.T) {
	r := createTestRepo(t)

	// No .graftmodules at all.
	err := r.UpdateModuleLock("ghost", object.Hash("aaa"), "https://example.com")
	if err == nil {
		t.Fatal("UpdateModuleLock for nonexistent module should fail, got nil error")
	}
}

func TestRepo_ModuleMetadataDir(t *testing.T) {
	r := createTestRepo(t)

	got := r.ModuleMetadataDir("libs/core")
	want := filepath.Join(r.GraftDir, "modules", "libs/core")
	if got != want {
		t.Errorf("ModuleMetadataDir = %q, want %q", got, want)
	}
}

func TestRepo_GetModule(t *testing.T) {
	r := createTestRepo(t)

	entry := ModuleEntry{
		Name:  "getme",
		URL:   "https://example.com/getme.git",
		Path:  "vendor/getme",
		Track: "main",
	}
	if err := r.AddModuleEntry(entry); err != nil {
		t.Fatalf("AddModuleEntry: %v", err)
	}

	commit := object.Hash("1111111111111111111111111111111111111111111111111111111111111111")
	if err := r.UpdateModuleLock("getme", commit, "https://resolved.example.com/getme.git"); err != nil {
		t.Fatalf("UpdateModuleLock: %v", err)
	}

	m, err := r.GetModule("getme")
	if err != nil {
		t.Fatalf("GetModule: %v", err)
	}
	if m.Name != "getme" {
		t.Errorf("Name = %q, want %q", m.Name, "getme")
	}
	if m.Commit != commit {
		t.Errorf("Commit = %q, want %q", m.Commit, commit)
	}
	if m.ResolvedURL != "https://resolved.example.com/getme.git" {
		t.Errorf("ResolvedURL = %q, want %q", m.ResolvedURL, "https://resolved.example.com/getme.git")
	}
}

func TestRepo_GetModule_NotFound(t *testing.T) {
	r := createTestRepo(t)

	_, err := r.GetModule("nonexistent")
	if err == nil {
		t.Fatal("GetModule for nonexistent module should fail, got nil error")
	}
}

func TestRepo_ListModules_WithLock(t *testing.T) {
	r := createTestRepo(t)

	entry := ModuleEntry{
		Name: "joined",
		URL:  "https://example.com/joined.git",
		Path: "vendor/joined",
		Pin:  "v1.0.0",
	}
	if err := r.AddModuleEntry(entry); err != nil {
		t.Fatalf("AddModuleEntry: %v", err)
	}

	commit := object.Hash("2222222222222222222222222222222222222222222222222222222222222222")
	if err := r.UpdateModuleLock("joined", commit, "https://resolved.example.com/joined.git"); err != nil {
		t.Fatalf("UpdateModuleLock: %v", err)
	}

	modules, err := r.ListModules()
	if err != nil {
		t.Fatalf("ListModules: %v", err)
	}
	if len(modules) != 1 {
		t.Fatalf("expected 1 module, got %d", len(modules))
	}
	if modules[0].Name != "joined" {
		t.Errorf("Name = %q, want %q", modules[0].Name, "joined")
	}
	if modules[0].Commit != commit {
		t.Errorf("Commit = %q, want %q", modules[0].Commit, commit)
	}
	if modules[0].ResolvedURL != "https://resolved.example.com/joined.git" {
		t.Errorf("ResolvedURL = %q, want %q", modules[0].ResolvedURL, "https://resolved.example.com/joined.git")
	}
	if modules[0].Pin != "v1.0.0" {
		t.Errorf("Pin = %q, want %q", modules[0].Pin, "v1.0.0")
	}
}
