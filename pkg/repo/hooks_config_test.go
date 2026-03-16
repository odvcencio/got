package repo

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadHooksConfig_NoFile(t *testing.T) {
	dir := t.TempDir()
	cfg, err := LoadHooksConfig(dir, nil)
	if err != nil {
		t.Fatalf("LoadHooksConfig: %v", err)
	}
	if len(cfg.Hooks) != 0 {
		t.Errorf("expected 0 hooks, got %d", len(cfg.Hooks))
	}
}

func TestLoadHooksConfig_RepoOnly(t *testing.T) {
	dir := t.TempDir()
	tomlContent := `
[pre-commit.lint]
run = "golangci-lint run"
timeout = "30s"

[pre-push.tests]
run = "go test ./..."
timeout = "120s"
`
	if err := os.WriteFile(filepath.Join(dir, "hooks.toml"), []byte(tomlContent), 0o644); err != nil {
		t.Fatalf("write hooks.toml: %v", err)
	}

	cfg, err := LoadHooksConfig(dir, nil)
	if err != nil {
		t.Fatalf("LoadHooksConfig: %v", err)
	}
	if len(cfg.Hooks) != 2 {
		t.Fatalf("expected 2 hooks, got %d", len(cfg.Hooks))
	}

	// Sorted by point then name: pre-commit.lint, pre-push.tests.
	if cfg.Hooks[0].Point != "pre-commit" || cfg.Hooks[0].Name != "lint" {
		t.Errorf("hooks[0] = %s.%s, want pre-commit.lint", cfg.Hooks[0].Point, cfg.Hooks[0].Name)
	}
	if cfg.Hooks[0].Run != "golangci-lint run" {
		t.Errorf("hooks[0].Run = %q, want %q", cfg.Hooks[0].Run, "golangci-lint run")
	}
	if cfg.Hooks[0].Source != "repo" {
		t.Errorf("hooks[0].Source = %q, want %q", cfg.Hooks[0].Source, "repo")
	}
	if cfg.Hooks[1].Point != "pre-push" || cfg.Hooks[1].Name != "tests" {
		t.Errorf("hooks[1] = %s.%s, want pre-push.tests", cfg.Hooks[1].Point, cfg.Hooks[1].Name)
	}
}

func TestLoadHooksConfig_UserExtendsRepo(t *testing.T) {
	dir := t.TempDir()
	tomlContent := `
[pre-commit.lint]
run = "golangci-lint run"
`
	if err := os.WriteFile(filepath.Join(dir, "hooks.toml"), []byte(tomlContent), 0o644); err != nil {
		t.Fatalf("write hooks.toml: %v", err)
	}

	userHooks := map[string]map[string]HookEntry{
		"pre-commit": {
			"format": {Run: "gofmt -w ."},
		},
		"post-commit": {
			"notify": {Run: "echo done"},
		},
	}

	cfg, err := LoadHooksConfig(dir, userHooks)
	if err != nil {
		t.Fatalf("LoadHooksConfig: %v", err)
	}
	if len(cfg.Hooks) != 3 {
		t.Fatalf("expected 3 hooks, got %d", len(cfg.Hooks))
	}

	// Sorted: post-commit.notify, pre-commit.format, pre-commit.lint.
	if cfg.Hooks[0].Point != "post-commit" || cfg.Hooks[0].Name != "notify" {
		t.Errorf("hooks[0] = %s.%s, want post-commit.notify", cfg.Hooks[0].Point, cfg.Hooks[0].Name)
	}
	if cfg.Hooks[0].Source != "user" {
		t.Errorf("hooks[0].Source = %q, want %q", cfg.Hooks[0].Source, "user")
	}
	if cfg.Hooks[1].Point != "pre-commit" || cfg.Hooks[1].Name != "format" {
		t.Errorf("hooks[1] = %s.%s, want pre-commit.format", cfg.Hooks[1].Point, cfg.Hooks[1].Name)
	}
	if cfg.Hooks[2].Point != "pre-commit" || cfg.Hooks[2].Name != "lint" {
		t.Errorf("hooks[2] = %s.%s, want pre-commit.lint", cfg.Hooks[2].Point, cfg.Hooks[2].Name)
	}
}

func TestLoadHooksConfig_UserCannotOverrideRepo(t *testing.T) {
	dir := t.TempDir()
	tomlContent := `
[pre-commit.lint]
run = "golangci-lint run"
timeout = "30s"
`
	if err := os.WriteFile(filepath.Join(dir, "hooks.toml"), []byte(tomlContent), 0o644); err != nil {
		t.Fatalf("write hooks.toml: %v", err)
	}

	// User tries to override repo's pre-commit.lint with a different command.
	userHooks := map[string]map[string]HookEntry{
		"pre-commit": {
			"lint": {Run: "echo 'bypassed!'"},
		},
	}

	cfg, err := LoadHooksConfig(dir, userHooks)
	if err != nil {
		t.Fatalf("LoadHooksConfig: %v", err)
	}
	if len(cfg.Hooks) != 1 {
		t.Fatalf("expected 1 hook (user duplicate dropped), got %d", len(cfg.Hooks))
	}
	if cfg.Hooks[0].Run != "golangci-lint run" {
		t.Errorf("Run = %q, want %q (repo version should win)", cfg.Hooks[0].Run, "golangci-lint run")
	}
}

func TestLoadHooksConfig_TimeoutPreserved(t *testing.T) {
	dir := t.TempDir()
	tomlContent := `
[pre-commit.slow]
run = "sleep 1"
timeout = "5s"
`
	if err := os.WriteFile(filepath.Join(dir, "hooks.toml"), []byte(tomlContent), 0o644); err != nil {
		t.Fatalf("write hooks.toml: %v", err)
	}

	cfg, err := LoadHooksConfig(dir, nil)
	if err != nil {
		t.Fatalf("LoadHooksConfig: %v", err)
	}
	if len(cfg.Hooks) != 1 {
		t.Fatalf("expected 1 hook, got %d", len(cfg.Hooks))
	}
	if cfg.Hooks[0].Timeout != "5s" {
		t.Errorf("Timeout = %q, want %q", cfg.Hooks[0].Timeout, "5s")
	}
}

func TestForPoint(t *testing.T) {
	cfg := &HooksConfig{
		Hooks: []HookEntry{
			{Point: "pre-commit", Name: "lint"},
			{Point: "pre-commit", Name: "format"},
			{Point: "pre-push", Name: "tests"},
		},
	}

	pre := cfg.ForPoint("pre-commit")
	if len(pre) != 2 {
		t.Fatalf("ForPoint(pre-commit) len = %d, want 2", len(pre))
	}

	push := cfg.ForPoint("pre-push")
	if len(push) != 1 {
		t.Fatalf("ForPoint(pre-push) len = %d, want 1", len(push))
	}

	none := cfg.ForPoint("post-merge")
	if len(none) != 0 {
		t.Fatalf("ForPoint(post-merge) len = %d, want 0", len(none))
	}
}
