package repo

import (
	"sort"

	"github.com/odvcencio/graft/pkg/object"
)

// ModuleMergeResult holds the outcome of a three-way merge on module entries.
type ModuleMergeResult struct {
	Resolved     map[string]object.Hash // path -> resolved commit hash
	Removed      []string               // paths removed
	Conflicts    []ModuleMergeConflict
	HasConflicts bool
}

// ModuleMergeConflict records a module entry that could not be auto-resolved.
type ModuleMergeConflict struct {
	Path         string
	OursCommit   object.Hash
	TheirsCommit object.Hash
	Reason       string
}

// mergeModuleEntries performs a three-way merge on module (gitlink) entries.
//
// The merge rules mirror standard three-way merge semantics adapted for
// opaque commit-hash pointers:
//
//   - Unchanged across all three sides: keep as-is.
//   - Changed on only one side: take that side's value.
//   - Both changed to the same value: take it.
//   - Both changed to different values: compare generation numbers;
//     the newer (higher generation) commit wins. On error, conflict
//     (default to ours).
//   - Added in both sides with same hash: take it.
//   - Added in both sides with different hashes: newer wins.
//   - Added in only one side: take it.
//   - Both deleted: remove.
//   - Deleted by one side, other unchanged from base: remove.
//   - Deleted by one side, other changed from base: keep the changed side.
func (r *Repo) mergeModuleEntries(
	baseMap, oursMap, theirsMap map[string]TreeModuleEntry,
) (*ModuleMergeResult, error) {
	result := &ModuleMergeResult{
		Resolved: make(map[string]object.Hash),
	}

	paths := collectModulePaths(baseMap, oursMap, theirsMap)

	for _, p := range paths {
		base, inBase := baseMap[p]
		ours, inOurs := oursMap[p]
		theirs, inTheirs := theirsMap[p]

		switch {
		// All three present.
		case inBase && inOurs && inTheirs:
			if base.BlobHash == ours.BlobHash && base.BlobHash == theirs.BlobHash {
				// All same — unchanged.
				result.Resolved[p] = ours.BlobHash
			} else if ours.BlobHash == theirs.BlobHash {
				// Both changed to same value.
				result.Resolved[p] = ours.BlobHash
			} else if ours.BlobHash == base.BlobHash {
				// Only theirs changed.
				result.Resolved[p] = theirs.BlobHash
			} else if theirs.BlobHash == base.BlobHash {
				// Only ours changed.
				result.Resolved[p] = ours.BlobHash
			} else {
				// Both changed to different values — compare generations.
				winner, err := r.newerCommit(ours.BlobHash, theirs.BlobHash)
				if err != nil {
					// On error, conflict — default to ours.
					result.Conflicts = append(result.Conflicts, ModuleMergeConflict{
						Path:         p,
						OursCommit:   ours.BlobHash,
						TheirsCommit: theirs.BlobHash,
						Reason:       "both sides changed to different commits: " + err.Error(),
					})
					result.HasConflicts = true
					result.Resolved[p] = ours.BlobHash
				} else {
					result.Resolved[p] = winner
				}
			}

		// Not in base, added in both.
		case !inBase && inOurs && inTheirs:
			if ours.BlobHash == theirs.BlobHash {
				result.Resolved[p] = ours.BlobHash
			} else {
				winner, err := r.newerCommit(ours.BlobHash, theirs.BlobHash)
				if err != nil {
					result.Conflicts = append(result.Conflicts, ModuleMergeConflict{
						Path:         p,
						OursCommit:   ours.BlobHash,
						TheirsCommit: theirs.BlobHash,
						Reason:       "added in both sides with different commits: " + err.Error(),
					})
					result.HasConflicts = true
					result.Resolved[p] = ours.BlobHash
				} else {
					result.Resolved[p] = winner
				}
			}

		// Added in ours only.
		case !inBase && inOurs && !inTheirs:
			result.Resolved[p] = ours.BlobHash

		// Added in theirs only.
		case !inBase && !inOurs && inTheirs:
			result.Resolved[p] = theirs.BlobHash

		// Both deleted (was in base, gone from both sides).
		case inBase && !inOurs && !inTheirs:
			result.Removed = append(result.Removed, p)

		// Deleted by theirs.
		case inBase && inOurs && !inTheirs:
			if ours.BlobHash == base.BlobHash {
				// Ours unchanged, theirs deleted — remove.
				result.Removed = append(result.Removed, p)
			} else {
				// Ours changed, theirs deleted — keep ours (module still needed).
				result.Resolved[p] = ours.BlobHash
			}

		// Deleted by ours.
		case inBase && !inOurs && inTheirs:
			if theirs.BlobHash == base.BlobHash {
				// Theirs unchanged, ours deleted — remove.
				result.Removed = append(result.Removed, p)
			} else {
				// Theirs changed, ours deleted — keep theirs.
				result.Resolved[p] = theirs.BlobHash
			}
		}
	}

	return result, nil
}

// newerCommit compares two commit hashes by generation number and returns
// the one with the higher generation (i.e., the newer commit). If they have
// equal generations, it returns a deterministically (the lexicographically
// smaller hash).
func (r *Repo) newerCommit(a, b object.Hash) (object.Hash, error) {
	state := r.getMergeTraversalState()

	genA, err := state.generation(r, a)
	if err != nil {
		return "", err
	}
	genB, err := state.generation(r, b)
	if err != nil {
		return "", err
	}

	if genA > genB {
		return a, nil
	}
	if genB > genA {
		return b, nil
	}
	// Equal generation — deterministic tiebreak.
	if a <= b {
		return a, nil
	}
	return b, nil
}

// collectModulePaths returns a sorted, deduplicated list of all module paths
// present across the given maps.
func collectModulePaths(maps ...map[string]TreeModuleEntry) []string {
	seen := make(map[string]struct{})
	for _, m := range maps {
		for p := range m {
			seen[p] = struct{}{}
		}
	}

	paths := make([]string, 0, len(seen))
	for p := range seen {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return paths
}
