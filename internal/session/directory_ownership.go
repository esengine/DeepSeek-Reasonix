package session

import (
	"os"
	"path/filepath"
	"strings"

	"reasonix/internal/filelock"
)

func directoryOwnershipPath(dir string) string {
	return filepath.Join(filepath.Dir(dir), "."+filepath.Base(dir)+".ownership.lock")
}

// Keep both claims for a writer's lifetime: the inner claim excludes existing
// writers and the outer claim remains usable while the directory is moved.
func acquireSessionWriter(dir string) (func(), error) {
	releaseDirectory, err := filelock.TryAcquire(directoryOwnershipPath(dir))
	if err != nil {
		return nil, err
	}
	releaseWriter, err := filelock.TryAcquire(filepath.Join(dir, "writer.lock"))
	if err != nil {
		releaseDirectory()
		return nil, err
	}
	return func() {
		releaseWriter()
		releaseDirectory()
	}, nil
}

// ProbeWriterHeld reports whether some runtime currently owns the session's
// writer lease. It is the occupancy oracle for final-format identities: the
// takeover protocol asks the holder to stand down and then watches this probe
// turn false before re-acquiring. A missing session directory counts as free.
func ProbeWriterHeld(dir string) bool {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return false
	}
	if _, err := os.Stat(filepath.Join(dir, "manifest.json")); err != nil {
		return false
	}
	release, err := filelock.TryAcquire(filepath.Join(dir, "writer.lock"))
	if err != nil {
		return true
	}
	release()
	return false
}
