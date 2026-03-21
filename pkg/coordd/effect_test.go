package coordd

import (
	"testing"
)

func TestEvaluateEffects_GitCommit(t *testing.T) {
	input := ActionPolicyInput{
		Action: ActionPolicyAction{
			Program:    "git",
			Subcommand: "commit",
		},
		Coord: ActionPolicyCoord{Active: true, AgentID: "abc"},
	}
	result := ExecResult{ExitCode: 0}

	effects, err := EvaluateEffects(input, result)
	if err != nil {
		t.Fatal(err)
	}

	handlers := make(map[string]bool)
	for _, e := range effects {
		handlers[e.Handler] = true
	}

	if !handlers["publish_commit_to_feed"] {
		t.Error("expected publish_commit_to_feed effect")
	}
	if !handlers["refresh_heartbeat"] {
		t.Error("expected refresh_heartbeat effect")
	}
}

func TestEvaluateEffects_NonZeroExit(t *testing.T) {
	input := ActionPolicyInput{
		Action: ActionPolicyAction{
			Program:    "git",
			Subcommand: "commit",
		},
		Coord: ActionPolicyCoord{Active: true},
	}
	result := ExecResult{ExitCode: 1}

	effects, err := EvaluateEffects(input, result)
	if err != nil {
		t.Fatal(err)
	}
	if len(effects) != 0 {
		t.Errorf("got %d effects, want 0 for non-zero exit", len(effects))
	}
}

func TestEvaluateEffects_ReadOnlyRegistersPresence(t *testing.T) {
	input := ActionPolicyInput{
		Action: ActionPolicyAction{
			Program:          "git",
			Subcommand:       "status",
			WritesFilesystem: false,
		},
		Coord: ActionPolicyCoord{
			Active:       true,
			AgentID:      "abc",
			FilesTouched: []string{"pkg/foo.go"},
		},
	}
	result := ExecResult{ExitCode: 0}

	effects, err := EvaluateEffects(input, result)
	if err != nil {
		t.Fatal(err)
	}

	handlers := make(map[string]bool)
	for _, e := range effects {
		handlers[e.Handler] = true
	}

	if !handlers["register_presence"] {
		t.Error("expected register_presence effect")
	}
	if !handlers["refresh_heartbeat"] {
		t.Error("expected refresh_heartbeat effect (fires on any success)")
	}
}

func TestEvaluateEffects_InactiveAgent_NoEffects(t *testing.T) {
	input := ActionPolicyInput{
		Action: ActionPolicyAction{
			Program:    "git",
			Subcommand: "status",
		},
		Coord: ActionPolicyCoord{Active: false},
	}
	result := ExecResult{ExitCode: 0}

	effects, err := EvaluateEffects(input, result)
	if err != nil {
		t.Fatal(err)
	}
	// RefreshHeartbeat requires coord.active == true, so it won't fire.
	// RegisterPresence requires coord.active == true, so it won't fire.
	// PublishGitCommit needs "commit" subcommand, not "status".
	// For git status with inactive agent, no effects should fire.
	if len(effects) != 0 {
		t.Errorf("got %d effects, want 0 for inactive agent with status", len(effects))
	}
}
