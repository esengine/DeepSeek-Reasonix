package boot

import (
	"context"
	"testing"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/ext/extension"
	"reasonix/internal/ext/extension/dispatch"
)

// The dispatcher the stage's hook feeds reaches the frontend sink as one typed
// notice naming the extension, the point and the reason — and it does so once.
func TestMissingSidecarReachesTheSinkAsATypedNotice(t *testing.T) {
	sink := &noticeSink{}
	stage, err := startExtensions(context.Background(), Options{}, config.RootsForHome(t.TempDir()), t.TempDir(), nil, sink)
	if err != nil {
		t.Fatal(err)
	}
	point := extension.PointToolBefore
	chain := map[extension.InterceptorPoint][]extension.Contribution{point: {{Source: extension.ContributionSource{PluginID: "aipush-ask-bridge"}}}}
	d := dispatch.New(chain, nil, func(string) dispatch.Client { return nil }, nil, dispatch.Options{
		Warn: stage.warn, SidecarDown: stage.sidecarDown,
	})
	for range 2 {
		if _, err := d.Intercept(context.Background(), point, &dispatch.ToolBeforePayload{}); err != nil {
			t.Fatal(err)
		}
	}
	if len(sink.ev) != 1 {
		t.Fatalf("notices = %+v, want exactly one", sink.ev)
	}
	ev := sink.ev[0]
	if ev.Kind != event.Notice || ev.Level != event.LevelWarn || ev.Code != event.NoticeCodeExtensionSkipped {
		t.Fatalf("notice = %+v, want a warn notice coded %q", ev, event.NoticeCodeExtensionSkipped)
	}
	got, ok := event.DecodeExtensionSkipped(ev.Detail)
	want := event.ExtensionSkipped{Extension: "aipush-ask-bridge", Point: string(point), Reason: event.ExtensionSkipReasonNoLiveSidecar}
	if !ok || got != want {
		t.Fatalf("payload = %+v (ok=%v), want %+v", got, ok, want)
	}
}
