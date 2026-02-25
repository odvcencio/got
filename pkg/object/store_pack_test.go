package object

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStoreGCIdempotentAndReadFallback(t *testing.T) {
	s := tempStore(t)

	blobHash, err := s.Write(TypeBlob, []byte("blob payload"))
	if err != nil {
		t.Fatalf("Write(blob): %v", err)
	}
	entityHash, err := s.Write(TypeEntity, []byte("entity payload"))
	if err != nil {
		t.Fatalf("Write(entity): %v", err)
	}

	summary, err := s.GC()
	if err != nil {
		t.Fatalf("GC: %v", err)
	}
	if summary.PackedObjects != 2 {
		t.Fatalf("PackedObjects = %d, want 2", summary.PackedObjects)
	}
	if summary.PrunedObjects != 2 {
		t.Fatalf("PrunedObjects = %d, want 2", summary.PrunedObjects)
	}
	if summary.PackFile == "" || summary.IndexFile == "" {
		t.Fatalf("expected non-empty pack/index names: %+v", summary)
	}
	packPath := filepath.Join(s.root, "objects", "pack", summary.PackFile)
	idxPath := filepath.Join(s.root, "objects", "pack", summary.IndexFile)
	if _, err := os.Stat(packPath); err != nil {
		t.Fatalf("pack file missing: %v", err)
	}
	if _, err := os.Stat(idxPath); err != nil {
		t.Fatalf("index file missing: %v", err)
	}

	if _, err := os.Stat(s.objectPath(blobHash)); !os.IsNotExist(err) {
		t.Fatalf("expected blob loose object to be pruned, stat err=%v", err)
	}
	if _, err := os.Stat(s.objectPath(entityHash)); !os.IsNotExist(err) {
		t.Fatalf("expected entity loose object to be pruned, stat err=%v", err)
	}

	blobType, blobData, err := s.Read(blobHash)
	if err != nil {
		t.Fatalf("Read(blob from pack): %v", err)
	}
	if blobType != TypeBlob {
		t.Fatalf("blob type = %q, want %q", blobType, TypeBlob)
	}
	if !bytes.Equal(blobData, []byte("blob payload")) {
		t.Fatalf("blob payload = %q, want %q", blobData, []byte("blob payload"))
	}

	entityType, entityData, err := s.Read(entityHash)
	if err != nil {
		t.Fatalf("Read(entity from pack): %v", err)
	}
	if entityType != TypeEntity {
		t.Fatalf("entity type = %q, want %q", entityType, TypeEntity)
	}
	if !bytes.Equal(entityData, []byte("entity payload")) {
		t.Fatalf("entity payload = %q, want %q", entityData, []byte("entity payload"))
	}

	summary2, err := s.GC()
	if err != nil {
		t.Fatalf("second GC: %v", err)
	}
	if summary2.PackedObjects != 0 {
		t.Fatalf("second GC PackedObjects = %d, want 0", summary2.PackedObjects)
	}
}

func TestStoreGCReachablePacksOnlyReachableObjectsAndIsIdempotent(t *testing.T) {
	s := tempStore(t)

	reachableBlob, err := s.Write(TypeBlob, []byte("reachable blob"))
	if err != nil {
		t.Fatalf("Write(reachable blob): %v", err)
	}
	treeHash, err := s.WriteTree(&TreeObj{
		Entries: []TreeEntry{
			{
				Name:     "main.go",
				IsDir:    false,
				Mode:     TreeModeFile,
				BlobHash: reachableBlob,
			},
		},
	})
	if err != nil {
		t.Fatalf("WriteTree: %v", err)
	}
	commitHash, err := s.WriteCommit(&CommitObj{
		TreeHash:  treeHash,
		Author:    "tester",
		Timestamp: 1,
		Message:   "initial",
	})
	if err != nil {
		t.Fatalf("WriteCommit: %v", err)
	}
	unreachableBlob, err := s.Write(TypeBlob, []byte("unreachable blob"))
	if err != nil {
		t.Fatalf("Write(unreachable blob): %v", err)
	}

	summary, err := s.GCReachable([]Hash{commitHash})
	if err != nil {
		t.Fatalf("GCReachable: %v", err)
	}
	if summary.PackedObjects != 3 {
		t.Fatalf("PackedObjects = %d, want 3", summary.PackedObjects)
	}
	if summary.PrunedObjects != 3 {
		t.Fatalf("PrunedObjects = %d, want 3", summary.PrunedObjects)
	}
	if summary.IndexFile == "" {
		t.Fatalf("expected non-empty index file name: %+v", summary)
	}

	for _, h := range []Hash{reachableBlob, treeHash, commitHash} {
		if _, err := os.Stat(s.objectPath(h)); !os.IsNotExist(err) {
			t.Fatalf("expected reachable loose object %s to be pruned, stat err=%v", h, err)
		}
	}
	if _, err := os.Stat(s.objectPath(unreachableBlob)); err != nil {
		t.Fatalf("expected unreachable loose object to remain, stat err=%v", err)
	}

	idxPath := filepath.Join(s.root, "objects", "pack", summary.IndexFile)
	idxData, err := os.ReadFile(idxPath)
	if err != nil {
		t.Fatalf("ReadFile(index): %v", err)
	}
	idx, err := ReadPackIndex(idxData)
	if err != nil {
		t.Fatalf("ReadPackIndex: %v", err)
	}
	if _, ok := idx.Find(unreachableBlob); ok {
		t.Fatalf("index unexpectedly contains unreachable hash %s", unreachableBlob)
	}

	if _, _, err := s.Read(commitHash); err != nil {
		t.Fatalf("Read(commit from pack): %v", err)
	}

	summary2, err := s.GCReachable([]Hash{commitHash})
	if err != nil {
		t.Fatalf("second GCReachable: %v", err)
	}
	if summary2.PackedObjects != 0 {
		t.Fatalf("second GCReachable PackedObjects = %d, want 0", summary2.PackedObjects)
	}
	if _, err := os.Stat(s.objectPath(unreachableBlob)); err != nil {
		t.Fatalf("expected unreachable loose object to remain after second GCReachable, stat err=%v", err)
	}
}

func TestStoreHasChecksPackedObjects(t *testing.T) {
	s := tempStore(t)

	h, err := s.Write(TypeBlob, []byte("packed only"))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if !s.Has(h) {
		t.Fatal("Has should report true for loose object")
	}

	summary, err := s.GC()
	if err != nil {
		t.Fatalf("GC: %v", err)
	}
	if summary.PackFile == "" || summary.IndexFile == "" {
		t.Fatalf("expected pack/index files from GC: %+v", summary)
	}

	if _, err := os.Stat(s.objectPath(h)); !os.IsNotExist(err) {
		t.Fatalf("expected loose object to be pruned, stat err=%v", err)
	}
	if !s.Has(h) {
		t.Fatal("Has should report true for packed-only object")
	}

	packPath := filepath.Join(s.root, "objects", "pack", summary.PackFile)
	if err := os.Remove(packPath); err != nil {
		t.Fatalf("Remove(pack file): %v", err)
	}
	if s.Has(h) {
		t.Fatal("Has should report false when matching index exists but pack file is missing")
	}
}

func TestStoreVerifyDetectsCorruptLooseObject(t *testing.T) {
	s := tempStore(t)

	h, err := s.Write(TypeBlob, []byte("hello"))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := os.WriteFile(s.objectPath(h), []byte("broken"), 0o644); err != nil {
		t.Fatalf("WriteFile(corrupt loose): %v", err)
	}

	if _, err := s.Verify(); err == nil {
		t.Fatal("Verify should fail for corrupt loose object")
	}
}

func TestStoreVerifyDetectsCorruptPackObject(t *testing.T) {
	s := tempStore(t)

	if _, err := s.Write(TypeBlob, []byte("hello")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	summary, err := s.GC()
	if err != nil {
		t.Fatalf("GC: %v", err)
	}
	if summary.PackFile == "" {
		t.Fatalf("expected non-empty pack file name: %+v", summary)
	}

	packPath := filepath.Join(s.root, "objects", "pack", summary.PackFile)
	data, err := os.ReadFile(packPath)
	if err != nil {
		t.Fatalf("ReadFile(pack): %v", err)
	}
	if len(data) < 1 {
		t.Fatalf("pack file unexpectedly empty")
	}
	data[len(data)-1] ^= 0xff
	if err := os.WriteFile(packPath, data, 0o644); err != nil {
		t.Fatalf("WriteFile(corrupt pack): %v", err)
	}

	_, err = s.Verify()
	if err == nil {
		t.Fatal("Verify should fail for corrupt pack")
	}
	if !strings.Contains(err.Error(), "verify pack") {
		t.Fatalf("Verify error = %q, want to contain %q", err.Error(), "verify pack")
	}
}
