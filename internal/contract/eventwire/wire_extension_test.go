package eventwire

import (
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/contract/event"
)

// TestToWireExtensionSurfacePayloads covers the stage-8a extension surface
// events: status rides ExtensionStatus, card/form/notification ride
// ExtensionSurface, and each carries exactly its own sub-struct.
func TestToWireExtensionSurfacePayloads(t *testing.T) {
	progress := 0.5
	tests := []struct {
		name string
		in   event.Event
		want []string
	}{
		{
			name: "status",
			in: event.Event{Kind: event.ExtensionStatus, Extension: &event.ExtensionSurfacePayload{
				PluginID: "alpha", SurfaceID: "s1", SessionID: "sess", Generation: 7,
				Kind:   event.ExtensionSurfaceStatus,
				Status: &event.ExtensionStatusView{Label: "working", Detail: "half", Severity: "warn", Progress: &progress},
			}},
			want: []string{
				`"kind":"extension_status"`, `"extension":{"pluginId":"alpha","surfaceId":"s1","sessionId":"sess","generation":7,"kind":"status"`,
				`"status":{"label":"working","detail":"half","severity":"warn","progress":0.5}`,
			},
		},
		{
			name: "card",
			in: event.Event{Kind: event.ExtensionSurface, Extension: &event.ExtensionSurfacePayload{
				PluginID: "alpha", SurfaceID: "c1", Kind: event.ExtensionSurfaceCard,
				Card: &event.ExtensionCardView{
					Title: "T", Markdown: "**m**", Text: "x", Progress: &progress,
					Fields:  []event.ExtensionKeyValue{{Key: "k", Value: "v"}},
					Actions: []event.ExtensionActionRef{{ActionID: "act1", Label: "go"}},
				},
			}},
			want: []string{
				`"kind":"extension_surface"`, `"kind":"card"`,
				`"card":{"title":"T","markdown":"**m**","text":"x","fields":[{"key":"k","value":"v"}],"progress":0.5,"actions":[{"actionId":"act1","label":"go"}]}`,
			},
		},
		{
			name: "form",
			in: event.Event{Kind: event.ExtensionSurface, Extension: &event.ExtensionSurfacePayload{
				PluginID: "alpha", SurfaceID: "f1", Kind: event.ExtensionSurfaceForm,
				Form: &event.ExtensionFormView{
					Title: "f", Message: "m",
					Fields: []event.ExtensionFormField{{
						Key: "field1", Label: "L", Kind: "multiselect",
						Options: []string{"a", "b"}, Default: "a", Required: true,
					}},
				},
			}},
			want: []string{
				`"kind":"extension_surface"`, `"kind":"form"`,
				`"form":{"title":"f","message":"m","fields":[{"key":"field1","label":"L","kind":"multiselect","options":["a","b"],"default":"a","required":true}]}`,
			},
		},
		{
			name: "notification",
			in: event.Event{Kind: event.ExtensionSurface, Extension: &event.ExtensionSurfacePayload{
				PluginID: "alpha", SurfaceID: "n1", Kind: event.ExtensionSurfaceNotification,
				Notification: &event.ExtensionNotificationView{Title: "n", Body: "b", Severity: "error"},
			}},
			want: []string{
				`"kind":"extension_surface"`, `"kind":"notification"`,
				`"notification":{"title":"n","body":"b","severity":"error"}`,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, err := json.Marshal(ToWire(tt.in))
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			s := string(b)
			for _, want := range tt.want {
				if !strings.Contains(s, want) {
					t.Fatalf("%s JSON = %s, want it to contain %s", tt.name, s, want)
				}
			}
		})
	}
}

// TestToWireExtensionNilPayload marshals no extension object instead of a
// half-filled one when the payload is missing.
func TestToWireExtensionNilPayload(t *testing.T) {
	b, err := json.Marshal(ToWire(event.Event{Kind: event.ExtensionStatus}))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(b), `"extension"`) {
		t.Fatalf("nil payload JSON = %s, must omit the extension field", b)
	}
}

// The JSON a view serializes to is what the frontend's ExtensionViewNode type
// is written against, and nothing checks the two against each other at build
// time. Pinning the wire shape here is what makes a renamed field a failing
// test rather than a node that silently stops rendering.
func TestToWireExtensionViewShape(t *testing.T) {
	progress := 0.62
	ev := event.Event{Kind: event.ExtensionSurface, Extension: &event.ExtensionSurfacePayload{
		PluginID: "alpha", SurfaceID: "usage", Kind: event.ExtensionSurfaceView,
		View: &event.ExtensionViewSurface{
			Slot: "composer-trailing",
			Body: []event.ExtensionViewNode{
				{Kind: "row", Children: []event.ExtensionViewNode{
					{Kind: "pip", Tone: "ok"},
					{Kind: "text", Value: "quota 62%", Tone: "strong"},
				}},
				{Kind: "meter", Progress: &progress},
				{Kind: "button", ActionID: "open-billing", Label: "manage"},
			},
		},
	}}
	raw, err := json.Marshal(ToWire(ev))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`"kind":"view"`,
		`"view":{"slot":"composer-trailing"`,
		`{"kind":"row","children":[{"kind":"pip","tone":"ok"},{"kind":"text","value":"quota 62%","tone":"strong"}]}`,
		`{"kind":"meter","progress":0.62}`,
		`{"kind":"button","label":"manage","actionId":"open-billing"}`,
	} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("wire JSON missing %s\ngot: %s", want, raw)
		}
	}
}
