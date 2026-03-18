# Phase 4: Polish — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add remaining convenience commands (clean, grep, shortlog, archive), and a git round-trip compatibility test suite.

**Architecture:** Each command is a thin CLI layer over pkg/repo methods. The compatibility test suite verifies graft can produce objects and refs that git can read.

**Tech Stack:** Go 1.25, cobra CLI, os/exec for git interop tests.

---

## Task 1: `graft clean` — Core & CLI

**Files:**
- Create: `pkg/repo/clean.go`
- Create: `pkg/repo/clean_test.go`
- Create: `cmd/graft/cmd_clean.go`
- Modify: `cmd/graft/main.go`

### API

- `Clean(opts CleanOptions) ([]string, error)` — Remove untracked files, return list of removed paths
- `CleanDryRun(opts CleanOptions) ([]string, error)` — List files that would be removed without removing them

```go
type CleanOptions struct {
    Directories bool // -d: also remove untracked directories
    Force       bool // -f: required to actually delete (safety)
    IgnoredOnly bool // -x: remove ignored files instead of untracked
    IgnoredToo  bool // -X: remove both untracked and ignored
}
```

### Algorithm

1. Walk working tree (same as Status walk)
2. Collect files not in staging index and not ignored (unless -x/-X)
3. If Force, remove them; otherwise, return error asking for -f
4. If Directories, remove empty untracked directories too

### CLI

- `graft clean` — Error without -f (safety)
- `graft clean -f` — Remove untracked files
- `graft clean -fd` — Remove untracked files and directories
- `graft clean -n` — Dry run (list what would be removed)
- `graft clean -fx` — Remove only ignored files
- `graft clean -fX` — Remove ignored files too

### Tests

1. `TestClean_RemovesUntrackedFiles` — Add untracked files, clean, verify removed
2. `TestClean_RequiresForce` — Error without -f flag
3. `TestClean_DryRun` — Lists files but doesn't remove them
4. `TestClean_PreservesTrackedFiles` — Tracked files are never removed
5. `TestClean_Directories` — With -d, removes empty untracked directories

---

## Task 2: `graft grep` — Core & CLI

**Files:**
- Create: `pkg/repo/grep.go`
- Create: `pkg/repo/grep_test.go`
- Create: `cmd/graft/cmd_grep.go`
- Modify: `cmd/graft/main.go`

### API

```go
type GrepResult struct {
    Path    string
    Line    int
    Content string
}

type GrepOptions struct {
    Pattern       string
    CaseInsensitive bool
    FixedString     bool   // -F: literal string match
    PathPattern     string // filter to specific paths
}
```

- `Grep(opts GrepOptions) ([]GrepResult, error)` — Search tracked files (from staging index) for pattern matches

### Algorithm

1. Read staging index to get list of tracked files
2. For each file, read content from disk (working tree version)
3. Search line by line for regexp (or fixed string) match
4. Return matches sorted by path then line number

### CLI

- `graft grep <pattern> [<pathspec>...]` — Search tracked files
- `graft grep -i <pattern>` — Case insensitive
- `graft grep -F <pattern>` — Fixed string (not regex)
- `graft grep -n <pattern>` — Show line numbers (default)

Output: `<path>:<line>:<content>`

### Tests

1. `TestGrep_FindsMatch` — Basic pattern match
2. `TestGrep_CaseInsensitive` — Case insensitive flag
3. `TestGrep_FixedString` — Literal match (no regex)
4. `TestGrep_NoMatch` — No results for non-matching pattern
5. `TestGrep_PathFilter` — Only searches specified paths

---

## Task 3: `graft shortlog` — Core & CLI

**Files:**
- Create: `pkg/repo/shortlog.go`
- Create: `pkg/repo/shortlog_test.go`
- Create: `cmd/graft/cmd_shortlog.go`
- Modify: `cmd/graft/main.go`

### API

```go
type ShortlogEntry struct {
    Author string
    Count  int
    Titles []string // commit message first lines
}
```

- `Shortlog(opts ShortlogOptions) ([]ShortlogEntry, error)` — Summarize commit log by author

```go
type ShortlogOptions struct {
    Summary  bool // -s: only show counts
    Numbered bool // -n: sort by count descending (default: sort by author)
    Limit    int  // max commits to walk (0 = all)
}
```

### Algorithm

1. Walk commit history from HEAD (first-parent only)
2. Group commits by author
3. Sort by count descending (-n) or author name alphabetically

### CLI

- `graft shortlog` — Full shortlog with commit titles
- `graft shortlog -s` — Summary: just counts
- `graft shortlog -sn` — Summary sorted by count

Output (full):
```
Author Name (3):
      commit message 1
      commit message 2
      commit message 3
```

Output (summary): `     3  Author Name`

### Tests

1. `TestShortlog_GroupsByAuthor` — Multiple authors grouped correctly
2. `TestShortlog_SortByCount` — -n sorts by commit count descending
3. `TestShortlog_Summary` — -s only returns counts, no titles

---

## Task 4: `graft archive` — Core & CLI

**Files:**
- Create: `pkg/repo/archive.go`
- Create: `pkg/repo/archive_test.go`
- Create: `cmd/graft/cmd_archive.go`
- Modify: `cmd/graft/main.go`

### API

```go
type ArchiveOptions struct {
    Format string // "tar" or "zip"
    Prefix string // optional path prefix inside archive
}
```

- `Archive(w io.Writer, treeish string, opts ArchiveOptions) error` — Write archive of tree to writer

### Algorithm

1. Resolve treeish to commit hash, get tree hash
2. Flatten tree to get all files
3. For tar: write tar entries (with optional prefix)
4. For zip: write zip entries

### CLI

- `graft archive [--format=tar|zip] [--prefix=<prefix>/] <tree-ish>` — Write archive to stdout
- `graft archive --format=zip HEAD > release.zip`

### Tests

1. `TestArchive_Tar` — Create tar, verify it contains expected files
2. `TestArchive_Zip` — Create zip, verify it contains expected files
3. `TestArchive_Prefix` — Files have correct prefix in archive
4. `TestArchive_SpecificCommit` — Archive a specific commit (not HEAD)

---

## Task 5: Git Compatibility Test Suite

**Files:**
- Create: `pkg/repo/compat_test.go`

### Tests

These tests require `git` to be installed. Skip with `testing.Short()` or check for git binary.

1. `TestCompat_InitCreatesValidRepo` — Run `graft init`, then verify `git status` doesn't error in the same directory (using .git bridge)
2. `TestCompat_CommitFormat` — Create a commit with graft, verify the commit object can be read and the format is valid
3. `TestCompat_BranchAndTag` — Create branches and tags with graft, verify they are valid ref format
4. `TestCompat_TreeRoundTrip` — Write files, commit with graft, verify tree content matches

These are basic format-validity tests rather than full git interop (since graft uses its own object format with SHA-256, not git's SHA-1 format).

---

## Implementation Order

Task 1 (clean) → Task 2 (grep) → Task 3 (shortlog) → Task 4 (archive) → Task 5 (compat tests)
