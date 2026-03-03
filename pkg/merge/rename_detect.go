package merge

import (
	"sort"

	"github.com/odvcencio/graft/pkg/diff3"
	"github.com/odvcencio/graft/pkg/entity"
)

// RenameCandidate records a detected rename from OldKey to NewKey with a
// similarity score (1.0 = exact body match).
type RenameCandidate struct {
	OldKey     string
	NewKey     string
	Similarity float64
}

// DetectRenames finds probable renames among unmatched entities. For each pair
// of (deleted, added) entities with the same DeclKind:
//   - If BodyHash matches exactly, it is a definite rename (similarity 1.0).
//   - Otherwise, line-level similarity is computed via diff3.LineDiff. If it
//     exceeds the threshold, it is a probable rename.
//
// Each deleted and added entity can participate in at most one rename. When
// multiple candidates compete, the highest-similarity pairing wins.
func DetectRenames(
	deleted map[string]*entity.Entity,
	added map[string]*entity.Entity,
	threshold float64,
) []RenameCandidate {
	if len(deleted) == 0 || len(added) == 0 {
		return nil
	}

	// Collect all candidate pairs above threshold.
	type scoredPair struct {
		delKey string
		addKey string
		sim    float64
	}
	var candidates []scoredPair

	for dk, de := range deleted {
		for ak, ae := range added {
			if de.DeclKind != ae.DeclKind {
				continue
			}

			// Exact body hash match => definite rename.
			if de.BodyHash == ae.BodyHash {
				candidates = append(candidates, scoredPair{dk, ak, 1.0})
				continue
			}

			// Compute line-level similarity.
			sim := lineSimilarity(de.Body, ae.Body)
			if sim >= threshold {
				candidates = append(candidates, scoredPair{dk, ak, sim})
			}
		}
	}

	// Sort by descending similarity so greedy assignment picks best matches first.
	// Break ties deterministically by lexicographic key order.
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].sim != candidates[j].sim {
			return candidates[i].sim > candidates[j].sim
		}
		if candidates[i].delKey != candidates[j].delKey {
			return candidates[i].delKey < candidates[j].delKey
		}
		return candidates[i].addKey < candidates[j].addKey
	})

	// Greedy 1:1 assignment.
	usedDel := map[string]bool{}
	usedAdd := map[string]bool{}
	var result []RenameCandidate

	for _, c := range candidates {
		if usedDel[c.delKey] || usedAdd[c.addKey] {
			continue
		}
		usedDel[c.delKey] = true
		usedAdd[c.addKey] = true
		result = append(result, RenameCandidate{
			OldKey:     c.delKey,
			NewKey:     c.addKey,
			Similarity: c.sim,
		})
	}

	return result
}

// lineSimilarity computes the ratio of shared lines between a and b using
// a line-level diff. The similarity is defined as:
//
//	2 * equalLines / (totalLinesA + totalLinesB)
//
// This yields 1.0 for identical content and 0.0 for completely different content.
func lineSimilarity(a, b []byte) float64 {
	diffs := diff3.LineDiff(a, b)
	if len(diffs) == 0 {
		// Both empty => identical.
		return 1.0
	}

	var equal, totalA, totalB int
	for _, d := range diffs {
		switch d.Type {
		case diff3.Equal:
			equal++
			totalA++
			totalB++
		case diff3.Delete:
			totalA++
		case diff3.Insert:
			totalB++
		}
	}

	total := totalA + totalB
	if total == 0 {
		return 1.0
	}
	return float64(2*equal) / float64(total)
}
