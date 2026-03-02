package repo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/odvcencio/graft/pkg/object"
)

func TestModuleSync_WorktreeIsolation(t *testing.T) {
	// Create main repo with a module.
	mainDir := t.TempDir()
	main, err := Init(mainDir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Write blob + tree + commit for the module content.
	blobHash, err := main.Store.WriteBlob(&object.Blob{Data: []byte("module content\n")})
	if err != nil {
		t.Fatalf("WriteBlob: %v", err)
	}

	treeHash, err := main.Store.WriteTree(&object.TreeObj{
		Entries: []object.TreeEntry{
			{Name: "lib.go", Mode: object.TreeModeFile, BlobHash: blobHash},
		},
	})
	if err != nil {
		t.Fatalf("WriteTree: %v", err)
	}

	commitHash, err := main.Store.WriteCommit(&object.CommitObj{
		TreeHash:  treeHash,
		Author:    "test",
		Message:   "init",
		Timestamp: 1000,
	})
	if err != nil {
		t.Fatalf("WriteCommit: %v", err)
	}

	// Add module entry and lock it.
	err = main.AddModuleEntry(ModuleEntry{
		Name:  "mylib",
		URL:   "github:myorg/mylib",
		Path:  "vendor/mylib",
		Track: "main",
	})
	if err != nil {
		t.Fatalf("AddModuleEntry: %v", err)
	}

	err = main.UpdateModuleLock("mylib", commitHash, "https://github.com/myorg/mylib.git")
	if err != nil {
		t.Fatalf("UpdateModuleLock: %v", err)
	}

	// Sync modules in main repo.
	if err := main.ModuleSync(); err != nil {
		t.Fatalf("ModuleSync main: %v", err)
	}

	// Module should be materialized in the main repo's working tree.
	mainModFile := filepath.Join(mainDir, "vendor", "mylib", "lib.go")
	if _, err := os.Stat(mainModFile); err != nil {
		t.Errorf("module file should exist in main worktree: %v", err)
	}

	// Verify module metadata is under the main repo's GraftDir.
	metaDir := main.ModuleMetadataDir("mylib")
	if _, err := os.Stat(metaDir); err != nil {
		t.Errorf("module metadata should exist: %v", err)
	}

	// Verify metadata is inside GraftDir (which is worktree-specific for
	// linked worktrees).
	if !strings.HasPrefix(metaDir, main.GraftDir) {
		t.Errorf("metadata dir should be under GraftDir: %s not in %s", metaDir, main.GraftDir)
	}

	// Verify file content is correct.
	data, err := os.ReadFile(mainModFile)
	if err != nil {
		t.Fatalf("read module file: %v", err)
	}
	if string(data) != "module content\n" {
		t.Errorf("module file content = %q, want %q", string(data), "module content\n")
	}

	// Verify HEAD file in metadata contains the locked commit hash.
	headData, err := os.ReadFile(filepath.Join(metaDir, "HEAD"))
	if err != nil {
		t.Fatalf("read module HEAD: %v", err)
	}
	if got := string(headData); got != string(commitHash)+"\n" {
		t.Errorf("module HEAD = %q, want %q", got, string(commitHash)+"\n")
	}

	// Verify .graft symlink resolves to the metadata dir.
	symlinkPath := filepath.Join(mainDir, "vendor", "mylib", ".graft")
	resolved, err := filepath.EvalSymlinks(symlinkPath)
	if err != nil {
		t.Fatalf("eval symlink: %v", err)
	}
	absMetaDir, _ := filepath.Abs(metaDir)
	absResolved, _ := filepath.Abs(resolved)
	if absResolved != absMetaDir {
		t.Errorf("symlink resolved to %q, want %q", absResolved, absMetaDir)
	}
}
