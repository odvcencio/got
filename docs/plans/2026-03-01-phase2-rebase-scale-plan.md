# Phase 2: Rebase & Scale — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add rebase (non-interactive and interactive), shallow clone support, sparse checkout, commit-graph acceleration, and .graftattributes.

**Architecture:** Rebase uses a sequencer (state machine persisted in `.graft/rebase-merge/`) that replays commits via CherryPick. Interactive rebase opens an editor for the todo list. Scale features add negotiation parameters to the existing protocol and new index files.

**Tech Stack:** Go 1.25, existing merge/cherry-pick engine, cobra CLI.

---

## Task 1: Non-Interactive Rebase — Core Library

**Files:**
- Create: `pkg/repo/rebase.go`
- Create: `pkg/repo/rebase_test.go`

### Rebase Algorithm

`Rebase(upstream string) error`:
1. Resolve upstream to a commit hash
2. Resolve HEAD to a commit hash
3. Find merge base between HEAD and upstream
4. Collect commits from merge-base..HEAD (the commits to replay)
5. For each commit in order (oldest first):
   - Cherry-pick onto the current tip (starting from upstream)
   - If conflict: save sequencer state, return error
6. Update branch ref to point to the new tip
7. Clean up sequencer state

### Sequencer State

Persisted in `.graft/rebase-merge/`:
- `head-name` — branch being rebased (e.g., `refs/heads/feature`)
- `orig-head` — original HEAD hash before rebase
- `onto` — the commit we're rebasing onto
- `todo` — remaining commits to replay (one hash per line)
- `done` — commits already replayed
- `stopped-sha` — commit that caused a conflict (if any)

### Continue/Abort/Skip

- `RebaseContinue() error` — After user resolves conflicts: commit the resolution, continue replay
- `RebaseAbort() error` — Reset HEAD and branch to orig-head, delete sequencer state
- `RebaseSkip() error` — Skip the conflicting commit, continue with next

### Tests

1. `TestRebase_LinearReplay` — Replay 3 commits onto upstream, verify all appear
2. `TestRebase_ConflictStopsAndContinues` — Conflict pauses, continue after resolve
3. `TestRebase_Abort` — Abort restores original state
4. `TestRebase_Skip` — Skip conflicting commit, continue
5. `TestRebase_AlreadyUpToDate` — No-op when HEAD is descendant of upstream
6. `TestRebase_Onto` — `--onto` rebases onto arbitrary point

---

## Task 2: Non-Interactive Rebase — CLI

**Files:**
- Create: `cmd/graft/cmd_rebase.go`
- Modify: `cmd/graft/main.go`

### Commands

- `graft rebase <upstream>` — Start rebase
- `graft rebase --onto <newbase> <upstream>` — Rebase onto arbitrary point
- `graft rebase --continue` — Continue after conflict resolution
- `graft rebase --abort` — Abort and restore original state
- `graft rebase --skip` — Skip conflicting commit

---

## Task 3: Interactive Rebase — Core Library

**Files:**
- Modify: `pkg/repo/rebase.go` (add interactive support)
- Create: `pkg/repo/rebase_interactive_test.go`

### Interactive Rebase

`RebaseInteractive(upstream string) error`:
1. Collect commits (same as non-interactive)
2. Write todo list to temp file in sequencer format:
   ```
   pick abc1234 first commit message
   pick def5678 second commit message
   ```
3. Open editor ($EDITOR or $VISUAL or "vi")
4. Parse edited todo list
5. Execute actions in order

### Todo Actions

- `pick <hash>` — Apply commit as-is
- `reword <hash>` — Apply commit, open editor for new message
- `squash <hash>` — Merge with previous commit, combine messages
- `fixup <hash>` — Merge with previous commit, discard this message
- `edit <hash>` — Apply commit then pause for user amendments
- `drop <hash>` — Skip this commit
- `exec <command>` — Run shell command

### Tests

1. `TestRebaseInteractive_PickAll` — All picks = same as non-interactive
2. `TestRebaseInteractive_DropCommit` — Removing a line drops the commit
3. `TestRebaseInteractive_SquashCombinesMessages` — Squash merges messages
4. `TestRebaseInteractive_FixupDiscardsMessage` — Fixup keeps only first message
5. `TestRebaseInteractive_ParseTodoList` — Parsing handles all action types

---

## Task 4: Interactive Rebase — CLI

**Files:**
- Modify: `cmd/graft/cmd_rebase.go` (add `-i` flag)

### Commands

- `graft rebase -i <upstream>` — Open editor with todo list

---

## Task 5: Commit Graph File

**Files:**
- Create: `pkg/object/commitgraph.go`
- Create: `pkg/object/commitgraph_test.go`
- Modify: `pkg/repo/gc.go` (update commit-graph during gc)

### Commit Graph Format

Binary file at `.graft/objects/info/commit-graph`:
- Header: magic bytes + version + entry count
- Entries (sorted by hash): commit hash, tree hash, parent hashes (up to 2 inline, overflow table for octopus), generation number, commit timestamp
- Fan-out table for fast lookup by first byte

### API

- `WriteCommitGraph(path string, commits []CommitGraphEntry) error`
- `ReadCommitGraph(path string) (*CommitGraph, error)`
- `(*CommitGraph).Lookup(hash Hash) (*CommitGraphEntry, bool)`
- `(*CommitGraph).Generation(hash Hash) uint32`

### Integration

- `graft gc` writes/updates commit-graph
- `FindMergeBase` checks commit-graph for generation numbers before walking

### Tests

1. `TestCommitGraph_WriteAndRead` — Round-trip
2. `TestCommitGraph_Lookup` — Find entry by hash
3. `TestCommitGraph_GenerationNumbers` — Correct generation computation
4. `TestCommitGraph_FanOut` — Fan-out table works for fast lookup

---

## Task 6: Sparse Checkout

**Files:**
- Create: `pkg/repo/sparse.go`
- Create: `pkg/repo/sparse_test.go`
- Modify: `pkg/repo/checkout.go` (respect sparse patterns)
- Modify: `pkg/repo/status.go` (respect sparse patterns)
- Create: `cmd/graft/cmd_sparse_checkout.go`
- Modify: `cmd/graft/main.go`

### API

- `SparseCheckoutSet(patterns []string) error` — Set sparse patterns (cone mode)
- `SparseCheckoutAdd(patterns []string) error` — Add to existing patterns
- `SparseCheckoutDisable() error` — Disable sparse checkout
- `SparseCheckoutList() ([]string, error)` — List current patterns
- `IsSparseEnabled() bool` — Check if sparse checkout is active

### Storage

- `.graft/info/sparse-checkout` — One pattern per line (directory paths for cone mode)

### Integration

- `Checkout` only materializes files matching sparse patterns
- `Status` only reports on files matching sparse patterns
- `Add` only stages files matching sparse patterns (or explicitly named)

### Tests

1. `TestSparseCheckout_OnlyMaterializesMatchingFiles`
2. `TestSparseCheckout_StatusIgnoresExcluded`
3. `TestSparseCheckout_AddPattern`
4. `TestSparseCheckout_Disable`

---

## Task 7: .graftattributes Support

**Files:**
- Create: `pkg/repo/attributes.go`
- Create: `pkg/repo/attributes_test.go`

### API

- `ReadAttributes() (*Attributes, error)` — Parse `.graftattributes` from repo root
- `(*Attributes).Match(path string) map[string]string` — Get attributes for a path

### Attribute Format

```
*.bin filter=lfs diff=lfs merge=lfs
*.proto merge=union
docs/** diff=text
```

Pattern matching uses the same glob engine as `.graftignore`.

### Tests

1. `TestAttributes_ParseFile` — Parse multi-line attributes file
2. `TestAttributes_MatchPattern` — Glob matching returns correct attributes
3. `TestAttributes_PriorityOrder` — Later rules override earlier ones
4. `TestAttributes_EmptyFile` — No attributes is fine

---

## Implementation Order

Tasks 1-2 (rebase) → Task 3-4 (interactive rebase) → Task 5 (commit-graph) → Task 6 (sparse checkout) → Task 7 (.graftattributes)
