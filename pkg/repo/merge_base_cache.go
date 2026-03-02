package repo

import (
	"errors"
	"fmt"
	"sync"

	"github.com/odvcencio/graft/pkg/object"
)

type mergeBaseCacheKey struct {
	left  object.Hash
	right object.Hash
}

type mergeBaseCacheEntry struct {
	base  object.Hash
	found bool
}

type mergeBaseTraversalState struct {
	mu sync.RWMutex

	commits     map[object.Hash]*object.CommitObj
	generations map[object.Hash]uint64
	mergeBases  map[mergeBaseCacheKey]mergeBaseCacheEntry
}

func newMergeBaseTraversalState() *mergeBaseTraversalState {
	return &mergeBaseTraversalState{
		commits:     make(map[object.Hash]*object.CommitObj),
		generations: make(map[object.Hash]uint64),
		mergeBases:  make(map[mergeBaseCacheKey]mergeBaseCacheEntry),
	}
}

func canonicalMergeBaseCacheKey(a, b object.Hash) mergeBaseCacheKey {
	if a <= b {
		return mergeBaseCacheKey{left: a, right: b}
	}
	return mergeBaseCacheKey{left: b, right: a}
}

func (s *mergeBaseTraversalState) loadMergeBase(a, b object.Hash) (mergeBaseCacheEntry, bool) {
	key := canonicalMergeBaseCacheKey(a, b)
	s.mu.RLock()
	entry, ok := s.mergeBases[key]
	s.mu.RUnlock()
	return entry, ok
}

func (s *mergeBaseTraversalState) storeMergeBase(a, b, base object.Hash, found bool) {
	key := canonicalMergeBaseCacheKey(a, b)
	s.mu.Lock()
	s.mergeBases[key] = mergeBaseCacheEntry{base: base, found: found}
	s.mu.Unlock()
}

func (s *mergeBaseTraversalState) mergeBaseCacheSize() int {
	s.mu.RLock()
	n := len(s.mergeBases)
	s.mu.RUnlock()
	return n
}

// ErrShallowBoundary is returned when a commit walk encounters a shallow boundary.
var ErrShallowBoundary = fmt.Errorf("shallow boundary reached")

func (s *mergeBaseTraversalState) readCommit(r *Repo, h object.Hash) (*object.CommitObj, error) {
	s.mu.RLock()
	cached, ok := s.commits[h]
	s.mu.RUnlock()
	if ok {
		return cached, nil
	}

	// Check if this is a shallow boundary before trying to read.
	shallow, _ := r.ShallowState()
	if shallow != nil && shallow.IsShallow(h) && !r.Store.Has(h) {
		return nil, fmt.Errorf("%w: commit %s", ErrShallowBoundary, h)
	}

	commit, err := r.Store.ReadCommit(h)
	if err != nil {
		// In shallow repos, missing commits at boundaries are expected.
		if shallow != nil && shallow.IsShallow(h) {
			return nil, fmt.Errorf("%w: commit %s", ErrShallowBoundary, h)
		}
		return nil, fmt.Errorf("find merge base: read commit %s: %w", h, err)
	}

	s.mu.Lock()
	if existing, exists := s.commits[h]; exists {
		s.mu.Unlock()
		return existing, nil
	}
	s.commits[h] = commit
	s.mu.Unlock()
	return commit, nil
}

func (s *mergeBaseTraversalState) loadGeneration(h object.Hash) (uint64, bool) {
	s.mu.RLock()
	g, ok := s.generations[h]
	s.mu.RUnlock()
	return g, ok
}

func (s *mergeBaseTraversalState) storeGeneration(h object.Hash, g uint64) {
	s.mu.Lock()
	s.generations[h] = g
	s.mu.Unlock()
}

func (s *mergeBaseTraversalState) generationCacheSize() int {
	s.mu.RLock()
	n := len(s.generations)
	s.mu.RUnlock()
	return n
}

func (s *mergeBaseTraversalState) generation(r *Repo, h object.Hash) (uint64, error) {
	return s.generationRecursive(r, h, make(map[object.Hash]bool))
}

func (s *mergeBaseTraversalState) generationRecursive(r *Repo, h object.Hash, visiting map[object.Hash]bool) (uint64, error) {
	if h == "" {
		return 0, nil
	}
	if g, ok := s.loadGeneration(h); ok {
		return g, nil
	}
	if visiting[h] {
		return 0, fmt.Errorf("find merge base: commit graph cycle detected at %s", h)
	}

	visiting[h] = true
	commit, err := s.readCommit(r, h)
	if err != nil {
		delete(visiting, h)
		// Shallow boundary: treat as generation 0 (root commit).
		if errors.Is(err, ErrShallowBoundary) {
			s.storeGeneration(h, 0)
			return 0, nil
		}
		return 0, err
	}

	var maxParentGeneration uint64
	for _, p := range commit.Parents {
		pg, err := s.generationRecursive(r, p, visiting)
		if err != nil {
			// Shallow boundary on a parent: just skip this parent.
			if errors.Is(err, ErrShallowBoundary) {
				continue
			}
			delete(visiting, h)
			return 0, err
		}
		if pg > maxParentGeneration {
			maxParentGeneration = pg
		}
	}

	generation := maxParentGeneration + 1
	s.storeGeneration(h, generation)
	delete(visiting, h)
	return generation, nil
}
