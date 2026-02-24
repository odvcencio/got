package repo

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/odvcencio/got/pkg/object"
)

// Test 1: Add a Go file — blob + entity list stored, both hashes non-empty.
func TestAdd_GoFile_BlobAndEntityList(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Write a Go source file into the working directory.
	goSrc := []byte("package main\n\nfunc hello() {\n\tprintln(\"hello\")\n}\n")
	if err := os.WriteFile(filepath.Join(dir, "main.go"), goSrc, 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}

	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	stg, err := r.ReadStaging()
	if err != nil {
		t.Fatalf("ReadStaging: %v", err)
	}

	entry, ok := stg.Entries["main.go"]
	if !ok {
		t.Fatalf("staging missing entry for main.go; entries: %v", stg.Entries)
	}

	if entry.BlobHash == "" {
		t.Error("BlobHash is empty, want non-empty")
	}
	if entry.EntityListHash == "" {
		t.Error("EntityListHash is empty, want non-empty for .go file")
	}

	// Verify the blob is readable from the store.
	blob, err := r.Store.ReadBlob(entry.BlobHash)
	if err != nil {
		t.Fatalf("ReadBlob: %v", err)
	}
	if string(blob.Data) != string(goSrc) {
		t.Errorf("blob data mismatch:\ngot:  %q\nwant: %q", blob.Data, goSrc)
	}

	// Verify the entity list is readable from the store.
	el, err := r.Store.ReadEntityList(entry.EntityListHash)
	if err != nil {
		t.Fatalf("ReadEntityList: %v", err)
	}
	if len(el.EntityRefs) == 0 {
		t.Error("EntityRefs is empty, want at least one entity ref")
	}
	if el.Path != "main.go" {
		t.Errorf("EntityList.Path = %q, want %q", el.Path, "main.go")
	}
}

// Test 2: Add binary/unknown file — blob stored, EntityListHash empty.
func TestAdd_BinaryFile_BlobOnly(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	binData := []byte{0x00, 0xFF, 0xDE, 0xAD, 0xBE, 0xEF}
	if err := os.WriteFile(filepath.Join(dir, "data.bin"), binData, 0o644); err != nil {
		t.Fatalf("write data.bin: %v", err)
	}

	if err := r.Add([]string{"data.bin"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	stg, err := r.ReadStaging()
	if err != nil {
		t.Fatalf("ReadStaging: %v", err)
	}

	entry, ok := stg.Entries["data.bin"]
	if !ok {
		t.Fatal("staging missing entry for data.bin")
	}

	if entry.BlobHash == "" {
		t.Error("BlobHash is empty, want non-empty")
	}
	if entry.EntityListHash != "" {
		t.Errorf("EntityListHash = %q, want empty for binary file", entry.EntityListHash)
	}

	// Verify blob content.
	blob, err := r.Store.ReadBlob(entry.BlobHash)
	if err != nil {
		t.Fatalf("ReadBlob: %v", err)
	}
	if string(blob.Data) != string(binData) {
		t.Error("blob data mismatch for binary file")
	}
}

// Test 3: Add multiple files — all entries present in staging.
func TestAdd_MultipleFiles(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	files := map[string][]byte{
		"a.go":  []byte("package a\n\nfunc A() {}\n"),
		"b.go":  []byte("package b\n\nfunc B() {}\n"),
		"c.txt": []byte("hello world"),
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	paths := []string{"a.go", "b.go", "c.txt"}
	if err := r.Add(paths); err != nil {
		t.Fatalf("Add: %v", err)
	}

	stg, err := r.ReadStaging()
	if err != nil {
		t.Fatalf("ReadStaging: %v", err)
	}

	for _, name := range paths {
		if _, ok := stg.Entries[name]; !ok {
			t.Errorf("staging missing entry for %s", name)
		}
	}

	// Go files should have entity lists, txt should not.
	if stg.Entries["a.go"].EntityListHash == "" {
		t.Error("a.go EntityListHash is empty, want non-empty")
	}
	if stg.Entries["b.go"].EntityListHash == "" {
		t.Error("b.go EntityListHash is empty, want non-empty")
	}
	if stg.Entries["c.txt"].EntityListHash != "" {
		t.Errorf("c.txt EntityListHash = %q, want empty", stg.Entries["c.txt"].EntityListHash)
	}
}

// Test 4: Re-add modified file — hash changes in staging entry.
func TestAdd_ReaddModifiedFile(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Write original file and add.
	original := []byte("package main\n\nfunc hello() {}\n")
	fpath := filepath.Join(dir, "main.go")
	if err := os.WriteFile(fpath, original, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add (original): %v", err)
	}

	stg1, err := r.ReadStaging()
	if err != nil {
		t.Fatalf("ReadStaging (1): %v", err)
	}
	hash1 := stg1.Entries["main.go"].BlobHash

	// Modify file and re-add.
	modified := []byte("package main\n\nfunc hello() { println(\"modified\") }\n")
	if err := os.WriteFile(fpath, modified, 0o644); err != nil {
		t.Fatalf("write modified: %v", err)
	}
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add (modified): %v", err)
	}

	stg2, err := r.ReadStaging()
	if err != nil {
		t.Fatalf("ReadStaging (2): %v", err)
	}
	hash2 := stg2.Entries["main.go"].BlobHash

	if hash1 == hash2 {
		t.Errorf("BlobHash did not change after modifying file: both = %s", hash1)
	}
}

// Test 5: Read/write staging round-trip — write staging, read it back, entries match.
func TestStaging_ReadWriteRoundTrip(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	stg := &Staging{
		Entries: map[string]*StagingEntry{
			"foo.go": {
				Path:           "foo.go",
				BlobHash:       object.Hash("aaaa"),
				EntityListHash: object.Hash("bbbb"),
				ModTime:        1234567890,
				Size:           42,
			},
			"bar.txt": {
				Path:     "bar.txt",
				BlobHash: object.Hash("cccc"),
				ModTime:  9876543210,
				Size:     100,
			},
		},
	}

	if err := r.WriteStaging(stg); err != nil {
		t.Fatalf("WriteStaging: %v", err)
	}

	got, err := r.ReadStaging()
	if err != nil {
		t.Fatalf("ReadStaging: %v", err)
	}

	if len(got.Entries) != len(stg.Entries) {
		t.Fatalf("entry count = %d, want %d", len(got.Entries), len(stg.Entries))
	}

	for path, want := range stg.Entries {
		g, ok := got.Entries[path]
		if !ok {
			t.Errorf("missing entry for %q after round-trip", path)
			continue
		}
		if g.Path != want.Path {
			t.Errorf("Path: got %q, want %q", g.Path, want.Path)
		}
		if g.BlobHash != want.BlobHash {
			t.Errorf("BlobHash: got %q, want %q", g.BlobHash, want.BlobHash)
		}
		if g.EntityListHash != want.EntityListHash {
			t.Errorf("EntityListHash: got %q, want %q", g.EntityListHash, want.EntityListHash)
		}
		if g.ModTime != want.ModTime {
			t.Errorf("ModTime: got %d, want %d", g.ModTime, want.ModTime)
		}
		if g.Size != want.Size {
			t.Errorf("Size: got %d, want %d", g.Size, want.Size)
		}
	}
}

// Test 6: ReadStaging on empty repo returns empty Staging (no error).
func TestStaging_ReadEmpty(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	stg, err := r.ReadStaging()
	if err != nil {
		t.Fatalf("ReadStaging on fresh repo: %v", err)
	}
	if stg == nil {
		t.Fatal("ReadStaging returned nil")
	}
	if len(stg.Entries) != 0 {
		t.Errorf("expected empty entries, got %d", len(stg.Entries))
	}
}

// Test 7: Add with absolute path converts to repo-relative path.
func TestAdd_AbsolutePathConverted(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	data := []byte("package main\n")
	absPath := filepath.Join(dir, "abs.go")
	if err := os.WriteFile(absPath, data, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Pass absolute path.
	if err := r.Add([]string{absPath}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	stg, err := r.ReadStaging()
	if err != nil {
		t.Fatalf("ReadStaging: %v", err)
	}

	// Entry should be keyed by repo-relative path, not absolute.
	if _, ok := stg.Entries["abs.go"]; !ok {
		t.Errorf("expected entry keyed as 'abs.go', got keys: %v", keys(stg.Entries))
	}
}

// Test 8: Add file in subdirectory preserves relative path.
func TestAdd_SubdirectoryPath(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	sub := filepath.Join(dir, "pkg", "util")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	data := []byte("package util\n\nfunc Util() {}\n")
	if err := os.WriteFile(filepath.Join(sub, "util.go"), data, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := r.Add([]string{"pkg/util/util.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	stg, err := r.ReadStaging()
	if err != nil {
		t.Fatalf("ReadStaging: %v", err)
	}

	if _, ok := stg.Entries["pkg/util/util.go"]; !ok {
		t.Errorf("expected entry keyed as 'pkg/util/util.go', got keys: %v", keys(stg.Entries))
	}
}

func TestAdd_DotStagesRecursivelyAndHonorsIgnore(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, ".gotignore"), []byte("ignored.txt\nbuild/\n"), 0o644); err != nil {
		t.Fatalf("write .gotignore: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "pkg"), 0o755); err != nil {
		t.Fatalf("mkdir pkg: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "build"), 0o755); err != nil {
		t.Fatalf("mkdir build: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pkg", "util.go"), []byte("package pkg\n"), 0o644); err != nil {
		t.Fatalf("write pkg/util.go: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ignored.txt"), []byte("nope\n"), 0o644); err != nil {
		t.Fatalf("write ignored.txt: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "build", "gen.go"), []byte("package build\n"), 0o644); err != nil {
		t.Fatalf("write build/gen.go: %v", err)
	}

	if err := r.Add([]string{"."}); err != nil {
		t.Fatalf("Add .: %v", err)
	}

	stg, err := r.ReadStaging()
	if err != nil {
		t.Fatalf("ReadStaging: %v", err)
	}

	if _, ok := stg.Entries["main.go"]; !ok {
		t.Fatalf("expected main.go to be staged; keys: %v", keys(stg.Entries))
	}
	if _, ok := stg.Entries["pkg/util.go"]; !ok {
		t.Fatalf("expected pkg/util.go to be staged; keys: %v", keys(stg.Entries))
	}
	if _, ok := stg.Entries["ignored.txt"]; ok {
		t.Fatalf("ignored.txt should not be staged")
	}
	if _, ok := stg.Entries["build/gen.go"]; ok {
		t.Fatalf("build/gen.go should not be staged")
	}
}

func TestAdd_GlobPathspecStagesMatchingFiles(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatalf("write a.go: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.go"), []byte("package b\n"), 0o644); err != nil {
		t.Fatalf("write b.go: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "c.txt"), []byte("plain\n"), 0o644); err != nil {
		t.Fatalf("write c.txt: %v", err)
	}

	if err := r.Add([]string{"*.go"}); err != nil {
		t.Fatalf("Add *.go: %v", err)
	}

	stg, err := r.ReadStaging()
	if err != nil {
		t.Fatalf("ReadStaging: %v", err)
	}

	if len(stg.Entries) != 2 {
		t.Fatalf("expected 2 staged files, got %d: %v", len(stg.Entries), keys(stg.Entries))
	}
	if _, ok := stg.Entries["a.go"]; !ok {
		t.Fatalf("expected a.go staged; keys: %v", keys(stg.Entries))
	}
	if _, ok := stg.Entries["b.go"]; !ok {
		t.Fatalf("expected b.go staged; keys: %v", keys(stg.Entries))
	}
	if _, ok := stg.Entries["c.txt"]; ok {
		t.Fatalf("did not expect c.txt staged")
	}
}

func TestRemove_RemovesFromIndexAndWorktree(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	if err := r.Remove([]string{"main.go"}, false); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected main.go removed from worktree, stat err=%v", err)
	}

	stg, err := r.ReadStaging()
	if err != nil {
		t.Fatalf("ReadStaging: %v", err)
	}
	if _, ok := stg.Entries["main.go"]; ok {
		t.Fatalf("main.go should be removed from staging")
	}
}

func TestRemove_CachedKeepsWorktreeFile(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	if err := r.Remove([]string{"main.go"}, true); err != nil {
		t.Fatalf("Remove --cached: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected main.go to remain on disk, stat err=%v", err)
	}

	stg, err := r.ReadStaging()
	if err != nil {
		t.Fatalf("ReadStaging: %v", err)
	}
	if _, ok := stg.Entries["main.go"]; ok {
		t.Fatalf("main.go should be removed from staging")
	}
}

func TestRemove_DirectoryPathRemovesTrackedPrefix(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	if err := os.MkdirAll(filepath.Join(dir, "pkg"), 0o755); err != nil {
		t.Fatalf("mkdir pkg: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pkg", "a.go"), []byte("package pkg\n"), 0o644); err != nil {
		t.Fatalf("write pkg/a.go: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pkg", "b.go"), []byte("package pkg\n"), 0o644); err != nil {
		t.Fatalf("write pkg/b.go: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}
	if err := r.Add([]string{"main.go", "pkg/a.go", "pkg/b.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	if err := r.Remove([]string{"pkg"}, true); err != nil {
		t.Fatalf("Remove pkg --cached: %v", err)
	}

	stg, err := r.ReadStaging()
	if err != nil {
		t.Fatalf("ReadStaging: %v", err)
	}
	if _, ok := stg.Entries["main.go"]; !ok {
		t.Fatalf("expected main.go to remain staged")
	}
	if _, ok := stg.Entries["pkg/a.go"]; ok {
		t.Fatalf("expected pkg/a.go to be removed from staging")
	}
	if _, ok := stg.Entries["pkg/b.go"]; ok {
		t.Fatalf("expected pkg/b.go to be removed from staging")
	}
}

// helper: keys of a map.
func keys(m map[string]*StagingEntry) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}
