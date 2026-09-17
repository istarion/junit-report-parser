// Package trim implements frame classification, trace trimming and
// fingerprints (plan §7, spec §13).
package trim

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/istarion/junit-report-parser/internal/model"
)

// FrameKind classifies one `at …` trace frame (spec §13 step 2).
type FrameKind int

const (
	FrameHeader FrameKind = iota // not an `at` frame: exception header, Caused by, "… N more"
	FrameProject
	FrameUnknown
	FrameFramework
)

// denylist is VERBATIM from spec §13.
var denylist = []string{
	"org.junit.", "junit.", "org.opentest4j.", "java.", "javax.", "jdk.",
	"sun.", "kotlin.reflect.", "kotlinx.coroutines.", "org.gradle.",
	"worker.org.gradle.", "org.springframework.test.", "org.mockito.",
	"io.mockk.", "org.assertj.core.internal.", "org.testcontainers.",
	"org.apache.maven.",
}

// FileIndex is the discovery source-basename index (only Has is required, so
// discover.FileIndex satisfies it without a dependency).
type FileIndex interface {
	Has(name string) bool
}

// FrameClass extracts the fully-qualified class name from an `at` frame:
// "at module/java.lang.Runnable.run(Run.java)" → "java.lang.Runnable".
// Returns "" for non-frames.
func FrameClass(line string) string {
	l := strings.TrimSpace(line)
	if !strings.HasPrefix(l, "at ") {
		return ""
	}
	seg := l[3:]
	if i := strings.IndexByte(seg, '('); i >= 0 {
		seg = seg[:i]
	}
	seg = strings.TrimSpace(seg)
	// Strip JVM module/source prefixes: "java.base/java.lang.Runnable".
	if i := strings.LastIndexByte(seg, '/'); i >= 0 {
		seg = seg[i+1:]
	}
	// Drop the method: everything after the last dot of the pre-paren part.
	i := strings.LastIndexByte(seg, '.')
	if i < 0 {
		return ""
	}
	return seg[:i]
}

var frameLocRe = regexp.MustCompile(`\(([^():]+):([0-9]+)\)\s*$`)

// Loc returns the fail/case record location (plan §9): `basename:NNN` from
// the first PROJECT frame's trailing "(…File.ext:NNN)", else from the first
// frame's, else the first `at` frame verbatim, else "-". Computed here (the
// classification owner) so render stays decoupled from trim.
func Loc(c *model.Case, rep *model.Report, files FileIndex) string {
	if c == nil || c.Failure == nil {
		return "-"
	}
	var (
		firstFrame    string
		firstFrameLoc string
		projectLoc    string
	)
	for _, line := range c.Failure.Trace {
		if FrameClass(line) == "" {
			continue
		}
		l := strings.TrimSpace(line)
		if firstFrame == "" {
			firstFrame = l
			if name, ok := frameSourceLoc(l); ok {
				firstFrameLoc = name
			}
		}
		if projectLoc == "" && Classify(l, rep, files) == FrameProject {
			if name, ok := frameSourceLoc(l); ok {
				projectLoc = name
			}
		}
	}
	if projectLoc != "" {
		return projectLoc
	}
	if firstFrameLoc != "" {
		return firstFrameLoc
	}
	if firstFrame != "" {
		return firstFrame
	}
	return "-"
}

// frameSourceLoc extracts "basename:NNN" from a frame's trailing
// "(…File.ext:NNN)".
func frameSourceLoc(frame string) (string, bool) {
	m := frameLocRe.FindStringSubmatch(frame)
	if m == nil {
		return "", false
	}
	return filepath.Base(m[1]) + ":" + m[2], true
}

// PackageOf returns the package of a dotted class name; "(default)" for the
// default package (spec §6 stats --by package convention).
func PackageOf(class string) string {
	i := strings.LastIndexByte(class, '.')
	if i < 0 {
		return "(default)"
	}
	return class[:i]
}

// SourceFile extracts the source basename from a frame's trailing
// "(…File.ext:NNN)", e.g. "at p.C.m(com/x/FooTest.kt:42)" → "FooTest.kt".
func SourceFile(line string) (string, bool) {
	l := strings.TrimSpace(line)
	i := strings.LastIndexByte(l, '(')
	if i < 0 {
		return "", false
	}
	inner := l[i+1:]
	if j := strings.LastIndexByte(inner, ')'); j >= 0 {
		inner = inner[:j]
	}
	// Need at least one dot and a ":NNN" suffix.
	k := strings.LastIndexByte(inner, ':')
	if k < 0 {
		return "", false
	}
	name := filepath.Base(inner[:k])
	if name == "" || !strings.Contains(name, ".") {
		return "", false
	}
	return name, true
}

// Classify returns the kind of a trace line: headers (non-`at` lines) are
// FrameHeader; `at` frames are framework (denylist), project (source basename
// in the FileIndex, or class package == the report's majority package) or
// unknown (plan §7).
func Classify(line string, rep *model.Report, files FileIndex) FrameKind {
	class := FrameClass(line)
	if class == "" {
		return FrameHeader
	}
	for _, p := range denylist {
		if strings.HasPrefix(class, p) {
			return FrameFramework
		}
	}
	if name, ok := SourceFile(line); ok && files != nil && files.Has(name) {
		return FrameProject
	}
	if rep != nil && PackageOf(class) == MajorityPackage(rep) {
		return FrameProject
	}
	return FrameUnknown
}

// MajorityPackage is the mode of classname packages among the report's cases
// (ties broken by lexicographically smaller package, deterministic).
func MajorityPackage(rep *model.Report) string {
	if rep == nil {
		return ""
	}
	count := map[string]int{}
	for _, c := range rep.Cases {
		if c.ClassName != "" {
			count[PackageOf(c.ClassName)]++
		}
	}
	best, bestN := "", 0
	for p, n := range count {
		if n > bestN || (n == bestN && p < best) {
			best, bestN = p, n
		}
	}
	return best
}

// Options control trace trimming.
type Options struct {
	MaxFrames int  // kept project/unknown frames (default 5, spec §13)
	FullStack bool // --full-stack: disable trimming entirely
	Files     FileIndex
}

// Result of trimming one trace.
type Result struct {
	Frames []string // kept frames, verbatim, in original order
	Hidden int      // raw frames not kept (drives `trunc <n> frames hidden`)
}

// FrameCount returns the number of `at` frames in a trace (headers and
// other lines excluded). Used by the small budget's 0-frame mode.
func FrameCount(trace []string) int {
	n := 0
	for _, l := range trace {
		if FrameClass(l) != "" {
			n++
		}
	}
	return n
}

// TrimTrace trims one failure trace (plan §7): exception headers incl. the
// full `Caused by:` chain are kept implicitly (they are never returned as
// frames); project and unknown frames are kept up to MaxFrames; bridging
// framework frames are dropped. When nothing survives, the first MaxFrames
// raw frames are emitted as fallback (fixture 21). FullStack returns every
// frame untrimmed. Kept frames are whitespace-trimmed (raw XML traces indent
// with tabs; the digest re-indents with the continuation prefix).
func TrimTrace(trace []string, rep *model.Report, opts Options) Result {
	var raw []string
	for _, l := range trace {
		if FrameClass(l) != "" {
			raw = append(raw, strings.TrimSpace(l))
		}
	}
	max := opts.MaxFrames
	if max <= 0 {
		max = 5
	}
	if opts.FullStack {
		return Result{Frames: append([]string(nil), raw...), Hidden: 0}
	}

	kept := make([]string, 0, len(raw))
	for _, f := range raw {
		switch Classify(f, rep, opts.Files) {
		case FrameProject, FrameUnknown:
			if len(kept) == max {
				continue // capped; still counted as hidden
			}
			kept = append(kept, f)
		} // framework: dropped (bridging noise)
	}
	if len(kept) == 0 && len(raw) > 0 {
		// Framework-only trace: fall back to the first MaxFrames raw frames.
		if len(raw) > max {
			kept = append([]string(nil), raw[:max]...)
		} else {
			kept = append([]string(nil), raw...)
		}
	}
	return Result{Frames: kept, Hidden: len(raw) - len(kept)}
}
