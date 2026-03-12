package coord

import (
	"testing"
	"time"

	"github.com/odvcencio/graft/pkg/repo"
)

func newTestCoordinator(t *testing.T) *Coordinator {
	t.Helper()
	r, err := repo.Init(t.TempDir())
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	return New(r, DefaultConfig)
}

func TestRegisterAgent(t *testing.T) {
	c := newTestCoordinator(t)

	info := AgentInfo{
		Name:      "test-agent",
		Workspace: "graft",
		Host:      "test-host",
	}
	id, err := c.RegisterAgent(info)
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty agent ID")
	}

	agents, err := c.ListAgents()
	if err != nil {
		t.Fatalf("ListAgents: %v", err)
	}
	if len(agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(agents))
	}
	if agents[0].Name != "test-agent" {
		t.Errorf("agent name = %q, want test-agent", agents[0].Name)
	}
}

func TestDeregisterAgent(t *testing.T) {
	c := newTestCoordinator(t)

	info := AgentInfo{Name: "temp-agent", Workspace: "graft", Host: "test"}
	id, err := c.RegisterAgent(info)
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}

	if err := c.DeregisterAgent(id); err != nil {
		t.Fatalf("DeregisterAgent: %v", err)
	}

	agents, err := c.ListAgents()
	if err != nil {
		t.Fatalf("ListAgents: %v", err)
	}
	if len(agents) != 0 {
		t.Fatalf("expected 0 agents after deregister, got %d", len(agents))
	}
}

func TestHeartbeat(t *testing.T) {
	c := newTestCoordinator(t)

	info := AgentInfo{Name: "hb-agent", Workspace: "graft", Host: "test"}
	id, err := c.RegisterAgent(info)
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}

	before, err := c.GetAgent(id)
	if err != nil {
		t.Fatalf("GetAgent: %v", err)
	}

	time.Sleep(10 * time.Millisecond)
	if err := c.Heartbeat(id); err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}

	after, err := c.GetAgent(id)
	if err != nil {
		t.Fatalf("GetAgent: %v", err)
	}

	if !after.HeartbeatAt.After(before.HeartbeatAt) {
		t.Error("heartbeat timestamp did not advance")
	}
}

func TestGCStaleAgents(t *testing.T) {
	c := newTestCoordinator(t)
	c.Config.StaleThreshold = 1 * time.Millisecond

	info := AgentInfo{Name: "stale-agent", Workspace: "graft", Host: "test"}
	_, err := c.RegisterAgent(info)
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}

	time.Sleep(5 * time.Millisecond)

	removed, err := c.GCStaleAgents()
	if err != nil {
		t.Fatalf("GCStaleAgents: %v", err)
	}
	if len(removed) != 1 {
		t.Fatalf("expected 1 removed, got %d", len(removed))
	}

	agents, err := c.ListAgents()
	if err != nil {
		t.Fatalf("ListAgents: %v", err)
	}
	if len(agents) != 0 {
		t.Fatalf("expected 0 agents after GC, got %d", len(agents))
	}
}
