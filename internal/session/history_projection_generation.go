package session

import (
	"fmt"
	"path/filepath"
)

func historyProjectionGeneration(dir string, viewSequence uint64) (string, error) {
	manifest, err := readStoredManifest(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return "", err
	}
	identity, err := readStorageIdentity(dir, manifest)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s:%d", identity.Generation, viewSequence), nil
}
