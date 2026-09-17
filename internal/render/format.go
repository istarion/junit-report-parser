// Package render implements the stdout text digest grammar (spec §10, plan
// §8). One file per command/format; this phase delivers the text summary.
package render

import (
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// EncodeValue encodes one value on a record line (spec §10.2): a bare token
// iff it is non-empty and contains no whitespace and no quote (backslash is
// also quoted to keep the token unambiguous); otherwise a double-quoted
// string using only \" and \\ escapes.
func EncodeValue(s string) string {
	bare := s != "" && !strings.ContainsFunc(s, func(r rune) bool {
		return r == '"' || r == '\\' || unicode.IsSpace(r)
	})
	if bare {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 2)
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// AbbreviateHome shortens an absolute path under the user's home directory
// to a `~`-prefixed path, like the spec §10.4 examples.
func AbbreviateHome(p string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}
	if p == home {
		return "~"
	}
	if strings.HasPrefix(p, home+string(filepath.Separator)) {
		return "~" + p[len(home):]
	}
	return p
}
