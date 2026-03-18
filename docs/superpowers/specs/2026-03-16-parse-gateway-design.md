# Parse Gateway: Design Specification

**Date:** 2026-03-16
**Status:** Draft
**Scope:** gotreesitter (primary), graft, gts-suite, orchard (consumers)

## Problem

Every tool in the Orchard stack reimplements the same walk → detect → read → parse loop with its own safety logic (or none). Graft's entity extraction has a data format denylist and memory budget. gts-suite's indexer has nothing — it OOMs on large repos. Orchard's grep service has nothing. Four consumers, four loops, one safe.

The fix is a shared parse gateway in gotreesitter that all consumers use. Safety becomes a property of the library, not a responsibility of each consumer.

## Goals

1. A streaming parse gateway in gotreesitter's `grammars` package that walks a directory, detects languages, and streams parsed files over a channel.
2. Policy-based control: concurrent parsing for normal files, throttled single-file mode for large files, progress reporting throughout.
3. No files are refused. Large files parse at a controlled pace with user feedback. The system is always responsive, always making progress, never dangerous.
4. Consumers that walk the filesystem (gts-suite indexer, graft structural grep, orchard grep service) replace their walk-parse loops with a single `WalkAndParse` call.

## Non-Goals

- Replacing `ParseFile`/`ParseFilePooled` for single-file parsing (pattern compilation, snippet parsing).
- Replacing graft's entity extraction staging pipeline (reads from object store, not filesystem).
- Incremental/cached parsing (gts-suite's `BuildPathIncremental` reuse logic stays in gts-suite).
- Cross-ref or symbol extraction — consumers handle that from the parsed tree.

---

## Design

### Core Types

```go
package grammars

// ParsePolicy controls parsing behavior and resource usage.
type ParsePolicy struct {
    LargeFileThreshold int64            // files >= this parse solo (default 256KB)
    MaxConcurrent      int              // max concurrent parses for normal files (default GOMAXPROCS)
    ChannelBuffer      int              // result channel buffer size (default MaxConcurrent + 1)
    SkipDirs           []string         // directories to skip (default: .git, .graft, vendor, node_modules)
    SkipExtensions     []string         // always skip (e.g., .min.js, .map, .wasm)
    ShouldParse        func(path string, size int64, modTime time.Time) bool // optional pre-filter
    OnProgress         func(ProgressEvent) // optional user feedback callback
}

// ProgressEvent reports what the gateway is doing.
type ProgressEvent struct {
    Phase   string // "walking", "parsing", "large_file", "walk_complete", "done"
    Path    string // current file (empty for walking/walk_complete/done)
    Size    int64
    FileNum int    // current file number
    Total   int    // total files discovered (accurate after walk_complete)
    Message string // human-readable status
}

// ParsedFile is a single result from the gateway.
type ParsedFile struct {
    Path   string
    Tree   *BoundTree    // bound tree with language context; consumer MUST call Close()
    Lang   *LangEntry    // language entry from grammars registry
    Source []byte
    Size   int64
    Err    error         // non-nil on failure; covers both I/O and parse errors
    IsRead bool          // false if Err is an I/O error (Source/Tree nil), true if parse error
}

// Close releases the parse tree's memory. Consumers MUST call this after
// processing each ParsedFile. Failure to call Close leaks parser memory.
// Safe to call on files with Err != nil.
func (pf *ParsedFile) Close() {
    if pf.Tree != nil {
        pf.Tree.Release()
        pf.Tree = nil
    }
    pf.Source = nil
}

// WalkStats summarizes the completed walk.
type WalkStats struct {
    FilesFound    int   // files with a detected language (sent to channel)
    FilesParsed   int   // successfully parsed
    FilesFailed   int   // attempted but errored (I/O or parse)
    FilesFiltered int   // skipped by ShouldParse callback
    LargeFiles    int   // files that parsed in solo mode
    BytesParsed   int64
}
```

### Ownership Protocol

**The consumer owns each `ParsedFile` received from the channel and MUST call `Close()` after processing.** This releases the `BoundTree` back to the parser pool and nils out the `Source` slice, allowing the garbage collector to reclaim the memory.

```go
for file := range results {
    // process file.Tree, file.Source
    file.Close() // MUST call — releases tree and source
}
```

Failure to call `Close()` causes memory to grow without bound, which is the exact problem the gateway exists to solve. The `Close()` method is safe to call multiple times and on error results.

### Default Policy

```go
func DefaultPolicy() ParsePolicy {
    workers := runtime.GOMAXPROCS(0)
    if workers < 1 { workers = 1 }
    return ParsePolicy{
        LargeFileThreshold: 256 * 1024, // 256KB
        MaxConcurrent:      workers,
        ChannelBuffer:      workers + 1, // +1 to prevent deadlock (see Pipeline section)
        SkipDirs:           []string{".git", ".graft", ".hg", ".svn", "vendor", "node_modules"},
        SkipExtensions:     []string{".min.js", ".min.css", ".map", ".wasm"},
    }
}
```

Environment variable overrides (checked at `DefaultPolicy()` call time):
- `GTS_LARGE_FILE_THRESHOLD` — bytes (e.g., `524288` for 512KB)
- `GTS_MAX_CONCURRENT` — integer (e.g., `2` for memory-constrained systems)

### Gateway Functions

```go
// WalkAndParse walks root, detects languages, and streams parsed files.
// Cancel via context to stop early. Call the returned function after the
// channel closes to get summary statistics.
func WalkAndParse(ctx context.Context, root string, policy ParsePolicy) (<-chan ParsedFile, func() WalkStats)
```

Symlinks are not followed. `filepath.WalkDir` visits the symlink entry itself; if it does not point to a regular file, it is skipped.

Binary files are detected by a NUL byte check in the first 8KB of content. Files identified as binary are skipped with no error.

### Internal Pipeline

The gateway is a single streaming pipeline, not a batch system. Files process in directory walk order. A semaphore controls concurrency, and large files temporarily acquire all slots.

```
walk directory
  → for each file:
      → detect language (skip if none, skip if extension in SkipExtensions)
      → call ShouldParse(path, size, modTime) if set (skip if false)
      → binary check: read first 8KB, skip if NUL found
      → check file size
      → if normal (< threshold):
          acquire 1 semaphore slot
          go:
            read file → parse → send to channel
            release semaphore slot (AFTER channel send completes)
      → if large (>= threshold):
          fire OnProgress("large_file", path, size)
          acquire ALL semaphore slots (drain concurrent work)
          read file → parse → send to channel
          release ALL semaphore slots
  → close channel
```

**Deadlock prevention:** Workers release their semaphore slot AFTER the channel send completes, not before. This means a worker holds its slot while blocked on channel send. The channel buffer is `MaxConcurrent + 1`, which guarantees at least one slot can always drain: if N workers are in flight and one finishes, it can always send to the channel (buffer has room for N+1 items but only N workers exist), then release its slot. This prevents the deadlock where all workers block on channel send while the walker tries to acquire all slots.

**Memory model:** Peak memory is bounded by `(MaxConcurrent + ChannelBuffer) * max_file_size_in_flight`. For normal files with the default policy (8 workers, buffer 9), peak is approximately 17 * largest-normal-file. For large files in solo mode, peak is 1 * large-file-size + buffer of already-sent results waiting for consumer. The consumer controls drain rate via how fast it processes and calls `Close()`.

**Backpressure:** The channel buffer bounds how far ahead the gateway parses. If the consumer is slow, workers block on channel send, which blocks them from releasing semaphore slots, which blocks new workers from starting. Memory stays bounded.

**Cancellation:** Context cancellation stops the walk and prevents new workers from starting. In-flight workers complete their current file (to release resources cleanly), send to channel, and release their slots. The channel is closed after all workers finish.

### Progress Reporting

The `OnProgress` callback fires at key moments:

| Phase | When | Message example |
|-------|------|-----------------|
| `walking` | Walk starts | `"scanning directory..."` |
| `walk_complete` | Walk finishes, Total known | `"found 342 files"` |
| `parsing` | First normal file starts | `"parsing (8 concurrent)..."` |
| `large_file` | Before a large file parse | `"parsing large file: package-lock.json (4.2MB)..."` |
| `done` | Channel closes | `"done: 342 files parsed, 3 large"` |

The `Total` field is accurate after the `walk_complete` event. During `walking`, it is 0.

The callback is optional. If nil, the gateway runs silently. CLI tools set it to print to stderr. Libraries leave it nil.

### Error Handling

Two kinds of failures come through the channel as `ParsedFile` with `Err != nil`:

1. **I/O errors** (`IsRead == false`): file disappeared, permission denied, read failure. `Tree`, `Source` are nil.
2. **Parse errors** (`IsRead == true`): tree-sitter parse failed or produced only errors. `Source` is populated (the file was read), `Tree` may be non-nil (partial parse).

Both kinds require `Close()` to be called. The gateway does not abort on individual file errors — it continues to the next file. The consumer decides how to handle errors (log, collect, abort).

Files skipped by policy (wrong extension, skip dir, `ShouldParse` returned false, binary) never appear on the channel.

### ShouldParse Hook

The `ShouldParse` callback allows consumers to pre-filter files before the gateway reads them:

```go
policy.ShouldParse = func(path string, size int64, modTime time.Time) bool {
    return cache.IsChanged(path, size, modTime) // only parse changed files
}
```

This is how gts-suite's incremental builds avoid re-parsing unchanged files. The gateway calls `ShouldParse` after language detection but before reading the file. Files that return `false` are counted in `WalkStats.FilesFiltered` and do not appear on the channel.

### Consumer Migration

Each filesystem-walking consumer replaces its walk-parse loop with `WalkAndParse`:

**gts-suite** (`pkg/index/builder.go`):
```go
policy := grammars.DefaultPolicy()
policy.ShouldParse = func(path string, size int64, modTime time.Time) bool {
    return !cache.IsUnchanged(path, size, modTime)
}
results, stats := grammars.WalkAndParse(ctx, root, policy)
for file := range results {
    if file.Err != nil {
        index.AddError(file.Path, file.Err)
        file.Close()
        continue
    }
    summary := parser.ExtractSymbols(file.Tree, file.Lang, file.Source)
    index.AddFile(file.Path, summary)
    file.Close()
}
```

Deletes: `collectCandidates`, `parseFiles`, `parseTask`, `parseResult`, `sourceCandidate`, `indexWorkerCount`. ~200 lines removed.

**graft structural grep** (`pkg/repo/structural_grep.go`):
```go
policy := grammars.DefaultPolicy()
results, _ := grammars.WalkAndParse(ctx, r.RootDir, policy)
for file := range results {
    if file.Err != nil { file.Close(); continue }
    matches, _ := tsgrep.Match(file.Lang, pattern, file.Source)
    // enrich with entity context
    file.Close()
}
```

**orchard grep service** (`internal/service/codeintel_grep.go`):
Same pattern — walk repo files, stream through gateway, match, close, return.

**graft entity extraction** (`pkg/entity/`): The staging pipeline reads from the object store, not the filesystem. It does NOT use `WalkAndParse`. The data format denylist in entity extraction stays as a consumer-level concern — it controls whether extracted entities are meaningful, which is separate from whether the file can be parsed.

### What Moves, What Stays

| Concern | Current Location | After Gateway |
|---------|-----------------|---------------|
| File walking + language detection | Each consumer | Gateway |
| Concurrent parsing with bounds | graft entity extraction only | Gateway |
| Large file throttling | None | Gateway |
| Progress reporting | None | Gateway |
| Data format denylist (entity extraction) | graft `pkg/entity/` | Stays in graft (consumer concern) |
| `GRAFT_ENTITY_MEMORY_MB` | graft `pkg/entity/` | Replaced by `GTS_MAX_CONCURRENT` |
| `GRAFT_ENTITY_WORKERS` | graft `pkg/entity/` | Replaced by `GTS_MAX_CONCURRENT` |
| Parser pool management | graft `pkg/entity/` | Gateway (semaphore + BoundTree pooling) |
| `indexWorkerCount` | gts-suite `pkg/index/` | Replaced by `GTS_MAX_CONCURRENT` |
| `collectCandidates`/`parseFiles` | gts-suite `pkg/index/` | Deleted (gateway replaces) |

Deprecation: `GRAFT_ENTITY_MEMORY_MB` and `GRAFT_ENTITY_WORKERS` env vars will be honored during a transition period, with a stderr warning pointing users to `GTS_MAX_CONCURRENT` and `GTS_LARGE_FILE_THRESHOLD`.

---

## Sequencing

### Phase 1: Build the gateway in gotreesitter
- `ParsePolicy`, `ParsedFile`, `ProgressEvent`, `WalkStats` types
- `DefaultPolicy()` with env var overrides
- `WalkAndParse(ctx, root, policy)` implementation
- `ParsedFile.Close()` ownership protocol
- Binary file detection (NUL byte check)
- `ShouldParse` hook
- Tests: normal files, large files, progress callback, cancellation, error handling, backpressure, Close lifecycle, binary skip, ShouldParse filtering

### Phase 2: Migrate consumers
- gts-suite: replace `collectCandidates`/`parseFiles` with `WalkAndParse` + `ShouldParse` for incremental
- graft structural grep: replace walk loop
- orchard grep service: replace walk loop

### Phase 3: Remove duplicated safety logic
- Remove graft's `GRAFT_ENTITY_WORKERS` and parser pool management (replaced by gateway)
- Remove gts-suite's `indexWorkerCount` and related types
- Deprecation warnings for old env vars
- Keep graft's data format denylist (consumer-level entity extraction concern)
