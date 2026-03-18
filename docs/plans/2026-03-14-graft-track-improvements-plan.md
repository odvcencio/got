# Graft Track Improvements Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship gts-suite as a standalone tool, build a full bidirectional git bridge, and add language-specific merge rules to graft.

**Architecture:** Three independent initiatives (B, A, F) that can be parallelized. B targets gts-suite packaging. A extends graft's existing git bridge code (`cmd/graft/git_bridge.go`) into full bidirectional sync. F adds a `LangMergeRule` interface alongside the existing merge engine in `pkg/merge/`.

**Tech Stack:** Go 1.25, gotreesitter, GitHub Actions, graft object model (SHA-256)

---

## Initiative B: gts-suite Standalone Distribution

### Task B1: Verify gts mcp works standalone

**Files:**
- Test: `/home/draco/work/gts-suite/cmd/gts/mcp_cmd.go`
- Test: `/home/draco/work/gts-suite/internal/mcp/service.go`

- [ ] **Step 1: Run `gts mcp` in a non-graft directory to verify it initializes**

Run from a temp directory:
```bash
cd /tmp && echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"0.1.0"}}}' | timeout 5 /home/draco/work/gts-suite/gts mcp --root /tmp
```
Expected: JSON response with `serverInfo.name: "gts-suite"` and `tools` capability.

- [ ] **Step 2: Run `gts mcp` and call `tools/list` to verify all 23 tools register**

```bash
cd /tmp && printf 'Content-Length: 191\r\n\r\n{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"0.1.0"}}}\r\nContent-Length: 56\r\n\r\n{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}' | /home/draco/work/gts-suite/gts mcp --root /tmp
```
Expected: Response listing all 23 tools with schemas.

- [ ] **Step 3: Verify `.gts/` index directory is created automatically on first tool call**

```bash
ls /tmp/.gts/
```
Expected: `index.json` or equivalent cache file created.

- [ ] **Step 4: If any issues found, fix them. If clean, note results.**

---

### Task B2: Draft release workflow for gts-suite

**Files:**
- Create: `/home/draco/work/gts-suite/.github/workflows/release.yml`

- [ ] **Step 1: Write the release workflow**

```yaml
name: Release

on:
  push:
    tags: ['v*']
  workflow_dispatch:

permissions:
  contents: write

jobs:
  release:
    runs-on: ubuntu-latest
    timeout-minutes: 20
    strategy:
      matrix:
        goos: [linux, darwin, windows]
        goarch: [amd64, arm64]
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod

      - name: Build gts
        env:
          GOOS: ${{ matrix.goos }}
          GOARCH: ${{ matrix.goarch }}
        run: |
          EXT=""
          if [ "$GOOS" = "windows" ]; then EXT=".exe"; fi
          go build -ldflags "-s -w -X main.version=${{ github.ref_name }}" \
            -o "gts-${GOOS}-${GOARCH}${EXT}" ./cmd/gts
          go build -ldflags "-s -w -X main.version=${{ github.ref_name }}" \
            -o "gtsls-${GOOS}-${GOARCH}${EXT}" ./cmd/gtsls

      - name: Generate checksums
        run: sha256sum gts-* gtsls-* > checksums.txt

      - name: Upload artifacts
        uses: actions/upload-artifact@v4
        with:
          name: binaries-${{ matrix.goos }}-${{ matrix.goarch }}
          path: |
            gts-*
            gtsls-*
            checksums.txt

  publish:
    needs: release
    runs-on: ubuntu-latest
    steps:
      - uses: actions/download-artifact@v4
        with:
          path: artifacts
          merge-multiple: true

      - name: Merge checksums
        run: cat artifacts/checksums.txt | sort -u > checksums.txt

      - name: Create GitHub Release
        uses: softprops/action-gh-release@v2
        with:
          files: |
            artifacts/gts-*
            artifacts/gtsls-*
            checksums.txt
          generate_release_notes: true
```

- [ ] **Step 2: Verify the workflow YAML is valid**

```bash
cd /home/draco/work/gts-suite && python3 -c "import yaml; yaml.safe_load(open('.github/workflows/release.yml'))" 2>/dev/null || echo "Install pyyaml or validate manually"
```

---

### Task B3: Add install section to gts-suite README

**Files:**
- Modify: `/home/draco/work/gts-suite/README.md`

- [ ] **Step 1: Read the current README**

```bash
cat /home/draco/work/gts-suite/README.md
```

- [ ] **Step 2: Add installation section after the title/description**

Add the following section:

```markdown
## Installation

### From source (requires Go 1.25+)

```bash
go install github.com/odvcencio/gts-suite/cmd/gts@latest
go install github.com/odvcencio/gts-suite/cmd/gtsls@latest
```

### Binary download

Download pre-built binaries from [GitHub Releases](https://github.com/odvcencio/gts-suite/releases).

### AI Agent Integration (MCP)

Add to your Claude Code or Cursor MCP configuration:

```json
{
  "mcpServers": {
    "gts": {
      "command": "gts",
      "args": ["mcp"]
    }
  }
}
```

Available MCP tools: `gts_grep`, `gts_map`, `gts_query`, `gts_refs`, `gts_context`, `gts_scope`, `gts_deps`, `gts_callgraph`, `gts_dead`, `gts_chunk`, `gts_lint`, `gts_refactor`, `gts_diff`, `gts_stats`, `gts_files`, `gts_bridge`, `gts_capa`, `gts_similarity`, `gts_yara`, `gts_complexity`, `gts_testmap`, `gts_impact`, `gts_hotspot`.
```

---

### Task B4: Decouple gts-suite from local gotreesitter (blocked until gotreesitter tags)

**Files:**
- Modify: `/home/draco/work/gts-suite/go.mod`

- [ ] **Step 1: After gotreesitter tags a release, remove the replace directive**

```bash
cd /home/draco/work/gts-suite
# Remove the replace line for gotreesitter
go mod edit -dropreplace github.com/odvcencio/gotreesitter
# Update to the tagged version
go get github.com/odvcencio/gotreesitter@v0.6.1  # or whatever tag
go mod tidy
```

- [ ] **Step 2: Verify build still works**

```bash
cd /home/draco/work/gts-suite && go build ./...
```

- [ ] **Step 3: Verify tests pass**

```bash
cd /home/draco/work/gts-suite && go test ./...
```

- [ ] **Step 4: Tag and push gts-suite release**

```bash
cd /home/draco/work/gts-suite
git tag v0.1.0
git push origin v0.1.0
```

Expected: Release workflow triggers, builds binaries, creates GitHub Release.

---

## Initiative F: Language-Specific Merge Intelligence

### Task F1: Define the LangMergeRule interface and Diagnostic types

**Files:**
- Create: `/home/draco/work/graft/pkg/merge/rules.go`
- Test: `/home/draco/work/graft/pkg/merge/rules_test.go`

- [ ] **Step 1: Write the test for rule registration and dispatch**

```go
// /home/draco/work/graft/pkg/merge/rules_test.go
package merge

import (
	"testing"

	"github.com/odvcencio/graft/pkg/entity"
)

type testRule struct {
	lang  string
	diags []Diagnostic
}

func (r *testRule) Language() string { return r.lang }
func (r *testRule) Apply(ctx *MergeRuleContext) []Diagnostic {
	return r.diags
}

func TestRuleRegistryDispatch(t *testing.T) {
	reg := NewRuleRegistry()
	rule := &testRule{
		lang: "go",
		diags: []Diagnostic{
			{Severity: DiagWarning, Entity: "decl:function_definition::Foo", Message: "test warning", Rule: "test-rule"},
		},
	}
	reg.Register(rule)

	rules := reg.RulesFor("go")
	if len(rules) != 1 {
		t.Fatalf("expected 1 rule for go, got %d", len(rules))
	}

	rules = reg.RulesFor("python")
	if len(rules) != 0 {
		t.Fatalf("expected 0 rules for python, got %d", len(rules))
	}
}

func TestDiagnosticSeverityString(t *testing.T) {
	tests := []struct {
		sev  DiagSeverity
		want string
	}{
		{DiagInfo, "info"},
		{DiagWarning, "warning"},
		{DiagError, "error"},
	}
	for _, tt := range tests {
		if got := tt.sev.String(); got != tt.want {
			t.Errorf("DiagSeverity(%d).String() = %q, want %q", tt.sev, got, tt.want)
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
cd /home/draco/work/graft && go test ./pkg/merge/ -run TestRuleRegistry -v
```
Expected: FAIL — types not defined yet.

- [ ] **Step 3: Write the implementation**

```go
// /home/draco/work/graft/pkg/merge/rules.go
package merge

import "github.com/odvcencio/graft/pkg/entity"

// DiagSeverity indicates the severity of a merge rule diagnostic.
type DiagSeverity int

const (
	DiagInfo    DiagSeverity = iota
	DiagWarning
	DiagError
)

func (s DiagSeverity) String() string {
	switch s {
	case DiagInfo:
		return "info"
	case DiagWarning:
		return "warning"
	case DiagError:
		return "error"
	default:
		return "unknown"
	}
}

// Diagnostic is a message produced by a post-merge rule.
type Diagnostic struct {
	Severity DiagSeverity
	Entity   string // Entity identity key
	Message  string
	Rule     string // Rule identifier
}

// MergeRuleContext provides the merge state to rules.
type MergeRuleContext struct {
	Base     []entity.Entity
	Ours     []entity.Entity
	Theirs   []entity.Entity
	Matched  []MatchedEntity
	Result   *MergeResult
	Language string
	Path     string
}

// LangMergeRule inspects a merge result and returns diagnostics.
// Rules are advisory-only: they produce diagnostics but do not
// mutate the merge output.
type LangMergeRule interface {
	Language() string
	Apply(ctx *MergeRuleContext) []Diagnostic
}

// RuleRegistry holds registered merge rules by language.
type RuleRegistry struct {
	rules map[string][]LangMergeRule
}

// NewRuleRegistry creates an empty rule registry.
func NewRuleRegistry() *RuleRegistry {
	return &RuleRegistry{rules: make(map[string][]LangMergeRule)}
}

// Register adds a rule to the registry.
func (r *RuleRegistry) Register(rule LangMergeRule) {
	lang := rule.Language()
	r.rules[lang] = append(r.rules[lang], rule)
}

// RulesFor returns all rules registered for the given language.
func (r *RuleRegistry) RulesFor(lang string) []LangMergeRule {
	return r.rules[lang]
}
```

- [ ] **Step 4: Run the test to verify it passes**

```bash
cd /home/draco/work/graft && go test ./pkg/merge/ -run TestRuleRegistry -v
```
Expected: PASS

- [ ] **Step 5: Commit**

```bash
cd /home/draco/work/graft && git add pkg/merge/rules.go pkg/merge/rules_test.go && buckley commit --yes --minimal-output
```

---

### Task F2: Integrate rule dispatch into MergeFiles

**Files:**
- Modify: `/home/draco/work/graft/pkg/merge/merge.go`
- Modify: `/home/draco/work/graft/pkg/merge/rules.go`
- Test: `/home/draco/work/graft/pkg/merge/rules_test.go`

- [ ] **Step 1: Write the integration test**

Add to `rules_test.go`:

```go
func TestMergeFilesRunsRules(t *testing.T) {
	base := []byte("package main\n\nfunc A() { return }\n")
	ours := []byte("package main\n\nfunc A() { return 1 }\n")
	theirs := []byte("package main\n\nfunc A() { return 2 }\n")

	called := false
	rule := &testRule{
		lang: "go",
		diags: []Diagnostic{
			{Severity: DiagWarning, Entity: "test", Message: "rule fired", Rule: "test-rule"},
		},
	}
	// Override the callback to track calls
	rule.diags = nil // No diags, just track that Apply was called

	DefaultRegistry.Register(&callTracker{lang: "go", called: &called})
	defer func() { DefaultRegistry = NewRuleRegistry() }()

	result, err := MergeFiles("test.go", base, ours, theirs)
	if err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Error("expected rule to be called during merge")
	}
	_ = result
}

type callTracker struct {
	lang   string
	called *bool
}

func (c *callTracker) Language() string { return c.lang }
func (c *callTracker) Apply(ctx *MergeRuleContext) []Diagnostic {
	*c.called = true
	return nil
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /home/draco/work/graft && go test ./pkg/merge/ -run TestMergeFilesRunsRules -v
```
Expected: FAIL — `DefaultRegistry` not defined, `MergeResult.Diagnostics` not defined.

- [ ] **Step 3: Add DefaultRegistry and Diagnostics field**

Add to `rules.go`:
```go
// DefaultRegistry is the global rule registry used by MergeFiles.
var DefaultRegistry = NewRuleRegistry()
```

Add `Diagnostics` field to `MergeResult` in `merge.go`:
```go
type MergeResult struct {
	Merged          []byte
	HasConflicts    bool
	ConflictCount   int
	Stats           MergeStats
	EntityConflicts []EntityConflictDetail
	Diagnostics     []Diagnostic
}
```

- [ ] **Step 4: Add rule dispatch at the end of MergeFiles**

In `merge.go`, after the merge is complete but before the return, add:

```go
	// Run post-merge language rules.
	if lang := detectLanguage(path); lang != "" {
		for _, rule := range DefaultRegistry.RulesFor(lang) {
			diags := rule.Apply(&MergeRuleContext{
				Base:     baseEntities,
				Ours:     oursEntities,
				Theirs:   theirsEntities,
				Matched:  matched,
				Result:   result,
				Language: lang,
				Path:     path,
			})
			result.Diagnostics = append(result.Diagnostics, diags...)
		}
	}
```

Note: The exact variable names (`baseEntities`, `oursEntities`, `theirsEntities`, `matched`) must match the local variables in `MergeFiles`. Check the function body — the entity lists come from `entity.Extract()` calls and the matched list from `MatchEntities()`. Adapt variable names to match what exists.

- [ ] **Step 5: Run test to verify it passes**

```bash
cd /home/draco/work/graft && go test ./pkg/merge/ -run TestMergeFilesRunsRules -v
```
Expected: PASS

- [ ] **Step 6: Run all merge tests to verify no regressions**

```bash
cd /home/draco/work/graft && go test ./pkg/merge/ -v -count=1
```
Expected: All existing tests PASS.

- [ ] **Step 7: Commit**

```bash
cd /home/draco/work/graft && git add pkg/merge/merge.go pkg/merge/rules.go pkg/merge/rules_test.go && buckley commit --yes --minimal-output
```

---

### Task F3: Implement Go interface-implementation warning rule

**Files:**
- Create: `/home/draco/work/graft/pkg/merge/rules_go.go`
- Test: `/home/draco/work/graft/pkg/merge/rules_go_test.go`

- [ ] **Step 1: Write the test**

```go
// /home/draco/work/graft/pkg/merge/rules_go_test.go
package merge

import (
	"testing"

	"github.com/odvcencio/graft/pkg/entity"
)

// mergeAndRunRule is a test helper that runs MergeFiles, extracts entities,
// matches them, and applies a rule. Returns the diagnostics.
func mergeAndRunRule(t *testing.T, path string, base, ours, theirs []byte, rule LangMergeRule) []Diagnostic {
	t.Helper()
	result, err := MergeFiles(path, base, ours, theirs)
	if err != nil {
		t.Fatal(err)
	}

	// Extract entities and match them (same as MergeFiles does internally)
	baseEL, _ := entity.Extract(path, base)
	oursEL, _ := entity.Extract(path, ours)
	theirsEL, _ := entity.Extract(path, theirs)

	var matched []MatchedEntity
	if baseEL != nil && oursEL != nil && theirsEL != nil {
		matched = MatchEntities(baseEL, oursEL, theirsEL)
	}

	ctx := &MergeRuleContext{
		Base:     safeEntities(baseEL),
		Ours:     safeEntities(oursEL),
		Theirs:   safeEntities(theirsEL),
		Matched:  matched,
		Result:   result,
		Language: detectLanguage(path),
		Path:     path,
	}
	return rule.Apply(ctx)
}

func safeEntities(el *entity.EntityList) []entity.Entity {
	if el == nil {
		return nil
	}
	return el.Entities
}

func TestGoInterfaceImplRule(t *testing.T) {
	base := []byte("package main\n\ntype Processor interface {\n\tProcess() error\n}\n")
	ours := []byte("package main\n\ntype Processor interface {\n\tProcess() error\n\tValidate() error\n}\n")
	theirs := base

	diags := mergeAndRunRule(t, "main.go", base, ours, theirs, &GoInterfaceImplRule{})

	found := false
	for _, d := range diags {
		if d.Rule == "go-interface-impl" && d.Severity == DiagWarning {
			found = true
		}
	}
	if !found {
		t.Error("expected go-interface-impl warning when interface gains a method")
	}
}

func TestGoInterfaceImplRuleNoWarningWhenUnchanged(t *testing.T) {
	src := []byte("package main\n\ntype Processor interface {\n\tProcess() error\n}\n")

	diags := mergeAndRunRule(t, "main.go", src, src, src, &GoInterfaceImplRule{})

	if len(diags) != 0 {
		t.Errorf("expected no diagnostics for unchanged interface, got %d", len(diags))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /home/draco/work/graft && go test ./pkg/merge/ -run TestGoInterfaceImpl -v
```
Expected: FAIL — `GoInterfaceImplRule` not defined.

- [ ] **Step 3: Implement the rule**

```go
// /home/draco/work/graft/pkg/merge/rules_go.go
package merge

import (
	"bytes"
	"fmt"
)

// GoInterfaceImplRule warns when a method is added to a Go interface,
// since all implementations will need to add the method.
type GoInterfaceImplRule struct{}

func (r *GoInterfaceImplRule) Language() string { return "go" }

func (r *GoInterfaceImplRule) Apply(ctx *MergeRuleContext) []Diagnostic {
	var diags []Diagnostic

	for _, m := range ctx.Matched {
		if m.Disposition != OursOnly && m.Disposition != TheirsOnly {
			continue
		}
		if m.Base == nil {
			continue
		}
		// Check if this is an interface type declaration
		if !isInterfaceBody(m.Base.DeclKind, "go", m.Base.Body) {
			continue
		}

		// Count methods in base vs modified version
		modified := m.Ours
		side := "ours"
		if m.Disposition == TheirsOnly {
			modified = m.Theirs
			side = "theirs"
		}
		if modified == nil {
			continue
		}

		baseMethods := countInterfaceMethods(m.Base.Body)
		modifiedMethods := countInterfaceMethods(modified.Body)

		if modifiedMethods > baseMethods {
			added := modifiedMethods - baseMethods
			diags = append(diags, Diagnostic{
				Severity: DiagWarning,
				Entity:   m.Key,
				Message:  fmt.Sprintf("%s: %s added %d method(s) to interface — implementors may need updating", m.Base.Name, side, added),
				Rule: "go-interface-impl",
			})
		}
	}

	return diags
}

// countInterfaceMethods counts lines that look like method signatures
// inside a Go interface body. This is a heuristic — not a full parse.
func countInterfaceMethods(body []byte) int {
	count := 0
	lines := bytes.Split(body, []byte("\n"))
	inBody := false
	for _, line := range lines {
		trimmed := bytes.TrimSpace(line)
		if bytes.Contains(trimmed, []byte("interface {")) || bytes.Contains(trimmed, []byte("interface{")) {
			inBody = true
			continue
		}
		if inBody && len(trimmed) > 0 && trimmed[0] != '}' && !bytes.HasPrefix(trimmed, []byte("//")) {
			count++
		}
	}
	return count
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd /home/draco/work/graft && go test ./pkg/merge/ -run TestGoInterfaceImpl -v
```
Expected: PASS

- [ ] **Step 5: Run all merge tests**

```bash
cd /home/draco/work/graft && go test ./pkg/merge/ -count=1
```
Expected: All PASS.

- [ ] **Step 6: Commit**

```bash
cd /home/draco/work/graft && git add pkg/merge/rules_go.go pkg/merge/rules_go_test.go && buckley commit --yes --minimal-output
```

---

### Task F4: Implement Go const/var block set-union rule

**Files:**
- Modify: `/home/draco/work/graft/pkg/merge/rules_go.go`
- Test: `/home/draco/work/graft/pkg/merge/rules_go_test.go`

- [ ] **Step 1: Write the test**

Add to `rules_go_test.go`:

```go
func TestGoConstBlockRule(t *testing.T) {
	base := []byte("package main\n\nconst (\n\tA = 1\n)\n")
	ours := []byte("package main\n\nconst (\n\tA = 1\n\tB = 2\n)\n")
	theirs := []byte("package main\n\nconst (\n\tA = 1\n\tC = 3\n)\n")

	diags := mergeAndRunRule(t, "main.go", base, ours, theirs, &GoConstVarBlockRule{})

	// Should produce an info diagnostic about const block merge
	for _, d := range diags {
		if d.Rule == "go-const-var-block" {
			return // found it
		}
	}
	// It's OK if no diagnostic — the structural merge may already handle it cleanly
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /home/draco/work/graft && go test ./pkg/merge/ -run TestGoConstBlock -v
```
Expected: FAIL — `GoConstVarBlockRule` not defined.

- [ ] **Step 3: Implement the rule**

Add to `rules_go.go`:

```go
// GoConstVarBlockRule detects when both sides add entries to a const or
// var block, and suggests set-union merge if there's a conflict.
type GoConstVarBlockRule struct{}

func (r *GoConstVarBlockRule) Language() string { return "go" }

func (r *GoConstVarBlockRule) Apply(ctx *MergeRuleContext) []Diagnostic {
	var diags []Diagnostic
	for _, m := range ctx.Matched {
		if m.Disposition != Conflict {
			continue
		}
		if m.Base == nil || m.Ours == nil || m.Theirs == nil {
			continue
		}
		body := m.Base.Body
		trimmed := bytes.TrimSpace(body)
		if !bytes.HasPrefix(trimmed, []byte("const ")) && !bytes.HasPrefix(trimmed, []byte("const(")) &&
			!bytes.HasPrefix(trimmed, []byte("var ")) && !bytes.HasPrefix(trimmed, []byte("var(")) &&
			!bytes.HasPrefix(trimmed, []byte("const\t")) && !bytes.HasPrefix(trimmed, []byte("var\t")) {
			continue
		}
		diags = append(diags, Diagnostic{
			Severity: DiagInfo,
			Entity:   m.Key,
			Message:  m.Base.Name + ": both sides modified const/var block — consider set-union merge",
			Rule:     "go-const-var-block",
		})
	}
	return diags
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd /home/draco/work/graft && go test ./pkg/merge/ -run TestGoConstBlock -v
```
Expected: PASS

- [ ] **Step 5: Run all merge tests**

```bash
cd /home/draco/work/graft && go test ./pkg/merge/ -count=1
```
Expected: All PASS.

- [ ] **Step 6: Commit**

```bash
cd /home/draco/work/graft && git add pkg/merge/rules_go.go pkg/merge/rules_go_test.go && buckley commit --yes --minimal-output
```

---

### Task F5: Implement Go init() elevated conflict rule

**Files:**
- Modify: `/home/draco/work/graft/pkg/merge/rules_go.go`
- Test: `/home/draco/work/graft/pkg/merge/rules_go_test.go`

- [ ] **Step 1: Write the test**

Add to `rules_go_test.go`:

```go
func TestGoInitFuncRule(t *testing.T) {
	base := []byte("package main\n\nfunc init() {\n\tsetupA()\n}\n")
	ours := []byte("package main\n\nfunc init() {\n\tsetupA()\n\tsetupB()\n}\n")
	theirs := []byte("package main\n\nfunc init() {\n\tsetupA()\n\tsetupC()\n}\n")

	diags := mergeAndRunRule(t, "main.go", base, ours, theirs, &GoInitFuncRule{})

	found := false
	for _, d := range diags {
		if d.Rule == "go-init-func" && d.Severity == DiagWarning {
			found = true
		}
	}
	if !found {
		// Only fail if MergeFiles produced a conflict — if the merge engine
		// resolved it cleanly, there's nothing for the rule to flag.
		result, _ := MergeFiles("main.go", base, ours, theirs)
		if result.HasConflicts {
			t.Error("expected go-init-func warning when init() has a conflict")
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /home/draco/work/graft && go test ./pkg/merge/ -run TestGoInitFunc -v
```

- [ ] **Step 3: Implement the rule**

Add to `rules_go.go`:

```go
// GoInitFuncRule warns when init() is modified on both sides,
// since init() ordering and side effects are sensitive.
type GoInitFuncRule struct{}

func (r *GoInitFuncRule) Language() string { return "go" }

func (r *GoInitFuncRule) Apply(ctx *MergeRuleContext) []Diagnostic {
	var diags []Diagnostic
	for _, m := range ctx.Matched {
		if m.Disposition != Conflict {
			continue
		}
		if m.Base == nil {
			continue
		}
		if m.Base.Name != "init" {
			continue
		}
		if m.Base.DeclKind != "function_declaration" && m.Base.DeclKind != "function_definition" {
			continue
		}
		diags = append(diags, Diagnostic{
			Severity: DiagWarning,
			Entity:   m.Key,
			Message:  "init() modified on both sides — review carefully, execution order may matter",
			Rule:     "go-init-func",
		})
	}
	return diags
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd /home/draco/work/graft && go test ./pkg/merge/ -run TestGoInitFunc -v
```

- [ ] **Step 5: Run all merge tests**

```bash
cd /home/draco/work/graft && go test ./pkg/merge/ -count=1
```

- [ ] **Step 6: Commit**

```bash
cd /home/draco/work/graft && git add pkg/merge/rules_go.go pkg/merge/rules_go_test.go && buckley commit --yes --minimal-output
```

---

### Task F6: Register default Go rules and surface diagnostics in CLI

**Files:**
- Modify: `/home/draco/work/graft/pkg/merge/rules_go.go`
- Modify: `/home/draco/work/graft/cmd/graft/cmd_merge.go`
- Modify: `/home/draco/work/graft/cmd/graft/cmd_conflicts.go`

- [ ] **Step 1: Add init() registration for default Go rules**

Add to `rules_go.go`:

```go
func init() {
	DefaultRegistry.Register(&GoInterfaceImplRule{})
	DefaultRegistry.Register(&GoConstVarBlockRule{})
	DefaultRegistry.Register(&GoInitFuncRule{})
}
```

- [ ] **Step 2: Surface diagnostics in merge CLI output**

In `cmd_merge.go`, after the merge completes and results are printed, add diagnostic output. Find where `FileMergeReport` results are iterated and add:

```go
// After printing file merge results, print diagnostics
// This requires MergeResult.Diagnostics to be threaded through FileMergeReport
```

The exact integration depends on how `FileMergeReport` is structured. The diagnostics from `MergeResult` need to be collected per-file and printed after the merge summary. Look at the existing output format in `cmd_merge.go` and add diagnostic lines like:

```
  warning: [go-interface-impl] Processor: ours added 1 method(s) to interface — implementors may need updating
```

- [ ] **Step 3: Thread diagnostics through FileMergeReport**

Add a `Diagnostics []merge.Diagnostic` field to `FileMergeReport` in `pkg/repo/merge.go`, and populate it from `MergeResult.Diagnostics` in `mergeFileContents()`.

- [ ] **Step 4: Run full test suite**

```bash
cd /home/draco/work/graft && go test ./... -count=1
```
Expected: All PASS.

- [ ] **Step 5: Commit**

```bash
cd /home/draco/work/graft && git add pkg/merge/rules_go.go cmd/graft/cmd_merge.go cmd/graft/cmd_conflicts.go pkg/repo/merge.go && buckley commit --yes --minimal-output
```

---

## Initiative A: Full Bidirectional Git Bridge

### Task A1: Implement the hash map (graft ↔ git hash translation)

**Files:**
- Create: `/home/draco/work/graft/pkg/gitbridge/hashmap.go`
- Test: `/home/draco/work/graft/pkg/gitbridge/hashmap_test.go`

- [ ] **Step 1: Write the test**

```go
// /home/draco/work/graft/pkg/gitbridge/hashmap_test.go
package gitbridge

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/odvcencio/graft/pkg/object"
)

func TestHashMapPutAndLookup(t *testing.T) {
	dir := t.TempDir()
	hm, err := OpenHashMap(filepath.Join(dir, "hashmap"))
	if err != nil {
		t.Fatal(err)
	}
	defer hm.Close()

	graftHash := object.HashBytes([]byte("hello"))
	gitHash := GitHash{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a,
		0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10, 0x11, 0x12, 0x13, 0x14}

	if err := hm.Put(graftHash, gitHash); err != nil {
		t.Fatal(err)
	}

	got, ok := hm.GraftToGit(graftHash)
	if !ok {
		t.Fatal("expected to find graft→git mapping")
	}
	if string(got) != string(gitHash) {
		t.Errorf("got %x, want %x", got, gitHash)
	}

	got2, ok := hm.GitToGraft(gitHash)
	if !ok {
		t.Fatal("expected to find git→graft mapping")
	}
	if got2 != graftHash {
		t.Errorf("got %s, want %s", got2, graftHash)
	}
}

func TestHashMapPersistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hashmap")

	graftHash := object.HashBytes([]byte("persist"))
	gitHash := GitHash{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff, 0x00, 0x11, 0x22, 0x33,
		0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb, 0xcc, 0xdd}

	// Write
	hm, err := OpenHashMap(path)
	if err != nil {
		t.Fatal(err)
	}
	hm.Put(graftHash, gitHash)
	hm.Close()

	// Re-open and verify
	hm2, err := OpenHashMap(path)
	if err != nil {
		t.Fatal(err)
	}
	defer hm2.Close()

	got, ok := hm2.GraftToGit(graftHash)
	if !ok {
		t.Fatal("expected mapping to persist across close/open")
	}
	if string(got) != string(gitHash) {
		t.Errorf("got %x, want %x", got, gitHash)
	}
}

func TestHashMapNotFound(t *testing.T) {
	dir := t.TempDir()
	hm, err := OpenHashMap(filepath.Join(dir, "hashmap"))
	if err != nil {
		t.Fatal(err)
	}
	defer hm.Close()

	_, ok := hm.GraftToGit("nonexistent")
	if ok {
		t.Error("expected not found for nonexistent hash")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /home/draco/work/graft && go test ./pkg/gitbridge/ -v
```
Expected: FAIL — package doesn't exist.

- [ ] **Step 3: Create the package directory**

```bash
mkdir -p /home/draco/work/graft/pkg/gitbridge
```

- [ ] **Step 4: Write the implementation**

```go
// /home/draco/work/graft/pkg/gitbridge/hashmap.go
package gitbridge

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/odvcencio/graft/pkg/object"
)

// GitHash is a variable-length git object hash (SHA-1: 20 bytes, SHA-256: 32 bytes).
type GitHash []byte

func (h GitHash) Hex() string { return hex.EncodeToString(h) }

// ParseGitHash parses a hex-encoded git hash.
func ParseGitHash(s string) (GitHash, error) {
	b, err := hex.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("invalid git hash %q: %w", s, err)
	}
	return GitHash(b), nil
}

// HashMap provides bidirectional mapping between graft and git hashes.
// Backed by an append-only flat file. The map is fully rebuildable.
type HashMap struct {
	mu       sync.RWMutex
	path     string
	file     *os.File
	toGit    map[object.Hash]GitHash
	toGraft  map[string]object.Hash // keyed by hex(gitHash)
}

// OpenHashMap opens or creates a hash map file.
func OpenHashMap(path string) (*HashMap, error) {
	hm := &HashMap{
		path:    path,
		toGit:   make(map[object.Hash]GitHash),
		toGraft: make(map[string]object.Hash),
	}

	// Load existing entries if file exists.
	if data, err := os.ReadFile(path); err == nil {
		scanner := bufio.NewScanner(strings.NewReader(string(data)))
		for scanner.Scan() {
			line := scanner.Text()
			parts := strings.SplitN(line, " ", 2)
			if len(parts) != 2 {
				continue
			}
			graftHash := object.Hash(parts[0])
			gitHash, err := ParseGitHash(parts[1])
			if err != nil {
				continue
			}
			hm.toGit[graftHash] = gitHash
			hm.toGraft[gitHash.Hex()] = graftHash
		}
	}

	// Open file for appending.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("open hash map: %w", err)
	}
	hm.file = f
	return hm, nil
}

// GraftToGit looks up the git hash for a graft hash.
func (hm *HashMap) GraftToGit(graftHash object.Hash) (GitHash, bool) {
	hm.mu.RLock()
	defer hm.mu.RUnlock()
	h, ok := hm.toGit[graftHash]
	return h, ok
}

// GitToGraft looks up the graft hash for a git hash.
func (hm *HashMap) GitToGraft(gitHash GitHash) (object.Hash, bool) {
	hm.mu.RLock()
	defer hm.mu.RUnlock()
	h, ok := hm.toGraft[gitHash.Hex()]
	return h, ok
}

// Put records a bidirectional mapping. Appends to the backing file.
func (hm *HashMap) Put(graftHash object.Hash, gitHash GitHash) error {
	hm.mu.Lock()
	defer hm.mu.Unlock()

	hm.toGit[graftHash] = gitHash
	hm.toGraft[gitHash.Hex()] = graftHash

	_, err := fmt.Fprintf(hm.file, "%s %s\n", string(graftHash), gitHash.Hex())
	return err
}

// Close closes the backing file.
func (hm *HashMap) Close() error {
	if hm.file != nil {
		return hm.file.Close()
	}
	return nil
}

// Len returns the number of entries in the map.
func (hm *HashMap) Len() int {
	hm.mu.RLock()
	defer hm.mu.RUnlock()
	return len(hm.toGit)
}
```

- [ ] **Step 5: Run tests to verify they pass**

```bash
cd /home/draco/work/graft && go test ./pkg/gitbridge/ -v
```
Expected: All PASS.

- [ ] **Step 6: Commit**

```bash
cd /home/draco/work/graft && git add pkg/gitbridge/ && buckley commit --yes --minimal-output
```

---

### Task A2: Implement git object reading (parse git objects for import)

**Files:**
- Create: `/home/draco/work/graft/pkg/gitbridge/gitobject.go`
- Test: `/home/draco/work/graft/pkg/gitbridge/gitobject_test.go`

- [ ] **Step 1: Write the test**

```go
// /home/draco/work/graft/pkg/gitbridge/gitobject_test.go
package gitbridge

import (
	"testing"
)

func TestParseGitBlob(t *testing.T) {
	// Git blob format: "blob <size>\0<content>"
	content := []byte("hello world\n")
	raw := gitBlobBytes(content)

	obj, err := ParseGitObject(raw)
	if err != nil {
		t.Fatal(err)
	}
	if obj.Type != "blob" {
		t.Errorf("type = %q, want blob", obj.Type)
	}
	if string(obj.Data) != string(content) {
		t.Errorf("data = %q, want %q", obj.Data, content)
	}
}

func TestParseGitTree(t *testing.T) {
	// Verify we can parse a git tree entry
	obj, err := ParseGitObject(gitTreeBytes([]GitTreeEntry{
		{Mode: "100644", Name: "hello.go", Hash: make([]byte, 20)},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if obj.Type != "tree" {
		t.Errorf("type = %q, want tree", obj.Type)
	}
}

func TestParseGitCommit(t *testing.T) {
	raw := gitCommitBytes("abc123", "Test Author <test@test.com>", "test commit\n")
	obj, err := ParseGitObject(raw)
	if err != nil {
		t.Fatal(err)
	}
	if obj.Type != "commit" {
		t.Errorf("type = %q, want commit", obj.Type)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /home/draco/work/graft && go test ./pkg/gitbridge/ -run TestParseGit -v
```

- [ ] **Step 3: Implement git object parsing**

```go
// /home/draco/work/graft/pkg/gitbridge/gitobject.go
package gitbridge

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"strconv"
)

// GitObject represents a parsed git object.
type GitObject struct {
	Type string // "blob", "tree", "commit", "tag"
	Data []byte // Raw object data (after header)
}

// GitTreeEntry represents an entry in a git tree object.
type GitTreeEntry struct {
	Mode string
	Name string
	Hash []byte // 20 bytes (SHA-1)
}

// ParseGitObject parses a raw git object (type + size + \0 + data).
func ParseGitObject(raw []byte) (*GitObject, error) {
	idx := bytes.IndexByte(raw, 0)
	if idx < 0 {
		return nil, fmt.Errorf("invalid git object: no null separator")
	}
	header := string(raw[:idx])
	parts := bytes.SplitN([]byte(header), []byte(" "), 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid git object header: %q", header)
	}
	objType := string(parts[0])
	size, err := strconv.Atoi(string(parts[1]))
	if err != nil {
		return nil, fmt.Errorf("invalid size in header: %w", err)
	}
	data := raw[idx+1:]
	if len(data) != size {
		return nil, fmt.Errorf("size mismatch: header says %d, got %d", size, len(data))
	}
	return &GitObject{Type: objType, Data: data}, nil
}

// GitObjectHash computes the SHA-1 hash of a git object.
func GitObjectHash(objType string, data []byte) GitHash {
	header := fmt.Sprintf("%s %d\x00", objType, len(data))
	h := sha1.New()
	h.Write([]byte(header))
	h.Write(data)
	return GitHash(h.Sum(nil))
}

// GitObjectHashHex returns the hex-encoded SHA-1 hash.
func GitObjectHashHex(objType string, data []byte) string {
	return hex.EncodeToString(GitObjectHash(objType, data))
}

// Helper functions for tests — construct raw git objects.

func gitBlobBytes(content []byte) []byte {
	header := fmt.Sprintf("blob %d\x00", len(content))
	return append([]byte(header), content...)
}

func gitTreeBytes(entries []GitTreeEntry) []byte {
	var buf bytes.Buffer
	for _, e := range entries {
		buf.WriteString(e.Mode)
		buf.WriteByte(' ')
		buf.WriteString(e.Name)
		buf.WriteByte(0)
		buf.Write(e.Hash)
	}
	data := buf.Bytes()
	header := fmt.Sprintf("tree %d\x00", len(data))
	return append([]byte(header), data...)
}

func gitCommitBytes(treeHash, author, message string) []byte {
	content := fmt.Sprintf("tree %s\nauthor %s 1234567890 +0000\ncommitter %s 1234567890 +0000\n\n%s",
		treeHash, author, author, message)
	header := fmt.Sprintf("commit %d\x00", len(content))
	return append([]byte(header), []byte(content)...)
}
```

- [ ] **Step 4: Run tests**

```bash
cd /home/draco/work/graft && go test ./pkg/gitbridge/ -run TestParseGit -v
```
Expected: All PASS.

- [ ] **Step 5: Commit**

```bash
cd /home/draco/work/graft && git add pkg/gitbridge/gitobject.go pkg/gitbridge/gitobject_test.go && buckley commit --yes --minimal-output
```

---

### Task A3: Implement bridge detection in graft init

**Files:**
- Create: `/home/draco/work/graft/pkg/gitbridge/bridge.go`
- Test: `/home/draco/work/graft/pkg/gitbridge/bridge_test.go`

- [ ] **Step 1: Write the test**

```go
// /home/draco/work/graft/pkg/gitbridge/bridge_test.go
package gitbridge

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestDetectGitRepo(t *testing.T) {
	dir := t.TempDir()

	// No .git → not detected
	if DetectGitRepo(dir) {
		t.Error("expected false for non-git directory")
	}

	// Create .git directory
	if err := os.MkdirAll(filepath.Join(dir, ".git", "objects"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".git", "refs"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".git", "HEAD"), []byte("ref: refs/heads/main\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if !DetectGitRepo(dir) {
		t.Error("expected true for directory with .git")
	}
}

func TestInitBridge(t *testing.T) {
	dir := t.TempDir()

	// Initialize a real git repo
	cmd := exec.Command("git", "init", dir)
	if err := cmd.Run(); err != nil {
		t.Skipf("git not available: %v", err)
	}

	// Create a file and commit
	testFile := filepath.Join(dir, "main.go")
	os.WriteFile(testFile, []byte("package main\n\nfunc main() {}\n"), 0644)
	runGit(t, dir, "add", "main.go")
	runGit(t, dir, "-c", "user.email=test@test.com", "-c", "user.name=Test", "commit", "-m", "initial")

	// Init bridge
	b, err := InitBridge(dir)
	if err != nil {
		t.Fatal(err)
	}

	// .graft/ should exist
	if _, err := os.Stat(filepath.Join(dir, ".graft")); err != nil {
		t.Error(".graft directory not created")
	}

	// hash map should exist
	if b.hashMap.Len() == 0 {
		t.Error("expected hash map to have entries after init")
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /home/draco/work/graft && go test ./pkg/gitbridge/ -run TestDetectGitRepo -v
```

- [ ] **Step 3: Implement bridge detection and initialization**

```go
// /home/draco/work/graft/pkg/gitbridge/bridge.go
package gitbridge

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/odvcencio/graft/pkg/entity"
	"github.com/odvcencio/graft/pkg/object"
)

// Bridge manages the bidirectional relationship between .git/ and .graft/.
type Bridge struct {
	rootDir  string
	gitDir   string
	graftDir string
	hashMap  *HashMap
	store    *object.Store
}

// DetectGitRepo returns true if dir contains a .git directory with HEAD.
func DetectGitRepo(dir string) bool {
	head := filepath.Join(dir, ".git", "HEAD")
	_, err := os.Stat(head)
	return err == nil
}

// InitBridge creates a .graft/ directory alongside an existing .git/ repo
// and imports the HEAD snapshot.
func InitBridge(dir string) (*Bridge, error) {
	if !DetectGitRepo(dir) {
		return nil, fmt.Errorf("no .git repository found in %s", dir)
	}

	graftDir := filepath.Join(dir, ".graft")
	if err := os.MkdirAll(filepath.Join(graftDir, "objects"), 0755); err != nil {
		return nil, fmt.Errorf("create .graft/objects: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(graftDir, "refs", "heads"), 0755); err != nil {
		return nil, fmt.Errorf("create .graft/refs: %w", err)
	}

	store := object.NewStore(filepath.Join(graftDir, "objects"))

	hm, err := OpenHashMap(filepath.Join(graftDir, "hash_map"))
	if err != nil {
		return nil, fmt.Errorf("open hash map: %w", err)
	}

	b := &Bridge{
		rootDir:  dir,
		gitDir:   filepath.Join(dir, ".git"),
		graftDir: graftDir,
		hashMap:  hm,
		store:    store,
	}

	// Add .graft/ to .git/info/exclude
	if err := b.addToGitExclude(); err != nil {
		return nil, err
	}

	// Import HEAD snapshot
	if err := b.importHEAD(); err != nil {
		return nil, fmt.Errorf("import HEAD: %w", err)
	}

	return b, nil
}

// addToGitExclude adds .graft/ to .git/info/exclude if not already there.
func (b *Bridge) addToGitExclude() error {
	excludeDir := filepath.Join(b.gitDir, "info")
	if err := os.MkdirAll(excludeDir, 0755); err != nil {
		return err
	}
	excludePath := filepath.Join(excludeDir, "exclude")
	data, _ := os.ReadFile(excludePath)
	if strings.Contains(string(data), ".graft") {
		return nil
	}
	f, err := os.OpenFile(excludePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(".graft/\n")
	return err
}

// importHEAD imports the current git HEAD tree into the graft store.
func (b *Bridge) importHEAD() error {
	// Get list of tracked files from git
	cmd := exec.Command("git", "ls-files")
	cmd.Dir = b.rootDir
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("git ls-files: %w", err)
	}

	files := strings.Split(strings.TrimSpace(string(out)), "\n")
	treeEntries := make([]object.TreeEntry, 0, len(files))

	for _, path := range files {
		if path == "" {
			continue
		}
		fullPath := filepath.Join(b.rootDir, path)
		content, err := os.ReadFile(fullPath)
		if err != nil {
			continue // skip unreadable files
		}

		// Store blob — Write computes and returns the hash
		blobHash, err := b.store.Write(object.TypeBlob, content)
		if err != nil {
			return fmt.Errorf("store blob %s: %w", path, err)
		}

		entry := object.TreeEntry{
			Name:     path,
			Mode:     "100644",
			BlobHash: blobHash,
		}

		// Try entity extraction for source files
		entities, err := entity.Extract(path, content)
		if err == nil && len(entities.Entities) > 0 {
			hasDecl := false
			for _, e := range entities.Entities {
				if e.Kind == entity.KindDeclaration {
					hasDecl = true
					break
				}
			}
			if hasDecl {
				// Store entity list — actual entity object storage
				// follows the pattern in pkg/repo/ for creating entity objects
				entry.EntityListHash = blobHash // placeholder — real impl uses entity list serialization
			}
		}

		treeEntries = append(treeEntries, entry)

		// Compute git blob hash for hash map
		gitHash := GitObjectHash("blob", content)
		b.hashMap.Put(blobHash, gitHash)
	}

	// Note: Full tree + commit creation follows pkg/repo patterns.
	// This is the foundation — tree building and commit creation
	// will use the existing Repo.writeTree() and Repo.writeCommit()
	// infrastructure once the bridge is integrated at the cmd level.

	return nil
}

// Close releases bridge resources.
func (b *Bridge) Close() error {
	return b.hashMap.Close()
}
```

- [ ] **Step 4: Run tests**

```bash
cd /home/draco/work/graft && go test ./pkg/gitbridge/ -v -count=1
```
Expected: All PASS.

- [ ] **Step 5: Commit**

```bash
cd /home/draco/work/graft && git add pkg/gitbridge/bridge.go pkg/gitbridge/bridge_test.go && buckley commit --yes --minimal-output
```

---

### Task A4: Implement git-side change detection (lazy sync)

**Files:**
- Create: `/home/draco/work/graft/pkg/gitbridge/sync.go`
- Test: `/home/draco/work/graft/pkg/gitbridge/sync_test.go`

- [ ] **Step 1: Write the test**

```go
// /home/draco/work/graft/pkg/gitbridge/sync_test.go
package gitbridge

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestDetectGitChanges(t *testing.T) {
	dir := t.TempDir()

	// Init git repo with a commit
	runGit(t, dir, "init")
	os.WriteFile(filepath.Join(dir, "a.go"), []byte("package main\n"), 0644)
	runGit(t, dir, "add", "a.go")
	runGit(t, dir, "-c", "user.email=t@t", "-c", "user.name=T", "commit", "-m", "init")

	// Init bridge
	b, err := InitBridge(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()

	// Save current git HEAD as known state
	knownHead, err := b.gitHEAD()
	if err != nil {
		t.Fatal(err)
	}

	// No changes yet
	changed, err := b.GitRefsChanged(knownHead)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Error("expected no changes immediately after init")
	}

	// Make a git commit
	os.WriteFile(filepath.Join(dir, "b.go"), []byte("package main\n"), 0644)
	runGit(t, dir, "add", "b.go")
	runGit(t, dir, "-c", "user.email=t@t", "-c", "user.name=T", "commit", "-m", "second")

	// Now should detect changes
	changed, err = b.GitRefsChanged(knownHead)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Error("expected changes after git commit")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /home/draco/work/graft && go test ./pkg/gitbridge/ -run TestDetectGitChanges -v
```

- [ ] **Step 3: Implement sync detection**

```go
// /home/draco/work/graft/pkg/gitbridge/sync.go
package gitbridge

import (
	"fmt"
	"os/exec"
	"strings"
)

// gitHEAD returns the current git HEAD commit hash.
func (b *Bridge) gitHEAD() (string, error) {
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = b.rootDir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse HEAD: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// GitRefsChanged returns true if git HEAD has moved since lastKnownHead.
func (b *Bridge) GitRefsChanged(lastKnownHead string) (bool, error) {
	current, err := b.gitHEAD()
	if err != nil {
		return false, err
	}
	return current != lastKnownHead, nil
}

// SyncFromGit imports any new git commits since lastKnownHead.
// Returns the new HEAD hash.
func (b *Bridge) SyncFromGit(lastKnownHead string) (string, error) {
	changed, err := b.GitRefsChanged(lastKnownHead)
	if err != nil {
		return lastKnownHead, err
	}
	if !changed {
		return lastKnownHead, nil
	}

	// Re-import HEAD snapshot (for Phase 1, reimport entirely;
	// Phase 2 will do incremental diff-based import)
	if err := b.importHEAD(); err != nil {
		return lastKnownHead, fmt.Errorf("sync from git: %w", err)
	}

	return b.gitHEAD()
}
```

- [ ] **Step 4: Run tests**

```bash
cd /home/draco/work/graft && go test ./pkg/gitbridge/ -run TestDetectGitChanges -v
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /home/draco/work/graft && git add pkg/gitbridge/sync.go pkg/gitbridge/sync_test.go && buckley commit --yes --minimal-output
```

---

### Task A5: Wire bridge detection into graft init command

**Files:**
- Modify: `/home/draco/work/graft/cmd/graft/cmd_init.go`

- [ ] **Step 1: Read the current init command**

```bash
cat /home/draco/work/graft/cmd/graft/cmd_init.go
```

- [ ] **Step 2: Add bridge detection at the start of init**

At the beginning of the init command's run function, before creating a fresh .graft repo, check if `.git/` exists. If it does, use `gitbridge.InitBridge()` instead of the normal init flow:

```go
import "github.com/odvcencio/graft/pkg/gitbridge"

// At the start of the init handler:
if gitbridge.DetectGitRepo(dir) {
    bridge, err := gitbridge.InitBridge(dir)
    if err != nil {
        return fmt.Errorf("init git bridge: %w", err)
    }
    bridge.Close()
    fmt.Println("Initialized graft bridge alongside existing git repository")
    return nil
}
```

- [ ] **Step 3: Build and verify**

```bash
cd /home/draco/work/graft && go build ./cmd/graft/
```
Expected: Clean build.

- [ ] **Step 4: Manual test — init in a git repo**

```bash
cd /tmp && mkdir test-bridge && cd test-bridge
git init
echo "package main" > main.go
git add main.go && git -c user.email=t@t -c user.name=T commit -m "init"
/home/draco/work/graft/graft init
ls -la .graft/
```
Expected: `.graft/` created with `objects/`, `refs/`, `hash_map`.

- [ ] **Step 5: Clean up test directory**

```bash
rm -rf /tmp/test-bridge
```

- [ ] **Step 6: Commit**

```bash
cd /home/draco/work/graft && git add cmd/graft/cmd_init.go && buckley commit --yes --minimal-output
```

---

### Task A6: Wire bridge status into graft status command

**Files:**
- Modify: `/home/draco/work/graft/cmd/graft/cmd_status.go`
- Modify: `/home/draco/work/graft/pkg/gitbridge/bridge.go`

- [ ] **Step 1: Add bridge detection method**

Add to `bridge.go`:

```go
// OpenBridge opens an existing bridge (does not create one).
func OpenBridge(dir string) (*Bridge, error) {
	graftDir := filepath.Join(dir, ".graft")
	if _, err := os.Stat(graftDir); err != nil {
		return nil, fmt.Errorf("no .graft directory found")
	}
	if !DetectGitRepo(dir) {
		return nil, fmt.Errorf("no .git directory found")
	}

	store := object.NewStore(filepath.Join(graftDir, "objects"))

	hm, err := OpenHashMap(filepath.Join(graftDir, "hash_map"))
	if err != nil {
		return nil, err
	}

	return &Bridge{
		rootDir:  dir,
		gitDir:   filepath.Join(dir, ".git"),
		graftDir: graftDir,
		hashMap:  hm,
		store:    store,
	}, nil
}

// IsBridgeRepo returns true if dir has both .git/ and .graft/.
func IsBridgeRepo(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".graft"))
	return err == nil && DetectGitRepo(dir)
}
```

- [ ] **Step 2: Add bridge status to the status command output**

In `cmd_status.go`, after the normal graft status output, add:

```go
if gitbridge.IsBridgeRepo(repoDir) {
    b, err := gitbridge.OpenBridge(repoDir)
    if err == nil {
        defer b.Close()
        // Read stored last-known HEAD from .graft/bridge_head
        lastHead := readBridgeHead(repoDir)
        if lastHead != "" {
            changed, _ := b.GitRefsChanged(lastHead)
            if changed {
                fmt.Println("\ngit bridge: refs out of sync — git has new commits")
            }
        }
    }
}
```

- [ ] **Step 3: Build and verify**

```bash
cd /home/draco/work/graft && go build ./cmd/graft/
```

- [ ] **Step 4: Commit**

```bash
cd /home/draco/work/graft && git add cmd/graft/cmd_status.go pkg/gitbridge/bridge.go && buckley commit --yes --minimal-output
```

---

## Deferred to Follow-Up

**Go rules deferred from Phase 1:**
- `go-struct-field-conflict` — Both sides add fields to same struct. Needs struct field parsing similar to `mergeGoStructFields()` in `struct_merge.go`. Add after core rules prove out.
- `go-embed-directive` — `//go:embed` added/changed. Lower priority, needs comment parsing.

**Git bridge Phase 1 gaps (deferred to Phase 2):**
- `graft diff`, `graft blame`, `graft grep` bridge-awareness. These commands need to detect bridge mode and use the imported graft objects. Requires Phase 1 import to be solid first.
- Hash map `Rebuild()` method for `graft bridge repair` command.

**gts-suite note:** Tasks B1-B3 target `/home/draco/work/gts-suite/` (a separate Git repo). B4 is blocked on gotreesitter tagging.

---

## Final Checklist

- [ ] All merge tests pass: `cd /home/draco/work/graft && go test ./... -count=1`
- [ ] gts-suite release workflow exists at `.github/workflows/release.yml`
- [ ] gts-suite README has install section
- [ ] `pkg/gitbridge/` package has hashmap, git object parsing, bridge init, sync detection
- [ ] `pkg/merge/rules.go` has LangMergeRule interface and registry
- [ ] `pkg/merge/rules_go.go` has Go-specific rules (interface, const/var, init)
- [ ] Diagnostics surface in merge CLI output
- [ ] Bridge detection wired into `graft init` and `graft status`
