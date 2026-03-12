package coord

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAnalyzeImpact(t *testing.T) {
	// Set up a provider workspace with a graft repo and export index.
	root := t.TempDir()

	// Provider workspace: has exported functions.
	providerDir := filepath.Join(root, "provider")
	os.MkdirAll(filepath.Join(providerDir, "pkg", "handler"), 0o755)

	os.WriteFile(filepath.Join(providerDir, "go.mod"), []byte(`module github.com/example/provider
go 1.25
`), 0o644)

	os.WriteFile(filepath.Join(providerDir, "pkg", "handler", "handler.go"), []byte(`package handler

func HandleRequest(name string) string {
	return "hello " + name
}
`), 0o644)

	// Create a graft repo in the provider dir.
	r := newTestRepoWithCommit(t, map[string]string{
		"pkg/handler/handler.go": `package handler

func HandleRequest(name string) string {
	return "hello " + name
}
`,
	})

	// Consumer workspace: imports and calls provider functions.
	consumerDir := filepath.Join(root, "consumer")
	os.MkdirAll(consumerDir, 0o755)
	os.WriteFile(filepath.Join(consumerDir, "go.mod"), []byte(`module github.com/example/consumer
go 1.25
require github.com/example/provider v1.0.0
`), 0o644)

	os.WriteFile(filepath.Join(consumerDir, "main.go"), []byte(`package main

import (
	"github.com/example/provider/pkg/handler"
)

func process() {
	handler.HandleRequest("test")
}
`), 0o644)

	// Set up coordinator with the provider repo.
	c := New(r, DefaultConfig)

	// Build and save export index.
	exportIdx, err := BuildExportIndex(r)
	if err != nil {
		t.Fatalf("BuildExportIndex: %v", err)
	}
	if err := c.SaveExportIndex(exportIdx); err != nil {
		t.Fatalf("SaveExportIndex: %v", err)
	}

	// We need a go.mod in the provider repo root for module path resolution.
	os.WriteFile(filepath.Join(r.RootDir, "go.mod"), []byte(`module github.com/example/provider
go 1.25
`), 0o644)

	// Define workspaces. Map the provider workspace to the repo root.
	workspaces := map[string]string{
		"provider": r.RootDir,
		"consumer": consumerDir,
	}

	// Simulate a change to HandleRequest.
	changes := []EntityChange{
		{
			Key:    "func:HandleRequest",
			File:   "pkg/handler/handler.go",
			Change: "signature_changed",
		},
	}

	report, err := c.AnalyzeImpact(changes, workspaces)
	if err != nil {
		t.Fatalf("AnalyzeImpact: %v", err)
	}

	// Consumer should be affected.
	consumerImpact, ok := report.Workspaces["consumer"]
	if !ok {
		t.Fatal("expected consumer workspace to be impacted")
	}

	if len(consumerImpact.Callers) == 0 {
		t.Error("expected at least one caller in consumer workspace")
	}

	// Verify the caller is from main.go:process
	foundCaller := false
	for _, caller := range consumerImpact.Callers {
		if caller == "main.go:process:8" {
			foundCaller = true
		}
	}
	if !foundCaller {
		t.Errorf("expected caller main.go:process:8, got %v", consumerImpact.Callers)
	}
}

func TestAnalyzeImpact_NoExportedChanges(t *testing.T) {
	root := t.TempDir()

	r := newTestRepoWithCommit(t, map[string]string{
		"internal.go": `package main

func helperFunc() {}
`,
	})

	// Write go.mod for the repo so workspace graph can be built.
	os.WriteFile(filepath.Join(r.RootDir, "go.mod"), []byte(`module github.com/example/myrepo
go 1.25
`), 0o644)

	// Create a consumer workspace that depends on myrepo.
	consumerDir := filepath.Join(root, "consumer")
	os.MkdirAll(consumerDir, 0o755)
	os.WriteFile(filepath.Join(consumerDir, "go.mod"), []byte(`module github.com/example/consumer
go 1.25
require github.com/example/myrepo v1.0.0
`), 0o644)

	c := New(r, DefaultConfig)

	exportIdx, err := BuildExportIndex(r)
	if err != nil {
		t.Fatalf("BuildExportIndex: %v", err)
	}
	c.SaveExportIndex(exportIdx)

	changes := []EntityChange{
		{
			Key:    "func:helperFunc",
			File:   "internal.go",
			Change: "body_changed",
		},
	}

	report, err := c.AnalyzeImpact(changes, map[string]string{
		"myrepo":   r.RootDir,
		"consumer": consumerDir,
	})
	if err != nil {
		t.Fatalf("AnalyzeImpact: %v", err)
	}

	if len(report.Workspaces) != 0 {
		t.Errorf("expected no workspace impacts for unexported change, got %d", len(report.Workspaces))
	}
}

func TestAnalyzeImpact_EmptyChanges(t *testing.T) {
	c := newTestCoordinator(t)

	report, err := c.AnalyzeImpact(nil, nil)
	if err != nil {
		t.Fatalf("AnalyzeImpact: %v", err)
	}
	if len(report.Workspaces) != 0 {
		t.Errorf("expected no impacts, got %d", len(report.Workspaces))
	}
}

func TestAnalyzeImpact_WithAffectedAgents(t *testing.T) {
	root := t.TempDir()

	// Provider
	r := newTestRepoWithCommit(t, map[string]string{
		"pkg/api/api.go": `package api

func Serve() error {
	return nil
}
`,
	})

	os.WriteFile(filepath.Join(r.RootDir, "go.mod"), []byte(`module github.com/example/provider
go 1.25
`), 0o644)

	// Consumer with a caller
	consumerDir := filepath.Join(root, "consumer")
	os.MkdirAll(consumerDir, 0o755)
	os.WriteFile(filepath.Join(consumerDir, "go.mod"), []byte(`module github.com/example/consumer
go 1.25
require github.com/example/provider v1.0.0
`), 0o644)

	os.WriteFile(filepath.Join(consumerDir, "server.go"), []byte(`package main

import "github.com/example/provider/pkg/api"

func StartServer() {
	api.Serve()
}
`), 0o644)

	c := New(r, DefaultConfig)

	// Register an agent and claim the caller entity.
	agentID, err := c.RegisterAgent(AgentInfo{
		Name:      "consumer-agent",
		Workspace: "consumer",
		Host:      "test",
	})
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}

	err = c.AcquireClaim(agentID, ClaimRequest{
		EntityKey: "func:StartServer",
		File:      "server.go",
		Mode:      ClaimEditing,
	})
	if err != nil {
		t.Fatalf("AcquireClaim: %v", err)
	}

	// Build and save export index.
	exportIdx, err := BuildExportIndex(r)
	if err != nil {
		t.Fatalf("BuildExportIndex: %v", err)
	}
	c.SaveExportIndex(exportIdx)

	workspaces := map[string]string{
		"provider": r.RootDir,
		"consumer": consumerDir,
	}

	changes := []EntityChange{
		{
			Key:    "func:Serve",
			File:   "pkg/api/api.go",
			Change: "signature_changed",
		},
	}

	report, err := c.AnalyzeImpact(changes, workspaces)
	if err != nil {
		t.Fatalf("AnalyzeImpact: %v", err)
	}

	impact, ok := report.Workspaces["consumer"]
	if !ok {
		t.Fatal("expected consumer in impact report")
	}

	if len(impact.AgentsAffected) == 0 {
		t.Error("expected affected agents")
	}

	foundAgent := false
	for _, a := range impact.AgentsAffected {
		if a == "consumer-agent" {
			foundAgent = true
		}
	}
	if !foundAgent {
		t.Errorf("expected consumer-agent in affected agents, got %v", impact.AgentsAffected)
	}
}
