package object

import (
	"bytes"
	"crypto/sha256"
	"testing"
)

func TestPackWriterSingleBlob(t *testing.T) {
	var buf bytes.Buffer
	pw, err := NewPackWriter(&buf, 1)
	if err != nil {
		t.Fatalf("NewPackWriter: %v", err)
	}

	blobData := []byte("hello world")
	if err := pw.WriteEntry(PackBlob, blobData); err != nil {
		t.Fatalf("WriteEntry: %v", err)
	}

	checksum, err := pw.Finish()
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}
	if checksum == "" {
		t.Fatal("expected non-empty checksum")
	}

	data := buf.Bytes()
	if len(data) <= packHeaderSize+sha256.Size {
		t.Fatalf("pack output too short: %d", len(data))
	}

	header, err := UnmarshalPackHeader(data[:packHeaderSize])
	if err != nil {
		t.Fatalf("UnmarshalPackHeader: %v", err)
	}
	if header.NumObjects != 1 {
		t.Fatalf("NumObjects = %d, want 1", header.NumObjects)
	}
}

func TestPackWriterMultipleObjects(t *testing.T) {
	var buf bytes.Buffer
	pw, err := NewPackWriter(&buf, 3)
	if err != nil {
		t.Fatalf("NewPackWriter: %v", err)
	}

	for i := 0; i < 3; i++ {
		if err := pw.WriteEntry(PackBlob, []byte("data")); err != nil {
			t.Fatalf("WriteEntry[%d]: %v", i, err)
		}
	}

	if _, err := pw.Finish(); err != nil {
		t.Fatalf("Finish: %v", err)
	}
}

func TestPackWriterCountMismatch(t *testing.T) {
	var buf bytes.Buffer
	pw, err := NewPackWriter(&buf, 2)
	if err != nil {
		t.Fatalf("NewPackWriter: %v", err)
	}
	if err := pw.WriteEntry(PackBlob, []byte("one")); err != nil {
		t.Fatalf("WriteEntry: %v", err)
	}

	if _, err := pw.Finish(); err == nil {
		t.Fatal("expected count mismatch error")
	}
}

func TestPackWriterRejectsWriteAfterFinish(t *testing.T) {
	var buf bytes.Buffer
	pw, err := NewPackWriter(&buf, 1)
	if err != nil {
		t.Fatalf("NewPackWriter: %v", err)
	}
	if err := pw.WriteEntry(PackBlob, []byte("one")); err != nil {
		t.Fatalf("WriteEntry: %v", err)
	}
	if _, err := pw.Finish(); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	if err := pw.WriteEntry(PackBlob, []byte("two")); err == nil {
		t.Fatal("expected write-after-finish error")
	}
}

func TestPackWriterFinishWithEntityTrailer(t *testing.T) {
	var buf bytes.Buffer
	pw, err := NewPackWriter(&buf, 1)
	if err != nil {
		t.Fatalf("NewPackWriter: %v", err)
	}

	blob := []byte("hello world")
	blobHash := HashObject(TypeBlob, blob)
	if err := pw.WriteEntry(PackBlob, blob); err != nil {
		t.Fatalf("WriteEntry: %v", err)
	}

	checksum, err := pw.FinishWithEntityTrailer([]PackEntityTrailerEntry{
		{
			ObjectHash: blobHash,
			StableID:   "decl:function_definition::Hello",
		},
	})
	if err != nil {
		t.Fatalf("FinishWithEntityTrailer: %v", err)
	}
	if checksum == "" {
		t.Fatal("expected non-empty pack checksum")
	}

	pf, err := ReadPack(buf.Bytes())
	if err != nil {
		t.Fatalf("ReadPack: %v", err)
	}
	if got := pf.Checksum; got != checksum {
		t.Fatalf("Pack checksum = %s, want %s", got, checksum)
	}
	if pf.EntityTrailer == nil {
		t.Fatal("expected non-nil EntityTrailer")
	}
	if len(pf.EntityTrailer.Entries) != 1 {
		t.Fatalf("len(EntityTrailer.Entries) = %d, want 1", len(pf.EntityTrailer.Entries))
	}
	if got := pf.EntityTrailer.Entries[0].ObjectHash; got != blobHash {
		t.Fatalf("EntityTrailer.Entries[0].ObjectHash = %s, want %s", got, blobHash)
	}
}
