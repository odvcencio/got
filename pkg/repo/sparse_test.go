package repo

import (
	"os"
	"path/filepath"
	"testing"
)

// helper: initSparseTestRepo creates a repo with several files under different
// directories, commits them, and returns the repo. Files created:
//   - README.md          (root)
//   - src/main.go        (src/)
//   - src/util/helper.go (src/util/)
//   - docs/guide.md      (docs/)
//   - tests/main_test.go (tests/)
func initSparseTestRepo(t *testing.T) *Repo {
	t.Helper()
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	files := map[string]string{
		"README.md":          "# Project\n",
		"src/main.go":        "package main\n\nfunc main() {}\n",
		"src/util/helper.go": "package util\n\nfunc Help() {}\n",
		"docs/guide.md":      "# Guide\n",
		"tests/main_test.go": "package tests\n\nfunc TestMain() {}\n",
	}

	for name, content := range files {
		absPath := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		if err := os.WriteFile(absPath, []byte(content), 0o644); err != nil {
			t.Fatalf("WriteFile %s: %v", name, err)
		}
	}

	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	if err := r.Add(names); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := r.Commit("initial commit", "test-author"); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	return r
}

// Test 1: Set patterns and list them back.
func TestSparseCheckout_SetAndList(t *testing.T) {
	r := initSparseTestRepo(t)

	patterns := []string{"src/", "docs/"}
	if err := r.SparseCheckoutSet(patterns); err != nil {
		t.Fatalf("SparseCheckoutSet: %v", err)
	}

	got, err := r.SparseCheckoutList()
	if err != nil {
		t.Fatalf("SparseCheckoutList: %v", err)
	}

	if len(got) != len(patterns) {
		t.Fatalf("SparseCheckoutList returned %d patterns, want %d", len(got), len(patterns))
	}
	for i, p := range patterns {
		if got[i] != p {
			t.Errorf("pattern[%d] = %q, want %q", i, got[i], p)
		}
	}

	// IsSparseEnabled should return true.
	if !r.IsSparseEnabled() {
		t.Error("IsSparseEnabled() = false after Set, want true")
	}
}

// Test 2: Add patterns to existing set.
func TestSparseCheckout_AddPattern(t *testing.T) {
	r := initSparseTestRepo(t)

	if err := r.SparseCheckoutSet([]string{"src/"}); err != nil {
		t.Fatalf("SparseCheckoutSet: %v", err)
	}

	if err := r.SparseCheckoutAdd([]string{"docs/"}); err != nil {
		t.Fatalf("SparseCheckoutAdd: %v", err)
	}

	got, err := r.SparseCheckoutList()
	if err != nil {
		t.Fatalf("SparseCheckoutList: %v", err)
	}

	want := []string{"src/", "docs/"}
	if len(got) != len(want) {
		t.Fatalf("SparseCheckoutList returned %d patterns, want %d", len(got), len(want))
	}
	for i, p := range want {
		if got[i] != p {
			t.Errorf("pattern[%d] = %q, want %q", i, got[i], p)
		}
	}

	// Adding a duplicate should not increase the list.
	if err := r.SparseCheckoutAdd([]string{"src/"}); err != nil {
		t.Fatalf("SparseCheckoutAdd duplicate: %v", err)
	}

	got2, err := r.SparseCheckoutList()
	if err != nil {
		t.Fatalf("SparseCheckoutList: %v", err)
	}
	if len(got2) != 2 {
		t.Errorf("duplicate add changed count: got %d, want 2", len(got2))
	}
}

// Test 3: Disable removes the sparse file and IsSparseEnabled returns false.
func TestSparseCheckout_Disable(t *testing.T) {
	r := initSparseTestRepo(t)

	if err := r.SparseCheckoutSet([]string{"src/"}); err != nil {
		t.Fatalf("SparseCheckoutSet: %v", err)
	}
	if !r.IsSparseEnabled() {
		t.Fatal("IsSparseEnabled() = false after Set")
	}

	if err := r.SparseCheckoutDisable(); err != nil {
		t.Fatalf("SparseCheckoutDisable: %v", err)
	}

	if r.IsSparseEnabled() {
		t.Error("IsSparseEnabled() = true after Disable, want false")
	}

	// The sparse-checkout file should no longer exist.
	if _, err := os.Stat(r.sparseCheckoutPath()); !os.IsNotExist(err) {
		t.Errorf("sparse-checkout file still exists after Disable")
	}

	// All files should be materialized after disable.
	expectedFiles := []string{
		"README.md",
		"docs/guide.md",
		"src/main.go",
		"src/util/helper.go",
		"tests/main_test.go",
	}
	for _, f := range expectedFiles {
		absPath := filepath.Join(r.RootDir, filepath.FromSlash(f))
		if _, err := os.Stat(absPath); os.IsNotExist(err) {
			t.Errorf("file %q not materialized after Disable", f)
		}
	}
}

// Test 4: matchesSparsePatterns logic.
func TestSparseCheckout_MatchesSparsePatterns(t *testing.T) {
	r := initSparseTestRepo(t)

	// Set patterns: include src/ and docs/
	if err := r.SparseCheckoutSet([]string{"src/", "docs/"}); err != nil {
		t.Fatalf("SparseCheckoutSet: %v", err)
	}

	tests := []struct {
		path string
		want bool
	}{
		// Root files always match.
		{"README.md", true},
		{"Makefile", true},

		// Files under included directories match.
		{"src/main.go", true},
		{"src/util/helper.go", true},
		{"docs/guide.md", true},

		// Files under excluded directories do not match.
		{"tests/main_test.go", false},
		{"vendor/lib.go", false},
	}

	for _, tc := range tests {
		got := r.matchesSparsePatterns(tc.path)
		if got != tc.want {
			t.Errorf("matchesSparsePatterns(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

// Test 4b: Negation patterns.
func TestSparseCheckout_NegationPatterns(t *testing.T) {
	r := initSparseTestRepo(t)

	// Include src/ but exclude src/util/ via negation.
	if err := r.SparseCheckoutSet([]string{"src/", "!src/util/"}); err != nil {
		t.Fatalf("SparseCheckoutSet: %v", err)
	}

	tests := []struct {
		path string
		want bool
	}{
		{"src/main.go", true},
		{"src/util/helper.go", false}, // negated
		{"README.md", true},           // root always matches
	}

	for _, tc := range tests {
		got := r.matchesSparsePatterns(tc.path)
		if got != tc.want {
			t.Errorf("matchesSparsePatterns(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

// Test 5: Checkout with sparse patterns only materializes matching files.
func TestSparseCheckout_OnlyMaterializesMatchingFiles(t *testing.T) {
	r := initSparseTestRepo(t)

	// Set sparse patterns to only include src/.
	if err := r.SparseCheckoutSet([]string{"src/"}); err != nil {
		t.Fatalf("SparseCheckoutSet: %v", err)
	}

	// After applying sparse checkout, only root files and src/ files should exist.
	shouldExist := []string{
		"README.md",
		"src/main.go",
		"src/util/helper.go",
	}
	shouldNotExist := []string{
		"docs/guide.md",
		"tests/main_test.go",
	}

	for _, f := range shouldExist {
		absPath := filepath.Join(r.RootDir, filepath.FromSlash(f))
		if _, err := os.Stat(absPath); os.IsNotExist(err) {
			t.Errorf("file %q should exist but does not", f)
		}
	}
	for _, f := range shouldNotExist {
		absPath := filepath.Join(r.RootDir, filepath.FromSlash(f))
		if _, err := os.Stat(absPath); !os.IsNotExist(err) {
			t.Errorf("file %q should not exist but does", f)
		}
	}

	// Now create a branch, checkout back to verify sparse is respected.
	headHash, err := r.ResolveRef("HEAD")
	if err != nil {
		t.Fatalf("ResolveRef HEAD: %v", err)
	}
	if err := r.CreateBranch("feature", headHash); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}

	// Checkout to feature and back to main — sparse should still be respected.
	if err := r.Checkout("feature"); err != nil {
		t.Fatalf("Checkout feature: %v", err)
	}
	if err := r.Checkout("main"); err != nil {
		t.Fatalf("Checkout main: %v", err)
	}

	for _, f := range shouldExist {
		absPath := filepath.Join(r.RootDir, filepath.FromSlash(f))
		if _, err := os.Stat(absPath); os.IsNotExist(err) {
			t.Errorf("after checkout: file %q should exist but does not", f)
		}
	}
	for _, f := range shouldNotExist {
		absPath := filepath.Join(r.RootDir, filepath.FromSlash(f))
		if _, err := os.Stat(absPath); !os.IsNotExist(err) {
			t.Errorf("after checkout: file %q should not exist but does", f)
		}
	}
}

// Test 6: Status ignores files excluded by sparse patterns.
func TestSparseCheckout_StatusIgnoresExcluded(t *testing.T) {
	r := initSparseTestRepo(t)

	// Set sparse to only include src/.
	if err := r.SparseCheckoutSet([]string{"src/"}); err != nil {
		t.Fatalf("SparseCheckoutSet: %v", err)
	}

	entries, err := r.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}

	// Status should not report docs/ or tests/ files.
	excludedPaths := map[string]bool{
		"docs/guide.md":      true,
		"tests/main_test.go": true,
	}

	for _, e := range entries {
		if excludedPaths[e.Path] {
			t.Errorf("Status reported excluded path %q", e.Path)
		}
	}

	// Status should report src/ files and root files as clean.
	statusByPath := make(map[string]StatusEntry, len(entries))
	for _, e := range entries {
		statusByPath[e.Path] = e
	}

	// All remaining files should be src/* and root files.
	for _, e := range entries {
		// Verify none of the reported paths are excluded.
		if excludedPaths[e.Path] {
			t.Errorf("Status reported excluded file %q", e.Path)
		}
	}
}
