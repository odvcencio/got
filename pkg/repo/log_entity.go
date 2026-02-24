package repo

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/odvcencio/got/pkg/entity"
	"github.com/odvcencio/got/pkg/object"
)

// LogEntry carries commit metadata with its hash for log output.
type LogEntry struct {
	Hash   object.Hash
	Commit *object.CommitObj
}

// LogByEntity walks first-parent history from start and returns up to limit
// commits that touched entityKey. If pathFilter is non-empty, matching is
// restricted to that path.
func (r *Repo) LogByEntity(start object.Hash, limit int, pathFilter, entityKey string) ([]LogEntry, error) {
	if limit <= 0 || start == "" || entityKey == "" {
		return nil, nil
	}

	normalizedPath := normalizeLogEntityPath(pathFilter)
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
		current = c.Parents[0]
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

	beforeEntries := map[string]TreeFileEntry{}
	if len(commit.Parents) > 0 {
		parent, err := r.Store.ReadCommit(commit.Parents[0])
		if err != nil {
			return false, fmt.Errorf("log by entity: read parent commit %s: %w", commit.Parents[0], err)
		}
		beforeEntries, err = r.treeEntriesByPath(parent.TreeHash)
		if err != nil {
			return false, err
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
