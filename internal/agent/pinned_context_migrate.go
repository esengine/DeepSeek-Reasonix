package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"reasonix/internal/fileutil"
	"reasonix/internal/store"
)

func migratePinnedContextSidecar(oldPath, newPath, newID string) error {
	source := store.SessionPinnedContext(oldPath)
	// As with the other session sidecars, an overlong source component cannot
	// exist. Avoid turning the recovery pass itself into ENAMETOOLONG.
	if len(filepath.Base(source)) > nameMaxBytes {
		return nil
	}
	raw, err := os.ReadFile(source)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var state map[string]json.RawMessage
	if err := json.Unmarshal(raw, &state); err != nil {
		return fmt.Errorf("decode pinned context sidecar: %w", err)
	}
	encodedID, err := json.Marshal(newID)
	if err != nil {
		return err
	}
	state["sessionId"] = encodedID
	raw, err = json.Marshal(state)
	if err != nil {
		return err
	}
	if err := fileutil.AtomicWriteFileStrict(store.SessionPinnedContext(newPath), append(raw, '\n'), 0o600); err != nil {
		return err
	}
	if err := os.Remove(source); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
