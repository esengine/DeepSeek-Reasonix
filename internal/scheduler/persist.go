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

// atomicWrite writes data to path atomically via a unique temp file + rename.
// The temp name is unique (os.CreateTemp) so two sessions saving the same
// sidecar concurrently can never collide on one .tmp file, and the rename is
// atomic so readers never observe a partial file. It is the same pattern goal
// state and other session sidecars use.
func atomicWrite(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after a successful rename
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
