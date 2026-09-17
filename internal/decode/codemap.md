# internal/decode/

## Responsibility
Streaming JUnit XML decoder (plan §4, spec §17): token-driven, never a
whole-document Unmarshal. Also owns the UTF-8 sanitizer chain and the
lazy-output re-read path.

## Design
- `Decode(path, input)` / `DecodeFile`: bufio + `sniffEncoding` (declared
  ISO-8859-1 → `latin1Reader`) → `sanitizingReader` (chunking-independent
  UTF-8 validation, invalid bytes → U+FFFD, sets `Invalid` → ENCODING warn)
  → `xml.Decoder` with a pass-through `CharsetReader` (conversion already
  happened upstream; unknown encodings fall back to sanitize).
- `parser.run` is a stack-machine over frames
  (`frSuites/frSuite/frCase/frFail/frOut/frSkip`); unknown subtrees are
  skipped balanced. Root validation: first element must be
  testsuite/testsuites else `ErrNotXML`; non-empty stack at EOF → truncated
  → PARSE_ERROR at the call site.
- Status precedence failure > error > skipped > passed; first `<failure>` is
  primary; `message` attr preferred else first trace line; flaky*/rerun*
  set `Flaky` only. `closeSuite` takes the newest suite timestamp and
  backfills suite-level out/err onto ref-less cases (case-level wins).
- Lazy output: `OutRef.Start` = `InputOffset()` after the start tag;
  `End` back-computed from the raw end-tag boundary (keeps escaped entities
  inside the range). `ReadRange` replays the identical reader chain to
  reproduce a span; `UnescapeText` resolves entities/numeric refs (CDATA
  wrapper stripped by the caller).

## Flow
`Decode` → root sniff → token loop (`startElement`/`endElement`/
`CharData` only into `failAcc.text`) → `finalizeCase`/`closeSuite` →
`model.Report` + warnings. Re-read: `ReadRange(path, start, end)`.

## Integration
Consumed by `discover` (parallel decode; PARSE_ERROR/NOT_XML/ENCODING
warnings) and `cli` (`outReader` for grep/show/report-output). Depends only
on `model`. Malformed/truncated files are skipped whole, never partially
reported.
