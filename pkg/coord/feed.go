package coord

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/odvcencio/graft/pkg/object"
)

// FeedEntry is the persisted feed blob -- a linked list via Parent hash.
type FeedEntry struct {
	Parent    string    `json:"parent,omitempty"` // hash of previous feed blob
	Event     FeedEvent `json:"event"`
	Timestamp time.Time `json:"timestamp"`
}

// FeedEvent is a single coordination event.
type FeedEvent struct {
	Event      string          `json:"event"`
	AgentID    string          `json:"agent_id"`
	AgentName  string          `json:"agent_name"`
	CommitHash string          `json:"commit_hash,omitempty"`
	Entities   []EntityChange  `json:"entities,omitempty"`
	Impact     *ImpactReport   `json:"impact,omitempty"`
	FeedHash   string          `json:"-"`
	Detail     map[string]any  `json:"detail,omitempty"`
	Digest     *ActivityDigest `json:"digest,omitempty"`
	Source     string          `json:"source,omitempty"`
}

// ActivityDigest summarizes agent activity over a time period.
type ActivityDigest struct {
	ToolCalls    int      `json:"tool_calls"`
	FilesRead    []string `json:"files_read,omitempty"`
	FilesWritten []string `json:"files_written,omitempty"`
	ActiveFiles  []string `json:"active_files"`
	Period       int      `json:"period_s"`
	Blocked      int      `json:"blocked,omitempty"`
	Advisories   int      `json:"advisories,omitempty"`
}

// EntityChange describes a single entity modification.
type EntityChange struct {
	Key          string `json:"key"`
	File         string `json:"file"`
	Change       string `json:"change"` // signature_changed, body_changed, entity_added, entity_removed
	Breaking     bool   `json:"breaking,omitempty"`
	OldSignature string `json:"old_signature,omitempty"`
	NewSignature string `json:"new_signature,omitempty"`
}

// ImpactReport summarizes cross-repo impact.
type ImpactReport struct {
	Workspaces map[string]WorkspaceImpact `json:"workspaces,omitempty"`
}

type WorkspaceImpact struct {
	Callers        []string `json:"callers,omitempty"`
	AgentsAffected []string `json:"agents_affected,omitempty"`
}

const feedHeadRef = "refs/coord/feed/head"
const maxFeedRetries = 20

// AppendFeed appends an event to the feed chain with CAS retry.
// Feed entries are stored as blobs (not commits) to avoid format coupling.
// On retry exhaustion, writes to overflow log -- events are never dropped.
func (c *Coordinator) AppendFeed(event FeedEvent) error {
	for attempt := 0; attempt < maxFeedRetries; attempt++ {
		// Read current head
		parentHash, _ := c.Repo.ResolveRef(feedHeadRef)

		entry := FeedEntry{
			Parent:    string(parentHash),
			Event:     event,
			Timestamp: time.Now().UTC(),
		}

		data, err := json.Marshal(entry)
		if err != nil {
			return fmt.Errorf("marshal feed entry: %w", err)
		}

		blobHash, err := c.Repo.Store.WriteBlob(&object.Blob{Data: data})
		if err != nil {
			return fmt.Errorf("write feed blob: %w", err)
		}

		// CAS update head ref (always use CAS for correctness under concurrency)
		err = c.Repo.UpdateRefCAS(feedHeadRef, blobHash, parentHash)
		if err == nil {
			// Auto-push coord refs if configured
			if c.ShouldAutoPush() {
				_ = c.PushCoordRefs()
			}
			return nil
		}

		// CAS mismatch -- retry with jitter
		jitter := time.Duration(rand.Intn(10*(attempt+1))) * time.Millisecond
		time.Sleep(jitter)
	}

	// Overflow: write to local overflow log so events are never dropped
	return c.writeOverflow(event)
}

// writeOverflow persists a feed event to .graft/coord/feed-overflow/ for later merge.
func (c *Coordinator) writeOverflow(event FeedEvent) error {
	dir := filepath.Join(c.Repo.GraftDir, "coord", "feed-overflow")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, _ := json.Marshal(event)
	name := fmt.Sprintf("%d-%s.json", time.Now().UnixNano(), event.AgentID)
	return os.WriteFile(filepath.Join(dir, name), data, 0o644)
}

// WalkFeed walks the feed chain from head, returning up to limit events.
// If sinceHash is non-empty, only returns events newer than sinceHash.
// If sinceHash points to a pruned entry, resets to oldest available.
func (c *Coordinator) WalkFeed(sinceHash string, limit int) ([]FeedEvent, error) {
	headHash, err := c.Repo.ResolveRef(feedHeadRef)
	if err != nil {
		return nil, nil // empty feed
	}

	var events []FeedEvent
	currentHash := headHash

	for i := 0; i < limit && currentHash != ""; i++ {
		if string(currentHash) == sinceHash {
			break
		}

		blob, err := c.Repo.Store.ReadBlob(currentHash)
		if err != nil {
			break // chain broken (pruned) -- return what we have
		}

		var entry FeedEntry
		if err := json.Unmarshal(blob.Data, &entry); err != nil {
			break // corrupt entry -- return what we have
		}
		entry.Event.FeedHash = string(currentHash)
		events = append(events, entry.Event)
		currentHash = object.Hash(entry.Parent)
	}

	return events, nil
}

// --- Task 20: Feed cursor management ---

// SaveCursor persists an agent's feed read position.
// Stored at .graft/coord/cursor/{agent-id} (local-only, not synced via refs).
func (c *Coordinator) SaveCursor(agentID, feedHash string) error {
	dir := filepath.Join(c.Repo.GraftDir, "coord", "cursor")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, agentID), []byte(feedHash), 0o644)
}

// LoadCursor reads an agent's last-read feed position.
func (c *Coordinator) LoadCursor(agentID string) (string, error) {
	data, err := os.ReadFile(filepath.Join(c.Repo.GraftDir, "coord", "cursor", agentID))
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil // no cursor = read from beginning
		}
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// WalkFeedSinceCursor walks the feed from head, stopping at the agent's cursor.
func (c *Coordinator) WalkFeedSinceCursor(agentID string, limit int) ([]FeedEvent, error) {
	cursor, err := c.LoadCursor(agentID)
	if err != nil {
		return nil, err
	}
	return c.WalkFeed(cursor, limit)
}

// --- Task 21: Feed pruning ---

// PruneFeed keeps the newest `keep` entries and truncates the rest.
// The oldest retained entry is rewritten with an empty parent to break the chain.
// Returns the number of entries pruned.
func (c *Coordinator) PruneFeed(keep int) (int, error) {
	headHash, err := c.Repo.ResolveRef(feedHeadRef)
	if err != nil {
		return 0, nil // empty feed
	}

	// Walk the full chain to collect all entries
	type rawEntry struct {
		entry FeedEntry
	}
	var all []rawEntry
	cur := headHash
	for cur != "" {
		blob, err := c.Repo.Store.ReadBlob(cur)
		if err != nil {
			break
		}
		var e FeedEntry
		if err := json.Unmarshal(blob.Data, &e); err != nil {
			break
		}
		all = append(all, rawEntry{entry: e})
		cur = object.Hash(e.Parent)
	}

	if len(all) <= keep {
		return 0, nil // nothing to prune
	}

	pruned := len(all) - keep
	kept := all[:keep] // newest entries (head-first order)

	// Rebuild the chain from tail to head with corrected parent pointers
	var prevHash string
	for i := len(kept) - 1; i >= 0; i-- {
		kept[i].entry.Parent = prevHash
		data, err := json.Marshal(kept[i].entry)
		if err != nil {
			return 0, fmt.Errorf("marshal pruned entry: %w", err)
		}
		h, err := c.Repo.Store.WriteBlob(&object.Blob{Data: data})
		if err != nil {
			return 0, fmt.Errorf("write pruned blob: %w", err)
		}
		prevHash = string(h)
	}

	// Update head ref to new chain head via CAS
	newHead := object.Hash(prevHash)
	if err := c.Repo.UpdateRefCAS(feedHeadRef, newHead, headHash); err != nil {
		return 0, fmt.Errorf("update head after prune: %w", err)
	}

	return pruned, nil
}

// --- Task 22: Feed overflow merge ---

// MergeOverflow reads .graft/coord/feed-overflow/*.json files and appends
// them to the feed chain. Successfully merged files are removed.
func (c *Coordinator) MergeOverflow() (int, error) {
	dir := filepath.Join(c.Repo.GraftDir, "coord", "feed-overflow")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}

	merged := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var evt FeedEvent
		if err := json.Unmarshal(data, &evt); err != nil {
			continue
		}
		if err := c.AppendFeed(evt); err != nil {
			continue // will retry next time
		}
		os.Remove(path)
		merged++
	}
	return merged, nil
}
