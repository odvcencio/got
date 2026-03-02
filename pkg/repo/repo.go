// Package repo implements core graft repository operations including
// initialization, staging, commits, branching, merging, checkout, status,
// and history traversal.
package repo

import (
	"sync"

	"github.com/odvcencio/graft/pkg/object"
	"github.com/odvcencio/graft/pkg/remote"
)

// Repo represents an opened Graft repository.
type Repo struct {
	RootDir   string        // working directory root
	GraftDir  string        // .graft/ directory (worktree-specific for linked worktrees)
	CommonDir string        // shared .graft/ directory (set for linked worktrees; empty for main)
	Store     *object.Store // content-addressed object store

	mergeTraversalStateOnce sync.Once
	mergeTraversalState     *mergeBaseTraversalState

	statusHashCacheMu sync.Mutex
	statusHashCache   map[string]statusFileHashCacheEntry
	statusBlobHasher  func([]byte) object.Hash

	shallowOnce  sync.Once
	shallowState *remote.ShallowState
	shallowErr   error
}

func (r *Repo) getMergeTraversalState() *mergeBaseTraversalState {
	r.mergeTraversalStateOnce.Do(func() {
		r.mergeTraversalState = newMergeBaseTraversalState()
	})
	return r.mergeTraversalState
}

// InvalidateMergeBaseCache clears cached merge base results. This should
// be called after operations that add new commits or move refs (e.g.
// Commit, UpdateRef, fetch) since those changes can make previously
// cached merge base answers stale. Content-addressed caches (commit
// objects, generation numbers) are preserved because they are immutable.
func (r *Repo) InvalidateMergeBaseCache() {
	if r.mergeTraversalState != nil {
		r.mergeTraversalState.invalidate()
	}
}

// ShallowState returns the shallow boundary state for this repository.
// The result is cached after the first call. If .graft/shallow does not
// exist, an empty state is returned without error.
func (r *Repo) ShallowState() (*remote.ShallowState, error) {
	r.shallowOnce.Do(func() {
		r.shallowState, r.shallowErr = remote.ReadShallowFile(r.GraftDir)
	})
	return r.shallowState, r.shallowErr
}

// IsShallowRepository returns true if this repository has shallow boundaries.
func (r *Repo) IsShallowRepository() bool {
	state, err := r.ShallowState()
	if err != nil {
		return false
	}
	return state.Len() > 0
}
