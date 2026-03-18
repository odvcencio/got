# Phase 3: Enterprise Features — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add LFS client support, bisect, worktree, entity-level reflog audit, protocol v2 extensions, and zero-config signature verification.

**Architecture:** LFS replaces large blobs with pointer files detected via `.graftattributes`, stored in `.graft/lfs/objects/`. Bisect uses binary search on commit history with state in `.graft/bisect/`. Worktree creates linked working trees sharing `.graft/objects/` with separate HEAD/index. Protocol v2 adds shallow/filter capabilities. Signing verification reads public keys from `~/.graft/allowed_signers` or Orchard.

**Tech Stack:** Go 1.25, existing merge/cherry-pick/attributes engine, cobra CLI.

---

## Task 1: Graft LFS — Core Library

**Files:**
- Create: `pkg/repo/lfs.go`
- Create: `pkg/repo/lfs_test.go`

### LFS Pointer Format

```
version graft-lfs/1
oid sha256:<hex-hash>
size <bytes>
```

### API

- `LFSPointer` struct: `Version string`, `OID string`, `Size int64`
- `ParseLFSPointer(data []byte) (*LFSPointer, bool)` — Parse pointer file content; returns nil,false if not a pointer
- `WriteLFSPointer(oid string, size int64) []byte` — Generate pointer file bytes
- `IsLFSTracked(path string) bool` — Check `.graftattributes` for `filter=lfs`
- `StoreLFSObject(data []byte) (oid string, err error)` — Write content to `.graft/lfs/objects/<oid[:2]>/<oid[2:]>`, return SHA-256 OID
- `ReadLFSObject(oid string) ([]byte, error)` — Read content from LFS store
- `LFSObjectPath(oid string) string` — Return filesystem path for an OID
- `LFSStatus() ([]LFSFileStatus, error)` — List tracked LFS files with their pointer/content status

### Storage Layout

```
.graft/lfs/objects/<oid[:2]>/<oid[2:]>  — actual file content
```

### Integration Points (for later tasks)

- During `Add`: if path matches `filter=lfs` in attributes, store content in LFS and replace blob with pointer
- During `Checkout`: if blob is a pointer, fetch LFS content and write that instead

### Tests

1. `TestLFS_WriteAndParsePointer` — Round-trip pointer format
2. `TestLFS_InvalidPointer` — Non-pointer data returns false
3. `TestLFS_StoreAndReadObject` — Store content, read it back
4. `TestLFS_IsLFSTracked` — Checks `.graftattributes` for `filter=lfs`
5. `TestLFS_ObjectPath` — Verify fan-out directory structure

---

## Task 2: Graft LFS — CLI & Integration

**Files:**
- Create: `cmd/graft/cmd_lfs.go`
- Modify: `cmd/graft/main.go`
- Modify: `pkg/repo/staging.go` (add LFS pointer replacement during Add)
- Modify: `pkg/repo/checkout.go` (restore LFS content during checkout)

### CLI Commands

- `graft lfs track <pattern>` — Add `<pattern> filter=lfs diff=lfs merge=lfs` to `.graftattributes`
- `graft lfs untrack <pattern>` — Remove pattern from `.graftattributes`
- `graft lfs ls-files` — List LFS-tracked files
- `graft lfs status` — Show LFS pointer/content status

### Integration

- In `Add` (staging.go): after computing blob hash, check `IsLFSTracked(path)`. If true, store original content in LFS, create pointer blob, and stage the pointer blob hash instead.
- In `Checkout` (checkout.go): after reading blob data, check `ParseLFSPointer(data)`. If valid pointer, read LFS content and write that to disk.

### Tests

1. `TestLFS_TrackUntrack` — Track a pattern, verify .graftattributes, untrack it
2. `TestLFS_AddStoresPointer` — Add a large file with filter=lfs, verify staged blob is a pointer
3. `TestLFS_CheckoutRestoresContent` — Checkout restores actual content from LFS

---

## Task 3: Bisect — Core Library

**Files:**
- Create: `pkg/repo/bisect.go`
- Create: `pkg/repo/bisect_test.go`

### Bisect Algorithm

Binary search on commit history:
1. User marks a "bad" commit (has the bug) and a "good" commit (doesn't have the bug)
2. Bisect picks the midpoint between good and bad in the commit graph
3. User tests and marks as good or bad
4. Repeat until the first bad commit is found

### State Storage

Persisted in `.graft/bisect/`:
- `bad` — The known bad commit hash
- `good` — One good commit hash per line
- `expected-steps` — Estimated remaining steps
- `log` — Bisect log (one line per action: `# bad: <hash>`, `# good: <hash>`)
- `start-ref` — The branch/ref HEAD was on when bisect started (for reset)

### API

- `BisectStart(bad, good object.Hash) error` — Initialize bisect: validate commits, save state, checkout midpoint
- `BisectGood() error` — Mark current HEAD as good, narrow search
- `BisectBad() error` — Mark current HEAD as bad, narrow search
- `BisectReset() error` — End bisect, restore original HEAD
- `BisectSkip() error` — Skip current commit (can't test), try another nearby
- `BisectLog() ([]string, error)` — Return bisect log lines
- `IsBisecting() bool` — Check if bisect session is active
- `bisectFindMidpoint(bad object.Hash, goods []object.Hash) (object.Hash, int, error)` — Find midpoint commit, return remaining steps estimate

### Midpoint Algorithm

1. Walk backwards from `bad`, collecting all commits reachable from bad but NOT reachable from any `good`
2. The candidate set is these commits (the "suspicious" range)
3. Pick the commit at index len(candidates)/2 (midpoint)
4. Return estimated remaining steps: log2(len(candidates))

### Tests

1. `TestBisect_LinearHistory` — 10 linear commits, bisect finds the right one
2. `TestBisect_StartSavesState` — Verify state files created
3. `TestBisect_ResetRestoresRef` — Reset returns to original branch
4. `TestBisect_AlreadyBisecting` — Error if bisect already in progress
5. `TestBisect_Skip` — Skip advances to another candidate
6. `TestBisect_MidpointSelection` — Verify midpoint is roughly in the middle

---

## Task 4: Bisect — CLI

**Files:**
- Create: `cmd/graft/cmd_bisect.go`
- Modify: `cmd/graft/main.go`

### Commands

- `graft bisect start <bad> <good>` — Start bisect session
- `graft bisect good` — Mark current as good
- `graft bisect bad` — Mark current as bad
- `graft bisect skip` — Skip current commit
- `graft bisect reset` — End session, restore original state
- `graft bisect log` — Print bisect log
- `graft bisect run <script>` — Automated bisect: run script at each step, exit 0 = good, non-zero = bad

### Output Format

```
Bisecting: N revisions left to test (roughly M steps)
[<hash>] <commit message first line>
```

---

## Task 5: Worktree — Core Library

**Files:**
- Create: `pkg/repo/worktree.go`
- Create: `pkg/repo/worktree_test.go`
- Modify: `pkg/repo/repo.go` (add IsWorktree, MainWorktreeDir fields)

### Architecture

Main repo at `/project/.graft/` stores:
- `objects/` — shared across all worktrees
- `worktrees/<name>/` — per-worktree: HEAD, index, refs state

Linked worktree at `/other-path/` has:
- `.graft` file (not directory) containing: `gitdir: /project/.graft/worktrees/<name>`
- Working tree files

The linked worktree's `.graft/worktrees/<name>/` dir contains:
- `HEAD` — separate HEAD for this worktree
- `index` — separate staging index
- `commondir` — path back to main `.graft/` (for shared objects, refs)

### API

- `WorktreeAdd(path, branch string) (*Repo, error)` — Create linked worktree
- `WorktreeList() ([]WorktreeInfo, error)` — List all worktrees
- `WorktreeRemove(name string) error` — Remove worktree (must be clean)
- `WorktreePrune() error` — Remove stale worktree entries
- `IsLinkedWorktree() bool` — True if this repo is a linked worktree

### WorktreeInfo Type

```go
type WorktreeInfo struct {
    Name   string
    Path   string
    Branch string
    Head   object.Hash
    Locked bool
}
```

### Tests

1. `TestWorktree_AddAndList` — Create worktree, list it
2. `TestWorktree_SeparateHEAD` — Worktree has independent HEAD
3. `TestWorktree_SharedObjects` — Commits in worktree visible from main
4. `TestWorktree_Remove` — Remove cleans up directory and registry
5. `TestWorktree_Prune` — Prune removes stale entries

---

## Task 6: Worktree — CLI

**Files:**
- Create: `cmd/graft/cmd_worktree.go`
- Modify: `cmd/graft/main.go`

### Commands

- `graft worktree add <path> [<branch>]` — Create new linked worktree
- `graft worktree list` — List all worktrees
- `graft worktree remove <name>` — Remove a worktree
- `graft worktree prune` — Remove stale worktree entries

---

## Task 7: Entity-Level Reflog Audit

**Files:**
- Modify: `pkg/repo/reflog.go` (add entity tracking)
- Create: `pkg/repo/reflog_entity_test.go`

### Design

Add optional entity metadata to reflog entries. When a ref update happens (commit, merge, rebase, cherry-pick), diff the old and new trees to find which entities changed, and record them.

### New Types & Functions

- `EntityChange` struct: `Path string`, `EntityKey string`, `ChangeType string` (create/modify/delete)
- `ReflogEntryWithEntities` struct: embeds `ReflogEntry`, adds `Entities []EntityChange`
- `appendReflogWithEntities(ref, oldHash, newHash, reason string, entities []EntityChange) error` — Write entity-enriched entry
- `ReadReflogWithEntities(ref string, limit int) ([]ReflogEntryWithEntities, error)` — Read entries with entity data
- `diffTreeEntities(r *Repo, oldTree, newTree object.Hash) ([]EntityChange, error)` — Compute entity-level diff between two tree hashes

### Storage

Entity data appended to reflog line after reason, tab-separated:
```
<old-hash> <new-hash> <timestamp> <reason>\t<entity-key>:<change-type>,<entity-key>:<change-type>,...
```

### Integration

Modify `Commit()` to call `diffTreeEntities` and pass to `appendReflogWithEntities`.

### Tests

1. `TestReflog_EntityTracking` — Commit that adds a function shows entity in reflog
2. `TestReflog_ReadWithEntities` — Parse entity data from reflog lines
3. `TestReflog_NoEntitiesGraceful` — Old-format entries work without entity data

---

## Task 8: Protocol v2 Extensions

**Files:**
- Modify: `pkg/remote/protocol.go` (add new capability constants)
- Create: `pkg/remote/shallow.go`
- Create: `pkg/remote/shallow_test.go`
- Modify: `pkg/remote/sync.go` (shallow fetch support)

### New Capabilities

Add to protocol.go:
- `CapShallow = "shallow"` — Server supports shallow clone
- `CapFilter = "filter"` — Server supports partial clone
- `CapIncludeTag = "include-tag"` — Server includes tag objects
- `CapObjectFormat = "object-format"` — Object hash algorithm negotiation

### Shallow Clone Support

- `ShallowState` struct: `Commits map[object.Hash]bool` (shallow boundaries)
- `ReadShallowFile(graftDir string) (*ShallowState, error)` — Read `.graft/shallow`
- `WriteShallowFile(graftDir string, state *ShallowState) error` — Write `.graft/shallow`
- `IsShallowCommit(state *ShallowState, hash object.Hash) bool` — Check if commit is shallow boundary

### Fetch Integration

In `FetchIntoStore`: if depth > 0, include `Graft-Depth: N` header. Stop walking parents at shallow boundary commits.

### Tests

1. `TestShallow_WriteAndRead` — Round-trip shallow file
2. `TestShallow_IsShallowCommit` — Check boundary detection
3. `TestProtocol_NewCapabilities` — Verify capability constants parse correctly

---

## Task 9: Signature Verification

**Files:**
- Create: `pkg/repo/verify.go`
- Create: `pkg/repo/verify_test.go`
- Modify: `cmd/graft/cmd_verify.go` (enhance existing verify command)

### API

- `VerifyCommitSignature(commitHash object.Hash) (*VerificationResult, error)` — Verify a single commit
- `VerificationResult` struct: `Valid bool`, `SignerKey string`, `Algorithm string`, `Error string`
- `LoadAllowedSigners(path string) (map[string][]byte, error)` — Load `~/.graft/allowed_signers` file
- `VerifyCommitAgainstAllowedSigners(commitHash object.Hash, signers map[string][]byte) (*VerificationResult, error)` — Verify with trust chain

### Allowed Signers Format

```
user@email.com ssh-ed25519 AAAAC3...
other@email.com ssh-ed25519 AAAAC3...
```

### CLI Enhancement

Modify existing `graft verify` command to support:
- `graft verify <commit>` — Verify commit signature
- `graft verify --all` — Verify all commits on current branch
- Output: `Good signature from <key-fingerprint>` or `BAD signature` or `No signature`

### Tests

1. `TestVerify_ValidSignature` — Sign a commit, verify it succeeds
2. `TestVerify_InvalidSignature` — Tamper with signature, verify it fails
3. `TestVerify_UnsignedCommit` — Unsigned commit returns appropriate result
4. `TestVerify_AllowedSigners` — Load and match against allowed signers file

---

## Implementation Order

Tasks 1-2 (LFS) → Tasks 3-4 (bisect) → Tasks 5-6 (worktree) → Task 7 (entity reflog) → Task 8 (protocol v2) → Task 9 (signature verification)
