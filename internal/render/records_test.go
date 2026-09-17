package render

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/istarion/junit-report-parser/internal/model"
)

func TestCapMessage(t *testing.T) {
	tests := []struct {
		in         string
		max        int
		want       string
		wantHidden int
	}{
		{"short", 500, "short", 0},
		{"short", 0, "short", 0},       // 0 = unlimited
		{"short", -1, "short", 0},      // negative = unlimited
		{"héllo wörld", 5, "héllo", 6}, // rune-based, not byte-based
	}
	for _, tt := range tests {
		got, hidden := CapMessage(tt.in, tt.max)
		if got != tt.want || hidden != tt.wantHidden {
			t.Errorf("CapMessage(%q, %d) = %q,%d; want %q,%d", tt.in, tt.max, got, hidden, tt.want, tt.wantHidden)
		}
	}
}

// D18: ids joined by ",", capped at 10, then ",+N more".
func TestTestsContinuation(t *testing.T) {
	ids := func(n int) []string {
		var out []string
		for i := 0; i < n; i++ {
			out = append(out, "id"+string(rune('a'+i)))
		}
		return out
	}
	if got := TestsContinuation(ids(3)); got != "ida,idb,idc" {
		t.Errorf("3 ids: %q", got)
	}
	if got := TestsContinuation(ids(10)); got != strings.Join(ids(10), ",") {
		t.Errorf("10 ids: %q", got)
	}
	got := TestsContinuation(ids(12))
	want := strings.Join(ids(10), ",") + ",+2 more"
	if got != want {
		t.Errorf("12 ids: %q, want %q", got, want)
	}
	if got := TestsContinuation(nil); got != "" {
		t.Errorf("nil: %q", got)
	}
}

func goldenTime() time.Time { return time.Date(2026, 9, 17, 9, 12, 0, 0, time.UTC) }

func baseInput() SummaryInput {
	return SummaryInput{
		Root:    "/p",
		RunTime: goldenTime(),
		Age:     4 * time.Minute,
		Reports: 2,
		Modules: 1,
		Totals:  model.Totals{Tests: 4, Passed: 1, Failed: 3, Time: 0.9},
		Groups: []GroupRecord{{
			Count:       3,
			Fingerprint: "9f2c1ab4",
			Sample:      "Connection refused: connect",
			IDs:         []string{"com.example.CartTest#testCheckout", "com.example.CartTest#testPayment", "com.example.CartTest#testRefund"},
		}},
		MaxMessage: MaxMessageRunes,
	}
}

// The summary/failures group block shape must match spec §10.4.
func TestGroupBlockShape(t *testing.T) {
	var buf bytes.Buffer
	writeGroup(&buf, baseInput().Groups[0], MaxMessageRunes)
	want := `group count=3 fingerprint=9f2c1ab4
  msg Connection refused: connect
  tests com.example.CartTest#testCheckout,com.example.CartTest#testPayment,com.example.CartTest#testRefund
`
	if buf.String() != want {
		t.Errorf("group block:\n%s\nwant:\n%s", buf.String(), want)
	}
}

func TestGroupBlockCapAndMore(t *testing.T) {
	g := GroupRecord{
		Count:       12,
		Fingerprint: "1c7de020",
		Sample:      "x",
		IDs:         []string{"t1", "t2", "t3", "t4", "t5", "t6", "t7", "t8", "t9", "t10", "t11", "t12"},
	}
	var buf bytes.Buffer
	writeGroup(&buf, g, MaxMessageRunes)
	got := buf.String()
	if !strings.HasSuffix(got, "  tests t1,t2,t3,t4,t5,t6,t7,t8,t9,t10,+2 more\n") {
		t.Errorf("D18 cap violated:\n%s", got)
	}
}

func TestFailuresTextShape(t *testing.T) {
	t.Setenv("HOME", "/home/testuser")
	in := baseInput()
	in.Root = "/home/testuser/proj"
	c := &model.Case{
		ID: "com.example.CartTest#testCheckout", ClassName: "com.example.CartTest",
		Name: "testCheckout", Module: "app", Status: model.StatusError, Time: 0.42,
		Failure: &model.Failure{
			Kind: model.StatusError, Type: "java.net.ConnectException",
			Message: "Connection refused: connect",
		},
	}
	var buf bytes.Buffer
	FailuresText(&buf, FailuresInput{
		Base: in,
		Fails: []FailRecord{{
			Case:   c,
			Loc:    "RedisClient.kt:31",
			Frames: []string{"at com.example.cart.RedisClient.connect(RedisClient.kt:31)", "at com.example.CartTest.testCheckout(CartTest.kt:88)"},
			Hidden: 14,
		}},
		HintID: c.ID,
	})
	want := `verdict=fail
root=~/proj run=2026-09-17T09:12 age=4m reports=2 modules=1 stale=false
totals tests=4 passed=1 failed=3 errors=0 skipped=0 flaky=0 time=0.9s
group count=3 fingerprint=9f2c1ab4
  msg Connection refused: connect
  tests com.example.CartTest#testCheckout,com.example.CartTest#testPayment,com.example.CartTest#testRefund
fail id=com.example.CartTest#testCheckout status=error module=app loc=RedisClient.kt:31 type=java.net.ConnectException time=0.42s
  msg Connection refused: connect
  at com.example.cart.RedisClient.connect(RedisClient.kt:31)
  at com.example.CartTest.testCheckout(CartTest.kt:88)
  trunc 14 frames hidden
hint junit-results show com.example.CartTest#testCheckout
`
	if buf.String() != want {
		t.Errorf("failures digest:\n%s\nwant:\n%s", buf.String(), want)
	}
}

func TestFailuresTextMultiLineMessage(t *testing.T) {
	in := baseInput()
	in.Groups = nil
	in.Totals = model.Totals{Tests: 1, Failed: 1}
	long := strings.Repeat("x", 600)
	c := &model.Case{
		ID: "A#m", ClassName: "A", Name: "m", Module: "app",
		Status: model.StatusFailed,
		Failure: &model.Failure{
			Kind:    model.StatusFailed,
			Type:    "T",
			Message: "first line\nsecond line\n" + long,
		},
	}
	var buf bytes.Buffer
	FailuresText(&buf, FailuresInput{Base: in, Fails: []FailRecord{{Case: c}}, HintID: "A#m"})
	got := buf.String()
	if !strings.Contains(got, "  msg first line\n  msg second line\n") {
		t.Errorf("multi-line message must become repeated msg lines:\n%s", got)
	}
	if !strings.Contains(got, "  trunc ") || !strings.Contains(got, " chars hidden") {
		t.Errorf("cap overrun must emit trunc chars line:\n%s", got)
	}
}

func TestSummaryTextGroupsBeforeFails(t *testing.T) {
	t.Setenv("HOME", "/home/testuser")
	in := baseInput()
	in.Root = "/home/testuser/proj"
	fail := &model.Case{
		ID: "com.example.CartTest#testCheckout", ClassName: "com.example.CartTest",
		Name: "testCheckout", Module: "app", Status: model.StatusError, Time: 0.42,
		Failure: &model.Failure{
			Kind: model.StatusError, Type: "java.net.ConnectException",
			Message: "Connection refused: connect",
		},
	}
	in.Fails = []FailRecord{{Case: fail, Loc: "CartTest.kt:42"}}
	var buf bytes.Buffer
	SummaryText(&buf, in)
	got := buf.String()
	want := `verdict=fail
root=~/proj run=2026-09-17T09:12 age=4m reports=2 modules=1 stale=false
totals tests=4 passed=1 failed=3 errors=0 skipped=0 flaky=0 time=0.9s
group count=3 fingerprint=9f2c1ab4
  msg Connection refused: connect
  tests com.example.CartTest#testCheckout,com.example.CartTest#testPayment,com.example.CartTest#testRefund
fail id=com.example.CartTest#testCheckout status=error module=app loc=CartTest.kt:42 type=java.net.ConnectException time=0.42s
  msg Connection refused: connect
hint junit-results failures
`
	if got != want {
		t.Errorf("summary digest:\n%s\nwant:\n%s", got, want)
	}
}
