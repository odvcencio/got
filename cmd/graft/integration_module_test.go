package main

import (
	"strings"
	"testing"
)

func TestIntegration_ModuleListEmpty(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dir := initRepo(t)

	out := mustRunGraft(t, dir, "module", "list")
	if !strings.Contains(out, "no modules") {
		t.Errorf("expected 'no modules' output, got: %s", out)
	}
}

func TestIntegration_ModuleAddAndList(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dir := initRepo(t)

	out := mustRunGraft(t, dir, "module", "add", "github:myorg/ui-kit", "vendor/ui-kit", "--track", "main")
	if !strings.Contains(out, "added module") {
		t.Errorf("expected 'added module' output, got: %s", out)
	}
	if !strings.Contains(out, "ui-kit") {
		t.Errorf("expected module name in output, got: %s", out)
	}

	listOut := mustRunGraft(t, dir, "module", "list")
	if !strings.Contains(listOut, "ui-kit") {
		t.Errorf("expected 'ui-kit' in list output, got: %s", listOut)
	}
	if !strings.Contains(listOut, "vendor/ui-kit") {
		t.Errorf("expected 'vendor/ui-kit' path in list output, got: %s", listOut)
	}
}

func TestIntegration_ModuleAddDuplicate(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dir := initRepo(t)

	// Add two different modules — both should succeed.
	mustRunGraft(t, dir, "module", "add", "github:myorg/ui-kit", "vendor/ui-kit", "--track", "main")
	mustRunGraft(t, dir, "module", "add", "github:myorg/core-lib", "vendor/core-lib", "--track", "main")

	listOut := mustRunGraft(t, dir, "module", "list")
	if !strings.Contains(listOut, "ui-kit") {
		t.Errorf("expected 'ui-kit' in list output, got: %s", listOut)
	}
	if !strings.Contains(listOut, "core-lib") {
		t.Errorf("expected 'core-lib' in list output, got: %s", listOut)
	}
}

func TestIntegration_ModuleRemove(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dir := initRepo(t)

	mustRunGraft(t, dir, "module", "add", "github:myorg/ui-kit", "vendor/ui-kit", "--track", "main")

	out := mustRunGraft(t, dir, "module", "rm", "ui-kit")
	if !strings.Contains(out, "removed module") {
		t.Errorf("expected 'removed module' output, got: %s", out)
	}

	listOut := mustRunGraft(t, dir, "module", "list")
	if !strings.Contains(listOut, "no modules") {
		t.Errorf("expected 'no modules' after removal, got: %s", listOut)
	}
}

func TestIntegration_ModuleRemoveNotFound(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dir := initRepo(t)

	_, err := runGraft(t, dir, "module", "rm", "nonexistent")
	if err == nil {
		t.Error("expected error when removing nonexistent module, got nil")
	}
}

func TestIntegration_ModuleStatus(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dir := initRepo(t)

	mustRunGraft(t, dir, "module", "add", "github:myorg/ui-kit", "vendor/ui-kit", "--track", "main")

	out := mustRunGraft(t, dir, "module", "status")
	if !strings.Contains(out, "ui-kit") {
		t.Errorf("expected 'ui-kit' in status output, got: %s", out)
	}
	if !strings.Contains(out, "not locked") {
		t.Errorf("expected 'not locked' in status output, got: %s", out)
	}
}

func TestIntegration_ModuleSync(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dir := initRepo(t)

	out := mustRunGraft(t, dir, "module", "sync")
	if !strings.Contains(out, "synced") {
		t.Errorf("expected 'synced' in output, got: %s", out)
	}
}

func TestIntegration_ModuleTrackAndPinExclusive(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dir := initRepo(t)

	_, err := runGraft(t, dir, "module", "add", "github:myorg/ui-kit", "vendor/ui-kit", "--track", "main", "--pin", "v1.0")
	if err == nil {
		t.Error("expected error when both --track and --pin are specified, got nil")
	}
}
