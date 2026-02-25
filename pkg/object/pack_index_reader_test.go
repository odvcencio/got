package object

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"testing"
)

func TestReadPackIndexRoundTripAndFind(t *testing.T) {
	entries := []PackIndexEntry{
		{Hash: Hash("02" + repeatHex("00", 31)), Offset: 8, CRC32: 0x11111111},
		{Hash: Hash("20" + repeatHex("00", 31)), Offset: uint64(packIndexLargeOffsetBit) + 9, CRC32: 0x22222222},
		{Hash: Hash("10" + repeatHex("00", 31)), Offset: 7, CRC32: 0x33333333},
	}
	packChecksum := Hash(repeatHex("aa", 32))

	var buf bytes.Buffer
	indexChecksum, err := WritePackIndex(&buf, entries, packChecksum)
	if err != nil {
		t.Fatalf("WritePackIndex: %v", err)
	}

	idx, err := ReadPackIndex(buf.Bytes())
	if err != nil {
		t.Fatalf("ReadPackIndex: %v", err)
	}

	if idx.PackChecksum != packChecksum {
		t.Fatalf("PackChecksum = %s, want %s", idx.PackChecksum, packChecksum)
	}
	if idx.IndexChecksum != indexChecksum {
		t.Fatalf("IndexChecksum = %s, want %s", idx.IndexChecksum, indexChecksum)
	}

	// Entries must be sorted by hash in index representation.
	for i := 1; i < len(idx.entries); i++ {
		if idx.entries[i-1].Hash > idx.entries[i].Hash {
			t.Fatalf("entries not sorted at %d: %s > %s", i, idx.entries[i-1].Hash, idx.entries[i].Hash)
		}
	}

	found, ok := idx.Find(Hash("10" + repeatHex("00", 31)))
	if !ok {
		t.Fatal("expected to find hash 10..")
	}
	if found.Offset != 7 || found.CRC32 != 0x33333333 {
		t.Fatalf("unexpected found entry: %+v", found)
	}

	found, ok = idx.Find(Hash("20" + repeatHex("00", 31)))
	if !ok {
		t.Fatal("expected to find hash 20..")
	}
	if found.Offset != uint64(packIndexLargeOffsetBit)+9 {
		t.Fatalf("large offset mismatch: got %d", found.Offset)
	}

	if _, ok := idx.Find(Hash("ff" + repeatHex("00", 31))); ok {
		t.Fatal("unexpected hit for missing hash")
	}
}

func TestReadPackIndexRejectsChecksumMismatch(t *testing.T) {
	entries := []PackIndexEntry{{Hash: Hash("10" + repeatHex("00", 31)), Offset: 1}}
	packChecksum := Hash(repeatHex("aa", 32))

	var buf bytes.Buffer
	if _, err := WritePackIndex(&buf, entries, packChecksum); err != nil {
		t.Fatalf("WritePackIndex: %v", err)
	}

	data := append([]byte(nil), buf.Bytes()...)
	data[len(data)-1] ^= 0xff

	if _, err := ReadPackIndex(data); err == nil {
		t.Fatal("expected checksum mismatch")
	}
}

func TestReadPackIndexRejectsBadMagic(t *testing.T) {
	var bad bytes.Buffer
	bad.WriteString("JUNK")
	bad.Write(make([]byte, minPackIndexPayloadSize()-4))

	// Rebuild trailing checksum so the parser reaches magic validation first.
	data := bad.Bytes()
	sum := sha256.Sum256(data[:len(data)-32])
	copy(data[len(data)-32:], sum[:])

	if _, err := ReadPackIndex(data); err == nil {
		t.Fatal("expected bad magic error")
	}
}

func minPackIndexPayloadSize() int {
	return packIndexHeaderSize + packIndexFanoutSize + 64
}

func TestReadPackIndexFromReader(t *testing.T) {
	entries := []PackIndexEntry{{Hash: Hash("55" + repeatHex("00", 31)), Offset: 3}}
	packChecksum := Hash(repeatHex("ab", 32))
	var buf bytes.Buffer
	if _, err := WritePackIndex(&buf, entries, packChecksum); err != nil {
		t.Fatalf("WritePackIndex: %v", err)
	}

	idx, err := ReadPackIndexFromReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("ReadPackIndexFromReader: %v", err)
	}

	if _, ok := idx.Find(entries[0].Hash); !ok {
		t.Fatal("expected to find entry by hash")
	}
}

func TestReadPackIndexChecksumFieldMatchesTrailer(t *testing.T) {
	entries := []PackIndexEntry{{Hash: Hash("66" + repeatHex("00", 31)), Offset: 4}}
	packChecksum := Hash(repeatHex("ef", 32))
	var buf bytes.Buffer
	if _, err := WritePackIndex(&buf, entries, packChecksum); err != nil {
		t.Fatalf("WritePackIndex: %v", err)
	}

	data := buf.Bytes()
	idx, err := ReadPackIndex(data)
	if err != nil {
		t.Fatalf("ReadPackIndex: %v", err)
	}
	gotTrailer := hex.EncodeToString(data[len(data)-32:])
	if string(idx.IndexChecksum) != gotTrailer {
		t.Fatalf("index checksum mismatch: got %s want %s", idx.IndexChecksum, gotTrailer)
	}
}

func TestReadPackIndexRejectsUnsortedHashTable(t *testing.T) {
	entries := []PackIndexEntry{
		{Hash: Hash("10" + repeatHex("00", 31)), Offset: 1},
		{Hash: Hash("10" + repeatHex("ff", 31)), Offset: 2},
	}
	packChecksum := Hash(repeatHex("aa", 32))

	var buf bytes.Buffer
	if _, err := WritePackIndex(&buf, entries, packChecksum); err != nil {
		t.Fatalf("WritePackIndex: %v", err)
	}
	data := append([]byte(nil), buf.Bytes()...)

	namesStart := packIndexHeaderSize + packIndexFanoutSize
	first := append([]byte(nil), data[namesStart:namesStart+32]...)
	second := append([]byte(nil), data[namesStart+32:namesStart+64]...)
	copy(data[namesStart:namesStart+32], second)
	copy(data[namesStart+32:namesStart+64], first)

	sum := sha256.Sum256(data[:len(data)-32])
	copy(data[len(data)-32:], sum[:])

	if _, err := ReadPackIndex(data); err == nil {
		t.Fatal("expected unsorted hash table error")
	}
}

func TestReadPackIndexRejectsFanoutMismatch(t *testing.T) {
	entries := []PackIndexEntry{
		{Hash: Hash("10" + repeatHex("00", 31)), Offset: 1},
	}
	packChecksum := Hash(repeatHex("aa", 32))

	var buf bytes.Buffer
	if _, err := WritePackIndex(&buf, entries, packChecksum); err != nil {
		t.Fatalf("WritePackIndex: %v", err)
	}
	data := append([]byte(nil), buf.Bytes()...)

	fanoutStart := packIndexHeaderSize
	binary.BigEndian.PutUint32(data[fanoutStart+(0x0f*4):], 1)

	sum := sha256.Sum256(data[:len(data)-32])
	copy(data[len(data)-32:], sum[:])

	if _, err := ReadPackIndex(data); err == nil {
		t.Fatal("expected fanout mismatch error")
	}
}
