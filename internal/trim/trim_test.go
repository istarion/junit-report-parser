package trim

import (
	"testing"

	"github.com/istarion/junit-report-parser/internal/model"
)

func testIndex(names ...string) FileIndex {
	m := map[string]struct{}{}
	for _, n := range names {
		m[n] = struct{}{}
	}
	return testIdx(m)
}

type testIdx map[string]struct{}

func (t testIdx) Has(name string) bool { _, ok := t[name]; return ok }

func repWithClasses(classes ...string) *model.Report {
	r := &model.Report{}
	for _, cl := range classes {
		r.Cases = append(r.Cases, &model.Case{ClassName: cl})
	}
	return r
}

// Every denylist prefix (verbatim from spec §13) must classify as framework.
func TestClassifyDenylist(t *testing.T) {
	prefixes := []string{
		"org.junit.", "junit.", "org.opentest4j.", "java.", "javax.", "jdk.",
		"sun.", "kotlin.reflect.", "kotlinx.coroutines.", "org.gradle.",
		"worker.org.gradle.", "org.springframework.test.", "org.mockito.",
		"io.mockk.", "org.assertj.core.internal.", "org.testcontainers.",
		"org.apache.maven.",
	}
	for _, p := range prefixes {
		class := p + "Something.doIt"
		frame := "at " + class + "(File.kt:1)"
		if got := Classify(frame, nil, nil); got != FrameFramework {
			t.Errorf("Classify(%q) = %v, want framework (prefix %q)", frame, got, p)
		}
	}
	// Denylist prefixes must not over-match near-misses.
	near := []string{
		"at org.junitpioneer.Foo.bar(Foo.java:1)",
		"at com.example.junit.Foo.bar(Foo.java:1)",
		"at org.mockito_kotlin.Foo.bar(Foo.java:1)", // not the denylisted package
	}
	for _, frame := range near {
		if got := Classify(frame, nil, nil); got == FrameFramework {
			t.Errorf("Classify(%q) = framework, want non-framework", frame)
		}
	}
}

func TestClassifyTable(t *testing.T) {
	rep := repWithClasses("com.example.FooTest", "com.example.OtherTest") // majority: com.example
	tests := []struct {
		name  string
		frame string
		rep   *model.Report
		files FileIndex
		want  FrameKind
	}{
		{"header exception", "java.net.ConnectException: boom", nil, nil, FrameHeader},
		{"header caused by", "Caused by: java.lang.IllegalStateException: x", nil, nil, FrameHeader},
		{"header more trailer", "... 3 more", nil, nil, FrameHeader},
		{"empty line", "", nil, nil, FrameHeader},
		{
			"project via file index",
			"at com.example.cart.RedisClient.connect(cart/RedisClient.kt:31)",
			nil, testIndex("RedisClient.kt"), FrameProject,
		},
		{
			"project via majority package",
			"at com.example.CartTest.testCheckout(CartTest.kt:88)",
			rep, nil, FrameProject,
		},
		{
			"unknown package no index hit",
			"at com.other.thing.doIt(Thing.kt:9)",
			rep, nil, FrameUnknown,
		},
		{
			"framework wins over index",
			"at org.junit.Platform.launch(Foo.kt:1)",
			nil, testIndex("Foo.kt"), FrameFramework,
		},
		{
			"module-prefixed frame classified",
			"at java.base/java.lang.reflect.Method.invoke(Method.java:568)",
			nil, nil, FrameFramework,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Classify(tt.frame, tt.rep, tt.files); got != tt.want {
				t.Errorf("Classify(%q) = %v, want %v", tt.frame, got, tt.want)
			}
		})
	}
}

func TestFrameClass(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"at com.example.Foo.bar(Foo.kt:1)", "com.example.Foo"},
		{"at java.base/java.lang.Method.invoke(M.java:5)", "java.lang.Method"},
		{"at com.example.FooTest$testLambda$1.invoke(Foo.kt:10)", "com.example.FooTest$testLambda$1"},
		{"  at p.C.m(F.kt:2)  ", "p.C"},
		{"java.net.ConnectException: x", ""},
		{"at nodots(x.kt:1)", ""},
	}
	for _, tt := range tests {
		if got := FrameClass(tt.in); got != tt.want {
			t.Errorf("FrameClass(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestSourceFile(t *testing.T) {
	tests := []struct {
		in   string
		want string
		ok   bool
	}{
		{"at p.C.m(com/x/FooTest.kt:42)", "FooTest.kt", true},
		{"at p.C.m(FooTest.kt:7)", "FooTest.kt", true},
		{"at p.C.m(FooTest.kt)", "", false}, // no line number
		{"java.lang.Exception: x", "", false},
	}
	for _, tt := range tests {
		got, ok := SourceFile(tt.in)
		if got != tt.want || ok != tt.ok {
			t.Errorf("SourceFile(%q) = %q,%v; want %q,%v", tt.in, got, ok, tt.want, tt.ok)
		}
	}
}

const cartTrace = `java.net.ConnectException: Connection refused: connect
	at com.example.cart.RedisClient.connect(RedisClient.kt:31)
	at org.junit.platform.launcher.core.ServiceLoaderQuitTest.launch(Launcher.java:10)
	at com.example.CartTest.testCheckout(CartTest.kt:88)
	at java.base/java.lang.reflect.Method.invoke(Method.java:568)
	at com.example.Common.helper(Common.kt:5)`

func traceLines(n int) []string {
	out := []string{"java.lang.Exception: boom"}
	for i := 0; i < n; i++ {
		out = append(out, " at java.base/java.lang.Frame.f(Method.java:1)")
	}
	return out
}

func TestTrimTrace(t *testing.T) {
	rep := repWithClasses("com.example.CartTest")
	files := testIndex("RedisClient.kt", "Common.kt")
	opts := Options{MaxFrames: 3, Files: files}

	res := TrimTrace(splitLines(cartTrace), rep, opts)
	want := []string{
		"at com.example.cart.RedisClient.connect(RedisClient.kt:31)", // project: index
		"at com.example.CartTest.testCheckout(CartTest.kt:88)",       // project: majority package
		"at com.example.Common.helper(Common.kt:5)",                  // project: index
	}
	if len(res.Frames) != 3 {
		t.Fatalf("frames = %v", res.Frames)
	}
	for i := range want {
		if res.Frames[i] != want[i] {
			t.Errorf("frame[%d] = %q, want %q", i, res.Frames[i], want[i])
		}
	}
	// 5 raw frames, 3 kept (2 framework bridging dropped).
	if res.Hidden != 2 {
		t.Errorf("hidden = %d, want 2", res.Hidden)
	}
}

func TestTrimTraceFallbackFrameworkOnly(t *testing.T) {
	// Fixture 21: framework-only trace falls back to first N raw frames.
	res := TrimTrace(traceLines(8), nil, Options{MaxFrames: 5})
	if len(res.Frames) != 5 {
		t.Fatalf("frames = %d, want 5 (fallback)", len(res.Frames))
	}
	if res.Frames[0] != "at java.base/java.lang.Frame.f(Method.java:1)" {
		t.Errorf("frame[0] = %q", res.Frames[0])
	}
	if res.Hidden != 3 {
		t.Errorf("hidden = %d, want 3", res.Hidden)
	}
}

func TestTrimTraceFullStack(t *testing.T) {
	full := TrimTrace(traceLines(8), nil, Options{FullStack: true})
	if len(full.Frames) != 8 || full.Hidden != 0 {
		t.Errorf("full-stack: frames=%d hidden=%d, want 8/0", len(full.Frames), full.Hidden)
	}
}

func TestTrimTraceEmpty(t *testing.T) {
	res := TrimTrace(nil, nil, Options{})
	if len(res.Frames) != 0 || res.Hidden != 0 {
		t.Errorf("empty trace: %+v", res)
	}
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == '\n' {
			line := s[start:i]
			if len(line) > 0 && line[len(line)-1] == '\r' {
				line = line[:len(line)-1]
			}
			out = append(out, line)
			start = i + 1
		}
	}
	return out
}
