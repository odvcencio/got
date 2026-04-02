package coordd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/odvcencio/arbiter"
	"github.com/odvcencio/arbiter/govern"
	"github.com/odvcencio/arbiter/overrides"
	"github.com/odvcencio/arbiter/vm"
	"github.com/odvcencio/graft/pkg/repo"
)

const (
	actionPolicyBundleID = "coordd/action"
	spawnPolicyBundleID  = "coordd/spawn"
)

type PolicyBundleInfo struct {
	Policy   string   `json:"policy"`
	BundleID string   `json:"bundle_id"`
	Embedded bool     `json:"embedded,omitempty"`
	Root     string   `json:"root,omitempty"`
	Files    []string `json:"files,omitempty"`
}

type PolicySourceOrigin struct {
	File string `json:"file,omitempty"`
	Line int    `json:"line,omitempty"`
	Kind string `json:"kind,omitempty"`
	Name string `json:"name,omitempty"`
}

type PolicyGovernanceStep struct {
	Check  string              `json:"check"`
	Result bool                `json:"result"`
	Detail string              `json:"detail,omitempty"`
	Origin *PolicySourceOrigin `json:"origin,omitempty"`
}

type policyBundle struct {
	info  PolicyBundleInfo
	full  *arbiter.Program
	unit  *arbiter.SourceUnit
	files map[string]policyFileState
}

type policyFileState struct {
	Size    int64
	ModTime int64
}

type policyBundleLoader struct {
	mu            sync.Mutex
	policyName    string
	bundleID      string
	defaultSource []byte
	cached        *policyBundle
}

var (
	actionPolicyLoader = &policyBundleLoader{
		policyName:    "action",
		bundleID:      actionPolicyBundleID,
		defaultSource: defaultActionPolicySource,
	}
	spawnPolicyLoader = &policyBundleLoader{
		policyName:    "spawn",
		bundleID:      spawnPolicyBundleID,
		defaultSource: defaultSpawnPolicySource,
	}
)

func GuardOverridesPath(graftDir string) string {
	return filepath.Join(BaseDir(graftDir), "arbiter-overrides.json")
}

func GuardPoliciesDir(graftDir string) string {
	return filepath.Join(BaseDir(graftDir), "policies")
}

func GuardPolicyBundleDir(graftDir, policyName string) string {
	return filepath.Join(GuardPoliciesDir(graftDir), policyName)
}

// GuardPolicyRootCandidates returns the only auto-loaded bundle roots for a
// coordd policy. This is intentionally narrow so example files such as
// action.example.arb or spawn.example.arb never become live policy by
// accident. Those files are safe to keep beside the real bundle unless a live
// policy explicitly includes them.
func GuardPolicyRootCandidates(graftDir, policyName string) []string {
	return []string{
		filepath.Join(GuardPoliciesDir(graftDir), policyName+".arb"),
		filepath.Join(GuardPolicyBundleDir(graftDir, policyName), "main.arb"),
	}
}

func LoadGuardOverrideStore(graftDir string) (*overrides.Store, error) {
	if strings.TrimSpace(graftDir) == "" {
		return nil, fmt.Errorf("empty graft dir")
	}
	return overrides.NewFileStore(GuardOverridesPath(graftDir))
}

func loadGuardOverrideView(graftDir string) (overrides.View, error) {
	if strings.TrimSpace(graftDir) == "" {
		return nil, nil
	}
	store, err := LoadGuardOverrideStore(graftDir)
	if err != nil {
		return nil, err
	}
	return store, nil
}

func LoadActionPolicyBundleInfo(r *repo.Repo) (PolicyBundleInfo, error) {
	bundle, err := actionPolicyLoader.load(r)
	if err != nil {
		return PolicyBundleInfo{}, err
	}
	return bundle.infoCopy(), nil
}

func LoadSpawnPolicyBundleInfo(r *repo.Repo) (PolicyBundleInfo, error) {
	bundle, err := spawnPolicyLoader.load(r)
	if err != nil {
		return PolicyBundleInfo{}, err
	}
	return bundle.infoCopy(), nil
}

func (l *policyBundleLoader) load(r *repo.Repo) (*policyBundle, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	rootPath := ""
	if r != nil {
		resolved, err := resolveGuardPolicyRoot(r.GraftDir, l.policyName)
		if err != nil {
			return nil, err
		}
		rootPath = resolved
	}

	if l.cached != nil && l.cached.matchesRoot(rootPath) && !l.cached.needsReload() {
		return l.cached, nil
	}

	var (
		bundle *policyBundle
		err    error
	)
	if strings.TrimSpace(rootPath) != "" {
		bundle, err = compileFilePolicyBundle(l.policyName, l.bundleID, rootPath)
	} else {
		bundle, err = compileEmbeddedPolicyBundle(l.policyName, l.bundleID, l.defaultSource)
	}
	if err != nil {
		return nil, err
	}
	l.cached = bundle
	return bundle, nil
}

func (b *policyBundle) matchesRoot(rootPath string) bool {
	if b == nil {
		return false
	}
	return strings.TrimSpace(b.info.Root) == strings.TrimSpace(rootPath)
}

func (b *policyBundle) needsReload() bool {
	if b == nil || b.info.Embedded || len(b.files) == 0 {
		return false
	}
	for path, want := range b.files {
		info, err := os.Stat(path)
		if err != nil {
			return true
		}
		if info.Size() != want.Size || info.ModTime().UTC().UnixNano() != want.ModTime {
			return true
		}
	}
	return false
}

func (b *policyBundle) infoCopy() PolicyBundleInfo {
	if b == nil {
		return PolicyBundleInfo{}
	}
	info := b.info
	info.Files = append([]string(nil), b.info.Files...)
	return info
}

func (b *policyBundle) ruleOrigin(name string) *PolicySourceOrigin {
	if b == nil || b.unit == nil || strings.TrimSpace(name) == "" {
		return nil
	}
	for _, origin := range b.unit.Origins {
		if origin.Name != name {
			continue
		}
		if origin.Kind != "rule_declaration" && origin.Kind != "expert_rule_declaration" {
			continue
		}
		return &PolicySourceOrigin{
			File: origin.File,
			Line: origin.SourceLine,
			Kind: origin.Kind,
			Name: origin.Name,
		}
	}
	return nil
}

func (b *policyBundle) segmentOrigin(name string) *PolicySourceOrigin {
	if b == nil || b.unit == nil || strings.TrimSpace(name) == "" {
		return nil
	}
	for _, origin := range b.unit.Origins {
		if origin.Name != name || origin.Kind != "segment_declaration" {
			continue
		}
		return &PolicySourceOrigin{
			File: origin.File,
			Line: origin.SourceLine,
			Kind: origin.Kind,
			Name: origin.Name,
		}
	}
	return nil
}

func resolveGuardPolicyRoot(graftDir, policyName string) (string, error) {
	for _, candidate := range GuardPolicyRootCandidates(graftDir, policyName) {
		info, err := os.Stat(candidate)
		if err == nil && !info.IsDir() {
			return candidate, nil
		}
		if err != nil && !os.IsNotExist(err) {
			return "", fmt.Errorf("stat %s policy bundle %s: %w", policyName, candidate, err)
		}
	}
	return "", nil
}

func compileEmbeddedPolicyBundle(policyName, bundleID string, source []byte) (*policyBundle, error) {
	full, err := arbiter.Compile(source)
	if err != nil {
		return nil, fmt.Errorf("compile %s policy: %w", policyName, err)
	}
	return &policyBundle{
		info: PolicyBundleInfo{
			Policy:   policyName,
			BundleID: bundleID,
			Embedded: true,
		},
		full: full,
	}, nil
}

func compileFilePolicyBundle(policyName, bundleID, rootPath string) (*policyBundle, error) {
	unit, err := arbiter.LoadFileUnit(rootPath)
	if err != nil {
		return nil, fmt.Errorf("load %s policy %s: %w", policyName, rootPath, err)
	}
	full, err := arbiter.CompileFile(rootPath)
	if err != nil {
		return nil, fmt.Errorf("compile %s policy %s: %w", policyName, rootPath, arbiter.WrapFileError(unit, err))
	}
	files, err := snapshotPolicyFiles(unit.Files)
	if err != nil {
		return nil, err
	}
	return &policyBundle{
		info: PolicyBundleInfo{
			Policy:   policyName,
			BundleID: bundleID,
			Root:     rootPath,
			Files:    append([]string(nil), unit.Files...),
		},
		full:  full,
		unit:  unit,
		files: files,
	}, nil
}

func snapshotPolicyFiles(paths []string) (map[string]policyFileState, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	states := make(map[string]policyFileState, len(paths))
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			return nil, fmt.Errorf("stat policy file %s: %w", path, err)
		}
		states[path] = policyFileState{
			Size:    info.Size(),
			ModTime: info.ModTime().UTC().UnixNano(),
		}
	}
	return states, nil
}

func orderedMatchedRules(matched []vm.MatchedRule) []vm.MatchedRule {
	ordered := append([]vm.MatchedRule(nil), matched...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Priority == ordered[j].Priority {
			return ordered[i].Name < ordered[j].Name
		}
		return ordered[i].Priority < ordered[j].Priority
	})
	return ordered
}

func matchedRuleTrace(bundle *policyBundle, matched []vm.MatchedRule) []ActionPolicyTrace {
	ordered := orderedMatchedRules(matched)
	trace := make([]ActionPolicyTrace, 0, len(ordered))
	for _, rule := range ordered {
		trace = append(trace, ActionPolicyTrace{
			Rule:     rule.Name,
			Matched:  true,
			Priority: rule.Priority,
			Action:   rule.Action,
			Params:   rule.Params,
			Fallback: rule.Fallback,
			Origin:   bundle.ruleOrigin(rule.Name),
		})
	}
	return trace
}

func governanceTraceSteps(bundle *policyBundle, trace *govern.Arbitrace) []PolicyGovernanceStep {
	if trace == nil || len(trace.Steps) == 0 {
		return nil
	}
	steps := make([]PolicyGovernanceStep, 0, len(trace.Steps))
	for _, step := range trace.Steps {
		steps = append(steps, PolicyGovernanceStep{
			Check:  step.Check,
			Result: step.Result,
			Detail: step.Detail,
			Origin: governanceStepOrigin(bundle, step.Check),
		})
	}
	return steps
}

func governanceStepOrigin(bundle *policyBundle, check string) *PolicySourceOrigin {
	if bundle == nil {
		return nil
	}
	check = strings.TrimSpace(check)
	switch {
	case strings.HasPrefix(check, "segment "):
		return bundle.segmentOrigin(strings.TrimSpace(strings.TrimPrefix(check, "segment ")))
	case strings.HasPrefix(check, "requires "):
		return bundle.ruleOrigin(strings.TrimSpace(strings.TrimPrefix(check, "requires ")))
	default:
		return nil
	}
}
