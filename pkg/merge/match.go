package merge

import (
	"fmt"

	"github.com/odvcencio/graft/pkg/entity"
)

// Disposition describes the merge status of a matched entity.
type Disposition int

const (
	Unchanged      Disposition = iota
	OursOnly                   // ours modified, theirs unchanged
	TheirsOnly                 // theirs modified, ours unchanged
	BothSame                   // both modified identically
	Conflict                   // both modified differently
	AddedOurs                  // new entity in ours, not in base
	AddedTheirs                // new entity in theirs, not in base
	DeletedOurs                // deleted by ours
	DeletedTheirs              // deleted by theirs
	DeleteVsModify             // one deleted, other modified
	RenamedOurs                // entity renamed by ours (deleted old key, added new key)
	RenamedTheirs              // entity renamed by theirs (deleted old key, added new key)
)

func (d Disposition) String() string {
	switch d {
	case Unchanged:
		return "Unchanged"
	case OursOnly:
		return "OursOnly"
	case TheirsOnly:
		return "TheirsOnly"
	case BothSame:
		return "BothSame"
	case Conflict:
		return "Conflict"
	case AddedOurs:
		return "AddedOurs"
	case AddedTheirs:
		return "AddedTheirs"
	case DeletedOurs:
		return "DeletedOurs"
	case DeletedTheirs:
		return "DeletedTheirs"
	case DeleteVsModify:
		return "DeleteVsModify"
	case RenamedOurs:
		return "RenamedOurs"
	case RenamedTheirs:
		return "RenamedTheirs"
	}
	return fmt.Sprintf("Disposition(%d)", int(d))
}

// Conflict type constants used in EntityConflictDetail.Type and across the merge pipeline.
const (
	ConflictTypeBothModified   = "both_modified"
	ConflictTypeDeleteVsModify = "delete_vs_modify"
	ConflictTypeRenameConflict = "rename_conflict"
)

// EntityConflictDetail describes a single entity-level conflict within a file merge.
type EntityConflictDetail struct {
	Key      string            // identity key
	Name     string            // display name ("func ProcessOrder")
	Kind     entity.EntityKind // Declaration, ImportBlock, etc.
	DeclKind string            // "function_definition"
	Type     string            // ConflictTypeBothModified or ConflictTypeDeleteVsModify
}

// MatchedEntity pairs an entity key with its three-way merge disposition.
type MatchedEntity struct {
	Key         string
	Disposition Disposition
	Base        *entity.Entity
	Ours        *entity.Entity
	Theirs      *entity.Entity
}

// MatchEntities performs three-way entity matching between base, ours, and theirs.
// It builds identity-keyed maps for each side, collects all unique keys in a
// stable order, and classifies each key's disposition based on presence and hash
// comparison across the three sides.
//
// New entities from ours and theirs are inserted at their correct positions
// relative to the base ordering (anchored after their nearest preceding base key)
// rather than being appended at the end.
func MatchEntities(base, ours, theirs *entity.EntityList) []MatchedEntity {
	baseMap := entity.BuildEntityMap(base)
	oursMap := entity.BuildEntityMap(ours)
	theirsMap := entity.BuildEntityMap(theirs)

	// Collect base keys in order.
	baseKeySet := map[string]bool{}
	baseKeys := entity.OrderedIdentityKeys(base)
	for _, key := range baseKeys {
		baseKeySet[key] = true
	}

	// For ours and theirs, find new keys (not in base) and determine their
	// anchor — the nearest preceding key that IS in base. Entities inserted
	// before any base entity get anchor "" (insert at front).
	oursInsertions := collectInsertions(ours, baseKeySet)
	theirsInsertions := collectInsertions(theirs, baseKeySet)

	// Build the merged key sequence: walk base keys in order, interleaving
	// new entities at their anchor points.
	seen := map[string]bool{}
	var keys []string

	// Insert entities anchored before any base entity (anchor="").
	for _, k := range oursInsertions[""] {
		if !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	for _, k := range theirsInsertions[""] {
		if !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}

	for _, bk := range baseKeys {
		if !seen[bk] {
			seen[bk] = true
			keys = append(keys, bk)
		}
		// Insert entities anchored after this base key.
		for _, k := range oursInsertions[bk] {
			if !seen[k] {
				seen[k] = true
				keys = append(keys, k)
			}
		}
		for _, k := range theirsInsertions[bk] {
			if !seen[k] {
				seen[k] = true
				keys = append(keys, k)
			}
		}
	}

	result := make([]MatchedEntity, 0, len(keys))
	for _, key := range keys {
		b := baseMap[key]
		o := oursMap[key]
		t := theirsMap[key]

		m := MatchedEntity{
			Key:    key,
			Base:   b,
			Ours:   o,
			Theirs: t,
		}
		m.Disposition = classify(b, o, t)
		result = append(result, m)
	}

	return applyRenameDetection(result)
}

// collectInsertions finds keys in el that are NOT in baseKeySet and groups them
// by their anchor — the nearest preceding key that IS in baseKeySet.
// Returns a map from anchor key to ordered slice of new keys to insert after it.
// The special anchor "" means the entity appears before any base entity.
func collectInsertions(el *entity.EntityList, baseKeySet map[string]bool) map[string][]string {
	insertions := map[string][]string{}
	seen := map[string]bool{}
	anchor := "" // before any base entity
	for i := range el.Entities {
		key := el.Entities[i].IdentityKey()
		if seen[key] {
			continue
		}
		seen[key] = true
		if baseKeySet[key] {
			anchor = key
		} else {
			insertions[anchor] = append(insertions[anchor], key)
		}
	}
	return insertions
}

// renameThreshold is the minimum line similarity required for a non-exact
// rename detection. Bodies with identical hashes always qualify (similarity 1.0).
const renameThreshold = 0.80

// applyRenameDetection runs rename detection as a post-processing step on
// the initial match results. It finds renames among unmatched entities and
// replaces separate delete+add entries with single renamed entries.
//
// Three rename scenarios are handled:
//  1. Ours renamed: DeletedOurs + AddedOurs => RenamedOurs
//  2. Theirs renamed: DeletedTheirs + AddedTheirs => RenamedTheirs
//  3. Both renamed same entity to different names => Conflict
func applyRenameDetection(matches []MatchedEntity) []MatchedEntity {
	// Index matches by key for fast lookup.
	byKey := make(map[string]int, len(matches))
	for i, m := range matches {
		byKey[m.Key] = i
	}

	// Collect candidates for rename detection, grouped by scenario.
	// Ours side: DeletedOurs base entities paired with AddedOurs entities.
	oursDeleted := map[string]*entity.Entity{}   // key -> base entity
	oursAdded := map[string]*entity.Entity{}     // key -> ours entity
	theirsDeleted := map[string]*entity.Entity{} // key -> base entity
	theirsAdded := map[string]*entity.Entity{}   // key -> theirs entity
	bothDeleted := map[string]*entity.Entity{}   // key -> base entity (both sides deleted)

	for _, m := range matches {
		switch m.Disposition {
		case DeletedOurs:
			if m.Base != nil && m.Base.Kind == entity.KindDeclaration {
				oursDeleted[m.Key] = m.Base
			}
		case DeletedTheirs:
			if m.Base != nil && m.Base.Kind == entity.KindDeclaration {
				theirsDeleted[m.Key] = m.Base
			}
		case AddedOurs:
			if m.Ours != nil && m.Ours.Kind == entity.KindDeclaration {
				oursAdded[m.Key] = m.Ours
			}
		case AddedTheirs:
			if m.Theirs != nil && m.Theirs.Kind == entity.KindDeclaration {
				theirsAdded[m.Key] = m.Theirs
			}
		case Unchanged:
			// Both sides deleted: base exists, ours=nil, theirs=nil.
			if m.Base != nil && m.Ours == nil && m.Theirs == nil && m.Base.Kind == entity.KindDeclaration {
				bothDeleted[m.Key] = m.Base
			}
		}
	}

	// Track which keys are consumed by rename detection.
	consumed := map[string]bool{}

	// Scenario 1: Ours renamed (DeletedOurs + AddedOurs).
	oursRenames := DetectRenames(oursDeleted, oursAdded, renameThreshold)
	for _, r := range oursRenames {
		delIdx := byKey[r.OldKey]
		addIdx := byKey[r.NewKey]
		// Replace the added entry with a RenamedOurs entry that carries all three sides.
		matches[addIdx].Disposition = RenamedOurs
		matches[addIdx].Base = matches[delIdx].Base
		// Ours is already set from the AddedOurs entry.
		// Theirs comes from the deleted entry (theirs still had the old version).
		matches[addIdx].Theirs = matches[delIdx].Theirs
		consumed[r.OldKey] = true
	}

	// Scenario 2: Theirs renamed (DeletedTheirs + AddedTheirs).
	theirsRenames := DetectRenames(theirsDeleted, theirsAdded, renameThreshold)
	for _, r := range theirsRenames {
		delIdx := byKey[r.OldKey]
		addIdx := byKey[r.NewKey]
		// Replace the added entry with a RenamedTheirs entry.
		matches[addIdx].Disposition = RenamedTheirs
		matches[addIdx].Base = matches[delIdx].Base
		// Theirs is already set from the AddedTheirs entry.
		// Ours comes from the deleted entry (ours still had the old version).
		matches[addIdx].Ours = matches[delIdx].Ours
		consumed[r.OldKey] = true
	}

	// Remove added entries already consumed by Scenarios 1 and 2 so
	// Scenario 3 does not double-match them.
	for _, r := range oursRenames {
		delete(oursAdded, r.NewKey)
	}
	for _, r := range theirsRenames {
		delete(theirsAdded, r.NewKey)
	}

	// Scenario 3: Both sides renamed the same entity.
	// Check bothDeleted entities against both AddedOurs and AddedTheirs.
	oursFromBoth := DetectRenames(bothDeleted, oursAdded, renameThreshold)
	theirsFromBoth := DetectRenames(bothDeleted, theirsAdded, renameThreshold)

	// Build a map of bothDeleted keys to their ours and theirs rename targets.
	bothOursTarget := map[string]RenameCandidate{}
	for _, r := range oursFromBoth {
		bothOursTarget[r.OldKey] = r
	}
	bothTheirsTarget := map[string]RenameCandidate{}
	for _, r := range theirsFromBoth {
		bothTheirsTarget[r.OldKey] = r
	}

	for baseKey := range bothDeleted {
		oursR, hasOurs := bothOursTarget[baseKey]
		theirsR, hasTheirs := bothTheirsTarget[baseKey]

		switch {
		case hasOurs && hasTheirs:
			if oursR.NewKey == theirsR.NewKey {
				// Both renamed to the same name — BothSame.
				addIdx := byKey[oursR.NewKey]
				matches[addIdx].Disposition = BothSame
				matches[addIdx].Base = matches[byKey[baseKey]].Base
				consumed[baseKey] = true
			} else {
				// Renamed to different names — Conflict.
				// Place the conflict at the ours rename target position.
				oursIdx := byKey[oursR.NewKey]
				theirsIdx := byKey[theirsR.NewKey]
				matches[oursIdx].Disposition = Conflict
				matches[oursIdx].Base = matches[byKey[baseKey]].Base
				// Ours is already set from the AddedOurs entry.
				matches[oursIdx].Theirs = matches[theirsIdx].Theirs
				consumed[baseKey] = true
				consumed[theirsR.NewKey] = true
			}
		case hasOurs && !hasTheirs:
			// Only ours renamed from a both-deleted base — treat as RenamedOurs.
			addIdx := byKey[oursR.NewKey]
			matches[addIdx].Disposition = RenamedOurs
			matches[addIdx].Base = matches[byKey[baseKey]].Base
			consumed[baseKey] = true
		case !hasOurs && hasTheirs:
			// Only theirs renamed from a both-deleted base — treat as RenamedTheirs.
			addIdx := byKey[theirsR.NewKey]
			matches[addIdx].Disposition = RenamedTheirs
			matches[addIdx].Base = matches[byKey[baseKey]].Base
			consumed[baseKey] = true
		}
	}

	// Filter out consumed entries (the old keys that were replaced by renames).
	filtered := make([]MatchedEntity, 0, len(matches))
	for _, m := range matches {
		if consumed[m.Key] {
			continue
		}
		filtered = append(filtered, m)
	}

	return filtered
}

// classify determines the Disposition for an entity across three revisions.
func classify(base, ours, theirs *entity.Entity) Disposition {
	inBase := base != nil
	inOurs := ours != nil
	inTheirs := theirs != nil

	switch {
	// Present in all three
	case inBase && inOurs && inTheirs:
		oursChanged := ours.BodyHash != base.BodyHash
		theirsChanged := theirs.BodyHash != base.BodyHash
		switch {
		case !oursChanged && !theirsChanged:
			return Unchanged
		case oursChanged && !theirsChanged:
			return OursOnly
		case !oursChanged && theirsChanged:
			return TheirsOnly
		case ours.BodyHash == theirs.BodyHash:
			return BothSame
		default:
			return Conflict
		}

	// In base and ours, not theirs: theirs deleted
	case inBase && inOurs && !inTheirs:
		if ours.BodyHash != base.BodyHash {
			return DeleteVsModify
		}
		return DeletedTheirs

	// In base and theirs, not ours: ours deleted
	case inBase && !inOurs && inTheirs:
		if theirs.BodyHash != base.BodyHash {
			return DeleteVsModify
		}
		return DeletedOurs

	// In base only: both deleted (treat as Unchanged since both agree)
	case inBase && !inOurs && !inTheirs:
		return Unchanged

	// Not in base, in ours only
	case !inBase && inOurs && !inTheirs:
		return AddedOurs

	// Not in base, in theirs only
	case !inBase && !inOurs && inTheirs:
		return AddedTheirs

	// Not in base, in both ours and theirs
	case !inBase && inOurs && inTheirs:
		if ours.BodyHash == theirs.BodyHash {
			return BothSame
		}
		return Conflict
	}

	// Should not reach here
	return Unchanged
}
