package sessionstore

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"reasonix/internal/state/store"
)

var errNotInactive = errors.New("session is no longer inactive")

// beforeArchiveLock runs between the unlocked check and the locked re-check.
var beforeArchiveLock func(path string)

// PinSessions marks every listed conversation pinned. It only adds: the kernel
// is the authority, and a window lacking a pin another one made cannot remove it.
func PinSessions(paths []string) error {
	var errs error
	for _, p := range paths {
		errs = errors.Join(errs, SetSessionPinned(p, true))
	}
	return errs
}

// ArchiveInactiveSessions archives each conversation in dir idle for at least
// idle, leaving pinned, leased, mid-turn, subagent and superseded ones. Only
// catalog state changes. hold names paths the caller keeps for a reason the
// store cannot see. Returns how many moved.
func ArchiveInactiveSessions(dir string, now time.Time, idle time.Duration, hold func(path string) bool) (int, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" || idle <= 0 {
		return 0, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	archived := 0
	var errs error
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !store.IsSessionTranscriptName(name) || store.IsSubagentTranscriptName(name) {
			continue
		}
		path := filepath.Join(dir, name)
		if !IsVisibleSession(path) || (hold != nil && hold(path)) {
			continue
		}
		cheap, _, err := LoadBranchMeta(path)
		if err != nil || !archivable(path, cheap, now, idle) {
			continue
		}
		if beforeArchiveLock != nil {
			beforeArchiveLock(path)
		}
		err = UpdateBranchMeta(path, false, func(m *BranchMeta) error {
			if !archivable(path, *m, now, idle) {
				return errNotInactive
			}
			m.Archived = true
			m.AutoArchivedAt = now
			return nil
		})
		switch {
		case err == nil:
			archived++
		case !errors.Is(err, errNotInactive):
			errs = errors.Join(errs, err)
		}
	}
	return archived, errs
}

func archivable(path string, m BranchMeta, now time.Time, idle time.Duration) bool {
	return !m.Archived && !m.Pinned && !m.Superseded && m.InFlightTurn == nil && !m.Unread() &&
		now.Sub(lastActivity(path, m)) >= idle && !SessionLeaseHeld(path)
}

func lastActivity(path string, m BranchMeta) time.Time {
	at := SessionContentModTime(path)
	if m.UpdatedAt.After(at) {
		at = m.UpdatedAt
	}
	return at
}
