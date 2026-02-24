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
	packEntityTrailerVersion    uint16 = 1
	packEntityTrailerHeaderSize        = 4 + 2 + 4
	maxPackEntityStableIDSize          = (1 << 16) - 1
)

var packEntityTrailerMagic = [4]byte{'G', 'E', 'N', 'T'}

// PackEntityTrailerEntry maps one object hash to an entity stable identifier.
type PackEntityTrailerEntry struct {
	ObjectHash Hash
	StableID   string
}

// PackEntityTrailer is the parsed Got-specific pack extension trailer.
type PackEntityTrailer struct {
	Version  uint16
	Entries  []PackEntityTrailerEntry
	Checksum Hash
}

// MarshalPackEntityTrailer serializes entries to the Got pack trailer format.
func MarshalPackEntityTrailer(entries []PackEntityTrailerEntry) ([]byte, error) {
	normalized, err := normalizePackEntityTrailerEntries(entries)
	if err != nil {
		return nil, err
	}
	if len(normalized) > int(^uint32(0)) {
		return nil, fmt.Errorf("entity trailer entry count exceeds uint32: %d", len(normalized))
	}

	var buf bytes.Buffer
	buf.Write(packEntityTrailerMagic[:])
	_ = binary.Write(&buf, binary.BigEndian, packEntityTrailerVersion)
	_ = binary.Write(&buf, binary.BigEndian, uint32(len(normalized)))

	for _, entry := range normalized {
		hashRaw, _ := hashHexToBytes(entry.ObjectHash)
		stableIDRaw := []byte(entry.StableID)
		buf.Write(hashRaw)
		_ = binary.Write(&buf, binary.BigEndian, uint16(len(stableIDRaw)))
		buf.Write(stableIDRaw)
	}

	sum := sha256.Sum256(buf.Bytes())
	buf.Write(sum[:])
	return buf.Bytes(), nil
}

// WritePackEntityTrailer writes the encoded trailer and returns its checksum.
func WritePackEntityTrailer(w io.Writer, entries []PackEntityTrailerEntry) (Hash, error) {
	raw, err := MarshalPackEntityTrailer(entries)
	if err != nil {
		return "", err
	}
	if _, err := w.Write(raw); err != nil {
		return "", fmt.Errorf("write entity trailer: %w", err)
	}
	checksumRaw := raw[len(raw)-sha256.Size:]
	return Hash(hex.EncodeToString(checksumRaw)), nil
}

// ReadPackEntityTrailer parses and validates a full trailer byte slice.
func ReadPackEntityTrailer(data []byte) (*PackEntityTrailer, error) {
	if len(data) < packEntityTrailerHeaderSize+sha256.Size {
		return nil, fmt.Errorf("entity trailer too short: %d", len(data))
	}
	if !bytes.Equal(data[:4], packEntityTrailerMagic[:]) {
		return nil, fmt.Errorf("invalid entity trailer magic %q", data[:4])
	}

	body := data[:len(data)-sha256.Size]
	checksumRaw := data[len(data)-sha256.Size:]
	sum := sha256.Sum256(body)
	if !bytes.Equal(sum[:], checksumRaw) {
		return nil, fmt.Errorf("entity trailer checksum mismatch")
	}

	version := binary.BigEndian.Uint16(body[4:6])
	if version != packEntityTrailerVersion {
		return nil, fmt.Errorf("unsupported entity trailer version %d", version)
	}

	count := binary.BigEndian.Uint32(body[6:10])
	offset := packEntityTrailerHeaderSize
	entries := make([]PackEntityTrailerEntry, 0, count)
	for i := uint32(0); i < count; i++ {
		if offset+32+2 > len(body) {
			return nil, fmt.Errorf("entity trailer entry %d truncated", i)
		}
		hashRaw := body[offset : offset+32]
		offset += 32

		stableLen := int(binary.BigEndian.Uint16(body[offset : offset+2]))
		offset += 2
		if offset+stableLen > len(body) {
			return nil, fmt.Errorf("entity trailer entry %d truncated stable id", i)
		}
		entry := PackEntityTrailerEntry{
			ObjectHash: Hash(hex.EncodeToString(hashRaw)),
			StableID:   string(body[offset : offset+stableLen]),
		}
		if err := validatePackEntityTrailerEntry(int(i), entry); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
		offset += stableLen
	}
	if offset != len(body) {
		return nil, fmt.Errorf("entity trailer has trailing data: %d bytes", len(body)-offset)
	}

	return &PackEntityTrailer{
		Version:  version,
		Entries:  entries,
		Checksum: Hash(hex.EncodeToString(checksumRaw)),
	}, nil
}

func normalizePackEntityTrailerEntries(entries []PackEntityTrailerEntry) ([]PackEntityTrailerEntry, error) {
	out := make([]PackEntityTrailerEntry, len(entries))
	copy(out, entries)
	for i := range out {
		if err := validatePackEntityTrailerEntry(i, out[i]); err != nil {
			return nil, err
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ObjectHash == out[j].ObjectHash {
			return out[i].StableID < out[j].StableID
		}
		return out[i].ObjectHash < out[j].ObjectHash
	})
	return out, nil
}

func validatePackEntityTrailerEntry(i int, entry PackEntityTrailerEntry) error {
	if _, err := hashHexToBytes(entry.ObjectHash); err != nil {
		return fmt.Errorf("entry %d: object hash: %w", i, err)
	}
	if entry.StableID == "" {
		return fmt.Errorf("entry %d: stable id is empty", i)
	}
	if len(entry.StableID) > maxPackEntityStableIDSize {
		return fmt.Errorf("entry %d: stable id too long: %d", i, len(entry.StableID))
	}
	return nil
}
