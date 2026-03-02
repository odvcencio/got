package repo

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/odvcencio/graft/pkg/object"
)

// ModuleSync reads .graftmodules and .graftmodules.lock, then materializes
// each locked module's working tree at the path declared in .graftmodules.
// Modules that appear in the config but have no lock entry are silently
// skipped. If neither file exists, ModuleSync returns nil.
func (r *Repo) ModuleSync() error {
	entries, err := r.ReadGraftModulesFile()
	if err != nil {
		return fmt.Errorf("module sync: %w", err)
	}
	if len(entries) == 0 {
		return nil
	}

	lock, err := r.ReadModuleLock()
	if err != nil {
		return fmt.Errorf("module sync: %w", err)
	}
	if lock == nil {
		// No lock file at all — nothing to sync.
		return nil
	}

	for _, entry := range entries {
		le, ok := lock.Modules[entry.Name]
		if !ok {
			// Module declared but not yet locked — skip silently.
			continue
		}
		if err := r.syncModule(entry, le); err != nil {
			return fmt.Errorf("module sync: %s: %w", entry.Name, err)
		}
	}
	return nil
}

// syncModule materializes a single module's working tree from its locked
// commit.
//
//  1. Read the commit object to obtain the tree hash.
//  2. Flatten the tree into a list of file entries.
//  3. Clean the existing module directory (preserving a .graft symlink).
//  4. Create the module directory and write all files from the tree.
//  5. Create a .graft symlink pointing to the module metadata directory.
//  6. Write a HEAD file inside the metadata directory with the commit hash.
func (r *Repo) syncModule(entry ModuleEntry, lockEntry ModuleLockEntry) error {
	commit, err := r.Store.ReadCommit(lockEntry.Commit)
	if err != nil {
		return fmt.Errorf("read commit %s: %w", lockEntry.Commit, err)
	}

	files, err := r.FlattenTree(commit.TreeHash)
	if err != nil {
		return fmt.Errorf("flatten tree: %w", err)
	}

	moduleDir := filepath.Join(r.RootDir, filepath.FromSlash(entry.Path))

	// Clean previous checkout, preserving .graft symlink.
	if err := cleanModuleDir(moduleDir); err != nil {
		return fmt.Errorf("clean module dir: %w", err)
	}

	// Ensure the module directory exists.
	if err := os.MkdirAll(moduleDir, 0o755); err != nil {
		return fmt.Errorf("mkdir module dir: %w", err)
	}

	// Write all files from the tree.
	for _, f := range files {
		absPath := filepath.Join(moduleDir, filepath.FromSlash(f.Path))

		dir := filepath.Dir(absPath)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("mkdir %q: %w", dir, err)
		}

		blob, err := r.Store.ReadBlob(f.BlobHash)
		if err != nil {
			return fmt.Errorf("read blob for %q: %w", f.Path, err)
		}

		if err := os.WriteFile(absPath, blob.Data, filePermFromMode(f.Mode)); err != nil {
			return fmt.Errorf("write %q: %w", f.Path, err)
		}
	}

	// Create .graft symlink pointing to module metadata dir.
	metaDir := r.ModuleMetadataDir(entry.Name)
	if err := os.MkdirAll(metaDir, 0o755); err != nil {
		return fmt.Errorf("mkdir metadata dir: %w", err)
	}

	symlinkPath := filepath.Join(moduleDir, ".graft")
	// Remove any existing symlink (cleanModuleDir preserves it, but we
	// recreate to ensure correctness).
	os.Remove(symlinkPath)

	// Compute a relative path from the module directory to the metadata dir.
	relPath, err := filepath.Rel(moduleDir, metaDir)
	if err != nil {
		return fmt.Errorf("compute relative path: %w", err)
	}
	if err := os.Symlink(relPath, symlinkPath); err != nil {
		return fmt.Errorf("create .graft symlink: %w", err)
	}

	// Write HEAD file inside the metadata directory.
	headPath := filepath.Join(metaDir, "HEAD")
	if err := os.WriteFile(headPath, []byte(string(lockEntry.Commit)+"\n"), 0o644); err != nil {
		return fmt.Errorf("write module HEAD: %w", err)
	}

	return nil
}

// cleanModuleDir removes all contents of dir except the .graft symlink.
// If the directory does not exist, cleanModuleDir returns nil.
func cleanModuleDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	for _, e := range entries {
		if e.Name() == ".graft" {
			continue
		}
		p := filepath.Join(dir, e.Name())
		if err := os.RemoveAll(p); err != nil {
			return fmt.Errorf("remove %q: %w", p, err)
		}
	}
	return nil
}

// ModuleSyncForCheckout performs an optimized module sync that only updates
// modules whose lock entries have changed between oldLock and newLock.
// If oldLock is nil, all modules in newLock are synced. If newLock is nil,
// no modules are synced (existing module dirs are left as-is).
func (r *Repo) ModuleSyncForCheckout(oldLock, newLock *ModuleLock) error {
	if newLock == nil || len(newLock.Modules) == 0 {
		return nil
	}

	entries, err := r.ReadGraftModulesFile()
	if err != nil {
		return fmt.Errorf("module sync for checkout: %w", err)
	}
	if len(entries) == 0 {
		return nil
	}

	// Build a lookup from module name to config entry.
	entryMap := make(map[string]ModuleEntry, len(entries))
	for _, e := range entries {
		entryMap[e.Name] = e
	}

	for name, newLE := range newLock.Modules {
		entry, ok := entryMap[name]
		if !ok {
			// Module in lock but not in config — skip.
			continue
		}

		// Check if this module actually changed.
		if oldLock != nil {
			if oldLE, exists := oldLock.Modules[name]; exists {
				if oldLE.Commit == newLE.Commit {
					continue // unchanged, skip
				}
			}
		}

		if err := r.syncModule(entry, newLE); err != nil {
			return fmt.Errorf("module sync for checkout: %s: %w", name, err)
		}
	}

	// Remove modules that were in the old lock but not in the new lock.
	if oldLock != nil {
		for name := range oldLock.Modules {
			if _, stillPresent := newLock.Modules[name]; stillPresent {
				continue
			}
			entry, ok := entryMap[name]
			if !ok {
				continue
			}
			moduleDir := filepath.Join(r.RootDir, filepath.FromSlash(entry.Path))
			if err := os.RemoveAll(moduleDir); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("module sync for checkout: remove %s: %w", name, err)
			}
		}
	}

	return nil
}

// moduleHEADPath returns the path to the HEAD file inside a module's
// metadata directory. This is a convenience for reading/writing the
// currently checked-out commit of a module.
func (r *Repo) moduleHEADPath(name string) string {
	return filepath.Join(r.ModuleMetadataDir(name), "HEAD")
}

// ReadModuleHEAD reads the commit hash stored in a module's HEAD file.
// Returns an empty hash and nil error if the file does not exist.
func (r *Repo) ReadModuleHEAD(name string) (object.Hash, error) {
	data, err := os.ReadFile(r.moduleHEADPath(name))
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("read module HEAD for %q: %w", name, err)
	}
	return object.Hash(strings.TrimSpace(string(data))), nil
}
