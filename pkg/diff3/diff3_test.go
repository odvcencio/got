package diff3

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// 11. MyersDiff basic test
// ---------------------------------------------------------------------------

func TestMyersDiff_Basic(t *testing.T) {
	a := []string{"a", "b", "c"}
	b := []string{"a", "x", "c"}

	ops := MyersDiff(a, b)

	// We expect: Equal "a", Delete "b", Insert "x", Equal "c"
	wantTypes := []DiffType{Equal, Delete, Insert, Equal}
	wantLines := []string{"a", "b", "x", "c"}

	if len(ops) != len(wantTypes) {
		t.Fatalf("got %d ops, want %d: %v", len(ops), len(wantTypes), ops)
	}
	for i, op := range ops {
		if op.Type != wantTypes[i] || op.Line != wantLines[i] {
			t.Errorf("op[%d] = {%v, %q}, want {%v, %q}",
				i, op.Type, op.Line, wantTypes[i], wantLines[i])
		}
	}
}

func TestMyersDiff_EmptyToNonEmpty(t *testing.T) {
	ops := MyersDiff(nil, []string{"a", "b"})
	for _, op := range ops {
		if op.Type != Insert {
			t.Errorf("expected all Insert ops, got %v", op)
		}
	}
	if len(ops) != 2 {
		t.Fatalf("expected 2 ops, got %d", len(ops))
	}
}

func TestMyersDiff_NonEmptyToEmpty(t *testing.T) {
	ops := MyersDiff([]string{"a", "b"}, nil)
	for _, op := range ops {
		if op.Type != Delete {
			t.Errorf("expected all Delete ops, got %v", op)
		}
	}
	if len(ops) != 2 {
		t.Fatalf("expected 2 ops, got %d", len(ops))
	}
}

func TestMyersDiff_Identical(t *testing.T) {
	a := []string{"a", "b", "c"}
	ops := MyersDiff(a, a)
	for _, op := range ops {
		if op.Type != Equal {
			t.Errorf("expected all Equal ops, got %v", op)
		}
	}
}

// ---------------------------------------------------------------------------
// 10. LineDiff basic test
// ---------------------------------------------------------------------------

func TestLineDiff_Basic(t *testing.T) {
	a := []byte("hello\nworld\n")
	b := []byte("hello\ngo\n")

	diffs := LineDiff(a, b)

	// Expect: Equal "hello", Delete "world", Insert "go"
	found := map[DiffType]bool{}
	for _, d := range diffs {
		found[d.Type] = true
	}
	if !found[Equal] {
		t.Error("expected at least one Equal line")
	}
	if !found[Delete] {
		t.Error("expected at least one Delete line")
	}
	if !found[Insert] {
		t.Error("expected at least one Insert line")
	}
}

func TestLineDiff_Identical(t *testing.T) {
	a := []byte("same\ncontent\n")
	diffs := LineDiff(a, a)
	for _, d := range diffs {
		if d.Type != Equal {
			t.Errorf("expected all Equal, got type=%v line=%q", d.Type, d.Content)
		}
	}
}

// ---------------------------------------------------------------------------
// 1. Clean merge — ours adds lines at top, theirs adds lines at bottom
// ---------------------------------------------------------------------------

func TestMerge_CleanTopBottom(t *testing.T) {
	base := []byte("line1\nline2\nline3\n")
	ours := []byte("new-top\nline1\nline2\nline3\n")
	theirs := []byte("line1\nline2\nline3\nnew-bottom\n")

	r := Merge(base, ours, theirs)

	if r.HasConflicts {
		t.Fatal("expected clean merge, got conflicts")
	}

	want := "new-top\nline1\nline2\nline3\nnew-bottom\n"
	if string(r.Merged) != want {
		t.Errorf("merged =\n%s\nwant =\n%s", r.Merged, want)
	}
}

// ---------------------------------------------------------------------------
// 2. Ours-only change — theirs unchanged
// ---------------------------------------------------------------------------

func TestMerge_OursOnly(t *testing.T) {
	base := []byte("aaa\nbbb\nccc\n")
	ours := []byte("aaa\nBBB\nccc\n")
	theirs := []byte("aaa\nbbb\nccc\n") // same as base

	r := Merge(base, ours, theirs)

	if r.HasConflicts {
		t.Fatal("expected clean merge, got conflicts")
	}
	want := "aaa\nBBB\nccc\n"
	if string(r.Merged) != want {
		t.Errorf("merged =\n%s\nwant =\n%s", r.Merged, want)
	}
}

// ---------------------------------------------------------------------------
// 3. Theirs-only change — ours unchanged
// ---------------------------------------------------------------------------

func TestMerge_TheirsOnly(t *testing.T) {
	base := []byte("aaa\nbbb\nccc\n")
	ours := []byte("aaa\nbbb\nccc\n") // same as base
	theirs := []byte("aaa\nBBB\nccc\n")

	r := Merge(base, ours, theirs)

	if r.HasConflicts {
		t.Fatal("expected clean merge, got conflicts")
	}
	want := "aaa\nBBB\nccc\n"
	if string(r.Merged) != want {
		t.Errorf("merged =\n%s\nwant =\n%s", r.Merged, want)
	}
}

// ---------------------------------------------------------------------------
// 4. Conflict — both change same line differently
// ---------------------------------------------------------------------------

func TestMerge_Conflict(t *testing.T) {
	base := []byte("aaa\nbbb\nccc\n")
	ours := []byte("aaa\nOURS\nccc\n")
	theirs := []byte("aaa\nTHEIRS\nccc\n")

	r := Merge(base, ours, theirs)

	if !r.HasConflicts {
		t.Fatal("expected conflicts, got clean merge")
	}

	// The merged output should contain conflict markers.
	if !bytes.Contains(r.Merged, []byte("<<<<<<<")) {
		t.Error("merged output missing <<<<<<< marker")
	}
	if !bytes.Contains(r.Merged, []byte("=======")) {
		t.Error("merged output missing ======= marker")
	}
	if !bytes.Contains(r.Merged, []byte(">>>>>>>")) {
		t.Error("merged output missing >>>>>>> marker")
	}

	// There should be at least one conflict hunk.
	hasConflictHunk := false
	for _, h := range r.Hunks {
		if h.Type == HunkConflict {
			hasConflictHunk = true
		}
	}
	if !hasConflictHunk {
		t.Error("expected at least one HunkConflict in Hunks")
	}
}

// ---------------------------------------------------------------------------
// 5. Both make identical change — no conflict
// ---------------------------------------------------------------------------

func TestMerge_IdenticalChange(t *testing.T) {
	base := []byte("aaa\nbbb\nccc\n")
	ours := []byte("aaa\nSAME\nccc\n")
	theirs := []byte("aaa\nSAME\nccc\n")

	r := Merge(base, ours, theirs)

	if r.HasConflicts {
		t.Fatal("expected clean merge when both sides make the same change")
	}
	want := "aaa\nSAME\nccc\n"
	if string(r.Merged) != want {
		t.Errorf("merged =\n%s\nwant =\n%s", r.Merged, want)
	}
}

// ---------------------------------------------------------------------------
// 6. Non-overlapping inserts in different parts of file — clean merge
// ---------------------------------------------------------------------------

func TestMerge_NonOverlappingInserts(t *testing.T) {
	base := []byte("aaa\nbbb\nccc\nddd\neee\n")
	ours := []byte("aaa\nOUR-INSERT\nbbb\nccc\nddd\neee\n")
	theirs := []byte("aaa\nbbb\nccc\nddd\nTHEIR-INSERT\neee\n")

	r := Merge(base, ours, theirs)

	if r.HasConflicts {
		t.Fatalf("expected clean merge, got conflicts:\n%s", r.Merged)
	}

	want := "aaa\nOUR-INSERT\nbbb\nccc\nddd\nTHEIR-INSERT\neee\n"
	if string(r.Merged) != want {
		t.Errorf("merged =\n%s\nwant =\n%s", r.Merged, want)
	}
}

// ---------------------------------------------------------------------------
// 7. Delete vs modify — conflict
// ---------------------------------------------------------------------------

func TestMerge_DeleteVsModify(t *testing.T) {
	base := []byte("aaa\nbbb\nccc\n")
	ours := []byte("aaa\nccc\n")            // deleted "bbb"
	theirs := []byte("aaa\nBBB-MOD\nccc\n") // modified "bbb"

	r := Merge(base, ours, theirs)

	if !r.HasConflicts {
		t.Fatal("expected conflict when one side deletes and the other modifies")
	}
}

// ---------------------------------------------------------------------------
// 8. Empty inputs
// ---------------------------------------------------------------------------

func TestMerge_EmptyBase(t *testing.T) {
	base := []byte("")
	ours := []byte("hello\n")
	theirs := []byte("world\n")

	r := Merge(base, ours, theirs)

	// Both sides added content to an empty base — this is a conflict
	// since both inserted at the same position.
	if !r.HasConflicts {
		t.Fatal("expected conflict when both sides add to empty base")
	}
}

func TestMerge_EmptyOurs(t *testing.T) {
	base := []byte("aaa\nbbb\n")
	ours := []byte("")
	theirs := []byte("aaa\nbbb\n") // same as base

	r := Merge(base, ours, theirs)

	if r.HasConflicts {
		t.Fatal("expected clean merge")
	}
	// Ours deleted everything, theirs unchanged → take ours.
	if string(r.Merged) != "" {
		t.Errorf("merged = %q, want empty", r.Merged)
	}
}

func TestMerge_EmptyTheirs(t *testing.T) {
	base := []byte("aaa\nbbb\n")
	ours := []byte("aaa\nbbb\n") // same as base
	theirs := []byte("")

	r := Merge(base, ours, theirs)

	if r.HasConflicts {
		t.Fatal("expected clean merge")
	}
	if string(r.Merged) != "" {
		t.Errorf("merged = %q, want empty", r.Merged)
	}
}

func TestMerge_AllEmpty(t *testing.T) {
	r := Merge([]byte{}, []byte{}, []byte{})
	if r.HasConflicts {
		t.Fatal("expected clean merge for all-empty inputs")
	}
	if len(r.Merged) != 0 {
		t.Errorf("expected empty merged, got %q", r.Merged)
	}
}

// ---------------------------------------------------------------------------
// 9. Large file performance sanity check
// ---------------------------------------------------------------------------

func TestMerge_LargeFile(t *testing.T) {
	var baseBuf, oursBuf, theirsBuf strings.Builder
	const n = 2000

	for i := 0; i < n; i++ {
		line := fmt.Sprintf("line-%04d\n", i)
		baseBuf.WriteString(line)
		oursBuf.WriteString(line)
		theirsBuf.WriteString(line)
	}

	// Ours changes line 100.
	oursLines := strings.Split(oursBuf.String(), "\n")
	oursLines[100] = "OURS-CHANGED"
	oursContent := []byte(strings.Join(oursLines, "\n"))

	// Theirs changes line 1900.
	theirsLines := strings.Split(theirsBuf.String(), "\n")
	theirsLines[1900] = "THEIRS-CHANGED"
	theirsContent := []byte(strings.Join(theirsLines, "\n"))

	base := []byte(baseBuf.String())

	start := time.Now()
	r := Merge(base, oursContent, theirsContent)
	elapsed := time.Since(start)

	if r.HasConflicts {
		t.Fatal("expected clean merge for non-overlapping changes")
	}

	if elapsed > 5*time.Second {
		t.Fatalf("merge took %v, expected < 5s for %d lines", elapsed, n)
	}

	if !bytes.Contains(r.Merged, []byte("OURS-CHANGED")) {
		t.Error("merged output missing OURS-CHANGED")
	}
	if !bytes.Contains(r.Merged, []byte("THEIRS-CHANGED")) {
		t.Error("merged output missing THEIRS-CHANGED")
	}
}
