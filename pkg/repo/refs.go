package repo

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/odvcencio/got/pkg/object"
)

// ListRefs lists references under .got/refs.
// Names are returned relative to refs root, e.g. "heads/main", "tags/v1".
func (r *Repo) ListRefs(prefix string) (map[string]object.Hash, error) {
	root := filepath.Join(r.GotDir, "refs")
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
