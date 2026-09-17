# internal/trim/

## Responsibility
Frame classification, trace trimming and fingerprints (plan §7, spec §13) —
the "what is project code" oracle and the root-cause grouping key.

## Design
- Classification: `denylist` framework prefixes VERBATIM from spec §13;
  `FrameClass` extracts the FQCN (JVM-module prefix and method stripped);
  project = source basename (`SourceFile`, `File.ext:NNN`) found in the
  injected `FileIndex` interface OR class package == `MajorityPackage`
  (mode of the report's classname packages, ties → lexicographically
  smaller); else unknown; non-`at` lines are headers.
- `TrimTrace`: keep project+unknown frames up to `MaxFrames`, drop
  bridging framework frames; nothing survives → first `MaxFrames` raw
  frames (fixture 21 fallback); `FullStack` disables; `FrameCount` supports
  the small budget's 0-frame mode. `Hidden` drives `trunc <n> frames hidden`.
- `Loc` (plan §9, gate ruling): basename:NNN from the first PROJECT frame,
  else the first frame's, else the first `at` frame verbatim, else `-`.
- Fingerprints (D4 conservative): `NormalizeMessage` collapses whitespace
  and replaces UUIDs/ISO-8601/epoch(10,13)/IPv4/IPv6/hex≥8/digits≥6 with
  `<var>`; `Fingerprint` = first 8 hex of SHA-256 over
  `type \0 normalized-message \0 firstProjectFrame` (raw-trace project
  frame, so fingerprints are stable across --max-frames).

## Flow
`cli.caseCtx` calls `Loc`/`FingerprintOf`/`TrimTrace` per case; results are
passed to `render` as plain strings — render never classifies.

## Integration
Consumed by `cli` only (`FileIndex` is satisfied by `discover.FileIndex`
without an import). Depends on `model`. Kwargs: `Options{MaxFrames,
FullStack, Files}`.
