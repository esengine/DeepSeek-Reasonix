package sessionstore

import "reasonix/internal/state/store"

// sessionSidecarMoves pairs what a renamed session carries from oldPath to
// newPath, in order. Blobs go before the event log that names them, so a
// rename cut short never leaves a log whose images stayed behind.
func sessionSidecarMoves(oldPath, newPath string) [][2]string {
	return [][2]string{
		{store.SessionGoalState(oldPath), store.SessionGoalState(newPath)},
		{store.SessionBlobsDir(oldPath), store.SessionBlobsDir(newPath)},
		{store.SessionEventLog(oldPath), store.SessionEventLog(newPath)},
		{store.SessionEventLogDamaged(oldPath), store.SessionEventLogDamaged(newPath)},
		{store.SessionEventIndex(oldPath), store.SessionEventIndex(newPath)},
		{store.SessionConflictLog(oldPath), store.SessionConflictLog(newPath)},
		{store.SessionRecoveryState(oldPath), store.SessionRecoveryState(newPath)},
		{store.SessionCheckpointDir(oldPath), store.SessionCheckpointDir(newPath)},
		{store.SessionJobsDir(oldPath), store.SessionJobsDir(newPath)},
		{store.SessionInboxDir(oldPath), store.SessionInboxDir(newPath)},
	}
}
