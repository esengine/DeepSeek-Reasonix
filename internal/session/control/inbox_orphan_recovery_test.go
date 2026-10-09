package control

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/base/filelock"
	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/state/sessioninbox"
	"reasonix/internal/state/sessionstore"
	"reasonix/internal/state/store"
)

func TestInboxSnapshotRecoversUnownedInFlightItem(t *testing.T) {
	dir := testenv.TempDir(t)
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
	dir := testenv.TempDir(t)
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
	dir := testenv.TempDir(t)
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
	dir := testenv.TempDir(t)
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
	dir := testenv.TempDir(t)
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
	dir := testenv.TempDir(t)
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
	dir := testenv.TempDir(t)
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
	dir := testenv.TempDir(t)
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

// Each way an item ends up uncertain names itself with a stable code, so a
// frontend can word it without reading the English diagnostic.
func TestInboxCompletionFailuresCarryTypedBlockCodes(t *testing.T) {
	setup := func(t *testing.T, withContent bool) (*Controller, string, string) {
		dir := testenv.TempDir(t)
		session := filepath.Join(dir, "s.jsonl")
		if err := os.WriteFile(session, []byte("{}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		opts := Options{SessionPath: session, SessionDir: dir, Sink: event.Discard}
		if withContent {
			sess := sessionstore.NewSession("system prompt")
			sess.Messages = append(sess.Messages, provider.Message{Role: provider.RoleUser, Content: "hello"})
			ex := agent.New(nil, nil, sess, agent.Options{}, event.Discard)
			opts.Runner, opts.Executor = ex, ex
		}
		c := New(opts)
		rec, err := c.EnqueueInbox(InboxRequest{Intent: sessioninbox.IntentFollowup, Submit: "work"})
		if err != nil {
			t.Fatal(err)
		}
		c.inbox.mu.Lock()
		c.inbox.trackActive(rec.ItemID)
		c.inbox.mu.Unlock()
		return c, session, rec.ItemID
	}
	held := func(t *testing.T, c *Controller, id string, want sessioninbox.BlockCode) {
		t.Helper()
		snap := c.InboxSnapshot()
		if !snap.Paused || len(snap.Items) != 1 || snap.Items[0].ID != id ||
			snap.Items[0].State != sessioninbox.StateUncertain || snap.Items[0].BlockCode != want {
			t.Fatalf("want %s held uncertain with code %q, got %+v", id, want, snap)
		}
	}

	t.Run("acknowledgement fails", func(t *testing.T) {
		c, _, id := setup(t, false) // still queued, so the durable ack refuses it
		c.onInboxTurnDone()
		held(t, c, id, sessioninbox.BlockAckFailed)
	})

	t.Run("transcript snapshot fails", func(t *testing.T) {
		c, session, id := setup(t, true)
		if err := os.Remove(session); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(session, 0o755); err != nil {
			t.Fatal(err)
		}
		c.onInboxTurnDone()
		held(t, c, id, sessioninbox.BlockSnapshotFailed)
	})
}

// After a skip, an item whose body can no longer be read is not requeued: the
// queue never re-runs what it cannot re-read, so it stays held for inspection.
func TestSkippedAskKeepsAnUnreadableItemHeld(t *testing.T) {
	dir := testenv.TempDir(t)
	session := filepath.Join(dir, "s.jsonl")
	if err := os.WriteFile(session, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := New(Options{SessionPath: session, SessionDir: dir, Sink: event.Discard})
	rec, err := c.EnqueueInbox(InboxRequest{Intent: sessioninbox.IntentSteer, Submit: "body that will vanish"})
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
	blobs := filepath.Join(store.SessionInboxDir(session), "blobs")
	entries, err := os.ReadDir(blobs)
	if err != nil || len(entries) == 0 {
		t.Fatalf("expected a blob to remove: %v %v", entries, err)
	}
	for _, e := range entries {
		if err := os.Remove(filepath.Join(blobs, e.Name())); err != nil {
			t.Fatal(err)
		}
	}
	c.mu.Lock()
	c.gate.running, c.gate.canceling, c.gate.cause = true, true, causeAskSkipped
	c.mu.Unlock()
	c.inbox.mu.Lock()
	c.inbox.trackActive(rec.ItemID)
	c.inbox.mu.Unlock()

	c.onInboxUnappliedSteer(rec.ItemID)

	snap := c.InboxSnapshot()
	if !snap.Paused || len(snap.Items) != 1 || snap.Items[0].State != sessioninbox.StateUncertain ||
		snap.Items[0].BlockCode != sessioninbox.BlockSteerUnapplied {
		t.Fatalf("an unreadable item must stay uncertain and paused, got %+v", snap)
	}
}

func TestRecoveredInboxNoticeCarriesItsCountAsAPayload(t *testing.T) {
	ev := inboxRecoveredNotice(3)
	if ev.Kind != event.Notice || ev.Level != event.LevelWarn || ev.Code != event.NoticeCodeInboxRecovered {
		t.Fatalf("notice = %+v, want a warn notice coded %q", ev, event.NoticeCodeInboxRecovered)
	}
	p, ok := event.DecodeInboxRecovered(ev.Detail)
	if !ok || p.Count != 3 {
		t.Fatalf("payload = %q, want a count of 3", ev.Detail)
	}
	if !strings.Contains(ev.Text, "3 pending instruction") {
		t.Fatalf("the English fallback must stay for frontends that do not word the code: %q", ev.Text)
	}
	if !event.DetailIsPayload(event.NoticeCodeInboxRecovered) {
		t.Fatal("a sink that prints Text must not print the payload again")
	}
	if _, ok := event.DecodeInboxRecovered("not json"); ok {
		t.Fatal("a Detail that is not the payload must read as none")
	}
}
