// Package discover implements report discovery (plan §5, spec §14): walk with
// SKIP_DIRS pruning, REPORT_DIRS collection, module naming, source labels,
// staleness, --newer-than pre-aggregation filtering, --path glob bypass, and
// a source-file basename index collected during the same walk. Reports are
// decoded in parallel (bounded workers) and merged deterministically sorted
// by path.
package discover

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/istarion/junit-report-parser/internal/clock"
	"github.com/istarion/junit-report-parser/internal/decode"
	"github.com/istarion/junit-report-parser/internal/model"
)

// SkipDirs pruned during the walk (spec §14.1).
var SkipDirs = map[string]bool{
	".git": true, ".hg": true, ".svn": true, ".idea": true,
	".gradle": true, "node_modules": true, ".venv": true, "venv": true,
	"__pycache__": true, ".cache": true, ".tox": true, "bazel-out": true,
}

// ReportDirs are the only directories reports are collected from (§14.1).
var ReportDirs = map[string]bool{
	"test-results": true, "surefire-reports": true, "failsafe-reports": true,
}

var sourceExts = map[string]bool{
	".java": true, ".kt": true, ".kts": true, ".rs": true, ".go": true,
	".py": true, ".ts": true, ".tsx": true, ".js": true, ".scala": true,
	".groovy": true,
}

// FileIndex holds source-file basenames seen during the walk, used later for
// project-frame classification (plan §5, §7).
type FileIndex map[string]struct{}

func (f FileIndex) Has(name string) bool { _, ok := f[name]; return ok }
func (f FileIndex) Len() int             { return len(f) }

// Discover errors map to exit codes at the CLI layer (spec §16).
var (
	ErrNoReports     = errors.New("no report files found")
	ErrNoneParseable = errors.New("reports found but none parseable")
)

type Options struct {
	Root  string
	Paths []string // --path globs: bypass the walk entirely

	HasNewerThan bool
	NewerThan    time.Time // resolved threshold (duration-ago/RFC3339/file mtime)

	MaxAge  time.Duration
	Sources []string // --source labels to keep (empty = all)
	Verbose bool

	Clock   clock.Clock
	Workers int
}

type Result struct {
	Root     string
	RepoRoot string
	Files    FileIndex

	Reports  []*model.Report
	Warnings []model.Warning

	RunTime         time.Time
	RunHasTimestamp bool
	Age             time.Duration
	Stale           bool
}

// Discover runs the full discovery pipeline and returns the selected reports
// plus run-level provenance. Errors: ErrNoReports (exit 2), ErrNoneParseable
// (exit 4), other (usage/IO → exit 3).
func Discover(opts Options) (*Result, error) {
	now := opts.Clock.Now()
	root := opts.Root
	if root == "" {
		root = "."
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	res := &Result{Root: root, Files: FileIndex{}}
	res.RepoRoot = findRepoRoot(rootAbs)

	candidates, err := collectCandidates(opts, res.Files)
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return nil, ErrNoReports
	}

	decoded := decodeAll(candidates, opts.Workers, res.RepoRoot)

	// Deterministic merge: sort by absolute path, then apply filters.
	sort.Slice(decoded, func(i, j int) bool { return decoded[i].path < decoded[j].path })
	decodedCount := 0
	for i := range decoded {
		dr := &decoded[i]
		if dr.err != nil {
			rel := relToRepo(res.RepoRoot, dr.path)
			if errors.Is(dr.err, decode.ErrNotXML) {
				if opts.Verbose {
					res.Warnings = append(res.Warnings, model.Warning{
						Code: "NOT_XML", Path: rel, Message: rel,
					})
				}
				continue
			}
			res.Warnings = append(res.Warnings, model.Warning{
				Code: "PARSE_ERROR", Path: rel, Message: rel,
			})
			continue
		}
		decodedCount++
		for j := range dr.warns {
			// Warning paths are repo-relative for deterministic digests.
			dr.warns[j].Path = relToRepo(res.RepoRoot, dr.path)
		}
		res.Warnings = append(res.Warnings, dr.warns...)
		if dr.module == "" {
			dr.module = filepath.Base(res.RepoRoot)
		}
		dr.report.Path = relToRepo(res.RepoRoot, dr.path)
		for _, c := range dr.report.Cases {
			c.ReportPath = dr.report.Path // repo-relative (plan §3.1, §11 reportPath)
		}
		dr.report.Module = dr.module
		dr.report.Source = dr.source
		res.Reports = append(res.Reports, dr.report)
	}

	if decodedCount == 0 {
		return nil, ErrNoneParseable
	}
	if len(opts.Sources) > 0 {
		keep := map[string]bool{}
		for _, s := range opts.Sources {
			keep[s] = true
		}
		res.Reports = filterReports(res.Reports, func(r *model.Report) bool { return keep[r.Source] })
	}
	if opts.HasNewerThan {
		res.Reports = filterReports(res.Reports, func(r *model.Report) bool {
			t := r.Mtime
			if r.HasTimestamp {
				t = r.Timestamp
			}
			return !t.Before(opts.NewerThan)
		})
	}
	if len(res.Reports) == 0 {
		return nil, ErrNoReports // filtered out: nothing selected (spec §16)
	}

	// Staleness: run timestamp = newest suite timestamp, fallback newest
	// mtime (spec §14.4).
	for _, r := range res.Reports {
		if r.HasTimestamp && (!res.RunHasTimestamp || r.Timestamp.After(res.RunTime)) {
			res.RunTime, res.RunHasTimestamp = r.Timestamp, true
		}
	}
	if !res.RunHasTimestamp {
		for _, r := range res.Reports {
			if r.Mtime.After(res.RunTime) {
				res.RunTime = r.Mtime
			}
		}
	}
	res.Age = now.Sub(res.RunTime)
	if res.Age < 0 {
		res.Age = 0
	}
	if res.Stale = res.Age > opts.MaxAge; res.Stale {
		res.Warnings = append(res.Warnings, model.Warning{
			Code:    "STALE",
			Message: fmt.Sprintf("newest report is %s old", model.FormatAge(res.Age)),
		})
	}
	return res, nil
}

func filterReports(rs []*model.Report, keep func(*model.Report) bool) []*model.Report {
	out := rs[:0:0]
	for _, r := range rs {
		if keep(r) {
			out = append(out, r)
		}
	}
	return out
}

// relToRepo renders an absolute path repo-relative (slash-separated); paths
// outside the repo stay absolute.
func relToRepo(repoRoot, abs string) string {
	if rel, err := filepath.Rel(repoRoot, abs); err == nil && !strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(rel)
	}
	return abs
}

// findRepoRoot walks up from dir to the nearest ancestor containing .git;
// when none is found, dir itself is the repo root (so module names stay
// relative to the discovery root, e.g. in fixture/scratch trees without VCS).
func findRepoRoot(dir string) string {
	start := dir
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir // covers .git dir and .git file (worktrees/submodules)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return start
		}
		dir = parent
	}
}

func collectCandidates(opts Options, idx FileIndex) ([]string, error) {
	if len(opts.Paths) > 0 {
		var out []string
		for _, pattern := range opts.Paths {
			matches, err := filepath.Glob(pattern)
			if err != nil {
				return nil, fmt.Errorf("bad --path glob %q: %w", pattern, err)
			}
			out = append(out, matches...)
		}
		return regularFiles(out), nil
	}
	root, err := filepath.Abs(opts.Root)
	if err != nil {
		return nil, err
	}
	return walkCandidates(root, idx)
}

func regularFiles(paths []string) []string {
	var out []string
	for _, p := range paths {
		if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() {
			out = append(out, p)
		}
	}
	return out
}

// walkCandidates walks root, pruning SKIP_DIRS, collecting XMLs under
// REPORT_DIRS (skipping child dir "binary"), and indexing source-file
// basenames along the way (plan §5).
func walkCandidates(root string, idx FileIndex) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if path == root {
				return err
			}
			return nil // unreadable subtree: skip, run continues
		}
		if d.IsDir() {
			base := d.Name()
			if path != root && SkipDirs[base] {
				return filepath.SkipDir
			}
			if ReportDirs[base] {
				xmls, cerr := collectXMLs(path)
				if cerr == nil {
					out = append(out, xmls...)
				}
				return filepath.SkipDir // subtree handled
			}
			return nil
		}
		if sourceExts[strings.ToLower(filepath.Ext(d.Name()))] {
			idx[d.Name()] = struct{}{}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}

// collectXMLs collects *.xml under a REPORT_DIR, skipping child dir "binary".
func collectXMLs(dir string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != dir && d.Name() == "binary" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.EqualFold(filepath.Ext(d.Name()), ".xml") {
			out = append(out, path)
		}
		return nil
	})
	return out, err
}

type decodedFile struct {
	path   string // absolute
	report *model.Report
	warns  []model.Warning
	err    error
	module string
	source string
}

// decodeAll decodes candidates with a bounded worker pool, naming
// modules/labels relative to repoRoot. Each result is written to its own
// slot keyed by input index; the caller sorts by path for determinism.
func decodeAll(paths []string, workers int, repoRoot string) []decodedFile {
	if workers <= 0 {
		workers = runtime.NumCPU()
		if workers > 8 {
			workers = 8
		}
	}
	if workers > len(paths) {
		workers = len(paths)
	}
	if workers < 1 {
		workers = 1
	}

	results := make([]decodedFile, len(paths))
	type job struct {
		idx  int
		path string
	}
	jobs := make(chan job)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				results[j.idx] = decodeOne(j.path, repoRoot)
			}
		}()
	}
	for i, p := range paths {
		jobs <- job{idx: i, path: p}
	}
	close(jobs)
	wg.Wait()
	return results
}

func decodeOne(path string, repoRoot string) decodedFile {
	out := decodedFile{path: path}
	out.module, out.source = moduleAndSource(repoRoot, path)
	if fi, err := os.Stat(path); err == nil {
		mtime := fi.ModTime()
		rep, warns, err := decode.DecodeFile(path)
		out.report, out.warns, out.err = rep, warns, err
		if out.report != nil {
			out.report.Mtime = mtime
		}
	}
	return out
}

// moduleAndSource derives the module name and source label from the report
// path relative to repoRoot (plan §5): strip `…/build|target/<REPORT_DIR>[/…]`
// from the report directory; whatever remains is the module. An empty
// remainder falls back to the repo root dir name at the call site.
func moduleAndSource(repoRoot, xmlPath string) (module, source string) {
	rel, err := filepath.Rel(repoRoot, filepath.Dir(xmlPath))
	if err != nil {
		return "", ""
	}
	rel = filepath.ToSlash(rel)
	if rel == "." || strings.HasPrefix(rel, "..") {
		rel = ""
	}
	segs := strings.Split(rel, "/")
	for i := 0; i+1 < len(segs); i++ {
		if (segs[i] == "build" || segs[i] == "target") && ReportDirs[segs[i+1]] {
			if i == 0 {
				return "", sourceLabel(segs[i], segs[i+1])
			}
			return strings.Join(segs[:i], "/"), sourceLabel(segs[i], segs[i+1])
		}
	}
	if rel == "" {
		return "", ""
	}
	return rel, ""
}

// sourceLabel: target/surefire-reports → surefire; target/failsafe-reports →
// failsafe; everything else → gradle (plan §5).
func sourceLabel(parent, reportDir string) string {
	switch {
	case parent == "target" && reportDir == "surefire-reports":
		return "surefire"
	case parent == "target" && reportDir == "failsafe-reports":
		return "failsafe"
	default:
		return "gradle"
	}
}
