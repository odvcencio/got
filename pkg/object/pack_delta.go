package object

import (
	"bytes"
	"fmt"
	"io"
)

func encodeDeltaVarint(v uint64) []byte {
	if v == 0 {
		return []byte{0}
	}
	out := make([]byte, 0, 10)
	for v > 0 {
		b := byte(v & 0x7f)
		v >>= 7
		if v > 0 {
			b |= 0x80
		}
		out = append(out, b)
	}
	return out
}

func decodeDeltaVarint(r io.ByteReader) (uint64, error) {
	var (
		value uint64
		shift uint
	)
	for {
		b, err := r.ReadByte()
		if err != nil {
			return 0, err
		}
		value |= uint64(b&0x7f) << shift
		if b&0x80 == 0 {
			return value, nil
		}
		shift += 7
		if shift > 63 {
			return 0, fmt.Errorf("delta varint too large")
		}
	}
}

// encodeOfsDeltaDistance encodes a backward distance for OFS_DELTA entries.
func encodeOfsDeltaDistance(distance uint64) []byte {
	if distance == 0 {
		return []byte{0}
	}
	b := []byte{byte(distance & 0x7f)}
	for distance >>= 7; distance > 0; distance >>= 7 {
		distance--
		b = append([]byte{byte((distance & 0x7f) | 0x80)}, b...)
	}
	return b
}

func decodeOfsDeltaDistance(data []byte) (uint64, int, error) {
	if len(data) == 0 {
		return 0, 0, fmt.Errorf("ofs-delta distance truncated")
	}
	i := 0
	c := data[i]
	i++
	offset := uint64(c & 0x7f)
	for c&0x80 != 0 {
		if i >= len(data) {
			return 0, 0, fmt.Errorf("ofs-delta distance truncated")
		}
		c = data[i]
		i++
		offset = ((offset + 1) << 7) | uint64(c&0x7f)
	}
	return offset, i, nil
}

// buildInsertOnlyDelta returns a valid Git delta stream by encoding the target
// object as literal insert chunks. This is intentionally simple and correct; it
// trades compression ratio for deterministic behavior.
func buildInsertOnlyDelta(base, target []byte) []byte {
	var out bytes.Buffer
	out.Write(encodeDeltaVarint(uint64(len(base))))
	out.Write(encodeDeltaVarint(uint64(len(target))))

	for pos := 0; pos < len(target); {
		chunk := len(target) - pos
		if chunk > 127 {
			chunk = 127
		}
		out.WriteByte(byte(chunk))
		out.Write(target[pos : pos+chunk])
		pos += chunk
	}
	return out.Bytes()
}

// applyDelta applies Git delta instructions to base and returns the result.
func applyDelta(base, delta []byte) ([]byte, error) {
	dr := bytes.NewReader(delta)

	baseSize, err := decodeDeltaVarint(dr)
	if err != nil {
		return nil, fmt.Errorf("read base size: %w", err)
	}
	if int(baseSize) != len(base) {
		return nil, fmt.Errorf("delta base size mismatch: got %d want %d", baseSize, len(base))
	}
	resultSize, err := decodeDeltaVarint(dr)
	if err != nil {
		return nil, fmt.Errorf("read result size: %w", err)
	}

	out := make([]byte, 0, resultSize)
	for dr.Len() > 0 {
		cmd, err := dr.ReadByte()
		if err != nil {
			return nil, err
		}
		if cmd&0x80 != 0 {
			var (
				offset int64
				size   int64
			)
			if cmd&0x01 != 0 {
				b, err := readDeltaCopyArgByte(dr, "offset byte 0")
				if err != nil {
					return nil, err
				}
				offset |= int64(b)
			}
			if cmd&0x02 != 0 {
				b, err := readDeltaCopyArgByte(dr, "offset byte 1")
				if err != nil {
					return nil, err
				}
				offset |= int64(b) << 8
			}
			if cmd&0x04 != 0 {
				b, err := readDeltaCopyArgByte(dr, "offset byte 2")
				if err != nil {
					return nil, err
				}
				offset |= int64(b) << 16
			}
			if cmd&0x08 != 0 {
				b, err := readDeltaCopyArgByte(dr, "offset byte 3")
				if err != nil {
					return nil, err
				}
				offset |= int64(b) << 24
			}
			if cmd&0x10 != 0 {
				b, err := readDeltaCopyArgByte(dr, "size byte 0")
				if err != nil {
					return nil, err
				}
				size |= int64(b)
			}
			if cmd&0x20 != 0 {
				b, err := readDeltaCopyArgByte(dr, "size byte 1")
				if err != nil {
					return nil, err
				}
				size |= int64(b) << 8
			}
			if cmd&0x40 != 0 {
				b, err := readDeltaCopyArgByte(dr, "size byte 2")
				if err != nil {
					return nil, err
				}
				size |= int64(b) << 16
			}
			if size == 0 {
				size = 0x10000
			}
			if offset < 0 || size < 0 || offset+size > int64(len(base)) {
				return nil, fmt.Errorf("delta copy out of bounds")
			}
			out = append(out, base[offset:offset+size]...)
			continue
		}

		if cmd == 0 {
			return nil, fmt.Errorf("invalid delta command: 0")
		}
		insert := make([]byte, int(cmd))
		if _, err := io.ReadFull(dr, insert); err != nil {
			return nil, fmt.Errorf("delta insert: %w", err)
		}
		out = append(out, insert...)
	}

	if uint64(len(out)) != resultSize {
		return nil, fmt.Errorf("delta result size mismatch: got %d expected %d", len(out), resultSize)
	}
	return out, nil
}

func readDeltaCopyArgByte(r io.ByteReader, field string) (byte, error) {
	b, err := r.ReadByte()
	if err != nil {
		return 0, fmt.Errorf("delta copy %s: %w", field, err)
	}
	return b, nil
}
