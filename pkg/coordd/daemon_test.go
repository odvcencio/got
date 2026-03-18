package coordd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/odvcencio/graft/pkg/coord"
	"github.com/odvcencio/graft/pkg/repo"
)

func TestCaptureSnapshot_StoresDirtyWorktree(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("hello coordd\n"), 0o644); err != nil {
		t.Fatalf("write note.txt: %v", err)
	}

	statusEntries, err := r.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}

	snapshot, err := CaptureSnapshot(r, "agent-a", statusEntries, 16)
	if err != nil {
		t.Fatalf("CaptureSnapshot: %v", err)
	}
	if snapshot == nil {
		t.Fatal("expected snapshot for changed worktree")
	}
	if snapshot.Summary.Changed == 0 {
		t.Fatal("expected changed summary")
	}
	if len(snapshot.Entries) != 1 {
		t.Fatalf("len(snapshot.Entries) = %d, want 1", len(snapshot.Entries))
	}
	if !snapshot.Entries[0].Stored {
		t.Fatal("expected file contents stored in snapshot")
	}

	loaded, err := LoadSnapshot(r.GraftDir, snapshot.ID)
	if err != nil {
		t.Fatalf("LoadSnapshot: %v", err)
	}
	if loaded == nil {
		t.Fatal("expected persisted snapshot")
	}
	if loaded.Entries[0].Path != "note.txt" {
		t.Fatalf("loaded path = %q, want note.txt", loaded.Entries[0].Path)
	}
}

func TestDaemonRunOnce_WritesStateAndEvents(t *testing.T) {
	dir := t.TempDir()
	r, err := repo.Init(dir)
	if err != nil {
		t.Fatalf("repo.Init: %v", err)
	}

	c := coord.New(r, coord.DefaultConfig)
	agentID, err := c.RegisterAgent(coord.AgentInfo{Name: "agent-a", Workspace: "graft", Host: "test"})
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(r.GraftDir, "coord"), 0o755); err != nil {
		t.Fatalf("mkdir coord: %v", err)
	}
	if err := os.WriteFile(filepath.Join(r.GraftDir, "coord", "agent-id"), []byte(agentID), 0o644); err != nil {
		t.Fatalf("write agent-id: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "dirty.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatalf("write dirty.txt: %v", err)
	}

	d := New(r, c, Options{})
	events, err := d.RunOnce()
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("expected coordd events")
	}

	state, err := LoadState(r.GraftDir)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if state == nil {
		t.Fatal("expected saved coordd state")
	}
	if state.ActiveAgentID != agentID {
		t.Fatalf("ActiveAgentID = %q, want %q", state.ActiveAgentID, agentID)
	}
	if state.Worktree.Changed == 0 {
		t.Fatal("expected worktree change summary")
	}
	if state.LastSnapshotID == "" {
		t.Fatal("expected snapshot ID recorded in state")
	}

	logged, err := ListEvents(r.GraftDir, 0)
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(logged) != len(events) {
		t.Fatalf("len(logged) = %d, want %d", len(logged), len(events))
	}
}
