## What does this PR do?

<!-- One or two sentences. What problem does it solve or what capability does it add? -->

## Why this approach?

<!-- Brief rationale. If there were alternatives you considered and rejected, say why. -->

## Correctness

- [ ] All existing tests pass (`go test ./...`)
- [ ] New tests added for new behavior
- [ ] 206/206 grammars still smoke-parse (`go test ./grammars/ -run TestSupportedLanguagesParseSmoke`)
- [ ] Correctness snapshots still match (`go test ./grammars/ -run TestCorrectness`)
- [ ] CGo parity holds for affected languages (`go test -tags 'cgo treesitter_c_parity' ./cgo_harness/`)

<!-- If any of these don't apply, explain why. -->

## Performance

- [ ] Ran benchmarks before and after (`go test -bench=. -benchmem -count=5`)
- [ ] No regressions in ns/op, B/op, or allocs/op beyond noise
- [ ] If this is a hot path change, included benchmark numbers in the PR

<!-- Paste before/after benchstat output if relevant. -->

## Maintainability

- [ ] No unnecessary abstractions — code does what it needs to and nothing more
- [ ] No speculative features or "while I'm here" cleanup outside the PR's scope
- [ ] Scanner ports follow existing conventions (MarkEnd before SetResultSymbol, symbol resolution via ExternalSymbols)
- [ ] No new dependencies without justification

## Self-review

Review your own diff before requesting review from others. Walk through it as if you're seeing it for the first time. Leave comments on your own PR pointing out the notable parts — the tricky bits, the non-obvious decisions, anything a reviewer's eye would naturally land on. Comment on what you think the crux of the solution is and why.

- [ ] Reviewed my own diff end to end
- [ ] Left comments on notable sections and the crux of the solution
- [ ] Called out anything I'm unsure about or want a second opinion on

## Due diligence

- [ ] Read the code you're changing before changing it
- [ ] Checked that similar patterns exist elsewhere in the codebase and stayed consistent
- [ ] If touching the parser or lexer: tested with at least 3 diverse grammars
- [ ] If adding a grammar or scanner: verified against upstream C output
