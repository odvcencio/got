package repo

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/odvcencio/graft/pkg/object"
)

// TestModule_OpenFromModuleDir verifies that Open() correctly detects a module
// working tree (via the .graft symlink) and configures the Repo so that:
//   - RootDir is the module working tree directory
//   - Blobs from the parent's shared object store are readable
func TestModule_OpenFromModuleDir(t *testing.T) {
	// 1. Init parent repo.
	parentDir := t.TempDir()
	parent, err := Init(parentDir)
	if err != nil {
		t.Fatalf("Init parent: %v", err)
	}

	// 2. Write a blob, tree, and commit into the parent store.
	commitHash, _, blobHash := writeTestCommitWithBlob(
		t, parent, "lib.txt", []byte("library content\n"), nil,
	)

	// 3. Add module, lock it, and sync to materialize the working tree.
	err = parent.AddModuleEntry(ModuleEntry{
		Name:  "mylib",
		URL:   "https://example.com/mylib.git",
		Path:  "vendor/mylib",
		Track: "main",
	})
	if err != nil {
		t.Fatalf("AddModuleEntry: %v", err)
	}
	err = parent.UpdateModuleLock("mylib", commitHash, "https://example.com/mylib.git")
	if err != nil {
		t.Fatalf("UpdateModuleLock: %v", err)
	}
	if err := parent.ModuleSync(); err != nil {
		t.Fatalf("ModuleSync: %v", err)
	}

	// 4. Open from the module directory.
	moduleDir := filepath.Join(parentDir, "vendor", "mylib")
	modRepo, err := Open(moduleDir)
	if err != nil {
		t.Fatalf("Open(moduleDir): %v", err)
	}

	// 5. Verify RootDir is the module working tree root.
	if modRepo.RootDir != moduleDir {
		t.Errorf("RootDir = %q, want %q", modRepo.RootDir, moduleDir)
	}

	// 6. Verify GraftDir is the module metadata dir.
	wantGraftDir := parent.ModuleMetadataDir("mylib")
	// Clean both for comparison.
	gotGraftDir := filepath.Clean(modRepo.GraftDir)
	wantGraftDir = filepath.Clean(wantGraftDir)
	if gotGraftDir != wantGraftDir {
		t.Errorf("GraftDir = %q, want %q", gotGraftDir, wantGraftDir)
	}

	// 7. Verify CommonDir is the parent's .graft/ directory.
	wantCommonDir := filepath.Clean(parent.GraftDir)
	gotCommonDir := filepath.Clean(modRepo.CommonDir)
	if gotCommonDir != wantCommonDir {
		t.Errorf("CommonDir = %q, want %q", gotCommonDir, wantCommonDir)
	}

	// 8. Verify we can read blobs from the parent's shared store.
	blob, err := modRepo.Store.ReadBlob(blobHash)
	if err != nil {
		t.Fatalf("ReadBlob from module repo: %v", err)
	}
	if string(blob.Data) != "library content\n" {
		t.Errorf("blob data = %q, want %q", string(blob.Data), "library content\n")
	}
}

// TestModule_CommitFromModuleDir verifies that a commit made from within a
// module working tree:
//   - Writes the new commit into the parent's shared object store
//   - Updates the module's HEAD file
func TestModule_CommitFromModuleDir(t *testing.T) {
	// 1. Init parent repo and create initial module content.
	parentDir := t.TempDir()
	parent, err := Init(parentDir)
	if err != nil {
		t.Fatalf("Init parent: %v", err)
	}

	commitHash, _, _ := writeTestCommitWithBlob(
		t, parent, "lib.txt", []byte("v1\n"), nil,
	)

	err = parent.AddModuleEntry(ModuleEntry{
		Name:  "mylib",
		URL:   "https://example.com/mylib.git",
		Path:  "vendor/mylib",
		Track: "main",
	})
	if err != nil {
		t.Fatalf("AddModuleEntry: %v", err)
	}
	err = parent.UpdateModuleLock("mylib", commitHash, "https://example.com/mylib.git")
	if err != nil {
		t.Fatalf("UpdateModuleLock: %v", err)
	}
	if err := parent.ModuleSync(); err != nil {
		t.Fatalf("ModuleSync: %v", err)
	}

	// 2. Open the repo from the module directory.
	moduleDir := filepath.Join(parentDir, "vendor", "mylib")
	modRepo, err := Open(moduleDir)
	if err != nil {
		t.Fatalf("Open(moduleDir): %v", err)
	}

	// 3. Modify a file in the module working tree.
	libPath := filepath.Join(moduleDir, "lib.txt")
	if err := os.WriteFile(libPath, []byte("v2\n"), 0o644); err != nil {
		t.Fatalf("write modified lib.txt: %v", err)
	}

	// 4. Add and commit from the module repo.
	if err := modRepo.Add([]string{"lib.txt"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	newHash, err := modRepo.Commit("update lib to v2", "test-author")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// 5. Verify the new commit is readable from the parent's shared store.
	c, err := parent.Store.ReadCommit(newHash)
	if err != nil {
		t.Fatalf("ReadCommit from parent store: %v", err)
	}
	if c.Message != "update lib to v2" {
		t.Errorf("commit message = %q, want %q", c.Message, "update lib to v2")
	}

	// 6. Verify the module HEAD was updated to the new commit.
	headData, err := os.ReadFile(filepath.Join(modRepo.GraftDir, "HEAD"))
	if err != nil {
		t.Fatalf("read module HEAD: %v", err)
	}
	headHash := object.Hash(headData[:len(headData)-1]) // strip trailing newline
	if headHash != newHash {
		t.Errorf("module HEAD = %q, want %q", headHash, newHash)
	}

	// 7. Verify the new commit has the sync commit as its parent.
	if len(c.Parents) != 1 {
		t.Fatalf("expected 1 parent, got %d", len(c.Parents))
	}
	if c.Parents[0] != commitHash {
		t.Errorf("parent = %q, want %q", c.Parents[0], commitHash)
	}
}
