package object

import (
	"bytes"
	"testing"
)

func TestOfsDeltaDistanceRoundTrip(t *testing.T) {
	tests := []uint64{
		1, 2, 10, 127, 128, 255, 1024, 65535, 1 << 20, (1 << 31) + 17,
	}
	for _, want := range tests {
		enc := encodeOfsDeltaDistance(want)
		got, n, err := decodeOfsDeltaDistance(enc)
		if err != nil {
			t.Fatalf("decode distance %d: %v", want, err)
		}
		if got != want {
			t.Fatalf("distance round-trip mismatch: got %d want %d", got, want)
		}
		if n != len(enc) {
			t.Fatalf("distance byte count mismatch: got %d want %d", n, len(enc))
		}
	}
}

func TestBuildInsertOnlyDeltaAppliesToTarget(t *testing.T) {
	base := []byte("hello world\n")
	target := []byte("hello there world\n")

	delta := buildInsertOnlyDelta(base, target)
	got, err := applyDelta(base, delta)
	if err != nil {
		t.Fatalf("applyDelta: %v", err)
	}
	if !bytes.Equal(got, target) {
		t.Fatalf("delta result mismatch: got %q want %q", got, target)
	}
}

func TestPackWriterWriteOfsDeltaRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	pw, err := NewPackWriter(&buf, 2)
	if err != nil {
		t.Fatalf("NewPackWriter: %v", err)
	}

	base := []byte("hello world\n")
	baseOffset := pw.CurrentOffset()
	if err := pw.WriteEntry(PackBlob, base); err != nil {
		t.Fatalf("WriteEntry base: %v", err)
	}

	target := []byte("hello there world\n")
	if err := pw.WriteOfsDelta(baseOffset, base, target); err != nil {
		t.Fatalf("WriteOfsDelta: %v", err)
	}

	if _, err := pw.Finish(); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	pf, err := ReadPack(buf.Bytes())
	if err != nil {
		t.Fatalf("ReadPack: %v", err)
	}
	if len(pf.Entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(pf.Entries))
	}
	if pf.Entries[1].Type != PackOfsDelta {
		t.Fatalf("entry[1] type = %d, want %d", pf.Entries[1].Type, PackOfsDelta)
	}
	if pf.Entries[1].BaseDistance == 0 {
		t.Fatal("expected non-zero delta base distance")
	}

	got, err := applyDelta(base, pf.Entries[1].Data)
	if err != nil {
		t.Fatalf("applyDelta from pack entry: %v", err)
	}
	if !bytes.Equal(got, target) {
		t.Fatalf("reconstructed target mismatch: got %q want %q", got, target)
	}
}

func TestPackWriterWriteOfsDeltaRejectsFutureBaseOffset(t *testing.T) {
	var buf bytes.Buffer
	pw, err := NewPackWriter(&buf, 1)
	if err != nil {
		t.Fatalf("NewPackWriter: %v", err)
	}

	base := []byte("a")
	target := []byte("b")
	if err := pw.WriteOfsDelta(pw.CurrentOffset()+10, base, target); err == nil {
		t.Fatal("expected invalid base offset error")
	}
}
