package coord

import (
	"testing"
	"time"
)

func TestRegisterAndListPresence(t *testing.T) {
	c := newTestCoordinator(t)

	id, err := c.RegisterAgent(AgentInfo{Name: "reader", Workspace: "graft", Host: "test"})
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}
	_ = id

	if err := c.RegisterPresence("pkg/coord/agent.go", ""); err != nil {
		t.Fatalf("RegisterPresence: %v", err)
	}

	entries, err := c.ListPresence()
	if err != nil {
		t.Fatalf("ListPresence: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].File != "pkg/coord/agent.go" {
		t.Errorf("File = %q, want pkg/coord/agent.go", entries[0].File)
	}
	if entries[0].AgentName != "reader" {
		t.Errorf("AgentName = %q, want reader", entries[0].AgentName)
	}
}

func TestPresenceWithEntity(t *testing.T) {
	c := newTestCoordinator(t)

	_, err := c.RegisterAgent(AgentInfo{Name: "entity-reader", Workspace: "graft", Host: "test"})
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}

	if err := c.RegisterPresence("pkg/coord/claim.go", "decl:function_definition::AcquireClaim"); err != nil {
		t.Fatalf("RegisterPresence: %v", err)
	}

	entries, err := c.ListPresence()
	if err != nil {
		t.Fatalf("ListPresence: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Entity != "decl:function_definition::AcquireClaim" {
		t.Errorf("Entity = %q, want decl:function_definition::AcquireClaim", entries[0].Entity)
	}
}

func TestPresenceExpiry(t *testing.T) {
	c := newTestCoordinator(t)

	_, err := c.RegisterAgent(AgentInfo{Name: "expiring", Workspace: "graft", Host: "test"})
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}

	if err := c.RegisterPresence("main.go", ""); err != nil {
		t.Fatalf("RegisterPresence: %v", err)
	}

	// With a very short TTL, the entry should be expired
	entries, err := ListPresenceEntries(c.Repo.GraftDir, 1*time.Millisecond)
	if err != nil {
		t.Fatalf("ListPresenceEntries: %v", err)
	}

	// Sleep briefly to ensure the entry is older than 1ms
	time.Sleep(5 * time.Millisecond)

	entries, err = ListPresenceEntries(c.Repo.GraftDir, 1*time.Millisecond)
	if err != nil {
		t.Fatalf("ListPresenceEntries: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries after expiry, got %d", len(entries))
	}
}

func TestPresenceMultipleAgents(t *testing.T) {
	c := newTestCoordinator(t)

	// Register two agents
	id1, err := c.RegisterAgent(AgentInfo{Name: "agent-a", Workspace: "graft", Host: "test"})
	if err != nil {
		t.Fatalf("RegisterAgent a: %v", err)
	}

	// Register presence for agent A
	if err := c.RegisterPresence("shared.go", ""); err != nil {
		t.Fatalf("RegisterPresence agent-a: %v", err)
	}

	// Create a second coordinator for agent B
	c2 := New(c.Repo, DefaultConfig)
	id2, err := c2.RegisterAgent(AgentInfo{Name: "agent-b", Workspace: "graft", Host: "test"})
	if err != nil {
		t.Fatalf("RegisterAgent b: %v", err)
	}
	_ = id1
	_ = id2

	if err := c2.RegisterPresence("shared.go", ""); err != nil {
		t.Fatalf("RegisterPresence agent-b: %v", err)
	}

	// Both should show up
	entries, err := c.ListPresence()
	if err != nil {
		t.Fatalf("ListPresence: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	names := map[string]bool{}
	for _, e := range entries {
		names[e.AgentName] = true
	}
	if !names["agent-a"] || !names["agent-b"] {
		t.Errorf("expected agent-a and agent-b, got %v", names)
	}
}

func TestClearPresence(t *testing.T) {
	c := newTestCoordinator(t)

	_, err := c.RegisterAgent(AgentInfo{Name: "clearer", Workspace: "graft", Host: "test"})
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}

	if err := c.RegisterPresence("file1.go", ""); err != nil {
		t.Fatalf("RegisterPresence 1: %v", err)
	}
	if err := c.RegisterPresence("file2.go", ""); err != nil {
		t.Fatalf("RegisterPresence 2: %v", err)
	}

	entries, err := c.ListPresence()
	if err != nil {
		t.Fatalf("ListPresence: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries before clear, got %d", len(entries))
	}

	if err := c.ClearPresence(); err != nil {
		t.Fatalf("ClearPresence: %v", err)
	}

	entries, err = c.ListPresence()
	if err != nil {
		t.Fatalf("ListPresence: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries after clear, got %d", len(entries))
	}
}

func TestClearPresence_OnlyOwn(t *testing.T) {
	c := newTestCoordinator(t)

	_, err := c.RegisterAgent(AgentInfo{Name: "owner", Workspace: "graft", Host: "test"})
	if err != nil {
		t.Fatalf("RegisterAgent owner: %v", err)
	}
	if err := c.RegisterPresence("mine.go", ""); err != nil {
		t.Fatalf("RegisterPresence: %v", err)
	}

	// Second agent
	c2 := New(c.Repo, DefaultConfig)
	_, err = c2.RegisterAgent(AgentInfo{Name: "other", Workspace: "graft", Host: "test"})
	if err != nil {
		t.Fatalf("RegisterAgent other: %v", err)
	}
	if err := c2.RegisterPresence("theirs.go", ""); err != nil {
		t.Fatalf("RegisterPresence other: %v", err)
	}

	// Clear only the first agent's presence
	if err := c.ClearPresence(); err != nil {
		t.Fatalf("ClearPresence: %v", err)
	}

	entries, err := c.ListPresence()
	if err != nil {
		t.Fatalf("ListPresence: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry after clearing own, got %d", len(entries))
	}
	if entries[0].AgentName != "other" {
		t.Errorf("remaining entry agent = %q, want other", entries[0].AgentName)
	}
}

func TestRegisterPresence_NoAgent(t *testing.T) {
	c := newTestCoordinator(t)

	err := c.RegisterPresence("file.go", "")
	if err == nil {
		t.Fatal("expected error when no agent is registered")
	}
}

func TestClearPresence_NoAgent(t *testing.T) {
	c := newTestCoordinator(t)

	err := c.ClearPresence()
	if err == nil {
		t.Fatal("expected error when no agent is registered")
	}
}
