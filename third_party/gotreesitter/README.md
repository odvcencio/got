# gotreesitter

Pure-Go [tree-sitter](https://tree-sitter.github.io/) runtime. No CGo, no C toolchain. Cross-compiles to any `GOOS`/`GOARCH` target Go supports, including `wasip1`.

```sh
go get github.com/odvcencio/gotreesitter
```

gotreesitter loads the same parse-table format that tree-sitter's C runtime uses. Grammar tables are extracted from upstream `parser.c` files by `ts2go`, compressed into binary blobs, and deserialized on first use. 206 grammars ship in the registry.

## Motivation

Every Go tree-sitter binding in the ecosystem depends on CGo:

- Cross-compilation requires a C cross-toolchain per target. `GOOS=wasip1`, `GOARCH=arm64` from a Linux host, or any Windows build without MSYS2/MinGW, will not link.
- CI images must carry `gcc` and the grammar's C sources. `go install` fails for downstream users who don't have a C compiler.
- The Go race detector, coverage instrumentation, and fuzzer cannot see across the CGo boundary. Bugs in the C runtime or in FFI marshaling are invisible to `go test -race`.

gotreesitter eliminates the C dependency entirely. The parser, lexer, query engine, incremental reparsing, arena allocator, external scanners, and tree cursor are all implemented in Go. The only input is the grammar blob.

## Quick start

```go
import (
    "fmt"

    "github.com/odvcencio/gotreesitter"
    "github.com/odvcencio/gotreesitter/grammars"
)

func main() {
    src := []byte(`package main

func main() {}
`)

    lang := grammars.GoLanguage()
    parser := gotreesitter.NewParser(lang)

    tree, _ := parser.Parse(src)
    fmt.Println(tree.RootNode())
}
```

`grammars.DetectLanguage("main.go")` resolves a filename to the appropriate `LangEntry`.

### Queries

```go
q, _ := gotreesitter.NewQuery(`(function_declaration name: (identifier) @fn)`, lang)
cursor := q.Exec(tree.RootNode(), lang, src)

for {
    match, ok := cursor.NextMatch()
    if !ok {
        break
    }
    for _, cap := range match.Captures {
        fmt.Println(cap.Node.Text(src))
    }
}
```

The query engine supports the full S-expression pattern language: structural quantifiers (`?`, `*`, `+`), alternation (`[...]`), field constraints, negated fields, anchor (`!`), and all standard predicates. See [Query API](#query-api).

### Typed query codegen

Generate type-safe Go wrappers from `.scm` query files:

```sh
go run ./cmd/tsquery -input queries/go_functions.scm -lang go -output go_functions_query.go -package queries
```

Given a query like `(function_declaration name: (identifier) @name body: (block) @body)`, `tsquery` generates:

```go
type FunctionDeclarationMatch struct {
    Name *gotreesitter.Node
    Body *gotreesitter.Node
}

q, _ := queries.NewGoFunctionsQuery(lang)
cursor := q.Exec(tree.RootNode(), lang, src)
for {
    match, ok := cursor.Next()
    if !ok { break }
    fmt.Println(match.Name.Text(src))
}
```

Multi-pattern queries generate one struct per pattern with `MatchPatternN` conversion helpers.

### Multi-language documents (injection parsing)

Parse documents with embedded languages (HTML+JS+CSS, Markdown+code fences, Vue/Svelte templates):

```go
ip := gotreesitter.NewInjectionParser()
ip.RegisterLanguage("html", htmlLang)
ip.RegisterLanguage("javascript", jsLang)
ip.RegisterLanguage("css", cssLang)
ip.RegisterInjectionQuery("html", injectionQuery)

result, _ := ip.Parse(source, "html")

for _, inj := range result.Injections {
    fmt.Printf("%s: %d ranges\n", inj.Language, len(inj.Ranges))
    // inj.Tree is the child language's parse tree
}
```

Supports static (`#set! injection.language "javascript"`) and dynamic (`@injection.language` capture) language detection, recursive nested injections, and incremental reparse with child tree reuse.

### Source rewriting

Collect source-level edits and apply atomically, producing `InputEdit` records for incremental reparse:

```go
rw := gotreesitter.NewRewriter(src)
rw.Replace(funcNameNode, []byte("newName"))
rw.InsertBefore(bodyNode, []byte("// added\n"))
rw.Delete(unusedNode)

newSrc, _ := rw.ApplyToTree(tree)
newTree, _ := parser.ParseIncremental(newSrc, tree)
```

`Apply()` returns both the new source bytes and the `[]InputEdit` records. `ApplyToTree()` is a convenience that calls `tree.Edit()` for each edit and returns source ready for `ParseIncremental`.

### Incremental reparsing

```go
tree, _ := parser.Parse(src)

// User types "x" at byte offset 42
src = append(src[:42], append([]byte("x"), src[42:]...)...)

tree.Edit(gotreesitter.InputEdit{
    StartByte:   42,
    OldEndByte:  42,
    NewEndByte:  43,
    StartPoint:  gotreesitter.Point{Row: 3, Column: 10},
    OldEndPoint: gotreesitter.Point{Row: 3, Column: 10},
    NewEndPoint: gotreesitter.Point{Row: 3, Column: 11},
})

tree2, _ := parser.ParseIncremental(src, tree)
```

`ParseIncremental` walks the old tree's spine, identifies the edit region, and reuses unchanged subtrees by reference. Only the invalidated span is re-lexed and re-parsed. Both leaf and non-leaf subtrees are eligible for reuse; non-leaf reuse is driven by pre-goto state tracking on interior nodes, so the parser can skip entire subtrees without re-deriving their contents.

When no edit has occurred, `ParseIncremental` detects the nil-edit on a pointer check and returns in single-digit nanoseconds with zero allocations.

### Tree cursor

`TreeCursor` maintains an explicit `(node, childIndex)` frame stack. Parent, child, and sibling movement are O(1) with zero allocations — sibling traversal indexes directly into the parent's `children[]` slice.

```go
c := gotreesitter.NewTreeCursorFromTree(tree)

c.GotoFirstChild()
c.GotoChildByFieldName("body")

for ok := c.GotoFirstNamedChild(); ok; ok = c.GotoNextNamedSibling() {
    fmt.Printf("%s at %d\n", c.CurrentNodeType(), c.CurrentNode().StartByte())
}

idx := c.GotoFirstChildForByte(128)
```

Movement methods: `GotoFirstChild`, `GotoLastChild`, `GotoNextSibling`, `GotoPrevSibling`, `GotoParent`, named-only variants (`GotoFirstNamedChild`, etc.), field-based (`GotoChildByFieldName`, `GotoChildByFieldID`), and position-based (`GotoFirstChildForByte`, `GotoFirstChildForPoint`).

Cursors hold direct pointers into tree nodes. Recreate after `Tree.Release()`, `Tree.Edit(...)`, or incremental reparse.

### Highlighting

```go
hl, _ := gotreesitter.NewHighlighter(lang, highlightQuery)
ranges := hl.Highlight(src)

for _, r := range ranges {
    fmt.Printf("%s: %q\n", r.Capture, src[r.StartByte:r.EndByte])
}
```

### Tagging

```go
entry := grammars.DetectLanguage("main.go")
lang := entry.Language()

tagger, _ := gotreesitter.NewTagger(lang, entry.TagsQuery)
tags := tagger.Tag(src)

for _, tag := range tags {
    fmt.Printf("%s %s at %d:%d\n", tag.Kind, tag.Name,
        tag.NameRange.StartPoint.Row, tag.NameRange.StartPoint.Column)
}
```

## Benchmarks

All measurements below use the same workload: a generated Go source file with 500 functions (`19294` bytes).
Numbers are medians from 10 runs on:

```text
goos: linux
goarch: amd64
cpu: Intel(R) Core(TM) Ultra 9 285
```

| Runtime | Full parse | Incremental (1-byte edit) | Incremental (no edit) |
|---|---:|---:|---:|
| Native C (pure C runtime) | 1.76 ms | 102.3 μs | 101.7 μs |
| CGo binding (C runtime via cgo) | ~2.0 ms | ~130 μs | — |
| gotreesitter (pure Go) | 3.52 ms | 2.46 μs | 2.22 ns |

On this workload:

- Full parse is ~2.0x slower than native C.
- Incremental single-byte edits are ~41.6x faster than native C (~52.8x faster than CGo).
- No-edit reparses are ~45,800x faster than native C, zero allocations.

<details>
<summary>Raw benchmark output</summary>

```sh
# Pure Go (this repo):
GOMAXPROCS=1 go test . -run '^$' \
  -bench 'BenchmarkGoParseFullDFA|BenchmarkGoParseIncrementalSingleByteEditDFA|BenchmarkGoParseIncrementalNoEditDFA' \
  -benchmem -count=10 -benchtime=750ms

# CGo binding benchmarks:
cd cgo_harness
GOMAXPROCS=1 go test . -run '^$' -tags treesitter_c_bench \
  -bench 'BenchmarkCTreeSitterGoParseFull|BenchmarkCTreeSitterGoParseIncrementalSingleByteEdit|BenchmarkCTreeSitterGoParseIncrementalNoEdit' \
  -benchmem -count=10 -benchtime=750ms

# Native C benchmarks (no Go, direct C binary):
./pure_c/run_go_benchmark.sh 500 2000 20000
```

| Benchmark | Median ns/op | B/op | allocs/op |
|---|---:|---:|---:|
| Native C full parse | 1,764,436 | — | — |
| Native C incremental (1-byte edit) | 102,336 | — | — |
| Native C incremental (no edit) | 101,740 | — | — |
| `CTreeSitterGoParseFull` | ~1,990,000 | 600 | 6 |
| `CTreeSitterGoParseIncrementalSingleByteEdit` | ~130,000 | 648 | 7 |
| `GoParseFullDFA` | 3,523,652 | 425 | 5 |
| `GoParseIncrementalSingleByteEditDFA` | 2,458 | 496 | 9 |
| `GoParseIncrementalNoEditDFA` | 2.223 | 0 | 0 |

</details>

### Benchmark matrix

For repeatable multi-workload tracking:

```sh
go run ./cmd/benchmatrix --count 10
```

Emits `bench_out/matrix.json` (machine-readable), `bench_out/matrix.md` (summary), and raw logs under `bench_out/raw/`.

## Supported languages

206 grammars ship in the registry. 203 currently produce error-free parse trees on smoke samples; 3 are degraded (`disassembly`, `norg`, `vimdoc`). Run `go run ./cmd/parity_report` for current status.

- 14 hand-written (😉) Go external scanners (python, elixir, comment, doxygen, foam, nginx, nushell, r, xml, yuck, purescript, typst, html, yaml)
- 8 hand-written Go token sources (authzed, c, go, html, java, json, lua, toml)
- Remaining languages use the DFA lexer generated from grammar tables

### Parse quality

Each `LangEntry` carries a `Quality` field:

| Quality | Meaning |
|---|---|
| `full` | All scanner and lexer components present. Parser has full access to the grammar. |
| `partial` | Missing external scanner. DFA lexer handles what it can; external tokens are skipped. |
| `none` | Cannot parse. |

`full` means the parser has every component the grammar requires. It does not guarantee error-free trees on all inputs — grammars with high GLR ambiguity may produce syntax errors on very large or deeply nested constructs due to parser safety limits (iteration cap, stack depth cap, node count cap). These limits scale with input size. Check `tree.RootNode().HasError()` at runtime.

<details>
<summary>Full language list (206)</summary>

`ada`, `agda`, `angular`, `apex`, `arduino`, `asm`, `astro`, `authzed`, `awk`, `bash`, `bass`, `beancount`, `bibtex`, `bicep`, `bitbake`, `blade`, `brightscript`, `c`, `c_sharp`, `caddy`, `cairo`, `capnp`, `chatito`, `circom`, `clojure`, `cmake`, `cobol`, `comment`, `commonlisp`, `cooklang`, `corn`, `cpon`, `cpp`, `crystal`, `css`, `csv`, `cuda`, `cue`, `cylc`, `d`, `dart`, `desktop`, `devicetree`, `dhall`, `diff`, `disassembly`, `djot`, `dockerfile`, `dot`, `doxygen`, `dtd`, `earthfile`, `ebnf`, `editorconfig`, `eds`, `eex`, `elisp`, `elixir`, `elm`, `elsa`, `embedded_template`, `enforce`, `erlang`, `facility`, `faust`, `fennel`, `fidl`, `firrtl`, `fish`, `foam`, `forth`, `fortran`, `fsharp`, `gdscript`, `git_config`, `git_rebase`, `gitattributes`, `gitcommit`, `gitignore`, `gleam`, `glsl`, `gn`, `go`, `godot_resource`, `gomod`, `graphql`, `groovy`, `hack`, `hare`, `haskell`, `haxe`, `hcl`, `heex`, `hlsl`, `html`, `http`, `hurl`, `hyprlang`, `ini`, `janet`, `java`, `javascript`, `jinja2`, `jq`, `jsdoc`, `json`, `json5`, `jsonnet`, `julia`, `just`, `kconfig`, `kdl`, `kotlin`, `ledger`, `less`, `linkerscript`, `liquid`, `llvm`, `lua`, `luau`, `make`, `markdown`, `markdown_inline`, `matlab`, `mermaid`, `meson`, `mojo`, `move`, `nginx`, `nickel`, `nim`, `ninja`, `nix`, `norg`, `nushell`, `objc`, `ocaml`, `odin`, `org`, `pascal`, `pem`, `perl`, `php`, `pkl`, `powershell`, `prisma`, `prolog`, `promql`, `properties`, `proto`, `pug`, `puppet`, `purescript`, `python`, `ql`, `r`, `racket`, `regex`, `rego`, `requirements`, `rescript`, `robot`, `ron`, `rst`, `ruby`, `rust`, `scala`, `scheme`, `scss`, `smithy`, `solidity`, `sparql`, `sql`, `squirrel`, `ssh_config`, `starlark`, `svelte`, `swift`, `tablegen`, `tcl`, `teal`, `templ`, `textproto`, `thrift`, `tlaplus`, `tmux`, `todotxt`, `toml`, `tsx`, `turtle`, `twig`, `typescript`, `typst`, `uxntal`, `v`, `verilog`, `vhdl`, `vimdoc`, `vue`, `wat`, `wgsl`, `wolfram`, `xml`, `yaml`, `yuck`, `zig`

</details>

## Query API

| Feature | Status |
|---|---|
| Compile + execute (`NewQuery`, `Execute`, `ExecuteNode`) | supported |
| Cursor streaming (`Exec`, `NextMatch`, `NextCapture`) | supported |
| Structural quantifiers (`?`, `*`, `+`) | supported |
| Alternation (`[...]`) | supported |
| Field matching (`name: (identifier)`) | supported |
| `#eq?` / `#not-eq?` | supported |
| `#match?` / `#not-match?` | supported |
| `#any-of?` / `#not-any-of?` | supported |
| `#lua-match?` | supported |
| `#has-ancestor?` / `#not-has-ancestor?` | supported |
| `#not-has-parent?` | supported |
| `#is?` / `#is-not?` | supported |
| `#any-eq?` / `#any-not-eq?` | supported |
| `#any-match?` / `#any-not-match?` | supported |
| `#select-adjacent!` | supported |
| `#strip!` | supported |
| `#set!` / `#offset!` directives | parsed and accepted |
| `SetValues` (read `#set!` metadata from matches) | supported |

All shipped highlight and tags queries compile (`156/156` highlight, `69/69` tags).

## Known limitations

- **Full-parse throughput**: ~2.0x slower than the C runtime on cold full parses (the 500-function Go benchmark). Incremental reparsing — the dominant operation in editor workloads — is 42x faster.
- **GLR safety caps**: The parser enforces iteration, stack depth, and node count limits proportional to input size. These prevent pathological blowup on grammars with high ambiguity but impose a ceiling on the maximum input complexity that parses without error. The caps are tunable but not removable without risking unbounded resource consumption.
- **Degraded grammars**: 3 of 206 grammars are currently degraded: `disassembly`, `norg`, and `vimdoc`. Check `entry.Quality` and `tree.RootNode().HasError()`.

## Adding a language

1. Add the grammar repo to `grammars/languages.manifest`
2. Refresh pinned refs in `grammars/languages.lock`:
   `go run ./cmd/grammar_updater -lock grammars/languages.lock -write -report grammars/grammar_updates.json`
3. Generate tables: `go run ./cmd/ts2go -manifest grammars/languages.manifest -outdir ./grammars -package grammars -compact=true`
4. Add smoke samples to `cmd/parity_report/main.go` and `grammars/parse_support_test.go`
5. Verify: `go run ./cmd/parity_report && go test ./grammars/...`

## Grammar lock updates

- `grammars/languages.lock` stores pinned refs for grammar update + parity automation.
- `cmd/grammar_updater` refreshes refs and emits a machine-readable report.
- `.github/workflows/grammar-lock-update.yml` opens scheduled/dispatch update PRs.

Manual refresh:

```sh
go run ./cmd/grammar_updater \
  -lock grammars/languages.lock \
  -allow-list grammars/update_tier1_core100.txt \
  -max-updates 10 \
  -write \
  -report grammars/grammar_updates.json
```

## Architecture

gotreesitter is a ground-up reimplementation of the tree-sitter runtime in Go. No code is shared with or translated from the C implementation.

**Parser** — Table-driven LR(1) with GLR fallback. When a `(state, symbol)` pair maps to multiple actions in the parse table, the parser forks the stack and explores all alternatives in parallel. Stack merging collapses equivalent paths. Safety limits (iteration count, stack depth, node count) scale with input size and prevent runaway exploration on ambiguous grammars.

**Incremental engine** — Walks the edit region of the previous tree and reuses unchanged subtrees by reference. Non-leaf subtree reuse is enabled by storing a pre-goto parser state on each interior node, allowing the parser to skip an entire subtree and resume in the correct state without re-deriving its contents. External scanner state is serialized on each node boundary so scanner-dependent subtrees can be reused without replaying the scanner from the start.

**Lexer** — Two paths. A DFA lexer is generated from the grammar's lex tables by `ts2go` and handles the majority of languages. For grammars where the DFA is insufficient (e.g., Go's automatic semicolons, YAML's indentation-sensitive structure), hand-written Go token sources implement the `TokenSource` interface directly.

**External scanners** — 112 grammars require external scanners for context-sensitive tokens (Python indentation, HTML implicit close tags, Rust raw string delimiters, etc.). Each scanner is a hand-written Go implementation of the grammar's `ExternalScanner` interface: `Create`, `Serialize`, `Deserialize`, `Scan`. Scanner state is snapshotted after every token and stored on tree nodes so incremental reuse can restore scanner state on skip.

**Arena allocator** — Nodes are allocated from slab-based arenas to reduce GC pressure. Arenas are released in bulk when a tree is freed.

**Query engine** — S-expression pattern compiler with predicate evaluation and streaming cursor iteration. Supports all standard tree-sitter predicates (`#eq?`, `#match?`, `#any-of?`, `#has-ancestor?`, etc.) and directive annotations (`#set!`, `#offset!`, `#select-adjacent!`, `#strip!`).

**Injection parser** — Orchestrates multi-language parsing. Runs injection queries against a parent tree to find embedded regions, spawns child parsers with `SetIncludedRanges()`, and recurses for nested injections. Incremental reparse reuses unchanged child trees.

**Rewriter** — Collects source-level edits (replace, insert, delete) targeting byte ranges, applies them atomically, and produces `InputEdit` records for incremental reparse. Edits are validated for non-overlap and applied in a single pass.

**Grammar loading** — `ts2go` extracts parse tables, lex tables, field maps, symbol metadata, and external token lists from upstream `parser.c` files. These are serialized to compressed binary blobs under `grammars/grammar_blobs/` and lazy-loaded via `loadEmbeddedLanguage()` with an LRU cache. String and transition interning reduce memory footprint across loaded grammars.

### Build tags and environment

**External grammar blobs** (avoid embedding in the binary):

```sh
go build -tags grammar_blobs_external
GOTREESITTER_GRAMMAR_BLOB_DIR=/path/to/blobs  # required
GOTREESITTER_GRAMMAR_BLOB_MMAP=false           # disable mmap (Unix only)
```

**Curated language set** (smaller binary):

```sh
go build -tags grammar_set_core  # curated Core100 embedded grammar set
GOTREESITTER_GRAMMAR_SET=go,json,python  # runtime restriction
```

**Grammar cache tuning** (long-lived processes):

```go
grammars.SetEmbeddedLanguageCacheLimit(8)    // LRU cap
grammars.UnloadEmbeddedLanguage("rust.bin")  // drop one
grammars.PurgeEmbeddedLanguageCache()        // drop all
```

```sh
GOTREESITTER_GRAMMAR_CACHE_LIMIT=8       # LRU cap via env
GOTREESITTER_GRAMMAR_IDLE_TTL=5m         # evict after idle
GOTREESITTER_GRAMMAR_IDLE_SWEEP=30s      # sweep interval
GOTREESITTER_GRAMMAR_COMPACT=true        # loader compaction (default)
GOTREESITTER_GRAMMAR_STRING_INTERN_LIMIT=200000
GOTREESITTER_GRAMMAR_TRANSITION_INTERN_LIMIT=20000
```

**GLR stack cap override**:

```sh
GOT_GLR_MAX_STACKS=8  # overrides default GLR stack cap (default: 2)
```

Default is tuned for performance. Increase this only if a grammar/workload
needs more GLR alternatives to preserve parity.

**Legacy benchmark compatibility only**:

```sh
GOT_PARSE_NODE_LIMIT_SCALE=3
```

`GOT_PARSE_NODE_LIMIT_SCALE` is only needed for comparisons against older truncation-prone benchmark baselines. On current branches, keep it unset.

## Testing

```sh
go test ./... -race -count=1
```

Correctness/parity gate commands used in CI and performance work:

```sh
# Top-50 smoke correctness
go test ./grammars -run '^TestTop50ParseSmokeNoErrors$' -count=1 -v

# C-oracle parity suites
cd cgo_harness
go test . -tags treesitter_c_parity -run '^TestParityFreshParse$|^TestParityHasNoErrors$|^TestParityIssue3Repros$|^TestParityGLRCanaryGo$' -count=1 -v
go test . -tags treesitter_c_parity -run '^TestParityCorpusFreshParse$' -count=1 -v
```

Test suite covers: smoke tests (206 grammars), golden S-expression snapshots, highlight query validation, query pattern matching, incremental reparse correctness, error recovery, GLR fork/merge, injection parsing, source rewriting, and fuzz targets.

## Roadmap

v0.6.0 — 206 grammars, GLR parser, incremental reparsing, query engine, tree cursor, highlighting, tagging, ABI 15 support, injection parser, typed query codegen, CST rewriter, and tightened parity/perf gates.

Next:
- Corpus parity testing against C tree-sitter reference output
- GLR ambiguity overhead reduction for large files
- Port norg external scanner
- Fuzz coverage expansion

Release history and retroactive notes are tracked in [CHANGELOG.md](CHANGELOG.md).

## License

[MIT](LICENSE)
