package boot

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/event"
	"reasonix/internal/extension/protocol"
)

// uiSinkRecorder collects controller events for the stage-8a boot tests.
type uiSinkRecorder struct {
	mu     sync.Mutex
	events []event.Event
}

func (r *uiSinkRecorder) emit(ev event.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, ev)
}

func (r *uiSinkRecorder) hasExtensionStatus() (event.ExtensionSurfacePayload, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, ev := range r.events {
		if ev.Kind == event.ExtensionStatus && ev.Extension != nil {
			return *ev.Extension, true
		}
	}
	return event.ExtensionSurfacePayload{}, false
}

// bootWithFakeUIPlugin mirrors bootWithFakePlugin but lets the test inject
// the sink the build's controller emits to.
func bootWithFakeUIPlugin(t *testing.T, name string, runtime map[string]any, sink event.Sink) *BuildResult {
	t.Helper()
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	writeRuntimeFixture(t, dir)
	installBootFakePlugin(t, config.ReasonixHomeDir(), name, runtime)
	res, err := BuildRuntime(context.Background(), Options{Sink: sink})
	if err != nil {
		t.Fatalf("BuildRuntime: %v", err)
	}
	t.Cleanup(res.Controller.Close)
	return res
}

// TestBootExtensionUIHubWired proves the stage-8a end-to-end path: the hub is
// installed as the sidecar's host/ui/* handler, a credential-bearing status
// publish reaches the frontend sink redacted, handshake-declared actions land
// in the registry, and the controller's Capabilities port enumerates and
// invokes them.
func TestBootExtensionUIHubWired(t *testing.T) {
	rec := &uiSinkRecorder{}
	res := bootWithFakeUIPlugin(t, "uibootplugin", map[string]any{
		"capabilities": []string{"ui"},
		"env": map[string]string{
			bootFakeEnvInitResult: `{"protocolVersion":"2","name":"uibootplugin","version":"1.0.0","stateSchemaVersion":0,` +
				`"uiActions":[{"actionId":"act1","label":"Act one"}]}`,
			bootFakeEnvUIPublish: "1",
		},
	}, event.FuncSink(rec.emit))

	if res.ExtensionUI == nil {
		t.Fatal("BuildRuntime returned no extension UI hub")
	}

	// The fake sidecar publishes one status surface right after the
	// handshake; the hub must gate it (bound session + generation), redact
	// it, and emit it to the controller sink.
	deadline := time.Now().Add(15 * time.Second)
	for {
		if payload, ok := rec.hasExtensionStatus(); ok {
			if payload.PluginID != "uibootplugin" || payload.SurfaceID != "boot-status" {
				t.Fatalf("extension status payload = %+v", payload)
			}
			if payload.Status == nil || !strings.Contains(payload.Status.Label, "boot fake ready") {
				t.Fatalf("status view = %+v", payload.Status)
			}
			if strings.Contains(payload.Status.Label, "sk-abcdef") {
				t.Fatalf("status label not redacted: %q", payload.Status.Label)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no ExtensionStatus event reached the controller sink")
		}
		time.Sleep(10 * time.Millisecond)
	}

	// The handshake-declared action is registered under its slash name.
	actions := res.ExtensionUI.Actions()
	if len(actions) != 1 || actions[0].Slash != "/uibootplugin:act1" || actions[0].Label != "Act one" {
		t.Fatalf("hub actions = %+v", actions)
	}
	portActions := res.Controller.ExtensionActions()
	if len(portActions) != 1 || portActions[0].Slash != "/uibootplugin:act1" {
		t.Fatalf("controller ExtensionActions = %+v", portActions)
	}

	// Invocation routes through the hub to the fake sidecar over the wire.
	message, err := res.Controller.InvokeExtensionAction(context.Background(), "/uibootplugin:act1", nil)
	if err != nil {
		t.Fatalf("InvokeExtensionAction: %v", err)
	}
	if message != "boot fake action ran" {
		t.Fatalf("action message = %q", message)
	}
	if err := res.Controller.SubmitExtensionForm(context.Background(), "uibootplugin", "boot-status", map[string]any{"k": "v"}); err != nil {
		t.Fatalf("SubmitExtensionForm: %v", err)
	}
}

// TestBootExtensionUIHubNilWithoutPlugins pins the zero-change contract: with
// no v1 runtime packages there is no hub, no controller install, and the
// Capabilities port degrades to empty/error.
func TestBootExtensionUIHubNilWithoutPlugins(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	writeRuntimeFixture(t, dir)
	res, err := BuildRuntime(context.Background(), Options{})
	if err != nil {
		t.Fatalf("BuildRuntime: %v", err)
	}
	t.Cleanup(res.Controller.Close)
	if res.ExtensionUI != nil {
		t.Fatal("build without extension packages returned a UI hub")
	}
	if got := res.Controller.ExtensionActions(); len(got) != 0 {
		t.Fatalf("ExtensionActions = %+v, want empty", got)
	}
	if _, err := res.Controller.InvokeExtensionAction(context.Background(), "/alpha:act1", nil); err == nil {
		t.Fatal("InvokeExtensionAction succeeded without a hub")
	}
}

// TestRebuildRebindsExtensionUIHub proves the reload contract: a rebuild
// starts fresh sidecars on a new generation and hands the controller a hub
// bound to that generation, so stale publications from the old generation
// can never land.
func TestRebuildRebindsExtensionUIHub(t *testing.T) {
	old := bootWithFakePlugin(t, "rebuilduiplugin", map[string]any{
		"capabilities": []string{"ui"},
		"env": map[string]string{
			bootFakeEnvInitResult: `{"protocolVersion":"2","name":"rebuilduiplugin","version":"1.0.0","stateSchemaVersion":0,` +
				`"uiActions":[{"actionId":"act1"}]}`,
		},
	})
	if old.ExtensionUI == nil {
		t.Fatal("old build has no UI hub")
	}
	oldGeneration := old.ExtensionUI.Generation()

	newRes, err := Rebuild(context.Background(), old.Controller, Options{})
	if err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	if newRes.ExtensionUI == nil {
		t.Fatal("rebuilt runtime has no UI hub")
	}
	if newRes.ExtensionUI.Generation() <= oldGeneration {
		t.Fatalf("hub generation did not increase: old %d, new %d", oldGeneration, newRes.ExtensionUI.Generation())
	}
	if got := newRes.ExtensionUI.Actions(); len(got) != 1 || got[0].Slash != "/rebuilduiplugin:act1" {
		t.Fatalf("rebuilt hub actions = %+v", got)
	}
	// The new hub is bound to the new generation: a late publication carrying
	// the old generation is dropped (debug), never overwriting the new state.
	raw, err := json.Marshal(map[string]any{"label": "late"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	result, err := newRes.ExtensionUI.HandlerFor("rebuilduiplugin").Publish(context.Background(), protocol.UIPublishParams{
		SurfaceID: "s1", SessionID: newRes.ExtensionUI.SessionID(), Generation: oldGeneration,
		Kind: protocol.UISurfaceStatus, Payload: raw,
	})
	if err != nil {
		t.Fatalf("old-generation publish errored (want a silent drop): %v", err)
	}
	if result.Accepted {
		t.Fatal("old-generation publish accepted by the re-bound hub")
	}
}
