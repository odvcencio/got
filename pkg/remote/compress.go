package remote

import (
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/klauspost/compress/zstd"
)

var zstdEncoderPool = sync.Pool{
	New: func() interface{} {
		enc, _ := zstd.NewWriter(nil)
		return enc
	},
}

var zstdDecoderPool = sync.Pool{
	New: func() interface{} {
		dec, _ := zstd.NewReader(nil)
		return dec
	},
}

// compressZstd compresses data using zstd.
func compressZstd(data []byte) ([]byte, error) {
	enc := zstdEncoderPool.Get().(*zstd.Encoder)
	defer zstdEncoderPool.Put(enc)
	return enc.EncodeAll(data, nil), nil
}

// decompressZstd decompresses zstd-compressed data.
func decompressZstd(data []byte) ([]byte, error) {
	dec := zstdDecoderPool.Get().(*zstd.Decoder)
	defer zstdDecoderPool.Put(dec)
	result, err := dec.DecodeAll(data, nil)
	if err != nil {
		return nil, fmt.Errorf("zstd decompress: %w", err)
	}
	return result, nil
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
