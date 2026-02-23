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
// stable order (base first, then new in ours, then new in theirs), and classifies
// each key's disposition based on presence and hash comparison across the three sides.
func MatchEntities(base, ours, theirs *entity.EntityList) []MatchedEntity {
	baseMap := buildEntityMap(base)
	oursMap := buildEntityMap(ours)
	theirsMap := buildEntityMap(theirs)

	// Collect keys in stable order: base entities first, then new in ours, then new in theirs.
	seen := map[string]bool{}
	var keys []string

	for i := range base.Entities {
		key := base.Entities[i].IdentityKey()
		if !seen[key] {
			seen[key] = true
			keys = append(keys, key)
		}
	}
	for i := range ours.Entities {
		key := ours.Entities[i].IdentityKey()
		if !seen[key] {
			seen[key] = true
			keys = append(keys, key)
		}
	}
	for i := range theirs.Entities {
		key := theirs.Entities[i].IdentityKey()
		if !seen[key] {
			seen[key] = true
			keys = append(keys, key)
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
