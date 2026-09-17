# internal/report/

## Responsibility
The §11 JSON artifact: full-fidelity document model, deterministic
marshaling, and the atomic write contract.

## Design
- `Artifact` and its nested types are plain structs (no maps) so JSON field
  order is fixed and matches §11.1: schemaVersion, tool, generatedAt,
  command, root, run, totals, modules, failures, groups, stats
  (status/duration/types/slowest/byGroup), cases, warnings.
- `Tool.Version` is `omitempty` (D12): the `--report` file keeps it,
  `--format json` leaves it empty to drop it.
- `Cases` is `*[]Case`: nil pointer = omitted (`--report-no-cases`), empty
  slice = `"cases": []` (plain omitempty would hide the empty array).
- `Case` is shared by `failures[]` and `cases[]`; `Output *CaseOutput`
  appears only with `--report-output` (tail-biased out/err text).
- `Marshal(compact)`: `json.Encoder` with `SetEscapeHTML(false)` (gate
  ruling: raw `<`, `>`, `&`), 2-space indent or compact, trailing newline.
- `Write`: `"-"` → stdout; otherwise temp file in the TARGET's directory →
  write → close → rename (no partial files ever). Existing target without
  force → `*ErrTargetExists` (cli maps to exit 5) and the target stays
  byte-identical.

## Flow
`cli.buildArtifact` assembles the struct → `Marshal(f.reportCompact)` →
`Write(path, data, force, stdout)`.

## Integration
Consumed only by `cli` (`artifact.go`, `emit.go`). Depends on stdlib only —
the model conversion happens in cli so this package stays a pure schema
owner. `SchemaVersion = 1`.
