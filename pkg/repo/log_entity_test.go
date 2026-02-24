package repo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/odvcencio/got/pkg/entity"
	"github.com/odvcencio/got/pkg/object"
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
