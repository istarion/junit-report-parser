# Repository Atlas: junit-results

## Project Responsibility

`junit-results` is a read-only Go CLI that converts raw JUnit XML reports
(Gradle/Maven) into a token-budgeted, agent-readable digest on stdout plus a
full-fidelity JSON artifact on disk. The CLI contract is the entire interface —
there is no library API. Two deliberately separated channels: stdout (token
budget, trimmed, human/model readable) and `--report` JSON (full fidelity, for
tooling/CI/baselines).

## System Entry Points

| Path | Role |
|---|---|
| `cmd/junit-results/main.go` | Process entry: injects version (ldflags) + real clock, delegates to `cli.Execute`, returns the process exit code |
| `internal/cli/root.go` | Cobra command tree, persistent global/selection flags, default-command redispatch to `summary` |
| `Makefile` | `build`, `test`, `accept`, `lint`, `fmt`, `vet`, `perf` targets |
| `junit-results-design.md` | Black-box specification (behavior contract) |
| `adoc/plan.md` | Implementation plan + **binding decision log** (D1–D21) |

## Data Flow

```
argv → cli (flags, exit codes)
        → discover   walk root, module naming, source labels, staleness, --newer-than
        → decode     streaming XML → model; system-out/err kept as lazy OutRef byte ranges
        → agg        dedup (classname,name), selection, totals, canonical sorts, stats
        → trim       frame classification, trace trimming, loc precedence, fingerprints
        → render     stdout formats (text | json | ndjson | md)
        → report     §11 JSON artifact (atomic write, --report-force)
        → search     grep over the decoded model, streamed out/err re-reads
```

Captured output never materializes unless asked for: `decode.ReadRange` replays
the sanitizer chain to re-read an `OutRef` span, then `decode.UnescapeText`
decodes entities (spans are raw XML, possibly CDATA-wrapped).

## Directory Map (Aggregated)

| Directory | Responsibility Summary | Detailed Map |
|---|---|---|
| `cmd/junit-results/` | Process entry point: version/clock injection and exit-code handoff. | [View Map](cmd/junit-results/codemap.md) |
| `internal/cli/` | Cobra command tree, flag surface, exit-code mapping, per-command pipelines (summary/failures/grep/stats/show/list/discover), budget presets, artifact wiring. | [View Map](internal/cli/codemap.md) |
| `internal/clock/` | Injectable time source so staleness/age are deterministic under test. | [View Map](internal/clock/codemap.md) |
| `internal/model/` | Shared domain types (Report, Suite, Case, Failure, OutRef, Status, Totals, Warning) and duration/age formatting rules. | [View Map](internal/model/codemap.md) |
| `internal/decode/` | Streaming `encoding/xml` decoder, lazy `OutRef` capture, UTF-8 sanitizing reader, charset fallback, entity/range replay. | [View Map](internal/decode/codemap.md) |
| `internal/discover/` | Report discovery walk (SKIP/REPORT_DIRS), module naming, source labels, staleness, `--newer-than`, `--path`, source-file index, parallel decode. | [View Map](internal/discover/codemap.md) |
| `internal/agg/` | Cross-report dedup, selection filtering, totals recomputation, canonical sort orders, duration percentiles, failure grouping. | [View Map](internal/agg/codemap.md) |
| `internal/trim/` | Framework/project frame classification, trace trimming with fallback, `loc` precedence, conservative failure fingerprinting. | [View Map](internal/trim/codemap.md) |
| `internal/render/` | stdout rendering: §10 text grammar, ndjson, markdown; trim-agnostic (loc/frames/fingerprints precomputed by `cli`). | [View Map](internal/render/codemap.md) |
| `internal/report/` | §11 artifact structs, deterministic (unescaped HTML) marshaling, atomic write, refusal/force semantics. | [View Map](internal/report/codemap.md) |
| `internal/search/` | Field-scoped grep semantics over the decoded model: regex/literal, invert, context, caps, streamed out/err matching. | [View Map](internal/search/codemap.md) |
| `internal/accept/` | Black-box acceptance harness: fixture trees, byte-exact goldens with masking, generators, determinism/perf checks. | [View Map](internal/accept/codemap.md) |

## Dependency Direction

`cli → {discover, decode, agg, trim, render, report, search} → {model, clock}`

- Nothing imports `cli`; `decode` never imports `render`/`trim`.
- `render` receives precomputed `loc`/trimmed frames so it stays independent of `trim`.
- `model`/`decode` APIs are shared by all later phases — additive changes only.
