package merge

import (
	"fmt"

	"github.com/odvcencio/graft/pkg/entity"
)

// ResolvedEntity wraps an entity with its merge resolution.
// For non-conflict entities, Body contains the resolved content.
// For conflict entities, OursBody and TheirsBody hold the two divergent versions.
type ResolvedEntity struct {
	entity.Entity
	Conflict             bool
	OursBody, TheirsBody []byte
}

// Reconstruct assembles source bytes from a sequence of resolved entities.
// Clean entities contribute their Body directly; conflict entities are
// wrapped in standard three-way conflict markers annotated with the entity
// display name (e.g. "<<<<<<< ours (func ProcessOrder)").
func Reconstruct(entities []ResolvedEntity) []byte {
	if len(entities) == 0 {
		return nil
	}

	var buf []byte
	for _, e := range entities {
		if !e.Conflict {
			buf = append(buf, e.Body...)
		} else {
			annotation := entityAnnotation(&e.Entity)
			buf = append(buf, fmt.Sprintf("<<<<<<< ours%s\n", annotation)...)
			buf = append(buf, e.OursBody...)
			buf = append(buf, "\n=======\n"...)
			buf = append(buf, e.TheirsBody...)
			buf = append(buf, fmt.Sprintf("\n>>>>>>> theirs%s\n", annotation)...)
		}
	}
	return buf
}

// entityAnnotation returns a parenthesized entity display name for conflict
// markers (e.g. " (func ProcessOrder)"), or an empty string if the entity
// has no meaningful display name (interstitials, preambles).
func entityAnnotation(e *entity.Entity) string {
	if e.Kind != entity.KindDeclaration && e.Kind != entity.KindImportBlock {
		return ""
	}
	name := entity.EntityDisplayName(e)
	return fmt.Sprintf(" (%s)", name)
}
