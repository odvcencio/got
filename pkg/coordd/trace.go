package coordd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type ExecTrace struct {
	ID            string            `json:"id"`
	CreatedAt     time.Time         `json:"created_at"`
	RepoRoot      string            `json:"repo_root,omitempty"`
	AgentID       string            `json:"agent_id,omitempty"`
	SpawnID       string            `json:"spawn_id,omitempty"`
	TaskID        string            `json:"task_id,omitempty"`
	Input         ActionPolicyInput `json:"input"`
	Result        *ExecResult       `json:"result,omitempty"`
	RequestedMode string            `json:"requested_mode,omitempty"`
}

type SpawnTrace struct {
	Record *SpawnRecord `json:"record,omitempty"`
	Lease  *SpawnLease  `json:"lease,omitempty"`
	Execs  []ExecTrace  `json:"execs,omitempty"`
	Events []Event      `json:"events,omitempty"`
}

type SpawnTraceViewOptions struct {
	MatchedOnly        bool     `json:"matched_only"`
	CollapseHeartbeats bool     `json:"collapse_heartbeats"`
	Phases             []string `json:"phases,omitempty"`
	NoDefaultFallbacks bool     `json:"no_default_fallbacks,omitempty"`
}

type TraceRuleView struct {
	Rule          string              `json:"rule"`
	Matched       bool                `json:"matched,omitempty"`
	Priority      int                 `json:"priority,omitempty"`
	Action        string              `json:"action,omitempty"`
	Params        map[string]any      `json:"params,omitempty"`
	Fallback      bool                `json:"fallback,omitempty"`
	FailedAtInstr uint32              `json:"failed_at_instr,omitempty"`
	Origin        *PolicySourceOrigin `json:"origin,omitempty"`
}

type TraceDecisionView struct {
	Action     string                 `json:"action,omitempty"`
	Code       string                 `json:"code,omitempty"`
	Reason     string                 `json:"reason,omitempty"`
	Rule       string                 `json:"rule,omitempty"`
	RuleOrigin *PolicySourceOrigin    `json:"rule_origin,omitempty"`
	Profile    string                 `json:"profile,omitempty"`
	Bundle     PolicyBundleInfo       `json:"bundle"`
	Rules      []TraceRuleView        `json:"rules,omitempty"`
	Governance []PolicyGovernanceStep `json:"governance,omitempty"`
}

type ExecTraceView struct {
	ID               string             `json:"id"`
	CreatedAt        time.Time          `json:"created_at"`
	AgentID          string             `json:"agent_id,omitempty"`
	Selector         string             `json:"selector,omitempty"`
	Program          string             `json:"program,omitempty"`
	Decision         *TraceDecisionView `json:"decision,omitempty"`
	ExitCode         int                `json:"exit_code"`
	Backend          string             `json:"backend,omitempty"`
	RequestedProfile RuntimeProfile     `json:"requested_profile,omitempty"`
	EffectiveProfile RuntimeProfile     `json:"effective_profile,omitempty"`
}

type TraceEventView struct {
	Phase    string            `json:"phase,omitempty"`
	Type     string            `json:"type"`
	FirstAt  time.Time         `json:"first_at"`
	LastAt   time.Time         `json:"last_at,omitempty"`
	Count    int               `json:"count,omitempty"`
	AgentID  string            `json:"agent_id,omitempty"`
	Status   string            `json:"status,omitempty"`
	Decision string            `json:"decision,omitempty"`
	Rule     string            `json:"rule,omitempty"`
	Profile  string            `json:"profile,omitempty"`
	Selector string            `json:"selector,omitempty"`
	Backend  string            `json:"backend,omitempty"`
	ExitCode *int              `json:"exit_code,omitempty"`
	Task     *SpawnTaskBinding `json:"task,omitempty"`
}

type TracePhaseView struct {
	Name   string           `json:"name"`
	Events []TraceEventView `json:"events,omitempty"`
}

type SpawnTraceView struct {
	Record              *SpawnRecord       `json:"record,omitempty"`
	Lease               *SpawnLease        `json:"lease,omitempty"`
	SpawnAction         *TraceDecisionView `json:"spawn_action,omitempty"`
	SpawnPolicy         *TraceDecisionView `json:"spawn_policy,omitempty"`
	Execs               []ExecTraceView    `json:"execs,omitempty"`
	Phases              []TracePhaseView   `json:"phases,omitempty"`
	RawEventCount       int                `json:"raw_event_count"`
	RenderedEventCount  int                `json:"rendered_event_count"`
	CollapsedHeartbeats int                `json:"collapsed_heartbeats,omitempty"`
}

func ExecTracesDir(graftDir string) string {
	return filepath.Join(BaseDir(graftDir), "execs")
}

func ExecTracePath(graftDir, id string) string {
	return filepath.Join(ExecTracesDir(graftDir), id+".json")
}

func SaveExecTrace(graftDir string, trace *ExecTrace) error {
	if trace == nil {
		return fmt.Errorf("nil exec trace")
	}
	if strings.TrimSpace(trace.ID) == "" {
		trace.ID = newID()
	}
	if trace.CreatedAt.IsZero() {
		trace.CreatedAt = time.Now().UTC()
	}
	if err := os.MkdirAll(ExecTracesDir(graftDir), 0o755); err != nil {
		return fmt.Errorf("create exec traces dir: %w", err)
	}
	data, err := json.MarshalIndent(trace, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal exec trace: %w", err)
	}
	if err := os.WriteFile(ExecTracePath(graftDir, trace.ID), data, 0o644); err != nil {
		return fmt.Errorf("write exec trace: %w", err)
	}
	return nil
}

func ListExecTraces(graftDir string, limit int) ([]ExecTrace, error) {
	entries, err := os.ReadDir(ExecTracesDir(graftDir))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read exec traces dir: %w", err)
	}

	var traces []ExecTrace
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(ExecTracesDir(graftDir), entry.Name()))
		if err != nil {
			continue
		}
		var trace ExecTrace
		if err := json.Unmarshal(data, &trace); err != nil {
			continue
		}
		traces = append(traces, trace)
	}

	sort.SliceStable(traces, func(i, j int) bool {
		if traces[i].CreatedAt.Equal(traces[j].CreatedAt) {
			return traces[i].ID > traces[j].ID
		}
		return traces[i].CreatedAt.After(traces[j].CreatedAt)
	})
	if limit > 0 && len(traces) > limit {
		traces = traces[:limit]
	}
	return traces, nil
}

func ListExecTracesBySpawn(graftDir, spawnID string, limit int) ([]ExecTrace, error) {
	all, err := ListExecTraces(graftDir, 0)
	if err != nil {
		return nil, err
	}
	var filtered []ExecTrace
	for _, trace := range all {
		if strings.TrimSpace(trace.SpawnID) == strings.TrimSpace(spawnID) {
			filtered = append(filtered, trace)
		}
	}
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return filtered, nil
}

func LoadSpawnTrace(graftDir, id string, execLimit, eventLimit int) (*SpawnTrace, error) {
	view, err := LoadSpawnView(graftDir, id)
	if err != nil {
		return nil, err
	}
	if view == nil || view.Record == nil {
		return nil, nil
	}
	trace := &SpawnTrace{
		Record: view.Record,
		Lease:  view.Lease,
	}
	execs, err := ListExecTracesBySpawn(graftDir, id, execLimit)
	if err != nil {
		return nil, err
	}
	trace.Execs = execs

	events, err := ListEvents(graftDir, 0)
	if err != nil {
		return nil, err
	}
	childID := ""
	if view.Record != nil {
		childID = view.Record.ChildAgentID
	}
	for _, event := range events {
		if eventMatchesSpawnTrace(event, id, childID) {
			trace.Events = append(trace.Events, event)
		}
	}
	if eventLimit > 0 && len(trace.Events) > eventLimit {
		trace.Events = trace.Events[len(trace.Events)-eventLimit:]
	}
	return trace, nil
}

func BuildSpawnTraceView(trace *SpawnTrace, opts SpawnTraceViewOptions) *SpawnTraceView {
	if trace == nil {
		return nil
	}
	view := &SpawnTraceView{
		Record:        trace.Record,
		Lease:         trace.Lease,
		RawEventCount: len(trace.Events),
	}
	phaseFilter := normalizeTracePhaseFilter(opts.Phases)
	if trace.Record != nil && phaseSelected(phaseFilter, "authorization") {
		view.SpawnAction = buildActionDecisionView(trace.Record.ActionDecision, opts)
		view.SpawnPolicy = buildSpawnDecisionView(trace.Record.SpawnDecision, opts)
	}
	if phaseSelected(phaseFilter, "execution") {
		for _, execTrace := range trace.Execs {
			view.Execs = append(view.Execs, ExecTraceView{
				ID:               execTrace.ID,
				CreatedAt:        execTrace.CreatedAt,
				AgentID:          execTrace.AgentID,
				Selector:         execTrace.Input.Action.Selector,
				Program:          execTrace.Input.Action.Program,
				Decision:         buildActionDecisionView(resultDecision(execTrace.Result), opts),
				ExitCode:         resultExitCode(execTrace.Result),
				Backend:          resultBackend(execTrace.Result),
				RequestedProfile: resultRequestedProfile(execTrace.Result),
				EffectiveProfile: resultEffectiveProfile(execTrace.Result),
			})
		}
	}
	eventViews, collapsed := buildTraceEventViews(trace.Events, opts.CollapseHeartbeats, phaseFilter)
	view.CollapsedHeartbeats = collapsed
	view.RenderedEventCount = len(eventViews)
	view.Phases = groupTraceEventsByPhase(eventViews)
	return view
}

func eventMatchesSpawnTrace(event Event, spawnID, childAgentID string) bool {
	if strings.TrimSpace(spawnID) == "" {
		return false
	}
	if value, ok := event.Data["id"].(string); ok && strings.TrimSpace(value) == strings.TrimSpace(spawnID) {
		return true
	}
	if value, ok := event.Data["spawn_id"].(string); ok && strings.TrimSpace(value) == strings.TrimSpace(spawnID) {
		return true
	}
	return strings.TrimSpace(childAgentID) != "" && strings.TrimSpace(event.AgentID) == strings.TrimSpace(childAgentID)
}

func buildActionDecisionView(decision *ActionPolicyDecision, opts SpawnTraceViewOptions) *TraceDecisionView {
	if decision == nil {
		return nil
	}
	view := &TraceDecisionView{
		Action:     decision.Action,
		Code:       decision.Code,
		Reason:     decision.Reason,
		Rule:       decision.Rule,
		RuleOrigin: decision.RuleOrigin,
		Profile:    decision.Profile,
		Bundle:     decision.Bundle,
		Governance: append([]PolicyGovernanceStep(nil), decision.Governance...),
	}
	for _, rule := range decision.Trace {
		if opts.MatchedOnly && !rule.Matched {
			continue
		}
		if opts.NoDefaultFallbacks && (rule.Fallback || strings.HasPrefix(rule.Rule, "Default")) {
			continue
		}
		view.Rules = append(view.Rules, TraceRuleView{
			Rule:          rule.Rule,
			Matched:       rule.Matched,
			Priority:      rule.Priority,
			Action:        rule.Action,
			Params:        rule.Params,
			Fallback:      rule.Fallback,
			FailedAtInstr: rule.FailedAtInstr,
			Origin:        rule.Origin,
		})
	}
	return view
}

func buildSpawnDecisionView(decision *SpawnPolicyDecision, opts SpawnTraceViewOptions) *TraceDecisionView {
	if decision == nil {
		return nil
	}
	view := &TraceDecisionView{
		Action:     decision.Action,
		Code:       decision.Code,
		Reason:     decision.Reason,
		Rule:       decision.Rule,
		RuleOrigin: decision.RuleOrigin,
		Profile:    decision.Profile,
		Bundle:     decision.Bundle,
		Governance: append([]PolicyGovernanceStep(nil), decision.Governance...),
	}
	for _, rule := range decision.Trace {
		if opts.MatchedOnly && !rule.Matched {
			continue
		}
		if opts.NoDefaultFallbacks && (rule.Fallback || strings.HasPrefix(rule.Rule, "Default")) {
			continue
		}
		view.Rules = append(view.Rules, TraceRuleView{
			Rule:          rule.Rule,
			Matched:       rule.Matched,
			Priority:      rule.Priority,
			Action:        rule.Action,
			Params:        rule.Params,
			Fallback:      rule.Fallback,
			FailedAtInstr: rule.FailedAtInstr,
			Origin:        rule.Origin,
		})
	}
	return view
}

func buildTraceEventViews(events []Event, collapseHeartbeats bool, phaseFilter map[string]bool) ([]TraceEventView, int) {
	var (
		out       []TraceEventView
		collapsed int
	)
	for _, event := range events {
		current := traceEventFromEvent(event)
		if !phaseSelected(phaseFilter, current.Phase) {
			continue
		}
		if collapseHeartbeats && current.Type == "spawn_heartbeat" && len(out) > 0 {
			last := &out[len(out)-1]
			if last.Type == "spawn_heartbeat" && last.Phase == current.Phase {
				last.Count++
				last.LastAt = current.FirstAt
				if current.Task != nil {
					last.Task = current.Task
				}
				if current.Status != "" {
					last.Status = current.Status
				}
				collapsed++
				continue
			}
		}
		out = append(out, current)
	}
	return out, collapsed
}

func normalizeTracePhaseFilter(phases []string) map[string]bool {
	filter := map[string]bool{}
	for _, phase := range phases {
		for _, part := range strings.Split(phase, ",") {
			part = strings.TrimSpace(strings.ToLower(part))
			if part == "" {
				continue
			}
			filter[part] = true
		}
	}
	if len(filter) == 0 {
		return nil
	}
	return filter
}

func phaseSelected(filter map[string]bool, phase string) bool {
	if len(filter) == 0 {
		return true
	}
	return filter[strings.TrimSpace(strings.ToLower(phase))]
}

func groupTraceEventsByPhase(events []TraceEventView) []TracePhaseView {
	phases := make([]TracePhaseView, 0, 4)
	index := map[string]int{}
	for _, event := range events {
		name := event.Phase
		if name == "" {
			name = "other"
		}
		phaseIdx, ok := index[name]
		if !ok {
			index[name] = len(phases)
			phases = append(phases, TracePhaseView{Name: name})
			phaseIdx = len(phases) - 1
		}
		phases[phaseIdx].Events = append(phases[phaseIdx].Events, event)
	}
	return phases
}

func traceEventFromEvent(event Event) TraceEventView {
	view := TraceEventView{
		Phase:    phaseForEventType(event.Type),
		Type:     event.Type,
		FirstAt:  event.Timestamp,
		Count:    1,
		AgentID:  event.AgentID,
		Status:   stringData(event.Data, "status"),
		Decision: stringData(event.Data, "decision"),
		Rule:     stringData(event.Data, "rule"),
		Profile:  stringData(event.Data, "profile"),
		Selector: stringData(event.Data, "selector"),
		Backend:  stringData(event.Data, "backend"),
		Task:     taskBindingFromAny(event.Data["task"]),
	}
	if exitCode, ok := intData(event.Data, "exit_code"); ok {
		view.ExitCode = &exitCode
	}
	if view.Status == "" && event.Type == "spawn_authorized" {
		view.Status = "authorized"
	}
	return view
}

func phaseForEventType(eventType string) string {
	switch eventType {
	case "spawn_preflight_allowed", "spawn_preflight_advisory", "spawn_preflight_blocked", "spawn_authorized":
		return "authorization"
	case "spawn_consumed", "spawn_attached", "spawn_heartbeat", "spawn_started":
		return "activation"
	case "action_preflight_allowed", "action_preflight_advisory", "action_preflight_blocked", "action_exec_started", "action_exec_finished":
		return "execution"
	case "spawn_finished":
		return "completion"
	default:
		return "other"
	}
}

func stringData(data map[string]any, key string) string {
	if data == nil {
		return ""
	}
	value, ok := data[key]
	if !ok {
		return ""
	}
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", value))
	}
}

func intData(data map[string]any, key string) (int, bool) {
	if data == nil {
		return 0, false
	}
	value, ok := data[key]
	if !ok {
		return 0, false
	}
	switch v := value.(type) {
	case int:
		return v, true
	case int32:
		return int(v), true
	case int64:
		return int(v), true
	case float64:
		return int(v), true
	default:
		return 0, false
	}
}

func taskBindingFromAny(value any) *SpawnTaskBinding {
	if value == nil {
		return nil
	}
	switch v := value.(type) {
	case *SpawnTaskBinding:
		if v == nil {
			return nil
		}
		copy := *v
		return &copy
	case SpawnTaskBinding:
		copy := v
		return &copy
	case map[string]any:
		task := &SpawnTaskBinding{
			ID:         stringData(v, "id"),
			Title:      stringData(v, "title"),
			Status:     stringData(v, "status"),
			AssignedTo: stringData(v, "assigned_to"),
		}
		if task.ID == "" && task.Title == "" && task.Status == "" && task.AssignedTo == "" {
			return nil
		}
		return task
	default:
		return nil
	}
}

func resultDecision(result *ExecResult) *ActionPolicyDecision {
	if result == nil {
		return nil
	}
	return result.Decision
}

func resultExitCode(result *ExecResult) int {
	if result == nil {
		return 0
	}
	return result.ExitCode
}

func resultBackend(result *ExecResult) string {
	if result == nil {
		return ""
	}
	return result.Backend
}

func resultRequestedProfile(result *ExecResult) RuntimeProfile {
	if result == nil {
		return RuntimeProfile{}
	}
	return result.RequestedProfile
}

func resultEffectiveProfile(result *ExecResult) RuntimeProfile {
	if result == nil {
		return RuntimeProfile{}
	}
	return result.EffectiveProfile
}
