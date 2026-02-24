package object

import (
	"bytes"
	"crypto/sha256"
	"testing"
)

func TestReadPackRoundTrip(t *testing.T) {
	var buf bytes.Buffer

	pw, err := NewPackWriter(&buf, 2)
	if err != nil {
		t.Fatalf("NewPackWriter: %v", err)
	}
	if err := pw.WriteEntry(PackBlob, []byte("hello")); err != nil {
		t.Fatalf("WriteEntry blob: %v", err)
	}
	if err := pw.WriteEntry(PackCommit, []byte("tree abc\n\nmsg\n")); err != nil {
		t.Fatalf("WriteEntry commit: %v", err)
	}
	if _, err := pw.Finish(); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	pf, err := ReadPack(buf.Bytes())
	if err != nil {
		t.Fatalf("ReadPack: %v", err)
	}
	if pf.Header.NumObjects != 2 {
		t.Fatalf("NumObjects = %d, want 2", pf.Header.NumObjects)
	}
	if len(pf.Entries) != 2 {
		t.Fatalf("len(Entries) = %d, want 2", len(pf.Entries))
	}

	if pf.Entries[0].Type != PackBlob || string(pf.Entries[0].Data) != "hello" {
		t.Fatalf("entry[0] mismatch: %+v", pf.Entries[0])
	}
	if pf.Entries[1].Type != PackCommit || string(pf.Entries[1].Data) != "tree abc\n\nmsg\n" {
		t.Fatalf("entry[1] mismatch: %+v", pf.Entries[1])
	}
}

func TestReadPackRejectsChecksumMismatch(t *testing.T) {
	var buf bytes.Buffer
	pw, err := NewPackWriter(&buf, 1)
	if err != nil {
		t.Fatalf("NewPackWriter: %v", err)
	}
	if err := pw.WriteEntry(PackBlob, []byte("hello")); err != nil {
		t.Fatalf("WriteEntry: %v", err)
	}
	if _, err := pw.Finish(); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	data := append([]byte(nil), buf.Bytes()...)
	data[len(data)-1] ^= 0xff

	if _, err := ReadPack(data); err == nil {
		t.Fatal("expected checksum mismatch error")
	}
}

func TestReadPackRejectsObjectCountMismatch(t *testing.T) {
	var buf bytes.Buffer
	pw, err := NewPackWriter(&buf, 1)
	if err != nil {
		t.Fatalf("NewPackWriter: %v", err)
	}
	if err := pw.WriteEntry(PackBlob, []byte("hello")); err != nil {
		t.Fatalf("WriteEntry: %v", err)
	}
	if _, err := pw.Finish(); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	data := append([]byte(nil), buf.Bytes()...)
	// bump object count from 1 -> 2 and update checksum so count mismatch is hit
	// during decode rather than checksum verification.
	data[11] = 2
	payload := data[:len(data)-32]
	sum := sha256.Sum256(payload)
	copy(data[len(data)-32:], sum[:])

	if _, err := ReadPack(data); err == nil {
		t.Fatal("expected object count mismatch error")
	}
}
