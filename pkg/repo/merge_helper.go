package repo

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/odvcencio/graft/pkg/merge"
)

// ThreeWayFileResult holds the outcome of a three-way merge for a single file.
type ThreeWayFileResult struct {
	Path      string
	Content   []byte
	Mode      string
	Status    string // "clean", "conflict", "added", "deleted", "unchanged"
	Conflicts int
}

// ThreeWayMergeResult holds the outcome of a complete three-way tree merge.
type ThreeWayMergeResult struct {
	Files           []ThreeWayFileResult
	DeletedPaths    []string
	HasConflicts    bool
	TotalConflicts  int
	ConflictDetails []string // paths with conflicts (for rebase error messages)
}

// threeWayTreeMerge performs a three-way merge across all files in three trees
// (base, ours, theirs). This is the shared logic used by merge, rebase cherry-pick,
// and rebase squash/fixup.
//
// The caller provides three maps of TreeFileEntry indexed by path, as returned
// by indexByPath(r.FlattenTree(...)).
func (r *Repo) threeWayTreeMerge(
	baseMap, oursMap, theirsMap map[string]TreeFileEntry,
) (*ThreeWayMergeResult, error) {
	allPaths := collectAllPaths(baseMap, oursMap, theirsMap)

	result := &ThreeWayMergeResult{}

	for _, path := range allPaths {
		_, inBase := baseMap[path]
		_, inOurs := oursMap[path]
		_, inTheirs := theirsMap[path]

		switch {
		case inBase && inOurs && inTheirs:
			// In all three: three-way merge.
			if oursMap[path].BlobHash == theirsMap[path].BlobHash {
				// Same content, no merge needed.
				result.Files = append(result.Files, ThreeWayFileResult{
					Path:   path,
					Status: "unchanged",
				})
				continue
			}
			if oursMap[path].BlobHash == baseMap[path].BlobHash {
				// Only theirs changed -- take theirs.
				content, err := r.readBlobData(theirsMap[path].BlobHash)
				if err != nil {
					return nil, err
				}
				result.Files = append(result.Files, ThreeWayFileResult{
					Path:    path,
					Content: content,
					Mode:    normalizeFileMode(theirsMap[path].Mode),
					Status:  "clean",
				})
				continue
			}
			if theirsMap[path].BlobHash == baseMap[path].BlobHash {
				// Only ours changed -- keep ours.
				result.Files = append(result.Files, ThreeWayFileResult{
					Path:   path,
					Status: "unchanged",
				})
				continue
			}
			// Both changed -- do full structural merge.
			baseData, err := r.readBlobData(baseMap[path].BlobHash)
			if err != nil {
				return nil, err
			}
			oursData, err := r.readBlobData(oursMap[path].BlobHash)
			if err != nil {
				return nil, err
			}
			theirsData, err := r.readBlobData(theirsMap[path].BlobHash)
			if err != nil {
				return nil, err
			}
			mergeResult, err := merge.MergeFiles(path, baseData, oursData, theirsData)
			if err != nil {
				return nil, fmt.Errorf("merge %q: %w", path, err)
			}
			status := "clean"
			if mergeResult.HasConflicts {
				status = "conflict"
				result.HasConflicts = true
				result.TotalConflicts += mergeResult.ConflictCount
				result.ConflictDetails = append(result.ConflictDetails, path)
			}
			result.Files = append(result.Files, ThreeWayFileResult{
				Path:      path,
				Content:   mergeResult.Merged,
				Mode:      normalizeFileMode(oursMap[path].Mode),
				Status:    status,
				Conflicts: mergeResult.ConflictCount,
			})

		case !inBase && !inOurs && inTheirs:
			// New in theirs only: add.
			content, err := r.readBlobData(theirsMap[path].BlobHash)
			if err != nil {
				return nil, err
			}
			result.Files = append(result.Files, ThreeWayFileResult{
				Path:    path,
				Content: content,
				Mode:    normalizeFileMode(theirsMap[path].Mode),
				Status:  "added",
			})

		case !inBase && inOurs && inTheirs:
			// Added in both.
			if oursMap[path].BlobHash == theirsMap[path].BlobHash {
				result.Files = append(result.Files, ThreeWayFileResult{
					Path:   path,
					Status: "unchanged",
				})
				continue
			}
			oursData, err := r.readBlobData(oursMap[path].BlobHash)
			if err != nil {
				return nil, err
			}
			theirsData, err := r.readBlobData(theirsMap[path].BlobHash)
			if err != nil {
				return nil, err
			}
			mergeResult, err := merge.MergeFiles(path, nil, oursData, theirsData)
			if err != nil {
				return nil, fmt.Errorf("merge %q: %w", path, err)
			}
			status := "clean"
			if mergeResult.HasConflicts {
				status = "conflict"
				result.HasConflicts = true
				result.TotalConflicts += mergeResult.ConflictCount
				result.ConflictDetails = append(result.ConflictDetails, path)
			}
			result.Files = append(result.Files, ThreeWayFileResult{
				Path:      path,
				Content:   mergeResult.Merged,
				Mode:      normalizeFileMode(oursMap[path].Mode),
				Status:    status,
				Conflicts: mergeResult.ConflictCount,
			})

		case inBase && inOurs && !inTheirs:
			// Deleted by theirs.
			if oursMap[path].BlobHash == baseMap[path].BlobHash {
				// Ours unchanged -- clean delete.
				result.Files = append(result.Files, ThreeWayFileResult{
					Path:   path,
					Status: "deleted",
				})
				result.DeletedPaths = append(result.DeletedPaths, path)
			} else {
				// Delete-vs-modify conflict.
				result.HasConflicts = true
				result.TotalConflicts++
				result.ConflictDetails = append(result.ConflictDetails, path)
				oursData, err := r.readBlobData(oursMap[path].BlobHash)
				if err != nil {
					return nil, err
				}
				content := renderFileConflict(oursData, nil)
				result.Files = append(result.Files, ThreeWayFileResult{
					Path:      path,
					Content:   content,
					Mode:      normalizeFileMode(oursMap[path].Mode),
					Status:    "conflict",
					Conflicts: 1,
				})
			}

		case inBase && !inOurs && inTheirs:
			// Deleted by ours.
			if theirsMap[path].BlobHash == baseMap[path].BlobHash {
				// Theirs unchanged -- keep deletion.
				result.Files = append(result.Files, ThreeWayFileResult{
					Path:   path,
					Status: "deleted",
				})
				result.DeletedPaths = append(result.DeletedPaths, path)
			} else {
				// Delete-vs-modify conflict.
				result.HasConflicts = true
				result.TotalConflicts++
				result.ConflictDetails = append(result.ConflictDetails, path)
				theirsData, err := r.readBlobData(theirsMap[path].BlobHash)
				if err != nil {
					return nil, err
				}
				content := renderFileConflict(nil, theirsData)
				result.Files = append(result.Files, ThreeWayFileResult{
					Path:      path,
					Content:   content,
					Mode:      normalizeFileMode(theirsMap[path].Mode),
					Status:    "conflict",
					Conflicts: 1,
				})
			}

		case !inBase && inOurs && !inTheirs:
			// Only in ours, not involved in merge target.
			result.Files = append(result.Files, ThreeWayFileResult{
				Path:   path,
				Status: "unchanged",
			})

		case inBase && !inOurs && !inTheirs:
			// Both deleted -- already gone.
			result.Files = append(result.Files, ThreeWayFileResult{
				Path:   path,
				Status: "deleted",
			})
			result.DeletedPaths = append(result.DeletedPaths, path)
		}
	}

	return result, nil
}

// conflictDetailsString returns a comma-separated string of conflicted paths
// suitable for error messages.
func (r *ThreeWayMergeResult) conflictDetailsString() string {
	return strings.Join(r.ConflictDetails, ", ")
}

// applyThreeWayResult writes the merge results to the working directory:
// writing changed/conflicted/added files and removing deleted files.
func (r *Repo) applyThreeWayResult(result *ThreeWayMergeResult) error {
	for _, f := range result.Files {
		if f.Status == "unchanged" || f.Status == "deleted" {
			continue
		}
		absPath := filepath.Join(r.RootDir, filepath.FromSlash(f.Path))
		dir := filepath.Dir(absPath)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("mkdir %q: %w", dir, err)
		}
		if err := os.WriteFile(absPath, f.Content, filePermFromMode(f.Mode)); err != nil {
			return fmt.Errorf("write %q: %w", f.Path, err)
		}
	}

	for _, path := range result.DeletedPaths {
		absPath := filepath.Join(r.RootDir, filepath.FromSlash(path))
		if err := os.Remove(absPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove %q: %w", path, err)
		}
		r.removeEmptyParents(filepath.Dir(absPath))
	}

	return nil
}
