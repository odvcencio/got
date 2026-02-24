package repo

import (
	"sync"

	"github.com/odvcencio/got/pkg/object"
)

// Repo represents an opened Got repository.
type Repo struct {
	RootDir string        // working directory root
	GotDir  string        // .got/ directory
	Store   *object.Store // content-addressed object store

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
