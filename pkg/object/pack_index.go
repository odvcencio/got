package object

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"sort"
)

const (
	packIndexVersion        = 2
	packIndexHeaderSize     = 8
	packIndexFanoutSize     = 256 * 4
	packIndexLargeOffsetBit = uint32(1 << 31)
)

var packIndexMagic = [4]byte{0xff, 't', 'O', 'c'}

// PackIndexEntry is one row in a pack index file.
type PackIndexEntry struct {
	Hash   Hash
	Offset uint64
	CRC32  uint32
}

func normalizePackIndexEntries(entries []PackIndexEntry) ([]PackIndexEntry, error) {
	out := make([]PackIndexEntry, len(entries))
	copy(out, entries)

	for i := range out {
		if _, err := hashHexToBytes(out[i].Hash); err != nil {
			return nil, fmt.Errorf("entry %d: %w", i, err)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Hash < out[j].Hash
	})
	return out, nil
}

func hashHexToBytes(h Hash) ([]byte, error) {
	if len(h) != 64 {
		return nil, fmt.Errorf("hash length must be 64 hex chars, got %d", len(h))
	}
	raw, err := hex.DecodeString(string(h))
	if err != nil {
		return nil, fmt.Errorf("invalid hash %q: %w", h, err)
	}
	return raw, nil
}

// WritePackIndex writes a Git idx v2 style index for the provided entries and
// pack checksum. It returns the hex-encoded index checksum.
func WritePackIndex(w io.Writer, entries []PackIndexEntry, packChecksum Hash) (Hash, error) {
	normalized, err := normalizePackIndexEntries(entries)
	if err != nil {
		return "", err
	}
	packChecksumRaw, err := hashHexToBytes(packChecksum)
	if err != nil {
		return "", fmt.Errorf("pack checksum: %w", err)
	}

	var buf bytes.Buffer
	buf.Write(packIndexMagic[:])
	_ = binary.Write(&buf, binary.BigEndian, uint32(packIndexVersion))

	fanout := buildPackIndexFanout(normalized)
	for i := 0; i < 256; i++ {
		_ = binary.Write(&buf, binary.BigEndian, fanout[i])
	}

	for _, entry := range normalized {
		raw, _ := hashHexToBytes(entry.Hash)
		buf.Write(raw)
	}
	for _, entry := range normalized {
		_ = binary.Write(&buf, binary.BigEndian, entry.CRC32)
	}

	largeOffsets := make([]uint64, 0)
	for _, entry := range normalized {
		if entry.Offset < uint64(packIndexLargeOffsetBit) {
			_ = binary.Write(&buf, binary.BigEndian, uint32(entry.Offset))
			continue
		}

		pos := uint32(len(largeOffsets))
		ref := packIndexLargeOffsetBit | pos
		_ = binary.Write(&buf, binary.BigEndian, ref)
		largeOffsets = append(largeOffsets, entry.Offset)
	}
	for _, offset := range largeOffsets {
		_ = binary.Write(&buf, binary.BigEndian, offset)
	}

	buf.Write(packChecksumRaw)
	indexSum := sha256.Sum256(buf.Bytes())
	buf.Write(indexSum[:])

	if _, err := w.Write(buf.Bytes()); err != nil {
		return "", fmt.Errorf("write pack index: %w", err)
	}
	return Hash(hex.EncodeToString(indexSum[:])), nil
}

func buildPackIndexFanout(entries []PackIndexEntry) [256]uint32 {
	var counts [256]uint32
	for _, entry := range entries {
		raw, _ := hashHexToBytes(entry.Hash)
		counts[int(raw[0])]++
	}

	var fanout [256]uint32
	var total uint32
	for i := 0; i < 256; i++ {
		total += counts[i]
		fanout[i] = total
	}
	return fanout
}
