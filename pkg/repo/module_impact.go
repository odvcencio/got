package repo

import (
	"fmt"
	"sort"
	"strings"

	"github.com/odvcencio/graft/pkg/object"
)

// ModuleEntityChange describes a single entity-level change between two
// versions of a module (old commit vs new commit).
type ModuleEntityChange struct {
	EntityKey  string // full identity key from the entity object (e.g. "function:DoWork")
	EntityName string // human-readable name (e.g. "DoWork")
	ChangeType string // "added", "modified", or "removed"
	FilePath   string // path within the module where the entity lives
}

// ImpactedEntity describes an entity in the parent repository that may be
// affected by a module change.
type ImpactedEntity struct {
	EntityKey  string // identity key of the parent entity
	EntityName string // human-readable name
	FilePath   string // path in the parent repo
	Reason     string // e.g. "references function DoWork from module mylib"
}

// ModuleImpactReport is the output of a cross-repository entity impact
// analysis. It lists what changed in the module and which entities in the
// parent repository reference those changes.
type ModuleImpactReport struct {
	ModuleName string
	OldCommit  object.Hash
	NewCommit  object.Hash
	Changes    []ModuleEntityChange // what changed in the module
	Impacted   []ImpactedEntity     // what in the parent repo is affected
}

// ModuleImpact performs entity-level dependency analysis between the old and
// new commits of a module. It diffs the two versions for entity changes, then
// searches the parent repo's source files for references to any changed
// entity names.
//
// The algorithm:
//  1. Look up the module and its lock state (old commit = HEAD, new commit = locked).
//  2. Diff old/new module commits for entity changes using the object store.
//  3. Search the parent repo's committed tree for references to changed names.
//  4. Build and return the impact report.
func (r *Repo) ModuleImpact(moduleName string) (*ModuleImpactReport, error) {
	mod, err := r.GetModule(moduleName)
	if err != nil {
		return nil, err
	}

	// Determine old and new commits.
	// The HEAD file in module metadata records what was previously checked out.
	// The lock file records the latest target commit.
	oldCommit, err := r.ReadModuleHEAD(moduleName)
	if err != nil {
		return nil, fmt.Errorf("module impact: read module HEAD: %w", err)
	}
	newCommit := mod.Commit

	if newCommit == "" {
		return nil, fmt.Errorf("module impact: module %q has no locked commit (run 'graft module update' first)", moduleName)
	}

	report := &ModuleImpactReport{
		ModuleName: moduleName,
		OldCommit:  oldCommit,
		NewCommit:  newCommit,
	}

	// If old and new are the same, there are no changes.
	if oldCommit == newCommit {
		return report, nil
	}

	// Step 1-3: Diff the two module versions for entity changes.
	changes, err := r.diffModuleEntities(oldCommit, newCommit)
	if err != nil {
		return nil, fmt.Errorf("module impact: diff entities: %w", err)
	}
	report.Changes = changes

	if len(changes) == 0 {
		return report, nil
	}

	// Step 4-6: Search parent repo for references to changed entity names.
	impacted, err := r.findImpactedEntities(moduleName, changes)
	if err != nil {
		return nil, fmt.Errorf("module impact: find impacted: %w", err)
	}
	report.Impacted = impacted

	return report, nil
}

// diffModuleEntities compares entity lists between two module commits and
// returns entity-level changes. If oldCommit is empty, all entities in
// newCommit are treated as additions.
func (r *Repo) diffModuleEntities(oldCommit, newCommit object.Hash) ([]ModuleEntityChange, error) {
	// Read new commit tree.
	newCommitObj, err := r.Store.ReadCommit(newCommit)
	if err != nil {
		return nil, fmt.Errorf("read new commit %s: %w", newCommit, err)
	}
	newEntries, err := r.FlattenTree(newCommitObj.TreeHash)
	if err != nil {
		return nil, fmt.Errorf("flatten new tree: %w", err)
	}

	newByPath := make(map[string]TreeFileEntry, len(newEntries))
	for _, e := range newEntries {
		newByPath[e.Path] = e
	}

	// Handle initial state (no old commit).
	isInitial := string(oldCommit) == "" || string(oldCommit) == zeroHash
	var oldByPath map[string]TreeFileEntry
	if !isInitial {
		oldCommitObj, err := r.Store.ReadCommit(oldCommit)
		if err != nil {
			return nil, fmt.Errorf("read old commit %s: %w", oldCommit, err)
		}
		oldEntries, err := r.FlattenTree(oldCommitObj.TreeHash)
		if err != nil {
			return nil, fmt.Errorf("flatten old tree: %w", err)
		}
		oldByPath = make(map[string]TreeFileEntry, len(oldEntries))
		for _, e := range oldEntries {
			oldByPath[e.Path] = e
		}
	} else {
		oldByPath = make(map[string]TreeFileEntry)
	}

	// Collect all unique paths.
	allPaths := make(map[string]struct{})
	for p := range oldByPath {
		allPaths[p] = struct{}{}
	}
	for p := range newByPath {
		allPaths[p] = struct{}{}
	}

	var sortedPaths []string
	for p := range allPaths {
		sortedPaths = append(sortedPaths, p)
	}
	sort.Strings(sortedPaths)

	var changes []ModuleEntityChange
	for _, path := range sortedPaths {
		oldEntry, inOld := oldByPath[path]
		newEntry, inNew := newByPath[path]

		oldHasEntities := inOld && oldEntry.EntityListHash != ""
		newHasEntities := inNew && newEntry.EntityListHash != ""
		if !oldHasEntities && !newHasEntities {
			continue
		}

		// Skip if entity list hash hasn't changed.
		if inOld && inNew && oldEntry.EntityListHash == newEntry.EntityListHash {
			continue
		}

		// Build old entity key -> (hash, name) map.
		oldEntityMap, err := r.buildEntityDetailMap(oldEntry.EntityListHash, oldHasEntities)
		if err != nil {
			return nil, fmt.Errorf("read old entities for %s: %w", path, err)
		}

		// Build new entity key -> (hash, name) map.
		newEntityMap, err := r.buildEntityDetailMap(newEntry.EntityListHash, newHasEntities)
		if err != nil {
			return nil, fmt.Errorf("read new entities for %s: %w", path, err)
		}

		// Compare: key in new but not old = added.
		for key, newDetail := range newEntityMap {
			oldDetail, inOldMap := oldEntityMap[key]
			if !inOldMap {
				changes = append(changes, ModuleEntityChange{
					EntityKey:  key,
					EntityName: newDetail.name,
					ChangeType: "added",
					FilePath:   path,
				})
			} else if oldDetail.hash != newDetail.hash {
				changes = append(changes, ModuleEntityChange{
					EntityKey:  key,
					EntityName: newDetail.name,
					ChangeType: "modified",
					FilePath:   path,
				})
			}
		}

		// Key in old but not new = removed.
		for key, oldDetail := range oldEntityMap {
			if _, inNewMap := newEntityMap[key]; !inNewMap {
				changes = append(changes, ModuleEntityChange{
					EntityKey:  key,
					EntityName: oldDetail.name,
					ChangeType: "removed",
					FilePath:   path,
				})
			}
		}
	}

	// Sort changes for deterministic output.
	sort.Slice(changes, func(i, j int) bool {
		if changes[i].FilePath != changes[j].FilePath {
			return changes[i].FilePath < changes[j].FilePath
		}
		return changes[i].EntityKey < changes[j].EntityKey
	})

	return changes, nil
}

// entityDetail holds an entity's object hash and human-readable name.
type entityDetail struct {
	hash object.Hash
	name string
}

// buildEntityDetailMap reads an entity list and builds a map from entity key
// to entityDetail (hash + name).
func (r *Repo) buildEntityDetailMap(entityListHash object.Hash, hasEntities bool) (map[string]entityDetail, error) {
	result := make(map[string]entityDetail)
	if !hasEntities || entityListHash == "" {
		return result, nil
	}

	el, err := r.Store.ReadEntityList(entityListHash)
	if err != nil {
		return nil, err
	}

	for _, ref := range el.EntityRefs {
		ent, err := r.Store.ReadEntity(ref)
		if err != nil {
			return nil, fmt.Errorf("read entity %s: %w", ref, err)
		}
		key := ent.Kind + ":" + ent.Name
		result[key] = entityDetail{hash: ref, name: ent.Name}
	}
	return result, nil
}

// findImpactedEntities searches the parent repo's HEAD tree for entities
// that reference any of the changed entity names from the module.
func (r *Repo) findImpactedEntities(moduleName string, changes []ModuleEntityChange) ([]ImpactedEntity, error) {
	// Build a set of entity names to search for (only non-empty, declaration-like names).
	searchNames := make(map[string]ModuleEntityChange)
	for _, c := range changes {
		name := c.EntityName
		if name == "" {
			continue
		}
		// Skip very short names to avoid false positives (e.g., single letters).
		if len(name) < 2 {
			continue
		}
		searchNames[name] = c
	}

	if len(searchNames) == 0 {
		return nil, nil
	}

	// Read the parent repo's HEAD commit tree.
	headHash, err := r.ResolveRef("HEAD")
	if err != nil {
		// No commits yet — nothing to search.
		return nil, nil
	}
	headCommit, err := r.Store.ReadCommit(headHash)
	if err != nil {
		return nil, fmt.Errorf("read HEAD commit: %w", err)
	}

	parentEntries, err := r.FlattenTree(headCommit.TreeHash)
	if err != nil {
		return nil, fmt.Errorf("flatten HEAD tree: %w", err)
	}

	// Get the module's path prefix so we can exclude the module's own files.
	mod, err := r.GetModule(moduleName)
	if err != nil {
		return nil, err
	}
	modulePathPrefix := mod.Path
	if modulePathPrefix != "" && !strings.HasSuffix(modulePathPrefix, "/") {
		modulePathPrefix += "/"
	}

	var impacted []ImpactedEntity

	for _, entry := range parentEntries {
		// Skip the module's own files.
		if modulePathPrefix != "" && strings.HasPrefix(entry.Path, modulePathPrefix) {
			continue
		}

		// Skip files without entity lists (non-source files).
		if entry.EntityListHash == "" {
			continue
		}

		// Read the blob content to search for references.
		blob, err := r.Store.ReadBlob(entry.BlobHash)
		if err != nil {
			continue // skip unreadable blobs
		}
		content := string(blob.Data)

		// Also read entity list to get entity names for the impacted entries.
		el, err := r.Store.ReadEntityList(entry.EntityListHash)
		if err != nil {
			continue
		}

		// For each entity in this file, check if its body references any
		// changed module entity.
		for _, ref := range el.EntityRefs {
			ent, err := r.Store.ReadEntity(ref)
			if err != nil {
				continue
			}

			// Skip non-declaration entities (preamble, interstitial, imports).
			if ent.Kind != "declaration" {
				continue
			}

			entBody := string(ent.Body)

			for searchName, change := range searchNames {
				if containsReference(entBody, searchName) {
					impacted = append(impacted, ImpactedEntity{
						EntityKey:  ent.Kind + ":" + ent.Name,
						EntityName: ent.Name,
						FilePath:   entry.Path,
						Reason:     fmt.Sprintf("references %s %s from module %s", change.ChangeType, searchName, moduleName),
					})
					break // only report each parent entity once per file
				}
			}
		}

		// Also do a file-level check: if the file references changed names
		// but has no entity-level matches (e.g. in import blocks or preamble),
		// report the file itself.
		if len(impacted) == 0 || impacted[len(impacted)-1].FilePath != entry.Path {
			for searchName, change := range searchNames {
				if containsReference(content, searchName) {
					impacted = append(impacted, ImpactedEntity{
						EntityKey:  "file:" + entry.Path,
						EntityName: entry.Path,
						FilePath:   entry.Path,
						Reason:     fmt.Sprintf("file references %s %s from module %s", change.ChangeType, searchName, moduleName),
					})
					break // one file-level entry per file
				}
			}
		}
	}

	// Sort for deterministic output.
	sort.Slice(impacted, func(i, j int) bool {
		if impacted[i].FilePath != impacted[j].FilePath {
			return impacted[i].FilePath < impacted[j].FilePath
		}
		return impacted[i].EntityKey < impacted[j].EntityKey
	})

	// Deduplicate: same entity key + file path should only appear once.
	impacted = deduplicateImpacted(impacted)

	return impacted, nil
}

// containsReference checks if content contains a reference to the given name.
// It uses word-boundary-aware matching to reduce false positives: the name
// must not be preceded or followed by an identifier character (letter, digit,
// underscore).
func containsReference(content, name string) bool {
	idx := 0
	for {
		pos := strings.Index(content[idx:], name)
		if pos < 0 {
			return false
		}
		absPos := idx + pos
		endPos := absPos + len(name)

		// Check word boundary before the match.
		if absPos > 0 && isIdentChar(content[absPos-1]) {
			idx = absPos + 1
			continue
		}

		// Check word boundary after the match.
		if endPos < len(content) && isIdentChar(content[endPos]) {
			idx = absPos + 1
			continue
		}

		return true
	}
}

// isIdentChar returns true if c is a letter, digit, or underscore.
func isIdentChar(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_'
}

// deduplicateImpacted removes duplicate entries (same EntityKey + FilePath).
// The input must be sorted.
func deduplicateImpacted(items []ImpactedEntity) []ImpactedEntity {
	if len(items) <= 1 {
		return items
	}
	result := make([]ImpactedEntity, 0, len(items))
	result = append(result, items[0])
	for i := 1; i < len(items); i++ {
		prev := result[len(result)-1]
		if items[i].EntityKey == prev.EntityKey && items[i].FilePath == prev.FilePath {
			continue
		}
		result = append(result, items[i])
	}
	return result
}
