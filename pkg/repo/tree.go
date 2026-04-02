package repo

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/odvcencio/graft/pkg/object"
)

// DefaultSidecarDirs lists directories whose contents are injected into every
// commit tree automatically, without requiring the user to stage them.
var DefaultSidecarDirs = []string{".gts"}

// TreeFileEntry represents a single file in a flattened tree.
type TreeFileEntry struct {
	Path           string
	BlobHash       object.Hash
	EntityListHash object.Hash
	Mode           string
}

// BuildTree converts the flat staging entries into a hierarchical tree
// structure, writing TreeObj objects to the store and returning the root hash.
//
// Staging entries use forward-slash paths (e.g. "pkg/util/util.go").
// BuildTree groups them by directory, recursively creates subtrees, and
// returns the root tree hash.
func (r *Repo) BuildTree(s *Staging) (object.Hash, error) {
	rootHash, err := r.buildTreeDir(s, "")
	if err != nil {
		return "", err
	}

	// Inject sidecar directories into the root tree.
	rootHash, err = r.injectSidecarDirs(rootHash, DefaultSidecarDirs)
	if err != nil {
		return "", fmt.Errorf("inject sidecar dirs: %w", err)
	}
	return rootHash, nil
}

// injectSidecarDirs reads each sidecar directory from the working tree,
// writes its files as blobs, builds subtrees, and merges them into the root
// tree. Directories that do not exist or are empty are silently skipped.
func (r *Repo) injectSidecarDirs(rootHash object.Hash, dirs []string) (object.Hash, error) {
	if len(dirs) == 0 {
		return rootHash, nil
	}

	// Collect sidecar subtree entries to add to the root tree.
	var sidecarEntries []object.TreeEntry
	for _, dir := range dirs {
		absDir := filepath.Join(r.RootDir, dir)
		info, err := os.Stat(absDir)
		if err != nil || !info.IsDir() {
			continue // directory does not exist or is not a directory
		}

		subtreeHash, err := r.buildSidecarSubtree(absDir)
		if err != nil {
			return "", fmt.Errorf("build sidecar subtree %q: %w", dir, err)
		}
		if subtreeHash == "" {
			continue // empty directory
		}

		sidecarEntries = append(sidecarEntries, object.TreeEntry{
			Name:        dir,
			IsDir:       true,
			Mode:        object.TreeModeDir,
			SubtreeHash: subtreeHash,
		})
	}

	if len(sidecarEntries) == 0 {
		return rootHash, nil
	}

	// Read the existing root tree and merge sidecar entries.
	rootTree, err := r.Store.ReadTree(rootHash)
	if err != nil {
		return "", fmt.Errorf("read root tree for sidecar merge: %w", err)
	}

	// Build a map of existing entries for dedup.
	entryMap := make(map[string]int, len(rootTree.Entries))
	for i, e := range rootTree.Entries {
		entryMap[e.Name] = i
	}

	for _, se := range sidecarEntries {
		if idx, exists := entryMap[se.Name]; exists {
			// Replace existing entry (sidecar overrides).
			rootTree.Entries[idx] = se
		} else {
			rootTree.Entries = append(rootTree.Entries, se)
		}
	}

	// Re-sort entries by name (required by tree format).
	sort.Slice(rootTree.Entries, func(i, j int) bool {
		return rootTree.Entries[i].Name < rootTree.Entries[j].Name
	})

	newHash, err := r.Store.WriteTree(rootTree)
	if err != nil {
		return "", fmt.Errorf("write merged root tree: %w", err)
	}
	return newHash, nil
}

// buildSidecarSubtree recursively walks a directory on disk, writes blobs for
// each file, and returns a tree hash. Returns ("", nil) if the directory is
// empty.
func (r *Repo) buildSidecarSubtree(dir string) (object.Hash, error) {
	dirEntries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("read dir %q: %w", dir, err)
	}

	var entries []object.TreeEntry
	for _, de := range dirEntries {
		name := de.Name()
		childPath := filepath.Join(dir, name)

		if de.IsDir() {
			subtreeHash, err := r.buildSidecarSubtree(childPath)
			if err != nil {
				return "", err
			}
			if subtreeHash == "" {
				continue // skip empty subdirectories
			}
			entries = append(entries, object.TreeEntry{
				Name:        name,
				IsDir:       true,
				Mode:        object.TreeModeDir,
				SubtreeHash: subtreeHash,
			})
		} else {
			data, err := os.ReadFile(childPath)
			if err != nil {
				return "", fmt.Errorf("read sidecar file %q: %w", childPath, err)
			}
			blobHash, err := r.Store.WriteBlob(&object.Blob{Data: data})
			if err != nil {
				return "", fmt.Errorf("write sidecar blob %q: %w", childPath, err)
			}
			entries = append(entries, object.TreeEntry{
				Name:     name,
				IsDir:    false,
				Mode:     object.TreeModeFile,
				BlobHash: blobHash,
			})
		}
	}

	if len(entries) == 0 {
		return "", nil
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name < entries[j].Name
	})

	treeObj := &object.TreeObj{Entries: entries}
	h, err := r.Store.WriteTree(treeObj)
	if err != nil {
		return "", fmt.Errorf("write sidecar tree: %w", err)
	}
	return h, nil
}

// buildTreeDir builds a TreeObj for the given directory prefix and writes it
// to the store. It returns the tree's hash.
func (r *Repo) buildTreeDir(s *Staging, prefix string) (object.Hash, error) {
	// Collect direct children: files and subdirectory names.
	files := make(map[string]*StagingEntry) // name -> entry
	subdirs := make(map[string]struct{})    // immediate child dir names

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
				Mode:           normalizeFileMode(entry.Mode),
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
				Mode:        object.TreeModeDir,
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
	result := make([]TreeFileEntry, 0, 64)
	if err := r.flattenTreeInto(h, "", &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (r *Repo) flattenTreeInto(h object.Hash, prefix string, out *[]TreeFileEntry) error {
	treeObj, err := r.Store.ReadTree(h)
	if err != nil {
		return fmt.Errorf("flatten tree: read %s: %w", h, err)
	}

	useFastJoin := false
	dropPrefix := false
	prefixWithSlash := ""
	if prefix != "" {
		if isSimpleCleanPath(prefix) {
			useFastJoin = true
			prefixWithSlash = prefix + "/"
		} else {
			// For unclean-but-joinable prefixes (for example "./dir"), clean once
			// and then fast-join simple child names without calling path.Join
			// repeatedly for every entry.
			cleanPrefix := path.Clean(prefix)
			switch {
			case cleanPrefix == ".":
				useFastJoin = true
				dropPrefix = true
			case isSimpleCleanPath(cleanPrefix):
				useFastJoin = true
				prefixWithSlash = cleanPrefix + "/"
			}
		}
	}

	for _, entry := range treeObj.Entries {
		if entry.Mode == object.TreeModeModule {
			continue // gitlinks are not files to checkout
		}

		fullPath := entry.Name
		if prefix != "" {
			if useFastJoin && isSimplePathElem(entry.Name) {
				if !dropPrefix {
					fullPath = prefixWithSlash + entry.Name
				}
			} else {
				fullPath = path.Join(prefix, entry.Name)
			}
		}

		if entry.IsDir {
			if err := r.flattenTreeInto(entry.SubtreeHash, fullPath, out); err != nil {
				return err
			}
		} else {
			*out = append(*out, TreeFileEntry{
				Path:           fullPath,
				BlobHash:       entry.BlobHash,
				EntityListHash: entry.EntityListHash,
				Mode:           normalizeFileMode(entry.Mode),
			})
		}
	}
	return nil
}

func isSimpleCleanPath(p string) bool {
	if p == "" {
		return false
	}

	segStart := 0
	for i := 0; i <= len(p); i++ {
		if i < len(p) && p[i] != '/' {
			continue
		}
		if i == segStart {
			return false
		}
		seg := p[segStart:i]
		if seg == "." || seg == ".." {
			return false
		}
		segStart = i + 1
	}

	return true
}

func isSimplePathElem(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	for i := 0; i < len(name); i++ {
		if name[i] == '/' {
			return false
		}
	}
	return true
}

// TreeModuleEntry represents a gitlink (submodule) entry in a flattened tree.
type TreeModuleEntry struct {
	Path     string      // full path with forward slashes
	BlobHash object.Hash // the module's pinned commit hash
}

// FlattenTreeWithModules walks a tree object recursively, returning both
// file entries and module (gitlink, mode 160000) entries separately.
func (r *Repo) FlattenTreeWithModules(h object.Hash) ([]TreeFileEntry, []TreeModuleEntry, error) {
	files := make([]TreeFileEntry, 0, 64)
	var modules []TreeModuleEntry
	if err := r.flattenTreeWithModulesInto(h, "", &files, &modules); err != nil {
		return nil, nil, err
	}
	return files, modules, nil
}

func (r *Repo) flattenTreeWithModulesInto(h object.Hash, prefix string, files *[]TreeFileEntry, modules *[]TreeModuleEntry) error {
	treeObj, err := r.Store.ReadTree(h)
	if err != nil {
		return fmt.Errorf("flatten tree: read %s: %w", h, err)
	}

	useFastJoin := false
	dropPrefix := false
	prefixWithSlash := ""
	if prefix != "" {
		if isSimpleCleanPath(prefix) {
			useFastJoin = true
			prefixWithSlash = prefix + "/"
		} else {
			cleanPrefix := path.Clean(prefix)
			switch {
			case cleanPrefix == ".":
				useFastJoin = true
				dropPrefix = true
			case isSimpleCleanPath(cleanPrefix):
				useFastJoin = true
				prefixWithSlash = cleanPrefix + "/"
			}
		}
	}

	for _, entry := range treeObj.Entries {
		fullPath := entry.Name
		if prefix != "" {
			if useFastJoin && isSimplePathElem(entry.Name) {
				if !dropPrefix {
					fullPath = prefixWithSlash + entry.Name
				}
			} else {
				fullPath = path.Join(prefix, entry.Name)
			}
		}

		if entry.Mode == object.TreeModeModule {
			*modules = append(*modules, TreeModuleEntry{
				Path:     fullPath,
				BlobHash: entry.BlobHash,
			})
			continue
		}

		if entry.IsDir {
			if err := r.flattenTreeWithModulesInto(entry.SubtreeHash, fullPath, files, modules); err != nil {
				return err
			}
		} else {
			*files = append(*files, TreeFileEntry{
				Path:           fullPath,
				BlobHash:       entry.BlobHash,
				EntityListHash: entry.EntityListHash,
				Mode:           normalizeFileMode(entry.Mode),
			})
		}
	}
	return nil
}
