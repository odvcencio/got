package repo

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/odvcencio/graft/pkg/object"
)

// ModuleLockEntry records the resolved state of a single submodule.
type ModuleLockEntry struct {
	Commit object.Hash `json:"commit"`
	URL    string      `json:"url"`
	Track  string      `json:"track,omitempty"`
	Pin    string      `json:"pin,omitempty"`
}

// ModuleLock holds the resolved commit hashes for all submodules,
// persisted as .graftmodules.lock in the repository root.
type ModuleLock struct {
	Modules map[string]ModuleLockEntry `json:"modules"`
}

func (r *Repo) moduleLockPath() string {
	return filepath.Join(r.RootDir, ".graftmodules.lock")
}

// ReadModuleLock reads .graftmodules.lock from the repository root.
// If the file does not exist, it returns nil, nil.
func (r *Repo) ReadModuleLock() (*ModuleLock, error) {
	data, err := os.ReadFile(r.moduleLockPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read modules lock: %w", err)
	}
	var lock ModuleLock
	if err := json.Unmarshal(data, &lock); err != nil {
		return nil, fmt.Errorf("read modules lock: unmarshal: %w", err)
	}
	if lock.Modules == nil {
		lock.Modules = make(map[string]ModuleLockEntry)
	}
	return &lock, nil
}

// WriteModuleLock atomically writes .graftmodules.lock to the repository root.
func (r *Repo) WriteModuleLock(lock *ModuleLock) error {
	if lock == nil {
		lock = &ModuleLock{}
	}
	if lock.Modules == nil {
		lock.Modules = make(map[string]ModuleLockEntry)
	}
	data, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		return fmt.Errorf("write modules lock: marshal: %w", err)
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(r.RootDir, ".graftmodules-lock-tmp-*")
	if err != nil {
		return fmt.Errorf("write modules lock: tmpfile: %w", err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("write modules lock: write: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("write modules lock: sync: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("write modules lock: close: %w", err)
	}
	if err := os.Rename(tmpName, r.moduleLockPath()); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("write modules lock: rename: %w", err)
	}
	return nil
}
