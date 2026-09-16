# junit-results — Black-Box Specification

A read-only CLI that turns raw JUnit XML reports into an agent-readable digest on
stdout and a full-fidelity JSON artifact on disk.

Status: proposal (pre-implementation)
Owner: orlan
Nature: **black-box contract only** — invocation, inputs, output grammar, exit
codes, guarantees. No implementation, language, or internal structure is
prescribed.
Related: `junit-test-results` skill (to be built on top of this contract)

---

## 1. Purpose

Answer five questions after a test run, cheaply and dependably, for a coding
agent that is reading shell output:

1. Did it pass?
2. What failed, exactly, and why?
3. Is this result from the run I just executed?
4. Which tests mention X?
5. Where is the time going, and what fails most?

## 2. Problem

| # | Failure mode | Reproducible evidence |
|---|---|---|
| 1 | Raw JUnit XML is dominated by `system-out` / `system-err` CDATA. Reading it blows the context window. | `msg-stat` reports embed megabytes of Testcontainers + Jacoco logs. |
| 2 | The HTML report is pure markup — worse than XML. | `build/reports/tests/**`. |
| 3 | Gradle console at LIFECYCLE shows short traces; `-i` adds enormous noise. | `TestLogging` defaults vary by log level. |
| 4 | Multi-module results scatter; aggregated reports double-count. | Per-module `build/test-results/**` plus an aggregated report. |
| 5 | Stale results are indistinguishable from fresh ones. | Working trees contain passing reports from months ago. |
| 6 | Traces are mostly framework frames; the project frame is buried. | Assertion via Spring/MockK/JUnit. |
| 7 | Exit status conflates "tests failed" with "nothing ran". | Agents retry or "fix" code incorrectly. |
| 8 | Searching means grepping XML: escaped, line-wrapped, uncorrelatable to a test. | `rg` over `TEST-*.xml` matches neither reliably nor usefully. |
| 9 | No cheap aggregate view of status, duration, or failure types. | Re-derived expensively every run. |

## 3. Goals

- **G1 One self-contained executable.** No runtime, no network, no side effects.
  Fast enough to call repeatedly (target: < 100 ms startup).
- **G2 Token-optimal on stdout.** Default output is a digest, not a dump.
- **G3 Trustworthy.** Provenance, staleness, and parse failures are explicit.
  Never present a partial or stale picture as complete.
- **G4 Correct under Gradle/Maven reality.** Auto-discovery, aggregation
  de-duplication, schema variants, reruns/flaky.
- **G5 Deterministic.** Stable ordering; identical inputs produce identical
  bytes (modulo timestamps).
- **G6 Read-only.** Never runs tests; never writes into the project; writes only
  to an explicitly named report path.
- **G7 Searchable.** Regex/fixed search over the decoded model, field-scoped and
  correlated to test ids.
- **G8 Aggregate insight.** Status, duration, failure types, modules, flaky.

## 4. Non-goals

- Not a test runner. Never invokes Gradle/Maven.
- Not a flakiness database or history service. Cross-run comparison uses an
  explicit baseline artifact only.
- Not a general text search over report *files*; search operates on the decoded
  model.
- Not an HTML report generator, not a CI publisher.
- Not a library API; the CLI contract is the interface.

## 5. Users & contexts

| User | Context | Primary commands |
|---|---|---|
| Coding agent | after `./gradlew test`: "pass? what failed?" | `summary`, `failures` |
| Agent debugging one test | full trace + captured output | `show` |
| Agent locating tests of a feature | find tests mentioning X | `grep` |
| Agent judging health | time sinks + dominant failures | `stats` |
| Human dev | terminal glance | `summary`, `stats` |
| CI script | gate + machine artifact | exit code + `--report` |

## 6. Design principles

1. **Breadth cheaply, depth on demand.** Two tiers of output.
2. **Signal over completeness.** Trim, group, fingerprint.
3. **Explicit uncertainty.** Every skipped file, stale report, or dedupe event
   produces a `warn` line.
4. **Two channels, two audiences.** stdout is for the model (token-budgeted);
   the report file is for tooling (full fidelity).
5. **Search the model, not the bytes.** Decode once, then query.
6. **Never trust mtime alone.** Prefer the suite `timestamp`; fall back to mtime.

## 7. Terminology

| Term | Meaning |
|---|---|
| Report | One JUnit XML file. |
| Suite | A `<testsuite>` element. |
| Case | One `<testcase>`; identified by `class#method`. |
| Module | Repo-relative project that owns a report (Gradle module / Maven artifact). |
| Run | The union of all selected reports; has one timestamp = newest suite timestamp. |
| Stale | `age > --max-age`. |
| Original | Emitted by the tool; must be reproduced exactly by any implementation. |

---

## 8. Interface

```
junit-results [global flags] <command> [args]
```

Default command: `summary`.

### 8.1 Commands

| Command | Purpose |
|---|---|
| `summary` | Verdict, totals, root-cause groups, one line per failure. |
| `failures` | Failure/error detail with trimmed traces. |
| `grep <pattern>` | Search the decoded model; field-scoped. |
| `stats` | Status, duration, slowest, failure types, modules, flaky. |
| `show <id-or-pattern>` | Full detail for matching case(s), incl. captured output. |
| `list` | Flat case list with status. |
| `discover` | Discovered report files and provenance; discovery debugging. |
| `diff --baseline FILE` | Optional (see §21). |

### 8.2 Global flags

| Flag | Default | Meaning |
|---|---|---|
| `--root DIR` | cwd | Discovery root. |
| `--path GLOB` | – | Explicit report path/glob (repeatable). Disables discovery. |
| `--format text\|json\|ndjson\|md` | `text` | stdout format (§9, §12). |
| `--report FILE` | – | Write the pretty JSON artifact to FILE (`-` = stdout). §11. |
| `--report-no-cases` | false | Omit the per-case array from the report. |
| `--report-output` | false | Include captured output in the report. |
| `--max-age DURATION` | `30m` | Staleness threshold. |
| `--newer-than WHEN` | – | Only include reports newer than duration-ago / RFC3339 / file mtime. |
| `--no-color` | auto | Disable ANSI. |
| `--verbose` | false | Include non-fatal detail in `warn` lines. |
| `--version` | – | Print version and exit 0. |
| `--help` | – | Print usage and exit 0. |

### 8.3 Selection flags (shared by all commands)

| Flag | Meaning |
|---|---|
| `--module NAME` | Restrict to module (repeatable, exact or substring). |
| `--status LIST` | `passed,failed,error,skipped` (default all). |
| `--filter SUBSTR` | Only ids containing `SUBSTR`. |
| `--exclude SUBSTR` | Exclude ids containing `SUBSTR`. |

### 8.4 Command flags

`failures`, `show`:

| Flag | Default | Meaning |
|---|---|---|
| `--max-frames N` | `5` | Trace frames kept. |
| `--full-stack` | false | Disable trimming. |
| `--max-message N` | `500` | Message character cap. |
| `--budget small\|medium\|large` | `medium` | Preset (§13). Explicit flags win. |
| `--group` / `--no-group` | group | Collapse failures by fingerprint. |
| `--show-output MODE` | `none` | `none` \| `on-failure` \| `all`. |
| `--output-lines N` | `30` | Captured-output lines per case. |

`grep`:

| Flag | Default | Meaning |
|---|---|---|
| `-e, --pattern P` | – | Pattern; repeatable, OR-combined. Positional arg accepted. |
| `--in FIELDS` | `id,class,name,type,message,trace` | Fields; `all` adds `out,err`. |
| `-i, --ignore-case` | false | Case-insensitive. |
| `-F, --fixed-strings` | false | Literal instead of regex. |
| `-v, --invert` | false | Select non-matching cases. |
| `-C, --context N` | `0` | Context lines (trace/out/err); max 5. |
| `-l, --ids` | false | Print matching ids only. |
| `-c, --count` | false | Print counts. |
| `--max-matches N` | `50` | Cap on matches. |
| `--max-line-length N` | `300` | Truncate matched text. |

`stats`:

| Flag | Default | Meaning |
|---|---|---|
| `--top N` | `5` | Rows for slowest and failure-types. |
| `--by module\|suite\|package` | `module` | Breakdown grouping. |
| `--only SECTIONS` | all | `status,duration,slowest,types,modules,flaky,skipped`. |

`summary`: `--top N` (root causes / failure examples shown).

`show` takes an id or substring; `--show-output all` is implied for `show`
unless overridden.

### 8.5 Behavioural contracts

- `--report FILE` and `--format` are independent; the report is always written
  in the §11 JSON shape regardless of `--format`.
- No report is written unless `--report` is given (read-only by default).
- If `--report` and `--format json` are both given, both are produced.
- Unknown flag → usage error, exit 3.
- No reports found → exit 2, never a silent success.

### 8.6 Examples

```bash
junit-results                                   # pass/fail verdict
junit-results summary --newer-than 10m          # fresh-run only
junit-results failures                          # why, trimmed
junit-results grep -iF redis -l                 # ids of tests mentioning redis
junit-results grep -F 'Connection refused' --in message,trace
junit-results grep -i 'timed out' --in out -C 2
junit-results stats --top 10
junit-results show 'FooTest#computesTotal' --show-output all
junit-results summary --report /tmp/run.json    # machine artifact
junit-results discover                          # what is being read
```

---

## 9. Output channel model

Two channels with different audiences, deliberately decoupled:

| Channel | Audience | Default shape | Budget |
|---|---|---|---|
| **stdout** | the model reading context | text digest (§10) | token-budgeted, trimmed |
| **report file** | tooling, `jq`, CI, baselines | pretty JSON (§11) | full fidelity, untrimmed |

Rationale: the report file never enters the model's context, so its formatting
cost is irrelevant; parsers do not care, and humans inspecting it appreciate
pretty output. stdout does enter the context, so it is optimized for tokens,
scannability, and truncation-safety.

`--format` overrides stdout only. `--report` controls the file (or `-` for
stdout) and is independent.

---

## 10. Text digest grammar (stdout default)

Line-oriented. Every line is independently meaningful. No padding, no boxes, no
ANSI (unless color is explicitly enabled), no escaping except as defined here.

### 10.1 Line classes

```
1. header      verdict=<pass|fail>
2. context     root=<path> run=<rfc3339> age=<dur> reports=<n> modules=<n> stale=<bool>
3. totals      totals tests=<n> passed=<n> failed=<n> errors=<n> skipped=<n> flaky=<n> time=<dur>
4. warn        warn <CODE> <verbatim text>
5. record      <type> key=value key=value …
6. continuation  <two spaces><key> <verbatim text>
                 <two spaces>at <frame>
7. hint        hint <verbatim text>
```

Emission order is fixed: header, context, totals, `warn`*, then records grouped
by type in the order a command defines, then `hint`*. This makes `head -20`
useful and truncation informative.

### 10.2 Value encoding

- On a **record line**, each value is either:
  - a **bare token** containing no whitespace and no `"`, or
  - a **double-quoted string** using `\"` and `\\` escapes.
- On a **continuation line**, everything after `  <key> ` is verbatim; no
  escaping is applied. Embedded newlines are emitted as separate continuation
  lines. This is why free text (messages, traces, log lines) never needs
  escaping.
- Durations are human-scaled with a unit suffix: `45.2s`, `342ms`, `1m12s`.
- Times are RFC3339 UTC. Ages are durations (`4m`, `74d`).
- Bools are `true` / `false`.

### 10.3 Record types (original)

| Type | Emitted by | Fields |
|---|---|---|
| `fail` | summary, failures | `id` `status` `module` `loc` `type` `time` |
| `group` | summary, failures | `count` `fingerprint` |
| `stat` | stats | `kind` + kind-specific fields |
| `case` | list, show | `id` `status` `module` `time` `loc` |
| `match` | grep | `id` `module` `status` `field` `line` |
| `report` | discover | `path` `module` `mtime` `suite` `cases` `status` |

Continuations:

| Type | Continuations |
|---|---|
| `fail` | `msg`, repeated `at`, `trunc`, optional `out`/`err` |
| `group` | `msg`, `tests` |
| `match` | `text` |
| `case` (show) | `msg`, `at`, `out`, `err` |
| `report` | none |

`stat` kinds and fields:

| `kind` | Fields |
|---|---|
| `status` | one line per status: `status=` `count=` `share=` |
| `duration` | `sum` `mean` `median` `p90` `p95` `max` |
| `slowest` | `rank` `id` `time` `status` |
| `type` | `count` `type` |
| `module` | `name` `tests` `failed` `errors` `skipped` `time` |
| `flaky` | one line per flaky case: `id` `time` |
| `skipped` | `count` `reason` |

### 10.4 Worked examples

`summary` (passing suite):

```
verdict=pass
root=~/IdeaProjects/msg-stat run=2026-09-17T09:12Z age=4m reports=14 modules=3 stale=false
totals tests=132 passed=128 failed=0 errors=0 skipped=4 flaky=0 time=45.2s
```

`summary` (failing suite):

```
verdict=fail
root=~/IdeaProjects/msg-stat run=2026-09-17T09:12Z age=4m reports=14 modules=3 stale=false
totals tests=132 passed=128 failed=3 errors=1 skipped=0 flaky=0 time=45.2s
group count=3 fingerprint=9f2c1ab4
  msg Connection refused: connect
  tests com.example.CartTest#testCheckout,com.example.CartTest#testPayment,com.example.CartTest#testRefund
group count=1 fingerprint=1c7de020
  msg expected: <3> but was: <4>
  tests com.example.FooTest#computesTotal
fail id=com.example.CartTest#testCheckout status=error module=app loc=CartTest.kt:88 type=java.net.ConnectException time=0.42s
fail id=com.example.FooTest#computesTotal status=failed module=app loc=FooTest.kt:42 type=org.opentest4j.AssertionFailedError time=0.11s
hint junit-results failures
```

`failures`:

```
verdict=fail
root=~/IdeaProjects/msg-stat run=2026-09-17T09:12Z age=4m reports=14 modules=3 stale=false
totals tests=132 passed=128 failed=3 errors=1 skipped=0 flaky=0 time=45.2s
fail id=com.example.FooTest#computesTotal status=failed module=app loc=FooTest.kt:42 type=org.opentest4j.AssertionFailedError time=0.11s
  msg expected: <3> but was: <4>
  at com.example.FooTest.computesTotal(FooTest.kt:42)
  at com.example.cart.CartService.total(CartService.kt:117)
  at java.base/java.lang.reflect.Method.invoke(Method.java:568)
  trunc 14 frames hidden
fail id=com.example.CartTest#testCheckout status=error module=app loc=CartTest.kt:88 type=java.net.ConnectException time=0.42s
  msg Connection refused: connect
  at com.example.cart.RedisClient.connect(RedisClient.kt:31)
  at com.example.CartTest.testCheckout(CartTest.kt:88)
```

`stats`:

```
verdict=fail
root=~/IdeaProjects/msg-stat run=2026-09-17T09:12Z age=4m reports=14 modules=3 stale=false
totals tests=132 passed=128 failed=3 errors=1 skipped=0 flaky=0 time=45.2s
stat kind=status status=passed count=128 share=97.0%
stat kind=status status=failed count=3 share=2.3%
stat kind=status status=error count=1 share=0.8%
stat kind=duration sum=45.2s mean=342ms median=90ms p90=1.2s p95=2.4s max=6.0s
stat kind=slowest rank=1 id=MsgStatApplicationTests#sendStat time=5.99s status=passed
stat kind=slowest rank=2 id=MsgStatCommonTests#basicTest time=5.00s status=passed
stat kind=type count=3 type=java.net.ConnectException
stat kind=type count=1 type=org.opentest4j.AssertionFailedError
stat kind=module name=app tests=90 failed=3 errors=1 skipped=0 time=39.1s
stat kind=module name=common tests=2 failed=0 errors=0 skipped=0 time=10.8s
```

`grep`:

```
verdict=fail
root=~/IdeaProjects/msg-stat run=2026-09-17T09:12Z age=4m reports=14 modules=3 stale=false
totals tests=132 passed=128 failed=3 errors=1 skipped=0 flaky=0 time=45.2s
match id=com.example.CartTest#testCheckout module=app status=error field=message line=1
  text java.net.ConnectException: Connection refused: connect
match id=com.example.CartTest#testPayment module=app status=error field=message line=1
  text java.net.ConnectException: Connection refused: connect
```

`grep -l`:

```
com.example.CartTest#testCheckout
com.example.CartTest#testPayment
```

`show` / captured output:

```
fail id=com.example.CartTest#testCheckout status=error module=app loc=CartTest.kt:88 type=java.net.ConnectException time=0.42s
  msg Connection refused: connect
  at com.example.cart.RedisClient.connect(RedisClient.kt:31)
  out 19:56:31 INFO  RedisClient - connecting to 127.0.0.1:6379
  err 19:56:31 ERROR RedisClient - connect failed
```

Notes:

- `verdict` is always the first line, so `head -1` answers question 1.
- All records of a type are sorted deterministically (§16).
- When `--format` is `text` and stdout is a TTY, ANSI color may be enabled;
  when piped, it is off unless `--color=always`.

---

## 11. JSON report artifact

Written only when `--report FILE` is given (or `-` for stdout). Pretty-printed
(two-space indent) by default. Full fidelity: no trace trimming, no message
truncation, no frame cap. Captured output is excluded unless `--report-output`.

`--report-compact` (optional) emits minified JSON for size-sensitive consumers.

### 11.1 Shape

```json
{
  "schemaVersion": 1,
  "tool": { "name": "junit-results", "version": "1.0.0" },
  "generatedAt": "2026-09-17T09:12:31Z",
  "command": "summary",
  "root": "/home/orlan/IdeaProjects/msg-stat",
  "run": {
    "timestamp": "2026-09-17T09:12:00Z",
    "ageSeconds": 240,
    "stale": false,
    "reports": 14,
    "modules": 3
  },
  "totals": {
    "tests": 132, "passed": 128, "failures": 3, "errors": 1,
    "skipped": 0, "flaky": 0, "timeSeconds": 45.2
  },
  "modules": [
    { "name": "app", "tests": 90, "failures": 3, "errors": 1, "skipped": 0, "timeSeconds": 39.1 }
  ],
  "failures": [
    {
      "id": "com.example.FooTest#computesTotal",
      "classname": "com.example.FooTest",
      "name": "computesTotal",
      "module": "app",
      "status": "failed",
      "type": "org.opentest4j.AssertionFailedError",
      "message": "expected: <3> but was: <4>",
      "trace": [
        "at com.example.FooTest.computesTotal(FooTest.kt:42)",
        "at java.base/java.lang.reflect.Method.invoke(Method.java:568)"
      ],
      "flaky": false,
      "timeSeconds": 0.11,
      "reportPath": "app/build/test-results/test/TEST-com.example.FooTest.xml"
    }
  ],
  "groups": [
    {
      "fingerprint": "9f2c1ab4",
      "count": 3,
      "sampleMessage": "Connection refused: connect",
      "tests": ["com.example.CartTest#testCheckout", "..."]
    }
  ],
  "stats": {
    "status": { "passed": 128, "failed": 3, "error": 1, "skipped": 0 },
    "duration": { "sumSeconds": 45.2, "meanSeconds": 0.342, "medianSeconds": 0.09, "p90Seconds": 1.2, "p95Seconds": 2.4, "maxSeconds": 6.0 },
    "types": [ { "type": "java.net.ConnectException", "count": 3 } ],
    "slowest": [ { "id": "MsgStatApplicationTests#sendStat", "timeSeconds": 5.99, "status": "passed" } ],
    "byGroup": [ { "name": "app", "tests": 90, "failed": 4, "timeSeconds": 39.1 } ]
  },
  "cases": [
    { "id": "com.example.FooTest#computesTotal", "module": "app", "status": "failed", "timeSeconds": 0.11, "loc": "FooTest.kt:42" }
  ],
  "warnings": [
    { "code": "STALE", "message": "newest report is 74d old", "path": null }
  ]
}
```

### 11.2 Guarantees

- `schemaVersion` is present and integer.
- All arrays are deterministically sorted (§16).
- Valid UTF-8; invalid input bytes are replaced, not dropped silently (a
  `PARSE_ERROR` warning records the file).
- Written atomically: the target path either does not exist or is a complete
  document. No partial files.
- Existing files at the target path are overwritten only with `--report-force`;
  otherwise the tool refuses with exit 5.
- `command` reflects the command that produced the artifact; `cases` is omitted
  with `--report-no-cases`; captured output appears only with `--report-output`.

---

## 12. Alternative stdout formats (optional)

| `--format` | Shape | Intended use |
|---|---|---|
| `text` | §10 grammar | Default; agent reading. |
| `json` | Compact §11 document, minus `tool.version` noise | Piping to `jq` without a file. |
| `ndjson` | One JSON object per line: header, then one per record | Streaming; partial-safe. |
| `md` | Markdown tables for totals/modules/slowest, fenced traces | PR comments, GitHub step summaries. |

All formats carry the same facts. `md` and `ndjson` are conveniences; `text` and
the `--report` JSON are the load-bearing contracts.

Rationale for the default choice is in Appendix A.

---

## 13. Token budgets and trace trimming

| Budget | Failures | Frames | Message cap | Captured output |
|---|---|---|---|---|
| `small` | grouped, one line each | 0 | 200 | none |
| `medium` (default) | all, grouped | 5 | 500 | none |
| `large` | all, ungrouped | 20 | 2000 | on-failure, 30 lines |

Explicit flags override the preset.

**Trace trimming** (observable rule):

1. Keep the exception header line(s), including `Caused by:` chain, up to a cap.
2. Classify `at …` frames:
   - **framework** if the package matches a denylist: `org.junit.`, `junit.`,
     `org.opentest4j.`, `java.`, `javax.`, `jdk.`, `sun.`, `kotlin.reflect.`,
     `kotlinx.coroutines.`, `org.gradle.`, `worker.org.gradle.`,
     `org.springframework.test.`, `org.mockito.`, `io.mockk.`,
     `org.assertj.core.internal.`, `org.testcontainers.`, `org.apache.maven.`
   - **project** if the frame's source path resolves under `--root`, or its class
     prefix matches the report's majority package.
   - otherwise **unknown**.
3. Keep project and unknown frames, up to `--max-frames`; drop framework frames
   that merely bridge two kept frames.
4. If trimming yields nothing, fall back to the first `--max-frames` raw frames.
5. Emit `trunc <n> frames hidden` when frames were dropped.

**Fingerprint** (observable grouping key): a short stable hash of
`type + normalized(message) + first project frame`, where normalization strips
addresses, UUIDs, timestamps, and run-varying numeric literals. Cases sharing a
fingerprint are reported as one `group`.

**Captured output** is never decoded unless `--show-output` or `--in out,err`
requests it, so a multi-megabyte `system-out` costs nothing in the default path.
When shown, output is tail-biased.

---

## 14. Discovery and provenance

### 14.1 Algorithm

Walk from `--root` (default cwd), pruning noise and collecting only known report
directories:

```
SKIP_DIRS   = .git .hg .svn .idea .gradle node_modules .venv venv
              __pycache__ .cache .tox bazel-out
REPORT_DIRS = test-results  surefire-reports  failsafe-reports

walk(dir):
  for each child dir:
    if base in SKIP_DIRS:   prune
    if base in REPORT_DIRS: collect *.xml here, skipping child dir `binary`
    else:                   walk(child)
```

- Files are validated by root element (`testsuite` / `testsuites`); others are
  ignored silently, or reported as `warn` under `--verbose`.
- Covers Gradle `build/test-results/**/TEST-*.xml` and Maven
  `target/{surefire,failsafe}-reports/TEST-*.xml` without per-tool logic.
- `--path` bypasses discovery entirely.

### 14.2 Module naming

Module = path segments between the repo root (nearest ancestor containing
`.git`) and the report directory. Fallback: the parent of the report directory.

### 14.3 De-duplication

Aggregated `testsuites` files can repeat per-class results.

- Key: `(classname, name)`.
- On collision, prefer the non-aggregated report (root `<testsuite>`); on a tie,
  prefer the later suite `timestamp`.
- Emit `warn DEDUP <n> cases collapsed`.
- `--no-dedupe` disables.

### 14.4 Staleness

- Run timestamp = newest suite `timestamp`; fallback = newest file mtime.
- `stale=true` when `age > --max-age`; also emitted as a `warn STALE` line.
- `--newer-than` filters reports before aggregation.
- `discover` prints raw paths and timestamps unfiltered.

---

## 15. `grep` semantics

Operates on the decoded model, never on raw XML bytes. Rationale: XML escapes
(`&lt;`, `&#10;`) and line wrapping make byte search miss; a byte match cannot
be attributed to a test id, status, or module; and the largest haystack
(`system-out`) is the largest noise source.

### 15.1 Fields

| Field | Source | Default | Cost |
|---|---|---|---|
| `id` | `class#method` | searched | trivial |
| `class` | classname | searched | trivial |
| `name` | method name | searched | trivial |
| `type` | failure type | searched | trivial |
| `message` | failure message | searched | trivial |
| `trace` | failure text | searched | small |
| `out` | `system-out` | **not** searched | potentially large |
| `err` | `system-err` | **not** searched | potentially large |

When a search finds nothing and output was excluded, emit:
`warn OUTPUT_NOT_SEARCHED add --in out,err`.

### 15.2 Rules

- Regex by default (RE2-equivalent semantics: no catastrophic backtracking);
  `-F` switches to literal.
- Multiple `-e` patterns OR together.
- Order: `(module, classname, name)`, then ascending line number.
- Zero matches is a normal outcome with a distinct exit code (§16).
- `out`/`err` are streamed line by line and never retained, so memory is bounded.

---

## 16. Exit codes

| Code | Meaning |
|---|---|
| `0` | Parsed; no failures/errors. For `grep`: ≥1 match. |
| `1` | Parsed; ≥1 failure or error. For `grep`: no matches. |
| `2` | No report files found. |
| `3` | Invalid usage, bad flag, or bad regex. |
| `4` | Reports found but none parseable. |
| `5` | Report write refused or failed (target exists without `--report-force`, or I/O error). |

`--fail-on-stale` promotes a stale result to exit `1`.

`grep` deliberately follows the `rg` convention where `1` means "no match". This
differs from the test-outcome meaning of `1`; it is stated in `--help` and must
be stated in the skill.

**Determinism (observable):** for identical inputs, byte-identical stdout,
excluding the `run`/`age` fields and `generatedAt`.

---

## 17. Error handling (observable)

| Condition | Behaviour |
|---|---|
| Malformed XML in one file | `warn PARSE_ERROR <path>`, file skipped, run continues. |
| File truncated mid-document | Same as malformed. |
| Non-UTF-8 bytes | Replaced with U+FFFD; case retained; `warn ENCODING`. |
| Missing attributes | Treated as empty; counts recomputed from cases. |
| `<testcase>` with both `skipped` and `failure` | `failure` wins. |
| Unknown/extra elements | Ignored. |
| `system-out` at suite and case level | Both available; case-level preferred for `show`. |
| All files unparseable | Exit 4. |
| Target `--report` exists | Refuse unless `--report-force`; exit 5. |
| Interrupted run | No partial report file (atomic write). |

Supported schema variants that must parse: root `<testsuite>`; root
`<testsuites>`; nested `<testsuite>`; self-closing `<failure>`; multiple
`<failure>` per case; `flakyFailure` / `rerunFailure` / `flakyError` (surfaced as
`flaky`); `<skipped>` with or without `message`.

---

## 18. Non-functional contract

| Property | Requirement |
|---|---|
| Side effects | Read-only except an explicit `--report` path. No network. |
| Startup | < 100 ms on the reference machine. |
| Throughput | 1 000 reports / 50 000 cases in < 300 ms. |
| Memory | < 50 MB resident on the same corpus. |
| Large input | A multi-megabyte `system-out` must not be loaded unless requested. |
| Portability | Single executable; no runtime dependency; runs on Linux/macOS. |
| Locale | Output independent of locale; durations use `.` as decimal separator. |

---

## 19. Acceptance tests (black-box)

Each is: given a fixture tree, run a command, assert stdout/exit/report.

| # | Fixture | Assertion |
|---|---|---|
| 1 | `build/test-results/test/TEST-a.xml`, all pass | `verdict=pass`, exit 0 |
| 2 | one assertion failure | `verdict=fail`, `fail` record with `msg` and project frame, exit 1 |
| 3 | one uncaught exception | `status=error`, type preserved |
| 4 | skipped case | counted in `skipped`, absent from `fail` |
| 5 | aggregated `<testsuites>` + per-class files | totals not double-counted; `warn DEDUP` |
| 6 | nested `<testsuite>` | cases discovered |
| 7 | `flakyFailure` then pass | `flaky=1`; not a failure |
| 8 | 5 MB `system-out` | runtime within budget; output absent unless `--show-output` |
| 9 | malformed XML alongside valid | `warn PARSE_ERROR`; valid results still reported |
| 10 | non-UTF-8 bytes | run succeeds; `warn ENCODING` |
| 11 | results 74 days old | `stale=true`, `warn STALE` |
| 12 | `--newer-than 10m` on stale tree | exit 2 (no reports selected) |
| 13 | pipe stdout | no ANSI |
| 14 | `--report /tmp/r.json` | pretty JSON, schema §11, `jq .` succeeds |
| 15 | `--report` to existing file | exit 5, file unchanged |
| 16 | `grep -F '&lt;'`-bearing message | matches decoded text (raw grep would not) |
| 17 | `grep` default on message in `system-out` | no match + `warn OUTPUT_NOT_SEARCHED` |
| 18 | `grep` no matches | exit 1 |
| 19 | `stats` on known durations | percentiles match nearest-rank definition |
| 20 | two identical runs | byte-identical stdout modulo time fields |
| 21 | framework-only trace | fallback frames emitted, `trunc` present |
| 22 | `--budget small` | zero `at` lines, grouped failures |

---

## 20. Milestones

| M | Deliverable | Exit criterion |
|---|---|---|
| M0 | Discovery + `summary` text + exit codes | Fixtures 1, 2, 6, 11, 12 pass |
| M1 | `failures`, trimming, fingerprints | Fixtures 3, 5, 7, 21 pass |
| M2 | `--report` JSON, `discover`, `list` | Fixtures 9, 10, 14, 15, 20 pass |
| M3 | `grep`, `stats` | Fixtures 16, 17, 18, 19 pass |
| M4 | `show`, budgets, `md`/`ndjson`, packaging | Fixture 22 and all remaining pass |
| M5 | `junit-test-results` skill (+ optional `diff`) | Agent answers "pass? why?" in ≤ 2 calls |

## 21. Open questions

1. Should `--report` default to a path (e.g. `.junit-results/report.json`) so
   agents get an artifact without thinking, or stay explicit to preserve the
   read-only guarantee? Proposal: explicit; the skill always passes a path.
2. `--newer-than` default off, but always print `age` and warn past `--max-age`.
3. Fingerprint aggressiveness: start conservative, keep `--no-group`.
4. Maven surefire vs failsafe: label the source, merge totals, allow
   `--source surefire|failsafe`.
5. `show` with a class name alone: allow substring, warn when > 10 matches.
6. `grep` exit `1` for "no match" collides with "tests failed": keep the `rg`
   convention, document loudly.
7. `stats --by suite`: Gradle gives per-class files and Maven class-level suites,
   so "suite" ≈ class; document `package` as the class-package grouping.

### Optional: `diff --baseline FILE`

Cross-run comparison, the only sanctioned cross-run behaviour, driven by an
explicit §11 artifact (no hidden state).

- Key: `id`.
- Classes emitted as records: `new`, `fixed`, `still_failing`, `new_test`,
  `removed`, plus duration deltas over a threshold.
- Refuse a baseline whose `root` differs unless `--force`; warn on
  `schemaVersion` mismatch.
- Value: answers "did my change introduce a regression?" — currently the one
  question agents cannot answer cheaply.

---

## Appendix A — Why this output format

The format decisions above are evidence-driven, not stylistic.

| Observation | Source | Consequence for this spec |
|---|---|---|
| Compact JSON uses ~37% fewer tokens than pretty JSON at equal accuracy; indentation is a token laid down repeatedly. | [format token measurement](https://jangwook.net/en/blog/en/llm-token-cost-data-format-experiment/) | Any JSON on stdout is compact; pretty JSON is confined to files, where it costs no context. |
| Tabular formats (CSV/TSV/markdown) win on uniform rows but "drop out entirely" for nested or variable-length data. | same | Stack traces and free-text messages are nested and multi-line, so a tabular stdout format is rejected. |
| TOON cuts 18–62% of tokens but "is not safe as a default in multi-turn agentic systems": parse failures cascade, and it is weakest on non-uniform/nested data. | [arXiv 2605.29676](https://arxiv.org/html/2605.29676v1) | TOON is not adopted; the payload topology is exactly its weak case, and it has no tooling. |
| Anthropic treats JSON as the trained-in tool-result convention and validates with JSON Schema. | [Claude tool use](https://platform.claude.com/docs/en/agents-and-tools/tool-use/how-tool-use-works) | A JSON artifact remains the machine contract. |
| A model reading shell output has no external parser; it is the parser. | — | The stdout digest is line-oriented and escaping-free, not JSON. |
| Pretty-printed JSON in a file does not enter the model's context. | — | The report is pretty by default; token cost is irrelevant there. |

Design consequence in one line: **key=value lines for the context budget,
pretty JSON on disk for tooling.**
