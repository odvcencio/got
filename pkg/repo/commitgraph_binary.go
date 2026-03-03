package repo

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"sort"

	"github.com/odvcencio/graft/pkg/object"
)

// Binary commit-graph format (GCG1):
//
//   Header (16 bytes):
//     Magic      [4]byte   "GCG1"
//     Version    uint32    1 (big-endian)
//     Count      uint32    number of entries (big-endian)
//     Reserved   uint32    0
//
//   Fanout table (256 * 4 = 1024 bytes):
//     fanout[i]  uint32    cumulative count of entries with rawHash[0] <= i
//
//   Entry section (Count * 141 bytes, sorted by raw hash):
//     Hash       [32]byte  commit hash (raw SHA-256)
//     TreeHash   [32]byte  tree hash (raw SHA-256)
//     Generation uint32    generation number (big-endian)
//     Timestamp  int64     unix timestamp (big-endian)
//     ParentCnt  uint8     0..2 = inline parents; 255 = overflow
//     Parent1    [32]byte  first parent (or overflow offset as uint32 in first 4 bytes, zero-padded)
//     Parent2    [32]byte  second parent (unused if overflow or < 2 parents)
//
//   Overflow section (variable):
//     For each overflow entry:
//       Count    uint32    number of parents (big-endian)
//       Parents  N*[32]byte parent hashes (raw SHA-256)
//
//   Trailer (32 bytes):
//     Checksum   [32]byte  SHA-256 of all preceding bytes

const (
	binaryMagic       = "GCG1"
	binaryVersion     = 1
	binaryHeaderSize  = 16
	binaryFanoutSize  = 256 * 4
	binaryEntrySize   = 32 + 32 + 4 + 8 + 1 + 32 + 32 // 141 bytes
	binaryChecksumLen = 32
	overflowSentinel  = 255
)

// WriteBinaryCommitGraph writes the commit graph entries to path in binary
// format. Entries are sorted by raw hash for the fanout table to work.
func WriteBinaryCommitGraph(path string, entries map[object.Hash]*CommitGraphEntry) error {
	// Collect and sort entries by raw hash bytes.
	type sortedEntry struct {
		hash    object.Hash
		rawHash [32]byte
		entry   *CommitGraphEntry
	}

	sorted := make([]sortedEntry, 0, len(entries))
	for h, e := range entries {
		raw, err := hashToRaw(h)
		if err != nil {
			return fmt.Errorf("binary commit graph: invalid hash %q: %w", h, err)
		}
		sorted = append(sorted, sortedEntry{hash: h, rawHash: raw, entry: e})
	}

	sort.Slice(sorted, func(i, j int) bool {
		return compareBytesLess(sorted[i].rawHash[:], sorted[j].rawHash[:])
	})

	count := uint32(len(sorted))

	// Build fanout table: fanout[b] = count of entries with rawHash[0] <= b.
	var fanout [256]uint32
	{
		idx := 0
		for b := 0; b < 256; b++ {
			for idx < len(sorted) && sorted[idx].rawHash[0] <= byte(b) {
				idx++
			}
			fanout[b] = uint32(idx)
		}
	}

	// Build overflow section: collect entries with >2 parents.
	// We need to know offsets before writing entries, so pre-compute.
	overflowOffsets := make(map[int]uint32)
	var overflowBuf []byte

	for i, se := range sorted {
		if len(se.entry.Parents) > 2 {
			overflowOffsets[i] = uint32(len(overflowBuf))

			// Write count + parent hashes to overflow buffer.
			var countBuf [4]byte
			binary.BigEndian.PutUint32(countBuf[:], uint32(len(se.entry.Parents)))
			overflowBuf = append(overflowBuf, countBuf[:]...)
			for _, p := range se.entry.Parents {
				raw, err := hashToRaw(p)
				if err != nil {
					return fmt.Errorf("binary commit graph: overflow parent hash %q: %w", p, err)
				}
				overflowBuf = append(overflowBuf, raw[:]...)
			}
		}
	}

	// Calculate total size.
	totalSize := binaryHeaderSize + binaryFanoutSize + int(count)*binaryEntrySize + len(overflowBuf) + binaryChecksumLen

	buf := make([]byte, 0, totalSize)

	// Write header.
	buf = append(buf, []byte(binaryMagic)...)
	buf = appendUint32(buf, binaryVersion)
	buf = appendUint32(buf, count)
	buf = appendUint32(buf, 0) // reserved

	// Write fanout table.
	for i := 0; i < 256; i++ {
		buf = appendUint32(buf, fanout[i])
	}

	// Write entries.
	for i, se := range sorted {
		// Hash
		buf = append(buf, se.rawHash[:]...)

		// TreeHash
		treeRaw, err := hashToRaw(se.entry.TreeHash)
		if err != nil {
			return fmt.Errorf("binary commit graph: tree hash %q: %w", se.entry.TreeHash, err)
		}
		buf = append(buf, treeRaw[:]...)

		// Generation
		buf = appendUint32(buf, se.entry.Generation)

		// Timestamp
		buf = appendInt64(buf, se.entry.Timestamp)

		// Parent count + parents
		nParents := len(se.entry.Parents)
		switch {
		case nParents == 0:
			buf = append(buf, 0)
			buf = append(buf, make([]byte, 64)...) // 2 empty parent slots
		case nParents == 1:
			buf = append(buf, 1)
			p1Raw, err := hashToRaw(se.entry.Parents[0])
			if err != nil {
				return fmt.Errorf("binary commit graph: parent hash: %w", err)
			}
			buf = append(buf, p1Raw[:]...)
			buf = append(buf, make([]byte, 32)...) // empty parent2 slot
		case nParents == 2:
			buf = append(buf, 2)
			p1Raw, err := hashToRaw(se.entry.Parents[0])
			if err != nil {
				return fmt.Errorf("binary commit graph: parent1 hash: %w", err)
			}
			buf = append(buf, p1Raw[:]...)
			p2Raw, err := hashToRaw(se.entry.Parents[1])
			if err != nil {
				return fmt.Errorf("binary commit graph: parent2 hash: %w", err)
			}
			buf = append(buf, p2Raw[:]...)
		default:
			// Overflow: sentinel + offset in parent1 slot.
			buf = append(buf, overflowSentinel)
			var offsetBuf [32]byte
			binary.BigEndian.PutUint32(offsetBuf[:4], overflowOffsets[i])
			buf = append(buf, offsetBuf[:]...)
			buf = append(buf, make([]byte, 32)...) // unused parent2 slot
		}
	}

	// Write overflow section.
	buf = append(buf, overflowBuf...)

	// Write checksum (SHA-256 of everything before it).
	checksum := sha256.Sum256(buf)
	buf = append(buf, checksum[:]...)

	// Atomic write via temp file + rename.
	tmp, err := os.CreateTemp("", "commit-graph.tmp.*")
	if err != nil {
		return fmt.Errorf("binary commit graph: create temp: %w", err)
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(buf); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("binary commit graph: write: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("binary commit graph: sync: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("binary commit graph: close: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("binary commit graph: rename: %w", err)
	}

	return nil
}

// ReadBinaryCommitGraph reads a binary-format commit graph file and returns
// the entries as a map.
func ReadBinaryCommitGraph(path string) (map[object.Hash]*CommitGraphEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("binary commit graph: read: %w", err)
	}

	if len(data) < binaryHeaderSize+binaryFanoutSize+binaryChecksumLen {
		return nil, fmt.Errorf("binary commit graph: file too small (%d bytes)", len(data))
	}

	// Verify magic.
	if string(data[:4]) != binaryMagic {
		return nil, fmt.Errorf("binary commit graph: bad magic %q", string(data[:4]))
	}

	// Verify checksum.
	checksumOffset := len(data) - binaryChecksumLen
	expectedChecksum := sha256.Sum256(data[:checksumOffset])
	var storedChecksum [32]byte
	copy(storedChecksum[:], data[checksumOffset:])
	if expectedChecksum != storedChecksum {
		return nil, fmt.Errorf("binary commit graph: checksum mismatch")
	}

	// Parse header.
	version := binary.BigEndian.Uint32(data[4:8])
	if version != binaryVersion {
		return nil, fmt.Errorf("binary commit graph: unsupported version %d", version)
	}
	count := binary.BigEndian.Uint32(data[8:12])
	// reserved := binary.BigEndian.Uint32(data[12:16])

	// Validate size.
	entryStart := binaryHeaderSize + binaryFanoutSize
	entryEnd := entryStart + int(count)*binaryEntrySize
	if entryEnd > checksumOffset {
		return nil, fmt.Errorf("binary commit graph: entry section overflows file")
	}

	overflowStart := entryEnd
	overflowData := data[overflowStart:checksumOffset]

	// Parse entries.
	entries := make(map[object.Hash]*CommitGraphEntry, count)

	for i := uint32(0); i < count; i++ {
		off := entryStart + int(i)*binaryEntrySize

		// Hash
		var rawHash [32]byte
		copy(rawHash[:], data[off:off+32])
		h := rawToHash(rawHash)
		off += 32

		// TreeHash
		var rawTree [32]byte
		copy(rawTree[:], data[off:off+32])
		treeHash := rawToHash(rawTree)
		off += 32

		// Generation
		gen := binary.BigEndian.Uint32(data[off : off+4])
		off += 4

		// Timestamp
		ts := int64(binary.BigEndian.Uint64(data[off : off+8]))
		off += 8

		// Parent count
		parentCnt := data[off]
		off += 1

		var rawP1, rawP2 [32]byte
		copy(rawP1[:], data[off:off+32])
		off += 32
		copy(rawP2[:], data[off:off+32])

		var parents []object.Hash

		switch {
		case parentCnt == 0:
			// No parents.
		case parentCnt == 1:
			parents = []object.Hash{rawToHash(rawP1)}
		case parentCnt == 2:
			parents = []object.Hash{rawToHash(rawP1), rawToHash(rawP2)}
		case parentCnt == overflowSentinel:
			// Overflow: read from overflow section.
			overflowOff := binary.BigEndian.Uint32(rawP1[:4])
			if int(overflowOff)+4 > len(overflowData) {
				return nil, fmt.Errorf("binary commit graph: overflow offset %d out of range", overflowOff)
			}
			pCount := binary.BigEndian.Uint32(overflowData[overflowOff : overflowOff+4])
			overflowOff += 4
			if int(overflowOff)+int(pCount)*32 > len(overflowData) {
				return nil, fmt.Errorf("binary commit graph: overflow parents overflow")
			}
			parents = make([]object.Hash, pCount)
			for j := uint32(0); j < pCount; j++ {
				var rawP [32]byte
				copy(rawP[:], overflowData[overflowOff:overflowOff+32])
				parents[j] = rawToHash(rawP)
				overflowOff += 32
			}
		default:
			return nil, fmt.Errorf("binary commit graph: invalid parent count %d", parentCnt)
		}

		entries[h] = &CommitGraphEntry{
			TreeHash:   treeHash,
			Parents:    parents,
			Generation: gen,
			Timestamp:  ts,
		}
	}

	return entries, nil
}

// isBinaryCommitGraph checks if data starts with the binary magic bytes.
func isBinaryCommitGraph(data []byte) bool {
	return len(data) >= 4 && string(data[:4]) == binaryMagic
}

// hashToRaw converts a hex-encoded Hash to a raw [32]byte.
func hashToRaw(h object.Hash) ([32]byte, error) {
	var raw [32]byte
	s := string(h)
	if len(s) != 64 {
		// Accept empty hash as zero bytes.
		if s == "" {
			return raw, nil
		}
		return raw, fmt.Errorf("invalid hash length %d", len(s))
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		return raw, err
	}
	copy(raw[:], b)
	return raw, nil
}

// rawToHash converts a raw [32]byte to a hex-encoded Hash.
func rawToHash(raw [32]byte) object.Hash {
	return object.Hash(hex.EncodeToString(raw[:]))
}

// compareBytesLess returns true if a < b lexicographically.
func compareBytesLess(a, b []byte) bool {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] < b[i] {
			return true
		}
		if a[i] > b[i] {
			return false
		}
	}
	return len(a) < len(b)
}

// appendUint32 appends a big-endian uint32 to the byte slice.
func appendUint32(buf []byte, v uint32) []byte {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], v)
	return append(buf, b[:]...)
}

// appendInt64 appends a big-endian int64 to the byte slice.
func appendInt64(buf []byte, v int64) []byte {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], uint64(v))
	return append(buf, b[:]...)
}
