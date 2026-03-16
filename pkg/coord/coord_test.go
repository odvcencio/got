package coord

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOnCommit(t *testing.T) {
	// Create a coordinator with a real repo that has a commit
	dir := t.TempDir()

	// Write a Go file so the commit has something
	if err := os.MkdirAll(filepath.Join(dir, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	c := newTestCoordinator(t)

	// Register agent
	id, err := c.RegisterAgent(AgentInfo{Name: "committer", Workspace: "graft", Host: "test"})
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}

	// Acquire a claim
	entityKey := "decl:function_definition::Foo:func Foo():0"
	if err := c.AcquireClaim(id, ClaimRequest{
		EntityKey: entityKey,
		File:      "foo.go",
		Mode:      ClaimEditing,
	}); err != nil {
		t.Fatalf("AcquireClaim: %v", err)
	}

	// Verify claim exists
	claims, _ := c.ListClaims()
	if len(claims) != 1 {
		t.Fatalf("expected 1 claim before commit, got %d", len(claims))
	}

	// Write a file to the repo and commit so we have a real commit hash
	goFile := filepath.Join(c.Repo.RootDir, "foo.go")
	if err := os.WriteFile(goFile, []byte("package main\n\nfunc Foo() {}\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := c.Repo.Add([]string{"foo.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	commitHash, err := c.Repo.Commit("test commit", "test-author")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// Call OnCommit
	workspaces := map[string]string{}
	if err := c.OnCommit(commitHash, workspaces); err != nil {
		t.Fatalf("OnCommit: %v", err)
	}

	// Verify feed event was appended
	events, err := c.WalkFeed("", 10)
	if err != nil {
		t.Fatalf("WalkFeed: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("expected at least 1 feed event after OnCommit")
	}
	if events[0].Event != "commit" {
		t.Errorf("feed event type = %q, want commit", events[0].Event)
	}
	if events[0].CommitHash != string(commitHash) {
		t.Errorf("feed event commit hash = %q, want %q", events[0].CommitHash, commitHash)
	}
	if events[0].AgentID != id {
		t.Errorf("feed event agent ID = %q, want %q", events[0].AgentID, id)
	}
}

func TestShouldAutoPush(t *testing.T) {
	c := newTestCoordinator(t)
	c.Config.AutoPushCoord = false

	if c.ShouldAutoPush() {
		t.Error("expected false when AutoPushCoord is disabled")
	}

	c.Config.AutoPushCoord = true
	if !c.ShouldAutoPush() {
		t.Error("expected true when AutoPushCoord is enabled")
	}
}

func TestPushCoordRefs_NoRemotes(t *testing.T) {
	c := newTestCoordinator(t)
	// With no remotes configured, PushCoordRefs should return nil
	if err := c.PushCoordRefs(); err != nil {
		t.Fatalf("PushCoordRefs with no remotes: %v", err)
	}
}

func TestOnCommit_NoAgent(t *testing.T) {
	c := newTestCoordinator(t)

	// OnCommit without registering an agent should fail
	err := c.OnCommit("deadbeef", nil)
	if err == nil {
		t.Fatal("expected error when no agent is registered")
	}
}

func TestPostCommitHook(t *testing.T) {
	c := newTestCoordinator(t)

	id, err := c.RegisterAgent(AgentInfo{Name: "git-user", Workspace: "graft", Host: "test"})
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}

	// Write a file and commit
	goFile := filepath.Join(c.Repo.RootDir, "main.go")
	if err := os.WriteFile(goFile, []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := c.Repo.Add([]string{"main.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	commitHash, err := c.Repo.Commit("git commit", "test-author")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// PostCommitHook should generate a feed event
	if err := c.PostCommitHook(commitHash); err != nil {
		t.Fatalf("PostCommitHook: %v", err)
	}

	events, err := c.WalkFeed("", 10)
	if err != nil {
		t.Fatalf("WalkFeed: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("expected at least 1 feed event after PostCommitHook")
	}
	if events[0].Event != "commit" {
		t.Errorf("feed event type = %q, want commit", events[0].Event)
	}
	if events[0].AgentID != id {
		t.Errorf("feed event agent ID = %q, want %q", events[0].AgentID, id)
	}
	if events[0].CommitHash != string(commitHash) {
		t.Errorf("feed event commit hash = %q, want %q", events[0].CommitHash, commitHash)
	}
}

func TestPostCommitHook_NoAgent(t *testing.T) {
	c := newTestCoordinator(t)

	err := c.PostCommitHook("deadbeef")
	if err == nil {
		t.Fatal("expected error when no agent is registered")
	}
}
