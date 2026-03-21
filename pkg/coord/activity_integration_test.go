package coord

import (
	"testing"
)

func TestCoordActivityFlow_EndToEnd(t *testing.T) {
	// Use the existing newTestCoordinator helper
	c := newTestCoordinator(t)

	// Agent 1 joins and claims a file
	id1, err := c.RegisterAgent(AgentInfo{Name: "cedar", Workspace: "graft", Host: "host-1"})
	if err != nil {
		t.Fatalf("RegisterAgent cedar: %v", err)
	}
	err = c.AcquireClaim(id1, ClaimRequest{
		EntityKey: "file:pkg/foo.go",
		File:      "pkg/foo.go",
		Mode:      ClaimEditing,
	})
	if err != nil {
		t.Fatalf("AcquireClaim: %v", err)
	}

	// Agent 2 joins (using same coordinator — shared repo)
	id2, err := c.RegisterAgent(AgentInfo{Name: "maple", Workspace: "graft", Host: "host-2"})
	if err != nil {
		t.Fatalf("RegisterAgent maple: %v", err)
	}

	// Verify feed has agent_joined and claim_acquired events
	events, err := c.WalkFeed("", 50)
	if err != nil {
		t.Fatalf("WalkFeed: %v", err)
	}
	eventTypes := make(map[string]int)
	for _, ev := range events {
		eventTypes[ev.Event]++
	}

	if eventTypes["agent_joined"] < 2 {
		t.Errorf("agent_joined count = %d, want >= 2", eventTypes["agent_joined"])
	}
	if eventTypes["claim_acquired"] < 1 {
		t.Errorf("claim_acquired count = %d, want >= 1", eventTypes["claim_acquired"])
	}

	// Verify agents can see each other
	agents, err := c.ListAgents()
	if err != nil {
		t.Fatalf("ListAgents: %v", err)
	}
	if len(agents) != 2 {
		t.Errorf("agents = %d, want 2", len(agents))
	}

	// Verify claims visible
	claims, err := c.ListClaims()
	if err != nil {
		t.Fatalf("ListClaims: %v", err)
	}
	if len(claims) != 1 {
		t.Errorf("claims = %d, want 1", len(claims))
	}
	if claims[0].AgentName != "cedar" {
		t.Errorf("claim agent = %q, want cedar", claims[0].AgentName)
	}

	// Agent 1 releases claim
	err = c.ReleaseClaim(claims[0].EntityKeyHash)
	if err != nil {
		t.Fatalf("ReleaseClaim: %v", err)
	}

	// Verify claim_released in feed
	events, _ = c.WalkFeed("", 50)
	foundRelease := false
	for _, ev := range events {
		if ev.Event == "claim_released" {
			foundRelease = true
			if ev.Detail["file"] != "pkg/foo.go" {
				t.Errorf("released file = %v, want pkg/foo.go", ev.Detail["file"])
			}
		}
	}
	if !foundRelease {
		t.Error("no claim_released event in feed")
	}

	// Agent 2 can now claim the same file
	err = c.AcquireClaim(id2, ClaimRequest{
		EntityKey: "file:pkg/foo.go",
		File:      "pkg/foo.go",
		Mode:      ClaimEditing,
	})
	if err != nil {
		t.Fatalf("AcquireClaim by maple after release: %v", err)
	}

	// Agent 1 deregisters
	c.AgentID = id1
	err = c.DeregisterAgent(id1)
	if err != nil {
		t.Fatalf("DeregisterAgent cedar: %v", err)
	}

	// Final feed walk — verify full event sequence
	events, _ = c.WalkFeed("", 50)
	finalTypes := make(map[string]int)
	for _, ev := range events {
		finalTypes[ev.Event]++
	}

	t.Logf("Final feed event counts: %v", finalTypes)

	// Should have: 2 agent_joined, 2+ claim_acquired, 1+ claim_released, 1 agent_left
	if finalTypes["agent_joined"] < 2 {
		t.Errorf("agent_joined = %d, want >= 2", finalTypes["agent_joined"])
	}
	if finalTypes["claim_acquired"] < 2 {
		t.Errorf("claim_acquired = %d, want >= 2 (cedar + maple)", finalTypes["claim_acquired"])
	}
	if finalTypes["claim_released"] < 1 {
		t.Errorf("claim_released = %d, want >= 1", finalTypes["claim_released"])
	}
	if finalTypes["agent_left"] < 1 {
		t.Errorf("agent_left = %d, want >= 1", finalTypes["agent_left"])
	}

	// Verify all events have Source set
	for _, ev := range events {
		if ev.Source == "" {
			t.Errorf("event %q has empty Source", ev.Event)
		}
	}
}

func TestCoordActivityFlow_PublishToFeedDigest(t *testing.T) {
	c := newTestCoordinator(t)
	_, err := c.RegisterAgent(AgentInfo{Name: "cedar"})
	if err != nil {
		t.Fatal(err)
	}

	// Publish a digest
	digest := &ActivityDigest{
		ToolCalls:    12,
		FilesRead:    []string{"a.go", "b.go", "c.go"},
		FilesWritten: []string{"d.go"},
		ActiveFiles:  []string{"a.go", "d.go"},
		Period:       30,
		Blocked:      1,
		Advisories:   2,
	}
	err = c.PublishDigestToFeed(digest)
	if err != nil {
		t.Fatal(err)
	}

	// Walk feed and verify digest event
	events, _ := c.WalkFeed("", 50)
	var digestEvent *FeedEvent
	for i := range events {
		if events[i].Event == "activity_digest" {
			digestEvent = &events[i]
			break
		}
	}
	if digestEvent == nil {
		t.Fatal("no activity_digest event found")
	}
	if digestEvent.Source != "mcp" {
		t.Errorf("source = %q, want mcp", digestEvent.Source)
	}
	if digestEvent.Digest == nil {
		t.Fatal("digest payload is nil")
	}
	if digestEvent.Digest.ToolCalls != 12 {
		t.Errorf("ToolCalls = %d, want 12", digestEvent.Digest.ToolCalls)
	}
	if digestEvent.Digest.Blocked != 1 {
		t.Errorf("Blocked = %d, want 1", digestEvent.Digest.Blocked)
	}
}
