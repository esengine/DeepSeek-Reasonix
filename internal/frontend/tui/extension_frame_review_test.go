package tui

import (
	"strings"
	"testing"

	"reasonix/internal/contract/eventwire"
)

func TestReviewFrameFallbackRemainsVisibleInTheTerminal(t *testing.T) {
	tr := &Transcript{}
	tr.Apply(eventwire.Event{
		Kind: "extension_surface",
		Extension: &eventwire.ExtensionSurface{
			PluginID: "board", SurfaceID: "tasks", Kind: "frame",
			Frame: &eventwire.ExtensionFrame{Frame: "board", Title: "Task board", Fallback: "3 pending tasks"},
		},
	})
	var text strings.Builder
	for i := range tr.Items {
		text.WriteString(renderItem(&tr.Items[i], 100, 0, false))
	}
	if !strings.Contains(text.String(), "3 pending tasks") {
		t.Fatalf("frame fallback disappeared from the real transcript: %q", text.String())
	}
}

func TestFrameFallbackRepublishUpdatesOneSafeTranscriptRow(t *testing.T) {
	tr := &Transcript{}
	ext := &eventwire.ExtensionSurface{
		SessionID: "s", Generation: 1, PluginID: "board", SurfaceID: "tasks", Kind: "frame",
		Frame: &eventwire.ExtensionFrame{Frame: "board", Title: "Task board", Fallback: "first"},
	}
	tr.Apply(eventwire.Event{Kind: "extension_surface", Extension: ext})
	id := tr.Items[0].ID
	ext.Frame.Fallback = "updated\x1b[2J\x1b]0;spoof\x07"
	tr.Apply(eventwire.Event{Kind: "extension_surface", Extension: ext})
	if len(tr.Items) != 1 || tr.Items[0].ID != id || !strings.Contains(tr.Items[0].Text, "updated") || strings.Contains(tr.Items[0].Text, "\x1b") {
		t.Fatalf("unsafe or duplicate fallback: %+v", tr.Items)
	}
	ext.Generation++
	tr.Apply(eventwire.Event{Kind: "extension_surface", Extension: ext})
	if len(tr.Items) != 2 {
		t.Fatal("new generation overwrote historical fallback")
	}
	ext.Code = "surface.expired"
	ext.Frame = nil
	tr.Apply(eventwire.Event{Kind: "extension_surface", Extension: ext})
	tr.Apply(eventwire.Event{Kind: "extension_surface"})
	if len(tr.Items) != 2 {
		t.Fatal("expired or empty event appended a fallback")
	}
}
