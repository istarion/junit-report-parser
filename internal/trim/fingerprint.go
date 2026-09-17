package trim

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"

	"github.com/istarion/junit-report-parser/internal/model"
)

// Conservative message normalization (plan D4, spec §13): collapse
// whitespace; replace UUIDs, IPv4/IPv6, ISO-8601 timestamps, epoch stamps
// (10/13-digit), hex tokens ≥ 8 chars and digit runs ≥ 6 with a placeholder.
// Short numerics ("expected: <3>") stay distinct. Replacement order is
// specific-first so compound forms (UUIDs, timestamps) normalize atomically.
var (
	reUUID  = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)
	reISO   = regexp.MustCompile(`\d{4}-\d{2}-\d{2}[Tt ]\d{2}:\d{2}(:\d{2}(\.\d+)?)?(Z|[+-]\d{2}:?\d{2})?`)
	reEpoch = regexp.MustCompile(`\b(?:[12]\d{12}|\d{10})\b`) // 13-digit ms, 10-digit s
	reIPv4  = regexp.MustCompile(`\b\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}\b`)
	reIPv6  = regexp.MustCompile(`(?:(?::)(?::[0-9a-fA-F]{1,4})+|(?:[0-9a-fA-F]{1,4}:){1,7}:(?:[0-9a-fA-F]{1,4}(?::[0-9a-fA-F]{1,4}){0,6})?|(?:[0-9a-fA-F]{1,4}:){7}[0-9a-fA-F]{1,4})`)
	reHex   = regexp.MustCompile(`\b[0-9a-fA-F]{8,}\b`)
	reDigit = regexp.MustCompile(`\b\d{6,}\b`)
)

const placeholder = "<var>"

// NormalizeMessage applies the conservative normalization.
func NormalizeMessage(msg string) string {
	out := reUUID.ReplaceAllString(msg, placeholder)
	out = reISO.ReplaceAllString(out, placeholder)
	out = reEpoch.ReplaceAllString(out, placeholder)
	out = reIPv4.ReplaceAllString(out, placeholder)
	out = reIPv6.ReplaceAllString(out, placeholder)
	out = reHex.ReplaceAllString(out, placeholder)
	out = reDigit.ReplaceAllString(out, placeholder)
	return strings.Join(strings.Fields(out), " ")
}

// FirstProjectFrame returns the first raw trace frame classified as project
// ("" when none). It is independent of --max-frames, so fingerprints stay
// stable across budget changes.
func FirstProjectFrame(trace []string, rep *model.Report, files FileIndex) string {
	for _, l := range trace {
		if Classify(l, rep, files) == FrameProject {
			return strings.TrimSpace(l)
		}
	}
	return ""
}

// Fingerprint is the observable grouping key (spec §13): first 8 hex of
// SHA-256 over type + "\x00" + normalized message + "\x00" + first project
// frame.
func Fingerprint(failureType, message string, trace []string, rep *model.Report, files FileIndex) string {
	h := sha256.Sum256([]byte(
		failureType + "\x00" + NormalizeMessage(message) + "\x00" + FirstProjectFrame(trace, rep, files)))
	return hex.EncodeToString(h[:4]) // first 8 hex chars
}

// FingerprintOf is the convenience form for a failed case.
func FingerprintOf(c *model.Case, rep *model.Report, files FileIndex) string {
	if c.Failure == nil {
		return ""
	}
	return Fingerprint(c.Failure.Type, c.Failure.Message, c.Failure.Trace, rep, files)
}
