package repo

import (
	"fmt"
	"testing"

	"github.com/odvcencio/got/pkg/object"
)

var benchmarkFlattenTreeEntryCount int

func BenchmarkFlattenTree(b *testing.B) {
	const (
		dirCount    = 16
		filesPerDir = 256
	)

	r, rootHash, wantEntries := buildFlattenBenchmarkTree(b, dirCount, filesPerDir)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		entries, err := r.FlattenTree(rootHash)
		if err != nil {
			b.Fatalf("FlattenTree: %v", err)
		}
		if len(entries) != wantEntries {
			b.Fatalf("FlattenTree returned %d entries, want %d", len(entries), wantEntries)
		}
		benchmarkFlattenTreeEntryCount += len(entries)
	}
}

func buildFlattenBenchmarkTree(b *testing.B, dirCount, filesPerDir int) (*Repo, object.Hash, int) {
	b.Helper()

	r, err := Init(b.TempDir())
	if err != nil {
		b.Fatalf("Init: %v", err)
	}

	rootEntries := make([]object.TreeEntry, 0, dirCount)
	fileSeed := 1
	for d := 0; d < dirCount; d++ {
		subtreeEntries := make([]object.TreeEntry, 0, filesPerDir)
		for f := 0; f < filesPerDir; f++ {
			subtreeEntries = append(subtreeEntries, object.TreeEntry{
				Name:     fmt.Sprintf("file-%04d.go", f),
				IsDir:    false,
				Mode:     object.TreeModeFile,
				BlobHash: testTreeHash(fileSeed),
			})
			fileSeed++
		}

		subtreeHash, err := r.Store.WriteTree(&object.TreeObj{Entries: subtreeEntries})
		if err != nil {
			b.Fatalf("write subtree %d: %v", d, err)
		}

		rootEntries = append(rootEntries, object.TreeEntry{
			Name:        fmt.Sprintf("dir-%02d", d),
			IsDir:       true,
			Mode:        object.TreeModeDir,
			SubtreeHash: subtreeHash,
		})
	}

	rootHash, err := r.Store.WriteTree(&object.TreeObj{Entries: rootEntries})
	if err != nil {
		b.Fatalf("write root tree: %v", err)
	}

	return r, rootHash, dirCount * filesPerDir
}
