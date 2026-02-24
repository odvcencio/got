package repo

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/odvcencio/got/pkg/entity"
	"github.com/odvcencio/got/pkg/object"
)

type entityLocator struct {
	Path string
	Key  string
}

type trackedEntity struct {
	Locator   entityLocator
	Kind      entity.EntityKind
	DeclKind  string
	Receiver  string
	Signature string
	BodyHash  string
}

type commitEntityCache struct {
	entries       map[string]TreeFileEntry
	loadedPaths   map[string]bool
	pathExists    map[string]bool
	entitiesByRel map[string][]trackedEntity
}

func newCommitEntityCache(entries map[string]TreeFileEntry) *commitEntityCache {
	return &commitEntityCache{
		entries:       entries,
		loadedPaths:   make(map[string]bool),
		pathExists:    make(map[string]bool),
		entitiesByRel: make(map[string][]trackedEntity),
	}
}

type entityExtractionError struct {
	commitHash object.Hash
	relPath    string
	cause      error
}

func (e *entityExtractionError) Error() string {
	return fmt.Sprintf("extract entities from %q at commit %s: %v", e.relPath, e.commitHash, e.cause)
}

func (e *entityExtractionError) Unwrap() error {
	return e.cause
}

func isEntityExtractionError(err error) bool {
	var extractionErr *entityExtractionError
	return errors.As(err, &extractionErr)
}

func (r *Repo) entitiesAtPath(cache *commitEntityCache, commitHash object.Hash, relPath string) ([]trackedEntity, bool, error) {
	if cache.loadedPaths[relPath] {
		return cache.entitiesByRel[relPath], cache.pathExists[relPath], nil
	}

	cache.loadedPaths[relPath] = true

	entry, ok := cache.entries[relPath]
	if !ok {
		cache.pathExists[relPath] = false
		return nil, false, nil
	}
	cache.pathExists[relPath] = true

	blob, err := r.Store.ReadBlob(entry.BlobHash)
	if err != nil {
		return nil, true, fmt.Errorf("read blob %s (%s): %w", entry.BlobHash, relPath, err)
	}

	el, err := entity.Extract(relPath, blob.Data)
	if err != nil {
		return nil, true, &entityExtractionError{
			commitHash: commitHash,
			relPath:    relPath,
			cause:      err,
		}
	}

	entities := make([]trackedEntity, 0, len(el.Entities))
	for i := range el.Entities {
		ent := el.Entities[i]
		entities = append(entities, trackedEntity{
			Locator: entityLocator{
				Path: relPath,
				Key:  ent.IdentityKey(),
			},
			Kind:      ent.Kind,
			DeclKind:  ent.DeclKind,
			Receiver:  ent.Receiver,
			Signature: ent.Signature,
			BodyHash:  ent.BodyHash,
		})
	}

	cache.entitiesByRel[relPath] = entities
	return entities, true, nil
}

func (r *Repo) findEntityByLocator(cache *commitEntityCache, commitHash object.Hash, locator entityLocator) (*trackedEntity, bool, error) {
	entities, pathExists, err := r.entitiesAtPath(cache, commitHash, locator.Path)
	if err != nil {
		return nil, false, err
	}
	if !pathExists {
		return nil, false, nil
	}

	for i := range entities {
		if entities[i].Locator.Key == locator.Key {
			match := entities[i]
			return &match, true, nil
		}
	}
	return nil, false, nil
}

func (r *Repo) resolveParentEntity(current *trackedEntity, parentHash object.Hash, parentCache *commitEntityCache, changedPaths []string) (*trackedEntity, bool, error) {
	if current == nil {
		return nil, false, nil
	}

	exact, inParent, err := r.findEntityByLocator(parentCache, parentHash, current.Locator)
	if err != nil {
		return nil, false, err
	}
	if inParent {
		return exact, true, nil
	}

	if current.Kind != entity.KindDeclaration {
		return nil, false, nil
	}

	samePathEntities, samePathExists, err := r.entitiesAtPath(parentCache, parentHash, current.Locator.Path)
	if err != nil {
		return nil, false, err
	}
	if samePathExists {
		if match, ok := uniqueBodyContinuityMatch(samePathEntities, current); ok {
			return match, true, nil
		}
		if match, ok := uniqueSignatureContinuityMatch(samePathEntities, current); ok {
			return match, true, nil
		}
	}

	if len(changedPaths) == 0 {
		return nil, false, nil
	}

	currentExt := strings.ToLower(filepath.Ext(current.Locator.Path))
	candidatePaths := make([]string, 0, len(changedPaths))
	for _, changedPath := range changedPaths {
		if changedPath == current.Locator.Path {
			continue
		}
		entry, exists := parentCache.entries[changedPath]
		if !exists {
			continue
		}
		if entry.EntityListHash == "" {
			continue
		}
		if currentExt != "" && !strings.EqualFold(filepath.Ext(changedPath), currentExt) {
			continue
		}
		candidatePaths = append(candidatePaths, changedPath)
	}
	sort.Strings(candidatePaths)

	candidates := make([]trackedEntity, 0)
	for _, p := range candidatePaths {
		pathEntities, _, err := r.entitiesAtPath(parentCache, parentHash, p)
		if err != nil {
			return nil, false, err
		}
		candidates = append(candidates, pathEntities...)
	}

	if match, ok := uniqueBodyContinuityMatch(candidates, current); ok {
		return match, true, nil
	}
	if match, ok := uniqueSignatureContinuityMatch(candidates, current); ok {
		return match, true, nil
	}

	return nil, false, nil
}

func uniqueBodyContinuityMatch(candidates []trackedEntity, current *trackedEntity) (*trackedEntity, bool) {
	if current == nil || current.BodyHash == "" {
		return nil, false
	}

	var match *trackedEntity
	for i := range candidates {
		candidate := candidates[i]
		if !declarationContinuityComparable(candidate, current) {
			continue
		}
		if candidate.BodyHash != current.BodyHash {
			continue
		}
		if match != nil {
			return nil, false
		}
		candidateCopy := candidate
		match = &candidateCopy
	}
	if match == nil {
		return nil, false
	}
	return match, true
}

func uniqueSignatureContinuityMatch(candidates []trackedEntity, current *trackedEntity) (*trackedEntity, bool) {
	if current == nil {
		return nil, false
	}

	targetSig := normalizeContinuityText(current.Signature)
	if targetSig == "" {
		return nil, false
	}

	var match *trackedEntity
	for i := range candidates {
		candidate := candidates[i]
		if !declarationContinuityComparable(candidate, current) {
			continue
		}
		if normalizeContinuityText(candidate.Signature) != targetSig {
			continue
		}
		if match != nil {
			return nil, false
		}
		candidateCopy := candidate
		match = &candidateCopy
	}
	if match == nil {
		return nil, false
	}
	return match, true
}

func declarationContinuityComparable(candidate trackedEntity, current *trackedEntity) bool {
	if current == nil {
		return false
	}
	if candidate.Kind != entity.KindDeclaration || current.Kind != entity.KindDeclaration {
		return false
	}
	if candidate.DeclKind != current.DeclKind {
		return false
	}
	return candidate.Receiver == current.Receiver
}

func normalizeContinuityText(s string) string {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return ""
	}
	return strings.Join(strings.Fields(trimmed), " ")
}
