package sessionstore

import (
	"errors"
	"os"
	"path/filepath"
	"reasonix/internal/state/store"
	"time"
)

// A spill directory outlived its transcript only by accident: while deleters
// were handed a list of sidecar *files*, the one sidecar that is a directory
// was skipped by every one of them. This sweep collects what they left.

// orphanOutputsGrace keeps the sweep off a directory whose session another
// process may still be creating: the spill directory can exist before the
// transcript is first written, and that window is not this process's to judge.
const orphanOutputsGrace = time.Hour

// reconcileStaleArtifacts collects what a session deleter left on disk, from
// whichever front end ran it. Grouped so a new sweep costs its own file rather
// than another branch in the reconcile path.
func reconcileStaleArtifacts(dir string) error {
	return errors.Join(reconcileRecoveryTrashStages(dir), reconcileOrphanOutputs(dir))
}

// reconcileOrphanOutputs removes spill directories whose transcript is gone.
// Individual failures are collected rather than aborting the sweep.
func reconcileOrphanOutputs(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var errs []error
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		path := filepath.Join(dir, e.Name())
		transcript := store.SessionPathForOutputsDir(path)
		if transcript == "" {
			continue
		}
		if _, err := os.Stat(transcript); !os.IsNotExist(err) {
			continue
		}
		info, err := e.Info()
		if err != nil || time.Since(info.ModTime()) < orphanOutputsGrace {
			continue
		}
		if err := os.RemoveAll(path); err != nil && !os.IsNotExist(err) {
			errs = append(errs, err)
		}
	}
	if err := sweepScratchOutputs(); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// sweepScratchOutputs ages out the last-resort spills. They belong to no
// transcript by construction, so there is no owner to test them against and
// nothing else will ever collect them.
func sweepScratchOutputs() error {
	dir := ScratchOutputsDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var errs []error
	remaining := 0
	for _, e := range entries {
		info, err := e.Info()
		if err != nil || time.Since(info.ModTime()) < orphanOutputsGrace {
			remaining++
			continue
		}
		if err := os.Remove(filepath.Join(dir, e.Name())); err != nil && !os.IsNotExist(err) {
			errs = append(errs, err)
			remaining++
		}
	}
	if remaining == 0 {
		_ = os.Remove(dir)
	}
	return errors.Join(errs...)
}

// ScratchOutputsDir is the spill of last resort, for an agent owning neither a
// transcript nor an archive. Nothing there has an owner to be collected with,
// so reconcileOrphanOutputs ages it out instead.
func ScratchOutputsDir() string {
	return filepath.Join(os.TempDir(), "reasonix-outputs")
}
