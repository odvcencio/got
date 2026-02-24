package diff

import "github.com/odvcencio/got/pkg/entity"

// ChangeType classifies what happened to an entity between two file revisions.
type ChangeType int

const (
	Added    ChangeType = iota // Entity exists only in the after revision.
	Removed                    // Entity exists only in the before revision.
	Modified                   // Entity exists in both revisions but its body changed.
)

// EntityChange records a single entity-level change between two revisions of a file.
type EntityChange struct {
	Type   ChangeType
	Key    string         // IdentityKey of the entity.
	Before *entity.Entity // nil for Added.
	After  *entity.Entity // nil for Removed.
}

// FileDiff holds the entity-level diff for a single file.
type FileDiff struct {
	Path    string
	Changes []EntityChange
}

// DiffFiles computes an entity-level diff between before and after revisions
// of the file at path. It extracts structural entities from both revisions,
// matches them by identity key, and reports additions, removals, and modifications.
func DiffFiles(path string, before, after []byte) (*FileDiff, error) {
	beforeList, err := entity.Extract(path, before)
	if err != nil {
		return nil, err
	}
	afterList, err := entity.Extract(path, after)
	if err != nil {
		return nil, err
	}

	// Build identity maps using shared matching logic.
	beforeMap := entity.BuildEntityMap(beforeList)
	afterMap := entity.BuildEntityMap(afterList)

	fd := &FileDiff{Path: path}

	// Walk before entities in order: detect Removed and Modified.
	for _, key := range entity.OrderedIdentityKeys(beforeList) {
		e := beforeMap[key]

		afterEnt, inAfter := afterMap[key]
		if !inAfter {
			fd.Changes = append(fd.Changes, EntityChange{
				Type:   Removed,
				Key:    key,
				Before: e,
			})
		} else if e.BodyHash != afterEnt.BodyHash {
			fd.Changes = append(fd.Changes, EntityChange{
				Type:   Modified,
				Key:    key,
				Before: e,
				After:  afterEnt,
			})
		}
		// Same hash → unchanged, skip.
	}

	// Walk after entities in order: detect Added (keys not in before).
	for _, key := range entity.OrderedIdentityKeys(afterList) {
		e := afterMap[key]

		if _, inBefore := beforeMap[key]; !inBefore {
			fd.Changes = append(fd.Changes, EntityChange{
				Type:  Added,
				Key:   key,
				After: e,
			})
		}
	}

	return fd, nil
}
