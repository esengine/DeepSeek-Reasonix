package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/filelock"
)

func warningFingerprint(label string) string {
	digest := sha256.Sum256([]byte(label))
	return hex.EncodeToString(digest[:])
}

func missingReasoningTestNow() time.Time {
	return time.Now().Add(-time.Hour).Truncate(time.Millisecond)
}

func TestMissingReasoningWarnStatePersistsCurrentIncidentAcrossInstances(t *testing.T) {
	dir := t.TempDir()
	fingerprint := warningFingerprint("openai\x00deepseek\x00v4-pro")
	observedAt := missingReasoningTestNow()
	if !newMissingReasoningWarnState(dir).claimAt(fingerprint, observedAt) {
		t.Fatal("fresh configuration must claim its first incident notice")
	}
	if newMissingReasoningWarnState(dir).claimAt(fingerprint, observedAt.Add(time.Minute)) {
		t.Fatal("fresh instance must suppress the same current incident")
	}

	b, err := os.ReadFile(filepath.Join(dir, missingReasoningWarnStateFilename))
	if err != nil {
		t.Fatalf("state file missing after claim: %v", err)
	}
	latestObservedAt := observedAt.Add(time.Minute)
	want := fmt.Sprintf(`{"version":2,"incidents":[{"fingerprint":"%s","warnedAtUnixMs":%d,"lastMissingAtUnixMs":%d,"lastMissingAtUnixNano":%d}]}`,
		fingerprint, observedAt.UnixMilli(), latestObservedAt.UnixMilli(), latestObservedAt.UnixNano())
	if got := string(b); got != want {
		t.Fatalf("state file = %s, want %s", got, want)
	}
	if strings.Contains(string(b), "deepseek") || strings.Contains(string(b), "v4-pro") {
		t.Fatalf("state file exposed raw provider configuration: %s", b)
	}
}

func TestMissingReasoningWarnStateSeparatesConfigurationFingerprints(t *testing.T) {
	dir := t.TempDir()
	s := newMissingReasoningWarnState(dir)
	now := missingReasoningTestNow()
	if !s.claimAt(warningFingerprint("endpoint-a\x00model-a"), now) {
		t.Fatal("first configuration must warn")
	}
	if !s.claimAt(warningFingerprint("endpoint-a\x00model-b"), now) {
		t.Fatal("model change must re-arm the warning")
	}
	if !s.claimAt(warningFingerprint("endpoint-b\x00model-a"), now) {
		t.Fatal("endpoint change must re-arm the warning")
	}
}

func TestMissingReasoningWarnStateExpiresCooldown(t *testing.T) {
	s := newMissingReasoningWarnState(t.TempDir())
	fingerprint := warningFingerprint("config")
	now := missingReasoningTestNow()
	if !s.claimAt(fingerprint, now) {
		t.Fatal("fresh incident must warn")
	}
	if s.claimAt(fingerprint, now.Add(missingReasoningWarnStateCooldown-time.Second)) {
		t.Fatal("incident inside cooldown must stay silent")
	}
	if !s.claimAt(fingerprint, now.Add(missingReasoningWarnStateCooldown)) {
		t.Fatal("incident at cooldown boundary must warn again")
	}
}

func TestMissingReasoningWarnStateHealthyTurnRearmsRegression(t *testing.T) {
	s := newMissingReasoningWarnState(t.TempDir())
	fingerprint := warningFingerprint("config")
	now := missingReasoningTestNow()
	if !s.claimAt(fingerprint, now) {
		t.Fatal("fresh incident must warn")
	}
	for healthy := 1; healthy <= missingReasoningHealthyResolveStreak; healthy++ {
		result := s.resolveAt(fingerprint, now.Add(time.Duration(healthy)*time.Minute))
		if !result.Recorded {
			t.Fatalf("healthy observation %d was not recorded", healthy)
		}
		if got, want := result.Resolved, healthy == missingReasoningHealthyResolveStreak; got != want {
			t.Fatalf("healthy observation %d resolved = %v, want %v", healthy, got, want)
		}
	}
	if !s.claimAt(fingerprint, now.Add(4*time.Minute)) {
		t.Fatal("regression after three healthy turns must warn again")
	}
}

func TestMissingReasoningWarnStateMissingTurnResetsHealthyStreak(t *testing.T) {
	s := newMissingReasoningWarnState(t.TempDir())
	fingerprint := warningFingerprint("config")
	now := missingReasoningTestNow()
	if !s.claimAt(fingerprint, now) {
		t.Fatal("fresh incident must warn")
	}
	for healthy := 1; healthy < missingReasoningHealthyResolveStreak; healthy++ {
		if result := s.resolveAt(fingerprint, now.Add(time.Duration(healthy)*time.Minute)); !result.Recorded || result.Resolved {
			t.Fatalf("pre-reset healthy observation %d = %+v", healthy, result)
		}
	}
	if s.claimAt(fingerprint, now.Add(3*time.Minute)) {
		t.Fatal("missing turn inside the active incident must stay suppressed")
	}
	for healthy := 1; healthy < missingReasoningHealthyResolveStreak; healthy++ {
		result := s.resolveAt(fingerprint, now.Add(time.Duration(3+healthy)*time.Minute))
		if !result.Recorded || result.Resolved {
			t.Fatalf("post-reset healthy observation %d = %+v", healthy, result)
		}
	}
	if s.claimAt(fingerprint, now.Add(6*time.Minute)) {
		t.Fatal("two healthy turns after a reset must not re-arm recovery")
	}
}

func TestMissingReasoningWarnStateStaleHealthCannotClearNewerFailure(t *testing.T) {
	s := newMissingReasoningWarnState(t.TempDir())
	fingerprint := warningFingerprint("config")
	now := missingReasoningTestNow()
	if !s.claimAt(fingerprint, now) {
		t.Fatal("fresh incident must warn")
	}
	if s.claimAt(fingerprint, now.Add(2*time.Millisecond)) {
		t.Fatal("newer observation inside cooldown must stay silent")
	}
	// Simulate an older healthy observation acquiring the lock after the newer
	// missing observation. It must not erase the newer incident.
	s.resolveAt(fingerprint, now.Add(time.Millisecond))
	if s.claimAt(fingerprint, now.Add(3*time.Millisecond)) {
		t.Fatal("stale healthy observation erased a newer incident")
	}
}

func TestMissingReasoningWarnStateDuplicateHealthAndDelayedFailureDoNotChangeStreak(t *testing.T) {
	s := newMissingReasoningWarnState(t.TempDir())
	fingerprint := warningFingerprint("config")
	now := missingReasoningTestNow()
	if !s.persistClaimAt(fingerprint, now) {
		t.Fatal("fresh incident must warn")
	}
	firstHealthyAt := now.Add(2 * time.Millisecond)
	if result := s.resolveAt(fingerprint, firstHealthyAt); !result.Recorded || result.Resolved {
		t.Fatalf("first healthy observation = %+v", result)
	}
	if result := s.resolveAt(fingerprint, firstHealthyAt); !result.Recorded || result.Resolved {
		t.Fatalf("duplicate healthy observation = %+v", result)
	}
	if s.persistClaimAt(fingerprint, now.Add(time.Millisecond)) {
		t.Fatal("delayed failure older than healthy progress revived the incident")
	}
	if result := s.resolveAt(fingerprint, now.Add(3*time.Millisecond)); !result.Recorded || result.Resolved {
		t.Fatalf("second unique healthy observation = %+v", result)
	}
	if result := s.resolveAt(fingerprint, now.Add(4*time.Millisecond)); !result.Recorded || !result.Resolved {
		t.Fatalf("third unique healthy observation = %+v", result)
	}
}

func TestMissingReasoningWarnStateDelayedFailureCannotReviveResolvedIncident(t *testing.T) {
	s := newMissingReasoningWarnState(t.TempDir())
	fingerprint := warningFingerprint("config")
	now := time.Now()
	firstMissingAt := now.Add(-10 * time.Millisecond)
	delayedMissingAt := now.Add(-8 * time.Millisecond)
	healthyAt := []time.Time{
		now.Add(-6 * time.Millisecond),
		now.Add(-4 * time.Millisecond),
		now.Add(-2 * time.Millisecond),
	}

	if !s.persistClaimAt(fingerprint, firstMissingAt) {
		t.Fatal("fresh incident must warn")
	}
	for i, observedAt := range healthyAt {
		result := s.resolveAt(fingerprint, observedAt)
		if !result.Recorded || result.Resolved != (i == len(healthyAt)-1) {
			t.Fatalf("healthy observation %d = %+v", i+1, result)
		}
	}
	// Simulate a missing observation that happened before the healthy result but
	// completed its cross-process transaction afterward.
	if s.persistClaimAt(fingerprint, delayedMissingAt) {
		t.Fatal("delayed pre-recovery failure revived a resolved incident")
	}
	if !s.claimAt(fingerprint, now) {
		t.Fatal("healthy result did not re-arm a later regression")
	}
}

func TestMissingReasoningWarnStateV2OptionalStreakFieldsResume(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, missingReasoningWarnStateFilename)
	fingerprint := warningFingerprint("config")
	now := missingReasoningTestNow()
	doc := fmt.Sprintf(`{"version":2,"incidents":[{"fingerprint":"%s","warnedAtUnixMs":%d,"lastMissingAtUnixMs":%d,"lastMissingAtUnixNano":%d,"resolveStreak":2,"lastHealthyAtUnixNano":%d}]}`,
		fingerprint, now.UnixMilli(), now.UnixMilli(), now.UnixNano(), now.Add(2*time.Minute).UnixNano())
	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}

	s := newMissingReasoningWarnState(dir)
	result := s.resolveAt(fingerprint, now.Add(3*time.Minute))
	if !result.Recorded || !result.Resolved {
		t.Fatalf("resumed third healthy observation = %+v", result)
	}
	if !s.claimAt(fingerprint, now.Add(4*time.Minute)) {
		t.Fatal("resumed v2 streak did not re-arm a later regression")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"version":2`) {
		t.Fatalf("optional fields changed the v2 document contract: %s", b)
	}
}

func TestMissingReasoningWarnStateFutureLastMissingSelfHeals(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, missingReasoningWarnStateFilename)
	fingerprint := warningFingerprint("config")
	now := time.Now().Truncate(time.Millisecond)
	doc := missingReasoningWarnDocument{
		Version: missingReasoningWarnStateVersion,
		Incidents: []missingReasoningIncident{{
			Fingerprint:       fingerprint,
			WarnedAtUnixMs:    now.UnixMilli(),
			LastMissingUnixMs: now.Add(time.Hour).UnixMilli(),
		}},
	}
	b, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}

	s := newMissingReasoningWarnState(dir)
	s.resolveAt(fingerprint, now.Add(time.Minute))
	if !s.claimAt(fingerprint, now.Add(2*time.Minute)) {
		t.Fatal("future last-missing timestamp suppressed a re-armed regression")
	}
}

func TestMissingReasoningWarnStateLegacyPreviewRearmsAndMigrates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, missingReasoningWarnStateFilename)
	if err := os.WriteFile(path, []byte(`{"providers":["deepseek"]}`), 0o600); err != nil {
		t.Fatalf("seed legacy state: %v", err)
	}
	s := newMissingReasoningWarnState(dir)
	if !s.claimAt(warningFingerprint("deepseek-current-config"), missingReasoningTestNow()) {
		t.Fatal("legacy provider-name marker must not suppress a configuration-scoped incident")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), `"providers"`) || !strings.Contains(string(b), `"version":2`) {
		t.Fatalf("legacy state was not migrated to v2: %s", b)
	}
}

func TestMissingReasoningWarnStateLoadsV2IncidentWithoutNanosecondField(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, missingReasoningWarnStateFilename)
	fingerprint := warningFingerprint("config")
	now := missingReasoningTestNow()
	doc := fmt.Sprintf(`{"version":2,"incidents":[{"fingerprint":"%s","warnedAtUnixMs":%d,"lastMissingAtUnixMs":%d}]}`,
		fingerprint, now.UnixMilli(), now.UnixMilli())
	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}

	s := newMissingReasoningWarnState(dir)
	if s.claimAt(fingerprint, now.Add(time.Minute)) {
		t.Fatal("v2 incident without nanosecond fields did not retain its active warning")
	}
}

func TestMissingReasoningWarnStateCorruptFileSelfHeals(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, missingReasoningWarnStateFilename)
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatalf("seed corrupt file: %v", err)
	}
	fingerprint := warningFingerprint("config")
	s := newMissingReasoningWarnState(dir)
	now := missingReasoningTestNow()
	if !s.claimAt(fingerprint, now) {
		t.Fatal("corrupt state must re-arm the incident")
	}
	if s.claimAt(fingerprint, now.Add(time.Minute)) {
		t.Fatal("rewritten state did not retain the incident")
	}
}

func TestMissingReasoningWarnStateUsesOwnerOnlyPermissions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	s := newMissingReasoningWarnState(dir)
	if !s.claimAt(warningFingerprint("config"), missingReasoningTestNow()) {
		t.Fatal("fresh incident must warn")
	}
	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := dirInfo.Mode().Perm(); runtime.GOOS != "windows" && got != 0o700 {
		t.Fatalf("state directory mode = %o, want 700", got)
	}
	fileInfo, err := os.Stat(filepath.Join(dir, missingReasoningWarnStateFilename))
	if err != nil {
		t.Fatal(err)
	}
	if got := fileInfo.Mode().Perm(); runtime.GOOS != "windows" && got != 0o600 {
		t.Fatalf("state file mode = %o, want 600", got)
	}
}

func TestMissingReasoningWarnStateIOFailureFallsBackVisible(t *testing.T) {
	path := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(path, []byte("occupied"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !newMissingReasoningWarnState(path).claimAt(warningFingerprint("config"), missingReasoningTestNow()) {
		t.Fatal("state I/O failure must keep the diagnostic visible")
	}
}

func TestMissingReasoningWarnStateReadFailureDoesNotOverwriteExistingIncidents(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod permissions are not portable to Windows")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, missingReasoningWarnStateFilename)
	s := newMissingReasoningWarnState(dir)
	now := missingReasoningTestNow()
	existingFingerprint := warningFingerprint("existing")
	newFingerprint := warningFingerprint("new")
	if !s.claimAt(existingFingerprint, now) {
		t.Fatal("fresh existing incident must warn")
	}
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	permissionsRestored := false
	defer func() {
		if !permissionsRestored {
			_ = os.Chmod(path, 0o600)
		}
	}()
	if !s.claimAt(newFingerprint, now.Add(time.Minute)) {
		t.Fatal("state read failure must keep the new diagnostic visible")
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	permissionsRestored = true

	incidents, err := s.load(now.Add(2 * time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := incidents[existingFingerprint]; !ok {
		t.Fatal("state read failure overwrote the existing incident")
	}
	if _, ok := incidents[newFingerprint]; ok {
		t.Fatal("new incident was unexpectedly persisted from a partial read")
	}
}

func TestMissingReasoningWarnStateEmptyDirFallsBackVisible(t *testing.T) {
	s := newMissingReasoningWarnState("")
	fingerprint := warningFingerprint("config")
	if !s.claim(fingerprint) {
		t.Fatal("first empty-dir claim must stay visible")
	}
	if !s.claim(fingerprint) {
		t.Fatal("repeated empty-dir claim must stay visible")
	}
}

func TestMissingReasoningWarnStateConcurrentSameIncidentWarnsOnce(t *testing.T) {
	dir := t.TempDir()
	fingerprint := warningFingerprint("shared-config")
	now := missingReasoningTestNow()
	start := make(chan struct{})
	var warned atomic.Int64
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			<-start
			if newMissingReasoningWarnState(dir).claimAt(fingerprint, now) {
				warned.Add(1)
			}
		})
	}
	close(start)
	wg.Wait()
	if got := warned.Load(); got != 1 {
		t.Fatalf("concurrent first warnings = %d, want 1", got)
	}
}

func TestMissingReasoningWarnStateConcurrentFollowerPersistsLatestObservation(t *testing.T) {
	dir := t.TempDir()
	s := newMissingReasoningWarnState(dir)
	fingerprint := warningFingerprint("shared-config")
	firstObservedAt := missingReasoningTestNow()
	latestObservedAt := firstObservedAt.Add(2 * time.Millisecond)

	releaseLock, err := filelock.Acquire(context.Background(), s.lockPath())
	if err != nil {
		t.Fatalf("hold state lock: %v", err)
	}
	released := false
	defer func() {
		if !released {
			releaseLock()
		}
	}()

	leaderResult := make(chan bool, 1)
	go func() {
		leaderResult <- s.claimAt(fingerprint, firstObservedAt)
	}()

	key := s.claimFlightKey(fingerprint)
	deadline := time.Now().Add(missingReasoningWarnStateLockTimeout / 2)
	for {
		missingReasoningWarnClaimFlights.Lock()
		flightPresent := missingReasoningWarnClaimFlights.flights[key] != nil
		missingReasoningWarnClaimFlights.Unlock()
		if flightPresent {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("leader did not register its claim flight")
		}
		time.Sleep(time.Millisecond)
	}

	if s.claimAt(fingerprint, latestObservedAt) {
		t.Fatal("concurrent follower must not emit a duplicate warning")
	}
	releaseLock()
	released = true
	if !<-leaderResult {
		t.Fatal("leader must keep the first incident warning visible")
	}

	incidents, err := s.load(latestObservedAt)
	if err != nil {
		t.Fatal(err)
	}
	incident, ok := incidents[fingerprint]
	if !ok || len(incidents) != 1 {
		t.Fatalf("persisted incidents = %#v, want only %q", incidents, fingerprint)
	}
	if got, want := incident.LastMissingUnixMs, latestObservedAt.UnixMilli(); got != want {
		t.Fatalf("last missing timestamp = %d, want %d", got, want)
	}
}

func TestMissingReasoningWarnStateConcurrentClaimsKeepEveryConfiguration(t *testing.T) {
	dir := t.TempDir()
	now := missingReasoningTestNow()
	labels := []string{"alpha", "bravo", "charlie", "delta"}
	start := make(chan struct{})
	var wg sync.WaitGroup
	for _, label := range labels {
		fingerprint := warningFingerprint(label)
		wg.Go(func() {
			<-start
			if !newMissingReasoningWarnState(dir).claimAt(fingerprint, now) {
				t.Errorf("fresh configuration %q did not claim its notice", label)
			}
		})
	}
	close(start)
	wg.Wait()

	fresh := newMissingReasoningWarnState(dir)
	for _, label := range labels {
		if fresh.claimAt(warningFingerprint(label), now.Add(time.Minute)) {
			t.Errorf("configuration %q was lost after concurrent claims", label)
		}
	}
}
