package repo

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/odvcencio/graft/pkg/object"
)

// CommitGraphEntry stores precomputed metadata for a single commit.
type CommitGraphEntry struct {
	TreeHash   object.Hash   `json:"tree"`
	Parents    []object.Hash `json:"parents"`
	Generation uint32        `json:"generation"`
	Timestamp  int64         `json:"timestamp"`
}

// CommitGraph is an in-memory commit graph mapping commit hashes to their
// precomputed metadata. It is persisted as a JSON file at
// .graft/objects/info/commit-graph.
type CommitGraph struct {
	Entries map[object.Hash]*CommitGraphEntry `json:"entries"`
}

// commitGraphFile is the on-disk JSON representation.
type commitGraphFile struct {
	Version int                               `json:"version"`
	Entries map[object.Hash]*CommitGraphEntry `json:"entries"`
}

// commitGraphPath returns the path to the commit-graph file.
func (r *Repo) commitGraphPath() string {
	return filepath.Join(r.GraftDir, "objects", "info", "commit-graph")
}

// WriteCommitGraph computes and writes the commit-graph by walking all
// reachable commits from every ref tip. It writes the graph as a JSON
// file at .graft/objects/info/commit-graph.
func (r *Repo) WriteCommitGraph() error {
	refs, err := r.ListRefs("")
	if err != nil {
		return fmt.Errorf("write commit graph: list refs: %w", err)
	}

	// Collect unique ref tip hashes.
	tips := make(map[object.Hash]struct{}, len(refs))
	for _, h := range refs {
		if h != "" {
			tips[h] = struct{}{}
		}
	}

	// Walk all reachable commits using BFS and compute generation numbers.
	entries := make(map[object.Hash]*CommitGraphEntry)

	// We need to compute generation numbers which depend on parent
	// generations, so we walk iteratively. Use a stack-based approach:
	// first collect all reachable commits, then compute generations
	// bottom-up.
	type stackFrame struct {
		hash object.Hash
	}

	visited := make(map[object.Hash]bool)
	var order []object.Hash // topological order (parents before children)

	// BFS to discover all reachable commits.
	queue := make([]object.Hash, 0, len(tips))
	for h := range tips {
		if !visited[h] {
			visited[h] = true
			queue = append(queue, h)
		}
	}

	for len(queue) > 0 {
		h := queue[0]
		queue = queue[1:]
		order = append(order, h)

		commit, err := r.Store.ReadCommit(h)
		if err != nil {
			// Skip commits we can't read (e.g., dangling refs).
			continue
		}

		entries[h] = &CommitGraphEntry{
			TreeHash:  commit.TreeHash,
			Parents:   commit.Parents,
			Timestamp: commit.Timestamp,
			// Generation will be computed in a second pass.
		}

		for _, p := range commit.Parents {
			if p != "" && !visited[p] {
				visited[p] = true
				queue = append(queue, p)
			}
		}
	}

	// Compute generation numbers. Root commits (no parents) have
	// generation 1. A commit's generation is max(parent generations) + 1.
	// Process in reverse BFS order (parents tend to appear later in BFS
	// from tips, so reverse gives us parents-first).
	generations := make(map[object.Hash]uint32, len(entries))

	// Use recursive computation with memoization since BFS order from
	// tips is children-first, not parents-first.
	var computeGen func(h object.Hash) uint32
	computeGen = func(h object.Hash) uint32 {
		if g, ok := generations[h]; ok {
			return g
		}
		entry, ok := entries[h]
		if !ok {
			return 0
		}
		var maxParentGen uint32
		for _, p := range entry.Parents {
			pg := computeGen(p)
			if pg > maxParentGen {
				maxParentGen = pg
			}
		}
		g := maxParentGen + 1
		generations[h] = g
		return g
	}

	for h := range entries {
		g := computeGen(h)
		entries[h].Generation = g
	}

	// Write the graph file.
	graphFile := commitGraphFile{
		Version: 1,
		Entries: entries,
	}

	data, err := json.MarshalIndent(graphFile, "", "  ")
	if err != nil {
		return fmt.Errorf("write commit graph: marshal: %w", err)
	}

	path := r.commitGraphPath()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("write commit graph: mkdir: %w", err)
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write commit graph: write file: %w", err)
	}

	return nil
}

// ReadCommitGraph loads the commit-graph file. Returns an empty graph if
// the file does not exist.
func (r *Repo) ReadCommitGraph() (*CommitGraph, error) {
	path := r.commitGraphPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &CommitGraph{
				Entries: make(map[object.Hash]*CommitGraphEntry),
			}, nil
		}
		return nil, fmt.Errorf("read commit graph: %w", err)
	}

	var gf commitGraphFile
	if err := json.Unmarshal(data, &gf); err != nil {
		return nil, fmt.Errorf("read commit graph: unmarshal: %w", err)
	}

	if gf.Entries == nil {
		gf.Entries = make(map[object.Hash]*CommitGraphEntry)
	}

	return &CommitGraph{Entries: gf.Entries}, nil
}

// Lookup returns the entry for a commit hash, or nil if not in the graph.
func (g *CommitGraph) Lookup(h object.Hash) *CommitGraphEntry {
	if g == nil || g.Entries == nil {
		return nil
	}
	return g.Entries[h]
}

// Generation returns the generation number for a commit hash, or 0 if
// the commit is not in the graph.
func (g *CommitGraph) Generation(h object.Hash) uint32 {
	entry := g.Lookup(h)
	if entry == nil {
		return 0
	}
	return entry.Generation
}
