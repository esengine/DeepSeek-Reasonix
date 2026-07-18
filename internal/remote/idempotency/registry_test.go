package idempotency

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/remote/protocol"
)

type registryClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *registryClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *registryClock) Advance(duration time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(duration)
	c.mu.Unlock()
}

type admissionParams struct {
	RequestID            protocol.RequestID     `json:"requestId"`
	ExpectedHostEpoch    protocol.HostEpoch     `json:"expectedHostEpoch"`
	Target               protocol.RuntimeTarget `json:"target,omitempty"`
	ExpectedRuntimeEpoch protocol.RuntimeEpoch  `json:"expectedRuntimeEpoch,omitempty"`
	Value                string                 `json:"value"`
}

type admissionResult struct {
	Accepted string `json:"accepted"`
	Ordinal  int    `json:"ordinal"`
}

func newTestRegistry(t *testing.T, options Options) *Registry {
	t.Helper()
	registry, err := New("host-epoch-a", options)
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

func TestRegistryDefaultsMatchFrozenV1Limits(t *testing.T) {
	registry := newTestRegistry(t, Options{})
	if protocol.IdempotencyRetention != 24*time.Hour {
		t.Fatalf("protocol retention = %v", protocol.IdempotencyRetention)
	}
	if registry.retention != protocol.IdempotencyRetention {
		t.Fatalf("retention = %v", registry.retention)
	}
	if protocol.IdempotencySessionEntries != 1024 {
		t.Fatalf("protocol per-Session entries = %d", protocol.IdempotencySessionEntries)
	}
	if registry.perSessionEntries != protocol.IdempotencySessionEntries {
		t.Fatalf("per-Session entries = %d", registry.perSessionEntries)
	}
	if protocol.IdempotencyHostEntries != 8192 {
		t.Fatalf("protocol per-Host entries = %d", protocol.IdempotencyHostEntries)
	}
	if registry.perHostEntries != protocol.IdempotencyHostEntries {
		t.Fatalf("per-Host entries = %d", registry.perHostEntries)
	}
}

func testRequest(id protocol.RequestID, target Target, value string) Request {
	params := admissionParams{
		RequestID: id, ExpectedHostEpoch: "host-epoch-a", Value: value,
	}
	if target.Kind == TargetSession {
		params.Target = protocol.RuntimeTarget{WorkspaceID: target.WorkspaceID, SessionID: target.SessionID}
		params.ExpectedRuntimeEpoch = "runtime-epoch-a"
	}
	return Request{RequestID: id, Method: "test/mutate", Target: target, Params: params}
}

func mustBegin(t *testing.T, registry *Registry, request Request, status Status) Attempt {
	t.Helper()
	attempt, err := registry.Begin(request)
	if err != nil {
		t.Fatal(err)
	}
	if attempt.Status() != status {
		t.Fatalf("status = %s, want %s", attempt.Status(), status)
	}
	return attempt
}

func mustClaim(t *testing.T, attempt Attempt) *Claim {
	t.Helper()
	claim, ok := attempt.Claim()
	if !ok {
		t.Fatalf("%s attempt did not contain claim", attempt.Status())
	}
	return claim
}

func decodeAdmission(t *testing.T, attempt Attempt) admissionResult {
	t.Helper()
	outcome, err := attempt.Wait(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var result admissionResult
	if err := outcome.Decode(&result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestRegistryCompletedReplayReturnsFirstAdmissionResult(t *testing.T) {
	registry := newTestRegistry(t, Options{})
	request := testRequest("request-a", HostTarget(), "first")
	first := mustBegin(t, registry, request, StatusNew)
	claim := mustClaim(t, first)
	if err := claim.Complete(admissionResult{Accepted: "first", Ordinal: 1}); err != nil {
		t.Fatal(err)
	}
	if got := decodeAdmission(t, first); got != (admissionResult{Accepted: "first", Ordinal: 1}) {
		t.Fatalf("first outcome = %+v", got)
	}

	replay := mustBegin(t, registry, request, StatusCompleted)
	if replay.Fingerprint() != first.Fingerprint() {
		t.Fatal("replay fingerprint changed")
	}
	if _, ok := replay.Claim(); ok {
		t.Fatal("completed replay unexpectedly owned a claim")
	}
	if got := decodeAdmission(t, replay); got.Ordinal != 1 {
		t.Fatalf("replay outcome = %+v", got)
	}
	if err := claim.Complete(admissionResult{Ordinal: 2}); !errors.Is(err, ErrClaimClosed) {
		t.Fatalf("second completion error = %v", err)
	}

	outcome, err := replay.Wait(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	bytes := outcome.ResultJSON()
	bytes[0] = '['
	if got := decodeAdmission(t, mustBegin(t, registry, request, StatusCompleted)); got.Ordinal != 1 {
		t.Fatal("caller mutated cached result bytes")
	}
}

func TestLookupNeverRegistersMissAndReturnsReplayOrConflict(t *testing.T) {
	registry := newTestRegistry(t, Options{})
	request := testRequest("request-lookup", HostTarget(), "first")
	if attempt, found, err := registry.Lookup(request); err != nil || found || attempt.Status() != 0 {
		t.Fatalf("lookup miss = status %s found=%v err=%v", attempt.Status(), found, err)
	}
	if stats := registry.Stats(); stats.Entries != 0 {
		t.Fatalf("lookup miss registered an entry: %+v", stats)
	}

	first := mustBegin(t, registry, request, StatusNew)
	if pending, found, err := registry.Lookup(request); err != nil || !found || pending.Status() != StatusPending {
		t.Fatalf("pending lookup = status %s found=%v err=%v", pending.Status(), found, err)
	}
	conflict := request
	conflict.Params = admissionParams{RequestID: request.RequestID, ExpectedHostEpoch: "host-epoch-a", Value: "changed"}
	if _, found, err := registry.Lookup(conflict); !found || err == nil {
		t.Fatalf("conflict lookup found=%v err=%v", found, err)
	} else {
		assertRemoteCode(t, err, protocol.ErrRequestIDConflict)
	}

	if err := mustClaim(t, first).Complete(admissionResult{Accepted: "first", Ordinal: 4}); err != nil {
		t.Fatal(err)
	}
	completed, found, err := registry.Lookup(request)
	if err != nil || !found || completed.Status() != StatusCompleted {
		t.Fatalf("completed lookup = status %s found=%v err=%v", completed.Status(), found, err)
	}
	if got := decodeAdmission(t, completed); got.Ordinal != 4 {
		t.Fatalf("lookup replay = %+v", got)
	}
}

func TestRegistryPreparedOutcomeSupportsAtomicActorAdmission(t *testing.T) {
	registry := newTestRegistry(t, Options{})
	attempt := mustBegin(t, registry, testRequest("request-a", HostTarget(), "value"), StatusNew)
	outcome, err := PrepareSuccess(admissionResult{Accepted: "prepared", Ordinal: 9})
	if err != nil {
		t.Fatal(err)
	}
	// A Session actor performs its business admission and semantic snapshot
	// commit here, then publishes the already-encoded short result.
	if err := mustClaim(t, attempt).Resolve(outcome); err != nil {
		t.Fatal(err)
	}
	if got := decodeAdmission(t, attempt); got.Ordinal != 9 {
		t.Fatalf("prepared outcome = %+v", got)
	}

	invalid := mustBegin(t, registry, testRequest("request-b", HostTarget(), "value"), StatusNew)
	if err := mustClaim(t, invalid).Resolve(Outcome{}); err == nil {
		t.Fatal("resolved a zero Outcome")
	}
	if stats := registry.Stats(); stats.Pending != 1 {
		t.Fatalf("invalid resolution changed pending claim: %+v", stats)
	}
	if err := mustClaim(t, invalid).Abort(nil); err != nil {
		t.Fatal(err)
	}
}

func TestRegistryPendingWaitersShareFirstOutcome(t *testing.T) {
	registry := newTestRegistry(t, Options{})
	request := testRequest("request-a", HostTarget(), "value")
	first := mustBegin(t, registry, request, StatusNew)
	pending := mustBegin(t, registry, request, StatusPending)

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := pending.Wait(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled waiter error = %v", err)
	}
	if stats := registry.Stats(); stats.Pending != 1 || stats.Completed != 0 {
		t.Fatalf("cancelled waiter changed registry: %+v", stats)
	}

	resultChannel := make(chan admissionResult, 1)
	errorChannel := make(chan error, 1)
	go func() {
		outcome, err := pending.Wait(context.Background())
		if err != nil {
			errorChannel <- err
			return
		}
		var result admissionResult
		if err := outcome.Decode(&result); err != nil {
			errorChannel <- err
			return
		}
		resultChannel <- result
	}()
	if err := mustClaim(t, first).Complete(admissionResult{Accepted: "shared", Ordinal: 7}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-errorChannel:
		t.Fatal(err)
	case result := <-resultChannel:
		if result.Ordinal != 7 {
			t.Fatalf("waiter result = %+v", result)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("pending waiter did not wake")
	}
}

func TestRegistryConflictingReuseNeverAffectsFirstRequest(t *testing.T) {
	registry := newTestRegistry(t, Options{})
	target := SessionTarget(protocol.RuntimeTarget{WorkspaceID: "workspace-a", SessionID: "session-a"})
	request := testRequest("request-a", target, "first")
	first := mustBegin(t, registry, request, StatusNew)

	conflicts := []Request{
		testRequest("request-a", target, "different params"),
		testRequest("request-a", SessionTarget(protocol.RuntimeTarget{WorkspaceID: "workspace-a", SessionID: "session-b"}), "first"),
		testRequest("request-a", target, "first"),
	}
	conflicts[2].Method = "test/other-mutation"
	for _, conflict := range conflicts {
		if _, err := registry.Begin(conflict); err == nil {
			t.Fatal("accepted conflicting requestId reuse")
		} else {
			assertRemoteCode(t, err, protocol.ErrRequestIDConflict)
		}
	}
	if err := mustClaim(t, first).Complete(admissionResult{Accepted: "first", Ordinal: 1}); err != nil {
		t.Fatal(err)
	}
	if result := decodeAdmission(t, mustBegin(t, registry, request, StatusCompleted)); result.Accepted != "first" {
		t.Fatalf("first record changed: %+v", result)
	}

	conflict := testRequest("request-a", target, "different after completion")
	_, err := registry.Begin(conflict)
	assertRemoteCode(t, err, protocol.ErrRequestIDConflict)
}

func TestRegistryCachesOnlyFrozenDeterministicBusinessErrors(t *testing.T) {
	registry := newTestRegistry(t, Options{})
	target := SessionTarget(protocol.RuntimeTarget{WorkspaceID: "workspace-a", SessionID: "session-a"})
	request := testRequest("request-a", target, "reject")
	first := mustBegin(t, registry, request, StatusNew)
	remote := protocol.MustRemoteError(protocol.ErrSessionBusy, protocol.ErrorOptions{Target: target.runtimeTarget()})
	if err := mustClaim(t, first).Reject(remote); err != nil {
		t.Fatal(err)
	}

	replay := mustBegin(t, registry, request, StatusCompleted)
	outcome, err := replay.Wait(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	cached := outcome.RemoteError()
	if cached == nil || cached.Code != protocol.ErrSessionBusy {
		t.Fatalf("cached error = %+v", cached)
	}
	var decoded admissionResult
	assertRemoteCode(t, outcome.Decode(&decoded), protocol.ErrSessionBusy)
	cached.Message = "mutated"
	cached.Data.Target.SessionID = "mutated"
	again, err := replay.Wait(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := again.RemoteError(); got.Message == "mutated" || got.Data.Target.SessionID != "session-a" {
		t.Fatal("caller mutated cached RemoteError")
	}

	second := mustBegin(t, registry, testRequest("request-b", target, "reject"), StatusNew)
	if err := mustClaim(t, second).Reject(errors.New("transient I/O error")); err == nil {
		t.Fatal("cached an arbitrary operational error")
	}
	modified := protocol.MustRemoteError(protocol.ErrSessionBusy, protocol.ErrorOptions{Target: target.runtimeTarget()})
	modified.Message = "not frozen"
	if err := mustClaim(t, second).Reject(modified); err == nil {
		t.Fatal("cached a modified protocol error")
	}
	if err := mustClaim(t, second).Abort(nil); err != nil {
		t.Fatal(err)
	}
}

func TestPrepareRejectionRefusesEveryPreAdmissionRemoteError(t *testing.T) {
	tests := []struct {
		code    protocol.ReasonixErrorCode
		options protocol.ErrorOptions
	}{
		{code: protocol.ErrRemoteNotInstalled},
		{code: protocol.ErrHostStopped},
		{code: protocol.ErrVersionMismatch},
		{code: protocol.ErrDaemonRestartRequired},
		{code: protocol.ErrHostBusy, options: protocol.ErrorOptions{RetryAfterMs: int64Pointer(1000)}},
		{code: protocol.ErrStaleHostEpoch},
		{code: protocol.ErrStaleRuntimeEpoch},
		{code: protocol.ErrRequestIDConflict},
		{code: protocol.ErrLeaseNotHeld},
		{code: protocol.ErrStaleConnection},
	}
	for _, test := range tests {
		t.Run(string(test.code), func(t *testing.T) {
			remote := protocol.MustRemoteError(test.code, test.options)
			if _, err := PrepareRejection(remote); err == nil {
				t.Fatalf("cached pre-admission error %s", test.code)
			}

			registry := newTestRegistry(t, Options{})
			attempt := mustBegin(t, registry, testRequest("request-a", HostTarget(), "value"), StatusNew)
			claim := mustClaim(t, attempt)
			if err := claim.Reject(remote); err == nil {
				t.Fatalf("claim cached pre-admission error %s", test.code)
			}
			if stats := registry.Stats(); stats.Pending != 1 || stats.Completed != 0 {
				t.Fatalf("rejected preparation changed registry: %+v", stats)
			}
			if err := claim.Abort(remote); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRegistryAbortIsUncachedAndWakesPendingDuplicates(t *testing.T) {
	registry := newTestRegistry(t, Options{})
	request := testRequest("request-a", HostTarget(), "value")
	first := mustBegin(t, registry, request, StatusNew)
	pending := mustBegin(t, registry, request, StatusPending)
	stale := protocol.MustRemoteError(protocol.ErrStaleHostEpoch, protocol.ErrorOptions{})
	if err := mustClaim(t, first).Abort(stale); err != nil {
		t.Fatal(err)
	}
	_, err := pending.Wait(context.Background())
	assertRemoteCode(t, err, protocol.ErrStaleHostEpoch)
	if stats := registry.Stats(); stats.Entries != 0 {
		t.Fatalf("aborted record remained cached: %+v", stats)
	}
	retry := mustBegin(t, registry, request, StatusNew)
	if err := mustClaim(t, retry).Abort(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := retry.Wait(context.Background()); !errors.Is(err, ErrAdmissionAbandoned) {
		t.Fatalf("default abort error = %v", err)
	}
}

func TestRegistryRetentionStartsAtCompletionAndNeverExpiresPending(t *testing.T) {
	clock := &registryClock{now: time.Unix(1_000, 0)}
	registry := newTestRegistry(t, Options{Now: clock.Now})
	completedRequest := testRequest("request-completed", HostTarget(), "value")
	completed := mustBegin(t, registry, completedRequest, StatusNew)
	clock.Advance(48 * time.Hour)
	if err := mustClaim(t, completed).Complete(admissionResult{Ordinal: 1}); err != nil {
		t.Fatal(err)
	}
	clock.Advance(protocol.IdempotencyRetention - time.Nanosecond)
	mustBegin(t, registry, completedRequest, StatusCompleted)
	clock.Advance(time.Nanosecond)
	expired := mustBegin(t, registry, completedRequest, StatusNew)

	pendingRequest := testRequest("request-pending", HostTarget(), "value")
	pending := mustBegin(t, registry, pendingRequest, StatusNew)
	clock.Advance(10 * protocol.IdempotencyRetention)
	mustBegin(t, registry, pendingRequest, StatusPending)
	if err := mustClaim(t, expired).Abort(nil); err != nil {
		t.Fatal(err)
	}
	if err := mustClaim(t, pending).Abort(nil); err != nil {
		t.Fatal(err)
	}
}

func TestRegistryHostLRUEvictsLeastRecentlyUsedCompletedEntry(t *testing.T) {
	clock := &registryClock{now: time.Unix(2_000, 0)}
	registry := newTestRegistry(t, Options{Now: clock.Now, PerSessionEntries: 10, PerHostEntries: 2})
	completeTestRequest(t, registry, testRequest("request-a", HostTarget(), "a"), 1)
	clock.Advance(time.Second)
	completeTestRequest(t, registry, testRequest("request-b", HostTarget(), "b"), 2)
	mustBegin(t, registry, testRequest("request-a", HostTarget(), "a"), StatusCompleted) // touch A
	clock.Advance(time.Second)
	third := mustBegin(t, registry, testRequest("request-c", HostTarget(), "c"), StatusNew)
	if hasEntry(registry, "request-b") {
		t.Fatal("least-recently-used completed Host entry was not evicted")
	}
	if !hasEntry(registry, "request-a") || !hasEntry(registry, "request-c") {
		t.Fatal("newer Host entries were evicted")
	}
	if err := mustClaim(t, third).Complete(admissionResult{Ordinal: 3}); err != nil {
		t.Fatal(err)
	}
}

func TestRegistrySessionLimitUsesStableWorkspaceSessionKey(t *testing.T) {
	registry := newTestRegistry(t, Options{PerSessionEntries: 2, PerHostEntries: 10})
	sessionA := SessionTarget(protocol.RuntimeTarget{WorkspaceID: "workspace-a", SessionID: "session-a"})
	sessionB := SessionTarget(protocol.RuntimeTarget{WorkspaceID: "workspace-a", SessionID: "session-b"})
	completeTestRequest(t, registry, testRequest("a-1", sessionA, "1"), 1)
	completeTestRequest(t, registry, testRequest("a-2", sessionA, "2"), 2)
	completeTestRequest(t, registry, testRequest("b-1", sessionB, "1"), 3)
	mustBegin(t, registry, testRequest("a-1", sessionA, "1"), StatusCompleted) // touch A-1
	thirdA := mustBegin(t, registry, testRequest("a-3", sessionA, "3"), StatusNew)
	if hasEntry(registry, "a-2") {
		t.Fatal("oldest completed entry in Session A was not evicted")
	}
	if !hasEntry(registry, "a-1") || !hasEntry(registry, "b-1") {
		t.Fatal("per-Session eviction crossed the Session boundary")
	}
	if err := mustClaim(t, thirdA).Complete(admissionResult{Ordinal: 4}); err != nil {
		t.Fatal(err)
	}
}

func TestRegistryNeverEvictsPendingEntriesAtCapacity(t *testing.T) {
	registry := newTestRegistry(t, Options{PerSessionEntries: 1, PerHostEntries: 1})
	target := SessionTarget(protocol.RuntimeTarget{WorkspaceID: "workspace-a", SessionID: "session-a"})
	first := mustBegin(t, registry, testRequest("request-a", target, "a"), StatusNew)
	second := mustBegin(t, registry, testRequest("request-b", target, "b"), StatusNew)
	if stats := registry.Stats(); stats.Entries != 2 || stats.Pending != 2 {
		t.Fatalf("pending overflow state = %+v", stats)
	}
	if err := mustClaim(t, second).Complete(admissionResult{Ordinal: 2}); err != nil {
		t.Fatal(err)
	}
	if !hasEntry(registry, "request-a") {
		t.Fatal("pending entry was evicted")
	}
	if hasEntry(registry, "request-b") {
		t.Fatal("completed overflow entry should be the only evictable entry")
	}
	if got := decodeAdmission(t, second); got.Ordinal != 2 {
		t.Fatalf("waiter lost evicted completion outcome: %+v", got)
	}
	if err := mustClaim(t, first).Complete(admissionResult{Ordinal: 1}); err != nil {
		t.Fatal(err)
	}
}

func TestRegistryHostEpochResetClearsRecordsAndWakesWaiters(t *testing.T) {
	registry := newTestRegistry(t, Options{})
	completedRequest := testRequest("request-completed", HostTarget(), "value")
	completeTestRequest(t, registry, completedRequest, 1)
	pendingRequest := testRequest("request-pending", HostTarget(), "value")
	pending := mustBegin(t, registry, pendingRequest, StatusNew)
	pendingDuplicate := mustBegin(t, registry, pendingRequest, StatusPending)

	if err := registry.ResetHostEpoch("host-epoch-a"); err != nil {
		t.Fatal(err)
	}
	mustBegin(t, registry, completedRequest, StatusCompleted)
	if err := registry.ResetHostEpoch("host-epoch-b"); err != nil {
		t.Fatal(err)
	}
	if _, err := pendingDuplicate.Wait(context.Background()); !errors.Is(err, ErrHostEpochChanged) {
		t.Fatalf("pending reset error = %v", err)
	}
	if err := mustClaim(t, pending).Complete(admissionResult{}); !errors.Is(err, ErrClaimClosed) {
		t.Fatalf("old claim completion = %v", err)
	}
	if stats := registry.Stats(); stats.HostEpoch != "host-epoch-b" || stats.Entries != 0 {
		t.Fatalf("post-reset stats = %+v", stats)
	}
	mustBegin(t, registry, completedRequest, StatusNew)
}

func TestRegistryRecordSurvivesRuntimeEpochReplacementWithinHostEpoch(t *testing.T) {
	registry := newTestRegistry(t, Options{})
	target := SessionTarget(protocol.RuntimeTarget{WorkspaceID: "workspace-a", SessionID: "session-a"})
	oldRequest := testRequest("request-a", target, "value")
	completeTestRequest(t, registry, oldRequest, 1)

	// Retrying the accepted request with its original expectedRuntimeEpoch is
	// still a replay even after the runtime actor has logically been replaced.
	mustBegin(t, registry, oldRequest, StatusCompleted)
	changedEpoch := oldRequest
	params := changedEpoch.Params.(admissionParams)
	params.ExpectedRuntimeEpoch = "runtime-epoch-b"
	changedEpoch.Params = params
	_, err := registry.Begin(changedEpoch)
	assertRemoteCode(t, err, protocol.ErrRequestIDConflict)

	newSemanticRequest := changedEpoch
	newSemanticRequest.RequestID = "request-b"
	params.RequestID = "request-b"
	newSemanticRequest.Params = params
	mustBegin(t, registry, newSemanticRequest, StatusNew)
}

func TestRegistryConcurrentDuplicateAdmissionHasOneOwner(t *testing.T) {
	registry := newTestRegistry(t, Options{})
	request := testRequest("request-race", HostTarget(), "value")
	const callers = 128
	start := make(chan struct{})
	attempts := make(chan Attempt, callers)
	errorsChannel := make(chan error, callers)
	var owners atomic.Int64
	var ownerMu sync.Mutex
	var owner *Claim
	var waitGroup sync.WaitGroup
	for range callers {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			<-start
			attempt, err := registry.Begin(request)
			if err != nil {
				errorsChannel <- err
				return
			}
			if claim, ok := attempt.Claim(); ok {
				owners.Add(1)
				ownerMu.Lock()
				owner = claim
				ownerMu.Unlock()
			}
			attempts <- attempt
		}()
	}
	close(start)
	waitGroup.Wait()
	close(attempts)
	close(errorsChannel)
	for err := range errorsChannel {
		t.Fatal(err)
	}
	if owners.Load() != 1 {
		t.Fatalf("claim owners = %d, want 1", owners.Load())
	}
	ownerMu.Lock()
	winningClaim := owner
	ownerMu.Unlock()
	if err := winningClaim.Complete(admissionResult{Accepted: "once", Ordinal: 1}); err != nil {
		t.Fatal(err)
	}
	for attempt := range attempts {
		if got := decodeAdmission(t, attempt); got != (admissionResult{Accepted: "once", Ordinal: 1}) {
			t.Fatalf("concurrent outcome = %+v", got)
		}
	}
}

func TestRegistryConcurrentUniqueCompletionsAndReplays(t *testing.T) {
	registry := newTestRegistry(t, Options{PerSessionEntries: 64, PerHostEntries: 256})
	const callers = 64
	var waitGroup sync.WaitGroup
	for i := range callers {
		i := i
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			id := protocol.RequestID(fmt.Sprintf("request-%d", i))
			request := testRequest(id, HostTarget(), fmt.Sprintf("value-%d", i))
			attempt, err := registry.Begin(request)
			if err != nil {
				t.Errorf("begin %d: %v", i, err)
				return
			}
			claim, ok := attempt.Claim()
			if !ok {
				t.Errorf("request %d was not new", i)
				return
			}
			if err := claim.Complete(admissionResult{Ordinal: i}); err != nil {
				t.Errorf("complete %d: %v", i, err)
				return
			}
			replay, err := registry.Begin(request)
			if err != nil {
				t.Errorf("replay %d: %v", i, err)
				return
			}
			outcome, err := replay.Wait(context.Background())
			if err != nil {
				t.Errorf("wait %d: %v", i, err)
				return
			}
			var result admissionResult
			if err := outcome.Decode(&result); err != nil || result.Ordinal != i {
				t.Errorf("decode %d: result=%+v err=%v", i, result, err)
			}
		}()
	}
	waitGroup.Wait()
	if stats := registry.Stats(); stats.Entries != callers || stats.Completed != callers || stats.Pending != 0 {
		t.Fatalf("concurrent stats = %+v", stats)
	}
}

func completeTestRequest(t *testing.T, registry *Registry, request Request, ordinal int) Attempt {
	t.Helper()
	attempt := mustBegin(t, registry, request, StatusNew)
	if err := mustClaim(t, attempt).Complete(admissionResult{Accepted: string(request.RequestID), Ordinal: ordinal}); err != nil {
		t.Fatal(err)
	}
	return attempt
}

func hasEntry(registry *Registry, requestID protocol.RequestID) bool {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	return registry.entries[requestID] != nil
}

func int64Pointer(value int64) *int64 { return &value }

func assertRemoteCode(t *testing.T, err error, code protocol.ReasonixErrorCode) *protocol.RemoteError {
	t.Helper()
	if err == nil {
		t.Fatalf("expected %s", code)
	}
	var remote *protocol.RemoteError
	if !errors.As(err, &remote) {
		t.Fatalf("error %T %v is not RemoteError", err, err)
	}
	if remote.Code != code {
		t.Fatalf("code = %s, want %s", remote.Code, code)
	}
	return remote
}
