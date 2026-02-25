package repo

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/odvcencio/got/pkg/object"
)

var benchmarkStatusEntrySink int

func BenchmarkStatus_StatShortcutAvoidsRehash(b *testing.B) {
	dir := b.TempDir()
	r, err := Init(dir)
	if err != nil {
		b.Fatalf("Init: %v", err)
	}

	const fileCount = 200
	paths := make([]string, 0, fileCount)
	for i := 0; i < fileCount; i++ {
		relPath := fmt.Sprintf("bench/file-%03d.txt", i)
		absPath := filepath.Join(dir, filepath.FromSlash(relPath))
		if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
			b.Fatalf("MkdirAll(%q): %v", relPath, err)
		}
		if err := os.WriteFile(absPath, []byte("line 1\nline 2\n"), 0o644); err != nil {
			b.Fatalf("WriteFile(%q): %v", relPath, err)
		}
		paths = append(paths, relPath)
	}

	if err := r.Add(paths); err != nil {
		b.Fatalf("Add: %v", err)
	}
	if _, err := r.Commit("seed", "bench"); err != nil {
		b.Fatalf("Commit: %v", err)
	}

	coarseTime := time.Now().Add(-10 * time.Second).Truncate(time.Second)
	for _, relPath := range paths {
		absPath := filepath.Join(dir, filepath.FromSlash(relPath))
		if err := os.Chtimes(absPath, coarseTime, coarseTime); err != nil {
			b.Fatalf("Chtimes(%q): %v", relPath, err)
		}
	}

	// Ensure both mtime and ctime are out of the racy-clean window before
	// priming staging metadata.
	time.Sleep(statusRacyCleanWindow + 100*time.Millisecond)

	if _, err := r.Status(); err != nil {
		b.Fatalf("Status(prime): %v", err)
	}
	stg, err := r.ReadStaging()
	if err != nil {
		b.Fatalf("ReadStaging: %v", err)
	}
	first := stg.Entries[paths[0]]
	if first == nil || !first.HasChangeTime {
		b.Skip("platform does not expose change time metadata")
	}

	r.invalidateStatusCache()

	hashCalls := 0
	r.statusBlobHasher = func(data []byte) object.Hash {
		hashCalls++
		return object.HashObject(object.TypeBlob, data)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		entries, err := r.Status()
		if err != nil {
			b.Fatalf("Status: %v", err)
		}
		benchmarkStatusEntrySink += len(entries)
	}
	b.StopTimer()

	if hashCalls != 0 {
		b.Fatalf("status re-hashed %d files during benchmark loop; want 0", hashCalls)
	}
}
