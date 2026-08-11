package control

import (
	"context"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/extension/protocol"
	"reasonix/internal/extension/uihub"
)

// fakeExtensionClient is the uihub.ActionClient double for the controller
// port tests.
type fakeExtensionClient struct {
	actionResult protocol.UIActionResult
	submitResult protocol.UISubmitResult
	gotAction    *protocol.UIActionParams
	gotSubmit    *protocol.UISubmitParams
}

func (f *fakeExtensionClient) UIAction(_ context.Context, p protocol.UIActionParams) (protocol.UIActionResult, error) {
	f.gotAction = &p
	return f.actionResult, nil
}

func (f *fakeExtensionClient) UISubmit(_ context.Context, p protocol.UISubmitParams) (protocol.UISubmitResult, error) {
	f.gotSubmit = &p
	return f.submitResult, nil
}

func newExtensionUIController(t *testing.T, client uihub.ActionClient) (*Controller, *uihub.Hub) {
	t.Helper()
	c := New(Options{Sink: event.Discard})
	hub := uihub.New(uihub.Options{
		SessionID: "sess-1", Generation: 3,
		Resolve: func(string) uihub.ActionClient { return client },
	})
	if err := hub.RegisterActions("alpha", []protocol.UIActionDecl{{ActionID: "act1", Label: "Act one"}}); err != nil {
		t.Fatalf("RegisterActions: %v", err)
	}
	c.SetExtensionUI(hub)
	return c, hub
}

func TestExtensionUIPortNilHub(t *testing.T) {
	c := New(Options{Sink: event.Discard})
	if got := c.ExtensionActions(); len(got) != 0 {
		t.Fatalf("ExtensionActions = %+v, want empty without a hub", got)
	}
	if _, err := c.InvokeExtensionAction(context.Background(), "/alpha:act1", nil); err == nil {
		t.Fatal("InvokeExtensionAction succeeded without a hub")
	}
	if err := c.SubmitExtensionForm(context.Background(), "alpha", "f1", nil); err == nil {
		t.Fatal("SubmitExtensionForm succeeded without a hub")
	}
}

func TestExtensionUIPortEnumeratesAndInvokes(t *testing.T) {
	client := &fakeExtensionClient{
		actionResult: protocol.UIActionResult{Accepted: true, Message: "done"},
		submitResult: protocol.UISubmitResult{Accepted: true},
	}
	c, hub := newExtensionUIController(t, client)

	actions := c.ExtensionActions()
	if len(actions) != 1 {
		t.Fatalf("ExtensionActions = %+v", actions)
	}
	if actions[0].Slash != "/alpha:act1" || actions[0].Label != "Act one" || actions[0].PluginID != "alpha" {
		t.Fatalf("action view = %+v", actions[0])
	}

	message, err := c.InvokeExtensionAction(context.Background(), "/alpha:act1", map[string]string{"k": "v"})
	if err != nil {
		t.Fatalf("InvokeExtensionAction: %v", err)
	}
	if message != "done" {
		t.Fatalf("message = %q", message)
	}
	if client.gotAction == nil || client.gotAction.ActionID != "act1" ||
		client.gotAction.SessionID != hub.SessionID() || client.gotAction.Generation != 3 || client.gotAction.Args["k"] != "v" {
		t.Fatalf("action params = %+v", client.gotAction)
	}

	if _, err := c.InvokeExtensionAction(context.Background(), "not-a-slash-name", nil); err == nil {
		t.Fatal("InvokeExtensionAction accepted a malformed name")
	}
	if _, err := c.InvokeExtensionAction(context.Background(), "/alpha:undeclared", nil); err == nil {
		t.Fatal("InvokeExtensionAction accepted an undeclared action")
	}

	if err := c.SubmitExtensionForm(context.Background(), "alpha", "f1", map[string]any{"name": "x"}); err != nil {
		t.Fatalf("SubmitExtensionForm: %v", err)
	}
	if client.gotSubmit == nil || client.gotSubmit.SurfaceID != "f1" || client.gotSubmit.Values["name"] != "x" {
		t.Fatalf("submit params = %+v", client.gotSubmit)
	}
}

func TestSetExtensionUIFirstInstallWins(t *testing.T) {
	client := &fakeExtensionClient{}
	c, hub := newExtensionUIController(t, client)
	replacement := uihub.New(uihub.Options{SessionID: "sess-2", Generation: 9})
	c.SetExtensionUI(replacement)
	if got := c.ExtensionActions(); len(got) != 1 {
		t.Fatalf("a second SetExtensionUI swapped the hub: actions = %+v", got)
	}
	_ = hub
}

func TestEmitExtensionEventReachesSink(t *testing.T) {
	var got []event.Event
	c := New(Options{Sink: event.FuncSink(func(e event.Event) { got = append(got, e) })})
	c.EmitExtensionEvent(event.Event{
		Kind: event.ExtensionStatus,
		Extension: &event.ExtensionSurfacePayload{
			PluginID: "alpha", SurfaceID: "s1", Kind: event.ExtensionSurfaceStatus,
			Status: &event.ExtensionStatusView{Label: "working"},
		},
	})
	if len(got) != 1 || got[0].Kind != event.ExtensionStatus || got[0].Extension.Status.Label != "working" {
		t.Fatalf("emitted events = %+v", got)
	}
}

// TestInvokeExtensionActionRedactsResultMessage proves the sidecar-sourced
// message reaches the frontend credential-free.
func TestInvokeExtensionActionRedactsResultMessage(t *testing.T) {
	client := &fakeExtensionClient{actionResult: protocol.UIActionResult{
		Accepted: true, Message: "token api_key=sk-abcdef1234567890SECRETKEY stored",
	}}
	c, _ := newExtensionUIController(t, client)
	message, err := c.InvokeExtensionAction(context.Background(), "/alpha:act1", nil)
	if err != nil {
		t.Fatalf("InvokeExtensionAction: %v", err)
	}
	if strings.Contains(message, "sk-abcdef") {
		t.Fatalf("message not redacted: %q", message)
	}
}
