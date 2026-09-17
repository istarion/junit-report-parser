# junit-results

A read-only CLI that turns raw JUnit XML reports (Gradle / Maven) into a
token-optimal digest for coding agents — plus a full-fidelity JSON artifact
for tooling and CI.

Raw `TEST-*.xml` files are dominated by megabytes of CDATA (`system-out`,
`system-err`), and Gradle/Maven scatter results across per-module directories
that aggregated reports double-count. `junit-results` decodes all of it once
and answers the five questions that matter after a test run:

1. Did it pass?
2. What failed, exactly, and why?
3. Is this result from the run I just executed?
4. Which tests mention X?
5. Where is the time going, and what fails most?

## Install

```sh
# With the Go toolchain (1.25+) — works once the first release tag is pushed:
go install github.com/istarion/junit-report-parser/cmd/junit-results@latest

# From source:
make build   # → bin/junit-results (version injected via ldflags)

# Arch Linux: PKGBUILDs live in packaging/
#   packaging/arch-git/  — builds from git HEAD
#   packaging/arch/      — release tag v0.1.0 (once pushed)
```

No runtime, no network, no side effects: it never runs tests and never writes
into your project. The only file it can create is an explicit `--report` path,
written atomically and refused if it exists without `--report-force`.

## Quick start

```sh
junit-results                                   # pass/fail verdict (summary)
junit-results summary --newer-than 10m          # fresh-run only
junit-results failures                          # what failed and why, trimmed
junit-results show 'CartTest#testCheckout' --show-output all
junit-results grep -iF 'connection refused' --in message,trace
junit-results stats --top 10                    # time sinks + failure types
junit-results summary --report /tmp/run.json    # machine-readable artifact
junit-results discover                          # what reports were found
```

A failing summary looks like:

```
verdict=fail
root=~/IdeaProjects/demo run=2026-09-17T09:12 age=4m reports=14 modules=3 stale=false
totals tests=132 passed=128 failed=3 errors=1 skipped=0 flaky=0 time=45.2s
group count=3 fingerprint=9f2c1ab4
  msg Connection refused: connect
  tests com.example.CartTest#testCheckout,com.example.CartTest#testPayment,com.example.CartTest#testRefund
fail id=com.example.FooTest#computesTotal status=failed module=app loc=FooTest.kt:42 type=org.opentest4j.AssertionFailedError time=0.11s
  msg expected: <3> but was: <4>
hint junit-results failures
```

`head -1` answers "did it pass"; traces are trimmed to project frames with
repeated failures fingerprinted into groups; `warn` lines flag stale results,
parse errors, and deduplicated reports instead of hiding them.

## Commands

| Command | Purpose |
|---|---|
| `summary` (default) | Verdict, totals, root-cause groups, one line per failure |
| `failures` | Failure/error detail with trimmed stack traces |
| `grep <pattern>` | Regex/literal search over the decoded model, field-scoped |
| `stats` | Status shares, duration percentiles, slowest tests, failure types |
| `show <id-or-substring>` | Full detail for matching case(s), incl. captured output |
| `list` | Flat case list with status |
| `discover` | Discovered report files and their provenance |

Every command accepts the shared selection flags `--module`, `--status`,
`--filter`, `--exclude`, which narrow the case universe before totals and
verdict are computed.

## Exit codes

Exit codes are a contract — CI can gate on them without parsing output:

| Code | Meaning |
|---|---|
| `0` | Parsed; no failures or errors (for grep: ≥1 match and clean corpus) |
| `1` | Parsed; ≥1 failure or error |
| `2` | No report files found |
| `3` | Invalid usage, bad flag, or bad regex |
| `4` | Reports found but none parseable |
| `5` | Report write refused or failed |
| `6` | **No match** (`grep` and `show` only) |

> **Note:** `grep`/`show` use exit 6 for "no match" (the classic grep-tool
> convention), so exit 1 always means "the corpus has failures" — for every
> command. This is also stated in every command's `--help`.

## Two channels, two audiences

- **stdout** — the digest: line-oriented, token-budgeted, trimmed traces,
  grouped duplicates. Formats: `text` (default), `json`, `ndjson`, `md`.
- **`--report FILE`** — the artifact: full fidelity (untrimmed traces, uncapped
  lists), pretty JSON by default, `-` for stdout, `--report-compact` for
  minified, `--report-no-cases` to slim it. Schema is stable
  (`schemaVersion: 1`) and deterministically sorted.

Both are independent: `--report` and `--format` can be combined freely.

## Report discovery

Walks from `--root` (default cwd), pruning noise (`.git`, `node_modules`, …)
and collecting XML from known report directories: Gradle
`build/test-results/**`, Maven `target/{surefire,failsafe}-reports/`.
Per-class and aggregated files are merged with de-duplication (`warn DEDUP`).

Freshness comes from the newest suite `timestamp` (fallback: file mtime);
`age=` is always printed and `warn STALE` fires past `--max-age` (default
30m). Use `--newer-than` to restrict to a fresh window and `--fail-on-stale`
to make CI fail on stale results.

Captured `system-out`/`system-err` are never loaded unless you ask for them
(`--show-output`, `--in out,err`, `--report-output`), so multi-megabyte logs
cost nothing in the default path.

## Performance

1000 reports / 50 000 cases decode, aggregate and render in ~0.15 s. Startup
is a few milliseconds; memory stays flat regardless of embedded CDATA size.

## Development

```sh
make build     # build bin/junit-results
make test      # unit tests, all packages
make accept    # 22 black-box acceptance fixtures (authoritative gate)
make lint      # golangci-lint
make perf      # perf smoke: 1000 reports / 50k cases (-tags perf)
```

Behavior is specified in [`junit-results-design.md`](junit-results-design.md);
the implementation plan and its binding decision log live in
[`adoc/plan.md`](adoc/plan.md). A repository map for agents is in
[`AGENTS.md`](AGENTS.md) and [`codemap.md`](codemap.md).

## License

[MIT](LICENSE) © 2026 Sergey Zavgorodniy
