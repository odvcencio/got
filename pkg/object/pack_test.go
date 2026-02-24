package object

import "testing"

func TestPackHeaderRoundTrip(t *testing.T) {
	h := PackHeader{
		Version:    supportedPackVersion,
		NumObjects: 42,
	}

	data := h.Marshal()
	if len(data) != packHeaderSize {
		t.Fatalf("header len = %d, want %d", len(data), packHeaderSize)
	}

	got, err := UnmarshalPackHeader(data)
	if err != nil {
		t.Fatalf("UnmarshalPackHeader: %v", err)
	}
	if got.Version != h.Version || got.NumObjects != h.NumObjects {
		t.Fatalf("round-trip mismatch: got %+v want %+v", got, h)
	}
}

func TestPackHeaderRejectsInvalidMagic(t *testing.T) {
	bad := []byte("JUNK00000000")
	if _, err := UnmarshalPackHeader(bad); err == nil {
		t.Fatal("expected error for invalid magic")
	}
}

func TestPackEntryTypeEncodingRoundTrip(t *testing.T) {
	tests := []struct {
		name    string
		objType PackObjectType
		size    uint64
	}{
		{name: "blob-zero", objType: PackBlob, size: 0},
		{name: "commit-small", objType: PackCommit, size: 127},
		{name: "tree-mid", objType: PackTree, size: 256},
		{name: "blob-large", objType: PackBlob, size: 1 << 20},
		{name: "ofs-delta", objType: PackOfsDelta, size: 100},
		{name: "ref-delta", objType: PackRefDelta, size: 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := encodePackEntryHeader(tt.objType, tt.size)
			gotType, gotSize, consumed := decodePackEntryHeader(data)
			if gotType != tt.objType || gotSize != tt.size {
				t.Fatalf("decode = (%d,%d), want (%d,%d)", gotType, gotSize, tt.objType, tt.size)
			}
			if consumed != len(data) {
				t.Fatalf("consumed = %d, want %d", consumed, len(data))
			}
		})
	}
}
