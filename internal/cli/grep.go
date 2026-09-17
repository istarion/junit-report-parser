package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/istarion/junit-report-parser/internal/agg"
	"github.com/istarion/junit-report-parser/internal/decode"
	"github.com/istarion/junit-report-parser/internal/model"
	"github.com/istarion/junit-report-parser/internal/render"
	"github.com/istarion/junit-report-parser/internal/search"
)

// grepCmd builds the `grep` command (plan §9, spec §15): field-scoped search
// over the decoded model.
func grepCmd(env *Env, f *flags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "grep [pattern]",
		Short: "Search the decoded model; field-scoped",
		Args: func(cmd *cobra.Command, args []string) error {
			// Positional arguments are OR-combined with -e patterns.
			f.grepPositional = append(f.grepPositional, args...)
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGrep(env, f)
		},
	}
	cmd.Flags().StringArrayVarP(&f.grepPatterns, "pattern", "e", nil, "pattern; repeatable, OR-combined")
	cmd.Flags().StringVar(&f.grepIn, "in", "id,class,name,type,message,trace", "fields to search; 'all' adds out,err")
	cmd.Flags().BoolVarP(&f.grepIgnoreCase, "ignore-case", "i", false, "case-insensitive")
	cmd.Flags().BoolVarP(&f.grepFixed, "fixed-strings", "F", false, "literal instead of regex")
	cmd.Flags().BoolVarP(&f.grepInvert, "invert", "v", false, "select cases with NO match")
	cmd.Flags().IntVarP(&f.grepContext, "context", "C", 0, "context lines (trace/out/err only); max 5")
	cmd.Flags().BoolVarP(&f.grepIds, "ids", "l", false, "print matching ids only (bare, no preamble)")
	cmd.Flags().BoolVarP(&f.grepCount, "count", "c", false, "print header + 'matches <N>'")
	cmd.Flags().IntVar(&f.grepMaxMatches, "max-matches", 50, "cap on match records (0 = default 50)")
	cmd.Flags().IntVar(&f.grepMaxLineLength, "max-line-length", 300, "truncate matched text with ellipsis (0 = unlimited)")
	return cmd
}

func runGrep(env *Env, f *flags) error {
	p, err := prepare(env, f)
	if err != nil {
		return err
	}
	cc := newCaseCtx(p.res)

	// Flag validation (usage errors, exit 3).
	patterns := append(append([]string(nil), f.grepPatterns...), f.grepPositional...)
	if len(patterns) == 0 {
		return &UsageError{Msg: "no pattern given (use -e/--pattern or a positional argument)"}
	}
	if f.grepContext < 0 || f.grepContext > 5 {
		return &UsageError{Msg: fmt.Sprintf("invalid --context %d (want 0..5)", f.grepContext)}
	}
	if f.grepMaxMatches < 0 {
		return &UsageError{Msg: fmt.Sprintf("invalid --max-matches %d (want >= 0)", f.grepMaxMatches)}
	}
	maxMatches := f.grepMaxMatches
	if maxMatches == 0 {
		maxMatches = 50 // 0 means the default cap (plan §9)
	}
	if f.grepMaxLineLength < 0 {
		return &UsageError{Msg: fmt.Sprintf("invalid --max-line-length %d (want >= 0)", f.grepMaxLineLength)}
	}
	fields, err := search.ParseFields(f.grepIn)
	if err != nil {
		return &UsageError{Msg: err.Error()}
	}
	m, err := search.Compile(search.Options{
		Patterns:      patterns,
		Fixed:         f.grepFixed,
		IgnoreCase:    f.grepIgnoreCase,
		Invert:        f.grepInvert,
		Fields:        fields,
		Context:       f.grepContext,
		MaxMatches:    maxMatches,
		MaxLineLength: f.grepMaxLineLength,
		ReadOut:       outReader(),
	})
	if err != nil {
		return &UsageError{Msg: err.Error()}
	}

	matches, err := m.Search(p.cases)
	if err != nil {
		return &UsageError{Msg: err.Error()}
	}

	// OUTPUT_NOT_SEARCHED: zero matches while captured output exists but was
	// excluded from the search (spec §15.1). Not emitted in -l mode, whose
	// contract is bare ids only (D17).
	searchedOut, searchedErr := false, false
	for _, fl := range fields {
		if fl == search.FieldOut {
			searchedOut = true
		}
		if fl == search.FieldErr {
			searchedErr = true
		}
	}
	if len(matches) == 0 && !f.grepIds && !(searchedOut && searchedErr) {
		for _, c := range p.cases {
			if c.Out.Valid || c.Err.Valid {
				p.res.Warnings = append(p.res.Warnings, model.Warning{
					Code:    "OUTPUT_NOT_SEARCHED",
					Message: "add --in out,err",
				})
				break
			}
		}
	}

	in := render.GrepInput{
		Base: render.SummaryInput{
			Root:     p.res.Root,
			RunTime:  p.res.RunTime,
			Age:      p.res.Age,
			Stale:    p.res.Stale,
			Reports:  len(p.res.Reports),
			Modules:  p.modules,
			Totals:   p.totals,
			Warnings: p.res.Warnings,
			UseColor: resolveColor(f, env.Stdout),
		},
		Records:    grepRecords(matches),
		IdsOnly:    f.grepIds,
		CountOnly:  f.grepCount,
		MatchCount: len(matches),
		MatchedIDs: matchedIDs(matches),
	}
	b := budgetPresets["medium"]
	switch f.format {
	case "text":
		render.GrepText(env.Stdout, in)
	case "md":
		render.MDGrep(env.Stdout, in)
	case "ndjson":
		render.NDJSONGrep(env.Stdout, in)
	case "json":
		groups := agg.GroupFailures(p.fails, cc.fingerprint)
		if err := printCompactJSON(env, f, "grep", p, groups, cc, b); err != nil {
			return err
		}
	}

	if err := writeArtifactIfRequested(env, f, "grep", p, agg.GroupFailures(p.fails, cc.fingerprint), cc, b); err != nil {
		return err
	}

	// D5: no matches → 6 regardless of corpus health; matches on a failing
	// corpus → 1; matches on a clean corpus → 0.
	if len(matches) == 0 {
		return &GrepNoMatchError{}
	}
	return finalExit(p, f)
}

func grepRecords(matches []search.Match) []render.GrepRecord {
	out := make([]render.GrepRecord, 0, len(matches))
	for _, m := range matches {
		out = append(out, render.GrepRecord{Case: m.Case, Field: m.Field, Line: m.Line, Lines: m.Lines})
	}
	return out
}

// matchedIDs dedups hit ids in canonical encounter order (D10/D17).
func matchedIDs(matches []search.Match) []string {
	seen := map[string]bool{}
	var ids []string
	for _, m := range matches {
		if !seen[m.Case.ID] {
			seen[m.Case.ID] = true
			ids = append(ids, m.Case.ID)
		}
	}
	return ids
}

// outReader wires streamed out/err re-reads: deterministic sanitized-stream
// replay (decode.ReadRange), CDATA-wrapper strip, then entity decoding
// (spec §15: search the decoded text, never raw bytes).
func outReader() func(ref model.OutRef) (string, error) {
	return func(ref model.OutRef) (string, error) {
		raw, err := decode.ReadRange(ref.Path, ref.Start, ref.End)
		if err != nil {
			return "", err
		}
		return decode.UnescapeText(stripCDATA(raw)), nil
	}
}

// stripCDATA removes a whole-span CDATA wrapper (the OutRef span is raw XML
// content, so CDATA markers ride along).
func stripCDATA(s string) string {
	t := strings.TrimSpace(s)
	const open, close = "<![CDATA[", "]]>"
	if strings.HasPrefix(t, open) && strings.HasSuffix(t, close) {
		return t[len(open) : len(t)-len(close)]
	}
	return s
}
