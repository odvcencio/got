package coordd

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/odvcencio/arbiter"
		"github.com/odvcencio/graft/pkg/repo"
)

//go:embed default_spawn_policy.arb
var defaultSpawnPolicySource []byte

var (
	defaultSpawnPolicyOnce sync.Once
	defaultSpawnPolicySet  *arbiter.Program
	defaultSpawnPolicyErr  error
)

type SpawnRequest struct {
	Name             string   `json:"name"`
	Command          []string `json:"command"`
	RequestedProfile string   `json:"requested_profile,omitempty"`
}

type SpawnPolicyInput struct {
	Action  SpawnPolicyAction   `json:"action"`
	Repo    ActionPolicyRepo    `json:"repo"`
	Session ActionPolicySession `json:"session"`
	Parent  SpawnPolicyParent   `json:"parent"`
	Spawn   SpawnPolicySpec     `json:"spawn"`
}

type SpawnPolicyAction struct {
	Decision  string `json:"decision,omitempty"`
	Profile   string `json:"profile,omitempty"`
	Selector  string `json:"selector,omitempty"`
	Advisory  bool   `json:"advisory"`
	HardBlock bool   `json:"hard_block"`
}

type SpawnPolicyParent struct {
	ProfileKnown bool   `json:"profile_known"`
	Profile      string `json:"profile,omitempty"`
	SpawnID      string `json:"spawn_id,omitempty"`
}

type SpawnPolicySpec struct {
	Name             string `json:"name,omitempty"`
	NamePresent      bool   `json:"name_present"`
	CommandPresent   bool   `json:"command_present"`
	RequestedProfile string `json:"requested_profile,omitempty"`
	RequestedValid   bool   `json:"requested_valid"`
	SelectedProfile  string `json:"selected_profile,omitempty"`
	SelectedValid    bool   `json:"selected_valid"`
	EscalatesParent  bool   `json:"escalates_parent"`
}

type SpawnPolicyDecision struct {
	Action  string              `json:"action"`
	Code    string              `json:"code,omitempty"`
	Reason  string              `json:"reason,omitempty"`
	Rule    string              `json:"rule,omitempty"`
	Profile string              `json:"profile,omitempty"`
	Trace   []ActionPolicyTrace `json:"trace,omitempty"`
}

type SpawnRecord struct {
	ID               string                `json:"id"`
	Name             string                `json:"name"`
	ParentAgentID    string                `json:"parent_agent_id,omitempty"`
	ParentSpawnID    string                `json:"parent_spawn_id,omitempty"`
	ParentProfile    string                `json:"parent_profile,omitempty"`
	RepoRoot         string                `json:"repo_root,omitempty"`
	Command          []string              `json:"command,omitempty"`
	Selector         string                `json:"selector,omitempty"`
	Backend          string                `json:"backend,omitempty"`
	RequestedProfile RuntimeProfile        `json:"requested_profile,omitempty"`
	EffectiveProfile RuntimeProfile        `json:"effective_profile,omitempty"`
	Degradations     []string              `json:"degradations,omitempty"`
	ActionDecision   *ActionPolicyDecision `json:"action_decision,omitempty"`
	SpawnDecision    *SpawnPolicyDecision  `json:"spawn_decision,omitempty"`
	Status           string                `json:"status"`
	PID              int                   `json:"pid,omitempty"`
	ContainerRuntime string                `json:"container_runtime,omitempty"`
	ContainerID      string                `json:"container_id,omitempty"`
	StdoutPath       string                `json:"stdout_path,omitempty"`
	StderrPath       string                `json:"stderr_path,omitempty"`
	SnapshotID       string                `json:"snapshot_id,omitempty"`
	StartedAt        time.Time             `json:"started_at"`
}

type SpawnResult struct {
	ActionInput      ActionPolicyInput     `json:"action_input"`
	ActionDecision   *ActionPolicyDecision `json:"action_decision,omitempty"`
	SpawnInput       SpawnPolicyInput      `json:"spawn_input"`
	SpawnDecision    *SpawnPolicyDecision  `json:"spawn_decision,omitempty"`
	Backend          string                `json:"backend,omitempty"`
	RequestedProfile RuntimeProfile        `json:"requested_profile,omitempty"`
	EffectiveProfile RuntimeProfile        `json:"effective_profile,omitempty"`
	Degradations     []string              `json:"degradations,omitempty"`
	SnapshotID       string                `json:"snapshot_id,omitempty"`
	Record           *SpawnRecord          `json:"record,omitempty"`
}

func defaultSpawnPolicyRuleset() (*arbiter.Program, error) {
	defaultSpawnPolicyOnce.Do(func() {
		defaultSpawnPolicySet, defaultSpawnPolicyErr = arbiter.Compile(defaultSpawnPolicySource)
	})
	return defaultSpawnPolicySet, defaultSpawnPolicyErr
}

func SpawnsDir(graftDir string) string {
	return filepath.Join(BaseDir(graftDir), "spawns")
}

func SpawnRecordPath(graftDir, id string) string {
	return filepath.Join(SpawnsDir(graftDir), id+".json")
}

func SpawnStdoutPath(graftDir, id string) string {
	return filepath.Join(SpawnsDir(graftDir), id+".stdout.log")
}

func SpawnStderrPath(graftDir, id string) string {
	return filepath.Join(SpawnsDir(graftDir), id+".stderr.log")
}

func SaveSpawnRecord(graftDir string, record *SpawnRecord) error {
	if record == nil {
		return fmt.Errorf("nil spawn record")
	}
	if err := os.MkdirAll(SpawnsDir(graftDir), 0o755); err != nil {
		return fmt.Errorf("create spawns dir: %w", err)
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal spawn record: %w", err)
	}
	if err := os.WriteFile(SpawnRecordPath(graftDir, record.ID), data, 0o644); err != nil {
		return fmt.Errorf("write spawn record: %w", err)
	}
	return nil
}

func LoadSpawnRecord(graftDir, id string) (*SpawnRecord, error) {
	data, err := os.ReadFile(SpawnRecordPath(graftDir, id))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read spawn record: %w", err)
	}
	var record SpawnRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return nil, fmt.Errorf("unmarshal spawn record: %w", err)
	}
	return &record, nil
}

func ListSpawnRecords(graftDir string) ([]SpawnRecord, error) {
	entries, err := os.ReadDir(SpawnsDir(graftDir))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read spawns dir: %w", err)
	}

	records := make([]SpawnRecord, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(SpawnsDir(graftDir), entry.Name()))
		if err != nil {
			continue
		}
		var record SpawnRecord
		if err := json.Unmarshal(data, &record); err != nil {
			continue
		}
		records = append(records, record)
	}

	sort.SliceStable(records, func(i, j int) bool {
		return records[i].StartedAt.After(records[j].StartedAt)
	})
	return records, nil
}

func EvaluateSpawnPolicy(input SpawnPolicyInput) (*SpawnPolicyDecision, error) {
	rs, err := defaultSpawnPolicyRuleset()
	if err != nil {
		return nil, fmt.Errorf("compile spawn policy: %w", err)
	}

	dc := arbiter.DataFromMap(spawnInputToMap(input), rs)
	debug := arbiter.EvalDebug(rs, dc)
	if debug.Error != nil {
		return nil, fmt.Errorf("evaluate spawn policy: %w", debug.Error)
	}

	decision := &SpawnPolicyDecision{}
	for _, failed := range debug.Failed {
		decision.Trace = append(decision.Trace, ActionPolicyTrace{
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
		decision.Trace = append(decision.Trace, ActionPolicyTrace{
			Rule:     rule.Name,
			Matched:  true,
			Priority: rule.Priority,
			Action:   rule.Action,
			Params:   rule.Params,
			Fallback: rule.Fallback,
		})
	}
	if len(matched) == 0 {
		return nil, fmt.Errorf("spawn policy produced no matched rules")
	}

	selected := matched[0]
	decision.Action = selected.Action
	decision.Code = stringParam(selected.Params, "code")
	decision.Reason = stringParam(selected.Params, "reason")
	decision.Rule = selected.Name
	decision.Profile = stringParam(selected.Params, "profile")
	return decision, nil
}

func BuildSpawnPolicyInput(actionInput ActionPolicyInput, actionDecision *ActionPolicyDecision, req SpawnRequest) SpawnPolicyInput {
	requestedName := strings.TrimSpace(req.RequestedProfile)
	requestedValid := requestedName == "" || isCoorddSpawnProfile(requestedName)

	selectedName := actionDecision.Profile
	if requestedName != "" && requestedValid {
		selectedName = requestedName
	}

	minimumProfile, minimumValid := lookupCoorddSpawnProfile(actionDecision.Profile)
	selectedProfile, selectedProfileValid := lookupCoorddSpawnProfile(selectedName)
	selectedValid := minimumValid && selectedProfileValid && runtimeProfileAllows(selectedProfile, minimumProfile)

	parentProfileName, parentSpawnID := coorddParentContext()
	parentProfile, parentProfileKnown := lookupCoorddSpawnProfile(parentProfileName)
	escalatesParent := false
	if parentProfileKnown && selectedProfileValid {
		escalatesParent = !runtimeProfileAllows(parentProfile, selectedProfile)
	}

	return SpawnPolicyInput{
		Action: SpawnPolicyAction{
			Decision:  actionDecision.Action,
			Profile:   actionDecision.Profile,
			Selector:  actionInput.Action.Selector,
			Advisory:  actionDecision.Action == "Advisory",
			HardBlock: actionDecision.Action == "HardBlock",
		},
		Repo:    actionInput.Repo,
		Session: actionInput.Session,
		Parent: SpawnPolicyParent{
			ProfileKnown: parentProfileKnown,
			Profile:      parentProfileName,
			SpawnID:      parentSpawnID,
		},
		Spawn: SpawnPolicySpec{
			Name:             strings.TrimSpace(req.Name),
			NamePresent:      strings.TrimSpace(req.Name) != "",
			CommandPresent:   len(req.Command) > 0,
			RequestedProfile: requestedName,
			RequestedValid:   requestedValid,
			SelectedProfile:  selectedName,
			SelectedValid:    selectedValid,
			EscalatesParent:  escalatesParent,
		},
	}
}

func RecordSpawnPreflightDecision(graftDir string, input SpawnPolicyInput, decision *SpawnPolicyDecision) error {
	if graftDir == "" || decision == nil {
		return nil
	}
	eventType := "spawn_preflight_allowed"
	switch decision.Action {
	case "HardBlock":
		eventType = "spawn_preflight_blocked"
	case "Advisory":
		eventType = "spawn_preflight_advisory"
	}
	return AppendEvent(graftDir, Event{
		ID:        newID(),
		Type:      eventType,
		Timestamp: time.Now().UTC(),
		RepoRoot:  input.Repo.Root,
		AgentID:   input.Session.AgentID,
		Data: map[string]any{
			"name":              input.Spawn.Name,
			"selector":          input.Action.Selector,
			"requested_profile": input.Spawn.RequestedProfile,
			"selected_profile":  input.Spawn.SelectedProfile,
			"decision":          decision.Action,
			"reason":            decision.Reason,
			"rule":              decision.Rule,
			"profile":           decision.Profile,
			"parent_spawn_id":   input.Parent.SpawnID,
		},
	})
}

func SpawnDetached(r *repo.Repo, activeAgentID string, req SpawnRequest) (*SpawnResult, error) {
	if r == nil {
		return nil, fmt.Errorf("spawn requires an open repo")
	}

	actionInput, err := BuildShellActionInput(r, activeAgentID, req.Command)
	if err != nil {
		return nil, err
	}
	actionDecision, err := EvaluateActionPolicy(actionInput)
	if err != nil {
		return nil, err
	}

	spawnInput := BuildSpawnPolicyInput(actionInput, actionDecision, req)
	spawnDecision, err := EvaluateSpawnPolicy(spawnInput)
	if err != nil {
		return nil, err
	}
	_ = RecordSpawnPreflightDecision(r.GraftDir, spawnInput, spawnDecision)

	result := &SpawnResult{
		ActionInput:    actionInput,
		ActionDecision: actionDecision,
		SpawnInput:     spawnInput,
		SpawnDecision:  spawnDecision,
	}

	requestedProfile := ResolveRuntimeProfile(spawnDecision.Profile, actionInput.Action)
	result.RequestedProfile = requestedProfile
	if spawnDecision.Action == "HardBlock" {
		result.EffectiveProfile = requestedProfile
		return result, &ExitCodeError{
			Code: 126,
			Err:  fmt.Errorf("coordd blocked spawn: %s", spawnDecision.Reason),
		}
	}

	cfg, err := loadGuardConfigForExec(r)
	if err != nil {
		return result, err
	}

	if requestedProfile.RequireSnapshot {
		snapshotID, snapshotErr := captureExecSnapshot(r, activeAgentID)
		if snapshotErr != nil {
			return result, snapshotErr
		}
		result.SnapshotID = snapshotID
	}

	backendName, effectiveProfile, degradations, err := selectExecBackend(r, cfg, requestedProfile)
	if err != nil {
		return result, err
	}
	result.Backend = backendName
	result.EffectiveProfile = effectiveProfile
	result.Degradations = append(result.Degradations, degradations...)

	record := &SpawnRecord{
		ID:               newID(),
		Name:             strings.TrimSpace(req.Name),
		ParentAgentID:    activeAgentID,
		ParentSpawnID:    spawnInput.Parent.SpawnID,
		ParentProfile:    spawnInput.Parent.Profile,
		RepoRoot:         r.RootDir,
		Command:          append([]string(nil), req.Command...),
		Selector:         actionInput.Action.Selector,
		Backend:          backendName,
		RequestedProfile: requestedProfile,
		EffectiveProfile: effectiveProfile,
		Degradations:     append([]string(nil), degradations...),
		ActionDecision:   actionDecision,
		SpawnDecision:    spawnDecision,
		Status:           "running",
		SnapshotID:       result.SnapshotID,
		StartedAt:        time.Now().UTC(),
	}

	switch backendName {
	case "host-direct":
		pid, stdoutPath, stderrPath, err := startDetachedDirect(r, actionInput, spawnDecision.Action, requestedProfile, effectiveProfile, record)
		if err != nil {
			return result, err
		}
		record.PID = pid
		record.StdoutPath = stdoutPath
		record.StderrPath = stderrPath
	case "host-bwrap":
		pid, stdoutPath, stderrPath, err := startDetachedBwrap(r, actionInput, spawnDecision.Action, requestedProfile, effectiveProfile, record)
		if err != nil {
			return result, err
		}
		record.PID = pid
		record.StdoutPath = stdoutPath
		record.StderrPath = stderrPath
	case "container":
		runtimeName, containerID, err := startDetachedContainer(r, cfg, actionInput, spawnDecision.Action, requestedProfile, effectiveProfile, record)
		if err != nil {
			return result, err
		}
		record.ContainerRuntime = runtimeName
		record.ContainerID = containerID
	default:
		return result, fmt.Errorf("unknown coordd backend %q", backendName)
	}

	if err := SaveSpawnRecord(r.GraftDir, record); err != nil {
		return result, err
	}

	_ = AppendEvent(r.GraftDir, Event{
		ID:        newID(),
		Type:      "spawn_started",
		Timestamp: time.Now().UTC(),
		RepoRoot:  r.RootDir,
		AgentID:   activeAgentID,
		Data: map[string]any{
			"id":                record.ID,
			"name":              record.Name,
			"selector":          record.Selector,
			"backend":           record.Backend,
			"requested_profile": record.RequestedProfile,
			"effective_profile": record.EffectiveProfile,
			"degradations":      record.Degradations,
			"pid":               record.PID,
			"container_runtime": record.ContainerRuntime,
			"container_id":      record.ContainerID,
			"snapshot_id":       record.SnapshotID,
			"parent_spawn_id":   record.ParentSpawnID,
		},
	})

	result.Record = record
	return result, nil
}

func spawnInputToMap(in SpawnPolicyInput) map[string]any {
	return map[string]any{
		"action": map[string]any{
			"decision":   in.Action.Decision,
			"profile":    in.Action.Profile,
			"selector":   in.Action.Selector,
			"advisory":   in.Action.Advisory,
			"hard_block": in.Action.HardBlock,
		},
		"repo": map[string]any{
			"present": in.Repo.Present,
		},
		"session": map[string]any{
			"active_agent": in.Session.ActiveAgent,
		},
		"parent": map[string]any{
			"profile_known": in.Parent.ProfileKnown,
			"profile":       in.Parent.Profile,
			"spawn_id":      in.Parent.SpawnID,
		},
		"spawn": map[string]any{
			"name":              in.Spawn.Name,
			"name_present":      in.Spawn.NamePresent,
			"command_present":   in.Spawn.CommandPresent,
			"requested_profile": in.Spawn.RequestedProfile,
			"requested_valid":   in.Spawn.RequestedValid,
			"selected_profile":  in.Spawn.SelectedProfile,
			"selected_valid":    in.Spawn.SelectedValid,
			"escalates_parent":  in.Spawn.EscalatesParent,
		},
	}
}

func coorddParentContext() (string, string) {
	parentProfile := strings.TrimSpace(os.Getenv("GRAFT_COORDD_REQUESTED_PROFILE"))
	if !isCoorddSpawnProfile(parentProfile) {
		parentProfile = strings.TrimSpace(os.Getenv("GRAFT_COORDD_EFFECTIVE_PROFILE"))
		if !isCoorddSpawnProfile(parentProfile) {
			parentProfile = ""
		}
	}
	return parentProfile, strings.TrimSpace(os.Getenv("GRAFT_COORDD_SPAWN_ID"))
}

func isCoorddSpawnProfile(name string) bool {
	_, ok := lookupCoorddSpawnProfile(name)
	return ok
}

func lookupCoorddSpawnProfile(name string) (RuntimeProfile, bool) {
	switch strings.TrimSpace(name) {
	case "read_only", "repo_write", "network_read", "repo_write_network":
		return ResolveRuntimeProfile(strings.TrimSpace(name), ActionPolicyAction{}), true
	default:
		return RuntimeProfile{}, false
	}
}

func runtimeProfileAllows(granted, required RuntimeProfile) bool {
	return filesystemScopeRank(granted.FilesystemScope) >= filesystemScopeRank(required.FilesystemScope) &&
		networkRank(granted.Network) >= networkRank(required.Network) &&
		deleteScopeRank(granted.DeleteScope) >= deleteScopeRank(required.DeleteScope)
}

func filesystemScopeRank(scope string) int {
	switch scope {
	case FilesystemScopeRepoRO:
		return 1
	case FilesystemScopeRepoRW:
		return 2
	case FilesystemScopeHostProc:
		return 3
	default:
		return 0
	}
}

func networkRank(mode string) int {
	switch mode {
	case NetworkDeny:
		return 1
	case NetworkAllow:
		return 2
	case NetworkAmbient:
		return 3
	default:
		return 0
	}
}

func deleteScopeRank(scope string) int {
	switch scope {
	case DeleteScopeNone:
		return 1
	case DeleteScopeRepo:
		return 2
	case DeleteScopeAmbient:
		return 3
	default:
		return 0
	}
}

func startDetachedDirect(r *repo.Repo, input ActionPolicyInput, decisionAction string, requested, effective RuntimeProfile, record *SpawnRecord) (int, string, string, error) {
	stdoutPath, stderrPath, stdoutFile, stderrFile, devNull, err := openSpawnIO(r.GraftDir, record.ID)
	if err != nil {
		return 0, "", "", err
	}
	defer stdoutFile.Close()
	defer stderrFile.Close()
	defer devNull.Close()

	cmd := exec.Command(input.Action.Argv[0], input.Action.Argv[1:]...)
	cmd.Stdin = devNull
	cmd.Stdout = stdoutFile
	cmd.Stderr = stderrFile
	cmd.Dir = coorddSpawnDir(r)
	cmd.Env = append(os.Environ(),
		coorddSpawnEnv(input, decisionAction, requested, effective, record)...,
	)
	if err := cmd.Start(); err != nil {
		return 0, "", "", err
	}
	return cmd.Process.Pid, stdoutPath, stderrPath, nil
}

func startDetachedBwrap(r *repo.Repo, input ActionPolicyInput, decisionAction string, requested, effective RuntimeProfile, record *SpawnRecord) (int, string, string, error) {
	if !canUseBwrap(r, requested) {
		return 0, "", "", fmt.Errorf("host-bwrap backend unavailable")
	}

	stdoutPath, stderrPath, stdoutFile, stderrFile, devNull, err := openSpawnIO(r.GraftDir, record.ID)
	if err != nil {
		return 0, "", "", err
	}
	defer stdoutFile.Close()
	defer stderrFile.Close()
	defer devNull.Close()

	args := buildDetachedBwrapArgs(r, input, requested)
	cmd := exec.Command("bwrap", args...)
	cmd.Stdin = devNull
	cmd.Stdout = stdoutFile
	cmd.Stderr = stderrFile
	cmd.Env = append(os.Environ(),
		coorddSpawnEnv(input, decisionAction, requested, effective, record)...,
	)
	if err := cmd.Start(); err != nil {
		return 0, "", "", err
	}
	return cmd.Process.Pid, stdoutPath, stderrPath, nil
}

func startDetachedContainer(r *repo.Repo, cfg *GuardConfig, input ActionPolicyInput, decisionAction string, requested, effective RuntimeProfile, record *SpawnRecord) (string, string, error) {
	runtimeName, err := resolveContainerRuntime(cfg)
	if err != nil {
		return "", "", err
	}
	hostRoot, containerWorkdir, err := containerWorkspacePaths(r)
	if err != nil {
		return "", "", fmt.Errorf("resolve container workspace: %w", err)
	}
	invocation, err := buildDetachedContainerInvocation(runtimeName, cfg.ContainerImage, hostRoot, containerWorkdir, input, decisionAction, requested, effective, record)
	if err != nil {
		return "", "", err
	}

	cmd := exec.Command(invocation.Runtime, invocation.Args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", "", fmt.Errorf("start detached container: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return runtimeName, strings.TrimSpace(string(output)), nil
}

func coorddSpawnDir(r *repo.Repo) string {
	if cwd, err := os.Getwd(); err == nil && cwd != "" {
		return cwd
	}
	if r != nil {
		return r.RootDir
	}
	return "."
}

func openSpawnIO(graftDir, id string) (string, string, *os.File, *os.File, *os.File, error) {
	if err := os.MkdirAll(SpawnsDir(graftDir), 0o755); err != nil {
		return "", "", nil, nil, nil, fmt.Errorf("create spawns dir: %w", err)
	}

	stdoutPath := SpawnStdoutPath(graftDir, id)
	stderrPath := SpawnStderrPath(graftDir, id)
	stdoutFile, err := os.OpenFile(stdoutPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return "", "", nil, nil, nil, fmt.Errorf("open spawn stdout: %w", err)
	}
	stderrFile, err := os.OpenFile(stderrPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		stdoutFile.Close()
		return "", "", nil, nil, nil, fmt.Errorf("open spawn stderr: %w", err)
	}
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		stdoutFile.Close()
		stderrFile.Close()
		return "", "", nil, nil, nil, fmt.Errorf("open dev null: %w", err)
	}
	return stdoutPath, stderrPath, stdoutFile, stderrFile, devNull, nil
}

func coorddSpawnEnv(input ActionPolicyInput, decisionAction string, requested, effective RuntimeProfile, record *SpawnRecord) []string {
	env := []string{
		"GRAFT_COORDD_GUARDED=1",
		"GRAFT_COORDD_SELECTOR=" + input.Action.Selector,
		"GRAFT_COORDD_POLICY_ACTION=" + decisionAction,
		"GRAFT_COORDD_REQUESTED_PROFILE=" + requested.Name,
		"GRAFT_COORDD_EFFECTIVE_PROFILE=" + effective.Name,
		"GRAFT_COORDD_PARENT_AGENT_ID=" + record.ParentAgentID,
		"GRAFT_COORDD_CHILD_NAME=" + record.Name,
		"GRAFT_COORDD_SPAWN_ID=" + record.ID,
	}
	if record.ParentSpawnID != "" {
		env = append(env, "GRAFT_COORDD_PARENT_SPAWN_ID="+record.ParentSpawnID)
	}
	if record.SnapshotID != "" {
		env = append(env, "GRAFT_COORDD_SNAPSHOT_ID="+record.SnapshotID)
	}
	return env
}

func buildDetachedBwrapArgs(r *repo.Repo, input ActionPolicyInput, requested RuntimeProfile) []string {
	cwd := coorddSpawnDir(r)
	args := []string{
		"--new-session",
		"--proc", "/proc",
		"--dev", "/dev",
		"--ro-bind", "/", "/",
		"--tmpfs", "/tmp",
	}
	if requested.Network == NetworkDeny {
		args = append(args, "--unshare-net")
	}
	if r != nil {
		switch requested.FilesystemScope {
		case FilesystemScopeRepoRW:
			args = append(args, "--bind", r.RootDir, r.RootDir)
		case FilesystemScopeRepoRO:
			args = append(args, "--ro-bind", r.RootDir, r.RootDir)
		}
	}
	if cwd != "" {
		if r != nil {
			if rel, relErr := filepath.Rel(r.RootDir, cwd); relErr == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				args = append(args, "--chdir", cwd)
			} else {
				args = append(args, "--chdir", r.RootDir)
			}
		} else {
			args = append(args, "--chdir", cwd)
		}
	}
	args = append(args, "--")
	args = append(args, input.Action.Argv...)
	return args
}

func buildDetachedContainerInvocation(runtimeName, image, hostRoot, containerWorkdir string, input ActionPolicyInput, decisionAction string, requested, effective RuntimeProfile, record *SpawnRecord) (*ContainerInvocation, error) {
	if runtimeName == "" {
		return nil, fmt.Errorf("missing container runtime")
	}
	if strings.TrimSpace(image) == "" {
		return nil, fmt.Errorf("missing container image")
	}
	if hostRoot == "" {
		return nil, fmt.Errorf("missing host root for container mount")
	}
	if containerWorkdir == "" {
		containerWorkdir = "/workspace"
	}

	args := []string{"run", "-d", "--read-only"}
	switch runtimeName {
	case "podman":
		args = append(args, "--userns=keep-id", "--security-opt", "no-new-privileges")
	case "docker":
		args = append(args, "--user", strconv.Itoa(os.Getuid())+":"+strconv.Itoa(os.Getgid()), "--security-opt", "no-new-privileges")
	default:
		return nil, fmt.Errorf("unsupported container runtime %q", runtimeName)
	}

	args = append(args,
		"--cap-drop=ALL",
		"--pids-limit=256",
		"--tmpfs", "/tmp:rw,nosuid,nodev",
		"--tmpfs", "/home/coordd:rw,nosuid,nodev",
		"--workdir", containerWorkdir,
		"--env", "HOME=/home/coordd",
		"--env", "GRAFT_COORDD_GUARDED=1",
		"--env", "GRAFT_COORDD_SELECTOR="+input.Action.Selector,
		"--env", "GRAFT_COORDD_POLICY_ACTION="+decisionAction,
		"--env", "GRAFT_COORDD_REQUESTED_PROFILE="+requested.Name,
		"--env", "GRAFT_COORDD_EFFECTIVE_PROFILE="+effective.Name,
		"--env", "GRAFT_COORDD_PARENT_AGENT_ID="+record.ParentAgentID,
		"--env", "GRAFT_COORDD_CHILD_NAME="+record.Name,
		"--env", "GRAFT_COORDD_SPAWN_ID="+record.ID,
	)
	if record.ParentSpawnID != "" {
		args = append(args, "--env", "GRAFT_COORDD_PARENT_SPAWN_ID="+record.ParentSpawnID)
	}
	if record.SnapshotID != "" {
		args = append(args, "--env", "GRAFT_COORDD_SNAPSHOT_ID="+record.SnapshotID)
	}
	if requested.Network == NetworkDeny {
		args = append(args, "--network", "none")
	}

	mountMode := ":ro"
	if requested.FilesystemScope == FilesystemScopeRepoRW {
		mountMode = ":rw"
	}
	args = append(args, "-v", hostRoot+":/workspace"+mountMode)
	args = append(args, image)
	args = append(args, input.Action.Argv...)
	return &ContainerInvocation{Runtime: runtimeName, Args: args}, nil
}
