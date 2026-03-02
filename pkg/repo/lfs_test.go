package repo

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Test 1: Round-trip pointer format — write then parse.
func TestLFS_WriteAndParsePointer(t *testing.T) {
	oid := "abc123def456abc123def456abc123def456abc123def456abc123def456abcd"
	size := int64(1048576)

	data := WriteLFSPointer(oid, size)

	// Verify the raw format.
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d: %q", len(lines), string(data))
	}
	if lines[0] != "version graft-lfs/1" {
		t.Errorf("line 0 = %q, want %q", lines[0], "version graft-lfs/1")
	}
	if lines[1] != "oid sha256:"+oid {
		t.Errorf("line 1 = %q, want %q", lines[1], "oid sha256:"+oid)
	}
	if lines[2] != "size 1048576" {
		t.Errorf("line 2 = %q, want %q", lines[2], "size 1048576")
	}

	// Parse it back.
	ptr, ok := ParseLFSPointer(data)
	if !ok {
		t.Fatal("ParseLFSPointer returned false for valid pointer")
	}
	if ptr.Version != "graft-lfs/1" {
		t.Errorf("Version = %q, want %q", ptr.Version, "graft-lfs/1")
	}
	if ptr.OID != oid {
		t.Errorf("OID = %q, want %q", ptr.OID, oid)
	}
	if ptr.Size != size {
		t.Errorf("Size = %d, want %d", ptr.Size, size)
	}
}

// Test 2: Non-pointer data returns false.
func TestLFS_InvalidPointer(t *testing.T) {
	cases := []struct {
		name string
		data []byte
	}{
		{"empty", []byte{}},
		{"random text", []byte("hello world\n")},
		{"binary data", []byte{0x00, 0x01, 0x02, 0x03}},
		{"wrong version", []byte("version git-lfs/1\noid sha256:abc123def456abc123def456abc123def456abc123def456abc123def456abcd\nsize 100\n")},
		{"missing oid prefix", []byte("version graft-lfs/1\noid md5:abc123\nsize 100\n")},
		{"short oid", []byte("version graft-lfs/1\noid sha256:abc123\nsize 100\n")},
		{"non-hex oid", []byte("version graft-lfs/1\noid sha256:zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz\nsize 100\n")},
		{"negative size", []byte("version graft-lfs/1\noid sha256:abc123def456abc123def456abc123def456abc123def456abc123def456abcd\nsize -1\n")},
		{"non-numeric size", []byte("version graft-lfs/1\noid sha256:abc123def456abc123def456abc123def456abc123def456abc123def456abcd\nsize abc\n")},
		{"extra lines", []byte("version graft-lfs/1\noid sha256:abc123def456abc123def456abc123def456abc123def456abc123def456abcd\nsize 100\nextra\n")},
		{"too few lines", []byte("version graft-lfs/1\noid sha256:abc123def456abc123def456abc123def456abc123def456abc123def456abcd\n")},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ptr, ok := ParseLFSPointer(tc.data)
			if ok {
				t.Errorf("ParseLFSPointer returned true for %q, ptr=%+v", tc.name, ptr)
			}
			if ptr != nil {
				t.Errorf("ParseLFSPointer returned non-nil pointer for %q", tc.name)
			}
		})
	}
}

// Test 3: Store content, read it back, verify OID matches SHA-256.
func TestLFS_StoreAndReadObject(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	content := []byte("This is some large binary content for LFS testing.\n")

	// Compute expected OID.
	hash := sha256.Sum256(content)
	expectedOID := hex.EncodeToString(hash[:])

	// Store the object.
	oid, err := r.StoreLFSObject(content)
	if err != nil {
		t.Fatalf("StoreLFSObject: %v", err)
	}
	if oid != expectedOID {
		t.Errorf("OID = %q, want %q", oid, expectedOID)
	}

	// Read it back.
	got, err := r.ReadLFSObject(oid)
	if err != nil {
		t.Fatalf("ReadLFSObject: %v", err)
	}
	if string(got) != string(content) {
		t.Errorf("ReadLFSObject content mismatch: got %q, want %q", string(got), string(content))
	}

	// Verify the file exists at the expected path.
	objPath := r.LFSObjectPath(oid)
	if _, err := os.Stat(objPath); err != nil {
		t.Errorf("object file does not exist at %q: %v", objPath, err)
	}
}

// Test 3b: Reading a non-existent OID returns an error.
func TestLFS_ReadNonExistentObject(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	_, err = r.ReadLFSObject("0000000000000000000000000000000000000000000000000000000000000000")
	if err == nil {
		t.Fatal("ReadLFSObject should fail for non-existent OID")
	}
}

// Test 4: Write .graftattributes with *.bin filter=lfs, verify matching.
func TestLFS_IsLFSTracked(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Write .graftattributes with LFS tracking for *.bin files.
	attrContent := "*.bin filter=lfs diff=lfs merge=lfs\n*.dat filter=lfs\n"
	if err := os.WriteFile(filepath.Join(dir, ".graftattributes"), []byte(attrContent), 0o644); err != nil {
		t.Fatalf("write .graftattributes: %v", err)
	}

	// These should match.
	tracked := []string{
		"image.bin",
		"dir/model.bin",
		"deep/nested/file.bin",
		"data.dat",
	}
	for _, p := range tracked {
		if !r.IsLFSTracked(p) {
			t.Errorf("expected %q to be LFS-tracked", p)
		}
	}

	// These should not match.
	untracked := []string{
		"readme.txt",
		"main.go",
		"dir/code.py",
		"image.png",
	}
	for _, p := range untracked {
		if r.IsLFSTracked(p) {
			t.Errorf("expected %q to NOT be LFS-tracked", p)
		}
	}
}

// Test 4b: No .graftattributes means nothing is tracked.
func TestLFS_IsLFSTracked_NoAttributes(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	if r.IsLFSTracked("anything.bin") {
		t.Error("expected no files to be LFS-tracked when .graftattributes is absent")
	}
}

// Test 5: Verify fan-out directory structure.
func TestLFS_ObjectPath(t *testing.T) {
	dir := t.TempDir()
	r := &Repo{
		RootDir: dir,
		GotDir:  filepath.Join(dir, ".graft"),
	}

	oid := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	got := r.LFSObjectPath(oid)

	// Expected: .graft/lfs/objects/ab/cdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890
	expected := filepath.Join(dir, ".graft", "lfs", "objects", "ab", "cdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890")
	if got != expected {
		t.Errorf("LFSObjectPath = %q, want %q", got, expected)
	}

	// Verify the first two characters form the directory.
	base := filepath.Base(got)
	parentDir := filepath.Base(filepath.Dir(got))
	if parentDir != "ab" {
		t.Errorf("fan-out directory = %q, want %q", parentDir, "ab")
	}
	if base != oid[2:] {
		t.Errorf("object filename = %q, want %q", base, oid[2:])
	}
}

// Test 6: LFSStatus reports pointer files with content status.
func TestLFS_Status(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Store an LFS object so we can create a pointer that has content.
	content := []byte("binary content here")
	oid, err := r.StoreLFSObject(content)
	if err != nil {
		t.Fatalf("StoreLFSObject: %v", err)
	}

	// Write a pointer file to the working tree.
	ptrData := WriteLFSPointer(oid, int64(len(content)))
	if err := os.WriteFile(filepath.Join(dir, "asset.bin"), ptrData, 0o644); err != nil {
		t.Fatalf("write asset.bin: %v", err)
	}

	// Stage the pointer file.
	if err := r.Add([]string{"asset.bin"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Also write a pointer file without the content in the store.
	missingOID := "0000000000000000000000000000000000000000000000000000000000000000"
	ptrData2 := WriteLFSPointer(missingOID, 999)
	if err := os.WriteFile(filepath.Join(dir, "missing.bin"), ptrData2, 0o644); err != nil {
		t.Fatalf("write missing.bin: %v", err)
	}
	if err := r.Add([]string{"missing.bin"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Write a non-pointer file to confirm it's excluded.
	if err := os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("write readme.txt: %v", err)
	}
	if err := r.Add([]string{"readme.txt"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	statuses, err := r.LFSStatus()
	if err != nil {
		t.Fatalf("LFSStatus: %v", err)
	}

	if len(statuses) != 2 {
		t.Fatalf("expected 2 LFS statuses, got %d", len(statuses))
	}

	// Build a map for easier lookup.
	byPath := make(map[string]LFSFileStatus)
	for _, s := range statuses {
		byPath[s.Path] = s
	}

	// asset.bin should have content.
	assetStatus, ok := byPath["asset.bin"]
	if !ok {
		t.Fatal("expected asset.bin in LFS status")
	}
	if assetStatus.OID != oid {
		t.Errorf("asset.bin OID = %q, want %q", assetStatus.OID, oid)
	}
	if assetStatus.Size != int64(len(content)) {
		t.Errorf("asset.bin Size = %d, want %d", assetStatus.Size, len(content))
	}
	if !assetStatus.HasContent {
		t.Error("asset.bin HasContent = false, want true")
	}

	// missing.bin should not have content.
	missingStatus, ok := byPath["missing.bin"]
	if !ok {
		t.Fatal("expected missing.bin in LFS status")
	}
	if missingStatus.OID != missingOID {
		t.Errorf("missing.bin OID = %q, want %q", missingStatus.OID, missingOID)
	}
	if missingStatus.HasContent {
		t.Error("missing.bin HasContent = true, want false")
	}
}

// Test 7: Store the same content twice, verify idempotent.
func TestLFS_StoreIdempotent(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	content := []byte("duplicate content")

	oid1, err := r.StoreLFSObject(content)
	if err != nil {
		t.Fatalf("first StoreLFSObject: %v", err)
	}

	oid2, err := r.StoreLFSObject(content)
	if err != nil {
		t.Fatalf("second StoreLFSObject: %v", err)
	}

	if oid1 != oid2 {
		t.Errorf("OIDs differ: %q vs %q", oid1, oid2)
	}

	// Content should still be readable.
	got, err := r.ReadLFSObject(oid1)
	if err != nil {
		t.Fatalf("ReadLFSObject: %v", err)
	}
	if string(got) != string(content) {
		t.Errorf("content mismatch after idempotent store")
	}
}

// Test 8: WriteLFSPointer with zero size.
func TestLFS_WritePointerZeroSize(t *testing.T) {
	oid := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" // sha256 of empty
	data := WriteLFSPointer(oid, 0)

	ptr, ok := ParseLFSPointer(data)
	if !ok {
		t.Fatal("ParseLFSPointer returned false for zero-size pointer")
	}
	if ptr.Size != 0 {
		t.Errorf("Size = %d, want 0", ptr.Size)
	}
	if ptr.OID != oid {
		t.Errorf("OID = %q, want %q", ptr.OID, oid)
	}
}
