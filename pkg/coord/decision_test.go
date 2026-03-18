package coord

import (
	"testing"
	"time"
)

func TestRecordClaimDecision_SavesGraph(t *testing.T) {
	c := newTestCoordinator(t)

	agentID, err := c.RegisterAgent(AgentInfo{Name: "agent-a", Workspace: "graft", Host: "test"})
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}

	req := ClaimRequest{
		EntityKey: "decl:function_definition::Foo:func Foo():0",
		File:      "foo.go",
		Mode:      ClaimEditing,
	}
	ctx := &ClaimDecisionContext{
		Input: ClaimPolicyInput{
			Attempt: ClaimPolicyAttempt{Mode: ClaimEditing},
			Repo:    ClaimPolicyRepo{ConflictMode: "soft_block"},
			Entity: ClaimPolicyEntity{
				Key:  req.EntityKey,
				File: req.File,
			},
			ExistingClaim: ClaimPolicyExistingClaim{
				Exists:    true,
				SameAgent: false,
				Mode:      ClaimEditing,
				HeldBy:    "agent-b",
				HeldByID:  "agent-b-id",
			},
			Owner: ClaimPolicyOwner{
				Alive: true,
			},
		},
		Existing: &ClaimInfo{
			EntityKey:     req.EntityKey,
			EntityKeyHash: EntityKeyHash(req.EntityKey),
			File:          req.File,
			Agent:         "agent-b-id",
			AgentName:     "agent-b",
			Mode:          ClaimEditing,
			ClaimedAt:     time.Now().UTC(),
		},
		Decision: &ClaimPolicyDecision{
			Action:       "SoftBlock",
			Code:         "editing_conflict",
			Reason:       "active editing claim held by another agent",
			Rule:         "ActiveConflictSoftBlock",
			Priority:     20,
			RequireForce: true,
			Trace: []PolicyRuleTrace{
				{Rule: "ProtectedEntity", Matched: false, FailedAtInstr: 1},
				{Rule: "ActiveConflictSoftBlock", Matched: true, Priority: 20, Action: "SoftBlock"},
			},
		},
	}

	graph, err := c.RecordClaimDecision("graft add", agentID, req, ctx, DecisionOutcome{
		Status:  "soft_blocked",
		Message: "coord: softblock for Foo in foo.go",
	})
	if err != nil {
		t.Fatalf("RecordClaimDecision: %v", err)
	}
	if graph.ID == "" {
		t.Fatal("expected non-empty graph ID")
	}
	if graph.Action != "SoftBlock" {
		t.Fatalf("graph.Action = %q, want SoftBlock", graph.Action)
	}
	if graph.Outcome.Status != "soft_blocked" {
		t.Fatalf("graph.Outcome.Status = %q, want soft_blocked", graph.Outcome.Status)
	}
	if graph.AgentName != "agent-a" {
		t.Fatalf("graph.AgentName = %q, want agent-a", graph.AgentName)
	}

	loaded, err := LoadDecision(c.Repo.GraftDir, graph.ID)
	if err != nil {
		t.Fatalf("LoadDecision: %v", err)
	}
	if loaded == nil {
		t.Fatal("expected persisted decision graph")
	}
	if loaded.Rule != "ActiveConflictSoftBlock" {
		t.Fatalf("loaded.Rule = %q, want ActiveConflictSoftBlock", loaded.Rule)
	}

	foundDecisionNode := false
	foundOutcomeNode := false
	foundSelectedEdge := false
	for _, node := range loaded.Nodes {
		switch node.Type {
		case "decision":
			foundDecisionNode = true
		case "outcome":
			foundOutcomeNode = true
		}
	}
	for _, edge := range loaded.Edges {
		if edge.Type == "selected_by" {
			foundSelectedEdge = true
		}
	}
	if !foundDecisionNode {
		t.Fatal("expected decision node in persisted graph")
	}
	if !foundOutcomeNode {
		t.Fatal("expected outcome node in persisted graph")
	}
	if !foundSelectedEdge {
		t.Fatal("expected selected_by edge in persisted graph")
	}
}

func TestListDecisions_NewestFirstAndLimit(t *testing.T) {
	dir := t.TempDir()

	older := &DecisionGraph{
		ID:        "older",
		Version:   1,
		Kind:      "claim_decision",
		CreatedAt: time.Date(2026, 3, 18, 9, 0, 0, 0, time.UTC),
		Outcome:   DecisionOutcome{Status: "advisory_reported"},
	}
	newer := &DecisionGraph{
		ID:        "newer",
		Version:   1,
		Kind:      "claim_decision",
		CreatedAt: time.Date(2026, 3, 18, 10, 0, 0, 0, time.UTC),
		Outcome:   DecisionOutcome{Status: "claim_acquired"},
	}

	if err := SaveDecision(dir, older); err != nil {
		t.Fatalf("SaveDecision older: %v", err)
	}
	if err := SaveDecision(dir, newer); err != nil {
		t.Fatalf("SaveDecision newer: %v", err)
	}

	decisions, err := ListDecisions(dir, 1)
	if err != nil {
		t.Fatalf("ListDecisions: %v", err)
	}
	if len(decisions) != 1 {
		t.Fatalf("len(decisions) = %d, want 1", len(decisions))
	}
	if decisions[0].ID != "newer" {
		t.Fatalf("decisions[0].ID = %q, want newer", decisions[0].ID)
	}
}
