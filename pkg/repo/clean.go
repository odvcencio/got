package repo

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// CleanOptions controls the behaviour of Clean and CleanDryRun.
type CleanOptions struct {
	Directories bool // -d: also remove untracked directories
	Force       bool // -f: required to actually delete (safety)
	IgnoredOnly bool // -x: remove only ignored files
	IgnoredToo  bool // -X: remove both untracked and ignored files
}

// Clean removes untracked files from the working tree and returns the list of
// removed paths (repo-relative, forward-slash separated). If Force is false the
// call returns an error without removing anything.
func (r *Repo) Clean(opts CleanOptions) ([]string, error) {
	if !opts.Force {
		return nil, fmt.Errorf("clean: refusing to clean without -f")
	}

	paths, err := r.collectCleanPaths(opts)
	if err != nil {
		return nil, err
	}

	// Remove files first, then directories (so dirs are empty when we try to
	// remove them).
	for _, rel := range paths {
		abs := filepath.Join(r.RootDir, filepath.FromSlash(rel))
		if err := os.RemoveAll(abs); err != nil {
			return nil, fmt.Errorf("clean: remove %q: %w", rel, err)
		}
	}

	return paths, nil
}

// CleanDryRun lists the files that would be removed by Clean without actually
// removing anything. The Force flag is not required.
func (r *Repo) CleanDryRun(opts CleanOptions) ([]string, error) {
	return r.collectCleanPaths(opts)
}

// collectCleanPaths walks the working tree and collects paths that should be
// cleaned according to opts.
func (r *Repo) collectCleanPaths(opts CleanOptions) ([]string, error) {
	stg, err := r.ReadStaging()
	if err != nil {
		return nil, fmt.Errorf("clean: %w", err)
	}

	tracked := make(map[string]bool, len(stg.Entries))
	for p := range stg.Entries {
		tracked[p] = true
	}

	ic := NewIgnoreChecker(r.RootDir)

	var files []string
	var dirs []string

	err = filepath.WalkDir(r.RootDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		rel, err := filepath.Rel(r.RootDir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)

		// Skip the root directory itself.
		if rel == "." {
			return nil
		}

		// Always skip .graft/ and .git/ directories.
		if d.IsDir() && (rel == ".graft" || rel == ".git") {
			return fs.SkipDir
		}

		ignored := ic.IsIgnored(rel)

		// Skip ignored directories entirely unless we care about ignored files.
		if d.IsDir() && ignored && !opts.IgnoredOnly && !opts.IgnoredToo {
			return fs.SkipDir
		}

		if d.IsDir() {
			// Directories are handled in a second pass if -d is set.
			// We still need to walk into them.
			return nil
		}

		// Skip tracked files — they are never cleaned.
		if tracked[rel] {
			return nil
		}

		// Decide whether to collect this file based on ignore flags.
		switch {
		case opts.IgnoredOnly:
			// -x: collect only ignored files.
			if ignored {
				files = append(files, rel)
			}
		case opts.IgnoredToo:
			// -X: collect both untracked and ignored files.
			files = append(files, rel)
		default:
			// Default: collect untracked, non-ignored files.
			if !ignored {
				files = append(files, rel)
			}
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("clean: walk: %w", err)
	}

	// If -d is set, collect empty untracked directories.
	if opts.Directories {
		dirs, err = r.collectEmptyUntrackedDirs(tracked, ic, opts)
		if err != nil {
			return nil, err
		}
	}

	// Combine files and dirs, sort, and return.
	result := make([]string, 0, len(files)+len(dirs))
	result = append(result, files...)
	result = append(result, dirs...)
	sort.Strings(result)

	return result, nil
}

// collectEmptyUntrackedDirs walks the working tree and collects directories
// that are untracked (no tracked files under them) and empty (or contain only
// files that would be cleaned). A directory qualifies if it contains no tracked
// files and no remaining non-cleanable files.
func (r *Repo) collectEmptyUntrackedDirs(tracked map[string]bool, ic *IgnoreChecker, opts CleanOptions) ([]string, error) {
	var dirs []string

	err := filepath.WalkDir(r.RootDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		rel, err := filepath.Rel(r.RootDir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)

		if rel == "." {
			return nil
		}

		// Always skip .graft/ and .git/.
		if d.IsDir() && (rel == ".graft" || rel == ".git") {
			return fs.SkipDir
		}

		if !d.IsDir() {
			return nil
		}

		// Check if the directory is empty.
		abs := filepath.Join(r.RootDir, filepath.FromSlash(rel))
		entries, readErr := os.ReadDir(abs)
		if readErr != nil {
			return nil
		}
		if len(entries) != 0 {
			return nil
		}

		// Empty directory — check it is untracked.
		hasTracked := false
		for p := range tracked {
			if p == rel || hasPathPrefix(p, rel) {
				hasTracked = true
				break
			}
		}
		if hasTracked {
			return nil
		}

		ignored := ic.IsIgnored(rel)

		switch {
		case opts.IgnoredOnly:
			if ignored {
				dirs = append(dirs, rel)
			}
		case opts.IgnoredToo:
			dirs = append(dirs, rel)
		default:
			if !ignored {
				dirs = append(dirs, rel)
			}
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("clean: walk dirs: %w", err)
	}

	return dirs, nil
}

// hasPathPrefix checks whether path starts with prefix as a directory segment.
func hasPathPrefix(path, prefix string) bool {
	if len(path) <= len(prefix) {
		return false
	}
	return path[:len(prefix)] == prefix && path[len(prefix)] == '/'
}
