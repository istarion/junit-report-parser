# internal/accept/

## Responsibility
Black-box acceptance harness (plan §11): every fixture is a real CLI
invocation against a copied fixture tree with a fixed clock, compared to a
masked stdout golden, an exit code, and optional artifact assertions. The
authoritative behavior gate.

## Design
- `RunFixture(t, id)` per fixture dir `testdata/FNN/`: copies `root/` to a
  temp dir, applies `mtime_age` via `os.Chtimes` (ages relative to
  `FixedNow`), runs `generate` (checked-in generators, e.g. the ~5 MB
  `bigout` fixture), substitutes `@REPORT@` with a temp path, seeds
  `report_seed` bytes for refusal fixtures, then runs `cli.NewRoot`
  in-process with `clock.Fixed(FixedNow)`.
- Conventions per fixture: `cmd` (first non-comment line, `#` comments),
  `want_exit`, `want_stdout.golden`, `want_report.json` (optional golden),
  `run_twice` (two invocations, masked stdout must be byte-identical),
  `max_seconds` (loose wall-time guard).
- Masking (`mask`): `root=`, `run=`, `age=` in digests and `"root"`/
  `"generatedAt"` in JSON artifacts — time- and environment-dependent
  fields only; the rest is byte-exact.
- Global invariants: piped stdout must contain no `\x1b` anywhere
  (fixture 13); written artifacts must be valid JSON with
  `schemaVersion: 1`, `cases` present unless `--report-no-cases`, and
  `"output"` only with `--report-output` (`checkReport`).
- `-fixture F01,F02` flag selects a subset (`Selected`).

## Flow
`TestAcceptance` (accept_test.go) → `Selected(all)` → `RunFixture` per id.
Side files: `TestBigOutputLazy` (fixture 8: lazy output, timing/memory) and
`TestPerf` (`//go:build perf`, `make perf`: generates 1000 reports / 50k
cases, runs summary in-process, soft-guards wall time and logs heap; the
generated corpus intentionally exits 1).

## Integration
Imports `cli`, `clock` (plus `model` in tests) and drives the real command
tree — no mocks. Exit codes come from `cli.ExitCode(err)`; stderr is
captured but only asserted on refusal/usage fixtures. Covers fixtures
F01–F22 (spec §19).
