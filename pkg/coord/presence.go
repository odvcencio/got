package coord

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// PresenceEntry tracks an agent reading a file or entity.
// Presence is ephemeral and auto-expires after PresenceTTL.
type PresenceEntry struct {
	AgentID   string    `json:"agent_id"`
	AgentName string    `json:"agent_name"`
	File      string    `json:"file"`
	Entity    string    `json:"entity,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// PresenceTTL is how long a presence entry is valid before auto-expiring.
const PresenceTTL = 60 * time.Second

// presenceDir returns the path to the presence directory.
func presenceDir(graftDir string) string {
	return filepath.Join(graftDir, "coord", "presence")
}

// presenceFileHash returns a filesystem-safe hash for a file path.
func presenceFileHash(filePath string) string {
	h := sha256.Sum256([]byte(filePath))
	return fmt.Sprintf("%x", h[:8])
}

// presenceEntryPath returns the path to a presence entry file.
func presenceEntryPath(graftDir, fileHash, agentID string) string {
	return filepath.Join(presenceDir(graftDir), fileHash+"-"+agentID+".json")
}

// RegisterPresence records that the coordinator's agent is reading a file.
func (c *Coordinator) RegisterPresence(file string, entityKey string) error {
	if c.AgentID == "" {
		return fmt.Errorf("no active agent; call RegisterAgent first")
	}

	agentName := c.AgentID
	if agent, err := c.GetAgent(c.AgentID); err == nil {
		agentName = agent.Name
	}

	entry := PresenceEntry{
		AgentID:   c.AgentID,
		AgentName: agentName,
		File:      file,
		Entity:    entityKey,
		Timestamp: time.Now().UTC(),
	}

	dir := presenceDir(c.Repo.GraftDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create presence dir: %w", err)
	}

	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal presence: %w", err)
	}

	fileHash := presenceFileHash(file)
	path := presenceEntryPath(c.Repo.GraftDir, fileHash, c.AgentID)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write presence: %w", err)
	}

	return nil
}

// ListPresence returns all non-expired presence entries.
func (c *Coordinator) ListPresence() ([]PresenceEntry, error) {
	return ListPresenceEntries(c.Repo.GraftDir, PresenceTTL)
}

// ListPresenceEntries reads all presence entries from disk and filters out expired ones.
func ListPresenceEntries(graftDir string, ttl time.Duration) ([]PresenceEntry, error) {
	dir := presenceDir(graftDir)
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read presence dir: %w", err)
	}

	cutoff := time.Now().UTC().Add(-ttl)
	var result []PresenceEntry
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var entry PresenceEntry
		if err := json.Unmarshal(data, &entry); err != nil {
			continue
		}
		if entry.Timestamp.Before(cutoff) {
			// Expired: clean up the file
			_ = os.Remove(filepath.Join(dir, e.Name()))
			continue
		}
		result = append(result, entry)
	}
	return result, nil
}

// ClearPresence removes all presence entries for the coordinator's agent.
func (c *Coordinator) ClearPresence() error {
	if c.AgentID == "" {
		return fmt.Errorf("no active agent; call RegisterAgent first")
	}
	return ClearAgentPresence(c.Repo.GraftDir, c.AgentID)
}

// ClearAgentPresence removes all presence entries for a specific agent ID.
func ClearAgentPresence(graftDir, agentID string) error {
	dir := presenceDir(graftDir)
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read presence dir: %w", err)
	}

	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var entry PresenceEntry
		if err := json.Unmarshal(data, &entry); err != nil {
			continue
		}
		if entry.AgentID == agentID {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
	return nil
}
