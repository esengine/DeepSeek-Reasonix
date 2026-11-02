package sessionstore

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"reasonix/internal/base/fileutil"
)

// The source belongs to 1.x; native deletion must outlive cleanup without
// changing that store or allowing its next discovery to import the row again.
func v4RemovalPath(sessionPath string) string {
	name, ok := strings.CutSuffix(filepath.Base(sessionPath), ".jsonl")
	id, prefixed := strings.CutPrefix(name, v4ImportPrefix)
	if !ok || !prefixed || !isV4SessionID(id) || v4RootFor(filepath.Dir(sessionPath)) == "" {
		return ""
	}
	return strings.TrimSuffix(sessionPath, ".jsonl") + ".v4-removed"
}

func isV4SessionRemoved(sessionPath string) bool {
	path := v4RemovalPath(sessionPath)
	if path == "" {
		return false
	}
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return false
	}
	defer root.Close()
	_, err = root.Stat(filepath.Base(path))
	return err == nil
}

func preserveV4Removal(sessionPath string) error {
	path := v4RemovalPath(sessionPath)
	if path == "" {
		return nil
	}
	root, err := os.OpenRoot(filepath.Dir(sessionPath))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer root.Close()
	raw, err := root.ReadFile(filepath.Base(CleanupPendingPath(sessionPath)))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var meta CleanupPendingMeta
	if err := json.Unmarshal(raw, &meta); err != nil {
		return err
	}
	switch meta.Operation {
	case "remove", "delete", "clear":
		return fileutil.AtomicWriteFile(path, raw, 0o644)
	default:
		return nil
	}
}
