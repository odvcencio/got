package repo

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/odvcencio/graft/pkg/object"
)

// CreateBranch creates a new branch pointing at the given target hash.
// It writes the hash to .graft/refs/heads/<name>. Returns an error if the
// branch already exists.
func (r *Repo) CreateBranch(name string, target object.Hash) error {
	refName := filepath.ToSlash(filepath.Join("refs", "heads", name))
	if err := r.UpdateRefCAS(refName, target, ""); err != nil {
		if errors.Is(err, ErrRefCASMismatch) {
			return fmt.Errorf("create branch: branch %q already exists", name)
		}
		return fmt.Errorf("create branch %q: %w", name, err)
	}
	return nil
}

// DeleteBranch removes the branch ref file .graft/refs/heads/<name>.
// Returns an error if the branch is the current branch or does not exist.
func (r *Repo) DeleteBranch(name string) error {
	// Check if this is the current branch.
	current, err := r.CurrentBranch()
	if err != nil {
		return fmt.Errorf("delete branch: %w", err)
	}
	if current == name {
		return fmt.Errorf("delete branch: cannot delete current branch %q", name)
	}

	refPath := filepath.Join(r.refsBaseDir(), "refs", "heads", name)
	if err := os.Remove(refPath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("delete branch: branch %q does not exist", name)
		}
		return fmt.Errorf("delete branch %q: %w", name, err)
	}
	return nil
}

// ListBranches reads .graft/refs/heads/ recursively and returns the branch
// names sorted alphabetically. Hierarchical branches (e.g. "feature/foo")
// are discovered by walking subdirectories under refs/heads/.
func (r *Repo) ListBranches() ([]string, error) {
	headsDir := filepath.Join(r.refsBaseDir(), "refs", "heads")

	info, err := os.Stat(headsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("list branches: %w", err)
	}
	if !info.IsDir() {
		return nil, nil
	}

	var names []string
	err = filepath.WalkDir(headsDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(headsDir, path)
		if err != nil {
			return err
		}
		names = append(names, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list branches: %w", err)
	}
	sort.Strings(names)
	return names, nil
}

// CurrentBranch reads HEAD and returns the branch name if HEAD is a symbolic
// ref (e.g. "ref: refs/heads/main" → "main"). If HEAD is detached (contains
// a raw hash), it returns "".
func (r *Repo) CurrentBranch() (string, error) {
	head, err := r.Head()
	if err != nil {
		return "", fmt.Errorf("current branch: %w", err)
	}

	const prefix = "refs/heads/"
	if strings.HasPrefix(head, prefix) {
		return strings.TrimPrefix(head, prefix), nil
	}

	// Detached HEAD or unexpected format.
	return "", nil
}
