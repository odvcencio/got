# Parse Gateway Phase 1: gotreesitter Implementation

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a streaming parse gateway in gotreesitter's `grammars` package that walks directories, detects languages, and streams parsed files over a channel with memory-safe concurrency.

**Architecture:** A `WalkAndParse` function streams `ParsedFile` results over a channel. A semaphore controls concurrency — normal files parse concurrently, large files temporarily drain all slots and parse solo. `ParsedFile.Close()` releases tree memory. `ShouldParse` hook lets consumers pre-filter. Progress callbacks report status for CLI feedback.

**Tech Stack:** Go 1.25, gotreesitter grammars package, `filepath.WalkDir`, semaphore pattern

**Spec:** `docs/superpowers/specs/2026-03-16-parse-gateway-design.md` (in the graft repo)

**Repo:** `~/work/gotreesitter` (module: `github.com/odvcencio/gotreesitter`)

### gotreesitter API Reference

Key existing types the gateway builds on:
```go
// grammars package
type LangEntry struct { ... }
func DetectLanguage(filename string) *LangEntry
func (e *LangEntry) Language() *gotreesitter.Language

// BoundTree — returned by ParseFilePooled
type BoundTree struct { ... }
func (bt *BoundTree) RootNode() *gotreesitter.Node
func (bt *BoundTree) Release()

// ParseFilePooled(filename string, source []byte) (*BoundTree, error)
```

Explore the actual grammars package before implementing — the types above are from earlier exploration and may have drifted.

---

## Chunk 1: Core Types + DefaultPolicy

### Task 1: Types and Policy

**Files:**
- Create: `grammars/gateway.go`
- Create: `grammars/gateway_test.go`

- [ ] **Step 1: Write tests for DefaultPolicy**

```go
// grammars/gateway_test.go
package grammars

import (
    "os"
    "runtime"
    "testing"
)

func TestDefaultPolicy(t *testing.T) {
    p := DefaultPolicy()
    if p.LargeFileThreshold != 256*1024 {
        t.Errorf("LargeFileThreshold = %d, want %d", p.LargeFileThreshold, 256*1024)
    }
    if p.MaxConcurrent < 1 {
        t.Error("MaxConcurrent should be >= 1")
    }
    if p.ChannelBuffer != p.MaxConcurrent+1 {
        t.Errorf("ChannelBuffer = %d, want MaxConcurrent+1 = %d", p.ChannelBuffer, p.MaxConcurrent+1)
    }
    if len(p.SkipDirs) == 0 {
        t.Error("SkipDirs should have defaults")
    }
}

func TestDefaultPolicy_EnvOverrides(t *testing.T) {
    os.Setenv("GTS_LARGE_FILE_THRESHOLD", "1048576")
    os.Setenv("GTS_MAX_CONCURRENT", "2")
    defer os.Unsetenv("GTS_LARGE_FILE_THRESHOLD")
    defer os.Unsetenv("GTS_MAX_CONCURRENT")

    p := DefaultPolicy()
    if p.LargeFileThreshold != 1048576 {
        t.Errorf("LargeFileThreshold = %d, want 1048576", p.LargeFileThreshold)
    }
    if p.MaxConcurrent != 2 {
        t.Errorf("MaxConcurrent = %d, want 2", p.MaxConcurrent)
    }
}

func TestParsedFile_Close(t *testing.T) {
    pf := &ParsedFile{
        Source: []byte("hello"),
        // Tree is nil — Close should handle gracefully
    }
    pf.Close() // should not panic
    if pf.Source != nil {
        t.Error("Source should be nil after Close")
    }
    pf.Close() // double close should not panic
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd ~/work/gotreesitter && go test ./grammars/ -v -run "TestDefault|TestParsedFile" -count=1`
Expected: FAIL (types not defined)

- [ ] **Step 3: Implement types and DefaultPolicy**

Create `grammars/gateway.go` with all types from the spec: `ParsePolicy`, `ParsedFile`, `ProgressEvent`, `WalkStats`, `DefaultPolicy()`, `ParsedFile.Close()`. Follow the spec exactly for field names, defaults, and env var handling.

Key implementation notes:
- `DefaultPolicy()` reads `GTS_LARGE_FILE_THRESHOLD` and `GTS_MAX_CONCURRENT` from env
- `ChannelBuffer` defaults to `MaxConcurrent + 1` (deadlock prevention)
- `Close()` releases `BoundTree` if non-nil, nils out `Source`
- `Close()` is safe to call multiple times

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd ~/work/gotreesitter && go test ./grammars/ -v -run "TestDefault|TestParsedFile" -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
cd ~/work/gotreesitter && graft add grammars/ && buckley commit --yes -min -graft
```

---

## Chunk 2: WalkAndParse Core Implementation

### Task 2: File Walking + Language Detection

**Files:**
- Modify: `grammars/gateway.go`
- Modify: `grammars/gateway_test.go`

- [ ] **Step 1: Write tests for basic walking**

```go
func TestWalkAndParse_BasicGo(t *testing.T) {
    dir := t.TempDir()
    writeTestFile(t, dir, "main.go", `package main
func Hello() {}
func World() {}
`)
    writeTestFile(t, dir, "README.md", "# test") // no grammar, should be skipped

    ctx := context.Background()
    results, stats := WalkAndParse(ctx, dir, DefaultPolicy())

    var files []string
    for file := range results {
        if file.Err != nil {
            t.Errorf("unexpected error for %s: %v", file.Path, file.Err)
        }
        files = append(files, file.Path)
        // Verify tree is usable
        if file.Tree == nil {
            t.Errorf("nil tree for %s", file.Path)
        }
        file.Close()
    }

    s := stats()
    if s.FilesParsed != 1 {
        t.Errorf("FilesParsed = %d, want 1", s.FilesParsed)
    }
    if len(files) != 1 || files[0] != "main.go" {
        t.Errorf("files = %v, want [main.go]", files)
    }
}

func TestWalkAndParse_SkipDirs(t *testing.T) {
    dir := t.TempDir()
    writeTestFile(t, dir, "main.go", "package main")
    os.MkdirAll(filepath.Join(dir, "vendor"), 0o755)
    writeTestFile(t, dir, "vendor/lib.go", "package lib")
    os.MkdirAll(filepath.Join(dir, ".git"), 0o755)
    writeTestFile(t, dir, ".git/config.go", "package git")

    results, stats := WalkAndParse(context.Background(), dir, DefaultPolicy())
    for file := range results { file.Close() }

    s := stats()
    if s.FilesParsed != 1 {
        t.Errorf("FilesParsed = %d, want 1 (only main.go)", s.FilesParsed)
    }
}

func TestWalkAndParse_SkipExtensions(t *testing.T) {
    dir := t.TempDir()
    writeTestFile(t, dir, "app.js", "function hello() {}")
    writeTestFile(t, dir, "app.min.js", "function hello(){}")

    results, _ := WalkAndParse(context.Background(), dir, DefaultPolicy())
    count := 0
    for file := range results { count++; file.Close() }
    if count != 1 {
        t.Errorf("expected 1 file (app.js only), got %d", count)
    }
}

func TestWalkAndParse_ShouldParse(t *testing.T) {
    dir := t.TempDir()
    writeTestFile(t, dir, "a.go", "package a")
    writeTestFile(t, dir, "b.go", "package b")
    writeTestFile(t, dir, "c.go", "package c")

    policy := DefaultPolicy()
    policy.ShouldParse = func(path string, size int64, modTime time.Time) bool {
        return filepath.Base(path) == "b.go" // only parse b.go
    }

    results, stats := WalkAndParse(context.Background(), dir, policy)
    var files []string
    for file := range results { files = append(files, file.Path); file.Close() }

    s := stats()
    if len(files) != 1 || files[0] != "b.go" {
        t.Errorf("files = %v, want [b.go]", files)
    }
    if s.FilesFiltered != 2 {
        t.Errorf("FilesFiltered = %d, want 2", s.FilesFiltered)
    }
}

// Helper
func writeTestFile(t *testing.T, dir, name, content string) {
    t.Helper()
    path := filepath.Join(dir, name)
    os.MkdirAll(filepath.Dir(path), 0o755)
    if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
        t.Fatal(err)
    }
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd ~/work/gotreesitter && go test ./grammars/ -v -run "TestWalkAndParse" -count=1`
Expected: FAIL (WalkAndParse not defined)

- [ ] **Step 3: Implement WalkAndParse**

The core implementation in `grammars/gateway.go`:

1. Walk directory with `filepath.WalkDir`
2. Skip dirs in `SkipDirs`, skip extensions in `SkipExtensions`
3. Detect language with `DetectLanguage(path)`
4. Call `ShouldParse` if set
5. Get file info for size
6. Use semaphore pattern for concurrency:
   - Normal files: acquire 1 slot, launch goroutine, parse, send, release after send
   - Large files: acquire all N slots, parse inline, send, release all
7. Binary detection: read first 8KB, check for NUL byte
8. Parse using `ParseFilePooled` (which handles token source factory)
9. Send `ParsedFile` on channel
10. Track stats atomically
11. Close channel when done, finalize stats

**Critical implementation details:**
- Channel buffer = `policy.ChannelBuffer` (default MaxConcurrent+1)
- Workers release semaphore AFTER channel send (deadlock prevention)
- Use `sync.WaitGroup` to track in-flight goroutines
- Context cancellation checked before each new file
- Large file drain: loop acquiring all slots, parse, loop releasing all slots

- [ ] **Step 4: Run tests and iterate**

Run: `cd ~/work/gotreesitter && go test ./grammars/ -v -run "TestWalkAndParse" -count=1`
Expected: iterate until all pass. The `BoundTree` type and `ParseFilePooled` API may need adjustment — explore grammars package to confirm exact API.

- [ ] **Step 5: Commit**

```bash
cd ~/work/gotreesitter && graft add grammars/ && buckley commit --yes -min -graft
```

---

### Task 3: Large File Throttling + Progress

**Files:**
- Modify: `grammars/gateway.go`
- Modify: `grammars/gateway_test.go`

- [ ] **Step 1: Write tests for large file handling**

```go
func TestWalkAndParse_LargeFileThrottled(t *testing.T) {
    dir := t.TempDir()
    // Create a "large" file (set threshold low for testing)
    writeTestFile(t, dir, "small.go", "package small")
    largeContent := "package large\n" + strings.Repeat("// padding\n", 1000)
    writeTestFile(t, dir, "large.go", largeContent)

    policy := DefaultPolicy()
    policy.LargeFileThreshold = 100 // 100 bytes — small.go is under, large.go is over

    var progressEvents []ProgressEvent
    policy.OnProgress = func(e ProgressEvent) {
        progressEvents = append(progressEvents, e)
    }

    results, stats := WalkAndParse(context.Background(), dir, policy)
    for file := range results { file.Close() }

    s := stats()
    if s.LargeFiles != 1 {
        t.Errorf("LargeFiles = %d, want 1", s.LargeFiles)
    }
    if s.FilesParsed != 2 {
        t.Errorf("FilesParsed = %d, want 2", s.FilesParsed)
    }

    // Check progress events
    hasLargeEvent := false
    for _, e := range progressEvents {
        if e.Phase == "large_file" {
            hasLargeEvent = true
        }
    }
    if !hasLargeEvent {
        t.Error("expected a large_file progress event")
    }
}

func TestWalkAndParse_ProgressCallback(t *testing.T) {
    dir := t.TempDir()
    writeTestFile(t, dir, "a.go", "package a")

    var phases []string
    policy := DefaultPolicy()
    policy.OnProgress = func(e ProgressEvent) {
        phases = append(phases, e.Phase)
    }

    results, _ := WalkAndParse(context.Background(), dir, policy)
    for file := range results { file.Close() }

    // Should have at least walking and done
    if len(phases) < 2 {
        t.Errorf("expected >= 2 progress events, got %d: %v", len(phases), phases)
    }
}
```

- [ ] **Step 2: Run tests**

Run: `cd ~/work/gotreesitter && go test ./grammars/ -v -run "TestWalkAndParse_(LargeFile|Progress)" -count=1`

- [ ] **Step 3: Implement large file handling and progress**

In the walk loop, after getting file size:
- If `size >= policy.LargeFileThreshold`: fire `OnProgress("large_file")`, acquire all semaphore slots, parse inline, release all
- Fire `OnProgress("walking")` at start, `OnProgress("walk_complete")` after walk, `OnProgress("done")` before closing channel
- Increment `stats.LargeFiles` for throttled files

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd ~/work/gotreesitter && go test ./grammars/ -v -run "TestWalkAndParse" -count=1`
Expected: all PASS

- [ ] **Step 5: Commit**

```bash
cd ~/work/gotreesitter && graft add grammars/ && buckley commit --yes -min -graft
```

---

### Task 4: Cancellation + Error Handling + Binary Detection

**Files:**
- Modify: `grammars/gateway.go`
- Modify: `grammars/gateway_test.go`

- [ ] **Step 1: Write tests**

```go
func TestWalkAndParse_Cancellation(t *testing.T) {
    dir := t.TempDir()
    for i := 0; i < 20; i++ {
        writeTestFile(t, dir, fmt.Sprintf("f%d.go", i), fmt.Sprintf("package f%d", i))
    }

    ctx, cancel := context.WithCancel(context.Background())
    results, stats := WalkAndParse(ctx, dir, DefaultPolicy())

    count := 0
    for file := range results {
        file.Close()
        count++
        if count >= 3 {
            cancel() // cancel after 3 files
        }
    }

    s := stats()
    // Should have processed fewer than 20 files
    if s.FilesParsed >= 20 {
        t.Errorf("expected cancellation to stop early, got %d files", s.FilesParsed)
    }
}

func TestWalkAndParse_BinaryFileSkipped(t *testing.T) {
    dir := t.TempDir()
    writeTestFile(t, dir, "good.go", "package good")
    // Write a binary file with .go extension
    os.WriteFile(filepath.Join(dir, "binary.go"), []byte("package\x00binary"), 0o644)

    results, stats := WalkAndParse(context.Background(), dir, DefaultPolicy())
    var files []string
    for file := range results { files = append(files, file.Path); file.Close() }

    s := stats()
    if s.FilesParsed != 1 {
        t.Errorf("FilesParsed = %d, want 1 (binary should be skipped)", s.FilesParsed)
    }
}

func TestWalkAndParse_ReadError(t *testing.T) {
    dir := t.TempDir()
    writeTestFile(t, dir, "good.go", "package good")
    // Create unreadable file
    path := filepath.Join(dir, "bad.go")
    os.WriteFile(path, []byte("package bad"), 0o000)
    defer os.Chmod(path, 0o644) // cleanup

    results, _ := WalkAndParse(context.Background(), dir, DefaultPolicy())
    var errs int
    for file := range results {
        if file.Err != nil { errs++ }
        file.Close()
    }
    // Should get an error result for the unreadable file
    if errs < 1 {
        t.Error("expected at least one error for unreadable file")
    }
}
```

- [ ] **Step 2: Implement cancellation, binary detection, error handling**

- Check `ctx.Err()` before starting each file
- Binary detection: read first 8KB, scan for `\x00`
- Read errors: send `ParsedFile{Err: err, IsRead: false}`
- Parse errors: send `ParsedFile{Err: err, IsRead: true, Source: source}`

- [ ] **Step 3: Run all gateway tests**

Run: `cd ~/work/gotreesitter && go test ./grammars/ -v -run "TestWalkAndParse|TestDefault|TestParsedFile" -count=1`
Expected: all PASS

- [ ] **Step 4: Run full grammars test suite to check for regressions**

Run: `cd ~/work/gotreesitter && go test ./grammars/ -count=1`
Expected: all PASS

- [ ] **Step 5: Commit**

```bash
cd ~/work/gotreesitter && graft add grammars/ && buckley commit --yes -min -graft
```

---

## Chunk 3: Integration Tests + Backpressure

### Task 5: Backpressure + Multi-Language Integration

**Files:**
- Create: `grammars/gateway_integration_test.go`

- [ ] **Step 1: Write integration and backpressure tests**

```go
// grammars/gateway_integration_test.go
package grammars

import (
    "context"
    "sync/atomic"
    "testing"
    "time"
)

func TestWalkAndParse_MultiLanguage(t *testing.T) {
    dir := t.TempDir()
    writeTestFile(t, dir, "main.go", "package main\nfunc Hello() {}")
    writeTestFile(t, dir, "app.js", "function hello() {}")
    writeTestFile(t, dir, "script.py", "def hello():\n    pass")
    writeTestFile(t, dir, "lib.rs", "fn hello() {}")

    results, stats := WalkAndParse(context.Background(), dir, DefaultPolicy())
    langs := make(map[string]bool)
    for file := range results {
        if file.Err == nil && file.Lang != nil {
            langs[file.Lang.Name] = true
        }
        file.Close()
    }

    s := stats()
    if s.FilesParsed < 3 {
        t.Errorf("expected >= 3 languages parsed, got %d", s.FilesParsed)
    }
}

func TestWalkAndParse_Backpressure(t *testing.T) {
    dir := t.TempDir()
    for i := 0; i < 50; i++ {
        writeTestFile(t, dir, fmt.Sprintf("f%d.go", i), fmt.Sprintf("package f%d", i))
    }

    policy := DefaultPolicy()
    policy.MaxConcurrent = 2
    policy.ChannelBuffer = 3

    results, _ := WalkAndParse(context.Background(), dir, policy)

    // Slow consumer — sleep between reads
    count := 0
    for file := range results {
        file.Close()
        count++
        if count <= 5 {
            time.Sleep(10 * time.Millisecond) // slow start
        }
    }

    // Should complete all 50 files despite slow consumer
    if count != 50 {
        t.Errorf("expected 50 files, got %d", count)
    }
}

func TestWalkAndParse_CloseLifecycle(t *testing.T) {
    dir := t.TempDir()
    writeTestFile(t, dir, "main.go", "package main\nfunc Hello() {}")

    results, _ := WalkAndParse(context.Background(), dir, DefaultPolicy())
    for file := range results {
        // Verify tree is usable before close
        if file.Tree != nil {
            _ = file.Tree.RootNode()
        }
        file.Close()
        // After close, Tree and Source should be nil
        if file.Tree != nil {
            t.Error("Tree should be nil after Close")
        }
        if file.Source != nil {
            t.Error("Source should be nil after Close")
        }
    }
}

func TestWalkAndParse_EmptyDirectory(t *testing.T) {
    dir := t.TempDir()
    results, stats := WalkAndParse(context.Background(), dir, DefaultPolicy())
    count := 0
    for range results { count++ }
    s := stats()
    if count != 0 || s.FilesFound != 0 {
        t.Errorf("expected 0 files in empty dir, got count=%d found=%d", count, s.FilesFound)
    }
}
```

- [ ] **Step 2: Run integration tests**

Run: `cd ~/work/gotreesitter && go test ./grammars/ -v -run "TestWalkAndParse" -count=1`
Expected: all PASS

- [ ] **Step 3: Run full repo test suite**

Run: `cd ~/work/gotreesitter && go test ./... -count=1 2>&1 | tail -10`
Expected: all packages PASS

- [ ] **Step 4: Commit**

```bash
cd ~/work/gotreesitter && graft add grammars/ && buckley commit --yes -min -graft
```

---

### Task 6: Documentation

**Files:**
- Modify: `grammars/gateway.go` (add package-level doc)

- [ ] **Step 1: Add documentation to gateway.go**

Ensure all exported types and functions have godoc comments. Add a usage example in the `WalkAndParse` doc:

```go
// WalkAndParse walks root, detects languages, parses source files, and
// streams results over a channel. Normal files parse concurrently up to
// policy.MaxConcurrent. Large files (>= LargeFileThreshold) parse one at
// a time with progress reporting.
//
// The consumer MUST call Close() on each ParsedFile after processing.
//
// Example:
//
//     results, stats := grammars.WalkAndParse(ctx, ".", grammars.DefaultPolicy())
//     for file := range results {
//         if file.Err != nil { file.Close(); continue }
//         fmt.Printf("%s: %d bytes\n", file.Path, file.Size)
//         file.Close()
//     }
//     fmt.Printf("parsed %d files\n", stats().FilesParsed)
```

- [ ] **Step 2: Run go vet**

Run: `cd ~/work/gotreesitter && go vet ./grammars/`
Expected: clean

- [ ] **Step 3: Commit**

```bash
cd ~/work/gotreesitter && graft add grammars/ && buckley commit --yes -min -graft
```

---

## Summary

| Task | What | Files |
|------|------|-------|
| 1 | Types + DefaultPolicy | `grammars/gateway.go`, `grammars/gateway_test.go` |
| 2 | WalkAndParse core (walk, detect, parse, stream) | same files |
| 3 | Large file throttling + progress callbacks | same files |
| 4 | Cancellation + error handling + binary detection | same files |
| 5 | Integration tests (multi-lang, backpressure, lifecycle) | `grammars/gateway_integration_test.go` |
| 6 | Documentation | `grammars/gateway.go` |

**Dependencies:** Tasks 1→2→3→4 are sequential (each builds on the previous). Task 5 depends on 4. Task 6 is last.

**All work is in 3 files:** `grammars/gateway.go`, `grammars/gateway_test.go`, `grammars/gateway_integration_test.go`.
