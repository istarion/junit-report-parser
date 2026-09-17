# cmd/junit-results/

## Responsibility
Process entry point. Wires the build-injected version and the real clock
into the CLI layer and turns the returned error into the process exit code.
Contains no business logic.

## Design
- `var version = "0.1.0"` is overwritten at build time via
  `-ldflags "-X main.version=$(VERSION)"` (Makefile `build`/`install`).
- All behavior lives in `internal/cli`; this package is a thin adapter from
  `cli`'s return-value convention to `os.Exit`.

## Flow
`main()` → `cli.Execute(version, clock.Real())` → cobra runs the command
tree → `Execute` prints typed errors to stderr and returns the mapped code →
`os.Exit(code)`. Codes: 0 ok; 1 tests failed; 2 no reports; 3 usage;
4 none parseable; 5 report write; 6 grep/show no match (D5).

## Integration
Only importer of `internal/cli` and `internal/clock`. Built by
`make build`/`make install`; exercised end-to-end by the acceptance harness
(`internal/accept` runs `cli.NewRoot` in-process against the same tree) and
by the perf smoke test (`-tags perf`).
