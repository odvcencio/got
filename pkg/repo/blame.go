package repo

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/odvcencio/got/pkg/entity"
	"github.com/odvcencio/got/pkg/object"
)

var (
	// ErrInvalidEntitySelector indicates a malformed selector. Selectors must
	// be in the form "<path::entity_key>".
	ErrInvalidEntitySelector = errors.New("invalid entity selector")
	// ErrEntityNotFound indicates that the selected entity could not be
	// attributed in the scanned history.
	ErrEntityNotFound = errors.New("entity not found")
)

// EntityBlame holds attribution details for a selected entity.
type EntityBlame struct {
	Path       string
	EntityKey  string
	Author     string
	CommitHash object.Hash
	Message    string
}

// BlameEntity returns the most recent commit on the current first-parent
// history where the selected entity changed relative to its parent.
//
// selector format: "<path::entity_key>".
func (r *Repo) BlameEntity(selector string, limit int) (*EntityBlame, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("blame: limit must be greater than 0")
	}

	pathSpec, entityKey, err := parseEntitySelector(selector)
	if err != nil {
		return nil, err
	}

	relPath, err := r.repoRelPath(pathSpec)
	if err != nil {
		return nil, fmt.Errorf("blame: resolve path %q: %w", pathSpec, err)
	}
	relPath = filepath.ToSlash(filepath.Clean(relPath))
	if relPath == "." || strings.TrimSpace(relPath) == "" {
		return nil, fmt.Errorf("blame: path is required in selector %q", selector)
	}
	if isOutsideRepo(relPath) {
		return nil, fmt.Errorf("blame: path %q is outside repository", pathSpec)
	}

	headHash, err := r.ResolveRef("HEAD")
	if err != nil {
		return nil, fmt.Errorf("blame: cannot resolve HEAD: %w", err)
	}

	currentHash := headHash
	scanned := 0
	sawEntity := false

	for currentHash != "" && scanned < limit {
		scanned++

		commit, err := r.Store.ReadCommit(currentHash)
		if err != nil {
			return nil, fmt.Errorf("blame: read commit %s: %w", currentHash, err)
		}

		currentBodyHash, inCurrent, err := r.entityBodyHashAtCommit(currentHash, commit, relPath, entityKey)
		if err != nil {
			return nil, err
		}
		if inCurrent {
			sawEntity = true
			parentHash := firstParentHash(commit)
			if parentHash == "" {
				return &EntityBlame{
					Path:       relPath,
					EntityKey:  entityKey,
					Author:     commit.Author,
					CommitHash: currentHash,
					Message:    commit.Message,
				}, nil
			}

			parentCommit, err := r.Store.ReadCommit(parentHash)
			if err != nil {
				return nil, fmt.Errorf("blame: read parent commit %s: %w", parentHash, err)
			}

			parentBodyHash, inParent, err := r.entityBodyHashAtCommit(parentHash, parentCommit, relPath, entityKey)
			if err != nil {
				return nil, err
			}
			if !inParent || parentBodyHash != currentBodyHash {
				return &EntityBlame{
					Path:       relPath,
					EntityKey:  entityKey,
					Author:     commit.Author,
					CommitHash: currentHash,
					Message:    commit.Message,
				}, nil
			}
		}

		parentHash := firstParentHash(commit)
		if parentHash == "" {
			break
		}
		currentHash = parentHash
	}

	selectorLabel := relPath + "::" + entityKey
	if sawEntity {
		return nil, fmt.Errorf("%w: %s (no change found within %d commits)", ErrEntityNotFound, selectorLabel, scanned)
	}
	return nil, fmt.Errorf("%w: %s (not found within %d commits)", ErrEntityNotFound, selectorLabel, scanned)
}

func parseEntitySelector(selector string) (pathSpec, entityKey string, err error) {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return "", "", fmt.Errorf("%w: expected <path::entity_key>", ErrInvalidEntitySelector)
	}

	parts := strings.SplitN(selector, "::", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("%w: expected <path::entity_key>, got %q", ErrInvalidEntitySelector, selector)
	}

	pathSpec = strings.TrimSpace(parts[0])
	entityKey = strings.TrimSpace(parts[1])
	if pathSpec == "" || entityKey == "" {
		return "", "", fmt.Errorf("%w: expected <path::entity_key>, got %q", ErrInvalidEntitySelector, selector)
	}

	return pathSpec, entityKey, nil
}

func firstParentHash(c *object.CommitObj) object.Hash {
	if c == nil || len(c.Parents) == 0 {
		return ""
	}
	return c.Parents[0]
}

func (r *Repo) entityBodyHashAtCommit(commitHash object.Hash, commit *object.CommitObj, relPath, entityKey string) (string, bool, error) {
	if commit == nil {
		return "", false, nil
	}

	blobData, found, err := r.blobDataAtTreePath(commit.TreeHash, relPath)
	if err != nil {
		return "", false, fmt.Errorf("blame: read %q at commit %s: %w", relPath, commitHash, err)
	}
	if !found {
		return "", false, nil
	}

	el, err := entity.Extract(relPath, blobData)
	if err != nil {
		return "", false, fmt.Errorf("blame: extract entities from %q at commit %s: %w", relPath, commitHash, err)
	}
	m := entity.BuildEntityMap(el)
	ent, ok := m[entityKey]
	if !ok {
		return "", false, nil
	}
	return ent.BodyHash, true, nil
}

func (r *Repo) blobDataAtTreePath(treeHash object.Hash, relPath string) ([]byte, bool, error) {
	entry, found, err := r.treeEntryAtPath(treeHash, relPath)
	if err != nil {
		return nil, false, err
	}
	if !found {
		return nil, false, nil
	}

	blob, err := r.Store.ReadBlob(entry.BlobHash)
	if err != nil {
		return nil, false, fmt.Errorf("read blob %s: %w", entry.BlobHash, err)
	}
	return blob.Data, true, nil
}

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
