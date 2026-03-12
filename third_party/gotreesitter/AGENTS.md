## Agent Workflow: GLR Parity + Performance

This file defines how agents should work in this repo.

### 1) Non-negotiables
- Do not use `gts-suite` for inspection/profiling (it can trigger memory pressure and process kills).
- Keep correctness gating separate from performance gating.
- Prefer reproducible runs over ad-hoc spot checks.

### 2) Correctness Gate (must stay green)
Run before and after performance changes:
- `go test ./...`
- Parity-focused suites under `cgo_harness` when applicable.

When changing GLR/incremental logic, require parity validation first, then perf validation.

### 3) Standard Perf Loop
Use this loop for optimization work:
1. Baseline with stable settings.
2. Make one focused change.
3. Re-run the same benchmarks.
4. Keep changes only if `benchstat` improves target metrics without correctness regressions.

Stable settings:
- `GOMAXPROCS=1`
- `-count=10`
- `-benchtime=750ms`
- `-benchmem`

Primary bench trio:
- `BenchmarkGoParseFullDFA`
- `BenchmarkGoParseIncrementalSingleByteEditDFA`
- `BenchmarkGoParseIncrementalNoEditDFA`

### 4) Metrics and Targets
Track at minimum:
- `ns/op`
- `B/op`
- `allocs/op`
- Max RSS on large-file runs (`/usr/bin/time -v`)

Current readiness targets for PR merge:
- Full parse: within `2x` of C/cgo baseline on agreed macro workload.
- Incremental single-byte edits: at or better than C/cgo baseline.
- No memory strangulation or crash behavior under large corpus runs.

### 5) Attribution for Incremental Hot Path
When profiling incremental edits, split attribution into:
- `Tree.Edit(edit)`
- reuse-cursor/reuse-selection work
- reparse/rebuild work

Use profiled runs to decide whether the next win comes from:
- reuse/invalidation scope,
- GLR/recovery/materialization cost,
- or allocator sizing/retention.

### 6) Commit Discipline
- Keep commits scoped and bisectable.
- Drop scratch/debug artifacts before commit.
- Use project commit flow:
  - `git add ...`
  - `buckley commit --yes --minimal-output`

### 7) Gate Presets
Correctness preset:
- `go test ./...`
- `go test ./grammars -run '^TestTop50ParseSmokeNoErrors$' -count=1 -v`
- `cd cgo_harness && go test . -tags treesitter_c_parity -run '^TestParityFreshParse$|^TestParityHasNoErrors$|^TestParityIssue3Repros$|^TestParityGLRCanaryGo$' -count=1 -v`
- `cd cgo_harness && go test . -tags treesitter_c_parity -run '^TestParityCorpusFreshParse$' -count=1 -v`

Perf preset (stable settings):
- `GOMAXPROCS=1 go test . -run '^$' -bench 'BenchmarkGoParseFullDFA|BenchmarkGoParseIncrementalSingleByteEditDFA|BenchmarkGoParseIncrementalNoEditDFA' -benchmem -count=10 -benchtime=750ms`

Full-parse non-truncation probe:
- `GOT_PARSE_NODE_LIMIT_SCALE=3` may be used for diagnostic full-parse runs when default node budget truncates benchmark/parity cases.
- `GOT_GLR_MAX_STACKS=...` overrides the default GLR stack cap (default 6).
