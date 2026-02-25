package repo

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/odvcencio/got/pkg/object"
)

func setMergeBaseTraversalLimitsForTest(t *testing.T, maxSteps, maxDepth int) {
	t.Helper()

	prevSteps := mergeBaseBFSStepsLimit
	prevDepth := mergeBaseBFSDepthLimit
	mergeBaseBFSStepsLimit = maxSteps
	mergeBaseBFSDepthLimit = maxDepth

	t.Cleanup(func() {
		mergeBaseBFSStepsLimit = prevSteps
		mergeBaseBFSDepthLimit = prevDepth
	})
}

func writeMergeBaseSafetyCommit(t *testing.T, r *Repo, treeHash object.Hash, parents []object.Hash, message string) object.Hash {
	t.Helper()

	h, err := r.Store.WriteCommit(&object.CommitObj{
		TreeHash:  treeHash,
		Parents:   parents,
		Author:    "test-author",
		Timestamp: 1_700_000_000,
		Message:   message,
	})
	if err != nil {
		t.Fatalf("WriteCommit(%q): %v", message, err)
	}
	return h
}

func writeCorruptCommitAtHash(t *testing.T, r *Repo, h object.Hash, commit *object.CommitObj) {
	t.Helper()

	data := object.MarshalCommit(commit)
	raw := []byte(fmt.Sprintf("%s %d\x00", object.TypeCommit, len(data)))
	raw = append(raw, data...)

	objPath := filepath.Join(r.GotDir, "objects", string(h[:2]), string(h[2:]))
	if err := os.MkdirAll(filepath.Dir(objPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", objPath, err)
	}
	if err := os.WriteFile(objPath, raw, 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", objPath, err)
	}
}

func buildDivergedTipsForMergeBaseSafety(t *testing.T, r *Repo) (leftTip, rightTip object.Hash) {
	t.Helper()

	treeHash, err := r.Store.WriteTree(&object.TreeObj{})
	if err != nil {
		t.Fatalf("WriteTree: %v", err)
	}

	base := writeMergeBaseSafetyCommit(t, r, treeHash, nil, "base")
	left1 := writeMergeBaseSafetyCommit(t, r, treeHash, []object.Hash{base}, "left-1")
	leftTip = writeMergeBaseSafetyCommit(t, r, treeHash, []object.Hash{left1}, "left-2")

	right1 := writeMergeBaseSafetyCommit(t, r, treeHash, []object.Hash{base}, "right-1")
	rightTip = writeMergeBaseSafetyCommit(t, r, treeHash, []object.Hash{right1}, "right-2")

	return leftTip, rightTip
}

func TestFindMergeBase_CorruptCycleGraphReturnsError(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	treeHash, err := r.Store.WriteTree(&object.TreeObj{})
	if err != nil {
		t.Fatalf("WriteTree: %v", err)
	}

	commitA := writeMergeBaseSafetyCommit(t, r, treeHash, nil, "A")
	commitB := writeMergeBaseSafetyCommit(t, r, treeHash, []object.Hash{commitA}, "B")

	corruptA, err := r.Store.ReadCommit(commitA)
	if err != nil {
		t.Fatalf("ReadCommit(A): %v", err)
	}
	corruptA.Parents = []object.Hash{commitB}
	writeCorruptCommitAtHash(t, r, commitA, corruptA)

	_, err = r.FindMergeBase(commitA, commitB)
	if err == nil {
		t.Fatal("expected cycle error, got nil")
	}
	if !strings.Contains(err.Error(), "cycle detected") {
		t.Fatalf("FindMergeBase cycle error = %q, want to contain %q", err, "cycle detected")
	}
}

func TestFindMergeBase_TraversalDepthLimit(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	leftTip, rightTip := buildDivergedTipsForMergeBaseSafety(t, r)
	setMergeBaseTraversalLimitsForTest(t, maxMergeBaseBFSSteps, 1)

	_, err = r.FindMergeBase(leftTip, rightTip)
	if err == nil {
		t.Fatal("expected depth-limit error, got nil")
	}
	if !strings.Contains(err.Error(), "maximum depth") {
		t.Fatalf("FindMergeBase depth-limit error = %q, want to contain %q", err, "maximum depth")
	}
}

func TestFindMergeBase_TraversalStepLimit(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	leftTip, rightTip := buildDivergedTipsForMergeBaseSafety(t, r)
	setMergeBaseTraversalLimitsForTest(t, 1, maxMergeBaseBFSDepth)

	_, err = r.FindMergeBase(leftTip, rightTip)
	if err == nil {
		t.Fatal("expected step-limit error, got nil")
	}
	if !strings.Contains(err.Error(), "maximum steps") {
		t.Fatalf("FindMergeBase step-limit error = %q, want to contain %q", err, "maximum steps")
	}
}

func TestMergeBaseTraversalLimits_AreBounded(t *testing.T) {
	setMergeBaseTraversalLimitsForTest(t, maxMergeBaseBFSSteps+42, maxMergeBaseBFSDepth+42)

	steps, depth := mergeBaseTraversalLimits()
	if steps != maxMergeBaseBFSSteps {
		t.Fatalf("steps limit = %d, want hard max %d", steps, maxMergeBaseBFSSteps)
	}
	if depth != maxMergeBaseBFSDepth {
		t.Fatalf("depth limit = %d, want hard max %d", depth, maxMergeBaseBFSDepth)
	}

	setMergeBaseTraversalLimitsForTest(t, 0, -1)
	steps, depth = mergeBaseTraversalLimits()
	if steps != maxMergeBaseBFSSteps {
		t.Fatalf("non-positive steps limit fallback = %d, want %d", steps, maxMergeBaseBFSSteps)
	}
	if depth != maxMergeBaseBFSDepth {
		t.Fatalf("non-positive depth limit fallback = %d, want %d", depth, maxMergeBaseBFSDepth)
	}
}
