package object

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"testing"
)

func TestWritePackIndexHeaderFanoutAndSorting(t *testing.T) {
	entries := []PackIndexEntry{
		{
			Hash:   Hash("ff" + repeatHex("00", 31)),
			Offset: 32,
			CRC32:  0x33333333,
		},
		{
			Hash:   Hash("01" + repeatHex("00", 31)),
			Offset: 16,
			CRC32:  0x11111111,
		},
		{
			Hash:   Hash("10" + repeatHex("00", 31)),
			Offset: 24,
			CRC32:  0x22222222,
		},
	}
	packChecksum := Hash(repeatHex("ab", 32))

	var buf bytes.Buffer
	if _, err := WritePackIndex(&buf, entries, packChecksum); err != nil {
		t.Fatalf("WritePackIndex: %v", err)
	}
	data := buf.Bytes()

	if len(data) < packIndexHeaderSize+packIndexFanoutSize+64 {
		t.Fatalf("index output too short: %d", len(data))
	}
	if !bytes.Equal(data[:4], packIndexMagic[:]) {
		t.Fatalf("magic = %x, want %x", data[:4], packIndexMagic)
	}
	version := binary.BigEndian.Uint32(data[4:8])
	if version != packIndexVersion {
		t.Fatalf("version = %d, want %d", version, packIndexVersion)
	}

	fanoutStart := packIndexHeaderSize
	fanout := data[fanoutStart : fanoutStart+packIndexFanoutSize]
	if got := binary.BigEndian.Uint32(fanout[0*4:]); got != 0 {
		t.Fatalf("fanout[0] = %d, want 0", got)
	}
	if got := binary.BigEndian.Uint32(fanout[1*4:]); got != 1 {
		t.Fatalf("fanout[1] = %d, want 1", got)
	}
	if got := binary.BigEndian.Uint32(fanout[0x10*4:]); got != 2 {
		t.Fatalf("fanout[0x10] = %d, want 2", got)
	}
	if got := binary.BigEndian.Uint32(fanout[0xff*4:]); got != 3 {
		t.Fatalf("fanout[0xff] = %d, want 3", got)
	}

	namesStart := packIndexHeaderSize + packIndexFanoutSize
	nameSize := 32
	nameCount := len(entries)
	namesEnd := namesStart + (nameCount * nameSize)
	nameTable := data[namesStart:namesEnd]

	got1 := hex.EncodeToString(nameTable[0:32])
	got2 := hex.EncodeToString(nameTable[32:64])
	got3 := hex.EncodeToString(nameTable[64:96])
	want1 := "01" + repeatHex("00", 31)
	want2 := "10" + repeatHex("00", 31)
	want3 := "ff" + repeatHex("00", 31)
	if got1 != want1 || got2 != want2 || got3 != want3 {
		t.Fatalf("name order mismatch: got [%s %s %s]", got1, got2, got3)
	}
}

func TestWritePackIndexChecksums(t *testing.T) {
	entries := []PackIndexEntry{
		{
			Hash:   Hash("42" + repeatHex("00", 31)),
			Offset: 123,
			CRC32:  0xabcdef12,
		},
	}
	packChecksum := Hash(repeatHex("cd", 32))

	var buf bytes.Buffer
	gotIndexChecksum, err := WritePackIndex(&buf, entries, packChecksum)
	if err != nil {
		t.Fatalf("WritePackIndex: %v", err)
	}
	data := buf.Bytes()
	if len(data) < 64 {
		t.Fatalf("index too short: %d", len(data))
	}

	packChecksumRaw, err := hex.DecodeString(string(packChecksum))
	if err != nil {
		t.Fatalf("decode pack checksum: %v", err)
	}
	gotPackChecksum := data[len(data)-64 : len(data)-32]
	if !bytes.Equal(gotPackChecksum, packChecksumRaw) {
		t.Fatalf("pack checksum mismatch: got %x want %x", gotPackChecksum, packChecksumRaw)
	}

	gotIndexRaw := data[len(data)-32:]
	expectedIndex := sha256.Sum256(data[:len(data)-32])
	if !bytes.Equal(gotIndexRaw, expectedIndex[:]) {
		t.Fatalf("index checksum mismatch: got %x want %x", gotIndexRaw, expectedIndex)
	}
	if string(gotIndexChecksum) != hex.EncodeToString(expectedIndex[:]) {
		t.Fatalf("returned index checksum mismatch: got %s want %s", gotIndexChecksum, hex.EncodeToString(expectedIndex[:]))
	}
}

func TestWritePackIndexLargeOffsets(t *testing.T) {
	entries := []PackIndexEntry{
		{
			Hash:   Hash("20" + repeatHex("00", 31)),
			Offset: 0x20,
		},
		{
			Hash:   Hash("30" + repeatHex("00", 31)),
			Offset: uint64(packIndexLargeOffsetBit) + 123,
		},
	}
	packChecksum := Hash(repeatHex("ef", 32))

	var buf bytes.Buffer
	if _, err := WritePackIndex(&buf, entries, packChecksum); err != nil {
		t.Fatalf("WritePackIndex: %v", err)
	}
	data := buf.Bytes()

	namesStart := packIndexHeaderSize + packIndexFanoutSize
	offsetTableStart := namesStart + (len(entries) * 32) + (len(entries) * 4)
	offset1 := binary.BigEndian.Uint32(data[offsetTableStart:])
	offset2 := binary.BigEndian.Uint32(data[offsetTableStart+4:])

	if offset1 != 0x20 {
		t.Fatalf("offset1 = %d, want %d", offset1, 0x20)
	}
	if offset2&packIndexLargeOffsetBit == 0 {
		t.Fatalf("offset2 expected large offset marker, got 0x%x", offset2)
	}
	index := offset2 & ^packIndexLargeOffsetBit
	if index != 0 {
		t.Fatalf("offset2 large index = %d, want 0", index)
	}

	largeOffsetStart := offsetTableStart + (len(entries) * 4)
	largeOffset := binary.BigEndian.Uint64(data[largeOffsetStart:])
	if largeOffset != uint64(packIndexLargeOffsetBit)+123 {
		t.Fatalf("large offset = %d, want %d", largeOffset, uint64(packIndexLargeOffsetBit)+123)
	}
}

func TestWritePackIndexRejectsDuplicateHashes(t *testing.T) {
	dup := Hash("20" + repeatHex("00", 31))
	entries := []PackIndexEntry{
		{Hash: dup, Offset: 1},
		{Hash: dup, Offset: 2},
	}
	packChecksum := Hash(repeatHex("ef", 32))

	var buf bytes.Buffer
	if _, err := WritePackIndex(&buf, entries, packChecksum); err == nil {
		t.Fatal("expected duplicate hash error")
	}
}

func repeatHex(h string, n int) string {
	var b bytes.Buffer
	for i := 0; i < n; i++ {
		b.WriteString(h)
	}
	return b.String()
}
