package repo

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/odvcencio/got/pkg/object"
)

// TreeFileEntry represents a single file in a flattened tree.
type TreeFileEntry struct {
	Path           string
	BlobHash       object.Hash
	EntityListHash object.Hash
}

// BuildTree converts the flat staging entries into a hierarchical tree
// structure, writing TreeObj objects to the store and returning the root hash.
//
// Staging entries use forward-slash paths (e.g. "pkg/util/util.go").
// BuildTree groups them by directory, recursively creates subtrees, and
// returns the root tree hash.
func (r *Repo) BuildTree(s *Staging) (object.Hash, error) {
	return r.buildTreeDir(s, "")
}

// buildTreeDir builds a TreeObj for the given directory prefix and writes it
// to the store. It returns the tree's hash.
func (r *Repo) buildTreeDir(s *Staging, prefix string) (object.Hash, error) {
	// Collect direct children: files and subdirectory names.
	files := make(map[string]*StagingEntry)   // name -> entry
	subdirs := make(map[string]struct{})       // immediate child dir names

	for p, entry := range s.Entries {
		// Determine the path relative to our prefix.
		var rel string
		if prefix == "" {
			rel = p
		} else {
			if !strings.HasPrefix(p, prefix+"/") {
				continue
			}
			rel = p[len(prefix)+1:]
		}

		// Split into first segment and rest.
		slash := strings.IndexByte(rel, '/')
		if slash < 0 {
			// Direct child file.
			files[rel] = entry
		} else {
			// Child is in a subdirectory.
			subdirs[rel[:slash]] = struct{}{}
		}
	}

	// Build the tree entries, sorted by name.
	names := make([]string, 0, len(files)+len(subdirs))
	for name := range files {
		names = append(names, name)
	}
	for name := range subdirs {
		// Only add if not already a file (a name cannot be both).
		if _, isFile := files[name]; !isFile {
			names = append(names, name)
		}
	}
	sort.Strings(names)

	var entries []object.TreeEntry
	for _, name := range names {
		if entry, isFile := files[name]; isFile {
			entries = append(entries, object.TreeEntry{
				Name:           name,
				IsDir:          false,
				BlobHash:       entry.BlobHash,
				EntityListHash: entry.EntityListHash,
			})
		} else {
			// Subdirectory: recurse.
			childPrefix := name
			if prefix != "" {
				childPrefix = prefix + "/" + name
			}
			subHash, err := r.buildTreeDir(s, childPrefix)
			if err != nil {
				return "", fmt.Errorf("build tree %q: %w", childPrefix, err)
			}
			entries = append(entries, object.TreeEntry{
				Name:        name,
				IsDir:       true,
				SubtreeHash: subHash,
			})
		}
	}

	treeObj := &object.TreeObj{Entries: entries}
	h, err := r.Store.WriteTree(treeObj)
	if err != nil {
		return "", fmt.Errorf("write tree (prefix=%q): %w", prefix, err)
	}
	return h, nil
}

// FlattenTree walks a tree object recursively, returning all file entries
// with their full paths (using forward slashes).
func (r *Repo) FlattenTree(h object.Hash) ([]TreeFileEntry, error) {
	return r.flattenTreeRec(h, "")
}

func (r *Repo) flattenTreeRec(h object.Hash, prefix string) ([]TreeFileEntry, error) {
	treeObj, err := r.Store.ReadTree(h)
	if err != nil {
		return nil, fmt.Errorf("flatten tree: read %s: %w", h, err)
	}

	var result []TreeFileEntry
	for _, entry := range treeObj.Entries {
		fullPath := entry.Name
		if prefix != "" {
			fullPath = path.Join(prefix, entry.Name)
		}

		if entry.IsDir {
			sub, err := r.flattenTreeRec(entry.SubtreeHash, fullPath)
			if err != nil {
				return nil, err
			}
			result = append(result, sub...)
		} else {
			result = append(result, TreeFileEntry{
				Path:           fullPath,
				BlobHash:       entry.BlobHash,
				EntityListHash: entry.EntityListHash,
			})
		}
	}
	return result, nil
}
