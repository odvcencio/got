package remote

import (
	"io"
	"strings"

	"github.com/klauspost/compress/zstd"
)

// compressZstd compresses data using zstd.
func compressZstd(data []byte) ([]byte, error) {
	enc, err := zstd.NewWriter(nil)
	if err != nil {
		return nil, err
	}
	defer enc.Close()
	return enc.EncodeAll(data, nil), nil
}

// decompressZstd decompresses zstd-compressed data.
func decompressZstd(data []byte) ([]byte, error) {
	dec, err := zstd.NewReader(nil)
	if err != nil {
		return nil, err
	}
	defer dec.Close()
	return dec.DecodeAll(data, nil)
}

// compressZstdStream compresses from src to dst using streaming zstd.
func compressZstdStream(dst io.Writer, src io.Reader) error {
	enc, err := zstd.NewWriter(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(enc, src); err != nil {
		enc.Close()
		return err
	}
	return enc.Close()
}

// decompressZstdStream decompresses from src to dst using streaming zstd.
func decompressZstdStream(dst io.Writer, src io.Reader) error {
	dec, err := zstd.NewReader(src)
	if err != nil {
		return err
	}
	defer dec.Close()
	_, err = io.Copy(dst, dec)
	return err
}

// newZstdReader wraps an io.Reader with zstd decompression.
func newZstdReader(r io.Reader) (io.ReadCloser, error) {
	dec, err := zstd.NewReader(r)
	if err != nil {
		return nil, err
	}
	return &zstdReadCloser{dec: dec}, nil
}

type zstdReadCloser struct {
	dec *zstd.Decoder
}

func (z *zstdReadCloser) Read(p []byte) (int, error) {
	return z.dec.Read(p)
}

func (z *zstdReadCloser) Close() error {
	z.dec.Close()
	return nil
}

// isZstdEncoded checks if the content encoding includes zstd.
func isZstdEncoded(contentEncoding string) bool {
	return strings.Contains(contentEncoding, "zstd")
}
