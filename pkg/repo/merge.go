package repo

import (
	"bytes"
	"container/heap"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/odvcencio/graft/pkg/merge"
	"github.com/odvcencio/graft/pkg/object"
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
	IsFastForward  bool        // true if fast-forward (no merge commit created)
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
			if errors.Is(err, ErrShallowBoundary) {
				continue
			}
			return false, err
		}
		if curGeneration <= ancestorGeneration {
			continue
		}

		commit, err := state.readCommit(r, cur)
		if err != nil {
			if errors.Is(err, ErrShallowBoundary) {
				continue
			}
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
				if errors.Is(err, ErrShallowBoundary) {
					continue
				}
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
			if errors.Is(err, ErrShallowBoundary) {
				continue
			}
			return "", false, err
		}

		for _, p := range commit.Parents {
			if p == "" {
				continue
			}

			parentGeneration, err := state.generation(r, p)
			if err != nil {
				if errors.Is(err, ErrShallowBoundary) {
					continue
				}
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

// isFastForward returns true if headHash is an ancestor of targetHash,
// meaning the merge can be done by simply moving HEAD forward.
func (r *Repo) isFastForward(headHash, targetHash object.Hash) bool {
	base, err := r.FindMergeBase(headHash, targetHash)
	if err != nil {
		return false
	}
	return base == headHash
}

// saveMergeState writes pre-merge state files so that MergeAbort can restore
// the original state. It writes MERGE_HEAD (the branch being merged) and
// ORIG_HEAD (the HEAD hash before the merge).
func (r *Repo) saveMergeState(origHead, mergeHead object.Hash) error {
	origHeadPath := filepath.Join(r.GraftDir, "ORIG_HEAD")
	if err := os.WriteFile(origHeadPath, []byte(string(origHead)+"\n"), 0o644); err != nil {
		return fmt.Errorf("write ORIG_HEAD: %w", err)
	}
	mergeHeadPath := filepath.Join(r.GraftDir, "MERGE_HEAD")
	if err := os.WriteFile(mergeHeadPath, []byte(string(mergeHead)+"\n"), 0o644); err != nil {
		return fmt.Errorf("write MERGE_HEAD: %w", err)
	}
	return nil
}

// cleanMergeState removes the merge state files (MERGE_HEAD, ORIG_HEAD).
func (r *Repo) cleanMergeState() {
	os.Remove(filepath.Join(r.GraftDir, "MERGE_HEAD"))
	os.Remove(filepath.Join(r.GraftDir, "ORIG_HEAD"))
}

// IsMergeInProgress returns true if a merge is currently in progress
// (MERGE_HEAD exists).
func (r *Repo) IsMergeInProgress() bool {
	_, err := os.Stat(filepath.Join(r.GraftDir, "MERGE_HEAD"))
	return err == nil
}

// MergeAbort cancels an in-progress merge and restores the working tree
// to the pre-merge state.
func (r *Repo) MergeAbort() error {
	if !r.IsMergeInProgress() {
		return fmt.Errorf("merge abort: no merge in progress")
	}

	// Read original HEAD.
	origHeadData, err := os.ReadFile(filepath.Join(r.GraftDir, "ORIG_HEAD"))
	if err != nil {
		return fmt.Errorf("merge abort: read ORIG_HEAD: %w", err)
	}
	origHead := object.Hash(strings.TrimSpace(string(origHeadData)))

	// Restore HEAD to orig-head by checking out the original commit's tree.
	origCommit, err := r.Store.ReadCommit(origHead)
	if err != nil {
		return fmt.Errorf("merge abort: read orig commit: %w", err)
	}
	if err := r.checkoutTree(origCommit); err != nil {
		return fmt.Errorf("merge abort: checkout: %w", err)
	}

	// Restore branch ref to original HEAD.
	head, err := r.Head()
	if err != nil {
		return fmt.Errorf("merge abort: read HEAD: %w", err)
	}
	if strings.HasPrefix(head, "refs/") {
		currentRef, _ := r.ResolveRef(head)
		if err := r.UpdateRefCAS(head, origHead, currentRef); err != nil {
			return fmt.Errorf("merge abort: restore ref: %w", err)
		}
	} else {
		if err := r.setHeadDetached(origHead); err != nil {
			return fmt.Errorf("merge abort: set HEAD: %w", err)
		}
	}

	// Clean up merge state files.
	r.cleanMergeState()
	r.invalidateStatusCache()

	return nil
}

// Merge merges the named branch into the current HEAD.
//
// Algorithm:
//  1. Resolve current HEAD and branch name to commit hashes
//  2. FindMergeBase(headHash, branchHash)
//  3. Fast-forward detection: if HEAD is ancestor of target, just advance HEAD
//  4. Flatten all three trees (base, ours=HEAD, theirs=branch)
//  5. For each file, perform the appropriate merge action via threeWayTreeMerge
//  6. If clean: write files, stage, auto-commit with two parents
//  7. If conflicts: write conflict-marker files, save merge state, do NOT commit
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

	// 3. Fast-forward detection: if merge base == HEAD, we can fast-forward.
	if baseHash == headHash {
		return r.mergeFastForward(branchName, headHash, branchHash)
	}

	// Save pre-merge state for possible --abort.
	if err := r.saveMergeState(headHash, branchHash); err != nil {
		return nil, fmt.Errorf("merge: save state: %w", err)
	}

	// 4. Flatten all three trees.
	headCommit, err := r.Store.ReadCommit(headHash)
	if err != nil {
		return nil, fmt.Errorf("merge: read head commit: %w", err)
	}
	branchCommit, err := r.Store.ReadCommit(branchHash)
	if err != nil {
		return nil, fmt.Errorf("merge: read branch commit: %w", err)
	}

	oursFiles, oursModules, err := r.FlattenTreeWithModules(headCommit.TreeHash)
	if err != nil {
		return nil, fmt.Errorf("merge: flatten ours tree: %w", err)
	}
	theirsFiles, theirsModules, err := r.FlattenTreeWithModules(branchCommit.TreeHash)
	if err != nil {
		return nil, fmt.Errorf("merge: flatten theirs tree: %w", err)
	}

	// Base tree may be empty if this is the first merge (no common ancestor).
	var baseFiles []TreeFileEntry
	var baseModules []TreeModuleEntry
	if baseHash != "" {
		baseCommit, err := r.Store.ReadCommit(baseHash)
		if err != nil {
			return nil, fmt.Errorf("merge: read base commit: %w", err)
		}
		baseFiles, baseModules, err = r.FlattenTreeWithModules(baseCommit.TreeHash)
		if err != nil {
			return nil, fmt.Errorf("merge: flatten base tree: %w", err)
		}
	}

	// Index files by path.
	baseMap := indexByPath(baseFiles)
	oursMap := indexByPath(oursFiles)
	theirsMap := indexByPath(theirsFiles)

	// 5. Process each file via the shared three-way merge helper.
	mergeResult, err := r.threeWayTreeMerge(baseMap, oursMap, theirsMap)
	if err != nil {
		return nil, fmt.Errorf("merge: %w", err)
	}

	// Build the MergeReport and collect data for writing files.
	report := &MergeReport{
		HasConflicts:   mergeResult.HasConflicts,
		TotalConflicts: mergeResult.TotalConflicts,
	}

	type mergedFile struct {
		path    string
		content []byte
		mode    string
	}
	var mergedFiles []mergedFile
	var conflictedFiles []mergeConflictState
	var deletedPaths []string

	for _, f := range mergeResult.Files {
		switch f.Status {
		case "unchanged":
			// No action needed; file didn't change relative to ours.
			continue
		case "clean":
			report.Files = append(report.Files, FileMergeReport{
				Path:        f.Path,
				Status:      "clean",
				EntityCount: f.Conflicts,
			})
			mergedFiles = append(mergedFiles, mergedFile{
				path: f.Path, content: f.Content, mode: f.Mode,
			})
		case "conflict":
			report.Files = append(report.Files, FileMergeReport{
				Path:          f.Path,
				Status:        "conflict",
				ConflictCount: f.Conflicts,
			})
			mergedFiles = append(mergedFiles, mergedFile{
				path: f.Path, content: f.Content, mode: f.Mode,
			})
			// Determine blob hashes for conflict state.
			var bh, oh, th object.Hash
			if base, ok := baseMap[f.Path]; ok {
				bh = base.BlobHash
			}
			if ours, ok := oursMap[f.Path]; ok {
				oh = ours.BlobHash
			}
			if theirs, ok := theirsMap[f.Path]; ok {
				th = theirs.BlobHash
			}
			conflictedFiles = append(conflictedFiles, mergeConflictState{
				path: f.Path, baseHash: bh, oursHash: oh, theirsHash: th, mode: f.Mode,
			})
		case "added":
			report.Files = append(report.Files, FileMergeReport{
				Path:   f.Path,
				Status: "added",
			})
			mergedFiles = append(mergedFiles, mergedFile{
				path: f.Path, content: f.Content, mode: f.Mode,
			})
		case "deleted":
			report.Files = append(report.Files, FileMergeReport{
				Path:   f.Path,
				Status: "deleted",
			})
			deletedPaths = append(deletedPaths, f.Path)
		}
	}

	// 5b. Merge module (gitlink) entries.
	baseModMap := indexModulesByPath(baseModules)
	oursModMap := indexModulesByPath(oursModules)
	theirsModMap := indexModulesByPath(theirsModules)

	modResult, err := r.mergeModuleEntries(baseModMap, oursModMap, theirsModMap)
	if err != nil {
		return nil, fmt.Errorf("merge modules: %w", err)
	}

	if modResult.HasConflicts {
		report.HasConflicts = true
		for _, c := range modResult.Conflicts {
			report.TotalConflicts++
			report.Files = append(report.Files, FileMergeReport{
				Path:          c.Path,
				Status:        "conflict",
				ConflictCount: 1,
			})
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

		// Stage resolved module entries and remove deleted files/modules.
		needsStagingWrite := len(deletedPaths) > 0 ||
			len(modResult.Resolved) > 0 ||
			len(modResult.Removed) > 0
		if needsStagingWrite {
			stg, err := r.ReadStaging()
			if err != nil {
				return nil, fmt.Errorf("merge: read staging: %w", err)
			}
			for _, p := range deletedPaths {
				delete(stg.Entries, p)
			}
			for modPath, commitHash := range modResult.Resolved {
				stg.Entries[modPath] = &StagingEntry{
					Path:     modPath,
					BlobHash: commitHash,
					Mode:     object.TreeModeModule,
				}
			}
			for _, modPath := range modResult.Removed {
				delete(stg.Entries, modPath)
			}
			if err := r.WriteStaging(stg); err != nil {
				return nil, fmt.Errorf("merge: write staging: %w", err)
			}
		}

		// Create merge commit with two parents using the resolved author.
		author := r.ResolveAuthor()
		mergeHash, err := r.commitMerge(
			fmt.Sprintf("Merge branch '%s'", branchName),
			author,
			headHash,
			branchHash,
		)
		if err != nil {
			return nil, fmt.Errorf("merge: commit: %w", err)
		}
		report.MergeCommit = mergeHash

		// Clean up merge state and run post-merge hook.
		r.cleanMergeState()
		_ = r.RunHook(HookPostMerge)
	} else {
		if err := r.stageConflictState(conflictedFiles, deletedPaths); err != nil {
			return nil, fmt.Errorf("merge: stage conflicts: %w", err)
		}
	}

	return report, nil
}

// mergeFastForward performs a fast-forward merge: HEAD is an ancestor of the
// target, so we simply update HEAD and check out the target tree.
func (r *Repo) mergeFastForward(branchName string, headHash, branchHash object.Hash) (*MergeReport, error) {
	branchCommit, err := r.Store.ReadCommit(branchHash)
	if err != nil {
		return nil, fmt.Errorf("merge: read branch commit: %w", err)
	}

	// Check out the target tree.
	if err := r.checkoutTree(branchCommit); err != nil {
		return nil, fmt.Errorf("merge: fast-forward checkout: %w", err)
	}

	// Update the current branch ref to point to the target.
	head, err := r.Head()
	if err != nil {
		return nil, fmt.Errorf("merge: read HEAD: %w", err)
	}
	if strings.HasPrefix(head, "refs/") {
		if err := r.UpdateRefCAS(head, branchHash, headHash); err != nil {
			return nil, fmt.Errorf("merge: update ref %q: %w", head, err)
		}
	} else {
		if err := r.UpdateRefCAS("HEAD", branchHash, headHash); err != nil {
			return nil, fmt.Errorf("merge: update detached HEAD: %w", err)
		}
	}

	r.invalidateStatusCache()
	_ = r.RunHook(HookPostMerge)

	return &MergeReport{
		IsFastForward: true,
		MergeCommit:   branchHash,
	}, nil
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

		entry := &StagingEntry{
			Path:           cf.path,
			BlobHash:       blobHash,
			EntityListHash: "",
			Conflict:       true,
			BaseBlobHash:   cf.baseHash,
			OursBlobHash:   cf.oursHash,
			TheirsBlobHash: cf.theirsHash,
		}
		setStagingEntryStat(entry, info, normalizeFileMode(cf.mode))
		stg.Entries[cf.path] = entry
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

// indexModulesByPath creates a map from module path to TreeModuleEntry.
func indexModulesByPath(modules []TreeModuleEntry) map[string]TreeModuleEntry {
	m := make(map[string]TreeModuleEntry, len(modules))
	for _, mod := range modules {
		m[mod.Path] = mod
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
