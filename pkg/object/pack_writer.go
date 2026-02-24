package object

import (
	"compress/zlib"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
)

// PackWriter writes Git-compatible pack streams with zlib-compressed object
// entries. The trailer checksum is SHA-256 over all bytes preceding the trailer.
type PackWriter struct {
	out      io.Writer
	hasher   hash.Hash
	hashedW  io.Writer
	expected uint32
	written  uint32
	finished bool
}

// NewPackWriter initializes a new writer and writes the fixed pack header.
func NewPackWriter(out io.Writer, numObjects uint32) (*PackWriter, error) {
	hasher := sha256.New()
	pw := &PackWriter{
		out:      out,
		hasher:   hasher,
		hashedW:  io.MultiWriter(out, hasher),
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

	zw := zlib.NewWriter(p.hashedW)
	if _, err := zw.Write(data); err != nil {
		_ = zw.Close()
		return fmt.Errorf("write compressed pack entry: %w", err)
	}
	if err := zw.Close(); err != nil {
		return fmt.Errorf("close compressed pack entry: %w", err)
	}

	p.written++
	return nil
}

// Finish validates object count, writes the trailing checksum, and returns the
// checksum as a hex digest.
func (p *PackWriter) Finish() (Hash, error) {
	if p.finished {
		return "", fmt.Errorf("pack writer already finished")
	}
	if p.written != p.expected {
		return "", fmt.Errorf("pack object count mismatch: wrote %d, expected %d", p.written, p.expected)
	}

	sum := p.hasher.Sum(nil)
	if _, err := p.out.Write(sum); err != nil {
		return "", fmt.Errorf("write pack trailer checksum: %w", err)
	}

	p.finished = true
	return Hash(hex.EncodeToString(sum)), nil
}
