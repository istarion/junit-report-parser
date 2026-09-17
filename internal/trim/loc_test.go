package trim

import (
	"testing"

	"github.com/istarion/junit-report-parser/internal/model"
)

// Gate ruling (P3): loc must follow plan §9 — first PROJECT frame's
// File:NNN wins even when framework frames precede it.
func TestLocProjectPrecedence(t *testing.T) {
	rep := repWithClasses("com.example.FooTest") // majority: com.example
	files := testIndex("RedisClient.kt")

	tests := []struct {
		name  string
		trace []string
		rep   *model.Report
		files FileIndex
		want  string
	}{
		{
			name: "project frame after framework frames wins",
			trace: []string{
				"java.lang.Exception: boom",
				"at java.base/java.lang.reflect.Method.invoke(Method.java:568)",
				"at com.example.FooTest.m(FooTest.kt:42)",
				"at org.junit.Platform.run(Platform.java:1)",
			},
			rep:   rep,
			files: nil,
			want:  "FooTest.kt:42",
		},
		{
			name: "project via file index beats earlier unknown frame",
			trace: []string{
				"at com.other.Thing.do(Thing.kt:3)", // unknown: no index hit
				"at com.example.cart.RedisClient.connect(cart/RedisClient.kt:31)",
			},
			rep:   nil,
			files: files,
			want:  "RedisClient.kt:31",
		},
		{
			name: "no project frame: first frame's File:NNN",
			trace: []string{
				"java.lang.Exception: boom",
				"at java.base/java.lang.reflect.Method.invoke(Method.java:568)",
			},
			want: "Method.java:568",
		},
		{
			name:  "no frame pattern: first frame verbatim",
			trace: []string{"at java.base/java.lang.Frame.f", "at com.example.FooTest.g"},
			rep:   rep,
			want:  "at java.base/java.lang.Frame.f",
		},
		{
			name:  "no frames at all",
			trace: []string{"just a message"},
			want:  "-",
		},
		{
			name:  "empty trace",
			trace: nil,
			want:  "-",
		},
		{
			name:  "no failure",
			trace: nil,
			want:  "-",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &model.Case{Failure: &model.Failure{Trace: tt.trace}}
			if tt.name == "no failure" {
				c = &model.Case{}
			}
			if got := Loc(c, tt.rep, tt.files); got != tt.want {
				t.Errorf("Loc = %q, want %q", got, tt.want)
			}
		})
	}
}
