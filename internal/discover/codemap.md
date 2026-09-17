# internal/discover/

## Responsibility
Report discovery (plan §5, spec §14): find candidate XMLs, decode them in
parallel, name modules, filter, and compute run-level staleness.

## Design
- Walk with pruning: `SkipDirs` (`.git`, `node_modules`, …) pruned;
  entering a `ReportDirs` dir (`test-results`, `surefire-reports`,
  `failsafe-reports`) collects `*.xml` recursively, skipping child `binary`,
  then `filepath.SkipDir`. `--path` globs bypass the walk. Source-file
  basenames (`sourceExts`) are indexed into `FileIndex` during the same
  walk (trim's project-frame evidence).
- `decodeAll`: bounded worker pool (NumCPU, cap 8), results written to
  per-index slots, merged sorted by path (D10 determinism).
- `moduleAndSource`: strip `…/build|target/<REPORT_DIR>[/…]` from the
  report dir; remainder = module (empty → repo-root basename);
  `sourceLabel` → gradle|surefire|failsafe. `findRepoRoot` walks up to
  `.git`, else `--root`.
- Filters after decode: `--source`, `--newer-than` (suite timestamp else
  mtime; nothing left → `ErrNoReports`). Staleness: newest suite timestamp,
  mtime fallback; age > `MaxAge` → `Stale` + `warn STALE newest report is …`
  (D8-formatted). Warning paths are repo-relative (`relToRepo`).

## Flow
`Discover(opts)` → `collectCandidates` → `decodeAll` (each worker:
`decode.DecodeFile` + stat mtime) → warnings/filters → `Result{Reports,
Warnings, RunTime, Age, Stale, Files}`. `decoded == 0` →
`ErrNoneParseable` (exit 4); zero candidates / zero selected →
`ErrNoReports` (exit 2).

## Integration
Consumed only by `cli.prepare`/`runDiscover`. Depends on `clock` (now),
`decode`, `model`. `FileIndex` satisfies `trim.FileIndex` (interface:
`Has(string) bool`).
