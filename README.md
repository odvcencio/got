# got

Structural version control. Merges functions, not lines.

Git treats source files as bags of lines. Two developers add different functions to the same file — conflict. Both add different imports — conflict. One renames a variable, another adds a function nearby — conflict. None of these are real conflicts.

**got** is a standalone version control system that decomposes source into structural entities via [tree-sitter](https://tree-sitter.github.io/) — functions, methods, classes, imports — and merges at that level. Independent additions merge cleanly. Import blocks get set-union merged. Only genuine semantic overlaps produce conflicts.

```
# Git: CONFLICT (both modified main.go)
# Got: clean merge — two independent functions added

$ got merge feature
merging feature into main...
  main.go: clean
merge completed cleanly
```

## How it works

Got parses every source file into an ordered list of **entities**:

| Entity kind | Examples |
|------------|---------|
| Preamble | `package main`, license headers |
| Import block | `import (...)`, `from x import y` |
| Declaration | Functions, methods, types, classes, structs, traits |
| Interstitial | Whitespace and comments between declarations |

Each entity has an **identity key** (e.g. `decl:function_definition::ProcessOrder`) that survives editing, reordering, and branch divergence. Merge operates on these identities instead of line numbers:

- **Unchanged** — keep as-is
- **Modified by one side** — take the modification
- **Modified identically by both** — no conflict
- **Modified differently by both** — diff3 fallback on that entity's body
- **Import blocks** — set-union merge (combine all imports, deduplicate)
- **Added by one side** — insert at correct position
- **Deleted by one side, unchanged by other** — remove
- **Deleted vs modified** — real conflict

The critical invariant: reconstructing entities always reproduces the original source byte-for-byte.

## Install

```bash
go install github.com/odvcencio/got/cmd/got@latest
```

Requires Go 1.25+. Pure Go, no C dependencies.

## Usage

Got follows the same mental model as Git:

```bash
# Initialize a repository
got init myproject
cd myproject

# Stage and commit
echo 'package main

func Hello() {}
' > main.go
got add main.go
got commit -m "initial commit"

# Branch and diverge
got branch feature
got checkout feature
# ... add func Goodbye() ...
got add main.go
got commit -m "add Goodbye"

# Back to main, make a different change
got checkout main
# ... add func Greet() ...
got add main.go
got commit -m "add Greet"

# Structural merge — no conflict
got merge feature
```

### Commands

```
got init [path]              Create a new repository
got add <files...>           Stage files for commit
got status                   Show working tree status
got commit -m <message>      Record changes
got log [--oneline] [-n N]   Show commit history
got diff [--staged] [--entity]  Show changes
got branch [name] [-d name]  List, create, or delete branches
got checkout <target> [-b]   Switch branches
got merge <branch>           Three-way structural merge
```

### Structural diff

```bash
# Line-level diff (default)
got diff

# Entity-level diff — shows which functions/types changed
got diff --entity
```

## Architecture

```
.got/
  HEAD                    ref: refs/heads/main
  objects/                SHA-256 content-addressed store (2-char fan-out)
  refs/heads/             Branch tips
  index                   Staging area
```

**Object types:** blob, entity, entitylist, tree, commit

**Hashing:** SHA-256 with type-length envelope (`type len\0content`)

### Packages

| Package | Purpose |
|---------|---------|
| `pkg/object` | Content-addressed store with atomic writes |
| `pkg/entity` | Tree-sitter entity extraction and reconstruction |
| `pkg/diff3` | Myers diff + three-way line merge |
| `pkg/diff` | Entity-level diff computation |
| `pkg/merge` | Structural three-way merge orchestrator |
| `pkg/repo` | Repository operations (init, commit, branch, checkout, merge) |

## Language support

Got uses [gotreesitter](https://github.com/odvcencio/gotreesitter), a pure-Go tree-sitter runtime with 205 embedded grammars. Entity extraction is tested against:

- Go
- Python
- Rust
- TypeScript
- C

Any language with a tree-sitter grammar can be parsed. Declaration classification is extensible via node type maps.

## Status

Early development. 160 tests passing across 6 packages. The core structural merge works and produces fewer false conflicts than Git on independent additions to the same file.

What exists:
- Content-addressed object store (SHA-256)
- Entity extraction via tree-sitter (205 languages)
- Three-way structural merge with entity-level resolution
- Set-union import merging
- Entity-level and line-level diff
- Full CLI: init, add, status, commit, log, diff, branch, checkout, merge
- `.gotignore` support

What doesn't exist yet:
- Remote push/pull
- Pack files (objects are loose)
- Rename detection
- Submodules
- Compression

## Dependencies

- [gotreesitter](https://github.com/odvcencio/gotreesitter) — Pure-Go tree-sitter runtime (205 languages, no CGo)
- [cobra](https://github.com/spf13/cobra) — CLI framework

## License

MIT
