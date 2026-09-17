package search

import (
	"strconv"
	"strings"
	"testing"

	"github.com/istarion/junit-report-parser/internal/model"
)

// A: error case with RedisClient trace + streamed out; B: assertion failure
// with "count < 5"; C: passing case with no failure and no output.
func testCases() []*model.Case {
	return []*model.Case{
		{
			ID: "com.example.CartTest#testCheckout", ClassName: "com.example.CartTest",
			Name: "testCheckout", Module: "app", Status: model.StatusError, Time: 0.42,
			Failure: &model.Failure{
				Kind: model.StatusError, Type: "java.net.ConnectException",
				Message: "Connection refused: connect",
				Trace: []string{
					"java.net.ConnectException: Connection refused: connect",
					"at com.example.cart.RedisClient.connect(RedisClient.kt:31)",
				},
			},
			Out: model.OutRef{Valid: true, Start: 0, End: 21},
		},
		{
			ID: "com.example.FooTest#computesTotal", ClassName: "com.example.FooTest",
			Name: "computesTotal", Module: "app", Status: model.StatusFailed, Time: 0.11,
			Failure: &model.Failure{
				Kind: model.StatusFailed, Type: "org.opentest4j.AssertionFailedError",
				Message: "expected count < 5",
				Trace:   []string{"org.opentest4j.AssertionFailedError: expected count < 5"},
			},
		},
		{
			ID: "lib.UtilTest#ok", ClassName: "lib.UtilTest", Name: "ok", Module: "lib",
			Status: model.StatusPassed,
		},
	}
}

func noReadOut(model.OutRef) (string, error) {
	return "connecting to redis host now\nsecond out line", nil
}

func compile(patterns []string, mutate func(*Options)) *Matcher {
	opts := Options{
		Patterns: patterns,
		Fields:   append([]string(nil), defaultFields...),
		ReadOut:  noReadOut,
	}
	if mutate != nil {
		mutate(&opts)
	}
	m, err := Compile(opts)
	if err != nil {
		panic(err)
	}
	return m
}

func TestSearchEngineTable(t *testing.T) {
	tests := []struct {
		name     string
		patterns []string
		mutate   func(*Options)
		wantHits int
		wantDesc []string // optional: exact id|field|line of each hit
	}{
		{
			name:     "literal substring: message and trace of A",
			patterns: []string{"refused"},
			wantHits: 2,
			wantDesc: []string{"com.example.CartTest#testCheckout|message|1", "com.example.CartTest#testCheckout|trace|1"},
		},
		{
			name:     "case-sensitive misses capitalized RedisClient",
			patterns: []string{"redis"},
			wantHits: 0,
		},
		{
			name:     "ignore-case hits trace",
			patterns: []string{"redis"},
			mutate:   func(o *Options) { o.IgnoreCase = true },
			wantHits: 1,
			wantDesc: []string{"com.example.CartTest#testCheckout|trace|2"},
		},
		{
			name:     "regex char class",
			patterns: []string{`count [<>] \d`},
			wantHits: 2,
			wantDesc: []string{"com.example.FooTest#computesTotal|message|1", "com.example.FooTest#computesTotal|trace|1"},
		},
		{
			name:     "OR of two patterns",
			patterns: []string{"refused: connect", "computesTotal"},
			wantHits: 4,
			// A: message, trace; B: id, name — canonical order A then B, line asc
			wantDesc: []string{
				"com.example.CartTest#testCheckout|message|1",
				"com.example.CartTest#testCheckout|trace|1",
				"com.example.FooTest#computesTotal|id|1",
				"com.example.FooTest#computesTotal|name|1",
			},
		},
		{
			name:     "fixed strings: literal dots only match id and class",
			patterns: []string{"com.example.CartTest"},
			mutate:   func(o *Options) { o.Fixed = true },
			wantHits: 2,
			wantDesc: []string{"com.example.CartTest#testCheckout|id|1", "com.example.CartTest#testCheckout|class|1"},
		},
		{
			name:     "field scope id only",
			patterns: []string{"CartTest"},
			mutate:   func(o *Options) { o.Fields = []string{FieldID} },
			wantHits: 1,
			wantDesc: []string{"com.example.CartTest#testCheckout|id|1"},
		},
		{
			name:     "out field via ReadOut",
			patterns: []string{"redis host"},
			mutate:   func(o *Options) { o.Fields = []string{FieldOut} },
			wantHits: 1,
			wantDesc: []string{"com.example.CartTest#testCheckout|out|1"},
		},
		{
			name:     "invert selects non-matching cases",
			patterns: []string{"refused"},
			mutate:   func(o *Options) { o.Invert = true },
			wantHits: 2,
			wantDesc: []string{"com.example.FooTest#computesTotal|-|0", "lib.UtilTest#ok|-|0"},
		},
		{
			name:     "max matches caps records",
			patterns: []string{"."}, // every non-empty line matches
			mutate:   func(o *Options) { o.MaxMatches = 4 },
			wantHits: 4,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := compile(tt.patterns, tt.mutate)
			got, err := m.Search(testCases())
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != tt.wantHits {
				t.Fatalf("hits = %d (%s), want %d", len(got), strings.Join(desc(got), ","), tt.wantHits)
			}
			if tt.wantDesc != nil {
				g := desc(got)
				if strings.Join(g, ",") != strings.Join(tt.wantDesc, ",") {
					t.Errorf("hits = %v, want %v", g, tt.wantDesc)
				}
			}
		})
	}
}

func desc(ms []Match) []string {
	var out []string
	for _, m := range ms {
		out = append(out, m.Case.ID+"|"+m.Field+"|"+strconv.Itoa(m.Line))
	}
	return out
}

func TestSearchContextBlock(t *testing.T) {
	opts := Options{Patterns: []string{"second"}, Fields: []string{FieldOut}, Context: 2, ReadOut: noReadOut}
	m, err := Compile(opts)
	if err != nil {
		t.Fatal(err)
	}
	got, err := m.Search(testCases())
	if err != nil || len(got) != 1 {
		t.Fatalf("hits = %d err %v", len(got), err)
	}
	want := []string{"connecting to redis host now", "second out line"} // clamped at block start
	if strings.Join(got[0].Lines, "|") != strings.Join(want, "|") {
		t.Errorf("context block = %q, want %q", got[0].Lines, want)
	}
	if got[0].Line != 2 {
		t.Errorf("line = %d, want 2", got[0].Line)
	}
}

func TestSearchNoContextForMessage(t *testing.T) {
	// -C applies to trace/out/err only (spec §15); message gets no context.
	m, _ := Compile(Options{Patterns: []string{"expected"}, Fields: []string{FieldMessage}, Context: 3})
	got, _ := m.Search(testCases())
	if len(got) != 1 || len(got[0].Lines) != 1 {
		t.Errorf("message match must have exactly one text line, got %+v", got)
	}
}

func TestSearchLineTruncation(t *testing.T) {
	long := strings.Repeat("x", 50)
	m, _ := Compile(Options{Patterns: []string{"xxx"}, Fields: []string{FieldMessage}, MaxLineLength: 10})
	got, err := m.Search([]*model.Case{{
		ID: "A#x", ClassName: "A", Name: "x", Module: "m", Status: model.StatusFailed,
		Failure: &model.Failure{Message: long},
	}})
	if err != nil || len(got) != 1 {
		t.Fatalf("hits = %d err %v", len(got), err)
	}
	if got[0].Lines[0] != strings.Repeat("x", 10)+"…" {
		t.Errorf("truncated = %q", got[0].Lines[0])
	}
}

func TestParseFields(t *testing.T) {
	def, err := ParseFields("")
	if err != nil || len(def) != 6 {
		t.Errorf("default fields = %v err %v", def, err)
	}
	all, err := ParseFields("all")
	if err != nil || len(all) != 8 || all[6] != FieldOut || all[7] != FieldErr {
		t.Errorf("all = %v err %v", all, err)
	}
	mixed, err := ParseFields("message,trace")
	if err != nil || len(mixed) != 2 {
		t.Errorf("mixed = %v err %v", mixed, err)
	}
	dup, err := ParseFields("id, id")
	if err != nil || len(dup) != 1 {
		t.Errorf("dedup = %v err %v", dup, err)
	}
	if _, err := ParseFields("bogus"); err == nil {
		t.Error("unknown field must error")
	}
}

func TestCompileErrors(t *testing.T) {
	if _, err := Compile(Options{Patterns: nil}); err == nil {
		t.Error("no patterns must error")
	}
	if _, err := Compile(Options{Patterns: []string{"("}}); err == nil {
		t.Error("bad regex must error")
	}
	if _, err := Compile(Options{Patterns: []string{"("}, Fixed: true}); err != nil {
		t.Errorf("literal '(' must compile: %v", err)
	}
}
