package object

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHashBytesDeterminism(t *testing.T) {
	data := []byte("hello world")
	h1 := HashBytes(data)
	h2 := HashBytes(data)
	if h1 != h2 {
		t.Errorf("HashBytes not deterministic: %q != %q", h1, h2)
	}
	if len(h1) != 64 {
		t.Errorf("Hash length: got %d, want 64", len(h1))
	}
}

func TestHashBytesDifferentInput(t *testing.T) {
	h1 := HashBytes([]byte("aaa"))
	h2 := HashBytes([]byte("bbb"))
	if h1 == h2 {
		t.Error("Different inputs produced same hash")
	}
}

func TestHashObjectEnvelope(t *testing.T) {
	data := []byte("hello")
	h1 := HashObject(TypeBlob, data)
	h2 := HashBytes(data)
	if h1 == h2 {
		t.Error("HashObject should differ from HashBytes due to envelope")
	}

	// Same type+data => same hash
	h3 := HashObject(TypeBlob, data)
	if h1 != h3 {
		t.Error("HashObject not deterministic")
	}

	// Different type => different hash
	h4 := HashObject(TypeEntity, data)
	if h1 == h4 {
		t.Error("Different types should produce different hashes")
	}
}

func tempStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	return NewStore(dir)
}

func TestStoreWriteRead(t *testing.T) {
	s := tempStore(t)
	data := []byte("hello world")
	h, err := s.Write(TypeBlob, data)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if len(h) != 64 {
		t.Errorf("Hash length: got %d, want 64", len(h))
	}

	gotType, gotData, err := s.Read(h)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if gotType != TypeBlob {
		t.Errorf("Type: got %q, want %q", gotType, TypeBlob)
	}
	if !bytes.Equal(gotData, data) {
		t.Errorf("Data: got %q, want %q", gotData, data)
	}
}

func TestStoreHas(t *testing.T) {
	s := tempStore(t)
	data := []byte("exists")
	h, err := s.Write(TypeBlob, data)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if !s.Has(h) {
		t.Error("Has returned false for existing object")
	}
	if s.Has(Hash("0000000000000000000000000000000000000000000000000000000000000000")) {
		t.Error("Has returned true for non-existing object")
	}
}

func TestStoreFanoutLayout(t *testing.T) {
	s := tempStore(t)
	data := []byte("fanout test")
	h, err := s.Write(TypeBlob, data)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	// Check 2-char fan-out directory
	prefix := string(h[:2])
	rest := string(h[2:])
	objPath := filepath.Join(s.root, "objects", prefix, rest)
	if _, err := os.Stat(objPath); os.IsNotExist(err) {
		t.Errorf("Expected fan-out file at %s", objPath)
	}
}

func TestStoreDuplicateWrite(t *testing.T) {
	s := tempStore(t)
	data := []byte("duplicate")
	h1, err := s.Write(TypeBlob, data)
	if err != nil {
		t.Fatalf("Write 1: %v", err)
	}
	h2, err := s.Write(TypeBlob, data)
	if err != nil {
		t.Fatalf("Write 2: %v", err)
	}
	if h1 != h2 {
		t.Errorf("Same content produced different hashes: %q vs %q", h1, h2)
	}
}

func TestStoreReadMissing(t *testing.T) {
	s := tempStore(t)
	_, _, err := s.Read(Hash("0000000000000000000000000000000000000000000000000000000000000000"))
	if err == nil {
		t.Error("Read of missing object should return error")
	}
}

func TestStoreWriteReadBlob(t *testing.T) {
	s := tempStore(t)
	orig := &Blob{Data: []byte("blob content\nwith newlines")}
	h, err := s.WriteBlob(orig)
	if err != nil {
		t.Fatalf("WriteBlob: %v", err)
	}
	got, err := s.ReadBlob(h)
	if err != nil {
		t.Fatalf("ReadBlob: %v", err)
	}
	if !bytes.Equal(got.Data, orig.Data) {
		t.Errorf("Blob round-trip: got %q, want %q", got.Data, orig.Data)
	}
}

func TestStoreWriteReadEntity(t *testing.T) {
	s := tempStore(t)
	orig := &EntityObj{
		Kind:     "function",
		Name:     "Serve",
		DeclKind: "func",
		Receiver: "Server",
		Body:     []byte("func (s Server) Serve() {}"),
		BodyHash: Hash("abcdabcdabcdabcdabcdabcdabcdabcdabcdabcdabcdabcdabcdabcdabcdabcd"),
	}
	h, err := s.WriteEntity(orig)
	if err != nil {
		t.Fatalf("WriteEntity: %v", err)
	}
	got, err := s.ReadEntity(h)
	if err != nil {
		t.Fatalf("ReadEntity: %v", err)
	}
	if got.Kind != orig.Kind || got.Name != orig.Name || got.DeclKind != orig.DeclKind ||
		got.Receiver != orig.Receiver || !bytes.Equal(got.Body, orig.Body) || got.BodyHash != orig.BodyHash {
		t.Errorf("Entity round-trip mismatch")
	}
}

func TestStoreWriteReadEntityList(t *testing.T) {
	s := tempStore(t)
	orig := &EntityListObj{
		Language: "go",
		Path:     "main.go",
		EntityRefs: []Hash{
			Hash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
			Hash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"),
		},
	}
	h, err := s.WriteEntityList(orig)
	if err != nil {
		t.Fatalf("WriteEntityList: %v", err)
	}
	got, err := s.ReadEntityList(h)
	if err != nil {
		t.Fatalf("ReadEntityList: %v", err)
	}
	if got.Language != orig.Language || got.Path != orig.Path {
		t.Errorf("EntityList header mismatch")
	}
	if len(got.EntityRefs) != len(orig.EntityRefs) {
		t.Fatalf("EntityRefs length: got %d, want %d", len(got.EntityRefs), len(orig.EntityRefs))
	}
	for i := range got.EntityRefs {
		if got.EntityRefs[i] != orig.EntityRefs[i] {
			t.Errorf("EntityRefs[%d]: got %q, want %q", i, got.EntityRefs[i], orig.EntityRefs[i])
		}
	}
}

func TestStoreWriteReadTree(t *testing.T) {
	s := tempStore(t)
	orig := &TreeObj{
		Entries: []TreeEntry{
			{
				Name:           "main.go",
				IsDir:          false,
				BlobHash:       Hash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
				EntityListHash: Hash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"),
			},
			{
				Name:        "pkg",
				IsDir:       true,
				SubtreeHash: Hash("cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"),
			},
		},
	}
	h, err := s.WriteTree(orig)
	if err != nil {
		t.Fatalf("WriteTree: %v", err)
	}
	got, err := s.ReadTree(h)
	if err != nil {
		t.Fatalf("ReadTree: %v", err)
	}
	if len(got.Entries) != 2 {
		t.Fatalf("Entries length: got %d, want 2", len(got.Entries))
	}
	// Should be sorted: main.go before pkg
	if got.Entries[0].Name != "main.go" || got.Entries[1].Name != "pkg" {
		t.Errorf("Tree entries not sorted correctly")
	}
}

func TestStoreWriteReadCommit(t *testing.T) {
	s := tempStore(t)
	orig := &CommitObj{
		TreeHash:  Hash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
		Parents:   []Hash{Hash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")},
		Author:    "Test User <test@example.com>",
		Timestamp: 1700000000,
		Message:   "test commit\n\nWith details.",
	}
	h, err := s.WriteCommit(orig)
	if err != nil {
		t.Fatalf("WriteCommit: %v", err)
	}
	got, err := s.ReadCommit(h)
	if err != nil {
		t.Fatalf("ReadCommit: %v", err)
	}
	if got.TreeHash != orig.TreeHash {
		t.Errorf("TreeHash mismatch")
	}
	if got.Author != orig.Author {
		t.Errorf("Author mismatch")
	}
	if got.Timestamp != orig.Timestamp {
		t.Errorf("Timestamp mismatch")
	}
	if got.Message != orig.Message {
		t.Errorf("Message mismatch: got %q, want %q", got.Message, orig.Message)
	}
}

func TestStoreObjectFormat(t *testing.T) {
	// Verify that the on-disk format is "type len\0content"
	s := tempStore(t)
	data := []byte("format check")
	h, err := s.Write(TypeBlob, data)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	// Read raw file
	prefix := string(h[:2])
	rest := string(h[2:])
	raw, err := os.ReadFile(filepath.Join(s.root, "objects", prefix, rest))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	// Should start with "blob 12\0"
	expected := "blob 12\x00format check"
	if string(raw) != expected {
		t.Errorf("On-disk format: got %q, want %q", raw, expected)
	}
}

func TestStoreMultipleTypes(t *testing.T) {
	s := tempStore(t)

	// Write objects of different types and verify they all work
	blob := &Blob{Data: []byte("data")}
	bh, err := s.WriteBlob(blob)
	if err != nil {
		t.Fatalf("WriteBlob: %v", err)
	}

	entity := &EntityObj{
		Kind: "function", Name: "F", DeclKind: "func",
		Body:     []byte("func F() {}"),
		BodyHash: HashBytes([]byte("func F() {}")),
	}
	eh, err := s.WriteEntity(entity)
	if err != nil {
		t.Fatalf("WriteEntity: %v", err)
	}

	// Verify hashes differ (different types and content)
	if bh == eh {
		t.Error("Blob and Entity hashes should differ")
	}

	// Verify each can be read back with correct type
	gotType, _, err := s.Read(bh)
	if err != nil {
		t.Fatalf("Read blob: %v", err)
	}
	if gotType != TypeBlob {
		t.Errorf("Blob type: got %q, want %q", gotType, TypeBlob)
	}

	gotType, _, err = s.Read(eh)
	if err != nil {
		t.Fatalf("Read entity: %v", err)
	}
	if gotType != TypeEntity {
		t.Errorf("Entity type: got %q, want %q", gotType, TypeEntity)
	}
}

func TestHashIsLowerHex(t *testing.T) {
	h := HashBytes([]byte("test"))
	for _, c := range string(h) {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("Hash contains non-lowercase-hex character: %c", c)
		}
	}
}

func TestStoreReadBlobTypeMismatch(t *testing.T) {
	s := tempStore(t)
	entity := &EntityObj{
		Kind: "function", Name: "F", DeclKind: "func",
		Body:     []byte("func F() {}"),
		BodyHash: HashBytes([]byte("func F() {}")),
	}
	h, err := s.WriteEntity(entity)
	if err != nil {
		t.Fatalf("WriteEntity: %v", err)
	}
	// Try to read entity as blob -- should fail
	_, err = s.ReadBlob(h)
	if err == nil {
		t.Error("ReadBlob on entity object should return error")
	}
	if !strings.Contains(err.Error(), "type mismatch") {
		t.Errorf("Expected type mismatch error, got: %v", err)
	}
}
