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
		Token:      "abc123",
		Username:   "draco",
		Owner:      "draco",
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

	profile := out.OrchardProfile(in.OrchardURL)
	if profile.Token != in.Token {
		t.Fatalf("OrchardProfile(%q).Token = %q, want %q", in.OrchardURL, profile.Token, in.Token)
	}
	if profile.Username != in.Username {
		t.Fatalf("OrchardProfile(%q).Username = %q, want %q", in.OrchardURL, profile.Username, in.Username)
	}
	if profile.Owner != in.Owner {
		t.Fatalf("OrchardProfile(%q).Owner = %q, want %q", in.OrchardURL, profile.Owner, in.Owner)
	}
}

func TestDefaultOrchardURLFallsBackToSingleProfile(t *testing.T) {
	cfg := &Config{
		OrchardProfiles: map[string]OrchardProfile{
			"https://Code.Example.com/": {
				Token:    "abc123",
				Username: "draco",
				Owner:    "draco",
			},
		},
	}

	cfg.normalize()

	if got := cfg.DefaultOrchardURL(); got != "https://code.example.com" {
		t.Fatalf("DefaultOrchardURL() = %q, want https://code.example.com", got)
	}
}

func TestConfigWorkspacesRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	in := &Config{
		Workspaces: map[string]string{
			"default": "/home/draco/work/myproject",
			"scratch": "/tmp/scratch-area",
		},
		Coord: CoordConfig{
			HeartbeatInterval:   "30s",
			StaleThreshold:      "5m",
			AutoPushCoord:       true,
			FeedRetention:       "72h",
			DefaultConflictMode: "manual",
		},
	}
	if err := Save(in); err != nil {
		t.Fatalf("Save: %v", err)
	}

	out, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Verify Workspaces round-trip
	if len(out.Workspaces) != len(in.Workspaces) {
		t.Fatalf("Workspaces length = %d, want %d", len(out.Workspaces), len(in.Workspaces))
	}
	for k, want := range in.Workspaces {
		if got := out.Workspaces[k]; got != want {
			t.Fatalf("Workspaces[%q] = %q, want %q", k, got, want)
		}
	}

	// Verify Coord round-trip
	if out.Coord.HeartbeatInterval != in.Coord.HeartbeatInterval {
		t.Fatalf("Coord.HeartbeatInterval = %q, want %q", out.Coord.HeartbeatInterval, in.Coord.HeartbeatInterval)
	}
	if out.Coord.StaleThreshold != in.Coord.StaleThreshold {
		t.Fatalf("Coord.StaleThreshold = %q, want %q", out.Coord.StaleThreshold, in.Coord.StaleThreshold)
	}
	if out.Coord.AutoPushCoord != in.Coord.AutoPushCoord {
		t.Fatalf("Coord.AutoPushCoord = %v, want %v", out.Coord.AutoPushCoord, in.Coord.AutoPushCoord)
	}
	if out.Coord.FeedRetention != in.Coord.FeedRetention {
		t.Fatalf("Coord.FeedRetention = %q, want %q", out.Coord.FeedRetention, in.Coord.FeedRetention)
	}
	if out.Coord.DefaultConflictMode != in.Coord.DefaultConflictMode {
		t.Fatalf("Coord.DefaultConflictMode = %q, want %q", out.Coord.DefaultConflictMode, in.Coord.DefaultConflictMode)
	}
}

func TestOrchardProfileDoesNotLeakAcrossHosts(t *testing.T) {
	cfg := &Config{
		OrchardURL: "https://orchard.example.com",
		Token:      "default-token",
		Username:   "default-user",
		Owner:      "default-owner",
		OrchardProfiles: map[string]OrchardProfile{
			"https://code.example.com/api/v1": {
				Token:    "code-token",
				Username: "code-user",
				Owner:    "code-owner",
			},
		},
	}

	cfg.normalize()

	defaultProfile := cfg.OrchardProfile("https://orchard.example.com")
	if defaultProfile.Token != "default-token" {
		t.Fatalf("default token = %q, want default-token", defaultProfile.Token)
	}

	codeProfile := cfg.OrchardProfile("https://code.example.com/api/v1")
	if codeProfile.Token != "code-token" {
		t.Fatalf("code token = %q, want code-token", codeProfile.Token)
	}
	if leaked := cfg.OrchardProfile("https://other.example.com"); !leaked.isZero() {
		t.Fatalf("unexpected profile leak: %+v", leaked)
	}
}
