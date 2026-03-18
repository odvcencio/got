package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/odvcencio/graft/pkg/coordd"
	"github.com/odvcencio/graft/pkg/repo"
)

func TestCoorddSpawnCmd_JSONAndList(t *testing.T) {
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

	output := captureCommandStdout(t, func() error {
		cmd := newCoorddCmd()
		cmd.SilenceUsage = true
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"spawn", "--name", "child-agent", "--json", "--", "printf", "hello"})
		return cmd.Execute()
	})

	var result coordd.SpawnResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("json.Unmarshal: %v\nraw: %s", err, output)
	}
	if result.Record == nil {
		t.Fatal("Record = nil, want spawn record")
	}
	if result.Record.Name != "child-agent" {
		t.Fatalf("Record.Name = %q, want child-agent", result.Record.Name)
	}
	if result.Record.Backend != "host-direct" {
		t.Fatalf("Record.Backend = %q, want host-direct", result.Record.Backend)
	}
	if got, ok := waitForCoorddSpawnFile(result.Record.StdoutPath, "hello", 2*time.Second); !ok {
		t.Fatalf("stdout log missing child output: %q", got)
	}

	listOutput := captureCommandStdout(t, func() error {
		cmd := newCoorddCmd()
		cmd.SilenceUsage = true
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"spawns", "--json"})
		return cmd.Execute()
	})

	var records []coordd.SpawnRecord
	if err := json.Unmarshal([]byte(listOutput), &records); err != nil {
		t.Fatalf("json.Unmarshal spawns: %v\nraw: %s", err, listOutput)
	}
	if len(records) != 1 {
		t.Fatalf("len(records) = %d, want 1", len(records))
	}
	if records[0].Name != "child-agent" {
		t.Fatalf("records[0].Name = %q, want child-agent", records[0].Name)
	}
}

func waitForCoorddSpawnFile(path, needle string, timeout time.Duration) (string, bool) {
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
