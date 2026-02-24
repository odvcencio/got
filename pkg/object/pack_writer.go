package object

import (
	"bytes"
	"compress/zlib"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
)

type packCountedWriter struct {
	w io.Writer
	n uint64
}

func (cw *packCountedWriter) Write(p []byte) (int, error) {
	n, err := cw.w.Write(p)
	cw.n += uint64(n)
	return n, err
}

func (cw *packCountedWriter) Count() uint64 {
	return cw.n
}

func compressPackPayload(raw []byte) ([]byte, error) {
	var buf bytes.Buffer
	zw := zlib.NewWriter(&buf)
	if _, err := zw.Write(raw); err != nil {
		_ = zw.Close()
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// PackWriter writes Git-compatible pack streams with zlib-compressed object
// entries. The trailer checksum is SHA-256 over all bytes preceding the trailer.
type PackWriter struct {
	out      io.Writer
	hasher   hash.Hash
	hashedW  io.Writer
	counter  *packCountedWriter
	expected uint32
	written  uint32
	finished bool
}

// NewPackWriter initializes a new writer and writes the fixed pack header.
func NewPackWriter(out io.Writer, numObjects uint32) (*PackWriter, error) {
	hasher := sha256.New()
	counter := &packCountedWriter{w: out}
	pw := &PackWriter{
		out:      out,
		hasher:   hasher,
		hashedW:  io.MultiWriter(counter, hasher),
		counter:  counter,
		expected: numObjects,
	}

	header := PackHeader{
		Version:    supportedPackVersion,
		NumObjects: numObjects,
	}
	if _, err := pw.hashedW.Write(header.Marshal()); err != nil {
		return nil, fmt.Errorf("write pack header: %w", err)
	}
	return pw, nil
}

// CurrentOffset returns the current byte offset in the pack stream (from pack
// start), excluding the trailing checksum written by Finish().
func (p *PackWriter) CurrentOffset() uint64 {
	return p.counter.Count()
}

// WriteEntry appends one object entry to the pack stream.
func (p *PackWriter) WriteEntry(objType PackObjectType, data []byte) error {
	if p.finished {
		return fmt.Errorf("pack writer already finished")
	}
	if p.written >= p.expected {
		return fmt.Errorf("pack object count exceeded: expected %d", p.expected)
	}

	header := encodePackEntryHeader(objType, uint64(len(data)))
	if _, err := p.hashedW.Write(header); err != nil {
		return fmt.Errorf("write pack entry header: %w", err)
	}

	compressed, err := compressPackPayload(data)
	if err != nil {
		return fmt.Errorf("compress pack entry: %w", err)
	}
	if _, err := p.hashedW.Write(compressed); err != nil {
		return fmt.Errorf("write compressed pack entry: %w", err)
	}

	p.written++
	return nil
}

// WriteOfsDelta writes an OFS_DELTA entry using an insert-only delta stream.
func (p *PackWriter) WriteOfsDelta(baseOffset uint64, baseData, targetData []byte) error {
	if p.finished {
		return fmt.Errorf("pack writer already finished")
	}
	if p.written >= p.expected {
		return fmt.Errorf("pack object count exceeded: expected %d", p.expected)
	}
	current := p.CurrentOffset()
	if baseOffset >= current {
		return fmt.Errorf("base offset %d must be before current offset %d", baseOffset, current)
	}

	delta := buildInsertOnlyDelta(baseData, targetData)
	header := encodePackEntryHeader(PackOfsDelta, uint64(len(delta)))
	ofs := encodeOfsDeltaDistance(current - baseOffset)
	compressed, err := compressPackPayload(delta)
	if err != nil {
		return fmt.Errorf("compress delta payload: %w", err)
	}

	if _, err := p.hashedW.Write(header); err != nil {
		return fmt.Errorf("write ofs-delta header: %w", err)
	}
	if _, err := p.hashedW.Write(ofs); err != nil {
		return fmt.Errorf("write ofs-delta base distance: %w", err)
	}
	if _, err := p.hashedW.Write(compressed); err != nil {
		return fmt.Errorf("write ofs-delta payload: %w", err)
	}

	p.written++
	return nil
}

// Finish validates object count, writes the trailing pack checksum, and returns
// that checksum as a hex digest.
func (p *PackWriter) Finish() (Hash, error) {
	return p.finish(nil)
}

// FinishWithEntityTrailer behaves like Finish and additionally appends the
// Got-specific entity trailer after the standard pack checksum.
func (p *PackWriter) FinishWithEntityTrailer(entries []PackEntityTrailerEntry) (Hash, error) {
	return p.finish(entries)
}

func (p *PackWriter) finish(entityTrailerEntries []PackEntityTrailerEntry) (Hash, error) {
	if p.finished {
		return "", fmt.Errorf("pack writer already finished")
	}
	if p.written != p.expected {
		return "", fmt.Errorf("pack object count mismatch: wrote %d, expected %d", p.written, p.expected)
	}

	var entityTrailerRaw []byte
	var err error
	if len(entityTrailerEntries) > 0 {
		entityTrailerRaw, err = MarshalPackEntityTrailer(entityTrailerEntries)
		if err != nil {
			return "", fmt.Errorf("marshal entity trailer: %w", err)
		}
	}

	sum := p.hasher.Sum(nil)
	if _, err := p.out.Write(sum); err != nil {
		return "", fmt.Errorf("write pack trailer checksum: %w", err)
	}
	if len(entityTrailerRaw) > 0 {
		if _, err := p.out.Write(entityTrailerRaw); err != nil {
			return "", fmt.Errorf("write entity trailer: %w", err)
		}
	}

	p.finished = true
	return Hash(hex.EncodeToString(sum)), nil
}
