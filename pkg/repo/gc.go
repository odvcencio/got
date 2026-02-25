package repo

import (
	"sort"
	"strings"

	"github.com/odvcencio/got/pkg/object"
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

	return r.Store.GCReachable(roots)
}
