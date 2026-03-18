# Graft Coord Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add multi-agent entity-level coordination to graft using its own object store and CAS refs.

**Architecture:** Coordination state stored as graft blobs referenced by refs under `refs/coord/`. Peer-mesh federation across local workspaces via `~/.graftconfig`. One engine (`pkg/coord/`), two transports (CLI + MCP). Cross-repo impact via workspace graph, export index, and xref index.

**Tech Stack:** Go 1.25, cobra CLI, graft object store, graft CAS refs, JSON serialization, MCP JSON-RPC over stdio.

**Spec:** `docs/plans/2026-03-12-graft-coord-design.md`

---

## File Structure

### New Files

```
pkg/coord/
  coord.go          — Coordinator struct, config, initialization
  coord_test.go     — Integration tests for coordinator lifecycle
  agent.go          — Agent registry: register, heartbeat, deregister, GC
  agent_test.go     — Agent registry tests
  claim.go          — Claim acquire/release/transfer, conflict detection
  claim_test.go     — Claim CAS tests including race scenarios
  feed.go           — Feed append (with CAS retry), walk, cursor, prune
  feed_test.go      — Feed chain tests
  workspace.go      — Workspace graph: discovery, go.mod parsing, federation
  workspace_test.go — Workspace graph tests
  export.go         — Export index: build, diff, incremental rebuild
  export_test.go    — Export index tests
  xref.go           — Xref index: build, reverse lookup, incremental rebuild
  xref_test.go      — Xref index tests
  impact.go         — Impact analysis: combine workspace graph + export + xref + claims
  impact_test.go    — Impact analysis tests
  transport.go      — PeerTransport interface: local (filesystem) and remote (graft client)
  transport_test.go — Transport tests

cmd/graft/
  cmd_workon.go     — graft workon command
  cmd_coord.go      — graft coord command tree
  cmd_workspace.go  — graft workspace command
  cmd_mcp.go        — graft mcp serve command
```

### Modified Files

```
pkg/repo/init.go          — Add DeleteRefCAS method
pkg/repo/refs.go          — Add ref namespace reservation for refs/coord/
pkg/repo/staging.go       — Hook coord claim acquisition into Add flow
pkg/userconfig/config.go  — Add Workspaces and Coord fields to Config struct
cmd/graft/main.go         — Register new commands (workon, coord, workspace, mcp)
```

---

## Chunk 1: Prerequisites and Core Data Model

### Task 1: Add DeleteRefCAS to pkg/repo

**Files:**
- Modify: `pkg/repo/init.go:331` (near UpdateRefCAS)
- Test: `pkg/repo/refs_test.go`

- [ ] **Step 1: Write failing test for DeleteRefCAS**

```go
// In pkg/repo/refs_test.go

func TestDeleteRefCAS_Success(t *testing.T) {
	r, err := Init(t.TempDir())
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	// Create a ref
	h := object.Hash("abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234")
	if err := r.UpdateRef("refs/coord/test/ref1", h); err != nil {
		t.Fatalf("UpdateRef: %v", err)
	}
	// Delete with correct expected old
	if err := r.DeleteRefCAS("refs/coord/test/ref1", h); err != nil {
		t.Fatalf("DeleteRefCAS: %v", err)
	}
	// Verify ref is gone
	_, err = r.ResolveRef("refs/coord/test/ref1")
	if err == nil {
		t.Fatal("expected error resolving deleted ref")
	}
}

func TestDeleteRefCAS_Mismatch(t *testing.T) {
	r, err := Init(t.TempDir())
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	h := object.Hash("abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234")
	if err := r.UpdateRef("refs/coord/test/ref1", h); err != nil {
		t.Fatalf("UpdateRef: %v", err)
	}
	wrong := object.Hash("0000000000000000000000000000000000000000000000000000000000000000")
	err = r.DeleteRefCAS("refs/coord/test/ref1", wrong)
	if err == nil {
		t.Fatal("expected CAS mismatch error")
	}
	if !errors.Is(err, ErrRefCASMismatch) {
		t.Fatalf("expected ErrRefCASMismatch, got: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/draco/work/graft && go test ./pkg/repo/ -run TestDeleteRefCAS -v`
Expected: FAIL — `DeleteRefCAS` undefined

- [ ] **Step 3: Implement DeleteRefCAS**

Add to `pkg/repo/init.go` after `UpdateRefCAS`. Must use the existing `acquireRefLock` helper with retry logic (matching `UpdateRefCAS` pattern), not raw `os.OpenFile`:

```go
// DeleteRefCAS removes a ref atomically, only if its current value matches expectedOld.
// Follows the same lock pattern as UpdateRefCAS: acquireRefLock on lockPath,
// readRefHash under lock, remove ref file, then clean up lock.
func (r *Repo) DeleteRefCAS(name string, expectedOld object.Hash) error {
	baseDir := r.refsBaseDir()
	refPath := filepath.Join(baseDir, name)
	lockPath := refPath + ".lock"

	lockFile, err := acquireRefLock(lockPath)
	if err != nil {
		return fmt.Errorf("delete ref %q: lock: %w", name, err)
	}
	defer func() {
		if lockFile != nil {
			_ = lockFile.Close()
		}
		_ = os.Remove(lockPath)
	}()

	// Read under lock — authoritative check
	oldHash, err := readRefHash(refPath)
	if err != nil {
		return fmt.Errorf("delete ref %q: not found: %w", name, err)
	}
	if oldHash != expectedOld {
		return fmt.Errorf(
			"delete ref %q: %w (expected %s, found %s)",
			name, ErrRefCASMismatch, expectedOld, oldHash,
		)
	}

	if err := os.Remove(refPath); err != nil {
		return fmt.Errorf("delete ref %q: remove: %w", name, err)
	}

	_ = lockFile.Close()
	lockFile = nil

	// Clean up empty parent directories up to refs/
	dir := filepath.Dir(refPath)
	refsDir := filepath.Join(baseDir, "refs")
	for dir != refsDir && dir != baseDir {
		entries, _ := os.ReadDir(dir)
		if len(entries) > 0 {
			break
		}
		os.Remove(dir)
		dir = filepath.Dir(dir)
	}

	return nil
}
```

Also add test for non-existent ref:
```go
func TestDeleteRefCAS_NotFound(t *testing.T) {
	r, err := Init(t.TempDir())
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	h := object.Hash("abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234")
	err = r.DeleteRefCAS("refs/coord/nonexistent", h)
	if err == nil {
		t.Fatal("expected error deleting non-existent ref")
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /home/draco/work/graft && go test ./pkg/repo/ -run TestDeleteRefCAS -v`
Expected: PASS

- [ ] **Step 5: Commit**

Run: `cd /home/draco/work/graft && buckley commit --yes --minimal-output`

---

### Task 2: Extend userconfig with Workspaces and Coord

**Files:**
- Modify: `pkg/userconfig/config.go:29`
- Test: `pkg/userconfig/config_test.go`

- [ ] **Step 1: Write failing test for workspace config**

```go
// In pkg/userconfig/config_test.go

func TestConfigWorkspacesRoundTrip(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", origHome)

	cfg := &Config{
		Version: 1,
		Name:    "draco",
		Workspaces: map[string]string{
			"graft":   "/home/draco/work/graft",
			"orchard": "/home/draco/work/orchard",
		},
		Coord: CoordConfig{
			HeartbeatInterval: "30s",
			StaleThreshold:    "120s",
			FeedRetention:     "7d",
			DefaultConflictMode: "advisory",
		},
	}
	if err := Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Workspaces["graft"] != "/home/draco/work/graft" {
		t.Errorf("workspace graft = %q, want /home/draco/work/graft", loaded.Workspaces["graft"])
	}
	if loaded.Coord.HeartbeatInterval != "30s" {
		t.Errorf("heartbeat = %q, want 30s", loaded.Coord.HeartbeatInterval)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/draco/work/graft && go test ./pkg/userconfig/ -run TestConfigWorkspaces -v`
Expected: FAIL — `Workspaces` and `CoordConfig` undefined

- [ ] **Step 3: Extend Config struct**

In `pkg/userconfig/config.go`, add to the `Config` struct:

```go
type CoordConfig struct {
	HeartbeatInterval   string `json:"heartbeat_interval,omitempty"`
	StaleThreshold      string `json:"stale_threshold,omitempty"`
	AutoPushCoord       bool   `json:"auto_push_coord,omitempty"`
	FeedRetention       string `json:"feed_retention,omitempty"`
	DefaultConflictMode string `json:"default_conflict_mode,omitempty"`
}

// Add to Config struct:
Workspaces map[string]string `json:"workspaces,omitempty"`
Coord      CoordConfig       `json:"coord,omitempty"`
```

- [ ] **Step 4: Run tests**

Run: `cd /home/draco/work/graft && go test ./pkg/userconfig/ -v`
Expected: PASS (all existing + new)

- [ ] **Step 5: Commit**

Run: `cd /home/draco/work/graft && buckley commit --yes --minimal-output`

---

### Task 3: Coordinator core and agent registry

**Files:**
- Create: `pkg/coord/coord.go`
- Create: `pkg/coord/agent.go`
- Create: `pkg/coord/agent_test.go`

- [ ] **Step 1: Create coord.go with Coordinator struct**

```go
package coord

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/odvcencio/graft/pkg/object"
	"github.com/odvcencio/graft/pkg/repo"
)

// Coordinator manages agent coordination for a graft repository.
type Coordinator struct {
	Repo     *repo.Repo
	AgentID  string
	Config   CoordinatorConfig
}

type CoordinatorConfig struct {
	HeartbeatInterval time.Duration
	StaleThreshold    time.Duration
	ConflictMode      string // "advisory", "soft_block", "hard_block"
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
```

- [ ] **Step 2: Write failing test for agent registration**

```go
// pkg/coord/agent_test.go
package coord

import (
	"testing"
	"time"

	"github.com/odvcencio/graft/pkg/repo"
)

func newTestCoordinator(t *testing.T) *Coordinator {
	t.Helper()
	r, err := repo.Init(t.TempDir())
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	return New(r, DefaultConfig)
}

func TestRegisterAgent(t *testing.T) {
	c := newTestCoordinator(t)

	info := AgentInfo{
		Name:      "test-agent",
		Workspace: "graft",
		Host:      "test-host",
	}
	id, err := c.RegisterAgent(info)
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty agent ID")
	}

	agents, err := c.ListAgents()
	if err != nil {
		t.Fatalf("ListAgents: %v", err)
	}
	if len(agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(agents))
	}
	if agents[0].Name != "test-agent" {
		t.Errorf("agent name = %q, want test-agent", agents[0].Name)
	}
}

func TestDeregisterAgent(t *testing.T) {
	c := newTestCoordinator(t)

	info := AgentInfo{Name: "temp-agent", Workspace: "graft", Host: "test"}
	id, err := c.RegisterAgent(info)
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}

	if err := c.DeregisterAgent(id); err != nil {
		t.Fatalf("DeregisterAgent: %v", err)
	}

	agents, err := c.ListAgents()
	if err != nil {
		t.Fatalf("ListAgents: %v", err)
	}
	if len(agents) != 0 {
		t.Fatalf("expected 0 agents after deregister, got %d", len(agents))
	}
}

func TestHeartbeat(t *testing.T) {
	c := newTestCoordinator(t)

	info := AgentInfo{Name: "hb-agent", Workspace: "graft", Host: "test"}
	id, err := c.RegisterAgent(info)
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}

	before, err := c.GetAgent(id)
	if err != nil {
		t.Fatalf("GetAgent: %v", err)
	}

	time.Sleep(10 * time.Millisecond)
	if err := c.Heartbeat(id); err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}

	after, err := c.GetAgent(id)
	if err != nil {
		t.Fatalf("GetAgent: %v", err)
	}

	if !after.HeartbeatAt.After(before.HeartbeatAt) {
		t.Error("heartbeat timestamp did not advance")
	}
}

func TestGCStaleAgents(t *testing.T) {
	c := newTestCoordinator(t)
	c.Config.StaleThreshold = 1 * time.Millisecond

	info := AgentInfo{Name: "stale-agent", Workspace: "graft", Host: "test"}
	_, err := c.RegisterAgent(info)
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}

	time.Sleep(5 * time.Millisecond)

	removed, err := c.GCStaleAgents()
	if err != nil {
		t.Fatalf("GCStaleAgents: %v", err)
	}
	if len(removed) != 1 {
		t.Fatalf("expected 1 removed, got %d", len(removed))
	}

	agents, err := c.ListAgents()
	if err != nil {
		t.Fatalf("ListAgents: %v", err)
	}
	if len(agents) != 0 {
		t.Fatalf("expected 0 agents after GC, got %d", len(agents))
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `cd /home/draco/work/graft && go test ./pkg/coord/ -v`
Expected: FAIL — package doesn't exist yet / types undefined

- [ ] **Step 4: Implement agent.go**

```go
// pkg/coord/agent.go
package coord

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/odvcencio/graft/pkg/object"
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
	for name, h := range refs {
		_ = name
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

// DeregisterAgent removes an agent and all its claims.
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
```

- [ ] **Step 5: Create minimal claim.go stubs**

`DeregisterAgent` calls `ListClaims()` and `ReleaseClaim()` which are defined in Task 4. Add minimal stubs so Task 3 compiles:

```go
// pkg/coord/claim.go — stubs, replaced in Task 4
package coord

// ListClaims returns all active claims. Stub — returns nil.
func (c *Coordinator) ListClaims() ([]ClaimInfo, error) { return nil, nil }

// ReleaseClaim removes a claim. Stub — no-op.
func (c *Coordinator) ReleaseClaim(keyHash string) error { return nil }

// ClaimInfo is the persisted claim data. Minimal definition for Task 3.
type ClaimInfo struct {
	Agent string `json:"agent"`
	EntityKeyHash string `json:"entity_key_hash"`
}
```

- [ ] **Step 6: Run tests**

Run: `cd /home/draco/work/graft && go test ./pkg/coord/ -run TestRegister -v && go test ./pkg/coord/ -run TestDeregister -v && go test ./pkg/coord/ -run TestHeartbeat -v && go test ./pkg/coord/ -run TestGCStale -v`
Expected: PASS

- [ ] **Step 7: Commit**

Run: `cd /home/draco/work/graft && buckley commit --yes --minimal-output`

---

### Task 4: Claim management

**Files:**
- Create: `pkg/coord/claim.go`
- Create: `pkg/coord/claim_test.go`

- [ ] **Step 1: Write failing tests for claims**

```go
// pkg/coord/claim_test.go
package coord

import (
	"testing"
)

func TestAcquireAndListClaims(t *testing.T) {
	c := newTestCoordinator(t)
	id, _ := c.RegisterAgent(AgentInfo{Name: "claimer", Workspace: "graft", Host: "test"})

	err := c.AcquireClaim(id, ClaimRequest{
		EntityKey: "decl:function_definition::DiffFiles:func DiffFiles():0",
		File:      "pkg/diff/diff.go",
		Mode:      ClaimEditing,
	})
	if err != nil {
		t.Fatalf("AcquireClaim: %v", err)
	}

	claims, err := c.ListClaims()
	if err != nil {
		t.Fatalf("ListClaims: %v", err)
	}
	if len(claims) != 1 {
		t.Fatalf("expected 1 claim, got %d", len(claims))
	}
	if claims[0].AgentName != "claimer" {
		t.Errorf("claim agent = %q, want claimer", claims[0].AgentName)
	}
}

func TestAcquireClaimConflict(t *testing.T) {
	c := newTestCoordinator(t)

	id1, _ := c.RegisterAgent(AgentInfo{Name: "agent-1", Workspace: "graft", Host: "test"})
	id2, _ := c.RegisterAgent(AgentInfo{Name: "agent-2", Workspace: "graft", Host: "test"})

	req := ClaimRequest{
		EntityKey: "decl:function_definition::Merge:func Merge():0",
		File:      "pkg/merge/merge.go",
		Mode:      ClaimEditing,
	}

	if err := c.AcquireClaim(id1, req); err != nil {
		t.Fatalf("first claim: %v", err)
	}

	err := c.AcquireClaim(id2, req)
	if err == nil {
		t.Fatal("expected conflict error on second claim")
	}
	conflict, ok := err.(*ClaimConflictError)
	if !ok {
		t.Fatalf("expected ClaimConflictError, got %T: %v", err, err)
	}
	if conflict.HeldBy != "agent-1" {
		t.Errorf("conflict held by %q, want agent-1", conflict.HeldBy)
	}
}

func TestReleaseClaim(t *testing.T) {
	c := newTestCoordinator(t)
	id, _ := c.RegisterAgent(AgentInfo{Name: "releaser", Workspace: "graft", Host: "test"})

	req := ClaimRequest{
		EntityKey: "decl:function_definition::Foo:func Foo():0",
		File:      "foo.go",
		Mode:      ClaimEditing,
	}
	if err := c.AcquireClaim(id, req); err != nil {
		t.Fatalf("AcquireClaim: %v", err)
	}

	keyHash := EntityKeyHash(req.EntityKey)
	if err := c.ReleaseClaim(keyHash); err != nil {
		t.Fatalf("ReleaseClaim: %v", err)
	}

	claims, _ := c.ListClaims()
	if len(claims) != 0 {
		t.Fatalf("expected 0 claims after release, got %d", len(claims))
	}
}

func TestTransferClaim(t *testing.T) {
	c := newTestCoordinator(t)
	id1, _ := c.RegisterAgent(AgentInfo{Name: "owner", Workspace: "graft", Host: "test"})
	id2, _ := c.RegisterAgent(AgentInfo{Name: "receiver", Workspace: "graft", Host: "test"})

	entityKey := "decl:function_definition::Transfer:func Transfer():0"
	if err := c.AcquireClaim(id1, ClaimRequest{EntityKey: entityKey, File: "t.go", Mode: ClaimEditing}); err != nil {
		t.Fatalf("AcquireClaim: %v", err)
	}

	keyHash := EntityKeyHash(entityKey)
	if err := c.TransferClaim(keyHash, id1, id2); err != nil {
		t.Fatalf("TransferClaim: %v", err)
	}

	claims, _ := c.ListClaims()
	if len(claims) != 1 {
		t.Fatalf("expected 1 claim, got %d", len(claims))
	}
	if claims[0].Agent != id2 {
		t.Errorf("claim agent = %s, want %s", claims[0].Agent, id2)
	}
}

func TestTransferClaim_WrongOwner(t *testing.T) {
	c := newTestCoordinator(t)
	id1, _ := c.RegisterAgent(AgentInfo{Name: "owner", Workspace: "graft", Host: "test"})
	id2, _ := c.RegisterAgent(AgentInfo{Name: "other", Workspace: "graft", Host: "test"})

	entityKey := "decl:function_definition::Owned:func Owned():0"
	c.AcquireClaim(id1, ClaimRequest{EntityKey: entityKey, File: "o.go", Mode: ClaimEditing})

	keyHash := EntityKeyHash(entityKey)
	err := c.TransferClaim(keyHash, id2, id1) // id2 doesn't own it
	if err == nil {
		t.Fatal("expected error transferring claim not owned by caller")
	}
}

func TestWatchingClaimDoesNotBlock(t *testing.T) {
	c := newTestCoordinator(t)
	id1, _ := c.RegisterAgent(AgentInfo{Name: "watcher", Workspace: "graft", Host: "test"})
	id2, _ := c.RegisterAgent(AgentInfo{Name: "editor", Workspace: "graft", Host: "test"})

	entityKey := "decl:function_definition::Bar:func Bar():0"

	// Watching claim
	if err := c.AcquireClaim(id1, ClaimRequest{EntityKey: entityKey, File: "bar.go", Mode: ClaimWatching}); err != nil {
		t.Fatalf("watch claim: %v", err)
	}

	// Editing claim should succeed (watching doesn't block)
	if err := c.AcquireClaim(id2, ClaimRequest{EntityKey: entityKey, File: "bar.go", Mode: ClaimEditing}); err != nil {
		t.Fatalf("edit claim should not be blocked by watcher: %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /home/draco/work/graft && go test ./pkg/coord/ -run TestAcquire -v`
Expected: FAIL

- [ ] **Step 3: Implement claim.go**

```go
// pkg/coord/claim.go
package coord

import (
	"crypto/sha256"
	"fmt"
	"time"
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
}

func (e *ClaimConflictError) Error() string {
	return fmt.Sprintf("entity %s already claimed by %s (mode: %s)", e.EntityKey, e.HeldBy, e.Mode)
}

// EntityKeyHash returns the SHA-256 hex hash of an entity identity key.
func EntityKeyHash(key string) string {
	h := sha256.Sum256([]byte(key))
	return fmt.Sprintf("%x", h)
}

// AcquireClaim attempts to claim an entity for the given agent.
func (c *Coordinator) AcquireClaim(agentID string, req ClaimRequest) error {
	keyHash := EntityKeyHash(req.EntityKey)
	ref := refPath("claims", keyHash)

	// Check if already claimed
	existingHash, err := c.Repo.ResolveRef(ref)
	if err == nil {
		// Ref exists — check who holds it
		var existing ClaimInfo
		if err := c.readJSONBlob(existingHash, &existing); err != nil {
			return fmt.Errorf("read existing claim: %w", err)
		}

		// Same agent reclaiming — update
		if existing.Agent == agentID {
			return c.writeClaimToRef(ref, agentID, req, keyHash)
		}

		// Existing is watching — editing can proceed (overwrite the watch)
		if existing.Mode == ClaimWatching && req.Mode == ClaimEditing {
			return c.writeClaimToRef(ref, agentID, req, keyHash)
		}

		// New is watching — don't conflict with existing editing claim
		if req.Mode == ClaimWatching {
			// Watching doesn't need the ref — store in a separate namespace
			watchRef := refPath("watches", keyHash, agentID)
			return c.writeClaimToRef(watchRef, agentID, req, keyHash)
		}

		// Conflict: existing editing claim held by another agent
		return &ClaimConflictError{
			EntityKey: req.EntityKey,
			HeldBy:    existing.AgentName,
			HeldByID:  existing.Agent,
			Mode:      existing.Mode,
		}
	}

	// No existing claim — create
	return c.writeClaimToRef(ref, agentID, req, keyHash)
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
// 1. Write tombstone blob (empty Agent) via CAS — crash-safe marker
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
```

- [ ] **Step 4: Run all claim tests**

Run: `cd /home/draco/work/graft && go test ./pkg/coord/ -run "TestAcquire|TestRelease|TestWatching" -v`
Expected: PASS

- [ ] **Step 5: Commit**

Run: `cd /home/draco/work/graft && buckley commit --yes --minimal-output`

---

### Task 5: Feed — commit chain with CAS retry

**Files:**
- Create: `pkg/coord/feed.go`
- Create: `pkg/coord/feed_test.go`

- [ ] **Step 1: Write failing tests for feed**

```go
// pkg/coord/feed_test.go
package coord

import (
	"testing"
)

func TestAppendAndWalkFeed(t *testing.T) {
	c := newTestCoordinator(t)
	id, _ := c.RegisterAgent(AgentInfo{Name: "feeder", Workspace: "graft", Host: "test"})

	event1 := FeedEvent{
		Event:     "entity_changed",
		AgentID:   id,
		AgentName: "feeder",
		Entities: []EntityChange{
			{Key: "decl:function_definition::Foo:func Foo():0", File: "foo.go", Change: "body_changed"},
		},
	}
	if err := c.AppendFeed(event1); err != nil {
		t.Fatalf("AppendFeed 1: %v", err)
	}

	event2 := FeedEvent{
		Event:     "entity_changed",
		AgentID:   id,
		AgentName: "feeder",
		Entities: []EntityChange{
			{Key: "decl:function_definition::Bar:func Bar():0", File: "bar.go", Change: "signature_changed"},
		},
	}
	if err := c.AppendFeed(event2); err != nil {
		t.Fatalf("AppendFeed 2: %v", err)
	}

	events, err := c.WalkFeed("", 10)
	if err != nil {
		t.Fatalf("WalkFeed: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	// Events are newest-first (head of chain)
	if events[0].Entities[0].File != "bar.go" {
		t.Errorf("first event file = %q, want bar.go", events[0].Entities[0].File)
	}
}

func TestWalkFeedSince(t *testing.T) {
	c := newTestCoordinator(t)
	id, _ := c.RegisterAgent(AgentInfo{Name: "feeder", Workspace: "graft", Host: "test"})

	// Append 3 events
	for i, name := range []string{"A", "B", "C"} {
		_ = i
		c.AppendFeed(FeedEvent{
			Event: "entity_changed", AgentID: id, AgentName: "feeder",
			Entities: []EntityChange{{Key: name, File: name + ".go", Change: "body_changed"}},
		})
	}

	// Get all, grab the hash of event B (second from head = index 1)
	all, _ := c.WalkFeed("", 10)
	if len(all) != 3 {
		t.Fatalf("expected 3, got %d", len(all))
	}

	// Walk since B's blob hash — should only return C (newer than B)
	sinceHash := all[1].FeedHash
	recent, err := c.WalkFeed(sinceHash, 10)
	if err != nil {
		t.Fatalf("WalkFeed since: %v", err)
	}
	if len(recent) != 1 {
		t.Fatalf("expected 1 event since B, got %d", len(recent))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /home/draco/work/graft && go test ./pkg/coord/ -run TestAppend -v`
Expected: FAIL

- [ ] **Step 3: Implement feed.go**

```go
// pkg/coord/feed.go
package coord

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"time"

	"github.com/odvcencio/graft/pkg/object"
)

// FeedEntry is the persisted feed blob — a linked list via Parent hash.
type FeedEntry struct {
	Parent    string    `json:"parent,omitempty"` // hash of previous feed blob
	Event     FeedEvent `json:"event"`
	Timestamp time.Time `json:"timestamp"`
}

// FeedEvent is a single coordination event.
type FeedEvent struct {
	Event      string         `json:"event"`
	AgentID    string         `json:"agent_id"`
	AgentName  string         `json:"agent_name"`
	CommitHash string         `json:"commit_hash,omitempty"`
	Entities   []EntityChange `json:"entities,omitempty"`
	Impact     *ImpactReport  `json:"impact,omitempty"`
	FeedHash   string         `json:"-"` // set on read
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
const maxFeedRetries = 5

// AppendFeed appends an event to the feed chain with CAS retry.
// Feed entries are stored as blobs (not commits) to avoid format coupling.
// On retry exhaustion, writes to overflow log — events are never dropped.
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

		// CAS update head ref
		if parentHash == "" {
			err = c.Repo.UpdateRef(feedHeadRef, blobHash)
		} else {
			err = c.Repo.UpdateRefCAS(feedHeadRef, blobHash, parentHash)
		}
		if err == nil {
			return nil
		}

		// CAS mismatch — retry with jitter
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
			break // chain broken (pruned) — return what we have
		}

		var entry FeedEntry
		if err := json.Unmarshal(blob.Data, &entry); err != nil {
			break // corrupt entry — return what we have
		}
		entry.Event.FeedHash = string(currentHash)
		events = append(events, entry.Event)
		currentHash = object.Hash(entry.Parent)
	}

	return events, nil
}
```

- [ ] **Step 4: Run feed tests**

Run: `cd /home/draco/work/graft && go test ./pkg/coord/ -run "TestAppend|TestWalkFeed" -v`
Expected: PASS

- [ ] **Step 5: Run all coord tests**

Run: `cd /home/draco/work/graft && go test ./pkg/coord/ -v`
Expected: PASS

- [ ] **Step 6: Commit**

Run: `cd /home/draco/work/graft && buckley commit --yes --minimal-output`

---

## Chunk 2: Workspace Discovery and Cross-Repo Resolution

### Task 6: Workspace graph and discovery

**Files:**
- Create: `pkg/coord/workspace.go`
- Create: `pkg/coord/workspace_test.go`

- [ ] **Step 1: Write failing test for workspace discovery**

```go
// pkg/coord/workspace_test.go
package coord

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseGoModDeps(t *testing.T) {
	dir := t.TempDir()
	gomod := `module github.com/odvcencio/orchard

go 1.25

require (
	github.com/odvcencio/graft v0.2.6
	github.com/odvcencio/gotreesitter v0.6.0
)

replace github.com/odvcencio/gotreesitter => /home/draco/work/gotreesitter
`
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o644); err != nil {
		t.Fatal(err)
	}

	deps, err := ParseGoModDeps(filepath.Join(dir, "go.mod"))
	if err != nil {
		t.Fatalf("ParseGoModDeps: %v", err)
	}

	if deps.Module != "github.com/odvcencio/orchard" {
		t.Errorf("module = %q", deps.Module)
	}
	if len(deps.Requires) != 2 {
		t.Fatalf("expected 2 requires, got %d", len(deps.Requires))
	}
	if deps.Replaces["github.com/odvcencio/gotreesitter"] != "/home/draco/work/gotreesitter" {
		t.Errorf("replace = %q", deps.Replaces["github.com/odvcencio/gotreesitter"])
	}
}

func TestBuildWorkspaceGraph(t *testing.T) {
	// Create mock workspace dirs with go.mod files
	root := t.TempDir()

	graftDir := filepath.Join(root, "graft")
	os.MkdirAll(graftDir, 0o755)
	os.WriteFile(filepath.Join(graftDir, "go.mod"), []byte(`module github.com/odvcencio/graft
go 1.25
require github.com/odvcencio/gotreesitter v0.6.0
replace github.com/odvcencio/gotreesitter => `+filepath.Join(root, "gotreesitter")+`
`), 0o644)

	orchardDir := filepath.Join(root, "orchard")
	os.MkdirAll(orchardDir, 0o755)
	os.WriteFile(filepath.Join(orchardDir, "go.mod"), []byte(`module github.com/odvcencio/orchard
go 1.25
require github.com/odvcencio/graft v0.2.6
`), 0o644)

	gtsDir := filepath.Join(root, "gotreesitter")
	os.MkdirAll(gtsDir, 0o755)
	os.WriteFile(filepath.Join(gtsDir, "go.mod"), []byte(`module github.com/odvcencio/gotreesitter
go 1.24
`), 0o644)

	workspaces := map[string]string{
		"graft":         graftDir,
		"orchard":       orchardDir,
		"gotreesitter":  gtsDir,
	}

	graph, err := BuildWorkspaceGraph(workspaces)
	if err != nil {
		t.Fatalf("BuildWorkspaceGraph: %v", err)
	}

	// orchard depends on graft
	deps := graph.DependentsOf("graft")
	found := false
	for _, d := range deps {
		if d == "orchard" {
			found = true
		}
	}
	if !found {
		t.Error("expected orchard to depend on graft")
	}

	// graft depends on gotreesitter
	deps2 := graph.DependentsOf("gotreesitter")
	found2 := false
	for _, d := range deps2 {
		if d == "graft" {
			found2 = true
		}
	}
	if !found2 {
		t.Error("expected graft to depend on gotreesitter")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /home/draco/work/graft && go test ./pkg/coord/ -run "TestParseGoMod|TestBuildWorkspace" -v`
Expected: FAIL

- [ ] **Step 3: Implement workspace.go**

Implement `ParseGoModDeps`, `BuildWorkspaceGraph`, and the `WorkspaceGraph` type with `DependentsOf(workspace)` method. Parse go.mod files to extract module path, require directives, and replace directives. Build an adjacency map of workspace → workspace edges by matching require module paths to workspace module paths.

- [ ] **Step 4: Run tests**

Run: `cd /home/draco/work/graft && go test ./pkg/coord/ -run "TestParseGoMod|TestBuildWorkspace" -v`
Expected: PASS

- [ ] **Step 5: Commit**

Run: `cd /home/draco/work/graft && buckley commit --yes --minimal-output`

---

### Task 7: Export index

**Files:**
- Create: `pkg/coord/export.go`
- Create: `pkg/coord/export_test.go`

- [ ] **Step 1: Write failing test for export index**

Test should: create a repo with a committed Go file containing exported functions, build the export index, verify the exported entities are indexed with correct keys and signatures.

Key types:
```go
type ExportIndex struct {
	Packages map[string]map[string]ExportedEntity `json:"packages"`
}

type ExportedEntity struct {
	Key       string `json:"key"`
	Signature string `json:"signature"`
	File      string `json:"file"`
	Hash      string `json:"hash"`
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/draco/work/graft && go test ./pkg/coord/ -run TestBuildExport -v`

- [ ] **Step 3: Implement export.go**

Build export index by: listing tree entries from HEAD commit, extracting entity lists for each file, filtering to exported entities (capitalized names in Go), storing as a JSON blob at `refs/coord/meta/exports`.

- [ ] **Step 4: Run tests**

Run: `cd /home/draco/work/graft && go test ./pkg/coord/ -run TestBuildExport -v`
Expected: PASS

- [ ] **Step 5: Commit**

Run: `cd /home/draco/work/graft && buckley commit --yes --minimal-output`

---

### Task 8: Xref index and impact analysis

**Files:**
- Create: `pkg/coord/xref.go`
- Create: `pkg/coord/xref_test.go`
- Create: `pkg/coord/impact.go`
- Create: `pkg/coord/impact_test.go`

- [ ] **Step 1: Write failing test for xref index**

Test should: create two workspace directories with Go files where workspace B imports and calls a function from workspace A. Build xref index for workspace B. Verify reverse lookup finds the caller.

Key types:
```go
type XrefIndex struct {
	// Map from qualified name (module/pkg.Function) → list of call sites in this repo
	Refs map[string][]XrefCallSite `json:"refs"`
}

type XrefCallSite struct {
	File   string `json:"file"`
	Entity string `json:"entity"`
	Line   int    `json:"line"`
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/draco/work/graft && go test ./pkg/coord/ -run TestBuildXref -v`

- [ ] **Step 3: Implement xref.go**

Build xref index by: scanning Go source files for import declarations, mapping imports to workspace module paths, scanning entity bodies for function calls matching imported symbols, storing as JSON blob at `refs/coord/meta/xrefs`.

- [ ] **Step 4: Write failing test for impact analysis**

Test should: set up workspace graph + export index + xref index + active claims, then call `AnalyzeImpact` for a changed entity. Verify it returns the correct affected callers and affected agents.

- [ ] **Step 5: Implement impact.go**

```go
// AnalyzeImpact computes cross-repo impact for a set of entity changes.
func (c *Coordinator) AnalyzeImpact(changes []EntityChange, workspaces map[string]string) (*ImpactReport, error)
```

Combines: workspace graph (which repos are downstream), export index (did a public entity change?), xref index from downstream repos (who calls it?), claim registry (is anyone editing those callers?).

- [ ] **Step 6: Run all tests**

Run: `cd /home/draco/work/graft && go test ./pkg/coord/ -v`
Expected: PASS

- [ ] **Step 7: Commit**

Run: `cd /home/draco/work/graft && buckley commit --yes --minimal-output`

---

## Chunk 3: CLI Surface

### Task 9: graft workon command

**Files:**
- Create: `cmd/graft/cmd_workon.go`
- Modify: `cmd/graft/main.go` (register command)

- [ ] **Step 1: Implement cmd_workon.go**

```go
func newWorkonCmd() *cobra.Command {
	var (
		name         string
		done         bool
		autoDiscover bool
		notifyMode   string
		conflictMode string
		watchOnly    bool
		scope        string
		jsonFlag     bool
	)

	cmd := &cobra.Command{
		Use:   "workon",
		Short: "Join or leave a coordination session",
		RunE: func(cmd *cobra.Command, args []string) error {
			// ... implementation
		},
	}

	cmd.Flags().StringVar(&name, "as", "", "agent name")
	cmd.Flags().BoolVar(&done, "done", false, "leave coordination session")
	cmd.Flags().BoolVar(&autoDiscover, "auto-discover", false, "discover workspaces from go.mod")
	cmd.Flags().StringVar(&notifyMode, "notify", "all", "notification filter: all, breaking")
	cmd.Flags().StringVar(&conflictMode, "conflict-mode", "", "override conflict mode")
	cmd.Flags().BoolVar(&watchOnly, "watch-only", false, "observe only, don't claim")
	cmd.Flags().StringVar(&scope, "scope", "", "limit coordination to package pattern")
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "JSON output")

	return cmd
}
```

When `--as` is provided: open repo, create coordinator, register agent, fetch peer state, print status.
When `--done` is provided: deregister agent, release all claims, print confirmation.
When `--auto-discover` is provided: scan go.mod for replace directives, suggest workspaces.

- [ ] **Step 2: Register in main.go**

Add `root.AddCommand(newWorkonCmd())` to the command registration block.

- [ ] **Step 3: Manual smoke test**

Run: `cd /home/draco/work/graft && go run ./cmd/graft workon --as "test-agent" --json`
Expected: JSON output with agent ID and workspace info

Run: `cd /home/draco/work/graft && go run ./cmd/graft workon --done --json`
Expected: JSON confirmation of agent deregistration

- [ ] **Step 4: Commit**

Run: `cd /home/draco/work/graft && buckley commit --yes --minimal-output`

---

### Task 10: graft coord command tree

**Files:**
- Create: `cmd/graft/cmd_coord.go`
- Modify: `cmd/graft/main.go` (register command)

- [ ] **Step 1: Implement coord command with subcommands**

Create the `coord` parent command and subcommands:

```
graft coord              — dashboard (default when no subcommand)
graft coord agents       — list agents
graft coord claims       — list claims
graft coord feed         — show feed
graft coord impact       — run impact analysis
graft coord check        — quick conflict check (hook-optimized)
graft coord diff <agent> — show another agent's changes
graft coord xrefs <key>  — reverse call lookup
graft coord graph        — workspace dependency graph
graft coord watch <key>  — add a watch
graft coord unwatch <key> — remove a watch
graft coord resolve <hash> — release/transfer claim
```

Each subcommand: open repo, create coordinator, call the appropriate `pkg/coord` method, format output (human or `--json`).

The dashboard (`graft coord` with no subcommand) aggregates: agent count, claim count, conflict count, unread feed count, then prints a summary table.

- [ ] **Step 2: Register in main.go**

Add `root.AddCommand(newCoordCmd())` to the command registration block.

- [ ] **Step 3: Manual smoke test**

Run: `cd /home/draco/work/graft && go run ./cmd/graft coord --json`
Expected: JSON dashboard output

- [ ] **Step 4: Commit**

Run: `cd /home/draco/work/graft && buckley commit --yes --minimal-output`

---

### Task 11: graft workspace command

**Files:**
- Create: `cmd/graft/cmd_workspace.go`
- Modify: `cmd/graft/main.go` (register command)

- [ ] **Step 1: Implement workspace command**

```
graft workspace add <name> <path-or-url>   — register a workspace
graft workspace list                        — list known workspaces
graft workspace remove <name>               — unregister a workspace
```

Each subcommand reads/writes `~/.graftconfig` via `pkg/userconfig`.

- [ ] **Step 2: Register in main.go**

- [ ] **Step 3: Manual smoke test**

Run: `cd /home/draco/work/graft && go run ./cmd/graft workspace list --json`

- [ ] **Step 4: Commit**

Run: `cd /home/draco/work/graft && buckley commit --yes --minimal-output`

---

### Task 12: Augment graft add and graft status with coordination

**Files:**
- Modify: `pkg/repo/staging.go:298` (prepareAddEntry — hook claim acquisition)
- Modify: `cmd/graft/cmd_status.go` (add coord context to output)
- Modify: `cmd/graft/cmd_add.go` (add coord claim output)

- [ ] **Step 1: Add CoordHook interface to repo**

Add an optional hook that the CLI layer can set before calling `repo.Add`:

```go
// In pkg/repo/staging.go or a new file pkg/repo/hooks.go
type AddEntityHook func(path string, changedEntities []string) error

// On Repo struct:
AddHook AddEntityHook
```

In `prepareAddEntry`, after entity extraction and diffing against HEAD, call the hook with the changed entity identity keys. The CLI layer sets this hook to call `coord.AcquireClaim` for each changed entity.

- [ ] **Step 2: Augment status command**

When a `.graft/coord/agents/` directory exists (coordination is active), append coordination summary to status output: claimed entities, conflicts, unread feed count.

- [ ] **Step 3: Test the integration**

Run: `cd /home/draco/work/graft && go test ./pkg/repo/ -run TestAdd -v` (ensure existing tests still pass)
Run: `cd /home/draco/work/graft && go test ./pkg/coord/ -v` (ensure coord tests still pass)

- [ ] **Step 4: Commit**

Run: `cd /home/draco/work/graft && buckley commit --yes --minimal-output`

---

## Chunk 4: MCP Server and Integration

### Task 13: MCP server — graft mcp serve

**Files:**
- Create: `cmd/graft/cmd_mcp.go`
- Modify: `cmd/graft/main.go`

- [ ] **Step 1: Implement MCP JSON-RPC server**

Follow the same pattern as gts-suite's MCP server (`/home/draco/work/gts-suite/internal/mcp/server.go`): Content-Length framed JSON-RPC 2.0 over stdio.

Implement the MCP protocol:
- `initialize` — return server info and capabilities (tools list)
- `tools/list` — return tool schemas
- `tools/call` — dispatch to tool handlers

Register tools:
- `graft_workon` — join coordination
- `graft_workon_done` — leave coordination
- `graft_coord_status` — dashboard
- `graft_coord_agents` — list agents
- `graft_coord_claims` — list claims
- `graft_coord_feed` — read feed
- `graft_coord_impact` — impact analysis
- `graft_coord_check` — quick conflict check
- `graft_coord_diff` — agent diff
- `graft_coord_xrefs` — reverse call lookup
- `graft_coord_graph` — workspace graph
- `graft_coord_watch` — add watch
- `graft_coord_unwatch` — remove watch
- `graft_coord_resolve` — claim resolution

Each tool returns JSON with `_meta` containing `tool`, `ok`, `duration_ms`, and `coord` summary.

- [ ] **Step 2: Add --with-codeintel flag**

When `--with-codeintel` is set, attempt to spawn `gts mcp` as a subprocess. Proxy its tools through the graft MCP server alongside native tools. If `gts` is not on PATH, log a warning and continue without code intelligence tools.

- [ ] **Step 3: Register in main.go**

Add `root.AddCommand(newMCPCmd())` to the command registration block.

- [ ] **Step 4: Manual smoke test**

Run: `echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"capabilities":{}}}' | cd /home/draco/work/graft && go run ./cmd/graft mcp serve`
Expected: JSON-RPC response with server capabilities

- [ ] **Step 5: Commit**

Run: `cd /home/draco/work/graft && buckley commit --yes --minimal-output`

---

### Task 14: Claude Code hook integration

**Files:**
- No code changes — configuration only

- [ ] **Step 1: Create hook configuration template**

Document the recommended `.claude/settings.local.json` for repos that want auto-coordination. This is not code — it's a configuration that users drop into their repos.

Per-repo config:
```json
{
  "hooks": {
    "SessionStart": [{"command": "graft workon --as claude-$CLAUDE_SESSION_ID --notify breaking --json", "timeout": 5000}],
    "PostToolUse": [{"matcher": "Edit|Write", "command": "graft coord check --json --quiet", "timeout": 3000}],
    "Stop": [{"command": "graft workon --done --json", "timeout": 3000}]
  },
  "mcpServers": {
    "graft": {"command": "graft", "args": ["mcp", "serve", "--with-codeintel"]}
  }
}
```

- [ ] **Step 2: Test end-to-end with two agents**

Manual test:
1. Open terminal 1 in graft repo: `graft workon --as "agent-1"`
2. Open terminal 2 in graft repo: `graft workon --as "agent-2"`
3. In terminal 1: edit a file, `graft add` it, verify claim acquired
4. In terminal 2: edit a file touching the same entity, `graft add` it, verify conflict warning
5. In terminal 1: `graft coord` — verify dashboard shows both agents
6. In terminal 1: `graft workon --done`
7. In terminal 2: `graft workon --done`

- [ ] **Step 3: Commit any final adjustments**

Run: `cd /home/draco/work/graft && buckley commit --yes --minimal-output`

---

## Chunk 5: Missing Spec Coverage

### Task 15: Ref namespace reservation and fetch --coord

**Files:**
- Modify: `pkg/repo/branch.go` (reject `coord/` prefix in branch creation)
- Modify: `pkg/repo/tag.go` (reject `coord/` prefix in tag creation)
- Modify: `pkg/remote/client.go` (add coord ref fetch support)
- Modify: `cmd/graft/cmd_fetch.go` (add `--coord` flag)

- [ ] **Step 1: Add ref namespace guard**

In `pkg/repo/branch.go` `CreateBranch` and `pkg/repo/tag.go` `CreateTag`, reject names starting with `coord/`:

```go
if strings.HasPrefix(name, "coord/") {
	return fmt.Errorf("refs/coord/ namespace is reserved for coordination")
}
```

- [ ] **Step 2: Write test for namespace guard**

```go
func TestCreateBranch_RejectsCoordNamespace(t *testing.T) {
	r, err := Init(t.TempDir())
	// ... setup
	err = r.CreateBranch("coord/test", someHash)
	if err == nil {
		t.Fatal("expected rejection of coord/ namespace")
	}
}
```

- [ ] **Step 3: Add --coord to fetch command**

Extend `cmd/graft/cmd_fetch.go` to accept `--coord` flag. When set, also fetch `refs/coord/` refs from the remote (agents, claims, feed, meta).

- [ ] **Step 4: Run tests**

Run: `cd /home/draco/work/graft && go test ./pkg/repo/ -run TestCreateBranch_Rejects -v`
Expected: PASS

- [ ] **Step 5: Commit**

Run: `cd /home/draco/work/graft && buckley commit --yes --minimal-output`

---

### Task 16: Augment graft commit with coordination

**Files:**
- Modify: `cmd/graft/cmd_commit.go`
- Modify: `pkg/coord/coord.go` (add OnCommit method)

- [ ] **Step 1: Implement OnCommit coordination hook**

```go
// In pkg/coord/coord.go
// OnCommit runs after a successful graft commit:
// 1. Diffs committed entities against parent
// 2. Builds impact report via AnalyzeImpact
// 3. Appends feed event
// 4. Releases editing claims on committed entities
func (c *Coordinator) OnCommit(commitHash object.Hash, workspaces map[string]string) error
```

- [ ] **Step 2: Write test for OnCommit**

Test should: register agent, acquire claims, call OnCommit with a mock commit hash, verify claims released and feed event appended with impact data.

- [ ] **Step 3: Hook into cmd_commit.go**

After the commit succeeds, if coordination is active (agent ref exists), call `coord.OnCommit()`.

- [ ] **Step 4: Run tests**

Run: `cd /home/draco/work/graft && go test ./pkg/coord/ -run TestOnCommit -v`
Expected: PASS

- [ ] **Step 5: Commit**

Run: `cd /home/draco/work/graft && buckley commit --yes --minimal-output`

---

### Task 17: Concurrent race tests

**Files:**
- Modify: `pkg/coord/claim_test.go`
- Modify: `pkg/coord/feed_test.go`

- [ ] **Step 1: Add concurrent claim race test**

```go
func TestAcquireClaimConcurrentRace(t *testing.T) {
	c := newTestCoordinator(t)
	const numAgents = 10
	entityKey := "decl:function_definition::RaceTarget:func RaceTarget():0"

	var wg sync.WaitGroup
	wins := make(chan string, numAgents)

	for i := 0; i < numAgents; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			name := fmt.Sprintf("agent-%d", n)
			id, _ := c.RegisterAgent(AgentInfo{Name: name, Workspace: "graft", Host: "test"})
			err := c.AcquireClaim(id, ClaimRequest{EntityKey: entityKey, File: "race.go", Mode: ClaimEditing})
			if err == nil {
				wins <- name
			}
		}(i)
	}

	wg.Wait()
	close(wins)

	winners := 0
	for range wins {
		winners++
	}
	// Exactly one agent should win the claim
	if winners != 1 {
		t.Fatalf("expected exactly 1 winner, got %d", winners)
	}
}
```

- [ ] **Step 2: Add concurrent feed append test**

```go
func TestAppendFeedConcurrentRetry(t *testing.T) {
	c := newTestCoordinator(t)
	const numAppends = 20

	var wg sync.WaitGroup
	for i := 0; i < numAppends; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			c.AppendFeed(FeedEvent{
				Event:     "entity_changed",
				AgentID:   fmt.Sprintf("agent-%d", n),
				AgentName: fmt.Sprintf("agent-%d", n),
				Entities:  []EntityChange{{Key: fmt.Sprintf("entity-%d", n), File: "test.go", Change: "body_changed"}},
			})
		}(i)
	}
	wg.Wait()

	events, err := c.WalkFeed("", 100)
	if err != nil {
		t.Fatalf("WalkFeed: %v", err)
	}
	// All events should be present (some may be in overflow)
	if len(events) < numAppends/2 {
		t.Fatalf("expected most events to land, got %d/%d", len(events), numAppends)
	}
}
```

- [ ] **Step 3: Run race tests with -race flag**

Run: `cd /home/draco/work/graft && go test ./pkg/coord/ -run "TestAcquireClaimConcurrent|TestAppendFeedConcurrent" -race -v`
Expected: PASS, no data races

- [ ] **Step 4: Commit**

Run: `cd /home/draco/work/graft && buckley commit --yes --minimal-output`

---

### Task 18: Transport abstraction (local + remote peers)

**Files:**
- Create: `pkg/coord/transport.go`
- Create: `pkg/coord/transport_test.go`

- [ ] **Step 1: Define PeerTransport interface**

```go
// PeerTransport abstracts reading coordination state from a peer workspace.
type PeerTransport interface {
	// ListAgents returns agents registered in the peer workspace.
	ListAgents() ([]AgentInfo, error)
	// ListClaims returns claims in the peer workspace.
	ListClaims() ([]ClaimInfo, error)
	// ReadExportIndex returns the peer's export index.
	ReadExportIndex() (*ExportIndex, error)
	// ReadXrefIndex returns the peer's xref index.
	ReadXrefIndex() (*XrefIndex, error)
}
```

- [ ] **Step 2: Implement LocalPeerTransport**

Reads directly from another workspace's `.graft/` directory on the local filesystem. Uses `repo.Open(path)` and the same Coordinator read methods.

- [ ] **Step 3: Implement RemotePeerTransport**

Uses `pkg/remote.Client` to fetch `refs/coord/` from a remote. Reads agent/claim/index blobs via `BatchObjects`.

- [ ] **Step 4: Write tests for local transport**

Test with two temp repos — write coord state to one, read via LocalPeerTransport from the other.

- [ ] **Step 5: Run tests**

Run: `cd /home/draco/work/graft && go test ./pkg/coord/ -run TestLocalPeer -v`
Expected: PASS

- [ ] **Step 6: Commit**

Run: `cd /home/draco/work/graft && buckley commit --yes --minimal-output`

---

### Task 19: Signal handling and per-repo config

**Files:**
- Modify: `cmd/graft/cmd_workon.go` (add SIGTERM/SIGINT handler)
- Create: `pkg/coord/repoconfig.go` (per-repo config read/write)
- Create: `pkg/coord/repoconfig_test.go`

- [ ] **Step 1: Add signal handler to workon**

In `cmd_workon.go`, after registering the agent, set up a signal handler:

```go
sigCh := make(chan os.Signal, 1)
signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
go func() {
	<-sigCh
	coordinator.DeregisterAgent(agentID)
	os.Exit(0)
}()
```

- [ ] **Step 2: Implement per-repo config**

```go
// pkg/coord/repoconfig.go
type RepoCoordConfig struct {
	ConflictMode      string   `json:"conflict_mode"`
	ProtectedEntities []string `json:"protected_entities,omitempty"`
	NotifyOn          []string `json:"notify_on,omitempty"`
	IgnorePatterns    []string `json:"ignore_patterns,omitempty"`
}

func (c *Coordinator) ReadRepoConfig() (*RepoCoordConfig, error)
func (c *Coordinator) WriteRepoConfig(cfg *RepoCoordConfig) error
func (c *Coordinator) IsEntityProtected(entityKey string) bool
```

Store at `refs/coord/meta/config` as a JSON blob. `IsEntityProtected` matches entity keys against `protected_entities` patterns using `filepath.Match` semantics with `*` not crossing colons.

- [ ] **Step 3: Write tests**

Test protected entity matching and hard block behavior:

```go
func TestIsEntityProtected(t *testing.T) {
	c := newTestCoordinator(t)
	cfg := &RepoCoordConfig{
		ProtectedEntities: []string{"decl:function_definition::MergeFiles:*"},
	}
	c.WriteRepoConfig(cfg)

	if !c.IsEntityProtected("decl:function_definition::MergeFiles:func MergeFiles():0") {
		t.Error("expected MergeFiles to be protected")
	}
	if c.IsEntityProtected("decl:function_definition::DiffFiles:func DiffFiles():0") {
		t.Error("expected DiffFiles to NOT be protected")
	}
}

func TestAcquireClaim_ProtectedEntityHardBlock(t *testing.T) {
	c := newTestCoordinator(t)
	cfg := &RepoCoordConfig{
		ProtectedEntities: []string{"decl:function_definition::MergeFiles:*"},
	}
	c.WriteRepoConfig(cfg)

	id, _ := c.RegisterAgent(AgentInfo{Name: "agent", Workspace: "graft", Host: "test"})
	err := c.AcquireClaim(id, ClaimRequest{
		EntityKey: "decl:function_definition::MergeFiles:func MergeFiles():0",
		File:      "merge.go",
		Mode:      ClaimEditing,
	})
	if err == nil {
		t.Fatal("expected hard block on protected entity")
	}
}
```

- [ ] **Step 4: Integrate into AcquireClaim**

In `claim.go`, before returning a conflict error, check `IsEntityProtected`. If protected, return a hard-block error regardless of `conflict_mode`.

- [ ] **Step 5: Run tests**

Run: `cd /home/draco/work/graft && go test ./pkg/coord/ -run "TestRepoConfig|TestIsEntity|TestAcquireClaim_Protected" -v`
Expected: PASS

- [ ] **Step 6: Commit**

Run: `cd /home/draco/work/graft && buckley commit --yes --minimal-output`

---

## Chunk 6: Feed Lifecycle and Discovery

### Task 20: Feed cursor management

**Files:**
- Modify: `pkg/coord/feed.go`
- Modify: `pkg/coord/feed_test.go`

- [ ] **Step 1: Write failing test for cursor save/load**

```go
func TestCursorSaveLoad(t *testing.T) {
	c := newTestCoordinator(t)
	id, _ := c.RegisterAgent(AgentInfo{Name: "cursor-agent", Workspace: "graft", Host: "test"})

	// Append some events
	for i := 0; i < 3; i++ {
		c.AppendFeed(FeedEvent{
			Event: "entity_changed", AgentID: id, AgentName: "cursor-agent",
			Entities: []EntityChange{{Key: fmt.Sprintf("e%d", i), File: "f.go", Change: "body_changed"}},
		})
	}

	events, _ := c.WalkFeed("", 10)
	cursorHash := events[0].FeedHash // newest event

	if err := c.SaveCursor(id, cursorHash); err != nil {
		t.Fatalf("SaveCursor: %v", err)
	}

	loaded, err := c.LoadCursor(id)
	if err != nil {
		t.Fatalf("LoadCursor: %v", err)
	}
	if loaded != cursorHash {
		t.Errorf("cursor = %q, want %q", loaded, cursorHash)
	}
}

func TestWalkFeedSinceCursor(t *testing.T) {
	c := newTestCoordinator(t)
	id, _ := c.RegisterAgent(AgentInfo{Name: "cursor-agent", Workspace: "graft", Host: "test"})

	// Append 3 events, save cursor after 2nd
	for i := 0; i < 3; i++ {
		c.AppendFeed(FeedEvent{
			Event: "entity_changed", AgentID: id, AgentName: "cursor-agent",
			Entities: []EntityChange{{Key: fmt.Sprintf("e%d", i), File: "f.go", Change: "body_changed"}},
		})
	}

	events, _ := c.WalkFeed("", 10)
	// Save cursor at the 2nd event (index 1, middle)
	c.SaveCursor(id, events[1].FeedHash)

	// WalkFeedSinceCursor should only return event 0 (newest, after cursor)
	recent, err := c.WalkFeedSinceCursor(id, 10)
	if err != nil {
		t.Fatalf("WalkFeedSinceCursor: %v", err)
	}
	if len(recent) != 1 {
		t.Fatalf("expected 1 event since cursor, got %d", len(recent))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /home/draco/work/graft && go test ./pkg/coord/ -run TestCursor -v`
Expected: FAIL

- [ ] **Step 3: Implement cursor management**

```go
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
```

- [ ] **Step 4: Run tests**

Run: `cd /home/draco/work/graft && go test ./pkg/coord/ -run TestCursor -v`
Expected: PASS

- [ ] **Step 5: Commit**

Run: `cd /home/draco/work/graft && buckley commit --yes --minimal-output`

---

### Task 21: Feed pruning

**Files:**
- Modify: `pkg/coord/feed.go`
- Modify: `pkg/coord/feed_test.go`

- [ ] **Step 1: Write failing test for feed pruning**

```go
func TestPruneFeed(t *testing.T) {
	c := newTestCoordinator(t)
	id, _ := c.RegisterAgent(AgentInfo{Name: "pruner", Workspace: "graft", Host: "test"})

	// Append 5 events
	for i := 0; i < 5; i++ {
		c.AppendFeed(FeedEvent{
			Event: "entity_changed", AgentID: id, AgentName: "pruner",
			Entities: []EntityChange{{Key: fmt.Sprintf("e%d", i), File: "f.go", Change: "body_changed"}},
		})
	}

	// Prune keeping only the 2 newest
	pruned, err := c.PruneFeed(2)
	if err != nil {
		t.Fatalf("PruneFeed: %v", err)
	}
	if pruned != 3 {
		t.Errorf("expected 3 pruned, got %d", pruned)
	}

	// Walk should return only 2
	events, _ := c.WalkFeed("", 10)
	if len(events) != 2 {
		t.Fatalf("expected 2 events after prune, got %d", len(events))
	}
}

func TestPruneFeed_ResetsStaleCursors(t *testing.T) {
	c := newTestCoordinator(t)
	id, _ := c.RegisterAgent(AgentInfo{Name: "stale-cursor", Workspace: "graft", Host: "test"})

	// Append 5 events, save cursor at oldest
	for i := 0; i < 5; i++ {
		c.AppendFeed(FeedEvent{
			Event: "entity_changed", AgentID: id, AgentName: "stale-cursor",
			Entities: []EntityChange{{Key: fmt.Sprintf("e%d", i), File: "f.go", Change: "body_changed"}},
		})
	}
	all, _ := c.WalkFeed("", 10)
	c.SaveCursor(id, all[4].FeedHash) // oldest

	// Prune keeps 2 — cursor now points to pruned range
	c.PruneFeed(2)

	// WalkFeedSinceCursor should gracefully return all available events
	events, err := c.WalkFeedSinceCursor(id, 10)
	if err != nil {
		t.Fatalf("WalkFeedSinceCursor after prune: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events (full walk after stale cursor), got %d", len(events))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /home/draco/work/graft && go test ./pkg/coord/ -run TestPruneFeed -v`

- [ ] **Step 3: Implement PruneFeed**

```go
// PruneFeed keeps the newest `keep` entries and truncates the rest.
// The oldest retained entry is rewritten with an empty parent to break the chain.
// Returns the number of entries pruned.
func (c *Coordinator) PruneFeed(keep int) (int, error) {
	events, err := c.WalkFeed("", keep+1000) // walk full chain
	if err != nil {
		return 0, err
	}
	if len(events) <= keep {
		return 0, nil // nothing to prune
	}

	// The entry at index keep-1 becomes the new tail — rewrite it with empty parent
	tailEvent := events[keep-1]
	headHash, _ := c.Repo.ResolveRef(feedHeadRef)

	// Walk blobs to find the tail's blob hash and rewrite it
	currentHash := headHash
	for i := 0; i < keep-1; i++ {
		blob, _ := c.Repo.Store.ReadBlob(currentHash)
		var entry FeedEntry
		json.Unmarshal(blob.Data, &entry)
		currentHash = object.Hash(entry.Parent)
	}

	// Read tail entry, clear parent, write new blob
	tailBlob, _ := c.Repo.Store.ReadBlob(currentHash)
	var tailEntry FeedEntry
	json.Unmarshal(tailBlob.Data, &tailEntry)
	tailEntry.Parent = "" // break the chain

	newTailData, _ := json.Marshal(tailEntry)
	newTailHash, _ := c.Repo.Store.WriteBlob(&object.Blob{Data: newTailData})

	// Now rebuild the chain from head to new tail
	// Walk again and rewrite parent pointers
	_ = tailEvent  // suppress unused
	_ = newTailHash // used below

	// Simpler approach: rebuild the kept entries as a new chain
	type rawEntry struct {
		hash  object.Hash
		entry FeedEntry
	}
	var kept []rawEntry
	cur := headHash
	for i := 0; i < keep; i++ {
		blob, _ := c.Repo.Store.ReadBlob(cur)
		var e FeedEntry
		json.Unmarshal(blob.Data, &e)
		kept = append(kept, rawEntry{hash: cur, entry: e})
		cur = object.Hash(e.Parent)
	}

	// Rewrite from tail to head with corrected parent pointers
	var prevHash string
	for i := len(kept) - 1; i >= 0; i-- {
		kept[i].entry.Parent = prevHash
		data, _ := json.Marshal(kept[i].entry)
		h, _ := c.Repo.Store.WriteBlob(&object.Blob{Data: data})
		prevHash = string(h)
	}

	// Update head ref to new chain head
	c.Repo.UpdateRefCAS(feedHeadRef, object.Hash(prevHash), headHash)

	return len(events) - keep, nil
}
```

- [ ] **Step 4: Run tests**

Run: `cd /home/draco/work/graft && go test ./pkg/coord/ -run TestPruneFeed -v`
Expected: PASS

- [ ] **Step 5: Commit**

Run: `cd /home/draco/work/graft && buckley commit --yes --minimal-output`

---

### Task 22: Feed overflow merge

**Files:**
- Modify: `pkg/coord/feed.go`
- Modify: `pkg/coord/feed_test.go`

- [ ] **Step 1: Write failing test for overflow recovery**

```go
func TestOverflowMerge(t *testing.T) {
	c := newTestCoordinator(t)

	// Manually write overflow files
	overflowDir := filepath.Join(c.Repo.GraftDir, "coord", "feed-overflow")
	os.MkdirAll(overflowDir, 0o755)
	for i := 0; i < 3; i++ {
		evt := FeedEvent{
			Event: "entity_changed", AgentID: "overflow-agent", AgentName: "overflow",
			Entities: []EntityChange{{Key: fmt.Sprintf("overflow-%d", i), File: "o.go", Change: "body_changed"}},
		}
		data, _ := json.Marshal(evt)
		os.WriteFile(filepath.Join(overflowDir, fmt.Sprintf("%d-overflow.json", i)), data, 0o644)
	}

	// Merge overflow into feed
	merged, err := c.MergeOverflow()
	if err != nil {
		t.Fatalf("MergeOverflow: %v", err)
	}
	if merged != 3 {
		t.Errorf("expected 3 merged, got %d", merged)
	}

	// Feed should now have 3 events
	events, _ := c.WalkFeed("", 10)
	if len(events) != 3 {
		t.Fatalf("expected 3 events after merge, got %d", len(events))
	}

	// Overflow directory should be empty
	entries, _ := os.ReadDir(overflowDir)
	if len(entries) != 0 {
		t.Errorf("expected overflow dir to be empty, got %d files", len(entries))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/draco/work/graft && go test ./pkg/coord/ -run TestOverflowMerge -v`

- [ ] **Step 3: Implement MergeOverflow**

```go
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
```

- [ ] **Step 4: Run tests**

Run: `cd /home/draco/work/graft && go test ./pkg/coord/ -run TestOverflowMerge -v`
Expected: PASS

- [ ] **Step 5: Hook into AppendFeed**

At the start of `AppendFeed`, call `c.MergeOverflow()` to drain any pending overflow before appending new events. This ensures overflow events are recovered promptly.

- [ ] **Step 6: Commit**

Run: `cd /home/draco/work/graft && buckley commit --yes --minimal-output`

---

### Task 23: Workspace auto-discovery

**Files:**
- Modify: `pkg/coord/workspace.go`
- Modify: `pkg/coord/workspace_test.go`

- [ ] **Step 1: Write failing test for auto-discovery**

```go
func TestAutoDiscoverWorkspaces(t *testing.T) {
	root := t.TempDir()

	// Create a repo with go.mod that has replace directives
	repoDir := filepath.Join(root, "myrepo")
	os.MkdirAll(repoDir, 0o755)

	siblingDir := filepath.Join(root, "sibling")
	os.MkdirAll(siblingDir, 0o755)
	os.WriteFile(filepath.Join(siblingDir, "go.mod"), []byte("module github.com/example/sibling\ngo 1.25\n"), 0o644)

	depDir := filepath.Join(root, "dep")
	os.MkdirAll(depDir, 0o755)
	os.WriteFile(filepath.Join(depDir, "go.mod"), []byte("module github.com/example/dep\ngo 1.25\n"), 0o644)

	gomod := fmt.Sprintf(`module github.com/example/myrepo
go 1.25
require github.com/example/dep v1.0.0
replace github.com/example/dep => %s
`, depDir)
	os.WriteFile(filepath.Join(repoDir, "go.mod"), []byte(gomod), 0o644)

	discovered, err := AutoDiscoverWorkspaces(repoDir)
	if err != nil {
		t.Fatalf("AutoDiscoverWorkspaces: %v", err)
	}

	// Should find dep (from replace) and sibling (from directory scan)
	if _, ok := discovered["dep"]; !ok {
		t.Error("expected dep from replace directive")
	}
	if _, ok := discovered["sibling"]; !ok {
		t.Error("expected sibling from directory scan")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/draco/work/graft && go test ./pkg/coord/ -run TestAutoDiscover -v`

- [ ] **Step 3: Implement AutoDiscoverWorkspaces**

```go
// AutoDiscoverWorkspaces finds related workspaces by:
// 1. Parsing go.mod replace directives (local paths)
// 2. Scanning sibling directories for go.mod files
// Returns a map of workspace name → absolute path.
func AutoDiscoverWorkspaces(repoDir string) (map[string]string, error) {
	discovered := make(map[string]string)

	// 1. Parse replace directives
	gomodPath := filepath.Join(repoDir, "go.mod")
	if deps, err := ParseGoModDeps(gomodPath); err == nil {
		for _, localPath := range deps.Replaces {
			if filepath.IsAbs(localPath) {
				name := filepath.Base(localPath)
				discovered[name] = localPath
			}
		}
	}

	// 2. Scan sibling directories
	parent := filepath.Dir(repoDir)
	entries, err := os.ReadDir(parent)
	if err != nil {
		return discovered, nil // non-fatal
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		siblingPath := filepath.Join(parent, entry.Name())
		if siblingPath == repoDir {
			continue // skip self
		}
		// Check for go.mod
		if _, err := os.Stat(filepath.Join(siblingPath, "go.mod")); err == nil {
			name := entry.Name()
			if _, exists := discovered[name]; !exists {
				discovered[name] = siblingPath
			}
		}
	}

	return discovered, nil
}
```

- [ ] **Step 4: Run tests**

Run: `cd /home/draco/work/graft && go test ./pkg/coord/ -run TestAutoDiscover -v`
Expected: PASS

- [ ] **Step 5: Commit**

Run: `cd /home/draco/work/graft && buckley commit --yes --minimal-output`

---

## Chunk 7: Augmented Commands

### Task 24: auto_push_coord

**Files:**
- Modify: `pkg/coord/coord.go`
- Modify: `pkg/coord/feed.go`
- Modify: `cmd/graft/cmd_workon.go`

- [ ] **Step 1: Write failing test for auto-push check**

```go
func TestShouldAutoPush(t *testing.T) {
	c := newTestCoordinator(t)
	c.Config.AutoPushCoord = false

	if c.ShouldAutoPush() {
		t.Error("expected false when AutoPushCoord is disabled")
	}

	c.Config.AutoPushCoord = true
	if !c.ShouldAutoPush() {
		t.Error("expected true when AutoPushCoord is enabled")
	}
}
```

- [ ] **Step 2: Add AutoPushCoord to CoordinatorConfig**

```go
// In coord.go, add to CoordinatorConfig:
AutoPushCoord bool
```

- [ ] **Step 3: Implement ShouldAutoPush and PushCoordRefs**

```go
func (c *Coordinator) ShouldAutoPush() bool {
	return c.Config.AutoPushCoord
}

// PushCoordRefs pushes refs/coord/ to all configured remotes.
// Called after AppendFeed and OnCommit when auto_push_coord is enabled.
func (c *Coordinator) PushCoordRefs() error {
	remotes, err := c.Repo.ListRemotes()
	if err != nil || len(remotes) == 0 {
		return nil // no remotes configured
	}
	for _, remoteName := range remotes {
		client, err := remote.NewClient(c.Repo, remoteName)
		if err != nil {
			continue
		}
		coordRefs, _ := c.Repo.ListRefs("coord")
		for refName, hash := range coordRefs {
			_ = client.UpdateRefs([]remote.RefUpdate{
				{Name: "refs/" + refName, New: hash},
			})
		}
	}
	return nil
}
```

- [ ] **Step 4: Hook into AppendFeed and OnCommit**

After successful feed append or commit, if `c.ShouldAutoPush()`, call `c.PushCoordRefs()`.

- [ ] **Step 5: Run tests**

Run: `cd /home/draco/work/graft && go test ./pkg/coord/ -run TestShouldAutoPush -v`
Expected: PASS

- [ ] **Step 6: Commit**

Run: `cd /home/draco/work/graft && buckley commit --yes --minimal-output`

---

### Task 25: graft diff --coord

**Files:**
- Modify: `cmd/graft/cmd_diff.go`
- Modify: `pkg/coord/claim.go` (add ClaimsForFile helper)

- [ ] **Step 1: Add ClaimsForFile helper**

```go
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
```

- [ ] **Step 2: Write test for ClaimsForFile**

```go
func TestClaimsForFile(t *testing.T) {
	c := newTestCoordinator(t)
	id, _ := c.RegisterAgent(AgentInfo{Name: "filer", Workspace: "graft", Host: "test"})

	c.AcquireClaim(id, ClaimRequest{EntityKey: "decl:function_definition::A:func A():0", File: "a.go", Mode: ClaimEditing})
	c.AcquireClaim(id, ClaimRequest{EntityKey: "decl:function_definition::B:func B():0", File: "b.go", Mode: ClaimEditing})

	claims, _ := c.ClaimsForFile("a.go")
	if len(claims) != 1 {
		t.Fatalf("expected 1 claim for a.go, got %d", len(claims))
	}
}
```

- [ ] **Step 3: Add --coord flag to diff command**

In `cmd/graft/cmd_diff.go`, add `--coord` bool flag. When set:
- Run the normal diff
- For each changed file, call `coord.ClaimsForFile(path)`
- Annotate diff output with claim info (agent name, mode) next to each entity section

- [ ] **Step 4: Run tests**

Run: `cd /home/draco/work/graft && go test ./pkg/coord/ -run TestClaimsForFile -v`
Expected: PASS

- [ ] **Step 5: Commit**

Run: `cd /home/draco/work/graft && buckley commit --yes --minimal-output`

---

### Task 26: graft blame --entity

**Files:**
- Create: `cmd/graft/cmd_blame.go`
- Modify: `cmd/graft/main.go`

- [ ] **Step 1: Implement blame command**

```go
func newBlameCmd() *cobra.Command {
	var (
		entityKey string
		jsonFlag  bool
	)

	cmd := &cobra.Command{
		Use:   "blame <file>",
		Short: "Show coordination history for entities in a file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			filePath := args[0]
			r, err := repo.Open(".")
			if err != nil {
				return err
			}
			c := coord.New(r, coord.DefaultConfig)

			// Get claims on this file
			claims, _ := c.ClaimsForFile(filePath)

			// Walk feed for events touching this file's entities
			events, _ := c.WalkFeed("", 100)
			var relevant []coord.FeedEvent
			for _, evt := range events {
				for _, ent := range evt.Entities {
					if ent.File == filePath {
						relevant = append(relevant, evt)
						break
					}
				}
			}

			if jsonFlag {
				result := map[string]interface{}{
					"file":            filePath,
					"active_claims":   claims,
					"recent_activity": relevant,
				}
				data, _ := json.MarshalIndent(result, "", "  ")
				fmt.Println(string(data))
			} else {
				fmt.Printf("Entity blame for %s\n\n", filePath)
				if len(claims) > 0 {
					fmt.Println("Active claims:")
					for _, cl := range claims {
						fmt.Printf("  %s — %s (%s, %s)\n", cl.EntityKey, cl.AgentName, cl.Mode, cl.ClaimedAt.Format(time.RFC3339))
					}
				}
				if len(relevant) > 0 {
					fmt.Println("\nRecent activity:")
					for _, evt := range relevant {
						fmt.Printf("  [%s] %s by %s\n", evt.Event, evt.Entities[0].Key, evt.AgentName)
					}
				}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&entityKey, "entity", "", "filter to specific entity key")
	cmd.Flags().BoolVar(&jsonFlag, "json", false, "JSON output")
	return cmd
}
```

- [ ] **Step 2: Register in main.go**

Add `root.AddCommand(newBlameCmd())`.

- [ ] **Step 3: Manual smoke test**

Run: `cd /home/draco/work/graft && go run ./cmd/graft blame pkg/coord/coord.go --json`

- [ ] **Step 4: Commit**

Run: `cd /home/draco/work/graft && buckley commit --yes --minimal-output`
