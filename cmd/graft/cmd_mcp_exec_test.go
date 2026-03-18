package main

import (
	"encoding/json"
	"testing"

	"github.com/odvcencio/graft/pkg/coordd"
	"github.com/odvcencio/graft/pkg/repo"
)

type mcpExecTestResult struct {
	Input    coordd.ActionPolicyInput    `json:"input"`
	Decision coordd.ActionPolicyDecision `json:"decision"`
	Allowed  bool                        `json:"allowed"`
	Exec     *coordd.ExecResult          `json:"exec"`
	Stdout   string                      `json:"stdout"`
	Stderr   string                      `json:"stderr"`
	ExitCode int                         `json:"exit_code"`
	Status   string                      `json:"status"`
	Error    string                      `json:"error"`
}

func TestMCPToolExec_CheckOnlyBlocked(t *testing.T) {
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

	restore := chdirForTest(t, dir)
	defer restore()

	resultAny, err := mcpDispatchAll(false, "graft_exec", map[string]any{
		"command":    "rm",
		"args":       []any{"-rf", "./"},
		"check_only": true,
	})
	if err != nil {
		t.Fatalf("mcpDispatchAll: %v", err)
	}

	result := decodeMCPExecResult(t, resultAny)
	if result.Status != "preflight" {
		t.Fatalf("Status = %q, want preflight", result.Status)
	}
	if result.Allowed {
		t.Fatal("Allowed = true, want false")
	}
	if result.Decision.Action != "HardBlock" {
		t.Fatalf("Decision.Action = %q, want HardBlock", result.Decision.Action)
	}
	if result.Decision.Profile != "blocked" {
		t.Fatalf("Decision.Profile = %q, want blocked", result.Decision.Profile)
	}
	if result.Exec != nil {
		t.Fatalf("Exec = %#v, want nil for check_only", result.Exec)
	}

	events, err := coordd.ListEvents(r.GraftDir, 1)
	if err != nil {
		t.Fatalf("coordd.ListEvents: %v", err)
	}
	if len(events) != 1 || events[0].Type != "action_preflight_blocked" {
		t.Fatalf("unexpected events: %#v", events)
	}
}

func TestMCPToolExec_RunsThroughCoorddHostDirect(t *testing.T) {
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

	restore := chdirForTest(t, dir)
	defer restore()

	resultAny, err := mcpDispatchAll(false, "graft_exec", map[string]any{
		"command": "printf",
		"args":    []any{"hello"},
	})
	if err != nil {
		t.Fatalf("mcpDispatchAll: %v", err)
	}

	result := decodeMCPExecResult(t, resultAny)
	if result.Status != "completed" {
		t.Fatalf("Status = %q, want completed", result.Status)
	}
	if !result.Allowed {
		t.Fatal("Allowed = false, want true")
	}
	if result.Stdout != "hello" {
		t.Fatalf("Stdout = %q, want hello", result.Stdout)
	}
	if result.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0", result.ExitCode)
	}
	if result.Exec == nil {
		t.Fatal("Exec = nil, want exec result")
	}
	if result.Exec.Backend != "host-direct" {
		t.Fatalf("Exec.Backend = %q, want host-direct", result.Exec.Backend)
	}
	if result.Exec.RequestedProfile.Name != "read_only" {
		t.Fatalf("RequestedProfile.Name = %q, want read_only", result.Exec.RequestedProfile.Name)
	}
}

func TestMCPToolExec_NonZeroExitReturnsStructuredFailure(t *testing.T) {
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

	restore := chdirForTest(t, dir)
	defer restore()

	resultAny, err := mcpDispatchAll(false, "graft_exec", map[string]any{
		"command": "false",
	})
	if err != nil {
		t.Fatalf("mcpDispatchAll: %v", err)
	}

	result := decodeMCPExecResult(t, resultAny)
	if result.Status != "failed" {
		t.Fatalf("Status = %q, want failed", result.Status)
	}
	if !result.Allowed {
		t.Fatal("Allowed = false, want true")
	}
	if result.ExitCode == 0 {
		t.Fatal("ExitCode = 0, want non-zero")
	}
	if result.Error == "" {
		t.Fatal("Error = empty, want structured execution error")
	}
	if result.Exec == nil {
		t.Fatal("Exec = nil, want exec result")
	}
	if result.Exec.Backend != "host-direct" {
		t.Fatalf("Exec.Backend = %q, want host-direct", result.Exec.Backend)
	}
}

func decodeMCPExecResult(t *testing.T, value any) mcpExecTestResult {
	t.Helper()

	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var result mcpExecTestResult
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("json.Unmarshal: %v\nraw: %s", err, string(data))
	}
	return result
}
