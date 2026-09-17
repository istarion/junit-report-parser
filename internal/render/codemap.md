# internal/render/

## Responsibility
All stdout rendering: the §10 text digest grammar, the §12 markdown and
NDJSON formats. Pure output — no discovery, classification or I/O beyond
the writer.

## Design
- `SummaryInput` is the shared preamble record (verdict context, totals,
  warnings, groups, fail records, `MaxMessage`, `ModuleRows`, `UseColor`);
  every command renderer takes an Input struct the CLI already assembled —
  `loc`/frames/fingerprints arrive precomputed strings, so render is
  trim-agnostic.
- Text grammar (§10): `writePreamble` (verdict → context with D6 naive-local
  `run=` and D8 `age=` → totals → `warn` lines), then records.
  `EncodeValue` (§10.2): bare token iff no whitespace/quote (backslash
  quoted too), else double-quoted with only `\"`/`\\`. Continuations are
  `  <key> <verbatim>` (`msg`, `at`, `out`, `err`, `tests`, `text`);
  `trunc <n> frames|chars hidden` reports cuts. `CapMessage` is rune-based;
  `TestsContinuation` caps ids at 10 + `,+N more` (D18); `MaxMessageRunes`
  = 500 (medium default).
- Formats: `MD*` (headings, totals/modules/status/slowest tables, fenced
  traces) and `NDJSON*` (header object then typed records in emission
  order; `writeJSONLine` with SetEscapeHTML(false)); text colors are gated
  on `UseColor` (auto+TTY) and never emitted by md/ndjson/json.

## Flow
`cli` builds an Input → `SummaryText`/`FailuresText`/`ListText`/
`DiscoverText`/`StatsText`/`GrepText`/`ShowText` (or the `MD*`/`NDJSON*`
siblings) → writer. Hints per D19 (failures → `hint junit-results failures`;
failures cmd → `show <first-id>`).

## Integration
Consumed only by `cli`. Depends on `model` (formatters, status strings).
Fingerprints/locs/frames are opaque strings here; §10 grammar and D10/D18
ordering rules are locked by golden fixtures.
