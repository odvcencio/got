# Graft Track Improvements Design

**Date:** 2026-03-14
**Scope:** graft + gts-suite (non-Orchard improvements)
**Status:** Draft

---

## Overview

Three initiatives to strengthen graft's foundation and adoption path, executed in sequence:

1. **B — gts-suite standalone distribution** (unblocked once gotreesitter perf PR lands)
2. **A — Full bidirectional git bridge** (highest impact, highest complexity)
3. **F — Language-specific merge intelligence** (polish layer on structural merge)

---

## B: gts-suite Standalone Distribution

### Goal

Ship gts-suite as an independently installable tool — MCP server, CLI, and LSP — that developers can use today without graft or Orchard. Structural code analysis for 206 languages, zero migration required.

### Rationale

gts-suite is the Trojan horse. Once developers depend on structural analysis (call graphs, complexity, impact analysis, entity-aware grep), the migration to structural version control is a natural next step. It's also the fastest win — the tool already works, it just needs packaging.

### What Exists

- `gts` CLI with 20+ commands (grep, map, query, refs, callgraph, dead, complexity, hotspot, impact, testmap, similarity, capa, yara, chunk, context, scope, deps, bridge, lint, refactor, diff, stats, files)
- `gtsls` LSP server (definition, references, symbols, hover, rename)
- MCP stdio server exposing all tools for AI agents
- Incremental indexing with file cache and watch mode
- Scope resolution for Go, Python, TypeScript, TSX

### What Needs to Happen

#### 1. Decouple the build

Remove the `replace` directive in `gts-suite/go.mod` that points to the local gotreesitter checkout. Pin to a published gotreesitter tag.

**Blocked by:** gotreesitter performance PR must land first, then tag a release (v0.6.1 or v0.7.0).

**Contingency:** If the perf PR takes longer than expected, ship gts-suite with a pinned pre-release tag of gotreesitter (e.g., `v0.6.1-rc1`) and update the dependency once the perf work stabilizes. Don't let a single PR block distribution.

#### 2. Release workflow

Create a GitHub Actions workflow in gts-suite that triggers on version tags (`v*`):
- Cross-compile `gts` and `gtsls` for linux/darwin/windows x amd64/arm64
- Generate checksums
- Create GitHub Release with binaries
- This is a tag-only workflow — no ongoing CI minute cost

#### 3. Install story

- `go install github.com/odvcencio/gts-suite/cmd/gts@latest`
- `go install github.com/odvcencio/gts-suite/cmd/gtsls@latest`
- Direct binary download from GitHub Releases
- Homebrew tap (nice-to-have, later)

#### 4. MCP config snippet

Ready-to-paste configuration for AI agent integration:

```json
{
  "mcpServers": {
    "gts": {
      "command": "gts",
      "args": ["mcp"]
    }
  }
}
```

Document which tools are available and what they do for discoverability.

#### 5. Prep work (unblocked now)

- Draft release workflow YAML
- Draft README install section
- Verify `gts mcp` works cleanly without a graft repo present
- Ensure `.gts/` index directory is created automatically on first use

### Sequencing

Steps 5 (prep) can start immediately. Steps 1-4 execute once gotreesitter tags a release.

---

## A: Full Bidirectional Git Bridge

### Goal

Make graft work inside existing `.git` repos. `.graft/` lives alongside `.git/`. Both object stores are maintained. Both CLIs work. Zero migration cost.

### Rationale

The single biggest adoption barrier is "replace your VCS." The git bridge turns graft from "replace git" to "upgrade git." Developers can `graft init` in any existing repo, use `graft merge` for structural merging, and `git log` still works. Team adoption is per-developer — one person uses graft, others use git, same repo.

### Architecture

#### Dual object stores

```
project/
  .git/               # standard git repo (SHA-1, blobs, trees)
    objects/
    refs/
  .graft/              # graft structural layer (SHA-256, entities)
    objects/
    refs/
    hash_map/          # bidirectional graft_hash <-> git_hash index
```

#### Hash translation layer

Bidirectional mapping between graft SHA-256 hashes and git SHA-1 hashes. This is the same concept Orchard uses in its `hash_mapping` database table, moved client-side as a local index.

```go
// GitHash is variable-length to support both SHA-1 (20 bytes) and
// SHA-256 (32 bytes) git object formats.
type GitHash []byte

type HashMap interface {
    GraftToGit(graftHash object.Hash) (gitHash GitHash, ok bool)
    GitToGraft(gitHash GitHash) (graftHash object.Hash, ok bool)
    Put(graftHash object.Hash, gitHash GitHash) error
    // Rebuild reconstructs the hash map by walking both object stores.
    // Used for recovery after crashes or corruption.
    Rebuild(graftStore, gitStore object.Store) error
}
```

Storage: append-only file in `.graft/hash_map/`. The map is rebuildable from scratch by walking both object stores, so corruption is recoverable via `graft bridge repair`.

**Hash algorithm note:** Graft uses SHA-256; git defaults to SHA-1 but increasingly supports SHA-256 (`extensions.objectFormat`). The `GitHash` type is variable-length to support both. We accept SHA-1's collision resistance as sufficient on the git side because git itself does.

#### Operations

**`graft init` in a git repo:**
1. Detect `.git/` exists
2. Create `.graft/` alongside it
3. Walk git HEAD tree only (not full history — import-on-access for older commits)
4. Import blobs as graft objects; non-source files (binaries, configs, images) are stored as plain blobs without entity lists
5. Extract entities from parseable source files via gotreesitter
6. Create graft entity lists (for source files), trees, commit objects
7. Populate hash map
8. Add `.graft/` to `.git/info/exclude` (avoids polluting `.gitignore`)

**`graft status` (bridge-aware):**
1. Show graft-side working tree status (staged, modified, untracked)
2. Detect if `.git/` refs have moved ahead since last sync
3. If out of sync, show advisory: "git refs ahead — run `graft bridge sync` or any graft operation to import"

**`graft commit` (dual-write):**
1. Write graft objects (entities, entity lists, trees, commit) to `.graft/objects/`
2. Read working tree files to create git blobs (no entity→blob reconstruction needed — the working tree IS the source of truth)
3. Write equivalent git objects (blobs, trees, commit) to `.git/objects/`
4. Update `.graft/refs/` first (source of truth), then `.git/refs/`. Not atomic — if crash occurs between the two, next graft operation reconciles from `.graft/refs/`
5. Update hash map

**Git-side changes (sync on next graft operation):**
1. Compare `.git/refs/` with last-known state
2. If git refs moved ahead (someone ran `git commit`, `git pull`, etc.), import new commits
3. Walk new git objects, create corresponding graft objects
4. Extract entities from new/changed blobs
5. Update hash map

**`graft merge` (structural merge, dual-write result):**
1. Perform three-way structural entity merge via existing merge engine
2. Write merged result as graft commit to `.graft/`
3. Synthesize equivalent git merge commit to `.git/`
4. Both `git log` and `graft log` show the merge

**Push/pull routing:**
- Remote is a git forge (GitHub, GitLab, Bitbucket) → route through `.git/` (git transport)
- Remote is Orchard → route through `.graft/` (graft protocol)
- Hash map keeps both sides consistent after transport operations

#### Key Invariants

1. **`.git/` is always a valid git repo.** Any git operation works at any time. Removing `.graft/` leaves a fully functional git repo with complete history.
2. **Working tree is source of truth for git blobs.** Git blobs are created from working tree files, not by reconstructing from entities. This avoids round-trip fidelity issues — entity extraction/reconstruction is byte-perfect for unmodified files, but after a structural merge the merged output becomes the new working tree content, and git blobs are derived from that. The invariant is: git blobs always match the working tree, not that they match entity reconstruction.
3. **`.graft/refs/` is the authoritative ref store.** On crash recovery, `.git/refs/` are reconciled from `.graft/refs/` via the hash map. Lazy sync on git-side changes imports git commits into graft on next access.
4. **Hash map is rebuildable.** If the hash map is lost or corrupted, `graft bridge repair` reconstructs it by walking both object stores.

#### Phasing

**Phase 1: Read-only bridge**
- `graft init` imports git history
- `graft diff`, `graft blame`, `graft grep` work against git repos
- Entity extraction and structural analysis available
- No writes back to `.git/`

**Phase 2: Dual-write commits**
- `graft commit` writes to both stores
- `graft merge` does structural merge, writes to both
- Git-side sync (detect and import git commits)

**Phase 3: Full interop**
- Push/pull routing based on remote type
- Lazy incremental import (don't walk full history on init)
- Worktree support (both `.git/` and `.graft/` worktrees)

### Risks

- **Performance on large repos:** Mitigated from Phase 1 — `graft init` only imports HEAD, older commits are imported on access.
- **Ref sync races:** If git and graft operations interleave, refs can temporarily diverge. `.graft/refs/` is authoritative; reconciliation happens lazily on next graft operation. No file locking needed — just detect and re-sync.
- **Merge representation:** A graft structural merge writes merged files to the working tree, then creates git blobs from those files. Git sees normal file content with a normal merge commit.
- **Non-source files:** Binaries, configs, images, lockfiles are stored as plain blobs without entity lists. `TreeEntry` already supports both `BlobHash` and `EntityListHash` fields — entries without entity lists use `BlobHash` only.

---

## F: Language-Specific Merge Intelligence

### Goal

Augment the structural merge engine with language-aware post-merge rules that go beyond import set-union merging. Turn "structurally correct merge" into "semantically correct merge."

### Rationale

The core three-way structural merge handles 80% of real-world cases. Language-specific rules handle the remaining 20% — scenarios where the merge is structurally valid but semantically incomplete or risky.

This is also the foundation for user-defined merge policies (business rules, compliance rules, org-specific constraints), making it a platform feature, not just a language feature.

### Architecture

#### Post-merge rule interface

```go
// LangMergeRule runs after structural merge to add
// language-specific warnings or auto-resolutions.
type LangMergeRule interface {
    // Language returns the language this rule applies to (e.g., "go", "rust").
    Language() string

    // Apply inspects the merge result and returns diagnostics.
    // Rules are advisory-only: they produce diagnostics but do not
    // mutate the merge output. Auto-resolution rules (e.g., enum
    // set-union) are implemented in the core merge engine, not here.
    Apply(ctx *MergeRuleContext) []Diagnostic
}

type MergeRuleContext struct {
    Base   *EntityList
    Ours   *EntityList
    Theirs *EntityList
    Merged *MergeResult
    Lang   string
}

type Diagnostic struct {
    Severity DiagSeverity // Warning, Error, Info
    Entity   string       // Entity identity key
    Message  string
    Rule     string       // Rule identifier
}

type DiagSeverity int
const (
    DiagInfo DiagSeverity = iota
    DiagWarning
    DiagError
)
```

#### Built-in rules (Phase 1 — Go)

| Rule | Trigger | Action |
|------|---------|--------|
| `go-interface-impl` | Method added to interface type | Warning: implementors may need updating |
| `go-struct-field-conflict` | Both sides add fields to same struct | Merge (set-union), warn if tag conflicts |
| `go-const-var-block` | Both sides add to const/var block | Set-union merge (like imports) |
| `go-init-func` | Both sides modify `init()` | Elevated conflict priority |
| `go-embed-directive` | `//go:embed` added/changed | Warning: check embedded file exists |

#### Built-in rules (Phase 2 — TypeScript, Python, Rust)

| Rule | Language | Trigger | Action |
|------|----------|---------|--------|
| `ts-enum-union` | TypeScript | Both sides add enum members | Set-union merge |
| `ts-type-union` | TypeScript | Both sides extend union type | Set-union merge |
| `py-init-conflict` | Python | Both sides modify `__init__` | Elevated conflict priority |
| `py-decorator-order` | Python | Decorator ordering changed | Warning: order may matter |
| `rust-trait-impl` | Rust | Method added to trait | Warning: implementors need updating |
| `rust-derive-merge` | Rust | Both sides add derive macros | Set-union merge |

#### User-defined rules (Phase 3 — extensibility)

The `LangMergeRule` interface extends naturally to user-defined merge policies loaded from `.graft/merge-rules/` or pushed via Orchard org settings. The concrete DSL for expressing custom rules (entity selectors, conditions, actions) needs its own design document — it is out of scope for this spec.

Conceptual examples of what user-defined rules would express:
- "Changes to `pricing.go` entities must not remove existing tiers"
- "Any entity touching PII fields must include an audit log call"
- "Proto field numbers are append-only — renumbering is an error"

### Integration point

Rules execute inside `merge.MergeFiles()` as a post-merge pass:

```go
func MergeFiles(...) (*MergeResult, error) {
    // ... existing structural merge ...

    // Post-merge language rules
    for _, rule := range registry.RulesFor(lang) {
        diags := rule.Apply(&MergeRuleContext{
            Base: base, Ours: ours, Theirs: theirs, Merged: result, Lang: lang,
        })
        result.Diagnostics = append(result.Diagnostics, diags...)
    }

    return result, nil
}
```

Diagnostics surface in:
- `graft merge` CLI output
- `graft conflicts` (alongside entity conflicts)
- Orchard PR merge preview
- MCP tools (for AI agent consumption)

### Phasing

1. **Interface + Go rules** — eat our own dogfood
2. **TypeScript, Python, Rust rules** — cover the major languages
3. **User-defined rules + Orchard integration** — org-level merge policies

---

## Sequencing Summary

B→A is a strategic dependency (adoption funnel), not a technical one. A and F have no technical dependencies on each other — sequencing is a resourcing choice.

```
NOW (unblocked)
  ├── B prep: release workflow, README, verify gts mcp standalone
  ├── F Phase 1: Define LangMergeRule interface, implement Go rules
  └── A Phase 1: read-only git bridge (init, diff, blame, grep)

AFTER gotreesitter perf PR lands + tag
  └── B ship: remove replace directive, tag gts-suite, publish

AFTER A Phase 1
  └── A Phase 2: dual-write (commit, merge write to both stores)

AFTER A Phase 2
  └── A Phase 3: full interop (push/pull routing, lazy import)

PARALLEL (no dependencies between these)
  └── F Phase 2: TypeScript, Python, Rust rules
      Note: Rust merge rules may be limited without scope resolution
      in gts-suite (currently only Go, Python, TypeScript, TSX have
      scope rules). Rust scope rules are a nice-to-have prerequisite.

LATER (with Orchard track)
  └── F Phase 3: user-defined rules, Orchard org policies (needs own design doc)
```

---

## Resolved Questions

1. **Hash map storage format:** Append-only flat file. Rebuildable from both object stores on corruption. Simple, no dependencies.
2. **Lazy vs eager import:** HEAD-only on init. Older commits imported on access. Matches existing `bootstrapGotFromGit` behavior.
3. **Git hooks vs poll:** Lazy detection on next graft operation. Matches existing `syncGotSnapshotFromGit` approach. Simpler, more reliable, no hook installation needed.
4. **Scope resolution expansion:** Separate track. Rust scope rules are a nice-to-have for F Phase 2 but not a blocker — Rust merge rules can work at the entity identity level without full scope resolution.
