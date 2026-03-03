package repo

import (
	"os"
	"path/filepath"
	"testing"
)

func TestListConflicts_AfterConflictedMerge(t *testing.T) {
	r, dir := setupMergeRepo(t)

	// On main: modify func A.
	oursContent := `package main

func A() { println("ours") }
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(oursContent), 0o644); err != nil {
		t.Fatalf("write main.go (ours): %v", err)
	}
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add main.go (ours): %v", err)
	}
	if _, err := r.Commit("modify A on main", "test-author"); err != nil {
		t.Fatalf("Commit (ours): %v", err)
	}

	// Switch to feature branch.
	if err := r.Checkout("feature"); err != nil {
		t.Fatalf("Checkout(feature): %v", err)
	}

	// On feature: modify func A differently.
	theirsContent := `package main

func A() { println("theirs") }
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(theirsContent), 0o644); err != nil {
		t.Fatalf("write main.go (theirs): %v", err)
	}
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add main.go (theirs): %v", err)
	}
	if _, err := r.Commit("modify A on feature", "test-author"); err != nil {
		t.Fatalf("Commit (theirs): %v", err)
	}

	// Switch back to main and merge.
	if err := r.Checkout("main"); err != nil {
		t.Fatalf("Checkout(main): %v", err)
	}
	report, err := r.Merge("feature")
	if err != nil {
		t.Fatalf("Merge(feature): %v", err)
	}
	if !report.HasConflicts {
		t.Fatal("expected conflicts")
	}

	// Now test ListConflicts.
	conflicts, err := r.ListConflicts()
	if err != nil {
		t.Fatalf("ListConflicts: %v", err)
	}

	if len(conflicts) == 0 {
		t.Fatal("expected at least one conflict entry")
	}

	found := false
	for _, c := range conflicts {
		if c.Path == "main.go" && c.EntityName == "func A" {
			found = true
			if c.ConflictType != "both_modified" {
				t.Errorf("expected ConflictType=both_modified, got %q", c.ConflictType)
			}
			break
		}
	}
	if !found {
		t.Errorf("expected conflict entry for main.go func A, got: %+v", conflicts)
	}
}

func TestListConflicts_NoConflicts(t *testing.T) {
	r, dir := setupMergeRepo(t)

	// Create a simple commit, no merge, no conflicts.
	content := `package main

func A() { println("a") }
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	conflicts, err := r.ListConflicts()
	if err != nil {
		t.Fatalf("ListConflicts: %v", err)
	}
	if len(conflicts) != 0 {
		t.Errorf("expected no conflicts, got %+v", conflicts)
	}
}

func TestListConflicts_MultipleEntitiesInSameFile(t *testing.T) {
	r, dir := setupMergeRepo(t)

	// On main: modify both A and add B.
	oursContent := `package main

func A() { println("ours-a") }

func B() { println("ours-b") }
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(oursContent), 0o644); err != nil {
		t.Fatalf("write main.go (ours): %v", err)
	}
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add (ours): %v", err)
	}
	if _, err := r.Commit("ours changes", "test-author"); err != nil {
		t.Fatalf("Commit (ours): %v", err)
	}

	// Switch to feature.
	if err := r.Checkout("feature"); err != nil {
		t.Fatalf("Checkout(feature): %v", err)
	}

	// On feature: modify A differently and add B differently.
	theirsContent := `package main

func A() { println("theirs-a") }

func B() { println("theirs-b") }
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(theirsContent), 0o644); err != nil {
		t.Fatalf("write main.go (theirs): %v", err)
	}
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add (theirs): %v", err)
	}
	if _, err := r.Commit("theirs changes", "test-author"); err != nil {
		t.Fatalf("Commit (theirs): %v", err)
	}

	// Switch back and merge.
	if err := r.Checkout("main"); err != nil {
		t.Fatalf("Checkout(main): %v", err)
	}
	report, err := r.Merge("feature")
	if err != nil {
		t.Fatalf("Merge(feature): %v", err)
	}
	if !report.HasConflicts {
		t.Fatal("expected conflicts")
	}

	conflicts, err := r.ListConflicts()
	if err != nil {
		t.Fatalf("ListConflicts: %v", err)
	}

	// We should have at least 2 conflict entries for the two functions.
	entityNames := map[string]bool{}
	for _, c := range conflicts {
		if c.Path == "main.go" && c.EntityName != "" {
			entityNames[c.EntityName] = true
		}
	}

	if !entityNames["func A"] {
		t.Errorf("expected conflict entry for func A, got names: %v", entityNames)
	}
	if !entityNames["func B"] {
		t.Errorf("expected conflict entry for func B, got names: %v", entityNames)
	}
}

func TestExtractAnnotation(t *testing.T) {
	tests := []struct {
		line string
		want string
	}{
		{"<<<<<<< ours (func ProcessOrder)", "func ProcessOrder"},
		{"<<<<<<< ours (func (OrderService) Process)", "func (OrderService) Process"},
		{"<<<<<<< ours (import_block:0)", "import_block:0"},
		{"<<<<<<< ours", ""},
		{"some random line", ""},
	}
	for _, tt := range tests {
		got := extractAnnotation(tt.line)
		if got != tt.want {
			t.Errorf("extractAnnotation(%q) = %q, want %q", tt.line, got, tt.want)
		}
	}
}
