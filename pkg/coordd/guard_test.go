package coordd

import (
	"path/filepath"
	"testing"

	"github.com/odvcencio/graft/pkg/repo"
)

func TestEvaluateActionPolicy_DestructiveRMBlocked(t *testing.T) {
	input := ActionPolicyInput{
		Action: InspectShellAction([]string{"rm", "-rf", "./"}),
		Guard:  GuardPolicy{Mode: "advisory"},
	}

	decision, err := EvaluateActionPolicy(input)
	if err != nil {
		t.Fatalf("EvaluateActionPolicy: %v", err)
	}
	if decision.Action != "HardBlock" {
		t.Fatalf("decision.Action = %q, want HardBlock", decision.Action)
	}
	if decision.Code != "destructive_action" {
		t.Fatalf("decision.Code = %q, want destructive_action", decision.Code)
	}
	if decision.Profile != "blocked" {
		t.Fatalf("decision.Profile = %q, want blocked", decision.Profile)
	}
}

func TestBuildShellActionInput_AllowlistRespected(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	cfg := &GuardConfig{
		Mode:           "enforce",
		AllowedActions: []string{"shell:touch *"},
	}
	if err := SaveGuardConfig(r.GraftDir, cfg); err != nil {
		t.Fatalf("SaveGuardConfig: %v", err)
	}

	input, err := BuildShellActionInput(r, "agent-1", []string{"touch", filepath.Join(dir, "note.txt")})
	if err != nil {
		t.Fatalf("BuildShellActionInput: %v", err)
	}
	if !input.Action.Allowlisted {
		t.Fatal("expected touch action to be allowlisted")
	}

	decision, err := EvaluateActionPolicy(input)
	if err != nil {
		t.Fatalf("EvaluateActionPolicy: %v", err)
	}
	if decision.Action != "Allow" {
		t.Fatalf("decision.Action = %q, want Allow", decision.Action)
	}
	if decision.Profile != "repo_write" {
		t.Fatalf("decision.Profile = %q, want repo_write", decision.Profile)
	}
}

func TestEvaluateActionPolicy_TrueIsReadOnly(t *testing.T) {
	input := ActionPolicyInput{
		Action: InspectShellAction([]string{"true"}),
		Guard:  GuardPolicy{Mode: "advisory"},
	}

	decision, err := EvaluateActionPolicy(input)
	if err != nil {
		t.Fatalf("EvaluateActionPolicy: %v", err)
	}
	if decision.Action != "Allow" {
		t.Fatalf("decision.Action = %q, want Allow", decision.Action)
	}
	if decision.Profile != "read_only" {
		t.Fatalf("decision.Profile = %q, want read_only", decision.Profile)
	}
}
