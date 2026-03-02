package userconfig

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissingReturnsEmptyConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg == nil {
		t.Fatalf("Load returned nil config")
	}
	if cfg.Version <= 0 {
		t.Fatalf("Version = %d, want > 0", cfg.Version)
	}
	if cfg.Token != "" || cfg.OrchardURL != "" || cfg.Username != "" || cfg.Owner != "" {
		t.Fatalf("expected empty defaults, got %+v", cfg)
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	in := &Config{
		OrchardURL: "https://code.example.com",
		Token:     "abc123",
		Username:  "draco",
		Owner:     "draco",
	}
	if err := Save(in); err != nil {
		t.Fatalf("Save: %v", err)
	}

	cfgPath := filepath.Join(home, ".graftconfig")
	info, err := os.Stat(cfgPath)
	if err != nil {
		t.Fatalf("stat config: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("config mode = %o, want 600", got)
	}

	out, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if out.OrchardURL != in.OrchardURL {
		t.Fatalf("OrchardURL = %q, want %q", out.OrchardURL, in.OrchardURL)
	}
	if out.Token != in.Token {
		t.Fatalf("Token = %q, want %q", out.Token, in.Token)
	}
	if out.Username != in.Username {
		t.Fatalf("Username = %q, want %q", out.Username, in.Username)
	}
	if out.Owner != in.Owner {
		t.Fatalf("Owner = %q, want %q", out.Owner, in.Owner)
	}
	if out.Version <= 0 {
		t.Fatalf("Version = %d, want > 0", out.Version)
	}
}
