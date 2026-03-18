# Graft Enterprise VCS Design

**Date:** 2026-03-01
**Status:** Approved
**Scope:** Take graft from functional VCS to enterprise-grade git alternative

## Context

Graft is a structural VCS that merges at the entity level (functions, methods, types, imports) rather than line level. It uses gotreesitter (205 languages, pure Go) for parsing and has a 4-layer hardened remote protocol targeting Orchard (managed hosting) with free self-hosting.

**What exists (~40K lines, 72 test files, 26 commands):**
- Content-addressed object store (SHA-256, pack files, delta encoding)
- Entity extraction via tree-sitter (205 languages)
- Three-way structural merge with entity-level resolution
- Set-union import merging
- Entity-level and line-level diff
- Full CLI: init, add, reset, rm, status, commit, log, show, blame, diff, branch, tag, checkout, merge, cherrypick-entity, remote, publish, clone, pull, push, reflog, gc, verify
- Remote protocol (retry, validation, pack wire, zstd, sideband, negotiation)
- Git forge bridges (GitHub, GitLab, Bitbucket via shelling out to git)
- SSH auth, user config, .graftignore

**What's missing for enterprise parity:** See sections below.

## Architecture Principle

Graft is the client-side VCS binary (like `git`). Orchard is the server/web UI (like GitHub/Gitea) — a separate project. gotreesitter is the parsing engine in its own repo. This design covers graft-the-client only.

Every workflow command supports entity-scoped operations. Entity awareness is not an add-on — it's the default behavior. Commands operate at commit level by default and accept `--entity` filters to scope down to individual functions, types, or import blocks.

---

## 1. Native Git Compatibility Layer

### Goal
Graft reads and writes `.git` format natively. Users can run graft on existing git repos as a drop-in replacement.

### Design

**New package: `pkg/gitcompat/`**

| Component | Purpose |
|-----------|---------|
| `packfile.go` | Read/write git pack format (differs from graft's in header magic, type encoding, delta refs) |
| `packindex.go` | Read/write `.idx` files (fan-out table, SHA-1/SHA-256 sorted entries) |
| `loose.go` | Read/write loose objects (`type size\0content`, zlib-compressed) |
| `refs.go` | Parse `.git/refs/`, `.git/packed-refs`, HEAD (symrefs + detached) |
| `index.go` | Parse/write `.git/index` (v2/v3/v4 binary format with stat cache) |
| `config.go` | Parse `.git/config` (INI-like: remotes, branch tracking, user info) |
| `mapping.go` | Map between git object types (blob/tree/commit/tag) and graft types (adds entity/entitylist) |

### Dual-Mode Repos

When graft operates on a directory with `.git/`:
- Git objects (blob, tree, commit, tag) are read/written in `.git/` in native git format.
- Graft-specific objects (entity, entitylist) live in a `.graft/` sidecar directory.
- `graft commit` produces a git-compatible commit in `.git/` AND entity metadata in `.graft/`.
- `git log` on the same repo works correctly — graft commits are valid git commits.

**Detection:** `repo.Open()` checks for `.git/` first, then `.graft/`. If both exist, dual-mode. If only `.graft/`, pure graft mode.

### Round-Trip Fidelity

A commit created by graft must be byte-identical to what git would produce for the same content. This means:
- Git object format: `type size\0content` with zlib compression
- Tree entries: sorted by name with mode prefix
- Commit format: `tree`, `parent`, `author`, `committer`, optional `gpgsig`, blank line, message
- SHA-1 hashing for git objects (graft uses SHA-256 internally; the mapping layer handles both)

---

## 2. Workflow Commands

All commands support entity-scoped operations via `--entity` flag or `:entity:` path syntax.

### Tier 1 — Essential Daily Workflow

**`graft stash [push|pop|list|drop|apply|show]`**
- Save work-in-progress as a commit on `refs/stash` (stack via reflog).
- `graft stash -- :entity:func:MyHandler` — stash changes to a single entity.
- Stores both index state and working tree state.
- Implementation: create a commit with two parents (current HEAD + index state), push ref.

**`graft cherry-pick <commit>...`**
- Apply one or more commits to the current branch.
- Uses structural merge engine with commit's parent as base.
- `graft cherry-pick <commit> --entity func:MyHandler` — pick only one entity.
- Entity-scoped cherry-pick already exists as `cherrypick-entity`; this wraps it with commit-level porcelain.

**`graft rebase <upstream> [<branch>]`**
- Replay commits onto a new base using structural merge.
- Graft's entity-level merge makes rebase smoother: independent function additions in separate commits won't conflict during replay.
- Sequencer state stored in `.graft/rebase-apply/` (or `.graft/rebase-merge/`).
- `--onto <newbase>` for rebasing onto arbitrary points.
- `--abort` / `--continue` / `--skip` for conflict resolution.

**`graft fetch [<remote>] [<refspec>...]`**
- Fetch objects and refs without merging.
- Populates `refs/remotes/<remote>/*`.
- Currently `pull` does fetch+fast-forward; extract fetch as independent operation.

### Tier 2 — Power User Workflow

**`graft rebase -i <upstream>`**
- Interactive rebase: opens editor with commit list.
- Actions: pick, reword, edit, squash, fixup, drop, exec.
- Sequencer pauses at `edit` and conflict points, resumes with `--continue`.

**`graft bisect [start|good|bad|reset|run]`**
- Binary search through commit history for regressions.
- `graft bisect run <script>` — automated bisection.
- Entity-aware: `graft bisect --entity func:MyHandler` could restrict the search to commits that touched a specific entity.

**`graft worktree [add|list|remove|prune]`**
- Multiple working trees sharing one object store.
- `graft worktree add <path> <branch>` — linked worktree with separate HEAD/index.
- Shared `.graft/objects/`, separate `.graft/worktrees/<name>/` for HEAD, index, refs.

### Tier 3 — Convenience

**`graft clean [-f] [-d] [-x]`** — Remove untracked files.

**`graft grep <pattern> [<pathspec>...]`** — Search tracked files. Entity-aware: `graft grep --entity-context` shows which entity each match belongs to.

**`graft shortlog [-s] [-n]`** — Summarized commit log by author.

**`graft archive [--format=tar|zip] <tree-ish>`** — Export a tree snapshot.

---

## 3. Scale & Performance

### Shallow & Partial Clone

**`graft clone --depth N <url>`**
- Fetch only N commits of history.
- Negotiation: client sends `depth N` in fetch request; server responds with shallow boundary.
- `.graft/shallow` file lists shallow commit boundaries.

**`graft clone --filter=blob:none <url>`**
- Fetch commits and trees but not blobs.
- Blobs fetched on demand when checked out (fault-in from Orchard).
- Requires Orchard to support single-object GET requests (already exists in protocol).

### Sparse Checkout

**`graft sparse-checkout [init|set|add|disable|list]`**
- Only materialize a subset of the tree in the working directory.
- Cone mode (directory-based) for performance.
- Stored in `.graft/info/sparse-checkout`.
- `status`, `diff`, `add` respect sparse patterns.

### Large File Support (Graft LFS)

**`graft lfs [track|untrack|pull|push|status|ls-files]`**
- Pointer files in tree for large/binary assets.
- `.graftattributes` patterns determine LFS tracking.
- Pointer format: `version graft-lfs/1\noid sha256:<hash>\nsize <bytes>\n`
- Separate upload/download via Orchard's LFS batch API.
- Resumable transfers for large files.

### Commit Graph

**`.graft/objects/info/commit-graph`**
- Persistent commit-graph file for fast traversal.
- Stores: commit hash, tree hash, parent list, generation number, commit timestamp.
- Accelerates: `log`, merge-base computation, reachability checks.
- Updated incrementally by `graft gc` and optionally after each `fetch`.
- Graft already has in-memory merge-base caching; this makes it persistent and covers cold starts.

### Worktree Support

See Section 2 (Tier 2 workflow commands). Shared object store with linked working trees.

---

## 4. Trust, Signing & Hooks

### Design Principle: Signing Is Invisible

Current git/GitHub signing UX is terrible (key generation, upload, gitconfig, gpg-agent). Graft makes signing automatic after one-time auth setup.

### Auto-Signing Flow

1. `graft auth setup` — Interactive first-time setup (already exists).
2. During setup, auto-generate an Ed25519 SSH signing key at `~/.graft/signing_key`.
3. Register the public key with Orchard in the same step.
4. From this point, **every commit is signed automatically**. No `-S` flag needed.
5. `graft commit --no-sign` to explicitly opt out (the exception, not the rule).

### Verification

- `graft log --show-signature` verifies each commit's signature.
- **Zero-config verification:** Fetch the signer's public key from Orchard automatically by their username.
- **Offline fallback:** `~/.graft/allowed_signers` file for manual key trust (self-hosted, air-gapped).
- `graft verify-commit <commit>` / `graft verify-tag <tag>` — Detailed verification output.

### GPG Support

- `graft config set user.signingKey <gpg-key-id>` — Override with GPG key.
- `graft config set gpg.format ssh|gpg` — Choose signing backend.
- Default is SSH (simpler, no gpg-agent needed). GPG available for orgs that require it.

### Client-Side Hooks

`.graft/hooks/` directory:

| Hook | Trigger | Use Case |
|------|---------|----------|
| `pre-commit` | Before commit | Linters, formatters, tests |
| `commit-msg` | After message entry | Enforce message format |
| `pre-push` | Before push | CI gate, protected branch check |
| `post-checkout` | After branch switch | Rebuild, env setup |
| `pre-rebase` | Before rebase | Safety check |
| `post-merge` | After merge | Dependency install |
| `pre-receive` | Server-side, before ref update | (Orchard implements this) |
| `post-receive` | Server-side, after ref update | (Orchard implements this) |

Hooks are executable scripts. `graft init` creates a `hooks/` directory with sample scripts.

### `.graftattributes`

Per-path attributes file (like `.gitattributes`):
```
*.bin filter=lfs diff=lfs merge=lfs
*.proto merge=union
docs/** diff=text
```

Attributes: `filter` (LFS), `diff` (driver), `merge` (strategy override), `text`/`binary`, `encoding`.

### Entity-Level Audit

Graft's unique advantage: reflog can track which entities changed in each operation.
- `graft reflog --entity` shows entity-level change history.
- Each reflog entry optionally records affected entity keys.
- Enables: "When was func:MyHandler last modified, and by which operation?"

---

## 5. Protocol Extensions

### Smart Protocol v2

Extend the existing capability-negotiated HTTP protocol:

**Multiplexed commands over single connection:**
- `POST /v2/ls-refs` — List refs with filtering.
- `POST /v2/fetch` — Negotiate and fetch objects.
- `POST /v2/push` — Push objects and update refs.

**New negotiation parameters:**
- `depth N` / `deepen-since <date>` — Shallow fetch.
- `filter blob:none` / `filter tree:N` — Partial clone filtering.
- `want-entity <key>` — Entity-scoped fetch (graft-specific extension).

### LFS Batch API

- `POST /lfs/objects/batch` — Batch upload/download authorization.
- `PUT /lfs/objects/<oid>` — Upload with resumable support.
- `GET /lfs/objects/<oid>` — Download.
- Compatible with Git LFS batch API spec for interoperability.

### Server-Side Merge Preview

- `POST /v2/merge-preview` — Check if a merge will conflict without committing.
- Server runs graft's structural merge engine.
- Returns: clean/conflict status, affected entities, conflict details.
- Orchard uses this for PR merge checks.

### Extensibility

The existing capability negotiation (`Graft-Capabilities: pack,zstd,sideband`) supports additive features. New capabilities (e.g., `shallow`, `filter`, `lfs`, `entity-fetch`) are negotiated per-connection. Old clients/servers gracefully degrade.

---

## 6. Implementation Priority

### Phase 1: Foundation (git compat + core workflow)
1. `pkg/gitcompat/` — Native .git format read/write
2. `graft stash` — Essential workflow command
3. `graft fetch` — Decouple from pull
4. `graft cherry-pick` — Commit-level porcelain
5. Auto-signing during auth setup
6. Client-side hooks infrastructure

### Phase 2: Rebase & Scale
7. `graft rebase` (non-interactive)
8. `graft rebase -i` (interactive)
9. Shallow clone support (protocol + client)
10. Sparse checkout
11. Commit-graph file
12. `.graftattributes` support

### Phase 3: Enterprise Features
13. Graft LFS (client-side)
14. `graft bisect`
15. `graft worktree`
16. Entity-level audit in reflog
17. Protocol v2 extensions
18. Zero-config signature verification via Orchard

### Phase 4: Polish
19. `graft clean`, `graft grep`, `graft shortlog`, `graft archive`
20. Performance tuning (benchmarks, profiling)
21. Comprehensive documentation
22. Compatibility test suite (roundtrip with git)

---

## Non-Goals (For This Design)

- **Orchard server implementation** — Separate project.
- **Web UI** — Part of Orchard.
- **IDE plugins** — Future work after CLI is stable.
- **Submodules** — Controversial even in git; defer unless demanded.
