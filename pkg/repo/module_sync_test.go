package repo

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/odvcencio/graft/pkg/object"
)

// writeTestCommitWithBlob creates a blob, a single-file tree, and a commit in the
// repo's object store. It returns (commitHash, treeHash, blobHash).
func writeTestCommitWithBlob(t *testing.T, r *Repo, filename string, content []byte, parents []object.Hash) (object.Hash, object.Hash, object.Hash) {
	t.Helper()

	blobHash, err := r.Store.WriteBlob(&object.Blob{Data: content})
	if err != nil {
		t.Fatalf("WriteBlob: %v", err)
	}

	treeHash, err := r.Store.WriteTree(&object.TreeObj{
		Entries: []object.TreeEntry{
			{
				Name:     filename,
				IsDir:    false,
				Mode:     object.TreeModeFile,
				BlobHash: blobHash,
			},
		},
	})
	if err != nil {
		t.Fatalf("WriteTree: %v", err)
	}

	commitHash, err := r.Store.WriteCommit(&object.CommitObj{
		TreeHash:  treeHash,
		Parents:   parents,
		Author:    "test",
		Timestamp: 1000000,
		Message:   "test commit",
	})
	if err != nil {
		t.Fatalf("WriteCommit: %v", err)
	}

	return commitHash, treeHash, blobHash
}

// writeTestCommitMultiFile creates a commit with multiple files in the tree.
func writeTestCommitMultiFile(t *testing.T, r *Repo, files map[string][]byte, parents []object.Hash) object.Hash {
	t.Helper()

	entries := make([]object.TreeEntry, 0, len(files))
	for name, content := range files {
		blobHash, err := r.Store.WriteBlob(&object.Blob{Data: content})
		if err != nil {
			t.Fatalf("WriteBlob(%s): %v", name, err)
		}
		entries = append(entries, object.TreeEntry{
			Name:     name,
			IsDir:    false,
			Mode:     object.TreeModeFile,
			BlobHash: blobHash,
		})
	}

	treeHash, err := r.Store.WriteTree(&object.TreeObj{Entries: entries})
	if err != nil {
		t.Fatalf("WriteTree: %v", err)
	}

	commitHash, err := r.Store.WriteCommit(&object.CommitObj{
		TreeHash:  treeHash,
		Parents:   parents,
		Author:    "test",
		Timestamp: 1000000,
		Message:   "test commit",
	})
	if err != nil {
		t.Fatalf("WriteCommit: %v", err)
	}

	return commitHash
}

func TestModuleSync_CheckoutAtCommit(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Create a commit with a single file in the object store.
	commitHash, _, _ := writeTestCommitWithBlob(t, r, "hello.txt", []byte("hello world\n"), nil)

	// Register a module pointing to a path and lock it.
	err = r.AddModuleEntry(ModuleEntry{
		Name:  "mylib",
		URL:   "https://example.com/mylib.git",
		Path:  "vendor/mylib",
		Track: "main",
	})
	if err != nil {
		t.Fatalf("AddModuleEntry: %v", err)
	}

	err = r.UpdateModuleLock("mylib", commitHash, "https://example.com/mylib.git")
	if err != nil {
		t.Fatalf("UpdateModuleLock: %v", err)
	}

	// Run ModuleSync.
	if err := r.ModuleSync(); err != nil {
		t.Fatalf("ModuleSync: %v", err)
	}

	// Verify: module file exists with correct content.
	filePath := filepath.Join(dir, "vendor", "mylib", "hello.txt")
	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("read module file: %v", err)
	}
	if string(data) != "hello world\n" {
		t.Errorf("module file content = %q, want %q", string(data), "hello world\n")
	}

	// Verify: .graft symlink points to correct metadata dir.
	symlinkPath := filepath.Join(dir, "vendor", "mylib", ".graft")
	target, err := os.Readlink(symlinkPath)
	if err != nil {
		t.Fatalf("readlink .graft: %v", err)
	}

	// The symlink should be a relative path that resolves to the metadata dir.
	metaDir := r.ModuleMetadataDir("mylib")
	moduleDir := filepath.Join(dir, "vendor", "mylib")
	expectedRel, err := filepath.Rel(moduleDir, metaDir)
	if err != nil {
		t.Fatalf("compute expected relative path: %v", err)
	}
	if target != expectedRel {
		t.Errorf(".graft symlink target = %q, want %q", target, expectedRel)
	}

	// Verify the symlink actually resolves to the metadata dir.
	resolved, err := filepath.EvalSymlinks(symlinkPath)
	if err != nil {
		t.Fatalf("eval symlink: %v", err)
	}
	absMetaDir, _ := filepath.Abs(metaDir)
	absResolved, _ := filepath.Abs(resolved)
	if absResolved != absMetaDir {
		t.Errorf("symlink resolved to %q, want %q", absResolved, absMetaDir)
	}

	// Verify: HEAD file in metadata dir contains commit hash.
	headPath := filepath.Join(metaDir, "HEAD")
	headData, err := os.ReadFile(headPath)
	if err != nil {
		t.Fatalf("read module HEAD: %v", err)
	}
	if got := string(headData); got != string(commitHash)+"\n" {
		t.Errorf("module HEAD = %q, want %q", got, string(commitHash)+"\n")
	}
}

func TestModuleSync_NoModules(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// ModuleSync on repo with no .graftmodules should succeed silently.
	if err := r.ModuleSync(); err != nil {
		t.Fatalf("ModuleSync: %v", err)
	}
}

func TestModuleSync_MissingLockEntry(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Add a module entry but do NOT create a lock entry.
	err = r.AddModuleEntry(ModuleEntry{
		Name:  "unlocked",
		URL:   "https://example.com/unlocked.git",
		Path:  "vendor/unlocked",
		Track: "main",
	})
	if err != nil {
		t.Fatalf("AddModuleEntry: %v", err)
	}

	// Write an empty lock file so it exists but has no entry for "unlocked".
	err = r.WriteModuleLock(&ModuleLock{
		Modules: map[string]ModuleLockEntry{},
	})
	if err != nil {
		t.Fatalf("WriteModuleLock: %v", err)
	}

	// ModuleSync should succeed without error — missing lock entry is skipped.
	if err := r.ModuleSync(); err != nil {
		t.Fatalf("ModuleSync: %v", err)
	}

	// The module directory should not have been created.
	moduleDir := filepath.Join(dir, "vendor", "unlocked")
	if _, err := os.Stat(moduleDir); !os.IsNotExist(err) {
		t.Errorf("module dir should not exist, got stat err = %v", err)
	}
}

func TestModuleSync_CleansPreviousCheckout(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Create v1 commit with file "a.txt".
	v1Hash := writeTestCommitMultiFile(t, r, map[string][]byte{
		"a.txt": []byte("version 1\n"),
	}, nil)

	// Create v2 commit with file "b.txt" (no a.txt).
	v2Hash := writeTestCommitMultiFile(t, r, map[string][]byte{
		"b.txt": []byte("version 2\n"),
	}, []object.Hash{v1Hash})

	// Register module.
	err = r.AddModuleEntry(ModuleEntry{
		Name:  "lib",
		URL:   "https://example.com/lib.git",
		Path:  "deps/lib",
		Track: "main",
	})
	if err != nil {
		t.Fatalf("AddModuleEntry: %v", err)
	}

	// Lock to v1 and sync.
	err = r.UpdateModuleLock("lib", v1Hash, "https://example.com/lib.git")
	if err != nil {
		t.Fatalf("UpdateModuleLock v1: %v", err)
	}
	if err := r.ModuleSync(); err != nil {
		t.Fatalf("ModuleSync v1: %v", err)
	}

	// Verify v1 file exists.
	aPath := filepath.Join(dir, "deps", "lib", "a.txt")
	if _, err := os.Stat(aPath); err != nil {
		t.Fatalf("v1 a.txt should exist: %v", err)
	}

	// Update lock to v2 and sync again.
	err = r.UpdateModuleLock("lib", v2Hash, "https://example.com/lib.git")
	if err != nil {
		t.Fatalf("UpdateModuleLock v2: %v", err)
	}
	if err := r.ModuleSync(); err != nil {
		t.Fatalf("ModuleSync v2: %v", err)
	}

	// Verify v2 content: b.txt exists with correct content.
	bPath := filepath.Join(dir, "deps", "lib", "b.txt")
	data, err := os.ReadFile(bPath)
	if err != nil {
		t.Fatalf("read b.txt: %v", err)
	}
	if string(data) != "version 2\n" {
		t.Errorf("b.txt = %q, want %q", string(data), "version 2\n")
	}

	// Verify v1 file was cleaned: a.txt should no longer exist.
	if _, err := os.Stat(aPath); !os.IsNotExist(err) {
		t.Errorf("v1 a.txt should be removed after syncing to v2, stat err = %v", err)
	}

	// Verify .graft symlink still exists and points correctly.
	symlinkPath := filepath.Join(dir, "deps", "lib", ".graft")
	if _, err := os.Lstat(symlinkPath); err != nil {
		t.Errorf(".graft symlink should still exist: %v", err)
	}

	// Verify HEAD was updated to v2.
	metaDir := r.ModuleMetadataDir("lib")
	headData, err := os.ReadFile(filepath.Join(metaDir, "HEAD"))
	if err != nil {
		t.Fatalf("read module HEAD: %v", err)
	}
	if got := string(headData); got != string(v2Hash)+"\n" {
		t.Errorf("module HEAD = %q, want %q", got, string(v2Hash)+"\n")
	}
}

func TestModuleSyncForCheckout_OnlyUpdatesChanged(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Create two commits for two different modules.
	commitA, _, _ := writeTestCommitWithBlob(t, r, "a.txt", []byte("alpha\n"), nil)
	commitB1, _, _ := writeTestCommitWithBlob(t, r, "b.txt", []byte("beta v1\n"), nil)
	commitB2, _, _ := writeTestCommitWithBlob(t, r, "b.txt", []byte("beta v2\n"), nil)

	// Register two modules.
	for _, entry := range []ModuleEntry{
		{Name: "moda", URL: "https://example.com/a.git", Path: "mods/a", Track: "main"},
		{Name: "modb", URL: "https://example.com/b.git", Path: "mods/b", Track: "main"},
	} {
		if err := r.AddModuleEntry(entry); err != nil {
			t.Fatalf("AddModuleEntry(%s): %v", entry.Name, err)
		}
	}

	oldLock := &ModuleLock{
		Modules: map[string]ModuleLockEntry{
			"moda": {Commit: commitA, URL: "https://example.com/a.git"},
			"modb": {Commit: commitB1, URL: "https://example.com/b.git"},
		},
	}
	newLock := &ModuleLock{
		Modules: map[string]ModuleLockEntry{
			"moda": {Commit: commitA, URL: "https://example.com/a.git"},  // unchanged
			"modb": {Commit: commitB2, URL: "https://example.com/b.git"}, // changed
		},
	}

	// First, do a full sync so both modules exist.
	if err := r.WriteModuleLock(oldLock); err != nil {
		t.Fatalf("WriteModuleLock: %v", err)
	}
	if err := r.ModuleSync(); err != nil {
		t.Fatalf("initial ModuleSync: %v", err)
	}

	// Place a marker file in moda to detect if it gets re-synced.
	markerPath := filepath.Join(dir, "mods", "a", ".marker")
	if err := os.WriteFile(markerPath, []byte("marker"), 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}

	// Now do an optimized sync for checkout.
	if err := r.ModuleSyncForCheckout(oldLock, newLock); err != nil {
		t.Fatalf("ModuleSyncForCheckout: %v", err)
	}

	// moda should NOT have been re-synced, so the marker should still exist.
	if _, err := os.Stat(markerPath); err != nil {
		t.Errorf("moda marker should still exist (module was unchanged): %v", err)
	}

	// modb should have been updated to v2.
	bData, err := os.ReadFile(filepath.Join(dir, "mods", "b", "b.txt"))
	if err != nil {
		t.Fatalf("read modb b.txt: %v", err)
	}
	if string(bData) != "beta v2\n" {
		t.Errorf("modb b.txt = %q, want %q", string(bData), "beta v2\n")
	}
}

func TestModuleSyncForCheckout_NilOldLock(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	commitHash, _, _ := writeTestCommitWithBlob(t, r, "file.txt", []byte("content\n"), nil)

	err = r.AddModuleEntry(ModuleEntry{
		Name:  "mod",
		URL:   "https://example.com/mod.git",
		Path:  "vendor/mod",
		Track: "main",
	})
	if err != nil {
		t.Fatalf("AddModuleEntry: %v", err)
	}

	newLock := &ModuleLock{
		Modules: map[string]ModuleLockEntry{
			"mod": {Commit: commitHash, URL: "https://example.com/mod.git"},
		},
	}

	// With nil oldLock, all modules should be synced.
	if err := r.ModuleSyncForCheckout(nil, newLock); err != nil {
		t.Fatalf("ModuleSyncForCheckout: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "vendor", "mod", "file.txt"))
	if err != nil {
		t.Fatalf("read file.txt: %v", err)
	}
	if string(data) != "content\n" {
		t.Errorf("file.txt = %q, want %q", string(data), "content\n")
	}
}

func TestCleanModuleDir_PreservesGraftSymlink(t *testing.T) {
	dir := t.TempDir()
	moduleDir := filepath.Join(dir, "mymod")
	if err := os.MkdirAll(moduleDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// Create some files and a .graft symlink.
	if err := os.WriteFile(filepath.Join(moduleDir, "file1.txt"), []byte("data"), 0o644); err != nil {
		t.Fatalf("write file1: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(moduleDir, "subdir"), 0o755); err != nil {
		t.Fatalf("mkdir subdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(moduleDir, "subdir", "file2.txt"), []byte("data"), 0o644); err != nil {
		t.Fatalf("write file2: %v", err)
	}
	if err := os.Symlink("/tmp/fake-meta", filepath.Join(moduleDir, ".graft")); err != nil {
		t.Fatalf("create .graft symlink: %v", err)
	}

	// Clean the directory.
	if err := cleanModuleDir(moduleDir); err != nil {
		t.Fatalf("cleanModuleDir: %v", err)
	}

	// .graft symlink should still exist.
	if _, err := os.Lstat(filepath.Join(moduleDir, ".graft")); err != nil {
		t.Errorf(".graft symlink should be preserved: %v", err)
	}

	// Other files and subdirs should be removed.
	if _, err := os.Stat(filepath.Join(moduleDir, "file1.txt")); !os.IsNotExist(err) {
		t.Errorf("file1.txt should be removed")
	}
	if _, err := os.Stat(filepath.Join(moduleDir, "subdir")); !os.IsNotExist(err) {
		t.Errorf("subdir should be removed")
	}
}

func TestCleanModuleDir_NonexistentDir(t *testing.T) {
	// Cleaning a directory that doesn't exist should not error.
	if err := cleanModuleDir("/tmp/does-not-exist-at-all-" + t.Name()); err != nil {
		t.Fatalf("cleanModuleDir on nonexistent dir: %v", err)
	}
}
