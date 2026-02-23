package object

import (
	"crypto/rand"
	"testing"
)

// BenchmarkStoreWriteSmall benchmarks writing a 100-byte blob to the store.
func BenchmarkStoreWriteSmall(b *testing.B) {
	dir := b.TempDir()
	s := NewStore(dir)

	// Generate distinct 100-byte payloads so each write is unique
	// (avoids the Has() fast path after the first write).
	payloads := make([][]byte, b.N)
	for i := range payloads {
		buf := make([]byte, 100)
		if _, err := rand.Read(buf); err != nil {
			b.Fatalf("rand.Read: %v", err)
		}
		payloads[i] = buf
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.Write(TypeBlob, payloads[i]); err != nil {
			b.Fatalf("Write: %v", err)
		}
	}
}

// BenchmarkStoreWriteLarge benchmarks writing a 100KB blob to the store.
func BenchmarkStoreWriteLarge(b *testing.B) {
	dir := b.TempDir()
	s := NewStore(dir)

	payloads := make([][]byte, b.N)
	for i := range payloads {
		buf := make([]byte, 100*1024)
		if _, err := rand.Read(buf); err != nil {
			b.Fatalf("rand.Read: %v", err)
		}
		payloads[i] = buf
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.Write(TypeBlob, payloads[i]); err != nil {
			b.Fatalf("Write: %v", err)
		}
	}
}

// BenchmarkStoreRead benchmarks reading back a previously written blob.
func BenchmarkStoreRead(b *testing.B) {
	dir := b.TempDir()
	s := NewStore(dir)

	data := make([]byte, 4096)
	if _, err := rand.Read(data); err != nil {
		b.Fatalf("rand.Read: %v", err)
	}
	h, err := s.Write(TypeBlob, data)
	if err != nil {
		b.Fatalf("Write: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, err := s.Read(h)
		if err != nil {
			b.Fatalf("Read: %v", err)
		}
	}
}
