package merge

import (
	"bytes"
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
	// Structural merge is undefined for binary content. Use safe binary-level
	// semantics instead of attempting parser-driven extraction.
	if isBinaryContent(base) || isBinaryContent(ours) || isBinaryContent(theirs) {
		return mergeBinaryFallback(base, ours, theirs), nil
	}

	baseEL, baseErr := entity.Extract(path, base)
	oursEL, oursErr := entity.Extract(path, ours)
	theirsEL, theirsErr := entity.Extract(path, theirs)
	if baseErr != nil || oursErr != nil || theirsErr != nil {
		// If structural extraction fails (unsupported grammar or parse failure),
		// fall back to line-level diff3 merge for text files.
		return mergeTextFallback(base, ours, theirs), nil
	}
	if !hasDeclaration(baseEL) || !hasDeclaration(oursEL) || !hasDeclaration(theirsEL) {
		// If any side has no declaration entities, structural matching becomes
		// unreliable. Prefer a safe line-level three-way merge.
		return mergeTextFallback(base, ours, theirs), nil
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
			// Interstitials whose identity keys shifted because a new entity
			// was inserted between their neighbors are not truly deleted —
			// keep the base version to preserve whitespace separators.
			if m.Base != nil && m.Base.Kind == entity.KindInterstitial {
				resolved = append(resolved, ResolvedEntity{
					Entity: *m.Base,
				})
				stats.Unchanged++
			} else {
				// Real deletion — omit from output.
				stats.Deleted++
			}

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

func mergeTextFallback(base, ours, theirs []byte) *MergeResult {
	result := diff3.Merge(base, ours, theirs)
	merged, conflictCount := resolveTextConflicts(result)
	stats := MergeStats{TotalEntities: 1}
	if conflictCount > 0 {
		stats.Conflicts = conflictCount
	} else {
		stats.BothModified = 1
	}
	return &MergeResult{
		Merged:        merged,
		HasConflicts:  conflictCount > 0,
		ConflictCount: conflictCount,
		Stats:         stats,
	}
}

func mergeBinaryFallback(base, ours, theirs []byte) *MergeResult {
	stats := MergeStats{TotalEntities: 1}
	switch {
	case bytes.Equal(ours, theirs):
		stats.Unchanged = 1
		return &MergeResult{
			Merged: append([]byte(nil), ours...),
			Stats:  stats,
		}
	case bytes.Equal(base, ours):
		stats.TheirsModified = 1
		return &MergeResult{
			Merged: append([]byte(nil), theirs...),
			Stats:  stats,
		}
	case bytes.Equal(base, theirs):
		stats.OursModified = 1
		return &MergeResult{
			Merged: append([]byte(nil), ours...),
			Stats:  stats,
		}
	default:
		// Keep ours bytes intact and force an explicit conflict state.
		stats.Conflicts = 1
		return &MergeResult{
			Merged:        append([]byte(nil), ours...),
			HasConflicts:  true,
			ConflictCount: 1,
			Stats:         stats,
		}
	}
}

func isBinaryContent(data []byte) bool {
	return bytes.IndexByte(data, 0) >= 0
}

func hasDeclaration(el *entity.EntityList) bool {
	for _, e := range el.Entities {
		if e.Kind == entity.KindDeclaration {
			return true
		}
	}
	return false
}

func resolveTextConflicts(result diff3.Result) ([]byte, int) {
	if !result.HasConflicts {
		return result.Merged, 0
	}

	var merged bytes.Buffer
	conflictCount := 0
	for _, h := range result.Hunks {
		if h.Type != diff3.HunkConflict {
			merged.Write(h.Merged)
			continue
		}
		if canResolveParallelInsertion(h) {
			merged.Write(mergeParallelInsertions(h.Ours, h.Theirs))
			continue
		}
		conflictCount++
		merged.WriteString("<<<<<<< ours\n")
		merged.Write(h.Ours)
		merged.WriteString("=======\n")
		merged.Write(h.Theirs)
		merged.WriteString(">>>>>>> theirs\n")
	}

	return merged.Bytes(), conflictCount
}

func canResolveParallelInsertion(h diff3.Hunk) bool {
	return len(bytes.TrimSpace(h.Base)) == 0 &&
		len(bytes.TrimSpace(h.Ours)) > 0 &&
		len(bytes.TrimSpace(h.Theirs)) > 0
}

func mergeParallelInsertions(ours, theirs []byte) []byte {
	ours = append([]byte(nil), ours...)
	if bytes.Equal(bytes.TrimSpace(ours), bytes.TrimSpace(theirs)) {
		return ours
	}
	if len(ours) == 0 {
		return append([]byte(nil), theirs...)
	}
	if len(theirs) == 0 {
		return ours
	}

	out := ours
	if out[len(out)-1] != '\n' {
		out = append(out, '\n')
	}
	out = append(out, theirs...)
	return out
}
