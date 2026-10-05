package uihub

import (
	"strings"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/ext/extension/protocol"
)

// A view arrives as a tree, and every string in it is shown to the user — so
// the redaction has to reach the leaves, not just the top level. A credential
// pasted into a nested label is exactly where one would survive an
// implementation that only walked the first row.
func TestPublishViewRedactsEveryLeaf(t *testing.T) {
	rec := &eventRecorder{}
	h := newTestHub(rec)
	progress := 0.62
	result := publishRaw(t, h, "alpha", protocol.UIPublishParams{
		SurfaceID: "usage", SessionID: "sess-1", Generation: 7, Kind: protocol.UISurfaceView,
		Payload: mustRaw(t, protocol.UIViewPayload{
			Slot: "composer-trailing",
			Body: []protocol.UINode{
				{Kind: protocol.UINodeRow, Children: []protocol.UINode{
					{Kind: protocol.UINodePip, Tone: protocol.UIToneOK},
					{Kind: protocol.UINodeText, Value: "quota " + testCredential, Tone: protocol.UIToneStrong},
				}},
				{Kind: protocol.UINodeMeter, Progress: &progress},
				{Kind: protocol.UINodeKV, Key: "key " + testCredential, Value: "value " + testCredential},
			},
		}),
	})
	if !result.Accepted {
		t.Fatal("view publish not accepted")
	}
	events := rec.all()
	if len(events) != 1 {
		t.Fatalf("emitted %d events, want 1", len(events))
	}
	view := events[0].Extension.View
	if view == nil || view.Slot != "composer-trailing" || len(view.Body) != 3 {
		t.Fatalf("view = %+v", view)
	}
	if events[0].Extension.Kind != event.ExtensionSurfaceView {
		t.Fatalf("payload kind = %q", events[0].Extension.Kind)
	}
	nested := view.Body[0].Children[1]
	if nested.Kind != "text" || nested.Tone != "strong" {
		t.Fatalf("nested node = %+v", nested)
	}
	kv := view.Body[2]
	for _, s := range []string{nested.Value, kv.Key, kv.Value} {
		if strings.Contains(s, "sk-abcdef") || !strings.Contains(s, "****") {
			t.Fatalf("view text not redacted: %q", s)
		}
	}
	if view.Body[1].Progress == nil || *view.Body[1].Progress != 0.62 {
		t.Fatalf("meter = %+v", view.Body[1])
	}
}

// A takeover has to survive the trip with its anchor intact, because the anchor
// is the only thing that says which card it replaces — and it is redacted like
// any other view, since a replaced card shows the same kind of text.
func TestPublishAnchoredViewKeepsItsAnchor(t *testing.T) {
	rec := &eventRecorder{}
	h := newTestHub(rec)
	result := publishRaw(t, h, "alpha", protocol.UIPublishParams{
		SurfaceID: "call-view", SessionID: "sess-1", Generation: 7, Kind: protocol.UISurfaceView,
		Payload: mustRaw(t, protocol.UIViewPayload{
			Anchor: "tool:call_42",
			Body:   []protocol.UINode{{Kind: protocol.UINodeText, Value: "read " + testCredential}},
		}),
	})
	if !result.Accepted {
		t.Fatal("anchored view not accepted")
	}
	view := rec.all()[0].Extension.View
	if view == nil || view.Anchor != "tool:call_42" {
		t.Fatalf("view = %+v, want the anchor carried through", view)
	}
	if strings.Contains(view.Body[0].Value, "sk-abcdef") {
		t.Fatalf("takeover text not redacted: %q", view.Body[0].Value)
	}
}
