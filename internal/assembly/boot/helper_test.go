package boot

import (
	"os"
	"path/filepath"
	"strings"
)

// writeFileRaw writes body to dir/name, trimming a leading newline so test
// literals can start on the line after the backtick. Parent directories are
// created so callers can write nested paths (e.g. .reasonix/skills/x.md).
func writeFileRaw(dir, name, body string) error {
	full := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	return os.WriteFile(full, []byte(strings.TrimPrefix(body, "\n")), 0o644)
}
