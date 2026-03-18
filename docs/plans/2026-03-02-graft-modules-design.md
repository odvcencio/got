# Graft Modules Design

**Goal:** Replace git submodules with a zero-ceremony, branch-tracking, merge-aware module system that makes multi-repo development painless.

**Architecture:** Modules are declared in `.graftmodules`, locked in `.graftmodules.lock`, and share the parent repo's object store. Module working trees appear as subdirectories, auto-fetched on clone, auto-updated on checkout, with full bidirectional commit/push support.

---

## 1. Data Model

### `.graftmodules` (committed, human-edited)

```
[module "ui-kit"]
  url = github:myorg/ui-kit
  path = vendor/ui-kit
  track = main

[module "proto"]
  url = orchard:myorg/proto
  path = lib/proto
  pin = v2.3.0
```

Fields per module:
- **name** — unique identifier (bracket key)
- **url** — remote URL, supports graft shorthand (`orchard:`, `github:`, full URLs)
- **path** — working tree location relative to repo root
- **track** — branch to follow (mutually exclusive with pin)
- **pin** — tag or commit to lock to (mutually exclusive with track)

### `.graftmodules.lock` (committed, auto-generated)

```json
{
  "modules": {
    "ui-kit": {
      "commit": "abc123def456...",
      "url": "https://github.com/myorg/ui-kit.git",
      "track": "main"
    },
    "proto": {
      "commit": "789def012345...",
      "url": "https://orchard.example.com/myorg/proto",
      "pin": "v2.3.0"
    }
  }
}
```

The lock file records the exact resolved commit for reproducible builds. Branch tracking provides convenience; the lock provides reproducibility.

### Tree representation

Modules appear as tree entries with mode `160000` (gitlink). The entry's BlobHash stores the module's pinned commit hash. This makes module versions part of the commit DAG integrity.

```go
TreeModeModule = "160000"
```

---

## 2. Object Store & Layout

### On-disk structure

```
project/
├── .graft/
│   ├── objects/              # shared — all module objects live here
│   ├── modules/
│   │   ├── ui-kit/
│   │   │   ├── HEAD          # current module commit
│   │   │   ├── refs/         # module remote tracking refs
│   │   │   └── shallow       # if fetched with --depth
│   │   └── proto/
│   │       ├── HEAD
│   │       ├── refs/
│   │       └── shallow
│   └── refs/                 # parent repo refs (unchanged)
├── .graftmodules
├── .graftmodules.lock
├── vendor/ui-kit/            # module working tree
│   ├── .graft -> ../../.graft/modules/ui-kit
│   └── src/...
└── lib/proto/
    ├── .graft -> ../../.graft/modules/proto
    └── api/...
```

Design decisions:
- **Shared object store** — one store, one GC, one fetch pipeline. Module objects are just more objects.
- **Module metadata under `.graft/modules/<name>/`** — HEAD, refs, shallow state. Separate namespace from parent refs.
- **Module working trees auto-ignored** — added to `.graftignore` so parent doesn't track module files.
- **Symlink `.graft`** inside module dirs — so graft commands work naturally from inside a module path. Same pattern as linked worktrees.

### Repo struct integration

```go
type Module struct {
    Name    string
    URL     string
    Path    string        // relative to parent root
    Track   string        // branch name, or ""
    Pin     string        // tag/commit, or ""
    Commit  object.Hash   // resolved from lock file
    Store   *object.Store // shared pointer to parent's store
}
```

The module reuses the parent's `object.Store`. Fetched module objects go into the shared store. Checkout reads from the same store.

---

## 3. CLI Commands

```
graft module add <url> [<path>]     # add, fetch, checkout
graft module rm <name>              # remove module and working tree
graft module update [<name>...]     # fetch latest, update lock
graft module sync                   # ensure working trees match lock
graft module status                 # show state vs lock vs upstream
graft module list                   # list modules with paths and versions
```

---

## 4. Lifecycle Flows

### Adding a module

```
$ graft module add github:myorg/ui-kit vendor/ui-kit --track main

  added module "ui-kit" -> vendor/ui-kit (tracking main)
  fetched 47 objects
  checked out abc123d (main: "Add button component")
```

Steps:
1. Parse URL, resolve shorthand
2. Write entry to `.graftmodules`
3. Fetch objects into shared store
4. Write resolved commit to `.graftmodules.lock`
5. Create `.graft/modules/ui-kit/` metadata dir
6. Checkout module tree into `vendor/ui-kit/`
7. Add path to `.graftignore`
8. Add tree entry (mode 160000) to staging

### Cloning with modules

```
$ graft clone orchard:myorg/project

  cloning project...
  fetching module ui-kit (main: abc123d)...
  fetching module proto (v2.3.0: 789def0)...
  ready. 2 modules synced.
```

Steps:
1. Normal clone
2. Detect `.graftmodules` + `.graftmodules.lock`
3. For each module: fetch objects, checkout at locked commit
4. Recursive: if a module has its own `.graftmodules`, repeat
5. Use `--no-modules` to skip

### Checking out a branch

```
$ graft checkout feature-branch

  switched to feature-branch
  module ui-kit: abc123d -> def456a (main: "Fix hover state")
  module proto: unchanged
```

Steps:
1. Normal checkout
2. Read new branch's `.graftmodules.lock`
3. Diff against current module state
4. Changed modules: fetch if needed, checkout new commit
5. Removed modules: delete working tree and metadata
6. Added modules: fetch, checkout, create metadata

### Updating modules

```
$ graft module update ui-kit

  ui-kit: abc123d -> ff9900a (main: 3 new commits)
  updated .graftmodules.lock
  run 'graft commit' to record the update
```

Steps:
1. Fetch latest refs from module remote
2. Resolve track/pin to new commit
3. Update lock file
4. Checkout new version
5. Stage lock file change (user commits when ready)

### Module status

```
$ graft module status

  ui-kit    vendor/ui-kit   main     abc123d  (2 behind, 1 local commit)
  proto     lib/proto       v2.3.0   789def0  (up to date)
```

---

## 5. Bidirectional Development

Modules are full repos sharing the object store. Developers commit and push from within:

```
$ cd vendor/ui-kit
$ graft commit -m "fix hover state"    # commits to module's HEAD
$ graft push                           # pushes to module's remote
$ cd ../..
$ graft module update ui-kit           # updates lock to include your commit
$ graft commit -m "update ui-kit"      # records in parent
```

The `.graft` symlink makes graft commands find module state. Module HEAD, refs, and remote config live under `.graft/modules/<name>/`.

---

## 6. Module-Aware Merge

When merging branches with different module versions:

```
$ graft merge feature-branch

  merging feature-branch into main...
  module ui-kit: v2.3 -> v2.5 (auto-resolved: newer wins)
  merge completed cleanly
```

Resolution rules:
- **One side changed**: take that side (trivial 3-way)
- **Both sides changed**: take the commit with higher generation number (newer wins). Generation numbers already computed by the merge base system.
- **Both sides changed tracking branch**: conflict only if they diverge to different branches. Same branch, different commits: newer wins.
- **Divergent branch tracking** (track changed from `main` to `develop`): conflict — user must choose.

The merge engine detects mode `160000` tree entries and applies module merge logic instead of blob merge. No conflict markers in files — either a clean resolution or a named conflict: `CONFLICT (module): ui-kit changed to abc123 in ours and def456 in theirs`.

---

## 7. Recursive Modules

Modules can have their own `.graftmodules`. Transitive dependencies are fetched depth-first.

```
project/
├── .graftmodules              # declares A
├── vendor/A/
│   ├── .graftmodules          # declares B
│   └── lib/B/
│       ├── .graftmodules      # declares C
│       └── vendor/C/
```

- **Fetch order**: depth-first. Fetch A, read A's modules, fetch B, read B's, fetch C. All objects into shared store.
- **Cycle detection**: track visited module URLs. Same URL twice in chain: `module cycle detected: A -> B -> A`
- **Depth limit**: default max 10. Configurable via `--max-depth`.

---

## 8. Edge Cases

| Situation | Behavior |
|-----------|----------|
| Module remote unreachable during clone | Clone succeeds with warning, module dir empty. `graft module sync` retries. |
| Module has dirty working tree on checkout | Refuse checkout: `module ui-kit has uncommitted changes` |
| Module deleted from .graftmodules | Checkout removes working tree and metadata dir |
| Module path conflicts with existing file | `graft module add` errors: `path vendor/ui-kit already exists` |
| Two modules claim same path | Parse error: `duplicate path vendor/ui-kit` |
| Shallow clone of parent | Modules fetched at same depth (or `--module-depth` override) |
| Worktrees with modules | Each worktree gets own module working trees, shared object store via CommonDir |

---

## 9. Testing Strategy

### Unit tests (pkg/repo/)

- Module config parsing: read/write/round-trip/malformed/duplicates
- Tree mode 160000: BuildTree, FlattenTree, tree lookup with module entries
- Module merge logic: both-changed-newer-wins, one-side, divergent-conflict, generation comparison
- Cycle detection: A->B->A errors, A->B->C works, depth limit
- Module status: behind/ahead/dirty/up-to-date

### Integration tests (pkg/repo/)

- Add + checkout round-trip
- Clone with modules (auto-fetch)
- Recursive fetch (parent -> child -> grandchild)
- Bidirectional commit and push
- Branch switch updates modules
- Merge with module version changes

### CLI tests (cmd/graft/)

- `graft module add/rm/update/sync/status/list` output and exit codes
- `graft clone` with modules end-to-end
- `graft checkout` triggers module sync
