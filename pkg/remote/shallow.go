package remote

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/odvcencio/graft/pkg/object"
)

// ShallowState tracks shallow commit boundaries for a repository.
type ShallowState struct {
	Commits map[object.Hash]bool // set of shallow boundary commit hashes
}

// NewShallowState creates an empty ShallowState.
func NewShallowState() *ShallowState {
	return &ShallowState{Commits: make(map[object.Hash]bool)}
}

// ReadShallowFile reads the shallow file from the given graft directory.
// The file contains one hash per line. If the file does not exist, an
// empty state is returned without error.
func ReadShallowFile(graftDir string) (*ShallowState, error) {
	p := filepath.Join(graftDir, "shallow")
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return NewShallowState(), nil
		}
		return nil, fmt.Errorf("read shallow file: %w", err)
	}
	state := NewShallowState()
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		h := object.Hash(line)
		if err := ValidateHash(h); err != nil {
			return nil, fmt.Errorf("invalid hash in shallow file: %w", err)
		}
		state.Commits[h] = true
	}
	return state, nil
}

// WriteShallowFile writes the shallow state to the graft directory.
// Hashes are written one per line in sorted order.
func WriteShallowFile(graftDir string, state *ShallowState) error {
	hashes := state.List()
	lines := make([]string, len(hashes))
	for i, h := range hashes {
		lines[i] = string(h)
	}
	content := ""
	if len(lines) > 0 {
		content = strings.Join(lines, "\n") + "\n"
	}
	p := filepath.Join(graftDir, "shallow")
	return os.WriteFile(p, []byte(content), 0o644)
}

// IsShallow returns true if the given commit hash is a shallow boundary.
func (s *ShallowState) IsShallow(hash object.Hash) bool {
	return s.Commits[hash]
}

// Add marks a commit hash as a shallow boundary.
func (s *ShallowState) Add(hash object.Hash) {
	s.Commits[hash] = true
}

// Remove removes a commit hash from the shallow boundaries.
func (s *ShallowState) Remove(hash object.Hash) {
	delete(s.Commits, hash)
}

// List returns a sorted slice of all shallow boundary hashes.
func (s *ShallowState) List() []object.Hash {
	hashes := make([]object.Hash, 0, len(s.Commits))
	for h := range s.Commits {
		hashes = append(hashes, h)
	}
	sort.Slice(hashes, func(i, j int) bool {
		return hashes[i] < hashes[j]
	})
	return hashes
}

// Len returns the number of shallow boundaries.
func (s *ShallowState) Len() int {
	return len(s.Commits)
}

// ObjectFilter represents a partial clone filter specification.
type ObjectFilter struct {
	Type      string // "blob:none", "blob:limit=<n>", "tree:<depth>"
	BlobLimit int64  // for blob:limit, the max size in bytes
	TreeDepth int    // for tree:<depth>, the max depth
}

// ParseObjectFilter parses a filter spec string such as "blob:none",
// "blob:limit=1048576", or "tree:0".
func ParseObjectFilter(spec string) (*ObjectFilter, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, fmt.Errorf("empty filter spec")
	}

	switch {
	case spec == "blob:none":
		return &ObjectFilter{Type: "blob:none"}, nil

	case strings.HasPrefix(spec, "blob:limit="):
		valStr := strings.TrimPrefix(spec, "blob:limit=")
		limit, err := strconv.ParseInt(valStr, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid blob limit %q: %w", valStr, err)
		}
		if limit < 0 {
			return nil, fmt.Errorf("blob limit must be non-negative, got %d", limit)
		}
		return &ObjectFilter{
			Type:      "blob:limit",
			BlobLimit: limit,
		}, nil

	case strings.HasPrefix(spec, "tree:"):
		valStr := strings.TrimPrefix(spec, "tree:")
		depth, err := strconv.Atoi(valStr)
		if err != nil {
			return nil, fmt.Errorf("invalid tree depth %q: %w", valStr, err)
		}
		if depth < 0 {
			return nil, fmt.Errorf("tree depth must be non-negative, got %d", depth)
		}
		return &ObjectFilter{
			Type:      "tree",
			TreeDepth: depth,
		}, nil

	default:
		return nil, fmt.Errorf("unknown filter spec: %q", spec)
	}
}

// String renders the filter back to its spec string representation.
func (f *ObjectFilter) String() string {
	switch f.Type {
	case "blob:none":
		return "blob:none"
	case "blob:limit":
		return fmt.Sprintf("blob:limit=%d", f.BlobLimit)
	case "tree":
		return fmt.Sprintf("tree:%d", f.TreeDepth)
	default:
		return f.Type
	}
}

// AllowsBlob reports whether a blob of the given size passes the filter.
// For "blob:none", no blobs pass (returns false).
// For "blob:limit", only blobs strictly under the limit pass.
// For "tree" filters, all blobs pass (returns true).
func (f *ObjectFilter) AllowsBlob(size int64) bool {
	switch f.Type {
	case "blob:none":
		return false
	case "blob:limit":
		return size < f.BlobLimit
	default:
		// tree filters do not restrict blobs
		return true
	}
}
