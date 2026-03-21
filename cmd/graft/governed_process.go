package main

import (
	"fmt"
	"strings"

	"github.com/odvcencio/graft/pkg/coord"
	"github.com/odvcencio/graft/pkg/coordd"
	"github.com/odvcencio/graft/pkg/repo"
)

func init() {
	repo.SetExternalProcessExecutor(coorddRepoProcessExecutor)
}

func coorddRepoProcessExecutor(spec repo.ExternalProcessSpec) error {
	if strings.TrimSpace(spec.Path) == "" {
		return nil
	}

	root := strings.TrimSpace(spec.Dir)
	if root == "" {
		root = "."
	}
	r, err := repo.Open(root)
	if err != nil {
		return repo.RunExternalProcessDirect(spec)
	}

	activeID := readActiveAgentID(r)
	input, err := coordd.BuildShellActionInputWithProcess(r, activeID, append([]string{spec.Path}, spec.Args...), processMetadataFromSpec(spec))
	if err != nil {
		return fmt.Errorf("coordd process executor: %w", err)
	}
	decision, err := coordd.EvaluateActionPolicyWithRepo(r, input)
	if err != nil {
		return fmt.Errorf("coordd process executor: %w", err)
	}
	if err := coordd.RecordPreflightDecision(r.GraftDir, input, decision); err != nil {
		return fmt.Errorf("coordd process executor: %w", err)
	}
	result, execErr := coordd.ExecuteGuardedProcessWithIO(r, spec, input, decision)

	// Run post-action effects (best-effort)
	if result != nil && activeID != "" {
		publisher := coordPublisherForRepo(r, activeID)
		if publisher != nil {
			_ = coordd.RunPostActionEffects(publisher, input, *result, r.RootDir, r.GraftDir)
		}
	}

	return execErr
}

// coordPublisherForRepo creates a Coordinator that satisfies CoordPublisher.
func coordPublisherForRepo(r *repo.Repo, agentID string) *coord.Coordinator {
	c := coord.New(r, coord.DefaultConfig)
	c.AgentID = agentID
	return c
}

func processMetadataFromSpec(spec repo.ExternalProcessSpec) coordd.ActionPolicyProcess {
	label := strings.TrimSpace(spec.Label)
	meta := coordd.ActionPolicyProcess{
		Label: label,
	}
	if label == "" {
		return meta
	}
	origin, point, found := strings.Cut(label, ":")
	if !found {
		meta.Origin = strings.ReplaceAll(origin, "-", "_")
		return meta
	}
	meta.Origin = strings.ReplaceAll(strings.TrimSpace(origin), "-", "_")
	meta.Point = strings.TrimSpace(point)
	if meta.Origin == "hook_entry" {
		if hookPoint, _, ok := strings.Cut(meta.Point, "."); ok {
			meta.Point = strings.TrimSpace(hookPoint)
		}
	}
	return meta
}
