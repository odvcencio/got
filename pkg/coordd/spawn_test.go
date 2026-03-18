package coordd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func TestSpawnDetached_StartsHostDirectAndPersistsRecord(t *testing.T) {
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

	result, err := SpawnDetached(r, "agent-parent", SpawnRequest{
		Name:    "child-agent",
		Command: []string{"printf", "hello"},
	})
	if err != nil {
		t.Fatalf("SpawnDetached: %v", err)
	}
	if result.Record == nil {
		t.Fatal("Record = nil, want persisted spawn record")
	}
	if result.Record.Backend != "host-direct" {
		t.Fatalf("Record.Backend = %q, want host-direct", result.Record.Backend)
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
