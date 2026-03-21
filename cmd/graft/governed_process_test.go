package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/odvcencio/graft/pkg/coord"
	"github.com/odvcencio/graft/pkg/coordd"
	"github.com/odvcencio/graft/pkg/repo"
)

func TestCoorddRepoProcessGuard_BlocksUnallowlistedWrite(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	if err := coordd.SaveGuardConfig(r.GraftDir, &coordd.GuardConfig{
		Mode:             "enforce",
		PreferredBackend: "host-direct",
	}); err != nil {
		t.Fatalf("SaveGuardConfig: %v", err)
	}
	target := filepath.Join(dir, "blocked.txt")

	err = repo.RunExternalProcess(repo.ExternalProcessSpec{
		Dir:  dir,
		Path: "touch",
		Args: []string{target},
	})
	if err == nil {
		t.Fatal("expected guarded process to be blocked")
	}

	var exitCoder interface{ ExitCode() int }
	if !errors.As(err, &exitCoder) {
		t.Fatalf("expected exit-coded error, got %T: %v", err, err)
	}
	if exitCoder.ExitCode() != 126 {
		t.Fatalf("ExitCode = %d, want 126", exitCoder.ExitCode())
	}
	if _, statErr := os.Stat(target); !os.IsNotExist(statErr) {
		t.Fatalf("blocked target should not exist, stat error=%v", statErr)
	}
}

func TestCoorddRepoProcessExecutor_RunsWithDirEnvAndMetadata(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	if err := coordd.SaveGuardConfig(r.GraftDir, &coordd.GuardConfig{
		Mode:             "advisory",
		PreferredBackend: "host-direct",
	}); err != nil {
		t.Fatalf("SaveGuardConfig: %v", err)
	}

	err = repo.RunExternalProcess(repo.ExternalProcessSpec{
		Dir:   dir,
		Path:  "sh",
		Args:  []string{"-c", "printf '%s' \"$HOOK_VALUE\" > hook-env.txt && pwd > hook-cwd.txt"},
		Env:   append(os.Environ(), "HOOK_VALUE=ok"),
		Label: "repo-hook:pre-commit",
	})
	if err != nil {
		t.Fatalf("RunExternalProcess: %v", err)
	}

	envData, err := os.ReadFile(filepath.Join(dir, "hook-env.txt"))
	if err != nil {
		t.Fatalf("ReadFile env: %v", err)
	}
	if string(envData) != "ok" {
		t.Fatalf("hook env output = %q, want ok", string(envData))
	}

	cwdData, err := os.ReadFile(filepath.Join(dir, "hook-cwd.txt"))
	if err != nil {
		t.Fatalf("ReadFile cwd: %v", err)
	}
	if got := string(cwdData); got != dir+"\n" {
		t.Fatalf("hook cwd output = %q, want %q", got, dir+"\\n")
	}

	events, err := coordd.ListEvents(r.GraftDir, 0)
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("expected coordd events for governed external process")
	}

	foundStart := false
	for _, event := range events {
		if event.Type != "action_exec_started" {
			continue
		}
		if event.Data["label"] == "repo-hook:pre-commit" && event.Data["origin"] == "repo_hook" && event.Data["point"] == "pre-commit" {
			foundStart = true
			break
		}
	}
	if !foundStart {
		t.Fatalf("expected action_exec_started event with process metadata, got %#v", events)
	}
}

func TestGovernedProcess_RunsPostActionEffects(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	// Register an agent
	c := coord.New(r, coord.DefaultConfig)
	id, err := c.RegisterAgent(coord.AgentInfo{Name: "cedar"})
	if err != nil {
		t.Fatal(err)
	}

	// Write agent-id file so governed process finds it
	coordDir := filepath.Join(r.GraftDir, "coord")
	os.MkdirAll(coordDir, 0o755)
	os.WriteFile(filepath.Join(coordDir, "agent-id"), []byte(id), 0o644)

	// Verify the helper creates a valid publisher
	pub := coordPublisherForRepo(r, id)
	if pub == nil {
		t.Fatal("coordPublisherForRepo returned nil")
	}
	if pub.AgentID != id {
		t.Errorf("AgentID = %q, want %q", pub.AgentID, id)
	}
}
