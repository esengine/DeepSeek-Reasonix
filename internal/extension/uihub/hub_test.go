package uihub

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/extension/protocol"
)

const testCredential = "api_key=sk-abcdef1234567890SECRETKEY"

// eventRecorder collects emitted events, safe for concurrent hub traffic.
type eventRecorder struct {
	mu     sync.Mutex
	events []event.Event
}

func (r *eventRecorder) emit(ev event.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, ev)
}

func (r *eventRecorder) all() []event.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]event.Event(nil), r.events...)
}

func newTestHub(rec *eventRecorder) *Hub {
	return New(Options{
		SessionID:  "sess-1",
		Generation: 7,
		Emit:       rec.emit,
		Warn:       func(string) {},
	})
}

func publishRaw(t *testing.T, h *Hub, pluginID string, p protocol.UIPublishParams) protocol.UIPublishResult {
	t.Helper()
	result, err := h.HandlerFor(pluginID).Publish(context.Background(), p)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	return result
}

func mustRaw(t *testing.T, v any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return raw
}

func TestPublishStatusEmitsRedactedStatusEvent(t *testing.T) {
	rec := &eventRecorder{}
	h := newTestHub(rec)
	progress := 0.5
	result := publishRaw(t, h, "alpha", protocol.UIPublishParams{
		SurfaceID: "s1", SessionID: "sess-1", Generation: 7, Kind: protocol.UISurfaceStatus,
		Payload: mustRaw(t, protocol.UIStatusPayload{
			Label: "working " + testCredential, Detail: "detail " + testCredential,
			Severity: protocol.UISeverityWarn, Progress: &progress,
		}),
	})
	if !result.Accepted {
		t.Fatal("status publish not accepted")
	}
	events := rec.all()
	if len(events) != 1 {
		t.Fatalf("emitted %d events, want 1", len(events))
	}
	ev := events[0]
	if ev.Kind != event.ExtensionStatus {
		t.Fatalf("event kind = %v, want ExtensionStatus", ev.Kind)
	}
	payload := ev.Extension
	if payload == nil || payload.Status == nil {
		t.Fatalf("extension payload = %+v", payload)
	}
	if payload.PluginID != "alpha" || payload.SurfaceID != "s1" || payload.SessionID != "sess-1" || payload.Generation != 7 {
		t.Fatalf("payload identity = %+v", payload)
	}
	if payload.Kind != event.ExtensionSurfaceStatus {
		t.Fatalf("payload kind = %q", payload.Kind)
	}
	if payload.Status.Severity != "warn" || payload.Status.Progress == nil || *payload.Status.Progress != 0.5 {
		t.Fatalf("status = %+v", payload.Status)
	}
	for _, s := range []string{payload.Status.Label, payload.Status.Detail} {
		if strings.Contains(s, "sk-abcdef") || !strings.Contains(s, "****") {
			t.Fatalf("status text not redacted: %q", s)
		}
	}
}

func TestPublishCardFormNotificationEmitSurfaceEvents(t *testing.T) {
	rec := &eventRecorder{}
	h := newTestHub(rec)
	handler := h.HandlerFor("alpha")

	cardProgress := 1.0
	card := protocol.UICardPayload{
		Title: "T " + testCredential, Markdown: "**m** " + testCredential, Text: "x",
		Fields:   []protocol.UIKeyValue{{Key: "k", Value: "v " + testCredential}},
		Progress: &cardProgress,
		Actions:  []protocol.UIActionRef{{ActionID: "act1", Label: "go " + testCredential}},
	}
	if result, err := handler.Publish(context.Background(), protocol.UIPublishParams{
		SurfaceID: "c1", SessionID: "sess-1", Generation: 7, Kind: protocol.UISurfaceCard,
		Payload: mustRaw(t, card),
	}); err != nil || !result.Accepted {
		t.Fatalf("card publish = %+v, %v", result, err)
	}

	form := protocol.UIFormPayload{
		Title: "f", Message: "m " + testCredential,
		Fields: []protocol.UIFormField{{
			Key: "field1", Label: "L " + testCredential, Kind: protocol.UIFieldSelect,
			Options: []string{"a " + testCredential, "b"}, Default: "d " + testCredential, Required: true,
		}},
	}
	if result, err := handler.Publish(context.Background(), protocol.UIPublishParams{
		SurfaceID: "f1", SessionID: "sess-1", Generation: 7, Kind: protocol.UISurfaceForm,
		Payload: mustRaw(t, form),
	}); err != nil || !result.Accepted {
		t.Fatalf("form publish = %+v, %v", result, err)
	}

	notification := protocol.UINotificationPayload{Title: "n " + testCredential, Body: "b " + testCredential, Severity: protocol.UISeverityError}
	if result, err := handler.Publish(context.Background(), protocol.UIPublishParams{
		SurfaceID: "n1", SessionID: "sess-1", Generation: 7, Kind: protocol.UISurfaceNotification,
		Payload: mustRaw(t, notification),
	}); err != nil || !result.Accepted {
		t.Fatalf("notification publish = %+v, %v", result, err)
	}

	events := rec.all()
	if len(events) != 3 {
		t.Fatalf("emitted %d events, want 3", len(events))
	}
	for _, ev := range events {
		if ev.Kind != event.ExtensionSurface {
			t.Fatalf("event kind = %v, want ExtensionSurface", ev.Kind)
		}
	}

	gotCard := events[0].Extension.Card
	if gotCard == nil || gotCard.Title == "" || len(gotCard.Fields) != 1 || len(gotCard.Actions) != 1 {
		t.Fatalf("card view = %+v", gotCard)
	}
	if gotCard.Actions[0].ActionID != "act1" {
		t.Fatalf("card action id = %q", gotCard.Actions[0].ActionID)
	}
	for _, s := range []string{gotCard.Title, gotCard.Markdown, gotCard.Fields[0].Value, gotCard.Actions[0].Label} {
		if strings.Contains(s, "sk-abcdef") {
			t.Fatalf("card text not redacted: %q", s)
		}
	}

	gotForm := events[1].Extension.Form
	if gotForm == nil || len(gotForm.Fields) != 1 {
		t.Fatalf("form view = %+v", gotForm)
	}
	field := gotForm.Fields[0]
	if field.Key != "field1" || field.Kind != "select" || !field.Required || len(field.Options) != 2 {
		t.Fatalf("form field = %+v", field)
	}
	if strings.Contains(gotForm.Message, "sk-abcdef") || strings.Contains(field.Label, "sk-abcdef") ||
		strings.Contains(field.Options[0], "sk-abcdef") || strings.Contains(field.Default.(string), "sk-abcdef") {
		t.Fatalf("form text not redacted: %+v", gotForm)
	}

	gotNotification := events[2].Extension.Notification
	if gotNotification == nil || gotNotification.Severity != "error" {
		t.Fatalf("notification view = %+v", gotNotification)
	}
	if strings.Contains(gotNotification.Title, "sk-abcdef") || strings.Contains(gotNotification.Body, "sk-abcdef") {
		t.Fatalf("notification text not redacted: %+v", gotNotification)
	}
}

func TestPublishStaleGenerationDropped(t *testing.T) {
	rec := &eventRecorder{}
	h := newTestHub(rec)
	result := publishRaw(t, h, "alpha", protocol.UIPublishParams{
		SurfaceID: "s1", SessionID: "sess-1", Generation: 6, Kind: protocol.UISurfaceStatus,
		Payload: mustRaw(t, protocol.UIStatusPayload{Label: "old"}),
	})
	if result.Accepted {
		t.Fatal("stale-generation publish accepted")
	}
	if len(rec.all()) != 0 {
		t.Fatalf("stale publish emitted events: %+v", rec.all())
	}
}

func TestPublishWrongSessionDropped(t *testing.T) {
	rec := &eventRecorder{}
	h := newTestHub(rec)
	result := publishRaw(t, h, "alpha", protocol.UIPublishParams{
		SurfaceID: "s1", SessionID: "sess-other", Generation: 7, Kind: protocol.UISurfaceStatus,
		Payload: mustRaw(t, protocol.UIStatusPayload{Label: "wrong session"}),
	})
	if result.Accepted {
		t.Fatal("wrong-session publish accepted")
	}
	if len(rec.all()) != 0 {
		t.Fatalf("wrong-session publish emitted events: %+v", rec.all())
	}
}

func TestPublishMalformedPayloadProtocolError(t *testing.T) {
	rec := &eventRecorder{}
	h := newTestHub(rec)
	_, err := h.HandlerFor("alpha").Publish(context.Background(), protocol.UIPublishParams{
		SurfaceID: "s1", SessionID: "sess-1", Generation: 7, Kind: protocol.UISurfaceStatus,
		Payload: json.RawMessage(`{"label":"x","bogus":true}`),
	})
	var protocolErr *protocol.ProtocolError
	if !errors.As(err, &protocolErr) || protocolErr.Reason != protocol.ErrInvalidParams {
		t.Fatalf("malformed payload error = %v, want invalid_params ProtocolError", err)
	}
}

func TestPublishUnknownClientRejected(t *testing.T) {
	rec := &eventRecorder{}
	h := newTestHub(rec)
	// The bare hub handler has no binding; neither does a fabricated binding
	// for a plugin the manager never announced.
	for _, handler := range []UIHandler{h, binding{pluginID: "ghost", hub: h}} {
		_, err := handler.Publish(context.Background(), protocol.UIPublishParams{
			SurfaceID: "s1", SessionID: "sess-1", Generation: 7, Kind: protocol.UISurfaceStatus,
			Payload: mustRaw(t, protocol.UIStatusPayload{Label: "x"}),
		})
		var protocolErr *protocol.ProtocolError
		if !errors.As(err, &protocolErr) {
			t.Fatalf("unknown client publish error = %v, want ProtocolError", err)
		}
	}
}

func TestPublishCrashedClientRejected(t *testing.T) {
	rec := &eventRecorder{}
	h := newTestHub(rec)
	handler := h.HandlerFor("alpha")
	h.ClientCrashed("alpha")
	_, err := handler.Publish(context.Background(), protocol.UIPublishParams{
		SurfaceID: "s1", SessionID: "sess-1", Generation: 7, Kind: protocol.UISurfaceStatus,
		Payload: mustRaw(t, protocol.UIStatusPayload{Label: "x"}),
	})
	var protocolErr *protocol.ProtocolError
	if !errors.As(err, &protocolErr) || protocolErr.Reason != protocol.ErrProviderInterrupted {
		t.Fatalf("crashed publish error = %v, want provider_interrupted", err)
	}
	// A fresh binding (replacement sidecar) is live again.
	if result := publishRaw(t, h, "alpha", protocol.UIPublishParams{
		SurfaceID: "s1", SessionID: "sess-1", Generation: 7, Kind: protocol.UISurfaceStatus,
		Payload: mustRaw(t, protocol.UIStatusPayload{Label: "revived"}),
	}); !result.Accepted {
		t.Fatal("publish after re-binding not accepted")
	}
}

func TestRequestKindsTranslateToAskChannel(t *testing.T) {
	tests := []struct {
		name     string
		kind     protocol.UIRequestKind
		form     protocol.UIFormPayload
		answers  []event.AskAnswer
		wantVals map[string]any
		checkQ   func(t *testing.T, q []event.AskQuestion)
	}{
		{
			name: "confirm",
			kind: protocol.UIRequestConfirm,
			form: protocol.UIFormPayload{Message: "proceed?", Fields: []protocol.UIFormField{}},
			answers: []event.AskAnswer{
				{QuestionID: "value", Selected: []string{"Yes"}},
			},
			wantVals: map[string]any{"value": true},
			checkQ: func(t *testing.T, qs []event.AskQuestion) {
				t.Helper()
				if len(qs) != 1 || len(qs[0].Options) != 2 || qs[0].Options[0].Label != "Yes" {
					t.Fatalf("confirm question = %+v", qs)
				}
			},
		},
		{
			name: "input",
			kind: protocol.UIRequestInput,
			form: protocol.UIFormPayload{Title: "T", Fields: []protocol.UIFormField{
				{Key: "name", Label: "Your name", Kind: protocol.UIFieldInput},
			}},
			answers: []event.AskAnswer{
				{QuestionID: "name", Selected: []string{"free text answer"}},
			},
			wantVals: map[string]any{"name": "free text answer"},
			checkQ: func(t *testing.T, qs []event.AskQuestion) {
				t.Helper()
				if len(qs) != 1 || len(qs[0].Options) != 0 || qs[0].Multi {
					t.Fatalf("input question = %+v", qs)
				}
			},
		},
		{
			name: "select",
			kind: protocol.UIRequestSelect,
			form: protocol.UIFormPayload{Fields: []protocol.UIFormField{
				{Key: "color", Label: "Pick", Kind: protocol.UIFieldSelect, Options: []string{"red", "blue"}},
			}},
			answers: []event.AskAnswer{
				{QuestionID: "color", Selected: []string{"blue"}},
			},
			wantVals: map[string]any{"color": "blue"},
			checkQ: func(t *testing.T, qs []event.AskQuestion) {
				t.Helper()
				if len(qs) != 1 || len(qs[0].Options) != 2 || qs[0].Multi {
					t.Fatalf("select question = %+v", qs)
				}
			},
		},
		{
			name: "multiselect",
			kind: protocol.UIRequestMultiselect,
			form: protocol.UIFormPayload{Fields: []protocol.UIFormField{
				{Key: "tags", Label: "Tags", Kind: protocol.UIFieldMultiselect, Options: []string{"a", "b", "c"}},
			}},
			answers: []event.AskAnswer{
				{QuestionID: "tags", Selected: []string{"a", "c"}},
			},
			wantVals: map[string]any{"tags": []string{"a", "c"}},
			checkQ: func(t *testing.T, qs []event.AskQuestion) {
				t.Helper()
				if len(qs) != 1 || !qs[0].Multi || len(qs[0].Options) != 3 {
					t.Fatalf("multiselect question = %+v", qs)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotQuestions []event.AskQuestion
			rec := &eventRecorder{}
			h := New(Options{
				SessionID: "sess-1", Generation: 7, Emit: rec.emit,
				Request: AskRequestFunc(func(_ context.Context, qs []event.AskQuestion) ([]event.AskAnswer, error) {
					gotQuestions = append([]event.AskQuestion(nil), qs...)
					return tt.answers, nil
				}),
			})
			result, err := h.HandlerFor("alpha").Request(context.Background(), protocol.UIRequestParams{
				SurfaceID: "r1", SessionID: "sess-1", Generation: 7, Kind: tt.kind,
				Payload: mustRaw(t, tt.form),
			})
			if err != nil {
				t.Fatalf("Request: %v", err)
			}
			if result.Cancelled {
				t.Fatal("request reported cancelled")
			}
			if len(result.Values) != len(tt.wantVals) {
				t.Fatalf("values = %+v, want %+v", result.Values, tt.wantVals)
			}
			for key, want := range tt.wantVals {
				got := result.Values[key]
				switch wantVal := want.(type) {
				case []string:
					gotSlice, ok := got.([]string)
					if !ok || len(gotSlice) != len(wantVal) {
						t.Fatalf("values[%q] = %#v, want %#v", key, got, want)
					}
					for i := range wantVal {
						if gotSlice[i] != wantVal[i] {
							t.Fatalf("values[%q] = %#v, want %#v", key, got, want)
						}
					}
				default:
					if got != want {
						t.Fatalf("values[%q] = %#v, want %#v", key, got, want)
					}
				}
			}
			tt.checkQ(t, gotQuestions)
		})
	}
}

func TestRequestCancelledWhenDismissed(t *testing.T) {
	h := New(Options{
		SessionID: "sess-1", Generation: 7,
		Request: AskRequestFunc(func(context.Context, []event.AskQuestion) ([]event.AskAnswer, error) {
			return nil, nil // the controller's skip path: no selections at all
		}),
	})
	result, err := h.HandlerFor("alpha").Request(context.Background(), protocol.UIRequestParams{
		SurfaceID: "r1", SessionID: "sess-1", Generation: 7, Kind: protocol.UIRequestConfirm,
		Payload: mustRaw(t, protocol.UIFormPayload{Message: "proceed?", Fields: []protocol.UIFormField{}}),
	})
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if !result.Cancelled {
		t.Fatalf("dismissed request = %+v, want cancelled", result)
	}
}

func TestRequestRedactsPromptText(t *testing.T) {
	var gotReq HubRequest
	h := New(Options{
		SessionID: "sess-1", Generation: 7,
		Request: func(_ context.Context, req HubRequest) (map[string]any, bool, error) {
			gotReq = req
			return map[string]any{"field1": "x"}, false, nil
		},
	})
	_, err := h.HandlerFor("alpha").Request(context.Background(), protocol.UIRequestParams{
		SurfaceID: "r1", SessionID: "sess-1", Generation: 7, Kind: protocol.UIRequestSelect,
		Payload: mustRaw(t, protocol.UIFormPayload{
			Title: "t " + testCredential, Message: "m " + testCredential,
			Fields: []protocol.UIFormField{{Key: "field1", Label: "L " + testCredential, Kind: protocol.UIFieldSelect, Options: []string{"o " + testCredential}}},
		}),
	})
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	for _, s := range []string{gotReq.Title, gotReq.Message, gotReq.Fields[0].Label, gotReq.Fields[0].Options[0]} {
		if strings.Contains(s, "sk-abcdef") {
			t.Fatalf("request prompt text not redacted: %q", s)
		}
	}
}

func TestRequestStaleGenerationAnsweredCancelled(t *testing.T) {
	called := false
	h := New(Options{
		SessionID: "sess-1", Generation: 7,
		Request: func(context.Context, HubRequest) (map[string]any, bool, error) {
			called = true
			return nil, false, nil
		},
	})
	result, err := h.HandlerFor("alpha").Request(context.Background(), protocol.UIRequestParams{
		SurfaceID: "r1", SessionID: "sess-1", Generation: 6, Kind: protocol.UIRequestConfirm,
		Payload: mustRaw(t, protocol.UIFormPayload{Message: "proceed?", Fields: []protocol.UIFormField{}}),
	})
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if !result.Cancelled {
		t.Fatalf("stale request = %+v, want cancelled", result)
	}
	if called {
		t.Fatal("stale request reached the Ask channel")
	}
}

func TestRebindDropsOldGeneration(t *testing.T) {
	rec := &eventRecorder{}
	h := newTestHub(rec)
	handler := h.HandlerFor("alpha")
	// The reload re-binds the hub; the old generation's late publications must
	// never overwrite the new state.
	h.BindGeneration("sess-2", 8)
	stale := func() protocol.UIPublishResult {
		result, err := handler.Publish(context.Background(), protocol.UIPublishParams{
			SurfaceID: "s1", SessionID: "sess-1", Generation: 7, Kind: protocol.UISurfaceStatus,
			Payload: mustRaw(t, protocol.UIStatusPayload{Label: "old"}),
		})
		if err != nil {
			t.Fatalf("Publish: %v", err)
		}
		return result
	}
	if result := stale(); result.Accepted {
		t.Fatal("old-generation publish accepted after rebind")
	}
	result, err := handler.Publish(context.Background(), protocol.UIPublishParams{
		SurfaceID: "s1", SessionID: "sess-2", Generation: 8, Kind: protocol.UISurfaceStatus,
		Payload: mustRaw(t, protocol.UIStatusPayload{Label: "new"}),
	})
	if err != nil || !result.Accepted {
		t.Fatalf("new-generation publish = %+v, %v", result, err)
	}
	if len(rec.all()) != 1 {
		t.Fatalf("emitted %d events, want exactly the new one", len(rec.all()))
	}
}

// fakeActionClient records UIAction/UISubmit calls for the action tests.
type fakeActionClient struct {
	mu           sync.Mutex
	actionParams []protocol.UIActionParams
	actionResult protocol.UIActionResult
	actionErr    error
	submitParams []protocol.UISubmitParams
	submitResult protocol.UISubmitResult
	submitErr    error
}

func (f *fakeActionClient) UIAction(_ context.Context, p protocol.UIActionParams) (protocol.UIActionResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.actionParams = append(f.actionParams, p)
	return f.actionResult, f.actionErr
}

func (f *fakeActionClient) UISubmit(_ context.Context, p protocol.UISubmitParams) (protocol.UISubmitResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.submitParams = append(f.submitParams, p)
	return f.submitResult, f.submitErr
}

func TestRegisterActionsRejectsInvalidIDs(t *testing.T) {
	h := newTestHub(&eventRecorder{})
	for _, id := range []string{"", "Upper", "has space", "under_score", "slash/"} {
		if err := h.RegisterActions("alpha", []protocol.UIActionDecl{{ActionID: id}}); err == nil {
			t.Fatalf("RegisterActions accepted invalid id %q", id)
		}
	}
	if err := h.RegisterActions("alpha", []protocol.UIActionDecl{{ActionID: "ok-action1"}}); err != nil {
		t.Fatalf("RegisterActions rejected a valid id: %v", err)
	}
}

func TestActionsEnumerateWithSlashNames(t *testing.T) {
	h := newTestHub(&eventRecorder{})
	if err := h.RegisterActions("beta", []protocol.UIActionDecl{{ActionID: "zap", Label: "Zap " + testCredential}}); err != nil {
		t.Fatal(err)
	}
	if err := h.RegisterActions("alpha", []protocol.UIActionDecl{{ActionID: "act1", Label: "Act"}}); err != nil {
		t.Fatal(err)
	}
	actions := h.Actions()
	if len(actions) != 2 {
		t.Fatalf("Actions = %+v", actions)
	}
	// Sorted by slash name: /alpha:act1 before /beta:zap.
	if actions[0].Slash != "/alpha:act1" || actions[1].Slash != "/beta:zap" {
		t.Fatalf("slash names = %+v", actions)
	}
	if actions[1].Label != "" && strings.Contains(actions[1].Label, "sk-abcdef") {
		t.Fatalf("action label not redacted: %q", actions[1].Label)
	}
}

func TestSlashNameRoundTrip(t *testing.T) {
	if got := SlashName("alpha", "act1"); got != "/alpha:act1" {
		t.Fatalf("SlashName = %q", got)
	}
	plugin, action, ok := ParseSlashName("/alpha:act1")
	if !ok || plugin != "alpha" || action != "act1" {
		t.Fatalf("ParseSlashName = %q, %q, %v", plugin, action, ok)
	}
	for _, bad := range []string{"alpha:act1", "/alpha", "/:act1", "/alpha:Bad Id", ""} {
		if _, _, ok := ParseSlashName(bad); ok {
			t.Fatalf("ParseSlashName accepted %q", bad)
		}
	}
}

func TestInvokeActionRoutesToOwningClient(t *testing.T) {
	fake := &fakeActionClient{actionResult: protocol.UIActionResult{Accepted: true, Message: "done " + testCredential}}
	h := New(Options{
		SessionID: "sess-1", Generation: 7,
		Resolve: func(pluginID string) ActionClient {
			if pluginID == "alpha" {
				return fake
			}
			return nil
		},
	})
	if err := h.RegisterActions("alpha", []protocol.UIActionDecl{{ActionID: "act1"}}); err != nil {
		t.Fatal(err)
	}
	result, err := h.InvokeAction(context.Background(), "alpha", "act1", "sess-1", map[string]string{"k": "v"})
	if err != nil {
		t.Fatalf("InvokeAction: %v", err)
	}
	if !result.Accepted {
		t.Fatal("action not accepted")
	}
	if strings.Contains(result.Message, "sk-abcdef") {
		t.Fatalf("result message not redacted: %q", result.Message)
	}
	if len(fake.actionParams) != 1 {
		t.Fatalf("client action calls = %+v", fake.actionParams)
	}
	call := fake.actionParams[0]
	if call.ActionID != "act1" || call.SessionID != "sess-1" || call.Generation != 7 || call.Args["k"] != "v" {
		t.Fatalf("action params = %+v", call)
	}
}

func TestInvokeActionRejectsUndeclaredUnknownAndStale(t *testing.T) {
	fake := &fakeActionClient{actionResult: protocol.UIActionResult{Accepted: true}}
	h := New(Options{
		SessionID: "sess-1", Generation: 7,
		Resolve: func(string) ActionClient { return fake },
	})
	if err := h.RegisterActions("alpha", []protocol.UIActionDecl{{ActionID: "act1"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.InvokeAction(context.Background(), "alpha", "nope", "sess-1", nil); err == nil {
		t.Fatal("InvokeAction accepted an undeclared action")
	}
	if _, err := h.InvokeAction(context.Background(), "ghost", "act1", "sess-1", nil); err == nil {
		t.Fatal("InvokeAction accepted an unknown plugin")
	}
	if _, err := h.InvokeAction(context.Background(), "alpha", "act1", "sess-old", nil); err == nil {
		t.Fatal("InvokeAction accepted a stale session")
	}
	if _, err := h.InvokeAction(context.Background(), "alpha", "Bad ID", "sess-1", nil); err == nil {
		t.Fatal("InvokeAction accepted an invalid action id")
	}
	if len(fake.actionParams) != 0 {
		t.Fatalf("rejected invocations reached the client: %+v", fake.actionParams)
	}
}

func TestSubmitRoutesFormValues(t *testing.T) {
	fake := &fakeActionClient{submitResult: protocol.UISubmitResult{Accepted: true}}
	h := New(Options{
		SessionID: "sess-1", Generation: 7,
		Resolve: func(string) ActionClient { return fake },
	})
	h.HandlerFor("alpha")
	result, err := h.Submit(context.Background(), "alpha", "f1", "sess-1", map[string]any{"name": "x"})
	if err != nil || !result.Accepted {
		t.Fatalf("Submit = %+v, %v", result, err)
	}
	if len(fake.submitParams) != 1 {
		t.Fatalf("client submit calls = %+v", fake.submitParams)
	}
	call := fake.submitParams[0]
	if call.SurfaceID != "f1" || call.SessionID != "sess-1" || call.Generation != 7 || call.Values["name"] != "x" {
		t.Fatalf("submit params = %+v", call)
	}
}

func TestHubConcurrentUse(t *testing.T) {
	rec := &eventRecorder{}
	fake := &fakeActionClient{actionResult: protocol.UIActionResult{Accepted: true}, submitResult: protocol.UISubmitResult{Accepted: true}}
	h := New(Options{
		SessionID: "sess-1", Generation: 7, Emit: rec.emit,
		Resolve: func(string) ActionClient { return fake },
		Request: AskRequestFunc(func(context.Context, []event.AskQuestion) ([]event.AskAnswer, error) {
			return []event.AskAnswer{{QuestionID: "value", Selected: []string{"Yes"}}}, nil
		}),
	})
	if err := h.RegisterActions("alpha", []protocol.UIActionDecl{{ActionID: "act1"}}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			handler := h.HandlerFor("alpha")
			_, _ = handler.Publish(context.Background(), protocol.UIPublishParams{
				SurfaceID: "s", SessionID: "sess-1", Generation: 7, Kind: protocol.UISurfaceStatus,
				Payload: mustRaw(t, protocol.UIStatusPayload{Label: "x"}),
			})
			_, _ = handler.Request(context.Background(), protocol.UIRequestParams{
				SurfaceID: "r", SessionID: "sess-1", Generation: 7, Kind: protocol.UIRequestConfirm,
				Payload: mustRaw(t, protocol.UIFormPayload{Message: "m"}),
			})
			_, _ = h.InvokeAction(context.Background(), "alpha", "act1", "sess-1", nil)
			_, _ = h.Submit(context.Background(), "alpha", "f", "sess-1", nil)
			_ = h.Actions()
			h.BindGeneration("sess-1", 7)
			h.ClientCrashed("other")
			h.SetResolver(func(string) ActionClient { return fake })
		}(i)
	}
	wg.Wait()
}
