package repo

import (
	"os"
	"path/filepath"
	"testing"
)

// Test 1: .graft/ is always ignored — no .graftignore file needed.
func TestIgnore_GraftDirAlwaysIgnored(t *testing.T) {
	dir := t.TempDir()

	ic := NewIgnoreChecker(dir)

	if !ic.IsIgnored(".graft/HEAD") {
		t.Error("expected .graft/HEAD to be ignored")
	}
	if !ic.IsIgnored(".graft/objects/abc") {
		t.Error("expected .graft/objects/abc to be ignored")
	}
	if !ic.IsIgnored(".graft") {
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

// Test 3: Simple pattern — .graftignore contains *.log, file debug.log is ignored.
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

// Test 4: Directory pattern — .graftignore contains build/, build/output.o is ignored.
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

// Test 5: Negation — .graftignore contains *.log and !important.log,
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

// Test 7: No .graftignore file — only hardcoded patterns apply.
func TestIgnore_NoGotignoreFile(t *testing.T) {
	dir := t.TempDir()

	ic := NewIgnoreChecker(dir)

	// Hardcoded patterns still work.
	if !ic.IsIgnored(".graft/HEAD") {
		t.Error("expected .graft/HEAD to be ignored even without .graftignore")
	}
	if !ic.IsIgnored(".git/config") {
		t.Error("expected .git/config to be ignored even without .graftignore")
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

func TestIgnore_WildcardPrefixBucketsPreserveLastMatchWins(t *testing.T) {
	dir := t.TempDir()

	writeGotignore(t, dir, "**/*.tmp\ndocs/*.tmp\n!docs/keep.tmp\ndocs/k*.tmp\n")
	ic := NewIgnoreChecker(dir)

	if !ic.IsIgnored("docs/a.tmp") {
		t.Error("expected docs/a.tmp to be ignored")
	}
	if !ic.IsIgnored("docs/keep.tmp") {
		t.Error("expected docs/keep.tmp to be ignored by later docs/k*.tmp pattern")
	}
	if !ic.IsIgnored("src/keep.tmp") {
		t.Error("expected src/keep.tmp to be ignored by globstar pattern")
	}
	if ic.IsIgnored("src/keep.txt") {
		t.Error("expected src/keep.txt to NOT be ignored")
	}
}

func TestIgnore_BracketWildcardWithLiteralPrefix(t *testing.T) {
	dir := t.TempDir()

	writeGotignore(t, dir, "artifact-[0-9][0-9].bin\nvendor/**/gen-?.go\n")
	ic := NewIgnoreChecker(dir)

	if !ic.IsIgnored("out/artifact-42.bin") {
		t.Error("expected out/artifact-42.bin to be ignored")
	}
	if ic.IsIgnored("out/artifact-aa.bin") {
		t.Error("expected out/artifact-aa.bin to NOT be ignored")
	}
	if !ic.IsIgnored("vendor/pkg/nested/gen-a.go") {
		t.Error("expected vendor/pkg/nested/gen-a.go to be ignored")
	}
	if ic.IsIgnored("src/vendor/pkg/gen-a.go") {
		t.Error("expected src/vendor/pkg/gen-a.go to NOT be ignored")
	}
}

// helper: write a .graftignore file in the given directory.
// Test: Directory patterns without leading / match at any depth.
// .next/ should match both root .next/ and frontend/.next/.
func TestIgnore_NestedDirectoryPattern(t *testing.T) {
	dir := t.TempDir()

	writeGotignore(t, dir, ".next/\nnode_modules/\n")

	ic := NewIgnoreChecker(dir)

	// Root level
	if !ic.IsIgnored(".next/BUILD_ID") {
		t.Error("expected .next/BUILD_ID to be ignored")
	}
	// Nested
	if !ic.IsIgnored("frontend/.next/BUILD_ID") {
		t.Error("expected frontend/.next/BUILD_ID to be ignored")
	}
	if !ic.IsIgnored("frontend/.next/server/app/page.js") {
		t.Error("expected frontend/.next/server/app/page.js to be ignored")
	}
	if !ic.IsIgnored("frontend/node_modules/react/index.js") {
		t.Error("expected frontend/node_modules/react/index.js to be ignored")
	}
	// Non-matching should not be ignored
	if ic.IsIgnored("frontend/src/app/page.tsx") {
		t.Error("expected frontend/src/app/page.tsx to NOT be ignored")
	}
}

// Test: Leading / anchors pattern to root only.
func TestIgnore_RootAnchoredPattern(t *testing.T) {
	dir := t.TempDir()

	writeGotignore(t, dir, "/build/\n")

	ic := NewIgnoreChecker(dir)

	// Root build/ should be ignored
	if !ic.IsIgnored("build/output.o") {
		t.Error("expected build/output.o to be ignored")
	}
	// Nested build/ should NOT be ignored (rooted pattern)
	if ic.IsIgnored("frontend/build/output.o") {
		t.Error("expected frontend/build/output.o to NOT be ignored (rooted pattern)")
	}
}

func TestIgnore_ExplainReportsFinalRule(t *testing.T) {
	dir := t.TempDir()

	writeGotignore(t, dir, "*.log\n!important.log\n")
	ic := NewIgnoreChecker(dir)

	explanation := ic.Explain("important.log")
	if explanation.Ignored {
		t.Fatal("important.log should be unignored by the negation rule")
	}
	if explanation.Final == nil {
		t.Fatal("expected a final matching rule")
	}
	if explanation.Final.Pattern != "!important.log" {
		t.Fatalf("final pattern = %q, want %q", explanation.Final.Pattern, "!important.log")
	}
	if explanation.Final.Source != ".graftignore" {
		t.Fatalf("final source = %q, want %q", explanation.Final.Source, ".graftignore")
	}
	if explanation.Final.Line != 2 {
		t.Fatalf("final line = %d, want 2", explanation.Final.Line)
	}
	if len(explanation.Matches) != 2 {
		t.Fatalf("len(matches) = %d, want 2", len(explanation.Matches))
	}
}

func TestIgnore_ExplainBuiltins(t *testing.T) {
	dir := t.TempDir()

	ic := NewIgnoreChecker(dir)
	explanation := ic.Explain(".graft/HEAD")
	if !explanation.Ignored {
		t.Fatal("expected .graft/HEAD to be ignored")
	}
	if explanation.Final == nil {
		t.Fatal("expected builtin rule to be reported")
	}
	if explanation.Final.Source != "builtin" {
		t.Fatalf("final source = %q, want %q", explanation.Final.Source, "builtin")
	}
	if explanation.Final.Pattern != ".graft" {
		t.Fatalf("final pattern = %q, want %q", explanation.Final.Pattern, ".graft")
	}
}

// Test: .gts/ is always ignored (hardcoded builtin pattern).
func TestIgnore_GtsDirAlwaysIgnored(t *testing.T) {
	dir := t.TempDir()

	ic := NewIgnoreChecker(dir)

	if !ic.IsIgnored(".gts") {
		t.Error("expected .gts to be ignored")
	}
	if !ic.IsIgnored(".gts/index.db") {
		t.Error("expected .gts/index.db to be ignored")
	}
	if !ic.IsIgnored(".gts/cache/data") {
		t.Error("expected .gts/cache/data to be ignored")
	}
}

func writeGotignore(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, ".graftignore"), []byte(content), 0o644); err != nil {
		t.Fatalf("write .graftignore: %v", err)
	}
}
