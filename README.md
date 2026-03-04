# graft

Structural version control. Merges functions, not lines.

Git treats source files as bags of lines. Two developers add different functions to the same file — conflict. Both add different imports — conflict. One renames a variable, another adds a function nearby — conflict. None of these are real conflicts.

**graft** is a standalone version control system that decomposes source into structural entities via [gotreesitter](https://github.com/odvcencio/gotreesitter) — functions, methods, classes, imports — and merges at that level. Independent additions merge cleanly. Import blocks get set-union merged. Only genuine semantic overlaps produce conflicts.

```
# Git: CONFLICT (both modified main.go)
# Graft: clean merge — two independent functions added

$ graft merge feature
merging feature into main...
  main.go: clean
merge completed cleanly
```

## How it works

Graft parses every source file into an ordered list of **entities**:

| Entity kind | Examples |
|------------|---------|
| Preamble | `package main`, license headers |
| Import block | `import (...)`, `from x import y` |
| Declaration | Functions, methods, types, classes, structs, traits |
| Interstitial | Whitespace and comments between declarations |

Each entity has an **identity key** (e.g. `decl:function_definition::ProcessOrder`) that survives editing, reordering, and branch divergence. Merge operates on these identities instead of line numbers:

- **Unchanged** — keep as-is
- **Modified by one side** — take the modification
- **Modified identically by both** — no conflict
- **Modified differently by both** — diff3 fallback on that entity's body
- **Import blocks** — set-union merge (combine all imports, deduplicate)
- **Added by one side** — insert at correct position
- **Deleted by one side, unchanged by other** — remove
- **Deleted vs modified** — real conflict

The critical invariant: reconstructing entities always reproduces the original source byte-for-byte.

## Install

```bash
go install github.com/odvcencio/graft/cmd/graft@latest
```

Requires Go 1.25+. Pure Go, no C dependencies.

## Usage

Graft follows the same mental model as Git:

```bash
# Initialize a repository
graft init myproject
cd myproject

# Stage and commit
echo 'package main

func Hello() {}
' > main.go
graft add main.go
graft commit -m "initial commit"

# Branch and diverge
graft branch feature
graft checkout feature
# ... add func Goodbye() ...
graft add main.go
graft commit -m "add Goodbye"

# Back to main, make a different change
graft checkout main
# ... add func Greet() ...
graft add main.go
graft commit -m "add Greet"

# Structural merge — no conflict
graft merge feature
```

### Commands

**Core**
```
graft init [path]                     Create a new repository
graft add <files...>                  Stage files for commit
graft commit -m <message>             Record changes
graft status                          Show working tree status
graft diff [ref1..ref2] [--staged] [--entity] [--review] [--json]
                                      Show changes (line-level, entity-level, or review summary)
graft log [--oneline] [-n N] [--entity <selector>]  Show commit history
graft show [commit-ish]               Show commit metadata and changed files
```

**Branching & Merging**
```
graft branch [name] [-d name]        List, create, or delete branches
graft checkout <target> [-b]          Switch branches
graft switch <branch> [-c <new>]      Switch branches (modern alternative to checkout)
graft merge <branch>                  Three-way structural merge
graft rebase [--onto] [-i] <upstream> Reapply commits on a new base (--continue/--abort/--skip/--autostash)
graft cherry-pick [--entity <sel>] <commit>  Cherry-pick a commit or entity (--continue/--abort/--skip)
graft revert <commit>                 Revert a commit by creating an inverse commit (--continue/--abort)
```

**Remote**
```
graft clone <url> [dir]               Clone from Graft/Orchard or Git forge
graft push [remote] [branch]          Push local branch to remote
graft pull [remote] [branch]          Fetch and fast-forward local branch
graft fetch [remote]                  Download objects and refs without merging
graft remote                          Manage remotes (add, remove, list)
graft publish [owner/repo]            Create remote repo on Orchard, set origin, and push
graft auth                            Authenticate with Orchard (setup, ssh-login, bootstrap-ssh, status, logout)
```

**History & Inspection**
```
graft blame [<path>] [--entity <path::key>] [--limit N] [--json]
                                      Structural blame for an entity or every entity in a file
graft bisect start|good|bad|skip|reset|log|run  Binary search for a bug-introducing commit
graft reflog                          Show local ref update history
graft shortlog [-s] [-n]              Summarise commit history by author
graft tag [name]                      List, create, or delete tags
```

**Working Tree**
```
graft clean [-n] [-f] [-d]            Remove untracked files from the working tree
graft grep [-i] [-F] [--entity] [--kind <kind>] [--json] <pattern>
                                      Search file content or entity names for a pattern
graft stash [push|pop|apply|list|drop|show]  Stash and restore working directory changes
graft reset [paths...]                Unstage paths (restore index from HEAD)
graft rm [--cached] <paths...>        Remove paths from index and/or working tree
graft sparse-checkout set|add|list|disable  Manage sparse checkout patterns
graft worktree add|list|remove|prune  Manage multiple linked working trees
```

**Modules**
```
graft module add <url> [path]         Add a module (--track <branch> or --pin <tag>)
graft module rm <name>                Remove a module and its working tree
graft module update [name...]         Fetch latest objects for modules (--depth N)
graft module sync                     Sync module working trees from lock file
graft module status                   Show module state vs lock vs upstream
graft module list                     List configured modules with paths and versions
```

**Large Files**
```
graft lfs track <pattern>             Track files matching pattern with LFS
graft lfs untrack <pattern>           Stop tracking pattern with LFS
graft lfs ls-files                    List LFS-tracked files in staging
graft lfs status                      Show LFS status for tracked files
```

**Archive & Maintenance**
```
graft archive [--format=tar|zip] <tree-ish>  Create an archive of files from a commit
graft gc                              Pack loose objects and prune unreachable data
graft verify [--signatures] [--json]  Verify object integrity and commit signatures
graft version                         Print version
```

### Remote shorthand

Use `orchard:owner/repo` instead of full URLs:

```bash
graft remote add origin orchard:alice/demo
graft clone orchard:alice/demo
graft publish alice/demo
```

### Auth configuration

`graft` supports global auth/config in `~/.graftconfig` (token, default host, owner/username).
Environment variables still override file values.

```bash
# Interactive setup (magic-link login + optional SSH key registration)
graft auth setup --host https://orchard.dev

# Agent-native login (no browser/magic-link flow, uses registered SSH key)
graft auth ssh-login --host https://orchard.dev --username alice --ssh-key ~/.ssh/id_ed25519

# First-key bootstrap for headless agents.
# If already authenticated, graft auto-mints a short-lived bootstrap token.
graft auth bootstrap-ssh --host https://orchard.dev --username alice --ssh-key ~/.ssh/id_ed25519

# First-time from terminal (no prior auth token):
# requests magic-link auth, verifies, mints bootstrap token, registers key.
graft auth bootstrap-ssh --host https://orchard.dev --email alice@example.com --username alice --ssh-key ~/.ssh/id_ed25519

# Optional explicit token override for automation:
GRAFT_BOOTSTRAP_TOKEN=... graft auth bootstrap-ssh --host https://orchard.dev --username alice --ssh-key ~/.ssh/id_ed25519

# Inspect stored auth state
graft auth status
```

Git forge shorthand is also supported:

```bash
graft clone github:owner/repo
graft clone gitlab:group/subgroup/repo
graft clone bitbucket:workspace/repo
```

For Git-forge clones, `graft` bootstraps a local `.graft` repository from the cloned Git HEAD snapshot.

For self-hosted instances, set `GRAFT_ORCHARD_URL`:

```bash
export GRAFT_ORCHARD_URL=https://code.example.com
graft remote add origin orchard:alice/demo
```

When a remote is a Git forge URL, `graft` routes `clone/pull/push` through Git transport; Orchard remotes continue to use native Graft transport.
`graft clone` from a Git forge bootstraps `.graft` from the cloned Git HEAD snapshot so structural workflows can start immediately.

### Structural diff

```bash
# Line-level diff (default)
graft diff

# Entity-level diff — shows which functions/types changed
graft diff --entity

# Review summary — declaration-level changes only, good for PR review
graft diff --review

# Diff between two branches or commits
graft diff main..feature
graft diff main..feature --entity

# JSON output for tooling (pairs with --entity or ref range)
graft diff --json
graft diff main..feature --json
```

## Architecture

```
.graft/
  HEAD                    ref: refs/heads/main
  objects/                SHA-256 content-addressed store (2-char fan-out)
  refs/heads/             Branch tips
  index                   Staging area
```

**Object types:** blob, entity, entitylist, tree, commit

**Hashing:** SHA-256 with type-length envelope (`type len\0content`)

### Packages

| Package | Purpose |
|---------|---------|
| `pkg/object` | Content-addressed store with atomic writes and pack files |
| `pkg/entity` | Tree-sitter entity extraction and reconstruction |
| `pkg/diff3` | Myers diff + three-way line merge |
| `pkg/diff` | Entity-level diff computation |
| `pkg/merge` | Structural three-way merge orchestrator |
| `pkg/repo` | Repository operations (init, commit, branch, checkout, merge, rebase, stash, bisect, ...) |
| `pkg/remote` | Remote sync, pack transport, and protocol client |
| `pkg/userconfig` | Global user configuration (`~/.graftconfig`) |

## Language support

Graft uses [gotreesitter](https://github.com/odvcencio/gotreesitter), a pure-Go tree-sitter runtime with 205 embedded grammars. Entity extraction is tested against:

- Go
- Python
- Rust
- TypeScript
- C

Any language with a tree-sitter grammar can be parsed. Declaration classification is extensible via node type maps.

## Status

Active development. 800+ tests passing across core packages. Structural merge is production-grade for supported scenarios, with pack files, object verification, remote sync, and entity-aware history workflows.

What exists:
- Content-addressed object store (SHA-256)
- Entity extraction via tree-sitter (205 languages)
- Three-way structural merge with entity-level resolution
- Set-union import merging
- Entity-level, line-level, and review-summary diff (`--entity`, `--review`)
- Branch-to-branch diff (`graft diff ref1..ref2`) with entity and JSON output
- Pack files with delta support (`graft gc`) and repository verification (`graft verify --json`)
- Full CLI: 39 commands covering core workflows, branching, remotes, history, working tree, modules, LFS, and maintenance
- Stash workflow (push, pop, apply, list, drop, show)
- Rebase (standard, `--onto`, interactive, `--autostash`, conflict resolution with `--continue`/`--abort`/`--skip`)
- Cherry-pick at commit level and entity level (`--entity`), with `--continue`/`--abort`/`--skip`
- Revert with conflict resolution (`--continue`/`--abort`)
- Bisect with automated script runner (`bisect run`)
- Modules (`.graftmodules` + `.graftmodules.lock`) with branch tracking, shared object store, bidirectional development, merge-aware version resolution, and recursive fetch
- Multiple worktrees, sparse checkout, clean, shortlog, archive
- Batch blame: `graft blame <path>` attributes every entity in a file (`--json` for tooling)
- Entity search: `graft grep --entity <pattern>` finds entities by name across the repo (`--kind`, `--json`)
- SSH challenge/response auth for Orchard remotes
- Git forge clone support (GitHub, GitLab, Bitbucket shorthand)
- Large file storage (LFS) with pattern-based tracking
- `.graftignore` support

## Dependencies

- [gotreesitter](https://github.com/odvcencio/gotreesitter) — Pure-Go tree-sitter runtime (205 languages, no CGo)
- [cobra](https://github.com/spf13/cobra) — CLI framework
- [klauspost/compress](https://github.com/klauspost/compress) — Zstd compression for pack transport
- [golang.org/x/crypto](https://pkg.go.dev/golang.org/x/crypto) — SSH key parsing and challenge/response auth

## License

MIT
