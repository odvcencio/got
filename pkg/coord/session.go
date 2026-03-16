package coord

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Session represents a persistent coordination session for an agent.
// Session files live in .graft/coord/sessions/{agent-name}.json and survive
// process restarts, allowing any terminal to resume a session.
type Session struct {
	AgentID    string    `json:"agent_id"`
	AgentName  string    `json:"agent_name"`
	Workspace  string    `json:"workspace"`
	Host       string    `json:"host"`
	StartedAt  time.Time `json:"started_at"`
	LastActive time.Time `json:"last_active"`
	PID        int       `json:"pid,omitempty"`
	Scope      string    `json:"scope,omitempty"`
	Mode       string    `json:"mode"`
}

// SessionStaleThreshold is how long since LastActive before a session is
// considered stale and eligible for replacement.
const SessionStaleThreshold = 2 * time.Minute

// sessionsDir returns the path to the sessions directory for a graft repo.
func sessionsDir(graftDir string) string {
	return filepath.Join(graftDir, "coord", "sessions")
}

// sessionFilePath returns the full path to a session file for the given agent name.
func sessionFilePath(graftDir, agentName string) string {
	return filepath.Join(sessionsDir(graftDir), agentName+".json")
}

// SaveSession writes the session to disk at .graft/coord/sessions/{agent-name}.json.
func SaveSession(graftDir string, s *Session) error {
	dir := sessionsDir(graftDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create sessions dir: %w", err)
	}

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal session: %w", err)
	}

	path := sessionFilePath(graftDir, s.AgentName)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write session file: %w", err)
	}
	return nil
}

// LoadSession reads a session file for the given agent name.
// Returns nil, nil if no session file exists.
func LoadSession(graftDir, agentName string) (*Session, error) {
	path := sessionFilePath(graftDir, agentName)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read session file: %w", err)
	}

	var s Session
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("unmarshal session: %w", err)
	}
	return &s, nil
}

// ListSessions returns all session files from the sessions directory.
func ListSessions(graftDir string) ([]Session, error) {
	dir := sessionsDir(graftDir)
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read sessions dir: %w", err)
	}

	var sessions []Session
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var s Session
		if err := json.Unmarshal(data, &s); err != nil {
			continue
		}
		sessions = append(sessions, s)
	}
	return sessions, nil
}

// RemoveSession deletes the session file for the given agent name.
func RemoveSession(graftDir, agentName string) error {
	path := sessionFilePath(graftDir, agentName)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove session file: %w", err)
	}
	return nil
}

// IsSessionStale returns true if the session's LastActive is older than the threshold.
func IsSessionStale(s *Session, threshold time.Duration) bool {
	return time.Since(s.LastActive) > threshold
}

// TouchSession updates the LastActive timestamp and PID, then saves.
func TouchSession(graftDir string, s *Session) error {
	s.LastActive = time.Now().UTC()
	s.PID = os.Getpid()
	return SaveSession(graftDir, s)
}
