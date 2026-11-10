package boot

import (
	"context"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/ext/extension"
	"reasonix/internal/ext/extension/dispatch"
	"reasonix/internal/ext/extension/protocol"
	"reasonix/internal/ext/extension/sidecar"
)

// The dispatcher the stage's hook feeds reaches the frontend sink as one typed
// notice naming the extension, the point and the reason — and it does so once.
func TestMissingSidecarReachesTheSinkAsATypedNotice(t *testing.T) {
	sink := &noticeSink{}
	stage, err := startExtensions(context.Background(), Options{}, config.RootsForHome(testenv.TempDir(t)), testenv.TempDir(t), nil, sink)
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

// A dispatcher staged by a subgraph rebuild keeps the stage's reporting, so a
// sidecar missing right after an extension was installed still reaches the sink.
func TestRebuiltDispatcherKeepsTheMissingSidecarNotice(t *testing.T) {
	sink := &noticeSink{}
	stage, err := startExtensions(context.Background(), Options{}, config.RootsForHome(testenv.TempDir(t)), testenv.TempDir(t), nil, sink)
	if err != nil {
		t.Fatal(err)
	}
	point := extension.PointToolBefore
	builder := extension.NewBuilder().WithSystemPrompt("p").WithGeneration(2)
	builder.AddContributor(extension.ContributorFunc{
		ContributorName: "rebuilt",
		Fn: func(context.Context) ([]extension.Contribution, error) {
			return []extension.Contribution{{
				Kind: extension.KindInterceptor, ID: string(point),
				Source: extension.ContributionSource{Scope: extension.ScopePlugin, PluginID: "aipush-ask-bridge", Origin: "plugin"},
			}}, nil
		},
	})
	snap, set, err := builder.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = set.Close() })
	previous := dispatch.New(nil, nil, func(string) dispatch.Client { return nil }, nil, dispatch.Options{
		Warn: stage.warn, SidecarDown: stage.sidecarDown,
	})
	mgr, _, err := sidecar.StartPackagesWithPlan(context.Background(), robustTempDir(t), protocol.SessionContext{SessionID: "s", WorkspaceRoot: robustTempDir(t), Generation: 2}, nil, nil, &extension.RuntimePlan{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mgr.Close() })
	res := &BuildResult{Snapshot: snap, Dispatcher: previous, Extensions: mgr}
	if err := stageDispatcher(res); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := res.Dispatcher.Intercept(context.Background(), point, &dispatch.ToolBeforePayload{}); err != nil {
			t.Fatal(err)
		}
	}
	if len(sink.ev) != 1 || sink.ev[0].Code != event.NoticeCodeExtensionSkipped {
		t.Fatalf("notices = %+v, want exactly one %q notice from the rebuilt dispatcher", sink.ev, event.NoticeCodeExtensionSkipped)
	}
}
