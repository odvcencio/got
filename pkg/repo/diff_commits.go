package repo

import (
	"fmt"
	"sort"

	"github.com/odvcencio/graft/pkg/object"
)

// CommitDiffFile describes a single file that differs between two commits.
type CommitDiffFile struct {
	Path        string
	Status      string // "added", "modified", "deleted"
	OldBlobHash object.Hash
	NewBlobHash object.Hash
}

// CommitDiffReport is the result of comparing two commits.
type CommitDiffReport struct {
	OldCommit     object.Hash
	NewCommit     object.Hash
	Files         []CommitDiffFile
	EntityChanges []ReflogEntityChange
}

// DiffCommits compares two commits and returns the set of file-level and
// entity-level changes between them.
func (r *Repo) DiffCommits(oldCommit, newCommit object.Hash) (*CommitDiffReport, error) {
	// Read old commit tree.
	oldCommitObj, err := r.Store.ReadCommit(oldCommit)
	if err != nil {
		return nil, fmt.Errorf("DiffCommits: read old commit %s: %w", oldCommit, err)
	}
	oldEntries, err := r.FlattenTree(oldCommitObj.TreeHash)
	if err != nil {
		return nil, fmt.Errorf("DiffCommits: flatten old tree: %w", err)
	}

	// Read new commit tree.
	newCommitObj, err := r.Store.ReadCommit(newCommit)
	if err != nil {
		return nil, fmt.Errorf("DiffCommits: read new commit %s: %w", newCommit, err)
	}
	newEntries, err := r.FlattenTree(newCommitObj.TreeHash)
	if err != nil {
		return nil, fmt.Errorf("DiffCommits: flatten new tree: %w", err)
	}

	// Build path maps.
	oldByPath := make(map[string]TreeFileEntry, len(oldEntries))
	for _, e := range oldEntries {
		oldByPath[e.Path] = e
	}
	newByPath := make(map[string]TreeFileEntry, len(newEntries))
	for _, e := range newEntries {
		newByPath[e.Path] = e
	}

	// Collect all unique paths.
	allPaths := make(map[string]struct{})
	for p := range oldByPath {
		allPaths[p] = struct{}{}
	}
	for p := range newByPath {
		allPaths[p] = struct{}{}
	}

	sortedPaths := make([]string, 0, len(allPaths))
	for p := range allPaths {
		sortedPaths = append(sortedPaths, p)
	}
	sort.Strings(sortedPaths)

	// Compare blob hashes to find added/modified/deleted files.
	var files []CommitDiffFile
	for _, p := range sortedPaths {
		oldEntry, inOld := oldByPath[p]
		newEntry, inNew := newByPath[p]

		switch {
		case inOld && inNew:
			if oldEntry.BlobHash != newEntry.BlobHash {
				files = append(files, CommitDiffFile{
					Path:        p,
					Status:      "modified",
					OldBlobHash: oldEntry.BlobHash,
					NewBlobHash: newEntry.BlobHash,
				})
			}
		case !inOld && inNew:
			files = append(files, CommitDiffFile{
				Path:        p,
				Status:      "added",
				NewBlobHash: newEntry.BlobHash,
			})
		case inOld && !inNew:
			files = append(files, CommitDiffFile{
				Path:        p,
				Status:      "deleted",
				OldBlobHash: oldEntry.BlobHash,
			})
		}
	}

	// Compute entity-level changes.
	entityChanges, err := DiffTreeEntities(r, oldCommit, newCommit)
	if err != nil {
		// Entity diffing is best-effort; include file diffs even if entity
		// diffing fails (e.g., no entity support for file types).
		entityChanges = nil
	}

	return &CommitDiffReport{
		OldCommit:     oldCommit,
		NewCommit:     newCommit,
		Files:         files,
		EntityChanges: entityChanges,
	}, nil
}

// DiffRefs resolves two ref names and delegates to DiffCommits.
func (r *Repo) DiffRefs(ref1, ref2 string) (*CommitDiffReport, error) {
	h1, err := r.ResolveRef(ref1)
	if err != nil {
		return nil, fmt.Errorf("DiffRefs: resolve %q: %w", ref1, err)
	}
	h2, err := r.ResolveRef(ref2)
	if err != nil {
		return nil, fmt.Errorf("DiffRefs: resolve %q: %w", ref2, err)
	}
	return r.DiffCommits(h1, h2)
}
