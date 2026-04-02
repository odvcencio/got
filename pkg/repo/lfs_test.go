package repo

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/odvcencio/graft/pkg/remote"
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
		RootDir:  dir,
		GraftDir: filepath.Join(dir, ".graft"),
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

// Test 9: PushLFSObjects uploads LFS objects referenced by a commit.
func TestLFS_PushLFSObjects(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Store LFS content and create a pointer file.
	binContent := []byte("binary content for push test")
	oid, err := r.StoreLFSObject(binContent)
	if err != nil {
		t.Fatalf("StoreLFSObject: %v", err)
	}

	// Write pointer file and a plain file.
	ptrData := WriteLFSPointer(oid, int64(len(binContent)))
	if err := os.WriteFile(filepath.Join(dir, "model.bin"), ptrData, 0o644); err != nil {
		t.Fatalf("write model.bin: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("write readme.txt: %v", err)
	}

	if err := r.Add([]string{"model.bin", "readme.txt"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	commitHash, err := r.Commit("add lfs file", "test")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// Set up a mock LFS server.
	var uploadedOIDs []string
	var uploadedContent []byte
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch {
		case strings.HasSuffix(req.URL.Path, "/lfs/objects/batch"):
			var batchReq remote.LFSBatchRequest
			body, _ := io.ReadAll(req.Body)
			_ = json.Unmarshal(body, &batchReq)

			resp := remote.LFSBatchResponse{}
			for _, obj := range batchReq.Objects {
				resp.Objects = append(resp.Objects, remote.LFSBatchResponseObject{
					OID:  obj.OID,
					Size: obj.Size,
					Actions: map[string]remote.LFSAction{
						"upload": {Href: "http://" + req.Host + "/lfs/upload/" + obj.OID},
					},
				})
			}
			w.Header().Set("Content-Type", "application/vnd.graft-lfs+json")
			_ = json.NewEncoder(w).Encode(resp)

		case strings.HasPrefix(req.URL.Path, "/lfs/upload/"):
			uploadedOIDs = append(uploadedOIDs, strings.TrimPrefix(req.URL.Path, "/lfs/upload/"))
			uploadedContent, _ = io.ReadAll(req.Body)
			w.WriteHeader(http.StatusOK)

		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer ts.Close()

	lfsClient := remote.NewLFSClientFromURL(ts.URL+"/graft/alice/repo", "")
	count, err := r.PushLFSObjects(t.Context(), lfsClient, commitHash)
	if err != nil {
		t.Fatalf("PushLFSObjects: %v", err)
	}

	if count != 1 {
		t.Fatalf("uploaded %d objects, want 1", count)
	}
	if len(uploadedOIDs) != 1 {
		t.Fatalf("server received %d uploads, want 1", len(uploadedOIDs))
	}
	if uploadedOIDs[0] != oid {
		t.Fatalf("uploaded OID = %q, want %q", uploadedOIDs[0], oid)
	}
	if string(uploadedContent) != string(binContent) {
		t.Fatalf("uploaded content mismatch")
	}
}

// Test 10: PushLFSObjects skips objects the server already has.
func TestLFS_PushLFSObjects_ServerAlreadyHas(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	binContent := []byte("content server already has")
	oid, err := r.StoreLFSObject(binContent)
	if err != nil {
		t.Fatalf("StoreLFSObject: %v", err)
	}

	ptrData := WriteLFSPointer(oid, int64(len(binContent)))
	if err := os.WriteFile(filepath.Join(dir, "existing.bin"), ptrData, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := r.Add([]string{"existing.bin"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	commitHash, err := r.Commit("existing lfs", "test")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	uploadCalls := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if strings.HasSuffix(req.URL.Path, "/lfs/objects/batch") {
			// No upload action = server already has it.
			var batchReq remote.LFSBatchRequest
			body, _ := io.ReadAll(req.Body)
			_ = json.Unmarshal(body, &batchReq)

			resp := remote.LFSBatchResponse{}
			for _, obj := range batchReq.Objects {
				resp.Objects = append(resp.Objects, remote.LFSBatchResponseObject{
					OID:  obj.OID,
					Size: obj.Size,
					// No actions = server already has it.
				})
			}
			w.Header().Set("Content-Type", "application/vnd.graft-lfs+json")
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		uploadCalls++
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	lfsClient := remote.NewLFSClientFromURL(ts.URL+"/graft/alice/repo", "")
	count, err := r.PushLFSObjects(t.Context(), lfsClient, commitHash)
	if err != nil {
		t.Fatalf("PushLFSObjects: %v", err)
	}
	if count != 0 {
		t.Fatalf("uploaded %d objects, want 0", count)
	}
	if uploadCalls != 0 {
		t.Fatalf("server received %d upload calls, want 0", uploadCalls)
	}
}

// Test 11: PushLFSObjects with no LFS pointers in commit.
func TestLFS_PushLFSObjects_NoPointers(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("no lfs"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := r.Add([]string{"readme.txt"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	commitHash, err := r.Commit("no lfs", "test")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	lfsClient := remote.NewLFSClientFromURL("https://example.com/graft/alice/repo", "")
	count, err := r.PushLFSObjects(t.Context(), lfsClient, commitHash)
	if err != nil {
		t.Fatalf("PushLFSObjects: %v", err)
	}
	if count != 0 {
		t.Fatalf("uploaded %d objects, want 0 (no LFS pointers)", count)
	}
}

// Test 12: FetchLFSObjects downloads missing LFS content.
func TestLFS_FetchLFSObjects(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Create content and compute its OID without storing in LFS.
	binContent := []byte("binary content for fetch test")
	hash := sha256.Sum256(binContent)
	oid := hex.EncodeToString(hash[:])

	// Write a pointer file for missing content.
	ptrData := WriteLFSPointer(oid, int64(len(binContent)))
	if err := os.WriteFile(filepath.Join(dir, "asset.bin"), ptrData, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := r.Add([]string{"asset.bin"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Verify content is missing.
	statuses, _ := r.LFSStatus()
	if len(statuses) != 1 || statuses[0].HasContent {
		t.Fatal("expected 1 LFS file with missing content")
	}

	// Mock LFS server that serves the content.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch {
		case strings.HasSuffix(req.URL.Path, "/lfs/objects/batch"):
			var batchReq remote.LFSBatchRequest
			body, _ := io.ReadAll(req.Body)
			_ = json.Unmarshal(body, &batchReq)

			resp := remote.LFSBatchResponse{}
			for _, obj := range batchReq.Objects {
				resp.Objects = append(resp.Objects, remote.LFSBatchResponseObject{
					OID:  obj.OID,
					Size: obj.Size,
					Actions: map[string]remote.LFSAction{
						"download": {Href: "http://" + req.Host + "/lfs/download/" + obj.OID},
					},
				})
			}
			w.Header().Set("Content-Type", "application/vnd.graft-lfs+json")
			_ = json.NewEncoder(w).Encode(resp)

		case strings.HasPrefix(req.URL.Path, "/lfs/download/"):
			w.Header().Set("Content-Type", "application/octet-stream")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(binContent)

		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer ts.Close()

	lfsClient := remote.NewLFSClientFromURL(ts.URL+"/graft/alice/repo", "")
	count, err := r.FetchLFSObjects(t.Context(), lfsClient)
	if err != nil {
		t.Fatalf("FetchLFSObjects: %v", err)
	}
	if count != 1 {
		t.Fatalf("downloaded %d objects, want 1", count)
	}

	// Verify content is now present.
	got, err := r.ReadLFSObject(oid)
	if err != nil {
		t.Fatalf("ReadLFSObject: %v", err)
	}
	if string(got) != string(binContent) {
		t.Fatalf("fetched content mismatch")
	}
}

// Test 13: FetchLFSObjects skips objects already present locally.
func TestLFS_FetchLFSObjects_AlreadyPresent(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	binContent := []byte("content already local")
	oid, err := r.StoreLFSObject(binContent)
	if err != nil {
		t.Fatalf("StoreLFSObject: %v", err)
	}

	ptrData := WriteLFSPointer(oid, int64(len(binContent)))
	if err := os.WriteFile(filepath.Join(dir, "local.bin"), ptrData, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := r.Add([]string{"local.bin"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// No server needed since nothing should be downloaded.
	lfsClient := remote.NewLFSClientFromURL("https://example.com/graft/alice/repo", "")
	count, err := r.FetchLFSObjects(t.Context(), lfsClient)
	if err != nil {
		t.Fatalf("FetchLFSObjects: %v", err)
	}
	if count != 0 {
		t.Fatalf("downloaded %d objects, want 0 (all present)", count)
	}
}

// Test 14: FetchLFSObjects verifies hash of downloaded content.
func TestLFS_FetchLFSObjects_HashMismatch(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Write a pointer that expects specific content.
	oid := strings.Repeat("a", 64)
	ptrData := WriteLFSPointer(oid, 100)
	if err := os.WriteFile(filepath.Join(dir, "bad.bin"), ptrData, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := r.Add([]string{"bad.bin"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Server returns wrong content.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch {
		case strings.HasSuffix(req.URL.Path, "/lfs/objects/batch"):
			var batchReq remote.LFSBatchRequest
			body, _ := io.ReadAll(req.Body)
			_ = json.Unmarshal(body, &batchReq)

			resp := remote.LFSBatchResponse{}
			for _, obj := range batchReq.Objects {
				resp.Objects = append(resp.Objects, remote.LFSBatchResponseObject{
					OID:  obj.OID,
					Size: obj.Size,
					Actions: map[string]remote.LFSAction{
						"download": {Href: "http://" + req.Host + "/lfs/download/" + obj.OID},
					},
				})
			}
			w.Header().Set("Content-Type", "application/vnd.graft-lfs+json")
			_ = json.NewEncoder(w).Encode(resp)

		case strings.HasPrefix(req.URL.Path, "/lfs/download/"):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("this content does not match the expected OID"))

		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer ts.Close()

	lfsClient := remote.NewLFSClientFromURL(ts.URL+"/graft/alice/repo", "")
	_, err = r.FetchLFSObjects(t.Context(), lfsClient)
	if err == nil {
		t.Fatal("expected hash mismatch error")
	}
	if !strings.Contains(err.Error(), "hash mismatch") {
		t.Fatalf("error = %v, expected hash mismatch", err)
	}
}
