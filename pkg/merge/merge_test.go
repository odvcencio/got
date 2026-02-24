package merge

import (
	"bytes"
	"strings"
	"testing"

	"github.com/odvcencio/got/pkg/entity"
)

// --- Test data helpers ---
// These build real Go source strings that entity.Extract can parse.

// baseSrc: package + import "fmt" + func A
const baseSrcOneFunc = `package main

import "fmt"

func A() {
	fmt.Println("a")
}
`

// ours adds func B after A
const oursSrcAddB = `package main

import "fmt"

func A() {
	fmt.Println("a")
}

func B() {
	fmt.Println("b")
}
`

// theirs adds func C after A
const theirsSrcAddC = `package main

import "fmt"

func A() {
	fmt.Println("a")
}

func C() {
	fmt.Println("c")
}
`

// TestMergeIndependentAdditions verifies that when ours adds func B and theirs
// adds func C, all three functions appear in the merged output with no conflicts.
func TestMergeIndependentAdditions(t *testing.T) {
	result, err := MergeFiles("test.go", []byte(baseSrcOneFunc), []byte(oursSrcAddB), []byte(theirsSrcAddC))
	if err != nil {
		t.Fatalf("MergeFiles failed: %v", err)
	}

	merged := string(result.Merged)

	if result.HasConflicts {
		t.Errorf("expected no conflicts, but HasConflicts=true\nmerged output:\n%s", merged)
	}
	if result.ConflictCount != 0 {
		t.Errorf("expected ConflictCount=0, got %d", result.ConflictCount)
	}

	// All three functions should be present
	for _, fn := range []string{"func A()", "func B()", "func C()"} {
		if !strings.Contains(merged, fn) {
			t.Errorf("merged output missing %q\nmerged:\n%s", fn, merged)
		}
	}

	// Package and import should still be present
	if !strings.Contains(merged, "package main") {
		t.Errorf("merged output missing package declaration")
	}

	// Stats checks
	if result.Stats.Added < 2 {
		t.Errorf("expected at least 2 added entities, got %d", result.Stats.Added)
	}
}

// TestMergeIndependentModifications verifies that when ours modifies A and
// theirs modifies B, both modifications appear cleanly.
func TestMergeIndependentModifications(t *testing.T) {
	base := `package main

import "fmt"

func A() {
	fmt.Println("a")
}

func B() {
	fmt.Println("b")
}
`
	ours := `package main

import "fmt"

func A() {
	fmt.Println("a-modified-by-ours")
}

func B() {
	fmt.Println("b")
}
`
	theirs := `package main

import "fmt"

func A() {
	fmt.Println("a")
}

func B() {
	fmt.Println("b-modified-by-theirs")
}
`

	result, err := MergeFiles("test.go", []byte(base), []byte(ours), []byte(theirs))
	if err != nil {
		t.Fatalf("MergeFiles failed: %v", err)
	}

	merged := string(result.Merged)

	if result.HasConflicts {
		t.Errorf("expected no conflicts, but HasConflicts=true\nmerged output:\n%s", merged)
	}

	// Ours' modification to A should be present
	if !strings.Contains(merged, "a-modified-by-ours") {
		t.Errorf("merged output missing ours' modification to A\nmerged:\n%s", merged)
	}

	// Theirs' modification to B should be present
	if !strings.Contains(merged, "b-modified-by-theirs") {
		t.Errorf("merged output missing theirs' modification to B\nmerged:\n%s", merged)
	}

	// Stats
	if result.Stats.OursModified < 1 {
		t.Errorf("expected OursModified >= 1, got %d", result.Stats.OursModified)
	}
	if result.Stats.TheirsModified < 1 {
		t.Errorf("expected TheirsModified >= 1, got %d", result.Stats.TheirsModified)
	}
}

// TestMergeSameEntityConflict verifies that when both sides modify the same
// entity differently, the output contains conflict markers.
func TestMergeSameEntityConflict(t *testing.T) {
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

	merged := string(result.Merged)

	if !result.HasConflicts {
		t.Errorf("expected conflicts, but HasConflicts=false\nmerged:\n%s", merged)
	}
	if result.ConflictCount < 1 {
		t.Errorf("expected ConflictCount >= 1, got %d", result.ConflictCount)
	}

	// Should contain conflict markers
	if !strings.Contains(merged, "<<<<<<< ours") {
		t.Errorf("missing <<<<<<< ours marker\nmerged:\n%s", merged)
	}
	if !strings.Contains(merged, "=======") {
		t.Errorf("missing ======= marker\nmerged:\n%s", merged)
	}
	if !strings.Contains(merged, ">>>>>>> theirs") {
		t.Errorf("missing >>>>>>> theirs marker\nmerged:\n%s", merged)
	}

	// Both versions should appear in the conflict block
	if !strings.Contains(merged, "return 1") {
		t.Errorf("missing ours body (return 1) in conflict\nmerged:\n%s", merged)
	}
	if !strings.Contains(merged, "return 2") {
		t.Errorf("missing theirs body (return 2) in conflict\nmerged:\n%s", merged)
	}

	// Package declaration should still be present (unchanged)
	if !strings.Contains(merged, "package main") {
		t.Errorf("missing package declaration in merged output")
	}

	// Stats
	if result.Stats.Conflicts < 1 {
		t.Errorf("expected Stats.Conflicts >= 1, got %d", result.Stats.Conflicts)
	}
}

// TestMergeImportAdditions verifies that when ours adds "os" and theirs adds
// "strings" to the import block, the merged output contains both via set-union.
func TestMergeImportAdditions(t *testing.T) {
	base := `package main

import "fmt"

func A() {
	fmt.Println("a")
}
`
	ours := `package main

import (
	"fmt"
	"os"
)

func A() {
	fmt.Println("a")
}
`
	theirs := `package main

import (
	"fmt"
	"strings"
)

func A() {
	fmt.Println("a")
}
`

	result, err := MergeFiles("test.go", []byte(base), []byte(ours), []byte(theirs))
	if err != nil {
		t.Fatalf("MergeFiles failed: %v", err)
	}

	merged := string(result.Merged)

	if result.HasConflicts {
		t.Errorf("expected no conflicts for import additions, but HasConflicts=true\nmerged:\n%s", merged)
	}

	// Both new imports should be present
	if !strings.Contains(merged, `"fmt"`) {
		t.Errorf("merged output missing \"fmt\"\nmerged:\n%s", merged)
	}
	if !strings.Contains(merged, `"os"`) {
		t.Errorf("merged output missing \"os\"\nmerged:\n%s", merged)
	}
	if !strings.Contains(merged, `"strings"`) {
		t.Errorf("merged output missing \"strings\"\nmerged:\n%s", merged)
	}
}

// TestMergeDeleteVsModify verifies that when one side deletes an entity and
// the other modifies it, the result is a conflict.
func TestMergeDeleteVsModify(t *testing.T) {
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
	// Theirs: deletes B entirely
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
		t.Errorf("expected conflicts for delete-vs-modify, but HasConflicts=false\nmerged:\n%s", string(result.Merged))
	}
	if result.ConflictCount < 1 {
		t.Errorf("expected ConflictCount >= 1, got %d", result.ConflictCount)
	}

	// func A should still be present (unchanged)
	if !strings.Contains(string(result.Merged), "func A()") {
		t.Errorf("func A() should be present in merged output")
	}
}

func TestMergeUnsupportedTextFallsBackToDiff3(t *testing.T) {
	base := []byte("line-a\nline-b\nline-c\n")
	ours := []byte("line-a-ours\nline-b\nline-c\n")
	theirs := []byte("line-a\nline-b\ntheirs-line-c\n")

	result, err := MergeFiles("notes.txt", base, ours, theirs)
	if err != nil {
		t.Fatalf("MergeFiles failed: %v", err)
	}
	if result.HasConflicts {
		t.Fatalf("expected clean line-merge fallback, got conflicts: %+v", result)
	}
	merged := string(result.Merged)
	if !strings.Contains(merged, "line-a-ours") {
		t.Fatalf("merged output missing ours change: %q", merged)
	}
	if !strings.Contains(merged, "theirs-line-c") {
		t.Fatalf("merged output missing theirs change: %q", merged)
	}
}

func TestMergeBinaryConflictPreservesOurs(t *testing.T) {
	base := []byte{0x00, 0x01, 0x02, 0x03}
	ours := []byte{0x00, 0x09, 0x02, 0x03}
	theirs := []byte{0x00, 0x01, 0x08, 0x03}

	result, err := MergeFiles("data.bin", base, ours, theirs)
	if err != nil {
		t.Fatalf("MergeFiles failed: %v", err)
	}
	if !result.HasConflicts || result.ConflictCount == 0 {
		t.Fatalf("expected binary conflict, got %+v", result)
	}
	if !bytes.Equal(result.Merged, ours) {
		t.Fatalf("expected ours bytes to be preserved, got %v", result.Merged)
	}
}

func TestMergeBinaryWhenOneSideUnchanged(t *testing.T) {
	base := []byte{0x00, 0x01, 0x02}
	ours := []byte{0x00, 0x01, 0x02}
	theirs := []byte{0x00, 0x07, 0x02}

	result, err := MergeFiles("data.bin", base, ours, theirs)
	if err != nil {
		t.Fatalf("MergeFiles failed: %v", err)
	}
	if result.HasConflicts {
		t.Fatalf("expected clean binary merge when one side unchanged, got %+v", result)
	}
	if !bytes.Equal(result.Merged, theirs) {
		t.Fatalf("expected theirs bytes, got %v", result.Merged)
	}
}

// TestMergeReconstructValidOutput verifies that the merged output of a clean
// merge is plausible Go source (contains package, import, functions in order).
func TestMergeReconstructValidOutput(t *testing.T) {
	base := `package main

import "fmt"

func Hello() {
	fmt.Println("hello")
}
`
	ours := `package main

import "fmt"

func Hello() {
	fmt.Println("hello")
}

func Greet() {
	fmt.Println("greet")
}
`
	theirs := `package main

import "fmt"

func Hello() {
	fmt.Println("hello")
}

func Farewell() {
	fmt.Println("farewell")
}
`

	result, err := MergeFiles("test.go", []byte(base), []byte(ours), []byte(theirs))
	if err != nil {
		t.Fatalf("MergeFiles failed: %v", err)
	}

	merged := string(result.Merged)

	if result.HasConflicts {
		t.Fatalf("expected no conflicts\nmerged:\n%s", merged)
	}

	// Verify the merged output can be re-parsed by entity.Extract
	el, err := entity.Extract("test.go", result.Merged)
	if err != nil {
		t.Fatalf("merged output failed to parse: %v", err)
	}

	// Should have at least: preamble, import, Hello, Greet, Farewell
	declNames := map[string]bool{}
	for _, e := range el.Entities {
		if e.Kind == entity.KindDeclaration {
			declNames[e.Name] = true
		}
	}
	for _, name := range []string{"Hello", "Greet", "Farewell"} {
		if !declNames[name] {
			t.Errorf("merged output missing declaration %q\ndeclNames: %v\nmerged:\n%s", name, declNames, merged)
		}
	}

	// Package declaration should appear before functions
	pkgIdx := strings.Index(merged, "package main")
	helloIdx := strings.Index(merged, "func Hello()")
	if pkgIdx == -1 || helloIdx == -1 || pkgIdx >= helloIdx {
		t.Error("package declaration should appear before function declarations")
	}
}

// TestMergeCleanDiff3Fallback verifies that when both sides modify the same
// declaration but in non-overlapping ways, diff3 resolves it cleanly.
func TestMergeCleanDiff3Fallback(t *testing.T) {
	base := `package main

func A() {
	line1()
	line2()
	line3()
}
`
	// Ours modifies line1
	ours := `package main

func A() {
	line1modified()
	line2()
	line3()
}
`
	// Theirs modifies line3
	theirs := `package main

func A() {
	line1()
	line2()
	line3modified()
}
`

	result, err := MergeFiles("test.go", []byte(base), []byte(ours), []byte(theirs))
	if err != nil {
		t.Fatalf("MergeFiles failed: %v", err)
	}

	merged := string(result.Merged)

	if result.HasConflicts {
		t.Errorf("expected clean diff3 merge, but HasConflicts=true\nmerged:\n%s", merged)
	}

	// Both modifications should appear
	if !strings.Contains(merged, "line1modified()") {
		t.Errorf("missing ours modification (line1modified)\nmerged:\n%s", merged)
	}
	if !strings.Contains(merged, "line3modified()") {
		t.Errorf("missing theirs modification (line3modified)\nmerged:\n%s", merged)
	}
}

// TestMergeStatsAccuracy verifies that MergeStats fields are populated correctly.
func TestMergeStatsAccuracy(t *testing.T) {
	base := `package main

import "fmt"

func A() {
	fmt.Println("a")
}

func B() {
	fmt.Println("b")
}
`
	// Ours: modifies A, adds C
	ours := `package main

import "fmt"

func A() {
	fmt.Println("a-ours")
}

func B() {
	fmt.Println("b")
}

func C() {
	fmt.Println("c")
}
`
	// Theirs: same base, unchanged
	theirs := `package main

import "fmt"

func A() {
	fmt.Println("a")
}

func B() {
	fmt.Println("b")
}
`

	result, err := MergeFiles("test.go", []byte(base), []byte(ours), []byte(theirs))
	if err != nil {
		t.Fatalf("MergeFiles failed: %v", err)
	}

	if result.HasConflicts {
		t.Errorf("expected no conflicts\nmerged:\n%s", string(result.Merged))
	}

	s := result.Stats

	// We should have some unchanged entities (preamble, import, interstitials, B)
	if s.Unchanged == 0 {
		t.Errorf("expected some Unchanged entities, got 0")
	}

	// A was modified by ours only
	if s.OursModified < 1 {
		t.Errorf("expected OursModified >= 1, got %d", s.OursModified)
	}

	// C was added by ours
	if s.Added < 1 {
		t.Errorf("expected Added >= 1, got %d", s.Added)
	}

	// No conflicts
	if s.Conflicts != 0 {
		t.Errorf("expected Conflicts=0, got %d", s.Conflicts)
	}

	// TotalEntities should be positive
	if s.TotalEntities == 0 {
		t.Errorf("expected TotalEntities > 0, got 0")
	}
}
