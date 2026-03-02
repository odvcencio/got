package object

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestPackIndexCacheReturnsSameDataAsUncached(t *testing.T) {
	s := tempStore(t)

	// Write several objects and pack them.
	payloads := map[Hash][]byte{}
	types := map[Hash]ObjectType{}
	for i := 0; i < 10; i++ {
		data := []byte(fmt.Sprintf("payload-%d", i))
		h, err := s.Write(TypeBlob, data)
		if err != nil {
			t.Fatalf("Write: %v", err)
		}
		payloads[h] = data
		types[h] = TypeBlob
	}

	summary, err := s.GC()
	if err != nil {
		t.Fatalf("GC: %v", err)
	}
	if summary.PackedObjects != 10 {
		t.Fatalf("PackedObjects = %d, want 10", summary.PackedObjects)
	}

	// Read each object — first call populates cache, second uses it.
	for h, want := range payloads {
		// First read (populates cache).
		objType, data, err := s.Read(h)
		if err != nil {
			t.Fatalf("Read(first) %s: %v", h, err)
		}
		if objType != TypeBlob {
			t.Fatalf("type = %q, want %q", objType, TypeBlob)
		}
		if !bytes.Equal(data, want) {
			t.Fatalf("data mismatch on first read for %s", h)
		}

		// Second read (from cache).
		objType2, data2, err := s.Read(h)
		if err != nil {
			t.Fatalf("Read(second) %s: %v", h, err)
		}
		if objType2 != objType {
			t.Fatalf("type mismatch on second read for %s", h)
		}
		if !bytes.Equal(data2, data) {
			t.Fatalf("data mismatch on second read for %s", h)
		}
	}
}

func TestPackIndexCacheInvalidatedByGC(t *testing.T) {
	s := tempStore(t)

	// Write and pack first batch.
	h1, err := s.Write(TypeBlob, []byte("batch-1-object"))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if _, err := s.GC(); err != nil {
		t.Fatalf("GC(1): %v", err)
	}

	// Read to populate cache.
	if _, _, err := s.Read(h1); err != nil {
		t.Fatalf("Read h1: %v", err)
	}

	// Write a second batch and GC again (should invalidate cache).
	h2, err := s.Write(TypeBlob, []byte("batch-2-object"))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if _, err := s.GC(); err != nil {
		t.Fatalf("GC(2): %v", err)
	}

	// Both objects should still be readable.
	if _, _, err := s.Read(h1); err != nil {
		t.Fatalf("Read h1 after second GC: %v", err)
	}
	if _, _, err := s.Read(h2); err != nil {
		t.Fatalf("Read h2 after second GC: %v", err)
	}
}

func TestPackIndexCacheMultipleObjectTypes(t *testing.T) {
	s := tempStore(t)

	blobH, err := s.Write(TypeBlob, []byte("blob data"))
	if err != nil {
		t.Fatalf("Write blob: %v", err)
	}
	entityH, err := s.Write(TypeEntity, []byte("entity data"))
	if err != nil {
		t.Fatalf("Write entity: %v", err)
	}
	commitH, err := s.WriteCommit(&CommitObj{
		TreeHash:  Hash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
		Author:    "test",
		Timestamp: 1,
		Message:   "msg",
	})
	if err != nil {
		t.Fatalf("WriteCommit: %v", err)
	}

	if _, err := s.GC(); err != nil {
		t.Fatalf("GC: %v", err)
	}

	// Read all back from pack (via cache).
	blobType, blobData, err := s.Read(blobH)
	if err != nil {
		t.Fatalf("Read blob: %v", err)
	}
	if blobType != TypeBlob || !bytes.Equal(blobData, []byte("blob data")) {
		t.Fatalf("blob mismatch")
	}

	entityType, entityData, err := s.Read(entityH)
	if err != nil {
		t.Fatalf("Read entity: %v", err)
	}
	if entityType != TypeEntity || !bytes.Equal(entityData, []byte("entity data")) {
		t.Fatalf("entity mismatch")
	}

	commitType, _, err := s.Read(commitH)
	if err != nil {
		t.Fatalf("Read commit: %v", err)
	}
	if commitType != TypeCommit {
		t.Fatalf("commit type = %q, want %q", commitType, TypeCommit)
	}
}

func TestPackIndexCacheHasWorksWithCache(t *testing.T) {
	s := tempStore(t)

	h, err := s.Write(TypeBlob, []byte("has-check"))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if _, err := s.GC(); err != nil {
		t.Fatalf("GC: %v", err)
	}

	// First Has call populates idx cache.
	if !s.Has(h) {
		t.Fatal("Has should return true for packed object (first call)")
	}
	// Second Has call uses cache.
	if !s.Has(h) {
		t.Fatal("Has should return true for packed object (cached call)")
	}

	// Non-existent hash should return false.
	missing := Hash("0000000000000000000000000000000000000000000000000000000000000000")
	if s.Has(missing) {
		t.Fatal("Has should return false for missing object")
	}
}

func TestPackIndexCacheExplicitInvalidation(t *testing.T) {
	s := tempStore(t)

	h, err := s.Write(TypeBlob, []byte("invalidate-me"))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if _, err := s.GC(); err != nil {
		t.Fatalf("GC: %v", err)
	}

	// Populate cache.
	if _, _, err := s.Read(h); err != nil {
		t.Fatalf("Read: %v", err)
	}

	// Verify cache is populated.
	s.packIdxMu.Lock()
	cacheLen := len(s.packIdxCache)
	s.packIdxMu.Unlock()
	if cacheLen == 0 {
		t.Fatal("expected non-empty cache after Read")
	}

	// Invalidate and verify cache is empty.
	s.InvalidatePackIndexCache()

	s.packIdxMu.Lock()
	cacheLen = len(s.packIdxCache)
	s.packIdxMu.Unlock()
	if cacheLen != 0 {
		t.Fatalf("expected empty cache after invalidation, got %d entries", cacheLen)
	}

	// Read should still work (re-populates cache).
	if _, _, err := s.Read(h); err != nil {
		t.Fatalf("Read after invalidation: %v", err)
	}
}

func TestReadPackEntryAtDirectly(t *testing.T) {
	s := tempStore(t)

	data := []byte("direct-read-test")
	h, err := s.Write(TypeBlob, data)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	summary, err := s.GC()
	if err != nil {
		t.Fatalf("GC: %v", err)
	}

	packPath := filepath.Join(s.root, "objects", "pack", summary.PackFile)
	idxPath := filepath.Join(s.root, "objects", "pack", summary.IndexFile)

	idxData, err := os.ReadFile(idxPath)
	if err != nil {
		t.Fatalf("ReadFile(idx): %v", err)
	}
	idx, err := ReadPackIndex(idxData)
	if err != nil {
		t.Fatalf("ReadPackIndex: %v", err)
	}

	indexEntry, ok := idx.Find(h)
	if !ok {
		t.Fatalf("hash %s not found in index", h)
	}

	entry, err := readResolvedPackEntryAt(packPath, indexEntry.Offset)
	if err != nil {
		t.Fatalf("readResolvedPackEntryAt: %v", err)
	}

	objType, objData, err := decodeIndexedPackEntry(h, entry)
	if err != nil {
		t.Fatalf("decodeIndexedPackEntry: %v", err)
	}
	if objType != TypeBlob {
		t.Fatalf("type = %q, want %q", objType, TypeBlob)
	}
	if !bytes.Equal(objData, data) {
		t.Fatalf("data mismatch: got %q, want %q", objData, data)
	}
}

func TestReadPackEntryAtMultipleEntries(t *testing.T) {
	s := tempStore(t)

	// Write multiple objects so the pack has several entries at different offsets.
	hashes := make([]Hash, 5)
	payloads := make([][]byte, 5)
	for i := 0; i < 5; i++ {
		payloads[i] = []byte(fmt.Sprintf("entry-%d-data", i))
		var err error
		hashes[i], err = s.Write(TypeBlob, payloads[i])
		if err != nil {
			t.Fatalf("Write(%d): %v", i, err)
		}
	}

	summary, err := s.GC()
	if err != nil {
		t.Fatalf("GC: %v", err)
	}

	packPath := filepath.Join(s.root, "objects", "pack", summary.PackFile)
	idxPath := filepath.Join(s.root, "objects", "pack", summary.IndexFile)

	idxData, err := os.ReadFile(idxPath)
	if err != nil {
		t.Fatalf("ReadFile(idx): %v", err)
	}
	idx, err := ReadPackIndex(idxData)
	if err != nil {
		t.Fatalf("ReadPackIndex: %v", err)
	}

	for i, h := range hashes {
		indexEntry, ok := idx.Find(h)
		if !ok {
			t.Fatalf("hash %d %s not found in index", i, h)
		}

		entry, err := readResolvedPackEntryAt(packPath, indexEntry.Offset)
		if err != nil {
			t.Fatalf("readResolvedPackEntryAt(%d): %v", i, err)
		}

		objType, objData, err := decodeIndexedPackEntry(h, entry)
		if err != nil {
			t.Fatalf("decodeIndexedPackEntry(%d): %v", i, err)
		}
		if objType != TypeBlob {
			t.Fatalf("entry %d type = %q, want %q", i, objType, TypeBlob)
		}
		if !bytes.Equal(objData, payloads[i]) {
			t.Fatalf("entry %d data mismatch", i)
		}
	}
}

// BenchmarkReadFromPackCached measures the cost of reading a packed object
// with the pack index cache warmed up. This is the common case after the
// first access.
func BenchmarkReadFromPackCached(b *testing.B) {
	dir := b.TempDir()
	s := NewStore(dir)

	// Write 100 objects and pack them.
	hashes := make([]Hash, 100)
	for i := range hashes {
		data := make([]byte, 256)
		if _, err := rand.Read(data); err != nil {
			b.Fatalf("rand.Read: %v", err)
		}
		var err error
		hashes[i], err = s.Write(TypeBlob, data)
		if err != nil {
			b.Fatalf("Write: %v", err)
		}
	}
	if _, err := s.GC(); err != nil {
		b.Fatalf("GC: %v", err)
	}

	// Warm the cache.
	if _, _, err := s.Read(hashes[0]); err != nil {
		b.Fatalf("warm Read: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h := hashes[i%len(hashes)]
		_, _, err := s.Read(h)
		if err != nil {
			b.Fatalf("Read: %v", err)
		}
	}
}

// BenchmarkReadFromPackUncached measures the cost of reading a packed object
// when the pack index cache is cold (invalidated before each read). This
// simulates the old behavior of re-parsing the idx on every lookup.
func BenchmarkReadFromPackUncached(b *testing.B) {
	dir := b.TempDir()
	s := NewStore(dir)

	// Write 100 objects and pack them.
	hashes := make([]Hash, 100)
	for i := range hashes {
		data := make([]byte, 256)
		if _, err := rand.Read(data); err != nil {
			b.Fatalf("rand.Read: %v", err)
		}
		var err error
		hashes[i], err = s.Write(TypeBlob, data)
		if err != nil {
			b.Fatalf("Write: %v", err)
		}
	}
	if _, err := s.GC(); err != nil {
		b.Fatalf("GC: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.InvalidatePackIndexCache()
		h := hashes[i%len(hashes)]
		_, _, err := s.Read(h)
		if err != nil {
			b.Fatalf("Read: %v", err)
		}
	}
}

// BenchmarkHasInPackCached benchmarks Has() for packed objects with a warm cache.
func BenchmarkHasInPackCached(b *testing.B) {
	dir := b.TempDir()
	s := NewStore(dir)

	hashes := make([]Hash, 100)
	for i := range hashes {
		data := make([]byte, 64)
		if _, err := rand.Read(data); err != nil {
			b.Fatalf("rand.Read: %v", err)
		}
		var err error
		hashes[i], err = s.Write(TypeBlob, data)
		if err != nil {
			b.Fatalf("Write: %v", err)
		}
	}
	if _, err := s.GC(); err != nil {
		b.Fatalf("GC: %v", err)
	}

	// Warm the cache.
	s.Has(hashes[0])

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h := hashes[i%len(hashes)]
		if !s.Has(h) {
			b.Fatalf("Has returned false for %s", h)
		}
	}
}
