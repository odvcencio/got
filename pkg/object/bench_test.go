package object

import (
	"fmt"
	"path/filepath"
	"testing"
)

func BenchmarkStoreWriteUniqueBlob(b *testing.B) {
	store := NewStore(filepath.Join(b.TempDir(), "store"))
	seed := []byte("0123456789abcdef0123456789abcdef")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		payload := []byte(fmt.Sprintf("blob-%d-%x", i, seed))
		if _, err := store.Write(TypeBlob, payload); err != nil {
			b.Fatalf("Write: %v", err)
		}
	}
}

func BenchmarkStoreReadBlob(b *testing.B) {
	store := NewStore(filepath.Join(b.TempDir(), "store"))
	payload := []byte("package main\n\nfunc main() { println(\"hello\") }\n")
	hash, err := store.Write(TypeBlob, payload)
	if err != nil {
		b.Fatalf("Write: %v", err)
	}

	b.SetBytes(int64(len(payload)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		typ, data, err := store.Read(hash)
		if err != nil {
			b.Fatalf("Read: %v", err)
		}
		if typ != TypeBlob {
			b.Fatalf("type = %q, want %q", typ, TypeBlob)
		}
		if len(data) != len(payload) {
			b.Fatalf("len(data) = %d, want %d", len(data), len(payload))
		}
	}
}
