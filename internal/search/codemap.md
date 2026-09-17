# internal/search/

## Responsibility
The grep engine (plan §9, spec §15): pattern matching over the decoded
model, independent of flag parsing and file I/O.

## Design
- `Compile(Options) (*Matcher, error)`: RE2 regexes by default, `-F`
  literals via `QuoteMeta`, `-i` via `(?i)` prefix; patterns are
  OR-combined at match time; any bad regex is an error (exit 3 upstream).
- `ParseFields`: `--in` validation — defaults
  `id,class,name,type,message,trace`; `all` adds `out,err`; unknown names
  error; duplicates deduped in first-seen order.
- `Matcher.Search(cases)`: per case, per field, split text into lines and
  match line-scoped; `-v` inverts by selecting cases with NO match anywhere
  (records carry `field=-`, `line=0`). Enumeration order: (module,
  classname, name), then line asc, then searched-field order. `Context`
  (trace/out/err only) emits the clamped `[i-C, i+C]` block with the
  matched line at its position. `MaxMatches` caps records after sorting;
  `MaxLineLength` rune-truncates emitted lines with `…`.
- Storage-agnostic: `Options.ReadOut func(model.OutRef) (string, error)`
  supplies already-decoded out/err text; search never touches files.

## Flow
`cli.runGrep`: `Compile` → `Search(p.cases)` with `ReadOut` wired to
`decode.ReadRange` + CDATA strip + `UnescapeText` → matches rendered or
counted; zero matches → `warn OUTPUT_NOT_SEARCHED` when unsearched output
refs exist → exit 6 (D5).

## Integration
Consumed only by `cli`. Depends on `model` (plus `decode` indirectly via
the injected reader closure, keeping the direct dependency graph clean).
