package merge

import (
	"strings"
	"testing"

	"github.com/odvcencio/got/pkg/entity"
)

// TestGoldenStructuralMerge is the core value-proposition test: two branches
// adding different functions to the same file merge cleanly where Git would
// produce a conflict. Ours adds Goodbye at the bottom, theirs adds Greet
// between the import and Hello.
func TestGoldenStructuralMerge(t *testing.T) {
	const base = `package main

import "fmt"

func Hello() {
	fmt.Println("hello")
}
`
	const ours = `package main

import "fmt"

func Hello() {
	fmt.Println("hello")
}

func Goodbye() {
	fmt.Println("goodbye")
}
`
	const theirs = `package main

import "fmt"

func Greet(name string) {
	fmt.Printf("hi %s\n", name)
}

func Hello() {
	fmt.Println("hello")
}
`

	result, err := MergeFiles("main.go", []byte(base), []byte(ours), []byte(theirs))
	if err != nil {
		t.Fatalf("MergeFiles failed: %v", err)
	}

	merged := string(result.Merged)

	// 1. No conflicts
	if result.HasConflicts {
		t.Errorf("expected no conflicts, got HasConflicts=true (ConflictCount=%d)\nmerged output:\n%s",
			result.ConflictCount, merged)
	}

	// 2-4. All three functions must be present
	for _, sig := range []string{"func Hello()", "func Goodbye()", "func Greet("} {
		if !strings.Contains(merged, sig) {
			t.Errorf("merged output missing %q\nmerged:\n%s", sig, merged)
		}
	}

	// 5. Package declaration must be present
	if !strings.Contains(merged, "package main") {
		t.Errorf("merged output missing package declaration\nmerged:\n%s", merged)
	}

	// 6. Import must be present
	if !strings.Contains(merged, `import "fmt"`) {
		t.Errorf("merged output missing import \"fmt\"\nmerged:\n%s", merged)
	}

	// 7. Merged output must be valid Go parseable by entity.Extract
	el, err := entity.Extract("main.go", result.Merged)
	if err != nil {
		t.Fatalf("merged output is not valid Go: entity.Extract failed: %v\nmerged:\n%s", err, merged)
	}

	// Verify all three functions are present as named declarations
	declNames := map[string]bool{}
	for _, e := range el.Entities {
		if e.Kind == entity.KindDeclaration {
			declNames[e.Name] = true
		}
	}
	for _, name := range []string{"Hello", "Goodbye", "Greet"} {
		if !declNames[name] {
			t.Errorf("entity.Extract on merged output missing declaration %q\ndeclNames: %v\nmerged:\n%s",
				name, declNames, merged)
		}
	}

	t.Logf("merged output (%d bytes):\n%s", len(result.Merged), merged)
}

// TestGoldenImportMerge verifies that when both sides add different imports,
// the merged output contains all three via set-union merge with no conflicts.
func TestGoldenImportMerge(t *testing.T) {
	const base = `package main

import "fmt"

func Main() {
	fmt.Println("main")
}
`
	const ours = `package main

import (
	"fmt"
	"os"
)

func Main() {
	fmt.Println("main")
}
`
	const theirs = `package main

import (
	"fmt"
	"strings"
)

func Main() {
	fmt.Println("main")
}
`

	result, err := MergeFiles("main.go", []byte(base), []byte(ours), []byte(theirs))
	if err != nil {
		t.Fatalf("MergeFiles failed: %v", err)
	}

	merged := string(result.Merged)

	// No conflicts
	if result.HasConflicts {
		t.Errorf("expected no conflicts for import merge, got HasConflicts=true (ConflictCount=%d)\nmerged output:\n%s",
			result.ConflictCount, merged)
	}

	// All three imports must be present
	for _, imp := range []string{`"fmt"`, `"os"`, `"strings"`} {
		if !strings.Contains(merged, imp) {
			t.Errorf("merged output missing import %s\nmerged:\n%s", imp, merged)
		}
	}

	// Package and function should survive
	if !strings.Contains(merged, "package main") {
		t.Errorf("merged output missing package declaration\nmerged:\n%s", merged)
	}
	if !strings.Contains(merged, "func Main()") {
		t.Errorf("merged output missing func Main()\nmerged:\n%s", merged)
	}

	// Merged output must be valid Go parseable by entity.Extract
	el, err := entity.Extract("main.go", result.Merged)
	if err != nil {
		t.Fatalf("merged output is not valid Go: entity.Extract failed: %v\nmerged:\n%s", err, merged)
	}

	// Verify import block exists
	hasImportBlock := false
	for _, e := range el.Entities {
		if e.Kind == entity.KindImportBlock {
			hasImportBlock = true
			break
		}
	}
	if !hasImportBlock {
		t.Errorf("entity.Extract on merged output has no import block\nmerged:\n%s", merged)
	}

	t.Logf("merged output (%d bytes):\n%s", len(result.Merged), merged)
}
