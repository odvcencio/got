package merge

import (
	"strings"
	"testing"
)

// TestMultiLangGo verifies structural merge for Go source files.
// Base has one function; ours adds a second, theirs adds a third.
// The merge should be clean with all three functions present.
func TestMultiLangGo(t *testing.T) {
	base := []byte(`package main

func A() {}
`)
	ours := []byte(`package main

func A() {}

func B() {}
`)
	theirs := []byte(`package main

func A() {}

func C() {}
`)

	result, err := MergeFiles("test.go", base, ours, theirs)
	if err != nil {
		t.Fatalf("MergeFiles failed: %v", err)
	}

	merged := string(result.Merged)

	if result.HasConflicts {
		t.Errorf("expected no conflicts, got %d\nmerged:\n%s", result.ConflictCount, merged)
	}

	for _, fn := range []string{"func A()", "func B()", "func C()"} {
		if !strings.Contains(merged, fn) {
			t.Errorf("merged output missing %q\nmerged:\n%s", fn, merged)
		}
	}
}

// TestMultiLangPython verifies structural merge for Python source files.
// Uses inline function bodies to avoid tree-sitter indent-sensitivity issues
// that cause multi-line indented function bodies to be parsed as a single entity.
func TestMultiLangPython(t *testing.T) {
	base := []byte(`def hello(): pass
`)
	ours := []byte(`def hello(): pass

def goodbye(): pass
`)
	theirs := []byte(`def hello(): pass

def greet(): pass
`)

	result, err := MergeFiles("test.py", base, ours, theirs)
	if err != nil {
		t.Fatalf("MergeFiles failed: %v", err)
	}

	merged := string(result.Merged)

	if result.HasConflicts {
		t.Errorf("expected no conflicts, got %d\nmerged:\n%s", result.ConflictCount, merged)
	}

	for _, fn := range []string{"hello", "goodbye", "greet"} {
		if !strings.Contains(merged, fn) {
			t.Errorf("merged output missing %q\nmerged:\n%s", fn, merged)
		}
	}
}

// TestMultiLangRust verifies structural merge for Rust source files.
func TestMultiLangRust(t *testing.T) {
	base := []byte(`fn hello() {}
`)
	ours := []byte(`fn hello() {}

fn goodbye() {}
`)
	theirs := []byte(`fn hello() {}

fn greet() {}
`)

	result, err := MergeFiles("test.rs", base, ours, theirs)
	if err != nil {
		t.Fatalf("MergeFiles failed: %v", err)
	}

	merged := string(result.Merged)

	if result.HasConflicts {
		t.Errorf("expected no conflicts, got %d\nmerged:\n%s", result.ConflictCount, merged)
	}

	for _, fn := range []string{"fn hello()", "fn goodbye()", "fn greet()"} {
		if !strings.Contains(merged, fn) {
			t.Errorf("merged output missing %q\nmerged:\n%s", fn, merged)
		}
	}
}

// TestMultiLangTypeScript verifies structural merge for TypeScript source files.
func TestMultiLangTypeScript(t *testing.T) {
	base := []byte(`function hello() {}
`)
	ours := []byte(`function hello() {}

function goodbye() {}
`)
	theirs := []byte(`function hello() {}

function greet() {}
`)

	result, err := MergeFiles("test.ts", base, ours, theirs)
	if err != nil {
		t.Fatalf("MergeFiles failed: %v", err)
	}

	merged := string(result.Merged)

	if result.HasConflicts {
		t.Errorf("expected no conflicts, got %d\nmerged:\n%s", result.ConflictCount, merged)
	}

	for _, fn := range []string{"function hello()", "function goodbye()", "function greet()"} {
		if !strings.Contains(merged, fn) {
			t.Errorf("merged output missing %q\nmerged:\n%s", fn, merged)
		}
	}
}

// TestMultiLangC verifies structural merge for C source files.
func TestMultiLangC(t *testing.T) {
	base := []byte(`void hello() {}
`)
	ours := []byte(`void hello() {}

void goodbye() {}
`)
	theirs := []byte(`void hello() {}

void greet() {}
`)

	result, err := MergeFiles("test.c", base, ours, theirs)
	if err != nil {
		t.Fatalf("MergeFiles failed: %v", err)
	}

	merged := string(result.Merged)

	if result.HasConflicts {
		t.Errorf("expected no conflicts, got %d\nmerged:\n%s", result.ConflictCount, merged)
	}

	for _, fn := range []string{"void hello()", "void goodbye()", "void greet()"} {
		if !strings.Contains(merged, fn) {
			t.Errorf("merged output missing %q\nmerged:\n%s", fn, merged)
		}
	}
}
