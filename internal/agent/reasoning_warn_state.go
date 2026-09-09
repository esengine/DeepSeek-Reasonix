package agent

// reasoning_warn_state.go rate-limits missing tool-call thinking recovery
// attempts across sessions and processes (#7059). Legacy warning-oriented Go
// names and JSON fields intentionally remain unchanged for upgrade and
// cross-version compatibility.
//
// DeepSeek's thinking-mode contract requires provider-issued thinking content
// to accompany tool calls and be replayed on later requests. Missing content is
// therefore a compatibility incident, not a permanent provider characteristic.
// State is keyed by an opaque configuration fingerprint, expires after a bounded
// cooldown, and is cleared after three consecutive healthy tool-call turns so
// a future isolated regression gets one fresh silent retry without flapping.

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"reasonix/internal/filelock"
	"reasonix/internal/fileutil"
)

const missingReasoningWarnStateFilename = "tool-call-reasoning-warning.json"

const (
	missingReasoningWarnStateLockFilename = "tool-call-reasoning-warning.lock"
	missingReasoningWarnStateVersion      = 2
	missingReasoningWarnStateCooldown     = 24 * time.Hour
	missingReasoningWarnStateMaxIncidents = 256
	missingReasoningHealthyResolveStreak  = 3
	// Recovery bookkeeping must never make a turn wait indefinitely behind
	// another process. On contention or I/O failure, callers fail open and allow
	// the bounded retry rather than silently losing self-healing.
	missingReasoningWarnStateLockTimeout = 200 * time.Millisecond
)

type missingReasoningIncident struct {
	Fingerprint            string `json:"fingerprint"`
	WarnedAtUnixMs         int64  `json:"warnedAtUnixMs,omitempty"`
	LastMissingUnixMs      int64  `json:"lastMissingAtUnixMs,omitempty"`
	LastMissingUnixNano    int64  `json:"lastMissingAtUnixNano,omitempty"`
	LastResolvedAtUnixNano int64  `json:"lastResolvedAtUnixNano,omitempty"`
	// These optional fields extend the v2 document without changing its version.
	// Older builds ignore them and may drop an in-progress streak on write, while
	// both generations continue to share the same active-incident boundary.
	ResolveStreak         int   `json:"resolveStreak,omitempty"`
	LastHealthyAtUnixNano int64 `json:"lastHealthyAtUnixNano,omitempty"`
}

type missingReasoningWarnDocument struct {
	Version   int                        `json:"version"`
	Incidents []missingReasoningIncident `json:"incidents,omitempty"`
	// Providers reads the unreleased v1 preview schema. Provider names cannot
	// safely identify endpoint/model/protocol changes and have no timestamp, so
	// they deliberately re-arm once and are omitted on the next v2 write.
	Providers []string `json:"providers,omitempty"`
}

type missingReasoningWarnState struct {
	dir string
}

// missingReasoningWarnClaimFlights coalesces concurrent observations of the
// same incident inside one process. filelock.Acquire intentionally has a short
// deadline so recovery bookkeeping cannot stall a turn behind another process. On a
// slower filesystem (notably Windows CI), several local callers can otherwise
// exhaust that deadline while queued behind the first atomic write and each
// take the fail-open path, producing duplicate recovery requests.
//
// Followers update the latest observation timestamp and let the leader persist
// it before the flight is removed. This preserves resolveAt's stale-health
// ordering guarantee instead of merely suppressing duplicate return values.
type missingReasoningWarnClaimFlight struct {
	latestObservedAt time.Time
}

var missingReasoningWarnClaimFlights = struct {
	sync.Mutex
	flights map[string]*missingReasoningWarnClaimFlight
}{flights: map[string]*missingReasoningWarnClaimFlight{}}

// missingReasoningWarnProcessLocks serializes transactions for one state file
// before they enter filelock.Acquire. The file lock's short deadline is meant
// to bound waits on another process, not make distinct local incidents lose
// persistence while queued behind a slower Windows atomic write. Reasonix uses
// one state path per home, so retaining these few mutexes for the process
// lifetime is bounded in normal operation.
var missingReasoningWarnProcessLocks sync.Map

func newMissingReasoningWarnState(dir string) *missingReasoningWarnState {
	return &missingReasoningWarnState{dir: strings.TrimSpace(dir)}
}

func (s *missingReasoningWarnState) path() string {
	return filepath.Join(s.dir, missingReasoningWarnStateFilename)
}

func (s *missingReasoningWarnState) lockPath() string {
	return filepath.Join(s.dir, missingReasoningWarnStateLockFilename)
}

func (s *missingReasoningWarnState) processLockKey() string {
	path, err := filepath.Abs(s.path())
	if err != nil {
		path = s.path()
	}
	path = filepath.Clean(path)
	if runtime.GOOS == "windows" {
		path = strings.ToLower(filepath.ToSlash(path))
	}
	return path
}

func (s *missingReasoningWarnState) processLock() *sync.Mutex {
	lock, _ := missingReasoningWarnProcessLocks.LoadOrStore(s.processLockKey(), &sync.Mutex{})
	return lock.(*sync.Mutex)
}

func (s *missingReasoningWarnState) claimFlightKey(fingerprint string) string {
	return s.processLockKey() + "\x00" + fingerprint
}

func validMissingReasoningFingerprint(fingerprint string) bool {
	if len(fingerprint) != 64 {
		return false
	}
	for _, r := range fingerprint {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

func missingReasoningUnixNanoFromMillis(unixMs int64) (int64, bool) {
	if unixMs <= 0 || unixMs > math.MaxInt64/int64(time.Millisecond) {
		return 0, false
	}
	return unixMs * int64(time.Millisecond), true
}

func validMissingReasoningIncidentFields(incident missingReasoningIncident) bool {
	return incident.LastMissingUnixNano >= 0 && incident.LastResolvedAtUnixNano >= 0 &&
		incident.LastHealthyAtUnixNano >= 0 &&
		incident.ResolveStreak >= 0 && incident.ResolveStreak < missingReasoningHealthyResolveStreak
}

func normalizeMissingReasoningIncident(incident missingReasoningIncident, now time.Time) (missingReasoningIncident, bool) {
	incident.Fingerprint = strings.TrimSpace(incident.Fingerprint)
	if !validMissingReasoningFingerprint(incident.Fingerprint) ||
		!validMissingReasoningIncidentFields(incident) {
		return missingReasoningIncident{}, false
	}
	incident, warnedAtUnixNano, ok := normalizeMissingReasoningTimestamps(incident)
	if !ok {
		return missingReasoningIncident{}, false
	}
	nowUnixNano := now.UnixNano()
	if warnedAtUnixNano > nowUnixNano || incident.LastMissingUnixNano > nowUnixNano ||
		incident.LastResolvedAtUnixNano > nowUnixNano || incident.LastHealthyAtUnixNano > nowUnixNano {
		return missingReasoningIncident{}, false
	}
	if incident.LastMissingUnixNano > incident.LastResolvedAtUnixNano {
		return normalizeActiveMissingReasoningIncident(incident, warnedAtUnixNano, now)
	}
	return normalizeResolvedMissingReasoningIncident(incident, now)
}

func normalizeMissingReasoningTimestamps(incident missingReasoningIncident) (missingReasoningIncident, int64, bool) {
	warnedAtUnixNano := int64(0)
	if incident.WarnedAtUnixMs != 0 {
		var ok bool
		warnedAtUnixNano, ok = missingReasoningUnixNanoFromMillis(incident.WarnedAtUnixMs)
		if !ok {
			return missingReasoningIncident{}, 0, false
		}
	}
	if incident.LastMissingUnixMs == 0 {
		return incident, warnedAtUnixNano, incident.LastMissingUnixNano == 0
	}
	lastMissingFromMillis, ok := missingReasoningUnixNanoFromMillis(incident.LastMissingUnixMs)
	if !ok {
		return missingReasoningIncident{}, 0, false
	}
	if incident.LastMissingUnixNano == 0 {
		incident.LastMissingUnixNano = lastMissingFromMillis
	} else if incident.LastMissingUnixNano/int64(time.Millisecond) != incident.LastMissingUnixMs {
		return missingReasoningIncident{}, 0, false
	}
	return incident, warnedAtUnixNano, true
}

func normalizeActiveMissingReasoningIncident(incident missingReasoningIncident, warnedAtUnixNano int64, now time.Time) (missingReasoningIncident, bool) {
	if warnedAtUnixNano == 0 || incident.LastMissingUnixNano < warnedAtUnixNano {
		return missingReasoningIncident{}, false
	}
	ageOrigin := time.UnixMilli(incident.WarnedAtUnixMs)
	maxAge := missingReasoningWarnStateCooldown
	age := now.Sub(ageOrigin)
	if age < 0 || age >= maxAge {
		return missingReasoningIncident{}, false
	}
	if incident.ResolveStreak > 0 {
		if incident.LastHealthyAtUnixNano <= incident.LastMissingUnixNano ||
			incident.LastHealthyAtUnixNano <= incident.LastResolvedAtUnixNano {
			return missingReasoningIncident{}, false
		}
	} else if incident.LastHealthyAtUnixNano != 0 {
		return missingReasoningIncident{}, false
	}
	return incident, true
}

func normalizeResolvedMissingReasoningIncident(incident missingReasoningIncident, now time.Time) (missingReasoningIncident, bool) {
	if incident.LastResolvedAtUnixNano <= 0 || incident.ResolveStreak != 0 ||
		incident.LastHealthyAtUnixNano > incident.LastResolvedAtUnixNano {
		return missingReasoningIncident{}, false
	}
	resolvedAt := time.Unix(0, incident.LastResolvedAtUnixNano)
	age := now.Sub(resolvedAt)
	if age < 0 || age >= missingReasoningWarnStateCooldown {
		return missingReasoningIncident{}, false
	}
	return incident, true
}

func (incident missingReasoningIncident) lastEventUnixNano() int64 {
	return incident.lastObservedUnixNano()
}

func (incident missingReasoningIncident) lastObservedUnixNano() int64 {
	return max(incident.LastHealthyAtUnixNano, max(incident.LastResolvedAtUnixNano, incident.LastMissingUnixNano))
}

// load returns only current v2 incidents and resolution watermarks. Missing,
// corrupt, legacy, expired, or future-dated entries are treated as absent and
// self-heal on the next write. Read failures other than a missing file remain
// visible to the transaction so it cannot overwrite state from a partial read.
func (s *missingReasoningWarnState) load(now time.Time) (map[string]missingReasoningIncident, error) {
	incidents := map[string]missingReasoningIncident{}
	b, err := os.ReadFile(s.path())
	if err != nil {
		if os.IsNotExist(err) {
			return incidents, nil
		}
		return nil, err
	}
	var doc missingReasoningWarnDocument
	if json.Unmarshal(b, &doc) != nil || doc.Version != missingReasoningWarnStateVersion {
		return incidents, nil
	}
	for _, rawIncident := range doc.Incidents {
		incident, ok := normalizeMissingReasoningIncident(rawIncident, now)
		if !ok {
			continue
		}
		if previous, exists := incidents[incident.Fingerprint]; !exists || previous.lastEventUnixNano() < incident.lastEventUnixNano() {
			incidents[incident.Fingerprint] = incident
		}
	}
	return incidents, nil
}

func (s *missingReasoningWarnState) save(incidents map[string]missingReasoningIncident) error {
	ordered := make([]missingReasoningIncident, 0, len(incidents))
	for _, incident := range incidents {
		ordered = append(ordered, incident)
	}
	if len(ordered) > missingReasoningWarnStateMaxIncidents {
		sort.Slice(ordered, func(i, j int) bool {
			return ordered[i].lastEventUnixNano() > ordered[j].lastEventUnixNano()
		})
		ordered = ordered[:missingReasoningWarnStateMaxIncidents]
	}
	sort.Slice(ordered, func(i, j int) bool {
		return ordered[i].Fingerprint < ordered[j].Fingerprint
	})
	b, err := json.Marshal(missingReasoningWarnDocument{
		Version:   missingReasoningWarnStateVersion,
		Incidents: ordered,
	})
	if err != nil {
		return err
	}
	return fileutil.AtomicWriteFile(s.path(), b, 0o600)
}

func (s *missingReasoningWarnState) acquire() (func(), error) {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), missingReasoningWarnStateLockTimeout)
	release, err := filelock.Acquire(ctx, s.lockPath())
	if err != nil {
		cancel()
		return nil, err
	}
	return func() {
		release()
		cancel()
	}, nil
}

func normalizeMissingReasoningObservedAt(observedAt time.Time) time.Time {
	if observedAt.IsZero() {
		return time.Now()
	}
	return observedAt
}

func missingReasoningTransactionNow(observedAt time.Time) time.Time {
	now := time.Now()
	if observedAt.After(now) {
		return observedAt
	}
	return now
}

// persistClaimAt performs one cross-process read-check-write transaction.
// Persistence failure returns true so recovery fails open rather than being
// disabled by local bookkeeping trouble.
func (s *missingReasoningWarnState) persistClaimAt(fingerprint string, observedAt time.Time) bool {
	processLock := s.processLock()
	processLock.Lock()
	defer processLock.Unlock()

	release, err := s.acquire()
	if err != nil {
		return true
	}
	defer release()

	incidents, err := s.load(missingReasoningTransactionNow(observedAt))
	if err != nil {
		return true
	}
	incident, exists := incidents[fingerprint]
	observedAtUnixNano := observedAt.UnixNano()
	if exists && (observedAtUnixNano <= incident.LastResolvedAtUnixNano ||
		observedAtUnixNano <= incident.LastHealthyAtUnixNano) {
		return false
	}
	activeIncident := exists && incident.LastMissingUnixNano > incident.LastResolvedAtUnixNano
	shouldRetry := !activeIncident
	if shouldRetry {
		lastResolvedAtUnixNano := incident.LastResolvedAtUnixNano
		incident = missingReasoningIncident{
			Fingerprint:            fingerprint,
			WarnedAtUnixMs:         observedAt.UnixMilli(),
			LastResolvedAtUnixNano: lastResolvedAtUnixNano,
		}
	}
	if incident.LastMissingUnixNano < observedAtUnixNano {
		incident.LastMissingUnixMs = observedAt.UnixMilli()
		incident.LastMissingUnixNano = observedAtUnixNano
		incident.ResolveStreak = 0
		incident.LastHealthyAtUnixNano = 0
	}
	incidents[fingerprint] = incident
	if err := s.save(incidents); err != nil {
		return true
	}
	return shouldRetry
}

// claimAt records the latest missing-reasoning observation and reports whether
// this active incident should receive its one recovery retry. Concurrent
// observations of the same incident in this process share one leader; the file
// lock covers the leader's read-check-write transactions against other
// processes sharing the Reasonix home.
func (s *missingReasoningWarnState) claimAt(fingerprint string, observedAt time.Time) bool {
	fingerprint = strings.TrimSpace(fingerprint)
	if s == nil || s.dir == "" || !validMissingReasoningFingerprint(fingerprint) {
		return true
	}
	observedAt = normalizeMissingReasoningObservedAt(observedAt)
	key := s.claimFlightKey(fingerprint)

	missingReasoningWarnClaimFlights.Lock()
	if flight := missingReasoningWarnClaimFlights.flights[key]; flight != nil {
		if observedAt.After(flight.latestObservedAt) {
			flight.latestObservedAt = observedAt
		}
		missingReasoningWarnClaimFlights.Unlock()
		return false
	}
	flight := &missingReasoningWarnClaimFlight{latestObservedAt: observedAt}
	missingReasoningWarnClaimFlights.flights[key] = flight
	missingReasoningWarnClaimFlights.Unlock()

	processedAt := time.Time{}
	shouldRetry := false
	for {
		missingReasoningWarnClaimFlights.Lock()
		nextObservedAt := flight.latestObservedAt
		missingReasoningWarnClaimFlights.Unlock()

		if nextObservedAt.After(processedAt) {
			shouldRetry = s.persistClaimAt(fingerprint, nextObservedAt) || shouldRetry
			processedAt = nextObservedAt
		}

		missingReasoningWarnClaimFlights.Lock()
		if !flight.latestObservedAt.After(processedAt) {
			delete(missingReasoningWarnClaimFlights.flights, key)
			missingReasoningWarnClaimFlights.Unlock()
			return shouldRetry
		}
		missingReasoningWarnClaimFlights.Unlock()
	}
}

func (s *missingReasoningWarnState) claim(fingerprint string) bool {
	return s.claimAt(fingerprint, time.Now())
}

type missingReasoningResolveResult struct {
	Recorded bool
	Resolved bool
}

// resolveAt records one healthy tool-call turn. Three consecutive healthy
// observations resolve the incident; another missing observation resets the
// streak. The healthy and resolved watermarks prevent events that happened
// earlier but acquired the cross-process lock later from changing newer state.
func (s *missingReasoningWarnState) resolveAt(fingerprint string, observedAt time.Time) missingReasoningResolveResult {
	fingerprint = strings.TrimSpace(fingerprint)
	if s == nil || s.dir == "" || !validMissingReasoningFingerprint(fingerprint) {
		return missingReasoningResolveResult{}
	}
	observedAt = normalizeMissingReasoningObservedAt(observedAt)
	processLock := s.processLock()
	processLock.Lock()
	defer processLock.Unlock()

	release, err := s.acquire()
	if err != nil {
		return missingReasoningResolveResult{}
	}
	defer release()

	incidents, err := s.load(missingReasoningTransactionNow(observedAt))
	if err != nil {
		return missingReasoningResolveResult{}
	}
	incident, exists := incidents[fingerprint]
	observedAtUnixNano := observedAt.UnixNano()
	if !exists || incident.LastMissingUnixNano <= incident.LastResolvedAtUnixNano {
		return missingReasoningResolveResult{Recorded: true, Resolved: true}
	}
	if incident.LastMissingUnixNano >= observedAtUnixNano || incident.LastHealthyAtUnixNano >= observedAtUnixNano {
		return missingReasoningResolveResult{Recorded: true}
	}
	incident.ResolveStreak++
	incident.LastHealthyAtUnixNano = observedAtUnixNano
	resolved := incident.ResolveStreak >= missingReasoningHealthyResolveStreak
	if resolved {
		incident.ResolveStreak = 0
		incident.LastResolvedAtUnixNano = observedAtUnixNano
	}
	incidents[fingerprint] = incident
	if s.save(incidents) != nil {
		return missingReasoningResolveResult{}
	}
	return missingReasoningResolveResult{Recorded: true, Resolved: resolved}
}
