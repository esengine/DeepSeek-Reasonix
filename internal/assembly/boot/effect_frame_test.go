package boot

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/eventwire"
	"reasonix/internal/ext/extension/protocol"
	"reasonix/internal/ext/extension/uihub"
	"reasonix/internal/ext/pluginpkg"
)

// declareBootFrame adds a frame document to an installed fake plugin, the way
// a package author ships one.
func declareBootFrame(t *testing.T, name, doc string) {
	t.Helper()
	root := filepath.Join(config.ReasonixHomeDir(), "plugins", name)
	path := filepath.Join(root, pluginpkg.NativeManifest)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest["contributes"] = map[string]any{"frames": []map[string]string{{"id": "board", "entry": "frames/board.html", "title": "Board"}}}
	raw, _ = json.Marshal(manifest)
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "frames"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "frames", "board.html"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestEffectAFramePublicationReachesTheFrontendAndItsDocumentIsServedFromThePackage(t *testing.T) {
	rec := &uiSinkRecorder{}
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	writeRuntimeFixture(t, dir)
	installBootFakePlugin(t, config.ReasonixHomeDir(), "framed", map[string]any{
		"capabilities": []string{"ui"},
		"env":          map[string]string{bootFakeEnvInitResult: `{"protocolVersion":"2","name":"framed","version":"1.0.0","stateSchemaVersion":0}`},
	})
	declareBootFrame(t, "framed", "<!doctype html><p>board</p>")
	res, err := BuildRuntime(context.Background(), Options{Sink: event.FuncSink(rec.emit)})
	if err != nil {
		t.Fatalf("BuildRuntime: %v", err)
	}
	t.Cleanup(res.Controller.Close)

	h := res.ExtensionUI
	payload := json.RawMessage(`{"frame":"board","slot":"pane.tab","title":"Board","fallback":"3 cards","props":{"token":"api_key=sk-abcdef1234567890SECRETKEY"}}`)
	result, err := h.HandlerFor("framed").Publish(context.Background(), protocol.UIPublishParams{
		SessionID: h.SessionID(), Generation: h.Generation(), SurfaceID: "kanban", Kind: protocol.UISurfaceFrame, Payload: payload,
	})
	if err != nil || !result.Accepted {
		t.Fatalf("publication: %+v %v", result, err)
	}
	snapshot := res.Controller.ExtensionSurfaces()
	if len(snapshot) != 1 || snapshot[0].Frame == nil {
		t.Fatalf("controller snapshot: %+v", snapshot)
	}
	wire, _ := json.Marshal(eventwire.ToWireExtensionSurface(&snapshot[0]))
	if !strings.Contains(string(wire), `"kind":"frame"`) || strings.Contains(string(wire), "sk-abcdef") {
		t.Fatalf("frontend wire: %s", wire)
	}

	doc, err := res.Controller.ExtensionFrameDocument("framed", "board", h.Generation())
	if err != nil || string(doc) != "<!doctype html><p>board</p>" {
		t.Fatalf("document = %q, %v", doc, err)
	}
	if _, err := res.Controller.ExtensionFrameDocument("framed", "board", h.Generation()+1); !errors.Is(err, uihub.ErrFrameStale) {
		t.Fatalf("a future generation was served: %v", err)
	}
	if _, err := res.Controller.ExtensionFrameDocument("framed", "nope", h.Generation()); !errors.Is(err, pluginpkg.ErrFrameNotDeclared) {
		t.Fatalf("an undeclared frame was served: %v", err)
	}
}
