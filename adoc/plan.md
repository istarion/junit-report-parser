# junit-results — Implementation Plan

Status: approved (decisions locked with orlan, 2026-09-17)
Source of truth for behavior: [`junit-results-design.md`](../junit-results-design.md) (the spec).
This document adds: locked decisions, deviations, canonical rules the spec leaves
illustrative, architecture, and phase/lane breakdown. Where spec and plan
conflict, the Decision Log below wins.

---

## 1. Scope

Implement milestones **M0–M4** of the spec: commands `summary` (default),
`failures`, `grep`, `stats`, `show`, `list`, `discover`; text digest grammar,
`json`/`ndjson`/`md` stdout formats; `--report` JSON artifact; exit codes;
acceptance fixtures 1–22 (§19).

**Out of scope:** `diff --baseline`, the `junit-test-results` skill (M5).

## 2. Decision Log

| # | Decision |
|---|---|
| D1 | Go module path: `github.com/istarion/junit-report-parser` |
| D2 | CLI library: **cobra** (only external dependency) |
| D3 | Scope M0–M4 |
| D4 | §21 resolutions: `--report` stays explicit; `age=` always printed; conservative fingerprint normalization; surefire/failsafe labeled + merged + `--source` filter; `show` substring matching with >10-match warn; `stats --by suite` ≈ classname |
| D5 | **DEV-1 (spec deviation):** `grep` no-match returns exit **6** (new code, replaces the §16 rg convention). Exit 1 uniformly means "corpus has failures/errors" for all commands. For `grep`: no match → 6; matches + failing corpus → 1; matches + clean corpus → 0. Documented in `--help`. |
| D6 | **DEV-2 (spec deviation):** `run=` in the text digest uses **naive local time** `2006-01-02T15:04` (no offset, no `Z`). JSON artifact (`run.timestamp`, `generatedAt`) stays **UTC RFC3339** with seconds. |
| D7 | **DEV-3:** canonical duration format (spec examples were illustrative): `< 0.1s` → integer ms (`90ms`); `< 60s` → seconds rounded to 2 decimals, trailing zeros trimmed, ≥1 decimal kept (`0.42s`, `45.2s`, `6.0s`); `≥ 60s` → `<m>m<s>s`, minutes unbounded (`1m12s`). Locale-independent, `.` decimal separator. |
| D8 | **DEV-3b:** canonical `age` format: `< 60s` → `42s`; `< 60m` → `4m`; `< 24h` → `3h` or `3h12m`; else `74d`. |
| D9 | **DEV-4:** flag surface additions the spec references but never tables: `--no-dedupe` (§14.3), `--report-force` + `--report-compact` (§11), `--fail-on-stale` (§16), `--source surefire|failsafe|gradle` (§21.4), `--color auto|always|never` (§10.4 mentions `--color=always`); `--no-color` kept as alias for `--color never`. |
| D10 | **DEV-5:** canonical sort orders (§16 only says "deterministic") — see §9.6 below. |
| D11 | **DEV-6:** selection flags (`--module`, `--status`, `--filter`, `--exclude`) filter the case universe **before** totals/verdict aggregation. Verdict reflects the filtered set. |
| D12 | `--format json` on stdout: compact (no indent), §11 shape minus `tool.version` (keeps `tool.name`, `schemaVersion`). |
| D13 | Initial version `0.1.0`, injected via ldflags (`-X main.version=`), overridable in Makefile. |
| D14 | Tooling: Makefile + golangci-lint. No README unless requested. |
| D15 | Warning codes: `PARSE_ERROR`, `ENCODING`, `STALE`, `DEDUP`, `OUTPUT_NOT_SEARCHED`, plus new `MANY_MATCHES` (show >10) and `NOT_XML` (verbose only). |
| D16 | Clock abstraction (`internal/clock`): injectable `now` so tests/goldens are deterministic; `main` wires `time.Now`. |
| D17 | `grep -l` prints bare ids only (no header/context lines) — matches the §10.4 example. `grep -c` prints header + single line `matches <N>`. |
| D18 | Group record `tests` continuation: ids joined by `,`, capped at 10, then `,+N more`. |
| D19 | Hint policy: at most one hint line. `summary` (fail) → `hint junit-results failures`; `failures` (non-empty) → `hint junit-results show <first-id>`. |

## 3. Architecture

```
cmd/junit-results/     main: cobra root, exit-code mapping, version
internal/cli/          per-command flag sets + RunE (one file per command)
internal/clock/        injectable now()
internal/model/        Report, Suite, Case, Failure, OutRef, Status, Totals
internal/decode/       streaming XML decoder; lazy out/err; UTF-8 sanitizer
internal/discover/     walk, module naming, source label, staleness, file index
internal/agg/          dedup, totals, stats, percentiles, flaky, grouping
internal/trim/         frame classification, trace trimming, fingerprint
internal/render/       text grammar + json/ndjson/md (one file per command/format)
internal/report/       §11 artifact: atomic write, force, compact
internal/search/       grep semantics, streamed out/err search
internal/accept/       §19 harness: fixture trees + goldens
```

Dependency direction: `cli → {discover, decode, agg, trim, render, report, search} → {model, clock}`.
No package imports `cli`. `decode` never imports `render`/`trim`.

### 3.1 Model sketch

```go
type Status int // Passed, Failed, Error, Skipped (fixed order for status stats)

type OutRef struct { Path string; Start, End int64 } // byte range in sanitized stream

type Case struct {
    ID, ClassName, Name, Module string // ID = ClassName + "#" + Name
    Status    Status
    Flaky     bool
    Time      float64 // seconds
    Failure   *Failure // non-nil for Failed/Error
    ReportPath string // repo-relative
    Out, Err  OutRef  // case-level captured output, valid=false when absent
    SuiteOut, SuiteErr OutRef
    SuiteName string
}

type Failure struct {
    Kind    Status // Failed | Error
    Type    string // exception type attr (or first trace token)
    Message string // message attr preferred; else first trace line
    Trace   []string // split lines of element text
}

type Report struct {
    Path, Module, Source string // source: gradle|surefire|failsafe
    SuiteName string; Timestamp time.Time; HasTimestamp bool
    Cases []*Case
}
```

## 4. Decoding (`internal/decode`)

- `encoding/xml` token stream, never `Unmarshal` of whole docs.
- Root element must be `testsuite` or `testsuites`; else the file is skipped
  (silently; `warn NOT_XML` only under `--verbose`).
- Nested `<testsuite>` handled recursively; cases collected from all levels.
- Case status precedence: `failure` > `error` > `skipped` > passed (§17).
  `flakyFailure`/`flakyError`/`rerunFailure`/`rerunError` set `Flaky=true`;
  they do not affect status by themselves (fixture 7: flaky then pass →
  `flaky=1`, not a failure).
- Multiple `<failure>`: first is primary. `message` attr preferred; text
  content split into `Trace`. Self-closing `<failure message="…"/>` valid.
- `<skipped>` with or without message; message kept for `stats` skipped-reasons.
- `<system-out>`/`<system-err>` at case and suite level: record `OutRef`
  (start/end via `Decoder.InputOffset()`), **never accumulate content**.
  Suite-level refs kept on the Report; case-level preferred for `show` (§17).
- UTF-8 sanitizing `io.Reader` wrapper: invalid sequences → U+FFFD, sets an
  `Encoding` flag → `warn ENCODING <path>`. The wrapper is deterministic, so
  re-reads of the same ranges via the same wrapper yield identical offsets.
  Multibyte chars split across read boundaries must survive.
- `xml.CharsetReader` wired for declared non-UTF-8 encodings (legacy surefire
  ISO-8859-1) — falls back to sanitize if unsupported.
- Errors mid-file (malformed/truncated): return partial `Report` + error;
  caller emits `warn PARSE_ERROR <path>` and skips the file entirely (spec:
  file skipped, run continues — we discard partials to avoid half-truths).

## 5. Discovery (`internal/discover`)

- Walk per §14.1: `SKIP_DIRS` prune; `REPORT_DIRS = {test-results,
  surefire-reports, failsafe-reports}` collect `*.xml`, skipping child dir
  `binary`. Parallel decode with bounded worker pool; merge sorted by path.
- During the same walk, index **source-file basenames** (`*.java *.kt *.kts
  *.rs *.go *.py *.ts *.tsx *.js *.scala *.groovy`) into a `FileIndex` for
  project-frame classification (bounded memory; SKIP_DIRS already pruned).
- Module naming: `rel = reportDir` relative to nearest `.git` ancestor.
  Strip known suffixes `…/build/<REPORT_DIR>[/…]` or `…/target/<REPORT_DIR>[/…]`
  → remaining path is the module (`app`, or nested `app/sub`). If empty,
  fall back up the report dir's ancestors to the first name that is not
  `build`/`target`/a REPORT_DIR; last resort the repo root dir name.
- Source label: `target/surefire-reports` → `surefire`;
  `target/failsafe-reports` → `failsafe`; `build/test-results…` → `gradle`.
  `--source` (repeatable) filters on label; totals merged across labels by default.
- Staleness: run timestamp = newest suite `timestamp` attr, fallback newest
  file mtime. `stale = age > --max-age` → `warn STALE`. `--fail-on-stale`
  promotes verdict-path to exit 1. `--newer-than`: duration-ago | RFC3339 |
  file path (use its mtime); filters reports before aggregation; nothing left
  → exit 2.
- `--path GLOB` (repeatable): expands globs as given, disables the walk.
- `discover` lists raw reports unfiltered by `--newer-than`/`--max-age`
  (still shows `stale=` per report).

## 6. Aggregation (`internal/agg`)

- Dedup key `(classname, name)`. Preference: non-aggregated source (root
  `<testsuite>`) wins; tie → later suite timestamp; tie → later mtime; tie →
  lexicographically greater path (fully deterministic). Collapsed count → one
  `warn DEDUP <n> cases collapsed`. `--no-dedupe` disables.
- Totals recomputed from cases, never trusted from attributes (§17).
  `flaky` counts cases with `Flaky=true`.
- Percentiles: **nearest-rank**, ascending sort, `k = ceil(p/100 * n)` (1-based).
  Empty set → all zeros.
- `stats` sections: status (fixed order passed/failed/error/skipped, share =
  1-decimal %), duration (sum/mean/median/p90/p95/max), slowest (top N),
  types (failure+error type counts), modules/flaky/skipped-reasons.
  `--by module|suite|package`: module = module name; suite = classname;
  package = classname package (everything before last `.`; default package →
  `(default)`).

## 7. Trimming & fingerprints (`internal/trim`)

- Framework denylist verbatim from §13 (`org.junit.`, `junit.`,
  `org.opentest4j.`, `java.`, `javax.`, `jdk.`, `sun.`, `kotlin.reflect.`,
  `kotlinx.coroutines.`, `org.gradle.`, `worker.org.gradle.`,
  `org.springframework.test.`, `org.mockito.`, `io.mockk.`,
  `org.assertj.core.internal.`, `org.testcontainers.`, `org.apache.maven.`).
- Project frame: source basename (from `File.ext:NNN` in the frame) exists in
  the discovery `FileIndex`, **or** frame's class prefix matches the report's
  majority package (mode of classname packages in that report). Else unknown.
- Keep exception header lines incl. `Caused by:` chain. Keep project+unknown
  `at` frames up to `--max-frames`; drop bridging framework frames. If nothing
  survives → first `--max-frames` raw frames (fixture 21). Emit
  `trunc <n> frames hidden`. `--full-stack` disables trimming.
- Fingerprint = first 8 hex of SHA-256 over
  `type + "\x00" + normalize(message) + "\x00" + firstProjectFrame`.
  Conservative normalization (D4): collapse whitespace; replace UUIDs, IPv4/IPv6,
  ISO-8601 timestamps, epoch (10/13-digit), hex tokens ≥ 8 chars, digit runs ≥ 6
  with a placeholder. Short numerics (`expected: <3>`) stay distinct.
- Groups: failures+errors by fingerprint, `--group`/`--no-group`;
  `tests` continuation capped per D18.

## 8. Rendering (`internal/render`)

### 8.1 Text grammar (§10 + canonical rules)

- Value encoding: bare token iff no whitespace and no `"`; else double-quoted
  with only `\"` and `\\` escapes. Continuation lines: two spaces, `<key> `,
  verbatim text, no escaping.
- `run=` naive local `2006-01-02T15:04` (D6). Durations/ages per D7/D8.
- `verdict` line first; then `context`; then `totals`; then `warn*`; then
  records; then `hint*` (D19).
- Colors: only when stdout is a TTY AND `--color auto` (default); piped → off
  unless `--color always`. Minimal palette (verdict, fail lines, warn).

### 8.2 Canonical sort orders (D10)

- `fail` records, `cases`, flaky list: `(module, classname, name)`.
- `group`: count desc, then fingerprint asc.
- `slowest`: time desc, then id asc. `type`: count desc, then type asc.
- `module`/`byGroup`: name asc. `status`: fixed order. `skipped`: count desc,
  then reason asc. `match`: `(module, classname, name)`, then line asc.
- `report` (discover): path asc. JSON arrays use the same orders.

### 8.3 `--format json|ndjson|md`

- `json`: compact §11 document minus `tool.version` (D12).
- `ndjson`: line 1 = header object (verdict/context/totals); then one object
  per record (`{"type":"fail","id":…,…}`).
- `md`: `## verdict` + totals table, modules table, slowest table; fenced
  code blocks for traces; grep as a table.

### 8.4 `--report` artifact (`internal/report`)

- §11 shape, `schemaVersion: 1`, pretty 2-space by default, `--report-compact`
  minified. Captured output only with `--report-output` (adds `output.out/.err`
  to failure entries, tail-biased like `--output-lines`). `cases` omitted with
  `--report-no-cases`.
- Atomic write: temp file in target dir, `rename`. Existing target without
  `--report-force` → refuse, exit 5, file untouched. `-` → stdout (pretty).
  I/O error → exit 5. `generatedAt` UTC.

## 9. Search (`internal/search`) and `show`

- Fields: `id class name type message trace` default; `all` adds `out err`.
  `out`/`err` streamed line-by-line from `OutRef` re-reads (same sanitizer),
  never retained; line budget respected.
- Regex (RE2) default; `-F` → `regexp.QuoteMeta`; `-i`; `-v` selects
  non-matching cases; `-C ≤ 5` context lines (trace/out/err fields only);
  `--max-matches 50` cap; `--max-line-length 300` truncate with `…`.
- Exit per D5. Zero matches + out/err not searched → `warn OUTPUT_NOT_SEARCHED
  add --in out,err`.
- `show <id-or-substring>`: substring over id; bare class name matches all its
  cases; > 10 matches → `warn MANY_MATCHES <n> matches` (still shows).
  `--show-output all` implied unless overridden. Emits `case` records with
  `msg`/`at`/`out`/`err` continuations (tail-biased, `--output-lines`).
- `loc` field (`fail`, `case` records): `File.ext:NNN` from the first
  project frame, else first frame, else `-`.

## 10. CLI wiring (`internal/cli`, cobra)

- Global flags = persistent on root (§8.2 + D9 additions). Selection flags
  persistent on root too (shared by all commands). Command flags local.
- Default command: root `RunE` re-dispatches to `summary` with remaining args
  (root has no own behavior). `--version`/`--help` → exit 0.
- Error mapping: `SilenceErrors` + `SilenceUsage` on root; typed errors →
  `UsageError` 3 (stderr usage), `NoReports` 2, `NoneParseable` 4,
  `ReportWriteError` 5, `GrepNoMatch` 6, `TestsFailed` 1.
- Not-yet-implemented subcommands (P1 stage) exit 3 with
  `junit-results <cmd>: not implemented yet`.

## 11. Testing & acceptance

- Unit tests per package: formatter goldens (duration/age/value-encoding),
  trimmer denylist + fallback table, fingerprint stability (normalization
  cases), nearest-rank percentiles, dedup preference chain, module naming,
  staleness/newer-than, sanitizer split-multibyte property test.
- `internal/accept`: one test per §19 fixture `F01`–`F22`. Layout:
  `testdata/FNN/root/…` fixture tree, `cmd` args file, `want_exit`,
  `want_stdout.golden` (mask `run=`/`age=`/`generatedAt`), optional
  `want_report.json` (mask `generatedAt`). Fixtures embed fixed suite
  timestamps; `clock` stub for `now`; mtimes set via `os.Chtimes`.
  F20 determinism: run twice, compare masked bytes. Harness supports
  `-fixture F01,F02` selection for phase gates.
- Perf (build tag `perf`): generate 1000 reports / 50k cases in `t.TempDir()`;
  assert completes; log wall time and `runtime.MemStats` RSS against §18
  budgets (soft assertions to avoid CI flake).

## 12. Tooling

- `Makefile`: `build` (bin/junit-results, ldflags version), `test`, `accept`,
  `lint` (golangci-lint run), `fmt`, `vet`, `clean`, `install`.
- `.golangci.yml`: errcheck, govet, staticcheck, unused, ineffassign, gofmt,
  goimports.
- `.gitignore`: `bin/`, `dist/`, `*.test`, `coverage.out`, `.idea/`.

## 13. Phases, gates, lanes

| Phase | Delivers | Gate (fixtures) |
|---|---|---|
| P0 | module, cobra scaffold, exit mapping, Makefile, lint, .gitignore | builds; usage tests |
| P1 = M0 | model, decode (lazy out), discover, agg totals, `summary` text, exit codes | 1, 2, 6, 11, 12 |
| P2 = M1 | `failures`, trim, fingerprints, groups, dedup, flaky | 3, 5, 7, 21 |
| P3 = M2 | `--report` JSON, `discover`, `list`, PARSE_ERROR/ENCODING | 9, 10, 14, 15, 20 |
| P4 = M3 | `grep` + streamed search, `stats` | 16, 17, 18, 19 |
| P5 = M4 | `show`, budgets, md/ndjson, `--fail-on-stale`, packaging | 22 + all 22 |

- One fixer lane per phase; orchestrator gates each milestone (runs harness,
  reviews diff). P2/P3/P4 can parallelize later with file-level ownership
  (per-command files in `cli/` and `render/`), decided at M0 gate.
- No git commits by agents; work stays uncommitted for owner review.

## 14. Risks

- Lazy `OutRef` offsets vs sanitizer boundary conditions → dedicated property
  tests incl. split multibyte chars (§11).
- Cobra default-command flag interleaving → usage tests in P0/P1.
- Real-world Gradle flaky/rerun XML variants → collect samples as decode
  fixtures during P2.
- Duration/age formatting determinism → golden tests from day one (D7/D8).
