package repo

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/odvcencio/graft/pkg/object"
)

// refsBaseDir returns the directory used for shared refs. For a linked
// worktree this is CommonDir (the main .graft/); for a normal repo it is
// GraftDir. HEAD and index always live in GraftDir.
func (r *Repo) refsBaseDir() string {
	if r.CommonDir != "" {
		return r.CommonDir
	}
	return r.GraftDir
}

// ResolveTreeish resolves a treeish string to a commit hash. It tries, in
// order: refs/tags/<treeish>, refs/heads/<treeish>, HEAD (if treeish is
// "HEAD"), and finally treats the value as a raw hash.
func (r *Repo) ResolveTreeish(treeish string) (object.Hash, error) {
	// Try tag ref first.
	if h, err := r.ResolveRef("refs/tags/" + treeish); err == nil {
		return h, nil
	}
	// Try branch ref.
	if h, err := r.ResolveRef("refs/heads/" + treeish); err == nil {
		return h, nil
	}
	// Try as-is (covers HEAD and full ref paths).
	if h, err := r.ResolveRef(treeish); err == nil {
		return h, nil
	}
	// Treat as raw hash: verify the commit exists.
	h := object.Hash(treeish)
	if _, err := r.Store.ReadCommit(h); err == nil {
		return h, nil
	}
	return "", fmt.Errorf("cannot resolve treeish %q", treeish)
}

// ListRefs lists references under .graft/refs.
// Names are returned relative to refs root, e.g. "heads/main", "tags/v1".
func (r *Repo) ListRefs(prefix string) (map[string]object.Hash, error) {
	root := filepath.Join(r.refsBaseDir(), "refs")
	dir := root
	if strings.TrimSpace(prefix) != "" {
		dir = filepath.Join(root, filepath.FromSlash(prefix))
	}

	refs := make(map[string]object.Hash)
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(rel)
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		refs[name] = object.Hash(strings.TrimSpace(string(data)))
		return nil
	})
	if os.IsNotExist(err) {
		return refs, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list refs: %w", err)
	}
	return refs, nil
}
