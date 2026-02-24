package object

import (
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// GCSummary reports the outcome of Store.GC.
type GCSummary struct {
	PackedObjects int
	PackFile      string
	IndexFile     string
}

// VerifySummary reports the outcome of Store.Verify.
type VerifySummary struct {
	LooseObjects int
	PackFiles    int
	PackObjects  int
}

// GC packs loose objects that are not already indexed by an existing pack idx.
// It is non-destructive: loose objects remain on disk.
func (s *Store) GC() (*GCSummary, error) {
	looseHashes, err := s.listLooseObjectHashes()
	if err != nil {
		return nil, err
	}

	packed, err := s.packedHashSet()
	if err != nil {
		return nil, err
	}

	toPack := make([]Hash, 0, len(looseHashes))
	for _, h := range looseHashes {
		if _, ok := packed[h]; ok {
			continue
		}
		toPack = append(toPack, h)
	}
	if len(toPack) == 0 {
		return &GCSummary{}, nil
	}
	if len(toPack) > int(^uint32(0)) {
		return nil, fmt.Errorf("gc: too many objects to pack: %d", len(toPack))
	}

	packDir := filepath.Join(s.root, "objects", "pack")
	if err := os.MkdirAll(packDir, 0o755); err != nil {
		return nil, fmt.Errorf("gc: mkdir pack dir: %w", err)
	}

	packTmp, err := os.CreateTemp(packDir, ".tmp-pack-*.pack")
	if err != nil {
		return nil, fmt.Errorf("gc: create pack temp file: %w", err)
	}
	packTmpPath := packTmp.Name()
	packTmpRemoved := false
	defer func() {
		if !packTmpRemoved {
			_ = os.Remove(packTmpPath)
		}
	}()

	pw, err := NewPackWriter(packTmp, uint32(len(toPack)))
	if err != nil {
		_ = packTmp.Close()
		return nil, fmt.Errorf("gc: create pack writer: %w", err)
	}

	indexEntries := make([]PackIndexEntry, 0, len(toPack))
	for _, h := range toPack {
		objType, content, err := s.readLoose(h)
		if err != nil {
			_ = packTmp.Close()
			return nil, fmt.Errorf("gc: read loose object %s: %w", h, err)
		}
		offset := pw.CurrentOffset()
		envelope := makeObjectEnvelope(objType, content)
		if err := pw.WriteEntry(PackBlob, envelope); err != nil {
			_ = packTmp.Close()
			return nil, fmt.Errorf("gc: write pack entry %s: %w", h, err)
		}
		indexEntries = append(indexEntries, PackIndexEntry{
			Hash:   h,
			Offset: offset,
		})
	}

	packChecksum, err := pw.Finish()
	if err != nil {
		_ = packTmp.Close()
		return nil, fmt.Errorf("gc: finalize pack: %w", err)
	}
	if err := packTmp.Close(); err != nil {
		return nil, fmt.Errorf("gc: close pack temp file: %w", err)
	}

	packBase := "pack-" + string(packChecksum)
	packPath := filepath.Join(packDir, packBase+".pack")
	idxPath := filepath.Join(packDir, packBase+".idx")
	if err := os.Rename(packTmpPath, packPath); err != nil {
		return nil, fmt.Errorf("gc: rename pack file: %w", err)
	}
	packTmpRemoved = true

	idxTmp, err := os.CreateTemp(packDir, ".tmp-pack-*.idx")
	if err != nil {
		_ = os.Remove(packPath)
		return nil, fmt.Errorf("gc: create index temp file: %w", err)
	}
	idxTmpPath := idxTmp.Name()
	idxTmpRemoved := false
	defer func() {
		if !idxTmpRemoved {
			_ = os.Remove(idxTmpPath)
		}
	}()

	if _, err := WritePackIndex(idxTmp, indexEntries, packChecksum); err != nil {
		_ = idxTmp.Close()
		_ = os.Remove(packPath)
		return nil, fmt.Errorf("gc: write pack index: %w", err)
	}
	if err := idxTmp.Close(); err != nil {
		_ = os.Remove(packPath)
		return nil, fmt.Errorf("gc: close index temp file: %w", err)
	}
	if err := os.Rename(idxTmpPath, idxPath); err != nil {
		_ = os.Remove(packPath)
		return nil, fmt.Errorf("gc: rename index file: %w", err)
	}
	idxTmpRemoved = true

	return &GCSummary{
		PackedObjects: len(toPack),
		PackFile:      filepath.Base(packPath),
		IndexFile:     filepath.Base(idxPath),
	}, nil
}

// Verify checks object integrity across loose objects and pack/index entries.
func (s *Store) Verify() (*VerifySummary, error) {
	report := &VerifySummary{}

	looseHashes, err := s.listLooseObjectHashes()
	if err != nil {
		return nil, err
	}
	for _, h := range looseHashes {
		objType, content, err := s.readLoose(h)
		if err != nil {
			return nil, fmt.Errorf("verify loose %s: %w", h, err)
		}
		if actual := HashObject(objType, content); actual != h {
			return nil, fmt.Errorf("verify loose %s: hash mismatch (computed %s)", h, actual)
		}
		report.LooseObjects++
	}

	idxPaths, err := s.listPackIndexPaths()
	if err != nil {
		return nil, err
	}
	for _, idxPath := range idxPaths {
		idxData, err := os.ReadFile(idxPath)
		if err != nil {
			return nil, fmt.Errorf("verify pack index %s: %w", filepath.Base(idxPath), err)
		}
		idx, err := ReadPackIndex(idxData)
		if err != nil {
			return nil, fmt.Errorf("verify pack index %s: %w", filepath.Base(idxPath), err)
		}

		packPath := packPathForIndex(idxPath)
		packData, err := os.ReadFile(packPath)
		if err != nil {
			return nil, fmt.Errorf("verify pack %s: %w", filepath.Base(packPath), err)
		}
		pf, err := ReadPackResolved(packData)
		if err != nil {
			return nil, fmt.Errorf("verify pack %s: %w", filepath.Base(packPath), err)
		}
		if pf.Checksum != idx.PackChecksum {
			return nil, fmt.Errorf(
				"verify pack %s: checksum mismatch between idx (%s) and pack (%s)",
				filepath.Base(packPath),
				idx.PackChecksum,
				pf.Checksum,
			)
		}

		offsets := make(map[uint64]PackEntry, len(pf.Entries))
		for _, entry := range pf.Entries {
			if _, exists := offsets[entry.Offset]; exists {
				return nil, fmt.Errorf("verify pack %s: duplicate offset %d", filepath.Base(packPath), entry.Offset)
			}
			offsets[entry.Offset] = entry
		}

		entries := idx.Entries()
		if len(entries) != len(offsets) {
			return nil, fmt.Errorf(
				"verify pack %s: idx entry count %d does not match pack entry count %d",
				filepath.Base(packPath),
				len(entries),
				len(offsets),
			)
		}
		for _, indexEntry := range entries {
			packEntry, ok := offsets[indexEntry.Offset]
			if !ok {
				return nil, fmt.Errorf(
					"verify pack %s: missing pack entry for hash %s at offset %d",
					filepath.Base(packPath),
					indexEntry.Hash,
					indexEntry.Offset,
				)
			}
			if _, _, err := decodeIndexedPackEntry(indexEntry.Hash, packEntry); err != nil {
				return nil, fmt.Errorf("verify pack %s hash %s: %w", filepath.Base(packPath), indexEntry.Hash, err)
			}
			report.PackObjects++
		}
		report.PackFiles++
	}

	return report, nil
}

func (s *Store) readFromPacks(h Hash) (ObjectType, []byte, error) {
	idxPaths, err := s.listPackIndexPaths()
	if err != nil {
		return "", nil, err
	}
	for _, idxPath := range idxPaths {
		idxData, err := os.ReadFile(idxPath)
		if err != nil {
			return "", nil, fmt.Errorf("object read %s: read pack index %s: %w", h, filepath.Base(idxPath), err)
		}
		idx, err := ReadPackIndex(idxData)
		if err != nil {
			return "", nil, fmt.Errorf("object read %s: parse pack index %s: %w", h, filepath.Base(idxPath), err)
		}
		indexEntry, ok := idx.Find(h)
		if !ok {
			continue
		}

		packPath := packPathForIndex(idxPath)
		packData, err := os.ReadFile(packPath)
		if err != nil {
			return "", nil, fmt.Errorf("object read %s: read pack %s: %w", h, filepath.Base(packPath), err)
		}

		pf, err := ReadPackResolved(packData)
		if err != nil {
			return "", nil, fmt.Errorf("object read %s: parse pack %s: %w", h, filepath.Base(packPath), err)
		}
		if pf.Checksum != idx.PackChecksum {
			return "", nil, fmt.Errorf(
				"object read %s: checksum mismatch between idx %s and pack %s",
				h,
				filepath.Base(idxPath),
				filepath.Base(packPath),
			)
		}

		packEntry, ok := findPackEntryByOffset(pf.Entries, indexEntry.Offset)
		if !ok {
			return "", nil, fmt.Errorf(
				"object read %s: pack %s missing entry at offset %d",
				h,
				filepath.Base(packPath),
				indexEntry.Offset,
			)
		}
		return decodeIndexedPackEntry(h, packEntry)
	}

	return "", nil, fmt.Errorf("object read %s: %w", h, os.ErrNotExist)
}

func decodeIndexedPackEntry(expected Hash, entry PackEntry) (ObjectType, []byte, error) {
	var envelopeHashMismatchErr error
	if envelopeType, envelopeData, err := parseObjectEnvelope(entry.Data, expected); err == nil {
		if computed := HashObject(envelopeType, envelopeData); computed == expected {
			return envelopeType, envelopeData, nil
		}
		envelopeHashMismatchErr = fmt.Errorf(
			"envelope hash mismatch: expected %s, computed %s",
			expected,
			HashObject(envelopeType, envelopeData),
		)
	}

	objType, ok := packObjectTypeToObjectType(entry.Type)
	if !ok {
		if envelopeHashMismatchErr != nil {
			return "", nil, envelopeHashMismatchErr
		}
		return "", nil, fmt.Errorf("unsupported packed object type %d", entry.Type)
	}
	computed := HashObject(objType, entry.Data)
	if computed != expected {
		return "", nil, fmt.Errorf(
			"packed object hash mismatch: expected %s, computed %s",
			expected,
			computed,
		)
	}
	return objType, entry.Data, nil
}

func findPackEntryByOffset(entries []PackEntry, offset uint64) (PackEntry, bool) {
	for _, entry := range entries {
		if entry.Offset == offset {
			return entry, true
		}
	}
	return PackEntry{}, false
}

func (s *Store) packedHashSet() (map[Hash]struct{}, error) {
	idxPaths, err := s.listPackIndexPaths()
	if err != nil {
		return nil, err
	}

	out := make(map[Hash]struct{})
	for _, idxPath := range idxPaths {
		packPath := packPathForIndex(idxPath)
		if _, err := os.Stat(packPath); err != nil {
			return nil, fmt.Errorf("read pack for index %s: %w", filepath.Base(idxPath), err)
		}

		idxData, err := os.ReadFile(idxPath)
		if err != nil {
			return nil, fmt.Errorf("read pack index %s: %w", filepath.Base(idxPath), err)
		}
		idx, err := ReadPackIndex(idxData)
		if err != nil {
			return nil, fmt.Errorf("parse pack index %s: %w", filepath.Base(idxPath), err)
		}
		for _, entry := range idx.Entries() {
			out[entry.Hash] = struct{}{}
		}
	}
	return out, nil
}

func (s *Store) listPackIndexPaths() ([]string, error) {
	packDir := filepath.Join(s.root, "objects", "pack")
	entries, err := os.ReadDir(packDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read pack dir: %w", err)
	}

	idxPaths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !strings.HasSuffix(entry.Name(), ".idx") {
			continue
		}
		idxPaths = append(idxPaths, filepath.Join(packDir, entry.Name()))
	}
	sort.Strings(idxPaths)
	return idxPaths, nil
}

func (s *Store) listLooseObjectHashes() ([]Hash, error) {
	objectsDir := filepath.Join(s.root, "objects")
	fanoutDirs, err := os.ReadDir(objectsDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read objects dir: %w", err)
	}

	hashes := make([]Hash, 0)
	for _, fanoutDir := range fanoutDirs {
		if !fanoutDir.IsDir() {
			continue
		}
		prefix := fanoutDir.Name()
		if prefix == "pack" || !isHexHashComponent(prefix, 2) {
			continue
		}

		objectDir := filepath.Join(objectsDir, prefix)
		objectEntries, err := os.ReadDir(objectDir)
		if err != nil {
			return nil, fmt.Errorf("read objects fanout %s: %w", prefix, err)
		}
		for _, objectEntry := range objectEntries {
			if objectEntry.IsDir() {
				continue
			}
			suffix := objectEntry.Name()
			if !isHexHashComponent(suffix, 62) {
				continue
			}
			hashes = append(hashes, Hash(prefix+suffix))
		}
	}

	sort.Slice(hashes, func(i, j int) bool {
		return hashes[i] < hashes[j]
	})
	return hashes, nil
}

func isHexHashComponent(s string, expectedLen int) bool {
	if len(s) != expectedLen {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

func packPathForIndex(idxPath string) string {
	return strings.TrimSuffix(idxPath, ".idx") + ".pack"
}

func makeObjectEnvelope(objType ObjectType, data []byte) []byte {
	header := fmt.Sprintf("%s %d\x00", objType, len(data))
	out := make([]byte, 0, len(header)+len(data))
	out = append(out, header...)
	out = append(out, data...)
	return out
}
