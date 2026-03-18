# Structural Grep: Design Specification

**Date:** 2026-03-16
**Status:** Draft
**Scope:** gotreesitter, graft, orchard

## Problem

Line-based search treats code as text. It cannot answer structural questions: "find all functions that return an error but never check context cancellation." AI agents operating on code need structural queries and precise symbol-level edits — not file reads and string replacement.

AST-grep proved that structural search is valuable. This design builds the same capability as a native primitive across the gotreesitter/graft/orchard stack, powered by tree-sitter parse trees instead of external tooling.

## Goals

1. A structural grep engine in gotreesitter that any Go program can use.
2. `graft grep` as a structural-first CLI command (line-level by flag).
3. Orchard API, UI, MCP tools, and webhook triggers for structural search.
4. A query language with code-pattern syntax, predicates, negation, and rewrite.
5. S-expression escape hatch for AI-generated complex queries.
6. Symbol-level edit operations (replace body, insert after/before, delete).

## Non-Goals

- Type-system-level analysis (cross-file type resolution, interface dispatch).
- Replacing LSP servers for IDE features like autocomplete or rename.
- Self-hosting the query language parser via grammargen (deferred to a later phase).

---

## Query Language

### Syntax

```
find <lang>::<code-pattern> [where { <constraints> }] [replace { <template> }]
```

The language prefix selects a gotreesitter grammar. The code pattern is parsed as real code in that language. Metavariables are identifiers starting with `$`.

### Metavariables

| Syntax | Meaning |
|--------|---------|
| `$NAME` | Captures exactly one named AST node |
| `$$$NAME` | Captures zero or more sibling nodes (variadic) |
| `$_` | Wildcard — matches one node, no capture |
| `$NAME:type` | Typed capture — constrains to a node type (e.g., `$E:expression`) |

### Examples

```
# Find Go functions returning error
find go::func $NAME($$$PARAMS) error { $$$BODY }

# Find console.log calls in TypeScript
find typescript::console.log($$$ARGS)

# Find Python functions with no docstring
find python::def $NAME($$$PARAMS): $$$BODY
  where { not matches($BODY, "^\\s*\"\"\"") }

# Rewrite unwrap to expect in Rust
find rust::$EXPR.unwrap()
  replace { $EXPR.expect("unexpected None") }

# Find functions that don't check context cancellation
find go::func $NAME($CTX context.Context, $$$) ($$$) { $$$BODY }
  where { not contains($BODY, $CTX.Err()) }
```

### Constraint Keywords (`where` block)

| Keyword | Meaning |
|---------|---------|
| `not <predicate>` | Negation |
| `contains($node, <pattern>)` | Subtree contains a match |
| `not contains($node, <pattern>)` | Subtree does not contain a match |
| `matches($node, "regex")` | Node text matches a regex |
| `is_exported($node)` | Language-aware export check |
| `ancestor(<pattern>)` | Node is nested inside a matching parent |
| `$A == $B` / `$A != $B` | Capture text equality |
| `kind($node) == "type"` | Node type check |
| `count($$$ITEMS) > N` | Variadic capture length constraint |

### S-Expression Mode

For AI agents and power users, raw S-expression queries bypass the code-pattern compiler:

```
find sexp::(function_definition
  name: (identifier) @name
  (#not-contains? @name "test")
  body: (block
    (#has-descendant? (call_expression
      function: (selector_expression
        field: (field_identifier) @method
        (#eq? @method "Close"))))))
```

### Fallback Behavior

In `graft grep`, the `find <lang>::` prefix is optional. Without it, graft detects the language from file extensions in the search scope. If a pattern contains `$` metavariables, it is always treated as structural. If a pattern has no metavariables and no `find` prefix, it is treated as a literal structural match (find this exact AST subtree).

If the pattern fails to parse as valid code in any detected language, graft falls back to line-level grep and emits a warning: `structural parse failed, falling back to line grep`. The `--structural` flag forces structural mode and errors instead of falling back.

In webhook triggers and Orchard API calls, the `find` keyword is optional when a `<lang>::` prefix is present. Both `find go::...` and `go::...` are accepted. Without a language prefix, the language is inferred from file extension.

### Breaking Change: `graft grep` Default Mode

The existing `graft grep` command performs line-level search by default. This spec changes the default to structural search, which is a breaking change. Users with scripts relying on the current line-level default must add `-L` / `--line` to preserve behavior. This is an intentional statement: graft is a structural VCS and its tools should be structural by default.

The existing `--entity` flag (search entity names by regex) is preserved. It searches the entity index for matching names — a fast metadata lookup, not a structural pattern match. `--entity-scope` is a separate flag that constrains structural search to within a named entity's body. Both flags can coexist: `--entity` searches *for* entities by name, `--entity-scope` searches *within* an entity structurally.

---

## Architecture

### Layer 1: gotreesitter `grep` Package

The core engine. Three components, no VCS awareness.

#### Pattern Compiler

Converts a code pattern string into an S-expression query. The compilation has three stages:

**Stage 1: Preprocessing.** Before parsing, replace metavariables with language-valid placeholder identifiers. `$NAME` becomes `__GREP_CAP_NAME__`, `$$$ITEMS` becomes `__GREP_VAR_ITEMS__`, and `$NAME:type` becomes `__GREP_TYPED_NAME_type__`. This ensures the target-language grammar produces a clean parse tree — `$` is not a valid identifier character in most languages (Go, Rust, Python, C), and `$$$` never parses as valid code. After parsing, a walk over the tree maps placeholders back to metavariable descriptors.

The `__GREP_` prefix is reserved. If user code contains identifiers matching this prefix, the pattern compiler emits a diagnostic error rather than silently producing incorrect matches.

**Stage 2: Tree-to-query translation.** Walk the preprocessed parse tree and emit S-expression query steps:

- Placeholder for `$NAME` → named capture on a wildcard node (`(_) @NAME`).
- Placeholder for `$$$NAME` → variadic capture. Variadics match zero or more *named sibling nodes* at the same depth. If the placeholder sits inside a list-like node, the capture applies to its named children, skipping punctuation (commas, semicolons). If the enclosing node is not list-like, the variadic captures consecutive siblings. A node is list-like if its grammar rule is a `repeat` or its type name ends in `_list`, `_arguments`, `_parameters`, or `_elements` — or if it has multiple named children of the same type.
- Placeholder for `$NAME:type` → capture constrained to the specified node type.
- Literal nodes → exact node-type + text match.

**Stage 3: Constraint compilation.** Parse the `where` block and append predicates to the S-expression query. See "Predicate Compilation" below for details.

Input: language string + pattern string.
Output: compiled S-expression query ready for execution.

#### Query Executor

The existing gotreesitter S-expression query engine (`query.go`), extended with new predicates:

| Predicate | Behavior |
|-----------|----------|
| `#not-contains? @cap <pattern>` | Captured subtree must not contain the pattern |
| `#has-descendant? <pattern>` | Subtree must contain a matching descendant |
| `#ancestor? <pattern>` | Node must be inside a matching ancestor |
| `#count? @cap op value` | Variadic capture count constraint |
| `#is-exported? @cap` | Language-aware export check (Go: capitalized, JS: `export`, Python: no `_` prefix) |

These extend the existing `QueryPredicate` system as post-match filters.

#### Predicate Compilation

Predicates that take sub-patterns (`#not-contains?`, `#has-descendant?`, `#ancestor?`) require nested query execution. The compilation strategy:

1. **At compile time**, sub-patterns inside `contains()`, `not contains()`, and `ancestor()` are compiled into separate `Query` objects. The parent query stores references to these sub-queries.
2. **At match time**, after the primary pattern matches, each predicate with a sub-query executes that sub-query against the relevant captured subtree. For `contains($BODY, <pattern>)`, the sub-query runs against the AST subtree rooted at the node captured by `$BODY`. For `ancestor(<pattern>)`, the sub-query runs against each ancestor of the matched node.
3. **Capture scoping**: metavariables inside sub-patterns that share a name with an outer capture (`$CTX`) bind by interpolation — the outer capture's text is substituted into the sub-pattern before compilation. Fresh metavariables in sub-patterns are scoped to the sub-query and do not leak into the parent match.
4. **`count($$$ITEMS) > N`** does not use a sub-query. The supported operators are `>`, `<`, `>=`, `<=`, `==`, `!=`. The predicate counts the nodes bound to the variadic capture and evaluates the comparison.
5. **`is_exported`** is best-effort for a known set of languages (Go: name starts with uppercase, JS/TS: `export` keyword ancestor, Python: no leading `_`). For unsupported languages, it always returns true. Use `matches(@name, "regex")` as an escape hatch.

#### Rewriter

Takes match results (captures with byte ranges) and a replacement template, substitutes captures, and returns edit operations. Does not touch files — returns edits for the caller to apply.

```go
type Edit struct {
    StartByte, EndByte uint32
    Replacement        []byte
}
```

**Edit ordering and overlaps.** Edits are sorted by `StartByte` descending and applied back-to-front so earlier byte ranges remain valid. Overlapping matches (where one match's byte range intersects another's) are detected at match time; the rewriter keeps only the outermost match and discards overlapping inner matches, with a diagnostic warning.

**Output validation.** After applying all edits, the rewriter re-parses the output with tree-sitter. If the result contains ERROR nodes that were not present in the input, the rewrite is flagged as producing invalid code. The caller decides whether to apply (with warning) or reject. The `Replace` API returns both the edits and a `Diagnostics` slice.

**Preview semantics.** The gotreesitter layer always returns edits without applying them. The caller (graft or orchard) decides whether to apply, preview, or reject. Orchard's API supports `preview: true` to return the diff without writing.

**S-expression mode rewrites.** When using S-expression patterns, the replacement template uses `@name` capture syntax (matching S-expression conventions) instead of `$NAME`. The rewriter detects which mode is active based on the query source.

#### Public API

```go
package grep

// Match finds all structural matches in source code.
func Match(lang *Language, pattern string, source []byte) ([]Result, error)

// MatchSexp finds matches using a raw S-expression query.
func MatchSexp(lang *Language, sexp string, source []byte) ([]Result, error)

// Replace finds matches and computes replacement edits.
func Replace(lang *Language, pattern string, replacement string, source []byte) (*ReplaceResult, error)

// ReplaceResult holds edits and any diagnostics from the rewrite.
type ReplaceResult struct {
    Edits       []Edit
    Diagnostics []Diagnostic // e.g., "rewrite produced invalid syntax at byte 142"
}

// Compile parses a pattern into a reusable compiled pattern.
func Compile(lang *Language, pattern string) (*CompiledPattern, error)

// Result holds a single match with its captures.
type Result struct {
    StartByte, EndByte uint32
    Captures           map[string]Capture
}

// Capture holds a matched metavariable binding.
type Capture struct {
    Name               string
    Text               []byte
    StartByte, EndByte uint32
    Node               *Node   // tree-sitter node for further inspection
}
```

#### Query Language Parser

Hand-rolled parser for the `find ... where ... replace ...` syntax. Extracts the language prefix, code pattern, constraint block, and replacement template. This parser will be replaced by a grammargen-generated parser when grammargen reaches parity.

### Layer 2: Graft

Graft wraps the grep engine with entity awareness, history search, and MCP tools.

#### `graft grep` Command

Structural grep is the default. Line-level grep is the fallback.

```bash
# Structural grep (default)
graft grep 'func $NAME($$$) error'

# With constraints
graft grep 'find go::func $NAME($$$) error where { not contains($BODY, ctx.Err()) }'

# Line-level grep (opt-in)
graft grep -L "processOrder"
graft grep --line "processOrder"

# Search across history
graft grep --history 'func $NAME($$$) error' --since=v0.4.0

# Scoped to an entity
graft grep --entity-scope 'type Config' '$FIELD string'

# Rewrite mode
graft grep --rewrite 'find go::$E.unwrap() replace { $E.expect("failed") }'

# S-expression mode
graft grep --sexp '(function_definition name: (identifier) @n (#match? @n "^Test"))'

# JSON output
graft grep --json 'func $NAME($$$)'
```

#### Entity Context in Results

Every match includes the enclosing entity's identity key, kind, and name. Output shows which structural unit contains the match, not just file and line number.

```
pkg/order/process.go :: func ProcessOrder (decl:function_definition::ProcessOrder)
  L42: match on 'err != nil' capture $ERR="err"
```

#### History Search

Walks commits, extracts entities, runs structural grep on each version. Reports when a pattern first appeared or disappeared.

**Pre-filtering.** The entity index (identity keys, declaration kinds, names) narrows the search before full structural matching. For a pattern like `func $NAME($$$) error`, the pre-filter selects only commits containing function entities — skipping commits that only touched imports or types. This reduces the work from O(commits * files) to O(commits * matching_entities).

**Limits.** History search defaults to the last 1,000 commits and can be bounded with `--since`, `--until`, or `--max-commits`. Results stream incrementally — the first match prints immediately, no need to wait for the full walk.

**Language availability.** If a historical file version uses a language without a tree-sitter grammar, that file is skipped with a diagnostic. The search continues over other files.

**Parallelism.** Commit walking is sequential (ordered by history), but structural matching within a commit runs concurrently across files, bounded by `GRAFT_ENTITY_WORKERS`.

#### Entity-Level Edit Operations

Extends the grep engine's raw `Edit` type with entity-aware operations:

```go
type EntityEdit struct {
    EntityKey string // identity key of target entity
    Operation string // "replace_body", "insert_after", "insert_before", "delete"
    Content   []byte
}
```

After rewrite, re-extracts entities and verifies identity keys are stable. Warns if a rewrite breaks entity tracking.

#### MCP Tools

| Tool | Description |
|------|-------------|
| `graft_grep` | Structural pattern search on working tree or ref |
| `graft_grep_replace` | Structural rewrite with preview |
| `graft_entity_edit` | Symbol-level operations: replace_body, insert_after, insert_before, delete |

These give AI agents structural search and precise edits without reading entire files.

### Layer 3: Orchard

Orchard exposes structural grep as a platform feature: API, UI, MCP tools, and webhook triggers.

#### API Endpoints

```
POST /api/v1/repos/{owner}/{repo}/grep/{ref}
  { "pattern": "func $NAME($$$) error", "lang": "go" }

POST /api/v1/repos/{owner}/{repo}/grep/{ref}/replace
  { "pattern": "...", "replacement": "...", "preview": true }

POST /api/v1/repos/{owner}/{repo}/grep/diff/{base}...{head}
  { "pattern": "..." }
```

The diff-scoped endpoint searches only within the structural diff between two refs. This enables review-time queries: "show me all new functions in this PR that don't handle errors."

#### MCP Tools

| Tool | Description |
|------|-------------|
| `orchard_grep` | Structural search across a repo at a ref |
| `orchard_grep_replace` | Structural rewrite with preview/apply |
| `orchard_grep_diff` | Search within a structural diff (PR-scoped) |
| `orchard_entity_edit` | Symbol-level insert/replace/delete |

#### Frontend

- **Search bar** in repo view accepts structural patterns.
- **Results** grouped by file, showing matched entity context and highlighted captures.
- **PR review tab** scopes structural queries to the diff.
- **Pattern builder UI** for visual query construction: pick language, construct type, fill constraints without writing pattern syntax.

#### Webhook Triggers

Structural grep patterns as push-time gates in hooks or Orchard webhook config:

```toml
[on-push.security-check]
grep = "go::$PKG.Exec($$$ARGS) where { not contains($ARGS, ctx) }"
action = "block"
message = "All Exec calls must pass context"
```

Structural linting as a platform primitive, not a separate CI step.

---

## Edge Cases and Performance

### Binary and Non-Parseable Files

Binary files are skipped (detected by null-byte scan, consistent with `graft add`). Files without a tree-sitter grammar are skipped with a diagnostic in structural mode. Files that fail to parse (severe syntax errors producing a root ERROR node) are searched best-effort — tree-sitter's error recovery still produces a partial tree, and matches within valid subtrees are returned. A diagnostic notes the parse errors.

### Large Repos

Structural grep is inherently more expensive than line-level grep — it parses every file. For large repos:

- **File-level parallelism.** Parse and match files concurrently, bounded by `GRAFT_ENTITY_WORKERS` (default: number of CPUs).
- **Extension filtering.** Only parse files with extensions that map to a known grammar. The language detection already has an extension map — reuse it.
- **Early termination.** `--max-results N` stops after N matches. `--max-files N` stops after scanning N files.
- **Compiled queries.** The `Compile()` API lets callers amortize pattern compilation across many files.
- **Data format denylist.** Consistent with entity extraction, skip JSON/YAML/TOML/CSV files over 256KB — they produce no structural entities and waste parse time.

---

## Sequencing

### Phase 1: gotreesitter `grep` package
- Pattern compiler (code patterns → S-expression queries)
- Extended predicates on existing query engine
- Rewriter with capture substitution
- Hand-rolled query language parser (`find ... where ... replace ...`)
- Public API: `Match`, `MatchSexp`, `Replace`, `Compile`

### Phase 2: Graft integration
- `graft grep` command (structural default, `-L` for line)
- Entity context in results
- Entity-level edit operations
- MCP tools: `graft_grep`, `graft_grep_replace`, `graft_entity_edit`
- History search (`--history`, `--since`)

### Phase 3: Orchard integration
- REST API endpoints (grep, grep/replace, grep/diff)
- MCP tools: `orchard_grep`, `orchard_grep_replace`, `orchard_grep_diff`, `orchard_entity_edit`
- Frontend search UI and PR review integration
- Webhook triggers with structural patterns

### Phase 4: Self-hosting (deferred)
- grammargen grammar for the query language
- Replace hand-rolled parser with grammargen-generated parser
- Query language becomes a grammargen showcase

---

## Inspiration

This feature is a love letter to [AST-grep](https://github.com/ast-grep/ast-grep) and its creator Herrington Darkholme, whose far-ahead vision proved that structural code search belongs in every developer's toolkit. Structural grep in graft builds on that vision by embedding it into version control, code review, and agent coordination — making structural understanding a platform primitive rather than a standalone tool.
