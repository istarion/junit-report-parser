# internal/clock/

## Responsibility
Injectable time source (plan D16) so commands, discovery staleness and
acceptance goldens are deterministic.

## Design
`Clock` is a one-method struct (`Now func() time.Time`) rather than an
interface — cheap to pass by value. Two constructors:
`Real()` wraps `time.Now` (wired in `cmd/junit-results/main`);
`Fixed(t)` pins the clock for tests and the acceptance harness
(`accept.FixedNow` = 2026-09-17T12:00:00Z, which fixture XML timestamps are
written against as absolute RFC3339 values).

## Flow
`main` → `clock.Real()` → `cli.Env.Clock` → `cli.now(env)` and
`discover.Options.Clock`; the accept harness constructs `clock.Fixed`
directly. Naive suite timestamps are parsed with `ParseInLocation(time.Local)`
so fixed-clock ages stay exact.

## Integration
Depended on by `cli`, `discover`, and `accept`; imports only `time`.
No consumers call `time.Now` directly outside this package.
