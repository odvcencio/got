package diff

import (
	"strings"
	"testing"

	"github.com/odvcencio/got/pkg/entity"
)

// --- Go source snippets used across tests ---

const goBase = `package main

import "fmt"

func Hello() {
	fmt.Println("hello")
}

func Goodbye() {
	fmt.Println("goodbye")
}
`

const goAddedFunc = `package main

import "fmt"

func Hello() {
	fmt.Println("hello")
}

func ValidateInput() {
	fmt.Println("validate")
}

func Goodbye() {
	fmt.Println("goodbye")
}
`

const goRemovedFunc = `package main

import "fmt"

func Goodbye() {
	fmt.Println("goodbye")
}
`

const goModifiedFunc = `package main

import "fmt"

func Hello() {
	fmt.Println("hello, world!")
}

func Goodbye() {
	fmt.Println("goodbye")
}
`

// Test 1: Added function — after has a function not in before → one Added change.
func TestDiffFiles_AddedFunction(t *testing.T) {
	d, err := DiffFiles("main.go", []byte(goBase), []byte(goAddedFunc))
	if err != nil {
		t.Fatalf("DiffFiles failed: %v", err)
	}
	if d.Path != "main.go" {
		t.Errorf("expected path %q, got %q", "main.go", d.Path)
	}

	added := filterDeclChanges(d.Changes, Added)
	if len(added) != 1 {
		t.Fatalf("expected 1 Added declaration change, got %d: %v", len(added), describeChanges(d.Changes))
	}
	if !strings.Contains(added[0].Key, "ValidateInput") {
		t.Errorf("expected Added key to contain 'ValidateInput', got %q", added[0].Key)
	}
	if added[0].Before != nil {
		t.Error("Added change should have nil Before")
	}
	if added[0].After == nil {
		t.Error("Added change should have non-nil After")
	}
}

// Test 2: Removed function — before has function not in after → one Removed change.
func TestDiffFiles_RemovedFunction(t *testing.T) {
	d, err := DiffFiles("main.go", []byte(goBase), []byte(goRemovedFunc))
	if err != nil {
		t.Fatalf("DiffFiles failed: %v", err)
	}

	removed := filterDeclChanges(d.Changes, Removed)
	if len(removed) != 1 {
		t.Fatalf("expected 1 Removed declaration change, got %d: %v", len(removed), describeChanges(d.Changes))
	}
	if !strings.Contains(removed[0].Key, "Hello") {
		t.Errorf("expected Removed key to contain 'Hello', got %q", removed[0].Key)
	}
	if removed[0].Before == nil {
		t.Error("Removed change should have non-nil Before")
	}
	if removed[0].After != nil {
		t.Error("Removed change should have nil After")
	}
}

// Test 3: Modified function body — same function, different body → one Modified change.
func TestDiffFiles_ModifiedFunction(t *testing.T) {
	d, err := DiffFiles("main.go", []byte(goBase), []byte(goModifiedFunc))
	if err != nil {
		t.Fatalf("DiffFiles failed: %v", err)
	}

	modified := filterDeclChanges(d.Changes, Modified)
	if len(modified) != 1 {
		t.Fatalf("expected 1 Modified declaration change, got %d: %v", len(modified), describeChanges(d.Changes))
	}
	if !strings.Contains(modified[0].Key, "Hello") {
		t.Errorf("expected Modified key to contain 'Hello', got %q", modified[0].Key)
	}
	if modified[0].Before == nil {
		t.Error("Modified change should have non-nil Before")
	}
	if modified[0].After == nil {
		t.Error("Modified change should have non-nil After")
	}
}

// Test 4: Unchanged file → empty diff (no changes).
func TestDiffFiles_Unchanged(t *testing.T) {
	d, err := DiffFiles("main.go", []byte(goBase), []byte(goBase))
	if err != nil {
		t.Fatalf("DiffFiles failed: %v", err)
	}
	if len(d.Changes) != 0 {
		t.Errorf("expected 0 changes for identical files, got %d: %v",
			len(d.Changes), describeChanges(d.Changes))
	}
}

// Test 5: FormatEntityDiff output contains +, ~, - markers.
func TestFormatEntityDiff(t *testing.T) {
	// Build a FileDiff with all three change types for formatting.
	d, err := DiffFiles("main.go", []byte(goBase), []byte(goAddedFunc))
	if err != nil {
		t.Fatalf("DiffFiles failed: %v", err)
	}
	out := FormatEntityDiff(d)
	if !strings.Contains(out, "+") {
		t.Errorf("FormatEntityDiff output should contain '+' marker for Added, got:\n%s", out)
	}

	// Also test with a removed function.
	d2, err := DiffFiles("main.go", []byte(goBase), []byte(goRemovedFunc))
	if err != nil {
		t.Fatalf("DiffFiles failed: %v", err)
	}
	out2 := FormatEntityDiff(d2)
	if !strings.Contains(out2, "-") {
		t.Errorf("FormatEntityDiff output should contain '-' marker for Removed, got:\n%s", out2)
	}

	// And a modified function.
	d3, err := DiffFiles("main.go", []byte(goBase), []byte(goModifiedFunc))
	if err != nil {
		t.Fatalf("DiffFiles failed: %v", err)
	}
	out3 := FormatEntityDiff(d3)
	if !strings.Contains(out3, "~") {
		t.Errorf("FormatEntityDiff output should contain '~' marker for Modified, got:\n%s", out3)
	}
}

// Test 6: FormatLineDiff output contains --- and +++ headers.
func TestFormatLineDiff(t *testing.T) {
	d, err := DiffFiles("main.go", []byte(goBase), []byte(goModifiedFunc))
	if err != nil {
		t.Fatalf("DiffFiles failed: %v", err)
	}
	out := FormatLineDiff(d)
	if !strings.Contains(out, "---") {
		t.Errorf("FormatLineDiff output should contain '---' header, got:\n%s", out)
	}
	if !strings.Contains(out, "+++") {
		t.Errorf("FormatLineDiff output should contain '+++' header, got:\n%s", out)
	}
}

// --- helpers ---

func filterChanges(changes []EntityChange, ct ChangeType) []EntityChange {
	var out []EntityChange
	for _, c := range changes {
		if c.Type == ct {
			out = append(out, c)
		}
	}
	return out
}

// filterDeclChanges returns only changes whose entity is a KindDeclaration.
func filterDeclChanges(changes []EntityChange, ct ChangeType) []EntityChange {
	var out []EntityChange
	for _, c := range changes {
		if c.Type != ct {
			continue
		}
		e := c.After
		if e == nil {
			e = c.Before
		}
		if e != nil && e.Kind == entity.KindDeclaration {
			out = append(out, c)
		}
	}
	return out
}

func describeChanges(changes []EntityChange) string {
	var parts []string
	for _, c := range changes {
		var typeStr string
		switch c.Type {
		case Added:
			typeStr = "Added"
		case Removed:
			typeStr = "Removed"
		case Modified:
			typeStr = "Modified"
		}
		parts = append(parts, typeStr+":"+c.Key)
	}
	return strings.Join(parts, ", ")
}
