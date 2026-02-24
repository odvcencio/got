package repo

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/odvcencio/got/pkg/object"
)

// Test 1: Clean repo — add file, then Status shows it as staged new
// (IndexStatus=New, WorkStatus=Clean).
func TestStatus_StagedNew_WorkClean(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Write and stage a file.
	content := []byte("package main\n\nfunc hello() {}\n")
	if err := os.WriteFile(filepath.Join(dir, "main.go"), content, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	entries, err := r.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}

	// Should have exactly one entry for main.go.
	var found *StatusEntry
	for i := range entries {
		if entries[i].Path == "main.go" {
			found = &entries[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("Status missing entry for main.go; got %d entries", len(entries))
	}

	if found.IndexStatus != StatusNew {
		t.Errorf("IndexStatus = %d, want StatusNew (%d)", found.IndexStatus, StatusNew)
	}
	if found.WorkStatus != StatusClean {
		t.Errorf("WorkStatus = %d, want StatusClean (%d)", found.WorkStatus, StatusClean)
	}
}

// Test 2: Untracked file — create file without adding → Untracked.
func TestStatus_Untracked(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Write a file but do NOT add it.
	content := []byte("some data\n")
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), content, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	entries, err := r.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}

	var found *StatusEntry
	for i := range entries {
		if entries[i].Path == "notes.txt" {
			found = &entries[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("Status missing entry for notes.txt; got %d entries", len(entries))
	}

	if found.IndexStatus != StatusUntracked {
		t.Errorf("IndexStatus = %d, want StatusUntracked (%d)", found.IndexStatus, StatusUntracked)
	}
	if found.WorkStatus != StatusUntracked {
		t.Errorf("WorkStatus = %d, want StatusUntracked (%d)", found.WorkStatus, StatusUntracked)
	}
}

// Test 3: Modified after staging — add file, modify it, Status shows
// WorkStatus=Dirty.
func TestStatus_DirtyAfterStaging(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	fpath := filepath.Join(dir, "main.go")
	original := []byte("package main\n\nfunc hello() {}\n")
	if err := os.WriteFile(fpath, original, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Modify the file after staging.
	modified := []byte("package main\n\nfunc hello() { println(\"changed\") }\n")
	if err := os.WriteFile(fpath, modified, 0o644); err != nil {
		t.Fatalf("write modified: %v", err)
	}

	entries, err := r.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}

	var found *StatusEntry
	for i := range entries {
		if entries[i].Path == "main.go" {
			found = &entries[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("Status missing entry for main.go; got %d entries", len(entries))
	}

	if found.IndexStatus != StatusNew {
		t.Errorf("IndexStatus = %d, want StatusNew (%d)", found.IndexStatus, StatusNew)
	}
	if found.WorkStatus != StatusDirty {
		t.Errorf("WorkStatus = %d, want StatusDirty (%d)", found.WorkStatus, StatusDirty)
	}
}

// Test 4: Deleted file — add file, delete from disk, Status shows
// WorkStatus appropriately.
func TestStatus_DeletedFromDisk(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	fpath := filepath.Join(dir, "gone.txt")
	if err := os.WriteFile(fpath, []byte("will be deleted\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := r.Add([]string{"gone.txt"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Delete the file from disk.
	if err := os.Remove(fpath); err != nil {
		t.Fatalf("remove: %v", err)
	}

	entries, err := r.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}

	var found *StatusEntry
	for i := range entries {
		if entries[i].Path == "gone.txt" {
			found = &entries[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("Status missing entry for gone.txt; got %d entries", len(entries))
	}

	// File is staged (new in index since no HEAD), but deleted on disk.
	if found.IndexStatus != StatusNew {
		t.Errorf("IndexStatus = %d, want StatusNew (%d)", found.IndexStatus, StatusNew)
	}
	if found.WorkStatus != StatusDeleted {
		t.Errorf("WorkStatus = %d, want StatusDeleted (%d)", found.WorkStatus, StatusDeleted)
	}
}

// Test 5: Multiple files — mix of untracked, staged, modified.
func TestStatus_MultipleFiles(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Create subdirectory.
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// staged_clean.txt — staged, not modified on disk
	if err := os.WriteFile(filepath.Join(dir, "staged_clean.txt"), []byte("clean\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := r.Add([]string{"staged_clean.txt"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// staged_dirty.txt — staged, then modified on disk
	if err := os.WriteFile(filepath.Join(dir, "staged_dirty.txt"), []byte("original\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := r.Add([]string{"staged_dirty.txt"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "staged_dirty.txt"), []byte("modified\n"), 0o644); err != nil {
		t.Fatalf("write modified: %v", err)
	}

	// untracked.txt — exists on disk but never added
	if err := os.WriteFile(filepath.Join(dir, "untracked.txt"), []byte("not staged\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// sub/nested.txt — untracked in subdirectory
	if err := os.WriteFile(filepath.Join(dir, "sub", "nested.txt"), []byte("nested\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	entries, err := r.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}

	// Build a lookup map.
	byPath := make(map[string]*StatusEntry, len(entries))
	for i := range entries {
		byPath[entries[i].Path] = &entries[i]
	}

	// staged_clean.txt: IndexStatus=New, WorkStatus=Clean
	if e, ok := byPath["staged_clean.txt"]; !ok {
		t.Error("missing staged_clean.txt")
	} else {
		if e.IndexStatus != StatusNew {
			t.Errorf("staged_clean.txt IndexStatus = %d, want StatusNew (%d)", e.IndexStatus, StatusNew)
		}
		if e.WorkStatus != StatusClean {
			t.Errorf("staged_clean.txt WorkStatus = %d, want StatusClean (%d)", e.WorkStatus, StatusClean)
		}
	}

	// staged_dirty.txt: IndexStatus=New, WorkStatus=Dirty
	if e, ok := byPath["staged_dirty.txt"]; !ok {
		t.Error("missing staged_dirty.txt")
	} else {
		if e.IndexStatus != StatusNew {
			t.Errorf("staged_dirty.txt IndexStatus = %d, want StatusNew (%d)", e.IndexStatus, StatusNew)
		}
		if e.WorkStatus != StatusDirty {
			t.Errorf("staged_dirty.txt WorkStatus = %d, want StatusDirty (%d)", e.WorkStatus, StatusDirty)
		}
	}

	// untracked.txt: both Untracked
	if e, ok := byPath["untracked.txt"]; !ok {
		t.Error("missing untracked.txt")
	} else {
		if e.IndexStatus != StatusUntracked {
			t.Errorf("untracked.txt IndexStatus = %d, want StatusUntracked (%d)", e.IndexStatus, StatusUntracked)
		}
	}

	// sub/nested.txt: both Untracked
	if e, ok := byPath["sub/nested.txt"]; !ok {
		t.Error("missing sub/nested.txt")
	} else {
		if e.IndexStatus != StatusUntracked {
			t.Errorf("sub/nested.txt IndexStatus = %d, want StatusUntracked (%d)", e.IndexStatus, StatusUntracked)
		}
	}

	// Entries should be sorted by path.
	for i := 1; i < len(entries); i++ {
		if entries[i].Path < entries[i-1].Path {
			t.Errorf("entries not sorted: [%d]=%q comes after [%d]=%q",
				i-1, entries[i-1].Path, i, entries[i].Path)
		}
	}
}

func TestStatus_RefreshesLegacySecondResolutionStatCache(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	filePath := filepath.Join(dir, "main.go")
	if err := os.WriteFile(filePath, []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	stg, err := r.ReadStaging()
	if err != nil {
		t.Fatalf("ReadStaging: %v", err)
	}
	entry := stg.Entries["main.go"]
	if entry == nil {
		t.Fatal("missing staging entry for main.go")
	}
	if entry.ModTime > 1_000_000_000_000 {
		entry.ModTime = entry.ModTime / 1_000_000_000
	}
	if err := r.WriteStaging(stg); err != nil {
		t.Fatalf("WriteStaging: %v", err)
	}

	entries, err := r.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected at least one status entry")
	}

	stg, err = r.ReadStaging()
	if err != nil {
		t.Fatalf("ReadStaging: %v", err)
	}
	refreshed := stg.Entries["main.go"]
	if refreshed == nil {
		t.Fatal("missing refreshed staging entry for main.go")
	}
	if refreshed.ModTime <= 1_000_000_000_000 {
		t.Fatalf("expected nanosecond staging modtime after status refresh, got %d", refreshed.ModTime)
	}
}

func TestStatus_DirtyWhenExecutableBitChangesOnDisk(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	path := filepath.Join(dir, "run.sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho hi\n"), 0o644); err != nil {
		t.Fatalf("write run.sh: %v", err)
	}
	if err := r.Add([]string{"run.sh"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := r.Commit("add script", "test-author"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatalf("chmod run.sh: %v", err)
	}

	entries, err := r.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}

	var found *StatusEntry
	for i := range entries {
		if entries[i].Path == "run.sh" {
			found = &entries[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("missing run.sh in status")
	}
	if found.IndexStatus != StatusClean {
		t.Fatalf("IndexStatus = %d, want %d", found.IndexStatus, StatusClean)
	}
	if found.WorkStatus != StatusDirty {
		t.Fatalf("WorkStatus = %d, want %d", found.WorkStatus, StatusDirty)
	}
}

func TestStatus_IndexModifiedWhenExecutableBitStaged(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	path := filepath.Join(dir, "run.sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho hi\n"), 0o644); err != nil {
		t.Fatalf("write run.sh: %v", err)
	}
	if err := r.Add([]string{"run.sh"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := r.Commit("add script", "test-author"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatalf("chmod run.sh: %v", err)
	}
	if err := r.Add([]string{"run.sh"}); err != nil {
		t.Fatalf("Add executable: %v", err)
	}

	entries, err := r.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}

	var found *StatusEntry
	for i := range entries {
		if entries[i].Path == "run.sh" {
			found = &entries[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("missing run.sh in status")
	}
	if found.IndexStatus != StatusModified {
		t.Fatalf("IndexStatus = %d, want %d", found.IndexStatus, StatusModified)
	}
	if found.WorkStatus != StatusClean {
		t.Fatalf("WorkStatus = %d, want %d", found.WorkStatus, StatusClean)
	}
}

func TestStatus_DetectsIndexRename(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	oldPath := filepath.Join(dir, "old.txt")
	if err := os.WriteFile(oldPath, []byte("rename me\n"), 0o644); err != nil {
		t.Fatalf("write old.txt: %v", err)
	}
	if err := r.Add([]string{"old.txt"}); err != nil {
		t.Fatalf("Add old.txt: %v", err)
	}
	if _, err := r.Commit("initial", "test-author"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	newPath := filepath.Join(dir, "new.txt")
	if err := os.Rename(oldPath, newPath); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if err := r.Add([]string{"new.txt"}); err != nil {
		t.Fatalf("Add new.txt: %v", err)
	}
	if err := r.Remove([]string{"old.txt"}, true); err != nil {
		t.Fatalf("Remove old.txt --cached: %v", err)
	}

	entries, err := r.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}

	var found *StatusEntry
	for i := range entries {
		if entries[i].Path == "new.txt" {
			found = &entries[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("missing new.txt in status")
	}
	if found.IndexStatus != StatusRenamed {
		t.Fatalf("IndexStatus = %d, want %d", found.IndexStatus, StatusRenamed)
	}
	if found.RenamedFrom != "old.txt" {
		t.Fatalf("RenamedFrom = %q, want %q", found.RenamedFrom, "old.txt")
	}
	if found.WorkStatus != StatusClean {
		t.Fatalf("WorkStatus = %d, want %d", found.WorkStatus, StatusClean)
	}

	for _, e := range entries {
		if e.Path == "old.txt" && e.IndexStatus == StatusDeleted {
			t.Fatalf("old.txt should be folded into rename, got deleted entry")
		}
	}
}

func TestStatus_DetectsWorktreeRename(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	oldPath := filepath.Join(dir, "old.txt")
	if err := os.WriteFile(oldPath, []byte("rename me\n"), 0o644); err != nil {
		t.Fatalf("write old.txt: %v", err)
	}
	if err := r.Add([]string{"old.txt"}); err != nil {
		t.Fatalf("Add old.txt: %v", err)
	}
	if _, err := r.Commit("initial", "test-author"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	newPath := filepath.Join(dir, "new.txt")
	if err := os.Rename(oldPath, newPath); err != nil {
		t.Fatalf("Rename: %v", err)
	}

	entries, err := r.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}

	var found *StatusEntry
	for i := range entries {
		if entries[i].Path == "new.txt" {
			found = &entries[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("missing new.txt in status")
	}
	if found.WorkStatus != StatusRenamed {
		t.Fatalf("WorkStatus = %d, want %d", found.WorkStatus, StatusRenamed)
	}
	if found.RenamedFrom != "old.txt" {
		t.Fatalf("RenamedFrom = %q, want %q", found.RenamedFrom, "old.txt")
	}

	for _, e := range entries {
		if e.Path == "old.txt" && e.WorkStatus == StatusDeleted {
			t.Fatalf("old.txt should be folded into rename, got deleted entry")
		}
	}
}

func TestStatus_CacheHitForTouchedTrackedFile(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := r.Commit("initial", "test-author"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	hashCalls := 0
	r.statusBlobHasher = func(data []byte) object.Hash {
		hashCalls++
		return object.HashObject(object.TypeBlob, data)
	}

	touchedTime := time.Now().Add(2 * time.Minute)
	if err := os.Chtimes(path, touchedTime, touchedTime); err != nil {
		t.Fatalf("Chtimes(main.go): %v", err)
	}

	entries, err := r.Status()
	if err != nil {
		t.Fatalf("Status(first): %v", err)
	}
	mainEntry := statusEntryForPath(entries, "main.go")
	if mainEntry == nil {
		t.Fatal("missing main.go in first status")
	}
	if mainEntry.IndexStatus != StatusClean || mainEntry.WorkStatus != StatusClean {
		t.Fatalf("first status = (%d, %d), want (%d, %d)",
			mainEntry.IndexStatus, mainEntry.WorkStatus, StatusClean, StatusClean)
	}
	if hashCalls != 1 {
		t.Fatalf("hash calls after first status = %d, want 1", hashCalls)
	}

	entries, err = r.Status()
	if err != nil {
		t.Fatalf("Status(second): %v", err)
	}
	mainEntry = statusEntryForPath(entries, "main.go")
	if mainEntry == nil {
		t.Fatal("missing main.go in second status")
	}
	if mainEntry.IndexStatus != StatusClean || mainEntry.WorkStatus != StatusClean {
		t.Fatalf("second status = (%d, %d), want (%d, %d)",
			mainEntry.IndexStatus, mainEntry.WorkStatus, StatusClean, StatusClean)
	}
	if hashCalls != 1 {
		t.Fatalf("hash calls after second status = %d, want cache hit with no additional hashes", hashCalls)
	}
}

func TestStatus_CacheRemainsCorrectAfterModifyFollowingTouch(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte("alpha\n"), 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := r.Commit("initial", "test-author"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	hashCalls := 0
	r.statusBlobHasher = func(data []byte) object.Hash {
		hashCalls++
		return object.HashObject(object.TypeBlob, data)
	}

	touchedTime := time.Now().Add(2 * time.Minute)
	if err := os.Chtimes(path, touchedTime, touchedTime); err != nil {
		t.Fatalf("Chtimes(main.go): %v", err)
	}
	if _, err := r.Status(); err != nil {
		t.Fatalf("Status(after touch): %v", err)
	}
	hashCallsAfterTouch := hashCalls
	if hashCallsAfterTouch == 0 {
		t.Fatal("expected at least one hash after touch")
	}

	// Same-size content change should still be detected and cannot reuse cache.
	if err := os.WriteFile(path, []byte("bravo\n"), 0o644); err != nil {
		t.Fatalf("write modified main.go: %v", err)
	}

	entries, err := r.Status()
	if err != nil {
		t.Fatalf("Status(after modify): %v", err)
	}
	mainEntry := statusEntryForPath(entries, "main.go")
	if mainEntry == nil {
		t.Fatal("missing main.go after modify")
	}
	if mainEntry.IndexStatus != StatusClean {
		t.Fatalf("IndexStatus after modify = %d, want %d", mainEntry.IndexStatus, StatusClean)
	}
	if mainEntry.WorkStatus != StatusDirty {
		t.Fatalf("WorkStatus after modify = %d, want %d", mainEntry.WorkStatus, StatusDirty)
	}
	if hashCalls <= hashCallsAfterTouch {
		t.Fatalf("expected a new hash after modify, hash calls stayed at %d", hashCalls)
	}
}

func TestStatus_CacheInvalidatedOnTrackedStateTransitions(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := r.Commit("initial", "test-author"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	touchedTime := time.Now().Add(2 * time.Minute)
	if err := os.Chtimes(path, touchedTime, touchedTime); err != nil {
		t.Fatalf("Chtimes(main.go): %v", err)
	}
	if _, err := r.Status(); err != nil {
		t.Fatalf("Status(prime cache): %v", err)
	}
	if got := statusCacheSize(r); got == 0 {
		t.Fatal("expected non-empty cache after priming status")
	}

	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add(restage): %v", err)
	}
	if got := statusCacheSize(r); got != 0 {
		t.Fatalf("cache size after Add = %d, want 0", got)
	}

	if _, err := r.Status(); err != nil {
		t.Fatalf("Status(re-prime before commit): %v", err)
	}
	if got := statusCacheSize(r); got == 0 {
		t.Fatal("expected cache to repopulate before commit")
	}

	if _, err := r.Commit("restage", "test-author"); err != nil {
		t.Fatalf("Commit(restage): %v", err)
	}
	if got := statusCacheSize(r); got != 0 {
		t.Fatalf("cache size after Commit = %d, want 0", got)
	}

	if _, err := r.Status(); err != nil {
		t.Fatalf("Status(re-prime before checkout): %v", err)
	}
	if got := statusCacheSize(r); got == 0 {
		t.Fatal("expected cache to repopulate before checkout")
	}

	if err := r.Checkout("main"); err != nil {
		t.Fatalf("Checkout(main): %v", err)
	}
	if got := statusCacheSize(r); got != 0 {
		t.Fatalf("cache size after Checkout = %d, want 0", got)
	}
}

func statusEntryForPath(entries []StatusEntry, path string) *StatusEntry {
	for i := range entries {
		if entries[i].Path == path {
			return &entries[i]
		}
	}
	return nil
}

func statusCacheSize(r *Repo) int {
	r.statusHashCacheMu.Lock()
	defer r.statusHashCacheMu.Unlock()
	return len(r.statusHashCache)
}
