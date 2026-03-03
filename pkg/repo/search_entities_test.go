package repo

import (
	"testing"
)

// commitGoSource creates a Go source file, adds it, and commits it.
// Returns the commit hash.
func commitGoSource(t *testing.T, r *Repo, dir, relPath, source, msg string) {
	t.Helper()
	writeRepoSource(t, dir, relPath, source)
	if err := r.Add([]string{relPath}); err != nil {
		t.Fatalf("Add(%s): %v", relPath, err)
	}
	if _, err := r.Commit(msg, "tester"); err != nil {
		t.Fatalf("Commit(%s): %v", msg, err)
	}
}

func TestSearchEntities_FindByName(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	source := "package main\n\nfunc Hello() {}\n\nfunc World() {}\n"
	commitGoSource(t, r, dir, "main.go", source, "add main.go")

	results, err := r.SearchEntities("Hello", EntitySearchOptions{})
	if err != nil {
		t.Fatalf("SearchEntities: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results[0].Name != "Hello" {
		t.Fatalf("got name %q, want %q", results[0].Name, "Hello")
	}
	if results[0].Path != "main.go" {
		t.Fatalf("got path %q, want %q", results[0].Path, "main.go")
	}
	if results[0].Kind != "declaration" {
		t.Fatalf("got kind %q, want %q", results[0].Kind, "declaration")
	}
}

func TestSearchEntities_Regex(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	source := "package main\n\nfunc FooBar() {}\n\nfunc FooBaz() {}\n\nfunc Other() {}\n"
	commitGoSource(t, r, dir, "main.go", source, "add main.go")

	results, err := r.SearchEntities("Foo.*", EntitySearchOptions{})
	if err != nil {
		t.Fatalf("SearchEntities: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
	if results[0].Name != "FooBar" {
		t.Fatalf("results[0].Name = %q, want %q", results[0].Name, "FooBar")
	}
	if results[1].Name != "FooBaz" {
		t.Fatalf("results[1].Name = %q, want %q", results[1].Name, "FooBaz")
	}
}

func TestSearchEntities_CaseInsensitive(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	source := "package main\n\nfunc Hello() {}\n\nfunc HELLO() {}\n\nfunc hello() {}\n"
	commitGoSource(t, r, dir, "main.go", source, "add main.go")

	// Case-sensitive: should match only exact case.
	results, err := r.SearchEntities("Hello", EntitySearchOptions{})
	if err != nil {
		t.Fatalf("SearchEntities case-sensitive: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("case-sensitive: got %d results, want 1", len(results))
	}

	// Case-insensitive: should match all three.
	results, err = r.SearchEntities("hello", EntitySearchOptions{CaseInsensitive: true})
	if err != nil {
		t.Fatalf("SearchEntities case-insensitive: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("case-insensitive: got %d results, want 3", len(results))
	}
}

func TestSearchEntities_KindFilter(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// This source produces entities of kind "declaration" (for the function)
	// and "preamble" (for the package clause), and potentially "interstitial".
	source := "package main\n\nfunc Target() {}\n"
	commitGoSource(t, r, dir, "main.go", source, "add main.go")

	// Search with kind filter "declaration" should find Target.
	results, err := r.SearchEntities("Target", EntitySearchOptions{KindFilter: "declaration"})
	if err != nil {
		t.Fatalf("SearchEntities with kind filter: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results[0].Kind != "declaration" {
		t.Fatalf("got kind %q, want %q", results[0].Kind, "declaration")
	}

	// Search with kind filter "preamble" should not find Target.
	results, err = r.SearchEntities("Target", EntitySearchOptions{KindFilter: "preamble"})
	if err != nil {
		t.Fatalf("SearchEntities with wrong kind: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("got %d results, want 0", len(results))
	}
}

func TestSearchEntities_PathFilter(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	source1 := "package foo\n\nfunc Alpha() {}\n"
	source2 := "package bar\n\nfunc Alpha() {}\n"

	writeRepoSource(t, dir, "foo.go", source1)
	writeRepoSource(t, dir, "bar.go", source2)
	if err := r.Add([]string{"foo.go", "bar.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := r.Commit("add both", "tester"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// Without path filter: should find Alpha in both files.
	results, err := r.SearchEntities("Alpha", EntitySearchOptions{})
	if err != nil {
		t.Fatalf("SearchEntities no filter: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("no filter: got %d results, want 2", len(results))
	}

	// With path filter matching only foo.go.
	results, err = r.SearchEntities("Alpha", EntitySearchOptions{PathPattern: "foo.go"})
	if err != nil {
		t.Fatalf("SearchEntities with path: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("path filter: got %d results, want 1", len(results))
	}
	if results[0].Path != "foo.go" {
		t.Fatalf("path filter: got path %q, want %q", results[0].Path, "foo.go")
	}
}

func TestSearchEntities_NoMatch(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	source := "package main\n\nfunc Hello() {}\n"
	commitGoSource(t, r, dir, "main.go", source, "add main.go")

	results, err := r.SearchEntities("NotPresent", EntitySearchOptions{})
	if err != nil {
		t.Fatalf("SearchEntities: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("got %d results, want 0", len(results))
	}
}

func TestSearchEntities_EmptyPattern(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	source := "package main\n\nfunc Hello() {}\n"
	commitGoSource(t, r, dir, "main.go", source, "add main.go")

	_, err = r.SearchEntities("", EntitySearchOptions{})
	if err == nil {
		t.Fatal("expected error for empty pattern")
	}
}

func TestSearchEntities_SortedByPathThenName(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Create two files with entities that should sort: b.go before a.go by
	// path, and within same file by name.
	source1 := "package b\n\nfunc Zeta() {}\n\nfunc Alpha() {}\n"
	source2 := "package a\n\nfunc Beta() {}\n"

	writeRepoSource(t, dir, "b.go", source1)
	writeRepoSource(t, dir, "a.go", source2)
	if err := r.Add([]string{"a.go", "b.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := r.Commit("add files", "tester"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// Match all declarations with a broad regex.
	results, err := r.SearchEntities(".*", EntitySearchOptions{KindFilter: "declaration"})
	if err != nil {
		t.Fatalf("SearchEntities: %v", err)
	}

	if len(results) < 3 {
		t.Fatalf("got %d results, want at least 3", len(results))
	}

	// Verify sorted by path then name.
	for i := 1; i < len(results); i++ {
		prev := results[i-1]
		curr := results[i]
		if prev.Path > curr.Path {
			t.Fatalf("results not sorted by path: %q > %q", prev.Path, curr.Path)
		}
		if prev.Path == curr.Path && prev.Name > curr.Name {
			t.Fatalf("results not sorted by name within %q: %q > %q", prev.Path, prev.Name, curr.Name)
		}
	}
}

func TestSearchEntities_EntityKey(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	source := "package main\n\nfunc Target() {}\n"
	commitGoSource(t, r, dir, "main.go", source, "add main.go")

	results, err := r.SearchEntities("Target", EntitySearchOptions{})
	if err != nil {
		t.Fatalf("SearchEntities: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	// Key should be "kind:name".
	expected := "declaration:Target"
	if results[0].Key != expected {
		t.Fatalf("got key %q, want %q", results[0].Key, expected)
	}
}

func TestSearchEntities_GlobPathFilter(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	source1 := "package foo\n\nfunc Target() {}\n"
	source2 := "package bar\n\nfunc Target() {}\n"

	writeRepoSource(t, dir, "foo.go", source1)
	writeRepoSource(t, dir, "bar.txt.go", source2)
	if err := r.Add([]string{"foo.go", "bar.txt.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := r.Commit("add files", "tester"); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// Glob matching *.go matches both since both end in .go.
	results, err := r.SearchEntities("Target", EntitySearchOptions{PathPattern: "*.go"})
	if err != nil {
		t.Fatalf("SearchEntities: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("glob *.go: got %d results, want 2", len(results))
	}

	// Glob matching foo.* should match only foo.go.
	results, err = r.SearchEntities("Target", EntitySearchOptions{PathPattern: "foo.*"})
	if err != nil {
		t.Fatalf("SearchEntities: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("glob foo.*: got %d results, want 1", len(results))
	}
	if results[0].Path != "foo.go" {
		t.Fatalf("got path %q, want %q", results[0].Path, "foo.go")
	}
}
