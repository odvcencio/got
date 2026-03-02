package repo

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/odvcencio/graft/pkg/object"
	"github.com/odvcencio/graft/pkg/remote"
)

const lfsPointerVersion = "graft-lfs/1"

// LFSPointer represents a parsed LFS pointer file.
type LFSPointer struct {
	Version string
	OID     string
	Size    int64
}

// LFSFileStatus records the status of a single LFS-tracked file.
type LFSFileStatus struct {
	Path       string
	OID        string
	Size       int64
	HasContent bool // true if content exists in .graft/lfs/objects/
}

// ParseLFSPointer parses pointer file content. Returns nil, false if the data
// is not a valid LFS pointer.
func ParseLFSPointer(data []byte) (*LFSPointer, bool) {
	// Pointer files are small text files with exactly 3 lines.
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 3 {
		return nil, false
	}

	// Line 1: version graft-lfs/1
	if !strings.HasPrefix(lines[0], "version ") {
		return nil, false
	}
	version := strings.TrimPrefix(lines[0], "version ")
	if version != lfsPointerVersion {
		return nil, false
	}

	// Line 2: oid sha256:<hex>
	if !strings.HasPrefix(lines[1], "oid sha256:") {
		return nil, false
	}
	oid := strings.TrimPrefix(lines[1], "oid sha256:")
	if len(oid) != 64 {
		return nil, false
	}
	// Validate hex characters.
	if _, err := hex.DecodeString(oid); err != nil {
		return nil, false
	}

	// Line 3: size <bytes>
	if !strings.HasPrefix(lines[2], "size ") {
		return nil, false
	}
	sizeStr := strings.TrimPrefix(lines[2], "size ")
	size, err := strconv.ParseInt(sizeStr, 10, 64)
	if err != nil {
		return nil, false
	}
	if size < 0 {
		return nil, false
	}

	return &LFSPointer{
		Version: version,
		OID:     oid,
		Size:    size,
	}, true
}

// WriteLFSPointer generates pointer file bytes for the given OID and size.
func WriteLFSPointer(oid string, size int64) []byte {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "version %s\n", lfsPointerVersion)
	fmt.Fprintf(&buf, "oid sha256:%s\n", oid)
	fmt.Fprintf(&buf, "size %d\n", size)
	return buf.Bytes()
}

// IsLFSTracked checks .graftattributes for filter=lfs on the given path.
func (r *Repo) IsLFSTracked(path string) bool {
	attrs, err := r.ReadAttributes()
	if err != nil {
		return false
	}
	m := attrs.Match(path)
	return m["filter"] == "lfs"
}

// StoreLFSObject writes content to .graft/lfs/objects/<oid[:2]>/<oid[2:]> and
// returns the SHA-256 OID.
func (r *Repo) StoreLFSObject(data []byte) (string, error) {
	hash := sha256.Sum256(data)
	oid := hex.EncodeToString(hash[:])

	objPath := r.LFSObjectPath(oid)
	dir := filepath.Dir(objPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("store lfs object: mkdir %s: %w", dir, err)
	}

	// Write atomically via temp file + rename.
	tmp, err := os.CreateTemp(dir, ".lfs-tmp-*")
	if err != nil {
		return "", fmt.Errorf("store lfs object: tmpfile: %w", err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return "", fmt.Errorf("store lfs object: write: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return "", fmt.Errorf("store lfs object: close: %w", err)
	}

	if err := os.Rename(tmpName, objPath); err != nil {
		os.Remove(tmpName)
		return "", fmt.Errorf("store lfs object: rename: %w", err)
	}

	return oid, nil
}

// ReadLFSObject reads content from the LFS store for the given OID.
func (r *Repo) ReadLFSObject(oid string) ([]byte, error) {
	objPath := r.LFSObjectPath(oid)
	data, err := os.ReadFile(objPath)
	if err != nil {
		return nil, fmt.Errorf("read lfs object %s: %w", oid, err)
	}
	return data, nil
}

// LFSObjectPath returns the filesystem path for an LFS object given its OID.
// The layout is .graft/lfs/objects/<oid[:2]>/<oid[2:]>.
func (r *Repo) LFSObjectPath(oid string) string {
	return filepath.Join(r.GraftDir, "lfs", "objects", oid[:2], oid[2:])
}

// LFSStatus lists tracked LFS files with pointer and content status.
// It reads the staging index, checks each entry for an LFS pointer, and
// reports whether the corresponding content exists in the local LFS store.
func (r *Repo) LFSStatus() ([]LFSFileStatus, error) {
	stg, err := r.ReadStaging()
	if err != nil {
		return nil, fmt.Errorf("lfs status: %w", err)
	}

	var result []LFSFileStatus
	for _, entry := range stg.Entries {
		// Read the file content from the working tree to check for LFS pointer.
		absPath := filepath.Join(r.RootDir, filepath.FromSlash(entry.Path))
		data, err := os.ReadFile(absPath)
		if err != nil {
			// File might have been deleted; try reading from the object store.
			if entry.BlobHash != "" {
				blob, blobErr := r.Store.ReadBlob(entry.BlobHash)
				if blobErr != nil {
					continue
				}
				data = blob.Data
			} else {
				continue
			}
		}

		ptr, ok := ParseLFSPointer(data)
		if !ok {
			continue
		}

		hasContent := false
		objPath := r.LFSObjectPath(ptr.OID)
		if _, statErr := os.Stat(objPath); statErr == nil {
			hasContent = true
		}

		result = append(result, LFSFileStatus{
			Path:       entry.Path,
			OID:        ptr.OID,
			Size:       ptr.Size,
			HasContent: hasContent,
		})
	}

	return result, nil
}

// CollectLFSPointersFromCommit walks the tree for a given commit hash and
// returns all LFS pointers found in the blobs. Each pointer's OID, size, and
// the file path are returned.
func (r *Repo) CollectLFSPointersFromCommit(commitHash object.Hash) ([]LFSFileStatus, error) {
	commit, err := r.Store.ReadCommit(commitHash)
	if err != nil {
		return nil, fmt.Errorf("collect lfs pointers: read commit %s: %w", commitHash, err)
	}

	files, err := r.FlattenTree(commit.TreeHash)
	if err != nil {
		return nil, fmt.Errorf("collect lfs pointers: flatten tree: %w", err)
	}

	var result []LFSFileStatus
	for _, f := range files {
		blob, err := r.Store.ReadBlob(f.BlobHash)
		if err != nil {
			continue
		}
		ptr, ok := ParseLFSPointer(blob.Data)
		if !ok {
			continue
		}
		hasContent := false
		if _, statErr := os.Stat(r.LFSObjectPath(ptr.OID)); statErr == nil {
			hasContent = true
		}
		result = append(result, LFSFileStatus{
			Path:       f.Path,
			OID:        ptr.OID,
			Size:       ptr.Size,
			HasContent: hasContent,
		})
	}
	return result, nil
}

// PushLFSObjects uploads locally-stored LFS objects that are referenced by the
// given commit to the remote via the LFS batch transfer protocol. It returns
// the number of objects uploaded.
func (r *Repo) PushLFSObjects(ctx context.Context, lfsClient *remote.LFSClient, commitHash object.Hash) (int, error) {
	pointers, err := r.CollectLFSPointersFromCommit(commitHash)
	if err != nil {
		return 0, err
	}
	if len(pointers) == 0 {
		return 0, nil
	}

	// Deduplicate OIDs and filter to objects we have locally.
	seen := make(map[string]struct{})
	var batchObjects []remote.LFSBatchObject
	for _, p := range pointers {
		if _, dup := seen[p.OID]; dup {
			continue
		}
		seen[p.OID] = struct{}{}
		if !p.HasContent {
			continue // cannot upload what we do not have
		}
		batchObjects = append(batchObjects, remote.LFSBatchObject{
			OID:  p.OID,
			Size: p.Size,
		})
	}

	if len(batchObjects) == 0 {
		return 0, nil
	}

	batchResp, err := lfsClient.BatchRequest(ctx, remote.LFSBatchRequest{
		Operation: "upload",
		Objects:   batchObjects,
	})
	if err != nil {
		return 0, fmt.Errorf("push lfs objects: batch request: %w", err)
	}

	uploaded := 0
	for _, obj := range batchResp.Objects {
		if obj.Error != nil {
			continue // server rejected this object; skip
		}
		uploadAction, ok := obj.Actions["upload"]
		if !ok {
			continue // server already has it
		}

		content, err := r.ReadLFSObject(obj.OID)
		if err != nil {
			return uploaded, fmt.Errorf("push lfs objects: read %s: %w", obj.OID, err)
		}

		if err := lfsClient.Upload(ctx, uploadAction, bytes.NewReader(content), int64(len(content))); err != nil {
			return uploaded, fmt.Errorf("push lfs objects: upload %s: %w", obj.OID, err)
		}
		uploaded++
	}

	return uploaded, nil
}

// FetchLFSObjects downloads missing LFS objects referenced by the staging
// index from the remote via the LFS batch transfer protocol. It returns the
// number of objects downloaded.
func (r *Repo) FetchLFSObjects(ctx context.Context, lfsClient *remote.LFSClient) (int, error) {
	statuses, err := r.LFSStatus()
	if err != nil {
		return 0, fmt.Errorf("fetch lfs objects: %w", err)
	}

	// Collect OIDs that are missing locally.
	seen := make(map[string]struct{})
	var batchObjects []remote.LFSBatchObject
	for _, s := range statuses {
		if s.HasContent {
			continue
		}
		if _, dup := seen[s.OID]; dup {
			continue
		}
		seen[s.OID] = struct{}{}
		batchObjects = append(batchObjects, remote.LFSBatchObject{
			OID:  s.OID,
			Size: s.Size,
		})
	}

	if len(batchObjects) == 0 {
		return 0, nil
	}

	batchResp, err := lfsClient.BatchRequest(ctx, remote.LFSBatchRequest{
		Operation: "download",
		Objects:   batchObjects,
	})
	if err != nil {
		return 0, fmt.Errorf("fetch lfs objects: batch request: %w", err)
	}

	downloaded := 0
	for _, obj := range batchResp.Objects {
		if obj.Error != nil {
			continue
		}
		dlAction, ok := obj.Actions["download"]
		if !ok {
			continue
		}

		rc, err := lfsClient.Download(ctx, dlAction)
		if err != nil {
			return downloaded, fmt.Errorf("fetch lfs objects: download %s: %w", obj.OID, err)
		}

		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return downloaded, fmt.Errorf("fetch lfs objects: read download %s: %w", obj.OID, err)
		}

		// Verify the downloaded content matches the expected OID.
		hash := sha256.Sum256(data)
		gotOID := hex.EncodeToString(hash[:])
		if gotOID != obj.OID {
			return downloaded, fmt.Errorf("fetch lfs objects: hash mismatch for %s: got %s", obj.OID, gotOID)
		}

		if _, err := r.StoreLFSObject(data); err != nil {
			return downloaded, fmt.Errorf("fetch lfs objects: store %s: %w", obj.OID, err)
		}
		downloaded++
	}

	return downloaded, nil
}
