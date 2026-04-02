package coordd

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/odvcencio/graft/pkg/coord"
	"github.com/odvcencio/graft/pkg/repo"
)

func TestEvaluateSpawnPolicy_BlocksParentProfileEscalation(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	t.Setenv("GRAFT_COORDD_REQUESTED_PROFILE", "read_only")
	t.Setenv("GRAFT_COORDD_SPAWN_ID", "parent-spawn")

	input, err := BuildShellActionInput(r, "agent-parent", []string{"printf", "hello"})
	if err != nil {
		t.Fatalf("BuildShellActionInput: %v", err)
	}
	decision, err := EvaluateActionPolicy(input)
	if err != nil {
		t.Fatalf("EvaluateActionPolicy: %v", err)
	}
	spawnInput := BuildSpawnPolicyInput(input, decision, SpawnRequest{
		Name:             "child-agent",
		Command:          []string{"printf", "hello"},
		RequestedProfile: "repo_write_network",
	})
	spawnDecision, err := EvaluateSpawnPolicy(spawnInput)
	if err != nil {
		t.Fatalf("EvaluateSpawnPolicy: %v", err)
	}
	if spawnDecision.Action != "HardBlock" {
		t.Fatalf("Action = %q, want HardBlock", spawnDecision.Action)
	}
	if spawnDecision.Code != "profile_escalation" {
		t.Fatalf("Code = %q, want profile_escalation", spawnDecision.Code)
	}
}

func TestEvaluateSpawnPolicyWithRepo_LoadsRepoLocalPolicyBundle(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	path := filepath.Join(GuardPoliciesDir(r.GraftDir), "spawn.arb")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(`rule RepoSpawnAdvisory priority 5 {
    when {
        session.active_agent == true
        and spawn.name_present == true
        and spawn.command_present == true
    }
    then Advisory {
        code: "repo_spawn",
        reason: "repo-local spawn policy",
        profile: "read_only",
    }
}

rule Fallback priority 999 {
    when { true }
    then Allow {
        code: "spawn_allowed",
        reason: "fallback",
        profile: "read_only",
    }
}
`), 0o644); err != nil {
		t.Fatalf("WriteFile spawn policy: %v", err)
	}

	input := SpawnPolicyInput{
		Action: SpawnPolicyAction{
			Decision: "Advisory",
			Profile:  "read_only",
			Selector: "shell:noop",
			Advisory: true,
		},
		Repo:    ActionPolicyRepo{Present: true, Root: r.RootDir},
		Session: ActionPolicySession{ActiveAgent: true, AgentID: "agent-1"},
		Spawn: SpawnPolicySpec{
			Name:            "child-agent",
			NamePresent:     true,
			CommandPresent:  true,
			SelectedProfile: "read_only",
			SelectedValid:   true,
			Runtime:         "detached",
			RuntimeValid:    true,
		},
	}

	decision, err := EvaluateSpawnPolicyWithRepo(r, input)
	if err != nil {
		t.Fatalf("EvaluateSpawnPolicyWithRepo: %v", err)
	}
	if decision.Rule != "RepoSpawnAdvisory" {
		t.Fatalf("decision.Rule = %q, want RepoSpawnAdvisory", decision.Rule)
	}
	if decision.Bundle.Embedded {
		t.Fatal("decision.Bundle.Embedded = true, want false")
	}
	if decision.Bundle.Root != path {
		t.Fatalf("decision.Bundle.Root = %q, want %q", decision.Bundle.Root, path)
	}
	if decision.RuleOrigin == nil || filepath.Base(decision.RuleOrigin.File) != "spawn.arb" {
		t.Fatalf("decision.RuleOrigin = %#v, want spawn.arb", decision.RuleOrigin)
	}
}

func TestSpawnDetached_StartsHostDirectAndPersistsRecord(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	if err := SaveGuardConfig(r.GraftDir, &GuardConfig{
		Mode:             "enforce",
		PreferredBackend: "container",
	}); err != nil {
		t.Fatalf("SaveGuardConfig: %v", err)
	}

	result, err := SpawnDetached(r, "agent-parent", SpawnRequest{
		Name:    "child-agent",
		Command: []string{"printf", "hello"},
		Runtime: "detached",
	})
	if err != nil {
		t.Fatalf("SpawnDetached: %v", err)
	}
	if result.Record == nil {
		t.Fatal("Record = nil, want persisted spawn record")
	}
	if result.Record.Backend != "host-direct" && result.Record.Backend != "host-bwrap" {
		t.Fatalf("Record.Backend = %q, want detached host backend", result.Record.Backend)
	}
	if result.Record.RequestedRuntime != "detached" {
		t.Fatalf("Record.RequestedRuntime = %q, want detached", result.Record.RequestedRuntime)
	}
	if result.Record.PID == 0 {
		t.Fatal("Record.PID = 0, want child pid")
	}
	if result.Record.StdoutPath == "" {
		t.Fatal("Record.StdoutPath = empty, want stdout log path")
	}

	if got, ok := waitForFileContains(result.Record.StdoutPath, "hello", 2*time.Second); !ok {
		t.Fatalf("stdout log missing child output: %q", got)
	}

	loaded, err := LoadSpawnRecord(r.GraftDir, result.Record.ID)
	if err != nil {
		t.Fatalf("LoadSpawnRecord: %v", err)
	}
	if loaded == nil {
		t.Fatal("loaded record = nil, want spawn record")
	}
	if loaded.Name != "child-agent" {
		t.Fatalf("loaded.Name = %q, want child-agent", loaded.Name)
	}

	events, err := ListEvents(r.GraftDir, 2)
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("expected spawn events")
	}
	if events[len(events)-1].Type != "spawn_started" {
		t.Fatalf("last event = %q, want spawn_started", events[len(events)-1].Type)
	}
}

func TestEvaluateSpawnPolicy_BlocksInvalidRuntime(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	input, err := BuildShellActionInput(r, "agent-parent", []string{"printf", "hello"})
	if err != nil {
		t.Fatalf("BuildShellActionInput: %v", err)
	}
	decision, err := EvaluateActionPolicy(input)
	if err != nil {
		t.Fatalf("EvaluateActionPolicy: %v", err)
	}
	spawnInput := BuildSpawnPolicyInput(input, decision, SpawnRequest{
		Name:    "child-agent",
		Command: []string{"printf", "hello"},
		Runtime: "sidecar",
	})
	spawnDecision, err := EvaluateSpawnPolicy(spawnInput)
	if err != nil {
		t.Fatalf("EvaluateSpawnPolicy: %v", err)
	}
	if spawnDecision.Action != "HardBlock" {
		t.Fatalf("Action = %q, want HardBlock", spawnDecision.Action)
	}
	if spawnDecision.Code != "invalid_runtime" {
		t.Fatalf("Code = %q, want invalid_runtime", spawnDecision.Code)
	}
}

func TestAuthorizeSpawn_LeaseHeartbeatAndFinish(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	if err := SaveGuardConfig(r.GraftDir, &GuardConfig{
		Mode:             "enforce",
		PreferredBackend: "container",
	}); err != nil {
		t.Fatalf("SaveGuardConfig: %v", err)
	}

	result, err := AuthorizeSpawn(r, "agent-parent", SpawnRequest{
		Name:           "child-agent",
		Command:        []string{"printf", "hello"},
		Runtime:        "detached",
		Launch:         "lease",
		BootstrapCoord: true,
	})
	if err != nil {
		t.Fatalf("AuthorizeSpawn: %v", err)
	}
	if result.Record == nil {
		t.Fatal("Record = nil, want authorized record")
	}
	if result.Record.LaunchMode != "lease" {
		t.Fatalf("LaunchMode = %q, want lease", result.Record.LaunchMode)
	}
	if !result.Record.BootstrapCoord {
		t.Fatal("BootstrapCoord = false, want true")
	}
	if result.Record.Status != "authorized" {
		t.Fatalf("Status = %q, want authorized", result.Record.Status)
	}
	if strings.TrimSpace(result.Record.ChildAgentID) == "" {
		t.Fatal("ChildAgentID = empty, want bootstrapped coord agent")
	}
	if strings.TrimSpace(result.Record.ChildAgentName) == "" {
		t.Fatal("ChildAgentName = empty, want bootstrapped coord session name")
	}
	if session, err := coord.LoadSession(r.GraftDir, result.Record.ChildAgentName); err != nil || session == nil {
		t.Fatalf("LoadSession(%q): session missing (%v)", result.Record.ChildAgentName, err)
	}
	if agent, err := coord.New(r, coord.DefaultConfig).GetAgent(result.Record.ChildAgentID); err != nil || agent == nil {
		t.Fatalf("GetAgent(%q): missing bootstrapped agent (%v)", result.Record.ChildAgentID, err)
	}
	if result.Record.PID != 0 || result.Record.ContainerID != "" {
		t.Fatalf("expected no launched runtime, got pid=%d container=%q", result.Record.PID, result.Record.ContainerID)
	}
	childAgentID := result.Record.ChildAgentID
	childAgentName := result.Record.ChildAgentName

	record, err := TouchSpawn(r.GraftDir, result.Record.ID, "child-subagent")
	if err != nil {
		t.Fatalf("TouchSpawn: %v", err)
	}
	if record.Status != "active" {
		t.Fatalf("Status after heartbeat = %q, want active", record.Status)
	}
	if record.ChildAgentID != childAgentID {
		t.Fatalf("ChildAgentID = %q, want preserved bootstrapped id %q", record.ChildAgentID, childAgentID)
	}

	record, err = FinishSpawn(r.GraftDir, result.Record.ID, "completed", "child-subagent")
	if err != nil {
		t.Fatalf("FinishSpawn: %v", err)
	}
	if record.Status != "completed" {
		t.Fatalf("final Status = %q, want completed", record.Status)
	}
	if record.FinishedAt.IsZero() {
		t.Fatal("FinishedAt is zero, want terminal timestamp")
	}
	if session, err := coord.LoadSession(r.GraftDir, childAgentName); err != nil || session != nil {
		t.Fatalf("expected bootstrapped session cleanup, got session=%#v err=%v", session, err)
	}
	if _, err := coord.New(r, coord.DefaultConfig).GetAgent(childAgentID); err == nil {
		t.Fatalf("expected child agent %q to be deregistered", childAgentID)
	}
}

func TestLoadSpawnViewAndWaitSpawn(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	if err := SaveGuardConfig(r.GraftDir, &GuardConfig{
		Mode:             "enforce",
		PreferredBackend: "container",
	}); err != nil {
		t.Fatalf("SaveGuardConfig: %v", err)
	}

	result, err := AuthorizeSpawn(r, "agent-parent", SpawnRequest{
		Name:           "lease-child",
		Command:        []string{"printf", "hello"},
		Runtime:        "detached",
		Launch:         "lease",
		BootstrapCoord: true,
	})
	if err != nil {
		t.Fatalf("AuthorizeSpawn: %v", err)
	}

	view, err := LoadSpawnView(r.GraftDir, result.Record.ID)
	if err != nil {
		t.Fatalf("LoadSpawnView: %v", err)
	}
	if view == nil || view.Record == nil || view.Lease == nil {
		t.Fatalf("LoadSpawnView returned incomplete view: %#v", view)
	}
	if view.Lease.ChildAgentID != result.Record.ChildAgentID {
		t.Fatalf("Lease.ChildAgentID = %q, want %q", view.Lease.ChildAgentID, result.Record.ChildAgentID)
	}
	if view.Lease.Env["GRAFT_COORD_AGENT_ID"] != result.Record.ChildAgentID {
		t.Fatalf("lease env child agent = %q, want %q", view.Lease.Env["GRAFT_COORD_AGENT_ID"], result.Record.ChildAgentID)
	}

	go func() {
		time.Sleep(50 * time.Millisecond)
		_, _ = FinishSpawn(r.GraftDir, result.Record.ID, "completed", "")
	}()

	record, err := WaitSpawn(r.GraftDir, result.Record.ID, 2*time.Second, 20*time.Millisecond)
	if err != nil {
		t.Fatalf("WaitSpawn: %v", err)
	}
	if record.Status != "completed" {
		t.Fatalf("WaitSpawn status = %q, want completed", record.Status)
	}
}

func TestConsumeSpawn_ActivatesBoundTask(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	if err := SaveGuardConfig(r.GraftDir, &GuardConfig{
		Mode:             "enforce",
		PreferredBackend: "container",
	}); err != nil {
		t.Fatalf("SaveGuardConfig: %v", err)
	}

	c := coord.New(r, coord.DefaultConfig)
	task := &coord.Task{Title: "Investigate lease flow"}
	if err := c.CreateTask(task); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	result, err := AuthorizeSpawn(r, "agent-parent", SpawnRequest{
		Name:           "lease-child",
		Command:        []string{"printf", "hello"},
		Runtime:        "detached",
		Launch:         "lease",
		BootstrapCoord: true,
		TaskID:         task.ID,
	})
	if err != nil {
		t.Fatalf("AuthorizeSpawn: %v", err)
	}

	view, err := ConsumeSpawn(r.GraftDir, result.Record.ID, "")
	if err != nil {
		t.Fatalf("ConsumeSpawn: %v", err)
	}
	if view == nil || view.Record == nil || view.Lease == nil {
		t.Fatalf("ConsumeSpawn returned incomplete view: %#v", view)
	}
	if view.Record.Status != "active" {
		t.Fatalf("record.Status = %q, want active", view.Record.Status)
	}
	if view.Lease.Env["GRAFT_COORDD_TASK_ID"] != task.ID {
		t.Fatalf("lease env task id = %q, want %q", view.Lease.Env["GRAFT_COORDD_TASK_ID"], task.ID)
	}
	if view.Record.Task == nil || view.Record.Task.Status != "in_progress" {
		t.Fatalf("record.Task = %#v, want in_progress binding", view.Record.Task)
	}
	got, err := c.GetTask(task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Status != "in_progress" {
		t.Fatalf("task.Status = %q, want in_progress", got.Status)
	}
	if got.AssignedTo != result.Record.ChildAgentName {
		t.Fatalf("task.AssignedTo = %q, want %q", got.AssignedTo, result.Record.ChildAgentName)
	}
}

func TestAttachSpawn_CompletesBoundTask(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	if err := SaveGuardConfig(r.GraftDir, &GuardConfig{
		Mode:             "enforce",
		PreferredBackend: "container",
	}); err != nil {
		t.Fatalf("SaveGuardConfig: %v", err)
	}

	c := coord.New(r, coord.DefaultConfig)
	task := &coord.Task{Title: "Run attached child"}
	if err := c.CreateTask(task); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	result, err := AuthorizeSpawn(r, "agent-parent", SpawnRequest{
		Name:           "attach-child",
		Command:        []string{"printf", "hello"},
		Runtime:        "detached",
		Launch:         "lease",
		BootstrapCoord: true,
		TaskID:         task.ID,
	})
	if err != nil {
		t.Fatalf("AuthorizeSpawn: %v", err)
	}

	record, err := AttachSpawn(r, result.Record.ID, 10*time.Millisecond, ExecIO{
		Stdout: io.Discard,
		Stderr: io.Discard,
	})
	if err != nil {
		t.Fatalf("AttachSpawn: %v", err)
	}
	if record.Status != "completed" {
		t.Fatalf("record.Status = %q, want completed", record.Status)
	}
	if record.Task == nil || record.Task.Status != "completed" {
		t.Fatalf("record.Task = %#v, want completed binding", record.Task)
	}
	got, err := c.GetTask(task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Status != "completed" {
		t.Fatalf("task.Status = %q, want completed", got.Status)
	}
	if got.AssignedTo != result.Record.ChildAgentName {
		t.Fatalf("task.AssignedTo = %q, want %q", got.AssignedTo, result.Record.ChildAgentName)
	}
}

func waitForFileContains(path, needle string, timeout time.Duration) (string, bool) {
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
