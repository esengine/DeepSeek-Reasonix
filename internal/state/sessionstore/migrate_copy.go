package sessionstore

// copySessionFile copies one message-format session into the import, reporting
// whether it landed. A copy that fails has to reach the caller: the one-time
// import marker is only truthful when every session arrived.
func copySessionFile(jsonlPath, dest string) bool {
	if isNativeSessionEventLog(SessionEventLogPath(jsonlPath)) {
		return saveNativeSessionCopy(jsonlPath, dest) == nil
	}
	return transformAndCopyJsonl(jsonlPath, dest) == nil
}
