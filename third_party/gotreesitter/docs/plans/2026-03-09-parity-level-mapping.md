# Parity Level Mapping And Current Status

This document maps the current repo test and harness surfaces onto the L0-L8
parity framework discussed for PR #8.

Status snapshot: branch head `043010f` on 2026-03-09.

## Merge Target

PR #8 merge target:

- L0-L4 green
- source-only real corpus inputs
- branch defaults only
- no L0-L4 allowlist

L5-L8 should continue as ratcheted streams that must not regress, but are not
the merge blocker.

## Level Mapping

### L0: No Crash

Definition:

- parse completes without panic, kill, or hard test timeout

Current repo surfaces:

- `cgo_harness.TestParityFreshParse`
- `cgo_harness.TestParityCorpusFreshParse`
- direct one-file Docker probes via `cgo_harness/cmd/corpus_parity`
- parser/runtime probes used in focused debugging

Current status:

- Green on current curated/default smoke + synthetic corpus gates.
- Not yet formalized as a full source-only real-corpus all-language board.

### L1: No Error Nodes

Definition:

- for the curated valid-input set, Go trees should not carry error nodes when C
  does not

Current repo surfaces:

- `cgo_harness.TestParityHasNoErrors`
- `grammars.TestTop50ParseSmokeNoErrors`

Current status:

- Green on current curated smoke gate.
- Not yet promoted to a full source-only real-corpus board.

### L2: Smoke Structural Parity

Definition:

- smoke samples structurally match the pinned C reference

Current repo surfaces:

- `cgo_harness.TestParityFreshParse`
- `cgo_harness.TestParityIssue3Repros`
- `cgo_harness.TestParityGLRCanaryGo`
- `cgo_harness.TestParityGateCoverageRatchet`

Current status:

- Green.
- `knownDegradedStructural` is now empty.
- This is the strongest and cleanest automated parity layer in the repo today.

### L3: Medium Real Files

Definition:

- medium source-only real-corpus files structurally match C at defaults

Current repo surfaces:

- `cgo_harness/cmd/build_real_corpus`
- manifest output from `build_real_corpus`
- `cgo_harness/real_corpus_quality_test.go`
- `cgo_harness/cmd/corpus_parity`
- targeted real-world tests such as `parity_scala_realworld_test.go`

Current status:

- Builder and manifest plumbing exist.
- Source-only selection is working.
- Multiple language-specific real-world wins have already landed.
- There is not yet a single automated all-language L3 board/gate keyed off the
  real-corpus manifest.

### L4: Large Real Files

Definition:

- large source-only real-corpus files structurally match C at defaults

Current repo surfaces:

- `build_real_corpus` large bucket
- `corpus_parity` one-file and batch runs
- bounded direct parse/runtime probes
- selected real-world benchmarks/probes in `cgo_harness`

Current status:

- Infrastructure exists, but the blocking board does not yet.
- Large-file correctness and large-file cost are still partially mixed in ad
  hoc workflows.

### L5a: Highlight Query/Init Correctness

Definition:

- query load/compile/init/oracle path works correctly

Current repo surfaces:

- `cgo_harness.TestParityHighlight`
- `cgo_harness.TestParityHighlightAllGrammars`
- YAML highlight corpus tests

Current status:

- Green in the current CI-shaped gate.
- Still combined with capture parity in one suite; not yet split into L5a/L5b.

### L5b: Highlight Capture Parity

Definition:

- highlight captures match C within ratcheted thresholds

Current repo surfaces:

- `cgo_harness/parity_highlight_test.go`
- `cgo_harness/parity_yaml_corpus_test.go`

Current status:

- Ratcheted and green under current thresholds.
- Still has a `knownDegradedHighlight` tolerance list, so not a zero-debt
  stream yet.

### L6a: Incremental Correctness

Definition:

- incremental tree matches fresh tree and C reference on valid edit sites

Current repo surfaces:

- `cgo_harness.TestParityIncrementalParse`
- parser-specific incremental tests in `parser_go_test.go`

Current status:

- Green on the current curated smoke suite.
- Known skip/info paths still exist for a few languages where no safe edit site
  is found under the current selector.

### L6b: Incremental Cost

Definition:

- incremental harness/runtime remains tractable

Current repo surfaces:

- `benchmark_test.go`
- parser profiling tests
- targeted incremental runtime probes

Current status:

- Partially ratcheted.
- `tsx` harness-cost noise has already been removed, but this is not yet a
  first-class board.

### L7: Field Names / Field Shape

Definition:

- field-bearing trees agree on field ownership, not just raw structure

Current repo surfaces:

- reducer and field tests in `parser_test.go`
- field-specific parity repros in `parity_issue3_test.go`
- targeted regression tests in grammar-specific files

Current status:

- Substantial unit and regression coverage exists.
- There is no dedicated field-only corpus parity gate yet.

### L8: Recovery Shape

Definition:

- malformed-input recovery shape is controlled and ratcheted

Current repo surfaces:

- `cgo_harness/parity_breaker_test.go`
- parser recovery tests
- focused malformed-input probes during debugging

Current status:

- Mostly opt-in and diagnostic.
- Not yet a regularly enforced ratchet stream.

## Current Progress Toward L4 100%

If "L4 100%" means the intended merge contract of:

- L0-L4 green
- source-only real corpus
- default settings
- no allowlist

then the current position is:

- L0: partially formalized, effectively green on current curated gates
- L1: partially formalized, effectively green on current curated gates
- L2: complete and green
- L3: infrastructure-ready, but not yet materialized as a full board
- L4: infrastructure-ready, but not yet materialized as a full board

In practical terms:

- The branch is at 100% for current L2 smoke structural parity.
- The branch is not yet at a measured 100% for L3/L4 because the all-language
  source-only/default board has not been wired up as a single artifact/gate.

So the honest overall read is:

- Smoke parity debt: cleared
- Real-corpus parity infrastructure: ready
- Real-corpus L3/L4 ratchet board: next required step

## Immediate Next Step

The next concrete move should be:

1. Treat `build_real_corpus` manifest as the source of truth.
2. Add a board-producing runner that joins manifest entries with `corpus_parity`
   results.
3. Classify each source-only entry into L3 or L4 by bucket.
4. Fail the board only for L0-L4 once the source-only/default contract is fully
   encoded.

That turns the current "we know the infrastructure exists" state into an
explicit progress board toward L4 100%.
