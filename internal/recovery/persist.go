package recovery

import (
	"encoding/json"
	"os"

	"reasonix/internal/fileutil"
	fileencoding "reasonix/internal/fileutil/encoding"
	"reasonix/internal/store"
)

// PathFor returns the recovery state sidecar for a main session path.
// Example: session.jsonl → session.recovery.json
func PathFor(sessionPath string) string {
	return store.SessionRecoveryState(sessionPath)
}

// SaveSnapshot writes the recovery gate state beside the session file.
func SaveSnapshot(sessionPath string, snap Snapshot) error {
	path := PathFor(sessionPath)
	if path == "" {
		return nil
	}
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return err
	}
	// Failure excerpts and command arguments may contain project-sensitive
	// details. Publish atomically with owner-only permissions so concurrent
	// root/sub-agent snapshots never expose a truncated JSON document.
	return fileutil.AtomicWriteFile(path, data, 0o600)
}

// LoadSnapshot reads a previously saved recovery gate state.
// Missing files return an empty snapshot and nil error.
func LoadSnapshot(sessionPath string) (Snapshot, error) {
	path := PathFor(sessionPath)
	if path == "" {
		return Snapshot{}, nil
	}
	data, err := fileencoding.ReadFileUTF8(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Snapshot{}, nil
		}
		return Snapshot{}, err
	}
	var snap Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return Snapshot{}, err
	}
	if snap.Tasks == nil {
		snap.Tasks = map[string]*TaskState{}
	}
	return snap, nil
}
