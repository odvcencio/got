package repo

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// sequencer manages state for interruptible operations (cherry-pick, revert, rebase).
// State is stored in a named subdirectory of .graft/ with atomic file writes.
type sequencer struct {
	dir string // absolute path to the state directory
}

// newSequencer creates a sequencer for the given state directory.
func newSequencer(dir string) *sequencer {
	return &sequencer{dir: dir}
}

// IsActive returns true if the sequencer state directory exists.
func (s *sequencer) IsActive() bool {
	_, err := os.Stat(s.dir)
	return err == nil
}

// Init creates the state directory.
func (s *sequencer) Init() error {
	return os.MkdirAll(s.dir, 0o755)
}

// Clean removes the state directory and all contents.
func (s *sequencer) Clean() error {
	return os.RemoveAll(s.dir)
}

// Dir returns the absolute path to the state directory.
func (s *sequencer) Dir() string {
	return s.dir
}

// ReadFile reads a named file from the state directory, returning trimmed content.
func (s *sequencer) ReadFile(name string) (string, error) {
	data, err := os.ReadFile(filepath.Join(s.dir, name))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// WriteFile atomically writes a named file to the state directory
// using temp file + fsync + rename.
func (s *sequencer) WriteFile(name, content string) error {
	target := filepath.Join(s.dir, name)

	tmp, err := os.CreateTemp(s.dir, name+".tmp.*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()

	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return os.Rename(tmpPath, target)
}

// WriteFiles atomically writes multiple named files.
func (s *sequencer) WriteFiles(files map[string]string) error {
	for name, content := range files {
		if err := s.WriteFile(name, content); err != nil {
			return fmt.Errorf("write %q: %w", name, err)
		}
	}
	return nil
}

// RemoveFile removes a named file from the state directory.
func (s *sequencer) RemoveFile(name string) {
	os.Remove(filepath.Join(s.dir, name))
}

// Sequencer accessors on Repo.

func (r *Repo) cherryPickSeq() *sequencer {
	return newSequencer(filepath.Join(r.GraftDir, "cherry-pick"))
}

func (r *Repo) revertSeq() *sequencer {
	return newSequencer(filepath.Join(r.GraftDir, "revert"))
}

func (r *Repo) rebaseSeq() *sequencer {
	return newSequencer(filepath.Join(r.GraftDir, "rebase-merge"))
}
