package decode

import (
	"bufio"
	"bytes"
	"io"
	"os"
	"strconv"
	"strings"
)

// UnescapeText resolves XML character references in raw report content
// (spec §15: search operates on the decoded model, so text re-read from an
// OutRef span must be decoded before matching). Handles the five predefined
// entities plus decimal and hex numeric references; unknown references are
// left verbatim.
func UnescapeText(s string) string {
	if !strings.Contains(s, "&") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		c := s[i]
		if c != '&' {
			b.WriteByte(c)
			i++
			continue
		}
		semi := strings.IndexByte(s[i:], ';')
		if semi < 0 || semi > 32 { // no plausible reference terminator
			b.WriteByte(c)
			i++
			continue
		}
		ref := s[i+1 : i+semi]
		switch ref {
		case "lt":
			b.WriteByte('<')
		case "gt":
			b.WriteByte('>')
		case "amp":
			b.WriteByte('&')
		case "quot":
			b.WriteByte('"')
		case "apos":
			b.WriteByte('\'')
		default:
			if r, ok := parseNumericRef(ref); ok {
				b.WriteRune(r)
			} else {
				b.WriteString(s[i : i+semi+1]) // unknown: verbatim
			}
		}
		i += semi + 1
	}
	return b.String()
}

func parseNumericRef(ref string) (rune, bool) {
	if len(ref) < 3 || ref[0] != '#' { // shortest: "#<digit>;"
		return 0, false
	}
	body, base := ref[1:], 10
	if body[0] == 'x' || body[0] == 'X' {
		body, base = body[1:], 16
	}
	if body == "" {
		return 0, false
	}
	n, err := strconv.ParseUint(body, base, 32)
	if err != nil {
		return 0, false
	}
	return rune(n), true
}

// ReadRange re-reads the byte range [start,end) of the SANITIZED stream of
// the report at path (an OutRef span). It replays the deterministic reader
// chain from Decode (sniff → latin1 → sanitizer), so offsets reproduce
// exactly; the returned bytes are the raw XML content of the span (possibly
// entity-escaped and CDATA-wrapped — callers decode via UnescapeText).
// Content is streamed: never retained beyond the returned slice's lifetime.
func ReadRange(path string, start, end int64) (string, error) {
	if end <= start {
		return "", nil
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	br := bufio.NewReaderSize(f, 64*1024)
	var src io.Reader = br
	if sniffEncoding(br) == "iso-8859-1" {
		src = newLatin1Reader(br)
	}
	san := newSanitizingReader(src)

	var buf bytes.Buffer
	var (
		pos   int64
		chunk [8192]byte
	)
	for buf.Len() < int(end-start) {
		n, rerr := san.Read(chunk[:])
		if n > 0 {
			lo, hi := pos, pos+int64(n)
			if s, e := max(lo, start), min(hi, end); e > s {
				buf.Write(chunk[s-lo : e-lo])
			}
			pos = hi
		}
		if rerr != nil {
			if rerr == io.EOF {
				break
			}
			return "", rerr
		}
		if n == 0 && rerr == nil {
			continue
		}
	}
	return buf.String(), nil
}
