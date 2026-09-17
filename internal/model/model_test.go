package model

import (
	"testing"
	"time"
)

func TestStatusString(t *testing.T) {
	tests := []struct {
		st   Status
		want string
	}{
		{StatusPassed, "passed"},
		{StatusFailed, "failed"},
		{StatusError, "error"},
		{StatusSkipped, "skipped"},
	}
	for _, tt := range tests {
		if got := tt.st.String(); got != tt.want {
			t.Errorf("Status(%d).String() = %q, want %q", tt.st, got, tt.want)
		}
		if back, ok := ParseStatus(tt.want); !ok || back != tt.st {
			t.Errorf("ParseStatus(%q) = (%v, %v)", tt.want, back, ok)
		}
	}
	if _, ok := ParseStatus("bogus"); ok {
		t.Error("ParseStatus(bogus) should not parse")
	}
}

// FormatDuration goldens lock plan D7.
func TestFormatDuration(t *testing.T) {
	tests := []struct {
		sec  float64
		want string
	}{
		{0, "0ms"},
		{0.001, "1ms"},
		{0.0899, "90ms"},
		{0.0999, "100ms"}, // still < 0.1s → ms bucket
		{0.1, "0.1s"},
		{0.11, "0.11s"},
		{0.42, "0.42s"},
		{1, "1.0s"},
		{6, "6.0s"},
		{45.2, "45.2s"},
		{59.994, "59.99s"},
		{60, "1m0s"},
		{72, "1m12s"},
		{600, "10m0s"},
		{3661, "61m1s"}, // minutes unbounded
		{-5, "0ms"},     // clamped
	}
	for _, tt := range tests {
		if got := FormatDuration(tt.sec); got != tt.want {
			t.Errorf("FormatDuration(%v) = %q, want %q", tt.sec, got, tt.want)
		}
	}
}

// FormatAge goldens lock plan D8.
func TestFormatAge(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{0, "0s"},
		{42 * time.Second, "42s"},
		{59 * time.Second, "59s"},
		{60 * time.Second, "1m"},
		{4 * time.Minute, "4m"},
		{59 * time.Minute, "59m"},
		{time.Hour, "1h"},
		{3 * time.Hour, "3h"},
		{3*time.Hour + 12*time.Minute, "3h12m"},
		{3*time.Hour + 5*time.Minute, "3h5m"},
		{23 * time.Hour, "23h"},
		{24 * time.Hour, "1d"},
		{74 * 24 * time.Hour, "74d"},
		{-time.Second, "0s"}, // clock skew clamped
	}
	for _, tt := range tests {
		if got := FormatAge(tt.d); got != tt.want {
			t.Errorf("FormatAge(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}

func TestTotalsVerdict(t *testing.T) {
	tests := []struct {
		name string
		t    Totals
		pass bool
	}{
		{"clean", Totals{Tests: 3, Passed: 3}, true},
		{"failed", Totals{Tests: 2, Passed: 1, Failed: 1}, false},
		{"error", Totals{Tests: 2, Passed: 1, Errors: 1}, false},
		{"skipped only", Totals{Tests: 2, Passed: 1, Skipped: 1}, true},
		{"empty", Totals{}, true},
	}
	for _, tt := range tests {
		if got := tt.t.Verdict(); got != tt.pass {
			t.Errorf("%s: Verdict() = %v, want %v", tt.name, got, tt.pass)
		}
	}
}
