package main

import (
	"testing"

	"github.com/odvcencio/graft/pkg/userconfig"
)

func TestResolvePublishTarget(t *testing.T) {
	t.Run("explicit owner and repo", func(t *testing.T) {
		owner, repo, err := resolvePublishTarget([]string{"alice/demo"}, "/tmp/demo", "https://orchard.example.com")
		if err != nil {
			t.Fatalf("resolvePublishTarget: %v", err)
		}
		if owner != "alice" || repo != "demo" {
			t.Fatalf("graft %q/%q, want alice/demo", owner, repo)
		}
	})

	t.Run("infer from env and root dir", func(t *testing.T) {
		t.Setenv("GRAFT_OWNER", "alice")
		owner, repo, err := resolvePublishTarget(nil, "/tmp/my-repo", "https://orchard.example.com")
		if err != nil {
			t.Fatalf("resolvePublishTarget: %v", err)
		}
		if owner != "alice" || repo != "my-repo" {
			t.Fatalf("graft %q/%q, want alice/my-repo", owner, repo)
		}
	})

	t.Run("infer owner from host profile", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)

		if err := userconfig.Save(&userconfig.Config{
			OrchardURL: "https://orchard.example.com",
			Token:      "default-token",
			Username:   "default-user",
			Owner:      "default-owner",
			OrchardProfiles: map[string]userconfig.OrchardProfile{
				"https://code.example.com/api/v1": {
					Token:    "code-token",
					Username: "code-user",
					Owner:    "code-owner",
				},
			},
		}); err != nil {
			t.Fatalf("Save: %v", err)
		}

		owner, repo, err := resolvePublishTarget(nil, "/tmp/my-repo", "https://code.example.com/api/v1")
		if err != nil {
			t.Fatalf("resolvePublishTarget: %v", err)
		}
		if owner != "code-owner" || repo != "my-repo" {
			t.Fatalf("graft %q/%q, want code-owner/my-repo", owner, repo)
		}
	})
}
