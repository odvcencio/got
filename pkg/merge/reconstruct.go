package merge

import "github.com/odvcencio/got/pkg/entity"

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
// wrapped in standard three-way conflict markers.
func Reconstruct(entities []ResolvedEntity) []byte {
	if len(entities) == 0 {
		return nil
	}

	var buf []byte
	for _, e := range entities {
		if !e.Conflict {
			buf = append(buf, e.Body...)
		} else {
			buf = append(buf, "<<<<<<< ours\n"...)
			buf = append(buf, e.OursBody...)
			buf = append(buf, "\n=======\n"...)
			buf = append(buf, e.TheirsBody...)
			buf = append(buf, "\n>>>>>>> theirs\n"...)
		}
	}
	return buf
}
