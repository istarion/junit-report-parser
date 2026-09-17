package trim

import (
	"testing"
)

func TestNormalizeMessageClasses(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain message stable", "expected: <3> but was: <4>", "expected: <3> but was: <4>"},
		{"short numerics distinct", "expected: <3>", "expected: <3>"},
		{"whitespace collapsed", "a\n\t b   c", "a b c"},
		{"uuid", "id 123e4567-e89b-12d3-a456-426614174000 done", "id <var> done"},
		{"iso timestamp", "at 2026-09-17T09:12:00Z sharp", "at <var> sharp"},
		{"iso naive", "2026-09-17 09:12:00.123", "<var>"},
		{"epoch 13", "took 1726579200000 ms", "took <var> ms"},
		{"epoch 10", "since 1726579200 ok", "since <var> ok"},
		{"ipv4", "connect 127.0.0.1:6379 failed", "connect <var>:6379 failed"},
		{"ipv6 full", "addr 2001:0db8:0000:0000:0000:0000:0000:0001 open", "addr <var> open"},
		{"ipv6 compressed", "addr ::1 refused", "addr <var> refused"},
		{"ipv6 short form", "addr fe80::1%eth0 x", "addr <var>%eth0 x"},
		{"hex token 8", "token deadbeef end", "token <var> end"},
		{"hex token long", "sha aabbccddeeff00112233", "sha <var>"},
		{"digit run 6", "run 123456 times", "run <var> times"},
		{"digit run 5 stays", "run 12345 times", "run 12345 times"},
		{"hex 7 stays", "val 0xdeadbee", "val 0xdeadbee"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeMessage(tt.in); got != tt.want {
				t.Errorf("NormalizeMessage(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// Fingerprint stability: identical inputs → identical fp; each varying input
// → different fp (within the conservative normalization).
func TestFingerprintStability(t *testing.T) {
	typ := "org.opentest4j.AssertionFailedError"
	rep := repWithClasses("com.example.FooTest") // so FooTest frames classify as project
	frame := "at com.example.FooTest.computesTotal(FooTest.kt:42)"
	base := Fingerprint(typ, "expected: <3> but was: <4>", []string{"hdr", frame}, rep, nil)
	if len(base) != 8 {
		t.Fatalf("fingerprint len = %d, want 8", len(base))
	}
	again := Fingerprint(typ, "expected: <3> but was: <4>", []string{"hdr", frame}, rep, nil)
	if base != again {
		t.Errorf("not stable: %s != %s", base, again)
	}

	tests := []struct {
		name      string
		otherType string
		otherMsg  string
	}{
		{"different type", "java.lang.IllegalStateException", "expected: <3> but was: <4>"},
		{"different short numeric", "", "expected: <5> but was: <4>"},
		{"different frame", "", "expected: <3> but was: <4>"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			otherType := tt.otherType
			if otherType == "" {
				otherType = typ
			}
			otherFrame := frame
			if tt.name == "different frame" {
				otherFrame = "at com.example.FooTest.other(FooTest.kt:99)"
			}
			other := Fingerprint(otherType, tt.otherMsg, []string{otherFrame}, rep, nil)
			if other == base {
				t.Errorf("%s: fingerprint collided: %s", tt.name, base)
			}
		})
	}
}

// Run-varying message details normalize to the same fingerprint (plan D4).
func TestFingerprintNormalizationCollapses(t *testing.T) {
	typ := "java.net.ConnectException"
	frame := "at com.example.cart.RedisClient.connect(RedisClient.kt:31)"
	a := Fingerprint(typ, "Connection refused: 127.0.0.1:6379 (attempt 2026-09-17T09:12:00Z id=123e4567-e89b-12d3-a456-426614174000)", []string{frame}, nil, nil)
	b := Fingerprint(typ, "Connection refused: 10.1.2.3:6379 (attempt 2025-01-01T00:00:00Z id=ffffffff-ffff-ffff-ffff-ffffffffffff)", []string{frame}, nil, nil)
	if a != b {
		t.Errorf("normalized messages must share fingerprint: %s != %s", a, b)
	}
}

func TestFirstProjectFrame(t *testing.T) {
	rep := repWithClasses("com.example.FooTest")
	trace := []string{
		"java.lang.Exception: boom",
		"at java.base/java.lang.reflect.Method.invoke(Method.java:568)",
		"at com.example.FooTest.m(FooTest.kt:42)",
	}
	got := FirstProjectFrame(trace, rep, nil)
	if got != "at com.example.FooTest.m(FooTest.kt:42)" {
		t.Errorf("FirstProjectFrame = %q", got)
	}
	if got := FirstProjectFrame(traceLines(3), nil, nil); got != "" {
		t.Errorf("framework-only: %q, want empty", got)
	}
}
