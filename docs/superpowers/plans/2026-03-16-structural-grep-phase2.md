# Structural Grep Phase 2: Graft Integration

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `graft grep` structural by default, with entity context in results and MCP tools for AI agents.

**Architecture:** The existing `cmd_grep.go` is refactored so structural grep is the default mode. A new `pkg/repo/structural_grep.go` wraps the gotreesitter `grep` package with entity context (identity key, kind, name for each match). MCP tools `graft_grep`, `graft_grep_replace`, and `graft_entity_edit` are added following the existing `graft_ci_*` pattern.

**Tech Stack:** Go, gotreesitter grep package, cobra CLI, graft entity/repo packages

**Spec:** `docs/superpowers/specs/2026-03-16-structural-grep-design.md`

**Repo:** `~/work/graft`

### gotreesitter API

graft already has `replace github.com/odvcencio/gotreesitter => ../gotreesitter` in go.mod, so the new `grep` package is available as `github.com/odvcencio/gotreesitter/grep`.

Key imports:
```go
import (
    tsgrep "github.com/odvcencio/gotreesitter/grep"
    "github.com/odvcencio/gotreesitter/grammars"
)
```

---

## Chunk 1: Structural Grep in the Repo Layer

### Task 1: Create `pkg/repo/structural_grep.go`

**Files:**
- Create: `pkg/repo/structural_grep.go`
- Create: `pkg/repo/structural_grep_test.go`

This wraps the gotreesitter grep engine with graft-specific features: file traversal, entity context, and language detection.

- [ ] **Step 1: Write failing tests**

```go
// pkg/repo/structural_grep_test.go
package repo

import (
    "os"
    "path/filepath"
    "testing"
)

func TestStructuralGrep_BasicMatch(t *testing.T) {
    // Create a temp repo with a Go file
    dir := t.TempDir()
    initTestRepo(t, dir)
    writeFile(t, dir, "main.go", `package main

func Hello() error { return nil }
func World() {}
`)
    r, err := Open(dir)
    if err != nil {
        t.Fatal(err)
    }

    results, err := r.StructuralGrep(StructuralGrepOptions{
        Pattern: "func $NAME() error",
    })
    if err != nil {
        t.Fatal(err)
    }

    if len(results) != 1 {
        t.Fatalf("expected 1 match, got %d", len(results))
    }
    if results[0].Captures["NAME"] != "Hello" {
        t.Errorf("expected NAME=Hello, got %s", results[0].Captures["NAME"])
    }
}

func TestStructuralGrep_EntityContext(t *testing.T) {
    dir := t.TempDir()
    initTestRepo(t, dir)
    writeFile(t, dir, "main.go", `package main

func ProcessOrder() error {
    err := validate()
    if err != nil {
        return err
    }
    return nil
}
`)
    r, err := Open(dir)
    if err != nil {
        t.Fatal(err)
    }

    results, err := r.StructuralGrep(StructuralGrepOptions{
        Pattern: "if $ERR != nil",
    })
    if err != nil {
        t.Fatal(err)
    }

    if len(results) == 0 {
        t.Fatal("expected at least one match")
    }
    // Should include entity context
    if results[0].EntityName == "" {
        t.Error("expected entity context (EntityName)")
    }
}

// Helpers
func initTestRepo(t *testing.T, dir string) {
    t.Helper()
    if err := Init(dir); err != nil {
        t.Fatal(err)
    }
}

func writeFile(t *testing.T, dir, name, content string) {
    t.Helper()
    path := filepath.Join(dir, name)
    os.MkdirAll(filepath.Dir(path), 0o755)
    if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
        t.Fatal(err)
    }
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd ~/work/graft && go test ./pkg/repo/ -v -run TestStructuralGrep -count=1`
Expected: FAIL (StructuralGrep not defined)

- [ ] **Step 3: Implement structural grep**

```go
// pkg/repo/structural_grep.go
package repo

import (
    "path/filepath"
    "os"

    tsgrep "github.com/odvcencio/gotreesitter/grep"
    "github.com/odvcencio/gotreesitter/grammars"
    "github.com/odvcencio/graft/pkg/entity"
)

// StructuralGrepOptions configures a structural grep search.
type StructuralGrepOptions struct {
    Pattern     string // code pattern with metavariables
    PathPattern string // glob filter on file path
    JSON        bool   // output as JSON
}

// StructuralGrepResult represents a single structural match with entity context.
type StructuralGrepResult struct {
    Path        string            // file path
    StartLine   int               // 1-based line number
    EndLine     int
    StartByte   uint32
    EndByte     uint32
    Captures    map[string]string // capture name → matched text
    EntityName  string            // enclosing entity name (if available)
    EntityKind  string            // enclosing entity kind
    EntityKey   string            // enclosing entity identity key
    MatchedText string            // the full matched source text
}

// StructuralGrep performs structural pattern matching across working tree files.
func (r *Repo) StructuralGrep(opts StructuralGrepOptions) ([]StructuralGrepResult, error) {
    // Walk working tree files
    var results []StructuralGrepResult

    err := filepath.Walk(r.RootDir, func(path string, info os.FileInfo, err error) error {
        if err != nil {
            return nil // skip errors
        }
        if info.IsDir() {
            base := filepath.Base(path)
            if base == ".graft" || base == ".git" || base == "vendor" || base == "node_modules" {
                return filepath.SkipDir
            }
            return nil
        }

        relPath, _ := filepath.Rel(r.RootDir, path)

        // Apply path filter
        if opts.PathPattern != "" {
            matched, _ := filepath.Match(opts.PathPattern, relPath)
            if !matched {
                return nil
            }
        }

        // Detect language
        entry := grammars.DetectLanguage(path)
        if entry == nil {
            return nil // skip files without grammar
        }
        lang := entry.Language()

        // Read file
        source, err := os.ReadFile(path)
        if err != nil {
            return nil
        }

        // Run structural match
        matches, err := tsgrep.Match(lang, opts.Pattern, source)
        if err != nil {
            return nil // skip files that fail to match
        }

        if len(matches) == 0 {
            return nil
        }

        // Extract entities for context (best-effort)
        entities, _ := entity.Extract(relPath, source)

        for _, m := range matches {
            result := StructuralGrepResult{
                Path:      relPath,
                StartByte: m.StartByte,
                EndByte:   m.EndByte,
                Captures:  make(map[string]string),
            }

            // Convert byte offsets to line numbers
            result.StartLine = byteToLine(source, m.StartByte)
            result.EndLine = byteToLine(source, m.EndByte)
            result.MatchedText = string(source[m.StartByte:m.EndByte])

            // Map captures
            for name, cap := range m.Captures {
                result.Captures[name] = string(cap.Text)
            }

            // Find enclosing entity
            if entities != nil {
                for _, ent := range entities.Entities {
                    if ent.StartByte <= m.StartByte && ent.EndByte >= m.EndByte {
                        result.EntityName = ent.Name
                        result.EntityKind = ent.Kind.String()
                        result.EntityKey = ent.IdentityKey()
                        break
                    }
                }
            }

            results = append(results, result)
        }
        return nil
    })

    return results, err
}

// byteToLine converts a byte offset to a 1-based line number.
func byteToLine(source []byte, offset uint32) int {
    line := 1
    for i := uint32(0); i < offset && i < uint32(len(source)); i++ {
        if source[i] == '\n' {
            line++
        }
    }
    return line
}
```

NOTE: The implementer should check if `Init()` exists in the repo package for test setup. If not, adapt the test to use whatever repo initialization the existing tests use. Look at existing test files in `pkg/repo/` for patterns.

- [ ] **Step 4: Run tests and iterate**

Run: `cd ~/work/graft && go test ./pkg/repo/ -v -run TestStructuralGrep -count=1`
Expected: iterate on test setup as needed

- [ ] **Step 5: Commit**

```bash
cd ~/work/graft && git add pkg/repo/structural_grep.go pkg/repo/structural_grep_test.go && buckley commit --yes --minimal-output
```

---

### Task 2: Refactor `graft grep` Command — Structural Default

**Files:**
- Modify: `cmd/graft/cmd_grep.go`

The existing command has two paths: `--entity` → entity search, default → line grep. We add structural grep as the new default, with `-L`/`--line` to opt into line mode.

- [ ] **Step 1: Read the existing cmd_grep.go**

Understand the current flag structure and execution paths before modifying.

- [ ] **Step 2: Add new flags and refactor execution paths**

Add flags:
- `-L, --line` (bool): force line-level grep (the old default)
- `-S, --structural` (bool): force structural grep (for clarity, though it's the default)
- `--rewrite` (string): replacement template for structural rewrite
- `--sexp` (bool): treat pattern as raw S-expression

Modify the `RunE` function:
1. If `--entity` → `runEntitySearch()` (unchanged)
2. If `--line` → `runLineGrep()` (unchanged, just renamed flag)
3. Default (or `--structural`) → `runStructuralGrep()` (new)
4. Fall back to line grep if pattern has no metavariables and fails structural parse

- [ ] **Step 3: Implement `runStructuralGrep()`**

```go
func runStructuralGrep(cmd *cobra.Command, pattern string, jsonOutput bool, rewrite string) error {
    r, err := repo.Open(".")
    if err != nil {
        return err
    }

    results, err := r.StructuralGrep(repo.StructuralGrepOptions{
        Pattern: pattern,
    })
    if err != nil {
        // Fall back to line grep with warning
        fmt.Fprintf(os.Stderr, "structural parse failed, falling back to line grep\n")
        return runLineGrep(cmd, pattern, jsonOutput)
    }

    if jsonOutput {
        return writeJSON(os.Stdout, results)
    }

    for _, m := range results {
        entityCtx := ""
        if m.EntityName != "" {
            entityCtx = fmt.Sprintf(" :: %s %s (%s)", m.EntityKind, m.EntityName, m.EntityKey)
        }
        fmt.Printf("%s:%d%s\n", m.Path, m.StartLine, entityCtx)
        for name, text := range m.Captures {
            fmt.Printf("  $%s = %s\n", name, text)
        }
    }
    return nil
}
```

- [ ] **Step 4: Update flag definitions**

Add new flags to the command, keep backward compat for existing flags.

- [ ] **Step 5: Test manually**

Run: `cd ~/work/graft && go build ./cmd/graft/ && ./graft grep 'func $NAME($$$) error'`
Verify structural results with entity context.

Run: `cd ~/work/graft && ./graft grep -L "processOrder"`
Verify line-level grep still works.

- [ ] **Step 6: Commit**

```bash
cd ~/work/graft && git add cmd/graft/cmd_grep.go && buckley commit --yes --minimal-output
```

---

## Chunk 2: MCP Tools

### Task 3: Add `graft_grep` MCP Tool

**Files:**
- Create: `cmd/graft/cmd_mcp_grep.go`
- Modify: `cmd/graft/cmd_mcp.go` (register new tools in dispatch)

Follow the pattern from `cmd_mcp_codeintel.go` exactly.

- [ ] **Step 1: Create tool definitions**

```go
// cmd/graft/cmd_mcp_grep.go
package main

import (
    "fmt"

    tsgrep "github.com/odvcencio/gotreesitter/grep"
    "github.com/odvcencio/gotreesitter/grammars"
    "github.com/odvcencio/graft/pkg/repo"
)

func mcpGrepToolDefs() []mcpTool {
    return []mcpTool{
        {
            Name:        "graft_grep",
            Description: "Structural pattern search across working tree files. Uses code patterns with metavariables ($NAME, $$$ARGS) to match AST structures.",
            InputSchema: mcpSchema{
                Properties: []mcpProp{
                    {Name: "pattern", Type: "string", Desc: "Code pattern with metavariables (e.g., 'func $NAME($$$) error')"},
                    {Name: "lang", Type: "string", Desc: "Language name (optional, auto-detected from file extensions)"},
                    {Name: "path_pattern", Type: "string", Desc: "Glob pattern to filter files (optional)"},
                },
                Required: []string{"pattern"},
            }.toMap(),
        },
        {
            Name:        "graft_grep_replace",
            Description: "Structural search and replace with preview. Finds pattern matches and generates replacement edits.",
            InputSchema: mcpSchema{
                Properties: []mcpProp{
                    {Name: "pattern", Type: "string", Desc: "Code pattern to match"},
                    {Name: "replacement", Type: "string", Desc: "Replacement template with capture references ($NAME)"},
                    {Name: "lang", Type: "string", Desc: "Language name (optional)"},
                    {Name: "path_pattern", Type: "string", Desc: "Glob pattern to filter files (optional)"},
                    {Name: "apply", Type: "boolean", Desc: "If true, apply edits to files. If false (default), return preview only."},
                },
                Required: []string{"pattern", "replacement"},
            }.toMap(),
        },
        {
            Name:        "graft_entity_edit",
            Description: "Symbol-level edit operations: replace body, insert after/before, delete an entity by identity key.",
            InputSchema: mcpSchema{
                Properties: []mcpProp{
                    {Name: "file", Type: "string", Desc: "File path relative to repo root"},
                    {Name: "entity_key", Type: "string", Desc: "Entity identity key (e.g., 'decl:function_definition::ProcessOrder')"},
                    {Name: "operation", Type: "string", Desc: "One of: replace_body, insert_after, insert_before, delete"},
                    {Name: "content", Type: "string", Desc: "New content (for replace_body, insert_after, insert_before)"},
                },
                Required: []string{"file", "entity_key", "operation"},
            }.toMap(),
        },
    }
}
```

- [ ] **Step 2: Implement tool dispatch**

```go
func mcpDispatchGrepTool(name string, args map[string]any) (any, error) {
    switch name {
    case "graft_grep":
        return mcpToolGrep(args)
    case "graft_grep_replace":
        return mcpToolGrepReplace(args)
    case "graft_entity_edit":
        return mcpToolEntityEdit(args)
    default:
        return nil, fmt.Errorf("unknown grep tool: %s", name)
    }
}
```

- [ ] **Step 3: Implement `mcpToolGrep`**

```go
func mcpToolGrep(args map[string]any) (any, error) {
    pattern := mcpArgString(args, "pattern")
    if pattern == "" {
        return nil, fmt.Errorf("pattern is required")
    }

    r, err := repo.Open(".")
    if err != nil {
        return nil, err
    }

    results, err := r.StructuralGrep(repo.StructuralGrepOptions{
        Pattern:     pattern,
        PathPattern: mcpArgString(args, "path_pattern"),
    })
    if err != nil {
        return nil, err
    }

    return results, nil
}
```

- [ ] **Step 4: Implement `mcpToolGrepReplace`**

Similar to mcpToolGrep but uses `tsgrep.Replace` on each file and returns edits. If `apply` is true, writes files.

- [ ] **Step 5: Implement `mcpToolEntityEdit`**

Reads the file, extracts entities, finds the entity by key, applies the operation (replace body, insert after/before, delete), writes the file.

- [ ] **Step 6: Register tools in cmd_mcp.go**

Add to `handleRequest()` tools/list:
```go
tools = append(tools, mcpGrepToolDefs()...)
```

Add to `mcpDispatchAll()`:
```go
case strings.HasPrefix(name, "graft_grep"), name == "graft_entity_edit":
    return mcpDispatchGrepTool(name, args)
```

- [ ] **Step 7: Test MCP tools**

Run: `cd ~/work/graft && go build ./cmd/graft/`
Test with: `echo '{"jsonrpc":"2.0","id":1,"method":"tools/list"}' | ./graft mcp serve`
Verify the three new tools appear in the list.

- [ ] **Step 8: Commit**

```bash
cd ~/work/graft && git add cmd/graft/cmd_mcp_grep.go cmd/graft/cmd_mcp.go && buckley commit --yes --minimal-output
```

---

## Summary

| Task | What | Files |
|------|------|-------|
| 1 | Structural grep in repo layer | `pkg/repo/structural_grep.go` |
| 2 | Refactor graft grep command | `cmd/graft/cmd_grep.go` |
| 3 | MCP tools (grep, replace, entity edit) | `cmd/graft/cmd_mcp_grep.go`, `cmd/graft/cmd_mcp.go` |

**Dependencies:** Task 1 first (repo layer), then Tasks 2 and 3 can run in parallel (CLI and MCP both depend on the repo layer).

**Not in this phase:** History search (`--history`, `--since`) — deferred to a follow-up since it requires walking commit history and extracting entity lists per commit. The spec acknowledges this as a separate feature.
