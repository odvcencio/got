package repo

import (
	"bytes"
	"container/heap"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/odvcencio/got/pkg/merge"
	"github.com/odvcencio/got/pkg/object"
)

// FileMergeReport records the merge outcome for a single file.
type FileMergeReport struct {
	Path          string
	Status        string // "clean", "conflict", "added", "deleted"
	EntityCount   int
	ConflictCount int
}

// MergeReport is the overall result of a repository-level merge.
type MergeReport struct {
	Files          []FileMergeReport
	HasConflicts   bool
	TotalConflicts int
	MergeCommit    object.Hash // set if auto-committed (clean merge)
}

type mergeConflictState struct {
	path       string
	baseHash   object.Hash
	oursHash   object.Hash
	theirsHash object.Hash
	mode       string
}

const (
	maxMergeBaseBFSSteps = 1_000_000
	maxMergeBaseBFSDepth = 1_000_000
)

// These vars allow tests to tighten safety limits without affecting
// production defaults.
var (
	mergeBaseBFSStepsLimit = maxMergeBaseBFSSteps
	mergeBaseBFSDepthLimit = maxMergeBaseBFSDepth
)

type mergeBaseTraversalQueueItem struct {
	hash  object.Hash
	depth int
}

func mergeBaseTraversalLimits() (maxSteps int, maxDepth int) {
	maxSteps = normalizeMergeBaseTraversalLimit(mergeBaseBFSStepsLimit, maxMergeBaseBFSSteps)
	maxDepth = normalizeMergeBaseTraversalLimit(mergeBaseBFSDepthLimit, maxMergeBaseBFSDepth)

	return maxSteps, maxDepth
}

func normalizeMergeBaseTraversalLimit(limit, hardMax int) int {
	// Keep safety defaults as hard bounds; test hooks may only tighten.
	if limit <= 0 || limit > hardMax {
		return hardMax
	}
	return limit
}

func mergeBaseStepsLimitError(limit int) error {
	return fmt.Errorf("find merge base: traversal exceeded maximum steps (%d)", limit)
}

func mergeBaseDepthLimitError(limit int) error {
	return fmt.Errorf("find merge base: traversal exceeded maximum depth (%d)", limit)
}

// FindMergeBase finds a common ancestor of two commits. It uses cached
// generation numbers for pruning, fast ancestor checks for linear histories,
// and a memoized pair cache for repeated queries.
func (r *Repo) FindMergeBase(a, b object.Hash) (object.Hash, error) {
	if a == "" || b == "" {
		return "", nil
	}
	if a == b {
		return a, nil
	}

	state := r.getMergeTraversalState()
	if cached, ok := state.loadMergeBase(a, b); ok {
		if cached.found {
			return cached.base, nil
		}
		return "", nil
	}

	genA, err := state.generation(r, a)
	if err != nil {
		return "", err
	}
	genB, err := state.generation(r, b)
	if err != nil {
		return "", err
	}

	// Fast path: one side already contains the other.
	if genA <= genB {
		isAncestor, err := r.isAncestorWithGeneration(state, a, b, genA, genB)
		if err != nil {
			return "", err
		}
		if isAncestor {
			state.storeMergeBase(a, b, a, true)
			return a, nil
		}
		isAncestor, err = r.isAncestorWithGeneration(state, b, a, genB, genA)
		if err != nil {
			return "", err
		}
		if isAncestor {
			state.storeMergeBase(a, b, b, true)
			return b, nil
		}
	} else {
		isAncestor, err := r.isAncestorWithGeneration(state, b, a, genB, genA)
		if err != nil {
			return "", err
		}
		if isAncestor {
			state.storeMergeBase(a, b, b, true)
			return b, nil
		}
		isAncestor, err = r.isAncestorWithGeneration(state, a, b, genA, genB)
		if err != nil {
			return "", err
		}
		if isAncestor {
			state.storeMergeBase(a, b, a, true)
			return a, nil
		}
	}

	base, found, err := r.findMergeBaseWithPruning(state, a, b, genA, genB)
	if err != nil {
		return "", err
	}
	state.storeMergeBase(a, b, base, found)
	if !found {
		return "", nil
	}
	return base, nil
}

func (r *Repo) isAncestorWithGeneration(state *mergeBaseTraversalState, ancestor, descendant object.Hash, ancestorGeneration, descendantGeneration uint64) (bool, error) {
	if ancestor == descendant {
		return true, nil
	}
	if ancestorGeneration > descendantGeneration {
		return false, nil
	}

	maxSteps, maxDepth := mergeBaseTraversalLimits()
	visited := map[object.Hash]struct{}{descendant: {}}
	queue := []mergeBaseTraversalQueueItem{{hash: descendant, depth: 0}}
	steps := 0

	for len(queue) > 0 {
		item := queue[0]
		queue = queue[1:]

		steps++
		if steps > maxSteps {
			return false, mergeBaseStepsLimitError(maxSteps)
		}
		if item.depth > maxDepth {
			return false, mergeBaseDepthLimitError(maxDepth)
		}

		cur := item.hash
		if cur == ancestor {
			return true, nil
		}

		curGeneration, err := state.generation(r, cur)
		if err != nil {
			return false, err
		}
		if curGeneration <= ancestorGeneration {
			continue
		}

		commit, err := state.readCommit(r, cur)
		if err != nil {
			return false, err
		}
		for _, p := range commit.Parents {
			if p == "" {
				continue
			}
			if _, seen := visited[p]; seen {
				continue
			}
			parentGeneration, err := state.generation(r, p)
			if err != nil {
				return false, err
			}
			if parentGeneration < ancestorGeneration {
				continue
			}
			childDepth := item.depth + 1
			if childDepth > maxDepth {
				return false, mergeBaseDepthLimitError(maxDepth)
			}
			visited[p] = struct{}{}
			queue = append(queue, mergeBaseTraversalQueueItem{hash: p, depth: childDepth})
		}
	}

	return false, nil
}

func (r *Repo) findMergeBaseWithPruning(state *mergeBaseTraversalState, a, b object.Hash, genA, genB uint64) (object.Hash, bool, error) {
	maxSteps, maxDepth := mergeBaseTraversalLimits()

	visitedA := map[object.Hash]struct{}{a: {}}
	visitedB := map[object.Hash]struct{}{b: {}}
	depthA := map[object.Hash]int{a: 0}
	depthB := map[object.Hash]int{b: 0}

	queueA := mergeBaseMaxHeap{{hash: a, generation: genA}}
	queueB := mergeBaseMaxHeap{{hash: b, generation: genB}}
	heap.Init(&queueA)
	heap.Init(&queueB)

	best := object.Hash("")
	var bestGeneration uint64
	steps := 0

	for queueA.Len() > 0 || queueB.Len() > 0 {
		if best != "" {
			topA, okA := queueA.Peek()
			topB, okB := queueB.Peek()
			if (!okA || topA.generation < bestGeneration) && (!okB || topB.generation < bestGeneration) {
				break
			}
		}

		traverseA := false
		switch {
		case queueA.Len() == 0:
			traverseA = false
		case queueB.Len() == 0:
			traverseA = true
		default:
			topA := queueA[0]
			topB := queueB[0]
			if topA.generation > topB.generation {
				traverseA = true
			} else if topA.generation < topB.generation {
				traverseA = false
			} else {
				traverseA = topA.hash <= topB.hash
			}
		}

		var item mergeBaseQueueItem
		if traverseA {
			item = heap.Pop(&queueA).(mergeBaseQueueItem)
		} else {
			item = heap.Pop(&queueB).(mergeBaseQueueItem)
		}

		steps++
		if steps > maxSteps {
			return "", false, mergeBaseStepsLimitError(maxSteps)
		}
		if best != "" && item.generation < bestGeneration {
			continue
		}

		itemDepth := 0
		if traverseA {
			itemDepth = depthA[item.hash]
		} else {
			itemDepth = depthB[item.hash]
		}
		if itemDepth > maxDepth {
			return "", false, mergeBaseDepthLimitError(maxDepth)
		}

		if traverseA {
			if _, seen := visitedB[item.hash]; seen {
				best, bestGeneration = chooseBetterMergeBase(best, bestGeneration, item.hash, item.generation)
			}
		} else {
			if _, seen := visitedA[item.hash]; seen {
				best, bestGeneration = chooseBetterMergeBase(best, bestGeneration, item.hash, item.generation)
			}
		}

		commit, err := state.readCommit(r, item.hash)
		if err != nil {
			return "", false, err
		}

		for _, p := range commit.Parents {
			if p == "" {
				continue
			}

			parentGeneration, err := state.generation(r, p)
			if err != nil {
				return "", false, err
			}
			if best != "" && parentGeneration < bestGeneration {
				continue
			}

			childDepth := itemDepth + 1
			if childDepth > maxDepth {
				return "", false, mergeBaseDepthLimitError(maxDepth)
			}

			if traverseA {
				if _, seen := visitedA[p]; seen {
					continue
				}
				visitedA[p] = struct{}{}
				depthA[p] = childDepth
				heap.Push(&queueA, mergeBaseQueueItem{hash: p, generation: parentGeneration})
				if _, seen := visitedB[p]; seen {
					best, bestGeneration = chooseBetterMergeBase(best, bestGeneration, p, parentGeneration)
				}
			} else {
				if _, seen := visitedB[p]; seen {
					continue
				}
				visitedB[p] = struct{}{}
				depthB[p] = childDepth
				heap.Push(&queueB, mergeBaseQueueItem{hash: p, generation: parentGeneration})
				if _, seen := visitedA[p]; seen {
					best, bestGeneration = chooseBetterMergeBase(best, bestGeneration, p, parentGeneration)
				}
			}
		}
	}

	if best == "" {
		return "", false, nil
	}
	return best, true, nil
}

func chooseBetterMergeBase(best object.Hash, bestGeneration uint64, candidate object.Hash, candidateGeneration uint64) (object.Hash, uint64) {
	if best == "" {
		return candidate, candidateGeneration
	}
	if candidateGeneration > bestGeneration {
		return candidate, candidateGeneration
	}
	if candidateGeneration < bestGeneration {
		return best, bestGeneration
	}
	if candidate < best {
		return candidate, candidateGeneration
	}
	return best, bestGeneration
}

// Merge merges the named branch into the current HEAD.
//
// Algorithm:
//  1. Resolve current HEAD and branch name to commit hashes
//  2. FindMergeBase(headHash, branchHash)
//  3. Flatten all three trees (base, ours=HEAD, theirs=branch)
//  4. Collect all file paths across all three trees
//  5. For each file, perform the appropriate merge action
//  6. If clean: write files, stage, auto-commit with two parents
//  7. If conflicts: write conflict-marker files, do NOT commit
func (r *Repo) Merge(branchName string) (*MergeReport, error) {
	// 1. Resolve HEAD and branch.
	headHash, err := r.ResolveRef("HEAD")
	if err != nil {
		return nil, fmt.Errorf("merge: resolve HEAD: %w", err)
	}
	branchHash, err := r.ResolveRef("refs/heads/" + branchName)
	if err != nil {
		return nil, fmt.Errorf("merge: resolve branch %q: %w", branchName, err)
	}

	// 2. Find merge base.
	baseHash, err := r.FindMergeBase(headHash, branchHash)
	if err != nil {
		return nil, fmt.Errorf("merge: %w", err)
	}

	// 3. Flatten all three trees.
	headCommit, err := r.Store.ReadCommit(headHash)
	if err != nil {
		return nil, fmt.Errorf("merge: read head commit: %w", err)
	}
	branchCommit, err := r.Store.ReadCommit(branchHash)
	if err != nil {
		return nil, fmt.Errorf("merge: read branch commit: %w", err)
	}

	oursFiles, err := r.FlattenTree(headCommit.TreeHash)
	if err != nil {
		return nil, fmt.Errorf("merge: flatten ours tree: %w", err)
	}
	theirsFiles, err := r.FlattenTree(branchCommit.TreeHash)
	if err != nil {
		return nil, fmt.Errorf("merge: flatten theirs tree: %w", err)
	}

	// Base tree may be empty if this is the first merge (no common ancestor).
	var baseFiles []TreeFileEntry
	if baseHash != "" {
		baseCommit, err := r.Store.ReadCommit(baseHash)
		if err != nil {
			return nil, fmt.Errorf("merge: read base commit: %w", err)
		}
		baseFiles, err = r.FlattenTree(baseCommit.TreeHash)
		if err != nil {
			return nil, fmt.Errorf("merge: flatten base tree: %w", err)
		}
	}

	// Index files by path.
	baseMap := indexByPath(baseFiles)
	oursMap := indexByPath(oursFiles)
	theirsMap := indexByPath(theirsFiles)

	// 4. Collect all file paths.
	allPaths := collectAllPaths(baseMap, oursMap, theirsMap)

	// 5. Process each file.
	report := &MergeReport{}
	// Track merged file contents to write and stage.
	type mergedFile struct {
		path    string
		content []byte
		mode    string
	}
	var mergedFiles []mergedFile
	var conflictedFiles []mergeConflictState
	var deletedPaths []string

	for _, path := range allPaths {
		_, inBase := baseMap[path]
		_, inOurs := oursMap[path]
		_, inTheirs := theirsMap[path]

		switch {
		case inBase && inOurs && inTheirs:
			// In all three: three-way merge.
			fr, content, err := r.mergeThreeWay(path, baseMap[path], oursMap[path], theirsMap[path])
			if err != nil {
				return nil, fmt.Errorf("merge file %q: %w", path, err)
			}
			report.Files = append(report.Files, fr)
			if fr.Status == "conflict" {
				report.HasConflicts = true
				report.TotalConflicts += fr.ConflictCount
				conflictedFiles = append(conflictedFiles, mergeConflictState{
					path:       path,
					baseHash:   baseMap[path].BlobHash,
					oursHash:   oursMap[path].BlobHash,
					theirsHash: theirsMap[path].BlobHash,
					mode:       normalizeFileMode(oursMap[path].Mode),
				})
			}
			mergedFiles = append(mergedFiles, mergedFile{
				path:    path,
				content: content,
				mode:    normalizeFileMode(oursMap[path].Mode),
			})

		case !inBase && inOurs && inTheirs:
			// New in both branches (not in base).
			if oursMap[path].BlobHash == theirsMap[path].BlobHash {
				// Same content: take either.
				content, err := r.readBlobData(oursMap[path].BlobHash)
				if err != nil {
					return nil, fmt.Errorf("merge read %q: %w", path, err)
				}
				report.Files = append(report.Files, FileMergeReport{
					Path:   path,
					Status: "clean",
				})
				mergedFiles = append(mergedFiles, mergedFile{
					path:    path,
					content: content,
					mode:    normalizeFileMode(oursMap[path].Mode),
				})
			} else {
				// Different content: conflict.
				oursData, err := r.readBlobData(oursMap[path].BlobHash)
				if err != nil {
					return nil, fmt.Errorf("merge read ours %q: %w", path, err)
				}
				theirsData, err := r.readBlobData(theirsMap[path].BlobHash)
				if err != nil {
					return nil, fmt.Errorf("merge read theirs %q: %w", path, err)
				}
				// Try structural merge with empty base.
				fr, content, err := r.mergeFileContents(path, nil, oursData, theirsData)
				if err != nil {
					return nil, fmt.Errorf("merge file %q: %w", path, err)
				}
				report.Files = append(report.Files, fr)
				if fr.Status == "conflict" {
					report.HasConflicts = true
					report.TotalConflicts += fr.ConflictCount
					conflictedFiles = append(conflictedFiles, mergeConflictState{
						path:       path,
						baseHash:   "",
						oursHash:   oursMap[path].BlobHash,
						theirsHash: theirsMap[path].BlobHash,
						mode:       normalizeFileMode(oursMap[path].Mode),
					})
				}
				mergedFiles = append(mergedFiles, mergedFile{
					path:    path,
					content: content,
					mode:    normalizeFileMode(oursMap[path].Mode),
				})
			}

		case inBase && inOurs && !inTheirs:
			// Deleted by theirs.
			if oursMap[path].BlobHash == baseMap[path].BlobHash {
				// Ours unchanged: clean delete.
				report.Files = append(report.Files, FileMergeReport{
					Path:   path,
					Status: "deleted",
				})
				deletedPaths = append(deletedPaths, path)
				continue
			}

			// Delete-vs-modify must be a conflict (avoid silent data loss).
			oursData, err := r.readBlobData(oursMap[path].BlobHash)
			if err != nil {
				return nil, fmt.Errorf("merge read ours %q: %w", path, err)
			}
			content := renderFileConflict(oursData, nil)
			report.Files = append(report.Files, FileMergeReport{
				Path:          path,
				Status:        "conflict",
				ConflictCount: 1,
			})
			report.HasConflicts = true
			report.TotalConflicts++
			mergedFiles = append(mergedFiles, mergedFile{
				path:    path,
				content: content,
				mode:    normalizeFileMode(oursMap[path].Mode),
			})
			conflictedFiles = append(conflictedFiles, mergeConflictState{
				path:       path,
				baseHash:   baseMap[path].BlobHash,
				oursHash:   oursMap[path].BlobHash,
				theirsHash: "",
				mode:       normalizeFileMode(oursMap[path].Mode),
			})

		case inBase && !inOurs && inTheirs:
			// Deleted by ours.
			if theirsMap[path].BlobHash == baseMap[path].BlobHash {
				// Theirs unchanged: clean delete.
				report.Files = append(report.Files, FileMergeReport{
					Path:   path,
					Status: "deleted",
				})
				deletedPaths = append(deletedPaths, path)
				continue
			}

			// Delete-vs-modify must be a conflict (avoid silent data loss).
			theirsData, err := r.readBlobData(theirsMap[path].BlobHash)
			if err != nil {
				return nil, fmt.Errorf("merge read theirs %q: %w", path, err)
			}
			content := renderFileConflict(nil, theirsData)
			report.Files = append(report.Files, FileMergeReport{
				Path:          path,
				Status:        "conflict",
				ConflictCount: 1,
			})
			report.HasConflicts = true
			report.TotalConflicts++
			mergedFiles = append(mergedFiles, mergedFile{
				path:    path,
				content: content,
				mode:    normalizeFileMode(theirsMap[path].Mode),
			})
			conflictedFiles = append(conflictedFiles, mergeConflictState{
				path:       path,
				baseHash:   baseMap[path].BlobHash,
				oursHash:   "",
				theirsHash: theirsMap[path].BlobHash,
				mode:       normalizeFileMode(theirsMap[path].Mode),
			})

		case !inBase && inOurs && !inTheirs:
			// New in ours only: keep as-is.
			content, err := r.readBlobData(oursMap[path].BlobHash)
			if err != nil {
				return nil, fmt.Errorf("merge read %q: %w", path, err)
			}
			report.Files = append(report.Files, FileMergeReport{
				Path:   path,
				Status: "added",
			})
			mergedFiles = append(mergedFiles, mergedFile{
				path:    path,
				content: content,
				mode:    normalizeFileMode(oursMap[path].Mode),
			})

		case !inBase && !inOurs && inTheirs:
			// New in theirs only: add.
			content, err := r.readBlobData(theirsMap[path].BlobHash)
			if err != nil {
				return nil, fmt.Errorf("merge read %q: %w", path, err)
			}
			report.Files = append(report.Files, FileMergeReport{
				Path:   path,
				Status: "added",
			})
			mergedFiles = append(mergedFiles, mergedFile{
				path:    path,
				content: content,
				mode:    normalizeFileMode(theirsMap[path].Mode),
			})

		case inBase && !inOurs && !inTheirs:
			// Both deleted: remove.
			report.Files = append(report.Files, FileMergeReport{
				Path:   path,
				Status: "deleted",
			})
			deletedPaths = append(deletedPaths, path)
		}
	}

	// 6/7. Write files to working directory.
	for _, mf := range mergedFiles {
		absPath := filepath.Join(r.RootDir, filepath.FromSlash(mf.path))
		dir := filepath.Dir(absPath)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("merge: mkdir %q: %w", dir, err)
		}
		if err := os.WriteFile(absPath, mf.content, filePermFromMode(mf.mode)); err != nil {
			return nil, fmt.Errorf("merge: write %q: %w", mf.path, err)
		}
	}

	// Remove deleted files.
	for _, path := range deletedPaths {
		absPath := filepath.Join(r.RootDir, filepath.FromSlash(path))
		if err := os.Remove(absPath); err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("merge: remove %q: %w", path, err)
		}
		r.removeEmptyParents(filepath.Dir(absPath))
	}

	if !report.HasConflicts {
		// Stage all merged files and commit.
		var pathsToAdd []string
		for _, mf := range mergedFiles {
			pathsToAdd = append(pathsToAdd, mf.path)
		}
		if len(pathsToAdd) > 0 {
			if err := r.Add(pathsToAdd); err != nil {
				return nil, fmt.Errorf("merge: stage: %w", err)
			}
		}

		// Remove deleted files from staging.
		if len(deletedPaths) > 0 {
			stg, err := r.ReadStaging()
			if err != nil {
				return nil, fmt.Errorf("merge: read staging: %w", err)
			}
			for _, p := range deletedPaths {
				delete(stg.Entries, p)
			}
			if err := r.WriteStaging(stg); err != nil {
				return nil, fmt.Errorf("merge: write staging: %w", err)
			}
		}

		// Create merge commit with two parents.
		mergeHash, err := r.commitMerge(
			fmt.Sprintf("Merge branch '%s'", branchName),
			"got-merge",
			headHash,
			branchHash,
		)
		if err != nil {
			return nil, fmt.Errorf("merge: commit: %w", err)
		}
		report.MergeCommit = mergeHash
	} else {
		if err := r.stageConflictState(conflictedFiles, deletedPaths); err != nil {
			return nil, fmt.Errorf("merge: stage conflicts: %w", err)
		}
	}

	return report, nil
}

func (r *Repo) stageConflictState(conflicted []mergeConflictState, deletedPaths []string) error {
	stg, err := r.ReadStaging()
	if err != nil {
		return fmt.Errorf("read staging: %w", err)
	}

	for _, p := range deletedPaths {
		delete(stg.Entries, p)
	}

	for _, cf := range conflicted {
		absPath := filepath.Join(r.RootDir, filepath.FromSlash(cf.path))
		info, err := os.Stat(absPath)
		if err != nil {
			return fmt.Errorf("stat conflicted file %q: %w", cf.path, err)
		}
		data, err := os.ReadFile(absPath)
		if err != nil {
			return fmt.Errorf("read conflicted file %q: %w", cf.path, err)
		}

		blobHash, err := r.Store.WriteBlob(&object.Blob{Data: data})
		if err != nil {
			return fmt.Errorf("write conflicted blob %q: %w", cf.path, err)
		}

		stg.Entries[cf.path] = &StagingEntry{
			Path:           cf.path,
			BlobHash:       blobHash,
			EntityListHash: "",
			Mode:           normalizeFileMode(cf.mode),
			Conflict:       true,
			BaseBlobHash:   cf.baseHash,
			OursBlobHash:   cf.oursHash,
			TheirsBlobHash: cf.theirsHash,
			ModTime:        info.ModTime().UnixNano(),
			Size:           info.Size(),
		}
	}

	if err := r.WriteStaging(stg); err != nil {
		return fmt.Errorf("write staging: %w", err)
	}
	return nil
}

func renderFileConflict(ours, theirs []byte) []byte {
	var buf bytes.Buffer
	buf.WriteString("<<<<<<< ours\n")
	buf.Write(ours)
	if len(ours) > 0 && ours[len(ours)-1] != '\n' {
		buf.WriteByte('\n')
	}
	buf.WriteString("=======\n")
	buf.Write(theirs)
	if len(theirs) > 0 && theirs[len(theirs)-1] != '\n' {
		buf.WriteByte('\n')
	}
	buf.WriteString(">>>>>>> theirs\n")
	return buf.Bytes()
}

// commitMerge creates a commit with two parents (for merge commits).
// This is similar to Commit() but takes explicit parent hashes instead
// of deriving them from HEAD.
func (r *Repo) commitMerge(message, author string, parent1, parent2 object.Hash) (object.Hash, error) {
	stg, err := r.ReadStaging()
	if err != nil {
		return "", fmt.Errorf("merge commit: %w", err)
	}
	if len(stg.Entries) == 0 {
		return "", fmt.Errorf("merge commit: nothing staged")
	}

	treeHash, err := r.BuildTree(stg)
	if err != nil {
		return "", fmt.Errorf("merge commit: %w", err)
	}

	commitObj := &object.CommitObj{
		TreeHash:  treeHash,
		Parents:   []object.Hash{parent1, parent2},
		Author:    author,
		Timestamp: time.Now().Unix(),
		Message:   message,
	}

	commitHash, err := r.Store.WriteCommit(commitObj)
	if err != nil {
		return "", fmt.Errorf("merge commit: write: %w", err)
	}

	// Update current branch ref.
	head, err := r.Head()
	if err != nil {
		return "", fmt.Errorf("merge commit: read HEAD: %w", err)
	}
	if strings.HasPrefix(head, "refs/") {
		if err := r.UpdateRefCAS(head, commitHash, parent1); err != nil {
			return "", fmt.Errorf("merge commit: update ref %q: %w", head, err)
		}
	} else {
		if err := r.UpdateRefCAS("HEAD", commitHash, parent1); err != nil {
			return "", fmt.Errorf("merge commit: update detached HEAD: %w", err)
		}
	}

	r.invalidateStatusCache()

	return commitHash, nil
}

// mergeThreeWay performs a three-way structural merge of a file that exists
// in base, ours, and theirs.
func (r *Repo) mergeThreeWay(path string, base, ours, theirs TreeFileEntry) (FileMergeReport, []byte, error) {
	// If ours and theirs have the same blob hash, no merge needed.
	if ours.BlobHash == theirs.BlobHash {
		content, err := r.readBlobData(ours.BlobHash)
		if err != nil {
			return FileMergeReport{}, nil, err
		}
		return FileMergeReport{Path: path, Status: "clean"}, content, nil
	}

	// If only one side changed from base, take that side.
	if ours.BlobHash == base.BlobHash {
		// Only theirs changed.
		content, err := r.readBlobData(theirs.BlobHash)
		if err != nil {
			return FileMergeReport{}, nil, err
		}
		return FileMergeReport{Path: path, Status: "clean"}, content, nil
	}
	if theirs.BlobHash == base.BlobHash {
		// Only ours changed.
		content, err := r.readBlobData(ours.BlobHash)
		if err != nil {
			return FileMergeReport{}, nil, err
		}
		return FileMergeReport{Path: path, Status: "clean"}, content, nil
	}

	// Both sides changed: full three-way merge.
	baseData, err := r.readBlobData(base.BlobHash)
	if err != nil {
		return FileMergeReport{}, nil, err
	}
	oursData, err := r.readBlobData(ours.BlobHash)
	if err != nil {
		return FileMergeReport{}, nil, err
	}
	theirsData, err := r.readBlobData(theirs.BlobHash)
	if err != nil {
		return FileMergeReport{}, nil, err
	}

	return r.mergeFileContents(path, baseData, oursData, theirsData)
}

// mergeFileContents calls the structural merge engine on raw file contents.
func (r *Repo) mergeFileContents(path string, base, ours, theirs []byte) (FileMergeReport, []byte, error) {
	result, err := merge.MergeFiles(path, base, ours, theirs)
	if err != nil {
		return FileMergeReport{}, nil, fmt.Errorf("structural merge %q: %w", path, err)
	}

	fr := FileMergeReport{
		Path:          path,
		ConflictCount: result.ConflictCount,
	}
	if result.HasConflicts {
		fr.Status = "conflict"
	} else {
		fr.Status = "clean"
	}

	return fr, result.Merged, nil
}

// readBlobData reads a blob from the store and returns its raw data.
func (r *Repo) readBlobData(h object.Hash) ([]byte, error) {
	blob, err := r.Store.ReadBlob(h)
	if err != nil {
		return nil, fmt.Errorf("read blob %s: %w", h, err)
	}
	return blob.Data, nil
}

// indexByPath creates a map from file path to TreeFileEntry.
func indexByPath(entries []TreeFileEntry) map[string]TreeFileEntry {
	m := make(map[string]TreeFileEntry, len(entries))
	for _, e := range entries {
		m[e.Path] = e
	}
	return m
}

// collectAllPaths returns a sorted, deduplicated list of all file paths
// across three file maps.
func collectAllPaths(base, ours, theirs map[string]TreeFileEntry) []string {
	seen := make(map[string]bool)
	for p := range base {
		seen[p] = true
	}
	for p := range ours {
		seen[p] = true
	}
	for p := range theirs {
		seen[p] = true
	}

	paths := make([]string, 0, len(seen))
	for p := range seen {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return paths
}
