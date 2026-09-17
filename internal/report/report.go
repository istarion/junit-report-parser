// Package report defines the §11 JSON artifact: deterministic structs
// (no maps → stable field order), pretty 2-space marshaling by default,
// compact on request, and the atomic write contract (temp file in the
// target's directory + rename; never a partial file).
package report

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// SchemaVersion of the artifact (§11.1).
const SchemaVersion = 1

// ErrTargetExists is returned when the target path exists without Force.
type ErrTargetExists struct{ Path string }

func (e *ErrTargetExists) Error() string {
	return fmt.Sprintf("report target exists: %s (use --report-force)", e.Path)
}

// Artifact is the full §11 document. Field order follows §11.1.
type Artifact struct {
	SchemaVersion int      `json:"schemaVersion"`
	Tool          Tool     `json:"tool"`
	GeneratedAt   string   `json:"generatedAt"` // UTC RFC3339 (D6)
	Command       string   `json:"command"`
	Root          string   `json:"root"` // absolute
	Run           Run      `json:"run"`
	Totals        Totals   `json:"totals"`
	Modules       []Module `json:"modules"`
	Failures      []Case   `json:"failures"`
	Groups        []Group  `json:"groups"`
	Stats         Stats    `json:"stats"`
	// Cases: nil pointer = omitted (--report-no-cases); non-nil empty slice
	// renders as []. (Plain slice + omitempty would hide the empty array.)
	Cases    *[]Case `json:"cases,omitempty"`
	Warnings []Warn  `json:"warnings"`
}

type Tool struct {
	Name string `json:"name"`
	// Version is omitted when empty: D12 drops tool.version from the compact
	// JSON printed by --format json, while the --report file keeps it.
	Version string `json:"version,omitempty"`
}

type Run struct {
	Timestamp  string  `json:"timestamp"` // UTC RFC3339 (D6)
	AgeSeconds float64 `json:"ageSeconds"`
	Stale      bool    `json:"stale"`
	Reports    int     `json:"reports"`
	Modules    int     `json:"modules"`
}

type Totals struct {
	Tests       int     `json:"tests"`
	Passed      int     `json:"passed"`
	Failures    int     `json:"failures"`
	Errors      int     `json:"errors"`
	Skipped     int     `json:"skipped"`
	Flaky       int     `json:"flaky"`
	TimeSeconds float64 `json:"timeSeconds"`
}

type Module struct {
	Name        string  `json:"name"`
	Tests       int     `json:"tests"`
	Failures    int     `json:"failures"`
	Errors      int     `json:"errors"`
	Skipped     int     `json:"skipped"`
	TimeSeconds float64 `json:"timeSeconds"`
}

// Case is shared by failures[] and cases[] (§11 uses the same shape with a
// few extra fields on failures: classname, name, type, message, trace,
// flaky, reportPath).
type Case struct {
	ID          string   `json:"id"`
	Classname   string   `json:"classname,omitempty"`
	Name        string   `json:"name,omitempty"`
	Module      string   `json:"module"`
	Status      string   `json:"status"`
	Type        string   `json:"type,omitempty"`
	Message     string   `json:"message,omitempty"`
	Trace       []string `json:"trace,omitempty"`
	Flaky       bool     `json:"flaky"`
	TimeSeconds float64  `json:"timeSeconds"`
	ReportPath  string   `json:"reportPath"`
	Loc         string   `json:"loc"`
	// Output is present only with --report-output (P5): tail-biased
	// captured output for the case.
	Output *CaseOutput `json:"output,omitempty"`
}

// CaseOutput is the tail-biased captured output of one case.
type CaseOutput struct {
	Out string `json:"out,omitempty"`
	Err string `json:"err,omitempty"`
}

type Group struct {
	Fingerprint   string   `json:"fingerprint"`
	Count         int      `json:"count"`
	SampleMessage string   `json:"sampleMessage"`
	Tests         []string `json:"tests"`
}

type Stats struct {
	Status   StatusStats `json:"status"`
	Duration Duration    `json:"duration"`
	Types    []TypeCount `json:"types"`
	Slowest  []Slow      `json:"slowest"`
	ByGroup  []ByGroup   `json:"byGroup"`
}

type StatusStats struct {
	Passed  int `json:"passed"`
	Failed  int `json:"failed"`
	Error   int `json:"error"`
	Skipped int `json:"skipped"`
}

type Duration struct {
	SumSeconds    float64 `json:"sumSeconds"`
	MeanSeconds   float64 `json:"meanSeconds"`
	MedianSeconds float64 `json:"medianSeconds"`
	P90Seconds    float64 `json:"p90Seconds"`
	P95Seconds    float64 `json:"p95Seconds"`
	MaxSeconds    float64 `json:"maxSeconds"`
}

type TypeCount struct {
	Type  string `json:"type"`
	Count int    `json:"count"`
}

type Slow struct {
	ID          string  `json:"id"`
	TimeSeconds float64 `json:"timeSeconds"`
	Status      string  `json:"status"`
}

type ByGroup struct {
	Name        string  `json:"name"`
	Tests       int     `json:"tests"`
	Failed      int     `json:"failed"`
	TimeSeconds float64 `json:"timeSeconds"`
}

// Warn mirrors a digest warning; Path is nullable (§11 shows "path": null).
type Warn struct {
	Code    string  `json:"code"`
	Message string  `json:"message"`
	Path    *string `json:"path"`
}

// Marshal renders the artifact: pretty 2-space by default, compact on
// request (plan §8.4). HTML escaping is disabled (gate ruling, §11.1):
// messages carry raw `<`, `>`, `&` instead of \u003c escapes. A trailing
// newline is appended.
func (a Artifact) Marshal(compact bool) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if compact {
		enc.SetIndent("", "")
	} else {
		enc.SetIndent("", "  ")
	}
	if err := enc.Encode(a); err != nil {
		return nil, err
	}
	// Encoder already appends one newline.
	return buf.Bytes(), nil
}

// Write writes the artifact bytes to path atomically: temp file in the
// target's directory, write, close, rename (§11.2 — the target either does
// not exist or is a complete document). An existing target without force is
// refused with *ErrTargetExists and left byte-identical. "-" writes to
// stdout instead (always pretty: the caller marshals accordingly).
func Write(path string, data []byte, force bool, stdout io.Writer) error {
	if path == "-" {
		_, err := stdout.Write(data)
		return err
	}
	if _, err := os.Stat(path); err == nil && !force {
		return &ErrTargetExists{Path: path}
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".junit-results-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}
