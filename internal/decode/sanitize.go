package decode

import (
	"bufio"
	"bytes"
	"io"
	"regexp"
	"strings"
	"unicode/utf8"
)

// sanitizingReader is a deterministic UTF-8 sanitizing io.Reader: invalid
// bytes are replaced with U+FFFD and recorded (spec §17, plan §4).
//
// Determinism contract: the output byte stream is a pure function of the
// input byte stream. Incomplete multibyte sequences split across read
// boundaries are buffered until they can be validated or proven invalid, so
// chunking never changes the output. This is what makes OutRef byte offsets
// replayable later through an identically wrapped reader.
type sanitizingReader struct {
	src     io.Reader
	scratch [4096]byte
	pending []byte // undecoded input bytes
	out     []byte // sanitized output awaiting delivery
	Invalid bool   // saw at least one invalid byte → warn ENCODING
	done    bool
	err     error
}

func newSanitizingReader(r io.Reader) *sanitizingReader {
	return &sanitizingReader{src: r}
}

func (s *sanitizingReader) Read(p []byte) (int, error) {
	for len(s.out) == 0 {
		if s.done {
			return 0, s.err
		}
		n, rerr := s.src.Read(s.scratch[:])
		if n > 0 {
			s.pending = append(s.pending, s.scratch[:n]...)
			s.drain(false)
		}
		if rerr != nil {
			s.done, s.err = true, rerr
			s.drain(true)
		} else if n == 0 {
			continue
		}
	}
	c := copy(p, s.out)
	s.out = s.out[c:]
	return c, nil
}

// drain converts pending bytes into s.out. When final is true, any leftover
// incomplete sequence is replaced rather than awaited.
func (s *sanitizingReader) drain(final bool) {
	for len(s.pending) > 0 {
		b := s.pending[0]
		var need int
		switch {
		case b < 0x80:
			need = 1
		case b < 0xC2: // stray continuation byte or overlong lead (0xC0/0xC1)
			need = -1
		case b < 0xE0:
			need = 2
		case b < 0xF0:
			need = 3
		case b < 0xF5: // 0xF5..0xFF can never start a valid sequence
			need = 4
		default:
			need = -1
		}
		switch {
		case need == 1:
			s.out = append(s.out, b)
			s.pending = s.pending[1:]
		case need == -1:
			s.replace(1)
		case len(s.pending) < need:
			if final || !continuationRun(s.pending[1:]) {
				s.replace(1)
				continue
			}
			return // wait for more input; sequence may complete
		default:
			r, size := utf8.DecodeRune(s.pending)
			if r == utf8.RuneError && size == 1 {
				s.replace(1)
				continue
			}
			s.out = append(s.out, s.pending[:size]...)
			s.pending = s.pending[size:]
		}
	}
}

// replace consumes n bytes from pending and emits one U+FFFD.
func (s *sanitizingReader) replace(n int) {
	s.Invalid = true
	s.out = append(s.out, []byte("\uFFFD")...)
	s.pending = s.pending[n:]
}

// continuationRun reports whether every byte is a UTF-8 continuation byte.
func continuationRun(bs []byte) bool {
	for _, c := range bs {
		if c&0xC0 != 0x80 {
			return false
		}
	}
	return true
}

// latin1Reader converts ISO-8859-1 bytes to UTF-8 (each byte maps to the
// rune of the same code point). Purely deterministic.
type latin1Reader struct {
	src     io.Reader
	scratch [4096]byte
	out     []byte
	pos     int
	done    bool
	err     error
}

func newLatin1Reader(r io.Reader) *latin1Reader {
	return &latin1Reader{src: r}
}

func (l *latin1Reader) Read(p []byte) (int, error) {
	for l.pos >= len(l.out) {
		if l.done {
			return 0, l.err
		}
		n, rerr := l.src.Read(l.scratch[:])
		if n == 0 && rerr == nil {
			continue
		}
		l.out = l.out[:0]
		for _, b := range l.scratch[:n] {
			if b < 0x80 {
				l.out = append(l.out, b)
			} else {
				l.out = append(l.out, 0xC2|byte(b>>6), 0x80|(b&0x3F))
			}
		}
		l.pos = 0
		if rerr != nil {
			l.done, l.err = true, rerr
		}
		if n == 0 {
			return 0, l.err
		}
	}
	c := copy(p, l.out[l.pos:])
	l.pos += c
	return c, nil
}

var encDeclRe = regexp.MustCompile(`(?i)encoding\s*=\s*["']([^"']+)["']`)

// sniffEncoding peeks at the start of br and reports the declared charset,
// normalized, when it is one we convert ourselves ("iso-8859-1"). Everything
// else ("", utf-8, unknown) leaves the sanitized stream to the decoder —
// unknown encodings fall back to sanitize (plan §4).
func sniffEncoding(br *bufio.Reader) string {
	head, _ := br.Peek(1024)
	if i := bytes.Index(head, []byte("?>")); i >= 0 {
		head = head[:i]
	}
	m := encDeclRe.FindSubmatch(head)
	if m == nil {
		return ""
	}
	switch strings.ToLower(string(m[1])) {
	case "iso-8859-1", "latin1", "latin-1", "windows-1252", "cp1252":
		return "iso-8859-1"
	default:
		return ""
	}
}
