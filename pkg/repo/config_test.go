package repo

import (
	"testing"

	"github.com/odvcencio/got/pkg/object"
)

func TestConfigRemoteRoundTrip(t *testing.T) {
	r, err := Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	if err := r.SetRemote("origin", "https://example.com/got/alice/repo"); err != nil {
		t.Fatalf("SetRemote: %v", err)
	}

	url, err := r.RemoteURL("origin")
	if err != nil {
		t.Fatalf("RemoteURL: %v", err)
	}
	if url != "https://example.com/got/alice/repo" {
		t.Fatalf("remote URL = %q, want %q", url, "https://example.com/got/alice/repo")
	}
}

func TestReadConfigMissingReturnsEmptyConfig(t *testing.T) {
	r, err := Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	cfg, err := r.ReadConfig()
	if err != nil {
		t.Fatalf("ReadConfig: %v", err)
	}
	if cfg == nil {
		t.Fatalf("config is nil")
	}
	if len(cfg.Remotes) != 0 {
		t.Fatalf("expected no remotes, got %d", len(cfg.Remotes))
	}
}

func TestListRefs(t *testing.T) {
	r, err := Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	if err := r.UpdateRef("refs/heads/main", object.Hash("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")); err != nil {
		t.Fatal(err)
	}
	if err := r.UpdateRef("refs/remotes/origin/heads/main", object.Hash("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")); err != nil {
		t.Fatal(err)
	}

	all, err := r.ListRefs("")
	if err != nil {
		t.Fatal(err)
	}
	if got := all["heads/main"]; got == "" {
		t.Fatalf("missing heads/main from ListRefs")
	}
	if got := all["remotes/origin/heads/main"]; got == "" {
		t.Fatalf("missing remotes/origin/heads/main from ListRefs")
	}

	heads, err := r.ListRefs("heads")
	if err != nil {
		t.Fatal(err)
	}
	if len(heads) != 1 {
		t.Fatalf("heads len = %d, want 1", len(heads))
	}
	if _, ok := heads["heads/main"]; !ok {
		t.Fatalf("expected heads/main in prefix listing")
	}
}
