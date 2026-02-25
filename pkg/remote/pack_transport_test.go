package remote

import (
	"bytes"
	"testing"

	"github.com/odvcencio/got/pkg/object"
)

func TestPackTransportRoundTrip(t *testing.T) {
	blob1 := object.MarshalBlob(&object.Blob{Data: []byte("hello\n")})
	blob2 := object.MarshalBlob(&object.Blob{Data: []byte("world\n")})
	hash1 := object.HashObject(object.TypeBlob, blob1)
	hash2 := object.HashObject(object.TypeBlob, blob2)

	records := []ObjectRecord{
		{Hash: hash1, Type: object.TypeBlob, Data: blob1},
		{Hash: hash2, Type: object.TypeBlob, Data: blob2},
	}

	var buf bytes.Buffer
	if err := EncodePackTransport(&buf, records); err != nil {
		t.Fatalf("EncodePackTransport: %v", err)
	}

	decoded, err := DecodePackTransport(buf.Bytes())
	if err != nil {
		t.Fatalf("DecodePackTransport: %v", err)
	}

	if len(decoded) != 2 {
		t.Fatalf("decoded %d records, want 2", len(decoded))
	}
	for i, rec := range decoded {
		if rec.Type != object.TypeBlob {
			t.Fatalf("record %d type = %s, want blob", i, rec.Type)
		}
	}
}

func TestPackTransportEmpty(t *testing.T) {
	var buf bytes.Buffer
	if err := EncodePackTransport(&buf, nil); err != nil {
		t.Fatalf("EncodePackTransport(nil): %v", err)
	}
	decoded, err := DecodePackTransport(buf.Bytes())
	if err != nil {
		t.Fatalf("DecodePackTransport: %v", err)
	}
	if len(decoded) != 0 {
		t.Fatalf("decoded %d records, want 0", len(decoded))
	}
}

func TestPackTransportCommitAndTree(t *testing.T) {
	blobData := object.MarshalBlob(&object.Blob{Data: []byte("data")})
	blobHash := object.HashObject(object.TypeBlob, blobData)

	treeData := object.MarshalTree(&object.TreeObj{
		Entries: []object.TreeEntry{{Name: "f.txt", BlobHash: blobHash}},
	})
	treeHash := object.HashObject(object.TypeTree, treeData)

	commitData := object.MarshalCommit(&object.CommitObj{
		TreeHash: treeHash, Author: "A", Timestamp: 1, Message: "init",
	})
	commitHash := object.HashObject(object.TypeCommit, commitData)

	records := []ObjectRecord{
		{Hash: commitHash, Type: object.TypeCommit, Data: commitData},
		{Hash: treeHash, Type: object.TypeTree, Data: treeData},
		{Hash: blobHash, Type: object.TypeBlob, Data: blobData},
	}

	var buf bytes.Buffer
	if err := EncodePackTransport(&buf, records); err != nil {
		t.Fatalf("EncodePackTransport: %v", err)
	}

	decoded, err := DecodePackTransport(buf.Bytes())
	if err != nil {
		t.Fatalf("DecodePackTransport: %v", err)
	}
	if len(decoded) != 3 {
		t.Fatalf("decoded %d, want 3", len(decoded))
	}

	types := map[object.ObjectType]int{}
	for _, r := range decoded {
		types[r.Type]++
	}
	if types[object.TypeCommit] != 1 || types[object.TypeTree] != 1 || types[object.TypeBlob] != 1 {
		t.Fatalf("type distribution: %v", types)
	}
}
