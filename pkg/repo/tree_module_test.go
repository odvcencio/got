package repo

import (
	"testing"

	"github.com/odvcencio/graft/pkg/object"
)

func mustWriteBlob(t *testing.T, store *object.Store, content string) object.Hash {
	t.Helper()
	h, err := store.WriteBlob(&object.Blob{Data: []byte(content)})
	if err != nil {
		t.Fatalf("WriteBlob: %v", err)
	}
	return h
}

func TestBuildTree_ModuleEntry(t *testing.T) {
	r, err := Init(t.TempDir())
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	fileBlob := mustWriteBlob(t, r.Store, "hello world\n")
	moduleCommitHash := testTreeHash(42) // fake pinned commit hash

	stg := &Staging{
		Entries: map[string]*StagingEntry{
			"readme.txt": {
				Path:     "readme.txt",
				BlobHash: fileBlob,
				Mode:     object.TreeModeFile,
			},
			"vendor/ui-kit": {
				Path:     "vendor/ui-kit",
				BlobHash: object.Hash(moduleCommitHash),
				Mode:     object.TreeModeModule,
			},
		},
	}

	rootHash, err := r.BuildTree(stg)
	if err != nil {
		t.Fatalf("BuildTree: %v", err)
	}

	// Read root tree; should have "readme.txt" (file) and "vendor" (dir).
	rootTree, err := r.Store.ReadTree(rootHash)
	if err != nil {
		t.Fatalf("ReadTree root: %v", err)
	}

	var vendorEntry *object.TreeEntry
	for i := range rootTree.Entries {
		if rootTree.Entries[i].Name == "vendor" {
			vendorEntry = &rootTree.Entries[i]
		}
	}
	if vendorEntry == nil {
		t.Fatal("expected 'vendor' subtree entry in root tree")
	}
	if !vendorEntry.IsDir {
		t.Fatal("expected 'vendor' to be a directory")
	}

	// Read vendor subtree; should contain "ui-kit" with mode 160000.
	vendorTree, err := r.Store.ReadTree(vendorEntry.SubtreeHash)
	if err != nil {
		t.Fatalf("ReadTree vendor: %v", err)
	}

	if len(vendorTree.Entries) != 1 {
		t.Fatalf("vendor tree has %d entries, want 1", len(vendorTree.Entries))
	}

	modEntry := vendorTree.Entries[0]
	if modEntry.Name != "ui-kit" {
		t.Fatalf("module entry name = %q, want %q", modEntry.Name, "ui-kit")
	}
	if modEntry.Mode != object.TreeModeModule {
		t.Fatalf("module entry mode = %q, want %q", modEntry.Mode, object.TreeModeModule)
	}
	if modEntry.IsDir {
		t.Fatal("module entry should not be a directory")
	}
	if modEntry.BlobHash != moduleCommitHash {
		t.Fatalf("module BlobHash = %q, want %q", modEntry.BlobHash, moduleCommitHash)
	}
}

func TestFlattenTree_SkipsModuleEntries(t *testing.T) {
	r, err := Init(t.TempDir())
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	fileBlob := mustWriteBlob(t, r.Store, "content\n")
	moduleCommitHash := testTreeHash(99)

	rootHash, err := r.Store.WriteTree(&object.TreeObj{
		Entries: []object.TreeEntry{
			{
				Name:     "main.go",
				IsDir:    false,
				Mode:     object.TreeModeFile,
				BlobHash: fileBlob,
			},
			{
				Name:     "libs/parser",
				IsDir:    false,
				Mode:     object.TreeModeModule,
				BlobHash: moduleCommitHash,
			},
		},
	})
	if err != nil {
		t.Fatalf("WriteTree: %v", err)
	}

	entries, err := r.FlattenTree(rootHash)
	if err != nil {
		t.Fatalf("FlattenTree: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("FlattenTree returned %d entries, want 1", len(entries))
	}
	if entries[0].Path != "main.go" {
		t.Fatalf("entry path = %q, want %q", entries[0].Path, "main.go")
	}
	if entries[0].BlobHash != fileBlob {
		t.Fatalf("entry BlobHash = %q, want %q", entries[0].BlobHash, fileBlob)
	}
}

func TestFlattenTreeWithModules(t *testing.T) {
	r, err := Init(t.TempDir())
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	fileBlob := mustWriteBlob(t, r.Store, "package main\n")
	moduleCommitHash := testTreeHash(77)

	// Build a tree with a subdirectory containing both a file and a module.
	subTreeHash, err := r.Store.WriteTree(&object.TreeObj{
		Entries: []object.TreeEntry{
			{
				Name:     "helper.go",
				IsDir:    false,
				Mode:     object.TreeModeFile,
				BlobHash: fileBlob,
			},
			{
				Name:     "ext-lib",
				IsDir:    false,
				Mode:     object.TreeModeModule,
				BlobHash: moduleCommitHash,
			},
		},
	})
	if err != nil {
		t.Fatalf("WriteTree sub: %v", err)
	}

	rootHash, err := r.Store.WriteTree(&object.TreeObj{
		Entries: []object.TreeEntry{
			{
				Name:     "main.go",
				IsDir:    false,
				Mode:     object.TreeModeFile,
				BlobHash: fileBlob,
			},
			{
				Name:        "pkg",
				IsDir:       true,
				Mode:        object.TreeModeDir,
				SubtreeHash: subTreeHash,
			},
		},
	})
	if err != nil {
		t.Fatalf("WriteTree root: %v", err)
	}

	files, modules, err := r.FlattenTreeWithModules(rootHash)
	if err != nil {
		t.Fatalf("FlattenTreeWithModules: %v", err)
	}

	// Expect 2 files: main.go and pkg/helper.go.
	if len(files) != 2 {
		t.Fatalf("files count = %d, want 2", len(files))
	}
	filePaths := map[string]bool{}
	for _, f := range files {
		filePaths[f.Path] = true
	}
	if !filePaths["main.go"] {
		t.Fatal("missing file entry: main.go")
	}
	if !filePaths["pkg/helper.go"] {
		t.Fatal("missing file entry: pkg/helper.go")
	}

	// Expect 1 module: pkg/ext-lib.
	if len(modules) != 1 {
		t.Fatalf("modules count = %d, want 1", len(modules))
	}
	if modules[0].Path != "pkg/ext-lib" {
		t.Fatalf("module path = %q, want %q", modules[0].Path, "pkg/ext-lib")
	}
	if modules[0].BlobHash != moduleCommitHash {
		t.Fatalf("module BlobHash = %q, want %q", modules[0].BlobHash, moduleCommitHash)
	}
}
