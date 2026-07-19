package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type remoteAppCapturedEvent struct {
	name    string
	payload []interface{}
}

type remoteAppLockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *remoteAppLockedBuffer) Write(value []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(value)
}

func (b *remoteAppLockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func newRemoteAppTestStore(t *testing.T) *RemoteHostStore {
	t.Helper()
	store, err := NewRemoteHostStore(filepath.Join(t.TempDir(), "remote-hosts.json"))
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func newRemoteAppTestHost(t *testing.T, store *RemoteHostStore, alias, label string) RemoteHostEntry {
	t.Helper()
	entry, err := NewRemoteHostEntry(alias, label)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Upsert(entry); err != nil {
		t.Fatal(err)
	}
	return entry
}

func installRemoteAppTestState(t *testing.T, app *App, store *RemoteHostStore, manager *TargetManager) {
	t.Helper()
	if app.ctx == nil {
		app.ctx = context.Background()
	}
	app.remote.initOnce.Do(func() {
		app.remote.store = store
		app.remote.manager = manager
		app.remote.pending = make(map[string]remoteAskPassPending)
	})
	t.Cleanup(func() {
		manager.SetStateSink(nil)
		manager.SetEventSink(nil)
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := manager.Shutdown(ctx); err != nil {
			t.Errorf("shutdown TargetManager: %v", err)
		}
	})
}

func TestRemoteAppHostCRUDPreservesPrivateStableIdentities(t *testing.T) {
	store := newRemoteAppTestStore(t)
	local := newTargetManagerTestAdapter(TargetDescriptor{Kind: TargetLocal, ID: remoteLocalTargetID, Label: "Local"})
	manager, err := NewTargetManager(TargetConnectorFunc(func(context.Context, TargetDescriptor) (TargetAdapter, error) {
		return nil, errors.New("unexpected target connection")
	}), local, TargetManagerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	app := &App{ctx: context.Background()}
	installRemoteAppTestState(t, app, store, manager)

	created, err := app.SaveRemoteHost(RemoteHostInput{Alias: "lab", Label: "Linux lab"})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.Mode != RemoteHostConnectionConfig || created.Alias != "lab" || created.Label != "Linux lab" {
		t.Fatalf("created Host view = %#v", created)
	}
	privateBefore, found, err := store.Get(created.ID)
	if err != nil || !found {
		t.Fatalf("load created Host = %#v, %v, found=%v", privateBefore, err, found)
	}
	const leaseID = "lease_stable_across_editor_updates"
	if err := store.UpdateResumeLease(created.ID, leaseID); err != nil {
		t.Fatal(err)
	}

	// Model an untrusted frontend payload containing private-looking fields.
	// RemoteHostInput has no such fields, so decoding cannot overwrite either
	// Desktop-owned identity.
	var input RemoteHostInput
	rawInput := []byte(`{"id":"` + created.ID + `","alias":"lab-renamed","label":"Renamed lab","clientInstanceId":"desktop_attacker","resumeLeaseId":"lease_attacker"}`)
	if err := json.Unmarshal(rawInput, &input); err != nil {
		t.Fatal(err)
	}
	updated, err := app.SaveRemoteHost(input)
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != created.ID || updated.Alias != "lab-renamed" || updated.Label != "Renamed lab" {
		t.Fatalf("updated Host view = %#v", updated)
	}
	privateAfter, found, err := store.Get(created.ID)
	if err != nil || !found {
		t.Fatalf("load updated Host = %#v, %v, found=%v", privateAfter, err, found)
	}
	if privateAfter.ClientInstanceID != privateBefore.ClientInstanceID || privateAfter.ResumeLeaseID != "" {
		t.Fatalf("private identities changed: before=%#v after=%#v", privateBefore, privateAfter)
	}

	hosts, err := app.RemoteHosts()
	if err != nil {
		t.Fatal(err)
	}
	if len(hosts) != 1 || hosts[0] != updated {
		t.Fatalf("RemoteHosts = %#v, want %#v", hosts, updated)
	}
	exposed, err := json.Marshal(struct {
		Created RemoteHostView   `json:"created"`
		Updated RemoteHostView   `json:"updated"`
		Hosts   []RemoteHostView `json:"hosts"`
	}{Created: created, Updated: updated, Hosts: hosts})
	if err != nil {
		t.Fatal(err)
	}
	for _, privateValue := range []string{privateBefore.ClientInstanceID, leaseID, "clientInstanceId", "resumeLeaseId"} {
		if bytes.Contains(exposed, []byte(privateValue)) {
			t.Fatalf("Host API exposed private identity %q in %s", privateValue, exposed)
		}
	}

	if err := app.DeleteRemoteHost(created.ID); err != nil {
		t.Fatal(err)
	}
	if _, found, err := store.Get(created.ID); err != nil || found {
		t.Fatalf("deleted Host lookup found=%v err=%v", found, err)
	}
}

func TestRemoteAppDirectHostDefaultsPortAndCanSwitchModes(t *testing.T) {
	store := newRemoteAppTestStore(t)
	local := newTargetManagerTestAdapter(TargetDescriptor{Kind: TargetLocal, ID: remoteLocalTargetID, Label: "Local"})
	manager, err := NewTargetManager(TargetConnectorFunc(func(context.Context, TargetDescriptor) (TargetAdapter, error) {
		return nil, errors.New("unexpected target connection")
	}), local, TargetManagerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	app := &App{ctx: context.Background()}
	installRemoteAppTestState(t, app, store, manager)

	created, err := app.SaveRemoteHost(RemoteHostInput{Destination: "taibai@EXAMPLE.com."})
	if err != nil {
		t.Fatal(err)
	}
	if created.Mode != RemoteHostConnectionDirect || created.Destination != "taibai@example.com" || created.Port != defaultRemoteSSHPort || created.Label != "taibai@example.com" || created.Alias != "" || created.SSHConfigPath != "" {
		t.Fatalf("created direct Host view = %#v", created)
	}
	privateBefore, found, err := store.Get(created.ID)
	if err != nil || !found {
		t.Fatalf("load direct Host = %#v, %v, found=%v", privateBefore, err, found)
	}
	if err := store.UpdateResumeLease(created.ID, "lease_survives_mode_change"); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(t.TempDir(), "ssh config")
	updated, err := app.SaveRemoteHost(RemoteHostInput{
		ID: created.ID, Mode: RemoteHostConnectionConfig, Alias: "advanced-host",
		SSHConfigPath: configPath, Label: "Advanced lab",
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Mode != RemoteHostConnectionConfig || updated.Alias != "advanced-host" || updated.SSHConfigPath != configPath || updated.Destination != "" || updated.Port != 0 {
		t.Fatalf("updated config Host view = %#v", updated)
	}
	privateAfter, found, err := store.Get(created.ID)
	if err != nil || !found {
		t.Fatalf("load changed Host = %#v, %v, found=%v", privateAfter, err, found)
	}
	if privateAfter.ID != privateBefore.ID || privateAfter.ClientInstanceID != privateBefore.ClientInstanceID || privateAfter.ResumeLeaseID != "" {
		t.Fatalf("private identity changed across connection mode edit: before=%#v after=%#v", privateBefore, privateAfter)
	}
}

func TestNormalizeRemoteHostInputRejectsAmbiguousAndInvalidDirectValues(t *testing.T) {
	for _, input := range []RemoteHostInput{
		{},
		{Destination: "user@host", Alias: "host"},
		{Mode: "unknown", Destination: "user@host", Port: 22},
		{Mode: RemoteHostConnectionDirect, Destination: "user@-oProxyCommand=evil", Port: 22},
		{Mode: RemoteHostConnectionDirect, Destination: "user@host", Port: 65536},
	} {
		if _, err := normalizeRemoteHostInput(input); err == nil {
			t.Errorf("normalizeRemoteHostInput(%#v) unexpectedly succeeded", input)
		}
	}
}

func TestRemoteAppRejectsEditingOrDeletingActiveHost(t *testing.T) {
	store := newRemoteAppTestStore(t)
	host := newRemoteAppTestHost(t, store, "active-host", "Active Host")
	if err := store.UpdateResumeLease(host.ID, "lease_must_survive_rejected_editor_calls"); err != nil {
		t.Fatal(err)
	}
	active := newTargetManagerTestAdapter(TargetDescriptor{Kind: TargetRemote, ID: host.ID, Label: host.Label})
	manager, err := NewTargetManager(TargetConnectorFunc(func(context.Context, TargetDescriptor) (TargetAdapter, error) {
		return nil, errors.New("unexpected target connection")
	}), active, TargetManagerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	app := &App{ctx: context.Background()}
	installRemoteAppTestState(t, app, store, manager)

	if _, err := app.SaveRemoteHost(RemoteHostInput{ID: host.ID, Alias: "changed", Label: "Changed"}); err == nil {
		t.Fatal("editing an active Remote Host unexpectedly succeeded")
	}
	if err := app.DeleteRemoteHost(host.ID); err == nil {
		t.Fatal("deleting an active Remote Host unexpectedly succeeded")
	}
	got, found, err := store.Get(host.ID)
	if err != nil || !found {
		t.Fatalf("active Host lookup found=%v err=%v", found, err)
	}
	if got.Alias != host.Alias || got.Label != host.Label || got.ClientInstanceID != host.ClientInstanceID || got.ResumeLeaseID == "" {
		t.Fatalf("active Host mutated after rejected editor calls: %#v", got)
	}
}

func TestRemoteAppConnectFailureDoesNotFallBackToLocal(t *testing.T) {
	store := newRemoteAppTestStore(t)
	host := newRemoteAppTestHost(t, store, "offline-host", "Offline Host")
	local := newTargetManagerTestAdapter(TargetDescriptor{Kind: TargetLocal, ID: remoteLocalTargetID, Label: "Local"})
	connectErr := errors.New("SSH connection refused")
	var calls atomic.Int32
	manager, err := NewTargetManager(TargetConnectorFunc(func(_ context.Context, target TargetDescriptor) (TargetAdapter, error) {
		calls.Add(1)
		if target.Kind != TargetRemote || target.ID != host.ID {
			t.Errorf("connector target = %#v, want saved Remote Host", target)
		}
		return nil, connectErr
	}), local, TargetManagerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	app := &App{ctx: context.Background()}
	installRemoteAppTestState(t, app, store, manager)

	if err := app.ConnectRemoteHost(host.ID); !errors.Is(err, connectErr) {
		t.Fatalf("ConnectRemoteHost error = %v, want %v", err, connectErr)
	}
	if calls.Load() != 1 || local.detachCalls.Load() != 1 {
		t.Fatalf("connect/detach calls = %d/%d, want 1/1", calls.Load(), local.detachCalls.Load())
	}
	snapshot := manager.Snapshot()
	if snapshot.State != TargetDisconnected || snapshot.Target.Kind != TargetRemote || snapshot.Target.ID != host.ID || snapshot.LastError != connectErr.Error() {
		t.Fatalf("failed Remote snapshot = %#v", snapshot)
	}
	if _, err := manager.RuntimeAPI(); !errors.Is(err, ErrRuntimeTargetUnavailable) {
		t.Fatalf("RuntimeAPI after failed Remote connection = %v, want unavailable", err)
	}
	status := app.RemoteTargetStatus()
	if status.State != TargetDisconnected || status.HostID != host.ID || status.Failure != connectErr.Error() {
		t.Fatalf("RemoteTargetStatus after failure = %#v", status)
	}
}

func TestRemoteAppRemoteToLocalRequiresExplicitConfirmation(t *testing.T) {
	store := newRemoteAppTestStore(t)
	host := newRemoteAppTestHost(t, store, "connected-host", "Connected Host")
	remote := newTargetManagerTestAdapter(TargetDescriptor{Kind: TargetRemote, ID: host.ID, Label: host.Label})
	local := newTargetManagerTestAdapter(TargetDescriptor{Kind: TargetLocal, ID: remoteLocalTargetID, Label: "Local"})
	var connectCalls atomic.Int32
	manager, err := NewTargetManager(TargetConnectorFunc(func(_ context.Context, target TargetDescriptor) (TargetAdapter, error) {
		connectCalls.Add(1)
		if target.Kind != TargetLocal {
			return nil, errors.New("unexpected non-Local target")
		}
		return local, nil
	}), remote, TargetManagerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	app := &App{ctx: context.Background()}
	installRemoteAppTestState(t, app, store, manager)

	if err := app.SwitchToLocalTarget(false); !errors.Is(err, ErrRemoteDetachConfirmation) {
		t.Fatalf("unconfirmed switch error = %v, want ErrRemoteDetachConfirmation", err)
	}
	if remote.detachCalls.Load() != 0 || connectCalls.Load() != 0 {
		t.Fatalf("unconfirmed detach/connect calls = %d/%d, want 0/0", remote.detachCalls.Load(), connectCalls.Load())
	}
	if snapshot := manager.Snapshot(); snapshot.State != TargetRemoteConnected || snapshot.Target.ID != host.ID {
		t.Fatalf("snapshot after unconfirmed switch = %#v", snapshot)
	}
	if err := app.SwitchToLocalTarget(true); err != nil {
		t.Fatal(err)
	}
	if remote.detachCalls.Load() != 1 || connectCalls.Load() != 1 {
		t.Fatalf("confirmed detach/connect calls = %d/%d, want 1/1", remote.detachCalls.Load(), connectCalls.Load())
	}
	if snapshot := manager.Snapshot(); snapshot.State != TargetLocalConnected || snapshot.Target.ID != remoteLocalTargetID {
		t.Fatalf("snapshot after confirmed switch = %#v", snapshot)
	}
}

func TestRemoteAppStateSinkAsynchronouslyPublishesLossAndReconnect(t *testing.T) {
	store := newRemoteAppTestStore(t)
	host := newRemoteAppTestHost(t, store, "flaky-host", "Flaky Host")
	app := &App{ctx: context.Background()}

	emitEntered := make(chan struct{})
	releaseEmit := make(chan struct{})
	emitted := make(chan RemoteTargetStatusView, 16)
	var enterOnce sync.Once
	app.runtimeEvents.emit = func(_ context.Context, name string, payload ...interface{}) {
		if name != remoteTargetStateEvent {
			return
		}
		enterOnce.Do(func() { close(emitEntered) })
		<-releaseEmit
		if len(payload) != 1 {
			t.Errorf("target-state payload count = %d, want 1", len(payload))
			return
		}
		view, ok := payload[0].(RemoteTargetStatusView)
		if !ok {
			t.Errorf("target-state payload = %T, want RemoteTargetStatusView", payload[0])
			return
		}
		emitted <- view
	}

	faults := make(chan error, 1)
	remote := newTargetManagerTestAdapter(TargetDescriptor{Kind: TargetRemote, ID: host.ID, Label: host.Label})
	remote.faults = faults
	replacement := newTargetManagerTestAdapter(remote.target)
	connector := &targetManagerTestConnector{
		connectFn: func(context.Context, TargetDescriptor) (TargetAdapter, error) {
			return nil, errors.New("Reconnect must preserve recovery instead of Connect")
		},
		reconnectFn: func(context.Context, TargetDescriptor, TargetAdapter) (TargetAdapter, error) {
			return replacement, nil
		},
	}
	manager, err := NewTargetManager(connector, remote, TargetManagerOptions{StateSink: app.handleTargetState})
	if err != nil {
		t.Fatal(err)
	}
	installRemoteAppTestState(t, app, store, manager)
	waitSignal(t, emitEntered, "blocked async target-state emitter")

	faults <- errors.New("SSH transport lost")
	lost := waitSnapshot(t, manager, func(snapshot TargetManagerSnapshot) bool {
		return snapshot.State == TargetRemoteReconnecting
	}, "Remote transport-loss state while Wails emitter is blocked")
	if !lost.RecoveryAvailable || lost.LastError != "SSH transport lost" {
		t.Fatalf("transport-loss snapshot = %#v", lost)
	}

	reconnected := make(chan error, 1)
	go func() { reconnected <- app.ReconnectRemoteTarget() }()
	if err := waitValue(t, reconnected, "ReconnectRemoteTarget while Wails emitter is blocked"); err != nil {
		t.Fatal(err)
	}
	if snapshot := manager.Snapshot(); snapshot.State != TargetRemoteConnected || snapshot.Generation <= lost.Generation {
		t.Fatalf("reconnected snapshot = %#v", snapshot)
	}
	close(releaseEmit)

	deadline := time.Now().Add(2 * time.Second)
	var sawLoss, sawConnecting, sawConnected bool
	for time.Now().Before(deadline) && !(sawLoss && sawConnecting && sawConnected) {
		select {
		case view := <-emitted:
			switch view.State {
			case TargetRemoteReconnecting:
				sawLoss = view.CanReconnect && view.Failure == "SSH transport lost" && view.HostID == host.ID
			case TargetRemoteConnecting:
				sawConnecting = view.HostID == host.ID
			case TargetRemoteConnected:
				if sawConnecting && view.Failure == "" && !view.CanReconnect && view.HostID == host.ID {
					sawConnected = true
				}
			}
		case <-time.After(10 * time.Millisecond):
		}
	}
	if !sawLoss || !sawConnecting || !sawConnected {
		t.Fatalf("state events: loss=%v connecting=%v connected=%v", sawLoss, sawConnecting, sawConnected)
	}
}

func TestRemoteAppAskPassBridgeIsOneShotAndSecretFree(t *testing.T) {
	store := newRemoteAppTestStore(t)
	host := newRemoteAppTestHost(t, store, "secret-host", "Secret Host")
	manager, err := NewTargetManager(TargetConnectorFunc(func(context.Context, TargetDescriptor) (TargetAdapter, error) {
		return nil, errors.New("unexpected target connection")
	}), nil, TargetManagerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	app := &App{ctx: context.Background()}
	installRemoteAppTestState(t, app, store, manager)

	events := make(chan remoteAppCapturedEvent, 8)
	app.runtimeEvents.emit = func(_ context.Context, name string, payload ...interface{}) {
		events <- remoteAppCapturedEvent{name: name, payload: append([]interface{}(nil), payload...)}
	}
	var logs remoteAppLockedBuffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	defer slog.SetDefault(previousLogger)

	const secret = "remote-app-answer-secret-9f43a1"
	answerResult := make(chan RemoteAskPassAnswer, 1)
	answerErr := make(chan error, 1)
	go func() {
		answer, err := app.handleRemoteAskPass(context.Background(), RemoteAskPassPrompt{
			Kind: RemoteAskPassPassword, Message: "Password required", HostLabel: host.Label,
		})
		answerResult <- answer
		answerErr <- err
	}()
	firstEvent := waitValue(t, events, "AskPass password event")
	firstView := remoteAppAskPassViewFromEvent(t, firstEvent)
	if !firstView.Secret || firstView.Kind != RemoteAskPassPassword || firstView.HostLabel != host.Label {
		t.Fatalf("password AskPass view = %#v", firstView)
	}
	serializedView, err := json.Marshal(firstView)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(serializedView, []byte(secret)) {
		t.Fatalf("AskPass event exposed answer secret: %s", serializedView)
	}
	if err := app.RespondRemoteAskPass(firstView.RequestID, secret+"\n", false); err == nil {
		t.Fatal("AskPass response containing a newline unexpectedly succeeded")
	}

	responses := make(chan error, 2)
	go func() { responses <- app.RespondRemoteAskPass(firstView.RequestID, secret, false) }()
	go func() { responses <- app.RespondRemoteAskPass(firstView.RequestID, secret, false) }()
	var successes int
	for range 2 {
		if err := <-responses; err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("concurrent AskPass response successes = %d, want exactly 1", successes)
	}
	if err := waitValue(t, answerErr, "AskPass password result"); err != nil {
		t.Fatal(err)
	}
	if answer := waitValue(t, answerResult, "AskPass password answer"); !answer.Accepted || answer.Value != secret {
		t.Fatalf("AskPass password answer = %#v", answer)
	}
	if err := app.RespondRemoteAskPass(firstView.RequestID, secret, false); err == nil {
		t.Fatal("stale answered AskPass request unexpectedly succeeded")
	}

	cancelResult := make(chan RemoteAskPassAnswer, 1)
	go func() {
		answer, _ := app.handleRemoteAskPass(context.Background(), RemoteAskPassPrompt{
			Kind: RemoteAskPassHostKeyConfirm, Message: "Trust this key?", HostLabel: host.Label,
		})
		cancelResult <- answer
	}()
	cancelView := remoteAppAskPassViewFromEvent(t, waitValue(t, events, "AskPass host-key event"))
	if cancelView.Secret {
		t.Fatalf("host-key confirmation was marked secret: %#v", cancelView)
	}
	if err := app.RespondRemoteAskPass(cancelView.RequestID, "", true); err != nil {
		t.Fatal(err)
	}
	if answer := waitValue(t, cancelResult, "cancelled AskPass answer"); answer.Accepted || answer.Value != "" {
		t.Fatalf("cancelled AskPass answer = %#v", answer)
	}
	if err := app.RespondRemoteAskPass(cancelView.RequestID, "", true); err == nil {
		t.Fatal("stale cancelled AskPass request unexpectedly succeeded")
	}

	ctx, cancel := context.WithCancel(context.Background())
	staleErr := make(chan error, 1)
	go func() {
		_, err := app.handleRemoteAskPass(ctx, RemoteAskPassPrompt{Kind: RemoteAskPassVerification, Message: "Verification code"})
		staleErr <- err
	}()
	staleView := remoteAppAskPassViewFromEvent(t, waitValue(t, events, "AskPass expiring event"))
	cancel()
	if err := waitValue(t, staleErr, "expired AskPass result"); !errors.Is(err, context.Canceled) {
		t.Fatalf("expired AskPass error = %v, want context.Canceled", err)
	}
	if err := app.RespondRemoteAskPass(staleView.RequestID, "123456", false); err == nil {
		t.Fatal("expired AskPass request unexpectedly succeeded")
	}
	for _, invalidID := range []string{"", "askpass_bad", "askpass_" + strings.Repeat("z", 64)} {
		if err := app.RespondRemoteAskPass(invalidID, "value", false); err == nil {
			t.Fatalf("invalid/unknown AskPass request %q unexpectedly succeeded", invalidID)
		}
	}

	persisted, err := os.ReadFile(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(persisted, []byte(secret)) {
		t.Fatalf("AskPass answer secret persisted in Host store: %s", persisted)
	}
	if strings.Contains(logs.String(), secret) {
		t.Fatalf("AskPass answer secret was logged: %s", logs.String())
	}
	app.remote.askMu.Lock()
	pendingCount := len(app.remote.pending)
	app.remote.askMu.Unlock()
	if pendingCount != 0 {
		t.Fatalf("pending AskPass requests = %d, want 0", pendingCount)
	}
}

func remoteAppAskPassViewFromEvent(t *testing.T, event remoteAppCapturedEvent) RemoteAskPassView {
	t.Helper()
	if event.name != remoteAskPassEvent {
		t.Fatalf("event name = %q, want %q", event.name, remoteAskPassEvent)
	}
	if len(event.payload) != 1 {
		t.Fatalf("AskPass payload count = %d, want 1", len(event.payload))
	}
	view, ok := event.payload[0].(RemoteAskPassView)
	if !ok {
		t.Fatalf("AskPass payload = %T, want RemoteAskPassView", event.payload[0])
	}
	return view
}

func TestRemoteAppConnectionLogsPreserveLifecycleOrder(t *testing.T) {
	store := newRemoteAppTestStore(t)
	manager, err := NewTargetManager(TargetConnectorFunc(func(context.Context, TargetDescriptor) (TargetAdapter, error) {
		return nil, errors.New("unexpected target connection")
	}), nil, TargetManagerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	app := &App{ctx: context.Background()}
	installRemoteAppTestState(t, app, store, manager)

	host := TargetDescriptor{Kind: TargetRemote, ID: "host_lifecycle", Label: "Lifecycle Host"}
	want := []TargetManagerSnapshot{
		{State: TargetRemoteConnecting, Target: host, Generation: 1},
		{State: TargetRemoteConnected, Target: host, Generation: 1},
		{State: TargetRemoteReconnecting, Target: host, Generation: 2, RecoveryAvailable: true, LastError: "connection interrupted"},
		{State: TargetRemoteConnecting, Target: host, Generation: 3},
		{State: TargetRemoteConnected, Target: host, Generation: 3},
	}
	for _, snapshot := range want {
		app.handleTargetState(snapshot)
	}

	got := app.RemoteConnectionLogs()
	if len(got) != len(want) {
		t.Fatalf("RemoteConnectionLogs length = %d, want %d: %#v", len(got), len(want), got)
	}
	for index, snapshot := range want {
		if got[index].State != snapshot.State || got[index].HostID != host.ID || got[index].HostLabel != host.Label {
			t.Fatalf("log[%d] = %#v, want state=%s Host=%#v", index, got[index], snapshot.State, host)
		}
		if index > 0 && got[index].AtMillis < got[index-1].AtMillis {
			t.Fatalf("log timestamps are not ordered at %d: %d < %d", index, got[index].AtMillis, got[index-1].AtMillis)
		}
	}
	if got[2].Message != "connection interrupted" {
		t.Fatalf("reconnecting message = %q, want connection interrupted", got[2].Message)
	}
}

func TestRemoteAppConnectionLogsAreBoundedAndReturnedSliceIsolated(t *testing.T) {
	store := newRemoteAppTestStore(t)
	manager, err := NewTargetManager(TargetConnectorFunc(func(context.Context, TargetDescriptor) (TargetAdapter, error) {
		return nil, errors.New("unexpected target connection")
	}), nil, TargetManagerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	app := &App{ctx: context.Background()}
	installRemoteAppTestState(t, app, store, manager)

	host := TargetDescriptor{Kind: TargetRemote, ID: "host_bounded", Label: "Bounded Host"}
	const extra = 37
	for index := 0; index < remoteConnectionLogMax+extra; index++ {
		app.recordRemoteConnectionState(TargetManagerSnapshot{
			State: TargetRemoteConnecting, Target: host, Generation: uint64(index + 1),
			LastError: fmt.Sprintf("lifecycle-%03d", index),
		})
	}

	first := app.RemoteConnectionLogs()
	if len(first) != remoteConnectionLogMax {
		t.Fatalf("RemoteConnectionLogs length = %d, want %d", len(first), remoteConnectionLogMax)
	}
	if first[0].Message != fmt.Sprintf("lifecycle-%03d", extra) {
		t.Fatalf("oldest retained log = %q, want lifecycle-%03d", first[0].Message, extra)
	}
	if first[len(first)-1].Message != fmt.Sprintf("lifecycle-%03d", remoteConnectionLogMax+extra-1) {
		t.Fatalf("newest retained log = %q", first[len(first)-1].Message)
	}

	first[0] = RemoteConnectionLogView{State: TargetDisconnected, HostID: "mutated", Message: "mutated"}
	first = append(first, RemoteConnectionLogView{Message: "caller append"})
	second := app.RemoteConnectionLogs()
	if len(second) != remoteConnectionLogMax {
		t.Fatalf("RemoteConnectionLogs after caller append length = %d, want %d", len(second), remoteConnectionLogMax)
	}
	if second[0].State != TargetRemoteConnecting || second[0].HostID != host.ID || second[0].Message != fmt.Sprintf("lifecycle-%03d", extra) {
		t.Fatalf("stored log was mutated through returned slice: %#v", second[0])
	}
}

type remoteConnectionLogsSensitiveFailure struct {
	message       string
	answer        string
	clientID      string
	leaseID       string
	rawOpenSSHArg string
}

func (e *remoteConnectionLogsSensitiveFailure) Error() string { return e.message }

func TestRemoteAppConnectionLogsDoNotExposeConnectionSecretsOrRawCommand(t *testing.T) {
	store := newRemoteAppTestStore(t)
	host := newRemoteAppTestHost(t, store, "private-host", "Private Host")
	const leaseID = "lease_private_log_marker_2ca901"
	if err := store.UpdateResumeLease(host.ID, leaseID); err != nil {
		t.Fatal(err)
	}
	host, found, err := store.Get(host.ID)
	if err != nil || !found {
		t.Fatalf("load Host with lease found=%v err=%v", found, err)
	}
	const answer = "askpass-answer-private-log-marker-7f812e"
	rawOpenSSHArgv := strings.Join([]string{
		"ssh", "-T", "-o", "RequestTTY=no", "-o", "StrictHostKeyChecking=ask",
		"-o", "ClearAllForwardings=yes", "-o", "PermitLocalCommand=no", "-o", "RemoteCommand=none",
		"--", host.Alias, "reasonix", "remote", "attach", "--stdio",
	}, " ")
	failure := &remoteConnectionLogsSensitiveFailure{
		message:       "OpenSSH authentication failed",
		answer:        answer,
		clientID:      host.ClientInstanceID,
		leaseID:       leaseID,
		rawOpenSSHArg: rawOpenSSHArgv,
	}

	app := &App{ctx: context.Background()}
	askPassEvents := make(chan RemoteAskPassView, 1)
	app.runtimeEvents.emit = func(_ context.Context, name string, payload ...interface{}) {
		if name != remoteAskPassEvent || len(payload) != 1 {
			return
		}
		view, ok := payload[0].(RemoteAskPassView)
		if ok {
			askPassEvents <- view
		}
	}
	local := newTargetManagerTestAdapter(TargetDescriptor{Kind: TargetLocal, ID: remoteLocalTargetID, Label: "Local"})
	manager, err := NewTargetManager(TargetConnectorFunc(func(context.Context, TargetDescriptor) (TargetAdapter, error) {
		return nil, failure
	}), local, TargetManagerOptions{StateSink: app.handleTargetState})
	if err != nil {
		t.Fatal(err)
	}
	installRemoteAppTestState(t, app, store, manager)
	if err := app.ConnectRemoteHost(host.ID); !errors.Is(err, failure) {
		t.Fatalf("ConnectRemoteHost error = %v, want structured failure", err)
	}

	answerResult := make(chan RemoteAskPassAnswer, 1)
	answerError := make(chan error, 1)
	go func() {
		got, err := app.handleRemoteAskPass(context.Background(), RemoteAskPassPrompt{
			Kind: RemoteAskPassPassword, Message: "Password required", HostLabel: host.Label,
		})
		answerResult <- got
		answerError <- err
	}()
	askPassView := waitValue(t, askPassEvents, "secret-free connection-log AskPass event")
	if err := app.RespondRemoteAskPass(askPassView.RequestID, answer, false); err != nil {
		t.Fatal(err)
	}
	if err := waitValue(t, answerError, "secret-free connection-log AskPass result"); err != nil {
		t.Fatal(err)
	}
	if got := waitValue(t, answerResult, "secret-free connection-log AskPass answer"); !got.Accepted || got.Value != answer {
		t.Fatalf("AskPass answer = %#v", got)
	}

	logs, err := json.Marshal(app.RemoteConnectionLogs())
	if err != nil {
		t.Fatal(err)
	}
	for label, privateValue := range map[string]string{
		"AskPass answer":        answer,
		"client instance ID":    host.ClientInstanceID,
		"resume lease ID":       leaseID,
		"raw OpenSSH argv":      rawOpenSSHArgv,
		"client identity field": "clientInstanceId",
		"lease identity field":  "resumeLeaseId",
	} {
		if bytes.Contains(logs, []byte(privateValue)) {
			t.Fatalf("RemoteConnectionLogs exposed %s %q: %s", label, privateValue, logs)
		}
	}
	if !bytes.Contains(logs, []byte(failure.message)) {
		t.Fatalf("RemoteConnectionLogs omitted safe structured failure message: %s", logs)
	}
}
