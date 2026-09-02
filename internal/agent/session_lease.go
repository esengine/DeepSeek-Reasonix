package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"reasonix/internal/fileutil"
	fileencoding "reasonix/internal/fileutil/encoding"
	"reasonix/internal/store"
)

var ErrSessionLeaseHeld = errors.New("session lease held by another runtime")

// sessionLeaseOwners reserves a canonical session path for one in-process
// acquisition attempt or live lease. sessionLeaseActiveOwners contains only
// generations that have acquired the cross-process lock and written their
// lease metadata. Keeping the two states separate prevents ownership-sensitive
// repair from treating a pending or failed acquisition as proof of ownership.
// Storing identities instead of bare sentinels lets release and reclaim use
// CompareAndDelete without an old generation evicting a newer one.
var (
	sessionLeaseOwners       sync.Map
	sessionLeaseActiveOwners sync.Map
	sessionLeaseSeq          atomic.Uint64
)

type SessionLeaseInfo struct {
	SessionPath string    `json:"session_path"`
	WriterID    string    `json:"writer_id"`
	PID         int       `json:"pid"`
	Hostname    string    `json:"hostname,omitempty"`
	AcquiredAt  time.Time `json:"acquired_at"`
	// HandoffTo, when non-empty, marks this lease as handed off by the
	// previous holder to the named runtime (writer_id). The OS lock is free
	// while this field is set; only the named target may acquire it, and only
	// until HandoffExpiresAt. This is the explicit, user-authorized path for
	// transferring session ownership between runtimes (serve <-> desktop).
	HandoffTo        string    `json:"handoff_to,omitempty"`
	HandoffExpiresAt time.Time `json:"handoff_expires_at,omitempty"`
}

const sessionLeaseOwnerOffset int64 = 1

func sessionLeaseOwnerBytes(b []byte) []byte {
	payload := make([]byte, sessionLeaseOwnerOffset+int64(len(b)))
	payload[0] = ' '
	copy(payload[sessionLeaseOwnerOffset:], b)
	return payload
}

// HandoffWindow is how long a released-for-handoff lease stays reserved for
// its intended target before falling back to normal acquire semantics.
const HandoffWindow = 30 * time.Second

type SessionLeaseError struct {
	Path string
	Info *SessionLeaseInfo
}

func (e *SessionLeaseError) Error() string {
	if e == nil {
		return ErrSessionLeaseHeld.Error()
	}
	if e.Info != nil && e.Info.WriterID != "" {
		return fmt.Sprintf("%s: %s is held by %s", ErrSessionLeaseHeld, e.Path, e.Info.WriterID)
	}
	return fmt.Sprintf("%s: %s", ErrSessionLeaseHeld, e.Path)
}

func (e *SessionLeaseError) Unwrap() error {
	return ErrSessionLeaseHeld
}

type SessionLease struct {
	path            string
	ownerID         uint64
	mu              sync.Mutex
	leaseLock       *sessionLockFile
	released        bool
	writeGeneration uint64
	// activeSaves counts authority-guarded save cycles still inside path/file
	// locks. Release waits for this to reach zero so a rebind cannot revoke
	// mid-write and create an ABA ownership hole.
	activeSaves int
	// releaseWait is closed when activeSaves drains to zero while a Release
	// is waiting. At most one waiter is parked.
	releaseWait chan struct{}
	// writerOnce caches the SessionWriter facade for this lease. One writer
	// per lease keeps the writer's save serialization meaningful across
	// controller rebinds.
	writerOnce sync.Once
	writer     *SessionWriter
	// beforeReleaseLock is a test hook for the registry-before-unlock invariant.
	beforeReleaseLock func()
	// beforeReleaseWait is a test hook reached only after Release observes an
	// in-flight authority-guarded save and before it parks.
	beforeReleaseWait func()
}

// Writer returns the single SessionWriter facade bound to this lease. The
// first call creates it; every authority minted for a controller rebind goes
// through the same writer, so all of the lease's saves serialize together.
func (l *SessionLease) Writer() *SessionWriter {
	if l == nil {
		return nil
	}
	l.writerOnce.Do(func() {
		info, err := LoadSessionLeaseInfo(l.path)
		if err != nil || info == nil {
			info = &SessionLeaseInfo{}
		}
		l.writer = &SessionWriter{lease: l, info: *info}
	})
	return l.writer
}

func TryAcquireSessionLease(path string) (*SessionLease, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("empty session path")
	}
	path = canonicalSessionSavePath(path)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	ownerID := sessionLeaseSeq.Add(1)
	if _, loaded := sessionLeaseOwners.LoadOrStore(path, ownerID); loaded {
		info, _ := LoadSessionLeaseInfo(path)
		return nil, &SessionLeaseError{Path: path, Info: info}
	}
	// Respect an unexpired handoff reservation: while the previous holder has
	// released the lease for a named target, only that target may acquire
	// (via TryAcquireSessionLeaseWithHandoff). A plain acquire during the
	// window would defeat the user-authorized transfer.
	if info, err := LoadSessionLeaseInfo(path); err == nil && info != nil && info.HandoffTo != "" {
		if info.HandoffExpiresAt.IsZero() || time.Now().UTC().Before(info.HandoffExpiresAt) {
			sessionLeaseOwners.CompareAndDelete(path, ownerID)
			return nil, &SessionLeaseError{Path: path, Info: info}
		}
	}
	leaseLock, err := tryTakeSessionLeaseLock(path)
	if err != nil {
		sessionLeaseOwners.CompareAndDelete(path, ownerID)
		if errors.Is(err, ErrSessionLeaseHeld) {
			info, _ := LoadSessionLeaseInfo(path)
			return nil, &SessionLeaseError{Path: path, Info: info}
		}
		return nil, err
	}
	// The OS lock proves any active-registry entry left without its reservation
	// is stale. Clear it before publishing this generation.
	sessionLeaseActiveOwners.Delete(path)
	lease := &SessionLease{path: path, ownerID: ownerID, leaseLock: leaseLock}
	if err := publishSessionLeaseOwner(leaseLock, path); err != nil {
		lease.Release()
		return nil, err
	}
	sessionLeaseActiveOwners.Store(path, ownerID)
	return lease, nil
}

// TryReclaimCurrentProcessSessionLease re-acquires a lease whose in-process
// owner entry was orphaned (a lease dropped without Release). The OS lease
// lock is the arbiter: an active holder keeps its lock file locked for the
// whole hold, so reclaiming from one fails with ErrSessionLeaseHeld without
// touching the holder's entry. Holding the lock proves nobody does, which
// also covers metadata-damage states — a missing or unreadable lease info
// (deleted by the user, quarantined by AV, torn by a crash) with a free lock
// is a leftover, not a holder, and must not wedge the session as busy.
func TryReclaimCurrentProcessSessionLease(path string) (*SessionLease, error) {
	path = canonicalSessionSavePath(path)
	info, err := LoadSessionLeaseInfo(path)
	switch {
	case err == nil:
		if info == nil || info.PID != os.Getpid() || info.WriterID != SessionWriterID() {
			// A readable info naming another live runtime: never steal it.
			// (A crashed foreign leftover is separated from a live holder by
			// the lock probe in SessionLeaseHeldByOtherRuntime; reclaim is
			// only for leases this process lost track of.)
			return nil, &SessionLeaseError{Path: path, Info: info}
		}
	case os.IsNotExist(err):
		// The holder finished releasing (info removed first) or the sidecar
		// was deleted out from under an orphaned entry. Either way the lock
		// probe below decides; info identity has nothing left to say.
		info = nil
	default:
		// Unreadable info hides the holder's identity, but the lock still
		// tells the truth: a live holder keeps it locked. Fall through to the
		// probe instead of wedging on metadata damage.
		info = nil
	}
	leaseLock, err := tryTakeSessionLeaseLock(path)
	if err != nil {
		if errors.Is(err, ErrSessionLeaseHeld) {
			return nil, &SessionLeaseError{Path: path, Info: info}
		}
		return nil, err
	}
	// Holding the OS lock proves no live lease owns this path right now, so
	// overwriting the stale owner entry is safe; concurrent reclaimers fail
	// the lock above and never reach this store, and a stale lease released
	// later misses its CompareAndDelete against the new owner id.
	ownerID := sessionLeaseSeq.Add(1)
	lease := &SessionLease{path: path, ownerID: ownerID, leaseLock: leaseLock}
	sessionLeaseActiveOwners.Delete(path)
	sessionLeaseOwners.Store(path, ownerID)
	if err := publishSessionLeaseOwner(leaseLock, path); err != nil {
		lease.Release()
		return nil, err
	}
	sessionLeaseActiveOwners.Store(path, ownerID)
	return lease, nil
}

// SessionLeaseHeldByOtherRuntime reports whether path's session lease is held
// by a live runtime other than the calling process. Callers use it to keep
// destructive operations away from sessions another process may be writing;
// leases held by this process report false because callers tear their own
// runtimes down before acting. The lock file is only probed when a foreign
// lease info file exists, so the common uncontended case never touches the
// lock; a probe cannot steal a live lease because holders keep the lock held
// for their whole lifetime.
func SessionLeaseHeldByOtherRuntime(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	path = canonicalSessionSavePath(path)
	if _, ok := sessionLeaseActiveOwners.Load(path); ok {
		// Held by this process; no need to touch the lock file.
		return false
	}
	info, err := LoadSessionLeaseInfo(path)
	if err != nil {
		if os.IsNotExist(err) {
			// No info file means no holder: live holders keep it present for
			// their whole hold.
			return false
		}
		unlock, lockErr := tryLockSessionLeaseFile(path)
		if lockErr == nil {
			// Corrupt/empty info with a free lock is a crash leftover. Remove the
			// bad metadata so future probes do not keep reporting a ghost owner.
			_ = os.Remove(sessionLeaseInfoPath(path))
			unlock()
			return false
		}
		// An unreadable info file with a live lock still hides the holder's
		// identity, so err on the side of treating the session as busy.
		return true
	}
	if info != nil && info.PID == os.Getpid() && info.WriterID == SessionWriterID() {
		return false
	}
	unlock, err := tryLockSessionLeaseFile(path)
	if err == nil {
		// Foreign info but a free lock: leftover from a crashed process.
		_ = os.Remove(sessionLeaseInfoPath(path))
		unlock()
		return false
	}
	return true
}

// SessionLeaseHeldByCurrentRuntime reports whether this process has completed
// acquisition of path's session lease. Pending reservations and generations
// already retiring report false, so callers cannot authorize destructive repair
// before the OS lock is held or after release has begun.
func SessionLeaseHeldByCurrentRuntime(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	_, ok := sessionLeaseActiveOwners.Load(canonicalSessionSavePath(path))
	return ok
}

func (l *SessionLease) Path() string {
	if l == nil {
		return ""
	}
	return l.path
}

// ReleaseForHandoff releases the lease like Release, but instead of
// deleting the lease info it rewrites it with a handoff reservation naming
// targetWriterID. The OS lock is freed (normal acquire proceeds), while the
// reservation makes TryAcquireSessionLeaseWithHandoff the only acquirer for
// HandoffWindow. This is the explicit user-authorized transfer path between
// runtimes; there is no silent stealing path.
func (l *SessionLease) ReleaseForHandoff(targetWriterID string) error {
	if l == nil {
		return nil
	}
	if strings.TrimSpace(targetWriterID) == "" {
		return fmt.Errorf("handoff target writer id is required")
	}
	// Same save-drain wait as Release: a concurrent save that already passed
	// Valid() must finish before ownership is revoked (ABA).
	for {
		l.mu.Lock()
		if l.released {
			l.mu.Unlock()
			return nil
		}
		if l.activeSaves == 0 {
			break
		}
		if l.releaseWait == nil {
			l.releaseWait = make(chan struct{})
		}
		wait := l.releaseWait
		l.mu.Unlock()
		<-wait
	}
	l.released = true
	leaseLock := l.leaseLock
	l.leaseLock = nil
	l.mu.Unlock()

	sessionLeaseActiveOwners.CompareAndDelete(l.path, l.ownerID)
	// Write the handoff reservation while the OS lock is still held so a
	// concurrent acquirer can never observe a free lock without the marker.
	info := newSessionLeaseInfo(l.path)
	info.HandoffTo = targetWriterID
	info.HandoffExpiresAt = time.Now().UTC().Add(HandoffWindow)
	if err := SaveSessionLeaseInfo(l.path, info); err != nil {
		// Reservation write failed: fall back to a plain release so the
		// session does not stay wedged. The target simply retries takeover.
		sessionLeaseOwners.CompareAndDelete(l.path, l.ownerID)
		if leaseLock != nil {
			_ = leaseLock.RemoveAndUnlock()
		}
		return err
	}
	sessionLeaseOwners.CompareAndDelete(l.path, l.ownerID)
	if leaseLock != nil {
		_ = leaseLock.RemoveAndUnlock()
	}
	_ = removeStaleSessionLockSidecar(l.path, store.SessionLockFile(l.path))
	return nil
}

// TryAcquireSessionLeaseWithHandoff acquires a lease that was released via
// ReleaseForHandoff for this runtime. It refuses leases without a valid,
// unexpired handoff reservation naming this writer, so a live holder is never
// disturbed and a stale reservation cannot wedge the session. On success the
// reservation fields are cleared by the normal SaveSessionLeaseInfo path.
func TryAcquireSessionLeaseWithHandoff(path, sourceWriterID string) (*SessionLease, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("empty session path")
	}
	path = canonicalSessionSavePath(path)
	info, err := LoadSessionLeaseInfo(path)
	if err != nil || info == nil {
		return nil, &SessionLeaseError{Path: path, Info: info}
	}
	if info.HandoffTo == "" {
		return nil, &SessionLeaseError{Path: path, Info: info}
	}
	// Only the named handoff target may acquire: the acquiring runtime's own
	// writer identity must match the reservation. The caller-supplied
	// sourceWriterID identifies the RELEASING runtime and must match
	// info.WriterID (the releasing side) — not handoff_to, so a runtime that
	// merely reads the lease info cannot spoof the target check by echoing
	// handoff_to back at us.
	if SessionWriterID() != info.HandoffTo {
		return nil, &SessionLeaseError{Path: path, Info: info}
	}
	if sourceWriterID != "" && info.WriterID != sourceWriterID {
		return nil, &SessionLeaseError{Path: path, Info: info}
	}
	if !info.HandoffExpiresAt.IsZero() && time.Now().UTC().After(info.HandoffExpiresAt) {
		return nil, &SessionLeaseError{Path: path, Info: info}
	}
	// Reservation valid: take the OS lock directly (the previous holder
	// released it). We cannot reuse TryAcquireSessionLease here because it
	// refuses unexpired handoff reservations.
	ownerID := sessionLeaseSeq.Add(1)
	if _, loaded := sessionLeaseOwners.LoadOrStore(path, ownerID); loaded {
		return nil, &SessionLeaseError{Path: path, Info: info}
	}
	leaseLock, err := tryTakeSessionLeaseLock(path)
	if err != nil {
		sessionLeaseOwners.CompareAndDelete(path, ownerID)
		if errors.Is(err, ErrSessionLeaseHeld) {
			return nil, &SessionLeaseError{Path: path, Info: info}
		}
		return nil, err
	}
	sessionLeaseActiveOwners.Delete(path)
	lease := &SessionLease{path: path, ownerID: ownerID, leaseLock: leaseLock}
	// Clear the reservation by publishing a fresh owner identity inside
	// .lease.lock itself (new-format owner info; the legacy .lease.json
	// sidecar is never written on the handoff path either).
	if err := publishSessionLeaseOwner(leaseLock, path); err != nil {
		lease.Release()
		return nil, err
	}
	sessionLeaseActiveOwners.Store(path, ownerID)
	return lease, nil
}

func (l *SessionLease) Release() {
	if l == nil {
		return
	}
	// Wait for authority-guarded saves to finish before revoking ownership.
	// Without this, a concurrent save that already passed Valid() can finish
	// after a successor lease is issued for the same path (ABA).
	for {
		l.mu.Lock()
		if l.released {
			l.mu.Unlock()
			return
		}
		if l.activeSaves == 0 {
			break
		}
		if l.releaseWait == nil {
			l.releaseWait = make(chan struct{})
		}
		wait := l.releaseWait
		beforeReleaseWait := l.beforeReleaseWait
		l.mu.Unlock()
		if beforeReleaseWait != nil {
			beforeReleaseWait()
		}
		<-wait
	}
	l.released = true
	leaseLock := l.leaseLock
	l.leaseLock = nil
	beforeReleaseLock := l.beforeReleaseLock
	l.mu.Unlock()

	// Revoke ownership-sensitive repair before the OS lock becomes available
	// to a successor. CompareAndDelete keeps a stale generation from
	// deauthorizing a newer reclaimed lease.
	sessionLeaseActiveOwners.CompareAndDelete(l.path, l.ownerID)
	_ = os.Remove(sessionLeaseInfoPath(l.path))
	// Only remove the entry this lease owns: after a reclaim the map may
	// already point at a newer lease for the same path.
	sessionLeaseOwners.CompareAndDelete(l.path, l.ownerID)
	if beforeReleaseLock != nil {
		beforeReleaseLock()
	}
	if leaseLock != nil {
		// Delete the exact lock file while its lock is still held. Besides
		// retiring the sidecar, retaining the lock object enables an atomic
		// handoff to SessionRemovalGuard without an unlock/reacquire window.
		_ = leaseLock.RemoveAndUnlock()
	}
	_ = removeStaleSessionLockSidecar(l.path, store.SessionLockFile(l.path))
}

func newSessionLeaseInfo(path string) SessionLeaseInfo {
	host, _ := os.Hostname()
	return SessionLeaseInfo{
		SessionPath: path,
		WriterID:    SessionWriterID(),
		PID:         os.Getpid(),
		Hostname:    host,
		AcquiredAt:  time.Now().UTC(),
	}
}

// publishSessionLeaseOwner writes the holder identity into the .lease.lock
// file itself through the held lock handle. New writes never create a
// .lease.json sidecar; readers fall back to it only for sessions last held
// by an older build.
func publishSessionLeaseOwner(leaseLock *sessionLockFile, path string) error {
	info := newSessionLeaseInfo(canonicalSessionSavePath(path))
	b, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	if leaseLock == nil {
		return errors.New("session lease lock not held")
	}
	return leaseLock.writeOwnerInfo(b)
}

// errSessionLeaseInfoCorrupt marks a present-but-undecodable lease-info
// source (empty or invalid bytes). Readers treat it as "identity hidden,
// let the lock decide" instead of "no holder".
var errSessionLeaseInfoCorrupt = errors.New("session lease info corrupt")

// LoadSessionLeaseInfo reports the holder identity for path. New writers
// publish it inside .lease.lock; the .lease.json sidecar is a read-only
// compatibility source for sessions last held by older builds. An empty or
// undecodable source reads as corrupt (the live lock is the truth); only the
// absence of both sources reads as no-holder.
func LoadSessionLeaseInfo(path string) (*SessionLeaseInfo, error) {
	lockPath := store.SessionLeaseLock(canonicalSessionSavePath(path))
	if raw, err := readSessionLeaseLockFile(lockPath); err == nil {
		b := fileencoding.DecodeToUTF8(raw)
		if info, decodeErr := decodeSessionLeaseInfo(b); decodeErr == nil {
			return info, nil
		} else if !errors.Is(decodeErr, os.ErrNotExist) {
			return nil, decodeErr
		}
	} else if !os.IsNotExist(err) {
		// An unreadable lock file (permission damage, torn disk) still hides
		// the holder identity; fail on the read rather than silently falling
		// back to the legacy sidecar.
		return nil, err
	}
	b, err := fileencoding.ReadFileUTF8(sessionLeaseInfoPath(path))
	if err != nil {
		return nil, err
	}
	return decodeSessionLeaseInfo(b)
}

func decodeSessionLeaseInfo(b []byte) (*SessionLeaseInfo, error) {
	if len(strings.TrimSpace(string(b))) == 0 {
		return nil, errSessionLeaseInfoCorrupt
	}
	var info SessionLeaseInfo
	if err := json.Unmarshal(b, &info); err != nil {
		return nil, fmt.Errorf("%w: %w", errSessionLeaseInfoCorrupt, err)
	}
	return &info, nil
}

// SaveSessionLeaseInfo writes the legacy .lease.json sidecar. Production
// writers publish owner identity inside .lease.lock instead; this remains for
// tests and tooling that need to stage the compatibility read path.
func SaveSessionLeaseInfo(path string, info SessionLeaseInfo) error {
	leasePath := sessionLeaseInfoPath(path)
	if err := os.MkdirAll(filepath.Dir(leasePath), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(leasePath), ".lease.*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := fileutil.ReplaceFile(tmpPath, leasePath); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return nil
}

func sessionLeaseInfoPath(path string) string {
	return store.SessionLeaseInfo(canonicalSessionSavePath(path))
}

// unleasedWriteObserved dedupes the write-authority probe below to one report
// per canonical path per process.
var unleasedWriteObserved sync.Map

// observeUnleasedSessionWrite is the store-P2 write-authority probe: the target
// model is "the lease holder is the only writer of a session's content", but
// enforcement can't land before we know every writer that currently saves
// without holding the lease (fresh-session creation saves before the first
// Rebind, headless runs, recovery tooling, ...). Until then this only records
// evidence: one structured warning per path per process, never a failure. The
// snapshot-conflict machinery stays the safety net for the writers this
// surfaces.
func observeUnleasedSessionWrite(path string, mode sessionSaveMode) {
	canonical := canonicalSessionSavePath(path)
	if _, ok := sessionLeaseOwners.Load(canonical); ok {
		return
	}
	if _, seen := unleasedWriteObserved.LoadOrStore(canonical, struct{}{}); seen {
		return
	}
	slog.Warn("session: save without a held lease (write-authority probe, store P2)",
		"path", filepath.Base(path),
		"mode", int(mode),
		"writer", SessionWriterID(),
	)
}
