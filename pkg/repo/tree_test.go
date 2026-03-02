package repo

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/odvcencio/graft/pkg/object"
)

// ---------------------------------------------------------------------------
// WriteTree (BuildTree) and read-back via FlattenTree
// ---------------------------------------------------------------------------

func TestTreeBuildAndFlatten_SingleFile(t *testing.T) {
	r := initRepoWithFile(t, "hello.txt", []byte("hello world"))

	stg, err := r.ReadStaging()
	if err != nil {
		t.Fatalf("ReadStaging: %v", err)
	}

	rootHash, err := r.BuildTree(stg)
	if err != nil {
		t.Fatalf("BuildTree: %v", err)
	}
	if rootHash == "" {
		t.Fatal("BuildTree returned empty hash")
	}

	entries, err := r.FlattenTree(rootHash)
	if err != nil {
		t.Fatalf("FlattenTree: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("FlattenTree returned %d entries, want 1", len(entries))
	}
	if entries[0].Path != "hello.txt" {
		t.Fatalf("entry path = %q, want %q", entries[0].Path, "hello.txt")
	}
	if entries[0].BlobHash != stg.Entries["hello.txt"].BlobHash {
		t.Fatalf("BlobHash mismatch")
	}
}

func TestTreeBuildAndFlatten_NestedDirectories(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Create a deeply nested file structure.
	files := map[string][]byte{
		"a/b/c.txt":     []byte("deep file"),
		"a/b/d.txt":     []byte("another deep file"),
		"a/top.txt":     []byte("mid-level file"),
		"root.txt":      []byte("root file"),
		"x/y/z/leaf.go": []byte("package leaf\n\nfunc Leaf() {}\n"),
	}

	for name, data := range files {
		parent := filepath.Dir(filepath.Join(dir, name))
		if err := os.MkdirAll(parent, 0o755); err != nil {
			t.Fatalf("MkdirAll(%s): %v", parent, err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

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

	rootHash, err := r.BuildTree(stg)
	if err != nil {
		t.Fatalf("BuildTree: %v", err)
	}

	entries, err := r.FlattenTree(rootHash)
	if err != nil {
		t.Fatalf("FlattenTree: %v", err)
	}

	if len(entries) != len(files) {
		t.Fatalf("FlattenTree returned %d entries, want %d", len(entries), len(files))
	}

	flatPaths := make(map[string]TreeFileEntry)
	for _, e := range entries {
		flatPaths[e.Path] = e
	}

	for path, data := range files {
		fe, ok := flatPaths[path]
		if !ok {
			t.Errorf("missing path %q in flattened tree", path)
			continue
		}
		// Verify blob hash matches what staging recorded.
		se := stg.Entries[path]
		if fe.BlobHash != se.BlobHash {
			t.Errorf("%s: BlobHash mismatch (flat=%q, staging=%q)", path, fe.BlobHash, se.BlobHash)
		}
		// Verify the blob data round-trips correctly.
		blob, err := r.Store.ReadBlob(fe.BlobHash)
		if err != nil {
			t.Errorf("%s: ReadBlob: %v", path, err)
			continue
		}
		if string(blob.Data) != string(data) {
			t.Errorf("%s: blob data mismatch: got %q, want %q", path, blob.Data, data)
		}
	}
}

func TestTreeBuild_EmptyStaging(t *testing.T) {
	r, err := Init(t.TempDir())
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	stg := &Staging{Entries: map[string]*StagingEntry{}}
	rootHash, err := r.BuildTree(stg)
	if err != nil {
		t.Fatalf("BuildTree empty: %v", err)
	}
	if rootHash == "" {
		t.Fatal("BuildTree empty returned empty hash")
	}

	entries, err := r.FlattenTree(rootHash)
	if err != nil {
		t.Fatalf("FlattenTree empty: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("FlattenTree empty returned %d entries, want 0", len(entries))
	}
}

func TestTreeBuild_PreservesFileMode(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Create a regular file and an executable file.
	if err := os.WriteFile(filepath.Join(dir, "regular.txt"), []byte("regular"), 0o644); err != nil {
		t.Fatalf("write regular.txt: %v", err)
	}
	scriptPath := filepath.Join(dir, "run.sh")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\necho hi\n"), 0o755); err != nil {
		t.Fatalf("write run.sh: %v", err)
	}

	if err := r.Add([]string{"regular.txt", "run.sh"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	stg, err := r.ReadStaging()
	if err != nil {
		t.Fatalf("ReadStaging: %v", err)
	}

	rootHash, err := r.BuildTree(stg)
	if err != nil {
		t.Fatalf("BuildTree: %v", err)
	}

	entries, err := r.FlattenTree(rootHash)
	if err != nil {
		t.Fatalf("FlattenTree: %v", err)
	}

	byPath := make(map[string]TreeFileEntry)
	for _, e := range entries {
		byPath[e.Path] = e
	}

	if e, ok := byPath["regular.txt"]; !ok {
		t.Fatal("missing regular.txt")
	} else if e.Mode != object.TreeModeFile {
		t.Fatalf("regular.txt mode = %q, want %q", e.Mode, object.TreeModeFile)
	}

	if e, ok := byPath["run.sh"]; !ok {
		t.Fatal("missing run.sh")
	} else if e.Mode != object.TreeModeExecutable {
		t.Fatalf("run.sh mode = %q, want %q", e.Mode, object.TreeModeExecutable)
	}
}

func TestTreeBuild_DeterministicHash(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Create files.
	files := map[string][]byte{
		"a.txt": []byte("alpha"),
		"b.txt": []byte("bravo"),
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	if err := r.Add([]string{"a.txt", "b.txt"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	stg, err := r.ReadStaging()
	if err != nil {
		t.Fatalf("ReadStaging: %v", err)
	}

	h1, err := r.BuildTree(stg)
	if err != nil {
		t.Fatalf("BuildTree(1): %v", err)
	}
	h2, err := r.BuildTree(stg)
	if err != nil {
		t.Fatalf("BuildTree(2): %v", err)
	}

	if h1 != h2 {
		t.Fatalf("BuildTree is not deterministic: %q != %q", h1, h2)
	}
}

func TestTreeBuild_CommitRoundTrip(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Create nested files, commit, then verify the tree from the commit.
	files := map[string][]byte{
		"src/main.go":  []byte("package main\n\nfunc main() {}\n"),
		"src/util.go":  []byte("package main\n\nfunc util() {}\n"),
		"README.md":    []byte("# readme"),
		"lib/helper.go": []byte("package lib\n\nfunc Helper() {}\n"),
	}
	for name, data := range files {
		parent := filepath.Dir(filepath.Join(dir, name))
		if err := os.MkdirAll(parent, 0o755); err != nil {
			t.Fatalf("MkdirAll(%s): %v", parent, err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	paths := make([]string, 0, len(files))
	for name := range files {
		paths = append(paths, name)
	}
	if err := r.Add(paths); err != nil {
		t.Fatalf("Add: %v", err)
	}

	commitHash, err := r.Commit("tree round-trip test", "test-author")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// Read commit to get tree hash.
	c, err := r.Store.ReadCommit(commitHash)
	if err != nil {
		t.Fatalf("ReadCommit: %v", err)
	}

	entries, err := r.FlattenTree(c.TreeHash)
	if err != nil {
		t.Fatalf("FlattenTree: %v", err)
	}

	if len(entries) != len(files) {
		t.Fatalf("FlattenTree returned %d entries, want %d", len(entries), len(files))
	}

	flatPaths := make(map[string]bool)
	for _, e := range entries {
		flatPaths[e.Path] = true
	}
	for path := range files {
		if !flatPaths[path] {
			t.Errorf("missing %q in committed tree", path)
		}
	}
}

// ---------------------------------------------------------------------------
// treeEntryAtPath (from tree_lookup.go)
// ---------------------------------------------------------------------------

func TestTreeEntryAtPath_RootLevelFile(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	data := []byte("hello world")
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), data, 0o644); err != nil {
		t.Fatalf("write hello.txt: %v", err)
	}
	if err := r.Add([]string{"hello.txt"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	stg, err := r.ReadStaging()
	if err != nil {
		t.Fatalf("ReadStaging: %v", err)
	}
	rootHash, err := r.BuildTree(stg)
	if err != nil {
		t.Fatalf("BuildTree: %v", err)
	}

	entry, found, err := r.treeEntryAtPath(rootHash, "hello.txt")
	if err != nil {
		t.Fatalf("treeEntryAtPath: %v", err)
	}
	if !found {
		t.Fatal("treeEntryAtPath returned found=false for hello.txt")
	}
	if entry.Name != "hello.txt" {
		t.Fatalf("entry.Name = %q, want %q", entry.Name, "hello.txt")
	}
	if entry.BlobHash != stg.Entries["hello.txt"].BlobHash {
		t.Fatalf("BlobHash mismatch")
	}
}

func TestTreeEntryAtPath_NestedFile(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	if err := os.MkdirAll(filepath.Join(dir, "a", "b"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	data := []byte("deep file content")
	if err := os.WriteFile(filepath.Join(dir, "a", "b", "c.txt"), data, 0o644); err != nil {
		t.Fatalf("write a/b/c.txt: %v", err)
	}
	if err := r.Add([]string{"a/b/c.txt"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	stg, err := r.ReadStaging()
	if err != nil {
		t.Fatalf("ReadStaging: %v", err)
	}
	rootHash, err := r.BuildTree(stg)
	if err != nil {
		t.Fatalf("BuildTree: %v", err)
	}

	entry, found, err := r.treeEntryAtPath(rootHash, "a/b/c.txt")
	if err != nil {
		t.Fatalf("treeEntryAtPath: %v", err)
	}
	if !found {
		t.Fatal("treeEntryAtPath returned found=false for a/b/c.txt")
	}
	if entry.Name != "c.txt" {
		t.Fatalf("entry.Name = %q, want %q", entry.Name, "c.txt")
	}
	if entry.BlobHash != stg.Entries["a/b/c.txt"].BlobHash {
		t.Fatalf("BlobHash mismatch")
	}
}

func TestTreeEntryAtPath_MissingFileReturnsNotFound(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "exists.txt"), []byte("here"), 0o644); err != nil {
		t.Fatalf("write exists.txt: %v", err)
	}
	if err := r.Add([]string{"exists.txt"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	stg, err := r.ReadStaging()
	if err != nil {
		t.Fatalf("ReadStaging: %v", err)
	}
	rootHash, err := r.BuildTree(stg)
	if err != nil {
		t.Fatalf("BuildTree: %v", err)
	}

	_, found, err := r.treeEntryAtPath(rootHash, "nonexistent.txt")
	if err != nil {
		t.Fatalf("treeEntryAtPath: %v", err)
	}
	if found {
		t.Fatal("expected found=false for nonexistent file")
	}
}

func TestTreeEntryAtPath_MissingNestedDirReturnsNotFound(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	if err := os.MkdirAll(filepath.Join(dir, "a"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a", "file.txt"), []byte("content"), 0o644); err != nil {
		t.Fatalf("write a/file.txt: %v", err)
	}
	if err := r.Add([]string{"a/file.txt"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	stg, err := r.ReadStaging()
	if err != nil {
		t.Fatalf("ReadStaging: %v", err)
	}
	rootHash, err := r.BuildTree(stg)
	if err != nil {
		t.Fatalf("BuildTree: %v", err)
	}

	// "b/file.txt" does not exist — intermediate dir "b" is missing.
	_, found, err := r.treeEntryAtPath(rootHash, "b/file.txt")
	if err != nil {
		t.Fatalf("treeEntryAtPath: %v", err)
	}
	if found {
		t.Fatal("expected found=false for nonexistent directory path")
	}
}

func TestTreeEntryAtPath_DirectoryNameReturnsNotFound(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	if err := os.MkdirAll(filepath.Join(dir, "subdir"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "subdir", "file.txt"), []byte("content"), 0o644); err != nil {
		t.Fatalf("write subdir/file.txt: %v", err)
	}
	if err := r.Add([]string{"subdir/file.txt"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	stg, err := r.ReadStaging()
	if err != nil {
		t.Fatalf("ReadStaging: %v", err)
	}
	rootHash, err := r.BuildTree(stg)
	if err != nil {
		t.Fatalf("BuildTree: %v", err)
	}

	// Looking up a directory name (not a file) should return not found,
	// because treeEntryAtPath only returns file entries.
	_, found, err := r.treeEntryAtPath(rootHash, "subdir")
	if err != nil {
		t.Fatalf("treeEntryAtPath: %v", err)
	}
	if found {
		t.Fatal("expected found=false when looking up a directory name")
	}
}

func TestTreeEntryAtPath_MultipleFilesInSameDir(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	if err := os.MkdirAll(filepath.Join(dir, "pkg"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	files := map[string][]byte{
		"pkg/a.go": []byte("package pkg\n\nfunc A() {}\n"),
		"pkg/b.go": []byte("package pkg\n\nfunc B() {}\n"),
		"pkg/c.go": []byte("package pkg\n\nfunc C() {}\n"),
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

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
	rootHash, err := r.BuildTree(stg)
	if err != nil {
		t.Fatalf("BuildTree: %v", err)
	}

	// Each file should be independently findable.
	for name := range files {
		entry, found, err := r.treeEntryAtPath(rootHash, name)
		if err != nil {
			t.Fatalf("treeEntryAtPath(%s): %v", name, err)
		}
		if !found {
			t.Fatalf("treeEntryAtPath(%s) returned found=false", name)
		}
		if entry.BlobHash != stg.Entries[name].BlobHash {
			t.Fatalf("%s: BlobHash mismatch", name)
		}
	}
}
