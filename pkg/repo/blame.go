package repo

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

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
	locator := entityLocator{Path: relPath, Key: entityKey}

	for currentHash != "" && scanned < limit {
		scanned++

		commit, err := r.Store.ReadCommit(currentHash)
		if err != nil {
			return nil, fmt.Errorf("blame: read commit %s: %w", currentHash, err)
		}

		currentEntries, err := r.treeEntriesByPath(commit.TreeHash)
		if err != nil {
			return nil, fmt.Errorf("blame: %w", err)
		}

		currentCache := newCommitEntityCache(currentEntries)
		currentEntity, inCurrent, err := r.findEntityByLocator(currentCache, currentHash, locator)
		if err != nil {
			return nil, fmt.Errorf("blame: %w", err)
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

			parentEntries, err := r.treeEntriesByPath(parentCommit.TreeHash)
			if err != nil {
				return nil, fmt.Errorf("blame: %w", err)
			}
			parentCache := newCommitEntityCache(parentEntries)

			parentEntity, inParent, err := r.resolveParentEntity(
				currentEntity,
				parentHash,
				parentCache,
				changedCandidatePaths(parentEntries, currentEntries, ""),
			)
			if err != nil {
				return nil, fmt.Errorf("blame: %w", err)
			}
			if !inParent || parentEntity.BodyHash != currentEntity.BodyHash {
				return &EntityBlame{
					Path:       relPath,
					EntityKey:  entityKey,
					Author:     commit.Author,
					CommitHash: currentHash,
					Message:    commit.Message,
				}, nil
			}

			locator = parentEntity.Locator
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
