package repo

import (
	"os"
	"path/filepath"
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
