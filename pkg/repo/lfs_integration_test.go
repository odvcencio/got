package repo

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLFS_AddStoresPointer verifies that Add() replaces file content with an
// LFS pointer when the file matches a .graftattributes filter=lfs pattern,
// and that the actual content is stored in the LFS object store.
func TestLFS_AddStoresPointer(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Write .graftattributes tracking *.bin files.
	attrContent := "*.bin filter=lfs diff=lfs merge=lfs\n"
	if err := os.WriteFile(filepath.Join(dir, ".graftattributes"), []byte(attrContent), 0o644); err != nil {
		t.Fatalf("write .graftattributes: %v", err)
	}

	// Write a binary file.
	binContent := []byte("this is large binary content for LFS testing")
	if err := os.WriteFile(filepath.Join(dir, "model.bin"), binContent, 0o644); err != nil {
		t.Fatalf("write model.bin: %v", err)
	}

	// Also write a non-LFS file for comparison.
	txtContent := []byte("this is a plain text file")
	if err := os.WriteFile(filepath.Join(dir, "readme.txt"), txtContent, 0o644); err != nil {
		t.Fatalf("write readme.txt: %v", err)
	}

	// Stage both files.
	if err := r.Add([]string{"model.bin", "readme.txt"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Read staging.
	stg, err := r.ReadStaging()
	if err != nil {
		t.Fatalf("ReadStaging: %v", err)
	}

	// Verify the staged blob for model.bin is a pointer.
	binEntry, ok := stg.Entries["model.bin"]
	if !ok {
		t.Fatal("model.bin not found in staging")
	}

	blob, err := r.Store.ReadBlob(binEntry.BlobHash)
	if err != nil {
		t.Fatalf("ReadBlob for model.bin: %v", err)
	}

	ptr, ok := ParseLFSPointer(blob.Data)
	if !ok {
		t.Fatalf("staged blob for model.bin is not an LFS pointer: %q", string(blob.Data))
	}

	// Verify OID matches SHA-256 of original content.
	hash := sha256.Sum256(binContent)
	expectedOID := hex.EncodeToString(hash[:])
	if ptr.OID != expectedOID {
		t.Errorf("pointer OID = %q, want %q", ptr.OID, expectedOID)
	}
	if ptr.Size != int64(len(binContent)) {
		t.Errorf("pointer Size = %d, want %d", ptr.Size, len(binContent))
	}

	// Verify LFS content exists in the store.
	lfsContent, err := r.ReadLFSObject(ptr.OID)
	if err != nil {
		t.Fatalf("ReadLFSObject: %v", err)
	}
	if string(lfsContent) != string(binContent) {
		t.Errorf("LFS content = %q, want %q", string(lfsContent), string(binContent))
	}

	// Verify the staged blob for readme.txt is NOT a pointer (untouched).
	txtEntry, ok := stg.Entries["readme.txt"]
	if !ok {
		t.Fatal("readme.txt not found in staging")
	}

	txtBlob, err := r.Store.ReadBlob(txtEntry.BlobHash)
	if err != nil {
		t.Fatalf("ReadBlob for readme.txt: %v", err)
	}

	if _, isPtr := ParseLFSPointer(txtBlob.Data); isPtr {
		t.Error("staged blob for readme.txt should NOT be an LFS pointer")
	}
	if string(txtBlob.Data) != string(txtContent) {
		t.Errorf("readme.txt blob = %q, want %q", string(txtBlob.Data), string(txtContent))
	}
}

// TestLFS_CheckoutRestoresContent verifies that Checkout restores LFS content
// from the local store when the blob is a pointer file.
func TestLFS_CheckoutRestoresContent(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Write .graftattributes tracking *.bin files.
	attrContent := "*.bin filter=lfs diff=lfs merge=lfs\n"
	if err := os.WriteFile(filepath.Join(dir, ".graftattributes"), []byte(attrContent), 0o644); err != nil {
		t.Fatalf("write .graftattributes: %v", err)
	}

	// Write and stage a binary file (LFS-tracked) and the attributes file.
	binContent := []byte("binary data that should be stored in LFS and restored on checkout")
	if err := os.WriteFile(filepath.Join(dir, "asset.bin"), binContent, 0o644); err != nil {
		t.Fatalf("write asset.bin: %v", err)
	}

	if err := r.Add([]string{".graftattributes", "asset.bin"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// After Add, the staged blob is the pointer but the working tree file
	// still has the original content. Write the pointer to disk so the
	// working tree matches the staged blob hash (required for clean status).
	stg, err := r.ReadStaging()
	if err != nil {
		t.Fatalf("ReadStaging: %v", err)
	}
	binEntry := stg.Entries["asset.bin"]
	ptrBlob, err := r.Store.ReadBlob(binEntry.BlobHash)
	if err != nil {
		t.Fatalf("ReadBlob for pointer: %v", err)
	}
	assetPath := filepath.Join(dir, "asset.bin")
	if err := os.WriteFile(assetPath, ptrBlob.Data, 0o644); err != nil {
		t.Fatalf("write pointer to asset.bin: %v", err)
	}
	// Update the staging entry's stat to match the new on-disk pointer file,
	// so the status check sees it as clean.
	ptrInfo, err := os.Stat(assetPath)
	if err != nil {
		t.Fatalf("stat asset.bin after pointer write: %v", err)
	}
	setStagingEntryStat(binEntry, ptrInfo, modeFromFileInfo(ptrInfo))
	if err := r.WriteStaging(stg); err != nil {
		t.Fatalf("WriteStaging: %v", err)
	}

	// Create a commit on main.
	commitHash, err := r.Commit("add LFS file", "test")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// Create a second branch with a different file so we can checkout back.
	if err := r.CreateBranch("other", commitHash); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}

	// Checkout "other" (which has the same content — triggers file write).
	if err := r.Checkout("other"); err != nil {
		t.Fatalf("Checkout other: %v", err)
	}

	// Verify the file on disk has the actual content, not the pointer.
	restored, err := os.ReadFile(filepath.Join(dir, "asset.bin"))
	if err != nil {
		t.Fatalf("read asset.bin after checkout: %v", err)
	}

	if string(restored) != string(binContent) {
		// Check if it's a pointer.
		if ptr, ok := ParseLFSPointer(restored); ok {
			t.Fatalf("asset.bin is an LFS pointer (oid=%s) instead of actual content", ptr.OID)
		}
		t.Errorf("asset.bin content = %q, want %q", string(restored), string(binContent))
	}
}

// TestLFS_TrackUntrack verifies the track and untrack operations on
// .graftattributes.
func TestLFS_TrackUntrack(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	attrPath := filepath.Join(r.RootDir, ".graftattributes")

	// Track *.bin pattern.
	writeTrackLine(t, attrPath, "*.bin")

	// Verify .graftattributes contains the line.
	data, err := os.ReadFile(attrPath)
	if err != nil {
		t.Fatalf("read .graftattributes: %v", err)
	}
	if !strings.Contains(string(data), "*.bin filter=lfs diff=lfs merge=lfs") {
		t.Errorf(".graftattributes missing *.bin line: %q", string(data))
	}

	// Verify it's actually tracked.
	if !r.IsLFSTracked("image.bin") {
		t.Error("image.bin should be LFS-tracked after track *.bin")
	}

	// Track *.dat pattern.
	writeTrackLine(t, attrPath, "*.dat")

	// Verify both patterns exist.
	data, err = os.ReadFile(attrPath)
	if err != nil {
		t.Fatalf("read .graftattributes: %v", err)
	}
	if !strings.Contains(string(data), "*.bin filter=lfs diff=lfs merge=lfs") {
		t.Errorf(".graftattributes missing *.bin line after adding *.dat")
	}
	if !strings.Contains(string(data), "*.dat filter=lfs diff=lfs merge=lfs") {
		t.Errorf(".graftattributes missing *.dat line")
	}

	// Don't duplicate if we track *.bin again.
	writeTrackLine(t, attrPath, "*.bin")
	data, err = os.ReadFile(attrPath)
	if err != nil {
		t.Fatalf("read .graftattributes: %v", err)
	}
	count := strings.Count(string(data), "*.bin filter=lfs")
	if count != 1 {
		t.Errorf("expected 1 occurrence of *.bin, got %d in:\n%s", count, string(data))
	}

	// Untrack *.bin.
	removeTrackLine(t, attrPath, "*.bin")
	data, err = os.ReadFile(attrPath)
	if err != nil {
		t.Fatalf("read .graftattributes: %v", err)
	}
	if strings.Contains(string(data), "*.bin") {
		t.Errorf(".graftattributes still contains *.bin after untrack: %q", string(data))
	}
	// *.dat should still be present.
	if !strings.Contains(string(data), "*.dat filter=lfs diff=lfs merge=lfs") {
		t.Errorf(".graftattributes missing *.dat line after untracking *.bin")
	}

	// Verify *.bin files are no longer tracked.
	if r.IsLFSTracked("image.bin") {
		t.Error("image.bin should NOT be LFS-tracked after untrack *.bin")
	}
	// *.dat should still be tracked.
	if !r.IsLFSTracked("file.dat") {
		t.Error("file.dat should still be LFS-tracked")
	}
}

// writeTrackLine appends a track line to .graftattributes if not already present.
// This mirrors the logic of "graft lfs track <pattern>" at the library level.
func writeTrackLine(t *testing.T, attrPath, pattern string) {
	t.Helper()
	line := pattern + " filter=lfs diff=lfs merge=lfs"

	existing, err := os.ReadFile(attrPath)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("read .graftattributes: %v", err)
	}

	// Check for duplicates.
	if len(existing) > 0 {
		for _, l := range strings.Split(strings.TrimRight(string(existing), "\n"), "\n") {
			fields := strings.Fields(l)
			if len(fields) > 0 && fields[0] == pattern {
				return // already present
			}
		}
	}

	f, err := os.OpenFile(attrPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open .graftattributes: %v", err)
	}
	defer f.Close()

	if _, err := f.WriteString(line + "\n"); err != nil {
		t.Fatalf("write .graftattributes: %v", err)
	}
}

// removeTrackLine removes a pattern's line from .graftattributes.
// This mirrors the logic of "graft lfs untrack <pattern>" at the library level.
func removeTrackLine(t *testing.T, attrPath, pattern string) {
	t.Helper()

	data, err := os.ReadFile(attrPath)
	if err != nil {
		t.Fatalf("read .graftattributes: %v", err)
	}

	var kept []string
	for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == pattern {
			continue
		}
		kept = append(kept, line)
	}

	content := strings.Join(kept, "\n")
	if len(kept) > 0 {
		content += "\n"
	}
	if err := os.WriteFile(attrPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write .graftattributes: %v", err)
	}
}
