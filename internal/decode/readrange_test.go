package decode

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUnescapeText(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"plain", "plain"},
		{"a &lt; b", "a < b"},
		{"a &gt; b", "a > b"},
		{"a &amp; b", "a & b"},
		{"&quot;quoted&quot;", `"quoted"`},
		{"&apos;apos&apos;", "'apos'"},
		{"&#65;&#x42;", "AB"},
		{"&#X48;", "H"}, // uppercase hex marker
		{"no terminator &amp", "no terminator &amp"},
		{"unknown &nbsp; stays", "unknown &nbsp; stays"},
		{"a&amp;b&lt;c&gt;d", "a&b<c>d"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := UnescapeText(tt.in); got != tt.want {
			t.Errorf("UnescapeText(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// ReadRange must reproduce OutRef spans exactly by replaying the sanitizer.
func TestReadRangeRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "TEST-out.xml")
	const xmlDoc = `<?xml version="1.0" encoding="UTF-8"?>
<testsuite name="s">
  <system-out>out &lt;first&gt; line
second line with &amp;
<![CDATA[cdata third]]>
</system-out>
  <system-err>err &#65; line</system-err>
  <testcase name="a" classname="K">
    <failure message="m"/>
    <system-out>case own output</system-out>
  </testcase>
  <testcase name="b" classname="K"/>
</testsuite>`
	if err := os.WriteFile(path, []byte(xmlDoc), 0o644); err != nil {
		t.Fatal(err)
	}
	rep, _, err := DecodeFile(path)
	if err != nil {
		t.Fatal(err)
	}

	// Case-level preferred (§17): case a keeps its own system-out.
	caseOut := UnescapeText(strings.TrimSpace(mustRead(t, path, rep.Cases[0].Out.Start, rep.Cases[0].Out.End)))
	if caseOut != "case own output" {
		t.Errorf("case out = %q", caseOut)
	}
	// Suite-level fallback (§17): case b has no own refs → suite backfill.
	suiteOut := UnescapeText(strings.TrimSpace(stripCdataForTest(mustRead(t, path, rep.Cases[1].Out.Start, rep.Cases[1].Out.End))))
	if !strings.Contains(suiteOut, "out <first> line") || !strings.Contains(suiteOut, "second line with &") || !strings.Contains(suiteOut, "cdata third") {
		t.Errorf("suite out decoded = %q", suiteOut)
	}
	errSpan := UnescapeText(strings.TrimSpace(mustRead(t, path, rep.Cases[1].Err.Start, rep.Cases[1].Err.End)))
	if errSpan != "err A line" {
		t.Errorf("err decoded = %q", errSpan)
	}

	// Zero-length range reads as empty (self-closing refs).
	if got, err := ReadRange(path, 5, 5); err != nil || got != "" {
		t.Errorf("empty range = %q, %v", got, err)
	}
}

func mustRead(t *testing.T, path string, start, end int64) string {
	t.Helper()
	s, err := ReadRange(path, start, end)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func stripCdataForTest(s string) string {
	s = strings.TrimSpace(s)
	const open, close = "<![CDATA[", "]]>"
	if strings.HasPrefix(s, open) && strings.HasSuffix(s, close) {
		return s[len(open) : len(s)-len(close)]
	}
	return s
}
