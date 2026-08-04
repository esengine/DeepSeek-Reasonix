package scheduler

import (
	"encoding/json"
	"os"
	"path/filepath"

	fileencoding "reasonix/internal/fileutil/encoding"
)

// saveTasks writes the task list to path as a compact JSON array. It is
// called by saveLocked, which serializes concurrent writes.
func saveTasks(path string, views []View) error {
	data, err := json.Marshal(views)
	if err != nil {
		return err
	}
	return atomicWrite(path, data)
}

// loadTasks reads the sidecar written by saveTasks. Malformed or unreadable
// files yield nil (callers treat an empty list as no tasks); a corrupt
// sidecar must not block session startup.
func loadTasks(path string) []Task {
	data, err := fileencoding.ReadFileUTF8(path)
	if err != nil {
		if !os.IsNotExist(err) {
			// Corrupt/unreadable sidecar: ignore rather than fail startup.
			// The next save overwrites it.
		}
		return nil
	}
	var tasks []Task
	if err := json.Unmarshal(data, &tasks); err != nil {
		return nil
	}
	return tasks
}

// atomicWrite writes data to path atomically via a temp file + rename. It is
// the same pattern goal state and other session sidecars use.
func atomicWrite(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
