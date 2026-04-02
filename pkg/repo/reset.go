package repo

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/odvcencio/graft/pkg/object"
)

// ResetMode controls how much state a commit-level reset modifies.
type ResetMode int

const (
	// ResetMixed moves HEAD and resets staging to match the target commit.
	// The working tree is left untouched. This is the default mode.
	ResetMixed ResetMode = iota

	// ResetSoft only moves HEAD to the target commit. Staging and working
	// tree are left untouched.
	ResetSoft

	// ResetHard moves HEAD, resets staging, and restores the working tree
	// to match the target commit's tree.
	ResetHard
)

// ResetToCommit moves HEAD to the given target commit hash and adjusts
// staging and/or working tree according to the specified mode.
//
//   - Soft:  Only move HEAD. Staging and working tree are unchanged.
//   - Mixed: Move HEAD and reset staging to match target tree. Working tree
//     is unchanged.
//   - Hard:  Move HEAD, reset staging, and restore working tree to match
//     the target commit's tree.
func (r *Repo) ResetToCommit(target object.Hash, mode ResetMode) error {
	// 1. Verify the target commit exists and read it.
	commit, err := r.Store.ReadCommit(target)
	if err != nil {
		return fmt.Errorf("reset: read target commit %s: %w", target, err)
	}

	// 2. For hard mode, snapshot currently tracked files BEFORE moving HEAD,
	// so we know which files to remove that aren't in the target tree.
	var currentFiles map[string]bool
	if mode == ResetHard {
		currentFiles = r.trackedFiles()
	}

	// 3. Read the current HEAD so we can CAS-update it.
	oldHeadHash, resolveErr := r.ResolveRef("HEAD")

	// 4. Move HEAD to the target commit.
	head, err := r.Head()
	if err != nil {
		return fmt.Errorf("reset: read HEAD: %w", err)
	}

	if strings.HasPrefix(head, "refs/") {
		if resolveErr == nil {
			err = r.UpdateRefCAS(head, target, oldHeadHash)
		} else {
			err = r.UpdateRefCAS(head, target)
		}
		if err != nil {
			return fmt.Errorf("reset: update ref %q: %w", head, err)
		}
	} else {
		// Detached HEAD.
		if err := r.setHeadDetached(target); err != nil {
			return fmt.Errorf("reset: update HEAD: %w", err)
		}
	}

	if mode == ResetSoft {
		r.invalidateStatusCache()
		r.GitShadowReset("soft", string(target))
		return nil
	}

	// 5. For mixed and hard: reset staging to match target tree.
	targetEntries, err := r.FlattenTree(commit.TreeHash)
	if err != nil {
		return fmt.Errorf("reset: flatten target tree: %w", err)
	}

	targetMap := make(map[string]TreeFileEntry, len(targetEntries))
	for _, e := range targetEntries {
		targetMap[e.Path] = e
	}

	stg := &Staging{Entries: make(map[string]*StagingEntry, len(targetEntries))}
	for _, e := range targetEntries {
		stg.Entries[e.Path] = &StagingEntry{
			Path:           e.Path,
			BlobHash:       e.BlobHash,
			EntityListHash: e.EntityListHash,
			Mode:           normalizeFileMode(e.Mode),
			ModTime:        0,
			Size:           -1,
		}
	}

	if err := r.WriteStaging(stg); err != nil {
		return fmt.Errorf("reset: write staging: %w", err)
	}

	if mode == ResetMixed {
		r.invalidateStatusCache()
		r.GitShadowReset("mixed", string(target))
		return nil
	}

	// 6. For hard: restore working tree to match target tree.

	// 6a. Remove files not in target tree.
	for path := range currentFiles {
		if _, inTarget := targetMap[path]; !inTarget {
			absPath := filepath.Join(r.RootDir, filepath.FromSlash(path))
			if err := os.Remove(absPath); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("reset --hard: remove %q: %w", path, err)
			}
			r.removeEmptyParents(filepath.Dir(absPath))
		}
	}

	// 6b. Write all files from target tree.
	for _, e := range targetEntries {
		absPath := filepath.Join(r.RootDir, filepath.FromSlash(e.Path))

		dir := filepath.Dir(absPath)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("reset --hard: mkdir %q: %w", dir, err)
		}

		blob, err := r.Store.ReadBlob(e.BlobHash)
		if err != nil {
			return fmt.Errorf("reset --hard: read blob for %q: %w", e.Path, err)
		}

		blobData := blob.Data
		if ptr, ok := ParseLFSPointer(blobData); ok {
			lfsContent, lfsErr := r.ReadLFSObject(ptr.OID)
			if lfsErr == nil {
				blobData = lfsContent
			}
		}

		if err := os.WriteFile(absPath, blobData, filePermFromMode(e.Mode)); err != nil {
			return fmt.Errorf("reset --hard: write %q: %w", e.Path, err)
		}
	}

	// 6c. Update staging with accurate stat info from the freshly written files.
	for path, se := range stg.Entries {
		absPath := filepath.Join(r.RootDir, filepath.FromSlash(path))
		info, err := os.Stat(absPath)
		if err != nil {
			continue
		}
		setStagingEntryStat(se, info, se.Mode)
	}
	if err := r.WriteStaging(stg); err != nil {
		return fmt.Errorf("reset --hard: update staging stats: %w", err)
	}

	r.invalidateStatusCache()
	r.GitShadowReset("hard", string(target))
	return nil
}

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
	r.GitShadowResetPaths(paths)
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
