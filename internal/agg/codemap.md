# internal/agg/

## Responsibility
Case selection, totals, dedup, failure grouping, breakdowns and duration
statistics — everything computed from the decoded case universe
(plan §6, D10/D11).

## Design
- `Selection` (`Modules` exact-or-substring OR, `Statuses`, `Include`,
  `Exclude`) applied in `Select` BEFORE totals/verdict (D11); module is
  stamped from the report during selection. `Totals` always recomputed from
  cases, never XML attributes. `ParseStatuses` validates `--status`.
- `Dedupe` collapses `(classname, name)` duplicates via `reportRank`
  preference chain: non-aggregated root > later suite timestamp > later
  mtime > lexicographically greater path; rebuilds `Cases` in place and
  returns the collapsed count (drives `warn DEDUP`).
- `GroupFailures` groups failed/error cases by an injected fingerprint
  function (agg stays trim-agnostic); order count desc then fingerprint asc;
  member IDs in canonical order.
- `Percentile` (nearest-rank, k=ceil(p/100·n)) / `DurationStats`
  (sum/mean/median/p90/p95/max); `SortCanonical` (module, classname, name).

## Flow
`cli.prepare`: `Dedupe(reports)` → `Select(reports, sel)` → `Totals(cases)`
→ `Failed(cases)`; `GroupFailures` + `statRecords`/`breakdown` per command;
percentiles feed the stats digest and the §11 `stats.duration`.

## Integration
Consumed by `cli` only. Depends on `model`. Fingerprinting arrives as a
closure over `trim.FingerprintOf`; dedup keys use the report rank, not case
content. Dedup runs before selection so totals never double-count
aggregated duplicates.
