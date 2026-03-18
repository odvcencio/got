# Graft Coord: Multi-Agent Entity-Level Coordination

**Status:** Approved
**Date:** 2026-03-12
**Scope:** `pkg/coord`, `cmd/graft` CLI extensions, MCP surface, cross-repo federation

---

## Problem

Multiple agents (human or AI) working across related repositories have no structural awareness of each other's in-flight changes. Conflicts surface only at merge time. Entity-level collisions — where one agent modifies a function signature while another is editing a caller — go undetected until code review or CI. This wastes cycles and produces preventable merge conflicts.

## Solution

Bake coordination into graft's workflow using its own primitives. Coordination state is stored as graft objects referenced by graft refs in a `refs/coord/` namespace. Claims are implicit from staging. Impact analysis traces entity references across repo boundaries. The same `graft fetch`/`graft push` protocol that syncs code syncs coordination state. Local and remote coordination use identical data models and commands.

## Non-Goals

- Real-time streaming (WebSocket/SSE) — poll-based and hook-driven delivery is sufficient
- Distributed consensus — single-writer CAS on refs is enough; no Raft/Paxos
- Language-specific type checking — entity-level structural analysis, not type inference
- Replacing graft's merge engine — coordination prevents conflicts; merge handles the ones that slip through
- Multi-language dependency resolution in v1 — workspace graph is Go-specific (`go.mod`). Extension point for `package.json`, `Cargo.toml`, etc. is deferred

## Prerequisites

- **`DeleteRefCAS` primitive:** `pkg/repo` currently only supports `DeleteBranch` (hardcoded to `refs/heads/`) and `DeleteTag` (hardcoded to `refs/tags/`). Coordination requires a general `DeleteRefCAS(name, oldHash)` that removes any ref atomically. This must be added before implementation begins.
- **Ref namespace reservation:** `refs/coord/` must be rejected by `graft branch` and `graft tag` to prevent accidental collisions with coordination refs.
- **`graft fetch` refspec extension:** The current fetch implementation pulls `refs/heads/` and `refs/tags/`. It must support `refs/coord/` as an additional prefix (via `--coord` flag or automatic detection).

---

## 1. Data Model

Coordination state lives at `.graft/coord/` logically, stored as graft objects in the repo's existing `.graft/objects/` store and referenced by refs under `refs/coord/`.

### 1.1 Agent Registry

**Ref:** `refs/coord/agents/{agent-id}`
**Object:** Graft blob

```json
{
  "id": "a1b2c3",
  "name": "backend-fixer",
  "workspace": "graft",
  "host": "draco-wsl",
  "pid": 48201,
  "started": "2026-03-12T14:30:00Z",
  "heartbeat": "2026-03-12T14:35:12Z"
}
```

- `name` is human-friendly, set by `graft workon --as`
- `workspace` is the workspace name from `~/.graftconfig` (portable, not a path)
- `host` distinguishes agents on different machines sharing a remote
- `heartbeat` updated every 30s via CAS ref update. Stale threshold: 120s
- Liveness detection (local): if heartbeat is stale and `pid` is not running, the agent is dead
- Liveness detection (remote): if heartbeat is stale beyond `stale_threshold`, the agent is presumed dead regardless of PID (no remote PID probing — would require a challenge-response protocol that violates our non-goal of avoiding distributed consensus)
- Any agent running `graft coord` garbage-collects dead agents and their orphaned claims

### 1.2 Claims

**Ref:** `refs/coord/claims/{entity-key-hash}`
**Object:** Graft blob

```json
{
  "entity_key": "decl:function_definition::DiffFiles:func DiffFiles():0",
  "file": "pkg/diff/diff.go",
  "agent": "a1b2c3",
  "agent_name": "backend-fixer",
  "mode": "editing",
  "claimed_at": "2026-03-12T14:31:00Z"
}
```

- `entity-key-hash` is SHA-256 of the full entity identity key string. This is the canonical identifier for claims — lookups are always by hash, never by parsing the key from the ref path. This sidesteps parsing ambiguity from colons in signatures.
- `mode`: `editing` (active modification, blocks others in same repo) or `watching` (notification-only, never blocks)
- CAS ref creation provides atomic claim acquisition — if two agents race, one gets a CAS mismatch
- Claims are released by writing a tombstone blob (empty `agent` field) via CAS, then deleting the ref via `DeleteRefCAS`. The two-step approach ensures crash-safety: a tombstone without deletion is cleaned up on the next GC pass, while a missing tombstone means the claim is still active.
- Claims are released on commit (editing claims for committed entities) or on `graft workon --done`
- Claim transfer (`graft coord resolve --transfer <agent>`) is a CAS swap: read current blob, verify caller owns the claim, write new blob with the target agent's ID, CAS update the ref. Atomic single operation.

### 1.3 Feed

**Ref:** `refs/coord/feed/head`
**Object:** Graft blob (JSON-encoded linked list)

Each feed entry is a graft blob containing a JSON `FeedEntry`:

```json
{
  "parent": "<hash of previous feed blob>",
  "event": { "...event payload..." },
  "timestamp": "2026-03-12T14:35:00Z"
}
```

Feed entries are stored as blobs (not commits) to avoid coupling to graft's commit serialization format. The linked-list structure is maintained via the `parent` hash pointer.

Event payload:

```json
{
  "event": "entity_changed",
  "commit": "a3f8b21...",
  "entities": [
    {
      "key": "decl:function_definition::DiffFiles:...",
      "file": "pkg/diff/diff.go",
      "change": "signature_changed",
      "breaking": true
    }
  ],
  "impact": {
    "orchard": {
      "callers": ["DiffService.computeDiff", "PRService.mergePreview"],
      "agents_affected": ["frontend-dev"]
    }
  }
}
```

Event types: `entity_changed`, `agent_joined`, `agent_left`, `claim_conflict`

Agents track their read position with a cursor file at `.graft/coord/cursor/{agent-id}` (local-only, not synced). Feed entries older than `feed_retention` (default 7d) are pruned by truncating the chain at a cutoff point (rewriting the oldest retained commit to have no parent). Agents with cursors pointing into the pruned range reset to the oldest available entry rather than erroring.

**Feed CAS retry:** When two agents commit simultaneously, both attempt to append to `refs/coord/feed/head`. One gets a CAS mismatch. Retry strategy: read current head, reparent the feed commit on the new head, CAS-retry. Maximum 5 retries with jittered backoff (10ms base). If retries are exhausted, the feed event is written to a local overflow log at `.graft/coord/feed-overflow/` and merged on the next successful append. Feed events are never silently dropped.

### 1.4 Metadata Indexes

**Export index:** `refs/coord/meta/exports` — blob containing exported entities per package. Rebuilt incrementally on commit for changed packages.

**Xref index:** `refs/coord/meta/xrefs` — blob containing reverse call mappings for external symbols called *within this repo*. Each repo owns its own xref data (callers that live inside this repo pointing to external definitions). This is the only direction a repo can build autonomously. Cross-repo impact queries read the downstream repo's xref index to find callers. Rebuilt incrementally on commit for changed files.

**Workspace graph:** `refs/coord/meta/workspace-graph` — blob containing the dependency graph derived from `go.mod` files across workspaces. V1 is Go-specific; the workspace graph builder is structured as a pluggable resolver to support other dependency manifest formats in the future.

**Config:** `refs/coord/meta/config` — blob containing per-repo coordination policy. Travels with push/fetch.

---

## 2. Coordination Protocol

### 2.1 Join

```bash
$ graft workon --as "backend-fixer"
```

1. Generate agent ID (short random hex)
2. Create blob with agent info, write `refs/coord/agents/{id}` via CAS
3. Start background heartbeat (update blob + CAS ref every 30s)
4. If remotes exist, push `refs/coord/agents/{id}`
5. Fetch `refs/coord/` from all known peers (workspaces + remotes) to hydrate local view
6. If first run, auto-discover workspaces from `go.mod` replace directives and sibling directories

**Leaving:** `graft workon --done` deletes agent ref and all claim refs. Also triggered on SIGTERM/SIGINT. Crash recovery via stale heartbeat detection.

### 2.2 Implicit Claims

Claims are acquired automatically during staging:

```bash
$ graft add pkg/diff/diff.go
```

1. Extract entities from the staged file (graft already does this)
2. Diff against HEAD's entity list — identify changed entities
3. For each changed entity, attempt CAS creation of `refs/coord/claims/{entity-key-hash}`
4. **CAS succeeds:** claim acquired
5. **CAS fails:** conflict. Behavior depends on context:
   - Same repo, same agent: already claimed by you, no-op
   - Same repo, other agent alive: soft block (warn, require `--force`)
   - Same repo, other agent dead: auto-reclaim, print notice
   - Cross-repo: advisory notification with impact context

### 2.3 Cross-Repo Impact on Commit

```bash
$ graft commit -m "refactor: DiffFiles takes options struct"
```

1. Normal graft commit
2. Diff commit against parent → extract entity-level changes
3. For each changed entity, trace the dependency graph:
   a. **Layer 1 — Workspace graph:** Which workspaces import the changed package? (from `go.mod`)
   b. **Layer 2 — Export index:** Did a public entity change? (diff old/new export index)
   c. **Layer 3 — Xref index:** Which specific entities in downstream workspaces call the changed entity?
4. Cross-reference affected entities against active claims in downstream workspaces
5. Build impact report, append feed commit to `refs/coord/feed/head`
6. Release `editing` claims on committed entities
7. Push coord refs to remotes if `auto_push_coord` is enabled

### 2.4 Notification Delivery

**Pull (default):** Agents check the feed at natural checkpoints — before `graft add`, before `graft commit`, when running `graft coord`, or on a configurable poll interval. Walk feed chain since cursor, filter for events touching claimed entities.

**Push (Claude Code agents):** `PostToolUse` hook on Edit/Write triggers `graft coord check --json --quiet`. Relevant events surface as hook output injected into the agent's context.

### 2.5 Conflict Resolution

| Level | Trigger | Response |
|-------|---------|----------|
| **Advisory** | Agent modifies entity that another agent is `watching` | Notification only |
| **Warning** | Agent stages entity claimed by another agent in a different repo | Print impact, suggest coordination |
| **Soft block** | Agent stages entity claimed by another agent in the same repo | Warn, require `--force` to override |
| **Hard block** | Entity matches `protected_entities` pattern | Reject staging unconditionally |
| **Merge gate** | Both agents commit changes to same entity | Graft structural merge handles it; both agents notified |

Cross-repo conflicts are advisory by default because the dependency direction means the downstream agent adapts, not the upstream one.

---

## 3. Cross-Repo Dependency Resolution

Three layers, each building on the last:

### 3.1 Layer 1: Workspace Graph (static, cheap)

Derived from `go.mod` files across known workspaces. Built on `graft workon`, refreshed on `graft fetch --coord`.

```
gotreesitter → graft (pkg/entity, pkg/object)
             → gts-suite (grammars, parser)
graft        → orchard (pkg/diff, pkg/merge, pkg/object)
```

Answers: "which repos could be affected?" Coarse filter.

### 3.2 Layer 2: Export Index (structural, medium cost)

Each repo maintains an index of exported entities at `refs/coord/meta/exports`. Built from graft's entity extraction, filtered to exported symbols.

```json
{
  "pkg/diff": {
    "DiffFiles": {
      "key": "decl:function_definition::DiffFiles:...",
      "signature": "func DiffFiles(base, head []Entity) []Change",
      "hash": "a3f8b21..."
    }
  }
}
```

Rebuilt on commit for changed packages. Diffing old/new export index identifies breaking vs non-breaking changes.

### 3.3 Layer 3: Import-Site Resolution (precise, on-demand)

Answers: "which specific functions in orchard call `graft/pkg/diff.DiffFiles()`?"

**Fast path:** Cached xref index at `refs/coord/meta/xrefs`. Reverse lookup by qualified name.

```json
{
  "github.com/odvcencio/graft/pkg/diff.DiffFiles": [
    {"file": "internal/service/diff.go", "entity": "DiffService.computeDiff", "line": 142},
    {"file": "internal/service/pr.go", "entity": "PRService.mergePreview", "line": 195}
  ]
}
```

**Slow path:** On-demand entity extraction + import resolution across the downstream workspace's source. Used when no cached xref exists (first run).

Xref index is rebuilt incrementally — only files whose entity list hash changed since last build. Stored as a graft blob, travels via fetch/push.

### Cost Profile

| Operation | When | Cost |
|-----------|------|------|
| Workspace graph | `graft workon`, `graft fetch` | Negligible — parse go.mod files |
| Export index rebuild | On commit, changed packages only | Low |
| Xref index rebuild | On commit, incremental | Medium |
| Xref lookup | On feed event | Cheap — hash map lookup |
| Full xref scan (cold) | First `graft workon` | Expensive once, cached after |

---

## 4. CLI & MCP Surface

### 4.1 Design Principle

One coordination engine, two transports. The CLI calls the engine directly. The MCP server wraps the same functions with JSON-RPC framing. No logic duplication. Every command supports `--json` for machine consumption.

### 4.2 Command/Tool Matrix

| CLI Command | MCP Tool | Purpose |
|---|---|---|
| `graft workon --as <name>` | `graft_workon` | Join coordination session |
| `graft workon --done` | `graft_workon_done` | Leave, release all claims |
| `graft coord` | `graft_coord_status` | Dashboard: agents, claims, conflicts, feed summary |
| `graft coord agents` | `graft_coord_agents` | List active agents across all workspaces |
| `graft coord claims [--workspace <w>]` | `graft_coord_claims` | List claims, optionally filtered |
| `graft coord feed [--since <h>] [--mine]` | `graft_coord_feed` | Read feed events |
| `graft coord diff <agent>` | `graft_coord_diff` | Entity diff of another agent's in-flight work |
| `graft coord impact [<entity>]` | `graft_coord_impact` | Blast radius with cross-repo tracing |
| `graft coord check` | `graft_coord_check` | Quick conflict check (hook-optimized) |
| `graft coord xrefs <entity>` | `graft_coord_xrefs` | Cross-repo reverse call lookup |
| `graft coord graph` | `graft_coord_graph` | Workspace dependency graph |
| `graft coord watch <entity>` | `graft_coord_watch` | Soft-claim: notify on change |
| `graft coord unwatch <entity>` | `graft_coord_unwatch` | Remove watch |
| `graft coord resolve <entity-key-hash>` | `graft_coord_resolve` | Release/transfer a conflicted claim (identified by entity-key-hash from the ref path) |
| `graft workspace add <name> <path>` | `graft_workspace_add` | Register a workspace |
| `graft workspace list` | `graft_workspace_list` | List known workspaces |

### 4.3 Augmented Existing Commands

Existing graft commands gain coordination context when an agent is active:

- **`graft status`** — shows claimed entities, other agents' claims on related entities, unread feed count
- **`graft add`** — implicit entity claim acquisition via CAS, conflict warnings
- **`graft commit`** — entity change detection, cross-repo impact analysis, feed event publication, claim release
- **`graft diff --coord`** — overlay claim annotations on entity diff output
- **`graft fetch --coord`** — pull `refs/coord/` from remotes
- **`graft blame --entity`** — show coordination history for an entity

**Note on `graft coord diff <agent>`:** This command reads the other agent's staged entities. For local workspaces, it reads the peer workspace's `.graft/index`. For remote agents, staged state is not accessible — the command shows the agent's latest committed changes instead and notes that in-flight work is not visible remotely.

### 4.4 MCP Integration

```bash
# Standalone graft MCP server
$ graft mcp serve

# With code intelligence (invokes gts-suite as a subprocess)
$ graft mcp serve --with-codeintel
# graft spawns `gts mcp` as a child process and proxies its tools
# alongside native graft tools. If gts-suite is not installed,
# --with-codeintel is a no-op with a warning.
```

Every MCP response includes ambient coordination context in `_meta`:

```json
{
  "result": { "..." },
  "_meta": {
    "tool": "graft_coord_status",
    "ok": true,
    "duration_ms": 42,
    "coord": {
      "active_agents": 3,
      "your_claims": 4,
      "conflicts": 1,
      "unread_feed": 2
    }
  }
}
```

### 4.5 Claude Code MCP Configuration

```json
{
  "mcpServers": {
    "graft": {
      "command": "graft",
      "args": ["mcp", "serve", "--with-codeintel"]
    }
  }
}
```

One connection provides both coordination tools and structural code intelligence.

---

## 5. Configuration

### 5.1 Global — `~/.graftconfig`

```json
{
  "name": "draco",
  "email": "draco@orchard.dev",
  "workspaces": {
    "gotreesitter": "/home/draco/work/gotreesitter",
    "graft": "/home/draco/work/graft",
    "gts-suite": "/home/draco/work/gts-suite",
    "orchard": "/home/draco/work/orchard"
  },
  "coord": {
    "heartbeat_interval": "30s",
    "stale_threshold": "120s",
    "auto_push_coord": false,
    "feed_retention": "7d",
    "default_conflict_mode": "advisory"
  }
}
```

**Backward compatibility:** The `workspaces` and `coord` keys are new additions to `~/.graftconfig`. The existing `json.Unmarshal` in `pkg/userconfig` silently ignores unknown fields, so older graft binaries will not break. No config version bump is needed — the struct simply gains new optional fields.

**Auto-discovery:** On first `graft workon` with no workspaces configured, scan `go.mod` replace directives and sibling directories. Suggest related repos interactively.

### 5.2 Per-Repo — `refs/coord/meta/config`

Stored as a graft blob. Travels with push/fetch so the team shares coordination policy.

```json
{
  "conflict_mode": "soft_block",
  "auto_index_exports": true,
  "auto_index_xrefs": true,
  "protected_entities": [
    "decl:function_definition::MergeFiles:*",
    "decl:function_definition::DiffFiles:*"
  ],
  "notify_on": ["signature_changed", "entity_removed"],
  "ignore_patterns": ["*_test.go", "internal/testutil/*"]
}
```

**`conflict_mode`:** `advisory` | `soft_block` | `hard_block`
**`protected_entities`:** patterns matched against entity identity keys. Uses `filepath.Match` semantics where `*` matches any sequence of non-colon characters (respecting the colon-delimited key format). Example: `decl:function_definition::MergeFiles:*` matches any signature and ordinal for `MergeFiles`. Entities that always get `hard_block` treatment.
**`notify_on`:** which change types generate feed events
**`ignore_patterns`:** file path glob patterns excluded from coordination (standard `filepath.Match`)

### 5.3 Per-Agent — Session Flags

```bash
$ graft workon --as "name" [--notify breaking] [--conflict-mode hard_block] [--watch-only] [--scope "pkg/diff/..."]
```

Runtime overrides. Don't persist.

### 5.4 Claude Code Auto-Integration

Per-repo `.claude/settings.local.json`:

```json
{
  "hooks": {
    "SessionStart": [
      {
        "command": "graft workon --as claude-$CLAUDE_SESSION_ID --notify breaking --json",
        "timeout": 5000
      }
    ],
    "PostToolUse": [
      {
        "matcher": "Edit|Write",
        "command": "graft coord check --json --quiet",
        "timeout": 3000
      }
    ],
    "Stop": [
      {
        "command": "graft workon --done --json",
        "timeout": 3000
      }
    ]
  }
}
```

Global `~/.claude/settings.json` for cross-repo auto-coordination:

```json
{
  "hooks": {
    "SessionStart": [
      {
        "command": "graft workon --as claude-$CLAUDE_SESSION_ID --auto-discover --json",
        "timeout": 8000
      }
    ]
  }
}
```

---

## 6. Package Structure

New package in graft: `pkg/coord/`

```
pkg/coord/
  coord.go          — Engine: the central coordinator struct and configuration
  agent.go          — Agent registry: join, leave, heartbeat, liveness detection
  claim.go          — Claim management: acquire, release, conflict detection
  feed.go           — Feed: append events, walk chain, cursor management
  impact.go         — Impact analysis: export index, xref index, cross-repo tracing
  workspace.go      — Workspace graph: discovery, go.mod parsing, federation
  transport.go      — Abstraction over local (filesystem) and remote (graft protocol) peers
```

CLI extensions in `cmd/graft/`:

```
cmd/graft/
  workon.go         — graft workon command
  coord.go          — graft coord command tree (agents, claims, feed, diff, impact, etc.)
  workspace.go      — graft workspace add/list/remove
```

MCP extensions:

```
cmd/graft/
  mcp_coord.go      — MCP tool registrations for coordination
```

---

## 7. Transport Unification

Local and remote coordination use identical data models. The difference is only in transport:

```
LOCAL (same machine)
  Agent reads/writes refs/coord/ via filesystem
  Cross-workspace reads via filesystem (paths from ~/.graftconfig)

REMOTE (different machines)
  Agent reads/writes local refs/coord/ via filesystem
  Cross-workspace sync via graft fetch/push (existing wire protocol)
  Orchard server serves refs/coord/ like any other ref namespace
```

No new protocol. No new auth. `refs/coord/` is just refs. The existing graft remote protocol handles sync. Orchard's web UI can display coordination state by reading `refs/coord/` — no new API needed.

---

## 8. Invariants

1. **CAS safety:** All claim acquisitions use compare-and-swap. Two agents racing for the same entity claim — exactly one wins, the other gets a conflict.
2. **Crash recovery:** Stale heartbeat + dead PID = garbage-collectible. No orphaned claims persist beyond `stale_threshold`.
3. **Feed ordering:** Commit chain provides causal ordering. Feed entries reference the graft commit that triggered them.
4. **Cross-repo consistency:** Export and xref indexes are rebuilt incrementally on commit. Staleness is bounded by commit frequency.
5. **Zero coordination overhead when solo:** If no other agents are active, coordination checks are O(1) — read `refs/coord/agents/`, find only yourself, skip everything else.
6. **Graceful degradation:** If a peer workspace is unreachable (remote down, path moved), coordination continues for reachable peers. Unreachable peers are logged, not fatal.
