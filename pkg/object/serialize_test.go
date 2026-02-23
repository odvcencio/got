package object

import (
	"bytes"
	"testing"
)

func TestMarshalUnmarshalBlob(t *testing.T) {
	orig := &Blob{Data: []byte("hello world\nline two")}
	data := MarshalBlob(orig)
	got, err := UnmarshalBlob(data)
	if err != nil {
		t.Fatalf("UnmarshalBlob: %v", err)
	}
	if !bytes.Equal(got.Data, orig.Data) {
		t.Errorf("Blob round-trip mismatch: got %q, want %q", got.Data, orig.Data)
	}
}

func TestMarshalBlobDeterminism(t *testing.T) {
	b := &Blob{Data: []byte("deterministic")}
	d1 := MarshalBlob(b)
	d2 := MarshalBlob(b)
	if !bytes.Equal(d1, d2) {
		t.Error("Blob marshal not deterministic")
	}
}

func TestMarshalUnmarshalEntity(t *testing.T) {
	orig := &EntityObj{
		Kind:     "function",
		Name:     "Foo",
		DeclKind: "func",
		Receiver: "Bar",
		Body:     []byte("func (b Bar) Foo() {}"),
		BodyHash: Hash("abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234"),
	}
	data := MarshalEntity(orig)
	got, err := UnmarshalEntity(data)
	if err != nil {
		t.Fatalf("UnmarshalEntity: %v", err)
	}
	if got.Kind != orig.Kind {
		t.Errorf("Kind: got %q, want %q", got.Kind, orig.Kind)
	}
	if got.Name != orig.Name {
		t.Errorf("Name: got %q, want %q", got.Name, orig.Name)
	}
	if got.DeclKind != orig.DeclKind {
		t.Errorf("DeclKind: got %q, want %q", got.DeclKind, orig.DeclKind)
	}
	if got.Receiver != orig.Receiver {
		t.Errorf("Receiver: got %q, want %q", got.Receiver, orig.Receiver)
	}
	if !bytes.Equal(got.Body, orig.Body) {
		t.Errorf("Body: got %q, want %q", got.Body, orig.Body)
	}
	if got.BodyHash != orig.BodyHash {
		t.Errorf("BodyHash: got %q, want %q", got.BodyHash, orig.BodyHash)
	}
}

func TestMarshalEntityEmptyReceiver(t *testing.T) {
	orig := &EntityObj{
		Kind:     "function",
		Name:     "TopLevel",
		DeclKind: "func",
		Receiver: "",
		Body:     []byte("func TopLevel() {}"),
		BodyHash: Hash("1111111111111111111111111111111111111111111111111111111111111111"),
	}
	data := MarshalEntity(orig)
	got, err := UnmarshalEntity(data)
	if err != nil {
		t.Fatalf("UnmarshalEntity: %v", err)
	}
	if got.Receiver != "" {
		t.Errorf("Receiver: got %q, want empty", got.Receiver)
	}
}

func TestMarshalEntityDeterminism(t *testing.T) {
	e := &EntityObj{
		Kind:     "function",
		Name:     "X",
		DeclKind: "func",
		Receiver: "",
		Body:     []byte("body"),
		BodyHash: Hash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
	}
	d1 := MarshalEntity(e)
	d2 := MarshalEntity(e)
	if !bytes.Equal(d1, d2) {
		t.Error("Entity marshal not deterministic")
	}
}

func TestMarshalUnmarshalEntityList(t *testing.T) {
	orig := &EntityListObj{
		Language: "go",
		Path:     "pkg/object/store.go",
		EntityRefs: []Hash{
			Hash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
			Hash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"),
		},
	}
	data := MarshalEntityList(orig)
	got, err := UnmarshalEntityList(data)
	if err != nil {
		t.Fatalf("UnmarshalEntityList: %v", err)
	}
	if got.Language != orig.Language {
		t.Errorf("Language: got %q, want %q", got.Language, orig.Language)
	}
	if got.Path != orig.Path {
		t.Errorf("Path: got %q, want %q", got.Path, orig.Path)
	}
	if len(got.EntityRefs) != len(orig.EntityRefs) {
		t.Fatalf("EntityRefs length: got %d, want %d", len(got.EntityRefs), len(orig.EntityRefs))
	}
	for i, h := range got.EntityRefs {
		if h != orig.EntityRefs[i] {
			t.Errorf("EntityRefs[%d]: got %q, want %q", i, h, orig.EntityRefs[i])
		}
	}
}

func TestMarshalEntityListEmpty(t *testing.T) {
	orig := &EntityListObj{
		Language:   "rust",
		Path:       "src/main.rs",
		EntityRefs: nil,
	}
	data := MarshalEntityList(orig)
	got, err := UnmarshalEntityList(data)
	if err != nil {
		t.Fatalf("UnmarshalEntityList: %v", err)
	}
	if len(got.EntityRefs) != 0 {
		t.Errorf("EntityRefs should be empty, got %d", len(got.EntityRefs))
	}
}

func TestMarshalUnmarshalTree(t *testing.T) {
	orig := &TreeObj{
		Entries: []TreeEntry{
			{
				Name:           "README.md",
				IsDir:          false,
				BlobHash:       Hash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
				EntityListHash: Hash(""),
				SubtreeHash:    Hash(""),
			},
			{
				Name:           "src",
				IsDir:          true,
				BlobHash:       Hash(""),
				EntityListHash: Hash(""),
				SubtreeHash:    Hash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"),
			},
		},
	}
	data := MarshalTree(orig)
	got, err := UnmarshalTree(data)
	if err != nil {
		t.Fatalf("UnmarshalTree: %v", err)
	}
	if len(got.Entries) != len(orig.Entries) {
		t.Fatalf("Entries length: got %d, want %d", len(got.Entries), len(orig.Entries))
	}
	for i, e := range got.Entries {
		o := orig.Entries[i]
		if e.Name != o.Name {
			t.Errorf("Entries[%d].Name: got %q, want %q", i, e.Name, o.Name)
		}
		if e.IsDir != o.IsDir {
			t.Errorf("Entries[%d].IsDir: got %v, want %v", i, e.IsDir, o.IsDir)
		}
		if e.BlobHash != o.BlobHash {
			t.Errorf("Entries[%d].BlobHash: got %q, want %q", i, e.BlobHash, o.BlobHash)
		}
		if e.EntityListHash != o.EntityListHash {
			t.Errorf("Entries[%d].EntityListHash: got %q, want %q", i, e.EntityListHash, o.EntityListHash)
		}
		if e.SubtreeHash != o.SubtreeHash {
			t.Errorf("Entries[%d].SubtreeHash: got %q, want %q", i, e.SubtreeHash, o.SubtreeHash)
		}
	}
}

func TestMarshalTreeSortsEntries(t *testing.T) {
	orig := &TreeObj{
		Entries: []TreeEntry{
			{Name: "z_file", IsDir: false, BlobHash: Hash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")},
			{Name: "a_file", IsDir: false, BlobHash: Hash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")},
		},
	}
	data := MarshalTree(orig)
	got, err := UnmarshalTree(data)
	if err != nil {
		t.Fatalf("UnmarshalTree: %v", err)
	}
	if got.Entries[0].Name != "a_file" {
		t.Errorf("Expected sorted entries, got first=%q", got.Entries[0].Name)
	}
	if got.Entries[1].Name != "z_file" {
		t.Errorf("Expected sorted entries, got second=%q", got.Entries[1].Name)
	}
}

func TestMarshalTreeDeterminism(t *testing.T) {
	tr := &TreeObj{
		Entries: []TreeEntry{
			{Name: "b", IsDir: false, BlobHash: Hash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")},
			{Name: "a", IsDir: true, SubtreeHash: Hash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")},
		},
	}
	d1 := MarshalTree(tr)
	d2 := MarshalTree(tr)
	if !bytes.Equal(d1, d2) {
		t.Error("Tree marshal not deterministic")
	}
}

func TestMarshalUnmarshalCommit(t *testing.T) {
	orig := &CommitObj{
		TreeHash: Hash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
		Parents: []Hash{
			Hash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"),
		},
		Author:    "Alice <alice@example.com>",
		Timestamp: 1700000000,
		Message:   "initial commit\n\nWith a multi-line body.",
	}
	data := MarshalCommit(orig)
	got, err := UnmarshalCommit(data)
	if err != nil {
		t.Fatalf("UnmarshalCommit: %v", err)
	}
	if got.TreeHash != orig.TreeHash {
		t.Errorf("TreeHash: got %q, want %q", got.TreeHash, orig.TreeHash)
	}
	if len(got.Parents) != len(orig.Parents) {
		t.Fatalf("Parents length: got %d, want %d", len(got.Parents), len(orig.Parents))
	}
	for i, p := range got.Parents {
		if p != orig.Parents[i] {
			t.Errorf("Parents[%d]: got %q, want %q", i, p, orig.Parents[i])
		}
	}
	if got.Author != orig.Author {
		t.Errorf("Author: got %q, want %q", got.Author, orig.Author)
	}
	if got.Timestamp != orig.Timestamp {
		t.Errorf("Timestamp: got %d, want %d", got.Timestamp, orig.Timestamp)
	}
	if got.Message != orig.Message {
		t.Errorf("Message: got %q, want %q", got.Message, orig.Message)
	}
}

func TestMarshalCommitNoParents(t *testing.T) {
	orig := &CommitObj{
		TreeHash:  Hash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
		Parents:   nil,
		Author:    "Bob <bob@example.com>",
		Timestamp: 1700000001,
		Message:   "root commit",
	}
	data := MarshalCommit(orig)
	got, err := UnmarshalCommit(data)
	if err != nil {
		t.Fatalf("UnmarshalCommit: %v", err)
	}
	if len(got.Parents) != 0 {
		t.Errorf("Parents should be empty, got %d", len(got.Parents))
	}
}

func TestMarshalCommitMultipleParents(t *testing.T) {
	orig := &CommitObj{
		TreeHash: Hash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
		Parents: []Hash{
			Hash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"),
			Hash("cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"),
		},
		Author:    "Carol <carol@example.com>",
		Timestamp: 1700000002,
		Message:   "merge commit",
	}
	data := MarshalCommit(orig)
	got, err := UnmarshalCommit(data)
	if err != nil {
		t.Fatalf("UnmarshalCommit: %v", err)
	}
	if len(got.Parents) != 2 {
		t.Fatalf("Parents length: got %d, want 2", len(got.Parents))
	}
}

func TestMarshalCommitDeterminism(t *testing.T) {
	c := &CommitObj{
		TreeHash:  Hash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
		Parents:   []Hash{Hash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")},
		Author:    "Test <t@t.com>",
		Timestamp: 100,
		Message:   "msg",
	}
	d1 := MarshalCommit(c)
	d2 := MarshalCommit(c)
	if !bytes.Equal(d1, d2) {
		t.Error("Commit marshal not deterministic")
	}
}
