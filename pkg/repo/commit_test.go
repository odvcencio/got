package repo

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/odvcencio/got/pkg/object"
)

// helper: initRepoWithFile creates a temp repo, writes a Go file, and stages it.
func initRepoWithFile(t *testing.T, name string, content []byte) *Repo {
	t.Helper()
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Create parent directory if needed.
	parent := filepath.Dir(filepath.Join(dir, name))
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, name), content, 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	if err := r.Add([]string{name}); err != nil {
		t.Fatalf("Add(%s): %v", name, err)
	}
	return r
}

// Test 1: Commit creates object in store.
func TestCommit_CreatesObject(t *testing.T) {
	r := initRepoWithFile(t, "main.go", []byte("package main\n\nfunc main() {}\n"))

	h, err := r.Commit("initial commit", "test-author")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if h == "" {
		t.Fatal("Commit returned empty hash")
	}

	// Read commit back from store.
	c, err := r.Store.ReadCommit(h)
	if err != nil {
		t.Fatalf("ReadCommit(%s): %v", h, err)
	}
	if c.Message != "initial commit" {
		t.Errorf("Message = %q, want %q", c.Message, "initial commit")
	}
	if c.Author != "test-author" {
		t.Errorf("Author = %q, want %q", c.Author, "test-author")
	}
	if c.TreeHash == "" {
		t.Error("TreeHash is empty")
	}
	if c.Timestamp == 0 {
		t.Error("Timestamp is zero")
	}
	if len(c.Parents) != 0 {
		t.Errorf("first commit should have no parents, got %d", len(c.Parents))
	}
}

// Test 2: Commit updates HEAD.
func TestCommit_UpdatesHEAD(t *testing.T) {
	r := initRepoWithFile(t, "main.go", []byte("package main\n\nfunc main() {}\n"))

	h, err := r.Commit("initial commit", "test-author")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	headHash, err := r.ResolveRef("HEAD")
	if err != nil {
		t.Fatalf("ResolveRef(HEAD): %v", err)
	}
	if headHash != h {
		t.Errorf("HEAD = %q, want %q", headHash, h)
	}
}

// Test 3: Second commit has first as parent.
func TestCommit_SecondHasParent(t *testing.T) {
	r := initRepoWithFile(t, "main.go", []byte("package main\n\nfunc main() {}\n"))

	h1, err := r.Commit("first commit", "test-author")
	if err != nil {
		t.Fatalf("first Commit: %v", err)
	}

	// Modify file and re-add for second commit.
	if err := os.WriteFile(filepath.Join(r.RootDir, "main.go"),
		[]byte("package main\n\nfunc main() { println(\"v2\") }\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	h2, err := r.Commit("second commit", "test-author")
	if err != nil {
		t.Fatalf("second Commit: %v", err)
	}

	c2, err := r.Store.ReadCommit(h2)
	if err != nil {
		t.Fatalf("ReadCommit(%s): %v", h2, err)
	}
	if len(c2.Parents) != 1 {
		t.Fatalf("second commit parents = %d, want 1", len(c2.Parents))
	}
	if c2.Parents[0] != h1 {
		t.Errorf("second commit parent = %q, want %q", c2.Parents[0], h1)
	}
}

// Test 4: Log returns reverse-chronological order.
func TestLog_ReverseChronological(t *testing.T) {
	r := initRepoWithFile(t, "main.go", []byte("package main\n\nfunc main() {}\n"))

	hashes := make([]object.Hash, 3)
	messages := []string{"first", "second", "third"}

	for i, msg := range messages {
		if i > 0 {
			content := []byte("package main\n\nfunc main() { _ = " + msg + " }\n")
			if err := os.WriteFile(filepath.Join(r.RootDir, "main.go"), content, 0o644); err != nil {
				t.Fatalf("write: %v", err)
			}
			if err := r.Add([]string{"main.go"}); err != nil {
				t.Fatalf("Add: %v", err)
			}
		}
		h, err := r.Commit(msg, "test-author")
		if err != nil {
			t.Fatalf("Commit(%q): %v", msg, err)
		}
		hashes[i] = h
	}

	// Log from the latest commit, limit 10 (more than we have).
	commits, err := r.Log(hashes[2], 10)
	if err != nil {
		t.Fatalf("Log: %v", err)
	}
	if len(commits) != 3 {
		t.Fatalf("Log returned %d commits, want 3", len(commits))
	}

	// Verify order: newest first.
	if commits[0].Message != "third" {
		t.Errorf("commits[0].Message = %q, want %q", commits[0].Message, "third")
	}
	if commits[1].Message != "second" {
		t.Errorf("commits[1].Message = %q, want %q", commits[1].Message, "second")
	}
	if commits[2].Message != "first" {
		t.Errorf("commits[2].Message = %q, want %q", commits[2].Message, "first")
	}

	// Log with limit = 2 should only return 2 commits.
	limited, err := r.Log(hashes[2], 2)
	if err != nil {
		t.Fatalf("Log(limit=2): %v", err)
	}
	if len(limited) != 2 {
		t.Fatalf("Log(limit=2) returned %d commits, want 2", len(limited))
	}
}

// Test 5: BuildTree + FlattenTree round-trip.
func TestBuildTree_FlattenTree_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Create files in nested directories.
	files := map[string][]byte{
		"README.md":          []byte("# readme"),
		"pkg/util/util.go":   []byte("package util\n\nfunc Util() {}\n"),
		"pkg/util/helper.go": []byte("package util\n\nfunc Helper() {}\n"),
		"cmd/main.go":        []byte("package main\n\nfunc main() {}\n"),
	}
	for name, data := range files {
		parent := filepath.Dir(filepath.Join(dir, name))
		if err := os.MkdirAll(parent, 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	// Add all files.
	paths := make([]string, 0, len(files))
	for name := range files {
		paths = append(paths, name)
	}
	if err := r.Add(paths); err != nil {
		t.Fatalf("Add: %v", err)
	}

	stg, err := r.ReadStaging()
	if err != nil {
		t.Fatalf("ReadStaging: %v", err)
	}

	// Build tree from staging.
	rootHash, err := r.BuildTree(stg)
	if err != nil {
		t.Fatalf("BuildTree: %v", err)
	}
	if rootHash == "" {
		t.Fatal("BuildTree returned empty hash")
	}

	// Flatten and verify all files are present.
	entries, err := r.FlattenTree(rootHash)
	if err != nil {
		t.Fatalf("FlattenTree: %v", err)
	}

	if len(entries) != len(files) {
		t.Fatalf("FlattenTree returned %d entries, want %d", len(entries), len(files))
	}

	// Build a set of paths from flattened entries.
	flatPaths := make(map[string]TreeFileEntry)
	for _, e := range entries {
		flatPaths[e.Path] = e
	}

	// Verify each staging entry appears in the flattened tree.
	for path, se := range stg.Entries {
		fe, ok := flatPaths[path]
		if !ok {
			t.Errorf("missing path %q in flattened tree", path)
			continue
		}
		if fe.BlobHash != se.BlobHash {
			t.Errorf("%s: BlobHash = %q, want %q", path, fe.BlobHash, se.BlobHash)
		}
		if fe.EntityListHash != se.EntityListHash {
			t.Errorf("%s: EntityListHash = %q, want %q", path, fe.EntityListHash, se.EntityListHash)
		}
	}
}
