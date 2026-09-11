package control

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/filelock"
	"reasonix/internal/sessioninbox"
)

func TestInboxSnapshotRecoversUnownedInFlightItem(t *testing.T) {
	dir := t.TempDir()
	session := filepath.Join(dir, "s.jsonl")
	if err := os.WriteFile(session, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := New(Options{SessionPath: session, SessionDir: dir, Sink: event.Discard})
	rec, err := c.EnqueueInbox(InboxRequest{Intent: sessioninbox.IntentSteer, Submit: "orphaned guidance"})
	if err != nil {
		t.Fatal(err)
	}
	st, err := c.ensureInbox()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetState(rec.ItemID, sessioninbox.StateSteerAccepted, ""); err != nil {
		t.Fatal(err)
	}

	snap := c.InboxSnapshot()
	if !snap.Paused || !snap.Recovered || snap.RecoveredN != 1 {
		t.Fatalf("orphan recovery metadata = %+v", snap)
	}
	if len(snap.Items) != 1 || snap.Items[0].State != sessioninbox.StateUncertain {
		t.Fatalf("orphan recovery items = %+v", snap.Items)
	}
	if err := c.DeleteInboxItem(rec.ItemID); err != nil {
		t.Fatalf("delete recovered orphan: %v", err)
	}
}

func TestInboxSnapshotPreservesActivelyOwnedSteer(t *testing.T) {
	dir := t.TempDir()
	session := filepath.Join(dir, "s.jsonl")
	if err := os.WriteFile(session, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := New(Options{SessionPath: session, SessionDir: dir, Sink: event.Discard})
	rec, err := c.EnqueueInbox(InboxRequest{Intent: sessioninbox.IntentSteer, Submit: "active guidance"})
	if err != nil {
		t.Fatal(err)
	}
	st, err := c.ensureInbox()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetState(rec.ItemID, sessioninbox.StateSteerAccepted, ""); err != nil {
		t.Fatal(err)
	}
	c.inbox.mu.Lock()
	c.inbox.trackActive(rec.ItemID)
	c.inbox.mu.Unlock()

	snap := c.InboxSnapshot()
	if snap.Paused || snap.Recovered || len(snap.Items) != 1 || snap.Items[0].State != sessioninbox.StateSteerAccepted {
		t.Fatalf("active steer was reclassified: %+v", snap)
	}

	c.inbox.mu.Lock()
	c.inbox.untrackActive(rec.ItemID)
	c.inbox.mu.Unlock()
	snap = c.InboxSnapshot()
	if !snap.Paused || len(snap.Items) != 1 || snap.Items[0].State != sessioninbox.StateUncertain {
		t.Fatalf("unowned steer was not recovered: %+v", snap)
	}
}

func TestTrySteerOrphanRequiresReviewBeforeExplicitRetry(t *testing.T) {
	dir := t.TempDir()
	session := filepath.Join(dir, "s.jsonl")
	if err := os.WriteFile(session, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &gatedTurnRunner{started: make(chan struct{}), release: make(chan struct{})}
	c := New(Options{Runner: runner, SessionPath: session, SessionDir: dir, Sink: event.Discard})
	defer c.autosaveWG.Wait()
	defer close(runner.release)
	rec, err := c.EnqueueInbox(InboxRequest{Intent: sessioninbox.IntentSteer, Submit: "retry me"})
	if err != nil {
		t.Fatal(err)
	}
	st, err := c.ensureInbox()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetState(rec.ItemID, sessioninbox.StateSteerAccepted, ""); err != nil {
		t.Fatal(err)
	}

	if _, err := c.TrySteerInboxItem(rec.ItemID); !errors.Is(err, sessioninbox.ErrPaused) {
		t.Fatalf("first orphan retry error = %v, want ErrPaused", err)
	}
	snap := c.InboxSnapshot()
	if len(snap.Items) != 1 || snap.Items[0].State != sessioninbox.StateUncertain {
		t.Fatalf("first orphan retry state = %+v", snap)
	}
	if err := c.SetInboxPaused(false); err != nil {
		t.Fatal(err)
	}
	receipt, err := c.TrySteerInboxItem(rec.ItemID)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Disposition != sessioninbox.DispositionQueuedFollowup {
		t.Fatalf("explicit retry disposition = %q", receipt.Disposition)
	}
	select {
	case <-runner.started:
	case <-time.After(time.Second):
		t.Fatal("explicit retry did not dispatch the recovered item")
	}
	meta, _, err := c.ReadInboxItem(rec.ItemID)
	if err != nil {
		t.Fatal(err)
	}
	if meta.State != sessioninbox.StateRunning || meta.Intent != sessioninbox.IntentFollowup {
		t.Fatalf("explicit retry meta = %+v", meta)
	}
}

func TestRetryThenStaleSteerTreatsAlreadyRunningItemAsIdempotent(t *testing.T) {
	dir := t.TempDir()
	session := filepath.Join(dir, "s.jsonl")
	if err := os.WriteFile(session, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &gatedTurnRunner{started: make(chan struct{}), release: make(chan struct{})}
	c := New(Options{
		Runner:      runner,
		SessionPath: session,
		SessionDir:  dir,
		Sink:        event.Discard,
	})
	defer c.autosaveWG.Wait()
	defer close(runner.release)
	rec, err := c.EnqueueInbox(InboxRequest{Intent: sessioninbox.IntentFollowup, Submit: "retry once"})
	if err != nil {
		t.Fatal(err)
	}
	st, err := c.ensureInbox()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetState(rec.ItemID, sessioninbox.StateUncertain, "review retry"); err != nil {
		t.Fatal(err)
	}

	if err := c.RetryInboxItem(rec.ItemID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-runner.started:
	case <-time.After(time.Second):
		t.Fatal("retry did not start the recovered item")
	}

	receipt, err := c.TrySteerInboxItem(rec.ItemID)
	if err != nil {
		t.Fatalf("retry already started the item, but stale steer returned: %v", err)
	}
	if receipt.Disposition != sessioninbox.DispositionSteerAccepted || !receipt.Idempotent {
		t.Fatalf("stale steer receipt = %+v, want idempotent accepted", receipt)
	}
}

func TestInboxAdmissionOwnsClaimBeforeSnapshotRecovery(t *testing.T) {
	dir := t.TempDir()
	session := filepath.Join(dir, "s.jsonl")
	if err := os.WriteFile(session, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := New(Options{
		Runner:      &fakeTurnRunner{},
		SessionPath: session,
		SessionDir:  dir,
		Sink:        event.Discard,
	})
	rec, err := c.EnqueueInbox(InboxRequest{Submit: "claimed atomically"})
	if err != nil {
		t.Fatal(err)
	}
	claimed := make(chan struct{})
	release := make(chan struct{})
	c.inbox.mu.Lock()
	c.inbox.beforePreparedAdmission = func() {
		close(claimed)
		<-release
	}
	c.inbox.mu.Unlock()
	type result struct {
		receipt sessioninbox.InboxReceipt
		err     error
	}
	resultCh := make(chan result, 1)
	go func() {
		receipt, submitErr := c.TrySubmitInboxItem(rec.ItemID)
		resultCh <- result{receipt: receipt, err: submitErr}
	}()
	<-claimed
	snapshotCh := make(chan sessioninbox.InboxSnapshot, 1)
	go func() { snapshotCh <- c.InboxSnapshot() }()
	var duringAdmission sessioninbox.InboxSnapshot
	select {
	case duringAdmission = <-snapshotCh:
	case <-time.After(time.Second):
		close(release)
		<-resultCh
		t.Fatal("snapshot recovery waited on the admission state machine")
	}
	if duringAdmission.Paused || len(duringAdmission.Items) != 1 || duringAdmission.Items[0].State != sessioninbox.StateRunning {
		close(release)
		<-resultCh
		t.Fatalf("snapshot recovered a live admission: %+v", duringAdmission)
	}
	if c.inbox.admissionMu.TryLock() {
		c.inbox.admissionMu.Unlock()
		close(release)
		<-resultCh
		t.Fatal("admission hook did not hold the admission state machine")
	}
	close(release)
	got := <-resultCh
	if got.err != nil || got.receipt.Disposition != sessioninbox.DispositionStarted {
		t.Fatalf("admission result = %+v, err=%v", got.receipt, got.err)
	}
	c.autosaveWG.Wait()
}

func TestInboxSnapshotDoesNotHoldAdmissionWhileDiskLocked(t *testing.T) {
	dir := t.TempDir()
	session := filepath.Join(dir, "s.jsonl")
	if err := os.WriteFile(session, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := New(Options{SessionPath: session, SessionDir: dir, Sink: event.Discard})
	st, err := c.ensureInbox()
	if err != nil {
		t.Fatal(err)
	}
	releaseDisk, err := filelock.Acquire(context.Background(), filepath.Join(st.Dir(), "transaction.lock"))
	if err != nil {
		t.Fatal(err)
	}
	reachedRead := make(chan struct{})
	c.inbox.mu.Lock()
	c.inbox.beforeSnapshotRead = func() { close(reachedRead) }
	c.inbox.mu.Unlock()
	done := make(chan struct{})
	go func() {
		_ = c.InboxSnapshot()
		close(done)
	}()
	<-reachedRead
	select {
	case <-done:
		releaseDisk()
		t.Fatal("snapshot bypassed the held Store transaction lock")
	default:
	}
	if !c.inbox.admissionMu.TryLock() {
		releaseDisk()
		<-done
		t.Fatal("snapshot held admissionMu while waiting on transaction.lock")
	}
	c.inbox.admissionMu.Unlock()
	releaseDisk()
	<-done
}

func TestInboxCompletionKeepsOwnershipWithoutHoldingAdmissionDuringSnapshot(t *testing.T) {
	dir := t.TempDir()
	session := filepath.Join(dir, "s.jsonl")
	if err := os.WriteFile(session, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := New(Options{SessionPath: session, SessionDir: dir, Sink: event.Discard})
	rec, err := c.EnqueueInbox(InboxRequest{Intent: sessioninbox.IntentSteer, Submit: "complete atomically"})
	if err != nil {
		t.Fatal(err)
	}
	st, err := c.ensureInbox()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetState(rec.ItemID, sessioninbox.StateSteerConsumed, ""); err != nil {
		t.Fatal(err)
	}
	beforeSnapshot := make(chan struct{})
	release := make(chan struct{})
	c.inbox.mu.Lock()
	c.inbox.trackActive(rec.ItemID)
	c.inbox.beforeCompletionSnapshot = func() {
		close(beforeSnapshot)
		<-release
	}
	c.inbox.mu.Unlock()
	done := make(chan struct{})
	go func() {
		c.onInboxTurnDone()
		close(done)
	}()
	<-beforeSnapshot
	if !c.inbox.admissionMu.TryLock() {
		t.Fatal("completion held admission lock across transcript snapshot boundary")
	}
	c.inbox.admissionMu.Unlock()
	whileSaving := c.InboxSnapshot()
	if whileSaving.Paused || len(whileSaving.Items) != 1 || whileSaving.Items[0].State != sessioninbox.StateSteerConsumed {
		t.Fatalf("snapshot recovery lost active ownership during transcript save: %+v", whileSaving)
	}
	close(release)
	<-done
	snap := c.InboxSnapshot()
	if snap.Paused || len(snap.Items) != 0 {
		t.Fatalf("completed item survived durable acknowledgement: %+v", snap)
	}
}

func TestInboxCompletionOwnsItemWithoutHoldingAdmissionDuringDurableAck(t *testing.T) {
	dir := t.TempDir()
	session := filepath.Join(dir, "s.jsonl")
	if err := os.WriteFile(session, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := New(Options{SessionPath: session, SessionDir: dir, Sink: event.Discard})
	rec, err := c.EnqueueInbox(InboxRequest{Intent: sessioninbox.IntentSteer, Submit: "ack atomically"})
	if err != nil {
		t.Fatal(err)
	}
	st, err := c.ensureInbox()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetState(rec.ItemID, sessioninbox.StateSteerConsumed, ""); err != nil {
		t.Fatal(err)
	}
	beforeAck := make(chan struct{})
	release := make(chan struct{})
	c.inbox.mu.Lock()
	c.inbox.trackActive(rec.ItemID)
	c.inbox.beforeCompletionAck = func() {
		close(beforeAck)
		<-release
	}
	c.inbox.mu.Unlock()
	done := make(chan struct{})
	go func() {
		c.onInboxTurnDone()
		close(done)
	}()
	<-beforeAck
	if !c.inbox.admissionMu.TryLock() {
		close(release)
		<-done
		t.Fatal("completion held admission lock across durable acknowledgement")
	}
	c.inbox.admissionMu.Unlock()
	whileAcking := c.InboxSnapshot()
	if whileAcking.Paused || len(whileAcking.Items) != 1 || whileAcking.Items[0].State != sessioninbox.StateSteerConsumed {
		close(release)
		<-done
		t.Fatalf("snapshot recovery lost active ownership during durable acknowledgement: %+v", whileAcking)
	}
	close(release)
	<-done
	snap := c.InboxSnapshot()
	if snap.Paused || len(snap.Items) != 0 {
		t.Fatalf("completed item survived durable acknowledgement: %+v", snap)
	}
}
