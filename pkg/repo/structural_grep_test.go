package repo

import (
	"os"
	"path/filepath"
	"testing"
)

const testGoSource = `package main

import "fmt"

func Hello(name string) string {
	return fmt.Sprintf("hello %s", name)
}

func Goodbye(name string) string {
	return fmt.Sprintf("goodbye %s", name)
}

type Config struct {
	Name string
	Port int
}
`

func TestStructuralGrep_FindsFunctions(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	writeFile(t, filepath.Join(dir, "main.go"), []byte(testGoSource))
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	results, err := r.StructuralGrep(StructuralGrepOptions{
		Pattern: `func $NAME($$$PARAMS) string`,
	})
	if err != nil {
		t.Fatalf("StructuralGrep: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}

	// Results should be sorted by line.
	if results[0].Path != "main.go" {
		t.Errorf("results[0].Path = %q, want %q", results[0].Path, "main.go")
	}
	if results[0].StartLine < 1 {
		t.Errorf("results[0].StartLine = %d, want >= 1", results[0].StartLine)
	}
	if results[1].StartLine <= results[0].StartLine {
		t.Errorf("results not sorted: line %d should be after line %d",
			results[1].StartLine, results[0].StartLine)
	}
}

func TestStructuralGrep_CapturesPopulated(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	writeFile(t, filepath.Join(dir, "main.go"), []byte(testGoSource))
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	results, err := r.StructuralGrep(StructuralGrepOptions{
		Pattern: `func $NAME($$$PARAMS) string`,
	})
	if err != nil {
		t.Fatalf("StructuralGrep: %v", err)
	}

	if len(results) < 2 {
		t.Fatalf("got %d results, want >= 2", len(results))
	}

	// Check captures contain expected function names.
	names := make(map[string]bool)
	for _, r := range results {
		if name, ok := r.Captures["NAME"]; ok {
			names[name] = true
		}
	}

	if !names["Hello"] {
		t.Errorf("expected capture NAME=Hello, got captures: %v", names)
	}
	if !names["Goodbye"] {
		t.Errorf("expected capture NAME=Goodbye, got captures: %v", names)
	}
}

func TestStructuralGrep_EntityContext(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	writeFile(t, filepath.Join(dir, "main.go"), []byte(testGoSource))
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	results, err := r.StructuralGrep(StructuralGrepOptions{
		Pattern: `func $NAME($$$PARAMS) string`,
	})
	if err != nil {
		t.Fatalf("StructuralGrep: %v", err)
	}

	if len(results) < 1 {
		t.Fatalf("got 0 results, want >= 1")
	}

	// Entity context should be populated for matches inside declarations.
	found := false
	for _, r := range results {
		if r.EntityName != "" {
			found = true
			if r.EntityKind == "" {
				t.Errorf("EntityKind empty for match with EntityName=%q", r.EntityName)
			}
			if r.EntityKey == "" {
				t.Errorf("EntityKey empty for match with EntityName=%q", r.EntityName)
			}
		}
	}
	if !found {
		t.Errorf("no results had EntityName populated; expected entity context")
	}
}

func TestStructuralGrep_PathFilter(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	if err := os.MkdirAll(filepath.Join(dir, "pkg"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	writeFile(t, filepath.Join(dir, "main.go"), []byte(testGoSource))
	writeFile(t, filepath.Join(dir, "pkg", "lib.go"), []byte(`package pkg

func Add(a, b int) int {
	return a + b
}
`))
	if err := r.Add([]string{"main.go", "pkg/lib.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Search only in main.go via path filter.
	results, err := r.StructuralGrep(StructuralGrepOptions{
		Pattern:     `func $NAME($$$PARAMS)`,
		PathPattern: "main.go",
	})
	if err != nil {
		t.Fatalf("StructuralGrep: %v", err)
	}

	for _, res := range results {
		if res.Path != "main.go" {
			t.Errorf("path filter leaked: got result in %q, want only main.go", res.Path)
		}
	}
}

func TestStructuralGrep_NoMatchReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	writeFile(t, filepath.Join(dir, "main.go"), []byte(testGoSource))
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	results, err := r.StructuralGrep(StructuralGrepOptions{
		Pattern: `func $NAME() error`,
	})
	if err != nil {
		t.Fatalf("StructuralGrep: %v", err)
	}

	if len(results) != 0 {
		t.Fatalf("got %d results, want 0", len(results))
	}
}

func TestStructuralGrep_SkipsDotGraft(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	writeFile(t, filepath.Join(dir, "main.go"), []byte(testGoSource))
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Put a Go file inside .graft — it should be ignored.
	writeFile(t, filepath.Join(dir, ".graft", "hidden.go"), []byte(`package hidden

func Secret() {}
`))

	results, err := r.StructuralGrep(StructuralGrepOptions{
		Pattern: `func Secret()`,
	})
	if err != nil {
		t.Fatalf("StructuralGrep: %v", err)
	}

	for _, res := range results {
		if res.Path == ".graft/hidden.go" {
			t.Errorf("should not match files inside .graft/")
		}
	}
}

func TestStructuralGrep_MatchedText(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	writeFile(t, filepath.Join(dir, "main.go"), []byte(testGoSource))
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	results, err := r.StructuralGrep(StructuralGrepOptions{
		Pattern: `func $NAME($$$PARAMS) string`,
	})
	if err != nil {
		t.Fatalf("StructuralGrep: %v", err)
	}

	if len(results) < 1 {
		t.Fatalf("got 0 results, want >= 1")
	}

	for _, res := range results {
		if res.MatchedText == "" {
			t.Errorf("MatchedText should not be empty for path=%s line=%d", res.Path, res.StartLine)
		}
	}
}

func TestStructuralGrep_EmptyPattern(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	_, err = r.StructuralGrep(StructuralGrepOptions{Pattern: ""})
	if err == nil {
		t.Fatal("expected error for empty pattern")
	}
}

func TestStructuralGrep_UnsupportedFilesSkipped(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// .dat has no grammar; should be silently skipped.
	writeFile(t, filepath.Join(dir, "notes.dat"), []byte("func Hello() {}"))
	writeFile(t, filepath.Join(dir, "main.go"), []byte(testGoSource))
	if err := r.Add([]string{"notes.dat", "main.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	results, err := r.StructuralGrep(StructuralGrepOptions{
		Pattern: `func $NAME($$$PARAMS) string`,
	})
	if err != nil {
		t.Fatalf("StructuralGrep: %v", err)
	}

	for _, res := range results {
		if res.Path == "notes.dat" {
			t.Errorf("should not match unsupported file types")
		}
	}
}
