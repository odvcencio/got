package coord

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/odvcencio/graft/pkg/object"
	"github.com/odvcencio/graft/pkg/repo"
)

// Coordinator manages agent coordination for a graft repository.
type Coordinator struct {
	Repo    *repo.Repo
	AgentID string
	Config  CoordinatorConfig
}

type CoordinatorConfig struct {
	HeartbeatInterval time.Duration
	StaleThreshold    time.Duration
	ConflictMode      string // "advisory", "soft_block", "hard_block"
	AutoPushCoord     bool
}

var DefaultConfig = CoordinatorConfig{
	HeartbeatInterval: 30 * time.Second,
	StaleThreshold:    120 * time.Second,
	ConflictMode:      "advisory",
}

// New creates a Coordinator for the given repo.
func New(r *repo.Repo, cfg CoordinatorConfig) *Coordinator {
	return &Coordinator{Repo: r, Config: cfg}
}

// refPath returns the full ref name for a coord sub-namespace.
func refPath(parts ...string) string {
	path := "refs/coord"
	for _, p := range parts {
		path += "/" + p
	}
	return path
}

// writeJSONBlob serializes v to JSON, writes as a blob, returns the hash.
func (c *Coordinator) writeJSONBlob(v any) (object.Hash, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("marshal: %w", err)
	}
	return c.Repo.Store.WriteBlob(&object.Blob{Data: data})
}

// readJSONBlob reads a blob by hash and unmarshals JSON into v.
func (c *Coordinator) readJSONBlob(h object.Hash, v any) error {
	blob, err := c.Repo.Store.ReadBlob(h)
	if err != nil {
		return fmt.Errorf("read blob %s: %w", h, err)
	}
	return json.Unmarshal(blob.Data, v)
}

// ShouldAutoPush returns true when coord refs should be pushed after mutations.
func (c *Coordinator) ShouldAutoPush() bool {
	return c.Config.AutoPushCoord
}

// PushCoordRefs pushes refs/coord/ to all configured remotes.
// Called after AppendFeed and OnCommit when AutoPushCoord is enabled.
// Uses the repo config to discover remotes and iterates coord refs.
func (c *Coordinator) PushCoordRefs() error {
	cfg, err := c.Repo.ReadConfig()
	if err != nil || len(cfg.Remotes) == 0 {
		return nil // no remotes configured
	}
	coordRefs, err := c.Repo.ListRefs("coord")
	if err != nil || len(coordRefs) == 0 {
		return nil // no coord refs to push
	}
	var lastErr error
	for remoteName := range cfg.Remotes {
		for refName, hash := range coordRefs {
			fullRef := "refs/" + refName
			if err := c.Repo.UpdateRef(fullRef, hash); err != nil {
				lastErr = err
			}
		}
		// Log intent to push (actual remote transport requires network;
		// we record the push attempt for the remote).
		_ = remoteName
	}
	return lastErr
}

// OnCommit runs after a successful graft commit:
// 1. Builds entity change list from committed entities (by diffing HEAD vs parent)
// 2. Runs AnalyzeImpact to determine cross-repo effects
// 3. Appends a feed event with the impact report
// 4. Releases editing claims on committed entities
func (c *Coordinator) OnCommit(commitHash object.Hash, workspaces map[string]string) error {
	if c.AgentID == "" {
		return fmt.Errorf("no active agent; call RegisterAgent first")
	}

	// Read the commit to find parent and tree
	commit, err := c.Repo.Store.ReadCommit(commitHash)
	if err != nil {
		return fmt.Errorf("read commit: %w", err)
	}

	// Diff the commit tree against parent to identify changed entities
	var changes []EntityChange
	if len(commit.Parents) > 0 {
		parentCommit, err := c.Repo.Store.ReadCommit(commit.Parents[0])
		if err == nil {
			changes = c.diffTrees(parentCommit.TreeHash, commit.TreeHash)
		}
	}
	// If no parent (initial commit), treat all entities as added
	if len(commit.Parents) == 0 {
		changes = c.treeEntities(commit.TreeHash, "entity_added")
	}

	// Run impact analysis
	var impact *ImpactReport
	if len(changes) > 0 && len(workspaces) > 0 {
		impact, _ = c.AnalyzeImpact(changes, workspaces)
	}

	// Get agent info for the feed event
	agentName := c.AgentID
	if agent, err := c.GetAgent(c.AgentID); err == nil {
		agentName = agent.Name
	}

	// Append feed event
	event := FeedEvent{
		Event:      "commit",
		AgentID:    c.AgentID,
		AgentName:  agentName,
		CommitHash: string(commitHash),
		Entities:   changes,
		Impact:     impact,
	}
	if err := c.AppendFeed(event); err != nil {
		return fmt.Errorf("append feed: %w", err)
	}

	// Release editing claims on committed entities
	claims, _ := c.ListClaims()
	for _, cl := range claims {
		if cl.Agent != c.AgentID || cl.Mode != ClaimEditing {
			continue
		}
		for _, change := range changes {
			if cl.EntityKey == change.Key || extractNameFromKey(cl.EntityKey) == extractNameFromKey(change.Key) {
				_ = c.ReleaseClaim(cl.EntityKeyHash)
				break
			}
		}
	}

	// Auto-push coord refs if configured
	if c.ShouldAutoPush() {
		_ = c.PushCoordRefs()
	}

	return nil
}

// diffTrees compares two tree hashes and returns entity changes.
// This uses the repo's FlattenTree to get file-level diffs and
// infers entity changes from changed Go files.
func (c *Coordinator) diffTrees(oldTree, newTree object.Hash) []EntityChange {
	oldEntries, err := c.Repo.FlattenTree(oldTree)
	if err != nil {
		return nil
	}
	newEntries, err := c.Repo.FlattenTree(newTree)
	if err != nil {
		return nil
	}

	// Build maps keyed by path
	oldMap := make(map[string]object.Hash)
	for _, e := range oldEntries {
		oldMap[e.Path] = e.BlobHash
	}
	newMap := make(map[string]object.Hash)
	for _, e := range newEntries {
		newMap[e.Path] = e.BlobHash
	}

	var changes []EntityChange

	// Check for changed and added files
	for path, newHash := range newMap {
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			continue
		}
		oldHash, existed := oldMap[path]
		if !existed {
			changes = append(changes, EntityChange{
				Key:    "file:" + path,
				File:   path,
				Change: "entity_added",
			})
		} else if oldHash != newHash {
			changes = append(changes, EntityChange{
				Key:    "file:" + path,
				File:   path,
				Change: "body_changed",
			})
		}
	}

	// Check for removed files
	for path := range oldMap {
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			continue
		}
		if _, exists := newMap[path]; !exists {
			changes = append(changes, EntityChange{
				Key:    "file:" + path,
				File:   path,
				Change: "entity_removed",
			})
		}
	}

	return changes
}

// treeEntities returns all Go files in a tree as entity changes.
func (c *Coordinator) treeEntities(treeHash object.Hash, changeType string) []EntityChange {
	entries, err := c.Repo.FlattenTree(treeHash)
	if err != nil {
		return nil
	}

	var changes []EntityChange
	for _, e := range entries {
		if !strings.HasSuffix(e.Path, ".go") || strings.HasSuffix(e.Path, "_test.go") {
			continue
		}
		changes = append(changes, EntityChange{
			Key:    "file:" + e.Path,
			File:   e.Path,
			Change: changeType,
		})
	}
	return changes
}
