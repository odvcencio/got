# Changelog

## v0.7.0

### Full Git Interop

Graft is now a drop-in companion to git. Every graft repo is also a git repo by default. `gh`, GitHub Actions, GitLab CI, and collaborators work without knowing graft exists.

**Dual-track init** — `graft init` creates both `.graft/` and `.git/`. Existing git repos get a bridge as before. Use `--no-git` to opt out.

**Full git shadow** — Every graft operation that mutates refs, staging, or the working tree shadows the equivalent git operation immediately. Branch, tag, checkout, stash, merge, rebase, reset — all mirrored. Git is the safety net; graft is authoritative.

**Shadow failure policy** — If a git shadow operation fails, graft warns but doesn't block. Failures are logged to `.graft/shadow-failures.log`. `graft status` shows a desync indicator. `graft repair resync-git` force-syncs git to match graft.

### Analysis Sidecar

The `.gts/` directory (owned by gts-suite) is automatically committed in graft trees without being staged. Fresh clones and branch switches restore it from the committed tree. CI and collaborators get analysis artifacts for free.

**Pre-commit-analysis hook** — New hook point runs before tree build during commit. gts-suite can register a hook to refresh `.gts/` incrementally. Failures are non-blocking.

**Sidecar injection** — `BuildTree` walks `.gts/` at commit time and injects its contents into the tree. Checkout restores sidecars from the target branch's tree. Stale sidecars are cleaned on switch.

### OOM Protection for `graft add`

- **File size limit** — Files above 100MB are rejected (configurable via `GRAFT_MAX_FILE_SIZE_MB`). Stat before read prevents unbounded allocation.
- **Binary detection** — Null-byte check in first 8KB. Binary files get blobs but skip entity extraction entirely.
- **Entity extraction cap** — Files above 10MB skip tree-sitter parsing to prevent runaway AST allocation.
- **No content retention** — Phase 1 content is released immediately; Phase 2 re-reads from blob store.

### Coordd: Governed Execution

Multi-agent coordination daemon with policy-driven execution.

- **Spawn lifecycle** — Lease-based and detached launch modes with heartbeat, finish, and wait commands.
- **Policy runtime** — File-based policy loading with caching, hot-reload, and governance tracing.
- **Bubblewrap sandboxing** — Container invocation support with environment and path rewriting.
- **Execution tracing** — Unified spawn/exec audit logging with phase grouping.
- **Post-action effects** — Policy rules that trigger after command execution.

### Coordination Features

- **Shared notes** — `graft coord note` for scratch, handoff, status, and decision notes across agents.
- **Activity feed** — Feed events for agent and claim lifecycle, with publishing and tracking.
- **Plans** — Multi-step coordination plans stored in `refs/coord/plans/`.
- **Process governance** — Centralized `RunExternalProcess` with guard and executor support across all git/hook/rebase operations.

### Other

- `graft status -s` — Short format output matching git's porcelain style.
- `graft check-ignore` — Improved git interop for ignore rule explanation.
- Arbiter API migrated from `CompileResult` to `Program` type.

### New Environment Variables

- `GRAFT_MAX_FILE_SIZE_MB` — Maximum file size for `graft add` (default: 100)
- `GRAFT_COORD_AGENT_ID` — Override agent identity from environment

### Requires

- gotreesitter v0.7.0+
- arbiter v0.0.0 (local)

## v0.6.0

Coordination daemon foundation, activity feed design, and governed process execution. See commit history for details.

## v0.5.0

### Hooks Engine

Declarative hooks system that replaces git's hidden `.git/hooks/` scripts with committed, configurable, entity-aware hooks.

**`hooks.toml`** — Committed with the repo. Repo hooks are mandatory and cannot be disabled by users. User hooks in `~/.graftconfig` extend repo hooks.

```toml
[pre-commit.lint]
run = "golangci-lint run --new-from-rev HEAD"
on-fail = "abort"

[post-push.mirror]
type = "mirror"
remote = "github"
```

**Structured JSON payloads** — Hooks receive rich context on stdin: staged files, entity diffs, commit hashes, ref updates. A pre-commit hook can see exactly which functions changed signatures.

**Hook points:** pre-commit, post-commit, pre-push, post-push. Pre-hooks can abort the operation. Post-hooks run all handlers even if one fails.

**Built-in types:**
- `mirror` — Push to a git remote after graft push. Makes graft the source of truth with GitHub as a read-only mirror.

**Timeout support** — Set `timeout = "120s"` per hook. Hooks without a timeout run until completion.

### Requires

- gotreesitter v0.7.0+

## v0.4.0

### Memory-Safe Entity Extraction

Entity extraction during `graft add` has been completely reworked for reliability. Large repositories that previously caused out-of-memory crashes now complete safely.

**Two-phase add pipeline** — Blob staging (parallel, I/O-bound) runs separately from entity extraction (bounded concurrency). File contents are released between phases to minimize memory pressure.

**Data format denylist** — Pure data files (JSON, YAML, TOML, INI, CSV) above 256KB are automatically skipped for entity extraction. Small config files like `package.json` still get entities. All code files are always extracted regardless of size.

**Parser pool reuse** — Entity extraction now uses `ParseFilePooled` with per-language parser pools, eliminating repeated parser allocation overhead during bulk operations.

**New CLI flags:**
- `--skip-entities` — Skip all entity extraction (fast bulk import)
- `--force-entities` — Force extraction on data format files above threshold

**New environment variables:**
- `GRAFT_ENTITY_WORKERS` — Max concurrent entity extraction workers (default: 2)
- `GRAFT_ENTITY_MEMORY_MB` — Source-bytes semaphore budget in MB (default: 64)

### Git Bridge Fix

`graft init` in a git repository no longer runs entity extraction during the initial import. This was loading WASM grammars for every git-tracked file and discarding the results, causing multi-GB memory usage and minutes-long init times. Init now completes in under 2 seconds.

### Requires

- gotreesitter v0.7.0+

## v0.3.0

Initial public release with structural merge, entity-aware diff, coordination protocol, and git bridge.
