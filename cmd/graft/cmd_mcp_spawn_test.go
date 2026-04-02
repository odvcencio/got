package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/odvcencio/graft/pkg/coord"
	"github.com/odvcencio/graft/pkg/coordd"
	"github.com/odvcencio/graft/pkg/repo"
)

type mcpSpawnTestResult struct {
	Status   string             `json:"status"`
	Result   coordd.SpawnResult `json:"result"`
	ExitCode int                `json:"exit_code"`
	Error    string             `json:"error"`
}

func TestMCPToolSpawn_StartsAndListsSpawns(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	if err := coordd.SaveGuardConfig(r.GraftDir, &coordd.GuardConfig{
		Mode:             "enforce",
		PreferredBackend: "container",
	}); err != nil {
		t.Fatalf("SaveGuardConfig: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(r.GraftDir, "coord"), 0o755); err != nil {
		t.Fatalf("MkdirAll coord: %v", err)
	}
	if err := os.WriteFile(filepath.Join(r.GraftDir, "coord", "agent-id"), []byte("agent-parent"), 0o644); err != nil {
		t.Fatalf("write agent-id: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	resultAny, err := mcpDispatchAll(false, "graft_spawn", map[string]any{
		"name":    "child-agent",
		"command": "printf",
		"args":    []any{"hello"},
		"runtime": "detached",
	})
	if err != nil {
		t.Fatalf("mcpDispatchAll spawn: %v", err)
	}

	result := decodeMCPSpawnResult(t, resultAny)
	if result.Status != "started" {
		t.Fatalf("Status = %q, want started", result.Status)
	}
	if result.Result.Record == nil {
		t.Fatal("Result.Record = nil, want spawn record")
	}
	if result.Result.Record.Backend != "host-direct" && result.Result.Record.Backend != "host-bwrap" {
		t.Fatalf("Result.Record.Backend = %q, want detached host backend", result.Result.Record.Backend)
	}
	if result.Result.Record.RequestedRuntime != "detached" {
		t.Fatalf("Result.Record.RequestedRuntime = %q, want detached", result.Result.Record.RequestedRuntime)
	}
	if got, ok := waitForMCPSpawnFile(result.Result.Record.StdoutPath, "hello", 2*time.Second); !ok {
		t.Fatalf("stdout log missing child output: %q", got)
	}

	listAny, err := mcpDispatchAll(false, "graft_spawns", map[string]any{})
	if err != nil {
		t.Fatalf("mcpDispatchAll spawns: %v", err)
	}

	records := decodeMCPSpawnList(t, listAny)
	if len(records) != 1 {
		t.Fatalf("len(records) = %d, want 1", len(records))
	}
	if records[0].Name != "child-agent" {
		t.Fatalf("records[0].Name = %q, want child-agent", records[0].Name)
	}
}

func TestMCPToolSpawn_LeaseHeartbeatAndFinish(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	if err := coordd.SaveGuardConfig(r.GraftDir, &coordd.GuardConfig{
		Mode:             "enforce",
		PreferredBackend: "container",
	}); err != nil {
		t.Fatalf("SaveGuardConfig: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(r.GraftDir, "coord"), 0o755); err != nil {
		t.Fatalf("MkdirAll coord: %v", err)
	}
	if err := os.WriteFile(filepath.Join(r.GraftDir, "coord", "agent-id"), []byte("agent-parent"), 0o644); err != nil {
		t.Fatalf("write agent-id: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	resultAny, err := mcpDispatchAll(false, "graft_spawn", map[string]any{
		"name":            "child-agent",
		"command":         "printf",
		"args":            []any{"hello"},
		"runtime":         "detached",
		"launch":          "lease",
		"bootstrap_coord": true,
	})
	if err != nil {
		t.Fatalf("mcpDispatchAll spawn: %v", err)
	}

	result := decodeMCPSpawnResult(t, resultAny)
	if result.Status != "authorized" {
		t.Fatalf("Status = %q, want authorized", result.Status)
	}
	if result.Result.Record == nil {
		t.Fatal("Result.Record = nil, want spawn record")
	}
	if result.Result.Record.LaunchMode != "lease" {
		t.Fatalf("LaunchMode = %q, want lease", result.Result.Record.LaunchMode)
	}
	if !result.Result.Record.BootstrapCoord {
		t.Fatal("BootstrapCoord = false, want true")
	}
	if result.Result.Record.Status != "authorized" {
		t.Fatalf("Record.Status = %q, want authorized", result.Result.Record.Status)
	}
	if result.Result.Record.ChildAgentID == "" || result.Result.Record.ChildAgentName == "" {
		t.Fatalf("missing bootstrapped child identity: %#v", result.Result.Record)
	}

	heartbeatAny, err := mcpDispatchAll(false, "graft_spawn_heartbeat", map[string]any{
		"id":             result.Result.Record.ID,
		"child_agent_id": "child-subagent",
	})
	if err != nil {
		t.Fatalf("mcpDispatchAll heartbeat: %v", err)
	}
	heartbeat := decodeMCPSpawnRecord(t, heartbeatAny)
	if heartbeat.Status != "active" {
		t.Fatalf("heartbeat Status = %q, want active", heartbeat.Status)
	}

	finishAny, err := mcpDispatchAll(false, "graft_spawn_finish", map[string]any{
		"id":             result.Result.Record.ID,
		"status":         "completed",
		"child_agent_id": "child-subagent",
	})
	if err != nil {
		t.Fatalf("mcpDispatchAll finish: %v", err)
	}
	finished := decodeMCPSpawnRecord(t, finishAny)
	if finished.Status != "completed" {
		t.Fatalf("finished Status = %q, want completed", finished.Status)
	}
}

func TestMCPToolSpawn_GetAndWait(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	if err := coordd.SaveGuardConfig(r.GraftDir, &coordd.GuardConfig{
		Mode:             "enforce",
		PreferredBackend: "container",
	}); err != nil {
		t.Fatalf("SaveGuardConfig: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(r.GraftDir, "coord"), 0o755); err != nil {
		t.Fatalf("MkdirAll coord: %v", err)
	}
	if err := os.WriteFile(filepath.Join(r.GraftDir, "coord", "agent-id"), []byte("agent-parent"), 0o644); err != nil {
		t.Fatalf("write agent-id: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	result, err := coordd.AuthorizeSpawn(r, "agent-parent", coordd.SpawnRequest{
		Name:           "get-child",
		Command:        []string{"printf", "hello"},
		Runtime:        "detached",
		Launch:         "lease",
		BootstrapCoord: true,
	})
	if err != nil {
		t.Fatalf("AuthorizeSpawn: %v", err)
	}

	viewAny, err := mcpDispatchAll(false, "graft_spawn_get", map[string]any{"id": result.Record.ID})
	if err != nil {
		t.Fatalf("mcpDispatchAll get: %v", err)
	}
	view := decodeMCPSpawnView(t, viewAny)
	if view.Lease == nil || view.Lease.Env["GRAFT_COORD_AGENT_ID"] != result.Record.ChildAgentID {
		t.Fatalf("missing expected lease env in view: %#v", view)
	}

	go func() {
		time.Sleep(50 * time.Millisecond)
		_, _ = coordd.FinishSpawn(r.GraftDir, result.Record.ID, "completed", "")
	}()

	waitAny, err := mcpDispatchAll(false, "graft_spawn_wait", map[string]any{
		"id":         result.Record.ID,
		"timeout_ms": 2000,
	})
	if err != nil {
		t.Fatalf("mcpDispatchAll wait: %v", err)
	}
	waited := decodeMCPSpawnRecord(t, waitAny)
	if waited.Status != "completed" {
		t.Fatalf("waited.Status = %q, want completed", waited.Status)
	}
}

func TestMCPToolSpawn_ConsumeWithTaskBinding(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	if err := coordd.SaveGuardConfig(r.GraftDir, &coordd.GuardConfig{
		Mode:             "enforce",
		PreferredBackend: "container",
	}); err != nil {
		t.Fatalf("SaveGuardConfig: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(r.GraftDir, "coord"), 0o755); err != nil {
		t.Fatalf("MkdirAll coord: %v", err)
	}
	if err := os.WriteFile(filepath.Join(r.GraftDir, "coord", "agent-id"), []byte("agent-parent"), 0o644); err != nil {
		t.Fatalf("write agent-id: %v", err)
	}
	c := coord.New(r, coord.DefaultConfig)
	task := &coord.Task{Title: "Consume leased child"}
	if err := c.CreateTask(task); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	resultAny, err := mcpDispatchAll(false, "graft_spawn", map[string]any{
		"name":            "lease-child",
		"command":         "printf",
		"args":            []any{"hello"},
		"runtime":         "detached",
		"launch":          "lease",
		"bootstrap_coord": true,
		"task_id":         task.ID,
	})
	if err != nil {
		t.Fatalf("mcpDispatchAll spawn: %v", err)
	}
	result := decodeMCPSpawnResult(t, resultAny)
	if result.Result.Record == nil || result.Result.Record.Task == nil || result.Result.Record.Task.ID != task.ID {
		t.Fatalf("spawn result task = %#v, want %q", result.Result.Record.Task, task.ID)
	}

	consumeAny, err := mcpDispatchAll(false, "graft_spawn_consume", map[string]any{
		"id": result.Result.Record.ID,
	})
	if err != nil {
		t.Fatalf("mcpDispatchAll consume: %v", err)
	}
	view := decodeMCPSpawnView(t, consumeAny)
	if view.Record == nil || view.Record.Task == nil || view.Record.Task.Status != "in_progress" {
		t.Fatalf("view.Record.Task = %#v, want in_progress", view.Record.Task)
	}
	if view.Lease == nil || view.Lease.Env["GRAFT_COORDD_TASK_ID"] != task.ID {
		t.Fatalf("view.Lease = %#v, want task env", view.Lease)
	}

	got, err := c.GetTask(task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Status != "in_progress" {
		t.Fatalf("task.Status = %q, want in_progress", got.Status)
	}
}

func TestMCPToolSpawn_Trace(t *testing.T) {
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
	if err := os.MkdirAll(filepath.Join(r.GraftDir, "coord"), 0o755); err != nil {
		t.Fatalf("MkdirAll coord: %v", err)
	}
	if err := os.WriteFile(filepath.Join(r.GraftDir, "coord", "agent-id"), []byte("agent-parent"), 0o644); err != nil {
		t.Fatalf("write agent-id: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	result, err := coordd.AuthorizeSpawn(r, "agent-parent", coordd.SpawnRequest{
		Name:           "trace-child",
		Command:        []string{"printf", "hello"},
		Runtime:        "detached",
		Launch:         "lease",
		BootstrapCoord: true,
	})
	if err != nil {
		t.Fatalf("AuthorizeSpawn: %v", err)
	}

	t.Setenv("GRAFT_COORDD_SPAWN_ID", result.Record.ID)
	input, err := coordd.BuildShellActionInput(r, result.Record.ChildAgentID, []string{"true"})
	if err != nil {
		t.Fatalf("BuildShellActionInput: %v", err)
	}
	decision, err := coordd.EvaluateActionPolicy(input)
	if err != nil {
		t.Fatalf("EvaluateActionPolicy: %v", err)
	}
	if _, err := coordd.ExecuteGuardedWithIO(r, input, decision, coordd.ExecIO{
		Stdout: io.Discard,
		Stderr: io.Discard,
	}); err != nil {
		t.Fatalf("ExecuteGuardedWithIO: %v", err)
	}

	traceAny, err := mcpDispatchAll(false, "graft_spawn_trace", map[string]any{
		"id": result.Record.ID,
	})
	if err != nil {
		t.Fatalf("mcpDispatchAll trace: %v", err)
	}
	trace := decodeMCPSpawnTraceView(t, traceAny)
	if trace.Record == nil || trace.Record.ID != result.Record.ID {
		t.Fatalf("trace.Record = %#v, want spawn %q", trace.Record, result.Record.ID)
	}
	if len(trace.Execs) == 0 {
		t.Fatal("expected trace.Execs to include persisted exec")
	}
	if len(trace.Phases) == 0 {
		t.Fatal("expected trace.Phases to include grouped events")
	}
}

func TestMCPToolSpawn_TraceExecutionOnlyNoFallbacks(t *testing.T) {
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
	if err := os.MkdirAll(filepath.Join(r.GraftDir, "coord"), 0o755); err != nil {
		t.Fatalf("MkdirAll coord: %v", err)
	}
	if err := os.WriteFile(filepath.Join(r.GraftDir, "coord", "agent-id"), []byte("agent-parent"), 0o644); err != nil {
		t.Fatalf("write agent-id: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	result, err := coordd.AuthorizeSpawn(r, "agent-parent", coordd.SpawnRequest{
		Name:           "trace-child-phase",
		Command:        []string{"printf", "hello"},
		Runtime:        "detached",
		Launch:         "lease",
		BootstrapCoord: true,
	})
	if err != nil {
		t.Fatalf("AuthorizeSpawn: %v", err)
	}

	t.Setenv("GRAFT_COORDD_SPAWN_ID", result.Record.ID)
	input, err := coordd.BuildShellActionInput(r, result.Record.ChildAgentID, []string{"true"})
	if err != nil {
		t.Fatalf("BuildShellActionInput: %v", err)
	}
	decision, err := coordd.EvaluateActionPolicy(input)
	if err != nil {
		t.Fatalf("EvaluateActionPolicy: %v", err)
	}
	if _, err := coordd.ExecuteGuardedWithIO(r, input, decision, coordd.ExecIO{
		Stdout: io.Discard,
		Stderr: io.Discard,
	}); err != nil {
		t.Fatalf("ExecuteGuardedWithIO: %v", err)
	}

	traceAny, err := mcpDispatchAll(false, "graft_spawn_trace", map[string]any{
		"id":                   result.Record.ID,
		"phases":               []any{"execution"},
		"no_default_fallbacks": true,
	})
	if err != nil {
		t.Fatalf("mcpDispatchAll trace: %v", err)
	}
	trace := decodeMCPSpawnTraceView(t, traceAny)
	if trace.SpawnAction != nil || trace.SpawnPolicy != nil {
		t.Fatalf("expected authorization decisions to be filtered out: %#v %#v", trace.SpawnAction, trace.SpawnPolicy)
	}
	if len(trace.Phases) != 1 || trace.Phases[0].Name != "execution" {
		t.Fatalf("trace.Phases = %#v, want only execution", trace.Phases)
	}
	if len(trace.Execs) != 1 || trace.Execs[0].Decision == nil {
		t.Fatalf("trace.Execs = %#v, want one execution trace with decision", trace.Execs)
	}
	if len(trace.Execs[0].Decision.Rules) != 1 || trace.Execs[0].Decision.Rules[0].Rule != "AllowReadOnly" {
		t.Fatalf("exec decision rules = %#v, want only AllowReadOnly", trace.Execs[0].Decision.Rules)
	}
}

func decodeMCPSpawnResult(t *testing.T, value any) mcpSpawnTestResult {
	t.Helper()

	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var result mcpSpawnTestResult
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("json.Unmarshal: %v\nraw: %s", err, string(data))
	}
	return result
}

func decodeMCPSpawnList(t *testing.T, value any) []coordd.SpawnRecord {
	t.Helper()

	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var records []coordd.SpawnRecord
	if err := json.Unmarshal(data, &records); err != nil {
		t.Fatalf("json.Unmarshal: %v\nraw: %s", err, string(data))
	}
	return records
}

func decodeMCPSpawnRecord(t *testing.T, value any) coordd.SpawnRecord {
	t.Helper()

	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var record coordd.SpawnRecord
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatalf("json.Unmarshal: %v\nraw: %s", err, string(data))
	}
	return record
}

func decodeMCPSpawnView(t *testing.T, value any) coordd.SpawnView {
	t.Helper()

	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var view coordd.SpawnView
	if err := json.Unmarshal(data, &view); err != nil {
		t.Fatalf("json.Unmarshal: %v\nraw: %s", err, string(data))
	}
	return view
}

func decodeMCPSpawnTraceView(t *testing.T, value any) coordd.SpawnTraceView {
	t.Helper()

	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var trace coordd.SpawnTraceView
	if err := json.Unmarshal(data, &trace); err != nil {
		t.Fatalf("json.Unmarshal: %v\nraw: %s", err, string(data))
	}
	return trace
}

func waitForMCPSpawnFile(path, needle string, timeout time.Duration) (string, bool) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(filepath.Clean(path))
		if err == nil {
			content := string(data)
			if strings.Contains(content, needle) {
				return content, true
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	data, _ := os.ReadFile(filepath.Clean(path))
	return string(data), false
}
