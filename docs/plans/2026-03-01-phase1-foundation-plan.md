# Phase 1: Foundation — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build the foundation for enterprise graft: standalone fetch, stash, commit-level cherry-pick, client-side hooks, auto-signing, and the beginnings of native git compatibility.

**Architecture:** Each feature follows existing patterns in pkg/repo/ (Repo methods + CAS ref updates + staging sync) with CLI wiring in cmd/graft/ (cobra commands). TDD throughout — write failing test, implement, verify, commit.

**Tech Stack:** Go 1.25, cobra CLI, SHA-256 object store, gotreesitter PR #8 head (`v0.6.1-0.20260311120359-68e85114acc0`), golang.org/x/crypto for SSH signing.

---

## Task 1: Extract `graft fetch` from `pull`

Currently `pull` does fetch+fast-forward inline. Extract fetch as a standalone repo operation and CLI command.

**Files:**
- Create: `pkg/repo/fetch.go`
- Create: `pkg/repo/fetch_test.go`
- Create: `cmd/graft/cmd_fetch.go`
- Modify: `cmd/graft/main.go` (register command)
- Modify: `cmd/graft/cmd_pull.go` (call Fetch internally)

### Step 1: Write the failing test for Fetch

```go
// pkg/repo/fetch_test.go
package repo

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFetchUpdatesRemoteTrackingRefs(t *testing.T) {
	// Setup: create two repos, "remote" and "local"
	remoteDir := t.TempDir()
	localDir := t.TempDir()

	remote, err := Init(remoteDir)
	if err != nil {
		t.Fatal(err)
	}

	// Create a commit in the remote
	writeFile(t, remoteDir, "hello.txt", "hello world")
	if err := remote.Add([]string{"hello.txt"}); err != nil {
		t.Fatal(err)
	}
	remoteHash, err := remote.Commit("initial", "test")
	if err != nil {
		t.Fatal(err)
	}

	// Init local, configure remote pointing to remoteDir
	local, err := Init(localDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := local.SetRemote("origin", remoteDir); err != nil {
		t.Fatal(err)
	}

	// Fetch
	result, err := local.Fetch("origin")
	if err != nil {
		t.Fatal(err)
	}

	// Remote-tracking ref should exist
	trackingRef := "refs/remotes/origin/heads/main"
	got, err := local.ResolveRef(trackingRef)
	if err != nil {
		t.Fatalf("tracking ref %s not found: %v", trackingRef, err)
	}
	if got != remoteHash {
		t.Errorf("tracking ref = %s, want %s", got, remoteHash)
	}

	// Result should report what was fetched
	if len(result.UpdatedRefs) == 0 {
		t.Error("expected at least one updated ref")
	}
}

func TestFetchDoesNotTouchWorkingTree(t *testing.T) {
	remoteDir := t.TempDir()
	localDir := t.TempDir()

	remote, err := Init(remoteDir)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, remoteDir, "hello.txt", "hello")
	remote.Add([]string{"hello.txt"})
	remote.Commit("initial", "test")

	local, err := Init(localDir)
	if err != nil {
		t.Fatal(err)
	}
	local.SetRemote("origin", remoteDir)

	local.Fetch("origin")

	// Working tree should NOT have the file
	if _, err := os.Stat(filepath.Join(localDir, "hello.txt")); !os.IsNotExist(err) {
		t.Error("fetch should not create files in working tree")
	}
}

// helper
func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	p := filepath.Join(dir, name)
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
```

### Step 2: Run test to verify it fails

Run: `go test ./pkg/repo/ -run TestFetch -v`
Expected: FAIL — `Fetch` method doesn't exist

### Step 3: Implement Fetch in pkg/repo

```go
// pkg/repo/fetch.go
package repo

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/odvcencio/graft/pkg/object"
	"github.com/odvcencio/graft/pkg/remote"
)

// FetchResult describes what a fetch operation updated.
type FetchResult struct {
	RemoteName  string
	UpdatedRefs []FetchRefUpdate
}

// FetchRefUpdate describes a single ref that was updated during fetch.
type FetchRefUpdate struct {
	RefName string
	OldHash object.Hash
	NewHash object.Hash
}

// Fetch downloads objects and refs from a remote without modifying the working
// tree or current branch. Remote refs are stored under refs/remotes/<name>/.
func (r *Repo) Fetch(remoteName string) (*FetchResult, error) {
	return r.FetchWithTransport(remoteName, nil)
}

// FetchWithTransport is like Fetch but accepts an optional remote.Client.
// When client is nil a new one is created from the configured remote URL.
func (r *Repo) FetchWithTransport(remoteName string, client *remote.Client) (*FetchResult, error) {
	remoteURL, err := r.RemoteURL(remoteName)
	if err != nil {
		return nil, fmt.Errorf("fetch: %w", err)
	}

	// Detect local-path remotes (for testing and local clones)
	isLocal := isLocalPath(remoteURL)

	result := &FetchResult{RemoteName: remoteName}

	if isLocal {
		return r.fetchLocal(remoteName, remoteURL, result)
	}

	if client == nil {
		client, err = remote.NewClient(remoteURL)
		if err != nil {
			return nil, fmt.Errorf("fetch: %w", err)
		}
	}

	return r.fetchRemote(remoteName, client, result)
}

func (r *Repo) fetchLocal(remoteName, remoteDir string, result *FetchResult) (*FetchResult, error) {
	remoteRepo, err := Open(remoteDir)
	if err != nil {
		return nil, fmt.Errorf("fetch: cannot open remote repo %s: %w", remoteDir, err)
	}

	// List remote refs
	remoteRefs, err := remoteRepo.ListRefs("refs/heads/")
	if err != nil {
		return nil, fmt.Errorf("fetch: list remote refs: %w", err)
	}
	remoteTags, err := remoteRepo.ListRefs("refs/tags/")
	if err != nil {
		return nil, fmt.Errorf("fetch: list remote tags: %w", err)
	}
	for k, v := range remoteTags {
		remoteRefs[k] = v
	}

	// Collect local haves
	localRefs, _ := r.ListRefs("refs/")
	haves := make(map[object.Hash]bool)
	for _, h := range localRefs {
		haves[h] = true
	}

	// Copy missing objects by walking from each remote ref
	for _, remoteHash := range remoteRefs {
		if err := r.copyObjectsFrom(remoteRepo, remoteHash, haves); err != nil {
			return nil, fmt.Errorf("fetch: copy objects: %w", err)
		}
	}

	// Update remote-tracking refs
	for refName, hash := range remoteRefs {
		trackingRef := toTrackingRef(remoteName, refName)
		oldHash, _ := r.ResolveRef(trackingRef)
		if oldHash == hash {
			continue
		}
		if err := r.UpdateRef(trackingRef, hash); err != nil {
			return nil, fmt.Errorf("fetch: update %s: %w", trackingRef, err)
		}
		result.UpdatedRefs = append(result.UpdatedRefs, FetchRefUpdate{
			RefName: trackingRef,
			OldHash: oldHash,
			NewHash: hash,
		})
	}

	return result, nil
}

func (r *Repo) fetchRemote(remoteName string, client *remote.Client, result *FetchResult) (*FetchResult, error) {
	ctx := context.Background()

	refs, err := client.ListRefs(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetch: list refs: %w", err)
	}

	// Collect local tips as "haves" for negotiation
	localRefs, _ := r.ListRefs("refs/")
	var haveHashes []object.Hash
	for _, h := range localRefs {
		haveHashes = append(haveHashes, h)
	}

	// Collect wants (all remote heads/tags we don't have)
	var wantHashes []object.Hash
	for _, h := range refs {
		if _, err := r.Store.ReadRaw(h); err != nil {
			wantHashes = append(wantHashes, h)
		}
	}

	if len(wantHashes) > 0 {
		if err := remote.FetchIntoStore(ctx, client, r.Store, wantHashes, haveHashes); err != nil {
			return nil, fmt.Errorf("fetch: %w", err)
		}
	}

	// Update tracking refs
	for refName, hash := range refs {
		trackingRef := toTrackingRef(remoteName, refName)
		oldHash, _ := r.ResolveRef(trackingRef)
		if oldHash == hash {
			continue
		}
		if err := r.UpdateRef(trackingRef, hash); err != nil {
			return nil, fmt.Errorf("fetch: update %s: %w", trackingRef, err)
		}
		result.UpdatedRefs = append(result.UpdatedRefs, FetchRefUpdate{
			RefName: trackingRef,
			OldHash: oldHash,
			NewHash: hash,
		})
	}

	return result, nil
}

// copyObjectsFrom recursively copies objects reachable from h that aren't in haves.
func (r *Repo) copyObjectsFrom(src *Repo, h object.Hash, haves map[object.Hash]bool) error {
	if haves[h] {
		return nil
	}
	if _, err := r.Store.ReadRaw(h); err == nil {
		haves[h] = true
		return nil
	}
	raw, err := src.Store.ReadRaw(h)
	if err != nil {
		return err
	}
	if err := r.Store.WriteRaw(h, raw); err != nil {
		return err
	}
	haves[h] = true

	// Walk children
	objType, err := r.Store.TypeOf(h)
	if err != nil {
		return nil // best effort
	}
	switch objType {
	case object.TypeCommit:
		c, _ := r.Store.ReadCommit(h)
		if c != nil {
			r.copyObjectsFrom(src, c.TreeHash, haves)
			for _, p := range c.Parents {
				r.copyObjectsFrom(src, p, haves)
			}
		}
	case object.TypeTree:
		tr, _ := r.Store.ReadTree(h)
		if tr != nil {
			for _, e := range tr.Entries {
				if e.IsDir {
					r.copyObjectsFrom(src, e.SubtreeHash, haves)
				} else {
					r.copyObjectsFrom(src, e.BlobHash, haves)
					if e.EntityListHash != "" {
						r.copyObjectsFrom(src, e.EntityListHash, haves)
					}
				}
			}
		}
	case object.TypeEntityList:
		el, _ := r.Store.ReadEntityList(h)
		if el != nil {
			for _, ref := range el.EntityRefs {
				r.copyObjectsFrom(src, ref, haves)
			}
		}
	}
	return nil
}

// toTrackingRef converts a remote ref name to a local tracking ref.
// "refs/heads/main" -> "refs/remotes/<remote>/heads/main"
// "refs/tags/v1.0"  -> "refs/remotes/<remote>/tags/v1.0"
func toTrackingRef(remoteName, refName string) string {
	// Strip "refs/" prefix for the tracking path
	trimmed := strings.TrimPrefix(refName, "refs/")
	return "refs/remotes/" + remoteName + "/" + trimmed
}

func isLocalPath(url string) bool {
	if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
		return false
	}
	if strings.Contains(url, "://") {
		return false
	}
	return filepath.IsAbs(url) || strings.HasPrefix(url, ".")
}
```

**Note:** The exact implementation will depend on what methods `Store` already exposes. The `ReadRaw`/`WriteRaw`/`TypeOf` methods may need to be checked — if they don't exist, use the typed read/write methods or add thin wrappers. Check `pkg/object/store.go` for the actual Store API before implementing.

### Step 4: Run tests to verify they pass

Run: `go test ./pkg/repo/ -run TestFetch -v`
Expected: PASS

### Step 5: Wire up CLI command

```go
// cmd/graft/cmd_fetch.go
package main

import (
	"fmt"

	"github.com/odvcencio/graft/pkg/repo"
	"github.com/spf13/cobra"
)

func newFetchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fetch [remote]",
		Short: "Download objects and refs from a remote",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := repo.Open(".")
			if err != nil {
				return err
			}

			remoteName := "origin"
			if len(args) > 0 {
				remoteName = args[0]
			}

			result, err := r.Fetch(remoteName)
			if err != nil {
				return err
			}

			if len(result.UpdatedRefs) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "Already up to date.")
				return nil
			}

			for _, u := range result.UpdatedRefs {
				short := u.NewHash
				if len(short) > 8 {
					short = short[:8]
				}
				if u.OldHash == "" {
					fmt.Fprintf(cmd.OutOrStdout(), " * [new ref]   %s -> %s\n", short, u.RefName)
				} else {
					oldShort := u.OldHash
					if len(oldShort) > 8 {
						oldShort = oldShort[:8]
					}
					fmt.Fprintf(cmd.OutOrStdout(), "   %s..%s  %s\n", oldShort, short, u.RefName)
				}
			}
			return nil
		},
	}
	return cmd
}
```

Register in `main.go`: add `rootCmd.AddCommand(newFetchCmd())` alongside other commands.

### Step 6: Run full test suite

Run: `go test ./pkg/repo/ -v -count=1`
Expected: All existing tests still pass + new fetch tests pass

### Step 7: Commit

```bash
git add pkg/repo/fetch.go pkg/repo/fetch_test.go cmd/graft/cmd_fetch.go cmd/graft/main.go
buckley commit --yes --minimal-output
```

---

## Task 2: `graft stash` — Core Library

Stash saves working tree + index state as a commit on `refs/stash`, reverts to HEAD, and can restore later. The stash stack uses reflog entries on `refs/stash`.

**Files:**
- Create: `pkg/repo/stash.go`
- Create: `pkg/repo/stash_test.go`

### Step 1: Write the failing test for Stash push

```go
// pkg/repo/stash_test.go
package repo

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStashSavesAndRevertsThenPops(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatal(err)
	}

	// Create initial commit
	writeFile(t, dir, "main.go", "package main\n")
	r.Add([]string{"main.go"})
	r.Commit("initial", "test")

	// Make a change
	writeFile(t, dir, "main.go", "package main\n\nfunc hello() {}\n")
	r.Add([]string{"main.go"})

	// Stash
	entry, err := r.Stash("test")
	if err != nil {
		t.Fatal(err)
	}
	if entry.CommitHash == "" {
		t.Fatal("stash returned empty hash")
	}

	// Working tree should be reverted to HEAD
	data, _ := os.ReadFile(filepath.Join(dir, "main.go"))
	if string(data) != "package main\n" {
		t.Errorf("working tree not reverted, got: %q", string(data))
	}

	// Pop should restore the change
	if err := r.StashPop(0); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(filepath.Join(dir, "main.go"))
	if string(data) != "package main\n\nfunc hello() {}\n" {
		t.Errorf("stash pop did not restore, got: %q", string(data))
	}
}

func TestStashListShowsEntries(t *testing.T) {
	dir := t.TempDir()
	r, _ := Init(dir)
	writeFile(t, dir, "a.txt", "a")
	r.Add([]string{"a.txt"})
	r.Commit("initial", "test")

	// Stash twice
	writeFile(t, dir, "a.txt", "a modified")
	r.Add([]string{"a.txt"})
	r.Stash("test")

	writeFile(t, dir, "a.txt", "a modified again")
	r.Add([]string{"a.txt"})
	r.Stash("test")

	entries, err := r.StashList()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Errorf("expected 2 stash entries, got %d", len(entries))
	}
}

func TestStashDropRemovesEntry(t *testing.T) {
	dir := t.TempDir()
	r, _ := Init(dir)
	writeFile(t, dir, "a.txt", "a")
	r.Add([]string{"a.txt"})
	r.Commit("initial", "test")

	writeFile(t, dir, "a.txt", "modified")
	r.Add([]string{"a.txt"})
	r.Stash("test")

	entries, _ := r.StashList()
	if len(entries) != 1 {
		t.Fatalf("expected 1, got %d", len(entries))
	}

	if err := r.StashDrop(0); err != nil {
		t.Fatal(err)
	}

	entries, _ = r.StashList()
	if len(entries) != 0 {
		t.Errorf("expected 0 after drop, got %d", len(entries))
	}
}

func TestStashOnCleanTreeReturnsError(t *testing.T) {
	dir := t.TempDir()
	r, _ := Init(dir)
	writeFile(t, dir, "a.txt", "a")
	r.Add([]string{"a.txt"})
	r.Commit("initial", "test")

	_, err := r.Stash("test")
	if err == nil {
		t.Error("expected error stashing with no changes")
	}
}
```

### Step 2: Run test to verify it fails

Run: `go test ./pkg/repo/ -run TestStash -v`
Expected: FAIL — `Stash` method doesn't exist

### Step 3: Implement stash core

```go
// pkg/repo/stash.go
package repo

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/odvcencio/graft/pkg/object"
)

// StashEntry represents a single stash entry.
type StashEntry struct {
	CommitHash object.Hash `json:"commit_hash"`
	Message    string      `json:"message"`
	Timestamp  int64       `json:"timestamp"`
}

// Stash saves the current working tree and index state, then reverts to HEAD.
// Returns the stash entry that was created.
func (r *Repo) Stash(author string) (*StashEntry, error) {
	// Check we have a HEAD to revert to
	headHash, err := r.ResolveRef("HEAD")
	if err != nil {
		return nil, fmt.Errorf("stash: no commits yet: %w", err)
	}

	// Check there are actual changes (staged or unstaged)
	stg, err := r.ReadStaging()
	if err != nil {
		return nil, fmt.Errorf("stash: %w", err)
	}

	headCommit, err := r.Store.ReadCommit(headHash)
	if err != nil {
		return nil, fmt.Errorf("stash: read HEAD: %w", err)
	}

	// Build tree from current staging (includes staged changes)
	stashTree, err := r.BuildTree(stg)
	if err != nil {
		return nil, fmt.Errorf("stash: build tree: %w", err)
	}

	// If tree matches HEAD tree, no changes to stash
	if stashTree == headCommit.TreeHash {
		return nil, fmt.Errorf("no local changes to save")
	}

	// Create stash commit with HEAD as parent
	now := time.Now()
	stashCommit := &object.CommitObj{
		TreeHash:          stashTree,
		Parents:           []object.Hash{headHash},
		Author:            author,
		Timestamp:         now.Unix(),
		AuthorTimezone:    formatTimezone(now),
		Committer:         author,
		CommitterTimestamp: now.Unix(),
		CommitterTimezone:  formatTimezone(now),
		Message:           fmt.Sprintf("WIP on %s", r.currentBranchShort()),
	}
	commitHash, err := r.Store.WriteCommit(stashCommit)
	if err != nil {
		return nil, fmt.Errorf("stash: write commit: %w", err)
	}

	// Push onto stash stack
	entry := &StashEntry{
		CommitHash: commitHash,
		Message:    stashCommit.Message,
		Timestamp:  now.Unix(),
	}
	if err := r.pushStashEntry(entry); err != nil {
		return nil, fmt.Errorf("stash: %w", err)
	}

	// Revert working tree and staging to HEAD
	if err := r.resetToCommit(headHash); err != nil {
		return nil, fmt.Errorf("stash: revert: %w", err)
	}

	return entry, nil
}

// StashPop applies the stash at the given index and removes it from the stack.
func (r *Repo) StashPop(index int) error {
	if err := r.StashApply(index); err != nil {
		return err
	}
	return r.StashDrop(index)
}

// StashApply applies the stash at the given index without removing it.
func (r *Repo) StashApply(index int) error {
	entries, err := r.StashList()
	if err != nil {
		return fmt.Errorf("stash apply: %w", err)
	}
	if index < 0 || index >= len(entries) {
		return fmt.Errorf("stash apply: index %d out of range (have %d entries)", index, len(entries))
	}

	entry := entries[index]
	stashCommit, err := r.Store.ReadCommit(entry.CommitHash)
	if err != nil {
		return fmt.Errorf("stash apply: %w", err)
	}

	// Flatten the stashed tree and apply to working directory + staging
	files, err := r.FlattenTree(stashCommit.TreeHash)
	if err != nil {
		return fmt.Errorf("stash apply: flatten: %w", err)
	}

	stg, err := r.ReadStaging()
	if err != nil {
		return fmt.Errorf("stash apply: %w", err)
	}

	for _, f := range files {
		blob, err := r.Store.ReadBlob(f.BlobHash)
		if err != nil {
			return fmt.Errorf("stash apply: read blob %s: %w", f.Path, err)
		}
		absPath := filepath.Join(r.RootDir, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(absPath, blob.Data, fileModeFromString(f.Mode)); err != nil {
			return err
		}
		stg.Entries[f.Path] = &StagingEntry{
			Path:           f.Path,
			BlobHash:       f.BlobHash,
			EntityListHash: f.EntityListHash,
			Mode:           f.Mode,
		}
	}

	return r.WriteStaging(stg)
}

// StashList returns all stash entries, newest first.
func (r *Repo) StashList() ([]StashEntry, error) {
	data, err := os.ReadFile(r.stashFile())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var entries []StashEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("stash list: parse: %w", err)
	}
	return entries, nil
}

// StashDrop removes the stash entry at the given index.
func (r *Repo) StashDrop(index int) error {
	entries, err := r.StashList()
	if err != nil {
		return err
	}
	if index < 0 || index >= len(entries) {
		return fmt.Errorf("stash drop: index %d out of range", index)
	}
	entries = append(entries[:index], entries[index+1:]...)
	return r.writeStashEntries(entries)
}

func (r *Repo) stashFile() string {
	return filepath.Join(r.GotDir, "stash")
}

func (r *Repo) pushStashEntry(entry *StashEntry) error {
	entries, err := r.StashList()
	if err != nil {
		return err
	}
	// Prepend (newest first)
	entries = append([]StashEntry{*entry}, entries...)
	return r.writeStashEntries(entries)
}

func (r *Repo) writeStashEntries(entries []StashEntry) error {
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	tmp := r.stashFile() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, r.stashFile())
}

func (r *Repo) currentBranchShort() string {
	head, err := r.Head()
	if err != nil {
		return "HEAD"
	}
	if b, ok := parseSymbolicHead(head); ok {
		return b
	}
	if len(head) > 8 {
		return head[:8]
	}
	return head
}

// resetToCommit resets the working tree and staging to match the given commit.
func (r *Repo) resetToCommit(commitHash object.Hash) error {
	commit, err := r.Store.ReadCommit(commitHash)
	if err != nil {
		return err
	}

	files, err := r.FlattenTree(commit.TreeHash)
	if err != nil {
		return err
	}

	// Build new staging and write files
	stg := &Staging{Entries: make(map[string]*StagingEntry)}
	for _, f := range files {
		blob, err := r.Store.ReadBlob(f.BlobHash)
		if err != nil {
			return err
		}
		absPath := filepath.Join(r.RootDir, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(absPath, blob.Data, fileModeFromString(f.Mode)); err != nil {
			return err
		}
		stg.Entries[f.Path] = &StagingEntry{
			Path:           f.Path,
			BlobHash:       f.BlobHash,
			EntityListHash: f.EntityListHash,
			Mode:           f.Mode,
		}
	}

	return r.WriteStaging(stg)
}

// parseSymbolicHead extracts branch name from "ref: refs/heads/main" format.
// Returns the branch name and true if symbolic, or ("", false) if detached.
func parseSymbolicHead(head string) (string, bool) {
	const prefix = "refs/heads/"
	if len(head) > len(prefix) && head[:len(prefix)] == prefix {
		return head[len(prefix):], true
	}
	return "", false
}

func formatTimezone(t time.Time) string {
	_, offset := t.Zone()
	sign := "+"
	if offset < 0 {
		sign = "-"
		offset = -offset
	}
	hours := offset / 3600
	minutes := (offset % 3600) / 60
	return fmt.Sprintf("%s%02d%02d", sign, hours, minutes)
}
```

**Note:** `fileModeFromString`, `parseSymbolicHead`, and `formatTimezone` may already exist in the codebase. Check `pkg/repo/filemode.go`, `pkg/repo/checkout.go`, and `pkg/repo/commit.go` before implementing — reuse existing helpers. The `resetToCommit` logic overlaps with checkout — check if `Checkout` can be reused or if a shared helper should be extracted.

### Step 4: Run tests to verify they pass

Run: `go test ./pkg/repo/ -run TestStash -v`
Expected: PASS

### Step 5: Run full test suite for regressions

Run: `go test ./pkg/repo/ -v -count=1`
Expected: All pass

### Step 6: Commit

```bash
git add pkg/repo/stash.go pkg/repo/stash_test.go
buckley commit --yes --minimal-output
```

---

## Task 3: `graft stash` — CLI Command

Wire the stash library into a cobra command with subcommands.

**Files:**
- Create: `cmd/graft/cmd_stash.go`
- Modify: `cmd/graft/main.go` (register command)

### Step 1: Implement CLI command

```go
// cmd/graft/cmd_stash.go
package main

import (
	"fmt"
	"strconv"
	"time"

	"github.com/odvcencio/graft/pkg/repo"
	"github.com/spf13/cobra"
)

func newStashCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stash",
		Short: "Stash working tree changes",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Default: stash push
			return stashPush(cmd)
		},
	}

	cmd.AddCommand(newStashPushCmd())
	cmd.AddCommand(newStashPopCmd())
	cmd.AddCommand(newStashApplyCmd())
	cmd.AddCommand(newStashListCmd())
	cmd.AddCommand(newStashDropCmd())
	cmd.AddCommand(newStashShowCmd())

	return cmd
}

func newStashPushCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "push",
		Short: "Save changes and revert working tree",
		RunE: func(cmd *cobra.Command, args []string) error {
			return stashPush(cmd)
		},
	}
}

func stashPush(cmd *cobra.Command) error {
	r, err := repo.Open(".")
	if err != nil {
		return err
	}

	author := resolveAuthor()
	entry, err := r.Stash(author)
	if err != nil {
		return err
	}

	short := string(entry.CommitHash)
	if len(short) > 8 {
		short = short[:8]
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Saved working directory: %s %s\n", short, entry.Message)
	return nil
}

func newStashPopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "pop [index]",
		Short: "Apply stash and remove it",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := repo.Open(".")
			if err != nil {
				return err
			}
			index := 0
			if len(args) > 0 {
				index, err = strconv.Atoi(args[0])
				if err != nil {
					return fmt.Errorf("invalid index: %s", args[0])
				}
			}
			if err := r.StashPop(index); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Dropped stash entry.")
			return nil
		},
	}
}

func newStashApplyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "apply [index]",
		Short: "Apply stash without removing it",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := repo.Open(".")
			if err != nil {
				return err
			}
			index := 0
			if len(args) > 0 {
				var parseErr error
				index, parseErr = strconv.Atoi(args[0])
				if parseErr != nil {
					return fmt.Errorf("invalid index: %s", args[0])
				}
			}
			return r.StashApply(index)
		},
	}
}

func newStashListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List stash entries",
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := repo.Open(".")
			if err != nil {
				return err
			}
			entries, err := r.StashList()
			if err != nil {
				return err
			}
			if len(entries) == 0 {
				return nil
			}
			for i, e := range entries {
				ts := time.Unix(e.Timestamp, 0).Format("2006-01-02 15:04:05")
				fmt.Fprintf(cmd.OutOrStdout(), "stash@{%d}: %s (%s)\n", i, e.Message, ts)
			}
			return nil
		},
	}
}

func newStashDropCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "drop [index]",
		Short: "Remove a stash entry",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := repo.Open(".")
			if err != nil {
				return err
			}
			index := 0
			if len(args) > 0 {
				var parseErr error
				index, parseErr = strconv.Atoi(args[0])
				if parseErr != nil {
					return fmt.Errorf("invalid index: %s", args[0])
				}
			}
			if err := r.StashDrop(index); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Dropped stash@{%d}.\n", index)
			return nil
		},
	}
}

func newStashShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show [index]",
		Short: "Show stash contents",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := repo.Open(".")
			if err != nil {
				return err
			}
			index := 0
			if len(args) > 0 {
				var parseErr error
				index, parseErr = strconv.Atoi(args[0])
				if parseErr != nil {
					return fmt.Errorf("invalid index: %s", args[0])
				}
			}
			entries, err := r.StashList()
			if err != nil {
				return err
			}
			if index < 0 || index >= len(entries) {
				return fmt.Errorf("stash@{%d} does not exist", index)
			}
			e := entries[index]
			short := string(e.CommitHash)
			if len(short) > 8 {
				short = short[:8]
			}
			fmt.Fprintf(cmd.OutOrStdout(), "stash@{%d}: %s\ncommit: %s\n", index, e.Message, short)
			return nil
		},
	}
}
```

### Step 2: Register in main.go

Add `rootCmd.AddCommand(newStashCmd())` where other commands are registered.

### Step 3: Build and smoke test

Run: `go build ./cmd/graft/ && ./graft stash --help`
Expected: Shows stash subcommands

### Step 4: Commit

```bash
git add cmd/graft/cmd_stash.go cmd/graft/main.go
buckley commit --yes --minimal-output
```

---

## Task 4: `graft cherry-pick` — Commit-Level Porcelain

Wraps the existing `cherrypick-entity` machinery to apply entire commits. Uses structural merge with the commit's parent as base.

**Files:**
- Create: `pkg/repo/cherrypick.go`
- Create: `pkg/repo/cherrypick_test.go`
- Create: `cmd/graft/cmd_cherrypick.go`
- Modify: `cmd/graft/main.go`

### Step 1: Write the failing test

```go
// pkg/repo/cherrypick_test.go
package repo

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCherryPickAppliesCommit(t *testing.T) {
	dir := t.TempDir()
	r, _ := Init(dir)

	// Initial commit on main
	writeFile(t, dir, "main.go", "package main\n")
	r.Add([]string{"main.go"})
	r.Commit("initial", "test")

	// Create a branch and add a file
	mainHash, _ := r.ResolveRef("HEAD")
	r.CreateBranch("feature", mainHash)

	// Simulate branch commit by building on current state
	writeFile(t, dir, "feature.go", "package main\n\nfunc Feature() {}\n")
	r.Add([]string{"feature.go"})
	featureHash, err := r.Commit("add feature", "test")
	if err != nil {
		t.Fatal(err)
	}

	// Switch back to main (reset to mainHash)
	r.Checkout("main")

	// Cherry-pick the feature commit
	result, err := r.CherryPick(featureHash)
	if err != nil {
		t.Fatal(err)
	}
	if result.CommitHash == "" {
		t.Error("expected a commit hash")
	}

	// feature.go should exist
	data, err := os.ReadFile(filepath.Join(dir, "feature.go"))
	if err != nil {
		t.Fatal("feature.go not found after cherry-pick")
	}
	if string(data) != "package main\n\nfunc Feature() {}\n" {
		t.Errorf("unexpected content: %q", string(data))
	}
}

func TestCherryPickConflictReportsError(t *testing.T) {
	dir := t.TempDir()
	r, _ := Init(dir)

	writeFile(t, dir, "main.go", "package main\n\nfunc Hello() { println(\"base\") }\n")
	r.Add([]string{"main.go"})
	baseHash, _ := r.Commit("initial", "test")

	// Branch: modify Hello
	r.CreateBranch("feature", baseHash)
	writeFile(t, dir, "main.go", "package main\n\nfunc Hello() { println(\"feature\") }\n")
	r.Add([]string{"main.go"})
	featureHash, _ := r.Commit("feature change", "test")

	// Main: modify Hello differently
	r.Checkout("main")
	writeFile(t, dir, "main.go", "package main\n\nfunc Hello() { println(\"main\") }\n")
	r.Add([]string{"main.go"})
	r.Commit("main change", "test")

	// Cherry-pick should report conflict
	result, err := r.CherryPick(featureHash)
	if err == nil && !result.HasConflicts {
		t.Error("expected conflict when cherry-picking divergent changes to same entity")
	}
}
```

### Step 2: Run test to verify it fails

Run: `go test ./pkg/repo/ -run TestCherryPick -v`
Expected: FAIL — `CherryPick` method doesn't exist

### Step 3: Implement cherry-pick

```go
// pkg/repo/cherrypick.go
package repo

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/odvcencio/graft/pkg/object"
)

// CherryPickResult describes the outcome of a cherry-pick operation.
type CherryPickResult struct {
	CommitHash   object.Hash
	HasConflicts bool
	Report       *MergeReport
}

// CherryPick applies the changes introduced by the given commit onto HEAD.
// It uses three-way structural merge with the commit's parent as base.
func (r *Repo) CherryPick(commitHash object.Hash) (*CherryPickResult, error) {
	// Read the commit to cherry-pick
	commit, err := r.Store.ReadCommit(commitHash)
	if err != nil {
		return nil, fmt.Errorf("cherry-pick: read commit: %w", err)
	}
	if len(commit.Parents) == 0 {
		return nil, fmt.Errorf("cherry-pick: cannot cherry-pick a root commit")
	}

	parentHash := commit.Parents[0]

	// Read HEAD
	headHash, err := r.ResolveRef("HEAD")
	if err != nil {
		return nil, fmt.Errorf("cherry-pick: resolve HEAD: %w", err)
	}
	headCommit, err := r.Store.ReadCommit(headHash)
	if err != nil {
		return nil, fmt.Errorf("cherry-pick: read HEAD: %w", err)
	}

	// Three-way merge: base=parent, ours=HEAD, theirs=commit
	report, err := r.mergeTreesThreeWay(parentHash, headCommit.TreeHash, commit.TreeHash)
	if err != nil {
		return nil, fmt.Errorf("cherry-pick: merge: %w", err)
	}

	result := &CherryPickResult{
		HasConflicts: report.HasConflicts,
		Report:       report,
	}

	if report.HasConflicts {
		return result, fmt.Errorf("cherry-pick: conflicts in %d file(s)", report.TotalConflicts)
	}

	// Auto-commit the clean cherry-pick
	now := time.Now()
	cpCommit := &object.CommitObj{
		TreeHash:          report.TreeHash,
		Parents:           []object.Hash{headHash},
		Author:            commit.Author,
		Timestamp:         commit.Timestamp,
		AuthorTimezone:    commit.AuthorTimezone,
		Committer:         commit.Author,
		CommitterTimestamp: now.Unix(),
		CommitterTimezone:  formatTimezone(now),
		Message:           commit.Message,
	}
	newHash, err := r.Store.WriteCommit(cpCommit)
	if err != nil {
		return nil, fmt.Errorf("cherry-pick: write commit: %w", err)
	}
	result.CommitHash = newHash

	// Update HEAD
	if err := r.updateHEAD(newHash, headHash); err != nil {
		return nil, fmt.Errorf("cherry-pick: update HEAD: %w", err)
	}

	// Update working tree and staging
	if err := r.resetToCommit(newHash); err != nil {
		return nil, fmt.Errorf("cherry-pick: reset: %w", err)
	}

	return result, nil
}
```

**Note:** This requires a `mergeTreesThreeWay(baseTree, oursTree, theirsTree)` method that returns a `MergeReport` with a `TreeHash`. Check if the existing `Merge()` in `pkg/repo/merge.go` can be refactored to expose tree-level merging (it currently takes a branch name). If not, extract the tree merge logic into a shared helper. The existing merge flow in `merge.go` does: resolve refs → flatten trees → merge files → build result tree → commit. Cherry-pick needs the same flow but with explicit tree hashes instead of branch resolution.

Also check if `updateHEAD` already exists or if the pattern is done inline in commit/checkout. The CAS ref update pattern is in `refs.go`.

### Step 4: Run tests to verify they pass

Run: `go test ./pkg/repo/ -run TestCherryPick -v`
Expected: PASS

### Step 5: Wire CLI command

```go
// cmd/graft/cmd_cherrypick.go
package main

import (
	"fmt"

	"github.com/odvcencio/graft/pkg/object"
	"github.com/odvcencio/graft/pkg/repo"
	"github.com/spf13/cobra"
)

func newCherryPickCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cherry-pick <commit>...",
		Short: "Apply changes from existing commits",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := repo.Open(".")
			if err != nil {
				return err
			}

			for _, arg := range args {
				hash := object.Hash(arg)
				result, err := r.CherryPick(hash)
				if err != nil {
					return err
				}
				short := string(result.CommitHash)
				if len(short) > 8 {
					short = short[:8]
				}
				fmt.Fprintf(cmd.OutOrStdout(), "[%s] %s\n", short, "cherry-picked")
			}
			return nil
		},
	}
	return cmd
}
```

### Step 6: Commit

```bash
git add pkg/repo/cherrypick.go pkg/repo/cherrypick_test.go cmd/graft/cmd_cherrypick.go cmd/graft/main.go
buckley commit --yes --minimal-output
```

---

## Task 5: Client-Side Hooks Infrastructure

Hook execution engine that runs scripts from `.graft/hooks/` at defined trigger points.

**Files:**
- Create: `pkg/repo/hooks.go`
- Create: `pkg/repo/hooks_test.go`
- Modify: `pkg/repo/commit.go` (call pre-commit + commit-msg hooks)

### Step 1: Write the failing test

```go
// pkg/repo/hooks_test.go
package repo

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestPreCommitHookBlocksCommit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("hook scripts require unix")
	}

	dir := t.TempDir()
	r, _ := Init(dir)

	// Create a pre-commit hook that fails
	hookDir := filepath.Join(r.GotDir, "hooks")
	os.MkdirAll(hookDir, 0o755)
	hookScript := "#!/bin/sh\nexit 1\n"
	os.WriteFile(filepath.Join(hookDir, "pre-commit"), []byte(hookScript), 0o755)

	writeFile(t, dir, "a.txt", "a")
	r.Add([]string{"a.txt"})

	_, err := r.Commit("should fail", "test")
	if err == nil {
		t.Error("expected commit to fail when pre-commit hook exits non-zero")
	}
}

func TestPreCommitHookAllowsCommit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("hook scripts require unix")
	}

	dir := t.TempDir()
	r, _ := Init(dir)

	hookDir := filepath.Join(r.GotDir, "hooks")
	os.MkdirAll(hookDir, 0o755)
	hookScript := "#!/bin/sh\nexit 0\n"
	os.WriteFile(filepath.Join(hookDir, "pre-commit"), []byte(hookScript), 0o755)

	writeFile(t, dir, "a.txt", "a")
	r.Add([]string{"a.txt"})

	_, err := r.Commit("should work", "test")
	if err != nil {
		t.Errorf("commit should succeed when hook exits 0: %v", err)
	}
}

func TestCommitMsgHookCanModifyMessage(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("hook scripts require unix")
	}

	dir := t.TempDir()
	r, _ := Init(dir)

	hookDir := filepath.Join(r.GotDir, "hooks")
	os.MkdirAll(hookDir, 0o755)
	// Hook that appends a sign-off
	hookScript := `#!/bin/sh
echo "" >> "$1"
echo "Signed-off-by: hook" >> "$1"
`
	os.WriteFile(filepath.Join(hookDir, "commit-msg"), []byte(hookScript), 0o755)

	writeFile(t, dir, "a.txt", "a")
	r.Add([]string{"a.txt"})

	hash, err := r.Commit("test message", "test")
	if err != nil {
		t.Fatal(err)
	}

	commit, _ := r.Store.ReadCommit(hash)
	if commit == nil {
		t.Fatal("commit not found")
	}
	// The hook should have modified the message
	if commit.Message == "test message" {
		t.Error("expected hook to modify the commit message")
	}
}

func TestNoHookDirIsOk(t *testing.T) {
	dir := t.TempDir()
	r, _ := Init(dir)

	writeFile(t, dir, "a.txt", "a")
	r.Add([]string{"a.txt"})

	_, err := r.Commit("works without hooks dir", "test")
	if err != nil {
		t.Errorf("commit should work without hooks directory: %v", err)
	}
}
```

### Step 2: Run test to verify it fails

Run: `go test ./pkg/repo/ -run TestPreCommitHook -v`
Expected: FAIL — hooks not wired into commit

### Step 3: Implement hooks engine

```go
// pkg/repo/hooks.go
package repo

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// HookName identifies a hook trigger point.
type HookName string

const (
	HookPreCommit    HookName = "pre-commit"
	HookCommitMsg    HookName = "commit-msg"
	HookPrePush      HookName = "pre-push"
	HookPostCheckout HookName = "post-checkout"
	HookPreRebase    HookName = "pre-rebase"
	HookPostMerge    HookName = "post-merge"
)

// RunHook executes a hook script if it exists. Returns nil if hook doesn't
// exist or isn't executable. Returns error if hook exists and fails.
// Args are passed as command-line arguments to the hook script.
func (r *Repo) RunHook(name HookName, args ...string) error {
	hookPath := filepath.Join(r.GotDir, "hooks", string(name))

	info, err := os.Stat(hookPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No hook, that's fine
		}
		return fmt.Errorf("hook %s: %w", name, err)
	}

	// Check executable bit
	if info.Mode()&0o111 == 0 {
		return nil // Not executable, skip
	}

	cmd := exec.Command(hookPath, args...)
	cmd.Dir = r.RootDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	// Set environment
	cmd.Env = append(os.Environ(),
		"GRAFT_DIR="+r.GotDir,
		"GRAFT_WORK_TREE="+r.RootDir,
	)

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("hook %s failed: %w", name, err)
	}
	return nil
}
```

### Step 4: Wire hooks into Commit

Modify `pkg/repo/commit.go` — in `Commit()` and `CommitWithSigner()`, add hook calls:

1. Before building the tree: `r.RunHook(HookPreCommit)`
2. After constructing message: write message to temp file, `r.RunHook(HookCommitMsg, tempFile)`, re-read message from temp file.

The exact modification depends on the current Commit() structure. Read commit.go, identify where to insert:
- `r.RunHook(HookPreCommit)` — early, before any work
- commit-msg hook — after message is known, before writing commit object

### Step 5: Run tests to verify they pass

Run: `go test ./pkg/repo/ -run "TestPreCommitHook|TestCommitMsg|TestNoHook" -v`
Expected: PASS

### Step 6: Run full test suite

Run: `go test ./pkg/repo/ -v -count=1`
Expected: All pass (existing tests don't have hooks dir, so they skip silently)

### Step 7: Commit

```bash
git add pkg/repo/hooks.go pkg/repo/hooks_test.go pkg/repo/commit.go
buckley commit --yes --minimal-output
```

---

## Task 6: Auto-Signing During Auth Setup

Extend `graft auth setup` to generate an Ed25519 SSH key and make signing the default for all commits.

**Files:**
- Create: `pkg/repo/signing.go`
- Create: `pkg/repo/signing_test.go`
- Modify: `cmd/graft/cmd_auth.go` (add key generation to setup flow)
- Modify: `cmd/graft/cmd_commit.go` (auto-sign when key exists)
- Modify: `pkg/userconfig/config.go` (add signing fields)

### Step 1: Write the failing test

```go
// pkg/repo/signing_test.go
package repo

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGenerateSigningKey(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "signing_key")

	err := GenerateSigningKey(keyPath)
	if err != nil {
		t.Fatal(err)
	}

	// Private key should exist
	if _, err := os.Stat(keyPath); err != nil {
		t.Errorf("private key not created: %v", err)
	}

	// Public key should exist
	if _, err := os.Stat(keyPath + ".pub"); err != nil {
		t.Errorf("public key not created: %v", err)
	}

	// Private key should be 0600
	info, _ := os.Stat(keyPath)
	if info.Mode().Perm() != 0o600 {
		t.Errorf("private key permissions = %o, want 0600", info.Mode().Perm())
	}
}

func TestSignAndVerifyCommit(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "key")

	GenerateSigningKey(keyPath)

	payload := []byte("tree abc123\nauthor test\n\ntest commit")

	signer, err := NewSSHSigner(keyPath)
	if err != nil {
		t.Fatal(err)
	}

	sig, err := signer(payload)
	if err != nil {
		t.Fatal(err)
	}
	if sig == "" {
		t.Error("expected non-empty signature")
	}

	// Verify
	pubKeyData, _ := os.ReadFile(keyPath + ".pub")
	err = VerifySSHSignature(payload, sig, pubKeyData)
	if err != nil {
		t.Errorf("signature verification failed: %v", err)
	}
}
```

### Step 2: Run test to verify it fails

Run: `go test ./pkg/repo/ -run "TestGenerateSigningKey|TestSignAndVerify" -v`
Expected: FAIL — functions don't exist

### Step 3: Implement signing helpers

```go
// pkg/repo/signing.go
package repo

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"os"

	"golang.org/x/crypto/ssh"
)

// GenerateSigningKey creates an Ed25519 SSH keypair at the given path.
// Writes private key to path (0600) and public key to path.pub (0644).
func GenerateSigningKey(path string) error {
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return fmt.Errorf("generate key: %w", err)
	}

	// Marshal private key to OpenSSH format
	privPEM, err := ssh.MarshalPrivateKey(privKey, "")
	if err != nil {
		return fmt.Errorf("marshal private key: %w", err)
	}
	if err := os.WriteFile(path, pem.EncodeToMemory(privPEM), 0o600); err != nil {
		return fmt.Errorf("write private key: %w", err)
	}

	// Marshal public key
	sshPub, err := ssh.NewPublicKey(pubKey)
	if err != nil {
		return fmt.Errorf("marshal public key: %w", err)
	}
	pubData := ssh.MarshalAuthorizedKey(sshPub)
	if err := os.WriteFile(path+".pub", pubData, 0o644); err != nil {
		return fmt.Errorf("write public key: %w", err)
	}

	return nil
}

// NewSSHSigner returns a CommitSigner that signs with the given SSH private key.
func NewSSHSigner(keyPath string) (CommitSigner, error) {
	keyData, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("read signing key: %w", err)
	}
	signer, err := ssh.ParsePrivateKey(keyData)
	if err != nil {
		return nil, fmt.Errorf("parse signing key: %w", err)
	}

	return func(payload []byte) (string, error) {
		sig, err := signer.Sign(rand.Reader, payload)
		if err != nil {
			return "", err
		}
		// Encode as SSH signature format
		blob := ssh.Marshal(sig)
		armored := pem.EncodeToMemory(&pem.Block{
			Type:  "SSH SIGNATURE",
			Bytes: blob,
		})
		return string(armored), nil
	}, nil
}

// VerifySSHSignature verifies an SSH signature against a payload and public key.
func VerifySSHSignature(payload []byte, signature string, pubKeyData []byte) error {
	pubKey, _, _, _, err := ssh.ParseAuthorizedKey(pubKeyData)
	if err != nil {
		return fmt.Errorf("parse public key: %w", err)
	}

	block, _ := pem.Decode([]byte(signature))
	if block == nil || block.Type != "SSH SIGNATURE" {
		return fmt.Errorf("invalid signature format")
	}

	sig := &ssh.Signature{}
	if err := ssh.Unmarshal(block.Bytes, sig); err != nil {
		return fmt.Errorf("unmarshal signature: %w", err)
	}

	return pubKey.Verify(payload, sig)
}
```

**Note:** Check `cmd/graft/signing_ssh.go` — there may already be SSH signing code. If so, move it to `pkg/repo/signing.go` and refactor to avoid duplication. The existing `newSSHCommitSigner()` in the CLI should call through to this package.

### Step 4: Run tests

Run: `go test ./pkg/repo/ -run "TestGenerateSigningKey|TestSignAndVerify" -v`
Expected: PASS

### Step 5: Modify auth setup and commit for auto-signing

In `pkg/userconfig/config.go`, add:
```go
type Config struct {
    // existing fields...
    SigningKeyPath string `json:"signing_key_path,omitempty"`
    AutoSign       bool   `json:"auto_sign,omitempty"`
}
```

In `cmd/graft/cmd_auth.go` setup flow, after successful login:
1. Check if `~/.graft/signing_key` exists
2. If not, call `GenerateSigningKey("~/.graft/signing_key")`
3. Set `config.SigningKeyPath` and `config.AutoSign = true`
4. Print: "Generated signing key. All commits will be signed automatically."

In `cmd/graft/cmd_commit.go`:
1. Load user config
2. If `AutoSign` is true and `SigningKeyPath` exists, create signer and call `CommitWithSigner`
3. If `--no-sign` flag is set, skip

### Step 6: Run full test suite

Run: `go test ./... -count=1`
Expected: All pass

### Step 7: Commit

```bash
git add pkg/repo/signing.go pkg/repo/signing_test.go pkg/userconfig/config.go cmd/graft/cmd_auth.go cmd/graft/cmd_commit.go
buckley commit --yes --minimal-output
```

---

## Task 7: Refactor `pull` to Use `Fetch`

Now that fetch is standalone, refactor pull to call it.

**Files:**
- Modify: `cmd/graft/cmd_pull.go`

### Step 1: Refactor pull to call Fetch internally

The current pull command does fetch + fast-forward inline. Replace the fetch portion with `r.Fetch(remoteName)`, then do the fast-forward/merge step using the tracking ref that fetch populated.

### Step 2: Run existing pull tests

Run: `go test ./cmd/graft/ -run Pull -v` (if pull tests exist) and `go test ./... -count=1`
Expected: All pass — behavior unchanged

### Step 3: Commit

```bash
git add cmd/graft/cmd_pull.go
buckley commit --yes --minimal-output
```

---

## Implementation Notes

### Patterns to Follow
- **Error wrapping:** `fmt.Errorf("context: %w", err)` everywhere
- **Atomic writes:** temp file + rename for any state file
- **CAS ref updates:** `UpdateRefCAS()` for concurrent safety
- **Test helpers:** Use `writeFile(t, dir, name, content)` for test setup
- **CLI output:** `cmd.OutOrStdout()` for testability
- **Short hashes:** First 8 chars for display

### What to Check Before Each Task
- Read the files you're about to modify — patterns may have changed
- Check if helper functions already exist (avoid duplication)
- Run `go vet ./...` and `go build ./...` after each change
- Run full test suite before committing

### Key Files Reference
- Repo struct: `pkg/repo/repo.go`
- Object types: `pkg/object/types.go`
- Store API: `pkg/object/store.go`
- Staging: `pkg/repo/staging.go`
- Refs: `pkg/repo/refs.go`
- Merge: `pkg/repo/merge.go`
- CLI registration: `cmd/graft/main.go`
- User config: `pkg/userconfig/config.go`
- Existing SSH signing: `cmd/graft/signing_ssh.go`
