package coordd

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/odvcencio/graft/pkg/coord"
	"github.com/odvcencio/graft/pkg/object"
	"github.com/odvcencio/graft/pkg/repo"
)

type Event struct {
	ID        string         `json:"id"`
	Type      string         `json:"type"`
	Timestamp time.Time      `json:"timestamp"`
	RepoRoot  string         `json:"repo_root,omitempty"`
	AgentID   string         `json:"agent_id,omitempty"`
	Data      map[string]any `json:"data,omitempty"`
}

type Snapshot struct {
	ID        string          `json:"id"`
	CreatedAt time.Time       `json:"created_at"`
	RepoRoot  string          `json:"repo_root,omitempty"`
	AgentID   string          `json:"agent_id,omitempty"`
	BaseHead  string          `json:"base_head,omitempty"`
	Summary   WorktreeSummary `json:"summary"`
	Entries   []SnapshotEntry `json:"entries,omitempty"`
}

type SnapshotEntry struct {
	Path        string `json:"path"`
	IndexStatus string `json:"index_status,omitempty"`
	WorkStatus  string `json:"work_status,omitempty"`
	BlobHash    string `json:"blob_hash,omitempty"`
	Size        int64  `json:"size,omitempty"`
	Stored      bool   `json:"stored,omitempty"`
	Missing     bool   `json:"missing,omitempty"`
}

type WorktreeSummary struct {
	Changed     int      `json:"changed"`
	Staged      int      `json:"staged"`
	Dirty       int      `json:"dirty"`
	Untracked   int      `json:"untracked"`
	Deleted     int      `json:"deleted"`
	Conflicts   int      `json:"conflicts"`
	Fingerprint string   `json:"fingerprint,omitempty"`
	Paths       []string `json:"paths,omitempty"`
}

type State struct {
	UpdatedAt      time.Time       `json:"updated_at"`
	ActiveAgentID  string          `json:"active_agent_id,omitempty"`
	FeedHead       string          `json:"feed_head,omitempty"`
	ClaimCount     int             `json:"claim_count"`
	TaskCount      int             `json:"task_count"`
	PlanCount      int             `json:"plan_count"`
	DecisionCount  int             `json:"decision_count"`
	SessionCount   int             `json:"session_count"`
	PresenceCount  int             `json:"presence_count"`
	LastSnapshotID string          `json:"last_snapshot_id,omitempty"`
	Worktree       WorktreeSummary `json:"worktree"`
}

type Options struct {
	Interval          time.Duration
	SnapshotFileLimit int
	PrintWriter       io.Writer
}

type Daemon struct {
	repo  *repo.Repo
	coord *coord.Coordinator
	opts  Options
}

func New(r *repo.Repo, c *coord.Coordinator, opts Options) *Daemon {
	if opts.Interval <= 0 {
		opts.Interval = 2 * time.Second
	}
	if opts.SnapshotFileLimit <= 0 {
		opts.SnapshotFileLimit = 256
	}
	return &Daemon{
		repo:  r,
		coord: c,
		opts:  opts,
	}
}

func BaseDir(graftDir string) string {
	return filepath.Join(graftDir, "coordd")
}

func EventsPath(graftDir string) string {
	return filepath.Join(BaseDir(graftDir), "events.jsonl")
}

func SnapshotsDir(graftDir string) string {
	return filepath.Join(BaseDir(graftDir), "snapshots")
}

func SnapshotPath(graftDir, id string) string {
	return filepath.Join(SnapshotsDir(graftDir), id+".json")
}

func StatePath(graftDir string) string {
	return filepath.Join(BaseDir(graftDir), "state.json")
}

func (d *Daemon) Run(ctx context.Context) error {
	if _, err := d.RunOnce(); err != nil {
		return err
	}

	ticker := time.NewTicker(d.opts.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if _, err := d.RunOnce(); err != nil {
				return err
			}
		}
	}
}

func (d *Daemon) RunOnce() ([]Event, error) {
	prev, err := LoadState(d.repo.GraftDir)
	if err != nil {
		return nil, err
	}

	current, statusEntries, err := d.collectState()
	if err != nil {
		return nil, err
	}

	var events []Event
	if prev == nil {
		events = append(events, newEvent("coordd_started", d.repo.RootDir, current.ActiveAgentID, map[string]any{
			"claims":    current.ClaimCount,
			"tasks":     current.TaskCount,
			"plans":     current.PlanCount,
			"decisions": current.DecisionCount,
			"worktree":  current.Worktree,
		}))
	}

	if prev == nil || prev.ActiveAgentID != current.ActiveAgentID {
		events = append(events, newEvent("active_agent_changed", d.repo.RootDir, current.ActiveAgentID, map[string]any{
			"previous": valueOrEmpty(prev, func(s *State) string { return s.ActiveAgentID }),
			"current":  current.ActiveAgentID,
		}))
	}
	if prev == nil || prev.FeedHead != current.FeedHead {
		if current.FeedHead != "" {
			events = append(events, newEvent("feed_advanced", d.repo.RootDir, current.ActiveAgentID, map[string]any{
				"previous": valueOrEmpty(prev, func(s *State) string { return s.FeedHead }),
				"current":  current.FeedHead,
			}))
		}
	}
	if prev == nil || prev.ClaimCount != current.ClaimCount {
		events = append(events, newEvent("claims_changed", d.repo.RootDir, current.ActiveAgentID, map[string]any{
			"previous": valueOrZero(prev, func(s *State) int { return s.ClaimCount }),
			"current":  current.ClaimCount,
		}))
	}
	if prev == nil || prev.TaskCount != current.TaskCount {
		events = append(events, newEvent("tasks_changed", d.repo.RootDir, current.ActiveAgentID, map[string]any{
			"previous": valueOrZero(prev, func(s *State) int { return s.TaskCount }),
			"current":  current.TaskCount,
		}))
	}
	if prev == nil || prev.PlanCount != current.PlanCount {
		events = append(events, newEvent("plans_changed", d.repo.RootDir, current.ActiveAgentID, map[string]any{
			"previous": valueOrZero(prev, func(s *State) int { return s.PlanCount }),
			"current":  current.PlanCount,
		}))
	}
	if prev == nil || prev.DecisionCount != current.DecisionCount {
		events = append(events, newEvent("decisions_changed", d.repo.RootDir, current.ActiveAgentID, map[string]any{
			"previous": valueOrZero(prev, func(s *State) int { return s.DecisionCount }),
			"current":  current.DecisionCount,
		}))
	}
	if prev == nil || prev.SessionCount != current.SessionCount {
		events = append(events, newEvent("sessions_changed", d.repo.RootDir, current.ActiveAgentID, map[string]any{
			"previous": valueOrZero(prev, func(s *State) int { return s.SessionCount }),
			"current":  current.SessionCount,
		}))
	}
	if prev == nil || prev.PresenceCount != current.PresenceCount {
		events = append(events, newEvent("presence_changed", d.repo.RootDir, current.ActiveAgentID, map[string]any{
			"previous": valueOrZero(prev, func(s *State) int { return s.PresenceCount }),
			"current":  current.PresenceCount,
		}))
	}

	if prev == nil || prev.Worktree.Fingerprint != current.Worktree.Fingerprint {
		if current.Worktree.Changed > 0 {
			snapshot, err := CaptureSnapshot(d.repo, current.ActiveAgentID, statusEntries, d.opts.SnapshotFileLimit)
			if err != nil {
				return nil, err
			}
			if snapshot != nil {
				current.LastSnapshotID = snapshot.ID
				events = append(events, newEvent("worktree_snapshot", d.repo.RootDir, current.ActiveAgentID, map[string]any{
					"snapshot_id": snapshot.ID,
					"summary":     snapshot.Summary,
				}))
			}
		} else if prev != nil && prev.Worktree.Changed > 0 {
			events = append(events, newEvent("worktree_clean", d.repo.RootDir, current.ActiveAgentID, map[string]any{
				"previous_changed": prev.Worktree.Changed,
			}))
		}
	}
	if current.LastSnapshotID == "" && prev != nil {
		current.LastSnapshotID = prev.LastSnapshotID
	}

	if err := SaveState(d.repo.GraftDir, current); err != nil {
		return nil, err
	}
	for _, event := range events {
		if err := AppendEvent(d.repo.GraftDir, event); err != nil {
			return nil, err
		}
		if d.opts.PrintWriter != nil {
			if err := writeEventJSONL(d.opts.PrintWriter, event); err != nil {
				return nil, err
			}
		}
	}
	return events, nil
}

func CaptureSnapshot(r *repo.Repo, activeAgentID string, statusEntries []repo.StatusEntry, fileLimit int) (*Snapshot, error) {
	summary := summarizeWorktree(statusEntries)
	if summary.Changed == 0 {
		return nil, nil
	}

	if fileLimit <= 0 {
		fileLimit = 256
	}

	entries := make([]SnapshotEntry, 0, len(statusEntries))
	for _, status := range statusEntries {
		if status.IndexStatus == repo.StatusClean && status.WorkStatus == repo.StatusClean {
			continue
		}
		entry := SnapshotEntry{
			Path:        status.Path,
			IndexStatus: fileStatusName(status.IndexStatus),
			WorkStatus:  fileStatusName(status.WorkStatus),
		}
		absPath := filepath.Join(r.RootDir, filepath.FromSlash(status.Path))
		if len(entries) < fileLimit {
			if data, err := os.ReadFile(absPath); err == nil {
				h, err := r.Store.WriteBlob(&object.Blob{Data: data})
				if err != nil {
					return nil, fmt.Errorf("write snapshot blob for %s: %w", status.Path, err)
				}
				entry.BlobHash = string(h)
				entry.Size = int64(len(data))
				entry.Stored = true
			} else if os.IsNotExist(err) {
				entry.Missing = true
			}
		}
		entries = append(entries, entry)
	}

	headHash := ""
	if resolved, err := r.ResolveTreeish("HEAD"); err == nil {
		headHash = string(resolved)
	}

	snapshot := &Snapshot{
		ID:        newID(),
		CreatedAt: time.Now().UTC(),
		RepoRoot:  r.RootDir,
		AgentID:   activeAgentID,
		BaseHead:  headHash,
		Summary:   summary,
		Entries:   entries,
	}
	if err := SaveSnapshot(r.GraftDir, snapshot); err != nil {
		return nil, err
	}
	return snapshot, nil
}

func SaveSnapshot(graftDir string, snapshot *Snapshot) error {
	if snapshot == nil {
		return fmt.Errorf("nil snapshot")
	}
	if snapshot.ID == "" {
		snapshot.ID = newID()
	}
	if snapshot.CreatedAt.IsZero() {
		snapshot.CreatedAt = time.Now().UTC()
	}

	if err := os.MkdirAll(SnapshotsDir(graftDir), 0o755); err != nil {
		return fmt.Errorf("create snapshots dir: %w", err)
	}
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal snapshot: %w", err)
	}
	if err := os.WriteFile(SnapshotPath(graftDir, snapshot.ID), data, 0o644); err != nil {
		return fmt.Errorf("write snapshot: %w", err)
	}
	return nil
}

func LoadSnapshot(graftDir, id string) (*Snapshot, error) {
	data, err := os.ReadFile(SnapshotPath(graftDir, id))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read snapshot: %w", err)
	}
	var snapshot Snapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return nil, fmt.Errorf("unmarshal snapshot: %w", err)
	}
	return &snapshot, nil
}

func ListSnapshots(graftDir string, limit int) ([]Snapshot, error) {
	entries, err := os.ReadDir(SnapshotsDir(graftDir))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read snapshots dir: %w", err)
	}

	var snapshots []Snapshot
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(SnapshotsDir(graftDir), entry.Name()))
		if err != nil {
			continue
		}
		var snapshot Snapshot
		if err := json.Unmarshal(data, &snapshot); err != nil {
			continue
		}
		snapshots = append(snapshots, snapshot)
	}
	sort.SliceStable(snapshots, func(i, j int) bool {
		return snapshots[i].CreatedAt.After(snapshots[j].CreatedAt)
	})
	if limit > 0 && len(snapshots) > limit {
		snapshots = snapshots[:limit]
	}
	return snapshots, nil
}

func AppendEvent(graftDir string, event Event) error {
	if event.ID == "" {
		event.ID = newID()
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}
	if err := os.MkdirAll(BaseDir(graftDir), 0o755); err != nil {
		return fmt.Errorf("create coordd dir: %w", err)
	}
	f, err := os.OpenFile(EventsPath(graftDir), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open event log: %w", err)
	}
	defer f.Close()
	return writeEventJSONL(f, event)
}

func ListEvents(graftDir string, limit int) ([]Event, error) {
	data, err := os.ReadFile(EventsPath(graftDir))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read event log: %w", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	var events []Event
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var event Event
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}
		events = append(events, event)
	}
	if limit > 0 && len(events) > limit {
		events = events[len(events)-limit:]
	}
	return events, nil
}

func LoadState(graftDir string) (*State, error) {
	data, err := os.ReadFile(StatePath(graftDir))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read coordd state: %w", err)
	}
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("unmarshal coordd state: %w", err)
	}
	return &state, nil
}

func SaveState(graftDir string, state *State) error {
	if state == nil {
		return fmt.Errorf("nil coordd state")
	}
	state.UpdatedAt = time.Now().UTC()
	if err := os.MkdirAll(BaseDir(graftDir), 0o755); err != nil {
		return fmt.Errorf("create coordd dir: %w", err)
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal coordd state: %w", err)
	}
	if err := os.WriteFile(StatePath(graftDir), data, 0o644); err != nil {
		return fmt.Errorf("write coordd state: %w", err)
	}
	return nil
}

func (d *Daemon) collectState() (*State, []repo.StatusEntry, error) {
	activeAgentID := strings.TrimSpace(readFile(filepath.Join(d.repo.GraftDir, "coord", "agent-id")))

	claims, err := d.coord.ListClaims()
	if err != nil {
		return nil, nil, fmt.Errorf("list claims: %w", err)
	}
	tasks, err := d.coord.ListTasks()
	if err != nil {
		return nil, nil, fmt.Errorf("list tasks: %w", err)
	}
	plans, err := d.coord.ListPlans()
	if err != nil {
		return nil, nil, fmt.Errorf("list plans: %w", err)
	}
	decisions, err := coord.ListDecisions(d.repo.GraftDir, 0)
	if err != nil {
		return nil, nil, fmt.Errorf("list decisions: %w", err)
	}
	sessions, err := coord.ListSessions(d.repo.GraftDir)
	if err != nil {
		return nil, nil, fmt.Errorf("list sessions: %w", err)
	}
	presence, err := coord.ListPresenceEntries(d.repo.GraftDir, coord.PresenceTTL)
	if err != nil {
		return nil, nil, fmt.Errorf("list presence: %w", err)
	}
	statusEntries, err := d.repo.Status()
	if err != nil {
		return nil, nil, fmt.Errorf("repo status: %w", err)
	}
	worktree := summarizeWorktree(statusEntries)

	feedHead := ""
	if h, err := d.repo.ResolveRef("refs/coord/feed/head"); err == nil {
		feedHead = string(h)
	}

	return &State{
		ActiveAgentID: activeAgentID,
		FeedHead:      feedHead,
		ClaimCount:    len(claims),
		TaskCount:     len(tasks),
		PlanCount:     len(plans),
		DecisionCount: len(decisions),
		SessionCount:  len(sessions),
		PresenceCount: len(presence),
		Worktree:      worktree,
	}, statusEntries, nil
}

func summarizeWorktree(entries []repo.StatusEntry) WorktreeSummary {
	var summary WorktreeSummary
	var keys []string
	var paths []string

	for _, entry := range entries {
		if entry.IndexStatus == repo.StatusClean && entry.WorkStatus == repo.StatusClean {
			continue
		}
		summary.Changed++
		if entry.IndexStatus != repo.StatusClean {
			summary.Staged++
		}
		if entry.WorkStatus == repo.StatusDirty {
			summary.Dirty++
		}
		if entry.IndexStatus == repo.StatusUntracked || entry.WorkStatus == repo.StatusUntracked {
			summary.Untracked++
		}
		if entry.IndexStatus == repo.StatusDeleted || entry.WorkStatus == repo.StatusDeleted {
			summary.Deleted++
		}
		if entry.IndexStatus == repo.StatusConflict || entry.WorkStatus == repo.StatusConflict {
			summary.Conflicts++
		}

		keys = append(keys, fmt.Sprintf("%s|%s|%s", entry.Path, fileStatusName(entry.IndexStatus), fileStatusName(entry.WorkStatus)))
		paths = append(paths, entry.Path)
	}

	sort.Strings(keys)
	sort.Strings(paths)
	if len(paths) > 20 {
		paths = paths[:20]
	}
	summary.Paths = paths
	if len(keys) > 0 {
		h := sha256.Sum256([]byte(strings.Join(keys, "\n")))
		summary.Fingerprint = hex.EncodeToString(h[:])
	}
	return summary
}

func writeEventJSONL(w io.Writer, event Event) error {
	enc := json.NewEncoder(w)
	return enc.Encode(event)
}

func newEvent(eventType, repoRoot, agentID string, data map[string]any) Event {
	return Event{
		ID:        newID(),
		Type:      eventType,
		Timestamp: time.Now().UTC(),
		RepoRoot:  repoRoot,
		AgentID:   agentID,
		Data:      data,
	}
}

func newID() string {
	var suffix [6]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("%d-%s", time.Now().UnixNano(), hex.EncodeToString(suffix[:]))
}

func valueOrEmpty(state *State, fn func(*State) string) string {
	if state == nil {
		return ""
	}
	return fn(state)
}

func valueOrZero(state *State, fn func(*State) int) int {
	if state == nil {
		return 0
	}
	return fn(state)
}

func readFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

func fileStatusName(status repo.FileStatus) string {
	switch status {
	case repo.StatusClean:
		return "clean"
	case repo.StatusNew:
		return "new"
	case repo.StatusModified:
		return "modified"
	case repo.StatusRenamed:
		return "renamed"
	case repo.StatusConflict:
		return "conflict"
	case repo.StatusDeleted:
		return "deleted"
	case repo.StatusUntracked:
		return "untracked"
	case repo.StatusDirty:
		return "dirty"
	default:
		return "unknown"
	}
}
