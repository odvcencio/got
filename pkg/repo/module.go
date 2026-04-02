package repo

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/odvcencio/graft/pkg/object"
)

// Module combines the declared configuration of a submodule (from
// .graftmodules) with its resolved lock state (from .graftmodules.lock).
type Module struct {
	ModuleEntry             // embedded config entry
	Commit      object.Hash // resolved commit from lock file
	ResolvedURL string      // canonicalized URL from lock file
}

// ModuleMetadataDir returns the per-module metadata directory inside .graft.
// The returned path is .graft/modules/<name>.
func (r *Repo) ModuleMetadataDir(name string) string {
	return filepath.Join(r.GraftDir, "modules", name)
}

// ListModules reads .graftmodules and joins each entry with the corresponding
// lock state from .graftmodules.lock. If .graftmodules does not exist, it
// returns nil, nil.
func (r *Repo) ListModules() ([]Module, error) {
	entries, err := r.ReadGraftModulesFile()
	if err != nil {
		return nil, err
	}
	if entries == nil {
		return nil, nil
	}

	lock, err := r.ReadModuleLock()
	if err != nil {
		return nil, err
	}

	modules := make([]Module, len(entries))
	for i, e := range entries {
		m := Module{ModuleEntry: e}
		if lock != nil {
			if le, ok := lock.Modules[e.Name]; ok {
				m.Commit = le.Commit
				m.ResolvedURL = le.URL
			}
		}
		modules[i] = m
	}
	return modules, nil
}

// GetModule returns the module with the given name, combining config and lock
// state. It returns an error if no module with that name is declared.
func (r *Repo) GetModule(name string) (*Module, error) {
	modules, err := r.ListModules()
	if err != nil {
		return nil, err
	}
	for i := range modules {
		if modules[i].Name == name {
			return &modules[i], nil
		}
	}
	return nil, fmt.Errorf("module %q not found", name)
}

// AddModuleEntry appends a new module declaration to .graftmodules. It
// validates that no existing module shares the same name or path, and creates
// the per-module metadata directory at .graft/modules/<name>/refs/.
func (r *Repo) AddModuleEntry(entry ModuleEntry) error {
	entries, err := r.ReadGraftModulesFile()
	if err != nil {
		return err
	}

	for _, e := range entries {
		if e.Name == entry.Name {
			return fmt.Errorf("add module: duplicate name %q", entry.Name)
		}
		if e.Path == entry.Path {
			return fmt.Errorf("add module: duplicate path %q", entry.Path)
		}
	}

	entries = append(entries, entry)
	if err := r.WriteGraftModulesFile(entries); err != nil {
		return err
	}

	// Create module metadata directory with refs/ subdirectory.
	refsDir := filepath.Join(r.ModuleMetadataDir(entry.Name), "refs")
	if err := os.MkdirAll(refsDir, 0o755); err != nil {
		return fmt.Errorf("add module: mkdir metadata: %w", err)
	}

	return nil
}

// RemoveModuleEntry removes a module by name from .graftmodules and
// .graftmodules.lock, and deletes its metadata directory. It returns an error
// if no module with that name exists.
func (r *Repo) RemoveModuleEntry(name string) error {
	entries, err := r.ReadGraftModulesFile()
	if err != nil {
		return err
	}

	found := false
	filtered := entries[:0]
	for _, e := range entries {
		if e.Name == name {
			found = true
			continue
		}
		filtered = append(filtered, e)
	}
	if !found {
		return fmt.Errorf("remove module: %q not found", name)
	}

	if err := r.WriteGraftModulesFile(filtered); err != nil {
		return err
	}

	// Remove from lock file if present.
	lock, err := r.ReadModuleLock()
	if err != nil {
		return err
	}
	if lock != nil {
		delete(lock.Modules, name)
		if err := r.WriteModuleLock(lock); err != nil {
			return err
		}
	}

	// Remove metadata directory.
	metaDir := r.ModuleMetadataDir(name)
	if err := os.RemoveAll(metaDir); err != nil {
		return fmt.Errorf("remove module: remove metadata dir: %w", err)
	}

	return nil
}

// UpdateModuleLock updates (or creates) the lock file entry for a single
// module. It reads the current lock state, sets the entry with the given
// commit and resolved URL, and copies the track/pin values from the module
// config.
func (r *Repo) UpdateModuleLock(name string, commit object.Hash, resolvedURL string) error {
	// Read module config to get track/pin.
	entries, err := r.ReadGraftModulesFile()
	if err != nil {
		return err
	}
	var entry *ModuleEntry
	for i := range entries {
		if entries[i].Name == name {
			entry = &entries[i]
			break
		}
	}
	if entry == nil {
		return fmt.Errorf("update module lock: module %q not found in .graftmodules", name)
	}

	lock, err := r.ReadModuleLock()
	if err != nil {
		return err
	}
	if lock == nil {
		lock = &ModuleLock{Modules: make(map[string]ModuleLockEntry)}
	}

	lock.Modules[name] = ModuleLockEntry{
		Commit: commit,
		URL:    resolvedURL,
		Track:  entry.Track,
		Pin:    entry.Pin,
	}

	return r.WriteModuleLock(lock)
}
