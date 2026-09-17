package accept

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
)

// generate writes synthetic fixture content at test start. Keeping the
// generator checked in (rather than the multi-MB output) keeps the repo lean
// (fixture 8).
func generate(kind, root string) error {
	switch kind {
	case "bigout":
		return generateBigOut(root)
	default:
		return fmt.Errorf("unknown generator %q", kind)
	}
}

// markerBigOut tags the 5 MB captured-output block so tests can assert its
// absence from default output.
const markerBigOut = "HUGEOUT-MARKER"

// generateBigOut writes a small passing suite with a ~5 MB CDATA system-out.
func generateBigOut(root string) error {
	dir := filepath.Join(root, "app", "build", "test-results", "test")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	var b bytes.Buffer
	b.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
	b.WriteString("<testsuite name=\"com.example.BigOutTest\" tests=\"2\" failures=\"0\" errors=\"0\" skipped=\"0\" " +
		"timestamp=\"2026-09-17T11:58:00Z\" time=\"0.2\">\n")
	b.WriteString("  <testcase name=\"p1\" classname=\"com.example.BigOutTest\" time=\"0.1\"/>\n")
	b.WriteString("  <testcase name=\"p2\" classname=\"com.example.BigOutTest\" time=\"0.1\"/>\n")
	b.WriteString("  <system-out><![CDATA[\n")
	line := markerBigOut + " 0123456789 abcdefghijklmnopqrstuvwxyz captured log filler line\n"
	for b.Len() < 5<<20 { // ~5 MB
		b.WriteString(line)
	}
	b.WriteString("]]></system-out>\n")
	b.WriteString("</testsuite>\n")
	return os.WriteFile(filepath.Join(dir, "TEST-com.example.BigOutTest.xml"), b.Bytes(), 0o644)
}
