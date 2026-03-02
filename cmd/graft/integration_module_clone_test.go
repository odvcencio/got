package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestIntegration_CloneWithModules verifies that cloning a repo that contains
// a .graftmodules file results in the file being present in the clone.
func TestIntegration_CloneWithModules(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Create a source repo with a .graftmodules file.
	src := initRepo(t)
	modulesContent := "[module \"libs/core\"]\n\tpath = libs/core\n\turl = https://example.com/core.git\n"
	commitFile(t, src, ".graftmodules", modulesContent, "add graftmodules")

	// Clone locally into a new directory.
	cloneDir := filepath.Join(t.TempDir(), "cloned")
	out := mustRunGraft(t, t.TempDir(), "clone", src, cloneDir)

	// The clone output should mention the clone destination.
	if !strings.Contains(out, "cloned") {
		t.Errorf("expected clone output to contain 'cloned', got: %s", out)
	}

	// Verify .graftmodules exists in the clone.
	gmPath := filepath.Join(cloneDir, ".graftmodules")
	data, err := os.ReadFile(gmPath)
	if err != nil {
		t.Fatalf("expected .graftmodules to exist in clone: %v", err)
	}
	if string(data) != modulesContent {
		t.Errorf("unexpected .graftmodules content:\ngot:  %q\nwant: %q", string(data), modulesContent)
	}
}

// TestIntegration_CloneNoModules verifies that cloning with --no-modules
// skips the module sync step (no "modules synced" in output).
func TestIntegration_CloneNoModules(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Create a source repo with a .graftmodules file.
	src := initRepo(t)
	modulesContent := "[module \"libs/core\"]\n\tpath = libs/core\n\turl = https://example.com/core.git\n"
	commitFile(t, src, ".graftmodules", modulesContent, "add graftmodules")

	// Clone locally with --no-modules.
	cloneDir := filepath.Join(t.TempDir(), "cloned-no-mod")
	out := mustRunGraft(t, t.TempDir(), "clone", "--no-modules", src, cloneDir)

	// Should NOT contain "modules synced" since we skipped module sync.
	if strings.Contains(out, "modules synced") {
		t.Errorf("expected no 'modules synced' in output with --no-modules, got: %s", out)
	}

	// The .graftmodules file should still be present (it's a regular
	// committed file, just module sync was skipped).
	gmPath := filepath.Join(cloneDir, ".graftmodules")
	if _, err := os.Stat(gmPath); err != nil {
		t.Fatalf("expected .graftmodules to exist in clone even with --no-modules: %v", err)
	}
}
