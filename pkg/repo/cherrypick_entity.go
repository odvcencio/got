package repo

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/odvcencio/got/pkg/entity"
	"github.com/odvcencio/got/pkg/merge"
	"github.com/odvcencio/got/pkg/object"
)

// CherryPickEntityResult captures the outcome of an entity-scoped cherry-pick.
type CherryPickEntityResult struct {
	Path         string
	EntityKey    string
	TargetCommit object.Hash
	CommitHash   object.Hash
	Message      string
}

type cherryPickFileState struct {
	data   []byte
	exists bool
	mode   string
}

// CherryPickEntity applies only the selected entity delta from target commit
// (first-parent diff target^..target) onto current HEAD via a structural
// three-way merge.
func (r *Repo) CherryPickEntity(selector string, targetHash object.Hash) (*CherryPickEntityResult, error) {
	targetHash = object.Hash(strings.TrimSpace(string(targetHash)))
	if targetHash == "" {
		return nil, fmt.Errorf("cherry-pick entity: target commit is required")
	}

	pathSpec, entityKey, err := parseEntitySelector(selector)
	if err != nil {
		return nil, err
	}

	relPath, err := r.repoRelPath(pathSpec)
	if err != nil {
		return nil, fmt.Errorf("cherry-pick entity: resolve path %q: %w", pathSpec, err)
	}
	relPath = filepath.ToSlash(filepath.Clean(relPath))
	if relPath == "." || strings.TrimSpace(relPath) == "" {
		return nil, fmt.Errorf("cherry-pick entity: path is required in selector %q", selector)
	}
	if isOutsideRepo(relPath) {
		return nil, fmt.Errorf("cherry-pick entity: path %q is outside repository", pathSpec)
	}
	selectorLabel := relPath + "::" + entityKey

	targetCommit, err := r.Store.ReadCommit(targetHash)
	if err != nil {
		return nil, fmt.Errorf("cherry-pick entity: read target commit %s: %w", targetHash, err)
	}
	if len(targetCommit.Parents) == 0 {
		return nil, fmt.Errorf("cherry-pick entity: commit %s has no parent; cannot derive delta", targetHash)
	}

	parentHash := targetCommit.Parents[0]
	parentCommit, err := r.Store.ReadCommit(parentHash)
	if err != nil {
		return nil, fmt.Errorf("cherry-pick entity: read parent commit %s: %w", parentHash, err)
	}

	headHash, err := r.ResolveRef("HEAD")
	if err != nil {
		return nil, fmt.Errorf("cherry-pick entity: resolve HEAD: %w", err)
	}
	headCommit, err := r.Store.ReadCommit(headHash)
	if err != nil {
		return nil, fmt.Errorf("cherry-pick entity: read HEAD commit %s: %w", headHash, err)
	}

	baseState, err := r.fileStateAtCommit(parentHash, parentCommit, relPath)
	if err != nil {
		return nil, err
	}
	targetState, err := r.fileStateAtCommit(targetHash, targetCommit, relPath)
	if err != nil {
		return nil, err
	}
	oursState, err := r.fileStateAtCommit(headHash, headCommit, relPath)
	if err != nil {
		return nil, err
	}
	if !oursState.exists {
		return nil, fmt.Errorf("cherry-pick entity: path %q does not exist at HEAD", relPath)
	}

	theirsData, err := buildSelectedEntityDeltaFile(relPath, selectorLabel, entityKey, baseState, targetState)
	if err != nil {
		return nil, err
	}

	mergeResult, err := merge.MergeFiles(relPath, baseState.data, oursState.data, theirsData)
	if err != nil {
		return nil, fmt.Errorf("cherry-pick entity: merge %q: %w", selectorLabel, err)
	}
	if mergeResult.HasConflicts {
		return nil, fmt.Errorf(
			"cherry-pick entity: conflict applying %q from %s onto HEAD %s",
			selectorLabel,
			targetHash,
			headHash,
		)
	}
	if bytes.Equal(mergeResult.Merged, oursState.data) {
		return nil, fmt.Errorf("cherry-pick entity: no changes to apply for %q", selectorLabel)
	}

	absPath := filepath.Join(r.RootDir, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return nil, fmt.Errorf("cherry-pick entity: mkdir %q: %w", filepath.Dir(absPath), err)
	}
	if err := os.WriteFile(absPath, mergeResult.Merged, filePermFromMode(oursState.mode)); err != nil {
		return nil, fmt.Errorf("cherry-pick entity: write %q: %w", relPath, err)
	}

	if err := r.Add([]string{relPath}); err != nil {
		return nil, fmt.Errorf("cherry-pick entity: stage %q: %w", relPath, err)
	}

	author := strings.TrimSpace(targetCommit.Author)
	if author == "" {
		author = "got-cherry-pick"
	}

	message := fmt.Sprintf("cherry-pick %s --entity %s", shortHash(targetHash), selectorLabel)
	commitHash, err := r.Commit(message, author)
	if err != nil {
		return nil, fmt.Errorf("cherry-pick entity: commit: %w", err)
	}

	return &CherryPickEntityResult{
		Path:         relPath,
		EntityKey:    entityKey,
		TargetCommit: targetHash,
		CommitHash:   commitHash,
		Message:      message,
	}, nil
}

func (r *Repo) fileStateAtCommit(commitHash object.Hash, commit *object.CommitObj, relPath string) (cherryPickFileState, error) {
	if commit == nil {
		return cherryPickFileState{}, nil
	}

	entry, found, err := r.treeEntryAtPath(commit.TreeHash, relPath)
	if err != nil {
		return cherryPickFileState{}, fmt.Errorf("cherry-pick entity: read %q at commit %s: %w", relPath, commitHash, err)
	}
	if !found {
		return cherryPickFileState{
			data:   nil,
			exists: false,
			mode:   object.TreeModeFile,
		}, nil
	}

	blob, err := r.Store.ReadBlob(entry.BlobHash)
	if err != nil {
		return cherryPickFileState{}, fmt.Errorf("cherry-pick entity: read blob %s (%s): %w", entry.BlobHash, relPath, err)
	}

	return cherryPickFileState{
		data:   blob.Data,
		exists: true,
		mode:   normalizeFileMode(entry.Mode),
	}, nil
}

func buildSelectedEntityDeltaFile(
	path, selectorLabel, entityKey string,
	baseState, targetState cherryPickFileState,
) ([]byte, error) {
	baseList, baseMap, err := extractEntityListAndMap(path, baseState)
	if err != nil {
		return nil, fmt.Errorf("cherry-pick entity: parent %w", err)
	}
	_, targetMap, err := extractEntityListAndMap(path, targetState)
	if err != nil {
		return nil, fmt.Errorf("cherry-pick entity: target %w", err)
	}

	baseEnt, inBase := baseMap[entityKey]
	targetEnt, inTarget := targetMap[entityKey]

	if !inBase && !inTarget {
		return nil, fmt.Errorf("%w: %s (not found in target commit or its parent)", ErrEntityNotFound, selectorLabel)
	}
	if !inBase && inTarget {
		return nil, fmt.Errorf("cherry-pick entity: %q was added in target commit; additions are ambiguous", selectorLabel)
	}
	if inBase && inTarget && baseEnt.BodyHash == targetEnt.BodyHash {
		return nil, fmt.Errorf("cherry-pick entity: target commit does not change %q", selectorLabel)
	}

	rebased := &entity.EntityList{
		Language: baseList.Language,
		Path:     baseList.Path,
		Entities: make([]entity.Entity, 0, len(baseList.Entities)),
	}

	applied := false
	for i := range baseList.Entities {
		ent := baseList.Entities[i]
		if ent.IdentityKey() != entityKey {
			rebased.Entities = append(rebased.Entities, ent)
			continue
		}
		applied = true
		if !inTarget {
			// Entity deleted in target commit.
			continue
		}

		updated := ent
		updated.Body = append([]byte(nil), targetEnt.Body...)
		updated.ComputeHash()
		rebased.Entities = append(rebased.Entities, updated)
	}
	if !applied {
		return nil, fmt.Errorf("cherry-pick entity: %q is ambiguous in parent commit", selectorLabel)
	}

	return entity.Reconstruct(rebased), nil
}

func extractEntityListAndMap(path string, state cherryPickFileState) (*entity.EntityList, map[string]*entity.Entity, error) {
	if !state.exists {
		return &entity.EntityList{Path: path}, map[string]*entity.Entity{}, nil
	}

	el, err := entity.Extract(path, state.data)
	if err != nil {
		return nil, nil, fmt.Errorf("extract entities from %q: %w", path, err)
	}
	return el, entity.BuildEntityMap(el), nil
}

func shortHash(h object.Hash) string {
	s := string(h)
	if len(s) > 8 {
		return s[:8]
	}
	return s
}
