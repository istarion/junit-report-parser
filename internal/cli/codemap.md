# internal/cli/

## Responsibility
Command layer: cobra command tree, flag surface, the shared pipeline
(discover → dedup → select → totals), format dispatch, artifact writing and
the typed-error → exit-code mapping.

## Design
- `Env` (version/clock/streams) is injected; `NewRoot` builds the tree per
  invocation (no package-global state, parallel-test safe). Root re-dispatches
  to `summary` as the default command; `SetFlagErrorFunc` maps flag failures
  to `*UsageError`.
- Typed errors (`UsageError`, `TestsFailedError`, `NoReportsError`,
  `NoneParseableError`, `ReportWriteError`, `GrepNoMatchError`, `NotYetError`)
  are classified by `ExitCode` in a type switch (0–6, D5: grep/show no-match
  → 6); `reportError` renders them to stderr (`show`/tests-failed print
  nothing — the digest + code carry it).
- `prepare` (summary.go) is the shared pipeline; `caseCtx` resolves per-case
  owning report + `FileIndex` and delegates to `trim` (`fingerprint`, `loc`,
  `rep`) — cli precomputes loc/frames/fingerprints so render stays
  trim-agnostic.
- `budget.go`: §13 presets small/medium/large with explicit-flag precedence
  (`applyOverrides` reads cobra `Changed()`); `--group`/`--no-group`,
  `--show-output`, `--output-lines`.
- `artifact.go` builds the §11 document (`buildArtifact`, D10-sorted arrays);
  `emit.go` has `finalExit` (verdict + `--fail-on-stale`) and
  `printCompactJSON` (D12: drops `tool.version`); `output.go` streams
  tail-biased out/err through `decode.ReadRange` + `UnescapeText`
  (`outReader`, `stripCDATA`).

## Flow
`main` → `Execute` → `NewRoot().Execute()` → `run<Cmd>` → `prepare` →
`caseCtx` → render (text/md/ndjson/json; `printCompactJSON` for json) →
`writeArtifactIfRequested` → `finalExit` (grep: no-match → 6 first).

## Integration
Consumes discover/agg/trim/render/report/decode/model/clock; external dep:
cobra. Cross-cutting contracts: selection before totals (D11), §13 budgets,
`--report-force` refusal → exit 5, warn codes carried on
`discover.Result.Warnings`.
