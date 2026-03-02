package main

import (
	"testing"

	"github.com/odvcencio/graft/pkg/userconfig"
)

func TestCanonicalizeRemoteSpec(t *testing.T) {
	t.Run("orchard shorthand uses default host", func(t *testing.T) {
		t.Setenv("GRAFT_ORCHARD_URL", "")
		got, err := canonicalizeRemoteSpec("orchard:alice/repo")
		if err != nil {
			t.Fatalf("canonicalizeRemoteSpec: %v", err)
		}
		want := "https://orchard.dev/graft/alice/repo"
		if got != want {
			t.Fatalf("graft %q, want %q", got, want)
		}
	})

	t.Run("orchard shorthand uses configured host", func(t *testing.T) {
		t.Setenv("GRAFT_ORCHARD_URL", "https://code.example.com/base")
		got, err := canonicalizeRemoteSpec("orchard:alice/repo")
		if err != nil {
			t.Fatalf("canonicalizeRemoteSpec: %v", err)
		}
		want := "https://code.example.com/base/graft/alice/repo"
		if got != want {
			t.Fatalf("graft %q, want %q", got, want)
		}
	})

	t.Run("orchard shorthand uses ~/.graftconfig host when env is unset", func(t *testing.T) {
		t.Setenv("GRAFT_ORCHARD_URL", "")
		home := t.TempDir()
		t.Setenv("HOME", home)
		if err := userconfig.Save(&userconfig.Config{OrchardURL: "https://cfg.example.dev"}); err != nil {
			t.Fatalf("Save user config: %v", err)
		}
		got, err := canonicalizeRemoteSpec("orchard:alice/repo")
		if err != nil {
			t.Fatalf("canonicalizeRemoteSpec: %v", err)
		}
		want := "https://cfg.example.dev/graft/alice/repo"
		if got != want {
			t.Fatalf("graft %q, want %q", got, want)
		}
	})

	t.Run("host shorthand", func(t *testing.T) {
		got, err := canonicalizeRemoteSpec("code.example.com:alice/repo")
		if err != nil {
			t.Fatalf("canonicalizeRemoteSpec: %v", err)
		}
		want := "https://code.example.com/graft/alice/repo"
		if got != want {
			t.Fatalf("graft %q, want %q", got, want)
		}
	})

	t.Run("github shorthand resolves to git url", func(t *testing.T) {
		got, err := canonicalizeRemoteSpec("github:alice/repo")
		if err != nil {
			t.Fatalf("canonicalizeRemoteSpec: %v", err)
		}
		want := "https://github.com/alice/repo.git"
		if got != want {
			t.Fatalf("graft %q, want %q", got, want)
		}
	})
}

func TestParseRemoteSpecSupportsShorthand(t *testing.T) {
	t.Setenv("GRAFT_ORCHARD_URL", "https://code.example.com")
	kind, got, err := parseRemoteSpec("orchard:alice/repo")
	if err != nil {
		t.Fatalf("parseRemoteSpec: %v", err)
	}
	want := "https://code.example.com/graft/alice/repo"
	if kind != remoteTransportGraft {
		t.Fatalf("kind = %q, want %q", kind, remoteTransportGraft)
	}
	if got != want {
		t.Fatalf("graft %q, want %q", got, want)
	}
}

func TestParseRemoteSpecClassifiesGitForgeHosts(t *testing.T) {
	kind, _, err := parseRemoteSpec("https://github.com/alice/repo.git")
	if err != nil {
		t.Fatalf("parseRemoteSpec: %v", err)
	}
	if kind != remoteTransportGit {
		t.Fatalf("kind = %q, want %q", kind, remoteTransportGit)
	}
}

func TestParseGotRemoteURLRejectsGitTransport(t *testing.T) {
	_, err := parseGotRemoteURL("github:alice/repo")
	if err == nil {
		t.Fatal("expected error for git transport remote")
	}
}
