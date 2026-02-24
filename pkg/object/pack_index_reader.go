package object

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
)

// PackIndex is an in-memory representation of an idx v2 file.
type PackIndex struct {
	fanout        [256]uint32
	entries       []PackIndexEntry
	PackChecksum  Hash
	IndexChecksum Hash
}

// Entries returns a copy of all index entries in lexicographic hash order.
func (idx *PackIndex) Entries() []PackIndexEntry {
	out := make([]PackIndexEntry, len(idx.entries))
	copy(out, idx.entries)
	return out
}

// Find performs fanout-bounded binary search for a hash in the index.
func (idx *PackIndex) Find(h Hash) (PackIndexEntry, bool) {
	raw, err := hashHexToBytes(h)
	if err != nil || len(raw) == 0 {
		return PackIndexEntry{}, false
	}

	bucket := int(raw[0])
	start := uint32(0)
	if bucket > 0 {
		start = idx.fanout[bucket-1]
	}
	end := idx.fanout[bucket]
	if end <= start {
		return PackIndexEntry{}, false
	}

	lo := int(start)
	hi := int(end)
	for lo < hi {
		mid := lo + (hi-lo)/2
		midHash := idx.entries[mid].Hash
		if midHash < h {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	if lo < int(end) && idx.entries[lo].Hash == h {
		return idx.entries[lo], true
	}
	return PackIndexEntry{}, false
}

// ReadPackIndexFromReader parses an idx v2 stream.
func ReadPackIndexFromReader(r io.Reader) (*PackIndex, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("read pack index stream: %w", err)
	}
	return ReadPackIndex(data)
}

// ReadPackIndex parses and validates an idx v2 file.
func ReadPackIndex(data []byte) (*PackIndex, error) {
	minLen := packIndexHeaderSize + packIndexFanoutSize + 64
	if len(data) < minLen {
		return nil, fmt.Errorf("pack index too short: %d", len(data))
	}
	if string(data[:4]) != string(packIndexMagic[:]) {
		return nil, fmt.Errorf("invalid pack index magic %q", data[:4])
	}
	version := binary.BigEndian.Uint32(data[4:8])
	if version != packIndexVersion {
		return nil, fmt.Errorf("unsupported pack index version %d", version)
	}

	gotChecksumRaw := data[len(data)-32:]
	sum := sha256.Sum256(data[:len(data)-32])
	if !equalBytes(gotChecksumRaw, sum[:]) {
		return nil, fmt.Errorf("pack index checksum mismatch")
	}

	var fanout [256]uint32
	cursor := packIndexHeaderSize
	for i := 0; i < 256; i++ {
		fanout[i] = binary.BigEndian.Uint32(data[cursor:])
		cursor += 4
	}
	n := int(fanout[255])

	namesLen := n * 32
	crcLen := n * 4
	offsetLen := n * 4
	if cursor+namesLen+crcLen+offsetLen+64 > len(data) {
		return nil, fmt.Errorf("pack index truncated")
	}

	namesStart := cursor
	namesEnd := namesStart + namesLen
	cursor = namesEnd

	crcStart := cursor
	crcEnd := crcStart + crcLen
	cursor = crcEnd

	offsetStart := cursor
	offsetEnd := offsetStart + offsetLen
	cursor = offsetEnd

	offset32 := make([]uint32, n)
	largeNeeded := uint32(0)
	for i := 0; i < n; i++ {
		v := binary.BigEndian.Uint32(data[offsetStart+(i*4):])
		offset32[i] = v
		if v&packIndexLargeOffsetBit != 0 {
			ref := v & ^packIndexLargeOffsetBit
			if ref+1 > largeNeeded {
				largeNeeded = ref + 1
			}
		}
	}

	largeOffsets := make([]uint64, largeNeeded)
	for i := uint32(0); i < largeNeeded; i++ {
		if cursor+8 > len(data)-64 {
			return nil, fmt.Errorf("pack index large-offset table truncated")
		}
		largeOffsets[i] = binary.BigEndian.Uint64(data[cursor:])
		cursor += 8
	}

	if cursor+64 != len(data) {
		return nil, fmt.Errorf("pack index trailing data: %d bytes", len(data)-(cursor+64))
	}

	packChecksumRaw := data[cursor : cursor+32]
	cursor += 32
	indexChecksumRaw := data[cursor : cursor+32]

	entries := make([]PackIndexEntry, n)
	for i := 0; i < n; i++ {
		hashRaw := data[namesStart+(i*32) : namesStart+((i+1)*32)]
		offset := uint64(offset32[i])
		if offset32[i]&packIndexLargeOffsetBit != 0 {
			ref := offset32[i] & ^packIndexLargeOffsetBit
			if int(ref) >= len(largeOffsets) {
				return nil, fmt.Errorf("pack index invalid large offset reference %d", ref)
			}
			offset = largeOffsets[ref]
		}
		entries[i] = PackIndexEntry{
			Hash:   Hash(hex.EncodeToString(hashRaw)),
			CRC32:  binary.BigEndian.Uint32(data[crcStart+(i*4):]),
			Offset: offset,
		}
	}

	return &PackIndex{
		fanout:        fanout,
		entries:       entries,
		PackChecksum:  Hash(hex.EncodeToString(packChecksumRaw)),
		IndexChecksum: Hash(hex.EncodeToString(indexChecksumRaw)),
	}, nil
}

func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
