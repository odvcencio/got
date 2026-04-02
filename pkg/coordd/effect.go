package coordd

import (
	_ "embed"
	"fmt"
	execpkg "os/exec"
	"strings"
	"sync"
	"time"

	"github.com/odvcencio/arbiter"
	"github.com/odvcencio/graft/pkg/object"
)

//go:embed default_effect_policy.arb
var defaultEffectPolicySource []byte

// CoordPublisher abstracts coord operations needed by effect handlers.
// Satisfied by *coord.Coordinator. Using an interface keeps pkg/coordd
// decoupled for testing and clean architecture.
type CoordPublisher interface {
	PublishToFeed(eventType string, detail map[string]any) error
	PostCommitHook(commitHash object.Hash) error
	RegisterPresence(file string, entityKey string) error
	Heartbeat(id string) error
}

// EffectResult describes a single matched effect.
type EffectResult struct {
	Rule    string
	Handler string
}

// effectPolicyLoader manages the compiled effect policy bundle.
var effectPolicyLoader = &policyBundleLoader{
	policyName:    "effect",
	bundleID:      "default-effect",
	defaultSource: defaultEffectPolicySource,
}

// EvaluateEffects runs the effect policy against post-execution state.
// Returns ALL matching effects (not just the first).
func EvaluateEffects(input ActionPolicyInput, result ExecResult) ([]EffectResult, error) {
	bundle, err := effectPolicyLoader.load(nil) // nil repo = use embedded default
	if err != nil {
		return nil, fmt.Errorf("load effect policy: %w", err)
	}

	// Build augmented input map with exec.* fields
	inputMap := actionInputToMap(input)
	inputMap["exec"] = map[string]any{
		"exit_code": result.ExitCode,
	}

	dc := arbiter.DataFromMap(inputMap, bundle.full)
	ctx := actionPolicyContext(input)

	matched, _, err := arbiter.EvalGoverned(bundle.full, dc, bundle.full.Segments, ctx)
	if err != nil {
		return nil, fmt.Errorf("evaluate effect policy: %w", err)
	}

	// Collect ALL matching effects (not just first match)
	var effects []EffectResult
	for _, m := range matched {
		handler := stringParam(m.Params, "reason") // handler name stored in reason field
		if handler == "" {
			continue
		}
		effects = append(effects, EffectResult{
			Rule:    m.Name,
			Handler: handler,
		})
	}
	return effects, nil
}

// PostActionHandler executes a single post-action effect.
type PostActionHandler func(ctx PostActionContext) error

// PostActionContext provides everything an effect handler needs.
type PostActionContext struct {
	Publisher CoordPublisher
	Input     ActionPolicyInput
	Result    ExecResult
	RepoRoot  string
	GraftDir  string
}

// Heartbeat rate limiter
var (
	lastHeartbeat   time.Time
	heartbeatMu     sync.Mutex
	heartbeatMinGap = 10 * time.Second
)

// PostActionHandlers maps handler names to implementations.
var PostActionHandlers = map[string]PostActionHandler{
	"publish_commit_to_feed": handlePublishCommitToFeed,
	"register_presence":      handleRegisterPresence,
	"refresh_heartbeat":      handleRefreshHeartbeat,
}

func handlePublishCommitToFeed(ctx PostActionContext) error {
	if ctx.Publisher == nil {
		return nil
	}
	// Resolve HEAD from git and call PostCommitHook
	head, err := resolveGitHEAD(ctx.RepoRoot)
	if err != nil {
		return nil // best effort
	}
	return ctx.Publisher.PostCommitHook(object.Hash(head))
}

func handleRegisterPresence(ctx PostActionContext) error {
	if ctx.Publisher == nil {
		return nil
	}
	for _, file := range ctx.Input.Coord.FilesTouched {
		_ = ctx.Publisher.RegisterPresence(file, "")
	}
	return nil
}

func handleRefreshHeartbeat(ctx PostActionContext) error {
	if ctx.Publisher == nil {
		return nil
	}
	heartbeatMu.Lock()
	defer heartbeatMu.Unlock()
	if time.Since(lastHeartbeat) < heartbeatMinGap {
		return nil // rate limited
	}
	if err := ctx.Publisher.Heartbeat(ctx.Input.Coord.AgentID); err != nil {
		return err
	}
	lastHeartbeat = time.Now()
	return nil
}

// resolveGitHEAD reads the current git HEAD commit hash.
func resolveGitHEAD(repoRoot string) (string, error) {
	cmd := execpkg.Command("git", "rev-parse", "HEAD")
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// RunPostActionEffects evaluates and executes all matching effects.
// All effects are best-effort -- errors don't fail the parent process.
func RunPostActionEffects(publisher CoordPublisher, input ActionPolicyInput, result ExecResult, repoRoot, graftDir string) error {
	effects, err := EvaluateEffects(input, result)
	if err != nil {
		return nil // best effort: don't fail if policy eval errors
	}
	ctx := PostActionContext{
		Publisher: publisher,
		Input:     input,
		Result:    result,
		RepoRoot:  repoRoot,
		GraftDir:  graftDir,
	}
	for _, eff := range effects {
		if handler, ok := PostActionHandlers[eff.Handler]; ok {
			_ = handler(ctx) // best effort
		}
	}
	return nil
}
