package remote

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"runtime"
	"sync"

	"github.com/odvcencio/graft/pkg/object"
)

// objectTypeToPackType maps Graft object types to pack object types.
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

// packTypeToObjectType maps pack types back to Graft object types.
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
	prepared, err := preparePackTransportEntries(records)
	if err != nil {
		return err
	}

	pw, err := object.NewPackWriter(w, uint32(len(records)))
	if err != nil {
		return fmt.Errorf("create pack writer: %w", err)
	}

	entityEntries := make([]object.PackEntityTrailerEntry, 0, len(prepared))
	for _, entry := range prepared {
		if entry.entityTrailer != nil {
			entityEntries = append(entityEntries, *entry.entityTrailer)
		}
		if err := pw.WriteCompressedEntry(entry.packType, entry.rawSize, entry.compressed); err != nil {
			return fmt.Errorf("write pack entry for %s: %w", entry.hash, err)
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

type preparedPackTransportEntry struct {
	hash          object.Hash
	packType      object.PackObjectType
	rawSize       uint64
	compressed    []byte
	entityTrailer *object.PackEntityTrailerEntry
}

func preparePackTransportEntries(records []ObjectRecord) ([]preparedPackTransportEntry, error) {
	if len(records) == 0 {
		return nil, nil
	}

	prepared := make([]preparedPackTransportEntry, len(records))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	jobs := make(chan int)
	var workers sync.WaitGroup
	var setErr sync.Once
	var firstErr error

	setFirstErr := func(err error) {
		setErr.Do(func() {
			firstErr = err
			cancel()
		})
	}

	for worker := 0; worker < packTransportWorkerCount(len(records)); worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case idx, ok := <-jobs:
					if !ok {
						return
					}
					entry, err := preparePackTransportEntry(records[idx])
					if err != nil {
						setFirstErr(fmt.Errorf("prepare pack object %d: %w", idx, err))
						return
					}
					prepared[idx] = entry
				}
			}
		}()
	}

enqueueLoop:
	for idx := range records {
		select {
		case <-ctx.Done():
			break enqueueLoop
		case jobs <- idx:
		}
	}
	close(jobs)
	workers.Wait()

	if firstErr != nil {
		return nil, firstErr
	}
	return prepared, nil
}

func preparePackTransportEntry(rec ObjectRecord) (preparedPackTransportEntry, error) {
	packType, ok := objectTypeToPackType(rec.Type)
	if !ok {
		return preparedPackTransportEntry{}, fmt.Errorf("unsupported object type %q", rec.Type)
	}

	compressed, err := object.CompressPackPayload(rec.Data)
	if err != nil {
		return preparedPackTransportEntry{}, fmt.Errorf("compress pack entry: %w", err)
	}

	hash := rec.Hash
	if hash == "" {
		hash = object.HashObject(rec.Type, rec.Data)
	}

	entry := preparedPackTransportEntry{
		hash:       hash,
		packType:   packType,
		rawSize:    uint64(len(rec.Data)),
		compressed: compressed,
	}
	if rec.Type == object.TypeEntity || rec.Type == object.TypeEntityList {
		entityEntry := object.PackEntityTrailerEntry{
			ObjectHash: hash,
			StableID:   "type:" + string(rec.Type),
		}
		entry.entityTrailer = &entityEntry
	}
	return entry, nil
}

func packTransportWorkerCount(total int) int {
	if total <= 1 {
		return total
	}
	workers := runtime.GOMAXPROCS(0)
	if workers < 1 {
		workers = 1
	}
	if workers > total {
		return total
	}
	return workers
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

		// Check for entity type overrides. The trailer records the correct
		// hash (computed with the real entity type by the sender), but
		// packTypeToObjectType maps all entity types to TypeBlob. We must
		// probe candidate entity types to find a matching override.
		if _, ok := typeOverrides[hash]; !ok && len(typeOverrides) > 0 {
			for _, candidateType := range []object.ObjectType{object.TypeEntity, object.TypeEntityList} {
				candidateHash := object.HashObject(candidateType, entry.Data)
				if override, ok := typeOverrides[candidateHash]; ok {
					objType = override
					hash = candidateHash
					break
				}
			}
		} else if override, ok := typeOverrides[hash]; ok {
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
