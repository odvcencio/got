// Package repo implements core graft repository operations including
// initialization, staging, commits, branching, merging, checkout, status,
// and history traversal.
package repo

import (
	"sync"

	"github.com/odvcencio/graft/pkg/object"
)

// Repo represents an opened Graft repository.
type Repo struct {
	RootDir   string        // working directory root
	GotDir    string        // .graft/ directory (worktree-specific for linked worktrees)
	CommonDir string        // shared .graft/ directory (set for linked worktrees; empty for main)
	Store     *object.Store // content-addressed object store

	mergeTraversalStateOnce sync.Once
	mergeTraversalState     *mergeBaseTraversalState

	statusHashCacheMu sync.Mutex
	statusHashCache   map[string]statusFileHashCacheEntry
	statusBlobHasher  func([]byte) object.Hash
}

func (r *Repo) getMergeTraversalState() *mergeBaseTraversalState {
	r.mergeTraversalStateOnce.Do(func() {
		r.mergeTraversalState = newMergeBaseTraversalState()
	})
	return r.mergeTraversalState
}
