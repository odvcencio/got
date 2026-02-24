package object

import (
	"encoding/binary"
	"fmt"
)

const (
	packHeaderSize       = 12
	supportedPackVersion = 2
)

var packMagic = [4]byte{'P', 'A', 'C', 'K'}

// PackObjectType is the Git pack object type encoding used in object entry
// headers. Values match the canonical Git wire/storage format.
type PackObjectType uint8

const (
	PackCommit   PackObjectType = 1
	PackTree     PackObjectType = 2
	PackBlob     PackObjectType = 3
	PackTag      PackObjectType = 4
	PackOfsDelta PackObjectType = 6
	PackRefDelta PackObjectType = 7
)

// PackHeader is the fixed-size Git pack header.
//
// Bytes:
//   - 0..3:  "PACK"
//   - 4..7:  version (big-endian)
//   - 8..11: number of objects (big-endian)
type PackHeader struct {
	Version    uint32
	NumObjects uint32
}

// Marshal serializes the header to the canonical 12-byte pack header.
func (h PackHeader) Marshal() []byte {
	buf := make([]byte, packHeaderSize)
	copy(buf[:4], packMagic[:])
	binary.BigEndian.PutUint32(buf[4:8], h.Version)
	binary.BigEndian.PutUint32(buf[8:12], h.NumObjects)
	return buf
}

// UnmarshalPackHeader parses a canonical Git pack header.
func UnmarshalPackHeader(data []byte) (*PackHeader, error) {
	if len(data) < packHeaderSize {
		return nil, fmt.Errorf("pack header too short: got %d bytes", len(data))
	}
	if string(data[:4]) != string(packMagic[:]) {
		return nil, fmt.Errorf("invalid pack magic %q", data[:4])
	}

	version := binary.BigEndian.Uint32(data[4:8])
	if version != supportedPackVersion {
		return nil, fmt.Errorf("unsupported pack version %d", version)
	}

	return &PackHeader{
		Version:    version,
		NumObjects: binary.BigEndian.Uint32(data[8:12]),
	}, nil
}

// encodePackEntryHeader encodes the variable-length object entry header used in
// Git pack files.
func encodePackEntryHeader(objType PackObjectType, size uint64) []byte {
	b := byte((objType & 0x7) << 4)
	b |= byte(size & 0x0f)
	size >>= 4

	out := make([]byte, 0, 10)
	if size > 0 {
		b |= 0x80
	}
	out = append(out, b)

	for size > 0 {
		next := byte(size & 0x7f)
		size >>= 7
		if size > 0 {
			next |= 0x80
		}
		out = append(out, next)
	}

	return out
}

// decodePackEntryHeader decodes an object entry header, returning object type,
// uncompressed object size, and bytes consumed. The caller must ensure input is
// a complete header.
func decodePackEntryHeader(data []byte) (PackObjectType, uint64, int) {
	if len(data) == 0 {
		return 0, 0, 0
	}

	b := data[0]
	objType := PackObjectType((b >> 4) & 0x7)
	size := uint64(b & 0x0f)
	shift := uint(4)
	consumed := 1

	for b&0x80 != 0 {
		if consumed >= len(data) {
			return objType, size, consumed
		}
		b = data[consumed]
		size |= uint64(b&0x7f) << shift
		shift += 7
		consumed++
	}

	return objType, size, consumed
}
