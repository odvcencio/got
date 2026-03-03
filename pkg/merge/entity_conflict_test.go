package merge

import (
	"strings"
	"testing"
)

// TestEntityConflictDetail_BothModified verifies that when both sides modify
// the same function, the MergeResult includes an EntityConflictDetail with
// Type="both_modified".
func TestEntityConflictDetail_BothModified(t *testing.T) {
	base := `package main

func A() {
	return 0
}
`
	ours := `package main

func A() {
	return 1
}
`
	theirs := `package main

func A() {
	return 2
}
`

	result, err := MergeFiles("test.go", []byte(base), []byte(ours), []byte(theirs))
	if err != nil {
		t.Fatalf("MergeFiles failed: %v", err)
	}

	if !result.HasConflicts {
		t.Fatal("expected conflicts")
	}

	if len(result.EntityConflicts) == 0 {
		t.Fatal("expected EntityConflicts to be populated")
	}

	found := false
	for _, ec := range result.EntityConflicts {
		if ec.Name == "func A" && ec.Type == "both_modified" {
			found = true
			if ec.DeclKind != "function_declaration" {
				t.Errorf("expected DeclKind=function_declaration, got %q", ec.DeclKind)
			}
			break
		}
	}
	if !found {
		t.Errorf("expected EntityConflictDetail for func A with both_modified, got: %+v", result.EntityConflicts)
	}
}

// TestEntityConflictDetail_DeleteVsModify verifies that when one side deletes
// and the other modifies, the conflict detail has Type="delete_vs_modify".
func TestEntityConflictDetail_DeleteVsModify(t *testing.T) {
	base := `package main

func A() {
	return 0
}

func B() {
	return 0
}
`
	// Ours: modifies B
	ours := `package main

func A() {
	return 0
}

func B() {
	return 99
}
`
	// Theirs: deletes B
	theirs := `package main

func A() {
	return 0
}
`

	result, err := MergeFiles("test.go", []byte(base), []byte(ours), []byte(theirs))
	if err != nil {
		t.Fatalf("MergeFiles failed: %v", err)
	}

	if !result.HasConflicts {
		t.Fatal("expected conflicts")
	}

	found := false
	for _, ec := range result.EntityConflicts {
		if ec.Name == "func B" && ec.Type == "delete_vs_modify" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected EntityConflictDetail for func B with delete_vs_modify, got: %+v", result.EntityConflicts)
	}
}

// TestEntityConflictDetail_MultipleConflicts verifies that when multiple
// entities conflict, all of them appear in EntityConflicts.
func TestEntityConflictDetail_MultipleConflicts(t *testing.T) {
	base := `package main

func A() {
	return 0
}

func B() {
	return 0
}
`
	ours := `package main

func A() {
	return 1
}

func B() {
	return 1
}
`
	theirs := `package main

func A() {
	return 2
}

func B() {
	return 2
}
`

	result, err := MergeFiles("test.go", []byte(base), []byte(ours), []byte(theirs))
	if err != nil {
		t.Fatalf("MergeFiles failed: %v", err)
	}

	if result.ConflictCount < 2 {
		t.Fatalf("expected at least 2 conflicts, got %d", result.ConflictCount)
	}

	if len(result.EntityConflicts) < 2 {
		t.Fatalf("expected at least 2 EntityConflicts, got %d", len(result.EntityConflicts))
	}

	names := map[string]bool{}
	for _, ec := range result.EntityConflicts {
		names[ec.Name] = true
	}
	if !names["func A"] {
		t.Error("expected EntityConflictDetail for func A")
	}
	if !names["func B"] {
		t.Error("expected EntityConflictDetail for func B")
	}
}

// TestEntityConflictDetail_CleanMergeHasNoDetails verifies that clean merges
// produce an empty EntityConflicts slice.
func TestEntityConflictDetail_CleanMergeHasNoDetails(t *testing.T) {
	base := `package main

func A() {
	return 0
}
`
	ours := `package main

func A() {
	return 1
}
`
	theirs := `package main

func A() {
	return 0
}
`

	result, err := MergeFiles("test.go", []byte(base), []byte(ours), []byte(theirs))
	if err != nil {
		t.Fatalf("MergeFiles failed: %v", err)
	}

	if result.HasConflicts {
		t.Fatal("expected no conflicts")
	}
	if len(result.EntityConflicts) != 0 {
		t.Errorf("expected empty EntityConflicts for clean merge, got %+v", result.EntityConflicts)
	}
}

// TestEntityConflictDetail_ConflictMarkersIncludeEntityName verifies that
// conflict markers in the merged output include the entity display name.
func TestEntityConflictDetail_ConflictMarkersIncludeEntityName(t *testing.T) {
	base := `package main

func ProcessOrder() {
	return 0
}
`
	ours := `package main

func ProcessOrder() {
	return 1
}
`
	theirs := `package main

func ProcessOrder() {
	return 2
}
`

	result, err := MergeFiles("test.go", []byte(base), []byte(ours), []byte(theirs))
	if err != nil {
		t.Fatalf("MergeFiles failed: %v", err)
	}

	merged := string(result.Merged)

	if !strings.Contains(merged, "<<<<<<< ours (func ProcessOrder)") {
		t.Errorf("expected conflict marker to include entity name, got:\n%s", merged)
	}
	if !strings.Contains(merged, ">>>>>>> theirs (func ProcessOrder)") {
		t.Errorf("expected closing conflict marker to include entity name, got:\n%s", merged)
	}
}

// TestEntityConflictDetail_FallbackMergeHasNoDetails verifies that text
// fallback merges (unsupported file types) produce empty EntityConflicts.
func TestEntityConflictDetail_FallbackMergeHasNoDetails(t *testing.T) {
	base := []byte("line-a\nline-b\n")
	ours := []byte("line-a-ours\nline-b\n")
	theirs := []byte("line-a-theirs\nline-b\n")

	result, err := MergeFiles("notes.txt", base, ours, theirs)
	if err != nil {
		t.Fatalf("MergeFiles failed: %v", err)
	}

	// Text fallback conflicts should not produce entity conflict details.
	if len(result.EntityConflicts) != 0 {
		t.Errorf("expected empty EntityConflicts for text fallback, got %+v", result.EntityConflicts)
	}
}
