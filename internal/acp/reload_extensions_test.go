package acp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"reasonix/internal/command"
	"reasonix/internal/control"
)

// reloadFactory wraps configurableFactory with the SessionRebuilder seam the
// reloadExtensions handler requires, recording the rebuild base controller.
type reloadFactory struct {
	*configurableFactory
	rebuildCalls int
	lastOld      *control.Controller
	rebuildErr   error
	replacement  *control.Controller
}

func (f *reloadFactory) RebuildSession(_ context.Context, _ SessionParams, old *control.Controller) (*control.Controller, error) {
	f.rebuildCalls++
	f.lastOld = old
	if f.rebuildErr != nil {
		return nil, f.rebuildErr
	}
	if f.replacement != nil {
		return f.replacement, nil
	}
	return control.New(control.Options{Label: "rebuilt"}), nil
}

func reloadExtensionsSession(t *testing.T, id string, ctrl acpController, notifier *fakeNotifier) *acpSession {
	t.Helper()
	return &acpSession{
		id:               id,
		ctrl:             ctrl,
		sink:             newUpdateSink(notifier, id),
		cwd:              t.TempDir(),
		model:            "fast",
		runtimeProfile:   "balanced",
		toolApprovalMode: control.ToolApprovalAsk,
		modeID:           sessionModeNormal,
	}
}

func marshalReloadParams(t *testing.T, sessionID string) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(SessionReloadExtensionsParams{SessionID: sessionID})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// TestSessionReloadExtensionsUnknownSession mirrors the sessionSteer unknown-
// session contract.
func TestSessionReloadExtensionsUnknownSession(t *testing.T) {
	svc := &service{factory: &configurableFactory{}, sessions: map[string]*acpSession{}}
	_, err := svc.sessionReloadExtensions(context.Background(), marshalReloadParams(t, "nope"))
	var rpcErr *RPCError
	if !errors.As(err, &rpcErr) {
		t.Fatalf("err = %T %v, want *RPCError", err, err)
	}
	if rpcErr.Code != ErrInvalidParams {
		t.Fatalf("code = %d, want ErrInvalidParams", rpcErr.Code)
	}
	if !strings.Contains(rpcErr.Message, "unknown session") {
		t.Fatalf("message = %q, want unknown-session detail", rpcErr.Message)
	}
}

// TestSessionReloadExtensionsUnavailableWithoutRebuilder: a Factory without
// the SessionRebuilder seam fails closed instead of falling back to a plain
// rebuild.
func TestSessionReloadExtensionsUnavailableWithoutRebuilder(t *testing.T) {
	notifier := &fakeNotifier{}
	sess := reloadExtensionsSession(t, "sess-reload-noseam", control.New(control.Options{}), notifier)
	svc := &service{factory: &configurableFactory{}, sessions: map[string]*acpSession{sess.id: sess}}
	_, err := svc.sessionReloadExtensions(context.Background(), marshalReloadParams(t, sess.id))
	var rpcErr *RPCError
	if !errors.As(err, &rpcErr) {
		t.Fatalf("err = %T %v, want *RPCError", err, err)
	}
	if rpcErr.Code != ErrInvalidRequest {
		t.Fatalf("code = %d, want ErrInvalidRequest", rpcErr.Code)
	}
	if !strings.Contains(rpcErr.Message, "unavailable") {
		t.Fatalf("message = %q, want unavailable detail", rpcErr.Message)
	}
}

// TestSessionReloadExtensionsSwapsAndClosesOldAfterSwap covers the success
// path: the replacement is built from the outgoing controller, published
// before the outgoing one is released, and clients get a fresh
// available_commands_update.
func TestSessionReloadExtensionsSwapsAndClosesOldAfterSwap(t *testing.T) {
	notifier := &fakeNotifier{}
	released := false
	var ctrlAtRelease acpController
	var sess *acpSession
	old := control.New(control.Options{
		Label: "old",
		Cleanup: func() {
			released = true
			ctrlAtRelease = sess.ctrl
		},
	})
	replacement := control.New(control.Options{
		Label:    "rebuilt",
		Commands: []command.Command{{Name: "fresh-cmd", Description: "from the reloaded runtime"}},
	})
	factory := &reloadFactory{configurableFactory: &configurableFactory{}, replacement: replacement}
	sess = reloadExtensionsSession(t, "sess-reload-ok", old, notifier)
	svc := &service{factory: factory, sessions: map[string]*acpSession{sess.id: sess}}

	res, err := svc.sessionReloadExtensions(context.Background(), marshalReloadParams(t, sess.id))
	if err != nil {
		t.Fatalf("sessionReloadExtensions: %v", err)
	}
	if got, ok := res.(SessionReloadExtensionsResult); !ok || got.Queued {
		t.Fatalf("result = %#v, want SessionReloadExtensionsResult{Queued:false}", res)
	}
	if factory.rebuildCalls != 1 {
		t.Fatalf("rebuild ran %d times, want 1", factory.rebuildCalls)
	}
	if factory.lastOld != old {
		t.Fatal("replacement was not built from the outgoing controller")
	}
	if sess.ctrl != replacement {
		t.Fatal("session controller was not swapped to the replacement")
	}
	if !released {
		t.Fatal("outgoing controller was not released")
	}
	if ctrlAtRelease != replacement {
		t.Fatal("outgoing controller was released before the swap published the replacement")
	}
	// Refreshed plugin commands are pushed to the client without waiting for
	// the next turn.
	foundCommands := false
	for i := range notifier.notifs {
		if reloadTestUpdateMap(t, notifier, i)["sessionUpdate"] == "available_commands_update" {
			foundCommands = true
			break
		}
	}
	if !foundCommands {
		t.Fatal("no available_commands_update notification after reload")
	}
}

// reloadTestUpdateMap decodes the i-th captured session/update notification's
// nested update object (fakeNotifier.updateMap pins another test's session
// id, so this package-local variant skips that check).
func reloadTestUpdateMap(t *testing.T, f *fakeNotifier, i int) map[string]any {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if i >= len(f.notifs) {
		t.Fatalf("only %d notifications captured, wanted index %d", len(f.notifs), i)
	}
	raw, err := json.Marshal(f.notifs[i].params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	var decoded struct {
		Update map[string]any `json:"update"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal params: %v", err)
	}
	return decoded.Update
}

// TestSessionReloadExtensionsBusyQueuesThenDrains covers the queue contract:
// exactly one reload is coalesced while a turn runs, and the drain rebuilds
// once the session is idle again.
func TestSessionReloadExtensionsBusyQueuesThenDrains(t *testing.T) {
	notifier := &fakeNotifier{}
	factory := &reloadFactory{configurableFactory: &configurableFactory{}}
	sess := reloadExtensionsSession(t, "sess-reload-busy", control.New(control.Options{Label: "old"}), notifier)
	svc := &service{factory: factory, sessions: map[string]*acpSession{sess.id: sess}}

	// A turn is in flight.
	if _, _, ok := sess.begin(context.Background()); !ok {
		t.Fatal("could not mark the session running")
	}
	res, err := svc.sessionReloadExtensions(context.Background(), marshalReloadParams(t, sess.id))
	if err != nil {
		t.Fatalf("busy sessionReloadExtensions: %v", err)
	}
	if got, ok := res.(SessionReloadExtensionsResult); !ok || !got.Queued {
		t.Fatalf("result = %#v, want SessionReloadExtensionsResult{Queued:true}", res)
	}
	// A second request while busy coalesces into the same queued reload.
	if _, err := svc.sessionReloadExtensions(context.Background(), marshalReloadParams(t, sess.id)); err != nil {
		t.Fatalf("second busy sessionReloadExtensions: %v", err)
	}
	if factory.rebuildCalls != 0 {
		t.Fatalf("rebuild ran %d times while busy, want 0", factory.rebuildCalls)
	}
	if !sess.pendingReload {
		t.Fatal("busy reload did not queue")
	}

	// The turn finishes; the drain runs exactly one rebuild against the idle
	// session.
	sess.finish()
	svc.drainPendingReload(context.Background(), sess)
	if factory.rebuildCalls != 1 {
		t.Fatalf("drain rebuilt %d times, want exactly 1", factory.rebuildCalls)
	}
	if sess.pendingReload {
		t.Fatal("queued reload flag survived the drain")
	}
}

// TestSessionReloadExtensionsFailureKeepsOldController: a failed build leaves
// the session on the outgoing controller and reports the error.
func TestSessionReloadExtensionsFailureKeepsOldController(t *testing.T) {
	notifier := &fakeNotifier{}
	old := control.New(control.Options{Label: "old"})
	factory := &reloadFactory{configurableFactory: &configurableFactory{}, rebuildErr: errReloadBuildForTest}
	sess := reloadExtensionsSession(t, "sess-reload-fail", old, notifier)
	svc := &service{factory: factory, sessions: map[string]*acpSession{sess.id: sess}}

	_, err := svc.sessionReloadExtensions(context.Background(), marshalReloadParams(t, sess.id))
	if err == nil {
		t.Fatal("failed build produced a nil error")
	}
	if sess.ctrl != old {
		t.Fatal("failed reload replaced the session controller")
	}
}

var errReloadBuildForTest = errors.New("build exploded")
