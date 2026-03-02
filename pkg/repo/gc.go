package repo

import (
	"fmt"
	"sort"
	"strings"

	"github.com/odvcencio/graft/pkg/object"
)

// GC packs loose objects reachable from refs.
func (r *Repo) GC() (*object.GCSummary, error) {
	refs, err := r.ListRefs("")
	if err != nil {
		return nil, err
	}

	rootSet := make(map[object.Hash]struct{}, len(refs))
	for _, h := range refs {
		h = object.Hash(strings.TrimSpace(string(h)))
		if h == "" {
			continue
		}
		rootSet[h] = struct{}{}
	}

	roots := make([]object.Hash, 0, len(rootSet))
	for h := range rootSet {
		roots = append(roots, h)
	}
	sort.Slice(roots, func(i, j int) bool { return roots[i] < roots[j] })

	summary, err := r.Store.GCReachable(roots)
	if err != nil {
		return nil, err
	}

	// Rebuild the commit-graph after packing so that generation numbers
	// and precomputed metadata stay up-to-date.
	if err := r.WriteCommitGraph(); err != nil {
		return summary, fmt.Errorf("gc: write commit graph: %w", err)
	}

	return summary, nil
}
