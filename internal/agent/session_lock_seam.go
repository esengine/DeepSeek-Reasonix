package agent

import (
	"time"

	"reasonix/internal/store"
)

// SetSessionFileLockWaitForTest shortens the bounded cross-process save-lock
// wait so a test can drive the lock-held path without the full window.
// Restore with the returned function. Production must leave the defaults.
func SetSessionFileLockWaitForTest(wait, poll time.Duration) (restore func()) {
	prevWait, prevPoll := sessionFileLockWait, sessionFileLockPollInterval
	sessionFileLockWait, sessionFileLockPollInterval = wait, poll
	return func() {
		sessionFileLockWait, sessionFileLockPollInterval = prevWait, prevPoll
	}
}

// HoldSessionFileLockForTest takes the session's compatibility save lock the
// way another process would, so saves in this process time out on it.
func HoldSessionFileLockForTest(path string) (release func(), err error) {
	lock, err := tryTakeSessionLockFile(store.SessionLockFile(path))
	if err != nil {
		return nil, err
	}
	return lock.Unlock, nil
}
