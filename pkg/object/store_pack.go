package object

import (
	"compress/zlib"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// GCSummary reports the outcome of Store.GC.
type GCSummary struct {
	PackedObjects int
	PrunedObjects int
	PackFile      string
	IndexFile     string
}

// VerifySummary reports the outcome of Store.Verify.
type VerifySummary struct {
	LooseObjects int
	PackFiles    int
	PackObjects  int
}

// GC packs all loose objects that are not already indexed by an existing pack
// idx. After a successful pack+index write, packed loose objects are removed.
func (s *Store) GC() (*GCSummary, error) {
	return s.gcWithReachableSet(nil)
}

// GCReachable packs loose objects reachable from roots that are not already
// indexed by an existing pack idx. After a successful pack+index write, packed
// loose objects are removed.
func (s *Store) GCReachable(roots []Hash) (*GCSummary, error) {
	reachable, err := s.ReachableSet(roots)
	if err != nil {
		return nil, err
	}
	return s.gcWithReachableSet(reachable)
}

func (s *Store) gcWithReachableSet(reachable map[Hash]struct{}) (*GCSummary, error) {
	if reachable != nil && len(reachable) == 0 {
		return &GCSummary{}, nil
	}

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
		if reachable != nil {
			if _, ok := reachable[h]; !ok {
				continue
			}
		}
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
		packType := objectTypeToPackType(objType)
		if err := pw.WriteEntry(packType, envelope); err != nil {
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

	// Invalidate the in-memory pack index cache so subsequent reads pick up
	// the newly written index.
	s.InvalidatePackIndexCache()

	pruned := 0
	for _, h := range toPack {
		if err := os.Remove(s.objectPath(h)); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("gc: remove loose object %s: %w", h, err)
		}
		pruned++
	}

	return &GCSummary{
		PackedObjects: len(toPack),
		PrunedObjects: pruned,
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

		seenIndexOffsets := make(map[uint64]struct{}, len(entries))
		indexHashes := make(map[Hash]struct{}, len(entries))
		for _, indexEntry := range entries {
			if _, exists := seenIndexOffsets[indexEntry.Offset]; exists {
				return nil, fmt.Errorf(
					"verify pack %s: duplicate idx offset %d",
					filepath.Base(packPath),
					indexEntry.Offset,
				)
			}
			seenIndexOffsets[indexEntry.Offset] = struct{}{}
			indexHashes[indexEntry.Hash] = struct{}{}

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
		if pf.EntityTrailer != nil {
			for _, trailerEntry := range pf.EntityTrailer.Entries {
				if _, ok := indexHashes[trailerEntry.ObjectHash]; !ok {
					return nil, fmt.Errorf(
						"verify pack %s: entity trailer references missing object hash %s",
						filepath.Base(packPath),
						trailerEntry.ObjectHash,
					)
				}
			}
		}
		report.PackFiles++
	}

	return report, nil
}

// cachedPackIndex returns the parsed PackIndex for the given idx file path,
// using an in-memory cache keyed by file path and validated by mod-time.
func (s *Store) cachedPackIndex(idxPath string) (*PackIndex, error) {
	info, err := os.Stat(idxPath)
	if err != nil {
		return nil, err
	}
	modNano := info.ModTime().UnixNano()

	s.packIdxMu.Lock()
	if s.packIdxCache != nil {
		if cached, ok := s.packIdxCache[idxPath]; ok && cached.modTime == modNano {
			s.packIdxMu.Unlock()
			return cached.idx, nil
		}
	}
	s.packIdxMu.Unlock()

	idxData, err := os.ReadFile(idxPath)
	if err != nil {
		return nil, err
	}
	idx, err := ReadPackIndex(idxData)
	if err != nil {
		return nil, err
	}

	s.packIdxMu.Lock()
	if s.packIdxCache == nil {
		s.packIdxCache = make(map[string]packIndexCacheEntry)
	}
	s.packIdxCache[idxPath] = packIndexCacheEntry{idx: idx, modTime: modNano}
	s.packIdxMu.Unlock()

	return idx, nil
}

// InvalidatePackIndexCache drops all cached pack indices, forcing a re-read on
// the next access. This is useful after GC or external pack modifications.
func (s *Store) InvalidatePackIndexCache() {
	s.packIdxMu.Lock()
	s.packIdxCache = nil
	s.packIdxMu.Unlock()
}

// readPackEntryAt reads and decompresses a single pack entry at the given byte
// offset from a pack file. It validates the pack header on first read but only
// decompresses the targeted entry, avoiding the cost of parsing every entry in
// the pack.
//
// The function uses an io.SectionReader over the file to avoid reading the
// entire pack tail into memory, which could be very large for big pack files.
func readPackEntryAt(packPath string, offset uint64) (PackEntry, error) {
	f, err := os.Open(packPath)
	if err != nil {
		return PackEntry{}, fmt.Errorf("open pack %s: %w", filepath.Base(packPath), err)
	}
	defer f.Close()

	// Validate pack header.
	headerBuf := make([]byte, packHeaderSize)
	if _, err := io.ReadFull(f, headerBuf); err != nil {
		return PackEntry{}, fmt.Errorf("read pack header %s: %w", filepath.Base(packPath), err)
	}
	if _, err := UnmarshalPackHeader(headerBuf); err != nil {
		return PackEntry{}, err
	}

	stat, err := f.Stat()
	if err != nil {
		return PackEntry{}, fmt.Errorf("stat pack %s: %w", filepath.Base(packPath), err)
	}
	// The entry data extends from offset to (at most) before the 32-byte
	// trailing checksum. Entity trailers may exist after the checksum, but
	// entries live before it.
	maxEntryEnd := stat.Size() - sha256.Size
	if int64(offset) >= maxEntryEnd {
		return PackEntry{}, fmt.Errorf("offset %d past pack data boundary in %s", offset, filepath.Base(packPath))
	}

	// Read a small buffer for the entry header (type byte + size varint +
	// possible delta base reference). The header is at most ~10 bytes for the
	// varint, plus up to 10 bytes for an ofs-delta distance, plus 32 bytes
	// for a ref-delta hash. 64 bytes is more than enough.
	const maxHeaderBuf = 64
	headerReadLen := maxEntryEnd - int64(offset)
	if headerReadLen > maxHeaderBuf {
		headerReadLen = maxHeaderBuf
	}
	entryHeaderBuf := make([]byte, headerReadLen)
	if _, err := f.ReadAt(entryHeaderBuf, int64(offset)); err != nil {
		return PackEntry{}, fmt.Errorf("read entry header at offset %d in %s: %w", offset, filepath.Base(packPath), err)
	}

	// Decode entry header.
	objType, size, headerLen, err := decodePackEntryHeaderStrict(entryHeaderBuf)
	if err != nil {
		return PackEntry{}, fmt.Errorf("decode entry header at offset %d: %w", offset, err)
	}
	pos := headerLen

	// Handle delta base references.
	baseDistance := uint64(0)
	baseRef := Hash("")
	if objType == PackOfsDelta {
		dist, dn, err := decodeOfsDeltaDistance(entryHeaderBuf[pos:])
		if err != nil {
			return PackEntry{}, fmt.Errorf("decode ofs-delta at offset %d: %w", offset, err)
		}
		baseDistance = dist
		pos += dn
	}
	if objType == PackRefDelta {
		if pos+32 > len(entryHeaderBuf) {
			return PackEntry{}, fmt.Errorf("truncated ref-delta base hash at offset %d", offset)
		}
		baseRef = Hash(hex.EncodeToString(entryHeaderBuf[pos : pos+32]))
		pos += 32
	}

	// Decompress the zlib payload using a section reader over the file,
	// avoiding reading the entire pack tail into memory.
	zlibStart := int64(offset) + int64(pos)
	zlibLen := maxEntryEnd - zlibStart
	sr := io.NewSectionReader(f, zlibStart, zlibLen)
	zr, err := zlib.NewReader(sr)
	if err != nil {
		return PackEntry{}, fmt.Errorf("zlib reader at offset %d: %w", offset, err)
	}
	lr := io.LimitReader(zr, int64(size)+1)
	raw, err := io.ReadAll(lr)
	if err != nil {
		_ = zr.Close()
		return PackEntry{}, fmt.Errorf("decompress entry at offset %d: %w", offset, err)
	}
	if uint64(len(raw)) > size {
		_ = zr.Close()
		return PackEntry{}, fmt.Errorf("decompressed size exceeds declared size at offset %d", offset)
	}
	if err := zr.Close(); err != nil {
		return PackEntry{}, fmt.Errorf("close zlib at offset %d: %w", offset, err)
	}
	if uint64(len(raw)) != size {
		return PackEntry{}, fmt.Errorf("size mismatch at offset %d: header=%d decoded=%d", offset, size, len(raw))
	}

	return PackEntry{
		Type:         objType,
		OriginalType: objType,
		Size:         size,
		Data:         raw,
		Offset:       offset,
		BaseDistance:  baseDistance,
		BaseRef:      baseRef,
	}, nil
}

const maxDeltaChainDepth = 50

// readResolvedPackEntryAt reads a single pack entry at the given offset,
// resolving delta chains by recursively reading base entries.
func readResolvedPackEntryAt(packPath string, offset uint64) (PackEntry, error) {
	return readResolvedPackEntryAtDepth(packPath, offset, 0)
}

func readResolvedPackEntryAtDepth(packPath string, offset uint64, depth int) (PackEntry, error) {
	if depth > maxDeltaChainDepth {
		return PackEntry{}, fmt.Errorf("delta chain depth exceeds limit (%d) at offset %d", maxDeltaChainDepth, offset)
	}

	entry, err := readPackEntryAt(packPath, offset)
	if err != nil {
		return PackEntry{}, err
	}

	switch entry.Type {
	case PackCommit, PackTree, PackBlob, PackTag:
		return entry, nil

	case PackOfsDelta:
		if entry.BaseDistance == 0 || entry.BaseDistance > entry.Offset {
			return PackEntry{}, fmt.Errorf("invalid ofs-delta base distance %d at offset %d", entry.BaseDistance, entry.Offset)
		}
		baseOffset := entry.Offset - entry.BaseDistance
		base, err := readResolvedPackEntryAtDepth(packPath, baseOffset, depth+1)
		if err != nil {
			return PackEntry{}, fmt.Errorf("resolve ofs-delta base at offset %d: %w", baseOffset, err)
		}
		out, err := applyDelta(base.Data, entry.Data)
		if err != nil {
			return PackEntry{}, fmt.Errorf("apply ofs-delta at offset %d: %w", offset, err)
		}
		entry.Type = base.Type
		entry.Data = out
		return entry, nil

	case PackRefDelta:
		return PackEntry{}, fmt.Errorf("ref-delta resolution not supported in seek-based reader at offset %d", offset)

	default:
		return PackEntry{}, fmt.Errorf("unsupported pack type %d at offset %d", entry.Type, offset)
	}
}

func (s *Store) readFromPacks(h Hash) (ObjectType, []byte, error) {
	idxPaths, err := s.listPackIndexPaths()
	if err != nil {
		return "", nil, err
	}
	for _, idxPath := range idxPaths {
		idx, err := s.cachedPackIndex(idxPath)
		if err != nil {
			return "", nil, fmt.Errorf("object read %s: pack index %s: %w", h, filepath.Base(idxPath), err)
		}
		indexEntry, ok := idx.Find(h)
		if !ok {
			continue
		}

		packPath := packPathForIndex(idxPath)
		packEntry, err := readResolvedPackEntryAt(packPath, indexEntry.Offset)
		if err != nil {
			return "", nil, fmt.Errorf("object read %s: pack %s: %w", h, filepath.Base(packPath), err)
		}
		return decodeIndexedPackEntry(h, packEntry)
	}

	return "", nil, fmt.Errorf("object read %s: %w", h, os.ErrNotExist)
}

func (s *Store) hasInPacks(h Hash) bool {
	idxPaths, err := s.listPackIndexPaths()
	if err != nil {
		return false
	}
	for _, idxPath := range idxPaths {
		idx, err := s.cachedPackIndex(idxPath)
		if err != nil {
			continue
		}
		if _, ok := idx.Find(h); !ok {
			continue
		}
		if _, err := os.Stat(packPathForIndex(idxPath)); err == nil {
			return true
		}
	}
	return false
}

// decodeIndexedPackEntry decodes the object type and content from a resolved
// pack entry. It first tries to parse a zlib envelope ("type len\0content")
// embedded in the entry data — this is the primary mechanism for recovering
// entity/entitylist types that have no distinct pack type representation
// (see objectTypeToPackType). If the envelope is absent or its hash does not
// match, it falls back to deriving the type from the pack entry's type field.
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

		idx, err := s.cachedPackIndex(idxPath)
		if err != nil {
			return nil, fmt.Errorf("pack index %s: %w", filepath.Base(idxPath), err)
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

// objectTypeToPackType maps object types to pack entry types for local GC.
//
// IMPORTANT: Entity and entitylist types cannot be represented as distinct
// pack types (they map to PackBlob). Type information is preserved via the
// zlib envelope embedded in each entry (see makeObjectEnvelope). The remote
// transport path in pkg/remote/pack_transport.go uses a different mechanism
// (PackEntityTrailer). Both strategies must agree: if an object is packed
// locally and later served to a remote client, the envelope must be parseable
// by decodeIndexedPackEntry's fallback path.
func objectTypeToPackType(t ObjectType) PackObjectType {
	switch t {
	case TypeCommit:
		return PackCommit
	case TypeTree:
		return PackTree
	case TypeTag:
		return PackTag
	default:
		return PackBlob
	}
}

func makeObjectEnvelope(objType ObjectType, data []byte) []byte {
	header := fmt.Sprintf("%s %d\x00", objType, len(data))
	out := make([]byte, 0, len(header)+len(data))
	out = append(out, header...)
	out = append(out, data...)
	return out
}
