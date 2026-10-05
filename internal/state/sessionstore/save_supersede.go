// Superseding a transcript instead of forking a second conversation.
package sessionstore

import (
	"fmt"
	"log/slog"
	"os"
	"time"

	"reasonix/internal/state/store"
)

// supersedingDecision is the write a leaseholder is entitled to over a
// transcript that disagrees with it. Holding a live lease and still failing to
// recognise the file means this runtime lost track of its own — not that
// somebody else wrote it — and forking there hands the user a second identical
// conversation whose copy carries the same mismatch into the next save.
func supersedingDecision(revision int64, repairLog bool) snapshotWriteDecision {
	return snapshotWriteDecision{revision: revision, repairLog: repairLog, supersedes: true}
}

// leaseholderDecision answers a save the content checks could not vouch for by
// asking who holds the lease: a stale or missing authority fails closed, a live
// one supersedes. handled is false only for a session that never bound any
// authority — the low-level CAS path the store's own tests exercise.
func (s *Session) leaseholderDecision(path string, revision int64, repairLog bool) (snapshotWriteDecision, bool, error) {
	if err := s.authorityErrorForPath(path); err != nil {
		return snapshotWriteDecision{}, true, err
	}
	if s.hasValidWriteAuthority(path) {
		return supersedingDecision(revision, repairLog), true, nil
	}
	return snapshotWriteDecision{}, false, nil
}

// keepSupersededTranscript copies what is on disk aside before a leaseholder
// writes over it: the lease makes the write authoritative, but bytes nobody
// chose to discard are still worth keeping. Failures only warn — an
// authoritative save must not wait on an archive of what it replaced.
func keepSupersededTranscript(path string, superseding bool) {
	if !superseding || path == "" {
		return
	}
	current, err := os.ReadFile(path)
	if err != nil || len(current) == 0 {
		return
	}
	dest := store.SessionSuperseded(path)
	if dest == "" {
		return
	}
	header := fmt.Sprintf("// superseded at %s by the runtime holding this session's lease\n",
		time.Now().UTC().Format(time.RFC3339Nano))
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		slog.Warn("session: could not keep the superseded transcript", "path", dest, "err", err)
		return
	}
	defer f.Close()
	if _, err := f.WriteString(header); err != nil {
		slog.Warn("session: could not keep the superseded transcript", "path", dest, "err", err)
		return
	}
	if _, err := f.Write(current); err != nil {
		slog.Warn("session: could not keep the superseded transcript", "path", dest, "err", err)
	}
}
