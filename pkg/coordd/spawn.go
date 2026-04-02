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
	"time"

	"github.com/odvcencio/arbiter"
	"github.com/odvcencio/arbiter/govern"
	"github.com/odvcencio/arbiter/overrides"
	"github.com/odvcencio/arbiter/vm"
	"github.com/odvcencio/graft/pkg/coord"
	"github.com/odvcencio/graft/pkg/repo"
)

//go:embed default_spawn_policy.arb
var defaultSpawnPolicySource []byte

type SpawnRequest struct {
	Name             string   `json:"name"`
	Command          []string `json:"command"`
	RequestedProfile string   `json:"requested_profile,omitempty"`
	Runtime          string   `json:"runtime,omitempty"`
	Launch           string   `json:"launch,omitempty"`
	BootstrapCoord   bool     `json:"bootstrap_coord,omitempty"`
	TaskID           string   `json:"task_id,omitempty"`
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
	Runtime          string `json:"runtime,omitempty"`
	RuntimeValid     bool   `json:"runtime_valid"`
	EscalatesParent  bool   `json:"escalates_parent"`
}

type SpawnPolicyDecision struct {
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

type SpawnTaskBinding struct {
	ID         string `json:"id"`
	Title      string `json:"title,omitempty"`
	Status     string `json:"status,omitempty"`
	AssignedTo string `json:"assigned_to,omitempty"`
}

type SpawnRecord struct {
	ID               string                `json:"id"`
	Name             string                `json:"name"`
	LaunchMode       string                `json:"launch_mode,omitempty"`
	BootstrapCoord   bool                  `json:"bootstrap_coord,omitempty"`
	ParentAgentID    string                `json:"parent_agent_id,omitempty"`
	ParentSpawnID    string                `json:"parent_spawn_id,omitempty"`
	ParentProfile    string                `json:"parent_profile,omitempty"`
	RepoRoot         string                `json:"repo_root,omitempty"`
	Command          []string              `json:"command,omitempty"`
	Selector         string                `json:"selector,omitempty"`
	Backend          string                `json:"backend,omitempty"`
	RequestedRuntime string                `json:"requested_runtime,omitempty"`
	RequestedProfile RuntimeProfile        `json:"requested_profile,omitempty"`
	EffectiveProfile RuntimeProfile        `json:"effective_profile,omitempty"`
	Degradations     []string              `json:"degradations,omitempty"`
	Task             *SpawnTaskBinding     `json:"task,omitempty"`
	ActionInput      ActionPolicyInput     `json:"action_input,omitempty"`
	ActionDecision   *ActionPolicyDecision `json:"action_decision,omitempty"`
	SpawnInput       SpawnPolicyInput      `json:"spawn_input,omitempty"`
	SpawnDecision    *SpawnPolicyDecision  `json:"spawn_decision,omitempty"`
	Status           string                `json:"status"`
	ChildAgentID     string                `json:"child_agent_id,omitempty"`
	ChildAgentName   string                `json:"child_agent_name,omitempty"`
	LastHeartbeatAt  time.Time             `json:"last_heartbeat_at,omitempty"`
	FinishedAt       time.Time             `json:"finished_at,omitempty"`
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
	LaunchMode       string                `json:"launch_mode,omitempty"`
	Backend          string                `json:"backend,omitempty"`
	RequestedRuntime string                `json:"requested_runtime,omitempty"`
	RequestedProfile RuntimeProfile        `json:"requested_profile,omitempty"`
	EffectiveProfile RuntimeProfile        `json:"effective_profile,omitempty"`
	Degradations     []string              `json:"degradations,omitempty"`
	SnapshotID       string                `json:"snapshot_id,omitempty"`
	Record           *SpawnRecord          `json:"record,omitempty"`
}

type SpawnLease struct {
	ID               string            `json:"id"`
	Name             string            `json:"name"`
	LaunchMode       string            `json:"launch_mode,omitempty"`
	BootstrapCoord   bool              `json:"bootstrap_coord,omitempty"`
	ParentAgentID    string            `json:"parent_agent_id,omitempty"`
	ParentSpawnID    string            `json:"parent_spawn_id,omitempty"`
	ChildAgentID     string            `json:"child_agent_id,omitempty"`
	ChildAgentName   string            `json:"child_agent_name,omitempty"`
	RepoRoot         string            `json:"repo_root,omitempty"`
	Command          []string          `json:"command,omitempty"`
	Backend          string            `json:"backend,omitempty"`
	RequestedRuntime string            `json:"requested_runtime,omitempty"`
	RequestedProfile RuntimeProfile    `json:"requested_profile,omitempty"`
	EffectiveProfile RuntimeProfile    `json:"effective_profile,omitempty"`
	SnapshotID       string            `json:"snapshot_id,omitempty"`
	Status           string            `json:"status,omitempty"`
	Task             *SpawnTaskBinding `json:"task,omitempty"`
	Env              map[string]string `json:"env,omitempty"`
}

type SpawnView struct {
	Record *SpawnRecord `json:"record,omitempty"`
	Lease  *SpawnLease  `json:"lease,omitempty"`
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

func BuildSpawnLease(record *SpawnRecord) *SpawnLease {
	if record == nil {
		return nil
	}
	var task *SpawnTaskBinding
	if record.Task != nil {
		copied := *record.Task
		task = &copied
	}
	lease := &SpawnLease{
		ID:               record.ID,
		Name:             record.Name,
		LaunchMode:       record.LaunchMode,
		BootstrapCoord:   record.BootstrapCoord,
		ParentAgentID:    record.ParentAgentID,
		ParentSpawnID:    record.ParentSpawnID,
		ChildAgentID:     record.ChildAgentID,
		ChildAgentName:   record.ChildAgentName,
		RepoRoot:         record.RepoRoot,
		Command:          append([]string(nil), record.Command...),
		Backend:          record.Backend,
		RequestedRuntime: record.RequestedRuntime,
		RequestedProfile: record.RequestedProfile,
		EffectiveProfile: record.EffectiveProfile,
		SnapshotID:       record.SnapshotID,
		Status:           record.Status,
		Task:             task,
		Env:              map[string]string{},
	}
	addSpawnLeaseEnv(lease.Env, record)
	return lease
}

func LoadSpawnView(graftDir, id string) (*SpawnView, error) {
	record, err := LoadSpawnRecord(graftDir, id)
	if err != nil {
		return nil, err
	}
	if record == nil {
		return nil, nil
	}
	return &SpawnView{
		Record: record,
		Lease:  BuildSpawnLease(record),
	}, nil
}

func WaitSpawn(graftDir, id string, timeout, pollInterval time.Duration) (*SpawnRecord, error) {
	if strings.TrimSpace(id) == "" {
		return nil, fmt.Errorf("spawn id is required")
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	if pollInterval <= 0 {
		pollInterval = 200 * time.Millisecond
	}

	deadline := time.Now().Add(timeout)
	for {
		record, err := LoadSpawnRecord(graftDir, id)
		if err != nil {
			return nil, err
		}
		if record == nil {
			return nil, fmt.Errorf("spawn %q not found", id)
		}
		if isTerminalSpawnStatus(record.Status) {
			return record, nil
		}
		if time.Now().After(deadline) {
			return record, fmt.Errorf("timed out waiting for spawn %q", id)
		}
		time.Sleep(pollInterval)
	}
}

func ConsumeSpawn(graftDir, id, childAgentID string) (*SpawnView, error) {
	record, err := TouchSpawn(graftDir, id, childAgentID)
	if err != nil {
		return nil, err
	}
	_ = AppendEvent(graftDir, Event{
		ID:        newID(),
		Type:      "spawn_consumed",
		Timestamp: time.Now().UTC(),
		RepoRoot:  record.RepoRoot,
		AgentID:   record.ParentAgentID,
		Data: map[string]any{
			"id":               record.ID,
			"name":             record.Name,
			"status":           record.Status,
			"child_agent_id":   record.ChildAgentID,
			"child_agent_name": record.ChildAgentName,
			"task":             record.Task,
		},
	})
	return &SpawnView{
		Record: record,
		Lease:  BuildSpawnLease(record),
	}, nil
}

func AttachSpawn(r *repo.Repo, id string, heartbeatInterval time.Duration, execIO ExecIO) (*SpawnRecord, error) {
	if r == nil {
		return nil, fmt.Errorf("attach requires an open repo")
	}
	view, err := ConsumeSpawn(r.GraftDir, id, "")
	if err != nil {
		return nil, err
	}
	if view == nil || view.Record == nil {
		return nil, fmt.Errorf("spawn %q not found", id)
	}
	record := view.Record
	if record.LaunchMode != "lease" {
		return nil, fmt.Errorf("spawn %q is not a lease", id)
	}
	if len(record.Command) == 0 {
		return nil, fmt.Errorf("spawn %q has no command to attach", id)
	}
	if heartbeatInterval <= 0 {
		heartbeatInterval = 5 * time.Second
	}

	cmd := exec.Command(record.Command[0], record.Command[1:]...)
	cmd.Stdin = execInput(execIO.Stdin, os.Stdin)
	cmd.Stdout = execOutput(execIO.Stdout, os.Stdout)
	cmd.Stderr = execOutput(execIO.Stderr, os.Stderr)
	cmd.Dir = coorddSpawnDir(r)

	lease := BuildSpawnLease(record)
	env := make([]string, 0, len(lease.Env))
	for key, value := range lease.Env {
		if strings.TrimSpace(key) == "" {
			continue
		}
		env = append(env, key+"="+value)
	}
	sort.Strings(env)
	cmd.Env = append(os.Environ(), env...)

	if err := cmd.Start(); err != nil {
		_, _ = FinishSpawn(r.GraftDir, id, "failed", record.ChildAgentID)
		return record, err
	}
	record.PID = cmd.Process.Pid
	if err := SaveSpawnRecord(r.GraftDir, record); err != nil {
		_, _ = FinishSpawn(r.GraftDir, id, "failed", record.ChildAgentID)
		return record, err
	}

	_ = AppendEvent(r.GraftDir, Event{
		ID:        newID(),
		Type:      "spawn_attached",
		Timestamp: time.Now().UTC(),
		RepoRoot:  r.RootDir,
		AgentID:   record.ParentAgentID,
		Data: map[string]any{
			"id":               record.ID,
			"name":             record.Name,
			"pid":              record.PID,
			"child_agent_id":   record.ChildAgentID,
			"child_agent_name": record.ChildAgentName,
			"task":             record.Task,
		},
	})

	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(heartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				_, _ = TouchSpawn(r.GraftDir, id, record.ChildAgentID)
			case <-done:
				return
			}
		}
	}()

	waitErr := cmd.Wait()
	close(done)

	status := "completed"
	if waitErr != nil {
		status = "failed"
	}
	finished, finishErr := FinishSpawn(r.GraftDir, id, status, record.ChildAgentID)
	if finishErr != nil {
		return finished, finishErr
	}
	if waitErr != nil {
		if exitErr, ok := waitErr.(*exec.ExitError); ok {
			return finished, &ExitCodeError{
				Code: exitErr.ExitCode(),
				Err:  waitErr,
			}
		}
		return finished, waitErr
	}
	return finished, nil
}

func EvaluateSpawnPolicy(input SpawnPolicyInput) (*SpawnPolicyDecision, error) {
	return evaluateSpawnPolicyWithView(nil, input, "", nil)
}

func EvaluateSpawnPolicyWithRepo(r *repo.Repo, input SpawnPolicyInput) (*SpawnPolicyDecision, error) {
	if r == nil {
		return EvaluateSpawnPolicy(input)
	}
	view, err := loadGuardOverrideView(r.GraftDir)
	if err != nil {
		return nil, err
	}
	return evaluateSpawnPolicyWithView(r, input, spawnPolicyBundleID, view)
}

func evaluateSpawnPolicyWithView(r *repo.Repo, input SpawnPolicyInput, bundleID string, view overrides.View) (*SpawnPolicyDecision, error) {
	bundle, err := spawnPolicyLoader.load(r)
	if err != nil {
		return nil, err
	}

	ctx := spawnPolicyContext(input)
	dc := arbiter.DataFromMap(spawnInputToMap(input), bundle.full)
	var (
		matched  []vm.MatchedRule
		govTrace *govern.Arbitrace
	)
	if view != nil {
		matched, govTrace, err = arbiter.EvalGovernedWithOverrides(bundle.full, dc, bundle.full.Segments, ctx, bundleID, view)
	} else {
		matched, govTrace, err = arbiter.EvalGoverned(bundle.full, dc, bundle.full.Segments, ctx)
	}
	if err != nil {
		return nil, fmt.Errorf("evaluate spawn policy: %w", err)
	}

	ordered := orderedMatchedRules(matched)
	decision := &SpawnPolicyDecision{
		Bundle:     bundle.infoCopy(),
		Trace:      matchedRuleTrace(bundle, matched),
		Governance: governanceTraceSteps(bundle, govTrace),
	}
	if len(ordered) == 0 {
		return nil, fmt.Errorf("spawn policy produced no matched rules")
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

func BuildSpawnPolicyInput(actionInput ActionPolicyInput, actionDecision *ActionPolicyDecision, req SpawnRequest) SpawnPolicyInput {
	requestedName := strings.TrimSpace(req.RequestedProfile)
	requestedValid := requestedName == "" || isCoorddSpawnProfile(requestedName)
	runtimeName := normalizeSpawnRuntime(req.Runtime)
	runtimeValid := isCoorddSpawnRuntime(runtimeName)

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
			Runtime:          runtimeName,
			RuntimeValid:     runtimeValid,
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
			"runtime":           input.Spawn.Runtime,
			"decision":          decision.Action,
			"reason":            decision.Reason,
			"rule":              decision.Rule,
			"profile":           decision.Profile,
			"parent_spawn_id":   input.Parent.SpawnID,
		},
	})
}

func SpawnDetached(r *repo.Repo, activeAgentID string, req SpawnRequest) (*SpawnResult, error) {
	prepared, cfg, err := prepareSpawn(r, activeAgentID, req)
	if err != nil {
		return prepared, err
	}
	result := prepared
	record := result.Record

	switch result.Backend {
	case "host-direct":
		pid, stdoutPath, stderrPath, err := startDetachedDirect(r, result.ActionInput, result.SpawnDecision.Action, result.RequestedProfile, result.EffectiveProfile, record)
		if err != nil {
			return result, err
		}
		record.PID = pid
		record.StdoutPath = stdoutPath
		record.StderrPath = stderrPath
	case "host-bwrap":
		pid, stdoutPath, stderrPath, err := startDetachedBwrap(r, result.ActionInput, result.SpawnDecision.Action, result.RequestedProfile, result.EffectiveProfile, record)
		if err != nil {
			return result, err
		}
		record.PID = pid
		record.StdoutPath = stdoutPath
		record.StderrPath = stderrPath
	case "container":
		runtimeName, containerID, err := startDetachedContainer(r, cfg, result.ActionInput, result.SpawnDecision.Action, result.RequestedProfile, result.EffectiveProfile, record)
		if err != nil {
			return result, err
		}
		record.ContainerRuntime = runtimeName
		record.ContainerID = containerID
	default:
		return result, fmt.Errorf("unknown coordd backend %q", result.Backend)
	}

	if err := activateSpawnTaskBinding(r, record); err != nil {
		return result, err
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
			"launch_mode":       record.LaunchMode,
			"bootstrap_coord":   record.BootstrapCoord,
			"selector":          record.Selector,
			"backend":           record.Backend,
			"requested_runtime": record.RequestedRuntime,
			"requested_profile": record.RequestedProfile,
			"effective_profile": record.EffectiveProfile,
			"degradations":      record.Degradations,
			"pid":               record.PID,
			"container_runtime": record.ContainerRuntime,
			"container_id":      record.ContainerID,
			"snapshot_id":       record.SnapshotID,
			"parent_spawn_id":   record.ParentSpawnID,
			"child_agent_id":    record.ChildAgentID,
			"child_agent_name":  record.ChildAgentName,
			"task":              record.Task,
		},
	})

	result.Record = record
	return result, nil
}

func AuthorizeSpawn(r *repo.Repo, activeAgentID string, req SpawnRequest) (*SpawnResult, error) {
	prepared, _, err := prepareSpawn(r, activeAgentID, req)
	if err != nil {
		return prepared, err
	}
	result := prepared
	record := result.Record
	record.Status = "authorized"
	record.LastHeartbeatAt = record.StartedAt

	if record.BootstrapCoord {
		if err := touchSpawnCoordIdentity(r, record); err != nil {
			return result, err
		}
	}

	if err := SaveSpawnRecord(r.GraftDir, record); err != nil {
		return result, err
	}

	_ = AppendEvent(r.GraftDir, Event{
		ID:        newID(),
		Type:      "spawn_authorized",
		Timestamp: time.Now().UTC(),
		RepoRoot:  r.RootDir,
		AgentID:   activeAgentID,
		Data: map[string]any{
			"id":                record.ID,
			"name":              record.Name,
			"launch_mode":       record.LaunchMode,
			"bootstrap_coord":   record.BootstrapCoord,
			"selector":          record.Selector,
			"backend":           record.Backend,
			"requested_runtime": record.RequestedRuntime,
			"requested_profile": record.RequestedProfile,
			"effective_profile": record.EffectiveProfile,
			"degradations":      record.Degradations,
			"snapshot_id":       record.SnapshotID,
			"parent_spawn_id":   record.ParentSpawnID,
			"child_agent_id":    record.ChildAgentID,
			"child_agent_name":  record.ChildAgentName,
			"task":              record.Task,
		},
	})

	return result, nil
}

func TouchSpawn(graftDir, id, childAgentID string) (*SpawnRecord, error) {
	record, err := LoadSpawnRecord(graftDir, id)
	if err != nil {
		return nil, err
	}
	if record == nil {
		return nil, fmt.Errorf("spawn %q not found", id)
	}

	now := time.Now().UTC()
	record.LastHeartbeatAt = now
	if strings.TrimSpace(childAgentID) != "" && strings.TrimSpace(record.ChildAgentID) == "" {
		record.ChildAgentID = strings.TrimSpace(childAgentID)
	}
	if record.Status == "authorized" {
		record.Status = "active"
	}
	if record.BootstrapCoord && record.RepoRoot != "" {
		if opened, err := repo.Open(record.RepoRoot); err == nil {
			if err := touchSpawnCoordIdentity(opened, record); err != nil {
				return nil, err
			}
			if err := activateSpawnTaskBinding(opened, record); err != nil {
				return nil, err
			}
		}
	} else if record.RepoRoot != "" {
		if opened, err := repo.Open(record.RepoRoot); err == nil {
			if err := activateSpawnTaskBinding(opened, record); err != nil {
				return nil, err
			}
		}
	}

	if err := SaveSpawnRecord(graftDir, record); err != nil {
		return nil, err
	}
	_ = AppendEvent(graftDir, Event{
		ID:        newID(),
		Type:      "spawn_heartbeat",
		Timestamp: now,
		RepoRoot:  record.RepoRoot,
		AgentID:   record.ParentAgentID,
		Data: map[string]any{
			"id":               record.ID,
			"name":             record.Name,
			"status":           record.Status,
			"child_agent_id":   record.ChildAgentID,
			"child_agent_name": record.ChildAgentName,
			"task":             record.Task,
		},
	})
	return record, nil
}

func FinishSpawn(graftDir, id, status, childAgentID string) (*SpawnRecord, error) {
	record, err := LoadSpawnRecord(graftDir, id)
	if err != nil {
		return nil, err
	}
	if record == nil {
		return nil, fmt.Errorf("spawn %q not found", id)
	}

	now := time.Now().UTC()
	record.Status = normalizeFinishedSpawnStatus(status)
	record.FinishedAt = now
	record.LastHeartbeatAt = now
	if strings.TrimSpace(childAgentID) != "" && strings.TrimSpace(record.ChildAgentID) == "" {
		record.ChildAgentID = strings.TrimSpace(childAgentID)
	}
	if record.BootstrapCoord && record.RepoRoot != "" {
		if opened, err := repo.Open(record.RepoRoot); err == nil {
			if err := finishSpawnCoordIdentity(opened, record); err != nil {
				return nil, err
			}
			if err := finishSpawnTaskBinding(opened, record); err != nil {
				return nil, err
			}
		}
	} else if record.RepoRoot != "" {
		if opened, err := repo.Open(record.RepoRoot); err == nil {
			if err := finishSpawnTaskBinding(opened, record); err != nil {
				return nil, err
			}
		}
	}

	if err := SaveSpawnRecord(graftDir, record); err != nil {
		return nil, err
	}
	_ = AppendEvent(graftDir, Event{
		ID:        newID(),
		Type:      "spawn_finished",
		Timestamp: now,
		RepoRoot:  record.RepoRoot,
		AgentID:   record.ParentAgentID,
		Data: map[string]any{
			"id":               record.ID,
			"name":             record.Name,
			"status":           record.Status,
			"child_agent_id":   record.ChildAgentID,
			"child_agent_name": record.ChildAgentName,
			"task":             record.Task,
		},
	})
	return record, nil
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
			"runtime":           in.Spawn.Runtime,
			"runtime_valid":     in.Spawn.RuntimeValid,
			"escalates_parent":  in.Spawn.EscalatesParent,
		},
	}
}

func spawnPolicyContext(in SpawnPolicyInput) map[string]any {
	ctx := spawnInputToMap(in)
	if agentID := strings.TrimSpace(in.Session.AgentID); agentID != "" {
		ctx["user.id"] = agentID
		ctx["user_id"] = agentID
	} else if spawnID := strings.TrimSpace(in.Parent.SpawnID); spawnID != "" {
		ctx["user.id"] = spawnID
		ctx["user_id"] = spawnID
	}
	if selector := strings.TrimSpace(in.Action.Selector); selector != "" {
		ctx["request.id"] = selector
	}
	return ctx
}

func selectSpawnBackend(r *repo.Repo, cfg *GuardConfig, requested RuntimeProfile, runtimeName string) (string, RuntimeProfile, []string, error) {
	switch normalizeSpawnRuntime(runtimeName) {
	case "auto":
		return selectExecBackend(r, cfg, requested)
	case "detached":
		if canUseBwrap(r, requested) {
			return "host-bwrap", requested, nil, nil
		}
		effective, degradations := directEffectiveProfile(requested)
		return "host-direct", effective, degradations, nil
	case "container":
		return selectExecBackendForPreference(r, cfg, requested, "container")
	default:
		return "", RuntimeProfile{}, nil, fmt.Errorf("unknown spawn runtime %q", runtimeName)
	}
}

func prepareSpawn(r *repo.Repo, activeAgentID string, req SpawnRequest) (*SpawnResult, *GuardConfig, error) {
	if r == nil {
		return nil, nil, fmt.Errorf("spawn requires an open repo")
	}

	launchMode := normalizeSpawnLaunch(req.Launch)
	if !isCoorddSpawnLaunch(launchMode) {
		return nil, nil, fmt.Errorf("unknown spawn launch %q", req.Launch)
	}

	actionInput, err := BuildShellActionInput(r, activeAgentID, req.Command)
	if err != nil {
		return nil, nil, err
	}
	actionDecision, err := EvaluateActionPolicyWithRepo(r, actionInput)
	if err != nil {
		return nil, nil, err
	}

	spawnInput := BuildSpawnPolicyInput(actionInput, actionDecision, req)
	spawnDecision, err := EvaluateSpawnPolicyWithRepo(r, spawnInput)
	if err != nil {
		return nil, nil, err
	}
	_ = RecordSpawnPreflightDecision(r.GraftDir, spawnInput, spawnDecision)

	result := &SpawnResult{
		ActionInput:      actionInput,
		ActionDecision:   actionDecision,
		SpawnInput:       spawnInput,
		SpawnDecision:    spawnDecision,
		LaunchMode:       launchMode,
		RequestedRuntime: spawnInput.Spawn.Runtime,
	}

	requestedProfile := ResolveRuntimeProfile(spawnDecision.Profile, actionInput.Action)
	result.RequestedProfile = requestedProfile
	if spawnDecision.Action == "HardBlock" {
		result.EffectiveProfile = requestedProfile
		return result, nil, &ExitCodeError{
			Code: 126,
			Err:  fmt.Errorf("coordd blocked spawn: %s", spawnDecision.Reason),
		}
	}

	cfg, err := loadGuardConfigForExec(r)
	if err != nil {
		return result, nil, err
	}
	if requestedProfile.RequireSnapshot {
		snapshotID, snapshotErr := captureExecSnapshot(r, activeAgentID)
		if snapshotErr != nil {
			return result, nil, snapshotErr
		}
		result.SnapshotID = snapshotID
	}

	backendName, effectiveProfile, degradations, err := selectSpawnBackend(r, cfg, requestedProfile, spawnInput.Spawn.Runtime)
	if err != nil {
		return result, nil, err
	}
	result.Backend = backendName
	result.EffectiveProfile = effectiveProfile
	result.Degradations = append(result.Degradations, degradations...)

	record := &SpawnRecord{
		ID:               newID(),
		Name:             strings.TrimSpace(req.Name),
		LaunchMode:       launchMode,
		BootstrapCoord:   req.BootstrapCoord,
		ParentAgentID:    activeAgentID,
		ParentSpawnID:    spawnInput.Parent.SpawnID,
		ParentProfile:    spawnInput.Parent.Profile,
		RepoRoot:         r.RootDir,
		Command:          append([]string(nil), req.Command...),
		Selector:         actionInput.Action.Selector,
		Backend:          backendName,
		RequestedRuntime: spawnInput.Spawn.Runtime,
		RequestedProfile: requestedProfile,
		EffectiveProfile: effectiveProfile,
		Degradations:     append([]string(nil), degradations...),
		ActionInput:      actionInput,
		ActionDecision:   actionDecision,
		SpawnInput:       spawnInput,
		SpawnDecision:    spawnDecision,
		Status:           "running",
		SnapshotID:       result.SnapshotID,
		StartedAt:        time.Now().UTC(),
	}
	if strings.TrimSpace(req.TaskID) != "" {
		task, taskErr := loadSpawnTask(r, req.TaskID)
		if taskErr != nil {
			return result, nil, taskErr
		}
		record.Task = buildSpawnTaskBinding(task)
	}
	if record.BootstrapCoord {
		if err := bootstrapSpawnCoordIdentity(r, record); err != nil {
			return result, nil, err
		}
	}
	if err := refreshSpawnTaskBinding(r, record); err != nil {
		return result, nil, err
	}
	result.Record = record
	return result, cfg, nil
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

func normalizeSpawnRuntime(runtimeName string) string {
	switch strings.TrimSpace(runtimeName) {
	case "", "auto":
		return "auto"
	case "detached":
		return "detached"
	case "container":
		return "container"
	default:
		return strings.TrimSpace(runtimeName)
	}
}

func normalizeSpawnLaunch(launch string) string {
	switch strings.TrimSpace(launch) {
	case "", "detached":
		return "detached"
	case "lease":
		return "lease"
	default:
		return strings.TrimSpace(launch)
	}
}

func isCoorddSpawnLaunch(launch string) bool {
	switch normalizeSpawnLaunch(launch) {
	case "detached", "lease":
		return true
	default:
		return false
	}
}

func normalizeFinishedSpawnStatus(status string) string {
	switch strings.TrimSpace(status) {
	case "", "completed":
		return "completed"
	case "failed", "aborted", "blocked":
		return strings.TrimSpace(status)
	default:
		return strings.TrimSpace(status)
	}
}

func isTerminalSpawnStatus(status string) bool {
	switch strings.TrimSpace(status) {
	case "completed", "failed", "aborted", "blocked":
		return true
	default:
		return false
	}
}

func bootstrapSpawnCoordIdentity(r *repo.Repo, record *SpawnRecord) error {
	if r == nil || record == nil || !record.BootstrapCoord {
		return nil
	}
	if strings.TrimSpace(record.ChildAgentID) != "" && strings.TrimSpace(record.ChildAgentName) != "" {
		return nil
	}

	hostname, _ := os.Hostname()
	c := coord.New(r, coord.DefaultConfig)
	agentName := spawnCoordAgentName(record)
	id, err := c.RegisterAgent(coord.AgentInfo{
		Name:      agentName,
		Workspace: filepath.Base(r.RootDir),
		Host:      hostname,
	})
	if err != nil {
		return fmt.Errorf("register spawn coord agent: %w", err)
	}

	startedAt := c.AgentStartedAt()
	session := &coord.Session{
		AgentID:    id,
		AgentName:  agentName,
		Workspace:  filepath.Base(r.RootDir),
		Host:       hostname,
		StartedAt:  startedAt,
		LastActive: startedAt,
		PID:        os.Getpid(),
		Mode:       "editing",
	}
	if err := coord.SaveSession(r.GraftDir, session); err != nil {
		_ = c.DeregisterAgent(id)
		return fmt.Errorf("save spawn coord session: %w", err)
	}

	record.ChildAgentID = id
	record.ChildAgentName = agentName
	return nil
}

func touchSpawnCoordIdentity(r *repo.Repo, record *SpawnRecord) error {
	if r == nil || record == nil || !record.BootstrapCoord {
		return nil
	}
	if strings.TrimSpace(record.ChildAgentID) == "" || strings.TrimSpace(record.ChildAgentName) == "" {
		return bootstrapSpawnCoordIdentity(r, record)
	}

	c := coord.New(r, coord.DefaultConfig)
	if err := c.Heartbeat(record.ChildAgentID); err != nil {
		return fmt.Errorf("heartbeat spawn coord agent: %w", err)
	}
	session, err := coord.LoadSession(r.GraftDir, record.ChildAgentName)
	if err != nil {
		return fmt.Errorf("load spawn coord session: %w", err)
	}
	if session != nil {
		if err := coord.TouchSession(r.GraftDir, session); err != nil {
			return fmt.Errorf("touch spawn coord session: %w", err)
		}
	}
	return nil
}

func finishSpawnCoordIdentity(r *repo.Repo, record *SpawnRecord) error {
	if r == nil || record == nil || !record.BootstrapCoord {
		return nil
	}
	if strings.TrimSpace(record.ChildAgentID) == "" {
		return nil
	}

	c := coord.New(r, coord.DefaultConfig)
	if err := c.DeregisterAgent(record.ChildAgentID); err != nil {
		return fmt.Errorf("deregister spawn coord agent: %w", err)
	}
	if strings.TrimSpace(record.ChildAgentName) != "" {
		if err := coord.RemoveSession(r.GraftDir, record.ChildAgentName); err != nil {
			return fmt.Errorf("remove spawn coord session: %w", err)
		}
	}
	return nil
}

func buildSpawnTaskBinding(task *coord.Task) *SpawnTaskBinding {
	if task == nil {
		return nil
	}
	return &SpawnTaskBinding{
		ID:         task.ID,
		Title:      task.Title,
		Status:     task.Status,
		AssignedTo: task.AssignedTo,
	}
}

func loadSpawnTask(r *repo.Repo, taskID string) (*coord.Task, error) {
	if r == nil || strings.TrimSpace(taskID) == "" {
		return nil, nil
	}
	c := coord.New(r, coord.DefaultConfig)
	task, err := c.GetTask(strings.TrimSpace(taskID))
	if err != nil {
		return nil, fmt.Errorf("get bound task %q: %w", taskID, err)
	}
	return task, nil
}

func refreshSpawnTaskBinding(r *repo.Repo, record *SpawnRecord) error {
	if record == nil || record.Task == nil || strings.TrimSpace(record.Task.ID) == "" {
		return nil
	}
	task, err := loadSpawnTask(r, record.Task.ID)
	if err != nil {
		return err
	}
	record.Task = buildSpawnTaskBinding(task)
	return nil
}

func spawnTaskAssignee(record *SpawnRecord) string {
	if record == nil {
		return ""
	}
	if strings.TrimSpace(record.ChildAgentName) != "" {
		return strings.TrimSpace(record.ChildAgentName)
	}
	if strings.TrimSpace(record.Name) != "" {
		return strings.TrimSpace(record.Name)
	}
	return strings.TrimSpace(record.ChildAgentID)
}

func activateSpawnTaskBinding(r *repo.Repo, record *SpawnRecord) error {
	if r == nil || record == nil || record.Task == nil || strings.TrimSpace(record.Task.ID) == "" {
		return nil
	}
	c := coord.New(r, coord.DefaultConfig)
	task, err := c.GetTask(record.Task.ID)
	if err != nil {
		return fmt.Errorf("load bound task: %w", err)
	}
	assignee := spawnTaskAssignee(record)
	changed := false
	if assignee != "" && task.AssignedTo != assignee {
		task.AssignedTo = assignee
		changed = true
	}
	if task.Status == "" || task.Status == "pending" {
		task.Status = "in_progress"
		changed = true
	}
	if changed {
		if err := c.UpdateTask(task); err != nil {
			return fmt.Errorf("activate bound task: %w", err)
		}
	}
	record.Task = buildSpawnTaskBinding(task)
	return nil
}

func finishSpawnTaskBinding(r *repo.Repo, record *SpawnRecord) error {
	if r == nil || record == nil || record.Task == nil || strings.TrimSpace(record.Task.ID) == "" {
		return nil
	}
	c := coord.New(r, coord.DefaultConfig)
	task, err := c.GetTask(record.Task.ID)
	if err != nil {
		return fmt.Errorf("load bound task: %w", err)
	}
	assignee := spawnTaskAssignee(record)
	changed := false
	if assignee != "" && task.AssignedTo != assignee {
		task.AssignedTo = assignee
		changed = true
	}
	switch normalizeFinishedSpawnStatus(record.Status) {
	case "completed":
		if task.Status != "completed" {
			task.Status = "completed"
			changed = true
		}
	case "failed", "aborted", "blocked":
		if task.Status != "completed" && task.Status != "blocked" {
			task.Status = "blocked"
			changed = true
		}
	}
	if changed {
		if err := c.UpdateTask(task); err != nil {
			return fmt.Errorf("finish bound task: %w", err)
		}
	}
	record.Task = buildSpawnTaskBinding(task)
	return nil
}

func spawnCoordAgentName(record *SpawnRecord) string {
	if record == nil {
		return ""
	}
	if strings.TrimSpace(record.ChildAgentName) != "" {
		return record.ChildAgentName
	}
	shortID := strings.TrimSpace(record.ID)
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}
	name := strings.TrimSpace(record.Name)
	if name == "" {
		name = "spawn"
	}
	if shortID == "" {
		return name
	}
	return name + "-" + shortID
}

func isCoorddSpawnRuntime(runtimeName string) bool {
	switch normalizeSpawnRuntime(runtimeName) {
	case "auto", "detached", "container":
		return true
	default:
		return false
	}
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
	hostRoot, containerWorkdir, err := containerWorkspacePaths(r, "")
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
	envMap := map[string]string{
		"GRAFT_COORDD_GUARDED":           "1",
		"GRAFT_COORDD_SELECTOR":          input.Action.Selector,
		"GRAFT_COORDD_POLICY_ACTION":     decisionAction,
		"GRAFT_COORDD_REQUESTED_PROFILE": requested.Name,
		"GRAFT_COORDD_EFFECTIVE_PROFILE": effective.Name,
		"GRAFT_COORDD_PARENT_AGENT_ID":   record.ParentAgentID,
		"GRAFT_COORDD_CHILD_NAME":        record.Name,
		"GRAFT_COORDD_SPAWN_ID":          record.ID,
	}
	addSpawnLeaseEnv(envMap, record)

	env := make([]string, 0, len(envMap))
	for key, value := range envMap {
		if strings.TrimSpace(key) == "" {
			continue
		}
		env = append(env, key+"="+value)
	}
	sort.Strings(env)
	return env
}

func addSpawnLeaseEnv(env map[string]string, record *SpawnRecord) {
	if env == nil || record == nil {
		return
	}
	if strings.TrimSpace(record.ChildAgentID) != "" {
		env["GRAFT_COORD_AGENT_ID"] = record.ChildAgentID
	}
	if strings.TrimSpace(record.ChildAgentName) != "" {
		env["GRAFT_COORD_AGENT_NAME"] = record.ChildAgentName
	}
	if strings.TrimSpace(record.ParentSpawnID) != "" {
		env["GRAFT_COORDD_PARENT_SPAWN_ID"] = record.ParentSpawnID
	}
	if strings.TrimSpace(record.SnapshotID) != "" {
		env["GRAFT_COORDD_SNAPSHOT_ID"] = record.SnapshotID
	}
	if strings.TrimSpace(record.RequestedProfile.Name) != "" {
		env["GRAFT_COORDD_REQUESTED_PROFILE"] = record.RequestedProfile.Name
	}
	if strings.TrimSpace(record.EffectiveProfile.Name) != "" {
		env["GRAFT_COORDD_EFFECTIVE_PROFILE"] = record.EffectiveProfile.Name
	}
	if strings.TrimSpace(record.ParentAgentID) != "" {
		env["GRAFT_COORDD_PARENT_AGENT_ID"] = record.ParentAgentID
	}
	if strings.TrimSpace(record.Name) != "" {
		env["GRAFT_COORDD_CHILD_NAME"] = record.Name
	}
	if strings.TrimSpace(record.ID) != "" {
		env["GRAFT_COORDD_SPAWN_ID"] = record.ID
	}
	if strings.TrimSpace(record.Selector) != "" {
		env["GRAFT_COORDD_SELECTOR"] = record.Selector
	}
	if strings.TrimSpace(record.RequestedRuntime) != "" {
		env["GRAFT_COORDD_REQUESTED_RUNTIME"] = record.RequestedRuntime
	}
	if record.Task != nil && strings.TrimSpace(record.Task.ID) != "" {
		env["GRAFT_COORDD_TASK_ID"] = record.Task.ID
	}
	env["GRAFT_COORDD_GUARDED"] = "1"
	if record.BootstrapCoord {
		env["GRAFT_COORDD_BOOTSTRAP_COORD"] = "1"
	}
}

func buildDetachedBwrapArgs(r *repo.Repo, input ActionPolicyInput, requested RuntimeProfile) []string {
	cwd := coorddSpawnDir(r)
	rootDir := ""
	if r != nil {
		rootDir = r.RootDir
	}
	return buildBwrapArgs(rootDir, cwd, input.Action.Argv, requested, false)
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
	if strings.TrimSpace(record.ChildAgentID) != "" {
		args = append(args, "--env", "GRAFT_COORD_AGENT_ID="+record.ChildAgentID)
	}
	if strings.TrimSpace(record.ChildAgentName) != "" {
		args = append(args, "--env", "GRAFT_COORD_AGENT_NAME="+record.ChildAgentName)
	}
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
	args = append(args, containerArgvForAction(hostRoot, input.Action.Argv)...)
	return &ContainerInvocation{Runtime: runtimeName, Args: args}, nil
}
