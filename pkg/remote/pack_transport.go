package remote

import (
	"bytes"
	"fmt"
	"io"

	"github.com/odvcencio/got/pkg/object"
)

// objectTypeToPackType maps Got object types to pack object types.
func objectTypeToPackType(t object.ObjectType) (object.PackObjectType, bool) {
	switch t {
	case object.TypeCommit:
		return object.PackCommit, true
	case object.TypeTree:
		return object.PackTree, true
	case object.TypeBlob, object.TypeEntity, object.TypeEntityList:
		return object.PackBlob, true
	case object.TypeTag:
		return object.PackTag, true
	default:
		return 0, false
	}
}

// packTypeToObjectType maps pack types back to Got object types.
func packTypeToObjectType(t object.PackObjectType) (object.ObjectType, bool) {
	switch t {
	case object.PackCommit:
		return object.TypeCommit, true
	case object.PackTree:
		return object.TypeTree, true
	case object.PackBlob:
		return object.TypeBlob, true
	case object.PackTag:
		return object.TypeTag, true
	default:
		return "", false
	}
}

// EncodePackTransport encodes ObjectRecords into a pack stream.
func EncodePackTransport(w io.Writer, records []ObjectRecord) error {
	pw, err := object.NewPackWriter(w, uint32(len(records)))
	if err != nil {
		return fmt.Errorf("create pack writer: %w", err)
	}

	var entityEntries []object.PackEntityTrailerEntry

	for _, rec := range records {
		packType, ok := objectTypeToPackType(rec.Type)
		if !ok {
			return fmt.Errorf("unsupported object type %q", rec.Type)
		}

		if rec.Type == object.TypeEntity || rec.Type == object.TypeEntityList {
			entityEntries = append(entityEntries, object.PackEntityTrailerEntry{
				ObjectHash: rec.Hash,
				StableID:   "type:" + string(rec.Type),
			})
		}

		if err := pw.WriteEntry(packType, rec.Data); err != nil {
			return fmt.Errorf("write pack entry for %s: %w", rec.Hash, err)
		}
	}

	if len(entityEntries) > 0 {
		if _, err := pw.FinishWithEntityTrailer(entityEntries); err != nil {
			return fmt.Errorf("finish pack with entity trailer: %w", err)
		}
	} else {
		if _, err := pw.Finish(); err != nil {
			return fmt.Errorf("finish pack: %w", err)
		}
	}

	return nil
}

// DecodePackTransport decodes a pack stream into ObjectRecords.
func DecodePackTransport(data []byte) ([]ObjectRecord, error) {
	if len(data) == 0 {
		return nil, nil
	}

	pf, err := object.ReadPack(data)
	if err != nil {
		return nil, fmt.Errorf("read pack: %w", err)
	}

	resolved, err := object.ResolvePackEntries(pf.Entries)
	if err != nil {
		return nil, fmt.Errorf("resolve deltas: %w", err)
	}

	// Build entity type overrides from trailer.
	typeOverrides := map[object.Hash]object.ObjectType{}
	if pf.EntityTrailer != nil {
		for _, entry := range pf.EntityTrailer.Entries {
			if len(entry.StableID) > 5 && entry.StableID[:5] == "type:" {
				typeOverrides[entry.ObjectHash] = object.ObjectType(entry.StableID[5:])
			}
		}
	}

	records := make([]ObjectRecord, 0, len(resolved))
	for _, entry := range resolved {
		objType, ok := packTypeToObjectType(entry.Type)
		if !ok {
			return nil, fmt.Errorf("unsupported pack type %d", entry.Type)
		}

		hash := object.HashObject(objType, entry.Data)

		// Check for entity type overrides.
		if override, ok := typeOverrides[hash]; ok {
			objType = override
			hash = object.HashObject(objType, entry.Data)
		}

		records = append(records, ObjectRecord{
			Hash: hash,
			Type: objType,
			Data: entry.Data,
		})
	}

	return records, nil
}

// EncodePackTransportToBytes is a convenience wrapper.
func EncodePackTransportToBytes(records []ObjectRecord) ([]byte, error) {
	var buf bytes.Buffer
	if err := EncodePackTransport(&buf, records); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
