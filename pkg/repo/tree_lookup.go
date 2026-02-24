package repo

import (
	"fmt"
	"strings"

	"github.com/odvcencio/got/pkg/object"
)

func (r *Repo) treeEntryAtPath(treeHash object.Hash, relPath string) (object.TreeEntry, bool, error) {
	parts := strings.Split(relPath, "/")
	current := treeHash

	for i, part := range parts {
		treeObj, err := r.Store.ReadTree(current)
		if err != nil {
			return object.TreeEntry{}, false, fmt.Errorf("read tree %s: %w", current, err)
		}

		var (
			entry object.TreeEntry
			found bool
		)
		for _, te := range treeObj.Entries {
			if te.Name == part {
				entry = te
				found = true
				break
			}
		}
		if !found {
			return object.TreeEntry{}, false, nil
		}

		last := i == len(parts)-1
		if last {
			if entry.IsDir {
				return object.TreeEntry{}, false, nil
			}
			return entry, true, nil
		}
		if !entry.IsDir || entry.SubtreeHash == "" {
			return object.TreeEntry{}, false, nil
		}
		current = entry.SubtreeHash
	}

	return object.TreeEntry{}, false, nil
}
