package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/odvcencio/graft/pkg/userconfig"
)

func TestIntegration_ConfigSetGetRepoLevel(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dir := initRepo(t)

	// Set user.name at repo level.
	mustRunGraft(t, dir, "config", "user.name", "Alice")

	// Get user.name.
	out := mustRunGraft(t, dir, "config", "user.name")
	if got := strings.TrimSpace(out); got != "Alice" {
		t.Fatalf("config user.name = %q, want %q", got, "Alice")
	}

	// Set user.email at repo level.
	mustRunGraft(t, dir, "config", "user.email", "alice@example.com")

	// Get user.email.
	out = mustRunGraft(t, dir, "config", "user.email")
	if got := strings.TrimSpace(out); got != "alice@example.com" {
		t.Fatalf("config user.email = %q, want %q", got, "alice@example.com")
	}

	// List should show both.
	out = mustRunGraft(t, dir, "config", "--list")
	if !strings.Contains(out, "user.name=Alice") {
		t.Fatalf("config --list missing user.name: %s", out)
	}
	if !strings.Contains(out, "user.email=alice@example.com") {
		t.Fatalf("config --list missing user.email: %s", out)
	}
}

func TestIntegration_ConfigGlobalSetGet(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Use a temp HOME to avoid polluting real config.
	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	dir := initRepo(t)

	// Set global user.name.
	mustRunGraft(t, dir, "config", "--global", "user.name", "GlobalAlice")

	// Get global user.name.
	out := mustRunGraft(t, dir, "config", "--global", "user.name")
	if got := strings.TrimSpace(out); got != "GlobalAlice" {
		t.Fatalf("config --global user.name = %q, want %q", got, "GlobalAlice")
	}

	// Verify it was written to ~/.graftconfig.
	data, err := os.ReadFile(filepath.Join(fakeHome, ".graftconfig"))
	if err != nil {
		t.Fatalf("read .graftconfig: %v", err)
	}
	if !strings.Contains(string(data), "GlobalAlice") {
		t.Fatalf(".graftconfig missing GlobalAlice: %s", string(data))
	}
}

func TestIntegration_ConfigFallbackToGlobal(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)

	dir := initRepo(t)

	// Set global user.name (no repo-level config).
	mustRunGraft(t, dir, "config", "--global", "user.name", "FallbackAlice")

	// Get user.name without --global — should fall back to global.
	out := mustRunGraft(t, dir, "config", "user.name")
	if got := strings.TrimSpace(out); got != "FallbackAlice" {
		t.Fatalf("config user.name (fallback) = %q, want %q", got, "FallbackAlice")
	}
}

func TestIntegration_CommitWithConfigAuthor(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dir := initRepo(t)

	// Set repo-level author identity.
	mustRunGraft(t, dir, "config", "user.name", "Config Author")
	mustRunGraft(t, dir, "config", "user.email", "config@example.com")

	// Create and commit a file WITHOUT --author.
	writeFile(t, dir, "hello.txt", "hello world\n")
	mustRunGraft(t, dir, "add", "hello.txt")
	commitOut := mustRunGraft(t, dir, "commit", "-m", "config author test", "--no-sign")

	if !strings.Contains(commitOut, "config author test") {
		t.Errorf("commit output missing message: %s", commitOut)
	}

	// Log should show the config author.
	logOut := mustRunGraft(t, dir, "log")
	if !strings.Contains(logOut, "Config Author") {
		t.Errorf("log should show config author: %s", logOut)
	}
	if !strings.Contains(logOut, "config@example.com") {
		t.Errorf("log should show config email: %s", logOut)
	}
}

func TestIntegration_ConfigUnknownKey(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dir := initRepo(t)

	// Unknown key should fail.
	_, err := runGraft(t, dir, "config", "foo.bar", "value")
	if err == nil {
		t.Fatal("expected error for unknown config key")
	}
}

func TestFormatUserConfigIncludesOrchardProfiles(t *testing.T) {
	lines := formatUserConfig(&userconfig.Config{
		OrchardURL: "https://orchard.example.com",
		Username:   "orchard-user",
		Owner:      "orchard-owner",
		OrchardProfiles: map[string]userconfig.OrchardProfile{
			"https://orchard.example.com": {
				Token:    "orchard-token",
				Username: "orchard-user",
				Owner:    "orchard-owner",
			},
			"https://code.example.com/api/v1": {
				Token:    "code-token",
				Username: "code-user",
				Owner:    "code-owner",
			},
		},
	})

	out := strings.Join(lines, "\n")
	for _, want := range []string{
		"orchard.url=https://orchard.example.com",
		"orchard.username=orchard-user",
		"orchard.owner=orchard-owner",
		"orchard.profile[https://orchard.example.com].default=true",
		"orchard.profile[https://orchard.example.com].token=set",
		"orchard.profile[https://code.example.com/api/v1].username=code-user",
		"orchard.profile[https://code.example.com/api/v1].owner=code-owner",
		"orchard.profile[https://code.example.com/api/v1].token=set",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("formatUserConfig output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "code-token") || strings.Contains(out, "orchard-token") {
		t.Fatalf("formatUserConfig leaked token values:\n%s", out)
	}
}
