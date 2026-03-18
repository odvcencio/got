package coord

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	graftrepo "github.com/odvcencio/graft/pkg/repo"
)

const (
	ClaimEditing  = "editing"
	ClaimWatching = "watching"
)

// ClaimInfo is the persisted claim data.
type ClaimInfo struct {
	EntityKey     string    `json:"entity_key"`
	EntityKeyHash string    `json:"entity_key_hash"`
	File          string    `json:"file"`
	Agent         string    `json:"agent"`
	AgentName     string    `json:"agent_name"`
	Mode          string    `json:"mode"`
	ClaimedAt     time.Time `json:"claimed_at"`
}

// ClaimRequest is the input for acquiring a claim.
type ClaimRequest struct {
	EntityKey string
	File      string
	Mode      string
}

// ClaimConflictError is returned when a claim is held by another agent.
type ClaimConflictError struct {
	EntityKey string
	HeldBy    string
	HeldByID  string
	Mode      string
	Decision  *ClaimPolicyDecision `json:"decision,omitempty"`
}

func (e *ClaimConflictError) Error() string {
	return fmt.Sprintf("entity %s already claimed by %s (mode: %s)", e.EntityKey, e.HeldBy, e.Mode)
}

// EntityKeyHash returns the SHA-256 hex hash of an entity identity key.
func EntityKeyHash(key string) string {
	h := sha256.Sum256([]byte(key))
	return fmt.Sprintf("%x", h)
}

// ForceClaimResult describes what happened when a force acquire succeeded.
type ForceClaimResult struct {
	Transferred       bool
	PreviousAgentID   string
	PreviousAgentName string
}

// AcquireClaim attempts to claim an entity for the given agent.
// Returns ErrEntityProtected if the entity matches a protected pattern.
//
// Watches are non-exclusive: multiple agents can watch the same entity.
// Each watch is stored at refs/coord/watches/{entity-key-hash}/{agent-id}.
// Editing claims are exclusive and stored at refs/coord/claims/{entity-key-hash}.
func (c *Coordinator) AcquireClaim(agentID string, req ClaimRequest) error {
	// Check if entity is protected (hard block regardless of conflict mode)
	if req.Mode == ClaimEditing && c.IsEntityProtected(req.EntityKey) {
		return fmt.Errorf("claim %q: %w", req.EntityKey, ErrEntityProtected)
	}

	keyHash := EntityKeyHash(req.EntityKey)

	// Watches always go to the watches namespace (non-exclusive).
	if req.Mode == ClaimWatching {
		watchRef := refPath("watches", keyHash, agentID)
		return c.writeClaimToRef(watchRef, agentID, req, keyHash)
	}

	// Editing claims use the exclusive claims namespace.
	ref := refPath("claims", keyHash)

	// Check if already claimed
	existing, err := c.LoadClaim(req.EntityKey)
	if err != nil {
		return err
	}
	if existing != nil {

		// Same agent reclaiming -- update
		if existing.Agent == agentID {
			return c.writeClaimToRef(ref, agentID, req, keyHash)
		}

		decision, err := c.EvaluateClaimDecision(agentID, req, existing)
		if err != nil {
			return fmt.Errorf("evaluate claim decision: %w", err)
		}

		// Conflict: existing editing claim held by another agent
		return &ClaimConflictError{
			EntityKey: req.EntityKey,
			HeldBy:    existing.AgentName,
			HeldByID:  existing.Agent,
			Mode:      existing.Mode,
			Decision:  decision,
		}
	}

	// No existing claim -- create
	return c.writeClaimToRef(ref, agentID, req, keyHash)
}

// ForceAcquireClaim atomically takes over an editing claim for the requesting
// agent. Callers should only use this after policy has produced a soft-block
// decision and the user has explicitly asked to override it.
func (c *Coordinator) ForceAcquireClaim(agentID string, req ClaimRequest, expectedOwnerID string) (*ForceClaimResult, error) {
	if req.Mode == ClaimEditing && c.IsEntityProtected(req.EntityKey) {
		return nil, fmt.Errorf("claim %q: %w", req.EntityKey, ErrEntityProtected)
	}
	if req.Mode == ClaimWatching {
		return &ForceClaimResult{}, c.AcquireClaim(agentID, req)
	}

	keyHash := EntityKeyHash(req.EntityKey)
	ref := refPath("claims", keyHash)
	oldHash, err := c.Repo.ResolveRef(ref)
	if err != nil {
		if err := c.writeClaimToRef(ref, agentID, req, keyHash); err != nil {
			return nil, err
		}
		return &ForceClaimResult{}, nil
	}

	var existing ClaimInfo
	if err := c.readJSONBlob(oldHash, &existing); err != nil {
		return nil, fmt.Errorf("read existing claim: %w", err)
	}
	if existing.Agent == agentID {
		if err := c.writeClaimToRef(ref, agentID, req, keyHash); err != nil {
			return nil, err
		}
		return &ForceClaimResult{}, nil
	}
	if expectedOwnerID != "" && existing.Agent != expectedOwnerID {
		decision, decisionErr := c.EvaluateClaimDecision(agentID, req, &existing)
		if decisionErr != nil {
			return nil, fmt.Errorf("evaluate claim decision: %w", decisionErr)
		}
		return nil, &ClaimConflictError{
			EntityKey: req.EntityKey,
			HeldBy:    existing.AgentName,
			HeldByID:  existing.Agent,
			Mode:      existing.Mode,
			Decision:  decision,
		}
	}

	agentName := agentID
	if agent, err := c.GetAgent(agentID); err == nil {
		agentName = agent.Name
	}
	previousAgentID := existing.Agent
	previousAgentName := existing.AgentName

	existing.File = req.File
	existing.Mode = req.Mode
	existing.Agent = agentID
	existing.AgentName = agentName
	existing.ClaimedAt = time.Now().UTC()

	newHash, err := c.writeJSONBlob(existing)
	if err != nil {
		return nil, err
	}
	if err := c.Repo.UpdateRefCAS(ref, newHash, oldHash); err != nil {
		if errors.Is(err, graftrepo.ErrRefCASMismatch) {
			current, loadErr := c.LoadClaim(req.EntityKey)
			if loadErr == nil && current != nil {
				decision, decisionErr := c.EvaluateClaimDecision(agentID, req, current)
				if decisionErr != nil {
					return nil, fmt.Errorf("evaluate claim decision: %w", decisionErr)
				}
				return nil, &ClaimConflictError{
					EntityKey: req.EntityKey,
					HeldBy:    current.AgentName,
					HeldByID:  current.Agent,
					Mode:      current.Mode,
					Decision:  decision,
				}
			}
		}
		return nil, err
	}

	return &ForceClaimResult{
		Transferred:       true,
		PreviousAgentID:   previousAgentID,
		PreviousAgentName: previousAgentName,
	}, nil
}

// LoadClaim returns the active editing claim for an entity, if one exists.
func (c *Coordinator) LoadClaim(entityKey string) (*ClaimInfo, error) {
	ref := refPath("claims", EntityKeyHash(entityKey))
	h, err := c.Repo.ResolveRef(ref)
	if err != nil {
		return nil, nil
	}

	var info ClaimInfo
	if err := c.readJSONBlob(h, &info); err != nil {
		return nil, fmt.Errorf("read existing claim: %w", err)
	}
	return &info, nil
}

// writeClaimToRef writes a claim blob and updates the given ref.
func (c *Coordinator) writeClaimToRef(ref, agentID string, req ClaimRequest, keyHash string) error {
	agentName := agentID
	if agent, err := c.GetAgent(agentID); err == nil {
		agentName = agent.Name
	}

	info := ClaimInfo{
		EntityKey:     req.EntityKey,
		EntityKeyHash: keyHash,
		File:          req.File,
		Agent:         agentID,
		AgentName:     agentName,
		Mode:          req.Mode,
		ClaimedAt:     time.Now().UTC(),
	}

	h, err := c.writeJSONBlob(info)
	if err != nil {
		return err
	}

	return c.Repo.UpdateRef(ref, h)
}

// ReleaseClaim removes a claim using the two-step tombstone protocol:
// 1. Write tombstone blob (empty Agent) via CAS -- crash-safe marker
// 2. Delete ref via DeleteRefCAS
// If step 1 succeeds but step 2 fails (crash), GC cleans up tombstones.
func (c *Coordinator) ReleaseClaim(keyHash string) error {
	ref := refPath("claims", keyHash)
	oldHash, err := c.Repo.ResolveRef(ref)
	if err != nil {
		return nil // already released
	}

	// Step 1: Write tombstone (empty Agent field)
	var existing ClaimInfo
	if err := c.readJSONBlob(oldHash, &existing); err != nil {
		return err
	}
	existing.Agent = ""
	existing.AgentName = ""
	tombstoneHash, err := c.writeJSONBlob(existing)
	if err != nil {
		return err
	}
	if err := c.Repo.UpdateRefCAS(ref, tombstoneHash, oldHash); err != nil {
		return fmt.Errorf("write tombstone: %w", err)
	}

	// Step 2: Delete the ref
	return c.Repo.DeleteRefCAS(ref, tombstoneHash)
}

// TransferClaim atomically transfers a claim from the current owner to a target agent.
// Caller must own the claim (verified via CAS).
func (c *Coordinator) TransferClaim(keyHash, fromAgentID, toAgentID string) error {
	ref := refPath("claims", keyHash)
	oldHash, err := c.Repo.ResolveRef(ref)
	if err != nil {
		return fmt.Errorf("claim not found: %w", err)
	}

	var existing ClaimInfo
	if err := c.readJSONBlob(oldHash, &existing); err != nil {
		return err
	}
	if existing.Agent != fromAgentID {
		return fmt.Errorf("claim owned by %s, not %s", existing.Agent, fromAgentID)
	}

	// Update owner to target agent
	targetAgent, err := c.GetAgent(toAgentID)
	if err != nil {
		return fmt.Errorf("target agent: %w", err)
	}
	existing.Agent = toAgentID
	existing.AgentName = targetAgent.Name
	existing.ClaimedAt = time.Now().UTC()

	newHash, err := c.writeJSONBlob(existing)
	if err != nil {
		return err
	}
	return c.Repo.UpdateRefCAS(ref, newHash, oldHash)
}

// ClaimsForFile returns all claims touching entities in a given file.
func (c *Coordinator) ClaimsForFile(filePath string) ([]ClaimInfo, error) {
	all, err := c.ListClaims()
	if err != nil {
		return nil, err
	}
	var matching []ClaimInfo
	for _, cl := range all {
		if cl.File == filePath {
			matching = append(matching, cl)
		}
	}
	return matching, nil
}

// ListClaims returns all active editing claims.
func (c *Coordinator) ListClaims() ([]ClaimInfo, error) {
	refs, err := c.Repo.ListRefs("coord/claims")
	if err != nil {
		return nil, fmt.Errorf("list claim refs: %w", err)
	}

	var claims []ClaimInfo
	for _, h := range refs {
		var info ClaimInfo
		if err := c.readJSONBlob(h, &info); err != nil {
			continue
		}
		claims = append(claims, info)
	}
	return claims, nil
}

// ListWatches returns all active watch claims.
func (c *Coordinator) ListWatches() ([]ClaimInfo, error) {
	refs, err := c.Repo.ListRefs("coord/watches")
	if err != nil {
		return nil, fmt.Errorf("list watch refs: %w", err)
	}

	var watches []ClaimInfo
	for _, h := range refs {
		var info ClaimInfo
		if err := c.readJSONBlob(h, &info); err != nil {
			continue
		}
		watches = append(watches, info)
	}
	return watches, nil
}

// ResolveEntityFile attempts to find the file path for an entity key
// by looking it up in the export index. Returns empty string if not found.
func (c *Coordinator) ResolveEntityFile(entityKey string) string {
	idx, err := c.LoadExportIndex()
	if err != nil {
		return ""
	}
	for _, pkg := range idx.Packages {
		for key, entity := range pkg {
			if key == entityKey {
				return entity.File
			}
		}
	}
	return ""
}

// ReleaseWatch removes a watch claim for a specific agent.
func (c *Coordinator) ReleaseWatch(keyHash, agentID string) error {
	ref := refPath("watches", keyHash, agentID)
	oldHash, err := c.Repo.ResolveRef(ref)
	if err != nil {
		return nil // already released
	}
	return c.Repo.DeleteRefCAS(ref, oldHash)
}
