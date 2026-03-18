package coord

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// DecisionGraph is graft's persisted, local-only explanation record for a
// coordination decision and the outcome that followed from it.
type DecisionGraph struct {
	ID           string               `json:"id"`
	Version      int                  `json:"version"`
	Kind         string               `json:"kind"`
	Source       string               `json:"source,omitempty"`
	CreatedAt    time.Time            `json:"created_at"`
	AgentID      string               `json:"agent_id,omitempty"`
	AgentName    string               `json:"agent_name,omitempty"`
	EntityKey    string               `json:"entity_key,omitempty"`
	File         string               `json:"file,omitempty"`
	Action       string               `json:"action,omitempty"`
	Rule         string               `json:"rule,omitempty"`
	Reason       string               `json:"reason,omitempty"`
	RequireForce bool                 `json:"require_force,omitempty"`
	Input        ClaimPolicyInput     `json:"input"`
	Decision     *ClaimPolicyDecision `json:"decision,omitempty"`
	Outcome      DecisionOutcome      `json:"outcome"`
	Nodes        []DecisionNode       `json:"nodes,omitempty"`
	Edges        []DecisionEdge       `json:"edges,omitempty"`
}

type DecisionOutcome struct {
	Status            string `json:"status"`
	Message           string `json:"message,omitempty"`
	ForceApplied      bool   `json:"force_applied,omitempty"`
	ClaimReleased     bool   `json:"claim_released,omitempty"`
	ClaimAcquired     bool   `json:"claim_acquired,omitempty"`
	ClaimTransferred  bool   `json:"claim_transferred,omitempty"`
	TransferredFrom   string `json:"transferred_from,omitempty"`
	TransferredFromID string `json:"transferred_from_id,omitempty"`
	Error             string `json:"error,omitempty"`
}

type DecisionNode struct {
	ID       string         `json:"id"`
	Type     string         `json:"type"`
	Label    string         `json:"label,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

type DecisionEdge struct {
	From     string         `json:"from"`
	To       string         `json:"to"`
	Type     string         `json:"type"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

func decisionsDir(graftDir string) string {
	return filepath.Join(graftDir, "coord", "decisions")
}

func decisionFilePath(graftDir, id string) string {
	return filepath.Join(decisionsDir(graftDir), id+".json")
}

// SaveDecision persists a local decision graph under .graft/coord/decisions/.
func SaveDecision(graftDir string, graph *DecisionGraph) error {
	if graph == nil {
		return fmt.Errorf("nil decision graph")
	}
	if graph.Version == 0 {
		graph.Version = 1
	}
	if graph.CreatedAt.IsZero() {
		graph.CreatedAt = time.Now().UTC()
	}
	if graph.ID == "" {
		graph.ID = generateDecisionID(graph.CreatedAt)
	}

	dir := decisionsDir(graftDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create decisions dir: %w", err)
	}

	data, err := json.MarshalIndent(graph, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal decision graph: %w", err)
	}
	if err := os.WriteFile(decisionFilePath(graftDir, graph.ID), data, 0o644); err != nil {
		return fmt.Errorf("write decision graph: %w", err)
	}
	return nil
}

// LoadDecision reads one decision graph from local coord storage.
func LoadDecision(graftDir, id string) (*DecisionGraph, error) {
	data, err := os.ReadFile(decisionFilePath(graftDir, id))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read decision graph: %w", err)
	}

	var graph DecisionGraph
	if err := json.Unmarshal(data, &graph); err != nil {
		return nil, fmt.Errorf("unmarshal decision graph: %w", err)
	}
	return &graph, nil
}

// ListDecisions returns the newest local decision graphs first.
func ListDecisions(graftDir string, limit int) ([]DecisionGraph, error) {
	entries, err := os.ReadDir(decisionsDir(graftDir))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read decisions dir: %w", err)
	}

	var decisions []DecisionGraph
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(decisionsDir(graftDir), entry.Name()))
		if err != nil {
			continue
		}
		var graph DecisionGraph
		if err := json.Unmarshal(data, &graph); err != nil {
			continue
		}
		decisions = append(decisions, graph)
	}

	sort.SliceStable(decisions, func(i, j int) bool {
		if decisions[i].CreatedAt.Equal(decisions[j].CreatedAt) {
			return decisions[i].ID > decisions[j].ID
		}
		return decisions[i].CreatedAt.After(decisions[j].CreatedAt)
	})

	if limit > 0 && len(decisions) > limit {
		decisions = decisions[:limit]
	}
	return decisions, nil
}

// RecordClaimDecision builds and stores a claim-decision graph owned by graft.
func (c *Coordinator) RecordClaimDecision(source, agentID string, req ClaimRequest, ctx *ClaimDecisionContext, outcome DecisionOutcome) (*DecisionGraph, error) {
	if ctx == nil || ctx.Decision == nil {
		return nil, fmt.Errorf("missing claim decision context")
	}

	createdAt := time.Now().UTC()
	graph := &DecisionGraph{
		Version:      1,
		Kind:         "claim_decision",
		Source:       source,
		CreatedAt:    createdAt,
		AgentID:      agentID,
		EntityKey:    req.EntityKey,
		File:         req.File,
		Action:       ctx.Decision.Action,
		Rule:         ctx.Decision.Rule,
		Reason:       ctx.Decision.Reason,
		RequireForce: ctx.Decision.RequireForce,
		Input:        ctx.Input,
		Decision:     ctx.Decision,
		Outcome:      outcome,
	}

	if agentID != "" {
		if agent, err := c.GetAgent(agentID); err == nil {
			graph.AgentName = agent.Name
		}
	}

	graph.Nodes, graph.Edges = buildClaimDecisionGraph(req, ctx, outcome)
	if err := SaveDecision(c.Repo.GraftDir, graph); err != nil {
		return nil, err
	}
	return graph, nil
}

func buildClaimDecisionGraph(req ClaimRequest, ctx *ClaimDecisionContext, outcome DecisionOutcome) ([]DecisionNode, []DecisionEdge) {
	nodes := []DecisionNode{
		{
			ID:    "attempt",
			Type:  "attempt",
			Label: "claim attempt",
			Metadata: map[string]any{
				"mode": req.Mode,
			},
		},
		{
			ID:    "entity",
			Type:  "entity",
			Label: req.EntityKey,
			Metadata: map[string]any{
				"key":       ctx.Input.Entity.Key,
				"file":      ctx.Input.Entity.File,
				"protected": ctx.Input.Entity.Protected,
			},
		},
		{
			ID:    "decision",
			Type:  "decision",
			Label: ctx.Decision.Action,
			Metadata: map[string]any{
				"action":        ctx.Decision.Action,
				"code":          ctx.Decision.Code,
				"reason":        ctx.Decision.Reason,
				"rule":          ctx.Decision.Rule,
				"priority":      ctx.Decision.Priority,
				"require_force": ctx.Decision.RequireForce,
			},
		},
		{
			ID:    "outcome",
			Type:  "outcome",
			Label: outcome.Status,
			Metadata: map[string]any{
				"status":              outcome.Status,
				"message":             outcome.Message,
				"force_applied":       outcome.ForceApplied,
				"claim_released":      outcome.ClaimReleased,
				"claim_acquired":      outcome.ClaimAcquired,
				"claim_transferred":   outcome.ClaimTransferred,
				"transferred_from":    outcome.TransferredFrom,
				"transferred_from_id": outcome.TransferredFromID,
				"error":               outcome.Error,
			},
		},
	}
	edges := []DecisionEdge{
		{From: "attempt", To: "decision", Type: "evaluated_as"},
		{From: "entity", To: "decision", Type: "targets"},
		{From: "decision", To: "outcome", Type: "executed_as"},
	}

	if ctx.Input.ExistingClaim.Exists {
		nodes = append(nodes, DecisionNode{
			ID:    "existing_claim",
			Type:  "claim",
			Label: ctx.Input.ExistingClaim.HeldBy,
			Metadata: map[string]any{
				"same_agent": ctx.Input.ExistingClaim.SameAgent,
				"mode":       ctx.Input.ExistingClaim.Mode,
				"held_by":    ctx.Input.ExistingClaim.HeldBy,
				"held_by_id": ctx.Input.ExistingClaim.HeldByID,
			},
		})
		edges = append(edges, DecisionEdge{From: "existing_claim", To: "decision", Type: "considered"})
	}

	if ctx.Input.ExistingClaim.HeldByID != "" || ctx.Input.Owner.Alive || ctx.Input.Owner.Stale {
		nodes = append(nodes, DecisionNode{
			ID:    "owner",
			Type:  "agent",
			Label: ctx.Input.ExistingClaim.HeldBy,
			Metadata: map[string]any{
				"id":    ctx.Input.ExistingClaim.HeldByID,
				"alive": ctx.Input.Owner.Alive,
				"stale": ctx.Input.Owner.Stale,
			},
		})
		if ctx.Input.ExistingClaim.Exists {
			edges = append(edges, DecisionEdge{From: "owner", To: "existing_claim", Type: "owns"})
		} else {
			edges = append(edges, DecisionEdge{From: "owner", To: "decision", Type: "considered"})
		}
	}

	for i, trace := range ctx.Decision.Trace {
		ruleID := fmt.Sprintf("rule_%02d", i+1)
		nodes = append(nodes, DecisionNode{
			ID:    ruleID,
			Type:  "rule",
			Label: trace.Rule,
			Metadata: map[string]any{
				"matched":         trace.Matched,
				"priority":        trace.Priority,
				"action":          trace.Action,
				"params":          trace.Params,
				"fallback":        trace.Fallback,
				"failed_at_instr": trace.FailedAtInstr,
			},
		})

		edgeType := "rejected_by"
		if trace.Matched {
			edgeType = "matched_by"
		}
		if trace.Rule == ctx.Decision.Rule {
			edgeType = "selected_by"
		}
		edges = append(edges, DecisionEdge{From: ruleID, To: "decision", Type: edgeType})
	}

	return nodes, edges
}

func generateDecisionID(ts time.Time) string {
	suffix := "local"
	buf := make([]byte, 3)
	if _, err := rand.Read(buf); err == nil {
		suffix = hex.EncodeToString(buf)
	}
	return fmt.Sprintf("%d-%s", ts.UnixNano(), suffix)
}
