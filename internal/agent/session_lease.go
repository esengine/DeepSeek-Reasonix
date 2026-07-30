package agent

import (
	"encoding/json"
	"errors"
	"fmt"
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
}

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
	path    string
	ownerID uint64
	unlock  func()
	once    sync.Once
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
	unlock, err := tryLockSessionLeaseFile(path)
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
	lease := &SessionLease{path: path, ownerID: ownerID, unlock: unlock}
	if err := SaveSessionLeaseInfo(path, newSessionLeaseInfo(path)); err != nil {
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
	unlock, err := tryLockSessionLeaseFile(path)
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
	lease := &SessionLease{path: path, ownerID: ownerID, unlock: unlock}
	sessionLeaseActiveOwners.Delete(path)
	sessionLeaseOwners.Store(path, ownerID)
	if err := SaveSessionLeaseInfo(path, newSessionLeaseInfo(path)); err != nil {
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

func (l *SessionLease) Release() {
	if l == nil {
		return
	}
	l.once.Do(func() {
		// Revoke ownership-sensitive repair before the OS lock becomes available
		// to a successor. CompareAndDelete keeps a stale generation from
		// deauthorizing a newer reclaimed lease.
		sessionLeaseActiveOwners.CompareAndDelete(l.path, l.ownerID)
		_ = os.Remove(sessionLeaseInfoPath(l.path))
		if l.unlock != nil {
			l.unlock()
		}
		// Only remove the entry this lease owns: after a reclaim the map may
		// already point at a newer lease for the same path.
		sessionLeaseOwners.CompareAndDelete(l.path, l.ownerID)
		// Best-effort retirement of the lock sidecars this session no longer
		// needs. Historically they were left behind on every release and only
		// swept on the next boot reconcile, so ordinary use accumulated
		// .lock/.lease.lock files (#6014). The helpers re-take each lock
		// non-blocking and delete it atomically with the release, so a new
		// holder or an in-flight save simply turns this into a no-op. This
		// must run after CompareAndDelete: the lease-lock helper skips paths
		// the owner registry still reports as held by this process.
		_ = removeStaleSessionLeaseLockSidecar(l.path, store.SessionLeaseLock(l.path))
		_ = removeStaleSessionLockSidecar(l.path, store.SessionLockFile(l.path))
	})
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

func LoadSessionLeaseInfo(path string) (*SessionLeaseInfo, error) {
	b, err := fileencoding.ReadFileUTF8(sessionLeaseInfoPath(path))
	if err != nil {
		return nil, err
	}
	var info SessionLeaseInfo
	if err := json.Unmarshal(b, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

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
