package repo

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveAuthor_RepoConfig(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatal(err)
	}

	// Set repo-level user config.
	cfg := &Config{
		Remotes: make(map[string]string),
		User: &UserConfig{
			Name:  "Alice",
			Email: "alice@example.com",
		},
	}
	if err := r.WriteConfig(cfg); err != nil {
		t.Fatal(err)
	}

	author := r.ResolveAuthor()
	if author != "Alice <alice@example.com>" {
		t.Fatalf("ResolveAuthor = %q, want %q", author, "Alice <alice@example.com>")
	}
}

func TestResolveAuthor_RepoConfigNameOnly(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatal(err)
	}

	cfg := &Config{
		Remotes: make(map[string]string),
		User: &UserConfig{
			Name: "Bob",
		},
	}
	if err := r.WriteConfig(cfg); err != nil {
		t.Fatal(err)
	}

	author := r.ResolveAuthor()
	if author != "Bob" {
		t.Fatalf("ResolveAuthor = %q, want %q", author, "Bob")
	}
}

func TestResolveAuthor_FallbackToUserConfig(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatal(err)
	}

	// No repo-level user config. Write a global config to a temp location.
	// We can't easily test the real ~/.graftconfig, but we can verify
	// the fallback to $USER works.
	t.Setenv("USER", "charlie")

	// Ensure no repo-level user config is set.
	cfg := &Config{Remotes: make(map[string]string)}
	if err := r.WriteConfig(cfg); err != nil {
		t.Fatal(err)
	}

	author := r.ResolveAuthor()
	if author != "charlie" {
		t.Fatalf("ResolveAuthor = %q, want %q", author, "charlie")
	}
}

func TestResolveAuthor_FallbackToUnknown(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatal(err)
	}

	t.Setenv("USER", "")

	// Ensure no repo-level user config.
	cfg := &Config{Remotes: make(map[string]string)}
	if err := r.WriteConfig(cfg); err != nil {
		t.Fatal(err)
	}

	// Override HOME so userconfig.Load doesn't find a real ~/.graftconfig.
	t.Setenv("HOME", dir)

	author := r.ResolveAuthor()
	if author != "unknown" {
		t.Fatalf("ResolveAuthor = %q, want %q", author, "unknown")
	}
}

func TestResolveAuthor_UserConfigFallback(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatal(err)
	}

	// No repo-level user config.
	cfg := &Config{Remotes: make(map[string]string)}
	if err := r.WriteConfig(cfg); err != nil {
		t.Fatal(err)
	}

	// Write a fake user config to a temp HOME.
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	userCfg := map[string]interface{}{
		"version": 1,
		"name":    "Diana",
		"email":   "diana@example.com",
	}
	data, _ := json.Marshal(userCfg)
	if err := os.WriteFile(filepath.Join(fakeHome, ".graftconfig"), data, 0o600); err != nil {
		t.Fatal(err)
	}

	author := r.ResolveAuthor()
	if author != "Diana <diana@example.com>" {
		t.Fatalf("ResolveAuthor = %q, want %q", author, "Diana <diana@example.com>")
	}
}

func TestResolveAuthor_RepoOverridesUserConfig(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatal(err)
	}

	// Set repo-level user config.
	repoCfg := &Config{
		Remotes: make(map[string]string),
		User: &UserConfig{
			Name:  "RepoUser",
			Email: "repo@example.com",
		},
	}
	if err := r.WriteConfig(repoCfg); err != nil {
		t.Fatal(err)
	}

	// Write a fake global user config.
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	userCfg := map[string]interface{}{
		"version": 1,
		"name":    "GlobalUser",
		"email":   "global@example.com",
	}
	data, _ := json.Marshal(userCfg)
	if err := os.WriteFile(filepath.Join(fakeHome, ".graftconfig"), data, 0o600); err != nil {
		t.Fatal(err)
	}

	author := r.ResolveAuthor()
	if author != "RepoUser <repo@example.com>" {
		t.Fatalf("ResolveAuthor = %q, want %q", author, "RepoUser <repo@example.com>")
	}
}

func TestFormatAuthor(t *testing.T) {
	tests := []struct {
		name, email, want string
	}{
		{"Alice", "alice@example.com", "Alice <alice@example.com>"},
		{"Bob", "", "Bob"},
		{"", "nope@example.com", ""},
		{"", "", ""},
		{"  Charlie  ", "  charlie@x.com  ", "Charlie <charlie@x.com>"},
	}
	for _, tt := range tests {
		got := formatAuthor(tt.name, tt.email)
		if got != tt.want {
			t.Errorf("formatAuthor(%q, %q) = %q, want %q", tt.name, tt.email, got, tt.want)
		}
	}
}
