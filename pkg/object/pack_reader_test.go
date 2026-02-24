package object

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func TestReadPackRoundTrip(t *testing.T) {
	var buf bytes.Buffer

	pw, err := NewPackWriter(&buf, 2)
	if err != nil {
		t.Fatalf("NewPackWriter: %v", err)
	}
	if err := pw.WriteEntry(PackBlob, []byte("hello")); err != nil {
		t.Fatalf("WriteEntry blob: %v", err)
	}
	if err := pw.WriteEntry(PackCommit, []byte("tree abc\n\nmsg\n")); err != nil {
		t.Fatalf("WriteEntry commit: %v", err)
	}
	if _, err := pw.Finish(); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	pf, err := ReadPack(buf.Bytes())
	if err != nil {
		t.Fatalf("ReadPack: %v", err)
	}
	if pf.Header.NumObjects != 2 {
		t.Fatalf("NumObjects = %d, want 2", pf.Header.NumObjects)
	}
	if len(pf.Entries) != 2 {
		t.Fatalf("len(Entries) = %d, want 2", len(pf.Entries))
	}

	if pf.Entries[0].Type != PackBlob || string(pf.Entries[0].Data) != "hello" {
		t.Fatalf("entry[0] mismatch: %+v", pf.Entries[0])
	}
	if pf.Entries[1].Type != PackCommit || string(pf.Entries[1].Data) != "tree abc\n\nmsg\n" {
		t.Fatalf("entry[1] mismatch: %+v", pf.Entries[1])
	}
	if pf.EntityTrailer != nil {
		t.Fatalf("EntityTrailer = %+v, want nil", pf.EntityTrailer)
	}
}

func TestReadPackRoundTripWithEntityTrailer(t *testing.T) {
	var buf bytes.Buffer

	blobData := []byte("hello")
	blobHash := HashObject(TypeBlob, blobData)
	pw, err := NewPackWriter(&buf, 1)
	if err != nil {
		t.Fatalf("NewPackWriter: %v", err)
	}
	if err := pw.WriteEntry(PackBlob, blobData); err != nil {
		t.Fatalf("WriteEntry blob: %v", err)
	}

	entityEntries := []PackEntityTrailerEntry{
		{
			ObjectHash: blobHash,
			StableID:   "decl:function_definition::Hello",
		},
	}
	if _, err := pw.FinishWithEntityTrailer(entityEntries); err != nil {
		t.Fatalf("FinishWithEntityTrailer: %v", err)
	}

	pf, err := ReadPack(buf.Bytes())
	if err != nil {
		t.Fatalf("ReadPack: %v", err)
	}
	if pf.EntityTrailer == nil {
		t.Fatal("expected non-nil EntityTrailer")
	}
	if got := pf.EntityTrailer.Version; got != packEntityTrailerVersion {
		t.Fatalf("EntityTrailer.Version = %d, want %d", got, packEntityTrailerVersion)
	}
	if len(pf.EntityTrailer.Entries) != 1 {
		t.Fatalf("len(EntityTrailer.Entries) = %d, want 1", len(pf.EntityTrailer.Entries))
	}
	if got := pf.EntityTrailer.Entries[0].ObjectHash; got != blobHash {
		t.Fatalf("EntityTrailer.Entries[0].ObjectHash = %s, want %s", got, blobHash)
	}
	if got := pf.EntityTrailer.Entries[0].StableID; got != "decl:function_definition::Hello" {
		t.Fatalf("EntityTrailer.Entries[0].StableID = %q, want %q", got, "decl:function_definition::Hello")
	}
}

func TestReadPackRejectsMalformedEntityTrailer(t *testing.T) {
	var buf bytes.Buffer

	pw, err := NewPackWriter(&buf, 1)
	if err != nil {
		t.Fatalf("NewPackWriter: %v", err)
	}
	if err := pw.WriteEntry(PackBlob, []byte("hello")); err != nil {
		t.Fatalf("WriteEntry: %v", err)
	}
	if _, err := pw.FinishWithEntityTrailer([]PackEntityTrailerEntry{
		{
			ObjectHash: HashObject(TypeBlob, []byte("hello")),
			StableID:   "decl:function_definition::Hello",
		},
	}); err != nil {
		t.Fatalf("FinishWithEntityTrailer: %v", err)
	}

	data := append([]byte(nil), buf.Bytes()...)
	data[len(data)-1] ^= 0xff

	if _, err := ReadPack(data); err == nil {
		t.Fatal("expected malformed entity trailer error")
	}
}

func TestReadPackRejectsChecksumMismatch(t *testing.T) {
	var buf bytes.Buffer
	pw, err := NewPackWriter(&buf, 1)
	if err != nil {
		t.Fatalf("NewPackWriter: %v", err)
	}
	if err := pw.WriteEntry(PackBlob, []byte("hello")); err != nil {
		t.Fatalf("WriteEntry: %v", err)
	}
	if _, err := pw.Finish(); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	data := append([]byte(nil), buf.Bytes()...)
	data[len(data)-1] ^= 0xff

	if _, err := ReadPack(data); err == nil {
		t.Fatal("expected checksum mismatch error")
	}
}

func TestReadPackRejectsObjectCountMismatch(t *testing.T) {
	var buf bytes.Buffer
	pw, err := NewPackWriter(&buf, 1)
	if err != nil {
		t.Fatalf("NewPackWriter: %v", err)
	}
	if err := pw.WriteEntry(PackBlob, []byte("hello")); err != nil {
		t.Fatalf("WriteEntry: %v", err)
	}
	if _, err := pw.Finish(); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	data := append([]byte(nil), buf.Bytes()...)
	// bump object count from 1 -> 2 and update checksum so count mismatch is hit
	// during decode rather than checksum verification.
	data[11] = 2
	payload := data[:len(data)-32]
	sum := sha256.Sum256(payload)
	copy(data[len(data)-32:], sum[:])

	if _, err := ReadPack(data); err == nil {
		t.Fatal("expected object count mismatch error")
	}
}

func TestReadPackResolvedOfsDelta(t *testing.T) {
	base := []byte("base")
	target := []byte("target")

	var buf bytes.Buffer
	pw, err := NewPackWriter(&buf, 2)
	if err != nil {
		t.Fatalf("NewPackWriter: %v", err)
	}
	baseOffset := pw.CurrentOffset()
	if err := pw.WriteEntry(PackBlob, base); err != nil {
		t.Fatalf("WriteEntry base: %v", err)
	}
	if err := pw.WriteOfsDelta(baseOffset, base, target); err != nil {
		t.Fatalf("WriteOfsDelta: %v", err)
	}
	if _, err := pw.Finish(); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	raw, err := ReadPack(buf.Bytes())
	if err != nil {
		t.Fatalf("ReadPack: %v", err)
	}
	if got := raw.Entries[1].Type; got != PackOfsDelta {
		t.Fatalf("raw delta type = %d, want %d", got, PackOfsDelta)
	}
	if got := raw.Entries[1].OriginalType; got != PackOfsDelta {
		t.Fatalf("raw delta original type = %d, want %d", got, PackOfsDelta)
	}
	if raw.Entries[1].BaseDistance == 0 {
		t.Fatal("expected non-zero base distance for OFS_DELTA entry")
	}

	resolved, err := ReadPackResolved(buf.Bytes())
	if err != nil {
		t.Fatalf("ReadPackResolved: %v", err)
	}
	if got := resolved.Entries[0].Type; got != PackBlob {
		t.Fatalf("entry[0] type = %d, want %d", got, PackBlob)
	}
	if got := resolved.Entries[1].Type; got != PackBlob {
		t.Fatalf("resolved delta type = %d, want %d", got, PackBlob)
	}
	if got := resolved.Entries[1].OriginalType; got != PackOfsDelta {
		t.Fatalf("resolved delta original type = %d, want %d", got, PackOfsDelta)
	}
	if got := string(resolved.Entries[1].Data); got != "target" {
		t.Fatalf("resolved delta data = %q, want %q", got, "target")
	}
}

func TestReadPackResolvedRefDelta(t *testing.T) {
	base := []byte("base")
	target := []byte("target")
	delta := buildInsertOnlyDelta(base, target)

	baseHash := HashObject(TypeBlob, base)
	baseHashBytes, err := hex.DecodeString(string(baseHash))
	if err != nil {
		t.Fatalf("DecodeString(base hash): %v", err)
	}
	if len(baseHashBytes) != 32 {
		t.Fatalf("base hash bytes len = %d, want 32", len(baseHashBytes))
	}

	refEntryPrefix := append(encodePackEntryHeader(PackRefDelta, uint64(len(delta))), baseHashBytes...)
	packData := makePackDataForReaderTests(t,
		packRawEntry{objType: PackBlob, raw: base},
		packRawEntry{rawPrefix: refEntryPrefix, raw: delta},
	)

	raw, err := ReadPack(packData)
	if err != nil {
		t.Fatalf("ReadPack: %v", err)
	}
	if got := raw.Entries[1].Type; got != PackRefDelta {
		t.Fatalf("raw ref-delta type = %d, want %d", got, PackRefDelta)
	}
	if got := raw.Entries[1].OriginalType; got != PackRefDelta {
		t.Fatalf("raw ref-delta original type = %d, want %d", got, PackRefDelta)
	}
	if got := raw.Entries[1].BaseRef; got != baseHash {
		t.Fatalf("raw ref-delta base ref = %s, want %s", got, baseHash)
	}

	resolved, err := ReadPackResolved(packData)
	if err != nil {
		t.Fatalf("ReadPackResolved: %v", err)
	}
	if got := resolved.Entries[1].Type; got != PackBlob {
		t.Fatalf("resolved ref-delta type = %d, want %d", got, PackBlob)
	}
	if got := resolved.Entries[1].OriginalType; got != PackRefDelta {
		t.Fatalf("resolved ref-delta original type = %d, want %d", got, PackRefDelta)
	}
	if got := string(resolved.Entries[1].Data); got != "target" {
		t.Fatalf("resolved ref-delta data = %q, want %q", got, "target")
	}
}

func TestResolvePackEntriesUnresolvedDelta(t *testing.T) {
	entries := []PackEntry{
		{
			Type:         PackRefDelta,
			OriginalType: PackRefDelta,
			Offset:       12,
			BaseRef:      Hash(strings.Repeat("f", 64)),
			Data:         buildInsertOnlyDelta([]byte("base"), []byte("target")),
		},
	}
	if _, err := ResolvePackEntries(entries); err == nil {
		t.Fatal("expected unresolved delta error")
	}
}

type packRawEntry struct {
	objType    PackObjectType
	rawPrefix  []byte
	raw        []byte
	useObjType bool
}

func makePackDataForReaderTests(t *testing.T, entries ...packRawEntry) []byte {
	t.Helper()

	var payload bytes.Buffer
	header := PackHeader{Version: supportedPackVersion, NumObjects: uint32(len(entries))}
	payload.Write(header.Marshal())

	for _, e := range entries {
		if len(e.rawPrefix) > 0 {
			payload.Write(e.rawPrefix)
		} else {
			payload.Write(encodePackEntryHeader(e.objType, uint64(len(e.raw))))
		}
		compressed, err := compressPackPayload(e.raw)
		if err != nil {
			t.Fatalf("compressPackPayload: %v", err)
		}
		payload.Write(compressed)
	}

	sum := sha256.Sum256(payload.Bytes())
	out := append([]byte(nil), payload.Bytes()...)
	out = append(out, sum[:]...)
	return out
}
