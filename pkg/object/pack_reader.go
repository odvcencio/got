package object

import (
	"bytes"
	"compress/zlib"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
)

// PackEntry represents one object entry in a pack stream.
type PackEntry struct {
	Type PackObjectType
	Size uint64
	Data []byte
}

// PackFile is the decoded content of a full pack stream.
type PackFile struct {
	Header   PackHeader
	Entries  []PackEntry
	Checksum Hash
}

// ReadPack parses a full pack file byte slice, verifies trailer checksum, and
// returns decoded entries.
func ReadPack(data []byte) (*PackFile, error) {
	if len(data) < packHeaderSize+sha256.Size {
		return nil, fmt.Errorf("pack too short: %d", len(data))
	}

	payload := data[:len(data)-sha256.Size]
	trailer := data[len(data)-sha256.Size:]

	sum := sha256.Sum256(payload)
	if !bytes.Equal(sum[:], trailer) {
		return nil, fmt.Errorf("pack checksum mismatch")
	}

	header, err := UnmarshalPackHeader(payload[:packHeaderSize])
	if err != nil {
		return nil, err
	}

	offset := packHeaderSize
	entries := make([]PackEntry, 0, header.NumObjects)
	for i := uint32(0); i < header.NumObjects; i++ {
		objType, size, n, err := decodePackEntryHeaderStrict(payload[offset:])
		if err != nil {
			return nil, fmt.Errorf("entry %d: %w", i, err)
		}
		offset += n
		if offset >= len(payload) {
			return nil, fmt.Errorf("entry %d: missing compressed payload", i)
		}

		sub := bytes.NewReader(payload[offset:])
		zr, err := zlib.NewReader(sub)
		if err != nil {
			return nil, fmt.Errorf("entry %d: zlib reader: %w", i, err)
		}
		raw, err := io.ReadAll(zr)
		if err != nil {
			_ = zr.Close()
			return nil, fmt.Errorf("entry %d: decompress: %w", i, err)
		}
		if err := zr.Close(); err != nil {
			return nil, fmt.Errorf("entry %d: close zlib stream: %w", i, err)
		}
		if uint64(len(raw)) != size {
			return nil, fmt.Errorf("entry %d: size mismatch header=%d decoded=%d", i, size, len(raw))
		}

		consumed := len(payload[offset:]) - sub.Len()
		offset += consumed

		entries = append(entries, PackEntry{
			Type: objType,
			Size: size,
			Data: raw,
		})
	}

	if offset != len(payload) {
		return nil, fmt.Errorf("pack has trailing undecoded bytes: %d", len(payload)-offset)
	}

	return &PackFile{
		Header:   *header,
		Entries:  entries,
		Checksum: Hash(hex.EncodeToString(trailer)),
	}, nil
}

// ReadPackFromReader reads a complete pack stream from r and delegates to
// ReadPack for decode and verification.
func ReadPackFromReader(r io.Reader) (*PackFile, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("read pack stream: %w", err)
	}
	return ReadPack(data)
}

func decodePackEntryHeaderStrict(data []byte) (PackObjectType, uint64, int, error) {
	if len(data) == 0 {
		return 0, 0, 0, fmt.Errorf("entry header truncated")
	}

	b := data[0]
	objType := PackObjectType((b >> 4) & 0x7)
	size := uint64(b & 0x0f)
	shift := uint(4)
	consumed := 1

	for b&0x80 != 0 {
		if consumed >= len(data) {
			return 0, 0, 0, fmt.Errorf("entry header truncated")
		}
		b = data[consumed]
		size |= uint64(b&0x7f) << shift
		shift += 7
		consumed++
	}

	return objType, size, consumed, nil
}
