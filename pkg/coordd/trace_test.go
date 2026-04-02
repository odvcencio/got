package coordd

import (
	"io"
	"testing"
	"time"

	"github.com/odvcencio/graft/pkg/repo"
)

func TestExecuteGuarded_SavesExecTraceAndLoadSpawnTrace(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	if err := SaveGuardConfig(r.GraftDir, &GuardConfig{
		Mode:             "enforce",
		PreferredBackend: "host-direct",
	}); err != nil {
		t.Fatalf("SaveGuardConfig: %v", err)
	}

	result, err := AuthorizeSpawn(r, "agent-parent", SpawnRequest{
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
	t.Setenv("GRAFT_COORDD_TASK_ID", "")
	input, err := BuildShellActionInput(r, result.Record.ChildAgentID, []string{"true"})
	if err != nil {
		t.Fatalf("BuildShellActionInput: %v", err)
	}
	decision, err := EvaluateActionPolicy(input)
	if err != nil {
		t.Fatalf("EvaluateActionPolicy: %v", err)
	}
	execResult, err := ExecuteGuardedWithIO(r, input, decision, ExecIO{
		Stdout: io.Discard,
		Stderr: io.Discard,
	})
	if err != nil {
		t.Fatalf("ExecuteGuardedWithIO: %v", err)
	}
	if execResult.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0", execResult.ExitCode)
	}

	execs, err := ListExecTracesBySpawn(r.GraftDir, result.Record.ID, 10)
	if err != nil {
		t.Fatalf("ListExecTracesBySpawn: %v", err)
	}
	if len(execs) == 0 {
		t.Fatal("expected persisted exec trace")
	}
	if execs[0].Result == nil || execs[0].Result.Decision == nil || execs[0].Result.Decision.Rule == "" {
		t.Fatalf("exec trace missing decision: %#v", execs[0])
	}

	trace, err := LoadSpawnTrace(r.GraftDir, result.Record.ID, 10, 20)
	if err != nil {
		t.Fatalf("LoadSpawnTrace: %v", err)
	}
	if trace == nil || trace.Record == nil {
		t.Fatalf("LoadSpawnTrace returned nil trace: %#v", trace)
	}
	if len(trace.Execs) == 0 {
		t.Fatal("expected unified spawn trace to include execs")
	}
	if len(trace.Events) == 0 {
		t.Fatal("expected unified spawn trace to include events")
	}
}

func TestBuildSpawnTraceView_CollapsesHeartbeatsAndFiltersMatchedRules(t *testing.T) {
	now := time.Now().UTC()
	trace := &SpawnTrace{
		Record: &SpawnRecord{
			ID: "spawn-1",
			ActionDecision: &ActionPolicyDecision{
				Action:  "Allow",
				Rule:    "AllowRepoWrite",
				Profile: "repo_write",
				Trace: []ActionPolicyTrace{
					{Rule: "DestructiveAction"},
					{Rule: "AllowRepoWrite", Matched: true, Priority: 910, Action: "Allow"},
					{Rule: "DefaultAllow", Matched: true, Priority: 999, Action: "Allow"},
				},
			},
		},
		Events: []Event{
			{Type: "spawn_authorized", Timestamp: now},
			{Type: "spawn_heartbeat", Timestamp: now.Add(10 * time.Millisecond)},
			{Type: "spawn_heartbeat", Timestamp: now.Add(20 * time.Millisecond)},
			{Type: "spawn_heartbeat", Timestamp: now.Add(30 * time.Millisecond)},
			{Type: "spawn_finished", Timestamp: now.Add(40 * time.Millisecond)},
		},
	}

	view := BuildSpawnTraceView(trace, SpawnTraceViewOptions{
		MatchedOnly:        true,
		CollapseHeartbeats: true,
	})
	if view == nil {
		t.Fatal("BuildSpawnTraceView returned nil")
	}
	if view.CollapsedHeartbeats != 2 {
		t.Fatalf("CollapsedHeartbeats = %d, want 2", view.CollapsedHeartbeats)
	}
	if view.RenderedEventCount != 3 {
		t.Fatalf("RenderedEventCount = %d, want 3", view.RenderedEventCount)
	}
	if view.SpawnAction == nil || len(view.SpawnAction.Rules) != 2 {
		t.Fatalf("SpawnAction.Rules = %#v, want 2 matched rules", view.SpawnAction)
	}
	if len(view.Phases) < 2 {
		t.Fatalf("len(Phases) = %d, want grouped phases", len(view.Phases))
	}
}

func TestBuildSpawnTraceView_FiltersPhasesAndHidesDefaultFallbacks(t *testing.T) {
	now := time.Now().UTC()
	trace := &SpawnTrace{
		Record: &SpawnRecord{
			ID: "spawn-2",
			ActionDecision: &ActionPolicyDecision{
				Action:  "Allow",
				Rule:    "AllowRepoWrite",
				Profile: "repo_write",
				Trace: []ActionPolicyTrace{
					{Rule: "AllowRepoWrite", Matched: true, Priority: 910, Action: "Allow"},
					{Rule: "DefaultAllow", Matched: true, Priority: 999, Action: "Allow", Fallback: true},
				},
			},
		},
		Execs: []ExecTrace{
			{
				ID:        "exec-1",
				CreatedAt: now.Add(25 * time.Millisecond),
				AgentID:   "child-agent",
				Input: ActionPolicyInput{
					Action: ActionPolicyAction{
						Selector: "shell:true",
						Program:  "true",
					},
				},
				Result: &ExecResult{
					ExitCode: 0,
					Backend:  "host-direct",
					Decision: &ActionPolicyDecision{
						Action:  "Allow",
						Rule:    "AllowReadOnly",
						Profile: "read_only",
						Trace: []ActionPolicyTrace{
							{Rule: "AllowReadOnly", Matched: true, Priority: 900, Action: "Allow"},
							{Rule: "DefaultAllow", Matched: true, Priority: 999, Action: "Allow", Fallback: true},
						},
					},
				},
			},
		},
		Events: []Event{
			{Type: "spawn_authorized", Timestamp: now},
			{Type: "spawn_heartbeat", Timestamp: now.Add(10 * time.Millisecond)},
			{Type: "action_exec_started", Timestamp: now.Add(20 * time.Millisecond)},
			{Type: "action_exec_finished", Timestamp: now.Add(30 * time.Millisecond)},
			{Type: "spawn_finished", Timestamp: now.Add(40 * time.Millisecond)},
		},
	}

	view := BuildSpawnTraceView(trace, SpawnTraceViewOptions{
		MatchedOnly:        true,
		CollapseHeartbeats: true,
		Phases:             []string{"execution"},
		NoDefaultFallbacks: true,
	})
	if view == nil {
		t.Fatal("BuildSpawnTraceView returned nil")
	}
	if view.SpawnAction != nil {
		t.Fatalf("SpawnAction = %#v, want nil when authorization phase is filtered out", view.SpawnAction)
	}
	if view.SpawnPolicy != nil {
		t.Fatalf("SpawnPolicy = %#v, want nil when authorization phase is filtered out", view.SpawnPolicy)
	}
	if len(view.Execs) != 1 {
		t.Fatalf("len(Execs) = %d, want 1", len(view.Execs))
	}
	if view.Execs[0].Decision == nil || len(view.Execs[0].Decision.Rules) != 1 {
		t.Fatalf("Exec decision rules = %#v, want 1 non-fallback rule", view.Execs[0].Decision)
	}
	if got := view.Execs[0].Decision.Rules[0].Rule; got != "AllowReadOnly" {
		t.Fatalf("Exec decision rule = %q, want AllowReadOnly", got)
	}
	if len(view.Phases) != 1 || view.Phases[0].Name != "execution" {
		t.Fatalf("Phases = %#v, want only execution", view.Phases)
	}
	if view.RenderedEventCount != 2 {
		t.Fatalf("RenderedEventCount = %d, want 2 execution events", view.RenderedEventCount)
	}
}
