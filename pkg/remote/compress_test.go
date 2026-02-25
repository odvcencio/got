package remote

import (
	"bytes"
	"testing"
)

func TestZstdRoundTrip(t *testing.T) {
	original := []byte("hello world, this is a test of zstd compression in the got protocol")
	compressed, err := compressZstd(original)
	if err != nil {
		t.Fatalf("compressZstd: %v", err)
	}
	if len(compressed) >= len(original) {
		t.Logf("warning: compressed %d >= original %d", len(compressed), len(original))
	}

	decompressed, err := decompressZstd(compressed)
	if err != nil {
		t.Fatalf("decompressZstd: %v", err)
	}
	if !bytes.Equal(decompressed, original) {
		t.Fatalf("round-trip mismatch")
	}
}

func TestZstdStreamRoundTrip(t *testing.T) {
	original := bytes.Repeat([]byte("got protocol compression test data\n"), 100)
	var compressed bytes.Buffer
	if err := compressZstdStream(&compressed, bytes.NewReader(original)); err != nil {
		t.Fatalf("compressZstdStream: %v", err)
	}

	var decompressed bytes.Buffer
	if err := decompressZstdStream(&decompressed, &compressed); err != nil {
		t.Fatalf("decompressZstdStream: %v", err)
	}
	if !bytes.Equal(decompressed.Bytes(), original) {
		t.Fatalf("stream round-trip mismatch: got %d bytes, want %d", decompressed.Len(), len(original))
	}
}

func TestZstdEmptyInput(t *testing.T) {
	compressed, err := compressZstd(nil)
	if err != nil {
		t.Fatalf("compressZstd(nil): %v", err)
	}
	decompressed, err := decompressZstd(compressed)
	if err != nil {
		t.Fatalf("decompressZstd: %v", err)
	}
	if len(decompressed) != 0 {
		t.Fatalf("expected empty, got %d bytes", len(decompressed))
	}
}
