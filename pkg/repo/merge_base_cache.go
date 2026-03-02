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

// generationFrame represents one stack frame in the iterative generation
// number computation. It mirrors what the old recursive approach stored
// implicitly on the call stack: the commit hash, its loaded parents, the
// index of the next parent to process, and the running maximum parent
// generation seen so far.
type generationFrame struct {
	hash              object.Hash
	parents           []object.Hash
	nextParent        int
	maxParentGeneration uint64
}

func (s *mergeBaseTraversalState) generation(r *Repo, h object.Hash) (uint64, error) {
	if h == "" {
		return 0, nil
	}
	if g, ok := s.loadGeneration(h); ok {
		return g, nil
	}

	// visiting tracks commits currently on the stack to detect cycles.
	visiting := make(map[object.Hash]bool)
	stack := []generationFrame{}

	// pushCommit loads a commit and pushes a new frame onto the stack.
	// It returns (generation, true, nil) if the result is already known
	// (cached or shallow boundary), meaning no frame was pushed.
	// It returns (0, false, nil) if a frame was pushed and processing
	// should continue. It returns (0, false, err) on hard errors.
	pushCommit := func(ch object.Hash) (uint64, bool, error) {
		if ch == "" {
			return 0, true, nil
		}
		if g, ok := s.loadGeneration(ch); ok {
			return g, true, nil
		}
		if visiting[ch] {
			return 0, false, fmt.Errorf("find merge base: commit graph cycle detected at %s", ch)
		}

		commit, err := s.readCommit(r, ch)
		if err != nil {
			if errors.Is(err, ErrShallowBoundary) {
				s.storeGeneration(ch, 0)
				return 0, true, nil
			}
			return 0, false, err
		}

		visiting[ch] = true
		stack = append(stack, generationFrame{
			hash:    ch,
			parents: commit.Parents,
		})
		return 0, false, nil
	}

	// Push the initial commit.
	if g, done, err := pushCommit(h); err != nil {
		return 0, err
	} else if done {
		return g, nil
	}

	for len(stack) > 0 {
		frame := &stack[len(stack)-1]

		// Process the next unvisited parent.
		if frame.nextParent < len(frame.parents) {
			p := frame.parents[frame.nextParent]
			frame.nextParent++

			pg, done, err := pushCommit(p)
			if err != nil {
				// Shallow boundary on a parent: skip this parent.
				if errors.Is(err, ErrShallowBoundary) {
					continue
				}
				return 0, err
			}
			if done {
				// Parent generation already known; incorporate it.
				if pg > frame.maxParentGeneration {
					frame.maxParentGeneration = pg
				}
				continue
			}
			// A new frame was pushed for this parent; loop back to
			// process it before continuing with remaining parents.
			continue
		}

		// All parents have been processed. Compute and cache the
		// generation number for this commit, then pop the frame.
		gen := frame.maxParentGeneration + 1
		s.storeGeneration(frame.hash, gen)
		delete(visiting, frame.hash)
		stack = stack[:len(stack)-1]

		// Propagate the computed generation to the parent frame.
		if len(stack) > 0 {
			parent := &stack[len(stack)-1]
			if gen > parent.maxParentGeneration {
				parent.maxParentGeneration = gen
			}
		}
	}

	// The generation for h was stored during processing.
	if g, ok := s.loadGeneration(h); ok {
		return g, nil
	}
	// Should not happen: h was pushed and processed.
	return 0, fmt.Errorf("find merge base: generation not computed for %s", h)
}
