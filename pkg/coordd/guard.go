package coordd

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/odvcencio/arbiter"
	"github.com/odvcencio/arbiter/govern"
	"github.com/odvcencio/arbiter/overrides"
	"github.com/odvcencio/arbiter/vm"
	"github.com/odvcencio/graft/pkg/repo"
)

//go:embed default_action_policy.arb
var defaultActionPolicySource []byte

type GuardConfig struct {
	Mode               string   `json:"mode,omitempty"`
	AllowedActions     []string `json:"allowed_actions,omitempty"`
	RequireActiveAgent bool     `json:"require_active_agent,omitempty"`
	PreferredBackend   string   `json:"preferred_backend,omitempty"`
	ContainerRuntime   string   `json:"container_runtime,omitempty"`
	ContainerImage     string   `json:"container_image,omitempty"`
}

type ActionPolicyInput struct {
	Action  ActionPolicyAction  `json:"action"`
	Repo    ActionPolicyRepo    `json:"repo"`
	Session ActionPolicySession `json:"session"`
	Guard   GuardPolicy         `json:"guard"`
	Process ActionPolicyProcess `json:"process,omitempty"`
	Coord   ActionPolicyCoord   `json:"coord,omitempty"`
}

type ActionPolicyAction struct {
	Kind             string   `json:"kind"`
	Selector         string   `json:"selector"`
	Program          string   `json:"program,omitempty"`
	Subcommand       string   `json:"subcommand,omitempty"`
	Argv             []string `json:"argv,omitempty"`
	DefaultAllowed   bool     `json:"default_allowed"`
	Allowlisted      bool     `json:"allowlisted"`
	RequiresRepo     bool     `json:"requires_repo"`
	WritesFilesystem bool     `json:"writes_filesystem"`
	WritesRepo       bool     `json:"writes_repo"`
	WritesCoord      bool     `json:"writes_coord"`
	Network          bool     `json:"network"`
	Destructive      bool     `json:"destructive"`
}

type ActionPolicyRepo struct {
	Present bool   `json:"present"`
	Root    string `json:"root,omitempty"`
}

type ActionPolicySession struct {
	ActiveAgent bool   `json:"active_agent"`
	AgentID     string `json:"agent_id,omitempty"`
}

type GuardPolicy struct {
	Mode               string `json:"mode"`
	RequireActiveAgent bool   `json:"require_active_agent"`
}

type ActionPolicyProcess struct {
	Label  string `json:"label,omitempty"`
	Origin string `json:"origin,omitempty"`
	Point  string `json:"point,omitempty"`
}

type ActionPolicyTrace struct {
	Rule          string              `json:"rule"`
	Matched       bool                `json:"matched,omitempty"`
	Priority      int                 `json:"priority,omitempty"`
	Action        string              `json:"action,omitempty"`
	Params        map[string]any      `json:"params,omitempty"`
	Fallback      bool                `json:"fallback,omitempty"`
	FailedAtInstr uint32              `json:"failed_at_instr,omitempty"`
	Origin        *PolicySourceOrigin `json:"origin,omitempty"`
}

type ActionPolicyDecision struct {
	Action     string                 `json:"action"`
	Code       string                 `json:"code,omitempty"`
	Reason     string                 `json:"reason,omitempty"`
	Rule       string                 `json:"rule,omitempty"`
	RuleOrigin *PolicySourceOrigin    `json:"rule_origin,omitempty"`
	Profile    string                 `json:"profile,omitempty"`
	Bundle     PolicyBundleInfo       `json:"bundle"`
	Trace      []ActionPolicyTrace    `json:"trace,omitempty"`
	Governance []PolicyGovernanceStep `json:"governance,omitempty"`
}

func GuardConfigPath(graftDir string) string {
	return filepath.Join(BaseDir(graftDir), "guard.json")
}

func LoadGuardConfig(graftDir string) (*GuardConfig, error) {
	data, err := os.ReadFile(GuardConfigPath(graftDir))
	if err != nil {
		if os.IsNotExist(err) {
			return &GuardConfig{
				Mode:             "advisory",
				PreferredBackend: "auto",
				ContainerRuntime: "auto",
			}, nil
		}
		return nil, fmt.Errorf("read coordd guard config: %w", err)
	}
	var cfg GuardConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal coordd guard config: %w", err)
	}
	if cfg.Mode == "" {
		cfg.Mode = "advisory"
	}
	if cfg.PreferredBackend == "" {
		cfg.PreferredBackend = "auto"
	}
	if cfg.ContainerRuntime == "" {
		cfg.ContainerRuntime = "auto"
	}
	return &cfg, nil
}

func SaveGuardConfig(graftDir string, cfg *GuardConfig) error {
	if cfg == nil {
		return fmt.Errorf("nil guard config")
	}
	if cfg.Mode == "" {
		cfg.Mode = "advisory"
	}
	if cfg.PreferredBackend == "" {
		cfg.PreferredBackend = "auto"
	}
	if cfg.ContainerRuntime == "" {
		cfg.ContainerRuntime = "auto"
	}
	if err := os.MkdirAll(BaseDir(graftDir), 0o755); err != nil {
		return fmt.Errorf("create coordd dir: %w", err)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal guard config: %w", err)
	}
	if err := os.WriteFile(GuardConfigPath(graftDir), data, 0o644); err != nil {
		return fmt.Errorf("write guard config: %w", err)
	}
	return nil
}

func InspectShellAction(argv []string) ActionPolicyAction {
	action := ActionPolicyAction{
		Kind:     "shell",
		Argv:     append([]string(nil), argv...),
		Selector: selectorForAction("shell", argv),
	}
	if len(argv) == 0 {
		return action
	}

	action.Program = filepath.Base(argv[0])
	action.Subcommand = shellSubcommand(argv)

	switch action.Program {
	case "cat", "date", "env", "false", "find", "git-status", "grep", "head", "less", "ls", "printenv", "printf", "pwd", "rg", "tail", "true", "uname", "wc", "whoami":
		action.DefaultAllowed = true
	case "sed":
		if hasArg(argv[1:], "-i") || hasInlinePrefix(argv[1:], "-i") {
			action.WritesFilesystem = true
		} else {
			action.DefaultAllowed = true
		}
	case "mkdir", "mv", "cp", "touch", "rmdir", "ln", "install", "chmod", "chown":
		action.WritesFilesystem = true
	case "rm":
		action.WritesFilesystem = true
		action.Destructive = rmLooksDestructive(argv[1:])
	case "curl", "wget", "scp", "ssh":
		action.Network = true
	case "git":
		classifyGitAction(&action, argv[1:])
	case "graft":
		classifyGraftAction(&action, argv[1:])
	case "bash", "sh", "zsh", "python", "python3", "node", "perl", "ruby", "make":
		action.WritesFilesystem = true
	default:
		action.WritesFilesystem = true
	}

	if action.DefaultAllowed {
		action.Allowlisted = true
	}
	return action
}

func EvaluateActionPolicy(input ActionPolicyInput) (*ActionPolicyDecision, error) {
	return evaluateActionPolicyWithView(nil, input, "", nil)
}

func EvaluateActionPolicyWithRepo(r *repo.Repo, input ActionPolicyInput) (*ActionPolicyDecision, error) {
	if r == nil {
		return EvaluateActionPolicy(input)
	}
	view, err := loadGuardOverrideView(r.GraftDir)
	if err != nil {
		return nil, err
	}
	return evaluateActionPolicyWithView(r, input, actionPolicyBundleID, view)
}

func evaluateActionPolicyWithView(r *repo.Repo, input ActionPolicyInput, bundleID string, view overrides.View) (*ActionPolicyDecision, error) {
	bundle, err := actionPolicyLoader.load(r)
	if err != nil {
		return nil, err
	}

	ctx := actionPolicyContext(input)
	dc := arbiter.DataFromMap(actionInputToMap(input), bundle.full.Ruleset)
	var (
		matched  []vm.MatchedRule
		govTrace *govern.Trace
	)
	if view != nil {
		matched, govTrace, err = arbiter.EvalGovernedWithOverrides(bundle.full.Ruleset, dc, bundle.full.Segments, ctx, bundleID, view)
	} else {
		matched, govTrace, err = arbiter.EvalGoverned(bundle.full.Ruleset, dc, bundle.full.Segments, ctx)
	}
	if err != nil {
		return nil, fmt.Errorf("evaluate action policy: %w", err)
	}

	ordered := orderedMatchedRules(matched)
	decision := &ActionPolicyDecision{
		Bundle:     bundle.infoCopy(),
		Trace:      matchedRuleTrace(bundle, matched),
		Governance: governanceTraceSteps(bundle, govTrace),
	}
	if len(ordered) == 0 {
		return nil, fmt.Errorf("action policy produced no matched rules")
	}

	selected := ordered[0]
	decision.Action = selected.Action
	decision.Code = stringParam(selected.Params, "code")
	decision.Reason = stringParam(selected.Params, "reason")
	decision.Rule = selected.Name
	decision.RuleOrigin = bundle.ruleOrigin(selected.Name)
	decision.Profile = stringParam(selected.Params, "profile")
	return decision, nil
}

func BuildShellActionInput(r *repo.Repo, activeAgentID string, argv []string) (ActionPolicyInput, error) {
	return BuildShellActionInputWithProcess(r, activeAgentID, argv, ActionPolicyProcess{})
}

func BuildShellActionInputWithProcess(r *repo.Repo, activeAgentID string, argv []string, process ActionPolicyProcess) (ActionPolicyInput, error) {
	action := InspectShellAction(argv)

	var cfg *GuardConfig
	var err error
	input := ActionPolicyInput{
		Action: action,
		Guard: GuardPolicy{
			Mode: "advisory",
		},
		Process: process,
	}
	if r != nil {
		cfg, err = LoadGuardConfig(r.GraftDir)
		if err != nil {
			return ActionPolicyInput{}, err
		}
		input.Repo = ActionPolicyRepo{
			Present: true,
			Root:    r.RootDir,
		}
		input.Session = ActionPolicySession{
			ActiveAgent: strings.TrimSpace(activeAgentID) != "",
			AgentID:     strings.TrimSpace(activeAgentID),
		}
		input.Guard.Mode = cfg.Mode
		input.Guard.RequireActiveAgent = cfg.RequireActiveAgent
		if action.DefaultAllowed || actionMatchesAllowlist(action.Selector, cfg.AllowedActions) {
			input.Action.Allowlisted = true
		}
	}
	input.Coord = LoadCoordContext(r, activeAgentID, argv)
	return input, nil
}

func RecordPreflightDecision(graftDir string, input ActionPolicyInput, decision *ActionPolicyDecision) error {
	if graftDir == "" || decision == nil {
		return nil
	}
	spawnID, taskID := coorddExecutionContextFromEnv()
	eventType := "action_preflight_allowed"
	switch decision.Action {
	case "HardBlock":
		eventType = "action_preflight_blocked"
	case "Advisory":
		eventType = "action_preflight_advisory"
	}
	return AppendEvent(graftDir, Event{
		ID:        newID(),
		Type:      eventType,
		Timestamp: time.Now().UTC(),
		RepoRoot:  input.Repo.Root,
		AgentID:   input.Session.AgentID,
		Data: map[string]any{
			"selector":    input.Action.Selector,
			"program":     input.Action.Program,
			"subcommand":  input.Action.Subcommand,
			"allowlisted": input.Action.Allowlisted,
			"decision":    decision.Action,
			"reason":      decision.Reason,
			"rule":        decision.Rule,
			"profile":     decision.Profile,
			"label":       input.Process.Label,
			"origin":      input.Process.Origin,
			"point":       input.Process.Point,
			"spawn_id":    spawnID,
			"task_id":     taskID,
		},
	})
}

func actionMatchesAllowlist(selector string, patterns []string) bool {
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		if pattern == selector {
			return true
		}
		if strings.HasSuffix(pattern, "*") && strings.HasPrefix(selector, strings.TrimSuffix(pattern, "*")) {
			return true
		}
	}
	return false
}

func actionInputToMap(in ActionPolicyInput) map[string]any {
	return map[string]any{
		"action": map[string]any{
			"kind":              in.Action.Kind,
			"selector":          in.Action.Selector,
			"program":           in.Action.Program,
			"subcommand":        in.Action.Subcommand,
			"default_allowed":   in.Action.DefaultAllowed,
			"allowlisted":       in.Action.Allowlisted,
			"requires_repo":     in.Action.RequiresRepo,
			"writes_filesystem": in.Action.WritesFilesystem,
			"writes_repo":       in.Action.WritesRepo,
			"writes_coord":      in.Action.WritesCoord,
			"network":           in.Action.Network,
			"destructive":       in.Action.Destructive,
		},
		"repo": map[string]any{
			"present": in.Repo.Present,
		},
		"session": map[string]any{
			"active_agent": in.Session.ActiveAgent,
		},
		"guard": map[string]any{
			"mode":                 in.Guard.Mode,
			"require_active_agent": in.Guard.RequireActiveAgent,
		},
		"process": map[string]any{
			"label":  in.Process.Label,
			"origin": in.Process.Origin,
			"point":  in.Process.Point,
		},
		"coord": map[string]any{
			"active":                   in.Coord.Active,
			"agent_id":                 in.Coord.AgentID,
			"agent_name":               in.Coord.AgentName,
			"files_touched":            in.Coord.FilesTouched,
			"files_touched_count":      len(in.Coord.FilesTouched),
			"conflicting_claims_count": len(in.Coord.ConflictingClaims),
			"unread_conflicts":         in.Coord.UnreadConflicts,
			"presence_overlap_count":   len(in.Coord.PresenceOverlap),
			"watching_claims_count":    in.Coord.WatchingClaims,
			"last_heartbeat_age_s":     in.Coord.LastHeartbeatAge,
		},
	}
}

func actionPolicyContext(in ActionPolicyInput) map[string]any {
	ctx := actionInputToMap(in)
	if agentID := strings.TrimSpace(in.Session.AgentID); agentID != "" {
		ctx["user.id"] = agentID
		ctx["user_id"] = agentID
	}
	if selector := strings.TrimSpace(in.Action.Selector); selector != "" {
		ctx["request.id"] = selector
	}
	return ctx
}

func selectorForAction(kind string, argv []string) string {
	if len(argv) == 0 {
		return kind + ":"
	}
	parts := make([]string, 0, len(argv))
	for _, arg := range argv {
		arg = strings.TrimSpace(arg)
		if arg == "" {
			continue
		}
		parts = append(parts, arg)
	}
	return kind + ":" + strings.Join(parts, " ")
}

func shellSubcommand(argv []string) string {
	for _, arg := range argv[1:] {
		if arg == "" || strings.HasPrefix(arg, "-") {
			continue
		}
		return arg
	}
	return ""
}

func classifyGitAction(action *ActionPolicyAction, args []string) {
	if len(args) == 0 {
		action.DefaultAllowed = true
		return
	}
	sub := action.Subcommand
	switch sub {
	case "status", "diff", "log", "show", "grep", "blame", "rev-parse":
		action.DefaultAllowed = true
	case "fetch":
		action.RequiresRepo = true
		action.Network = true
	case "push", "pull":
		action.RequiresRepo = true
		action.WritesRepo = true
		action.Network = true
	case "add", "reset", "checkout", "switch", "restore", "clean", "rm", "mv", "commit", "merge", "rebase", "cherry-pick", "revert", "stash", "apply", "am", "branch", "tag":
		action.RequiresRepo = true
		action.WritesRepo = true
		if gitLooksDestructive(sub, args[1:]) {
			action.Destructive = true
		}
	default:
		action.RequiresRepo = true
		action.WritesRepo = true
	}
}

func classifyGraftAction(action *ActionPolicyAction, args []string) {
	if len(args) == 0 {
		action.DefaultAllowed = true
		return
	}
	top := args[0]
	action.Subcommand = top
	switch top {
	case "version", "status", "log", "show", "blame", "diff", "reflog", "grep", "shortlog", "verify", "coord", "coordd", "workon":
		action.DefaultAllowed = true
		if top == "coord" || top == "coordd" || top == "workon" {
			action.WritesCoord = true
		}
	case "init", "clone":
		action.DefaultAllowed = true
		action.WritesFilesystem = true
	case "fetch":
		action.RequiresRepo = true
		action.Network = true
	case "push", "pull":
		action.RequiresRepo = true
		action.WritesRepo = true
		action.Network = true
	case "add", "reset", "rm", "commit", "branch", "tag", "checkout", "switch", "merge", "conflicts", "cherrypick", "revert", "remote", "config", "publish", "gc", "stash", "rebase", "sparse-checkout", "lfs", "bisect", "worktree", "clean", "module", "workspace":
		action.RequiresRepo = true
		action.WritesRepo = true
	default:
		action.RequiresRepo = true
		action.WritesFilesystem = true
	}
}

func rmLooksDestructive(args []string) bool {
	if len(args) == 0 {
		return false
	}
	for _, arg := range args {
		if arg == "" {
			continue
		}
		if strings.HasPrefix(arg, "-") {
			if strings.Contains(arg, "r") && strings.Contains(arg, "f") {
				return true
			}
			continue
		}
		switch arg {
		case ".", "./", "/", "/*":
			return true
		}
	}
	return false
}

func gitLooksDestructive(sub string, args []string) bool {
	switch sub {
	case "reset":
		return hasArg(args, "--hard")
	case "clean":
		return hasArg(args, "-f") || hasArg(args, "-fd") || hasArg(args, "-fdx") || hasArg(args, "-xdf")
	case "checkout":
		return hasArg(args, "--")
	case "restore":
		return hasArg(args, "--worktree") || hasArg(args, "--staged")
	default:
		return false
	}
}

func hasArg(args []string, target string) bool {
	for _, arg := range args {
		if arg == target {
			return true
		}
	}
	return false
}

func hasInlinePrefix(args []string, prefix string) bool {
	for _, arg := range args {
		if strings.HasPrefix(arg, prefix) {
			return true
		}
	}
	return false
}

func stringParam(params map[string]any, key string) string {
	if params == nil {
		return ""
	}
	if v, ok := params[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}
