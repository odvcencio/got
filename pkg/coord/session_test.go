package coord

import (
	"os"
	"testing"
	"time"
)

func TestSaveLoadSession(t *testing.T) {
	dir := t.TempDir()

	s := &Session{
		AgentID:    "abc123",
		AgentName:  "test-agent",
		Workspace:  "graft",
		Host:       "test-host",
		StartedAt:  time.Now().UTC().Truncate(time.Millisecond),
		LastActive: time.Now().UTC().Truncate(time.Millisecond),
		PID:        1234,
		Mode:       "editing",
	}

	if err := SaveSession(dir, s); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}

	loaded, err := LoadSession(dir, "test-agent")
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if loaded == nil {
		t.Fatal("expected non-nil session")
	}
	if loaded.AgentID != "abc123" {
		t.Errorf("AgentID = %q, want abc123", loaded.AgentID)
	}
	if loaded.AgentName != "test-agent" {
		t.Errorf("AgentName = %q, want test-agent", loaded.AgentName)
	}
	if loaded.Mode != "editing" {
		t.Errorf("Mode = %q, want editing", loaded.Mode)
	}
	if loaded.PID != 1234 {
		t.Errorf("PID = %d, want 1234", loaded.PID)
	}
}

func TestLoadSession_NotFound(t *testing.T) {
	dir := t.TempDir()

	loaded, err := LoadSession(dir, "nonexistent")
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if loaded != nil {
		t.Fatal("expected nil session for nonexistent agent")
	}
}

func TestListSessions(t *testing.T) {
	dir := t.TempDir()

	s1 := &Session{AgentID: "a1", AgentName: "agent-one", Mode: "editing", StartedAt: time.Now().UTC(), LastActive: time.Now().UTC()}
	s2 := &Session{AgentID: "a2", AgentName: "agent-two", Mode: "watching", StartedAt: time.Now().UTC(), LastActive: time.Now().UTC()}

	if err := SaveSession(dir, s1); err != nil {
		t.Fatalf("SaveSession s1: %v", err)
	}
	if err := SaveSession(dir, s2); err != nil {
		t.Fatalf("SaveSession s2: %v", err)
	}

	sessions, err := ListSessions(dir)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(sessions))
	}

	// Verify both are present (order may vary)
	names := map[string]bool{}
	for _, s := range sessions {
		names[s.AgentName] = true
	}
	if !names["agent-one"] || !names["agent-two"] {
		t.Errorf("expected agent-one and agent-two, got %v", names)
	}
}

func TestRemoveSession(t *testing.T) {
	dir := t.TempDir()

	s := &Session{AgentID: "rm1", AgentName: "removable", Mode: "editing", StartedAt: time.Now().UTC(), LastActive: time.Now().UTC()}
	if err := SaveSession(dir, s); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}

	if err := RemoveSession(dir, "removable"); err != nil {
		t.Fatalf("RemoveSession: %v", err)
	}

	loaded, err := LoadSession(dir, "removable")
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if loaded != nil {
		t.Fatal("expected nil session after removal")
	}
}

func TestRemoveSession_Nonexistent(t *testing.T) {
	dir := t.TempDir()

	// Should not error on nonexistent session
	if err := RemoveSession(dir, "ghost"); err != nil {
		t.Fatalf("RemoveSession nonexistent: %v", err)
	}
}

func TestIsSessionStale(t *testing.T) {
	fresh := &Session{
		LastActive: time.Now().UTC(),
	}
	if IsSessionStale(fresh, SessionStaleThreshold) {
		t.Error("fresh session should not be stale")
	}

	stale := &Session{
		LastActive: time.Now().UTC().Add(-3 * time.Minute),
	}
	if !IsSessionStale(stale, SessionStaleThreshold) {
		t.Error("old session should be stale")
	}
}

func TestTouchSession(t *testing.T) {
	dir := t.TempDir()

	s := &Session{
		AgentID:    "touch1",
		AgentName:  "touchable",
		Mode:       "editing",
		StartedAt:  time.Now().UTC().Add(-10 * time.Second),
		LastActive: time.Now().UTC().Add(-10 * time.Second),
		PID:        9999,
	}
	if err := SaveSession(dir, s); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}

	before := s.LastActive
	if err := TouchSession(dir, s); err != nil {
		t.Fatalf("TouchSession: %v", err)
	}

	if !s.LastActive.After(before) {
		t.Error("LastActive should have advanced after touch")
	}
	if s.PID != os.Getpid() {
		t.Errorf("PID = %d, want %d", s.PID, os.Getpid())
	}

	// Verify it was persisted
	loaded, err := LoadSession(dir, "touchable")
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if loaded.PID != os.Getpid() {
		t.Errorf("persisted PID = %d, want %d", loaded.PID, os.Getpid())
	}
}

func TestSessionResume(t *testing.T) {
	// Simulate: create session, "restart" (new process), resume with same agent ID
	dir := t.TempDir()

	original := &Session{
		AgentID:    "resume-id",
		AgentName:  "resumable",
		Workspace:  "graft",
		Host:       "host1",
		StartedAt:  time.Now().UTC(),
		LastActive: time.Now().UTC(),
		PID:        1000,
		Mode:       "editing",
	}
	if err := SaveSession(dir, original); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}

	// "New process" loads the session
	loaded, err := LoadSession(dir, "resumable")
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if loaded == nil {
		t.Fatal("expected existing session")
	}

	// Session is not stale
	if IsSessionStale(loaded, SessionStaleThreshold) {
		t.Fatal("fresh session should not be stale")
	}

	// Verify same agent ID is reused
	if loaded.AgentID != "resume-id" {
		t.Errorf("AgentID = %q, want resume-id", loaded.AgentID)
	}

	// Update host for the "new process", then touch (which also sets PID to current process)
	loaded.Host = "host2"
	if err := TouchSession(dir, loaded); err != nil {
		t.Fatalf("TouchSession: %v", err)
	}

	// Reload and verify updates
	reloaded, err := LoadSession(dir, "resumable")
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	// TouchSession sets PID to os.Getpid()
	if reloaded.PID != os.Getpid() {
		t.Errorf("PID = %d, want %d (current process)", reloaded.PID, os.Getpid())
	}
	if reloaded.Host != "host2" {
		t.Errorf("Host = %q, want host2", reloaded.Host)
	}
	if reloaded.AgentID != "resume-id" {
		t.Errorf("AgentID changed: got %q, want resume-id", reloaded.AgentID)
	}
}

func TestSessionStaleReplacement(t *testing.T) {
	dir := t.TempDir()

	// Create a stale session
	stale := &Session{
		AgentID:    "old-id",
		AgentName:  "stale-agent",
		Workspace:  "graft",
		Host:       "old-host",
		StartedAt:  time.Now().UTC().Add(-10 * time.Minute),
		LastActive: time.Now().UTC().Add(-5 * time.Minute),
		PID:        100,
		Mode:       "editing",
	}
	if err := SaveSession(dir, stale); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}

	// Load and check it's stale
	loaded, err := LoadSession(dir, "stale-agent")
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if !IsSessionStale(loaded, SessionStaleThreshold) {
		t.Fatal("session should be stale")
	}

	// Replace with new session (same name, new ID)
	replacement := &Session{
		AgentID:    "new-id",
		AgentName:  "stale-agent",
		Workspace:  "graft",
		Host:       "new-host",
		StartedAt:  time.Now().UTC(),
		LastActive: time.Now().UTC(),
		PID:        200,
		Mode:       "editing",
	}
	if err := SaveSession(dir, replacement); err != nil {
		t.Fatalf("SaveSession replacement: %v", err)
	}

	reloaded, err := LoadSession(dir, "stale-agent")
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if reloaded.AgentID != "new-id" {
		t.Errorf("AgentID = %q, want new-id (replacement)", reloaded.AgentID)
	}
}
