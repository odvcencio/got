package main

import "testing"

func TestCanonicalizeRemoteSpec(t *testing.T) {
	t.Run("gothub shorthand uses default host", func(t *testing.T) {
		t.Setenv("GOT_GOTHUB_URL", "")
		got, err := canonicalizeRemoteSpec("gothub:alice/repo")
		if err != nil {
			t.Fatalf("canonicalizeRemoteSpec: %v", err)
		}
		want := "https://gothub.dev/got/alice/repo"
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})

	t.Run("gothub shorthand uses configured host", func(t *testing.T) {
		t.Setenv("GOT_GOTHUB_URL", "https://code.example.com/base")
		got, err := canonicalizeRemoteSpec("gothub:alice/repo")
		if err != nil {
			t.Fatalf("canonicalizeRemoteSpec: %v", err)
		}
		want := "https://code.example.com/base/got/alice/repo"
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})

	t.Run("host shorthand", func(t *testing.T) {
		got, err := canonicalizeRemoteSpec("code.example.com:alice/repo")
		if err != nil {
			t.Fatalf("canonicalizeRemoteSpec: %v", err)
		}
		want := "https://code.example.com/got/alice/repo"
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})

	t.Run("github shorthand resolves to git url", func(t *testing.T) {
		got, err := canonicalizeRemoteSpec("github:alice/repo")
		if err != nil {
			t.Fatalf("canonicalizeRemoteSpec: %v", err)
		}
		want := "https://github.com/alice/repo.git"
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})
}

func TestParseRemoteSpecSupportsShorthand(t *testing.T) {
	t.Setenv("GOT_GOTHUB_URL", "https://code.example.com")
	kind, got, err := parseRemoteSpec("gothub:alice/repo")
	if err != nil {
		t.Fatalf("parseRemoteSpec: %v", err)
	}
	want := "https://code.example.com/got/alice/repo"
	if kind != remoteTransportGot {
		t.Fatalf("kind = %q, want %q", kind, remoteTransportGot)
	}
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
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
