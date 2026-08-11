package checkpoint

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"reasonix/internal/fileutil"
)

// writeJSONAtomic marshals v and publishes it via AtomicWriteFileStrict.
func writeJSONAtomic(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return fileutil.AtomicWriteFileStrict(path, b, 0o644)
}

// readJSONFile unmarshals path into v.
func readJSONFile(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("decode %s: %w", filepath.Base(path), err)
	}
	return nil
}
