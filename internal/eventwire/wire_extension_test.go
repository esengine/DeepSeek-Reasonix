package eventwire

import (
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/event"
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
