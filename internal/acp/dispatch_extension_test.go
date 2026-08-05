package acp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/extension/protocol"
	"reasonix/internal/extension/uihub"
)

func extStatusEvent(severity string) event.Event {
	return event.Event{
		Kind: event.ExtensionStatus,
		Extension: &event.ExtensionSurfacePayload{
			PluginID: "alpha", SurfaceID: "s1", Kind: event.ExtensionSurfaceStatus,
			Status: &event.ExtensionStatusView{Label: "building", Detail: "3 of 9", Severity: severity},
		},
	}
}

func extCardEvent() event.Event {
	return event.Event{
		Kind: event.ExtensionSurface,
		Extension: &event.ExtensionSurfacePayload{
			PluginID: "alpha", SurfaceID: "c1", Kind: event.ExtensionSurfaceCard,
			Card: &event.ExtensionCardView{
				Title:  "CI status",
				Text:   "all green",
				Fields: []event.ExtensionKeyValue{{Key: "branch", Value: "main"}},
			},
		},
	}
}

// chunkText extracts the text of an agent_message_chunk update map.
func chunkText(t *testing.T, u map[string]any) string {
	t.Helper()
	if u["sessionUpdate"] != "agent_message_chunk" {
		t.Fatalf("sessionUpdate = %v, want agent_message_chunk", u["sessionUpdate"])
	}
	content, _ := u["content"].(map[string]any)
	text, _ := content["text"].(string)
	return text
}

func TestUpdateSinkExtensionUnsupportedClientGetsTextOnly(t *testing.T) {
	fn := &fakeNotifier{}
	sink := newUpdateSink(fn, "sess-1") // extensionSurface unbound → unsupported

	sink.Emit(extCardEvent())
	if len(fn.notifs) != 1 {
		t.Fatalf("emitted %d notifications, want 1 (text fallback only)", len(fn.notifs))
	}
	text := chunkText(t, fn.updateMap(t, 0))
	for _, want := range []string{"CI status", "all green", "branch: main"} {
		if !strings.Contains(text, want) {
			t.Errorf("card fallback missing %q: %q", want, text)
		}
	}
	if strings.Contains(text, "[warning]") {
		t.Errorf("severity-less card must not carry the warning prefix: %q", text)
	}
}

func TestUpdateSinkExtensionStatusAndSeverityPrefixes(t *testing.T) {
	fn := &fakeNotifier{}
	sink := newUpdateSink(fn, "sess-1")

	sink.Emit(extStatusEvent("info"))
	sink.Emit(extStatusEvent("warn"))
	sink.Emit(extStatusEvent("error"))

	if len(fn.notifs) != 3 {
		t.Fatalf("emitted %d notifications, want 3", len(fn.notifs))
	}
	info := chunkText(t, fn.updateMap(t, 0))
	if !strings.Contains(info, "[alpha] building: 3 of 9") || strings.Contains(info, "[warning]") {
		t.Errorf("info status = %q", info)
	}
	for _, i := range []int{1, 2} {
		if text := chunkText(t, fn.updateMap(t, i)); !strings.Contains(text, "[warning] [alpha] building") {
			t.Errorf("notif %d = %q, want [warning] prefix", i, text)
		}
	}
}

func TestUpdateSinkExtensionNotification(t *testing.T) {
	fn := &fakeNotifier{}
	sink := newUpdateSink(fn, "sess-1")
	sink.Emit(event.Event{
		Kind: event.ExtensionSurface,
		Extension: &event.ExtensionSurfacePayload{
			PluginID: "alpha", SurfaceID: "n1", Kind: event.ExtensionSurfaceNotification,
			Notification: &event.ExtensionNotificationView{Title: "Deploy done", Body: "v2 live", Severity: "warn"},
		},
	})
	text := chunkText(t, fn.updateMap(t, 0))
	if !strings.Contains(text, "[warning] Deploy done") || !strings.Contains(text, "v2 live") {
		t.Fatalf("notification fallback = %q", text)
	}
}

func TestUpdateSinkExtensionSupportedClientGetsMetaAndText(t *testing.T) {
	fn := &fakeNotifier{}
	sink := newUpdateSink(fn, "sess-1")
	sink.bindExtensionSurface(true)

	sink.Emit(extCardEvent())
	if len(fn.notifs) != 2 {
		t.Fatalf("emitted %d notifications, want 2 (vendor _meta + text fallback)", len(fn.notifs))
	}

	u := fn.updateMap(t, 0)
	if u["sessionUpdate"] != extensionSurfaceUpdateKind {
		t.Fatalf("structured update sessionUpdate = %v, want %q", u["sessionUpdate"], extensionSurfaceUpdateKind)
	}
	meta, _ := u["_meta"].(map[string]any)
	vendor, _ := meta["reasonix.io"].(map[string]any)
	surface, _ := vendor["extensionSurface"].(map[string]any)
	if surface == nil {
		t.Fatalf("structured update missing _meta.reasonix.io.extensionSurface: %v", u)
	}
	if surface["kind"] != "card" || surface["pluginId"] != "alpha" || surface["surfaceId"] != "c1" {
		t.Errorf("surface DTO = %v", surface)
	}
	card, _ := surface["card"].(map[string]any)
	if card["title"] != "CI status" || card["text"] != "all green" {
		t.Errorf("card DTO = %v", card)
	}

	// Belt and suspenders: the text fallback still rides behind it.
	text := chunkText(t, fn.updateMap(t, 1))
	if !strings.Contains(text, "CI status") {
		t.Errorf("text fallback = %q", text)
	}
}

func TestUpdateSinkExtensionFormFlattensToAnnouncement(t *testing.T) {
	// Published form surfaces flatten to title + message; the blocking prompt
	// side never reaches this sink — it rides AskRequest →
	// session/request_permission (covered by
	// TestUpdateSinkAskRequestUsesPermissionChoices).
	fn := &fakeNotifier{}
	sink := newUpdateSink(fn, "sess-1")
	sink.Emit(event.Event{
		Kind: event.ExtensionSurface,
		Extension: &event.ExtensionSurfacePayload{
			PluginID: "alpha", SurfaceID: "f1", Kind: event.ExtensionSurfaceForm,
			Form: &event.ExtensionFormView{Title: "Setup", Message: "pick options"},
		},
	})
	if len(fn.notifs) != 1 {
		t.Fatalf("emitted %d notifications, want 1", len(fn.notifs))
	}
	text := chunkText(t, fn.updateMap(t, 0))
	if !strings.Contains(text, "Setup") || !strings.Contains(text, "pick options") {
		t.Fatalf("form fallback = %q", text)
	}
}

func TestUpdateSinkExtensionNilPayloadDropped(t *testing.T) {
	fn := &fakeNotifier{}
	sink := newUpdateSink(fn, "sess-1")
	sink.bindExtensionSurface(true)
	sink.Emit(event.Event{Kind: event.ExtensionSurface})
	sink.Emit(event.Event{Kind: event.ExtensionStatus})
	if len(fn.notifs) != 0 {
		t.Fatalf("nil payloads emitted %d notifications, want 0", len(fn.notifs))
	}
}

func TestInitializeAdvertisesExtensionSurface(t *testing.T) {
	svc := &service{}
	result, err := svc.initialize(context.Background(), nil)
	if err != nil {
		t.Fatalf("initialize: %v", err)
	}
	ir, ok := result.(InitializeResult)
	if !ok {
		t.Fatalf("initialize returned %T", result)
	}
	vendor, ok := ir.AgentCapabilities.Meta["reasonix.io"].(ReasonixExtensionCapabilities)
	if !ok {
		t.Fatalf("_meta[reasonix.io] = %T", ir.AgentCapabilities.Meta["reasonix.io"])
	}
	if vendor.ExtensionSurface == nil || !vendor.ExtensionSurface.Supported ||
		vendor.ExtensionSurface.SchemaVersion != reasonixExtensionSurfaceSchemaVersion {
		t.Fatalf("extensionSurface capability = %+v", vendor.ExtensionSurface)
	}

	// The wire shape keeps the vendor namespace and camelCase keys.
	raw, err := json.Marshal(ir)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded struct {
		AgentCapabilities struct {
			Meta map[string]struct {
				ExtensionSurface *struct {
					Supported     bool `json:"supported"`
					SchemaVersion int  `json:"schemaVersion"`
				} `json:"extensionSurface"`
			} `json:"_meta"`
		} `json:"agentCapabilities"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	got := decoded.AgentCapabilities.Meta["reasonix.io"].ExtensionSurface
	if got == nil || !got.Supported || got.SchemaVersion != reasonixExtensionSurfaceSchemaVersion {
		t.Fatalf("wire extensionSurface = %+v", got)
	}
}

func TestClientExtensionSurfaceSupportedParsing(t *testing.T) {
	tests := []struct {
		name string
		meta map[string]any
		want bool
	}{
		{"absent", nil, false},
		{"vendor block absent", map[string]any{"other": true}, false},
		{"capability absent", map[string]any{"reasonix.io": map[string]any{}}, false},
		{"supported", map[string]any{"reasonix.io": map[string]any{
			"extensionSurface": map[string]any{"supported": true, "schemaVersion": 1},
		}}, true},
		{"explicit false", map[string]any{"reasonix.io": map[string]any{
			"extensionSurface": map[string]any{"supported": false},
		}}, false},
		{"malformed vendor", map[string]any{"reasonix.io": "nope"}, false},
		{"malformed capability", map[string]any{"reasonix.io": map[string]any{
			"extensionSurface": "nope",
		}}, false},
		{"malformed flag", map[string]any{"reasonix.io": map[string]any{
			"extensionSurface": map[string]any{"supported": "yes"},
		}}, false},
	}
	for _, tt := range tests {
		if got := clientExtensionSurfaceSupported(ClientCapabilities{Meta: tt.meta}); got != tt.want {
			t.Errorf("%s: got %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestInitializeRecordsClientExtensionSurfaceSupport(t *testing.T) {
	svc := &service{}
	if svc.extensionSurfaceSupported() {
		t.Fatal("supported before initialize")
	}
	params := InitializeParams{
		ProtocolVersion: 1,
		ClientCapabilities: ClientCapabilities{Meta: map[string]any{
			"reasonix.io": map[string]any{
				"extensionSurface": map[string]any{"supported": true, "schemaVersion": 1},
			},
		}},
	}
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	if _, err := svc.initialize(context.Background(), raw); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if !svc.extensionSurfaceSupported() {
		t.Fatal("client support not recorded")
	}
}

// extActionController builds a real controller with one registered extension
// action backed by a fake sidecar client — the same wiring boot installs.
type extActionClient struct {
	result protocol.UIActionResult
	got    *protocol.UIActionParams
}

func (f *extActionClient) UIAction(_ context.Context, p protocol.UIActionParams) (protocol.UIActionResult, error) {
	f.got = &p
	return f.result, nil
}

func (f *extActionClient) UISubmit(_ context.Context, p protocol.UISubmitParams) (protocol.UISubmitResult, error) {
	return protocol.UISubmitResult{Accepted: true}, nil
}

func newExtActionController(t *testing.T, client *extActionClient) acpController {
	t.Helper()
	ctrl := control.New(control.Options{Sink: event.Discard})
	hub := uihub.New(uihub.Options{
		SessionID: "sess-1", Generation: 1,
		Resolve: func(string) uihub.ActionClient { return client },
	})
	if err := hub.RegisterActions("alpha", []protocol.UIActionDecl{{ActionID: "act1", Label: "Act one"}}); err != nil {
		t.Fatalf("RegisterActions: %v", err)
	}
	ctrl.SetExtensionUI(hub)
	return ctrl
}

func TestAvailableCommandsIncludeExtensionActions(t *testing.T) {
	ctrl := newExtActionController(t, &extActionClient{})
	cmds := availableCommandsFor(ctrl)
	var found *AvailableCommand
	for i := range cmds {
		if cmds[i].Name == "alpha:act1" {
			found = &cmds[i]
		}
	}
	if found == nil {
		t.Fatalf("extension action missing from available commands: %+v", cmds)
	}
	if found.Description != "Act one" {
		t.Errorf("description = %q, want the action label", found.Description)
	}
}

func TestResolveSlashPromptFallsThroughToExtensionAction(t *testing.T) {
	client := &extActionClient{result: protocol.UIActionResult{Accepted: true, Message: "rerun scheduled"}}
	sess := &acpSession{id: "sess-1", ctrl: newExtActionController(t, client)}
	svc := &service{}

	got := svc.resolveSlashPrompt(context.Background(), sess, "/alpha:act1 k=v extra")
	if got != "rerun scheduled" {
		t.Fatalf("resolveSlashPrompt = %q, want the action result", got)
	}
	if client.got == nil || client.got.ActionID != "act1" ||
		client.got.Args["k"] != "v" || client.got.Args["arg1"] != "extra" {
		t.Fatalf("action params = %+v", client.got)
	}

	// Undeclared actions and non-action lines pass through untouched.
	if got := svc.resolveSlashPrompt(context.Background(), sess, "/alpha:other"); got != "/alpha:other" {
		t.Fatalf("undeclared action rewrote to %q", got)
	}
	if got := svc.resolveSlashPrompt(context.Background(), sess, "/plain"); got != "/plain" {
		t.Fatalf("plain slash rewrote to %q", got)
	}

	// A failed invocation leaves the line untouched rather than prompting the
	// model with an error string.
	failing := &acpSession{id: "sess-1", ctrl: newExtActionController(t, &extActionClient{
		result: protocol.UIActionResult{Accepted: false, Message: "nope"},
	})}
	if got := svc.resolveSlashPrompt(context.Background(), failing, "/alpha:act1"); got != "/alpha:act1" {
		t.Fatalf("failed action rewrote to %q", got)
	}
}
