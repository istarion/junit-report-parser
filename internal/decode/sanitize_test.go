package decode

import (
	"bufio"
	"io"
	"strings"
	"testing"
)

// chunkReader yields data in fixed-size chunks.
type chunkReader struct {
	data []byte
	pos  int
	chk  int
}

func (c *chunkReader) Read(p []byte) (int, error) {
	if c.pos >= len(c.data) {
		return 0, io.EOF
	}
	n := c.chk
	if n > len(c.data)-c.pos {
		n = len(c.data) - c.pos
	}
	if n > len(p) {
		n = len(p)
	}
	copy(p, c.data[c.pos:c.pos+n])
	c.pos += n
	return n, nil
}

// sanitizeString runs data through the sanitizer reading in chunk-byte reads.
func sanitizeString(t *testing.T, in string, chunk int) (string, bool) {
	t.Helper()
	s := newSanitizingReader(&chunkReader{data: []byte(in), chk: chunk})
	var b strings.Builder
	buf := make([]byte, 64)
	for {
		n, err := s.Read(buf)
		b.Write(buf[:n])
		if err == io.EOF {
			return b.String(), s.Invalid
		}
		if err != nil {
			t.Fatalf("read: %v", err)
		}
	}
}

// Property: sanitizing output is chunking-independent; multibyte chars split
// across reader boundaries survive (plan §4, §11).
func TestSanitizerChunkIndependence(t *testing.T) {
	input := "ascii ünïcödé 日本語 \U0001F600 tail \xe4\xb8\xad end"
	want, _ := sanitizeString(t, input, 1<<20)
	for chunk := 1; chunk <= 12; chunk++ {
		got, _ := sanitizeString(t, input, chunk)
		if got != want {
			t.Fatalf("chunk=%d: got %q, want %q", chunk, got, want)
		}
	}
}

func TestSanitizerReplacesInvalidBytes(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"clean", "hello", "hello"},
		{"lone continuation", "a\x80b", "a\uFFFDb"},
		{"overlong lead", "a\xc0\xafb", "a\uFFFD\uFFFDb"},
		{"truncated 3-byte at eof", "a\xe4\xb8", "a\uFFFD\uFFFD"},
		{"complete 3-byte survives", "a\xe4\xb8\xad", "a中"},
		{"0xff", "\xff", "\uFFFD"},
		{"real replacement char passthrough", "a\uFFFDb", "a\uFFFDb"},
		{"invalid after valid lead", "\xe4\x41", "\uFFFDA"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, chunk := range []int{1, 2, 3, 1 << 16} {
				got, _ := sanitizeString(t, tt.in, chunk)
				if got != tt.want {
					t.Errorf("chunk=%d: got %q, want %q", chunk, got, tt.want)
				}
			}
		})
	}
}

func TestSanitizerInvalidFlag(t *testing.T) {
	if _, invalid := sanitizeString(t, "plain ok", 7); invalid {
		t.Error("clean input must not set Invalid")
	}
	if _, invalid := sanitizeString(t, "bad \xff byte", 7); !invalid {
		t.Error("invalid input must set Invalid")
	}
}

func TestLatin1Reader(t *testing.T) {
	tests := []struct {
		in    string
		chunk int
		want  string
	}{
		{"caf\xe9", 2, "café"},
		{"na\xefve", 1, "naïve"},
		{"", 4, ""},
		{"\x00\x41", 3, "\x00A"},
	}
	for _, tt := range tests {
		l := newLatin1Reader(&chunkReader{data: []byte(tt.in), chk: tt.chunk})
		got, err := io.ReadAll(l)
		if err != nil {
			t.Fatalf("chunk=%d: %v", tt.chunk, err)
		}
		if string(got) != tt.want {
			t.Errorf("latin1(%q, chunk=%d) = %q, want %q", tt.in, tt.chunk, got, tt.want)
		}
	}
}

func TestSniffEncoding(t *testing.T) {
	tests := []struct {
		head string
		want string
	}{
		{`<?xml version="1.0" encoding="ISO-8859-1"?>`, "iso-8859-1"},
		{`<?xml version="1.0" encoding='iso-8859-1'?>`, "iso-8859-1"},
		{`<?xml version="1.0" encoding="UTF-8"?>`, ""},
		{`<?xml version="1.0"?>`, ""},
		{`<testsuite>`, ""},
	}
	for _, tt := range tests {
		if got := sniffEncoding(bufio.NewReader(strings.NewReader(tt.head))); got != tt.want {
			t.Errorf("sniffEncoding(%q) = %q, want %q", tt.head, got, tt.want)
		}
	}
}
