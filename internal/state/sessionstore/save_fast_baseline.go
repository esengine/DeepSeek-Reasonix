package sessionstore

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"io"
	"os"
	"strings"

	"reasonix/internal/contract/provider"
	"reasonix/internal/state/store"
)

type fileStampOnDisk struct {
	size, mod int64
	here      bool
}

// baselineStamp prints the four files a save is judged against: the
// checkpoint, the event log, the ledger sidecar and the damage marker.
type baselineStamp [4]fileStampOnDisk

func stampBaseline(path string) baselineStamp {
	var out baselineStamp
	for i, name := range []string{path, store.SessionEventLog(path), store.SessionMeta(path), store.SessionEventLogDamaged(path)} {
		if info, err := os.Stat(name); err == nil {
			out[i] = fileStampOnDisk{size: info.Size(), mod: info.ModTime().UnixNano(), here: true}
		}
	}
	return out
}

// persistedDisk is what this runtime knows about disk right after its own
// completed save: the files' print and how many messages they hold.
type persistedDisk struct {
	stamp baselineStamp
	n     int
	armed bool
}

// armDiskBaseline records the print of the files this save just wrote. It runs
// under the save locks, so no cooperating writer can land between the write and
// the print.
func (s *Session) armDiskBaseline(path string, digest [sha256.Size]byte, n int) {
	stamp := stampBaseline(path)
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.persisted.ok || !s.persisted.saveVerified || s.persisted.path != canonicalSessionSavePath(path) || s.persisted.digest != digest {
		return
	}
	s.persisted.disk = persistedDisk{stamp: stamp, n: n, armed: true}
}

// diskPrefixLen is how many leading messages the next save must account for to
// use fastSnapshotDecision, or -1 when this runtime has no print to trust.
func (s *Session) diskPrefixLen(path string) int {
	st := s.persistState(path)
	if !st.ok || !st.saveVerified || !st.revisionKnown || !st.disk.armed {
		return -1
	}
	return st.disk.n
}

// fastSnapshotDecision answers checkSnapshotWrite without reading the
// transcript back. The files are exactly as this runtime left them, so disk
// holds the persisted messages, and prefix — the digest of next's first n
// messages, taken in the pass that digested all of it — says whether next still
// begins with them. Anything else falls to the full comparison.
func (s *Session) fastSnapshotDecision(path string, next []provider.Message, prefix [sha256.Size]byte) (snapshotWriteDecision, bool) {
	st := s.persistState(path)
	if !st.ok || !st.saveVerified || !st.revisionKnown || !st.disk.armed {
		return snapshotWriteDecision{}, false
	}
	n := st.disk.n
	if n > len(next) || prefix != st.digest || stampBaseline(path) != st.disk.stamp {
		return snapshotWriteDecision{}, false
	}
	for _, m := range next {
		if len(m.Images) > 0 || len(m.UnavailableImages) > 0 {
			return snapshotWriteDecision{}, false
		}
	}
	return snapshotWriteDecision{
		revision:   st.revision,
		upToDate:   len(next) == n,
		appendOnly: len(next) > n,
		appendFrom: n,
	}, true
}

// digestSizeAndPrefix is digestAndSizeSessionMessagesContext that also reports
// the digest of the first k messages, accumulated in the same pass. k < 0 asks
// for none.
func digestSizeAndPrefix(ctx context.Context, msgs []provider.Message, k int) (digest [sha256.Size]byte, size int64, prefix [sha256.Size]byte, err error) {
	h := sha256.New()
	if k == 0 {
		copy(prefix[:], h.Sum(nil))
	}
	w := &countingWriter{w: h}
	enc := json.NewEncoder(w)
	var m provider.Message
	for i := range msgs {
		if err := ctx.Err(); err != nil {
			return digest, 0, prefix, err
		}
		m = messageForSessionIdentity(msgs[i])
		if err := enc.Encode(&m); err != nil {
			return digest, 0, prefix, err
		}
		if i+1 == k {
			copy(prefix[:], h.Sum(nil))
		}
	}
	copy(digest[:], h.Sum(nil))
	return digest, w.n, prefix, nil
}

// countingWriter lets the encoder write each message and its newline straight
// into the hash, so the digest holds no per-message copy of the encoding.
type countingWriter struct {
	w io.Writer
	n int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	c.n += int64(len(p))
	return c.w.Write(p)
}

// snapshotDecision is checkSnapshotWrite, answered from the print when the
// files are as this runtime left them.
func (s *Session) snapshotDecision(path string, msgs []provider.Message, digest, prefix [sha256.Size]byte, version uint64, ownedRewrite bool) (snapshotWriteDecision, error) {
	if decision, ok := s.fastSnapshotDecision(path, msgs, prefix); ok {
		return decision, nil
	}
	return s.checkSnapshotWrite(path, msgs, digest, version, ownedRewrite)
}

// loadAgreesWithLedger reports that what a load read is the transcript the
// ledger describes, with nothing waiting to be repaired. Such a baseline is as
// good as a save's: a snapshot of the session while it is unchanged has nothing
// to write. A ledger that disagrees is what an interrupted save leaves, and the
// first save after the load has to run in full to heal it.
func loadAgreesWithLedger(s *Session, ledgerPresent bool, meta BranchMeta, digest [sha256.Size]byte) bool {
	return ledgerPresent && !s.normalizedDirty && !s.eventLogDamaged &&
		strings.TrimSpace(meta.ContentDigest) == digestString(digest)
}
