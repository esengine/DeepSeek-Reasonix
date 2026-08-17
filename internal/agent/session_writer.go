package agent

import (
	"fmt"
	"strings"
	"sync"
)

// SessionWriter is the cross-process write identity for one session file:
// lease, owner metadata, generation, save serialization, and event-log baseline.
// Production saves go through a SessionWriter; one-shot callers use
// SaveWithEphemeralWriter.
type SessionWriter struct {
	lease *SessionLease
	info  SessionLeaseInfo

	// saveMu serializes save cycles issued through this writer. It is held
	// from authority BeginSave until the save's release runs.
	saveMu sync.Mutex

	mu sync.Mutex
	// baseline is the event-log state this writer last persisted or adopted
	// for baselinePath. A zero revisionKnown marks "not yet learned".
	baselinePath   string
	baselineRev    int64
	baselineDigest string
	revisionKnown  bool
	logTail        int64
}

// AcquireSessionWriter takes path's session lease and returns the writer that
// owns it. It fails with *SessionLeaseError when another runtime holds the
// lease.
func AcquireSessionWriter(path string) (*SessionWriter, error) {
	lease, err := TryAcquireSessionLease(path)
	if err != nil {
		return nil, err
	}
	return lease.Writer(), nil
}

// WriterForSessionLease returns the writer facade for an already-held lease
// (keeper and desktop-tab handoff paths keep their own acquire/rebind
// choreography). The facade is cached on the lease so rebinds share one
// save-serialization domain.
func WriterForSessionLease(lease *SessionLease) (*SessionWriter, error) {
	if lease == nil {
		return nil, fmt.Errorf("session lease is nil")
	}
	return lease.Writer(), nil
}

// Lease returns the underlying lease. Callers must not Release it; release
// the writer instead.
func (w *SessionWriter) Lease() *SessionLease {
	if w == nil {
		return nil
	}
	return w.lease
}

// Info returns the writer identity published with the lease.
func (w *SessionWriter) Info() SessionLeaseInfo {
	if w == nil {
		return SessionLeaseInfo{}
	}
	return w.info
}

// WriterID returns this writer's stable identity string.
func (w *SessionWriter) WriterID() string {
	if w == nil {
		return ""
	}
	return w.info.WriterID
}

// PID returns the holding process id.
func (w *SessionWriter) PID() int {
	if w == nil {
		return 0
	}
	return w.info.PID
}

// Hostname returns the holding machine name, when known.
func (w *SessionWriter) Hostname() string {
	if w == nil {
		return ""
	}
	return w.info.Hostname
}

// Path returns the canonical session path this writer owns.
func (w *SessionWriter) Path() string {
	if w == nil {
		return ""
	}
	return w.lease.Path()
}

// Release drops the lease and invalidates every authority minted through
// this writer. It waits for in-flight authority-guarded saves first.
func (w *SessionWriter) Release() {
	if w == nil {
		return
	}
	w.lease.Release()
}

// IssueWriteAuthority mints a generation-bound authority for generation and
// attaches it to this writer, so saves guarded by the authority serialize
// through the writer and update its baseline.
func (w *SessionWriter) IssueWriteAuthority(generation uint64) (*SessionWriteAuthority, error) {
	if w == nil {
		return nil, ErrSessionWriteAuthorityMissing
	}
	auth, err := w.lease.IssueWriteAuthority(generation)
	if err != nil {
		return nil, err
	}
	auth.writer = w
	return auth, nil
}

// Bind issues a generation-bound authority from this writer and binds it to
// sess, putting sess on the production fail-closed save path. Controllers and
// keepers call this after acquiring the writer.
func (w *SessionWriter) Bind(sess *Session, generation uint64) error {
	if w == nil {
		return ErrSessionWriteAuthorityMissing
	}
	if sess == nil {
		return fmt.Errorf("bind session writer: session is nil")
	}
	sess.RequireWriteAuthority()
	auth, err := w.IssueWriteAuthority(generation)
	if err != nil {
		sess.ClearWriteAuthority()
		return err
	}
	sess.BindWriteAuthority(auth)
	sess.syncWriterBaseline(w.Path())
	return nil
}

// Writer returns the writer this authority was minted through, if any.
// Authorities minted directly from a lease (legacy paths) have none.
func (a *SessionWriteAuthority) Writer() *SessionWriter {
	if a == nil {
		return nil
	}
	return a.writer
}

// RecordBaseline stores the event-log state a completed save left behind.
// Only the writer covering path records; other paths are recorded on the
// writer that owns them (a session moved by recovery rebinds writers).
func (w *SessionWriter) RecordBaseline(path string, revision int64, digest string, known bool, logTail int64) {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.baselinePath = canonicalSessionSavePath(path)
	w.baselineRev = revision
	w.baselineDigest = digest
	w.revisionKnown = known
	w.logTail = logTail
}

// WriterBaseline is a snapshot of the writer's event-log baseline.
type WriterBaseline struct {
	Path          string
	Revision      int64
	ContentDigest string
	RevisionKnown bool
	LogTail       int64
}

// Baseline returns the recorded baseline when the writer covers path.
func (w *SessionWriter) Baseline(path string) (WriterBaseline, bool) {
	if w == nil {
		return WriterBaseline{}, false
	}
	canonical := canonicalSessionSavePath(path)
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.baselinePath != canonical {
		return WriterBaseline{}, false
	}
	return WriterBaseline{
		Path:          w.baselinePath,
		Revision:      w.baselineRev,
		ContentDigest: w.baselineDigest,
		RevisionKnown: w.revisionKnown,
		LogTail:       w.logTail,
	}, true
}

// SaveWithEphemeralWriter acquires path's session lease, saves, then releases.
// It leaves any existing authority binding untouched so fork copies stay adoptable.
func (s *Session) SaveWithEphemeralWriter(path string, save func(target string) error) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("empty session path")
	}
	if save == nil {
		save = func(target string) error { return s.SaveSnapshot(target) }
	}
	lease, err := TryAcquireSessionLease(path)
	if err != nil {
		return err
	}
	defer lease.Release()
	return save(path)
}
