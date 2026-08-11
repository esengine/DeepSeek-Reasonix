package agent

import (
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"strings"

	"reasonix/internal/provider"
)

// shutdownRecoverySessionPath names this writer's isolated copy. A content
// digest would name a new file per save, and the depth cap saturates instead
// of terminating, so the chain would never end (#8342).
func shutdownRecoverySessionPath(originalPath string) string {
	writerDigest := sha256.Sum256([]byte(SessionWriterID()))
	suffix := fmt.Sprintf("-recovery-%x", writerDigest[:6])
	id := BranchID(originalPath)
	if strings.HasSuffix(id, suffix) {
		// Already this writer's copy: rewriting it in place is what bounds the
		// chain, since each recovery becomes the parent of the next one.
		return originalPath
	}
	return filepath.Join(filepath.Dir(originalPath),
		fmt.Sprintf("%s%s.jsonl", recoveryParentStem(id), suffix))
}

// writeRecoveryEventLog seeds a recovery branch's event log with the recovered
// transcript. A replace event carries the whole transcript, so the rewritten
// isolated lane compacts instead: appending would trade one file per save for
// one transcript per save in a single log.
func writeRecoveryEventLog(path string, msgs []provider.Message, digest [sha256.Size]byte, isolated bool) error {
	if isolated {
		return compactSessionEventLog(path, msgs, digest, 0, "recovery")
	}
	return appendSessionReplaceEvent(path, msgs, digest, 0, "recovery")
}
