package repo

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/odvcencio/graft/pkg/entity"
	"github.com/odvcencio/graft/pkg/object"
)

// LogEntry carries commit metadata with its hash for log output.
type LogEntry struct {
	Hash   object.Hash
	Commit *object.CommitObj
}

// LogAll walks the commit history from all branches and tags, collecting
// up to limit unique commits sorted by timestamp (newest first). Each
// branch/tag tip is walked independently; commits reachable from multiple
// refs are deduplicated. In a shallow repository, walking stops at shallow
// boundaries.
func (r *Repo) LogAll(limit int) ([]LogEntry, error) {
	if limit <= 0 {
		return nil, nil
	}

	// Collect all ref tips: branches + tags.
	branchRefs, err := r.ListRefs("heads")
	if err != nil {
		return nil, fmt.Errorf("log all: list branches: %w", err)
	}
	tagRefs, err := r.ListRefs("tags")
	if err != nil {
		return nil, fmt.Errorf("log all: list tags: %w", err)
	}

	seen := make(map[object.Hash]struct{})
	var all []LogEntry

	shallow, _ := r.ShallowState()

	// Walk from each ref tip collecting all reachable commits.
	for _, refs := range []map[string]object.Hash{branchRefs, tagRefs} {
		for _, tip := range refs {
			current := tip
			for current != "" {
				if _, dup := seen[current]; dup {
					break
				}

				c, err := r.Store.ReadCommit(current)
				if err != nil {
					if errors.Is(err, os.ErrNotExist) {
						break
					}
					return nil, fmt.Errorf("log all: read commit %s: %w", current, err)
				}

				seen[current] = struct{}{}
				all = append(all, LogEntry{Hash: current, Commit: c})

				// Follow first parent.
				if len(c.Parents) == 0 {
					break
				}

				// Also enqueue non-first parents for walking.
				for _, p := range c.Parents[1:] {
					if _, dup := seen[p]; !dup {
						if shallow != nil && shallow.IsShallow(p) {
							continue
						}
						// Walk secondary parents iteratively via a stack.
						stack := []object.Hash{p}
						for len(stack) > 0 {
							top := stack[len(stack)-1]
							stack = stack[:len(stack)-1]

							if _, dup := seen[top]; dup {
								continue
							}

							pc, err := r.Store.ReadCommit(top)
							if err != nil {
								if errors.Is(err, os.ErrNotExist) {
									continue
								}
								return nil, fmt.Errorf("log all: read commit %s: %w", top, err)
							}

							seen[top] = struct{}{}
							all = append(all, LogEntry{Hash: top, Commit: pc})

							for _, pp := range pc.Parents {
								if _, dup := seen[pp]; !dup {
									if shallow == nil || !shallow.IsShallow(pp) {
										stack = append(stack, pp)
									}
								}
							}
						}
					}
				}

				next := c.Parents[0]
				if shallow != nil && shallow.IsShallow(next) {
					break
				}
				current = next
			}
		}
	}

	// Sort by timestamp descending (newest first), break ties by hash.
	sort.Slice(all, func(i, j int) bool {
		if all[i].Commit.Timestamp != all[j].Commit.Timestamp {
			return all[i].Commit.Timestamp > all[j].Commit.Timestamp
		}
		return all[i].Hash < all[j].Hash
	})

	// Apply limit.
	if len(all) > limit {
		all = all[:limit]
	}

	return all, nil
}

// LogByEntity walks first-parent history from start and returns up to limit
// commits that touched entityKey. If pathFilter is non-empty, matching is
// restricted to that path. In a shallow repository, walking stops at shallow
// boundaries.
func (r *Repo) LogByEntity(start object.Hash, limit int, pathFilter, entityKey string) ([]LogEntry, error) {
	if limit <= 0 || start == "" || entityKey == "" {
		return nil, nil
	}

	normalizedPath := normalizeLogEntityPath(pathFilter)
	if normalizedPath != "" {
		return r.logByEntityTrackedPath(start, limit, normalizedPath, entityKey)
	}

	shallow, _ := r.ShallowState()

	results := make([]LogEntry, 0, limit)
	current := start

	for current != "" && len(results) < limit {
		c, err := r.Store.ReadCommit(current)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				break
			}
			return nil, fmt.Errorf("log by entity: read commit %s: %w", current, err)
		}

		matches, err := r.commitTouchesEntity(c, normalizedPath, entityKey)
		if err != nil {
			return nil, err
		}
		if matches {
			results = append(results, LogEntry{Hash: current, Commit: c})
		}

		if len(c.Parents) == 0 {
			break
		}
		next := c.Parents[0]
		if shallow != nil && shallow.IsShallow(next) {
			break
		}
		current = next
	}

	return results, nil
}

func (r *Repo) logByEntityTrackedPath(start object.Hash, limit int, relPath, entityKey string) ([]LogEntry, error) {
	shallowState, _ := r.ShallowState()

	results := make([]LogEntry, 0, limit)
	currentHash := start
	locator := entityLocator{Path: relPath, Key: entityKey}

	for currentHash != "" && len(results) < limit {
		commit, err := r.Store.ReadCommit(currentHash)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				break
			}
			return nil, fmt.Errorf("log by entity: read commit %s: %w", currentHash, err)
		}

		afterEntries, err := r.treeEntriesByPath(commit.TreeHash)
		if err != nil {
			return nil, err
		}
		afterCache := newCommitEntityCache(afterEntries)

		currentEntity, inCurrent, err := r.findEntityByLocator(afterCache, currentHash, locator)
		if err != nil {
			if isEntityExtractionError(err) {
				parentHash := firstParentHash(commit)
				if parentHash == "" {
					break
				}
				currentHash = parentHash
				continue
			}
			return nil, fmt.Errorf("log by entity: %w", err)
		}

		parentHash := firstParentHash(commit)
		touched := false
		nextLocator := locator

		// Treat a shallow boundary parent the same as no parent.
		parentIsShallow := parentHash != "" && shallowState != nil && shallowState.IsShallow(parentHash)

		if parentHash == "" || parentIsShallow {
			touched = inCurrent
		} else {
			parentCommit, err := r.Store.ReadCommit(parentHash)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) && shallowState != nil {
					// Parent missing in a shallow repo — treat as boundary.
					touched = inCurrent
				} else {
					return nil, fmt.Errorf("log by entity: read parent commit %s: %w", parentHash, err)
				}
			}
			if err == nil {
				beforeEntries, err := r.treeEntriesByPath(parentCommit.TreeHash)
				if err != nil {
					return nil, err
				}
				beforeCache := newCommitEntityCache(beforeEntries)

				if inCurrent {
					parentEntity, inParent, err := r.resolveParentEntity(
						currentEntity,
						parentHash,
						beforeCache,
						changedCandidatePaths(beforeEntries, afterEntries, ""),
					)
					if err != nil {
						if isEntityExtractionError(err) {
							currentHash = parentHash
							continue
						}
						return nil, fmt.Errorf("log by entity: %w", err)
					}
					if !inParent || parentEntity.BodyHash != currentEntity.BodyHash {
						touched = true
					}
					if inParent {
						nextLocator = parentEntity.Locator
					}
				} else {
					parentEntity, inParent, err := r.findEntityByLocator(beforeCache, parentHash, locator)
					if err != nil {
						if isEntityExtractionError(err) {
							currentHash = parentHash
							continue
						}
						return nil, fmt.Errorf("log by entity: %w", err)
					}
					if inParent {
						touched = true
						nextLocator = parentEntity.Locator
					}
				}
			}
		}

		if touched {
			results = append(results, LogEntry{Hash: currentHash, Commit: commit})
		}
		locator = nextLocator

		if parentHash == "" || parentIsShallow {
			break
		}
		currentHash = parentHash
	}

	return results, nil
}

func normalizeLogEntityPath(path string) string {
	if path == "" {
		return ""
	}
	cleaned := filepath.ToSlash(filepath.Clean(path))
	if cleaned == "." {
		return ""
	}
	return cleaned
}

func (r *Repo) commitTouchesEntity(commit *object.CommitObj, pathFilter, entityKey string) (bool, error) {
	afterEntries, err := r.treeEntriesByPath(commit.TreeHash)
	if err != nil {
		return false, err
	}

	shallow, _ := r.ShallowState()
	beforeEntries := map[string]TreeFileEntry{}
	if len(commit.Parents) > 0 {
		parentHash := commit.Parents[0]
		isShallowParent := shallow != nil && shallow.IsShallow(parentHash)
		if !isShallowParent {
			parent, err := r.Store.ReadCommit(parentHash)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) && shallow != nil {
					// Missing parent in shallow repo — treat as root.
				} else {
					return false, fmt.Errorf("log by entity: read parent commit %s: %w", parentHash, err)
				}
			} else {
				beforeEntries, err = r.treeEntriesByPath(parent.TreeHash)
				if err != nil {
					return false, err
				}
			}
		}
	}

	paths := changedCandidatePaths(beforeEntries, afterEntries, pathFilter)
	if len(paths) == 0 {
		return false, nil
	}

	matches := false
	for _, p := range paths {
		before, hasBefore := beforeEntries[p]
		after, hasAfter := afterEntries[p]
		if hasBefore && hasAfter && before.BlobHash == after.BlobHash {
			continue
		}

		// If neither side has entity metadata, this path cannot contribute.
		if before.EntityListHash == "" && after.EntityListHash == "" {
			continue
		}

		beforeData, err := r.readBlobForEntry(before, hasBefore, p)
		if err != nil {
			return false, err
		}
		afterData, err := r.readBlobForEntry(after, hasAfter, p)
		if err != nil {
			return false, err
		}

		beforeList, err := entity.Extract(p, beforeData)
		if err != nil {
			// Safe behavior: if extraction fails, skip this commit.
			return false, nil
		}
		afterList, err := entity.Extract(p, afterData)
		if err != nil {
			return false, nil
		}

		if entityChangedForKey(beforeList, afterList, entityKey) {
			matches = true
		}
	}

	return matches, nil
}

func (r *Repo) treeEntriesByPath(treeHash object.Hash) (map[string]TreeFileEntry, error) {
	entries, err := r.FlattenTree(treeHash)
	if err != nil {
		return nil, fmt.Errorf("log by entity: flatten tree %s: %w", treeHash, err)
	}
	result := make(map[string]TreeFileEntry, len(entries))
	for _, entry := range entries {
		result[entry.Path] = entry
	}
	return result, nil
}

func changedCandidatePaths(beforeEntries, afterEntries map[string]TreeFileEntry, pathFilter string) []string {
	if pathFilter != "" {
		before, hasBefore := beforeEntries[pathFilter]
		after, hasAfter := afterEntries[pathFilter]
		if !hasBefore && !hasAfter {
			return nil
		}
		if hasBefore && hasAfter && before.BlobHash == after.BlobHash {
			return nil
		}
		return []string{pathFilter}
	}

	paths := make([]string, 0, len(beforeEntries)+len(afterEntries))
	seen := make(map[string]struct{}, len(beforeEntries)+len(afterEntries))
	for p := range beforeEntries {
		seen[p] = struct{}{}
	}
	for p := range afterEntries {
		seen[p] = struct{}{}
	}

	for p := range seen {
		before, hasBefore := beforeEntries[p]
		after, hasAfter := afterEntries[p]
		if hasBefore && hasAfter && before.BlobHash == after.BlobHash {
			continue
		}
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return paths
}

func (r *Repo) readBlobForEntry(entry TreeFileEntry, exists bool, path string) ([]byte, error) {
	if !exists {
		return []byte{}, nil
	}
	blob, err := r.Store.ReadBlob(entry.BlobHash)
	if err != nil {
		return nil, fmt.Errorf("log by entity: read blob %s (%s): %w", entry.BlobHash, path, err)
	}
	return blob.Data, nil
}

func entityChangedForKey(beforeList, afterList *entity.EntityList, key string) bool {
	beforeMap := entity.BuildEntityMap(beforeList)
	afterMap := entity.BuildEntityMap(afterList)

	beforeEnt, inBefore := beforeMap[key]
	afterEnt, inAfter := afterMap[key]

	if !inBefore && !inAfter {
		return false
	}
	if !inBefore || !inAfter {
		return true
	}
	return beforeEnt.BodyHash != afterEnt.BodyHash
}
