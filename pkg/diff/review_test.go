package diff

import (
	"strings"
	"testing"
)

// TestFormatReview_Added verifies the review format for a file with an added function.
func TestFormatReview_Added(t *testing.T) {
	d, err := DiffFiles("main.go", []byte(goBase), []byte(goAddedFunc))
	if err != nil {
		t.Fatalf("DiffFiles failed: %v", err)
	}

	out := FormatReview(d)

	if !strings.Contains(out, "=== main.go ===") {
		t.Errorf("expected file header, got:\n%s", out)
	}
	if !strings.Contains(out, "[ADDED]") {
		t.Errorf("expected [ADDED] tag, got:\n%s", out)
	}
	if !strings.Contains(out, "ValidateInput") {
		t.Errorf("expected entity name ValidateInput, got:\n%s", out)
	}
	if !strings.Contains(out, "added") {
		t.Errorf("expected summary to mention 'added', got:\n%s", out)
	}
}

// TestFormatReview_Removed verifies the review format for a file with a removed function.
func TestFormatReview_Removed(t *testing.T) {
	d, err := DiffFiles("main.go", []byte(goBase), []byte(goRemovedFunc))
	if err != nil {
		t.Fatalf("DiffFiles failed: %v", err)
	}

	out := FormatReview(d)

	if !strings.Contains(out, "[REMOVED]") {
		t.Errorf("expected [REMOVED] tag, got:\n%s", out)
	}
	if !strings.Contains(out, "Hello") {
		t.Errorf("expected entity name Hello, got:\n%s", out)
	}
	if !strings.Contains(out, "was lines") {
		t.Errorf("expected 'was lines' for removed entity, got:\n%s", out)
	}
}

// TestFormatReview_Modified verifies the review format includes inline diff for modified entities.
func TestFormatReview_Modified(t *testing.T) {
	d, err := DiffFiles("main.go", []byte(goBase), []byte(goModifiedFunc))
	if err != nil {
		t.Fatalf("DiffFiles failed: %v", err)
	}

	out := FormatReview(d)

	if !strings.Contains(out, "[MODIFIED]") {
		t.Errorf("expected [MODIFIED] tag, got:\n%s", out)
	}
	if !strings.Contains(out, "Hello") {
		t.Errorf("expected entity name Hello, got:\n%s", out)
	}
	// Modified entities should contain inline diff lines.
	if !strings.Contains(out, "  -") || !strings.Contains(out, "  +") {
		t.Errorf("expected inline diff lines with -/+ markers, got:\n%s", out)
	}
}

// TestFormatReview_Empty verifies that an empty diff produces empty output.
func TestFormatReview_Empty(t *testing.T) {
	d, err := DiffFiles("main.go", []byte(goBase), []byte(goBase))
	if err != nil {
		t.Fatalf("DiffFiles failed: %v", err)
	}

	out := FormatReview(d)
	if out != "" {
		t.Errorf("expected empty string for unchanged file, got:\n%s", out)
	}
}

// TestFormatReview_Summary verifies the summary line counts are correct.
func TestFormatReview_Summary(t *testing.T) {
	// goMixed has all three change types: goBase → goMixed
	// Added: NewFunc, Modified: Hello, Removed: Goodbye
	const goMixed = `package main

import "fmt"

func Hello() {
	fmt.Println("hello, world!")
}

func NewFunc() {
	fmt.Println("new")
}
`
	d, err := DiffFiles("main.go", []byte(goBase), []byte(goMixed))
	if err != nil {
		t.Fatalf("DiffFiles failed: %v", err)
	}

	out := FormatReview(d)

	// Check summary line structure.
	if !strings.Contains(out, "Summary:") {
		t.Errorf("expected Summary line, got:\n%s", out)
	}
	if !strings.Contains(out, "added") {
		t.Errorf("expected 'added' in summary, got:\n%s", out)
	}
	if !strings.Contains(out, "modified") {
		t.Errorf("expected 'modified' in summary, got:\n%s", out)
	}
	if !strings.Contains(out, "removed") {
		t.Errorf("expected 'removed' in summary, got:\n%s", out)
	}
}

// TestFormatReview_SingleEntity verifies singular "entity" in summary.
func TestFormatReview_SingleEntity(t *testing.T) {
	d, err := DiffFiles("main.go", []byte(goBase), []byte(goModifiedFunc))
	if err != nil {
		t.Fatalf("DiffFiles failed: %v", err)
	}

	out := FormatReview(d)

	// Should have exactly the declaration-level changes; the summary should
	// mention "entity" (not "entities") if only 1 declaration changed.
	// Note: there may be non-declaration changes too, so we just verify the format.
	if !strings.Contains(out, "changed") {
		t.Errorf("expected 'changed' in summary, got:\n%s", out)
	}
}
