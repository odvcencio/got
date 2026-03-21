package coordd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/odvcencio/arbiter/overrides"
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

func TestEvaluateActionPolicy_GovernanceTraceIncludesSegment(t *testing.T) {
	input := ActionPolicyInput{
		Action: ActionPolicyAction{
			Kind:           "shell",
			Selector:       "shell:noop",
			Program:        "noop",
			DefaultAllowed: false,
			Allowlisted:    false,
		},
		Guard: GuardPolicy{Mode: "advisory"},
	}

	decision, err := EvaluateActionPolicy(input)
	if err != nil {
		t.Fatalf("EvaluateActionPolicy: %v", err)
	}
	if decision.Action != "Advisory" {
		t.Fatalf("decision.Action = %q, want Advisory", decision.Action)
	}
	if len(decision.Governance) == 0 {
		t.Fatal("expected governance trace")
	}
	found := false
	for _, step := range decision.Governance {
		if step.Check == "segment guard_advisory" && step.Result {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("governance trace = %#v, want successful segment guard_advisory", decision.Governance)
	}
}

func TestEvaluateActionPolicyWithRepo_RuleOverrideKillSwitch(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	store, err := LoadGuardOverrideStore(r.GraftDir)
	if err != nil {
		t.Fatalf("LoadGuardOverrideStore: %v", err)
	}

	input := ActionPolicyInput{
		Action: ActionPolicyAction{
			Kind:           "shell",
			Selector:       "shell:noop",
			Program:        "noop",
			DefaultAllowed: false,
			Allowlisted:    false,
		},
		Repo:    ActionPolicyRepo{Present: true, Root: r.RootDir},
		Session: ActionPolicySession{ActiveAgent: true, AgentID: "agent-1"},
		Guard:   GuardPolicy{Mode: "advisory"},
	}

	before, err := EvaluateActionPolicyWithRepo(r, input)
	if err != nil {
		t.Fatalf("EvaluateActionPolicyWithRepo(before): %v", err)
	}
	if before.Rule != "AdvisoryReadOnly" {
		t.Fatalf("before.Rule = %q, want AdvisoryReadOnly", before.Rule)
	}

	kill := true
	if err := store.SetRule(actionPolicyBundleID, "AdvisoryReadOnly", overrides.RuleOverride{KillSwitch: &kill}); err != nil {
		t.Fatalf("SetRule: %v", err)
	}

	after, err := EvaluateActionPolicyWithRepo(r, input)
	if err != nil {
		t.Fatalf("EvaluateActionPolicyWithRepo(after): %v", err)
	}
	if after.Rule != "AllowReadOnly" {
		t.Fatalf("after.Rule = %q, want AllowReadOnly", after.Rule)
	}
	if after.Action != "Allow" {
		t.Fatalf("after.Action = %q, want Allow", after.Action)
	}
}

func TestEvaluateActionPolicyWithRepo_LoadsRepoLocalPolicyBundleAndOrigins(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	bundleDir := GuardPolicyBundleDir(r.GraftDir, "action")
	if err := os.MkdirAll(bundleDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	segmentsPath := filepath.Join(bundleDir, "segments.arb")
	mainPath := filepath.Join(bundleDir, "main.arb")
	if err := os.WriteFile(segmentsPath, []byte(`segment repo_local_gate {
    guard.mode == "advisory"
}
`), 0o644); err != nil {
		t.Fatalf("WriteFile segments: %v", err)
	}
	if err := os.WriteFile(mainPath, []byte(`include "segments.arb"

rule RepoLocalAdvisory priority 5 {
    when segment repo_local_gate {
        action.selector == "shell:noop"
    }
    then Advisory {
        code: "repo_local",
        reason: "repo-local action policy",
        profile: "read_only",
    }
}

rule Fallback priority 999 {
    when { true }
    then Allow {
        code: "allow",
        reason: "fallback",
        profile: "read_only",
    }
}
`), 0o644); err != nil {
		t.Fatalf("WriteFile main: %v", err)
	}

	input := ActionPolicyInput{
		Action: ActionPolicyAction{
			Kind:           "shell",
			Selector:       "shell:noop",
			Program:        "noop",
			DefaultAllowed: false,
			Allowlisted:    false,
		},
		Repo:    ActionPolicyRepo{Present: true, Root: r.RootDir},
		Session: ActionPolicySession{ActiveAgent: true, AgentID: "agent-1"},
		Guard:   GuardPolicy{Mode: "advisory"},
	}

	decision, err := EvaluateActionPolicyWithRepo(r, input)
	if err != nil {
		t.Fatalf("EvaluateActionPolicyWithRepo: %v", err)
	}
	if decision.Rule != "RepoLocalAdvisory" {
		t.Fatalf("decision.Rule = %q, want RepoLocalAdvisory", decision.Rule)
	}
	if decision.Bundle.Embedded {
		t.Fatal("decision.Bundle.Embedded = true, want false")
	}
	if decision.Bundle.Root != mainPath {
		t.Fatalf("decision.Bundle.Root = %q, want %q", decision.Bundle.Root, mainPath)
	}
	if len(decision.Bundle.Files) != 2 {
		t.Fatalf("len(decision.Bundle.Files) = %d, want 2", len(decision.Bundle.Files))
	}
	if decision.RuleOrigin == nil || filepath.Base(decision.RuleOrigin.File) != "main.arb" {
		t.Fatalf("decision.RuleOrigin = %#v, want main.arb", decision.RuleOrigin)
	}
	if len(decision.Trace) == 0 || decision.Trace[0].Origin == nil {
		t.Fatalf("decision.Trace = %#v, want matched rule origin", decision.Trace)
	}
	foundSegmentOrigin := false
	for _, step := range decision.Governance {
		if step.Check == "segment repo_local_gate" && step.Result && step.Origin != nil && filepath.Base(step.Origin.File) == "segments.arb" {
			foundSegmentOrigin = true
			break
		}
	}
	if !foundSegmentOrigin {
		t.Fatalf("decision.Governance = %#v, want repo_local_gate origin from segments.arb", decision.Governance)
	}
}

func TestEvaluateActionPolicyWithRepo_ReloadsRepoLocalPolicyBundle(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	path := filepath.Join(GuardPoliciesDir(r.GraftDir), "action.arb")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	writePolicy := func(ruleName, reason string, modTime time.Time) {
		t.Helper()
		source := `rule ` + ruleName + ` priority 5 {
    when {
        action.selector == "shell:noop"
    }
    then Advisory {
        code: "repo_local",
        reason: "` + reason + `",
        profile: "read_only",
    }
}

rule Fallback priority 999 {
    when { true }
    then Allow {
        code: "allow",
        reason: "fallback",
        profile: "read_only",
    }
}
`
		if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
			t.Fatalf("WriteFile(%s): %v", ruleName, err)
		}
		if err := os.Chtimes(path, modTime, modTime); err != nil {
			t.Fatalf("Chtimes(%s): %v", ruleName, err)
		}
	}

	input := ActionPolicyInput{
		Action: ActionPolicyAction{
			Kind:           "shell",
			Selector:       "shell:noop",
			Program:        "noop",
			DefaultAllowed: false,
			Allowlisted:    false,
		},
		Repo:    ActionPolicyRepo{Present: true, Root: r.RootDir},
		Session: ActionPolicySession{ActiveAgent: true, AgentID: "agent-1"},
		Guard:   GuardPolicy{Mode: "advisory"},
	}

	now := time.Now().UTC()
	writePolicy("FirstRule", "first policy", now)
	first, err := EvaluateActionPolicyWithRepo(r, input)
	if err != nil {
		t.Fatalf("EvaluateActionPolicyWithRepo(first): %v", err)
	}
	if first.Rule != "FirstRule" {
		t.Fatalf("first.Rule = %q, want FirstRule", first.Rule)
	}

	writePolicy("SecondRule", "second policy", now.Add(2*time.Second))
	second, err := EvaluateActionPolicyWithRepo(r, input)
	if err != nil {
		t.Fatalf("EvaluateActionPolicyWithRepo(second): %v", err)
	}
	if second.Rule != "SecondRule" {
		t.Fatalf("second.Rule = %q, want SecondRule", second.Rule)
	}
	if !strings.Contains(second.Reason, "second policy") {
		t.Fatalf("second.Reason = %q, want updated policy text", second.Reason)
	}
}

func TestEvaluateActionPolicyWithRepo_IgnoresExamplePolicyFiles(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	examplePath := filepath.Join(GuardPoliciesDir(r.GraftDir), "action.example.arb")
	if err := os.MkdirAll(filepath.Dir(examplePath), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(examplePath, []byte(`rule BrokenExample {
    when {
        this is not valid arbiter syntax
    }
    then Advisory {}
}
`), 0o644); err != nil {
		t.Fatalf("WriteFile example: %v", err)
	}

	input := ActionPolicyInput{
		Action: ActionPolicyAction{
			Kind:           "shell",
			Selector:       "shell:noop",
			Program:        "noop",
			DefaultAllowed: false,
			Allowlisted:    false,
		},
		Repo:    ActionPolicyRepo{Present: true, Root: r.RootDir},
		Session: ActionPolicySession{ActiveAgent: true, AgentID: "agent-1"},
		Guard:   GuardPolicy{Mode: "advisory"},
	}

	decision, err := EvaluateActionPolicyWithRepo(r, input)
	if err != nil {
		t.Fatalf("EvaluateActionPolicyWithRepo: %v", err)
	}
	if !decision.Bundle.Embedded {
		t.Fatalf("decision.Bundle = %#v, want embedded defaults", decision.Bundle)
	}
	if decision.Rule != "AdvisoryReadOnly" {
		t.Fatalf("decision.Rule = %q, want AdvisoryReadOnly from embedded defaults", decision.Rule)
	}
}

func TestConflictingEntityWrite_HardBlock(t *testing.T) {
	input := ActionPolicyInput{
		Action: ActionPolicyAction{
			Selector:   "git:add pkg/foo.go",
			Program:    "git",
			Subcommand: "add",
			WritesRepo: true,
		},
		Guard: GuardPolicy{Mode: "advisory"},
		Coord: ActionPolicyCoord{
			Active: true,
			ConflictingClaims: []CoordClaimConflict{
				{AgentID: "other", AgentName: "cedar", File: "pkg/foo.go", Mode: "editing"},
			},
		},
	}
	decision, err := EvaluateActionPolicy(input)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Action != "HardBlock" {
		t.Errorf("decision = %q, want HardBlock", decision.Action)
	}
	if decision.Code != "conflicting_claim" {
		t.Errorf("code = %q, want conflicting_claim", decision.Code)
	}
}

func TestConflictingEntityWrite_Inactive_NoBlock(t *testing.T) {
	input := ActionPolicyInput{
		Action: ActionPolicyAction{
			Selector:   "git:add pkg/foo.go",
			Program:    "git",
			Subcommand: "add",
			WritesRepo: true,
		},
		Guard: GuardPolicy{Mode: "advisory"},
		Coord: ActionPolicyCoord{
			Active: false, // No agent = no coord enforcement
			ConflictingClaims: []CoordClaimConflict{
				{AgentID: "other", AgentName: "cedar", File: "pkg/foo.go"},
			},
		},
	}
	decision, err := EvaluateActionPolicy(input)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Action == "HardBlock" {
		t.Errorf("should not block when coord.active is false")
	}
}

func TestStaleAgentWrite_HardBlock(t *testing.T) {
	input := ActionPolicyInput{
		Action: ActionPolicyAction{
			WritesRepo: true,
		},
		Guard: GuardPolicy{Mode: "advisory"},
		Coord: ActionPolicyCoord{
			Active:           true,
			LastHeartbeatAge: 200, // > 120s threshold
		},
	}
	decision, err := EvaluateActionPolicy(input)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Action != "HardBlock" {
		t.Errorf("decision = %q, want HardBlock", decision.Action)
	}
	if decision.Code != "stale_agent" {
		t.Errorf("code = %q, want stale_agent", decision.Code)
	}
}

func TestPresenceOverlapWrite_Advisory(t *testing.T) {
	input := ActionPolicyInput{
		Action: ActionPolicyAction{
			WritesFilesystem: true,
		},
		Guard: GuardPolicy{Mode: "advisory"},
		Coord: ActionPolicyCoord{
			Active: true,
			PresenceOverlap: []CoordPresenceEntry{
				{AgentID: "other", AgentName: "maple", File: "pkg/foo.go"},
			},
		},
	}
	decision, err := EvaluateActionPolicy(input)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Action != "Advisory" {
		t.Errorf("decision = %q, want Advisory", decision.Action)
	}
	if decision.Code != "presence_overlap" {
		t.Errorf("code = %q, want presence_overlap", decision.Code)
	}
}
