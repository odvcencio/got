package merge

import (
	"path/filepath"
	"strings"

	"github.com/odvcencio/got/pkg/diff3"
	"github.com/odvcencio/got/pkg/entity"
)

// MergeStats tracks counts of entity dispositions during a structural merge.
type MergeStats struct {
	TotalEntities  int
	Unchanged      int
	OursModified   int
	TheirsModified int
	BothModified   int
	Added          int
	Deleted        int
	Conflicts      int
}

// MergeResult holds the output of a structural three-way merge.
type MergeResult struct {
	Merged        []byte
	HasConflicts  bool
	ConflictCount int
	Stats         MergeStats
}

// MergeFiles performs a structural three-way merge of source files.
// It extracts entities from base, ours, and theirs, matches them by identity,
// resolves each entity's disposition, and reconstructs the merged output.
//
// Algorithm:
//  1. Extract entities from base, ours, theirs via entity.Extract
//  2. Match entities via MatchEntities
//  3. For each matched entity, build a ResolvedEntity based on disposition
//  4. Reconstruct output via Reconstruct
//  5. Count stats and conflicts
func MergeFiles(path string, base, ours, theirs []byte) (*MergeResult, error) {
	baseEL, err := entity.Extract(path, base)
	if err != nil {
		return nil, err
	}
	oursEL, err := entity.Extract(path, ours)
	if err != nil {
		return nil, err
	}
	theirsEL, err := entity.Extract(path, theirs)
	if err != nil {
		return nil, err
	}

	matches := MatchEntities(baseEL, oursEL, theirsEL)

	language := detectLanguage(path)

	var resolved []ResolvedEntity
	var stats MergeStats
	stats.TotalEntities = len(matches)

	for _, m := range matches {
		switch m.Disposition {
		case Unchanged:
			if m.Base != nil {
				resolved = append(resolved, ResolvedEntity{
					Entity: *m.Base,
				})
			}
			// If base is nil (both deleted from empty), skip.
			stats.Unchanged++

		case OursOnly:
			resolved = append(resolved, ResolvedEntity{
				Entity: *m.Ours,
			})
			stats.OursModified++

		case TheirsOnly:
			resolved = append(resolved, ResolvedEntity{
				Entity: *m.Theirs,
			})
			stats.TheirsModified++

		case BothSame:
			resolved = append(resolved, ResolvedEntity{
				Entity: *m.Ours,
			})
			stats.BothModified++

		case AddedOurs:
			resolved = append(resolved, ResolvedEntity{
				Entity: *m.Ours,
			})
			stats.Added++

		case AddedTheirs:
			resolved = append(resolved, ResolvedEntity{
				Entity: *m.Theirs,
			})
			stats.Added++

		case DeletedOurs, DeletedTheirs:
			// Skip — omit from output.
			stats.Deleted++

		case Conflict:
			re := resolveConflict(m, language)
			resolved = append(resolved, re)
			if re.Conflict {
				stats.Conflicts++
			} else {
				stats.BothModified++
			}

		case DeleteVsModify:
			re := resolveDeleteVsModify(m)
			resolved = append(resolved, re)
			stats.Conflicts++
		}
	}

	merged := Reconstruct(resolved)

	conflictCount := 0
	for _, re := range resolved {
		if re.Conflict {
			conflictCount++
		}
	}

	return &MergeResult{
		Merged:        merged,
		HasConflicts:  conflictCount > 0,
		ConflictCount: conflictCount,
		Stats:         stats,
	}, nil
}

// resolveConflict handles entities where both sides modified differently.
// For import blocks, it uses set-union merge. For declarations, it attempts
// a line-level diff3 merge; if that fails, it produces a conflict.
func resolveConflict(m MatchedEntity, language string) ResolvedEntity {
	oursBody := m.Ours.Body
	theirsBody := m.Theirs.Body

	// Import blocks get set-union merge.
	if m.Ours.Kind == entity.KindImportBlock {
		var baseBody []byte
		if m.Base != nil {
			baseBody = m.Base.Body
		}
		merged, _ := MergeImports(baseBody, oursBody, theirsBody, language)
		e := *m.Ours
		e.Body = merged
		return ResolvedEntity{Entity: e}
	}

	// Declarations and other entities: try diff3 line merge on entity bodies.
	var baseBody []byte
	if m.Base != nil {
		baseBody = m.Base.Body
	}
	result := diff3.Merge(baseBody, oursBody, theirsBody)
	if !result.HasConflicts {
		// Clean merge — use the diff3 result.
		e := *m.Ours
		e.Body = trimTrailingNewline(result.Merged)
		return ResolvedEntity{Entity: e}
	}

	// Unresolvable conflict — mark it.
	e := *m.Ours
	return ResolvedEntity{
		Entity:     e,
		Conflict:   true,
		OursBody:   oursBody,
		TheirsBody: theirsBody,
	}
}

// resolveDeleteVsModify handles the case where one side deleted and the other
// modified. This is always a conflict.
func resolveDeleteVsModify(m MatchedEntity) ResolvedEntity {
	var oursBody, theirsBody []byte
	if m.Ours != nil {
		oursBody = m.Ours.Body
	}
	if m.Theirs != nil {
		theirsBody = m.Theirs.Body
	}

	// Use base entity as the shell.
	e := *m.Base
	return ResolvedEntity{
		Entity:     e,
		Conflict:   true,
		OursBody:   oursBody,
		TheirsBody: theirsBody,
	}
}

// trimTrailingNewline removes a single trailing newline from merged diff3
// output, since entity bodies typically do not end with a trailing newline
// (the interstitial between entities carries that whitespace).
func trimTrailingNewline(b []byte) []byte {
	if len(b) > 0 && b[len(b)-1] == '\n' {
		return b[:len(b)-1]
	}
	return b
}

// detectLanguage returns the language name based on file extension.
func detectLanguage(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".rs":
		return "rust"
	case ".ts", ".tsx":
		return "typescript"
	case ".js", ".jsx":
		return "javascript"
	case ".c", ".h":
		return "c"
	case ".cpp", ".cc", ".cxx", ".hpp":
		return "cpp"
	case ".java":
		return "java"
	default:
		return ""
	}
}
