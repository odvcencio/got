package repo

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/odvcencio/graft/pkg/object"
)

// makeHash creates a deterministic test hash from an index.
func makeHash(i int) object.Hash {
	raw := sha256.Sum256([]byte(fmt.Sprintf("test-hash-%d", i)))
	return object.Hash(hex.EncodeToString(raw[:]))
}

// TestBinaryCommitGraph_RoundTrip_Empty tests writing and reading an empty graph.
func TestBinaryCommitGraph_RoundTrip_Empty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "commit-graph")

	entries := map[object.Hash]*CommitGraphEntry{}

	if err := WriteBinaryCommitGraph(path, entries); err != nil {
		t.Fatalf("WriteBinaryCommitGraph: %v", err)
	}

	got, err := ReadBinaryCommitGraph(path)
	if err != nil {
		t.Fatalf("ReadBinaryCommitGraph: %v", err)
	}

	if len(got) != 0 {
		t.Errorf("expected 0 entries, got %d", len(got))
	}
}

// TestBinaryCommitGraph_RoundTrip_SingleEntry tests a single root commit.
func TestBinaryCommitGraph_RoundTrip_SingleEntry(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "commit-graph")

	h := makeHash(1)
	tree := makeHash(100)

	entries := map[object.Hash]*CommitGraphEntry{
		h: {
			TreeHash:   tree,
			Parents:    nil,
			Generation: 1,
			Timestamp:  1700000000,
		},
	}

	if err := WriteBinaryCommitGraph(path, entries); err != nil {
		t.Fatalf("WriteBinaryCommitGraph: %v", err)
	}

	got, err := ReadBinaryCommitGraph(path)
	if err != nil {
		t.Fatalf("ReadBinaryCommitGraph: %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(got))
	}

	e, ok := got[h]
	if !ok {
		t.Fatalf("entry for hash %s not found", h)
	}
	if e.TreeHash != tree {
		t.Errorf("TreeHash = %s, want %s", e.TreeHash, tree)
	}
	if len(e.Parents) != 0 {
		t.Errorf("Parents = %v, want empty", e.Parents)
	}
	if e.Generation != 1 {
		t.Errorf("Generation = %d, want 1", e.Generation)
	}
	if e.Timestamp != 1700000000 {
		t.Errorf("Timestamp = %d, want 1700000000", e.Timestamp)
	}
}

// TestBinaryCommitGraph_RoundTrip_LinearChain tests a linear chain of commits.
func TestBinaryCommitGraph_RoundTrip_LinearChain(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "commit-graph")

	hashes := make([]object.Hash, 5)
	trees := make([]object.Hash, 5)
	for i := range hashes {
		hashes[i] = makeHash(i)
		trees[i] = makeHash(100 + i)
	}

	entries := make(map[object.Hash]*CommitGraphEntry, 5)
	for i, h := range hashes {
		var parents []object.Hash
		if i > 0 {
			parents = []object.Hash{hashes[i-1]}
		}
		entries[h] = &CommitGraphEntry{
			TreeHash:   trees[i],
			Parents:    parents,
			Generation: uint32(i + 1),
			Timestamp:  int64(1700000000 + i*100),
		}
	}

	if err := WriteBinaryCommitGraph(path, entries); err != nil {
		t.Fatalf("WriteBinaryCommitGraph: %v", err)
	}

	got, err := ReadBinaryCommitGraph(path)
	if err != nil {
		t.Fatalf("ReadBinaryCommitGraph: %v", err)
	}

	if len(got) != 5 {
		t.Fatalf("expected 5 entries, got %d", len(got))
	}

	for i, h := range hashes {
		e, ok := got[h]
		if !ok {
			t.Errorf("entry %d (hash %s) not found", i, h)
			continue
		}
		if e.TreeHash != trees[i] {
			t.Errorf("entry %d: TreeHash = %s, want %s", i, e.TreeHash, trees[i])
		}
		if e.Generation != uint32(i+1) {
			t.Errorf("entry %d: Generation = %d, want %d", i, e.Generation, i+1)
		}
		if e.Timestamp != int64(1700000000+i*100) {
			t.Errorf("entry %d: Timestamp = %d, want %d", i, e.Timestamp, 1700000000+i*100)
		}
		if i == 0 {
			if len(e.Parents) != 0 {
				t.Errorf("entry 0: Parents = %v, want empty", e.Parents)
			}
		} else {
			if len(e.Parents) != 1 || e.Parents[0] != hashes[i-1] {
				t.Errorf("entry %d: Parents = %v, want [%s]", i, e.Parents, hashes[i-1])
			}
		}
	}
}

// TestBinaryCommitGraph_RoundTrip_TwoParents tests a merge commit with 2 parents.
func TestBinaryCommitGraph_RoundTrip_TwoParents(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "commit-graph")

	root := makeHash(0)
	childA := makeHash(1)
	childB := makeHash(2)
	merge := makeHash(3)
	tree := makeHash(100)

	entries := map[object.Hash]*CommitGraphEntry{
		root: {
			TreeHash:   tree,
			Parents:    nil,
			Generation: 1,
			Timestamp:  1700000000,
		},
		childA: {
			TreeHash:   tree,
			Parents:    []object.Hash{root},
			Generation: 2,
			Timestamp:  1700000100,
		},
		childB: {
			TreeHash:   tree,
			Parents:    []object.Hash{root},
			Generation: 2,
			Timestamp:  1700000200,
		},
		merge: {
			TreeHash:   tree,
			Parents:    []object.Hash{childA, childB},
			Generation: 3,
			Timestamp:  1700000300,
		},
	}

	if err := WriteBinaryCommitGraph(path, entries); err != nil {
		t.Fatalf("WriteBinaryCommitGraph: %v", err)
	}

	got, err := ReadBinaryCommitGraph(path)
	if err != nil {
		t.Fatalf("ReadBinaryCommitGraph: %v", err)
	}

	if len(got) != 4 {
		t.Fatalf("expected 4 entries, got %d", len(got))
	}

	mergeEntry := got[merge]
	if mergeEntry == nil {
		t.Fatal("merge entry not found")
	}
	if len(mergeEntry.Parents) != 2 {
		t.Fatalf("merge Parents count = %d, want 2", len(mergeEntry.Parents))
	}
	if mergeEntry.Parents[0] != childA || mergeEntry.Parents[1] != childB {
		t.Errorf("merge Parents = %v, want [%s, %s]", mergeEntry.Parents, childA, childB)
	}
}

// TestBinaryCommitGraph_RoundTrip_OverflowParents tests a commit with >2 parents
// (octopus merge), which triggers the overflow mechanism.
func TestBinaryCommitGraph_RoundTrip_OverflowParents(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "commit-graph")

	// Create 5 parents + 1 octopus merge.
	parents := make([]object.Hash, 5)
	tree := makeHash(100)
	entries := make(map[object.Hash]*CommitGraphEntry)

	for i := range parents {
		parents[i] = makeHash(i)
		entries[parents[i]] = &CommitGraphEntry{
			TreeHash:   tree,
			Parents:    nil,
			Generation: 1,
			Timestamp:  int64(1700000000 + i*100),
		}
	}

	octopus := makeHash(99)
	entries[octopus] = &CommitGraphEntry{
		TreeHash:   tree,
		Parents:    parents,
		Generation: 2,
		Timestamp:  1700000500,
	}

	if err := WriteBinaryCommitGraph(path, entries); err != nil {
		t.Fatalf("WriteBinaryCommitGraph: %v", err)
	}

	got, err := ReadBinaryCommitGraph(path)
	if err != nil {
		t.Fatalf("ReadBinaryCommitGraph: %v", err)
	}

	if len(got) != 6 {
		t.Fatalf("expected 6 entries, got %d", len(got))
	}

	octEntry := got[octopus]
	if octEntry == nil {
		t.Fatal("octopus entry not found")
	}
	if len(octEntry.Parents) != 5 {
		t.Fatalf("octopus Parents count = %d, want 5", len(octEntry.Parents))
	}
	for i, p := range octEntry.Parents {
		if p != parents[i] {
			t.Errorf("octopus Parents[%d] = %s, want %s", i, p, parents[i])
		}
	}
}

// TestBinaryCommitGraph_FanoutCorrectness verifies the fanout table enables
// correct lookups by checking that entries with different first-byte hash
// prefixes all round-trip correctly.
func TestBinaryCommitGraph_FanoutCorrectness(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "commit-graph")

	// Generate many entries to get a spread of first-byte values.
	entries := make(map[object.Hash]*CommitGraphEntry)
	tree := makeHash(9999)
	for i := 0; i < 200; i++ {
		h := makeHash(i)
		entries[h] = &CommitGraphEntry{
			TreeHash:   tree,
			Parents:    nil,
			Generation: 1,
			Timestamp:  int64(1700000000 + i),
		}
	}

	if err := WriteBinaryCommitGraph(path, entries); err != nil {
		t.Fatalf("WriteBinaryCommitGraph: %v", err)
	}

	got, err := ReadBinaryCommitGraph(path)
	if err != nil {
		t.Fatalf("ReadBinaryCommitGraph: %v", err)
	}

	if len(got) != 200 {
		t.Fatalf("expected 200 entries, got %d", len(got))
	}

	// Verify every entry is present.
	for i := 0; i < 200; i++ {
		h := makeHash(i)
		if _, ok := got[h]; !ok {
			t.Errorf("entry %d (hash %s) not found after round-trip", i, h)
		}
	}
}

// TestBinaryCommitGraph_JSONFallback tests that ReadCommitGraph on a Repo
// can still read old JSON-format files.
func TestBinaryCommitGraph_JSONFallback(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Manually write a JSON commit-graph file (old format).
	h := makeHash(1)
	tree := makeHash(100)
	gf := commitGraphFile{
		Version: 1,
		Entries: map[object.Hash]*CommitGraphEntry{
			h: {
				TreeHash:   tree,
				Parents:    nil,
				Generation: 1,
				Timestamp:  1700000000,
			},
		},
	}
	data, err := json.Marshal(gf)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	graphPath := filepath.Join(r.GraftDir, "objects", "info", "commit-graph")
	if err := os.MkdirAll(filepath.Dir(graphPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(graphPath, data, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// ReadCommitGraph should detect JSON and read it correctly.
	graph, err := r.ReadCommitGraph()
	if err != nil {
		t.Fatalf("ReadCommitGraph: %v", err)
	}

	if len(graph.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(graph.Entries))
	}

	e := graph.Lookup(h)
	if e == nil {
		t.Fatalf("entry for %s not found", h)
	}
	if e.TreeHash != tree {
		t.Errorf("TreeHash = %s, want %s", e.TreeHash, tree)
	}
	if e.Generation != 1 {
		t.Errorf("Generation = %d, want 1", e.Generation)
	}
}

// TestBinaryCommitGraph_FormatDetection tests that ReadCommitGraph on a Repo
// correctly dispatches between binary and JSON formats.
func TestBinaryCommitGraph_FormatDetection(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	h := makeHash(42)
	tree := makeHash(200)

	graphPath := filepath.Join(r.GraftDir, "objects", "info", "commit-graph")
	if err := os.MkdirAll(filepath.Dir(graphPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Write binary format directly.
	entries := map[object.Hash]*CommitGraphEntry{
		h: {
			TreeHash:   tree,
			Parents:    nil,
			Generation: 7,
			Timestamp:  1700000042,
		},
	}
	if err := WriteBinaryCommitGraph(graphPath, entries); err != nil {
		t.Fatalf("WriteBinaryCommitGraph: %v", err)
	}

	// ReadCommitGraph should detect binary magic and read it.
	graph, err := r.ReadCommitGraph()
	if err != nil {
		t.Fatalf("ReadCommitGraph: %v", err)
	}

	if len(graph.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(graph.Entries))
	}
	e := graph.Lookup(h)
	if e == nil {
		t.Fatalf("entry not found")
	}
	if e.Generation != 7 {
		t.Errorf("Generation = %d, want 7", e.Generation)
	}
	if e.Timestamp != 1700000042 {
		t.Errorf("Timestamp = %d, want 1700000042", e.Timestamp)
	}
}

// TestBinaryCommitGraph_Checksum verifies that a corrupted file is detected.
func TestBinaryCommitGraph_Checksum(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "commit-graph")

	entries := map[object.Hash]*CommitGraphEntry{
		makeHash(1): {
			TreeHash:   makeHash(100),
			Parents:    nil,
			Generation: 1,
			Timestamp:  1700000000,
		},
	}

	if err := WriteBinaryCommitGraph(path, entries); err != nil {
		t.Fatalf("WriteBinaryCommitGraph: %v", err)
	}

	// Corrupt a byte in the middle of the file.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	data[len(data)/2] ^= 0xff
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, err = ReadBinaryCommitGraph(path)
	if err == nil {
		t.Fatal("expected error reading corrupted file, got nil")
	}
}

// TestBinaryCommitGraph_RoundTrip_ThreeParents tests a commit with exactly
// 3 parents (triggers overflow, but just barely).
func TestBinaryCommitGraph_RoundTrip_ThreeParents(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "commit-graph")

	p1 := makeHash(1)
	p2 := makeHash(2)
	p3 := makeHash(3)
	merge := makeHash(4)
	tree := makeHash(100)

	entries := map[object.Hash]*CommitGraphEntry{
		p1: {TreeHash: tree, Parents: nil, Generation: 1, Timestamp: 1700000000},
		p2: {TreeHash: tree, Parents: nil, Generation: 1, Timestamp: 1700000100},
		p3: {TreeHash: tree, Parents: nil, Generation: 1, Timestamp: 1700000200},
		merge: {
			TreeHash:   tree,
			Parents:    []object.Hash{p1, p2, p3},
			Generation: 2,
			Timestamp:  1700000300,
		},
	}

	if err := WriteBinaryCommitGraph(path, entries); err != nil {
		t.Fatalf("WriteBinaryCommitGraph: %v", err)
	}

	got, err := ReadBinaryCommitGraph(path)
	if err != nil {
		t.Fatalf("ReadBinaryCommitGraph: %v", err)
	}

	if len(got) != 4 {
		t.Fatalf("expected 4 entries, got %d", len(got))
	}

	mergeEntry := got[merge]
	if mergeEntry == nil {
		t.Fatal("merge entry not found")
	}
	if len(mergeEntry.Parents) != 3 {
		t.Fatalf("merge Parents count = %d, want 3", len(mergeEntry.Parents))
	}
	if mergeEntry.Parents[0] != p1 || mergeEntry.Parents[1] != p2 || mergeEntry.Parents[2] != p3 {
		t.Errorf("merge Parents = %v, want [%s, %s, %s]", mergeEntry.Parents, p1, p2, p3)
	}
}

// BenchmarkBinaryCommitGraph_Write benchmarks writing binary commit graphs
// of various sizes.
func BenchmarkBinaryCommitGraph_Write(b *testing.B) {
	for _, size := range []int{1000, 10000, 50000, 100000} {
		entries := makeBenchEntries(size)
		b.Run(fmt.Sprintf("binary_%d", size), func(b *testing.B) {
			dir := b.TempDir()
			path := filepath.Join(dir, "commit-graph")
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := WriteBinaryCommitGraph(path, entries); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkBinaryCommitGraph_Read benchmarks reading binary commit graphs
// of various sizes.
func BenchmarkBinaryCommitGraph_Read(b *testing.B) {
	for _, size := range []int{1000, 10000, 50000, 100000} {
		entries := makeBenchEntries(size)
		dir := b.TempDir()
		path := filepath.Join(dir, "commit-graph")
		if err := WriteBinaryCommitGraph(path, entries); err != nil {
			b.Fatal(err)
		}
		b.Run(fmt.Sprintf("binary_%d", size), func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := ReadBinaryCommitGraph(path); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkJSONCommitGraph_Write benchmarks writing JSON commit graphs
// of various sizes.
func BenchmarkJSONCommitGraph_Write(b *testing.B) {
	for _, size := range []int{1000, 10000, 50000, 100000} {
		entries := makeBenchEntries(size)
		b.Run(fmt.Sprintf("json_%d", size), func(b *testing.B) {
			dir := b.TempDir()
			path := filepath.Join(dir, "commit-graph")
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				gf := commitGraphFile{Version: 1, Entries: entries}
				data, err := json.Marshal(gf)
				if err != nil {
					b.Fatal(err)
				}
				if err := os.WriteFile(path, data, 0o644); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkJSONCommitGraph_Read benchmarks reading JSON commit graphs
// of various sizes.
func BenchmarkJSONCommitGraph_Read(b *testing.B) {
	for _, size := range []int{1000, 10000, 50000, 100000} {
		entries := makeBenchEntries(size)
		dir := b.TempDir()
		path := filepath.Join(dir, "commit-graph")
		gf := commitGraphFile{Version: 1, Entries: entries}
		data, err := json.Marshal(gf)
		if err != nil {
			b.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			b.Fatal(err)
		}
		b.Run(fmt.Sprintf("json_%d", size), func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				raw, err := os.ReadFile(path)
				if err != nil {
					b.Fatal(err)
				}
				var gf commitGraphFile
				if err := json.Unmarshal(raw, &gf); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// makeBenchEntries generates n commit graph entries for benchmarking.
func makeBenchEntries(n int) map[object.Hash]*CommitGraphEntry {
	entries := make(map[object.Hash]*CommitGraphEntry, n)
	tree := makeHash(999999)
	hashes := make([]object.Hash, n)
	for i := 0; i < n; i++ {
		hashes[i] = makeHash(i)
	}
	for i := 0; i < n; i++ {
		var parents []object.Hash
		if i > 0 {
			parents = []object.Hash{hashes[i-1]}
		}
		entries[hashes[i]] = &CommitGraphEntry{
			TreeHash:   tree,
			Parents:    parents,
			Generation: uint32(i + 1),
			Timestamp:  int64(1700000000 + i),
		}
	}
	return entries
}
