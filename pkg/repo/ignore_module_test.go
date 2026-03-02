package repo

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIgnore_ModulePaths(t *testing.T) {
	dir := t.TempDir()

	// Write a .graftmodules file with two modules.
	modulesContent := `[module "ui-kit"]
	url = https://example.com/ui-kit.git
	path = vendor/ui-kit
	track = main

[module "core-lib"]
	url = https://example.com/core-lib.git
	path = libs/core
	pin = v1.0.0
`
	if err := os.WriteFile(filepath.Join(dir, ".graftmodules"), []byte(modulesContent), 0o644); err != nil {
		t.Fatalf("write .graftmodules: %v", err)
	}

	ic := NewIgnoreChecker(dir)

	// Files inside module paths should be ignored.
	if !ic.IsIgnored("vendor/ui-kit/README.md") {
		t.Error("expected vendor/ui-kit/README.md to be ignored")
	}
	if !ic.IsIgnored("vendor/ui-kit/src/main.go") {
		t.Error("expected vendor/ui-kit/src/main.go to be ignored")
	}
	if !ic.IsIgnored("libs/core/lib.go") {
		t.Error("expected libs/core/lib.go to be ignored")
	}
	if !ic.IsIgnored("libs/core/internal/util.go") {
		t.Error("expected libs/core/internal/util.go to be ignored")
	}

	// The module directory itself should be ignored.
	if !ic.IsIgnored("vendor/ui-kit") {
		t.Error("expected vendor/ui-kit to be ignored")
	}
	if !ic.IsIgnored("libs/core") {
		t.Error("expected libs/core to be ignored")
	}

	// Files outside module paths should NOT be ignored.
	if ic.IsIgnored("vendor/other/file.go") {
		t.Error("expected vendor/other/file.go to NOT be ignored")
	}
	if ic.IsIgnored("src/main.go") {
		t.Error("expected src/main.go to NOT be ignored")
	}
	if ic.IsIgnored("libs/other.go") {
		t.Error("expected libs/other.go to NOT be ignored")
	}
	if ic.IsIgnored("README.md") {
		t.Error("expected README.md to NOT be ignored")
	}
}

func TestIgnore_ModulePaths_NoModulesFile(t *testing.T) {
	dir := t.TempDir()

	// No .graftmodules file exists — should not cause errors.
	ic := NewIgnoreChecker(dir)

	// Hardcoded patterns still work.
	if !ic.IsIgnored(".graft/HEAD") {
		t.Error("expected .graft/HEAD to be ignored")
	}

	// Regular files are not ignored.
	if ic.IsIgnored("main.go") {
		t.Error("expected main.go to NOT be ignored")
	}
}

func TestIgnore_ModulePaths_WithGraftignore(t *testing.T) {
	dir := t.TempDir()

	// Both .graftignore and .graftmodules present.
	writeGotignore(t, dir, "*.log\n")

	modulesContent := `[module "deps"]
	url = https://example.com/deps.git
	path = third_party/deps
	track = main
`
	if err := os.WriteFile(filepath.Join(dir, ".graftmodules"), []byte(modulesContent), 0o644); err != nil {
		t.Fatalf("write .graftmodules: %v", err)
	}

	ic := NewIgnoreChecker(dir)

	// .graftignore patterns still work.
	if !ic.IsIgnored("debug.log") {
		t.Error("expected debug.log to be ignored by .graftignore")
	}

	// Module paths are ignored.
	if !ic.IsIgnored("third_party/deps/lib.go") {
		t.Error("expected third_party/deps/lib.go to be ignored")
	}

	// Non-module, non-ignored files are fine.
	if ic.IsIgnored("src/app.go") {
		t.Error("expected src/app.go to NOT be ignored")
	}
}

func TestIgnore_ModulePaths_SimplePathNoSlash(t *testing.T) {
	dir := t.TempDir()

	// Module with a simple path (no slash).
	modulesContent := `[module "ext"]
	url = https://example.com/ext.git
	path = external
	track = main
`
	if err := os.WriteFile(filepath.Join(dir, ".graftmodules"), []byte(modulesContent), 0o644); err != nil {
		t.Fatalf("write .graftmodules: %v", err)
	}

	ic := NewIgnoreChecker(dir)

	if !ic.IsIgnored("external/README.md") {
		t.Error("expected external/README.md to be ignored")
	}
	if !ic.IsIgnored("external/sub/file.go") {
		t.Error("expected external/sub/file.go to be ignored")
	}
	if !ic.IsIgnored("external") {
		t.Error("expected external to be ignored")
	}
	if ic.IsIgnored("external-other/file.go") {
		t.Error("expected external-other/file.go to NOT be ignored")
	}
}
