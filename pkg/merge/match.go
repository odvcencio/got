package merge

import (
	"fmt"

	"github.com/odvcencio/got/pkg/entity"
)

// Disposition describes the merge status of a matched entity.
type Disposition int

const (
	Unchanged    Disposition = iota
	OursOnly                 // ours modified, theirs unchanged
	TheirsOnly               // theirs modified, ours unchanged
	BothSame                 // both modified identically
	Conflict                 // both modified differently
	AddedOurs                // new entity in ours, not in base
	AddedTheirs              // new entity in theirs, not in base
	DeletedOurs              // deleted by ours
	DeletedTheirs            // deleted by theirs
	DeleteVsModify           // one deleted, other modified
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
	}
	return fmt.Sprintf("Disposition(%d)", int(d))
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
	baseMap := buildEntityMap(base)
	oursMap := buildEntityMap(ours)
	theirsMap := buildEntityMap(theirs)

	// Collect base keys in order.
	baseKeySet := map[string]bool{}
	var baseKeys []string
	for i := range base.Entities {
		key := base.Entities[i].IdentityKey()
		if !baseKeySet[key] {
			baseKeySet[key] = true
			baseKeys = append(baseKeys, key)
		}
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

	return result
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

// buildEntityMap indexes entities by their identity key.
// If duplicate keys exist, the last entity with that key wins.
func buildEntityMap(el *entity.EntityList) map[string]*entity.Entity {
	m := make(map[string]*entity.Entity, len(el.Entities))
	for i := range el.Entities {
		key := el.Entities[i].IdentityKey()
		m[key] = &el.Entities[i]
	}
	return m
}
