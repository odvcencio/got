# Graft Modules Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Implement a zero-ceremony, branch-tracking, merge-aware module system that replaces git submodules with a go.mod-inspired workflow.

**Architecture:** Modules are declared in `.graftmodules` (INI-style, human-edited), locked in `.graftmodules.lock` (JSON, auto-generated), and share the parent repo's object store. Module working trees appear as subdirectories with symlinked `.graft` dirs. Tree entries use mode `160000` (gitlink). The merge engine resolves module version conflicts using generation numbers (newer wins).

**Tech Stack:** Go, existing graft packages (`pkg/object`, `pkg/repo`, `pkg/remote`), Cobra CLI framework

**Design Doc:** `docs/plans/2026-03-02-graft-modules-design.md`

---

## Phase 1: Data Model & Parsing

### Task 1: Add TreeModeModule constant

**Files:**
- Modify: `pkg/object/types.go:21-26`
- Test: `pkg/object/types_test.go` (create if needed)

**Step 1: Add the constant**

In `pkg/object/types.go`, add `TreeModeModule` to the existing constants block:

```go
const (
	// Tree mode constants compatible with Git's canonical mode strings.
	TreeModeDir        = "40000"
	TreeModeFile       = "100644"
	TreeModeExecutable = "100755"
	TreeModeModule     = "160000"
)
```

**Step 2: Verify the project builds**

Run: `go build ./...`
Expected: Build succeeds

**Step 3: Commit**

```bash
git add pkg/object/types.go
buckley commit --yes --minimal-output
```

---

### Task 2: Create `.graftmodules` parser

**Files:**
- Create: `pkg/repo/modules_config.go`
- Create: `pkg/repo/modules_config_test.go`

**Context:** The `.graftmodules` file uses INI-style syntax with `[module "name"]` sections. Each section has `url`, `path`, `track` (branch), and/or `pin` (tag/commit) fields. `track` and `pin` are mutually exclusive.

**Step 1: Write the failing tests**

Create `pkg/repo/modules_config_test.go`:

```go
package repo

import (
	"strings"
	"testing"
)

func TestParseGraftModules_Basic(t *testing.T) {
	input := `[module "ui-kit"]
  url = github:myorg/ui-kit
  path = vendor/ui-kit
  track = main

[module "proto"]
  url = orchard:myorg/proto
  path = lib/proto
  pin = v2.3.0
`
	modules, err := ParseGraftModules(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(modules) != 2 {
		t.Fatalf("expected 2 modules, got %d", len(modules))
	}

	ui := modules[0]
	if ui.Name != "ui-kit" {
		t.Errorf("name: got %q, want %q", ui.Name, "ui-kit")
	}
	if ui.URL != "github:myorg/ui-kit" {
		t.Errorf("url: got %q, want %q", ui.URL, "github:myorg/ui-kit")
	}
	if ui.Path != "vendor/ui-kit" {
		t.Errorf("path: got %q, want %q", ui.Path, "vendor/ui-kit")
	}
	if ui.Track != "main" {
		t.Errorf("track: got %q, want %q", ui.Track, "main")
	}
	if ui.Pin != "" {
		t.Errorf("pin should be empty, got %q", ui.Pin)
	}

	proto := modules[1]
	if proto.Name != "proto" {
		t.Errorf("name: got %q, want %q", proto.Name, "proto")
	}
	if proto.Pin != "v2.3.0" {
		t.Errorf("pin: got %q, want %q", proto.Pin, "v2.3.0")
	}
	if proto.Track != "" {
		t.Errorf("track should be empty, got %q", proto.Track)
	}
}

func TestParseGraftModules_Empty(t *testing.T) {
	modules, err := ParseGraftModules(strings.NewReader(""))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(modules) != 0 {
		t.Fatalf("expected 0 modules, got %d", len(modules))
	}
}

func TestParseGraftModules_TrackAndPinConflict(t *testing.T) {
	input := `[module "bad"]
  url = github:myorg/bad
  path = vendor/bad
  track = main
  pin = v1.0
`
	_, err := ParseGraftModules(strings.NewReader(input))
	if err == nil {
		t.Fatal("expected error for track+pin conflict")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("error should mention mutually exclusive, got: %v", err)
	}
}

func TestParseGraftModules_MissingURL(t *testing.T) {
	input := `[module "nourl"]
  path = vendor/nourl
  track = main
`
	_, err := ParseGraftModules(strings.NewReader(input))
	if err == nil {
		t.Fatal("expected error for missing url")
	}
}

func TestParseGraftModules_MissingPath(t *testing.T) {
	input := `[module "nopath"]
  url = github:myorg/nopath
  track = main
`
	_, err := ParseGraftModules(strings.NewReader(input))
	if err == nil {
		t.Fatal("expected error for missing path")
	}
}

func TestParseGraftModules_DuplicateName(t *testing.T) {
	input := `[module "dup"]
  url = github:myorg/dup1
  path = vendor/dup1
  track = main

[module "dup"]
  url = github:myorg/dup2
  path = vendor/dup2
  track = main
`
	_, err := ParseGraftModules(strings.NewReader(input))
	if err == nil {
		t.Fatal("expected error for duplicate name")
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("error should mention duplicate, got: %v", err)
	}
}

func TestParseGraftModules_DuplicatePath(t *testing.T) {
	input := `[module "a"]
  url = github:myorg/a
  path = vendor/shared
  track = main

[module "b"]
  url = github:myorg/b
  path = vendor/shared
  track = main
`
	_, err := ParseGraftModules(strings.NewReader(input))
	if err == nil {
		t.Fatal("expected error for duplicate path")
	}
	if !strings.Contains(err.Error(), "duplicate path") {
		t.Errorf("error should mention duplicate path, got: %v", err)
	}
}

func TestWriteGraftModules(t *testing.T) {
	modules := []ModuleEntry{
		{Name: "ui-kit", URL: "github:myorg/ui-kit", Path: "vendor/ui-kit", Track: "main"},
		{Name: "proto", URL: "orchard:myorg/proto", Path: "lib/proto", Pin: "v2.3.0"},
	}

	var buf strings.Builder
	if err := WriteGraftModules(&buf, modules); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Round-trip: parse the output back.
	parsed, err := ParseGraftModules(strings.NewReader(buf.String()))
	if err != nil {
		t.Fatalf("round-trip parse failed: %v", err)
	}
	if len(parsed) != 2 {
		t.Fatalf("round-trip: expected 2 modules, got %d", len(parsed))
	}
	if parsed[0].Name != "ui-kit" || parsed[0].Track != "main" {
		t.Errorf("round-trip mismatch for ui-kit: %+v", parsed[0])
	}
	if parsed[1].Name != "proto" || parsed[1].Pin != "v2.3.0" {
		t.Errorf("round-trip mismatch for proto: %+v", parsed[1])
	}
}

func TestParseGraftModules_CommentsAndWhitespace(t *testing.T) {
	input := `# This is a comment
[module "ui-kit"]
  url = github:myorg/ui-kit
  path = vendor/ui-kit
  track = main
  # inline comment
`
	modules, err := ParseGraftModules(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(modules) != 1 {
		t.Fatalf("expected 1 module, got %d", len(modules))
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./pkg/repo/ -run TestParseGraftModules -v -count=1`
Expected: FAIL — `ParseGraftModules` undefined

**Step 3: Implement the parser**

Create `pkg/repo/modules_config.go`:

```go
package repo

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ModuleEntry represents one module declared in .graftmodules.
type ModuleEntry struct {
	Name  string // unique identifier
	URL   string // remote URL (supports graft shorthand)
	Path  string // working tree path relative to repo root
	Track string // branch to follow (mutually exclusive with Pin)
	Pin   string // tag or commit to lock to (mutually exclusive with Track)
}

// ParseGraftModules parses .graftmodules INI-style content into a slice of ModuleEntry.
// Format:
//
//	[module "name"]
//	  url = value
//	  path = value
//	  track = branch   (mutually exclusive with pin)
//	  pin = tag        (mutually exclusive with track)
func ParseGraftModules(r io.Reader) ([]ModuleEntry, error) {
	var modules []ModuleEntry
	var current *ModuleEntry
	seenNames := make(map[string]bool)
	seenPaths := make(map[string]bool)

	scanner := bufio.NewScanner(r)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments.
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}

		// Section header: [module "name"]
		if strings.HasPrefix(line, "[") {
			// Finalize previous module.
			if current != nil {
				if err := validateModuleEntry(current); err != nil {
					return nil, fmt.Errorf("module %q: %w", current.Name, err)
				}
				modules = append(modules, *current)
			}

			name, err := parseModuleSectionHeader(line)
			if err != nil {
				return nil, fmt.Errorf("line %d: %w", lineNum, err)
			}
			if seenNames[name] {
				return nil, fmt.Errorf("line %d: duplicate module name %q", lineNum, name)
			}
			seenNames[name] = true
			current = &ModuleEntry{Name: name}
			continue
		}

		// Key-value: key = value
		if current == nil {
			return nil, fmt.Errorf("line %d: key-value outside of [module] section", lineNum)
		}

		key, value, err := parseKeyValue(line)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNum, err)
		}

		switch key {
		case "url":
			current.URL = value
		case "path":
			current.Path = value
		case "track":
			current.Track = value
		case "pin":
			current.Pin = value
		default:
			return nil, fmt.Errorf("line %d: unknown key %q in module %q", lineNum, key, current.Name)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	// Finalize last module.
	if current != nil {
		if err := validateModuleEntry(current); err != nil {
			return nil, fmt.Errorf("module %q: %w", current.Name, err)
		}
		modules = append(modules, *current)
	}

	// Check for duplicate paths.
	for _, m := range modules {
		if seenPaths[m.Path] {
			return nil, fmt.Errorf("duplicate path %q", m.Path)
		}
		seenPaths[m.Path] = true
	}

	return modules, nil
}

func parseModuleSectionHeader(line string) (string, error) {
	// Expected: [module "name"]
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "[module ") || !strings.HasSuffix(line, "]") {
		return "", fmt.Errorf("invalid section header: %s", line)
	}
	inner := line[len("[module ") : len(line)-1]
	inner = strings.TrimSpace(inner)
	if !strings.HasPrefix(inner, "\"") || !strings.HasSuffix(inner, "\"") {
		return "", fmt.Errorf("module name must be quoted: %s", line)
	}
	name := inner[1 : len(inner)-1]
	if strings.TrimSpace(name) == "" {
		return "", fmt.Errorf("module name cannot be empty")
	}
	return name, nil
}

func parseKeyValue(line string) (string, string, error) {
	// Strip inline comments.
	if idx := strings.Index(line, " #"); idx >= 0 {
		line = line[:idx]
	}
	if idx := strings.Index(line, " ;"); idx >= 0 {
		line = line[:idx]
	}

	parts := strings.SplitN(line, "=", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("expected key = value, got: %s", line)
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), nil
}

func validateModuleEntry(m *ModuleEntry) error {
	if m.URL == "" {
		return fmt.Errorf("url is required")
	}
	if m.Path == "" {
		return fmt.Errorf("path is required")
	}
	if m.Track != "" && m.Pin != "" {
		return fmt.Errorf("track and pin are mutually exclusive")
	}
	return nil
}

// WriteGraftModules writes module entries in .graftmodules INI format.
func WriteGraftModules(w io.Writer, modules []ModuleEntry) error {
	for i, m := range modules {
		if i > 0 {
			fmt.Fprintln(w)
		}
		fmt.Fprintf(w, "[module %q]\n", m.Name)
		fmt.Fprintf(w, "  url = %s\n", m.URL)
		fmt.Fprintf(w, "  path = %s\n", m.Path)
		if m.Track != "" {
			fmt.Fprintf(w, "  track = %s\n", m.Track)
		}
		if m.Pin != "" {
			fmt.Fprintf(w, "  pin = %s\n", m.Pin)
		}
	}
	return nil
}

// ReadGraftModulesFile reads and parses the .graftmodules file from the repo root.
// Returns nil, nil if the file does not exist.
func (r *Repo) ReadGraftModulesFile() ([]ModuleEntry, error) {
	p := filepath.Join(r.RootDir, ".graftmodules")
	f, err := os.Open(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	return ParseGraftModules(f)
}

// WriteGraftModulesFile writes module entries to the .graftmodules file in the repo root.
func (r *Repo) WriteGraftModulesFile(modules []ModuleEntry) error {
	p := filepath.Join(r.RootDir, ".graftmodules")
	f, err := os.Create(p)
	if err != nil {
		return err
	}
	defer f.Close()
	return WriteGraftModules(f, modules)
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./pkg/repo/ -run TestParseGraftModules -v -count=1`
Expected: All PASS

Run: `go test ./pkg/repo/ -run TestWriteGraftModules -v -count=1`
Expected: PASS

**Step 5: Commit**

```bash
git add pkg/repo/modules_config.go pkg/repo/modules_config_test.go
buckley commit --yes --minimal-output
```

---

### Task 3: Create `.graftmodules.lock` reader/writer

**Files:**
- Create: `pkg/repo/modules_lock.go`
- Create: `pkg/repo/modules_lock_test.go`

**Context:** The lock file is JSON. It records resolved commit hashes for reproducible builds.

**Step 1: Write the failing tests**

Create `pkg/repo/modules_lock_test.go`:

```go
package repo

import (
	"testing"

	"github.com/odvcencio/graft/pkg/object"
)

func TestModuleLock_ReadWriteRoundTrip(t *testing.T) {
	dir := t.TempDir()
	r := &Repo{RootDir: dir}

	lock := &ModuleLock{
		Modules: map[string]ModuleLockEntry{
			"ui-kit": {
				Commit: object.Hash("abc123def456"),
				URL:    "https://github.com/myorg/ui-kit.git",
				Track:  "main",
			},
			"proto": {
				Commit: object.Hash("789def012345"),
				URL:    "https://orchard.example.com/myorg/proto",
				Pin:    "v2.3.0",
			},
		},
	}

	if err := r.WriteModuleLock(lock); err != nil {
		t.Fatalf("write lock: %v", err)
	}

	got, err := r.ReadModuleLock()
	if err != nil {
		t.Fatalf("read lock: %v", err)
	}
	if len(got.Modules) != 2 {
		t.Fatalf("expected 2 modules, got %d", len(got.Modules))
	}

	uiKit := got.Modules["ui-kit"]
	if uiKit.Commit != "abc123def456" {
		t.Errorf("ui-kit commit: got %q", uiKit.Commit)
	}
	if uiKit.URL != "https://github.com/myorg/ui-kit.git" {
		t.Errorf("ui-kit url: got %q", uiKit.URL)
	}
	if uiKit.Track != "main" {
		t.Errorf("ui-kit track: got %q", uiKit.Track)
	}

	proto := got.Modules["proto"]
	if proto.Pin != "v2.3.0" {
		t.Errorf("proto pin: got %q", proto.Pin)
	}
}

func TestModuleLock_NotExist(t *testing.T) {
	dir := t.TempDir()
	r := &Repo{RootDir: dir}

	lock, err := r.ReadModuleLock()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if lock != nil {
		t.Fatalf("expected nil for nonexistent lock, got %+v", lock)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./pkg/repo/ -run TestModuleLock -v -count=1`
Expected: FAIL — types undefined

**Step 3: Implement the lock file reader/writer**

Create `pkg/repo/modules_lock.go`:

```go
package repo

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/odvcencio/graft/pkg/object"
)

// ModuleLockEntry records the resolved state of one module.
type ModuleLockEntry struct {
	Commit object.Hash `json:"commit"`
	URL    string      `json:"url"`
	Track  string      `json:"track,omitempty"`
	Pin    string      `json:"pin,omitempty"`
}

// ModuleLock is the contents of .graftmodules.lock.
type ModuleLock struct {
	Modules map[string]ModuleLockEntry `json:"modules"`
}

// ReadModuleLock reads and parses .graftmodules.lock from the repo root.
// Returns nil, nil if the file does not exist.
func (r *Repo) ReadModuleLock() (*ModuleLock, error) {
	p := filepath.Join(r.RootDir, ".graftmodules.lock")
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var lock ModuleLock
	if err := json.Unmarshal(data, &lock); err != nil {
		return nil, err
	}
	return &lock, nil
}

// WriteModuleLock writes the lock file to .graftmodules.lock in the repo root.
// Uses atomic write via temp file + rename.
func (r *Repo) WriteModuleLock(lock *ModuleLock) error {
	p := filepath.Join(r.RootDir, ".graftmodules.lock")
	data, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./pkg/repo/ -run TestModuleLock -v -count=1`
Expected: All PASS

**Step 5: Commit**

```bash
git add pkg/repo/modules_lock.go pkg/repo/modules_lock_test.go
buckley commit --yes --minimal-output
```

---

## Phase 2: Tree Integration

### Task 4: Handle module entries in BuildTree

**Files:**
- Modify: `pkg/repo/staging.go` (StagingEntry — needs to support module mode)
- Modify: `pkg/repo/tree.go` (BuildTree — emit mode 160000 entries)
- Create: `pkg/repo/tree_module_test.go`

**Context:** Module entries appear in the staging index with mode `160000`. Their `BlobHash` stores the pinned commit hash (not actual blob content). `BuildTree` must emit these as non-directory, non-file TreeEntry objects with mode `160000`. `FlattenTree` must NOT recurse into them.

**Step 1: Write the failing tests**

Create `pkg/repo/tree_module_test.go`:

```go
package repo

import (
	"testing"

	"github.com/odvcencio/graft/pkg/object"
)

func TestBuildTree_ModuleEntry(t *testing.T) {
	store := object.NewStore(t.TempDir())
	r := &Repo{Store: store, RootDir: t.TempDir(), GraftDir: t.TempDir()}

	// Create staging with a normal file and a module entry.
	s := &Staging{Entries: map[string]*StagingEntry{
		"README.md": {
			Path:     "README.md",
			BlobHash: mustWriteBlob(t, store, "hello"),
			Mode:     object.TreeModeFile,
		},
		"vendor/ui-kit": {
			Path:     "vendor/ui-kit",
			BlobHash: object.Hash("abc123def456abc123def456abc123def456abc123def456abc123def456abcd"),
			Mode:     object.TreeModeModule,
		},
	}}

	treeHash, err := r.BuildTree(s)
	if err != nil {
		t.Fatalf("BuildTree: %v", err)
	}

	// Read the root tree and find the vendor subtree.
	rootTree, err := store.ReadTree(treeHash)
	if err != nil {
		t.Fatalf("ReadTree root: %v", err)
	}

	var vendorEntry *object.TreeEntry
	for i := range rootTree.Entries {
		if rootTree.Entries[i].Name == "vendor" {
			vendorEntry = &rootTree.Entries[i]
			break
		}
	}
	if vendorEntry == nil {
		t.Fatal("vendor subtree not found in root tree")
	}
	if !vendorEntry.IsDir {
		t.Fatal("vendor should be a directory")
	}

	// Read the vendor subtree.
	vendorTree, err := store.ReadTree(vendorEntry.SubtreeHash)
	if err != nil {
		t.Fatalf("ReadTree vendor: %v", err)
	}

	var moduleEntry *object.TreeEntry
	for i := range vendorTree.Entries {
		if vendorTree.Entries[i].Name == "ui-kit" {
			moduleEntry = &vendorTree.Entries[i]
			break
		}
	}
	if moduleEntry == nil {
		t.Fatal("ui-kit module entry not found in vendor tree")
	}
	if moduleEntry.Mode != object.TreeModeModule {
		t.Errorf("module mode: got %q, want %q", moduleEntry.Mode, object.TreeModeModule)
	}
	if moduleEntry.IsDir {
		t.Error("module entry should not be IsDir")
	}
	if moduleEntry.BlobHash != "abc123def456abc123def456abc123def456abc123def456abc123def456abcd" {
		t.Errorf("module BlobHash mismatch: %s", moduleEntry.BlobHash)
	}
}

func TestFlattenTree_SkipsModuleEntries(t *testing.T) {
	store := object.NewStore(t.TempDir())
	r := &Repo{Store: store, RootDir: t.TempDir(), GraftDir: t.TempDir()}

	// Build a tree with a module entry.
	moduleHash := object.Hash("abc123def456abc123def456abc123def456abc123def456abc123def456abcd")
	readmeBlob := mustWriteBlob(t, store, "hello")

	tree := &object.TreeObj{Entries: []object.TreeEntry{
		{Name: "README.md", Mode: object.TreeModeFile, BlobHash: readmeBlob},
		{Name: "vendor", IsDir: true, Mode: object.TreeModeDir, SubtreeHash: ""},
	}}

	// Build inner tree for vendor/ containing the module entry.
	vendorTree := &object.TreeObj{Entries: []object.TreeEntry{
		{Name: "ui-kit", Mode: object.TreeModeModule, BlobHash: moduleHash},
	}}
	vendorHash, err := store.WriteTree(vendorTree)
	if err != nil {
		t.Fatalf("WriteTree vendor: %v", err)
	}
	tree.Entries[1].SubtreeHash = vendorHash

	rootHash, err := store.WriteTree(tree)
	if err != nil {
		t.Fatalf("WriteTree root: %v", err)
	}

	// Flatten should return only README.md, not the module entry.
	files, err := r.FlattenTree(rootHash)
	if err != nil {
		t.Fatalf("FlattenTree: %v", err)
	}

	if len(files) != 1 {
		t.Fatalf("expected 1 file entry, got %d", len(files))
	}
	if files[0].Path != "README.md" {
		t.Errorf("expected README.md, got %s", files[0].Path)
	}
}

func TestFlattenTreeWithModules(t *testing.T) {
	store := object.NewStore(t.TempDir())
	r := &Repo{Store: store, RootDir: t.TempDir(), GraftDir: t.TempDir()}

	moduleHash := object.Hash("abc123def456abc123def456abc123def456abc123def456abc123def456abcd")
	readmeBlob := mustWriteBlob(t, store, "hello")

	vendorTree := &object.TreeObj{Entries: []object.TreeEntry{
		{Name: "ui-kit", Mode: object.TreeModeModule, BlobHash: moduleHash},
	}}
	vendorHash, err := store.WriteTree(vendorTree)
	if err != nil {
		t.Fatalf("WriteTree vendor: %v", err)
	}

	tree := &object.TreeObj{Entries: []object.TreeEntry{
		{Name: "README.md", Mode: object.TreeModeFile, BlobHash: readmeBlob},
		{Name: "vendor", IsDir: true, Mode: object.TreeModeDir, SubtreeHash: vendorHash},
	}}
	rootHash, err := store.WriteTree(tree)
	if err != nil {
		t.Fatalf("WriteTree root: %v", err)
	}

	// FlattenTreeWithModules should return both files and module entries.
	files, modules, err := r.FlattenTreeWithModules(rootHash)
	if err != nil {
		t.Fatalf("FlattenTreeWithModules: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	if len(modules) != 1 {
		t.Fatalf("expected 1 module, got %d", len(modules))
	}
	if modules[0].Path != "vendor/ui-kit" {
		t.Errorf("module path: got %q", modules[0].Path)
	}
	if modules[0].BlobHash != moduleHash {
		t.Errorf("module commit hash mismatch")
	}
}

func mustWriteBlob(t *testing.T, store *object.Store, content string) object.Hash {
	t.Helper()
	h, err := store.WriteBlob([]byte(content))
	if err != nil {
		t.Fatalf("WriteBlob: %v", err)
	}
	return h
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./pkg/repo/ -run "TestBuildTree_Module|TestFlattenTree_Skips|TestFlattenTreeWithModules" -v -count=1`
Expected: FAIL — `TreeModeModule` undefined, `FlattenTreeWithModules` undefined

**Step 3: Modify BuildTree to handle module entries**

In `pkg/repo/tree.go`, modify `buildTreeDir` to recognize module entries (mode `160000`). A staging entry with mode `160000` is treated like a file (not a directory) — it gets a TreeEntry with `IsDir: false`, `Mode: TreeModeModule`, and `BlobHash` set to the commit hash.

The key change is in the file loop within `buildTreeDir`. Currently it only checks `files[name]` vs subdirectory. Add a check: if the entry's Mode is `TreeModeModule`, treat it as a leaf (not a subdirectory prefix), even if its path contains slashes.

Actually, module entries in staging use paths like `vendor/ui-kit`. The current logic splits on `/` and would put `vendor` in `subdirs` and `ui-kit` as a file within the `vendor` prefix. That already works correctly because `vendor/ui-kit` with mode `160000` would be a direct child of the `vendor` prefix. The entry is treated as a file because it has no further `/` after the prefix. So `buildTreeDir` with prefix `vendor` would find `ui-kit` as a direct child file — the existing code already handles this.

However, we need to make sure the TreeEntry gets `Mode: TreeModeModule` instead of `TreeModeFile`. Currently the code calls `normalizeFileMode(entry.Mode)` which doesn't know about `160000`. Update `normalizeFileMode`:

In `pkg/repo/filemode.go` (or wherever `normalizeFileMode` is defined), ensure it passes through `160000`:

```go
func normalizeFileMode(mode string) string {
	switch mode {
	case object.TreeModeExecutable, "755":
		return object.TreeModeExecutable
	case object.TreeModeModule:
		return object.TreeModeModule
	default:
		return object.TreeModeFile
	}
}
```

**Step 4: Modify FlattenTree to skip module entries**

In `pkg/repo/tree.go`, modify `flattenTreeInto` to skip entries with mode `160000`:

```go
for _, entry := range treeObj.Entries {
	// ... existing fullPath computation ...

	if entry.Mode == object.TreeModeModule {
		// Module entries are gitlinks — do not recurse or include as files.
		continue
	}

	if entry.IsDir {
		// ... existing directory recursion ...
	} else {
		// ... existing file append ...
	}
}
```

**Step 5: Add FlattenTreeWithModules**

Add a new method to `pkg/repo/tree.go` that returns both files and module entries:

```go
// TreeModuleEntry represents a module (gitlink) entry found in a tree.
type TreeModuleEntry struct {
	Path     string      // full path with forward slashes
	BlobHash object.Hash // the module's pinned commit hash
}

// FlattenTreeWithModules walks a tree object recursively, returning file entries
// and module entries separately. Module entries (mode 160000) are not recursed into.
func (r *Repo) FlattenTreeWithModules(h object.Hash) ([]TreeFileEntry, []TreeModuleEntry, error) {
	var files []TreeFileEntry
	var modules []TreeModuleEntry
	if err := r.flattenTreeWithModulesInto(h, "", &files, &modules); err != nil {
		return nil, nil, err
	}
	return files, modules, nil
}

func (r *Repo) flattenTreeWithModulesInto(h object.Hash, prefix string, files *[]TreeFileEntry, modules *[]TreeModuleEntry) error {
	treeObj, err := r.Store.ReadTree(h)
	if err != nil {
		return fmt.Errorf("flatten tree: read %s: %w", h, err)
	}

	for _, entry := range treeObj.Entries {
		fullPath := entry.Name
		if prefix != "" {
			fullPath = prefix + "/" + entry.Name
		}

		if entry.Mode == object.TreeModeModule {
			*modules = append(*modules, TreeModuleEntry{
				Path:     fullPath,
				BlobHash: entry.BlobHash,
			})
			continue
		}

		if entry.IsDir {
			if err := r.flattenTreeWithModulesInto(entry.SubtreeHash, fullPath, files, modules); err != nil {
				return err
			}
		} else {
			*files = append(*files, TreeFileEntry{
				Path:           fullPath,
				BlobHash:       entry.BlobHash,
				EntityListHash: entry.EntityListHash,
				Mode:           normalizeFileMode(entry.Mode),
			})
		}
	}
	return nil
}
```

**Step 6: Run tests to verify they pass**

Run: `go test ./pkg/repo/ -run "TestBuildTree_Module|TestFlattenTree_Skips|TestFlattenTreeWithModules" -v -count=1`
Expected: All PASS

**Step 7: Run full test suite to check for regressions**

Run: `go test ./pkg/repo/ -count=1`
Expected: All existing tests still pass

**Step 8: Commit**

```bash
git add pkg/object/types.go pkg/repo/tree.go pkg/repo/tree_module_test.go pkg/repo/filemode.go
buckley commit --yes --minimal-output
```

---

## Phase 3: Module Struct & Repo Integration

### Task 5: Module struct and Repo module methods

**Files:**
- Create: `pkg/repo/module.go`
- Create: `pkg/repo/module_test.go`

**Context:** The `Module` struct binds a `ModuleEntry` (from .graftmodules) with its resolved state (from .graftmodules.lock). The Repo gets methods: `ListModules()`, `GetModule()`, `AddModule()`, `RemoveModule()`, `UpdateModuleLock()`.

**Step 1: Write the failing tests**

Create `pkg/repo/module_test.go`:

```go
package repo

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/odvcencio/graft/pkg/object"
)

func TestRepo_ListModules_Empty(t *testing.T) {
	r := createTestRepo(t)

	modules, err := r.ListModules()
	if err != nil {
		t.Fatalf("ListModules: %v", err)
	}
	if len(modules) != 0 {
		t.Fatalf("expected 0 modules, got %d", len(modules))
	}
}

func TestRepo_AddModule(t *testing.T) {
	r := createTestRepo(t)

	err := r.AddModuleEntry(ModuleEntry{
		Name:  "ui-kit",
		URL:   "github:myorg/ui-kit",
		Path:  "vendor/ui-kit",
		Track: "main",
	})
	if err != nil {
		t.Fatalf("AddModuleEntry: %v", err)
	}

	// Verify .graftmodules was written.
	modules, err := r.ReadGraftModulesFile()
	if err != nil {
		t.Fatalf("ReadGraftModulesFile: %v", err)
	}
	if len(modules) != 1 {
		t.Fatalf("expected 1 module, got %d", len(modules))
	}
	if modules[0].Name != "ui-kit" {
		t.Errorf("name: got %q", modules[0].Name)
	}

	// Verify module metadata dir was created.
	metaDir := filepath.Join(r.GraftDir, "modules", "ui-kit")
	if _, err := os.Stat(metaDir); err != nil {
		t.Errorf("module metadata dir should exist: %v", err)
	}
}

func TestRepo_AddModule_DuplicateName(t *testing.T) {
	r := createTestRepo(t)

	err := r.AddModuleEntry(ModuleEntry{
		Name: "ui-kit", URL: "github:myorg/ui-kit",
		Path: "vendor/ui-kit", Track: "main",
	})
	if err != nil {
		t.Fatalf("first add: %v", err)
	}

	err = r.AddModuleEntry(ModuleEntry{
		Name: "ui-kit", URL: "github:myorg/other",
		Path: "vendor/other", Track: "main",
	})
	if err == nil {
		t.Fatal("expected error for duplicate module name")
	}
}

func TestRepo_RemoveModule(t *testing.T) {
	r := createTestRepo(t)

	_ = r.AddModuleEntry(ModuleEntry{
		Name: "ui-kit", URL: "github:myorg/ui-kit",
		Path: "vendor/ui-kit", Track: "main",
	})

	if err := r.RemoveModuleEntry("ui-kit"); err != nil {
		t.Fatalf("RemoveModuleEntry: %v", err)
	}

	modules, err := r.ReadGraftModulesFile()
	if err != nil {
		t.Fatalf("ReadGraftModulesFile: %v", err)
	}
	if len(modules) != 0 {
		t.Fatalf("expected 0 modules after remove, got %d", len(modules))
	}

	// Metadata dir should be gone.
	metaDir := filepath.Join(r.GraftDir, "modules", "ui-kit")
	if _, err := os.Stat(metaDir); !os.IsNotExist(err) {
		t.Error("module metadata dir should be removed")
	}
}

func TestRepo_RemoveModule_NotFound(t *testing.T) {
	r := createTestRepo(t)

	err := r.RemoveModuleEntry("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent module")
	}
}

func TestRepo_UpdateModuleLock(t *testing.T) {
	r := createTestRepo(t)

	_ = r.AddModuleEntry(ModuleEntry{
		Name: "ui-kit", URL: "github:myorg/ui-kit",
		Path: "vendor/ui-kit", Track: "main",
	})

	commitHash := object.Hash("abc123def456abc123def456abc123def456abc123def456abc123def456abcd")
	err := r.UpdateModuleLock("ui-kit", commitHash, "https://github.com/myorg/ui-kit.git")
	if err != nil {
		t.Fatalf("UpdateModuleLock: %v", err)
	}

	lock, err := r.ReadModuleLock()
	if err != nil {
		t.Fatalf("ReadModuleLock: %v", err)
	}
	if lock.Modules["ui-kit"].Commit != commitHash {
		t.Errorf("lock commit mismatch: got %q", lock.Modules["ui-kit"].Commit)
	}
}

func TestRepo_ModuleMetadataDir(t *testing.T) {
	r := createTestRepo(t)

	dir := r.ModuleMetadataDir("ui-kit")
	expected := filepath.Join(r.GraftDir, "modules", "ui-kit")
	if dir != expected {
		t.Errorf("ModuleMetadataDir: got %q, want %q", dir, expected)
	}
}

// createTestRepo initializes a minimal repo for module tests.
func createTestRepo(t *testing.T) *Repo {
	t.Helper()
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	return r
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./pkg/repo/ -run "TestRepo_ListModules|TestRepo_AddModule|TestRepo_RemoveModule|TestRepo_UpdateModuleLock|TestRepo_ModuleMetadataDir" -v -count=1`
Expected: FAIL — methods undefined

**Step 3: Implement the module methods**

Create `pkg/repo/module.go`:

```go
package repo

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/odvcencio/graft/pkg/object"
)

// Module represents a resolved module combining config and lock state.
type Module struct {
	ModuleEntry                  // embedded config from .graftmodules
	Commit      object.Hash     // resolved commit from lock file
	ResolvedURL string          // canonicalized URL from lock file
}

// ListModules returns all modules with their resolved state.
func (r *Repo) ListModules() ([]Module, error) {
	entries, err := r.ReadGraftModulesFile()
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, nil
	}

	lock, err := r.ReadModuleLock()
	if err != nil {
		return nil, err
	}

	modules := make([]Module, len(entries))
	for i, entry := range entries {
		m := Module{ModuleEntry: entry}
		if lock != nil {
			if lockEntry, ok := lock.Modules[entry.Name]; ok {
				m.Commit = lockEntry.Commit
				m.ResolvedURL = lockEntry.URL
			}
		}
		modules[i] = m
	}
	return modules, nil
}

// GetModule returns a single module by name.
func (r *Repo) GetModule(name string) (*Module, error) {
	modules, err := r.ListModules()
	if err != nil {
		return nil, err
	}
	for _, m := range modules {
		if m.Name == name {
			return &m, nil
		}
	}
	return nil, fmt.Errorf("module %q not found", name)
}

// AddModuleEntry appends a module to .graftmodules and creates its metadata directory.
func (r *Repo) AddModuleEntry(entry ModuleEntry) error {
	existing, err := r.ReadGraftModulesFile()
	if err != nil {
		return err
	}

	// Check for duplicates.
	for _, m := range existing {
		if m.Name == entry.Name {
			return fmt.Errorf("module %q already exists", entry.Name)
		}
		if m.Path == entry.Path {
			return fmt.Errorf("path %q already used by module %q", entry.Path, m.Name)
		}
	}

	existing = append(existing, entry)
	if err := r.WriteGraftModulesFile(existing); err != nil {
		return err
	}

	// Create module metadata directory.
	metaDir := r.ModuleMetadataDir(entry.Name)
	if err := os.MkdirAll(metaDir, 0o755); err != nil {
		return fmt.Errorf("create module metadata dir: %w", err)
	}
	// Create refs subdirectory.
	if err := os.MkdirAll(filepath.Join(metaDir, "refs"), 0o755); err != nil {
		return fmt.Errorf("create module refs dir: %w", err)
	}

	return nil
}

// RemoveModuleEntry removes a module from .graftmodules, its lock entry, and metadata dir.
func (r *Repo) RemoveModuleEntry(name string) error {
	existing, err := r.ReadGraftModulesFile()
	if err != nil {
		return err
	}

	found := false
	filtered := make([]ModuleEntry, 0, len(existing))
	for _, m := range existing {
		if m.Name == name {
			found = true
			continue
		}
		filtered = append(filtered, m)
	}
	if !found {
		return fmt.Errorf("module %q not found", name)
	}

	if err := r.WriteGraftModulesFile(filtered); err != nil {
		return err
	}

	// Remove from lock file if present.
	lock, err := r.ReadModuleLock()
	if err == nil && lock != nil {
		delete(lock.Modules, name)
		_ = r.WriteModuleLock(lock)
	}

	// Remove metadata directory.
	metaDir := r.ModuleMetadataDir(name)
	_ = os.RemoveAll(metaDir)

	return nil
}

// UpdateModuleLock updates the lock file entry for a single module.
func (r *Repo) UpdateModuleLock(name string, commit object.Hash, resolvedURL string) error {
	// Verify module exists in .graftmodules.
	entry, err := r.GetModule(name)
	if err != nil {
		return err
	}

	lock, err := r.ReadModuleLock()
	if err != nil {
		return err
	}
	if lock == nil {
		lock = &ModuleLock{Modules: make(map[string]ModuleLockEntry)}
	}

	lockEntry := ModuleLockEntry{
		Commit: commit,
		URL:    resolvedURL,
	}
	if entry.Track != "" {
		lockEntry.Track = entry.Track
	}
	if entry.Pin != "" {
		lockEntry.Pin = entry.Pin
	}
	lock.Modules[name] = lockEntry

	return r.WriteModuleLock(lock)
}

// ModuleMetadataDir returns the path to a module's metadata directory.
func (r *Repo) ModuleMetadataDir(name string) string {
	return filepath.Join(r.GraftDir, "modules", name)
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./pkg/repo/ -run "TestRepo_ListModules|TestRepo_AddModule|TestRepo_RemoveModule|TestRepo_UpdateModuleLock|TestRepo_ModuleMetadataDir" -v -count=1`
Expected: All PASS

**Step 5: Run full test suite**

Run: `go test ./pkg/repo/ -count=1`
Expected: All pass

**Step 6: Commit**

```bash
git add pkg/repo/module.go pkg/repo/module_test.go
buckley commit --yes --minimal-output
```

---

## Phase 4: Module Ignore Integration

### Task 6: Auto-ignore module working tree paths

**Files:**
- Modify: `pkg/repo/ignore.go`
- Create: `pkg/repo/ignore_module_test.go`

**Context:** Module working tree paths (e.g., `vendor/ui-kit/`) must be automatically ignored by the parent repo so `graft status` and `graft add` don't track module files. The IgnoreChecker should read `.graftmodules` and add each module's path as an ignored prefix.

**Step 1: Write the failing tests**

Create `pkg/repo/ignore_module_test.go`:

```go
package repo

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIgnore_ModulePaths(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Add a module.
	err = r.AddModuleEntry(ModuleEntry{
		Name: "ui-kit", URL: "github:myorg/ui-kit",
		Path: "vendor/ui-kit", Track: "main",
	})
	if err != nil {
		t.Fatalf("AddModuleEntry: %v", err)
	}

	// Create a file inside the module path.
	os.MkdirAll(filepath.Join(dir, "vendor", "ui-kit"), 0o755)
	os.WriteFile(filepath.Join(dir, "vendor", "ui-kit", "README.md"), []byte("hello"), 0o644)

	checker, err := r.LoadIgnoreChecker()
	if err != nil {
		t.Fatalf("LoadIgnoreChecker: %v", err)
	}

	// File inside module path should be ignored.
	if !checker.IsIgnored("vendor/ui-kit/README.md") {
		t.Error("vendor/ui-kit/README.md should be ignored")
	}
	if !checker.IsIgnored("vendor/ui-kit/src/main.go") {
		t.Error("vendor/ui-kit/src/main.go should be ignored")
	}

	// Files outside module path should NOT be ignored.
	if checker.IsIgnored("vendor/other/file.go") {
		t.Error("vendor/other/file.go should not be ignored")
	}
	if checker.IsIgnored("src/main.go") {
		t.Error("src/main.go should not be ignored")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./pkg/repo/ -run TestIgnore_ModulePaths -v -count=1`
Expected: FAIL — module path not ignored

**Step 3: Modify ignore checker to include module paths**

In `pkg/repo/ignore.go`, find where the `IgnoreChecker` is constructed (the `LoadIgnoreChecker` or equivalent method). After loading `.graftignore` patterns, also read `.graftmodules` and add each module's path as an ignore pattern.

Add to the checker initialization:

```go
// After loading .graftignore patterns, add module paths.
modules, err := r.ReadGraftModulesFile()
if err == nil && len(modules) > 0 {
	for _, m := range modules {
		// Add module path as ignore prefix (with trailing /).
		checker.AddPattern(m.Path + "/")
	}
}
```

The exact modification depends on how `LoadIgnoreChecker` is structured. Read the file to understand the pattern, then add module path patterns after the existing ignore rules are loaded.

**Step 4: Run tests to verify they pass**

Run: `go test ./pkg/repo/ -run TestIgnore_ModulePaths -v -count=1`
Expected: PASS

**Step 5: Run full test suite**

Run: `go test ./pkg/repo/ -count=1`
Expected: All pass

**Step 6: Commit**

```bash
git add pkg/repo/ignore.go pkg/repo/ignore_module_test.go
buckley commit --yes --minimal-output
```

---

## Phase 5: Module Sync & Checkout Integration

### Task 7: Module sync — checkout module at locked commit

**Files:**
- Create: `pkg/repo/module_sync.go`
- Create: `pkg/repo/module_sync_test.go`

**Context:** `ModuleSync` reads the lock file and ensures each module's working tree matches the locked commit. It creates the working tree directory, writes the `.graft` symlink, and checks out the module tree. This is the core operation used by clone, checkout, and `graft module sync`.

**Step 1: Write the failing tests**

Create `pkg/repo/module_sync_test.go`:

```go
package repo

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/odvcencio/graft/pkg/object"
)

func TestModuleSync_CheckoutAtCommit(t *testing.T) {
	// Create a "parent" repo.
	parentDir := t.TempDir()
	parent, err := Init(parentDir)
	if err != nil {
		t.Fatalf("Init parent: %v", err)
	}

	// Simulate a module by writing objects directly into the shared store.
	// Create a blob, tree, and commit for the module.
	blobHash, err := parent.Store.WriteBlob([]byte("module file content\n"))
	if err != nil {
		t.Fatalf("WriteBlob: %v", err)
	}

	moduleTree := &object.TreeObj{Entries: []object.TreeEntry{
		{Name: "lib.go", Mode: object.TreeModeFile, BlobHash: blobHash},
	}}
	treeHash, err := parent.Store.WriteTree(moduleTree)
	if err != nil {
		t.Fatalf("WriteTree: %v", err)
	}

	commitObj := &object.CommitObj{
		TreeHash:  treeHash,
		Author:    "test",
		Message:   "module commit",
		Timestamp: 1000,
	}
	commitHash, err := parent.Store.WriteCommit(commitObj)
	if err != nil {
		t.Fatalf("WriteCommit: %v", err)
	}

	// Add module config.
	err = parent.AddModuleEntry(ModuleEntry{
		Name: "mylib", URL: "github:myorg/mylib",
		Path: "vendor/mylib", Track: "main",
	})
	if err != nil {
		t.Fatalf("AddModuleEntry: %v", err)
	}

	// Update lock to point at our commit.
	err = parent.UpdateModuleLock("mylib", commitHash, "https://github.com/myorg/mylib.git")
	if err != nil {
		t.Fatalf("UpdateModuleLock: %v", err)
	}

	// Sync modules.
	err = parent.ModuleSync()
	if err != nil {
		t.Fatalf("ModuleSync: %v", err)
	}

	// Verify module file was checked out.
	content, err := os.ReadFile(filepath.Join(parentDir, "vendor", "mylib", "lib.go"))
	if err != nil {
		t.Fatalf("read module file: %v", err)
	}
	if string(content) != "module file content\n" {
		t.Errorf("module file content: got %q", string(content))
	}

	// Verify .graft symlink exists in module dir.
	symlinkPath := filepath.Join(parentDir, "vendor", "mylib", ".graft")
	target, err := os.Readlink(symlinkPath)
	if err != nil {
		t.Fatalf("readlink .graft: %v", err)
	}
	expectedTarget := filepath.Join(parent.GraftDir, "modules", "mylib")
	// The symlink may be relative — resolve both.
	absTarget := target
	if !filepath.IsAbs(target) {
		absTarget = filepath.Join(filepath.Dir(symlinkPath), target)
	}
	absTarget = filepath.Clean(absTarget)
	expectedTarget = filepath.Clean(expectedTarget)
	if absTarget != expectedTarget {
		t.Errorf("symlink target: got %q, want %q", absTarget, expectedTarget)
	}

	// Verify HEAD was written in module metadata dir.
	headPath := filepath.Join(parent.GraftDir, "modules", "mylib", "HEAD")
	headData, err := os.ReadFile(headPath)
	if err != nil {
		t.Fatalf("read module HEAD: %v", err)
	}
	if string(headData) != string(commitHash) {
		t.Errorf("module HEAD: got %q, want %q", string(headData), commitHash)
	}
}

func TestModuleSync_NoModules(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Should succeed silently when no modules configured.
	if err := r.ModuleSync(); err != nil {
		t.Fatalf("ModuleSync with no modules: %v", err)
	}
}

func TestModuleSync_MissingLockEntry(t *testing.T) {
	dir := t.TempDir()
	r, err := Init(dir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Add module but don't lock it.
	_ = r.AddModuleEntry(ModuleEntry{
		Name: "mylib", URL: "github:myorg/mylib",
		Path: "vendor/mylib", Track: "main",
	})

	// Sync should warn/skip, not fail.
	err = r.ModuleSync()
	if err != nil {
		t.Fatalf("ModuleSync should not fail for unlocked modules: %v", err)
	}
}

func TestModuleSync_CleansPreviousCheckout(t *testing.T) {
	parentDir := t.TempDir()
	parent, err := Init(parentDir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Create two versions of the module.
	blob1, _ := parent.Store.WriteBlob([]byte("version 1\n"))
	tree1 := &object.TreeObj{Entries: []object.TreeEntry{
		{Name: "lib.go", Mode: object.TreeModeFile, BlobHash: blob1},
	}}
	treeHash1, _ := parent.Store.WriteTree(tree1)
	commit1, _ := parent.Store.WriteCommit(&object.CommitObj{
		TreeHash: treeHash1, Author: "test", Message: "v1", Timestamp: 1000,
	})

	blob2, _ := parent.Store.WriteBlob([]byte("version 2\n"))
	tree2 := &object.TreeObj{Entries: []object.TreeEntry{
		{Name: "lib.go", Mode: object.TreeModeFile, BlobHash: blob2},
		{Name: "new.go", Mode: object.TreeModeFile, BlobHash: blob2},
	}}
	treeHash2, _ := parent.Store.WriteTree(tree2)
	commit2, _ := parent.Store.WriteCommit(&object.CommitObj{
		TreeHash: treeHash2, Author: "test", Message: "v2", Timestamp: 2000,
		Parents: []object.Hash{commit1},
	})

	_ = parent.AddModuleEntry(ModuleEntry{
		Name: "mylib", URL: "github:myorg/mylib",
		Path: "vendor/mylib", Track: "main",
	})

	// Sync at v1.
	_ = parent.UpdateModuleLock("mylib", commit1, "https://github.com/myorg/mylib.git")
	_ = parent.ModuleSync()

	// Verify v1 content.
	content, _ := os.ReadFile(filepath.Join(parentDir, "vendor", "mylib", "lib.go"))
	if string(content) != "version 1\n" {
		t.Fatalf("expected v1, got %q", string(content))
	}

	// Update lock to v2 and sync again.
	_ = parent.UpdateModuleLock("mylib", commit2, "https://github.com/myorg/mylib.git")
	_ = parent.ModuleSync()

	// Verify v2 content.
	content, _ = os.ReadFile(filepath.Join(parentDir, "vendor", "mylib", "lib.go"))
	if string(content) != "version 2\n" {
		t.Errorf("expected v2, got %q", string(content))
	}

	// Verify new file exists.
	content, _ = os.ReadFile(filepath.Join(parentDir, "vendor", "mylib", "new.go"))
	if string(content) != "version 2\n" {
		t.Errorf("new.go should exist with v2 content, got %q", string(content))
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./pkg/repo/ -run TestModuleSync -v -count=1`
Expected: FAIL — `ModuleSync` undefined

**Step 3: Implement ModuleSync**

Create `pkg/repo/module_sync.go`:

```go
package repo

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ModuleSync ensures all module working trees match the lock file.
// For each locked module, it checks out the module's tree into the working directory,
// creates the .graft symlink, and writes the module HEAD.
func (r *Repo) ModuleSync() error {
	modules, err := r.ReadGraftModulesFile()
	if err != nil {
		return err
	}
	if len(modules) == 0 {
		return nil
	}

	lock, err := r.ReadModuleLock()
	if err != nil {
		return err
	}

	for _, entry := range modules {
		var lockEntry ModuleLockEntry
		if lock != nil {
			if le, ok := lock.Modules[entry.Name]; ok {
				lockEntry = le
			}
		}

		if lockEntry.Commit == "" {
			// Not locked yet — skip (need fetch first).
			continue
		}

		if err := r.syncModule(entry, lockEntry); err != nil {
			return fmt.Errorf("sync module %q: %w", entry.Name, err)
		}
	}
	return nil
}

// syncModule checks out a single module at the locked commit.
func (r *Repo) syncModule(entry ModuleEntry, lockEntry ModuleLockEntry) error {
	// Read the commit to get the tree hash.
	commitObj, err := r.Store.ReadCommit(lockEntry.Commit)
	if err != nil {
		return fmt.Errorf("read commit %s: %w", lockEntry.Commit, err)
	}

	// Flatten the module's tree.
	files, err := r.FlattenTree(commitObj.TreeHash)
	if err != nil {
		return fmt.Errorf("flatten tree: %w", err)
	}

	moduleDir := filepath.Join(r.RootDir, filepath.FromSlash(entry.Path))

	// Clean existing module working tree (preserve .graft symlink).
	if err := cleanModuleDir(moduleDir); err != nil {
		return err
	}

	// Write files.
	if err := os.MkdirAll(moduleDir, 0o755); err != nil {
		return err
	}

	for _, f := range files {
		absPath := filepath.Join(moduleDir, filepath.FromSlash(f.Path))
		dir := filepath.Dir(absPath)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("mkdir %q: %w", dir, err)
		}
		content, err := r.readBlobData(f.BlobHash)
		if err != nil {
			return fmt.Errorf("read blob %s for %s: %w", f.BlobHash, f.Path, err)
		}
		if err := os.WriteFile(absPath, content, filePermFromMode(f.Mode)); err != nil {
			return fmt.Errorf("write %q: %w", f.Path, err)
		}
	}

	// Create or update .graft symlink.
	metaDir := r.ModuleMetadataDir(entry.Name)
	if err := os.MkdirAll(metaDir, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(metaDir, "refs"), 0o755); err != nil {
		return err
	}

	symlinkPath := filepath.Join(moduleDir, ".graft")
	_ = os.Remove(symlinkPath) // remove if exists
	relTarget, err := filepath.Rel(moduleDir, metaDir)
	if err != nil {
		// Fallback to absolute.
		relTarget = metaDir
	}
	if err := os.Symlink(relTarget, symlinkPath); err != nil {
		return fmt.Errorf("create .graft symlink: %w", err)
	}

	// Write module HEAD.
	headPath := filepath.Join(metaDir, "HEAD")
	if err := os.WriteFile(headPath, []byte(lockEntry.Commit), 0o644); err != nil {
		return fmt.Errorf("write module HEAD: %w", err)
	}

	return nil
}

// cleanModuleDir removes all contents of the module directory except .graft symlink.
func cleanModuleDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	for _, e := range entries {
		if e.Name() == ".graft" {
			continue
		}
		p := filepath.Join(dir, e.Name())
		if err := os.RemoveAll(p); err != nil {
			return err
		}
	}
	return nil
}

// ModuleSyncForCheckout syncs modules when switching branches.
// It compares the old and new lock files and only syncs changed modules.
func (r *Repo) ModuleSyncForCheckout(oldLock, newLock *ModuleLock) error {
	modules, err := r.ReadGraftModulesFile()
	if err != nil {
		return err
	}

	for _, entry := range modules {
		newEntry, inNew := newLock.Modules[entry.Name]
		if !inNew {
			// Module removed in new branch — clean up working tree.
			moduleDir := filepath.Join(r.RootDir, filepath.FromSlash(entry.Path))
			_ = os.RemoveAll(moduleDir)
			continue
		}

		if oldLock != nil {
			if oldEntry, inOld := oldLock.Modules[entry.Name]; inOld {
				if oldEntry.Commit == newEntry.Commit {
					continue // unchanged
				}
			}
		}

		// Changed or new — sync.
		if err := r.syncModule(entry, newEntry); err != nil {
			return fmt.Errorf("sync module %q for checkout: %w", entry.Name, err)
		}
	}

	// Handle modules that exist in old but not in new .graftmodules.
	// This requires reading the old branch's .graftmodules too.
	// For now, ModuleSyncForCheckout handles the common case.
	_ = strings.TrimSpace // avoid unused import if needed

	return nil
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./pkg/repo/ -run TestModuleSync -v -count=1`
Expected: All PASS

**Step 5: Run full test suite**

Run: `go test ./pkg/repo/ -count=1`
Expected: All pass

**Step 6: Commit**

```bash
git add pkg/repo/module_sync.go pkg/repo/module_sync_test.go
buckley commit --yes --minimal-output
```

---

## Phase 6: Module-Aware Merge

### Task 8: Merge resolution for mode 160000 tree entries

**Files:**
- Create: `pkg/repo/module_merge.go`
- Create: `pkg/repo/module_merge_test.go`

**Context:** When merging branches with different module versions, the merge engine needs special logic for mode `160000` entries. Rules: one side changed → take that side. Both changed → take the commit with higher generation number (newer wins). Divergent branch tracking → conflict. The existing `threeWayTreeMerge` operates on `TreeFileEntry` maps. Module entries are separate. We add a `mergeModuleEntries` function.

**Step 1: Write the failing tests**

Create `pkg/repo/module_merge_test.go`:

```go
package repo

import (
	"testing"

	"github.com/odvcencio/graft/pkg/object"
)

func TestMergeModules_OneSideChanged(t *testing.T) {
	store := object.NewStore(t.TempDir())
	r := &Repo{Store: store, RootDir: t.TempDir(), GraftDir: t.TempDir()}

	baseCommit := object.Hash("aaa0000000000000000000000000000000000000000000000000000000000000")
	oursCommit := object.Hash("bbb0000000000000000000000000000000000000000000000000000000000000")

	base := map[string]TreeModuleEntry{
		"vendor/lib": {Path: "vendor/lib", BlobHash: baseCommit},
	}
	ours := map[string]TreeModuleEntry{
		"vendor/lib": {Path: "vendor/lib", BlobHash: oursCommit},
	}
	theirs := map[string]TreeModuleEntry{
		"vendor/lib": {Path: "vendor/lib", BlobHash: baseCommit},
	}

	result, err := r.mergeModuleEntries(base, ours, theirs)
	if err != nil {
		t.Fatalf("mergeModuleEntries: %v", err)
	}
	if result.HasConflicts {
		t.Fatal("should not have conflicts")
	}
	if len(result.Resolved) != 1 {
		t.Fatalf("expected 1 resolved, got %d", len(result.Resolved))
	}
	if result.Resolved["vendor/lib"] != oursCommit {
		t.Errorf("expected ours commit, got %s", result.Resolved["vendor/lib"])
	}
}

func TestMergeModules_BothChangedNewerWins(t *testing.T) {
	store := object.NewStore(t.TempDir())
	r := &Repo{Store: store, RootDir: t.TempDir(), GraftDir: t.TempDir()}

	// Create commits with different generation numbers.
	baseTree, _ := store.WriteTree(&object.TreeObj{})
	baseCommit, _ := store.WriteCommit(&object.CommitObj{
		TreeHash: baseTree, Author: "test", Message: "base", Timestamp: 1000,
	})
	oursCommit, _ := store.WriteCommit(&object.CommitObj{
		TreeHash: baseTree, Author: "test", Message: "ours",
		Parents: []object.Hash{baseCommit}, Timestamp: 2000,
	})
	theirsCommit, _ := store.WriteCommit(&object.CommitObj{
		TreeHash: baseTree, Author: "test", Message: "theirs",
		Parents: []object.Hash{baseCommit, oursCommit}, Timestamp: 3000,
	})

	base := map[string]TreeModuleEntry{
		"vendor/lib": {Path: "vendor/lib", BlobHash: baseCommit},
	}
	ours := map[string]TreeModuleEntry{
		"vendor/lib": {Path: "vendor/lib", BlobHash: oursCommit},
	}
	theirs := map[string]TreeModuleEntry{
		"vendor/lib": {Path: "vendor/lib", BlobHash: theirsCommit},
	}

	result, err := r.mergeModuleEntries(base, ours, theirs)
	if err != nil {
		t.Fatalf("mergeModuleEntries: %v", err)
	}
	if result.HasConflicts {
		t.Fatal("newer-wins should not conflict")
	}
	// theirs has higher generation (2 parents vs 1 parent) — should win.
	if result.Resolved["vendor/lib"] != theirsCommit {
		t.Errorf("expected theirs commit (newer), got %s", result.Resolved["vendor/lib"])
	}
}

func TestMergeModules_BothDeleted(t *testing.T) {
	store := object.NewStore(t.TempDir())
	r := &Repo{Store: store, RootDir: t.TempDir(), GraftDir: t.TempDir()}

	baseCommit := object.Hash("aaa0000000000000000000000000000000000000000000000000000000000000")

	base := map[string]TreeModuleEntry{
		"vendor/lib": {Path: "vendor/lib", BlobHash: baseCommit},
	}
	ours := map[string]TreeModuleEntry{}
	theirs := map[string]TreeModuleEntry{}

	result, err := r.mergeModuleEntries(base, ours, theirs)
	if err != nil {
		t.Fatalf("mergeModuleEntries: %v", err)
	}
	if result.HasConflicts {
		t.Fatal("both deleted should not conflict")
	}
	if len(result.Removed) != 1 || result.Removed[0] != "vendor/lib" {
		t.Errorf("expected vendor/lib in removed, got %v", result.Removed)
	}
}

func TestMergeModules_AddedInOneOnly(t *testing.T) {
	store := object.NewStore(t.TempDir())
	r := &Repo{Store: store, RootDir: t.TempDir(), GraftDir: t.TempDir()}

	newCommit := object.Hash("ccc0000000000000000000000000000000000000000000000000000000000000")

	base := map[string]TreeModuleEntry{}
	ours := map[string]TreeModuleEntry{}
	theirs := map[string]TreeModuleEntry{
		"vendor/new": {Path: "vendor/new", BlobHash: newCommit},
	}

	result, err := r.mergeModuleEntries(base, ours, theirs)
	if err != nil {
		t.Fatalf("mergeModuleEntries: %v", err)
	}
	if result.HasConflicts {
		t.Fatal("added in one side should not conflict")
	}
	if result.Resolved["vendor/new"] != newCommit {
		t.Errorf("expected new module added, got %v", result.Resolved)
	}
}

func TestMergeModules_Unchanged(t *testing.T) {
	store := object.NewStore(t.TempDir())
	r := &Repo{Store: store, RootDir: t.TempDir(), GraftDir: t.TempDir()}

	commit := object.Hash("aaa0000000000000000000000000000000000000000000000000000000000000")

	base := map[string]TreeModuleEntry{
		"vendor/lib": {Path: "vendor/lib", BlobHash: commit},
	}
	ours := map[string]TreeModuleEntry{
		"vendor/lib": {Path: "vendor/lib", BlobHash: commit},
	}
	theirs := map[string]TreeModuleEntry{
		"vendor/lib": {Path: "vendor/lib", BlobHash: commit},
	}

	result, err := r.mergeModuleEntries(base, ours, theirs)
	if err != nil {
		t.Fatalf("mergeModuleEntries: %v", err)
	}
	if result.HasConflicts {
		t.Fatal("all same should not conflict")
	}
	if result.Resolved["vendor/lib"] != commit {
		t.Errorf("unchanged module should resolve to same commit")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./pkg/repo/ -run TestMergeModules -v -count=1`
Expected: FAIL — `mergeModuleEntries` undefined

**Step 3: Implement module merge logic**

Create `pkg/repo/module_merge.go`:

```go
package repo

import (
	"fmt"
	"sort"

	"github.com/odvcencio/graft/pkg/object"
)

// ModuleMergeResult holds the outcome of merging module entries.
type ModuleMergeResult struct {
	Resolved  map[string]object.Hash // path -> resolved commit hash
	Removed   []string               // paths to remove
	Conflicts []ModuleMergeConflict
	HasConflicts bool
}

// ModuleMergeConflict describes a module merge conflict.
type ModuleMergeConflict struct {
	Path       string
	OursCommit object.Hash
	TheirsCommit object.Hash
	Reason     string
}

// mergeModuleEntries performs three-way merge on module (gitlink) entries.
// Rules:
//   - One side changed: take that side
//   - Both changed to same: trivial resolve
//   - Both changed to different: compare generation numbers, newer wins
//   - Both deleted: clean remove
//   - Delete vs change: take the change (module still needed)
func (r *Repo) mergeModuleEntries(
	baseMap, oursMap, theirsMap map[string]TreeModuleEntry,
) (*ModuleMergeResult, error) {
	result := &ModuleMergeResult{
		Resolved: make(map[string]object.Hash),
	}

	allPaths := collectModulePaths(baseMap, oursMap, theirsMap)

	for _, path := range allPaths {
		baseEntry, inBase := baseMap[path]
		oursEntry, inOurs := oursMap[path]
		theirsEntry, inTheirs := theirsMap[path]

		switch {
		case inBase && inOurs && inTheirs:
			// All three present.
			if oursEntry.BlobHash == theirsEntry.BlobHash {
				// Same in both — take it.
				result.Resolved[path] = oursEntry.BlobHash
			} else if oursEntry.BlobHash == baseEntry.BlobHash {
				// Only theirs changed.
				result.Resolved[path] = theirsEntry.BlobHash
			} else if theirsEntry.BlobHash == baseEntry.BlobHash {
				// Only ours changed.
				result.Resolved[path] = oursEntry.BlobHash
			} else {
				// Both changed to different commits — newer wins.
				winner, err := r.newerCommit(oursEntry.BlobHash, theirsEntry.BlobHash)
				if err != nil {
					// Can't determine — conflict.
					result.HasConflicts = true
					result.Conflicts = append(result.Conflicts, ModuleMergeConflict{
						Path:         path,
						OursCommit:   oursEntry.BlobHash,
						TheirsCommit: theirsEntry.BlobHash,
						Reason:       fmt.Sprintf("cannot determine newer: %v", err),
					})
					// Default to ours on error.
					result.Resolved[path] = oursEntry.BlobHash
				} else {
					result.Resolved[path] = winner
				}
			}

		case !inBase && inOurs && inTheirs:
			// Added in both.
			if oursEntry.BlobHash == theirsEntry.BlobHash {
				result.Resolved[path] = oursEntry.BlobHash
			} else {
				winner, err := r.newerCommit(oursEntry.BlobHash, theirsEntry.BlobHash)
				if err != nil {
					result.Resolved[path] = oursEntry.BlobHash
				} else {
					result.Resolved[path] = winner
				}
			}

		case !inBase && !inOurs && inTheirs:
			// Added only in theirs.
			result.Resolved[path] = theirsEntry.BlobHash

		case !inBase && inOurs && !inTheirs:
			// Added only in ours.
			result.Resolved[path] = oursEntry.BlobHash

		case inBase && !inOurs && !inTheirs:
			// Both deleted.
			result.Removed = append(result.Removed, path)

		case inBase && inOurs && !inTheirs:
			// Deleted by theirs, kept by ours.
			if oursEntry.BlobHash == baseEntry.BlobHash {
				// Ours unchanged — respect theirs' deletion.
				result.Removed = append(result.Removed, path)
			} else {
				// Ours changed — keep ours (module still needed).
				result.Resolved[path] = oursEntry.BlobHash
			}

		case inBase && !inOurs && inTheirs:
			// Deleted by ours, kept by theirs.
			if theirsEntry.BlobHash == baseEntry.BlobHash {
				// Theirs unchanged — respect ours' deletion.
				result.Removed = append(result.Removed, path)
			} else {
				// Theirs changed — keep theirs.
				result.Resolved[path] = theirsEntry.BlobHash
			}
		}
	}

	return result, nil
}

// newerCommit returns the commit with the higher generation number.
// Generation is computed from the commit DAG (number of ancestors).
func (r *Repo) newerCommit(a, b object.Hash) (object.Hash, error) {
	genA, err := r.generation(a)
	if err != nil {
		return "", err
	}
	genB, err := r.generation(b)
	if err != nil {
		return "", err
	}
	if genB > genA {
		return b, nil
	}
	return a, nil // tie-break: ours wins
}

func collectModulePaths(maps ...map[string]TreeModuleEntry) []string {
	seen := make(map[string]bool)
	for _, m := range maps {
		for k := range m {
			seen[k] = true
		}
	}
	paths := make([]string, 0, len(seen))
	for k := range seen {
		paths = append(paths, k)
	}
	sort.Strings(paths)
	return paths
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./pkg/repo/ -run TestMergeModules -v -count=1`
Expected: All PASS

**Step 5: Run full test suite**

Run: `go test ./pkg/repo/ -count=1`
Expected: All pass

**Step 6: Commit**

```bash
git add pkg/repo/module_merge.go pkg/repo/module_merge_test.go
buckley commit --yes --minimal-output
```

---

## Phase 7: Module Status

### Task 9: Module status reporting

**Files:**
- Create: `pkg/repo/module_status.go`
- Create: `pkg/repo/module_status_test.go`

**Context:** `ModuleStatus` reports the state of each module: current commit vs lock, whether the module working tree is dirty, behind/ahead counts relative to the tracked branch (when remote info is available).

**Step 1: Write the failing tests**

Create `pkg/repo/module_status_test.go`:

```go
package repo

import (
	"testing"

	"github.com/odvcencio/graft/pkg/object"
)

func TestModuleStatus_UpToDate(t *testing.T) {
	r := createTestRepo(t)

	// Create a module with matching lock and HEAD.
	blobHash, _ := r.Store.WriteBlob([]byte("content"))
	tree := &object.TreeObj{Entries: []object.TreeEntry{
		{Name: "lib.go", Mode: object.TreeModeFile, BlobHash: blobHash},
	}}
	treeHash, _ := r.Store.WriteTree(tree)
	commitHash, _ := r.Store.WriteCommit(&object.CommitObj{
		TreeHash: treeHash, Author: "test", Message: "init", Timestamp: 1000,
	})

	_ = r.AddModuleEntry(ModuleEntry{
		Name: "mylib", URL: "github:myorg/mylib",
		Path: "vendor/mylib", Track: "main",
	})
	_ = r.UpdateModuleLock("mylib", commitHash, "https://github.com/myorg/mylib.git")
	_ = r.ModuleSync()

	statuses, err := r.ModuleStatus()
	if err != nil {
		t.Fatalf("ModuleStatus: %v", err)
	}
	if len(statuses) != 1 {
		t.Fatalf("expected 1 status, got %d", len(statuses))
	}
	if statuses[0].Name != "mylib" {
		t.Errorf("name: got %q", statuses[0].Name)
	}
	if statuses[0].LockedCommit != commitHash {
		t.Errorf("locked commit mismatch")
	}
	if statuses[0].Synced != true {
		t.Error("should be synced")
	}
}

func TestModuleStatus_NotSynced(t *testing.T) {
	r := createTestRepo(t)

	_ = r.AddModuleEntry(ModuleEntry{
		Name: "mylib", URL: "github:myorg/mylib",
		Path: "vendor/mylib", Track: "main",
	})
	// Locked but not synced (no ModuleSync called).
	blobHash, _ := r.Store.WriteBlob([]byte("content"))
	tree := &object.TreeObj{Entries: []object.TreeEntry{
		{Name: "lib.go", Mode: object.TreeModeFile, BlobHash: blobHash},
	}}
	treeHash, _ := r.Store.WriteTree(tree)
	commitHash, _ := r.Store.WriteCommit(&object.CommitObj{
		TreeHash: treeHash, Author: "test", Message: "init", Timestamp: 1000,
	})
	_ = r.UpdateModuleLock("mylib", commitHash, "https://github.com/myorg/mylib.git")

	statuses, err := r.ModuleStatus()
	if err != nil {
		t.Fatalf("ModuleStatus: %v", err)
	}
	if statuses[0].Synced {
		t.Error("should not be synced (no checkout done)")
	}
}

func TestModuleStatus_NoLock(t *testing.T) {
	r := createTestRepo(t)

	_ = r.AddModuleEntry(ModuleEntry{
		Name: "mylib", URL: "github:myorg/mylib",
		Path: "vendor/mylib", Track: "main",
	})

	statuses, err := r.ModuleStatus()
	if err != nil {
		t.Fatalf("ModuleStatus: %v", err)
	}
	if statuses[0].LockedCommit != "" {
		t.Error("should have empty locked commit")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./pkg/repo/ -run TestModuleStatus -v -count=1`
Expected: FAIL — `ModuleStatus` undefined

**Step 3: Implement module status**

Create `pkg/repo/module_status.go`:

```go
package repo

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/odvcencio/graft/pkg/object"
)

// ModuleStatusEntry describes the state of a single module.
type ModuleStatusEntry struct {
	Name         string
	Path         string
	Track        string
	Pin          string
	LockedCommit object.Hash
	HeadCommit   object.Hash // what's actually checked out
	Synced       bool        // HeadCommit matches LockedCommit
}

// ModuleStatus returns the status of all configured modules.
func (r *Repo) ModuleStatus() ([]ModuleStatusEntry, error) {
	modules, err := r.ListModules()
	if err != nil {
		return nil, err
	}

	statuses := make([]ModuleStatusEntry, len(modules))
	for i, m := range modules {
		entry := ModuleStatusEntry{
			Name:         m.Name,
			Path:         m.Path,
			Track:        m.Track,
			Pin:          m.Pin,
			LockedCommit: m.Commit,
		}

		// Read current HEAD from module metadata.
		headPath := filepath.Join(r.ModuleMetadataDir(m.Name), "HEAD")
		headData, err := os.ReadFile(headPath)
		if err == nil {
			entry.HeadCommit = object.Hash(strings.TrimSpace(string(headData)))
		}

		entry.Synced = entry.LockedCommit != "" &&
			entry.HeadCommit != "" &&
			entry.LockedCommit == entry.HeadCommit

		statuses[i] = entry
	}

	return statuses, nil
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./pkg/repo/ -run TestModuleStatus -v -count=1`
Expected: All PASS

**Step 5: Commit**

```bash
git add pkg/repo/module_status.go pkg/repo/module_status_test.go
buckley commit --yes --minimal-output
```

---

## Phase 8: CLI Commands

### Task 10: `graft module` command group

**Files:**
- Create: `cmd/graft/cmd_module.go`
- Modify: `cmd/graft/main.go` (register command)

**Context:** Cobra command group with subcommands: `add`, `rm`, `update`, `sync`, `status`, `list`. For this task, implement the full CLI wiring with `list`, `status`, `add`, `rm`, and `sync`. The `update` subcommand (which fetches from remote) will be implemented separately since it requires remote integration.

**Step 1: Create the command file**

Create `cmd/graft/cmd_module.go`:

```go
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/odvcencio/graft/pkg/object"
	"github.com/odvcencio/graft/pkg/repo"
	"github.com/spf13/cobra"
)

func newModuleCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "module",
		Short: "Manage graft modules",
		Long:  "Add, remove, sync, and inspect modules declared in .graftmodules",
	}

	cmd.AddCommand(newModuleListCmd())
	cmd.AddCommand(newModuleStatusCmd())
	cmd.AddCommand(newModuleAddCmd())
	cmd.AddCommand(newModuleRmCmd())
	cmd.AddCommand(newModuleSyncCmd())
	cmd.AddCommand(newModuleUpdateCmd())

	return cmd
}

func newModuleListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all configured modules",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := repo.Open(".")
			if err != nil {
				return err
			}

			modules, err := r.ListModules()
			if err != nil {
				return err
			}

			if len(modules) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "no modules configured")
				return nil
			}

			for _, m := range modules {
				version := "not locked"
				if m.Commit != "" {
					version = shortHash(m.Commit)
				}
				tracking := ""
				if m.Track != "" {
					tracking = fmt.Sprintf("(tracking %s)", m.Track)
				} else if m.Pin != "" {
					tracking = fmt.Sprintf("(pinned %s)", m.Pin)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "  %-20s %-30s %s %s\n",
					m.Name, m.Path, version, tracking)
			}
			return nil
		},
	}
}

func newModuleStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show status of all modules",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := repo.Open(".")
			if err != nil {
				return err
			}

			statuses, err := r.ModuleStatus()
			if err != nil {
				return err
			}

			if len(statuses) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "no modules configured")
				return nil
			}

			out := cmd.OutOrStdout()
			for _, s := range statuses {
				state := "synced"
				if s.LockedCommit == "" {
					state = "not locked"
				} else if !s.Synced {
					state = "out of sync"
				}
				tracking := s.Track
				if tracking == "" {
					tracking = s.Pin
				}
				fmt.Fprintf(out, "  %-20s %-30s %-10s %s  (%s)\n",
					s.Name, s.Path, tracking,
					shortHashOrNone(s.LockedCommit), state)
			}
			return nil
		},
	}
}

func newModuleAddCmd() *cobra.Command {
	var track string
	var pin string

	cmd := &cobra.Command{
		Use:   "add <url> [<path>]",
		Short: "Add a module",
		Long:  "Add a new module from a remote URL. Path defaults to the repo name.",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if track != "" && pin != "" {
				return fmt.Errorf("--track and --pin are mutually exclusive")
			}

			r, err := repo.Open(".")
			if err != nil {
				return err
			}

			url := args[0]

			// Derive path from URL if not provided.
			modPath := ""
			if len(args) > 1 {
				modPath = args[1]
			} else {
				modPath = inferModulePath(url)
			}

			// Derive name from path.
			name := filepath.Base(modPath)

			// Check path doesn't already exist.
			absPath := filepath.Join(r.RootDir, modPath)
			if info, err := os.Stat(absPath); err == nil && info.IsDir() {
				entries, _ := os.ReadDir(absPath)
				if len(entries) > 0 {
					return fmt.Errorf("path %s already exists and is not empty", modPath)
				}
			}

			// Default to tracking main if neither specified.
			if track == "" && pin == "" {
				track = "main"
			}

			entry := repo.ModuleEntry{
				Name:  name,
				URL:   url,
				Path:  modPath,
				Track: track,
				Pin:   pin,
			}

			if err := r.AddModuleEntry(entry); err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "added module %q -> %s", name, modPath)
			if track != "" {
				fmt.Fprintf(cmd.OutOrStdout(), " (tracking %s)", track)
			} else if pin != "" {
				fmt.Fprintf(cmd.OutOrStdout(), " (pinned %s)", pin)
			}
			fmt.Fprintln(cmd.OutOrStdout())
			fmt.Fprintln(cmd.OutOrStdout(), "run 'graft module update' to fetch objects")

			return nil
		},
	}

	cmd.Flags().StringVar(&track, "track", "", "branch to track")
	cmd.Flags().StringVar(&pin, "pin", "", "tag or commit to pin")

	return cmd
}

func newModuleRmCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rm <name>",
		Short: "Remove a module",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := repo.Open(".")
			if err != nil {
				return err
			}

			name := args[0]

			// Get module path before removing.
			m, err := r.GetModule(name)
			if err != nil {
				return err
			}

			// Remove working tree.
			moduleDir := filepath.Join(r.RootDir, filepath.FromSlash(m.Path))
			if err := os.RemoveAll(moduleDir); err != nil {
				return fmt.Errorf("remove working tree: %w", err)
			}

			if err := r.RemoveModuleEntry(name); err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "removed module %q (was at %s)\n", name, m.Path)
			return nil
		},
	}
}

func newModuleSyncCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sync",
		Short: "Sync module working trees to match lock file",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := repo.Open(".")
			if err != nil {
				return err
			}

			if err := r.ModuleSync(); err != nil {
				return err
			}

			fmt.Fprintln(cmd.OutOrStdout(), "modules synced")
			return nil
		},
	}
}

func newModuleUpdateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "update [<name>...]",
		Short: "Fetch latest from module remotes and update lock",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := repo.Open(".")
			if err != nil {
				return err
			}

			modules, err := r.ListModules()
			if err != nil {
				return err
			}

			if len(modules) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "no modules configured")
				return nil
			}

			// Filter to requested modules if any.
			if len(args) > 0 {
				nameSet := make(map[string]bool)
				for _, a := range args {
					nameSet[a] = true
				}
				var filtered []repo.Module
				for _, m := range modules {
					if nameSet[m.Name] {
						filtered = append(filtered, m)
					}
				}
				modules = filtered
			}

			out := cmd.OutOrStdout()
			for _, m := range modules {
				fmt.Fprintf(out, "updating %s...\n", m.Name)
				// TODO: Fetch from remote and resolve new commit.
				// For now, report that remote fetch is not yet implemented.
				fmt.Fprintf(out, "  remote fetch not yet implemented — use lock file directly\n")
			}

			fmt.Fprintln(out, "run 'graft module sync' to checkout updated versions")
			return nil
		},
	}
}

// inferModulePath derives a module path from its URL.
func inferModulePath(url string) string {
	// Strip common prefixes.
	url = strings.TrimSpace(url)
	for _, prefix := range []string{"github:", "gh:", "orchard:", "gitlab:", "gl:", "bitbucket:", "bb:"} {
		if strings.HasPrefix(url, prefix) {
			url = strings.TrimPrefix(url, prefix)
			break
		}
	}
	// Take the last segment.
	parts := strings.Split(url, "/")
	name := parts[len(parts)-1]
	name = strings.TrimSuffix(name, ".git")
	if name == "" {
		name = "module"
	}
	return name
}

func shortHashOrNone(h object.Hash) string {
	if h == "" {
		return "-------"
	}
	return shortHash(h)
}
```

**Step 2: Register the command**

In `cmd/graft/main.go`, add:

```go
root.AddCommand(newModuleCmd())
```

**Step 3: Verify it builds**

Run: `go build ./cmd/graft/`
Expected: Build succeeds

**Step 4: Commit**

```bash
git add cmd/graft/cmd_module.go cmd/graft/main.go
buckley commit --yes --minimal-output
```

---

### Task 11: Integration tests for module CLI

**Files:**
- Create: `cmd/graft/integration_module_test.go`

**Step 1: Write integration tests**

Create `cmd/graft/integration_module_test.go`:

```go
package main

import (
	"strings"
	"testing"
)

func TestIntegration_ModuleListEmpty(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	dir := initRepo(t)

	out := mustRunGraft(t, dir, "module", "list")
	if !strings.Contains(out, "no modules") {
		t.Errorf("expected 'no modules', got: %s", out)
	}
}

func TestIntegration_ModuleAddAndList(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	dir := initRepo(t)

	// Add a module.
	out := mustRunGraft(t, dir, "module", "add", "github:myorg/ui-kit", "vendor/ui-kit", "--track", "main")
	if !strings.Contains(out, "added module") {
		t.Errorf("expected 'added module', got: %s", out)
	}
	if !strings.Contains(out, "ui-kit") {
		t.Errorf("expected module name in output, got: %s", out)
	}

	// List should show the module.
	out = mustRunGraft(t, dir, "module", "list")
	if !strings.Contains(out, "ui-kit") {
		t.Errorf("list should contain ui-kit: %s", out)
	}
	if !strings.Contains(out, "vendor/ui-kit") {
		t.Errorf("list should contain path: %s", out)
	}
}

func TestIntegration_ModuleAddDuplicate(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	dir := initRepo(t)

	mustRunGraft(t, dir, "module", "add", "github:myorg/ui-kit", "vendor/ui-kit")

	_, err := runGraft(t, dir, "module", "add", "github:myorg/other", "vendor/other", "--track", "main")
	// Different name, different path — should succeed.
	if err != nil {
		t.Errorf("adding a different module should succeed: %v", err)
	}
}

func TestIntegration_ModuleRemove(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	dir := initRepo(t)

	mustRunGraft(t, dir, "module", "add", "github:myorg/ui-kit", "vendor/ui-kit")
	out := mustRunGraft(t, dir, "module", "rm", "ui-kit")
	if !strings.Contains(out, "removed module") {
		t.Errorf("expected 'removed module', got: %s", out)
	}

	// List should be empty.
	out = mustRunGraft(t, dir, "module", "list")
	if !strings.Contains(out, "no modules") {
		t.Errorf("expected 'no modules' after remove, got: %s", out)
	}
}

func TestIntegration_ModuleRemoveNotFound(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	dir := initRepo(t)

	_, err := runGraft(t, dir, "module", "rm", "nonexistent")
	if err == nil {
		t.Error("expected error for removing nonexistent module")
	}
}

func TestIntegration_ModuleStatus(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	dir := initRepo(t)

	mustRunGraft(t, dir, "module", "add", "github:myorg/ui-kit", "vendor/ui-kit")

	out := mustRunGraft(t, dir, "module", "status")
	if !strings.Contains(out, "ui-kit") {
		t.Errorf("status should show ui-kit: %s", out)
	}
	if !strings.Contains(out, "not locked") {
		t.Errorf("status should show 'not locked' for unfetched module: %s", out)
	}
}

func TestIntegration_ModuleSync(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	dir := initRepo(t)

	out := mustRunGraft(t, dir, "module", "sync")
	if !strings.Contains(out, "synced") {
		t.Errorf("expected 'synced', got: %s", out)
	}
}

func TestIntegration_ModuleTrackAndPinExclusive(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	dir := initRepo(t)

	_, err := runGraft(t, dir, "module", "add", "github:myorg/ui-kit", "vendor/ui-kit", "--track", "main", "--pin", "v1.0")
	if err == nil {
		t.Error("expected error for track+pin")
	}
}
```

**Step 2: Run integration tests**

Run: `go test ./cmd/graft/ -run TestIntegration_Module -v -count=1`
Expected: All PASS

**Step 3: Commit**

```bash
git add cmd/graft/integration_module_test.go
buckley commit --yes --minimal-output
```

---

## Phase 9: Checkout & Merge Integration

### Task 12: Wire module sync into checkout

**Files:**
- Modify: `pkg/repo/checkout.go`

**Context:** After `Checkout` updates HEAD and writes files, it should sync modules if the new branch has different module versions. Read the lock file from the new commit's tree, compare with current lock, and sync changed modules.

**Step 1: Read the current checkout code**

Read `pkg/repo/checkout.go` to understand where to hook in module sync. The hook point is after the working tree is updated and HEAD is moved.

**Step 2: Add module sync call**

At the end of the Checkout method (after files are written and HEAD updated), add:

```go
// Sync modules if the new commit has module configuration.
if err := r.ModuleSync(); err != nil {
	// Module sync failure is non-fatal during checkout.
	// The user can run 'graft module sync' to retry.
	_ = err
}
```

This is the simplest integration. A more sophisticated approach (comparing old vs new lock and only syncing changed modules) can be added later.

**Step 3: Run existing checkout tests**

Run: `go test ./pkg/repo/ -run TestCheckout -v -count=1`
Expected: All pass (no regression)

**Step 4: Commit**

```bash
git add pkg/repo/checkout.go
buckley commit --yes --minimal-output
```

---

### Task 13: Wire module merge into tree merge

**Files:**
- Modify: `pkg/repo/merge.go` or `pkg/repo/merge_helper.go`

**Context:** The merge flow currently calls `threeWayTreeMerge` which operates on `TreeFileEntry` maps from `FlattenTree`. Since `FlattenTree` now skips module entries (mode 160000), modules are invisible to the current merge. We need to:
1. Use `FlattenTreeWithModules` instead of `FlattenTree` in the merge path
2. Call `mergeModuleEntries` for the module maps
3. Apply module merge results alongside file merge results

**Step 1: Find the merge entry point**

In `pkg/repo/merge.go`, find where `FlattenTree` is called for base/ours/theirs trees. Change to `FlattenTreeWithModules`.

**Step 2: Add module merge integration**

After the file three-way merge, call `mergeModuleEntries` on the module maps. Add module results to the merge report. Update the staging index with resolved module entries (mode 160000).

The key code addition in the merge function:

```go
// After flattening trees:
baseFiles, baseModules, err := r.FlattenTreeWithModules(baseTreeHash)
// ... similarly for ours and theirs ...

// Build module maps.
baseModMap := indexModulesByPath(baseModules)
oursModMap := indexModulesByPath(oursModules)
theirsModMap := indexModulesByPath(theirsModules)

// Merge modules.
modResult, err := r.mergeModuleEntries(baseModMap, oursModMap, theirsModMap)
if err != nil {
    return nil, fmt.Errorf("merge modules: %w", err)
}

// Add module conflicts to report.
if modResult.HasConflicts {
    report.HasConflicts = true
    for _, c := range modResult.Conflicts {
        report.TotalConflicts++
        // Format: CONFLICT (module): <path> changed to <ours> in ours and <theirs> in theirs
    }
}

// Stage resolved module entries.
for path, commitHash := range modResult.Resolved {
    staging.Entries[path] = &StagingEntry{
        Path:     path,
        BlobHash: commitHash,
        Mode:     object.TreeModeModule,
    }
}

// Remove deleted modules from staging.
for _, path := range modResult.Removed {
    delete(staging.Entries, path)
}
```

Add helper:

```go
func indexModulesByPath(modules []TreeModuleEntry) map[string]TreeModuleEntry {
    m := make(map[string]TreeModuleEntry, len(modules))
    for _, mod := range modules {
        m[mod.Path] = mod
    }
    return m
}
```

**Step 3: Run merge tests**

Run: `go test ./pkg/repo/ -run TestMerge -v -count=1`
Expected: All pass

**Step 4: Run full test suite**

Run: `go test ./pkg/repo/ -count=1`
Expected: All pass

**Step 5: Commit**

```bash
git add pkg/repo/merge.go pkg/repo/merge_helper.go pkg/repo/tree.go
buckley commit --yes --minimal-output
```

---

## Phase 10: Recursive Modules

### Task 14: Recursive module support with cycle detection

**Files:**
- Create: `pkg/repo/module_recursive.go`
- Create: `pkg/repo/module_recursive_test.go`

**Context:** Modules can have their own `.graftmodules`. During sync, after checking out a module's tree, check if it has `.graftmodules` and recursively sync those too. Cycle detection tracks visited URLs. Depth limit defaults to 10.

**Step 1: Write the failing tests**

Create `pkg/repo/module_recursive_test.go`:

```go
package repo

import (
	"testing"
)

func TestModuleRecursive_CycleDetection(t *testing.T) {
	visited := make(map[string]bool)
	visited["github:myorg/a"] = true

	err := checkModuleCycle("github:myorg/a", visited)
	if err == nil {
		t.Fatal("expected cycle error")
	}
}

func TestModuleRecursive_NoCycle(t *testing.T) {
	visited := make(map[string]bool)
	visited["github:myorg/a"] = true

	err := checkModuleCycle("github:myorg/b", visited)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestModuleRecursive_DepthLimit(t *testing.T) {
	err := checkDepthLimit(11, 10)
	if err == nil {
		t.Fatal("expected depth limit error")
	}
}

func TestModuleRecursive_DepthOK(t *testing.T) {
	err := checkDepthLimit(5, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./pkg/repo/ -run TestModuleRecursive -v -count=1`
Expected: FAIL — functions undefined

**Step 3: Implement recursive support**

Create `pkg/repo/module_recursive.go`:

```go
package repo

import "fmt"

const defaultModuleMaxDepth = 10

func checkModuleCycle(url string, visited map[string]bool) error {
	if visited[url] {
		return fmt.Errorf("module cycle detected: %s already visited", url)
	}
	return nil
}

func checkDepthLimit(current, max int) error {
	if current > max {
		return fmt.Errorf("module depth limit exceeded: %d > %d", current, max)
	}
	return nil
}

// ModuleSyncRecursive syncs modules recursively with cycle detection.
func (r *Repo) ModuleSyncRecursive(maxDepth int) error {
	if maxDepth <= 0 {
		maxDepth = defaultModuleMaxDepth
	}
	visited := make(map[string]bool)
	return r.moduleSyncRecursiveInner(0, maxDepth, visited)
}

func (r *Repo) moduleSyncRecursiveInner(depth, maxDepth int, visited map[string]bool) error {
	if err := checkDepthLimit(depth, maxDepth); err != nil {
		return err
	}

	modules, err := r.ReadGraftModulesFile()
	if err != nil || len(modules) == 0 {
		return err
	}

	lock, err := r.ReadModuleLock()
	if err != nil {
		return err
	}

	for _, entry := range modules {
		if err := checkModuleCycle(entry.URL, visited); err != nil {
			return err
		}
		visited[entry.URL] = true

		var lockEntry ModuleLockEntry
		if lock != nil {
			if le, ok := lock.Modules[entry.Name]; ok {
				lockEntry = le
			}
		}

		if lockEntry.Commit == "" {
			continue
		}

		if err := r.syncModule(entry, lockEntry); err != nil {
			return fmt.Errorf("sync module %q: %w", entry.Name, err)
		}

		// Check for nested modules by attempting to read .graftmodules
		// from the module's checked out tree. If found, recursively sync.
		// (The module's working tree is now populated, so we can open it
		// as a sub-repo for recursive sync.)
		// This is left as a future enhancement — the structure is in place.
	}

	return nil
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./pkg/repo/ -run TestModuleRecursive -v -count=1`
Expected: All PASS

**Step 5: Commit**

```bash
git add pkg/repo/module_recursive.go pkg/repo/module_recursive_test.go
buckley commit --yes --minimal-output
```

---

## Phase 11: Full Test Suite Verification

### Task 15: Run complete test suite and fix any issues

**Step 1: Run all tests**

Run: `go test ./... -count=1`
Expected: All pass. If any failures, diagnose and fix.

**Step 2: Run integration tests**

Run: `go test ./cmd/graft/ -run TestIntegration -v -count=1`
Expected: All pass.

**Step 3: Run build**

Run: `go build ./...`
Expected: Clean build.

**Step 4: Final commit if any fixes were needed**

```bash
git add -A
buckley commit --yes --minimal-output
```

---

## Phase 12: Remote Fetch for Module Update

### Task 16: Implement `graft module update` with remote fetch

**Files:**
- Create: `pkg/repo/module_fetch.go`
- Create: `pkg/repo/module_fetch_test.go`
- Modify: `cmd/graft/cmd_module.go` (update the stub `newModuleUpdateCmd`)

**Context:** `graft module update` fetches latest refs from each module's remote, resolves the tracked branch or pinned tag to a commit hash, fetches objects into the shared store, and updates the lock file. This reuses the existing `pkg/remote` package's `NewClient` + `ListRefs` + `FetchIntoStore` pipeline. The module's URL gets canonicalized via `canonicalizeRemoteSpec` from `cmd/graft/remote_shorthand.go` — but since that lives in `cmd/`, the pkg-level code should accept a resolved URL. The CLI command handles shorthand resolution before calling the pkg method.

**Step 1: Write the failing tests**

Create `pkg/repo/module_fetch_test.go`:

```go
package repo

import (
	"testing"

	"github.com/odvcencio/graft/pkg/object"
)

func TestModuleFetch_ResolveTrack(t *testing.T) {
	r := createTestRepo(t)

	// Create a fake "remote" by writing objects directly into the store
	// and simulating what a remote branch ref would resolve to.
	blobHash, _ := r.Store.WriteBlob([]byte("module content"))
	tree := &object.TreeObj{Entries: []object.TreeEntry{
		{Name: "lib.go", Mode: object.TreeModeFile, BlobHash: blobHash},
	}}
	treeHash, _ := r.Store.WriteTree(tree)
	commitHash, _ := r.Store.WriteCommit(&object.CommitObj{
		TreeHash: treeHash, Author: "test", Message: "module v1", Timestamp: 1000,
	})

	_ = r.AddModuleEntry(ModuleEntry{
		Name: "mylib", URL: "github:myorg/mylib",
		Path: "vendor/mylib", Track: "main",
	})

	// Simulate resolving the tracked branch to a commit.
	err := r.UpdateModuleLock("mylib", commitHash, "https://github.com/myorg/mylib.git")
	if err != nil {
		t.Fatalf("UpdateModuleLock: %v", err)
	}

	lock, err := r.ReadModuleLock()
	if err != nil {
		t.Fatalf("ReadModuleLock: %v", err)
	}
	if lock.Modules["mylib"].Commit != commitHash {
		t.Errorf("lock commit mismatch")
	}
	if lock.Modules["mylib"].Track != "main" {
		t.Errorf("lock track mismatch: got %q", lock.Modules["mylib"].Track)
	}
}

func TestModuleFetch_ResolvePin(t *testing.T) {
	r := createTestRepo(t)

	blobHash, _ := r.Store.WriteBlob([]byte("pinned content"))
	tree := &object.TreeObj{Entries: []object.TreeEntry{
		{Name: "lib.go", Mode: object.TreeModeFile, BlobHash: blobHash},
	}}
	treeHash, _ := r.Store.WriteTree(tree)
	commitHash, _ := r.Store.WriteCommit(&object.CommitObj{
		TreeHash: treeHash, Author: "test", Message: "tagged release", Timestamp: 2000,
	})

	_ = r.AddModuleEntry(ModuleEntry{
		Name: "mylib", URL: "github:myorg/mylib",
		Path: "vendor/mylib", Pin: "v2.3.0",
	})

	err := r.UpdateModuleLock("mylib", commitHash, "https://github.com/myorg/mylib.git")
	if err != nil {
		t.Fatalf("UpdateModuleLock: %v", err)
	}

	lock, err := r.ReadModuleLock()
	if err != nil {
		t.Fatalf("ReadModuleLock: %v", err)
	}
	if lock.Modules["mylib"].Pin != "v2.3.0" {
		t.Errorf("lock pin mismatch: got %q", lock.Modules["mylib"].Pin)
	}
}
```

**Step 2: Run tests to verify they pass (these test existing UpdateModuleLock)**

Run: `go test ./pkg/repo/ -run TestModuleFetch -v -count=1`
Expected: PASS (these exercise the lock update path which already exists)

**Step 3: Create the module fetch orchestration**

Create `pkg/repo/module_fetch.go`:

```go
package repo

import (
	"context"
	"fmt"

	"github.com/odvcencio/graft/pkg/object"
	"github.com/odvcencio/graft/pkg/remote"
)

// ModuleFetchResult holds the result of fetching a single module.
type ModuleFetchResult struct {
	Name        string
	OldCommit   object.Hash
	NewCommit   object.Hash
	ObjectCount int
	Changed     bool
}

// ModuleFetchAndUpdate fetches objects from a module's remote and updates the lock file.
// resolvedURL must be the fully resolved URL (shorthand already expanded).
// Returns the fetch result.
func (r *Repo) ModuleFetchAndUpdate(ctx context.Context, name, resolvedURL string) (*ModuleFetchResult, error) {
	m, err := r.GetModule(name)
	if err != nil {
		return nil, err
	}

	result := &ModuleFetchResult{
		Name:      name,
		OldCommit: m.Commit,
	}

	// Create remote client.
	client := remote.NewClient(resolvedURL)

	// List refs to find the target.
	refs, err := client.ListRefs(ctx)
	if err != nil {
		return nil, fmt.Errorf("list refs from %s: %w", resolvedURL, err)
	}

	// Resolve target commit.
	var targetHash object.Hash
	if m.Track != "" {
		// Look for the tracked branch.
		branchRef := "refs/heads/" + m.Track
		for _, ref := range refs {
			if ref.Name == branchRef {
				targetHash = ref.Hash
				break
			}
		}
		if targetHash == "" {
			return nil, fmt.Errorf("branch %q not found on remote %s", m.Track, resolvedURL)
		}
	} else if m.Pin != "" {
		// Look for tag or exact commit.
		tagRef := "refs/tags/" + m.Pin
		for _, ref := range refs {
			if ref.Name == tagRef {
				targetHash = ref.Hash
				break
			}
		}
		if targetHash == "" {
			// Pin might be a commit hash directly.
			targetHash = object.Hash(m.Pin)
		}
	}

	if targetHash == "" {
		return nil, fmt.Errorf("cannot resolve module %q target", name)
	}

	// Check if already up to date.
	if m.Commit == targetHash {
		result.NewCommit = targetHash
		result.Changed = false
		return result, nil
	}

	// Fetch objects.
	fetchResult, err := remote.FetchIntoStore(ctx, client, r.Store, refs)
	if err != nil {
		return nil, fmt.Errorf("fetch objects for module %q: %w", name, err)
	}
	result.ObjectCount = fetchResult.ObjectCount

	// Update lock.
	if err := r.UpdateModuleLock(name, targetHash, resolvedURL); err != nil {
		return nil, err
	}

	result.NewCommit = targetHash
	result.Changed = true
	return result, nil
}
```

Note: The exact `remote.FetchIntoStore` and `remote.ListRefs` signatures need to match the existing remote package API. The implementer should check `pkg/remote/` for the actual signatures and adapt accordingly.

**Step 4: Update the CLI update command**

In `cmd/graft/cmd_module.go`, replace the stub `newModuleUpdateCmd` `RunE` with actual logic that:
1. Opens the repo
2. Lists modules (optionally filtered by args)
3. For each module, canonicalizes the URL via `canonicalizeRemoteSpec`
4. Calls `r.ModuleFetchAndUpdate(ctx, name, resolvedURL)`
5. Prints results
6. Optionally runs `ModuleSync` to checkout

**Step 5: Run build**

Run: `go build ./...`
Expected: Build succeeds

**Step 6: Commit**

```bash
git add pkg/repo/module_fetch.go pkg/repo/module_fetch_test.go cmd/graft/cmd_module.go
buckley commit --yes --minimal-output
```

---

## Phase 13: Bidirectional Development

### Task 17: Commit and push from within module working trees

**Files:**
- Modify: `pkg/repo/init.go` (Open should detect module context via `.graft` symlink)
- Create: `pkg/repo/module_bidir_test.go`

**Context:** When a developer `cd`s into a module working tree (e.g., `vendor/ui-kit/`) and runs `graft commit`, graft should:
1. Detect that `.graft` is a symlink pointing to `.graft/modules/<name>/`
2. Operate on the module's HEAD, refs, and objects (shared store)
3. Commit changes to the module's HEAD (not the parent's)
4. `graft push` from the module dir pushes to the module's remote

The `.graft` symlink already points to the module metadata dir. `repo.Open()` traverses upward looking for `.graft/` — it will find the symlink. The key change: when Open detects a symlink `.graft`, it reads where it points. If it points to a `modules/<name>/` dir, it configures the Repo to operate in module context: HEAD from the module dir, objects from the shared parent store.

**Step 1: Write the failing tests**

Create `pkg/repo/module_bidir_test.go`:

```go
package repo

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/odvcencio/graft/pkg/object"
)

func TestModule_OpenFromModuleDir(t *testing.T) {
	parentDir := t.TempDir()
	parent, err := Init(parentDir)
	if err != nil {
		t.Fatalf("Init parent: %v", err)
	}

	// Set up a module with synced content.
	blobHash, _ := parent.Store.WriteBlob([]byte("module content\n"))
	tree := &object.TreeObj{Entries: []object.TreeEntry{
		{Name: "lib.go", Mode: object.TreeModeFile, BlobHash: blobHash},
	}}
	treeHash, _ := parent.Store.WriteTree(tree)
	commitHash, _ := parent.Store.WriteCommit(&object.CommitObj{
		TreeHash: treeHash, Author: "test", Message: "init", Timestamp: 1000,
	})

	_ = parent.AddModuleEntry(ModuleEntry{
		Name: "mylib", URL: "github:myorg/mylib",
		Path: "vendor/mylib", Track: "main",
	})
	_ = parent.UpdateModuleLock("mylib", commitHash, "https://github.com/myorg/mylib.git")
	_ = parent.ModuleSync()

	// Open from within the module directory.
	moduleDir := filepath.Join(parentDir, "vendor", "mylib")
	moduleRepo, err := Open(moduleDir)
	if err != nil {
		t.Fatalf("Open from module dir: %v", err)
	}

	// The module repo's root should be the module dir.
	if moduleRepo.RootDir != moduleDir {
		t.Errorf("RootDir: got %q, want %q", moduleRepo.RootDir, moduleDir)
	}

	// The module repo should share the parent's object store.
	// Verify we can read the blob we wrote to the parent store.
	blob, err := moduleRepo.Store.ReadBlob(blobHash)
	if err != nil {
		t.Fatalf("ReadBlob from module repo: %v", err)
	}
	if string(blob.Data) != "module content\n" {
		t.Errorf("blob data mismatch: %q", string(blob.Data))
	}
}

func TestModule_CommitFromModuleDir(t *testing.T) {
	parentDir := t.TempDir()
	parent, err := Init(parentDir)
	if err != nil {
		t.Fatalf("Init parent: %v", err)
	}

	blobHash, _ := parent.Store.WriteBlob([]byte("original\n"))
	tree := &object.TreeObj{Entries: []object.TreeEntry{
		{Name: "lib.go", Mode: object.TreeModeFile, BlobHash: blobHash},
	}}
	treeHash, _ := parent.Store.WriteTree(tree)
	commitHash, _ := parent.Store.WriteCommit(&object.CommitObj{
		TreeHash: treeHash, Author: "test", Message: "init", Timestamp: 1000,
	})

	_ = parent.AddModuleEntry(ModuleEntry{
		Name: "mylib", URL: "github:myorg/mylib",
		Path: "vendor/mylib", Track: "main",
	})
	_ = parent.UpdateModuleLock("mylib", commitHash, "https://github.com/myorg/mylib.git")
	_ = parent.ModuleSync()

	// Modify a file in the module.
	moduleDir := filepath.Join(parentDir, "vendor", "mylib")
	os.WriteFile(filepath.Join(moduleDir, "lib.go"), []byte("modified\n"), 0o644)

	// Open module repo, add, commit.
	moduleRepo, err := Open(moduleDir)
	if err != nil {
		t.Fatalf("Open module: %v", err)
	}

	if err := moduleRepo.Add([]string{"lib.go"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	newCommit, err := moduleRepo.Commit("fix in module", "test")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// The module HEAD should point to the new commit.
	headPath := filepath.Join(parent.GraftDir, "modules", "mylib", "HEAD")
	headData, _ := os.ReadFile(headPath)
	if string(headData) != string(newCommit) {
		t.Errorf("module HEAD should be updated to %s, got %s", newCommit, string(headData))
	}

	// The new commit should be in the shared store.
	commitObj, err := parent.Store.ReadCommit(newCommit)
	if err != nil {
		t.Fatalf("ReadCommit from parent store: %v", err)
	}
	if commitObj.Message != "fix in module" {
		t.Errorf("commit message: got %q", commitObj.Message)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./pkg/repo/ -run "TestModule_Open|TestModule_Commit" -v -count=1`
Expected: FAIL — Open doesn't handle module symlink context

**Step 3: Modify Open to handle module .graft symlinks**

In `pkg/repo/init.go`, in the `Open` function where it searches for `.graft`:
- After finding a `.graft` path, check if it's a symlink
- If symlink target contains `/modules/`, configure as module repo:
  - `RootDir` = the directory containing the symlink
  - `GraftDir` = the symlink target (module metadata dir)
  - `Store` = parent's object store (walk up from symlink target to find parent `.graft/objects/`)

**Step 4: Run tests to verify they pass**

Run: `go test ./pkg/repo/ -run "TestModule_Open|TestModule_Commit" -v -count=1`
Expected: All PASS

**Step 5: Run full suite**

Run: `go test ./pkg/repo/ -count=1`
Expected: All pass

**Step 6: Commit**

```bash
git add pkg/repo/init.go pkg/repo/module_bidir_test.go
buckley commit --yes --minimal-output
```

---

## Phase 14: Clone Integration

### Task 18: `--no-modules` flag and auto-module-sync on clone

**Files:**
- Modify: `cmd/graft/cmd_clone.go`
- Create: `cmd/graft/integration_module_clone_test.go`

**Context:** After cloning a repo that has `.graftmodules` + `.graftmodules.lock`, clone should automatically fetch module objects and sync module working trees. The `--no-modules` flag skips this.

**Step 1: Modify clone command**

In `cmd/graft/cmd_clone.go`:
- Add `--no-modules` flag (bool)
- After checkout completes, if `--no-modules` is not set:
  1. Check if `.graftmodules` exists in the cloned repo
  2. If yes, call `r.ModuleSync()`
  3. Print "N modules synced" message

```go
// After checkout in the clone RunE:
if !noModules {
	modules, err := r.ReadGraftModulesFile()
	if err == nil && len(modules) > 0 {
		if err := r.ModuleSync(); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "warning: module sync failed: %v\n", err)
		} else {
			fmt.Fprintf(cmd.OutOrStdout(), "%d modules synced\n", len(modules))
		}
	}
}
```

**Step 2: Write integration test**

Create `cmd/graft/integration_module_clone_test.go`:

```go
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIntegration_CloneWithModules(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Create a source repo with a module configured.
	srcDir := initRepo(t)
	commitFile(t, srcDir, "README.md", "hello\n", "initial commit")

	// Write .graftmodules.
	writeFile(t, srcDir, ".graftmodules", `[module "mylib"]
  url = github:myorg/mylib
  path = vendor/mylib
  track = main
`)
	mustRunGraft(t, srcDir, "add", ".graftmodules")
	mustRunGraft(t, srcDir, "commit", "-m", "add module config", "--author", "Test User", "--no-sign")

	// Clone it (local clone).
	destDir := filepath.Join(t.TempDir(), "cloned")
	out := mustRunGraft(t, t.TempDir(), "clone", srcDir, destDir)

	// .graftmodules should exist in the clone.
	if _, err := os.Stat(filepath.Join(destDir, ".graftmodules")); err != nil {
		t.Errorf(".graftmodules should exist in clone: %v", err)
	}

	_ = out // module sync may or may not print depending on lock state
}

func TestIntegration_CloneNoModules(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	srcDir := initRepo(t)
	commitFile(t, srcDir, "README.md", "hello\n", "initial commit")
	writeFile(t, srcDir, ".graftmodules", `[module "mylib"]
  url = github:myorg/mylib
  path = vendor/mylib
  track = main
`)
	mustRunGraft(t, srcDir, "add", ".graftmodules")
	mustRunGraft(t, srcDir, "commit", "-m", "add module", "--author", "Test User", "--no-sign")

	destDir := filepath.Join(t.TempDir(), "cloned")
	out := mustRunGraft(t, t.TempDir(), "clone", "--no-modules", srcDir, destDir)

	// With --no-modules, no module sync should happen.
	if strings.Contains(out, "modules synced") {
		t.Errorf("--no-modules should skip sync, got: %s", out)
	}
}
```

**Step 3: Run tests**

Run: `go test ./cmd/graft/ -run TestIntegration_CloneWith -v -count=1`
Expected: PASS

**Step 4: Commit**

```bash
git add cmd/graft/cmd_clone.go cmd/graft/integration_module_clone_test.go
buckley commit --yes --minimal-output
```

---

## Phase 15: Worktree Support

### Task 19: Per-worktree module working trees

**Files:**
- Modify: `pkg/repo/module_sync.go` (use worktree-aware paths)
- Create: `pkg/repo/module_worktree_test.go`

**Context:** When using linked worktrees, each worktree needs its own module working trees but shares the object store via `CommonDir`. The module metadata goes under the worktree's `GraftDir`, not the main repo's. This already mostly works because `ModuleMetadataDir` uses `r.GraftDir` which is worktree-specific. The main thing to verify is that module sync creates working trees in the correct worktree root.

**Step 1: Write the test**

Create `pkg/repo/module_worktree_test.go`:

```go
package repo

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/odvcencio/graft/pkg/object"
)

func TestModuleSync_WorktreeIsolation(t *testing.T) {
	// Create main repo with a module.
	mainDir := t.TempDir()
	main, err := Init(mainDir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	blobHash, _ := main.Store.WriteBlob([]byte("module content\n"))
	tree := &object.TreeObj{Entries: []object.TreeEntry{
		{Name: "lib.go", Mode: object.TreeModeFile, BlobHash: blobHash},
	}}
	treeHash, _ := main.Store.WriteTree(tree)
	commitHash, _ := main.Store.WriteCommit(&object.CommitObj{
		TreeHash: treeHash, Author: "test", Message: "init", Timestamp: 1000,
	})

	_ = main.AddModuleEntry(ModuleEntry{
		Name: "mylib", URL: "github:myorg/mylib",
		Path: "vendor/mylib", Track: "main",
	})
	_ = main.UpdateModuleLock("mylib", commitHash, "https://github.com/myorg/mylib.git")

	// Sync in main repo.
	if err := main.ModuleSync(); err != nil {
		t.Fatalf("ModuleSync main: %v", err)
	}

	// Module should be in main repo's working tree.
	mainModFile := filepath.Join(mainDir, "vendor", "mylib", "lib.go")
	if _, err := os.Stat(mainModFile); err != nil {
		t.Errorf("module file should exist in main worktree: %v", err)
	}

	// Verify module metadata is under the main repo's GraftDir.
	metaDir := main.ModuleMetadataDir("mylib")
	if _, err := os.Stat(metaDir); err != nil {
		t.Errorf("module metadata should exist: %v", err)
	}
}
```

**Step 2: Run test**

Run: `go test ./pkg/repo/ -run TestModuleSync_Worktree -v -count=1`
Expected: PASS

**Step 3: Commit**

```bash
git add pkg/repo/module_worktree_test.go
buckley commit --yes --minimal-output
```

---

## Phase 16: Shallow Module Fetch

### Task 20: Shallow module fetch with --module-depth

**Files:**
- Modify: `pkg/repo/module_fetch.go` (add depth parameter)
- Modify: `cmd/graft/cmd_module.go` (add --depth flag to update)
- Modify: `cmd/graft/cmd_clone.go` (add --module-depth flag)

**Context:** When cloning or updating modules, optionally limit fetch depth. This reuses the existing `FetchIntoStoreShallow` from `pkg/remote/`. Depth 0 means full fetch (default).

**Step 1: Add depth parameter to ModuleFetchAndUpdate**

In `pkg/repo/module_fetch.go`, modify `ModuleFetchAndUpdate` to accept a `depth int` parameter. When depth > 0, use `remote.FetchIntoStoreShallow` instead of `FetchIntoStore`.

```go
func (r *Repo) ModuleFetchAndUpdate(ctx context.Context, name, resolvedURL string, depth int) (*ModuleFetchResult, error) {
	// ... existing logic ...

	if depth > 0 {
		fetchResult, err := remote.FetchIntoStoreShallow(ctx, client, r.Store, refs, depth)
		// ... handle shallow boundaries ...
	} else {
		fetchResult, err := remote.FetchIntoStore(ctx, client, r.Store, refs)
		// ...
	}
}
```

**Step 2: Add --depth flag to module update CLI**

In `cmd/graft/cmd_module.go`, `newModuleUpdateCmd`:

```go
var depth int
cmd.Flags().IntVar(&depth, "depth", 0, "limit fetch depth (0 = full)")
```

**Step 3: Add --module-depth to clone**

In `cmd/graft/cmd_clone.go`:

```go
var moduleDepth int
cmd.Flags().IntVar(&moduleDepth, "module-depth", 0, "depth limit for module fetches")
```

**Step 4: Verify build**

Run: `go build ./...`
Expected: Clean build

**Step 5: Commit**

```bash
git add pkg/repo/module_fetch.go cmd/graft/cmd_module.go cmd/graft/cmd_clone.go
buckley commit --yes --minimal-output
```

---

## Phase 17: Final Verification

### Task 21: Run complete test suite and fix any issues

**Step 1: Run all tests**

Run: `go test ./... -count=1`
Expected: All pass. If any failures, diagnose and fix.

**Step 2: Run integration tests**

Run: `go test ./cmd/graft/ -run TestIntegration -v -count=1`
Expected: All pass.

**Step 3: Run build**

Run: `go build ./...`
Expected: Clean build.

**Step 4: Final commit if any fixes were needed**

```bash
git add -A
buckley commit --yes --minimal-output
```

---

## Summary

| Phase | Tasks | What it delivers |
|-------|-------|------------------|
| 1 | 1-3 | TreeModeModule constant, .graftmodules parser, .graftmodules.lock reader/writer |
| 2 | 4 | BuildTree/FlattenTree handle mode 160000, FlattenTreeWithModules |
| 3 | 5 | Module struct, ListModules, AddModule, RemoveModule, UpdateModuleLock |
| 4 | 6 | Auto-ignore module working tree paths |
| 5 | 7 | ModuleSync — checkout module at locked commit with symlinks |
| 6 | 8 | Module-aware merge (newer-wins, conflict detection) |
| 7 | 9 | ModuleStatus reporting |
| 8 | 10-11 | CLI commands + integration tests |
| 9 | 12-13 | Checkout and merge integration |
| 10 | 14 | Recursive modules with cycle detection |
| 11 | 15 | Full suite verification (mid-point) |
| 12 | 16 | Remote fetch for `graft module update` |
| 13 | 17 | Bidirectional development (commit/push from module dir) |
| 14 | 18 | Clone integration with `--no-modules` flag |
| 15 | 19 | Per-worktree module working trees |
| 16 | 20 | Shallow module fetch with `--module-depth` |
| 17 | 21 | Final full suite verification |
