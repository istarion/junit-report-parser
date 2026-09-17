package cli

import (
	"strings"

	"github.com/istarion/junit-report-parser/internal/model"
)

// tailLines returns the last n lines of decoded captured output (tail-biased
// per spec §13). Empty refs and empty text yield nil.
func tailLines(ref model.OutRef, n int) []string {
	if !ref.Valid {
		return nil
	}
	text, err := readCaseOut(ref)
	if err != nil || text == "" {
		return nil
	}
	text = strings.TrimRight(text, "\r\n")
	if text == "" {
		return nil
	}
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSuffix(l, "\r")
	}
	if n > 0 && len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}

// tailText is tailLines joined for the JSON artifact's output object.
func tailText(ref model.OutRef, n int) string {
	return strings.Join(tailLines(ref, n), "\n")
}

// readCaseOut re-reads and decodes one captured-output span (same path as
// grep: deterministic sanitized replay → CDATA strip → entity decode).
func readCaseOut(ref model.OutRef) (string, error) {
	return outReader()(ref)
}
