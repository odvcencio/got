# Changelog

## v0.4.0

### Memory-Safe Entity Extraction

Entity extraction during `graft add` has been completely reworked for reliability. Large repositories that previously caused out-of-memory crashes now complete safely.

**Two-phase add pipeline** — Blob staging (parallel, I/O-bound) runs separately from entity extraction (bounded concurrency). File contents are released between phases to minimize memory pressure.

**Data format denylist** — Pure data files (JSON, YAML, TOML, INI, CSV) above 256KB are automatically skipped for entity extraction. Small config files like `package.json` still get entities. All code files are always extracted regardless of size.

**Parser pool reuse** — Entity extraction now uses `ParseFilePooled` with per-language parser pools, eliminating repeated parser allocation overhead during bulk operations.

**New CLI flags:**
- `--skip-entities` — Skip all entity extraction (fast bulk import)
- `--force-entities` — Force extraction on data format files above threshold

**New environment variables:**
- `GRAFT_ENTITY_WORKERS` — Max concurrent entity extraction workers (default: 2)
- `GRAFT_ENTITY_MEMORY_MB` — Source-bytes semaphore budget in MB (default: 64)

### Git Bridge Fix

`graft init` in a git repository no longer runs entity extraction during the initial import. This was loading WASM grammars for every git-tracked file and discarding the results, causing multi-GB memory usage and minutes-long init times. Init now completes in under 2 seconds.

### Requires

- gotreesitter v0.7.0+

## v0.3.0

Initial public release with structural merge, entity-aware diff, coordination protocol, and git bridge.
