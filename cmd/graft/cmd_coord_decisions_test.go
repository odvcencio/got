package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/odvcencio/graft/pkg/coord"
	"github.com/odvcencio/graft/pkg/repo"
)

func TestCoordDecisionsCmd_JSON(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	if err := coord.SaveDecision(r.GraftDir, &coord.DecisionGraph{
		ID:        "decision-1",
		Version:   1,
		Kind:      "claim_decision",
		Source:    "graft add",
		CreatedAt: time.Date(2026, 3, 18, 10, 0, 0, 0, time.UTC),
		AgentID:   "agent-1",
		EntityKey: "decl:function_definition::Foo:func Foo():0",
		File:      "foo.go",
		Action:    "Allow",
		Rule:      "DefaultAllow",
		Outcome: coord.DecisionOutcome{
			Status:        "claim_acquired",
			ClaimAcquired: true,
		},
	}); err != nil {
		t.Fatalf("coord.SaveDecision: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	output := captureCommandStdout(t, func() error {
		cmd := newCoordCmd()
		cmd.SilenceUsage = true
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"decisions", "--json"})
		return cmd.Execute()
	})

	var decisions []coord.DecisionGraph
	if err := json.Unmarshal([]byte(output), &decisions); err != nil {
		t.Fatalf("json.Unmarshal: %v\nraw: %s", err, output)
	}
	if len(decisions) != 1 {
		t.Fatalf("len(decisions) = %d, want 1", len(decisions))
	}
	if decisions[0].Outcome.Status != "claim_acquired" {
		t.Fatalf("decisions[0].Outcome.Status = %q, want claim_acquired", decisions[0].Outcome.Status)
	}
	if decisions[0].Rule != "DefaultAllow" {
		t.Fatalf("decisions[0].Rule = %q, want DefaultAllow", decisions[0].Rule)
	}
}

func TestCoordCheck_RecordsDecisionTrace(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	c := coord.New(r, coord.DefaultConfig)
	activeID, err := c.RegisterAgent(coord.AgentInfo{Name: "agent-a", Workspace: "graft", Host: "test"})
	if err != nil {
		t.Fatalf("RegisterAgent active: %v", err)
	}
	otherID, err := c.RegisterAgent(coord.AgentInfo{Name: "agent-b", Workspace: "graft", Host: "test"})
	if err != nil {
		t.Fatalf("RegisterAgent other: %v", err)
	}

	req := coord.ClaimRequest{
		EntityKey: "decl:function_definition::Foo:func Foo():0",
		File:      "foo.go",
		Mode:      coord.ClaimEditing,
	}
	if err := c.AcquireClaim(otherID, req); err != nil {
		t.Fatalf("AcquireClaim other: %v", err)
	}

	coordDir := filepath.Join(r.GraftDir, "coord")
	if err := os.MkdirAll(coordDir, 0o755); err != nil {
		t.Fatalf("MkdirAll coord dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(coordDir, "agent-id"), []byte(activeID), 0o644); err != nil {
		t.Fatalf("WriteFile agent-id: %v", err)
	}

	restore := chdirForTest(t, dir)
	defer restore()

	output := captureCommandStdout(t, func() error {
		cmd := newCoordCmd()
		cmd.SilenceUsage = true
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"check", "--json"})
		return cmd.Execute()
	})

	var result struct {
		OK        bool `json:"ok"`
		Conflicts []struct {
			EntityKey string `json:"entity_key"`
			Decision  string `json:"decision"`
		} `json:"conflicts"`
	}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("json.Unmarshal check output: %v\nraw: %s", err, output)
	}
	if result.OK {
		t.Fatal("expected conflict result")
	}
	if len(result.Conflicts) != 1 {
		t.Fatalf("len(conflicts) = %d, want 1", len(result.Conflicts))
	}

	decisions, err := coord.ListDecisions(r.GraftDir, 10)
	if err != nil {
		t.Fatalf("coord.ListDecisions: %v", err)
	}
	if len(decisions) == 0 {
		t.Fatal("expected recorded decision trace")
	}
	if decisions[0].Source != "graft coord check" {
		t.Fatalf("decisions[0].Source = %q, want graft coord check", decisions[0].Source)
	}
	if decisions[0].Outcome.Status != "inspection_reported" {
		t.Fatalf("decisions[0].Outcome.Status = %q, want inspection_reported", decisions[0].Outcome.Status)
	}
}

func captureCommandStdout(t *testing.T, fn func() error) string {
	t.Helper()

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w
	defer func() {
		os.Stdout = oldStdout
	}()

	runErr := fn()
	if err := w.Close(); err != nil {
		t.Fatalf("close stdout pipe: %v", err)
	}

	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("io.ReadAll: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("close stdout reader: %v", err)
	}
	if runErr != nil {
		t.Fatalf("command execute: %v", runErr)
	}
	return string(data)
}
