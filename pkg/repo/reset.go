package repo

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// Reset unstages paths by restoring index entries to their HEAD versions.
//
// Behavior:
// - If a path exists in HEAD, its staging entry is reset to HEAD's blob/mode.
// - If a path does not exist in HEAD, its staging entry is removed.
// - If no paths are provided, the entire index is reset to HEAD.
//
// Reset does not modify the working tree.
func (r *Repo) Reset(paths []string) error {
	stg, err := r.ReadStaging()
	if err != nil {
		return fmt.Errorf("reset: %w", err)
	}

	headEntries, err := r.headTreeFileEntryMap()
	if err != nil {
		return fmt.Errorf("reset: %w", err)
	}

	targets, err := r.resolveResetTargets(paths, stg, headEntries)
	if err != nil {
		return fmt.Errorf("reset: %w", err)
	}

	for _, p := range targets {
		if headEntry, ok := headEntries[p]; ok {
			// Force status to hash-check this path after reset to avoid stale
			// stat-only matches when worktree content differs from HEAD.
			stg.Entries[p] = &StagingEntry{
				Path:           p,
				BlobHash:       headEntry.BlobHash,
				EntityListHash: headEntry.EntityListHash,
				Mode:           normalizeFileMode(headEntry.Mode),
				ModTime:        0,
				Size:           -1,
			}
			continue
		}
		delete(stg.Entries, p)
	}

	if err := r.WriteStaging(stg); err != nil {
		return fmt.Errorf("reset: %w", err)
	}
	return nil
}

func (r *Repo) headTreeFileEntryMap() (map[string]TreeFileEntry, error) {
	result := make(map[string]TreeFileEntry)

	headHash, err := r.ResolveRef("HEAD")
	if err != nil {
		return result, nil
	}
	commit, err := r.Store.ReadCommit(headHash)
	if err != nil {
		return nil, fmt.Errorf("read HEAD commit: %w", err)
	}
	entries, err := r.FlattenTree(commit.TreeHash)
	if err != nil {
		return nil, fmt.Errorf("flatten HEAD tree: %w", err)
	}
	for _, e := range entries {
		result[e.Path] = e
	}
	return result, nil
}

func (r *Repo) resolveResetTargets(paths []string, stg *Staging, head map[string]TreeFileEntry) ([]string, error) {
	all := make(map[string]struct{}, len(stg.Entries)+len(head))
	for p := range stg.Entries {
		all[p] = struct{}{}
	}
	for p := range head {
		all[p] = struct{}{}
	}

	if len(paths) == 0 {
		return sortedPathSet(all), nil
	}

	targets := make(map[string]struct{})
	for _, raw := range paths {
		rel, err := r.repoRelPath(raw)
		if err != nil {
			return nil, err
		}
		rel = filepath.ToSlash(filepath.Clean(strings.TrimSpace(rel)))
		if rel == "" || rel == "." {
			for p := range all {
				targets[p] = struct{}{}
			}
			continue
		}

		matched := false
		if _, ok := all[rel]; ok {
			targets[rel] = struct{}{}
			matched = true
		}

		prefix := rel + "/"
		for p := range all {
			if strings.HasPrefix(p, prefix) {
				targets[p] = struct{}{}
				matched = true
			}
		}

		if !matched {
			return nil, fmt.Errorf("path %q did not match staged or HEAD entries", raw)
		}
	}

	return sortedPathSet(targets), nil
}

func sortedPathSet(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}
