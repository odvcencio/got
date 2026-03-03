package diff

import "github.com/odvcencio/graft/pkg/entity"

// ReviewComment anchors a review comment on an entity identity key rather than
// a line number. This allows comments to survive rebases as long as the entity
// exists — the key remains stable even if lines shift.
type ReviewComment struct {
	EntityKey string
	Body      string
}

// ResolveCommentPosition looks up an entity by its identity key in the given
// EntityList and returns its current line range. If the entity is not found,
// ok is false and the returned line numbers are zero.
//
// This is a convenience wrapper that builds the entity map on each call.
// For resolving multiple comments against the same file, use
// ResolveCommentPositionFromMap with a pre-built map to avoid repeated work.
func ResolveCommentPosition(el *entity.EntityList, key string) (start, end int, ok bool) {
	m := entity.BuildEntityMap(el)
	return ResolveCommentPositionFromMap(m, key)
}

// ResolveCommentPositionFromMap looks up an entity by its identity key in a
// pre-built entity map and returns its current line range. Build the map once
// with entity.BuildEntityMap and reuse it when resolving multiple comments.
func ResolveCommentPositionFromMap(m map[string]*entity.Entity, key string) (start, end int, ok bool) {
	e, found := m[key]
	if !found {
		return 0, 0, false
	}
	return e.StartLine, e.EndLine, true
}
