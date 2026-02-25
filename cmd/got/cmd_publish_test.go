package main

import "testing"

func TestResolvePublishTarget(t *testing.T) {
	t.Run("explicit owner and repo", func(t *testing.T) {
		owner, repo, err := resolvePublishTarget([]string{"alice/demo"}, "/tmp/demo")
		if err != nil {
			t.Fatalf("resolvePublishTarget: %v", err)
		}
		if owner != "alice" || repo != "demo" {
			t.Fatalf("got %q/%q, want alice/demo", owner, repo)
		}
	})

	t.Run("infer from env and root dir", func(t *testing.T) {
		t.Setenv("GOT_OWNER", "alice")
		owner, repo, err := resolvePublishTarget(nil, "/tmp/my-repo")
		if err != nil {
			t.Fatalf("resolvePublishTarget: %v", err)
		}
		if owner != "alice" || repo != "my-repo" {
			t.Fatalf("got %q/%q, want alice/my-repo", owner, repo)
		}
	})
}
