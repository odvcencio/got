package repo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/odvcencio/graft/pkg/entity"
	"github.com/odvcencio/graft/pkg/object"
)

func TestLogByEntity_SkipsCommitWhenExtractionFails(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	mainV1 := "package main\n\nfunc Target() int {\n\treturn 1\n}\n"
	mainV2 := "package main\n\nfunc Target() int {\n\treturn 2\n}\n"
	mainV3 := "package main\n\nfunc Target() int {\n\treturn 3\n}\n"

	writeRepoSource(t, dir, "main.go", mainV1)
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add(main.go): %v", err)
	}
	h1, err := r.Commit("first", "tester")
	if err != nil {
		t.Fatalf("Commit(first): %v", err)
	}

	targetKey := declarationIdentityKey(t, "main.go", mainV1, "Target")

	writeRepoSource(t, dir, "main.go", mainV2)
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add(main.go v2): %v", err)
	}

	stg, err := r.ReadStaging()
	if err != nil {
		t.Fatalf("ReadStaging: %v", err)
	}

	badBlobHash, err := r.Store.WriteBlob(&object.Blob{Data: []byte("not go code")})
	if err != nil {
		t.Fatalf("WriteBlob(bad): %v", err)
	}
	fakeEntityListHash, err := r.Store.WriteEntityList(&object.EntityListObj{
		Language: "go",
		Path:     "bad.txt",
	})
	if err != nil {
		t.Fatalf("WriteEntityList(fake): %v", err)
	}

	stg.Entries["bad.txt"] = &StagingEntry{
		Path:           "bad.txt",
		BlobHash:       badBlobHash,
		EntityListHash: fakeEntityListHash,
		Mode:           object.TreeModeFile,
	}
	if err := r.WriteStaging(stg); err != nil {
		t.Fatalf("WriteStaging(with bad.txt): %v", err)
	}

	if _, err := r.Commit("parse fail commit", "tester"); err != nil {
		t.Fatalf("Commit(parse fail commit): %v", err)
	}

	writeRepoSource(t, dir, "main.go", mainV3)
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add(main.go v3): %v", err)
	}
	h3, err := r.Commit("third", "tester")
	if err != nil {
		t.Fatalf("Commit(third): %v", err)
	}

	head, err := r.ResolveRef("HEAD")
	if err != nil {
		t.Fatalf("ResolveRef(HEAD): %v", err)
	}

	entries, err := r.LogByEntity(head, 10, "", targetKey)
	if err != nil {
		t.Fatalf("LogByEntity: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("LogByEntity returned %d entries, want 2", len(entries))
	}
	if entries[0].Hash != h3 {
		t.Fatalf("entries[0].Hash = %q, want %q", entries[0].Hash, h3)
	}
	if entries[1].Hash != h1 {
		t.Fatalf("entries[1].Hash = %q, want %q", entries[1].Hash, h1)
	}
	for _, entry := range entries {
		if entry.Commit.Message == "parse fail commit" {
			t.Fatalf("parse-failure commit should be skipped, got %q", entry.Commit.Message)
		}
	}
}

func TestLogByEntity_PathFilterTracksKeyShiftContinuity(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	mainV1 := "package main\n\nfunc Target() int {\n\treturn 1\n}\n"
	mainV2 := "package main\n\nfunc Target() int {\n\treturn 99\n}\n\nfunc Target() int {\n\treturn 1\n}\n"
	mainV3 := "package main\n\nfunc Target() int {\n\treturn 99\n}\n\nfunc Target() int {\n\treturn 2\n}\n"

	writeRepoSource(t, dir, "main.go", mainV1)
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add(main.go v1): %v", err)
	}
	h1, err := r.Commit("initial target", "alice")
	if err != nil {
		t.Fatalf("Commit(initial target): %v", err)
	}

	writeRepoSource(t, dir, "main.go", mainV2)
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add(main.go v2): %v", err)
	}
	h2, err := r.Commit("shift key only", "bob")
	if err != nil {
		t.Fatalf("Commit(shift key only): %v", err)
	}

	writeRepoSource(t, dir, "main.go", mainV3)
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add(main.go v3): %v", err)
	}
	h3, err := r.Commit("change tracked target", "carol")
	if err != nil {
		t.Fatalf("Commit(change tracked target): %v", err)
	}

	targetKey := declarationIdentityKeyByBody(t, "main.go", mainV3, "Target", "return 2")

	head, err := r.ResolveRef("HEAD")
	if err != nil {
		t.Fatalf("ResolveRef(HEAD): %v", err)
	}

	entries, err := r.LogByEntity(head, 10, "main.go", targetKey)
	if err != nil {
		t.Fatalf("LogByEntity: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("LogByEntity returned %d entries, want 2", len(entries))
	}
	if entries[0].Hash != h3 {
		t.Fatalf("entries[0].Hash = %q, want %q", entries[0].Hash, h3)
	}
	if entries[1].Hash != h1 {
		t.Fatalf("entries[1].Hash = %q, want %q", entries[1].Hash, h1)
	}
	for _, entry := range entries {
		if entry.Hash == h2 {
			t.Fatalf("key-shift commit should not be treated as a body change")
		}
	}
}

func TestLogByEntity_PathFilterTracksMoveContinuity(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	oldPath := "old/main.go"
	newPath := "new/main.go"
	mainV1 := "package main\n\nfunc Target() int {\n\treturn 1\n}\n"
	mainV2 := "package main\n\nfunc Target() int {\n\treturn 2\n}\n"

	writeRepoSource(t, dir, oldPath, mainV1)
	if err := r.Add([]string{oldPath}); err != nil {
		t.Fatalf("Add(%s): %v", oldPath, err)
	}
	h1, err := r.Commit("add old path target", "alice")
	if err != nil {
		t.Fatalf("Commit(add old path target): %v", err)
	}

	oldAbs := filepath.Join(dir, filepath.FromSlash(oldPath))
	newAbs := filepath.Join(dir, filepath.FromSlash(newPath))
	if err := os.MkdirAll(filepath.Dir(newAbs), 0o755); err != nil {
		t.Fatalf("MkdirAll(new path): %v", err)
	}
	if err := os.Rename(oldAbs, newAbs); err != nil {
		t.Fatalf("Rename(%s -> %s): %v", oldPath, newPath, err)
	}
	if err := r.Add([]string{newPath}); err != nil {
		t.Fatalf("Add(%s): %v", newPath, err)
	}
	if err := r.Remove([]string{oldPath}, true); err != nil {
		t.Fatalf("Remove(%s): %v", oldPath, err)
	}
	h2, err := r.Commit("move target path", "bob")
	if err != nil {
		t.Fatalf("Commit(move target path): %v", err)
	}

	writeRepoSource(t, dir, newPath, mainV2)
	if err := r.Add([]string{newPath}); err != nil {
		t.Fatalf("Add(%s v2): %v", newPath, err)
	}
	h3, err := r.Commit("change moved target", "carol")
	if err != nil {
		t.Fatalf("Commit(change moved target): %v", err)
	}

	targetKey := declarationIdentityKeyByBody(t, newPath, mainV2, "Target", "return 2")

	head, err := r.ResolveRef("HEAD")
	if err != nil {
		t.Fatalf("ResolveRef(HEAD): %v", err)
	}

	entries, err := r.LogByEntity(head, 10, newPath, targetKey)
	if err != nil {
		t.Fatalf("LogByEntity: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("LogByEntity returned %d entries, want 2", len(entries))
	}
	if entries[0].Hash != h3 {
		t.Fatalf("entries[0].Hash = %q, want %q", entries[0].Hash, h3)
	}
	if entries[1].Hash != h1 {
		t.Fatalf("entries[1].Hash = %q, want %q", entries[1].Hash, h1)
	}
	for _, entry := range entries {
		if entry.Hash == h2 {
			t.Fatalf("move-only commit should not be treated as a body change")
		}
	}
}

func declarationIdentityKey(t *testing.T, path, source, name string) string {
	t.Helper()

	el, err := entity.Extract(path, []byte(source))
	if err != nil {
		t.Fatalf("entity.Extract(%q): %v", path, err)
	}
	for i := range el.Entities {
		e := el.Entities[i]
		if e.Kind == entity.KindDeclaration && e.Name == name {
			return e.IdentityKey()
		}
	}
	t.Fatalf("declaration %q not found in %q", name, path)
	return ""
}

func declarationIdentityKeyByBody(t *testing.T, path, source, name, bodyNeedle string) string {
	t.Helper()

	el, err := entity.Extract(path, []byte(source))
	if err != nil {
		t.Fatalf("entity.Extract(%q): %v", path, err)
	}
	for i := range el.Entities {
		e := el.Entities[i]
		if e.Kind != entity.KindDeclaration || e.Name != name {
			continue
		}
		if strings.Contains(string(e.Body), bodyNeedle) {
			return e.IdentityKey()
		}
	}
	t.Fatalf("declaration %q with body containing %q not found in %q", name, bodyNeedle, path)
	return ""
}

func writeRepoSource(t *testing.T, root, relPath, content string) {
	t.Helper()

	absPath := filepath.Join(root, relPath)
	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", relPath, err)
	}
	if err := os.WriteFile(absPath, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", relPath, err)
	}
}

// TestLogAll_ShowsCommitsFromMultipleBranches verifies that LogAll returns
// commits from all branches, not just HEAD.
func TestLogAll_ShowsCommitsFromMultipleBranches(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Create initial commit on main.
	writeRepoSource(t, dir, "main.go", "package main\n\nfunc Main() {}\n")
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add(main.go): %v", err)
	}
	h1, err := r.Commit("commit on main", "alice")
	if err != nil {
		t.Fatalf("Commit(main): %v", err)
	}

	// Create a feature branch from head.
	headHash, err := r.ResolveRef("HEAD")
	if err != nil {
		t.Fatalf("ResolveRef(HEAD): %v", err)
	}
	if err := r.CreateBranch("feature", headHash); err != nil {
		t.Fatalf("CreateBranch(feature): %v", err)
	}

	// Switch to feature branch and add a commit.
	if err := r.Checkout("feature"); err != nil {
		t.Fatalf("Checkout(feature): %v", err)
	}
	writeRepoSource(t, dir, "feature.go", "package main\n\nfunc Feature() {}\n")
	if err := r.Add([]string{"feature.go"}); err != nil {
		t.Fatalf("Add(feature.go): %v", err)
	}
	h2, err := r.Commit("commit on feature", "bob")
	if err != nil {
		t.Fatalf("Commit(feature): %v", err)
	}

	// Switch back to main and add another commit.
	if err := r.Checkout("main"); err != nil {
		t.Fatalf("Checkout(main): %v", err)
	}
	writeRepoSource(t, dir, "main2.go", "package main\n\nfunc Main2() {}\n")
	if err := r.Add([]string{"main2.go"}); err != nil {
		t.Fatalf("Add(main2.go): %v", err)
	}
	h3, err := r.Commit("second commit on main", "alice")
	if err != nil {
		t.Fatalf("Commit(main2): %v", err)
	}

	entries, err := r.LogAll(100)
	if err != nil {
		t.Fatalf("LogAll: %v", err)
	}

	// Should have 3 commits total: h1 (shared), h2 (feature), h3 (main).
	if len(entries) != 3 {
		t.Fatalf("LogAll returned %d entries, want 3", len(entries))
	}

	// Check all three hashes are present.
	foundHashes := make(map[object.Hash]bool)
	for _, e := range entries {
		foundHashes[e.Hash] = true
	}
	for _, h := range []object.Hash{h1, h2, h3} {
		if !foundHashes[h] {
			t.Errorf("LogAll missing commit %s", h)
		}
	}
}

// TestLogAll_DeduplicatesSharedCommits verifies that commits reachable from
// multiple branches are only returned once.
func TestLogAll_DeduplicatesSharedCommits(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Create initial commit on main.
	writeRepoSource(t, dir, "main.go", "package main\n\nfunc Main() {}\n")
	if err := r.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add(main.go): %v", err)
	}
	sharedHash, err := r.Commit("shared commit", "alice")
	if err != nil {
		t.Fatalf("Commit(shared): %v", err)
	}

	// Create a feature branch from the same commit.
	headHash, err := r.ResolveRef("HEAD")
	if err != nil {
		t.Fatalf("ResolveRef(HEAD): %v", err)
	}
	if err := r.CreateBranch("feature", headHash); err != nil {
		t.Fatalf("CreateBranch(feature): %v", err)
	}

	entries, err := r.LogAll(100)
	if err != nil {
		t.Fatalf("LogAll: %v", err)
	}

	// The shared commit should appear exactly once.
	if len(entries) != 1 {
		t.Fatalf("LogAll returned %d entries, want 1 (dedup)", len(entries))
	}
	if entries[0].Hash != sharedHash {
		t.Fatalf("entry hash = %q, want %q", entries[0].Hash, sharedHash)
	}
}

// TestLogAll_SortsByTimestamp verifies LogAll sorts commits newest first
// and returns all commits from the branch.
func TestLogAll_SortsByTimestamp(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Create commits sequentially on a single branch.
	writeRepoSource(t, dir, "a.go", "package main\n\nfunc A() {}\n")
	if err := r.Add([]string{"a.go"}); err != nil {
		t.Fatalf("Add(a.go): %v", err)
	}
	if _, err := r.Commit("first", "alice"); err != nil {
		t.Fatalf("Commit(first): %v", err)
	}

	writeRepoSource(t, dir, "b.go", "package main\n\nfunc B() {}\n")
	if err := r.Add([]string{"b.go"}); err != nil {
		t.Fatalf("Add(b.go): %v", err)
	}
	if _, err := r.Commit("second", "alice"); err != nil {
		t.Fatalf("Commit(second): %v", err)
	}

	writeRepoSource(t, dir, "c.go", "package main\n\nfunc C() {}\n")
	if err := r.Add([]string{"c.go"}); err != nil {
		t.Fatalf("Add(c.go): %v", err)
	}
	if _, err := r.Commit("third", "alice"); err != nil {
		t.Fatalf("Commit(third): %v", err)
	}

	entries, err := r.LogAll(100)
	if err != nil {
		t.Fatalf("LogAll: %v", err)
	}

	if len(entries) != 3 {
		t.Fatalf("LogAll returned %d entries, want 3", len(entries))
	}

	// Verify ordering: timestamps should be non-ascending (descending or equal).
	for i := 0; i < len(entries)-1; i++ {
		if entries[i].Commit.Timestamp < entries[i+1].Commit.Timestamp {
			t.Fatalf("entries[%d].Timestamp (%d) < entries[%d].Timestamp (%d); want non-ascending",
				i, entries[i].Commit.Timestamp, i+1, entries[i+1].Commit.Timestamp)
		}
	}

	// Verify all three messages are present (order may vary when timestamps match).
	messages := make(map[string]bool)
	for _, e := range entries {
		messages[e.Commit.Message] = true
	}
	for _, msg := range []string{"first", "second", "third"} {
		if !messages[msg] {
			t.Errorf("LogAll missing commit message %q", msg)
		}
	}
}
