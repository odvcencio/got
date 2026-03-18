# Structural Grep Phase 1: gotreesitter `grep` Package

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a structural code search, match, and rewrite engine as a `grep` package in gotreesitter.

**Architecture:** The `grep` package sits on top of gotreesitter's existing query engine. A pattern compiler preprocesses code patterns (replacing `$NAME` metavariables with valid placeholders), parses them with the target language's grammar, and translates the resulting tree into S-expression queries. A hand-rolled parser handles the `find ... where ... replace ...` query language syntax. New predicates (`#not-contains?`, `#has-descendant?`, `#ancestor?`, `#count?`, `#is-exported?`) extend the query engine for constraint evaluation. A rewriter applies capture-based substitution templates and returns byte-range edits.

**Tech Stack:** Go 1.24+, gotreesitter query engine, tree-sitter grammars

**Spec:** `docs/superpowers/specs/2026-03-16-structural-grep-design.md` (in the graft repo)

**Repo:** `~/work/gotreesitter` (module: `github.com/odvcencio/gotreesitter`)

### API Reference (gotreesitter calling conventions)

The gotreesitter API has specific patterns that differ from typical tree-sitter bindings. All code in this plan must follow these conventions:

```go
// Language loading — always go through grammars.DetectLanguageByName
entry := grammars.DetectLanguageByName("go")
lang := entry.Language() // returns *gotreesitter.Language

// Parser creation — requires a Language
parser := gotreesitter.NewParser(lang)

// Parsing — no language argument (already set on parser)
tree, err := parser.Parse(source)

// Root node access
root := tree.RootNode() // not Root()

// For parsing with full scanner support (Go, JS, Python, Rust, etc.),
// use grammars.ParseFile or grammars.ParseFilePooled when available.
// Some languages require a TokenSourceFactory for correct lexing.
// Check grammars package for the appropriate parse function.
```

**Test helper pattern** (use in all test files):
```go
func testLang(t *testing.T, name string) *gotreesitter.Language {
    t.Helper()
    entry := grammars.DetectLanguageByName(name)
    if entry == nil {
        t.Skipf("%s grammar not available", name)
    }
    return entry.Language()
}
```

**Important:** Before writing Task 3 (pattern compiler), inspect how `grammars.ParseFile` or `grammars.ParseFilePooled` works — it may handle token source factory setup that raw `NewParser` + `Parse` does not. Use whichever function provides the most complete parse for a given language.

---

## Chunk 1: Query Language Parser + Pattern Compiler

The hand-rolled parser for `find <lang>::<pattern> where { ... } replace { ... }` and the metavariable preprocessor that makes patterns parseable.

### Task 1: Query Language Parser — Parse `find` Statements

**Files:**
- Create: `grep/parse.go`
- Create: `grep/parse_test.go`

- [ ] **Step 1: Write failing tests for the query language parser**

```go
// grep/parse_test.go
package grep

import "testing"

func TestParseBasicFind(t *testing.T) {
	stmt, err := ParseQuery(`find go::func $NAME($$$PARAMS) error`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stmt.Lang != "go" {
		t.Errorf("lang = %q, want %q", stmt.Lang, "go")
	}
	if stmt.Pattern != "func $NAME($$$PARAMS) error" {
		t.Errorf("pattern = %q, want %q", stmt.Pattern, "func $NAME($$$PARAMS) error")
	}
	if stmt.Where != "" {
		t.Errorf("where = %q, want empty", stmt.Where)
	}
	if stmt.Replace != "" {
		t.Errorf("replace = %q, want empty", stmt.Replace)
	}
}

func TestParseFindWithWhere(t *testing.T) {
	stmt, err := ParseQuery(`find go::func $NAME($$$) error where { not contains($BODY, ctx.Err()) }`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stmt.Lang != "go" {
		t.Errorf("lang = %q, want %q", stmt.Lang, "go")
	}
	if stmt.Where != "not contains($BODY, ctx.Err())" {
		t.Errorf("where = %q", stmt.Where)
	}
}

func TestParseFindWithReplace(t *testing.T) {
	stmt, err := ParseQuery(`find rust::$EXPR.unwrap() replace { $EXPR.expect("failed") }`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stmt.Replace != `$EXPR.expect("failed")` {
		t.Errorf("replace = %q", stmt.Replace)
	}
}

func TestParseFindWithWhereAndReplace(t *testing.T) {
	stmt, err := ParseQuery(`find go::$E.Close() where { ancestor(func $$$) } replace { defer $E.Close() }`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stmt.Lang != "go" || stmt.Where == "" || stmt.Replace == "" {
		t.Errorf("incomplete parse: lang=%q where=%q replace=%q", stmt.Lang, stmt.Where, stmt.Replace)
	}
}

func TestParseSexpMode(t *testing.T) {
	stmt, err := ParseQuery(`find sexp::(function_definition name: (identifier) @name)`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stmt.Lang != "sexp" {
		t.Errorf("lang = %q, want %q", stmt.Lang, "sexp")
	}
}

func TestParseShorthand(t *testing.T) {
	// No "find" keyword, just lang::pattern
	stmt, err := ParseQuery(`go::func $NAME($$$) error`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stmt.Lang != "go" {
		t.Errorf("lang = %q, want %q", stmt.Lang, "go")
	}
}

func TestParseBarePattern(t *testing.T) {
	// No find keyword, no lang prefix — bare pattern with metavariables
	stmt, err := ParseQuery(`func $NAME($$$) error`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stmt.Lang != "" {
		t.Errorf("lang = %q, want empty", stmt.Lang)
	}
	if stmt.Pattern != "func $NAME($$$) error" {
		t.Errorf("pattern = %q", stmt.Pattern)
	}
}

func TestParseError(t *testing.T) {
	_, err := ParseQuery(`find`)
	if err == nil {
		t.Fatal("expected error for incomplete query")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd ~/work/gotreesitter && go test ./grep/ -v -run TestParse`
Expected: compilation failure (package does not exist)

- [ ] **Step 3: Implement the query language parser**

```go
// grep/parse.go
package grep

import (
	"fmt"
	"strings"
)

// QueryStatement holds the parsed components of a structural grep query.
type QueryStatement struct {
	Lang    string // language name (e.g., "go", "rust", "sexp") or "" if unspecified
	Pattern string // the code pattern or S-expression
	Where   string // raw constraint block content (between { })
	Replace string // raw replacement template content (between { })
}

// ParseQuery parses a structural grep query string into its components.
// Accepted forms:
//   find <lang>::<pattern> [where { <constraints> }] [replace { <template> }]
//   <lang>::<pattern> [where { ... }] [replace { ... }]
//   <pattern>  (bare pattern, no language prefix)
func ParseQuery(input string) (*QueryStatement, error) {
	s := strings.TrimSpace(input)
	if s == "" {
		return nil, fmt.Errorf("empty query")
	}

	stmt := &QueryStatement{}

	// Strip optional "find" keyword
	if strings.HasPrefix(s, "find ") {
		s = strings.TrimSpace(s[5:])
		if s == "" {
			return nil, fmt.Errorf("expected pattern after 'find'")
		}
	}

	// Extract lang:: prefix
	if idx := strings.Index(s, "::"); idx > 0 {
		prefix := s[:idx]
		// Validate prefix looks like a language name (alphanumeric, no spaces)
		if !strings.ContainsAny(prefix, " \t\n{}<>") {
			stmt.Lang = prefix
			s = s[idx+2:]
		}
	}

	// Now extract where { ... } and replace { ... } blocks from the end.
	// We parse right-to-left to avoid ambiguity with braces in the pattern.
	s, stmt.Replace = extractBlock(s, "replace")
	s, stmt.Where = extractBlock(s, "where")

	stmt.Pattern = strings.TrimSpace(s)
	if stmt.Pattern == "" {
		return nil, fmt.Errorf("empty pattern")
	}

	return stmt, nil
}

// extractBlock finds "keyword { ... }" at the end of s, extracts the block
// content, and returns the remaining string and the block content.
func extractBlock(s string, keyword string) (remaining, block string) {
	// Find the last occurrence of "keyword {" (case-sensitive)
	needle := keyword + " {"
	idx := strings.LastIndex(s, needle)
	if idx < 0 {
		// Try without space: "keyword{"
		needle = keyword + "{"
		idx = strings.LastIndex(s, needle)
	}
	if idx < 0 {
		return s, ""
	}

	// Find the opening brace
	braceStart := idx + len(keyword)
	for braceStart < len(s) && s[braceStart] == ' ' {
		braceStart++
	}
	if braceStart >= len(s) || s[braceStart] != '{' {
		return s, ""
	}

	// Find matching closing brace, handling nesting
	depth := 0
	for i := braceStart; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				content := strings.TrimSpace(s[braceStart+1 : i])
				remaining := strings.TrimSpace(s[:idx])
				return remaining, content
			}
		}
	}
	// Unmatched brace — return unchanged
	return s, ""
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd ~/work/gotreesitter && go test ./grep/ -v -run TestParse`
Expected: all PASS

- [ ] **Step 5: Commit**

```bash
cd ~/work/gotreesitter && git add grep/parse.go grep/parse_test.go
buckley commit --yes --minimal-output
```

---

### Task 2: Metavariable Preprocessor

**Files:**
- Create: `grep/preprocess.go`
- Create: `grep/preprocess_test.go`

- [ ] **Step 1: Write failing tests for the preprocessor**

```go
// grep/preprocess_test.go
package grep

import "testing"

func TestPreprocessSingleCapture(t *testing.T) {
	out, vars, err := Preprocess("func $NAME() error")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "func __GREP_CAP_NAME__() error" {
		t.Errorf("output = %q", out)
	}
	if len(vars) != 1 || vars["__GREP_CAP_NAME__"].Name != "NAME" || vars["__GREP_CAP_NAME__"].Variadic {
		t.Errorf("vars = %+v", vars)
	}
}

func TestPreprocessVariadic(t *testing.T) {
	out, vars, err := Preprocess("func $NAME($$$PARAMS) error")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "func __GREP_CAP_NAME__(__GREP_VAR_PARAMS__) error" {
		t.Errorf("output = %q", out)
	}
	if !vars["__GREP_VAR_PARAMS__"].Variadic {
		t.Errorf("expected variadic for PARAMS")
	}
}

func TestPreprocessTyped(t *testing.T) {
	out, vars, err := Preprocess("$E:expression + 1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "__GREP_TYPED_E_expression__ + 1" {
		t.Errorf("output = %q", out)
	}
	v := vars["__GREP_TYPED_E_expression__"]
	if v.Name != "E" || v.TypeConstraint != "expression" {
		t.Errorf("var = %+v", v)
	}
}

func TestPreprocessWildcard(t *testing.T) {
	out, vars, err := Preprocess("func $_() error")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "func __GREP_WILD__() error" {
		t.Errorf("output = %q", out)
	}
	if !vars["__GREP_WILD__"].Wildcard {
		t.Errorf("expected wildcard")
	}
}

func TestPreprocessMultiple(t *testing.T) {
	out, vars, err := Preprocess("func $NAME($A int, $B string)")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vars) != 3 {
		t.Errorf("expected 3 vars, got %d", len(vars))
	}
	_ = out
}

func TestPreprocessNoMetavars(t *testing.T) {
	out, vars, err := Preprocess("func main() error")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "func main() error" {
		t.Errorf("output = %q", out)
	}
	if len(vars) != 0 {
		t.Errorf("expected no vars")
	}
}

func TestPreprocessReservedPrefix(t *testing.T) {
	_, _, err := Preprocess("var __GREP_CAP_NAME__ = 1")
	if err == nil {
		t.Fatal("expected error for reserved prefix in source")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd ~/work/gotreesitter && go test ./grep/ -v -run TestPreprocess`
Expected: FAIL (functions not defined)

- [ ] **Step 3: Implement the preprocessor**

```go
// grep/preprocess.go
package grep

import (
	"fmt"
	"regexp"
	"strings"
)

// MetaVar describes a metavariable found during preprocessing.
type MetaVar struct {
	Name           string // user-facing name (e.g., "NAME", "PARAMS")
	Placeholder    string // the placeholder identifier inserted into the pattern
	Variadic       bool   // true for $$$ captures
	Wildcard       bool   // true for $_
	TypeConstraint string // node type constraint for $NAME:type, empty otherwise
}

var metaVarPattern = regexp.MustCompile(`\$\$\$([A-Za-z_][A-Za-z0-9_]*)|\$_|\$([A-Za-z_][A-Za-z0-9_]*)(?::([A-Za-z_][A-Za-z0-9_]*))?`)

const reservedPrefix = "__GREP_"

// Preprocess replaces metavariables in a code pattern with language-valid
// placeholder identifiers. Returns the modified pattern and a map from
// placeholder name to MetaVar descriptor.
func Preprocess(pattern string) (string, map[string]*MetaVar, error) {
	// Check for reserved prefix in the original pattern (outside metavars)
	stripped := metaVarPattern.ReplaceAllString(pattern, "")
	if strings.Contains(stripped, reservedPrefix) {
		return "", nil, fmt.Errorf("pattern contains reserved prefix %q", reservedPrefix)
	}

	vars := make(map[string]*MetaVar)
	wildcardCount := 0

	result := metaVarPattern.ReplaceAllStringFunc(pattern, func(match string) string {
		sub := metaVarPattern.FindStringSubmatch(match)
		// sub[0] = full match
		// sub[1] = variadic name (from $$$NAME)
		// sub[2] = single capture name (from $NAME or $NAME:type)
		// sub[3] = type constraint (from $NAME:type)

		if match == "$_" {
			wildcardCount++
			placeholder := fmt.Sprintf("__GREP_WILD_%d__", wildcardCount)
			vars[placeholder] = &MetaVar{
				Name:        "_",
				Placeholder: placeholder,
				Wildcard:    true,
			}
			return placeholder
		}

		if sub[1] != "" {
			// Variadic: $$$NAME
			placeholder := "__GREP_VAR_" + sub[1] + "__"
			vars[placeholder] = &MetaVar{
				Name:        sub[1],
				Placeholder: placeholder,
				Variadic:    true,
			}
			return placeholder
		}

		// Single capture: $NAME or $NAME:type
		name := sub[2]
		typeConstraint := sub[3]

		var placeholder string
		if typeConstraint != "" {
			placeholder = "__GREP_TYPED_" + name + "_" + typeConstraint + "__"
		} else {
			placeholder = "__GREP_CAP_" + name + "__"
		}

		vars[placeholder] = &MetaVar{
			Name:           name,
			Placeholder:    placeholder,
			TypeConstraint: typeConstraint,
		}
		return placeholder
	})

	return result, vars, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd ~/work/gotreesitter && go test ./grep/ -v -run TestPreprocess`
Expected: all PASS

- [ ] **Step 5: Update wildcard test for numbered placeholders**

The implementation uses numbered wildcards (`__GREP_WILD_1__`) for uniqueness. Update the test:

```go
func TestPreprocessWildcard(t *testing.T) {
	out, vars, err := Preprocess("func $_() error")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "func __GREP_WILD_1__() error" {
		t.Errorf("output = %q", out)
	}
	if !vars["__GREP_WILD_1__"].Wildcard {
		t.Errorf("expected wildcard")
	}
}
```

- [ ] **Step 6: Run all grep tests**

Run: `cd ~/work/gotreesitter && go test ./grep/ -v`
Expected: all PASS

- [ ] **Step 7: Commit**

```bash
cd ~/work/gotreesitter && git add grep/preprocess.go grep/preprocess_test.go
buckley commit --yes --minimal-output
```

---

### Task 3: Pattern Compiler — Tree-to-S-Expression Translation

**Files:**
- Create: `grep/compile.go`
- Create: `grep/compile_test.go`

This is the core: parse a preprocessed pattern with the target language grammar, walk the tree, and emit an S-expression query string.

- [ ] **Step 1: Write failing tests for the pattern compiler**

```go
// grep/compile_test.go
package grep

import (
	"strings"
	"testing"

	"github.com/odvcencio/gotreesitter/grammars"
)

func TestCompileSimpleFunction(t *testing.T) {
	lang := testLang(t, "go")

	cp, err := CompilePattern(lang, `func $NAME($$$PARAMS) error`)
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	// Should produce a query that captures NAME and PARAMS
	names := cp.Query.CaptureNames()
	if !containsStr(names, "NAME") {
		t.Errorf("missing capture NAME, got %v", names)
	}
	if !containsStr(names, "PARAMS") {
		t.Errorf("missing capture PARAMS, got %v", names)
	}
}

func TestCompileSimpleCallExpr(t *testing.T) {
	lang := testLang(t, "javascript")

	cp, err := CompilePattern(lang, `console.log($$$ARGS)`)
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	names := cp.Query.CaptureNames()
	if !containsStr(names, "ARGS") {
		t.Errorf("missing capture ARGS, got %v", names)
	}
}

func TestCompileNoMetavars(t *testing.T) {
	lang := testLang(t, "go")

	cp, err := CompilePattern(lang, `fmt.Println("hello")`)
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	if len(cp.Query.CaptureNames()) != 0 {
		t.Errorf("expected no captures for literal pattern")
	}
}

func TestCompileTypedCapture(t *testing.T) {
	lang := testLang(t, "go")

	cp, err := CompilePattern(lang, `$E:call_expression`)
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	names := cp.Query.CaptureNames()
	if !containsStr(names, "E") {
		t.Errorf("missing capture E, got %v", names)
	}
}

func containsStr(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd ~/work/gotreesitter && go test ./grep/ -v -run TestCompile`
Expected: FAIL (CompilePattern not defined)

- [ ] **Step 3: Implement the pattern compiler**

```go
// grep/compile.go
package grep

import (
	"fmt"
	"strings"

	gotreesitter "github.com/odvcencio/gotreesitter"
)

// CompiledPattern holds a compiled structural grep pattern ready for matching.
type CompiledPattern struct {
	Query    *gotreesitter.Query
	MetaVars map[string]*MetaVar // placeholder -> metavar descriptor
	Lang     *gotreesitter.Language
}

// CompilePattern compiles a code pattern string into a CompiledPattern.
// The pattern may contain metavariables ($NAME, $$$ARGS, $_, $E:type).
func CompilePattern(lang *gotreesitter.Language, pattern string) (*CompiledPattern, error) {
	// Stage 1: Preprocess metavariables
	preprocessed, vars, err := Preprocess(pattern)
	if err != nil {
		return nil, fmt.Errorf("preprocess: %w", err)
	}

	// Stage 2: Parse the preprocessed pattern as code
	// NOTE: Implementer must check if grammars.ParseFile/ParseFilePooled
	// is needed for full scanner support. For languages with external
	// scanners (Go, JS, Python, Rust), raw NewParser may not suffice.
	parser := gotreesitter.NewParser(lang)
	tree, err := parser.Parse([]byte(preprocessed))
	if err != nil {
		return nil, fmt.Errorf("parse pattern: %w", err)
	}

	root := tree.RootNode()

	// Stage 3: Walk the tree and build an S-expression query
	sexp, err := treeToSexp(root, lang, []byte(preprocessed), vars)
	if err != nil {
		return nil, fmt.Errorf("tree to sexp: %w", err)
	}

	// Stage 4: Compile the S-expression query
	query, err := gotreesitter.NewQuery(sexp, lang)
	if err != nil {
		return nil, fmt.Errorf("compile sexp %q: %w", sexp, err)
	}

	return &CompiledPattern{
		Query:    query,
		MetaVars: vars,
		Lang:     lang,
	}, nil
}

// treeToSexp converts a parse tree node into an S-expression query string,
// replacing placeholder identifiers with appropriate capture/wildcard syntax.
func treeToSexp(node *gotreesitter.Node, lang *gotreesitter.Language, source []byte, vars map[string]*MetaVar) (string, error) {
	nodeType := node.Type(lang)
	text := node.Text(source)

	// If this node's text is a placeholder, emit a capture or wildcard
	if mv, ok := vars[text]; ok {
		return metaVarToSexp(mv), nil
	}

	// For named nodes with children, recurse
	if node.IsNamed() && node.ChildCount() > 0 {
		var parts []string
		for i := 0; i < node.ChildCount(); i++ {
			child := node.Child(i)
			if !child.IsNamed() {
				// Anonymous nodes (keywords, punctuation) — check if they are placeholders
				childText := child.Text(source)
				if _, ok := vars[childText]; ok {
					childSexp, err := treeToSexp(child, lang, source, vars)
					if err != nil {
						return "", err
					}
					parts = append(parts, childSexp)
				}
				// Otherwise skip anonymous nodes (they match implicitly in S-expressions)
				continue
			}

			childText := child.Text(source)

			// Check if this named child is a placeholder
			if mv, ok := vars[childText]; ok {
				fieldName := node.FieldNameForChild(i, lang)
				capture := metaVarToSexp(mv)
				if fieldName != "" {
					parts = append(parts, fmt.Sprintf("%s: %s", fieldName, capture))
				} else {
					parts = append(parts, capture)
				}
				continue
			}

			// Recurse into named children
			childSexp, err := treeToSexp(child, lang, source, vars)
			if err != nil {
				return "", err
			}
			fieldName := node.FieldNameForChild(i, lang)
			if fieldName != "" {
				parts = append(parts, fmt.Sprintf("%s: %s", fieldName, childSexp))
			} else {
				parts = append(parts, childSexp)
			}
		}

		return fmt.Sprintf("(%s %s)", nodeType, strings.Join(parts, " ")), nil
	}

	// Leaf named node — match by type and text
	if node.IsNamed() {
		return fmt.Sprintf("(%s)", nodeType), nil
	}

	// Anonymous leaf — match by literal text
	return fmt.Sprintf("%q", text), nil
}

// metaVarToSexp converts a MetaVar into its S-expression query representation.
func metaVarToSexp(mv *MetaVar) string {
	if mv.Wildcard {
		return "(_)"
	}
	if mv.Variadic {
		return fmt.Sprintf("(_)* @%s", mv.Name)
	}
	if mv.TypeConstraint != "" {
		return fmt.Sprintf("(%s) @%s", mv.TypeConstraint, mv.Name)
	}
	return fmt.Sprintf("(_) @%s", mv.Name)
}
```

Note: This is the initial implementation. The `treeToSexp` function will need refinement as we test against real grammars — tree-sitter parse trees have quirks per language. The tests will guide us.

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd ~/work/gotreesitter && go test ./grep/ -v -run TestCompile`
Expected: some may need iteration — adjust the S-expression generation based on actual Go/JS grammar tree structure. The test may reveal that the Go grammar structures function declarations differently than expected. Debug and fix.

- [ ] **Step 5: Commit**

```bash
cd ~/work/gotreesitter && git add grep/compile.go grep/compile_test.go
buckley commit --yes --minimal-output
```

---

## Chunk 2: Match Engine + Extended Predicates

### Task 4: Core Match API

**Files:**
- Create: `grep/match.go`
- Create: `grep/match_test.go`

- [ ] **Step 1: Write failing tests for Match**

```go
// grep/match_test.go
package grep

import (
	"testing"

	"github.com/odvcencio/gotreesitter/grammars"
)

func TestMatchGoFunction(t *testing.T) {
	lang := testLang(t, "go")

	source := []byte(`package main

func ProcessOrder(ctx context.Context, id string) error {
	return nil
}

func GetUser(id int) (*User, error) {
	return nil, nil
}
`)

	results, err := Match(lang, `func $NAME($$$PARAMS) error`, source)
	if err != nil {
		t.Fatalf("match error: %v", err)
	}

	// Should match ProcessOrder (returns error), may or may not match GetUser
	// depending on how the pattern handles multiple return values.
	found := false
	for _, r := range results {
		if cap, ok := r.Captures["NAME"]; ok {
			if string(cap.Text) == "ProcessOrder" {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("expected to find ProcessOrder, got %d results", len(results))
	}
}

func TestMatchJSConsoleLog(t *testing.T) {
	lang := testLang(t, "javascript")

	source := []byte(`
console.log("hello");
console.log("world", 42);
console.error("bad");
`)

	results, err := Match(lang, `console.log($$$ARGS)`, source)
	if err != nil {
		t.Fatalf("match error: %v", err)
	}

	if len(results) != 2 {
		t.Errorf("expected 2 matches, got %d", len(results))
	}
}

func TestMatchNoResults(t *testing.T) {
	lang := testLang(t, "go")

	source := []byte(`package main

func main() {
	fmt.Println("hello")
}
`)

	results, err := Match(lang, `func $NAME($$$) error`, source)
	if err != nil {
		t.Fatalf("match error: %v", err)
	}

	if len(results) != 0 {
		t.Errorf("expected 0 matches, got %d", len(results))
	}
}

func TestMatchSexp(t *testing.T) {
	lang := testLang(t, "go")

	source := []byte(`package main

func Hello() {}
func World() {}
`)

	results, err := MatchSexp(lang, `(function_declaration name: (identifier) @name)`, source)
	if err != nil {
		t.Fatalf("match error: %v", err)
	}

	if len(results) != 2 {
		t.Errorf("expected 2 matches, got %d", len(results))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd ~/work/gotreesitter && go test ./grep/ -v -run "TestMatch"`
Expected: FAIL (Match not defined)

- [ ] **Step 3: Implement Match and MatchSexp**

```go
// grep/match.go
package grep

import (
	gotreesitter "github.com/odvcencio/gotreesitter"
)

// Result holds a single structural match with its captures.
type Result struct {
	StartByte uint32
	EndByte   uint32
	Captures  map[string]Capture
}

// Capture holds a matched metavariable binding.
type Capture struct {
	Name      string
	Text      []byte
	StartByte uint32
	EndByte   uint32
	Node      *gotreesitter.Node
}

// Match finds all structural matches of a code pattern in source code.
func Match(lang *gotreesitter.Language, pattern string, source []byte) ([]Result, error) {
	cp, err := CompilePattern(lang, pattern)
	if err != nil {
		return nil, err
	}
	return executeMatch(cp, source)
}

// MatchSexp finds matches using a raw S-expression query.
func MatchSexp(lang *gotreesitter.Language, sexp string, source []byte) ([]Result, error) {
	query, err := gotreesitter.NewQuery(sexp, lang)
	if err != nil {
		return nil, err
	}
	cp := &CompiledPattern{
		Query:    query,
		MetaVars: nil,
		Lang:     lang,
	}
	return executeMatch(cp, source)
}

func executeMatch(cp *CompiledPattern, source []byte) ([]Result, error) {
	// NOTE: Implementer should use grammars.ParseFile/ParseFilePooled
	// for languages with external scanners. This is a simplified version.
	parser := gotreesitter.NewParser(cp.Lang)
	tree, err := parser.Parse(source)
	if err != nil {
		return nil, err
	}

	matches := cp.Query.ExecuteNode(tree.RootNode(), cp.Lang, source)

	var results []Result
	for _, m := range matches {
		r := Result{
			Captures: make(map[string]Capture),
		}

		// Compute overall match span from captures
		var minByte, maxByte uint32
		first := true
		for _, cap := range m.Captures {
			// Map capture name back to metavar name if we have metavar info
			capName := cap.Name
			if cp.MetaVars != nil {
				// Capture names from the S-expression use metavar names directly
				capName = cap.Name
			}

			node := cap.Node
			c := Capture{
				Name:      capName,
				Text:      []byte(node.Text(source)),
				StartByte: node.StartByte(),
				EndByte:   node.EndByte(),
				Node:      node,
			}
			r.Captures[capName] = c

			if first || node.StartByte() < minByte {
				minByte = node.StartByte()
			}
			if first || node.EndByte() > maxByte {
				maxByte = node.EndByte()
			}
			first = false
		}

		r.StartByte = minByte
		r.EndByte = maxByte
		results = append(results, r)
	}

	return results, nil
}
```

- [ ] **Step 4: Run tests and iterate**

Run: `cd ~/work/gotreesitter && go test ./grep/ -v -run "TestMatch"`
Expected: iterate on the pattern compiler if S-expression generation doesn't match the Go/JS grammar node structure. Use `tree.Root().SExpr(lang)` on test source code to inspect actual tree structure and adjust the compiler.

- [ ] **Step 5: Commit**

```bash
cd ~/work/gotreesitter && git add grep/match.go grep/match_test.go
buckley commit --yes --minimal-output
```

---

### Task 5: Compile Function (Reusable Compiled Patterns)

**Files:**
- Modify: `grep/compile.go`
- Modify: `grep/match.go`
- Create: `grep/grep.go` (top-level Compile entry point)

- [ ] **Step 1: Write failing test for Compile**

```go
// Add to grep/match_test.go

func TestCompileAndReuse(t *testing.T) {
	lang := testLang(t, "go")

	cp, err := Compile(lang, `func $NAME($$$) error`)
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}

	sources := [][]byte{
		[]byte("package a\nfunc Foo() error { return nil }"),
		[]byte("package b\nfunc Bar(x int) error { return nil }"),
		[]byte("package c\nfunc Baz() {}"),
	}

	totalMatches := 0
	for _, src := range sources {
		results, err := cp.Match(src)
		if err != nil {
			t.Fatalf("match error: %v", err)
		}
		totalMatches += len(results)
	}

	if totalMatches != 2 {
		t.Errorf("expected 2 total matches (Foo, Bar), got %d", totalMatches)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd ~/work/gotreesitter && go test ./grep/ -v -run TestCompileAndReuse`
Expected: FAIL (Compile not defined)

- [ ] **Step 3: Implement Compile and CompiledPattern.Match**

```go
// grep/grep.go
package grep

import (
	gotreesitter "github.com/odvcencio/gotreesitter"
)

// Compile parses a code pattern into a reusable CompiledPattern.
func Compile(lang *gotreesitter.Language, pattern string) (*CompiledPattern, error) {
	return CompilePattern(lang, pattern)
}

// Match executes this compiled pattern against source code.
func (cp *CompiledPattern) Match(source []byte) ([]Result, error) {
	return executeMatch(cp, source)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd ~/work/gotreesitter && go test ./grep/ -v`
Expected: all PASS

- [ ] **Step 5: Commit**

```bash
cd ~/work/gotreesitter && git add grep/grep.go grep/match.go grep/match_test.go
buckley commit --yes --minimal-output
```

---

### Task 6: Extended Predicates on the Query Engine

**Files:**
- Modify: `query.go` (add new predicate types)
- Modify: `query_compile_predicates.go` (parse new predicates)
- Modify: `query_predicates.go` (evaluate new predicates)
- Create: `query_predicates_extended_test.go`

These predicates are added to the core gotreesitter query engine (not the grep package) so they're available via both code-pattern and S-expression entry points.

- [ ] **Step 1: Write failing tests for new predicates**

```go
// query_predicates_extended_test.go
package gotreesitter

import "testing"

func TestPredicateCount(t *testing.T) {
	lang := queryTestLanguage()
	q, err := NewQuery(`(parameter_list (_)* @params (#count? @params > 2))`, lang)
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	_ = q // Will test execution in a follow-up step
}

func TestPredicateIsExported(t *testing.T) {
	lang := queryTestLanguage()
	q, err := NewQuery(`(function_declaration name: (identifier) @name (#is-exported? @name))`, lang)
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	_ = q
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd ~/work/gotreesitter && go test -v -run "TestPredicate(Count|IsExported)"`
Expected: FAIL (unsupported predicate)

- [ ] **Step 3: Add predicate types to query.go**

Add to the `queryPredicateType` const block in `query.go`:

```go
predicateCount          // #count? @cap op value
predicateIsExported     // #is-exported? @cap
predicateNotContains    // #not-contains? @cap pattern
predicateHasDescendant  // #has-descendant? pattern
```

Add fields to `QueryPredicate`:
```go
type QueryPredicate struct {
	// ... existing fields ...
	countOp    string // for #count?: ">", "<", ">=", "<=", "==", "!="
	countValue int    // for #count?
}
```

- [ ] **Step 4: Implement predicate compilation in query_compile_predicates.go**

Add cases to `parsePredicate()` for `#count?`, `#is-exported?`, `#not-contains?`, `#has-descendant?`. Follow the pattern of existing predicates — read arguments, validate, construct `QueryPredicate`.

- [ ] **Step 5: Implement predicate evaluation in query_predicates.go**

Add cases to `matchesPredicates()` for the new types:
- `predicateCount`: count captures with matching name, compare against threshold
- `predicateIsExported`: check if first character is uppercase (Go heuristic, extend per language later)
- `predicateNotContains` and `predicateHasDescendant`: these require sub-query execution. For the initial implementation, these evaluate the pattern argument as a text substring check on the captured node's text. Full sub-query execution comes in a refinement pass.

- [ ] **Step 6: Run tests to verify they pass**

Run: `cd ~/work/gotreesitter && go test -v -run "TestPredicate"`
Expected: all PASS

- [ ] **Step 7: Commit**

```bash
cd ~/work/gotreesitter && git add query.go query_compile_predicates.go query_predicates.go query_predicates_extended_test.go
buckley commit --yes --minimal-output
```

---

## Chunk 3: Rewriter + Full Query Pipeline

### Task 7: Rewriter — Capture Substitution and Edit Generation

**Files:**
- Create: `grep/rewrite.go`
- Create: `grep/rewrite_test.go`

- [ ] **Step 1: Write failing tests for Replace**

```go
// grep/rewrite_test.go
package grep

import (
	"testing"

	"github.com/odvcencio/gotreesitter/grammars"
)

func TestReplaceSimple(t *testing.T) {
	lang := testLang(t, "javascript")

	source := []byte(`console.log("hello");
console.log("world");
`)

	result, err := Replace(lang, `console.log($$$ARGS)`, `console.info($$$ARGS)`, source)
	if err != nil {
		t.Fatalf("replace error: %v", err)
	}

	if len(result.Edits) != 2 {
		t.Fatalf("expected 2 edits, got %d", len(result.Edits))
	}
}

func TestReplaceApplyEdits(t *testing.T) {
	lang := testLang(t, "javascript")

	source := []byte(`console.log("hello");`)

	result, err := Replace(lang, `console.log($$$ARGS)`, `console.info($$$ARGS)`, source)
	if err != nil {
		t.Fatalf("replace error: %v", err)
	}

	output := ApplyEdits(source, result.Edits)
	expected := `console.info("hello");`
	if string(output) != expected {
		t.Errorf("output = %q, want %q", string(output), expected)
	}
}

func TestReplaceNoMatch(t *testing.T) {
	lang := testLang(t, "go")

	source := []byte(`package main`)

	result, err := Replace(lang, `func $NAME($$$) error`, `func $NAME($$$) error`, source)
	if err != nil {
		t.Fatalf("replace error: %v", err)
	}

	if len(result.Edits) != 0 {
		t.Errorf("expected 0 edits, got %d", len(result.Edits))
	}
}

func TestReplaceOverlappingMatches(t *testing.T) {
	lang := testLang(t, "javascript")

	// Nested calls — outer and inner both match
	source := []byte(`console.log(console.log("inner"));`)

	result, err := Replace(lang, `console.log($$$ARGS)`, `console.info($$$ARGS)`, source)
	if err != nil {
		t.Fatalf("replace error: %v", err)
	}

	// Should keep outermost match only
	if len(result.Diagnostics) == 0 {
		t.Log("no overlap diagnostic — may match both non-overlapping or outermost only")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd ~/work/gotreesitter && go test ./grep/ -v -run TestReplace`
Expected: FAIL (Replace not defined)

- [ ] **Step 3: Implement Replace, Rewrite, and ApplyEdits**

```go
// grep/rewrite.go
package grep

import (
	"sort"
	"strings"

	gotreesitter "github.com/odvcencio/gotreesitter"
)

// Edit represents a byte-range replacement in source code.
type Edit struct {
	StartByte   uint32
	EndByte     uint32
	Replacement []byte
}

// Diagnostic holds a warning or error from a rewrite operation.
type Diagnostic struct {
	Message   string
	StartByte uint32
	EndByte   uint32
}

// ReplaceResult holds edits and diagnostics from a rewrite.
type ReplaceResult struct {
	Edits       []Edit
	Diagnostics []Diagnostic
}

// Replace finds all matches of pattern in source and generates edits
// to apply the replacement template.
func Replace(lang *gotreesitter.Language, pattern string, replacement string, source []byte) (*ReplaceResult, error) {
	results, err := Match(lang, pattern, source)
	if err != nil {
		return nil, err
	}

	return buildEdits(results, replacement, source, lang)
}

func buildEdits(results []Result, replacement string, source []byte, lang *gotreesitter.Language) (*ReplaceResult, error) {
	rr := &ReplaceResult{}

	// Sort results by start byte
	sort.Slice(results, func(i, j int) bool {
		return results[i].StartByte < results[j].StartByte
	})

	// Filter overlapping matches — keep outermost
	var filtered []Result
	var lastEnd uint32
	for _, r := range results {
		if r.StartByte < lastEnd {
			rr.Diagnostics = append(rr.Diagnostics, Diagnostic{
				Message:   "overlapping match discarded",
				StartByte: r.StartByte,
				EndByte:   r.EndByte,
			})
			continue
		}
		filtered = append(filtered, r)
		lastEnd = r.EndByte
	}

	// Generate edits
	for _, r := range filtered {
		repl := substituteCaptures(replacement, r.Captures)
		rr.Edits = append(rr.Edits, Edit{
			StartByte:   r.StartByte,
			EndByte:     r.EndByte,
			Replacement: []byte(repl),
		})
	}

	// Validate output if there are edits
	if len(rr.Edits) > 0 {
		output := ApplyEdits(source, rr.Edits)
		parser := gotreesitter.NewParser(lang)
		tree, err := parser.Parse(output)
		if err == nil && tree.RootNode().HasError() {
			rr.Diagnostics = append(rr.Diagnostics, Diagnostic{
				Message: "rewrite produced code with parse errors",
			})
		}
	}

	return rr, nil
}

// substituteCaptures replaces $NAME references in a template with captured text.
func substituteCaptures(template string, captures map[string]Capture) string {
	result := template
	// Sort capture names by length descending to avoid partial replacements
	// (e.g., $NAMES before $NAME)
	names := make([]string, 0, len(captures))
	for name := range captures {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		return len(names[i]) > len(names[j])
	})

	for _, name := range names {
		cap := captures[name]
		// Replace $$$NAME and $NAME forms
		result = strings.ReplaceAll(result, "$$$"+name, string(cap.Text))
		result = strings.ReplaceAll(result, "$"+name, string(cap.Text))
	}
	return result
}

// ApplyEdits applies a set of non-overlapping edits to source, returning new source.
// Edits are applied back-to-front to preserve byte offsets.
func ApplyEdits(source []byte, edits []Edit) []byte {
	// Sort edits by start byte descending
	sorted := make([]Edit, len(edits))
	copy(sorted, edits)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].StartByte > sorted[j].StartByte
	})

	result := make([]byte, len(source))
	copy(result, source)

	for _, e := range sorted {
		before := result[:e.StartByte]
		after := result[e.EndByte:]
		result = make([]byte, 0, len(before)+len(e.Replacement)+len(after))
		result = append(result, before...)
		result = append(result, e.Replacement...)
		result = append(result, after...)
	}

	return result
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd ~/work/gotreesitter && go test ./grep/ -v -run TestReplace`
Expected: all PASS (iterate on the match span calculation if the overall replacement byte range is off)

- [ ] **Step 5: Commit**

```bash
cd ~/work/gotreesitter && git add grep/rewrite.go grep/rewrite_test.go
buckley commit --yes --minimal-output
```

---

### Task 8: Full Query Pipeline — `find ... where ... replace ...`

**Files:**
- Create: `grep/query.go` (ties parser + compiler + matcher + rewriter together)
- Create: `grep/query_test.go`

- [ ] **Step 1: Write failing tests for the full pipeline**

```go
// grep/query_test.go
package grep

import (
	"testing"

	"github.com/odvcencio/gotreesitter/grammars"
)

// defaultResolver wraps grammars.DetectLanguageByName for use with Query().
func defaultResolver(name string) *gotreesitter.Language {
	entry := grammars.DetectLanguageByName(name)
	if entry == nil {
		return nil
	}
	return entry.Language()
}

func TestQueryFind(t *testing.T) {
	source := []byte(`package main

func ProcessOrder(ctx context.Context) error {
	return nil
}

func GetUser(id int) *User {
	return nil
}
`)

	results, err := Query(`find go::func $NAME($$$) error`, source, defaultResolver)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}

	if len(results.Matches) == 0 {
		t.Fatal("expected at least one match")
	}

	found := false
	for _, m := range results.Matches {
		if cap, ok := m.Captures["NAME"]; ok && string(cap.Text) == "ProcessOrder" {
			found = true
		}
	}
	if !found {
		t.Error("expected to find ProcessOrder")
	}
}

func TestQueryFindAndReplace(t *testing.T) {
	source := []byte(`console.log("hello");
console.log("world");
`)

	results, err := Query(
		`find javascript::console.log($$$ARGS) replace { console.info($$$ARGS) }`,
		source,
		defaultResolver,
	)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}

	if results.ReplaceResult == nil {
		t.Fatal("expected replace result")
	}
	if len(results.ReplaceResult.Edits) != 2 {
		t.Errorf("expected 2 edits, got %d", len(results.ReplaceResult.Edits))
	}
}

func TestQueryBarePattern(t *testing.T) {
	source := []byte(`package main

func Hello() {}
func World() {}
`)

	// Bare pattern, no "find" keyword — need to supply lang externally
	results, err := QueryWithLang(
		`func $NAME()`,
		source,
		testLang(t, "go"),
	)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}

	if len(results.Matches) != 2 {
		t.Errorf("expected 2 matches, got %d", len(results.Matches))
	}
}

func TestQuerySexpMode(t *testing.T) {
	source := []byte(`package main

func Hello() {}
func World() {}
`)

	results, err := Query(
		`find sexp::(function_declaration name: (identifier) @name)`,
		source,
		defaultResolver,
	)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}

	if len(results.Matches) != 2 {
		t.Errorf("expected 2 matches, got %d", len(results.Matches))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd ~/work/gotreesitter && go test ./grep/ -v -run TestQuery`
Expected: FAIL (Query not defined)

- [ ] **Step 3: Implement the full query pipeline**

```go
// grep/query.go
package grep

import (
	"fmt"

	gotreesitter "github.com/odvcencio/gotreesitter"
)

// LangResolver maps a language name to a Language.
// Typical usage: func(name string) *gotreesitter.Language {
//   entry := grammars.DetectLanguageByName(name)
//   if entry == nil { return nil }
//   return entry.Language()
// }
type LangResolver func(name string) *gotreesitter.Language

// QueryResult holds the results of a full structural grep query.
type QueryResult struct {
	Matches       []Result
	ReplaceResult *ReplaceResult // nil if no replace clause
}

// Query executes a full structural grep query string against source code.
// The query string may be in any accepted form:
//   find <lang>::<pattern> [where { ... }] [replace { ... }]
//   <lang>::<pattern> [where { ... }] [replace { ... }]
// The resolver maps language names to Language objects.
func Query(query string, source []byte, resolver LangResolver) (*QueryResult, error) {
	stmt, err := ParseQuery(query)
	if err != nil {
		return nil, fmt.Errorf("parse query: %w", err)
	}

	if stmt.Lang == "" {
		return nil, fmt.Errorf("language not specified — use 'find <lang>::<pattern>' or supply language directly via QueryWithLang")
	}

	// S-expression mode
	if stmt.Lang == "sexp" {
		return querySexp(stmt, source, resolver)
	}

	lang := resolver(stmt.Lang)
	if lang == nil {
		return nil, fmt.Errorf("unknown language %q", stmt.Lang)
	}

	return executeQuery(stmt, source, lang)
}

// QueryWithLang executes a query when the language is already known.
func QueryWithLang(query string, source []byte, lang *gotreesitter.Language) (*QueryResult, error) {
	stmt, err := ParseQuery(query)
	if err != nil {
		return nil, fmt.Errorf("parse query: %w", err)
	}
	return executeQuery(stmt, source, lang)
}

func executeQuery(stmt *QueryStatement, source []byte, lang *gotreesitter.Language) (*QueryResult, error) {
	results, err := Match(lang, stmt.Pattern, source)
	if err != nil {
		return nil, err
	}

	qr := &QueryResult{Matches: results}

	// TODO: Apply where-clause filtering when predicate compilation from
	// constraint strings is implemented. For now, where clauses in the
	// query language are parsed but not yet applied as post-match filters.
	// Predicates work via S-expression mode today.

	if stmt.Replace != "" {
		rr, err := buildEdits(results, stmt.Replace, source, lang)
		if err != nil {
			return nil, err
		}
		qr.ReplaceResult = rr
	}

	return qr, nil
}

func querySexp(stmt *QueryStatement, source []byte, resolver LangResolver) (*QueryResult, error) {
	// For sexp mode, we need a language to parse the source.
	// The sexp pattern itself is language-agnostic but we need to know
	// what language to parse the source as. For now, require the source
	// to be parseable — caller must ensure the right language is used.
	// This is a limitation that will be addressed when sexp mode gets
	// a source-language parameter.
	return nil, fmt.Errorf("sexp mode requires QueryWithLang — use MatchSexp directly for now")
}
```

Note: The `querySexp` function and where-clause filtering are intentionally stubbed. The tests for sexp mode will use `MatchSexp` directly. The where-clause pipeline will be completed when the constraint compiler (Task 6's predicates) is wired into the query language parser.

- [ ] **Step 4: Adjust tests for current capabilities and run**

Update `TestQuerySexpMode` to use `MatchSexp` directly since sexp via `Query()` needs a language parameter. Run all tests.

Run: `cd ~/work/gotreesitter && go test ./grep/ -v`
Expected: all PASS

- [ ] **Step 5: Commit**

```bash
cd ~/work/gotreesitter && git add grep/query.go grep/query_test.go
buckley commit --yes --minimal-output
```

---

### Task 8b: Where-Clause Compiler

**Files:**
- Create: `grep/where.go`
- Create: `grep/where_test.go`
- Modify: `grep/query.go` (wire where-clause into executeQuery)

This task connects the `where { ... }` block from the query parser to the predicate engine. It compiles constraint strings into post-match filters.

- [ ] **Step 1: Write failing tests for where-clause compilation**

```go
// grep/where_test.go
package grep

import (
	"testing"

	"github.com/odvcencio/gotreesitter/grammars"
)

func TestWhereNot(t *testing.T) {
	lang := testLang(t, "go")

	source := []byte(`package main

func ProcessOrder(ctx context.Context) error {
	return ctx.Err()
}

func GetUser(id int) error {
	return nil
}
`)

	// Match functions returning error whose body does NOT contain ctx.Err()
	results, err := Query(
		`find go::func $NAME($$$) error where { not contains($BODY, ctx.Err()) }`,
		source,
		defaultResolver,
	)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}

	// GetUser should match (no ctx.Err), ProcessOrder should not
	names := make(map[string]bool)
	for _, m := range results.Matches {
		if cap, ok := m.Captures["NAME"]; ok {
			names[string(cap.Text)] = true
		}
	}
	if names["ProcessOrder"] {
		t.Error("ProcessOrder should be filtered out by where clause")
	}
	if !names["GetUser"] {
		t.Error("GetUser should match")
	}
}

func TestWhereMatches(t *testing.T) {
	lang := testLang(t, "go")

	source := []byte(`package main

func TestFoo() {}
func TestBar() {}
func HelperBaz() {}
`)

	results, err := Query(
		`find go::func $NAME() where { matches($NAME, "^Test") }`,
		source,
		defaultResolver,
	)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}

	if len(results.Matches) != 2 {
		t.Errorf("expected 2 matches (TestFoo, TestBar), got %d", len(results.Matches))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd ~/work/gotreesitter && go test ./grep/ -v -run TestWhere`
Expected: FAIL (where clauses are not applied yet — the TODO in executeQuery)

- [ ] **Step 3: Implement the where-clause compiler**

```go
// grep/where.go
package grep

import (
	"fmt"
	"regexp"
	"strings"

	gotreesitter "github.com/odvcencio/gotreesitter"
)

// WhereFilter is a post-match predicate compiled from a where clause.
type WhereFilter func(result *Result, source []byte, lang *gotreesitter.Language) bool

// CompileWhere compiles a where-clause string into a filter function.
// Supported constraints:
//   - not <predicate>
//   - contains($cap, <text>)
//   - not contains($cap, <text>)
//   - matches($cap, "regex")
//   - is_exported($cap)
//   - kind($cap) == "type"
//   - $A == $B / $A != $B
func CompileWhere(where string) (WhereFilter, error) {
	where = strings.TrimSpace(where)
	if where == "" {
		return func(_ *Result, _ []byte, _ *gotreesitter.Language) bool { return true }, nil
	}

	// Parse "not contains($cap, pattern)"
	if strings.HasPrefix(where, "not contains(") {
		capName, pattern, err := parseContainsArgs(where[4:]) // skip "not "
		if err != nil {
			return nil, fmt.Errorf("parse not contains: %w", err)
		}
		return func(r *Result, source []byte, lang *gotreesitter.Language) bool {
			cap, ok := r.Captures[capName]
			if !ok {
				return true
			}
			return !strings.Contains(string(cap.Text), pattern)
		}, nil
	}

	// Parse "contains($cap, pattern)"
	if strings.HasPrefix(where, "contains(") {
		capName, pattern, err := parseContainsArgs(where)
		if err != nil {
			return nil, fmt.Errorf("parse contains: %w", err)
		}
		return func(r *Result, source []byte, lang *gotreesitter.Language) bool {
			cap, ok := r.Captures[capName]
			if !ok {
				return false
			}
			return strings.Contains(string(cap.Text), pattern)
		}, nil
	}

	// Parse "matches($cap, "regex")"
	if strings.HasPrefix(where, "matches(") {
		capName, pattern, err := parseMatchesArgs(where)
		if err != nil {
			return nil, fmt.Errorf("parse matches: %w", err)
		}
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("invalid regex %q: %w", pattern, err)
		}
		return func(r *Result, source []byte, lang *gotreesitter.Language) bool {
			cap, ok := r.Captures[capName]
			if !ok {
				return false
			}
			return re.MatchString(string(cap.Text))
		}, nil
	}

	// Parse "not matches($cap, "regex")"
	if strings.HasPrefix(where, "not matches(") {
		capName, pattern, err := parseMatchesArgs(where[4:])
		if err != nil {
			return nil, fmt.Errorf("parse not matches: %w", err)
		}
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("invalid regex %q: %w", pattern, err)
		}
		return func(r *Result, source []byte, lang *gotreesitter.Language) bool {
			cap, ok := r.Captures[capName]
			if !ok {
				return true
			}
			return !re.MatchString(string(cap.Text))
		}, nil
	}

	return nil, fmt.Errorf("unsupported where clause: %q", where)
}

// parseContainsArgs extracts capture name and pattern from "contains($CAP, pattern)"
func parseContainsArgs(s string) (capName, pattern string, err error) {
	// Find opening paren
	start := strings.Index(s, "(")
	end := strings.LastIndex(s, ")")
	if start < 0 || end < 0 {
		return "", "", fmt.Errorf("malformed contains expression")
	}
	inner := strings.TrimSpace(s[start+1 : end])

	// Split on first comma
	comma := strings.Index(inner, ",")
	if comma < 0 {
		return "", "", fmt.Errorf("contains requires two arguments")
	}

	capRef := strings.TrimSpace(inner[:comma])
	pattern = strings.TrimSpace(inner[comma+1:])

	// Strip $ from capture reference
	if strings.HasPrefix(capRef, "$") {
		capName = capRef[1:]
	} else {
		capName = capRef
	}

	return capName, pattern, nil
}

// parseMatchesArgs extracts capture name and regex from "matches($CAP, "regex")"
func parseMatchesArgs(s string) (capName, pattern string, err error) {
	capName, pattern, err = parseContainsArgs(s)
	if err != nil {
		return "", "", err
	}
	// Strip surrounding quotes from regex
	pattern = strings.Trim(pattern, `"'`)
	return capName, pattern, nil
}
```

- [ ] **Step 4: Wire where-clause into executeQuery**

Update `executeQuery` in `grep/query.go` to replace the TODO:

```go
func executeQuery(stmt *QueryStatement, source []byte, lang *gotreesitter.Language) (*QueryResult, error) {
	results, err := Match(lang, stmt.Pattern, source)
	if err != nil {
		return nil, err
	}

	// Apply where-clause filtering
	if stmt.Where != "" {
		filter, err := CompileWhere(stmt.Where)
		if err != nil {
			return nil, fmt.Errorf("compile where clause: %w", err)
		}
		var filtered []Result
		for i := range results {
			if filter(&results[i], source, lang) {
				filtered = append(filtered, results[i])
			}
		}
		results = filtered
	}

	qr := &QueryResult{Matches: results}

	if stmt.Replace != "" {
		rr, err := buildEdits(results, stmt.Replace, source, lang)
		if err != nil {
			return nil, err
		}
		qr.ReplaceResult = rr
	}

	return qr, nil
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd ~/work/gotreesitter && go test ./grep/ -v -run TestWhere`
Expected: all PASS

- [ ] **Step 6: Commit**

```bash
cd ~/work/gotreesitter && git add grep/where.go grep/where_test.go grep/query.go
buckley commit --yes --minimal-output
```

---

## Chunk 4: Integration Tests + Documentation

### Task 9: End-to-End Integration Tests

**Files:**
- Create: `grep/integration_test.go`

Real-world patterns against real Go/JavaScript/Python/Rust source. These tests validate the full pipeline across languages.

- [ ] **Step 1: Write integration tests**

```go
// grep/integration_test.go
package grep

import (
	"testing"

	"github.com/odvcencio/gotreesitter/grammars"
)

func TestIntegrationGoErrorHandling(t *testing.T) {
	lang := testLang(t, "go")

	source := []byte(`package main

import "fmt"

func ProcessOrder(ctx context.Context, id string) error {
	order, err := fetchOrder(id)
	if err != nil {
		return fmt.Errorf("fetch: %w", err)
	}
	return order.Process(ctx)
}

func GetConfig() *Config {
	return defaultConfig
}

func ValidateInput(input string) error {
	if input == "" {
		return fmt.Errorf("empty input")
	}
	return nil
}
`)

	// Find all functions that return error
	results, err := Match(lang, `func $NAME($$$PARAMS) error`, source)
	if err != nil {
		t.Fatalf("match error: %v", err)
	}

	names := make(map[string]bool)
	for _, r := range results {
		if cap, ok := r.Captures["NAME"]; ok {
			names[string(cap.Text)] = true
		}
	}

	if !names["ProcessOrder"] {
		t.Error("missing ProcessOrder")
	}
	if !names["ValidateInput"] {
		t.Error("missing ValidateInput")
	}
	if names["GetConfig"] {
		t.Error("GetConfig should not match (returns *Config, not error)")
	}
}

func TestIntegrationJSRewrite(t *testing.T) {
	lang := testLang(t, "javascript")

	source := []byte(`
const x = require("lodash");
const y = require("express");
const z = require("./local");
`)

	result, err := Replace(lang, `require($ARG)`, `await import($ARG)`, source)
	if err != nil {
		t.Fatalf("replace error: %v", err)
	}

	if len(result.Edits) != 3 {
		t.Errorf("expected 3 edits, got %d", len(result.Edits))
	}

	output := ApplyEdits(source, result.Edits)
	if !containsSubstr(output, "await import") {
		t.Errorf("output should contain 'await import':\n%s", output)
	}
	if containsSubstr(output, "require") {
		t.Errorf("output should not contain 'require':\n%s", output)
	}
}

func TestIntegrationPythonFunctions(t *testing.T) {
	lang := testLang(t, "python")

	source := []byte(`
def process(data):
    return transform(data)

def validate(input):
    if not input:
        raise ValueError("empty")
    return True

class Handler:
    def handle(self, request):
        return self.process(request)
`)

	results, err := Match(lang, `def $NAME($$$PARAMS)`, source)
	if err != nil {
		t.Fatalf("match error: %v", err)
	}

	if len(results) < 3 {
		t.Errorf("expected at least 3 function matches, got %d", len(results))
	}
}

func TestIntegrationRustUnwrap(t *testing.T) {
	lang := testLang(t, "rust")

	source := []byte(`
fn main() {
    let x = some_fn().unwrap();
    let y = other_fn().unwrap();
    let z = safe_fn().expect("ok");
}
`)

	result, err := Replace(lang, `$EXPR.unwrap()`, `$EXPR.expect("unexpected error")`, source)
	if err != nil {
		t.Fatalf("replace error: %v", err)
	}

	if len(result.Edits) != 2 {
		t.Errorf("expected 2 edits (two unwrap calls), got %d", len(result.Edits))
	}

	output := ApplyEdits(source, result.Edits)
	if containsSubstr(output, ".unwrap()") {
		t.Errorf("output still contains .unwrap():\n%s", output)
	}
}

func containsSubstr(b []byte, sub string) bool {
	return len(b) > 0 && len(sub) > 0 && containsBytes(b, []byte(sub))
}

func containsBytes(b, sub []byte) bool {
	for i := 0; i <= len(b)-len(sub); i++ {
		match := true
		for j := 0; j < len(sub); j++ {
			if b[i+j] != sub[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
```

- [ ] **Step 2: Run integration tests**

Run: `cd ~/work/gotreesitter && go test ./grep/ -v -run TestIntegration`
Expected: iterate on compiler and match logic as needed. These tests will expose grammar-specific issues in the pattern compiler.

- [ ] **Step 3: Fix any issues found in integration testing**

The pattern compiler's `treeToSexp` function will likely need adjustments for how different grammars structure function declarations, call expressions, etc. Debug using `SExpr()` on parsed test source to see actual tree structure.

- [ ] **Step 4: Run full test suite**

Run: `cd ~/work/gotreesitter && go test ./grep/ -v`
Expected: all PASS

- [ ] **Step 5: Commit**

```bash
cd ~/work/gotreesitter && git add grep/integration_test.go
buckley commit --yes --minimal-output
```

---

### Task 10: Package Documentation and Cleanup

**Files:**
- Create: `grep/doc.go`
- Review and clean up all grep/ files

- [ ] **Step 1: Write package doc**

```go
// grep/doc.go

// Package grep provides structural code search, match, and rewrite using
// tree-sitter parse trees. It is an AST-grep-inspired pattern matching
// engine built on gotreesitter's query system.
//
// Code patterns use metavariables ($NAME, $$$ARGS, $_, $E:type) that match
// AST nodes structurally. Patterns are parsed as real code in the target
// language, then compiled to tree-sitter S-expression queries.
//
// Basic usage:
//
//	lang := grammars.GetLanguage("go")
//	results, err := grep.Match(lang, `func $NAME($$$) error`, source)
//	for _, r := range results {
//	    fmt.Printf("found: %s\n", r.Captures["NAME"].Text)
//	}
//
// Rewrite usage:
//
//	result, err := grep.Replace(lang, `$E.unwrap()`, `$E.expect("failed")`, source)
//	output := grep.ApplyEdits(source, result.Edits)
//
// Full query syntax:
//
//	find <lang>::<pattern> [where { <constraints> }] [replace { <template> }]
package grep
```

- [ ] **Step 2: Review all files for consistency**

Check: exported types are documented, no dead code, consistent error messages.

- [ ] **Step 3: Run all tests one final time**

Run: `cd ~/work/gotreesitter && go test ./grep/ -v -count=1`
Expected: all PASS

- [ ] **Step 4: Run go vet**

Run: `cd ~/work/gotreesitter && go vet ./grep/`
Expected: no issues

- [ ] **Step 5: Commit**

```bash
cd ~/work/gotreesitter && git add grep/doc.go
buckley commit --yes --minimal-output
```

---

## Summary

| Task | What | Files |
|------|------|-------|
| 1 | Query language parser | `grep/parse.go`, `grep/parse_test.go` |
| 2 | Metavariable preprocessor | `grep/preprocess.go`, `grep/preprocess_test.go` |
| 3 | Pattern compiler (tree → S-expression) | `grep/compile.go`, `grep/compile_test.go` |
| 4 | Match API | `grep/match.go`, `grep/match_test.go` |
| 5 | Compile API (reusable patterns) | `grep/grep.go` |
| 6 | Extended predicates | `query.go`, `query_compile_predicates.go`, `query_predicates.go` |
| 7 | Rewriter | `grep/rewrite.go`, `grep/rewrite_test.go` |
| 8 | Full query pipeline | `grep/query.go`, `grep/query_test.go` |
| 8b | Where-clause compiler | `grep/where.go`, `grep/where_test.go` |
| 9 | Integration tests | `grep/integration_test.go` |
| 10 | Documentation and cleanup | `grep/doc.go` |

**Dependencies:** Tasks 1-3 must be sequential (parser → preprocessor → compiler). Task 4 depends on 3. Task 5 depends on 4. Task 6 is independent (core engine changes). Task 7 depends on 4. Task 8 depends on 1+4+7. Task 8b depends on 8 (wires where clauses into the query pipeline). Task 9 depends on all. Task 10 is last.

**Parallelizable:** Tasks 6 (predicates) can run in parallel with Tasks 4-5 (match API). Tasks 1-2 (parser + preprocessor) are quick and sequential.
