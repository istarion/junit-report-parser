package cli

import (
	"os"
	"time"
)

// osWriteFile creates a file with the given content and mtime (test helper).
func osWriteFile(path string, data []byte, mtime time.Time) error {
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return err
	}
	return os.Chtimes(path, mtime, mtime)
}
