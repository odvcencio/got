package coord

import "testing"

func TestEvaluateClaimPolicy_Allow(t *testing.T) {
	decision, err := EvaluateClaimPolicy(ClaimPolicyInput{
		Attempt: ClaimPolicyAttempt{Mode: ClaimEditing},
		Repo:    ClaimPolicyRepo{ConflictMode: "advisory"},
		Entity:  ClaimPolicyEntity{Key: "decl:function_definition::Foo:func Foo():0", File: "foo.go"},
	})
	if err != nil {
		t.Fatalf("EvaluateClaimPolicy: %v", err)
	}
	if decision.Action != "Allow" {
		t.Fatalf("decision action = %q, want Allow", decision.Action)
	}
	if decision.Rule != "DefaultAllow" {
		t.Fatalf("decision rule = %q, want DefaultAllow", decision.Rule)
	}
}

func TestEvaluateClaimPolicy_ProtectedEntity(t *testing.T) {
	decision, err := EvaluateClaimPolicy(ClaimPolicyInput{
		Attempt: ClaimPolicyAttempt{Mode: ClaimEditing},
		Repo:    ClaimPolicyRepo{ConflictMode: "advisory"},
		Entity: ClaimPolicyEntity{
			Key:       "decl:function_definition::MergeFiles:func MergeFiles():0",
			File:      "merge.go",
			Protected: true,
		},
	})
	if err != nil {
		t.Fatalf("EvaluateClaimPolicy: %v", err)
	}
	if decision.Action != "HardBlock" {
		t.Fatalf("decision action = %q, want HardBlock", decision.Action)
	}
	if decision.Code != "protected_entity" {
		t.Fatalf("decision code = %q, want protected_entity", decision.Code)
	}
}

func TestEvaluateClaimPolicy_ConflictModes(t *testing.T) {
	tests := []struct {
		name         string
		conflictMode string
		wantAction   string
		wantForce    bool
	}{
		{name: "advisory", conflictMode: "advisory", wantAction: "Advisory", wantForce: false},
		{name: "soft block", conflictMode: "soft_block", wantAction: "SoftBlock", wantForce: true},
		{name: "hard block", conflictMode: "hard_block", wantAction: "HardBlock", wantForce: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision, err := EvaluateClaimPolicy(ClaimPolicyInput{
				Attempt: ClaimPolicyAttempt{Mode: ClaimEditing},
				Repo:    ClaimPolicyRepo{ConflictMode: tt.conflictMode},
				Entity:  ClaimPolicyEntity{Key: "decl:function_definition::Foo:func Foo():0", File: "foo.go"},
				ExistingClaim: ClaimPolicyExistingClaim{
					Exists:    true,
					SameAgent: false,
					Mode:      ClaimEditing,
					HeldBy:    "agent-b",
					HeldByID:  "b",
				},
				Owner: ClaimPolicyOwner{
					Alive: true,
				},
			})
			if err != nil {
				t.Fatalf("EvaluateClaimPolicy: %v", err)
			}
			if decision.Action != tt.wantAction {
				t.Fatalf("decision action = %q, want %q", decision.Action, tt.wantAction)
			}
			if decision.RequireForce != tt.wantForce {
				t.Fatalf("decision require_force = %v, want %v", decision.RequireForce, tt.wantForce)
			}
		})
	}
}

func TestAcquireClaimConflictIncludesPolicyDecision(t *testing.T) {
	c := newTestCoordinator(t)
	c.Config.ConflictMode = "soft_block"

	id1, err := c.RegisterAgent(AgentInfo{Name: "agent-a", Workspace: "graft", Host: "test"})
	if err != nil {
		t.Fatalf("RegisterAgent agent-a: %v", err)
	}
	id2, err := c.RegisterAgent(AgentInfo{Name: "agent-b", Workspace: "graft", Host: "test"})
	if err != nil {
		t.Fatalf("RegisterAgent agent-b: %v", err)
	}

	req := ClaimRequest{
		EntityKey: "decl:function_definition::Foo:func Foo():0",
		File:      "foo.go",
		Mode:      ClaimEditing,
	}
	if err := c.AcquireClaim(id1, req); err != nil {
		t.Fatalf("AcquireClaim agent-a: %v", err)
	}

	err = c.AcquireClaim(id2, req)
	conflict, ok := err.(*ClaimConflictError)
	if !ok {
		t.Fatalf("expected ClaimConflictError, got %T: %v", err, err)
	}
	if conflict.Decision == nil {
		t.Fatal("expected policy decision on conflict")
	}
	if conflict.Decision.Action != "SoftBlock" {
		t.Fatalf("conflict decision = %q, want SoftBlock", conflict.Decision.Action)
	}
	if !conflict.Decision.RequireForce {
		t.Fatal("expected soft block to require force")
	}
}
