package object

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"strings"
	"testing"
)

func TestPackEntityTrailerRoundTrip(t *testing.T) {
	entries := []PackEntityTrailerEntry{
		{
			ObjectHash: Hash("ff" + repeatHex("00", 31)),
			StableID:   "decl:function_definition::Z",
		},
		{
			ObjectHash: Hash("01" + repeatHex("00", 31)),
			StableID:   "decl:function_definition::B",
		},
		{
			ObjectHash: Hash("01" + repeatHex("00", 31)),
			StableID:   "decl:function_definition::A",
		},
	}

	raw, err := MarshalPackEntityTrailer(entries)
	if err != nil {
		t.Fatalf("MarshalPackEntityTrailer: %v", err)
	}
	trailer, err := ReadPackEntityTrailer(raw)
	if err != nil {
		t.Fatalf("ReadPackEntityTrailer: %v", err)
	}

	if trailer.Version != packEntityTrailerVersion {
		t.Fatalf("Version = %d, want %d", trailer.Version, packEntityTrailerVersion)
	}
	if len(trailer.Entries) != 3 {
		t.Fatalf("len(Entries) = %d, want 3", len(trailer.Entries))
	}

	if trailer.Entries[0].ObjectHash != Hash("01"+repeatHex("00", 31)) || trailer.Entries[0].StableID != "decl:function_definition::A" {
		t.Fatalf("entry[0] mismatch: %+v", trailer.Entries[0])
	}
	if trailer.Entries[1].ObjectHash != Hash("01"+repeatHex("00", 31)) || trailer.Entries[1].StableID != "decl:function_definition::B" {
		t.Fatalf("entry[1] mismatch: %+v", trailer.Entries[1])
	}
	if trailer.Entries[2].ObjectHash != Hash("ff"+repeatHex("00", 31)) || trailer.Entries[2].StableID != "decl:function_definition::Z" {
		t.Fatalf("entry[2] mismatch: %+v", trailer.Entries[2])
	}

	expectedSum := sha256.Sum256(raw[:len(raw)-sha256.Size])
	if got, want := trailer.Checksum, Hash(hex.EncodeToString(expectedSum[:])); got != want {
		t.Fatalf("Checksum = %s, want %s", got, want)
	}
}

func TestWritePackEntityTrailerWritesChecksum(t *testing.T) {
	entries := []PackEntityTrailerEntry{
		{
			ObjectHash: Hash("10" + repeatHex("00", 31)),
			StableID:   "decl:function_definition::Main",
		},
	}

	var buf bytes.Buffer
	writtenChecksum, err := WritePackEntityTrailer(&buf, entries)
	if err != nil {
		t.Fatalf("WritePackEntityTrailer: %v", err)
	}

	trailer, err := ReadPackEntityTrailer(buf.Bytes())
	if err != nil {
		t.Fatalf("ReadPackEntityTrailer: %v", err)
	}
	if trailer.Checksum != writtenChecksum {
		t.Fatalf("checksum mismatch: read %s write %s", trailer.Checksum, writtenChecksum)
	}
}

func TestMarshalPackEntityTrailerRejectsInvalidEntry(t *testing.T) {
	tooLong := strings.Repeat("a", maxPackEntityStableIDSize+1)

	tests := []struct {
		name    string
		entries []PackEntityTrailerEntry
	}{
		{
			name: "invalid hash",
			entries: []PackEntityTrailerEntry{
				{ObjectHash: Hash("abcd"), StableID: "ok"},
			},
		},
		{
			name: "empty stable id",
			entries: []PackEntityTrailerEntry{
				{ObjectHash: Hash("10" + repeatHex("00", 31)), StableID: ""},
			},
		},
		{
			name: "stable id too long",
			entries: []PackEntityTrailerEntry{
				{ObjectHash: Hash("10" + repeatHex("00", 31)), StableID: tooLong},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := MarshalPackEntityTrailer(tc.entries); err == nil {
				t.Fatal("expected marshal validation error")
			}
		})
	}
}

func TestReadPackEntityTrailerRejectsBadMagic(t *testing.T) {
	raw := mustMarshalPackEntityTrailerForTests(t, []PackEntityTrailerEntry{
		{ObjectHash: Hash("20" + repeatHex("00", 31)), StableID: "decl:function_definition::X"},
	})
	raw[0] = 'B'

	if _, err := ReadPackEntityTrailer(raw); err == nil {
		t.Fatal("expected bad magic error")
	}
}

func TestReadPackEntityTrailerRejectsChecksumMismatch(t *testing.T) {
	raw := mustMarshalPackEntityTrailerForTests(t, []PackEntityTrailerEntry{
		{ObjectHash: Hash("30" + repeatHex("00", 31)), StableID: "decl:function_definition::X"},
	})
	raw[len(raw)-1] ^= 0xff

	if _, err := ReadPackEntityTrailer(raw); err == nil {
		t.Fatal("expected checksum mismatch")
	}
}

func TestReadPackEntityTrailerRejectsUnsupportedVersion(t *testing.T) {
	raw := mustMarshalPackEntityTrailerForTests(t, []PackEntityTrailerEntry{
		{ObjectHash: Hash("40" + repeatHex("00", 31)), StableID: "decl:function_definition::X"},
	})
	body := append([]byte(nil), raw[:len(raw)-sha256.Size]...)
	binary.BigEndian.PutUint16(body[4:6], 99)
	raw = appendEntityTrailerChecksum(body)

	if _, err := ReadPackEntityTrailer(raw); err == nil {
		t.Fatal("expected unsupported version error")
	}
}

func TestReadPackEntityTrailerRejectsTruncatedEntry(t *testing.T) {
	raw := mustMarshalPackEntityTrailerForTests(t, []PackEntityTrailerEntry{
		{ObjectHash: Hash("50" + repeatHex("00", 31)), StableID: "decl:function_definition::X"},
	})
	body := append([]byte(nil), raw[:len(raw)-sha256.Size]...)
	binary.BigEndian.PutUint32(body[6:10], 2)
	raw = appendEntityTrailerChecksum(body)

	if _, err := ReadPackEntityTrailer(raw); err == nil {
		t.Fatal("expected truncated entry error")
	}
}

func mustMarshalPackEntityTrailerForTests(t *testing.T, entries []PackEntityTrailerEntry) []byte {
	t.Helper()
	raw, err := MarshalPackEntityTrailer(entries)
	if err != nil {
		t.Fatalf("MarshalPackEntityTrailer: %v", err)
	}
	return raw
}

func appendEntityTrailerChecksum(body []byte) []byte {
	sum := sha256.Sum256(body)
	out := append([]byte(nil), body...)
	out = append(out, sum[:]...)
	return out
}
