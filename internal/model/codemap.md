# internal/model/

## Responsibility
Shared data vocabulary for the whole tool: the decoded report model, the
lazy captured-output reference, totals, warnings, and the canonical
duration/age formatters. Pure data + pure functions; no I/O.

## Design
- `Status` (passed/failed/error/skipped) with fixed declaration order
  (`Statuses`), `String`, `ParseStatus`.
- `Case` (ID = `ClassName#Name`, `OutRef` pairs, `Failure`, `SkipMessage`,
  `ReportPath` repo-relative), `Failure` (Kind/Type/Message/Trace),
  `Report` (`Aggregated` marks a `<testsuites>` root for dedup; newest suite
  `Timestamp` + `Mtime` feed staleness), `Suite` (minimal), `Totals` with
  `Verdict()` = zero failed+errors.
- `OutRef` is the lazy-output contract: `Path` + `Start`/`End` byte range in
  the sanitized stream; content is never held in the model.
- `Warning{Code, Message, Path}` (PARSE_ERROR, ENCODING, STALE, DEDUP,
  NOT_XML, OUTPUT_NOT_SEARCHED, MANY_MATCHES).
- `FormatDuration` (D7: <0.1s → ms; <60s → ≤2 decimals, ≥1 kept; ≥60s →
  `1m12s`) and `FormatAge` (D8: 42s/4m/3h12m/74d) live here as the single
  source of truth for both discover (STALE text) and render.

## Flow
Produced by `decode` (cases) and `discover` (module/source/mtime); consumed
by `agg` (selection/totals), `trim` (classification inputs), `cli`
(artifact conversion), `render` (display). Nothing in this package imports
siblings.

## Integration
Bottom of the dependency graph (only stdlib). Every other internal package
imports it; its API is the stability contract between pipeline phases.
