package repo

import (
	"os"
	"path/filepath"
	"testing"
)

// Test 1: .got/ is always ignored — no .gotignore file needed.
func TestIgnore_GotDirAlwaysIgnored(t *testing.T) {
	dir := t.TempDir()

	ic := NewIgnoreChecker(dir)

	if !ic.IsIgnored(".got/HEAD") {
		t.Error("expected .got/HEAD to be ignored")
	}
	if !ic.IsIgnored(".got/objects/abc") {
		t.Error("expected .got/objects/abc to be ignored")
	}
	if !ic.IsIgnored(".got") {
		t.Error("expected .got to be ignored")
	}
}

// Test 2: .git/ is always ignored.
func TestIgnore_GitDirAlwaysIgnored(t *testing.T) {
	dir := t.TempDir()

	ic := NewIgnoreChecker(dir)

	if !ic.IsIgnored(".git/config") {
		t.Error("expected .git/config to be ignored")
	}
	if !ic.IsIgnored(".git") {
		t.Error("expected .git to be ignored")
	}
}

// Test 3: Simple pattern — .gotignore contains *.log, file debug.log is ignored.
func TestIgnore_SimpleGlobPattern(t *testing.T) {
	dir := t.TempDir()

	writeGotignore(t, dir, "*.log\n")

	ic := NewIgnoreChecker(dir)

	if !ic.IsIgnored("debug.log") {
		t.Error("expected debug.log to be ignored")
	}
	if ic.IsIgnored("debug.txt") {
		t.Error("expected debug.txt to NOT be ignored")
	}
}

// Test 4: Directory pattern — .gotignore contains build/, build/output.o is ignored.
func TestIgnore_DirectoryPattern(t *testing.T) {
	dir := t.TempDir()

	writeGotignore(t, dir, "build/\n")

	ic := NewIgnoreChecker(dir)

	if !ic.IsIgnored("build/output.o") {
		t.Error("expected build/output.o to be ignored")
	}
	if !ic.IsIgnored("build/sub/file.txt") {
		t.Error("expected build/sub/file.txt to be ignored")
	}
}

// Test 5: Negation — .gotignore contains *.log and !important.log,
// important.log is NOT ignored.
func TestIgnore_NegationPattern(t *testing.T) {
	dir := t.TempDir()

	writeGotignore(t, dir, "*.log\n!important.log\n")

	ic := NewIgnoreChecker(dir)

	if !ic.IsIgnored("debug.log") {
		t.Error("expected debug.log to be ignored")
	}
	if ic.IsIgnored("important.log") {
		t.Error("expected important.log to NOT be ignored (negation)")
	}
}

// Test 6: Comment lines — lines starting with # are skipped.
func TestIgnore_CommentLines(t *testing.T) {
	dir := t.TempDir()

	writeGotignore(t, dir, "# this is a comment\n*.log\n# another comment\n")

	ic := NewIgnoreChecker(dir)

	if !ic.IsIgnored("debug.log") {
		t.Error("expected debug.log to be ignored")
	}
	// Make sure comments are not treated as patterns.
	if ic.IsIgnored("# this is a comment") {
		t.Error("expected comment text to NOT match as a pattern")
	}
}

// Test 7: No .gotignore file — only hardcoded patterns apply.
func TestIgnore_NoGotignoreFile(t *testing.T) {
	dir := t.TempDir()

	ic := NewIgnoreChecker(dir)

	// Hardcoded patterns still work.
	if !ic.IsIgnored(".got/HEAD") {
		t.Error("expected .got/HEAD to be ignored even without .gotignore")
	}
	if !ic.IsIgnored(".git/config") {
		t.Error("expected .git/config to be ignored even without .gotignore")
	}

	// Regular files are not ignored.
	if ic.IsIgnored("main.go") {
		t.Error("expected main.go to NOT be ignored")
	}
	if ic.IsIgnored("src/util.go") {
		t.Error("expected src/util.go to NOT be ignored")
	}
}

// Test 8: Subdirectory file — *.o matches src/foo.o.
func TestIgnore_SubdirectoryFileMatch(t *testing.T) {
	dir := t.TempDir()

	writeGotignore(t, dir, "*.o\n")

	ic := NewIgnoreChecker(dir)

	if !ic.IsIgnored("src/foo.o") {
		t.Error("expected src/foo.o to be ignored")
	}
	if !ic.IsIgnored("foo.o") {
		t.Error("expected foo.o to be ignored")
	}
	if ic.IsIgnored("src/foo.go") {
		t.Error("expected src/foo.go to NOT be ignored")
	}
}

func TestIgnore_GlobstarMatchesNestedPaths(t *testing.T) {
	dir := t.TempDir()

	writeGotignore(t, dir, "**/*.gen.go\n")
	ic := NewIgnoreChecker(dir)

	if !ic.IsIgnored("generated.gen.go") {
		t.Error("expected generated.gen.go to be ignored")
	}
	if !ic.IsIgnored("src/generated.gen.go") {
		t.Error("expected src/generated.gen.go to be ignored")
	}
	if !ic.IsIgnored("src/nested/generated.gen.go") {
		t.Error("expected src/nested/generated.gen.go to be ignored")
	}
	if ic.IsIgnored("src/nested/generated.go") {
		t.Error("expected src/nested/generated.go to NOT be ignored")
	}
}

func TestIgnore_GlobstarPrefixPattern(t *testing.T) {
	dir := t.TempDir()

	writeGotignore(t, dir, "docs/**\n")
	ic := NewIgnoreChecker(dir)

	if !ic.IsIgnored("docs/index.md") {
		t.Error("expected docs/index.md to be ignored")
	}
	if !ic.IsIgnored("docs/api/v1/openapi.yaml") {
		t.Error("expected docs/api/v1/openapi.yaml to be ignored")
	}
	if ic.IsIgnored("src/docs/index.md") {
		t.Error("expected src/docs/index.md to NOT be ignored")
	}
}

func TestIgnore_LastMatchWinsAcrossWildcardAndLiteral(t *testing.T) {
	dir := t.TempDir()

	writeGotignore(t, dir, "*.log\n!important.log\nimportant.log\n")
	ic := NewIgnoreChecker(dir)

	if !ic.IsIgnored("debug.log") {
		t.Error("expected debug.log to be ignored")
	}
	if !ic.IsIgnored("important.log") {
		t.Error("expected important.log to be ignored by final literal pattern")
	}
}

func TestIgnore_DirPatternOverriddenByExactPathNegation(t *testing.T) {
	dir := t.TempDir()

	writeGotignore(t, dir, "build/\n!build/keep.txt\n")
	ic := NewIgnoreChecker(dir)

	if !ic.IsIgnored("build/out.bin") {
		t.Error("expected build/out.bin to be ignored")
	}
	if ic.IsIgnored("build/keep.txt") {
		t.Error("expected build/keep.txt to be unignored by exact path negation")
	}
}

func TestIgnore_GlobstarOverriddenByExactPathNegation(t *testing.T) {
	dir := t.TempDir()

	writeGotignore(t, dir, "**/*.gen.go\n!cmd/main.gen.go\n")
	ic := NewIgnoreChecker(dir)

	if !ic.IsIgnored("pkg/nested/file.gen.go") {
		t.Error("expected pkg/nested/file.gen.go to be ignored")
	}
	if ic.IsIgnored("cmd/main.gen.go") {
		t.Error("expected cmd/main.gen.go to be unignored by later exact path negation")
	}
}

// helper: write a .gotignore file in the given directory.
func writeGotignore(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, ".gotignore"), []byte(content), 0o644); err != nil {
		t.Fatalf("write .gotignore: %v", err)
	}
}
