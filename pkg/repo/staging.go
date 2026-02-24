package repo

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/odvcencio/got/pkg/entity"
	"github.com/odvcencio/got/pkg/object"
)

// StagingEntry records the staged state of a single file.
type StagingEntry struct {
	Path           string      `json:"path"`
	BlobHash       object.Hash `json:"blob_hash"`
	EntityListHash object.Hash `json:"entity_list_hash,omitempty"`
	Mode           string      `json:"mode,omitempty"`
	Conflict       bool        `json:"conflict,omitempty"`
	BaseBlobHash   object.Hash `json:"base_blob_hash,omitempty"`
	OursBlobHash   object.Hash `json:"ours_blob_hash,omitempty"`
	TheirsBlobHash object.Hash `json:"theirs_blob_hash,omitempty"`
	ModTime        int64       `json:"mod_time"`
	Size           int64       `json:"size"`
}

// Staging holds the full staging area (index) for a Got repository.
type Staging struct {
	Entries map[string]*StagingEntry `json:"entries"`
}

// indexPath returns the filesystem path to the staging index file.
func (r *Repo) indexPath() string {
	return filepath.Join(r.GotDir, "index")
}

// ReadStaging loads the staging area from .got/index. If the file does not
// exist, an empty Staging is returned (no error).
func (r *Repo) ReadStaging() (*Staging, error) {
	data, err := os.ReadFile(r.indexPath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &Staging{Entries: make(map[string]*StagingEntry)}, nil
		}
		return nil, fmt.Errorf("read staging: %w", err)
	}

	var stg Staging
	if err := json.Unmarshal(data, &stg); err != nil {
		return nil, fmt.Errorf("read staging: unmarshal: %w", err)
	}
	if stg.Entries == nil {
		stg.Entries = make(map[string]*StagingEntry)
	}
	return &stg, nil
}

// WriteStaging atomically writes the staging area to .got/index.
func (r *Repo) WriteStaging(s *Staging) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("write staging: marshal: %w", err)
	}

	// Atomic write via temp file + rename.
	tmp, err := os.CreateTemp(r.GotDir, ".index-tmp-*")
	if err != nil {
		return fmt.Errorf("write staging: tmpfile: %w", err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("write staging: write: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("write staging: close: %w", err)
	}

	if err := os.Rename(tmpName, r.indexPath()); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("write staging: rename: %w", err)
	}
	return nil
}

// Add stages the given file paths. Each path is resolved relative to the
// repo root. For each file:
//  1. The raw content is written as a blob to the object store.
//  2. Entity extraction is attempted. If successful, each entity is written
//     as an EntityObj, and an EntityListObj referencing them is also stored.
//  3. A StagingEntry is created/updated with the resulting hashes and file
//     metadata, and the staging area is flushed to disk.
func (r *Repo) Add(paths []string) error {
	stg, err := r.ReadStaging()
	if err != nil {
		return fmt.Errorf("add: %w", err)
	}

	toAdd, err := r.expandAddPaths(paths)
	if err != nil {
		return fmt.Errorf("add: %w", err)
	}
	if len(toAdd) == 0 {
		return fmt.Errorf("add: no files matched")
	}

	for _, relPath := range toAdd {
		absPath := filepath.Join(r.RootDir, filepath.FromSlash(relPath))
		content, err := os.ReadFile(absPath)
		if err != nil {
			return fmt.Errorf("add: read %q: %w", relPath, err)
		}

		info, err := os.Stat(absPath)
		if err != nil {
			return fmt.Errorf("add: stat %q: %w", relPath, err)
		}

		// Write blob.
		blobHash, err := r.Store.WriteBlob(&object.Blob{Data: content})
		if err != nil {
			return fmt.Errorf("add: write blob %q: %w", relPath, err)
		}

		// Try entity extraction.
		var entityListHash object.Hash
		el, extractErr := entity.Extract(relPath, content)
		if extractErr == nil && len(el.Entities) > 0 {
			entityListHash, err = r.writeEntityList(relPath, el)
			if err != nil {
				return fmt.Errorf("add: write entities %q: %w", relPath, err)
			}
		}
		// If extraction fails (unsupported file type), entityListHash stays empty.

		stg.Entries[relPath] = &StagingEntry{
			Path:           relPath,
			BlobHash:       blobHash,
			EntityListHash: entityListHash,
			Mode:           modeFromFileInfo(info),
			ModTime:        info.ModTime().Unix(),
			Size:           info.Size(),
		}
	}

	if err := r.WriteStaging(stg); err != nil {
		return fmt.Errorf("add: %w", err)
	}
	return nil
}

// Remove stages file deletions and optionally removes files from disk.
func (r *Repo) Remove(paths []string, cached bool) error {
	stg, err := r.ReadStaging()
	if err != nil {
		return fmt.Errorf("rm: %w", err)
	}

	toRemove, err := r.expandRemovePaths(paths, stg)
	if err != nil {
		return fmt.Errorf("rm: %w", err)
	}
	if len(toRemove) == 0 {
		return fmt.Errorf("rm: no tracked files matched")
	}

	for _, relPath := range toRemove {
		delete(stg.Entries, relPath)
		if cached {
			continue
		}

		absPath := filepath.Join(r.RootDir, filepath.FromSlash(relPath))
		if err := os.Remove(absPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("rm: remove %q: %w", relPath, err)
		}
		r.removeEmptyParents(filepath.Dir(absPath))
	}

	if err := r.WriteStaging(stg); err != nil {
		return fmt.Errorf("rm: %w", err)
	}
	return nil
}

// writeEntityList writes each entity as an EntityObj to the store, collects
// their hashes, then writes and returns the hash of the EntityListObj.
func (r *Repo) writeEntityList(relPath string, el *entity.EntityList) (object.Hash, error) {
	var refs []object.Hash
	for _, ent := range el.Entities {
		entObj := &object.EntityObj{
			Kind:     ent.Kind.String(),
			Name:     ent.Name,
			DeclKind: ent.DeclKind,
			Receiver: ent.Receiver,
			Body:     ent.Body,
			BodyHash: object.Hash(ent.BodyHash),
		}
		h, err := r.Store.WriteEntity(entObj)
		if err != nil {
			return "", fmt.Errorf("write entity %q in %q: %w", ent.Name, relPath, err)
		}
		refs = append(refs, h)
	}

	elObj := &object.EntityListObj{
		Language:   el.Language,
		Path:       relPath,
		EntityRefs: refs,
	}
	return r.Store.WriteEntityList(elObj)
}

// repoRelPath converts a path (absolute, or relative to CWD) into a path
// relative to the repository root. If the path is already relative and does
// not start with the repo root, it is assumed to already be repo-relative.
func (r *Repo) repoRelPath(p string) (string, error) {
	if filepath.IsAbs(p) {
		rel, err := filepath.Rel(r.RootDir, p)
		if err != nil {
			return "", fmt.Errorf("cannot make %q relative to %q: %w", p, r.RootDir, err)
		}
		return filepath.ToSlash(rel), nil
	}

	// Try to resolve via CWD.
	cwd, err := os.Getwd()
	if err != nil {
		// Fall through to treating p as repo-relative.
		return filepath.ToSlash(filepath.Clean(p)), nil
	}

	abs := filepath.Join(cwd, p)
	// Check if the absolute path lives within the repo root.
	rel, err := filepath.Rel(r.RootDir, abs)
	if err != nil {
		return filepath.ToSlash(filepath.Clean(p)), nil
	}

	// If the relative path starts with "..", p is outside the repo.
	// In that case, treat the original p as already repo-relative.
	if len(rel) >= 2 && rel[:2] == ".." {
		return filepath.ToSlash(filepath.Clean(p)), nil
	}

	return filepath.ToSlash(rel), nil
}

func (r *Repo) expandAddPaths(inputs []string) ([]string, error) {
	ic := NewIgnoreChecker(r.RootDir)
	seen := make(map[string]struct{})

	for _, input := range inputs {
		input = strings.TrimSpace(input)
		if input == "" {
			continue
		}

		if hasGlobMeta(input) {
			spec, err := r.repoRelPath(input)
			if err != nil {
				return nil, fmt.Errorf("resolve path %q: %w", input, err)
			}
			if isOutsideRepo(spec) {
				return nil, fmt.Errorf("path %q is outside repository", input)
			}
			globPattern := filepath.Join(r.RootDir, filepath.FromSlash(spec))
			matches, err := filepath.Glob(globPattern)
			if err != nil {
				return nil, fmt.Errorf("glob %q: %w", input, err)
			}
			if len(matches) == 0 {
				return nil, fmt.Errorf("pathspec %q did not match any files", input)
			}
			for _, m := range matches {
				if err := r.collectAddPath(m, ic, seen); err != nil {
					return nil, err
				}
			}
			continue
		}
		if err := r.collectAddPath(input, ic, seen); err != nil {
			return nil, err
		}
	}

	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)
	return out, nil
}

func (r *Repo) collectAddPath(input string, ic *IgnoreChecker, seen map[string]struct{}) error {
	relPath, err := r.repoRelPath(input)
	if err != nil {
		return fmt.Errorf("resolve path %q: %w", input, err)
	}
	if isOutsideRepo(relPath) {
		return fmt.Errorf("path %q is outside repository", input)
	}
	if relPath == "." {
		relPath = ""
	}

	absPath := filepath.Join(r.RootDir, filepath.FromSlash(relPath))
	info, err := os.Stat(absPath)
	if err != nil {
		return fmt.Errorf("stat %q: %w", relPath, err)
	}
	if !info.IsDir() {
		rel := filepath.ToSlash(relPath)
		if ic.IsIgnored(rel) {
			return nil
		}
		seen[rel] = struct{}{}
		return nil
	}

	return filepath.WalkDir(absPath, func(path string, d fs.DirEntry, walkErr error) error {
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
		if d.IsDir() {
			if ic.IsIgnored(rel) {
				return filepath.SkipDir
			}
			return nil
		}
		if ic.IsIgnored(rel) {
			return nil
		}
		seen[rel] = struct{}{}
		return nil
	})
}

func (r *Repo) expandRemovePaths(inputs []string, stg *Staging) ([]string, error) {
	tracked := make([]string, 0, len(stg.Entries))
	for p := range stg.Entries {
		tracked = append(tracked, filepath.ToSlash(p))
	}
	sort.Strings(tracked)

	seen := make(map[string]struct{})
	for _, input := range inputs {
		input = strings.TrimSpace(input)
		if input == "" {
			continue
		}

		spec, err := r.repoRelPath(input)
		if err != nil {
			return nil, fmt.Errorf("resolve path %q: %w", input, err)
		}
		spec = filepath.ToSlash(spec)
		if isOutsideRepo(spec) {
			return nil, fmt.Errorf("path %q is outside repository", input)
		}

		matched := false
		if spec == "." || spec == "" {
			for _, p := range tracked {
				seen[p] = struct{}{}
			}
			matched = len(tracked) > 0
		} else if hasGlobMeta(spec) {
			for _, p := range tracked {
				if matchPathspec(spec, p) {
					seen[p] = struct{}{}
					matched = true
				}
			}
		} else {
			for _, p := range tracked {
				if p == spec || strings.HasPrefix(p, spec+"/") {
					seen[p] = struct{}{}
					matched = true
				}
			}
		}
		if !matched {
			return nil, fmt.Errorf("pathspec %q did not match tracked files", input)
		}
	}

	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)
	return out, nil
}

func hasGlobMeta(path string) bool {
	return strings.ContainsAny(path, "*?[")
}

func matchPathspec(spec, path string) bool {
	if strings.Contains(spec, "/") {
		ok, _ := filepath.Match(spec, path)
		return ok
	}
	ok, _ := filepath.Match(spec, filepath.Base(path))
	return ok
}

func isOutsideRepo(rel string) bool {
	rel = filepath.ToSlash(filepath.Clean(rel))
	return rel == ".." || strings.HasPrefix(rel, "../")
}
