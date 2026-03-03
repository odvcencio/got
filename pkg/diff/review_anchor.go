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
func ResolveCommentPosition(el *entity.EntityList, key string) (start, end int, ok bool) {
	m := entity.BuildEntityMap(el)
	e, found := m[key]
	if !found {
		return 0, 0, false
	}
	return e.StartLine, e.EndLine, true
}
