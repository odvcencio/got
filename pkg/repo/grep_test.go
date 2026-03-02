package repo

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGrep_FindsMatch(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	writeFile(t, filepath.Join(dir, "hello.txt"), []byte("hello world\ngoodbye world\nhello again\n"))
	if err := r.Add([]string{"hello.txt"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	results, err := r.Grep(GrepOptions{Pattern: "hello"})
	if err != nil {
		t.Fatalf("Grep: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
	if results[0].Path != "hello.txt" || results[0].Line != 1 || results[0].Content != "hello world" {
		t.Fatalf("results[0] = %+v, want hello.txt:1:hello world", results[0])
	}
	if results[1].Path != "hello.txt" || results[1].Line != 3 || results[1].Content != "hello again" {
		t.Fatalf("results[1] = %+v, want hello.txt:3:hello again", results[1])
	}
}

func TestGrep_CaseInsensitive(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	writeFile(t, filepath.Join(dir, "mixed.txt"), []byte("Hello World\nhello world\nHELLO WORLD\n"))
	if err := r.Add([]string{"mixed.txt"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Case-sensitive should match only lowercase.
	results, err := r.Grep(GrepOptions{Pattern: "hello"})
	if err != nil {
		t.Fatalf("Grep case-sensitive: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("case-sensitive: got %d results, want 1", len(results))
	}

	// Case-insensitive should match all three.
	results, err = r.Grep(GrepOptions{Pattern: "hello", CaseInsensitive: true})
	if err != nil {
		t.Fatalf("Grep case-insensitive: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("case-insensitive: got %d results, want 3", len(results))
	}
}

func TestGrep_FixedString(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	writeFile(t, filepath.Join(dir, "regex.txt"), []byte("foo.bar\nfooXbar\nfoo\\.bar\n"))
	if err := r.Add([]string{"regex.txt"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Without FixedString, "foo.bar" is a regex: . matches any char.
	results, err := r.Grep(GrepOptions{Pattern: "foo.bar"})
	if err != nil {
		t.Fatalf("Grep regex: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("regex mode: got %d results, want 2 (foo.bar and fooXbar)", len(results))
	}

	// With FixedString, "foo.bar" is literal.
	results, err = r.Grep(GrepOptions{Pattern: "foo.bar", FixedString: true})
	if err != nil {
		t.Fatalf("Grep fixed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("fixed mode: got %d results, want 1 (only foo.bar)", len(results))
	}
	if results[0].Content != "foo.bar" {
		t.Fatalf("fixed mode: got %q, want %q", results[0].Content, "foo.bar")
	}
}

func TestGrep_NoMatch(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	writeFile(t, filepath.Join(dir, "data.txt"), []byte("alpha\nbeta\ngamma\n"))
	if err := r.Add([]string{"data.txt"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	results, err := r.Grep(GrepOptions{Pattern: "zzz_not_present"})
	if err != nil {
		t.Fatalf("Grep: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("got %d results, want 0", len(results))
	}
}

func TestGrep_PathFilter(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	if err := os.MkdirAll(filepath.Join(dir, "src"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	writeFile(t, filepath.Join(dir, "src", "main.go"), []byte("package main\nfunc main() {}\n"))
	writeFile(t, filepath.Join(dir, "readme.txt"), []byte("main documentation\nfunc description\n"))
	if err := r.Add([]string{"src/main.go", "readme.txt"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Without path filter, both files match "main".
	results, err := r.Grep(GrepOptions{Pattern: "main"})
	if err != nil {
		t.Fatalf("Grep all: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("no filter: got %d results, want 3", len(results))
	}

	// With path filter matching only .txt files.
	results, err = r.Grep(GrepOptions{Pattern: "main", PathPattern: "*.txt"})
	if err != nil {
		t.Fatalf("Grep filtered: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("filtered: got %d results, want 1", len(results))
	}
	if results[0].Path != "readme.txt" {
		t.Fatalf("filtered: got path %q, want %q", results[0].Path, "readme.txt")
	}
}
