package main

import (
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/odvcencio/graft/pkg/coordd"
	"github.com/odvcencio/graft/pkg/repo"
)

func TestCoorddSnapshotCmd_JSON(t *testing.T) {
	dir := t.TempDir()
	_, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	writeTestFile(t, filepath.Join(dir, "note.txt"), []byte("hello snapshot\n"))

	restore := chdirForTest(t, dir)
	defer restore()

	output := captureCommandStdout(t, func() error {
		cmd := newCoorddCmd()
		cmd.SilenceUsage = true
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"snapshot", "--json"})
		return cmd.Execute()
	})

	var snapshot coordd.Snapshot
	if err := json.Unmarshal([]byte(output), &snapshot); err != nil {
		t.Fatalf("json.Unmarshal: %v\nraw: %s", err, output)
	}
	if snapshot.Summary.Changed == 0 {
		t.Fatal("expected snapshot to include changed files")
	}
	if len(snapshot.Entries) != 1 {
		t.Fatalf("len(snapshot.Entries) = %d, want 1", len(snapshot.Entries))
	}
	if snapshot.Entries[0].Path != "note.txt" {
		t.Fatalf("snapshot path = %q, want note.txt", snapshot.Entries[0].Path)
	}
}

func TestCoorddServeOnce_PrintsAndLogsEvents(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	writeTestFile(t, filepath.Join(dir, "dirty.txt"), []byte("hello coordd\n"))

	restore := chdirForTest(t, dir)
	defer restore()

	output := captureCommandStdout(t, func() error {
		cmd := newCoorddCmd()
		cmd.SilenceUsage = true
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"serve", "--once", "--print"})
		return cmd.Execute()
	})

	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) == "" {
		t.Fatal("expected printed coordd events")
	}

	var event coordd.Event
	if err := json.Unmarshal([]byte(lines[0]), &event); err != nil {
		t.Fatalf("json.Unmarshal event: %v\nraw: %s", err, lines[0])
	}
	if event.Type == "" {
		t.Fatal("expected event type")
	}

	events, err := coordd.ListEvents(r.GraftDir, 0)
	if err != nil {
		t.Fatalf("coordd.ListEvents: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("expected event journal entries")
	}
}

func TestCoorddPreflightCmd_JSON(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	if err := coordd.SaveGuardConfig(r.GraftDir, &coordd.GuardConfig{Mode: "enforce"}); err != nil {
		t.Fatalf("SaveGuardConfig: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	output := captureCommandStdout(t, func() error {
		cmd := newCoorddCmd()
		cmd.SilenceUsage = true
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"preflight", "--json", "--", "rm", "-rf", "./"})
		return cmd.Execute()
	})

	var result struct {
		Input    coordd.ActionPolicyInput    `json:"input"`
		Decision coordd.ActionPolicyDecision `json:"decision"`
	}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("json.Unmarshal: %v\nraw: %s", err, output)
	}
	if result.Decision.Action != "HardBlock" {
		t.Fatalf("Decision.Action = %q, want HardBlock", result.Decision.Action)
	}
	if result.Decision.Profile != "blocked" {
		t.Fatalf("Decision.Profile = %q, want blocked", result.Decision.Profile)
	}

	events, err := coordd.ListEvents(r.GraftDir, 1)
	if err != nil {
		t.Fatalf("coordd.ListEvents: %v", err)
	}
	if len(events) != 1 || events[0].Type != "action_preflight_blocked" {
		t.Fatalf("unexpected events: %#v", events)
	}
}

func TestCoorddGuardAllowThenPreflightAllows(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	if err := coordd.SaveGuardConfig(r.GraftDir, &coordd.GuardConfig{Mode: "enforce", PreferredBackend: "host-direct"}); err != nil {
		t.Fatalf("SaveGuardConfig: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	if err := func() error {
		cmd := newCoorddCmd()
		cmd.SilenceUsage = true
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"guard", "allow", "shell:touch *"})
		return cmd.Execute()
	}(); err != nil {
		t.Fatalf("guard allow: %v", err)
	}

	output := captureCommandStdout(t, func() error {
		cmd := newCoorddCmd()
		cmd.SilenceUsage = true
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"preflight", "--json", "--", "touch", "note.txt"})
		return cmd.Execute()
	})

	var result struct {
		Decision coordd.ActionPolicyDecision `json:"decision"`
	}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("json.Unmarshal: %v\nraw: %s", err, output)
	}
	if result.Decision.Action != "Allow" {
		t.Fatalf("Decision.Action = %q, want Allow", result.Decision.Action)
	}
	if result.Decision.Profile != "repo_write" {
		t.Fatalf("Decision.Profile = %q, want repo_write", result.Decision.Profile)
	}
}

func TestCoorddExecCmd_BlocksDestructiveCommand(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	if err := coordd.SaveGuardConfig(r.GraftDir, &coordd.GuardConfig{Mode: "enforce", PreferredBackend: "host-direct"}); err != nil {
		t.Fatalf("SaveGuardConfig: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	cmd := newCoorddCmd()
	cmd.SilenceUsage = true
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"exec", "--", "rm", "-rf", "./"})
	err = cmd.Execute()
	if err == nil {
		t.Fatal("expected destructive command to be blocked")
	}

	events, err := coordd.ListEvents(r.GraftDir, 1)
	if err != nil {
		t.Fatalf("coordd.ListEvents: %v", err)
	}
	if len(events) != 1 || events[0].Type != "action_preflight_blocked" {
		t.Fatalf("unexpected events: %#v", events)
	}
}

func TestCoorddExecCmd_RunsAllowedCommand(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	if err := coordd.SaveGuardConfig(r.GraftDir, &coordd.GuardConfig{Mode: "enforce", PreferredBackend: "host-direct"}); err != nil {
		t.Fatalf("SaveGuardConfig: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	output := captureCommandStdout(t, func() error {
		cmd := newCoorddCmd()
		cmd.SilenceUsage = true
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"exec", "--json", "--", "cat", "/dev/null"})
		return cmd.Execute()
	})

	var result coordd.ExecResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("json.Unmarshal: %v\nraw: %s", err, output)
	}
	if result.Backend != "host-direct" {
		t.Fatalf("result.Backend = %q, want host-direct", result.Backend)
	}
	if result.RequestedProfile.Name != "read_only" {
		t.Fatalf("RequestedProfile.Name = %q, want read_only", result.RequestedProfile.Name)
	}
	if result.EffectiveProfile.Name != "host_direct" {
		t.Fatalf("EffectiveProfile.Name = %q, want host_direct", result.EffectiveProfile.Name)
	}

	events, err := coordd.ListEvents(r.GraftDir, 3)
	if err != nil {
		t.Fatalf("coordd.ListEvents: %v", err)
	}
	if len(events) < 2 {
		t.Fatalf("expected execution events, got %#v", events)
	}
	if events[len(events)-1].Type != "action_exec_finished" {
		t.Fatalf("last event = %q, want action_exec_finished", events[len(events)-1].Type)
	}
}

func TestCoorddGuardRuntimeAndImagePersist(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	for _, args := range [][]string{
		{"guard", "runtime", "podman"},
		{"guard", "image", "docker.io/library/alpine:3.20"},
	} {
		if err := func() error {
			cmd := newCoorddCmd()
			cmd.SilenceUsage = true
			cmd.SetErr(io.Discard)
			cmd.SetArgs(args)
			return cmd.Execute()
		}(); err != nil {
			t.Fatalf("cmd.Execute(%v): %v", args, err)
		}
	}

	cfg, err := coordd.LoadGuardConfig(r.GraftDir)
	if err != nil {
		t.Fatalf("LoadGuardConfig: %v", err)
	}
	if cfg.ContainerRuntime != "podman" {
		t.Fatalf("ContainerRuntime = %q, want podman", cfg.ContainerRuntime)
	}
	if cfg.ContainerImage != "docker.io/library/alpine:3.20" {
		t.Fatalf("ContainerImage = %q", cfg.ContainerImage)
	}
}

func TestCoorddExecCmd_JSONReservesStdout(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	if err := coordd.SaveGuardConfig(r.GraftDir, &coordd.GuardConfig{Mode: "enforce", PreferredBackend: "host-direct"}); err != nil {
		t.Fatalf("SaveGuardConfig: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	output := captureCommandStdout(t, func() error {
		cmd := newCoorddCmd()
		cmd.SilenceUsage = true
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"exec", "--json", "--", "printf", "hello"})
		return cmd.Execute()
	})

	var result coordd.ExecResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("json.Unmarshal: %v\nraw: %s", err, output)
	}
	if result.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0", result.ExitCode)
	}
	if !strings.HasPrefix(strings.TrimSpace(output), "{") {
		t.Fatalf("expected stdout to contain JSON only, got %q", output)
	}
}
