package coord

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"time"
)

// AgentInfo describes a registered coordination agent.
type AgentInfo struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Workspace   string    `json:"workspace"`
	Host        string    `json:"host"`
	PID         int       `json:"pid"`
	StartedAt   time.Time `json:"started_at"`
	HeartbeatAt time.Time `json:"heartbeat_at"`
}

func generateAgentID() (string, error) {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// RegisterAgent creates a new agent entry in refs/coord/agents/{id}.
func (c *Coordinator) RegisterAgent(info AgentInfo) (string, error) {
	id, err := generateAgentID()
	if err != nil {
		return "", fmt.Errorf("generate agent ID: %w", err)
	}
	info.ID = id
	info.PID = os.Getpid()
	info.StartedAt = time.Now().UTC()
	info.HeartbeatAt = info.StartedAt

	h, err := c.writeJSONBlob(info)
	if err != nil {
		return "", err
	}

	ref := refPath("agents", id)
	if err := c.Repo.UpdateRef(ref, h); err != nil {
		return "", fmt.Errorf("write agent ref: %w", err)
	}

	c.AgentID = id
	return id, nil
}

// AgentStartedAt returns the StartedAt time for the current agent.
// Returns the zero value if no agent is registered.
func (c *Coordinator) AgentStartedAt() time.Time {
	if c.AgentID == "" {
		return time.Time{}
	}
	agent, err := c.GetAgent(c.AgentID)
	if err != nil {
		return time.Time{}
	}
	return agent.StartedAt
}

// GetAgent reads a single agent's info by ID.
func (c *Coordinator) GetAgent(id string) (*AgentInfo, error) {
	ref := refPath("agents", id)
	h, err := c.Repo.ResolveRef(ref)
	if err != nil {
		return nil, fmt.Errorf("agent %s not found: %w", id, err)
	}
	var info AgentInfo
	if err := c.readJSONBlob(h, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

// ListAgents returns all registered agents.
func (c *Coordinator) ListAgents() ([]AgentInfo, error) {
	prefix := "coord/agents"
	refs, err := c.Repo.ListRefs(prefix)
	if err != nil {
		return nil, fmt.Errorf("list agent refs: %w", err)
	}

	var agents []AgentInfo
	for _, h := range refs {
		var info AgentInfo
		if err := c.readJSONBlob(h, &info); err != nil {
			continue // skip corrupt entries
		}
		agents = append(agents, info)
	}
	return agents, nil
}

// Heartbeat updates the agent's heartbeat timestamp.
func (c *Coordinator) Heartbeat(id string) error {
	ref := refPath("agents", id)
	oldHash, err := c.Repo.ResolveRef(ref)
	if err != nil {
		return fmt.Errorf("agent %s not found: %w", id, err)
	}

	var info AgentInfo
	if err := c.readJSONBlob(oldHash, &info); err != nil {
		return err
	}

	info.HeartbeatAt = time.Now().UTC()

	newHash, err := c.writeJSONBlob(info)
	if err != nil {
		return err
	}

	return c.Repo.UpdateRefCAS(ref, newHash, oldHash)
}

// DeregisterAgent removes an agent and all its claims and watches.
func (c *Coordinator) DeregisterAgent(id string) error {
	// Remove claims owned by this agent
	claims, err := c.ListClaims()
	if err != nil && !strings.Contains(err.Error(), "not found") {
		return fmt.Errorf("list claims for cleanup: %w", err)
	}
	for _, cl := range claims {
		if cl.Agent == id {
			_ = c.ReleaseClaim(cl.EntityKeyHash)
		}
	}

	// Remove watches owned by this agent
	watches, err := c.ListWatches()
	if err == nil {
		for _, w := range watches {
			if w.Agent == id {
				_ = c.ReleaseWatch(w.EntityKeyHash, id)
			}
		}
	}

	// Remove agent ref
	ref := refPath("agents", id)
	h, err := c.Repo.ResolveRef(ref)
	if err != nil {
		return nil // already gone
	}
	return c.Repo.DeleteRefCAS(ref, h)
}

// GCStaleAgents removes agents whose heartbeat is older than StaleThreshold.
func (c *Coordinator) GCStaleAgents() ([]AgentInfo, error) {
	agents, err := c.ListAgents()
	if err != nil {
		return nil, err
	}

	var removed []AgentInfo
	cutoff := time.Now().UTC().Add(-c.Config.StaleThreshold)
	for _, a := range agents {
		if a.HeartbeatAt.Before(cutoff) {
			if err := c.DeregisterAgent(a.ID); err == nil {
				removed = append(removed, a)
			}
		}
	}
	return removed, nil
}
