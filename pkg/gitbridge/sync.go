package gitbridge

import (
	"fmt"
	"os/exec"
	"strings"
)

// GitHEAD returns the current git HEAD commit hash.
func (b *Bridge) GitHEAD() (string, error) {
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = b.rootDir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse HEAD: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// GitRefsChanged returns true if git HEAD has moved since lastKnownHead.
func (b *Bridge) GitRefsChanged(lastKnownHead string) (bool, error) {
	current, err := b.GitHEAD()
	if err != nil {
		return false, err
	}
	return current != lastKnownHead, nil
}

// SyncFromGit imports any new git commits since lastKnownHead.
// Returns the new HEAD hash.
func (b *Bridge) SyncFromGit(lastKnownHead string) (string, error) {
	changed, err := b.GitRefsChanged(lastKnownHead)
	if err != nil {
		return lastKnownHead, err
	}
	if !changed {
		return lastKnownHead, nil
	}

	// Re-import HEAD snapshot (Phase 1: full reimport;
	// Phase 2 will do incremental diff-based import)
	if err := b.importHEAD(); err != nil {
		return lastKnownHead, fmt.Errorf("sync from git: %w", err)
	}

	return b.GitHEAD()
}
