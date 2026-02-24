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
	Type         PackObjectType
	OriginalType PackObjectType
	Size         uint64
	Data         []byte
	Offset       uint64
	BaseDistance uint64 // populated for OFS_DELTA entries
	BaseRef      Hash   // populated for REF_DELTA entries
}

// PackFile is the decoded content of a full pack stream.
type PackFile struct {
	Header        PackHeader
	Entries       []PackEntry
	Checksum      Hash
	EntityTrailer *PackEntityTrailer
}

// ReadPack parses a full pack file byte slice, verifies trailer checksum, and
// returns decoded entries.
func ReadPack(data []byte) (*PackFile, error) {
	if len(data) < packHeaderSize+sha256.Size {
		return nil, fmt.Errorf("pack too short: %d", len(data))
	}

	header, err := UnmarshalPackHeader(data[:packHeaderSize])
	if err != nil {
		return nil, err
	}

	offset := packHeaderSize
	entries := make([]PackEntry, 0, header.NumObjects)
	for i := uint32(0); i < header.NumObjects; i++ {
		entryOffset := offset
		objType, size, n, err := decodePackEntryHeaderStrict(data[offset:])
		if err != nil {
			return nil, fmt.Errorf("entry %d: %w", i, err)
		}
		offset += n
		if offset >= len(data) {
			return nil, fmt.Errorf("entry %d: missing compressed payload", i)
		}
		baseDistance := uint64(0)
		baseRef := Hash("")
		if objType == PackOfsDelta {
			dist, dn, err := decodeOfsDeltaDistance(data[offset:])
			if err != nil {
				return nil, fmt.Errorf("entry %d: decode ofs-delta distance: %w", i, err)
			}
			baseDistance = dist
			offset += dn
		}
		if objType == PackRefDelta {
			if offset+32 > len(data) {
				return nil, fmt.Errorf("entry %d: truncated ref-delta base hash", i)
			}
			baseRef = Hash(hex.EncodeToString(data[offset : offset+32]))
			offset += 32
		}

		sub := bytes.NewReader(data[offset:])
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

		consumed := len(data[offset:]) - sub.Len()
		offset += consumed

		entries = append(entries, PackEntry{
			Type:         objType,
			OriginalType: objType,
			Size:         size,
			Data:         raw,
			Offset:       uint64(entryOffset),
			BaseDistance: baseDistance,
			BaseRef:      baseRef,
		})
	}

	if offset+sha256.Size > len(data) {
		return nil, fmt.Errorf("missing pack trailer checksum")
	}

	checksumRaw := data[offset : offset+sha256.Size]
	sum := sha256.Sum256(data[:offset])
	if !bytes.Equal(sum[:], checksumRaw) {
		return nil, fmt.Errorf("pack checksum mismatch")
	}

	remaining := data[offset+sha256.Size:]
	var entityTrailer *PackEntityTrailer
	if len(remaining) > 0 {
		if !bytes.Equal(remaining[:min(4, len(remaining))], packEntityTrailerMagic[:min(4, len(remaining))]) {
			return nil, fmt.Errorf("pack has trailing undecoded bytes: %d", len(remaining))
		}
		entityTrailer, err = ReadPackEntityTrailer(remaining)
		if err != nil {
			return nil, fmt.Errorf("read entity trailer: %w", err)
		}
	}

	return &PackFile{
		Header:        *header,
		Entries:       entries,
		Checksum:      Hash(hex.EncodeToString(checksumRaw)),
		EntityTrailer: entityTrailer,
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

// ReadPackResolved parses and resolves delta entries to their materialized
// object contents.
func ReadPackResolved(data []byte) (*PackFile, error) {
	pf, err := ReadPack(data)
	if err != nil {
		return nil, err
	}
	resolved, err := ResolvePackEntries(pf.Entries)
	if err != nil {
		return nil, err
	}
	pf.Entries = resolved
	return pf, nil
}

// ResolvePackEntries resolves OFS_DELTA and REF_DELTA entries to full object
// contents. Returned entries preserve OriginalType while Type is set to the
// resolved concrete object type.
func ResolvePackEntries(entries []PackEntry) ([]PackEntry, error) {
	resolved := make([]PackEntry, len(entries))
	done := make([]bool, len(entries))
	remaining := len(entries)

	byOffset := make(map[uint64]int, len(entries))
	for i, entry := range entries {
		byOffset[entry.Offset] = i
	}
	byHash := make(map[Hash]int, len(entries))

	for remaining > 0 {
		progress := false
		for i, entry := range entries {
			if done[i] {
				continue
			}

			switch entry.Type {
			case PackCommit, PackTree, PackBlob, PackTag:
				resolved[i] = entry
				if resolved[i].OriginalType == 0 {
					resolved[i].OriginalType = entry.Type
				}
				done[i] = true
				remaining--
				progress = true
				if h, ok := packEntryResolvedHash(resolved[i]); ok {
					byHash[h] = i
				}
			case PackOfsDelta:
				if entry.BaseDistance == 0 || entry.BaseDistance > entry.Offset {
					return nil, fmt.Errorf("entry %d: invalid ofs-delta base distance %d", i, entry.BaseDistance)
				}
				baseOffset := entry.Offset - entry.BaseDistance
				baseIndex, ok := byOffset[baseOffset]
				if !ok || !done[baseIndex] {
					continue
				}
				out, err := applyDelta(resolved[baseIndex].Data, entry.Data)
				if err != nil {
					return nil, fmt.Errorf("entry %d: apply ofs-delta: %w", i, err)
				}
				r := entry
				r.Type = resolved[baseIndex].Type
				r.Data = out
				resolved[i] = r
				done[i] = true
				remaining--
				progress = true
				if h, ok := packEntryResolvedHash(r); ok {
					byHash[h] = i
				}
			case PackRefDelta:
				baseIndex, ok := byHash[entry.BaseRef]
				if !ok {
					continue
				}
				out, err := applyDelta(resolved[baseIndex].Data, entry.Data)
				if err != nil {
					return nil, fmt.Errorf("entry %d: apply ref-delta: %w", i, err)
				}
				r := entry
				r.Type = resolved[baseIndex].Type
				r.Data = out
				resolved[i] = r
				done[i] = true
				remaining--
				progress = true
				if h, ok := packEntryResolvedHash(r); ok {
					byHash[h] = i
				}
			default:
				return nil, fmt.Errorf("entry %d: unsupported type %d", i, entry.Type)
			}
		}

		if !progress {
			return nil, fmt.Errorf("unable to resolve remaining delta entries")
		}
	}

	return resolved, nil
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

func packEntryResolvedHash(entry PackEntry) (Hash, bool) {
	objType, ok := packObjectTypeToObjectType(entry.Type)
	if !ok {
		return "", false
	}
	return HashObject(objType, entry.Data), true
}

func packObjectTypeToObjectType(t PackObjectType) (ObjectType, bool) {
	switch t {
	case PackCommit:
		return TypeCommit, true
	case PackTree:
		return TypeTree, true
	case PackBlob:
		return TypeBlob, true
	case PackTag:
		return TypeTag, true
	default:
		return "", false
	}
}
