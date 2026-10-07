package control

import (
	"errors"
	"os"

	"reasonix/internal/state/sessionstore"
	"reasonix/internal/state/store"
)

func saveFork(path string, child *sessionstore.Session, meta sessionstore.BranchMeta, copyCheckpoints func() error) (err error) {
	lease, err := sessionstore.TryAcquireSessionLease(path)
	if err != nil {
		return err
	}
	defer lease.Release()
	// A collision must never hide or clean up another session's artifacts.
	paths := append([]string{path, sessionstore.CleanupPendingPath(path), ckptDir(path)}, store.SessionSidecarFiles(path)...)
	paths = append(paths, store.SessionSidecarDirs(path)...)
	for _, artifact := range paths {
		if _, err := os.Lstat(artifact); err == nil {
			return os.ErrExist
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	auth, err := lease.IssueWriteAuthority(sessionstore.NextSessionWriteGeneration())
	if err != nil {
		return err
	}
	child.BindWriteAuthority(auth)
	if err := sessionstore.MarkCleanupPending(path, sessionstore.ForkPendingOperation); err != nil {
		return err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, removeSessionArtifacts(path))
		}
	}()
	if err := child.SaveIfAbsent(path); err != nil {
		return err
	}
	if err := sessionstore.SaveBranchMeta(path, meta); err != nil {
		return err
	}
	if err := copyCheckpoints(); err != nil {
		return err
	}
	return sessionstore.ClearCleanupPending(path)
}
