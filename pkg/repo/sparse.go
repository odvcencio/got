package repo

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// sparseCheckoutPath returns the filesystem path to the sparse-checkout file.
func (r *Repo) sparseCheckoutPath() string {
	return filepath.Join(r.GotDir, "info", "sparse-checkout")
}

// IsSparseEnabled returns true if sparse checkout is active (the sparse
// file exists and contains at least one non-blank, non-comment pattern).
func (r *Repo) IsSparseEnabled() bool {
	patterns, err := r.SparseCheckoutList()
	if err != nil {
		return false
	}
	return len(patterns) > 0
}

// SparseCheckoutList reads the current sparse-checkout patterns from
// .graft/info/sparse-checkout. Blank lines and comment lines (starting
// with #) are stripped from the result.
func (r *Repo) SparseCheckoutList() ([]string, error) {
	f, err := os.Open(r.sparseCheckoutPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("sparse-checkout list: %w", err)
	}
	defer f.Close()

	var patterns []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		patterns = append(patterns, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("sparse-checkout list: %w", err)
	}
	return patterns, nil
}

// SparseCheckoutSet replaces the sparse-checkout patterns with the given
// list and applies the result to the working tree.
func (r *Repo) SparseCheckoutSet(patterns []string) error {
	if err := r.writeSparsePatterns(patterns); err != nil {
		return err
	}
	return r.applySparseCheckout()
}

// SparseCheckoutAdd appends patterns to the existing sparse-checkout list
// (deduplicating) and applies the result to the working tree.
func (r *Repo) SparseCheckoutAdd(patterns []string) error {
	existing, err := r.SparseCheckoutList()
	if err != nil {
		return err
	}

	seen := make(map[string]bool, len(existing))
	for _, p := range existing {
		seen[p] = true
	}
	for _, p := range patterns {
		if !seen[p] {
			existing = append(existing, p)
			seen[p] = true
		}
	}

	if err := r.writeSparsePatterns(existing); err != nil {
		return err
	}
	return r.applySparseCheckout()
}

// SparseCheckoutDisable disables sparse checkout by removing the sparse
// file and materializing all files from the current HEAD tree.
func (r *Repo) SparseCheckoutDisable() error {
	path := r.sparseCheckoutPath()
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("sparse-checkout disable: %w", err)
	}

	// Materialize all files that may have been excluded.
	return r.applySparseCheckout()
}

// writeSparsePatterns writes the given patterns to the sparse-checkout file,
// creating the parent directory if needed.
func (r *Repo) writeSparsePatterns(patterns []string) error {
	dir := filepath.Dir(r.sparseCheckoutPath())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("sparse-checkout: mkdir %q: %w", dir, err)
	}

	var b strings.Builder
	for _, p := range patterns {
		b.WriteString(p)
		b.WriteByte('\n')
	}

	if err := os.WriteFile(r.sparseCheckoutPath(), []byte(b.String()), 0o644); err != nil {
		return fmt.Errorf("sparse-checkout: write: %w", err)
	}
	return nil
}

// applySparseCheckout re-materializes the working tree from the current
// HEAD commit, respecting the current sparse patterns (if any). Files that
// no longer match are removed; files that now match are written.
func (r *Repo) applySparseCheckout() error {
	headHash, err := r.ResolveRef("HEAD")
	if err != nil {
		// No commits yet — nothing to apply.
		return nil
	}

	commit, err := r.Store.ReadCommit(headHash)
	if err != nil {
		return fmt.Errorf("sparse-checkout apply: read commit: %w", err)
	}

	targetFiles, err := r.FlattenTree(commit.TreeHash)
	if err != nil {
		return fmt.Errorf("sparse-checkout apply: flatten tree: %w", err)
	}

	sparseEnabled := r.IsSparseEnabled()

	for _, f := range targetFiles {
		absPath := filepath.Join(r.RootDir, filepath.FromSlash(f.Path))

		if sparseEnabled && !r.matchesSparsePatterns(f.Path) {
			// File should not be materialized — remove it if it exists.
			if err := os.Remove(absPath); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("sparse-checkout apply: remove %q: %w", f.Path, err)
			}
			r.removeEmptyParents(filepath.Dir(absPath))
			continue
		}

		// File should be materialized — write it if missing or stale.
		dir := filepath.Dir(absPath)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("sparse-checkout apply: mkdir %q: %w", dir, err)
		}

		blob, err := r.Store.ReadBlob(f.BlobHash)
		if err != nil {
			return fmt.Errorf("sparse-checkout apply: read blob for %q: %w", f.Path, err)
		}

		if err := os.WriteFile(absPath, blob.Data, filePermFromMode(f.Mode)); err != nil {
			return fmt.Errorf("sparse-checkout apply: write %q: %w", f.Path, err)
		}
	}

	// Rebuild staging to match what is on disk.
	stg := &Staging{Entries: make(map[string]*StagingEntry, len(targetFiles))}
	for _, f := range targetFiles {
		absPath := filepath.Join(r.RootDir, filepath.FromSlash(f.Path))
		info, err := os.Stat(absPath)
		if err != nil {
			if os.IsNotExist(err) {
				// File is excluded by sparse — skip staging entry.
				continue
			}
			return fmt.Errorf("sparse-checkout apply: stat %q: %w", f.Path, err)
		}

		entry := &StagingEntry{
			Path:           f.Path,
			BlobHash:       f.BlobHash,
			EntityListHash: f.EntityListHash,
		}
		setStagingEntryStat(entry, info, normalizeFileMode(f.Mode))
		stg.Entries[f.Path] = entry
	}
	if err := r.WriteStaging(stg); err != nil {
		return fmt.Errorf("sparse-checkout apply: %w", err)
	}

	return nil
}

// matchesSparsePatterns returns true if the given repo-relative path
// (forward-slash separated) matches the current sparse-checkout patterns.
//
// Cone mode rules:
//   - Top-level files (no directory component) always match.
//   - Patterns ending with "/" are directory prefixes: a file matches if
//     its path starts with the pattern directory.
//   - Patterns without trailing "/" match as path prefixes.
//   - Patterns starting with "!" negate a previous match.
//   - Last matching pattern wins (like .gitignore).
func (r *Repo) matchesSparsePatterns(path string) bool {
	patterns, err := r.SparseCheckoutList()
	if err != nil || len(patterns) == 0 {
		// If we can't read patterns, treat everything as matching.
		return true
	}

	// Top-level files (no slash) always match.
	if !strings.Contains(path, "/") {
		return true
	}

	matched := false
	for _, pat := range patterns {
		negated := false
		p := pat

		if strings.HasPrefix(p, "!") {
			negated = true
			p = p[1:]
		}

		// Normalize: remove trailing slash for comparison.
		p = strings.TrimSuffix(p, "/")

		if p == "" {
			continue
		}

		// A pattern matches if the file path equals the pattern, starts
		// with pattern + "/", or the pattern is a parent directory of the
		// file.
		if path == p || strings.HasPrefix(path, p+"/") {
			matched = !negated
		}
	}

	return matched
}

// dirCouldContainSparseMatch returns true if any sparse pattern could
// potentially match a file under the given directory prefix.
func (r *Repo) dirCouldContainSparseMatch(dirPath string) bool {
	patterns, err := r.SparseCheckoutList()
	if err != nil || len(patterns) == 0 {
		return true
	}

	for _, pat := range patterns {
		p := pat
		if strings.HasPrefix(p, "!") {
			p = p[1:]
		}
		p = strings.TrimSuffix(p, "/")
		if p == "" {
			continue
		}

		// The directory could contain matches if:
		// 1. The pattern is under this directory (pattern starts with dir/)
		// 2. This directory is under the pattern (dir starts with pattern/)
		// 3. They are the same path
		if p == dirPath || strings.HasPrefix(p, dirPath+"/") || strings.HasPrefix(dirPath, p+"/") {
			return true
		}
	}

	return false
}
