package uihub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/ext/extension/protocol"
	"reasonix/internal/ext/pluginpkg"
)

func framePackage(t *testing.T) pluginpkg.Package {
	t.Helper()
	root := testenv.TempDir(t)
	if err := os.MkdirAll(filepath.Join(root, "frames"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "frames", "board.html"), []byte("<p>board</p>"), 0o644); err != nil {
		t.Fatal(err)
	}
	return pluginpkg.Package{Root: root, Manifest: pluginpkg.Manifest{Frames: []pluginpkg.FrameRef{{ID: "board", Entry: "frames/board.html", Title: "Board"}}}}
}

func framePublish(surface, frame string, props any) protocol.UIPublishParams {
	body := map[string]any{"frame": frame, "title": "Board " + testCredential, "fallback": "3 cards " + testCredential}
	if props != nil {
		body["props"] = props
	}
	raw, _ := json.Marshal(body)
	return protocol.UIPublishParams{SurfaceID: surface, SessionID: "sess-1", Generation: 7, Kind: protocol.UISurfaceFrame, Payload: raw}
}

func TestPublishFrameRedactsEveryStringItCarries(t *testing.T) {
	rec := &eventRecorder{}
	h := newTestHub(rec)
	h.HandlerFor("alpha")
	h.DeclareFrames("alpha", framePackage(t))
	result := publishRaw(t, h, "alpha", framePublish("kanban", "board", map[string]any{
		"cards": []any{map[string]any{"title": "ship " + testCredential, "points": 3}},
	}))
	if !result.Accepted {
		t.Fatal("frame publish not accepted")
	}
	events := rec.all()
	if len(events) != 1 || events[0].Extension.Kind != event.ExtensionSurfaceFrame || events[0].Extension.Frame == nil {
		t.Fatalf("events = %+v", events)
	}
	frame := events[0].Extension.Frame
	for _, s := range []string{frame.Title, frame.Fallback, string(frame.Props)} {
		if strings.Contains(s, "sk-abcdef") {
			t.Fatalf("frame text not redacted: %q", s)
		}
	}
	if !strings.Contains(string(frame.Props), `"points":3`) {
		t.Fatalf("props lost their numbers: %s", frame.Props)
	}
}

func TestPublishRefusesAFrameThePackageDoesNotDeclare(t *testing.T) {
	h := newTestHub(&eventRecorder{})
	h.DeclareFrames("alpha", framePackage(t))
	_, err := h.HandlerFor("alpha").Publish(context.Background(), framePublish("x", "other", nil))
	var pe *protocol.ProtocolError
	if !errors.As(err, &pe) || pe.Reason != protocol.ErrCapabilityNotDeclared {
		t.Fatalf("err = %v", err)
	}
	_, err = h.HandlerFor("beta").Publish(context.Background(), framePublish("x", "board", nil))
	if !errors.As(err, &pe) || pe.Reason != protocol.ErrCapabilityNotDeclared {
		t.Fatalf("a plugin with no frames published one: %v", err)
	}
}

func TestASessionKeepsAtMostSixFramesAndARepublishIsNotANewOne(t *testing.T) {
	h := newTestHub(&eventRecorder{})
	h.DeclareFrames("alpha", framePackage(t))
	handler := h.HandlerFor("alpha")
	for i := range maxLiveFrames {
		if _, err := handler.Publish(context.Background(), framePublish(fmt.Sprintf("s%d", i), "board", nil)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := handler.Publish(context.Background(), framePublish("s0", "board", map[string]any{"v": 2})); err != nil {
		t.Fatalf("republish refused: %v", err)
	}
	if _, err := handler.Publish(context.Background(), framePublish("s6", "board", nil)); !errors.Is(err, ErrFrameLimit) {
		t.Fatalf("seventh frame = %v", err)
	}
}

func TestFrameDocumentAnswersOnlyTheLiveGeneration(t *testing.T) {
	h := newTestHub(&eventRecorder{})
	h.HandlerFor("alpha")
	h.DeclareFrames("alpha", framePackage(t))
	if doc, err := h.FrameDocument("alpha", "board", 7); err != nil || string(doc) != "<p>board</p>" {
		t.Fatalf("doc = %q, %v", doc, err)
	}
	if _, err := h.FrameDocument("alpha", "board", 6); !errors.Is(err, ErrFrameStale) {
		t.Fatalf("old generation = %v", err)
	}
	if _, err := h.FrameDocument("alpha", "other", 7); !errors.Is(err, pluginpkg.ErrFrameNotDeclared) {
		t.Fatalf("undeclared = %v", err)
	}
	if _, err := h.FrameDocument("ghost", "board", 7); !errors.Is(err, ErrFrameStale) {
		t.Fatalf("unknown plugin = %v", err)
	}
	h.ClientCrashed("alpha")
	if _, err := h.FrameDocument("alpha", "board", 7); !errors.Is(err, ErrFrameStale) {
		t.Fatalf("crashed plugin = %v", err)
	}
}

func TestTheHostTellsASidecarItAcceptsFrames(t *testing.T) {
	d, ok := newTestHub(&eventRecorder{}).HandlerFor("alpha").(interface{ DrawsFrames() bool })
	if !ok || !d.DrawsFrames() {
		t.Fatal("a hub binding does not accept frames")
	}
}
