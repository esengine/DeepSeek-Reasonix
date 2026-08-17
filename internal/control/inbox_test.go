package control

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/memory"
	"reasonix/internal/provider"
	"reasonix/internal/sessioninbox"
	"reasonix/internal/skill"
	"reasonix/internal/tool"
)

func TestEnqueueInboxDurableAndSnapshot(t *testing.T) {
	dir := t.TempDir()
	session := filepath.Join(dir, "s.jsonl")
	if err := os.WriteFile(session, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := New(Options{SessionPath: session, SessionDir: dir, Sink: event.Discard})
	rec, err := c.EnqueueInbox(InboxRequest{
		Intent:  sessioninbox.IntentFollowup,
		Display: "hello durable",
		Submit:  "hello durable",
		Source:  "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if rec.ItemID == "" {
		t.Fatal("empty item id")
	}
	snap := c.InboxSnapshot()
	if len(snap.Items) != 1 || snap.Items[0].Preview == "" {
		t.Fatalf("snapshot = %+v", snap)
	}
	if snap.SessionPath != session {
		t.Fatalf("snapshot session path = %q, want %q", snap.SessionPath, session)
	}
	_, env, err := c.ReadInboxItem(rec.ItemID)
	if err != nil || env.SubmitText != "hello durable" {
		t.Fatalf("read = %+v err=%v", env, err)
	}
}

func TestSessionRebindOnlyPausesInboxWithPendingWork(t *testing.T) {
	for _, tc := range []struct {
		name    string
		pending bool
	}{
		{name: "empty"},
		{name: "pending", pending: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			oldPath := filepath.Join(dir, "old.jsonl")
			c := New(Options{SessionPath: oldPath, SessionDir: dir, Sink: event.Discard})
			if tc.pending {
				if _, err := c.EnqueueInbox(InboxRequest{Submit: "work"}); err != nil {
					t.Fatal(err)
				}
			}

			c.SetSessionPath(filepath.Join(dir, "new.jsonl"))
			oldInbox, err := sessioninbox.Open(oldPath, sessioninbox.Limits{})
			if err != nil {
				t.Fatal(err)
			}
			defer oldInbox.Close()
			if got := oldInbox.Snapshot().Paused; got != tc.pending {
				t.Fatalf("paused = %v, want %v", got, tc.pending)
			}
		})
	}
}

func TestTryEnqueueAndSteerWhenPausedKeepsQueuedFollowup(t *testing.T) {
	dir := t.TempDir()
	session := filepath.Join(dir, "s.jsonl")
	c := New(Options{SessionPath: session, SessionDir: dir, Sink: event.Discard})
	if err := c.SetInboxPaused(true); err != nil {
		t.Fatal(err)
	}
	got, err := c.TryEnqueueAndSteer(InboxRequest{Submit: "later"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Disposition != sessioninbox.DispositionQueuedFollowup || !got.Paused || got.ItemID == "" {
		t.Fatalf("receipt = %+v", got)
	}
	meta, _, err := c.ReadInboxItem(got.ItemID)
	if err != nil {
		t.Fatal(err)
	}
	if meta.State != sessioninbox.StateQueued {
		t.Fatalf("meta = %+v", meta)
	}
}

func TestDeleteInboxItemRecoversOrphanThenRemoves(t *testing.T) {
	dir := t.TempDir()
	session := filepath.Join(dir, "s.jsonl")
	c := New(Options{SessionPath: session, SessionDir: dir, Sink: event.Discard})
	rec, err := c.EnqueueInbox(InboxRequest{Submit: "stuck"})
	if err != nil {
		t.Fatal(err)
	}
	st, err := c.ensureInbox()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.ClaimItem(rec.ItemID); err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteInboxItem(rec.ItemID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.ReadInboxItem(rec.ItemID); !errors.Is(err, sessioninbox.ErrNotFound) {
		t.Fatalf("item still present: %v", err)
	}
	if snap := c.InboxSnapshot(); snap.Paused || snap.Recovered || len(snap.Items) != 0 {
		t.Fatalf("empty inbox stayed paused after deleting last orphan: %+v", snap)
	}
}

func TestDeleteInboxItemWithdrawsUnconsumedSteer(t *testing.T) {
	dir := t.TempDir()
	session := filepath.Join(dir, "s.jsonl")
	c := New(Options{SessionPath: session, SessionDir: dir, Sink: event.Discard})
	rec, err := c.EnqueueInbox(InboxRequest{Intent: sessioninbox.IntentSteer, Submit: "withdraw me"})
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
	if err := c.DeleteInboxItem(rec.ItemID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.ReadInboxItem(rec.ItemID); !errors.Is(err, sessioninbox.ErrNotFound) {
		t.Fatalf("accepted steer still present: %v", err)
	}
	if snap := c.InboxSnapshot(); snap.Paused || len(snap.Items) != 0 {
		t.Fatalf("withdrawing last steer left a paused empty inbox: %+v", snap)
	}
}

func TestTrySteerRejectedBecomesFollowup(t *testing.T) {
	dir := t.TempDir()
	session := filepath.Join(dir, "s.jsonl")
	_ = os.WriteFile(session, []byte("{}\n"), 0o644)
	runner := &gatedTurnRunner{started: make(chan struct{}), release: make(chan struct{})}
	c := New(Options{Runner: runner, SessionPath: session, SessionDir: dir, Sink: event.Discard})
	defer c.autosaveWG.Wait()
	defer close(runner.release)
	rec, err := c.EnqueueInbox(InboxRequest{
		Intent: sessioninbox.IntentSteer,
		Submit: "mid-turn please",
	})
	if err != nil {
		t.Fatal(err)
	}
	// No running turn → reject, keep as follow-up.
	got, err := c.TrySteerInboxItem(rec.ItemID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Disposition != sessioninbox.DispositionQueuedFollowup {
		t.Fatalf("disposition = %s, want queued_followup", got.Disposition)
	}
	select {
	case <-runner.started:
	case <-time.After(time.Second):
		t.Fatal("rejected idle steer did not dispatch as a follow-up")
	}
	meta, _, err := c.ReadInboxItem(rec.ItemID)
	if err != nil {
		t.Fatal(err)
	}
	if meta.State != sessioninbox.StateRunning || meta.Intent != sessioninbox.IntentFollowup {
		t.Fatalf("meta = %+v", meta)
	}
}

func TestIdempotentEnqueue(t *testing.T) {
	dir := t.TempDir()
	session := filepath.Join(dir, "s.jsonl")
	_ = os.WriteFile(session, []byte("{}\n"), 0o644)
	c := New(Options{SessionPath: session, SessionDir: dir, Sink: event.Discard})
	a, err := c.EnqueueInbox(InboxRequest{Submit: "x", Idempotency: "k1"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := c.EnqueueInbox(InboxRequest{Submit: "x", Idempotency: "k1"})
	if err != nil {
		t.Fatal(err)
	}
	if a.ItemID != b.ItemID || !b.Idempotent {
		t.Fatalf("a=%+v b=%+v", a, b)
	}
}

func TestIdempotentEnqueueDoesNotReclassifyExistingItem(t *testing.T) {
	dir := t.TempDir()
	workspace := filepath.Join(dir, "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	c := New(Options{
		SessionPath:   filepath.Join(dir, "s.jsonl"),
		SessionDir:    dir,
		WorkspaceRoot: workspace,
		Sink:          event.Discard,
	})
	first, err := c.EnqueueInbox(InboxRequest{Submit: "original", Idempotency: "same"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := c.EnqueueInbox(InboxRequest{Submit: "original", Idempotency: "same"})
	if err != nil {
		t.Fatal(err)
	}
	if first.ItemID != second.ItemID || !second.Idempotent {
		t.Fatalf("first=%+v second=%+v", first, second)
	}
	snapshot := c.InboxSnapshot()
	if snapshot.Paused || len(snapshot.Items) != 1 || snapshot.Items[0].State != sessioninbox.StateQueued {
		t.Fatalf("idempotent replay reclassified original item: %+v", snapshot)
	}
}

func TestIdempotentEnqueueRejectsDifferentInput(t *testing.T) {
	dir := t.TempDir()
	c := New(Options{
		SessionPath: filepath.Join(dir, "s.jsonl"),
		SessionDir:  dir,
		Sink:        event.Discard,
	})
	if _, err := c.EnqueueInbox(InboxRequest{Submit: "original", Idempotency: "same"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.EnqueueInbox(InboxRequest{Submit: "replacement", Idempotency: "same"}); !errors.Is(err, sessioninbox.ErrIdempotencyConflict) {
		t.Fatalf("conflicting replay error = %v, want ErrIdempotencyConflict", err)
	}
}

type inboxSteerProvider struct {
	started  chan struct{}
	release  chan struct{}
	requests []provider.Request
}

func (p *inboxSteerProvider) Name() string { return "inbox-steer" }

func (p *inboxSteerProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.requests = append(p.requests, req)
	ch := make(chan provider.Chunk, 2)
	if len(p.requests) == 1 {
		close(p.started)
		go func() {
			defer close(ch)
			select {
			case <-p.release:
				ch <- provider.Chunk{Type: provider.ChunkText, Text: "ready"}
				ch <- provider.Chunk{Type: provider.ChunkDone}
			case <-ctx.Done():
			}
		}()
		return ch, nil
	}
	ch <- provider.Chunk{Type: provider.ChunkText, Text: "applied"}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func TestThirtySteersApplyAndAckExactlyOnce(t *testing.T) {
	dir := t.TempDir()
	prov := &inboxSteerProvider{started: make(chan struct{}), release: make(chan struct{})}
	sess := agent.NewSession("sys")
	exec := agent.New(prov, tool.NewRegistry(), sess, agent.Options{}, event.Discard)
	sink, done, _ := collectSink()
	c := New(Options{
		Runner:      exec,
		Executor:    exec,
		Sink:        sink,
		SessionDir:  dir,
		SessionPath: filepath.Join(dir, "s.jsonl"),
	})
	defer c.autosaveWG.Wait()
	c.Submit("initial turn")
	select {
	case <-prov.started:
	case <-time.After(time.Second):
		t.Fatal("initial provider turn did not start")
	}

	const steerCount = 30
	for i := range steerCount {
		body := fmt.Sprintf("durable-steer-%02d", i)
		rec, err := c.EnqueueInbox(InboxRequest{Intent: sessioninbox.IntentSteer, Submit: body})
		if err != nil {
			t.Fatal(err)
		}
		got, err := c.TrySteerInboxItem(rec.ItemID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Disposition != sessioninbox.DispositionSteerAccepted {
			t.Fatalf("steer %d disposition = %q", i, got.Disposition)
		}
	}
	close(prov.release)
	// Thirty durable round trips are real filesystem work; a loaded Windows
	// runner spends most of the default five seconds before the turn is even
	// released. This asserts exactly-once acknowledgement, not latency.
	waitForDoneWithin(t, done, 60*time.Second)

	if items := c.InboxSnapshot().Items; len(items) != 0 {
		t.Fatalf("accepted steers were not all acknowledged: %+v", items)
	}
	if got := len(prov.requests); got != steerCount+1 {
		t.Fatalf("provider requests = %d, want %d", got, steerCount+1)
	}
	messages := sess.Snapshot()
	for i := range steerCount {
		body := fmt.Sprintf("durable-steer-%02d", i)
		count := 0
		for _, message := range messages {
			count += strings.Count(message.Content, body)
		}
		if count != 1 {
			t.Fatalf("%q appears %d times in transcript, want exactly once", body, count)
		}
	}
}

func TestMultiSteerActiveSetAcksAll(t *testing.T) {
	dir := t.TempDir()
	session := filepath.Join(dir, "s.jsonl")
	_ = os.WriteFile(session, []byte("{}\n"), 0o644)
	c := New(Options{SessionPath: session, SessionDir: dir, Sink: event.Discard})

	st, err := c.ensureInbox()
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for i := range 3 {
		rec, err := c.EnqueueInbox(InboxRequest{Submit: "body-" + string(rune('a'+i))})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, rec.ItemID)
		_ = st.SetState(rec.ItemID, sessioninbox.StateSteerConsumed, "")
	}
	c.inbox.mu.Lock()
	c.inbox.clearActive()
	for _, id := range ids {
		c.inbox.trackActive(id)
	}
	c.inbox.mu.Unlock()

	c.onInboxTurnDone()
	if n := len(c.InboxSnapshot().Items); n != 0 {
		t.Fatalf("want all 3 steers acked/dequeued, still have %d items", n)
	}
}

func TestSubmitInboxUsesFrozenReferenceWithoutLiveReresolve(t *testing.T) {
	dir := t.TempDir()
	workspace := filepath.Join(dir, "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	refPath := filepath.Join(workspace, "note.txt")
	if err := os.WriteFile(refPath, []byte("enqueue-time-body"), 0o600); err != nil {
		t.Fatal(err)
	}
	sessionPath := filepath.Join(dir, "s.jsonl")
	sess := agent.NewSession("sys")
	exec := agent.New(nil, nil, sess, agent.Options{}, event.Discard)
	sink, done, _ := collectSink()
	c := New(Options{
		Runner:        appendingRunner{session: sess},
		Executor:      exec,
		Sink:          sink,
		SessionDir:    dir,
		SessionPath:   sessionPath,
		WorkspaceRoot: workspace,
	})
	defer c.autosaveWG.Wait()

	rec, err := c.EnqueueInbox(InboxRequest{Submit: "review @note.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(refPath, []byte("live-body-after-enqueue"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := c.TrySubmitInboxItem(rec.ItemID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Disposition != sessioninbox.DispositionStarted {
		t.Fatalf("disposition = %q, want started", got.Disposition)
	}
	waitForDone(t, done)

	messages := sess.Snapshot()
	if len(messages) < 2 {
		t.Fatalf("messages = %+v", messages)
	}
	input := messages[len(messages)-1].Content
	if !strings.Contains(input, "enqueue-time-body") {
		t.Fatalf("prepared inbox turn omitted frozen body: %q", input)
	}
	if strings.Contains(input, "live-body-after-enqueue") {
		t.Fatalf("prepared inbox turn re-resolved live reference: %q", input)
	}
	if strings.Count(input, "enqueue-time-body") != 1 {
		t.Fatalf("frozen body injected more than once: %q", input)
	}
}

func TestInboxFreezesTypedDirectoryAndPathInstructions(t *testing.T) {
	dir := t.TempDir()
	workspace := filepath.Join(dir, "workspace")
	service := filepath.Join(workspace, "service")
	if err := os.MkdirAll(service, 0o755); err != nil {
		t.Fatal(err)
	}
	for path, body := range map[string]string{
		filepath.Join(workspace, "AGENTS.md"): "ROOT RULE",
		filepath.Join(service, "AGENTS.md"):   "SERVICE RULE",
		filepath.Join(service, "old.go"):      "package service",
	} {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	sessionPath := filepath.Join(dir, "s.jsonl")
	sess := agent.NewSession("sys")
	exec := agent.New(nil, nil, sess, agent.Options{}, event.Discard)
	sink, done, _ := collectSink()
	c := New(Options{
		Runner:        appendingRunner{session: sess},
		Executor:      exec,
		Sink:          sink,
		SessionDir:    dir,
		SessionPath:   sessionPath,
		WorkspaceRoot: workspace,
		Memory:        memory.Load(memory.Options{CWD: workspace}),
	})
	defer c.autosaveWG.Wait()

	rec, err := c.EnqueueInbox(InboxRequest{Submit: "review @service"})
	if err != nil {
		t.Fatal(err)
	}
	_, env, err := c.ReadInboxItem(rec.ItemID)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"<dir ", "old.go", "<path-instructions", "SERVICE RULE"} {
		if !strings.Contains(env.FrozenRefBlock, want) {
			t.Fatalf("frozen typed context missing %q:\n%s", want, env.FrozenRefBlock)
		}
	}
	if err := os.WriteFile(filepath.Join(service, "new.go"), []byte("package changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := c.TrySubmitInboxItem(rec.ItemID); err != nil {
		t.Fatal(err)
	}
	waitForDone(t, done)
	input := sess.Snapshot()[len(sess.Snapshot())-1].Content
	if !strings.Contains(input, "old.go") || strings.Contains(input, "new.go") {
		t.Fatalf("directory reference was re-resolved live: %q", input)
	}
}

func TestInboxUsesFrozenImageBytesAfterWorkspaceChanges(t *testing.T) {
	dir := t.TempDir()
	workspace := filepath.Join(dir, "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	writeVisionTestConfig(t, workspace)
	imagePath := filepath.Join(workspace, "diagram.png")
	if err := os.WriteFile(imagePath, mustBase64(t, tinyPNG), 0o644); err != nil {
		t.Fatal(err)
	}
	prov := &recordingProvider{streams: [][]provider.Chunk{{
		{Type: provider.ChunkText, Text: "done"},
		{Type: provider.ChunkDone},
	}}}
	sess := agent.NewSession("sys")
	exec := agent.New(prov, tool.NewRegistry(), sess, agent.Options{}, event.Discard)
	sink, done, _ := collectSink()
	c := New(Options{
		Runner:        exec,
		Executor:      exec,
		Sink:          sink,
		SessionDir:    dir,
		SessionPath:   filepath.Join(dir, "s.jsonl"),
		WorkspaceRoot: workspace,
		ModelRef:      "custom/vision-pro",
	})
	defer c.autosaveWG.Wait()

	rec, err := c.EnqueueInbox(InboxRequest{Submit: "inspect @diagram.png"})
	if err != nil {
		t.Fatal(err)
	}
	_, env, err := c.ReadInboxItem(rec.ItemID)
	if err != nil || len(env.FrozenImages) != 1 {
		t.Fatalf("frozen image envelope = %+v err=%v", env, err)
	}
	frozen := env.FrozenImages[0]
	if err := os.WriteFile(imagePath, []byte("changed after enqueue"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := c.TrySubmitInboxItem(rec.ItemID); err != nil {
		t.Fatal(err)
	}
	waitForDone(t, done)
	if len(prov.requests) != 1 {
		t.Fatalf("provider requests = %d, want 1", len(prov.requests))
	}
	messages := prov.requests[0].Messages
	if len(messages) == 0 || len(messages[len(messages)-1].Images) != 1 || messages[len(messages)-1].Images[0] != frozen {
		t.Fatalf("provider did not receive the enqueue-time image snapshot: %+v", messages)
	}
}

func TestTrySubmitInboxAdmissionRaceRestoresQueuedItem(t *testing.T) {
	dir := t.TempDir()
	session := filepath.Join(dir, "s.jsonl")
	_ = os.WriteFile(session, []byte("{}\n"), 0o644)
	c := New(Options{SessionPath: session, SessionDir: dir, Sink: event.Discard})
	rec, err := c.EnqueueInbox(InboxRequest{Submit: "must remain durable"})
	if err != nil {
		t.Fatal(err)
	}
	competingStarted := make(chan struct{})
	releaseCompeting := make(chan struct{})
	c.inbox.mu.Lock()
	c.inbox.beforePreparedAdmission = func() {
		if result := c.runGuarded(func(context.Context) error {
			close(competingStarted)
			<-releaseCompeting
			return nil
		}); result != turnStarted {
			t.Errorf("competing admission = %v, want turnStarted", result)
		}
		select {
		case <-competingStarted:
		case <-time.After(time.Second):
			t.Error("competing turn did not start")
		}
	}
	c.inbox.mu.Unlock()

	receipt, err := c.TrySubmitInboxItem(rec.ItemID)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Disposition != sessioninbox.DispositionRejectedBusy {
		t.Fatalf("race disposition = %q, want rejected_busy", receipt.Disposition)
	}
	meta, _, err := c.ReadInboxItem(rec.ItemID)
	if err != nil || meta.State != sessioninbox.StateQueued {
		t.Fatalf("raced item = %+v err=%v, want durable queued", meta, err)
	}
	if err := c.SetInboxPaused(true); err != nil {
		t.Fatal(err)
	}
	close(releaseCompeting)
	c.autosaveWG.Wait()
}

func TestCancelWithInboxItemsDiscardsOnlyOwnedPendingItems(t *testing.T) {
	dir := t.TempDir()
	session := filepath.Join(dir, "s.jsonl")
	_ = os.WriteFile(session, []byte("{}\n"), 0o644)
	c := New(Options{SessionPath: session, SessionDir: dir, Sink: event.Discard})
	owned, err := c.EnqueueInbox(InboxRequest{Submit: "owned by composer", Source: "desktop"})
	if err != nil {
		t.Fatal(err)
	}
	unrelated, err := c.EnqueueInbox(InboxRequest{Submit: "owned by bot", Source: "bot"})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.CancelWithInboxItems([]string{owned.ItemID, unrelated.ItemID}, "desktop"); err != nil {
		t.Fatal(err)
	}
	snap := c.InboxSnapshot()
	if snap.Paused {
		t.Fatal("successful scoped cancel left inbox paused")
	}
	if len(snap.Items) != 1 || snap.Items[0].ID != unrelated.ItemID {
		t.Fatalf("scoped cancel left items = %+v", snap.Items)
	}
}

func TestRunTurnAcknowledgesAcceptedDurableItems(t *testing.T) {
	dir := t.TempDir()
	runner := &fakeTurnRunner{}
	c := New(Options{
		Runner:      runner,
		SessionPath: filepath.Join(dir, "s.jsonl"),
		SessionDir:  dir,
		Sink:        event.Discard,
	})
	rec, err := c.EnqueueInbox(InboxRequest{Submit: "accepted steer", Idempotency: "steer-1"})
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
	c.inbox.mu.Lock()
	c.inbox.trackActive(rec.ItemID)
	c.inbox.mu.Unlock()

	if err := c.RunTurn(context.Background(), "foreground"); err != nil {
		t.Fatal(err)
	}
	if got := c.InboxSnapshot().Items; len(got) != 0 {
		t.Fatalf("synchronous completion left accepted item queued: %+v", got)
	}
	if len(runner.inputs) != 1 || runner.inputs[0] != "foreground" {
		t.Fatalf("runner inputs = %q", runner.inputs)
	}
}

func TestRunInboxTurnClaimsAndAcknowledgesFIFOItems(t *testing.T) {
	dir := t.TempDir()
	runner := &fakeTurnRunner{}
	c := New(Options{
		Runner:      runner,
		SessionPath: filepath.Join(dir, "s.jsonl"),
		SessionDir:  dir,
		Sink:        event.Discard,
	})
	var ids []string
	for _, input := range []string{"first", "second"} {
		rec, err := c.EnqueueInbox(InboxRequest{Submit: input, Idempotency: "msg-" + input})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, rec.ItemID)
	}
	for _, id := range ids {
		if err := c.RunInboxTurn(context.Background(), id); err != nil {
			t.Fatal(err)
		}
	}
	if got := runner.inputs; len(got) != 2 || got[0] != "first" || got[1] != "second" {
		t.Fatalf("durable FIFO inputs = %q", got)
	}
	if got := c.InboxSnapshot().Items; len(got) != 0 {
		t.Fatalf("completed FIFO items remain queued: %+v", got)
	}
}

func TestStructuredInboxInvocationSurvivesReopenAndRunsSkill(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	skills := []skill.Skill{{
		Name: "init", Body: "INITIALIZE_FROM_DURABLE_INBOX", RunAs: skill.RunInline, Scope: skill.ScopeGlobal,
	}}
	first := New(Options{SessionPath: path, SessionDir: dir, Skills: skills, Sink: event.Discard})
	rec, err := first.EnqueueInbox(InboxRequest{
		Display:     "/init",
		Idempotency: "desktop-submit-1",
		Invocations: []InvocationRequest{{Name: "init", Kind: "skill", Offset: 0}},
	})
	if err != nil {
		t.Fatal(err)
	}
	first.inbox.mu.Lock()
	first.inbox.store.Close()
	first.inbox.store = nil
	first.inbox.mu.Unlock()

	runner := &fakeTurnRunner{}
	reopened := New(Options{
		Runner: runner, SessionPath: path, SessionDir: dir, Skills: skills, Sink: event.Discard,
	})
	if err := reopened.RunInboxTurn(context.Background(), rec.ItemID); err != nil {
		t.Fatal(err)
	}
	if len(runner.inputs) != 1 || !strings.Contains(runner.inputs[0], "INITIALIZE_FROM_DURABLE_INBOX") {
		t.Fatalf("reopened structured turn lost skill semantics: %q", runner.inputs)
	}
	if strings.Contains(runner.inputs[0], "/init") {
		t.Fatalf("structured turn degraded to slash text: %q", runner.inputs[0])
	}
	if got := reopened.InboxSnapshot().Items; len(got) != 0 {
		t.Fatalf("structured item was not acknowledged: %+v", got)
	}
}

func TestLegacySingularInboxInvocationInfersSkillKind(t *testing.T) {
	dir := t.TempDir()
	runner := &fakeTurnRunner{}
	c := New(Options{
		Runner: runner, SessionPath: filepath.Join(dir, "s.jsonl"), SessionDir: dir, Sink: event.Discard,
		Skills: []skill.Skill{{Name: "legacy", Body: "LEGACY_SKILL_BODY", RunAs: skill.RunInline, Scope: skill.ScopeGlobal}},
	})
	st, err := c.ensureInbox()
	if err != nil {
		t.Fatal(err)
	}
	rec, err := st.Enqueue(sessioninbox.EnqueueRequest{Envelope: sessioninbox.PromptEnvelope{
		DisplayText: "/legacy",
		Invocation:  &sessioninbox.StructuredInvocation{Name: "legacy"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.RunInboxTurn(context.Background(), rec.ItemID); err != nil {
		t.Fatal(err)
	}
	if len(runner.inputs) != 1 || !strings.Contains(runner.inputs[0], "LEGACY_SKILL_BODY") {
		t.Fatalf("legacy structured input = %q", runner.inputs)
	}
}
