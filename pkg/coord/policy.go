package coord

import (
	_ "embed"
	"fmt"
	"os"
	"sort"
	"sync"
	"syscall"
	"time"

	"github.com/odvcencio/arbiter"
	arbcompiler "github.com/odvcencio/arbiter/compiler"
)

//go:embed default_claim_policy.arb
var defaultClaimPolicySource []byte

var (
	defaultClaimPolicyOnce sync.Once
	defaultClaimPolicySet  *arbcompiler.CompiledRuleset
	defaultClaimPolicyErr  error
)

// ClaimPolicyInput is the normalized fact set graft sends to arbiter for a
// single claim decision. graft owns this contract and can persist it in a
// decision graph later without exposing arbiter internals.
type ClaimPolicyInput struct {
	Attempt       ClaimPolicyAttempt       `json:"attempt"`
	Repo          ClaimPolicyRepo          `json:"repo"`
	Entity        ClaimPolicyEntity        `json:"entity"`
	ExistingClaim ClaimPolicyExistingClaim `json:"existing_claim"`
	Owner         ClaimPolicyOwner         `json:"owner"`
}

type ClaimPolicyAttempt struct {
	Mode string `json:"mode"`
}

type ClaimPolicyRepo struct {
	ConflictMode string `json:"conflict_mode"`
}

type ClaimPolicyEntity struct {
	Key       string `json:"key"`
	File      string `json:"file"`
	Protected bool   `json:"protected"`
}

type ClaimPolicyExistingClaim struct {
	Exists    bool   `json:"exists"`
	SameAgent bool   `json:"same_agent"`
	Mode      string `json:"mode,omitempty"`
	HeldBy    string `json:"held_by,omitempty"`
	HeldByID  string `json:"held_by_id,omitempty"`
}

type ClaimPolicyOwner struct {
	Alive bool `json:"alive"`
	Stale bool `json:"stale"`
}

// PolicyRuleTrace captures the arbiter rule-level trace for a decision.
type PolicyRuleTrace struct {
	Rule          string         `json:"rule"`
	Matched       bool           `json:"matched,omitempty"`
	Priority      int            `json:"priority"`
	Action        string         `json:"action,omitempty"`
	Params        map[string]any `json:"params,omitempty"`
	Fallback      bool           `json:"fallback,omitempty"`
	FailedAtInstr uint32         `json:"failed_at_instr,omitempty"`
}

// ClaimPolicyDecision is graft's selected policy outcome plus the arbiter
// evaluation trace that produced it.
type ClaimPolicyDecision struct {
	Action       string            `json:"action"`
	Code         string            `json:"code,omitempty"`
	Reason       string            `json:"reason,omitempty"`
	Rule         string            `json:"rule,omitempty"`
	Priority     int               `json:"priority,omitempty"`
	RequireForce bool              `json:"require_force,omitempty"`
	Trace        []PolicyRuleTrace `json:"trace,omitempty"`
}

// ClaimDecisionContext is graft's full view of a claim decision before any
// mutation happens: current state snapshot, normalized arbiter input, and the
// selected policy outcome.
type ClaimDecisionContext struct {
	Input    ClaimPolicyInput     `json:"input"`
	Existing *ClaimInfo           `json:"existing,omitempty"`
	Decision *ClaimPolicyDecision `json:"decision,omitempty"`
}

func defaultClaimPolicyRuleset() (*arbcompiler.CompiledRuleset, error) {
	defaultClaimPolicyOnce.Do(func() {
		defaultClaimPolicySet, defaultClaimPolicyErr = arbiter.Compile(defaultClaimPolicySource)
	})
	return defaultClaimPolicySet, defaultClaimPolicyErr
}

// EvaluateClaimPolicy runs the default arbiter policy for a normalized claim
// decision input and returns the selected outcome plus trace metadata.
func EvaluateClaimPolicy(input ClaimPolicyInput) (*ClaimPolicyDecision, error) {
	rs, err := defaultClaimPolicyRuleset()
	if err != nil {
		return nil, fmt.Errorf("compile default claim policy: %w", err)
	}

	dc := arbiter.DataFromMap(input.toMap(), rs)
	debug := arbiter.EvalDebug(rs, dc)
	if debug.Error != nil {
		return nil, fmt.Errorf("evaluate claim policy: %w", debug.Error)
	}

	decision := &ClaimPolicyDecision{}
	for _, failed := range debug.Failed {
		decision.Trace = append(decision.Trace, PolicyRuleTrace{
			Rule:          failed.Name,
			Matched:       false,
			FailedAtInstr: failed.FailedAtInstr,
		})
	}

	type matchedRule struct {
		Name     string
		Priority int
		Action   string
		Params   map[string]any
		Fallback bool
	}
	matched := make([]matchedRule, 0, len(debug.Matched))
	for _, rule := range debug.Matched {
		matched = append(matched, matchedRule{
			Name:     rule.Name,
			Priority: rule.Priority,
			Action:   rule.Action,
			Params:   rule.Params,
			Fallback: rule.Fallback,
		})
	}
	sort.SliceStable(matched, func(i, j int) bool {
		if matched[i].Priority == matched[j].Priority {
			return matched[i].Name < matched[j].Name
		}
		return matched[i].Priority < matched[j].Priority
	})

	for _, rule := range matched {
		decision.Trace = append(decision.Trace, PolicyRuleTrace{
			Rule:     rule.Name,
			Matched:  true,
			Priority: rule.Priority,
			Action:   rule.Action,
			Params:   rule.Params,
			Fallback: rule.Fallback,
		})
	}

	if len(matched) == 0 {
		return nil, fmt.Errorf("claim policy produced no matched rules")
	}

	selected := matched[0]
	decision.Rule = selected.Name
	decision.Priority = selected.Priority
	decision.Action = selected.Action
	decision.Code = policyStringParam(selected.Params, "code")
	decision.Reason = policyStringParam(selected.Params, "reason")
	decision.RequireForce = policyBoolParam(selected.Params, "require_force")
	return decision, nil
}

// EvaluateClaimDecision builds the arbiter input from current coord state for
// an editing claim attempt. This does not mutate repo state.
func (c *Coordinator) EvaluateClaimDecision(agentID string, req ClaimRequest, existing *ClaimInfo) (*ClaimPolicyDecision, error) {
	ctx, err := c.inspectClaimDecision(agentID, req, existing)
	if err != nil {
		return nil, err
	}
	return ctx.Decision, nil
}

// InspectClaimDecision loads the current claim state for an entity, normalizes
// it into graft's policy input contract, and evaluates the default claim
// policy without mutating coordination refs.
func (c *Coordinator) InspectClaimDecision(agentID string, req ClaimRequest) (*ClaimDecisionContext, error) {
	existing, err := c.LoadClaim(req.EntityKey)
	if err != nil {
		return nil, fmt.Errorf("load existing claim: %w", err)
	}
	return c.inspectClaimDecision(agentID, req, existing)
}

// InspectClaimDecisionWithExisting evaluates claim policy using an existing
// claim snapshot that the caller has already loaded.
func (c *Coordinator) InspectClaimDecisionWithExisting(agentID string, req ClaimRequest, existing *ClaimInfo) (*ClaimDecisionContext, error) {
	return c.inspectClaimDecision(agentID, req, existing)
}

func (c *Coordinator) inspectClaimDecision(agentID string, req ClaimRequest, existing *ClaimInfo) (*ClaimDecisionContext, error) {
	input, err := c.claimPolicyInput(agentID, req, existing)
	if err != nil {
		return nil, err
	}
	decision, err := EvaluateClaimPolicy(input)
	if err != nil {
		return nil, err
	}
	return &ClaimDecisionContext{
		Input:    input,
		Existing: existing,
		Decision: decision,
	}, nil
}

func (c *Coordinator) claimPolicyInput(agentID string, req ClaimRequest, existing *ClaimInfo) (ClaimPolicyInput, error) {
	cfg, err := c.ReadRepoConfig()
	if err != nil {
		return ClaimPolicyInput{}, fmt.Errorf("read repo config: %w", err)
	}

	conflictMode := cfg.ConflictMode
	if conflictMode == "" {
		conflictMode = c.Config.ConflictMode
	}

	input := ClaimPolicyInput{
		Attempt: ClaimPolicyAttempt{
			Mode: req.Mode,
		},
		Repo: ClaimPolicyRepo{
			ConflictMode: conflictMode,
		},
		Entity: ClaimPolicyEntity{
			Key:       req.EntityKey,
			File:      req.File,
			Protected: req.Mode == ClaimEditing && c.IsEntityProtected(req.EntityKey),
		},
	}

	if existing != nil {
		input.ExistingClaim = ClaimPolicyExistingClaim{
			Exists:    true,
			SameAgent: existing.Agent == agentID,
			Mode:      existing.Mode,
			HeldBy:    existing.AgentName,
			HeldByID:  existing.Agent,
		}

		if owner, ownerErr := c.GetAgent(existing.Agent); ownerErr == nil {
			alive, stale := c.agentLiveness(owner)
			input.Owner = ClaimPolicyOwner{
				Alive: alive,
				Stale: stale,
			}
		}
	}

	return input, nil
}

func (in ClaimPolicyInput) toMap() map[string]any {
	return map[string]any{
		"attempt": map[string]any{
			"mode": in.Attempt.Mode,
		},
		"repo": map[string]any{
			"conflict_mode": in.Repo.ConflictMode,
		},
		"entity": map[string]any{
			"key":       in.Entity.Key,
			"file":      in.Entity.File,
			"protected": in.Entity.Protected,
		},
		"existing_claim": map[string]any{
			"exists":     in.ExistingClaim.Exists,
			"same_agent": in.ExistingClaim.SameAgent,
			"mode":       in.ExistingClaim.Mode,
			"held_by":    in.ExistingClaim.HeldBy,
			"held_by_id": in.ExistingClaim.HeldByID,
		},
		"owner": map[string]any{
			"alive": in.Owner.Alive,
			"stale": in.Owner.Stale,
		},
	}
}

func policyStringParam(params map[string]any, key string) string {
	if len(params) == 0 {
		return ""
	}
	if v, ok := params[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
		return fmt.Sprintf("%v", v)
	}
	return ""
}

func policyBoolParam(params map[string]any, key string) bool {
	if len(params) == 0 {
		return false
	}
	v, ok := params[key]
	if !ok {
		return false
	}
	b, ok := v.(bool)
	return ok && b
}

func (c *Coordinator) agentLiveness(agent *AgentInfo) (alive bool, stale bool) {
	if agent == nil {
		return false, false
	}

	cutoff := time.Now().UTC().Add(-c.Config.StaleThreshold)
	stale = agent.HeartbeatAt.Before(cutoff)
	if !stale {
		return true, false
	}

	hostname, err := os.Hostname()
	if err != nil || hostname == "" || agent.Host != hostname || agent.PID <= 0 {
		return false, true
	}

	return processAlive(agent.PID), true
}

func processAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}
