package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// removalRaceErrors names the errors a directory being deleted produces while
// it can still be stat'ed; each platform file appends its own.
var removalRaceErrors []error

// vanishedMidWalk reports whether a walk error means the entry was removed
// under the walker. A removal-race error counts only when the path is gone on
// a re-stat, so an unreadable directory that still exists keeps failing.
func vanishedMidWalk(path string, err error) bool {
	if errors.Is(err, fs.ErrNotExist) {
		return true
	}
	racing := false
	for _, e := range removalRaceErrors {
		if errors.Is(err, e) {
			racing = true
		}
	}
	if !racing {
		return false
	}
	for range 50 {
		if _, serr := os.Lstat(path); errors.Is(serr, fs.ErrNotExist) {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

var errRemovalRace = errors.New("fake removal race")

func TestVanishedMidWalkClassifiesByIdentityAndRestat(t *testing.T) {
	old := removalRaceErrors
	removalRaceErrors = append(append([]error(nil), old...), errRemovalRace)
	t.Cleanup(func() { removalRaceErrors = old })

	gone := filepath.Join(t.TempDir(), "gone")
	present := t.TempDir()
	race := &fs.PathError{Op: "open", Path: "x", Err: errRemovalRace}

	if !vanishedMidWalk(gone, fs.ErrNotExist) {
		t.Error("not-exist must count as vanished")
	}
	if !vanishedMidWalk(gone, race) {
		t.Error("a removal-race error on a path that is gone must count as vanished")
	}
	if vanishedMidWalk(present, race) {
		t.Error("a removal-race error on a path that still exists must fail the walk")
	}
	if vanishedMidWalk(gone, errors.New("other")) {
		t.Error("an unrelated error must fail the walk")
	}
	if vanishedMidWalk(gone, fs.ErrPermission) {
		t.Error("a bare permission error must fail the walk")
	}
}
