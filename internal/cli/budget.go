package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// budget is a token-budget preset (spec §13). Explicit flags override it.
type budget struct {
	name        string
	group       bool   // collapse failures by fingerprint (D20 group records)
	groupsOnly  bool   // small: group records only, no detailed fail records
	maxFrames   int    // trace frames kept
	maxMessage  int    // message character cap
	outputMore  bool   // preset expresses an output intent (small/large)
	showOutput  string // none|on-failure|all
	outputLines int    // captured-output lines per case
}

// budgetPresets per the spec §13 table; medium is the default.
var budgetPresets = map[string]budget{
	"small": {
		name: "small", group: true, groupsOnly: true,
		maxFrames: 0, maxMessage: 200,
		outputMore: true, showOutput: "none", outputLines: 30,
	},
	"medium": {
		name: "medium", group: true,
		maxFrames: 5, maxMessage: 500,
		outputMore: false, showOutput: "none", outputLines: 30,
	},
	"large": {
		name: "large", group: false,
		maxFrames: 20, maxMessage: 2000,
		outputMore: true, showOutput: "on-failure", outputLines: 30,
	},
}

func resolveBudget(name string) (budget, error) {
	b, ok := budgetPresets[name]
	if !ok {
		return budget{}, &UsageError{Msg: fmt.Sprintf("invalid --budget %q (want small|medium|large)", name)}
	}
	return b, nil
}

// applyOverrides layers explicit flags over the preset. defaultOutput is the
// command's own default captured-output mode (show implies "all"; failures
// resolves to "none"), used when the preset expresses no output intent.
func (b budget) applyOverrides(cmd *cobra.Command, f *flags, defaultOutput string) (budget, error) {
	fl := cmd.Flags()
	if fl.Changed("max-frames") || fl.Changed("full-stack") {
		b.maxFrames = f.maxFrames
	}
	if fl.Changed("max-message") {
		b.maxMessage = f.maxMessage
	}
	if fl.Changed("output-lines") {
		b.outputLines = f.outputLines
	}
	switch {
	case fl.Changed("show-output"):
		b.showOutput = f.showOutput
		b.outputMore = true
	case b.outputMore:
		// preset intent (small → none, large → on-failure)
	default:
		b.showOutput = defaultOutput
	}
	switch {
	case fl.Changed("group") && f.group:
		b.group = true
	case fl.Changed("no-group") && f.noGroup:
		b.group = false
	}
	if b.showOutput != "none" && b.showOutput != "on-failure" && b.showOutput != "all" {
		return b, &UsageError{Msg: fmt.Sprintf("invalid --show-output %q (want none|on-failure|all)", b.showOutput)}
	}
	if b.maxMessage < 0 {
		return b, &UsageError{Msg: fmt.Sprintf("invalid --max-message %d (want >= 0)", b.maxMessage)}
	}
	if b.maxFrames < 0 {
		return b, &UsageError{Msg: fmt.Sprintf("invalid --max-frames %d (want >= 0)", b.maxFrames)}
	}
	if b.outputLines < 0 {
		return b, &UsageError{Msg: fmt.Sprintf("invalid --output-lines %d (want >= 0)", b.outputLines)}
	}
	return b, nil
}

// wantsOutput reports whether captured output should be shown for a case.
func (b budget) wantsOutput(failed bool) bool {
	switch b.showOutput {
	case "all":
		return true
	case "on-failure":
		return failed
	default:
		return false
	}
}

// outputLinesOrDefault returns the configured tail line count.
func (b budget) outputLinesOrDefault() int {
	if b.outputLines <= 0 {
		return 30
	}
	return b.outputLines
}
