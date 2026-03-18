package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

	resultAny, err := mcpDispatchAll(false, "graft_spawn", map[string]any{
		"name":    "child-agent",
		"command": "printf",
		"args":    []any{"hello"},
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
	if result.Result.Record.Backend != "host-direct" {
		t.Fatalf("Result.Record.Backend = %q, want host-direct", result.Result.Record.Backend)
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
