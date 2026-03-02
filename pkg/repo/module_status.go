package repo

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/odvcencio/graft/pkg/object"
)

// ModuleStatusEntry describes the current state of a single module in the
// working tree, combining configuration, lock, and checkout information.
type ModuleStatusEntry struct {
	Name         string
	Path         string
	Track        string
	Pin          string
	LockedCommit object.Hash // commit from lock file
	HeadCommit   object.Hash // what's actually checked out (from module metadata HEAD file)
	Synced       bool        // HeadCommit matches LockedCommit
}

// ModuleStatus returns the status of every declared module by combining the
// config entries, lock file state, and on-disk HEAD for each module.
func (r *Repo) ModuleStatus() ([]ModuleStatusEntry, error) {
	modules, err := r.ListModules()
	if err != nil {
		return nil, err
	}
	if modules == nil {
		return nil, nil
	}

	entries := make([]ModuleStatusEntry, len(modules))
	for i, m := range modules {
		e := ModuleStatusEntry{
			Name:         m.Name,
			Path:         m.Path,
			Track:        m.Track,
			Pin:          m.Pin,
			LockedCommit: m.Commit,
		}

		// Read the HEAD file from the module metadata directory.
		headPath := filepath.Join(r.ModuleMetadataDir(m.Name), "HEAD")
		data, err := os.ReadFile(headPath)
		if err == nil {
			e.HeadCommit = object.Hash(strings.TrimSpace(string(data)))
		}
		// If the file doesn't exist, HeadCommit stays empty — that's fine.

		e.Synced = e.LockedCommit != "" && e.HeadCommit == e.LockedCommit

		entries[i] = e
	}

	return entries, nil
}
