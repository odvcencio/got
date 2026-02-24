package repo

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/odvcencio/got/pkg/object"
)

// CreateBranch creates a new branch pointing at the given target hash.
// It writes the hash to .got/refs/heads/<name>. Returns an error if the
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

// DeleteBranch removes the branch ref file .got/refs/heads/<name>.
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

	refPath := filepath.Join(r.GotDir, "refs", "heads", name)
	if err := os.Remove(refPath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("delete branch: branch %q does not exist", name)
		}
		return fmt.Errorf("delete branch %q: %w", name, err)
	}
	return nil
}

// ListBranches reads .got/refs/heads/ and returns the branch names sorted
// alphabetically.
func (r *Repo) ListBranches() ([]string, error) {
	headsDir := filepath.Join(r.GotDir, "refs", "heads")

	entries, err := os.ReadDir(headsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("list branches: %w", err)
	}

	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		names = append(names, e.Name())
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
