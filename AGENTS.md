# AGENTS.md — junit-results

## What this is

`junit-results` is a read-only Go CLI that turns raw JUnit XML reports (Gradle /
Maven) into a token-optimal digest on stdout plus a full-fidelity JSON artifact
on disk. Two audiences, deliberately decoupled: coding agents reading shell
output (stdout digest), and tooling/CI (`--report` JSON file).

**Authoritative documents — read before changing behavior:**

- `junit-results-design.md` — the black-box specification (invocation, output
  grammar, exit codes, guarantees, acceptance fixtures).
- `adoc/plan.md` — implementation plan with the **binding decision log**
  (D1–D21). Where the plan and the spec disagree, the plan wins. Known
  supersessions: `grep` no-match exits 6 (D5), `run=` is naive local time (D6),
  duration/age formats (D7/D8), canonical sort orders (D10),
  selection-before-totals (D11), `--format json` drops `tool.version` (D12),
  `stats` breakdown record shape (D21).

## Build, test, verify

| Command | Purpose |
|---|---|
| `make build` | Build `bin/junit-results` (version injected via ldflags) |
| `make test` | Unit tests, all packages |
| `make accept` | **Authoritative behavior gate**: 22 black-box fixtures (spec §19) |
| `make lint` | golangci-lint (may be absent locally) |
| `make vet` / `make fmt` | Always-available gates: `go vet ./...`, `gofmt -s` |
| `make perf` | Tagged perf smoke: 1000 reports / 50k cases (`go test -tags perf`) |

Run a fixture subset: `go test -run TestAcceptance ./internal/accept/ -fixture F01,F02`

Minimum gate for any change: `go build ./... && go test -count=1 ./... && go vet ./... && gofmt -l .` (must print nothing).

## Non-negotiables

- **Exit codes are contract.** 0 ok / 1 tests failed / 2 no reports / 3 usage /
  4 none parseable / 5 report write refused / 6 `grep` no match. Never
  repurpose 1 for grep; the exit-6 note is in every command's `--help`.
- **Read-only by default.** Never write into the project. Only an explicit
  `--report` path is written, atomically (temp + rename), and refused if it
  exists without `--report-force`.
- **Two channels, two budgets.** stdout is token-budgeted and trimmed; the
  `--report` JSON is full fidelity (untrimmed traces, uncapped lists,
  `--report-no-cases` to slim it). Do not blur them.
- **Determinism.** Identical inputs → byte-identical stdout modulo
  `run=`/`age=`/`generatedAt`. Every new output needs a canonical sort order
  (plan D10) and golden coverage.
- **Lazy captured output.** `<system-out>`/`<system-err>` are never
  materialized during decode — they are `model.OutRef` byte ranges over the
  sanitized stream. Read on demand only: `decode.ReadRange` +
  `decode.UnescapeText` (raw spans are entity-escaped and may be CDATA-wrapped).
- **Text grammar (spec §10).** Bare token iff no whitespace, quote, or
  backslash; otherwise double-quoted with only `\"` and `\\`. Continuation
  lines are verbatim, two-space indented, `  <key> <text>`.
- **Warnings** use `warn <CODE> <text>` with codes `PARSE_ERROR`, `ENCODING`,
  `STALE`, `DEDUP`, `OUTPUT_NOT_SEARCHED`, `MANY_MATCHES`, `NOT_XML`.

## Package dependency direction

`cli → {discover, decode, agg, trim, render, report, search} → {model, clock}`

- Nothing imports `cli`. `decode` must not import `render` or `trim`.
- `render` is trim-agnostic: `loc`, trimmed frames, and fingerprints are
  precomputed on the `cli` side and passed in.
- `model` and `decode` APIs are shared by every later phase — add, don't break.

## Workflow rules

- **Do not commit.** The owner commits. Leave work uncommitted in the working
  tree and report the changed-file list plus a suggested commit message.
- Plans, handoffs, and generated docs go in `adoc/`.
- **`adoc/plan.md` is a living contract.** When a behavior rule changes, update
  the Decision Log in the same change and keep code and plan in sync.
- Behavior changes require fixture updates in the same change; when fixing a
  bug, add the fixture that would have caught it. Goldens are byte-exact after
  masking (`root=`, `run=`, `age=`, `generatedAt`); never regenerate one without
  stating why.
- Acceptance harness conventions (`internal/accept/testdata/FNN/`): `root/`
  fixture tree, `cmd` args, `want_exit`, `want_stdout.golden`, optional
  `want_report.json`; per-fixture `generate` hooks and `max_seconds` guards are
  supported, plus `run_twice` (determinism) and `report_seed` (refusal) checks.
  ANSI escapes are forbidden in all fixture output (checked globally).
- Decode/discovery edge cases belong in unit tests under the owning package;
  only black-box, end-to-end behavior belongs in `internal/accept`.

## Repository Map

A full codemap is available at `codemap.md` in the project root.

Before working on any task, read `codemap.md` to understand:

- Project architecture and entry points
- Directory responsibilities and design patterns
- Data flow and integration points between modules

For deep work on a specific folder, also read that folder's `codemap.md`.
