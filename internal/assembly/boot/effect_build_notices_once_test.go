package boot

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/surface"
	"reasonix/internal/ext/extension"
	"reasonix/internal/runtime/agent/testutil"
	"reasonix/internal/safety/sandbox"
	"reasonix/internal/state/workspacelease"
)

func buildNoticeFixture(t *testing.T, permissions string) (*[]event.Event, event.Sink) {
	t.Helper()
	isolateConfigHome(t)
	t.Chdir(robustTempDir(t))
	writeUserConfig(t, userModel+permissions)
	registerBootTokenProfileTestProvider()
	setBootTokenProfileTestProvider(t, testutil.NewMock("notices"))
	var (
		mu  sync.Mutex
		got []event.Event
	)
	return &got, event.FuncSink(func(e event.Event) {
		if e.Kind == event.Notice && e.Code == event.NoticeCodePermissionRulesDormant {
			mu.Lock()
			got = append(got, e)
			mu.Unlock()
		}
	})
}

// A rebuild re-derives every build-time notice from unchanged inputs; the
// operator has already read it, so only a notice that is new or changed is
// emitted again.
func TestEffectRebuildEmitsOnlyChangedBuildNotices(t *testing.T) {
	notices, sink := buildNoticeFixture(t, "\n[permissions]\nask = [\"rm\"]\n")
	ctx := context.Background()
	ctrl, err := Build(ctx, Options{Sink: sink})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	if len(*notices) != 1 {
		t.Fatalf("first build notices = %d, want 1", len(*notices))
	}

	res, err := Rebuild(ctx, ctrl, Options{Sink: sink})
	if err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	defer res.Controller.Close()
	if len(*notices) != 1 {
		t.Fatalf("an unchanged rebuild repeated the notice: %d emitted", len(*notices))
	}

	writeUserConfig(t, userModel+"\n[permissions]\nask = [\"rm\", \"git reset\"]\n")
	res2, err := Rebuild(ctx, res.Controller, Options{Sink: sink})
	if err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	defer res2.Controller.Close()
	if len(*notices) != 2 {
		t.Fatalf("a changed notice must be emitted: %d emitted", len(*notices))
	}

	writeUserConfig(t, userModel)
	res3, err := Rebuild(ctx, res2.Controller, Options{Sink: sink})
	if err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	defer res3.Controller.Close()
	writeUserConfig(t, userModel+"\n[permissions]\nask = [\"rm\", \"git reset\"]\n")
	res4, err := Rebuild(ctx, res3.Controller, Options{Sink: sink})
	if err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	defer res4.Controller.Close()
	if len(*notices) != 3 {
		t.Fatalf("a notice that cleared and returned must be emitted again: %d emitted", len(*notices))
	}
}

func TestEffectIndependentBuildsEachEmitBuildNotices(t *testing.T) {
	notices, sink := buildNoticeFixture(t, "\n[permissions]\nask = [\"rm\"]\n")
	for range 2 {
		ctrl, err := Build(context.Background(), Options{Sink: sink})
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
		defer ctrl.Close()
	}
	if len(*notices) != 2 {
		t.Fatalf("independent builds emitted %d notices, want 2", len(*notices))
	}
}

// A build that dies before delivering a runtime has shown its notices but
// replaced nothing, even when it dies by panic rather than by error: the
// runtime still serving keeps its notices as seen.
func TestEffectPanickingBuildDoesNotCountAsDelivered(t *testing.T) {
	isolateConfigHome(t)
	t.Chdir(robustTempDir(t))
	registerBootTokenProfileTestProvider()
	setBootTokenProfileTestProvider(t, testutil.NewMock("panic"))
	var notices []event.Event
	owner := extension.NewRuntimeOwner()
	opts := func(tune func(*sandbox.ShellDiscovery)) Options {
		return Options{Sink: staleDefaultNotices(&notices), StatsSource: surface.Desktop, OpenOnFallbackModel: true,
			RuntimeReload: RuntimeReload{Owner: owner}, tuneShell: tune}
	}
	writeUserConfig(t, staleDefaultUserConfig)
	first, err := Build(context.Background(), opts(nil))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer first.Close()
	writeUserConfig(t, strings.Replace(staleDefaultUserConfig, "deepseek-v4-flash", "deepseek-v9", 1))
	func() {
		defer func() { _ = recover() }()
		_, _ = Build(context.Background(), opts(func(*sandbox.ShellDiscovery) { panic("boom") }))
	}()
	if len(notices) != 2 {
		t.Fatalf("the panicking build must have raised its own notice: %d", len(notices))
	}
	writeUserConfig(t, staleDefaultUserConfig)
	again, err := Build(context.Background(), opts(nil))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer again.Close()
	if len(notices) != 2 {
		t.Fatalf("a notice the serving runtime already showed was repeated after a failed build: %d", len(notices))
	}
}

// The lease pair is raised by waits at run time, after the build window has
// closed, so a rebuild never swallows the next wait's notice.
func TestEffectRebuildKeepsWorkspaceLeaseNotices(t *testing.T) {
	isolateConfigHome(t)
	root := robustTempDir(t)
	holder, err := workspacelease.New(root, config.WorkspaceLeaseDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	holder.BeginRun()
	if err := holder.AcquireWrite(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer holder.EndRun()

	ledger := event.NewNoticeLedger()
	var began int
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.Notice && e.Code == event.NoticeCodeWorkspaceLease {
			began++
		}
	})
	for gen := 1; gen <= 2; gen++ {
		w := ledger.Begin(sink)
		session, err := startSessionRuntime(Options{SessionDir: robustTempDir(t)}, config.Default(), root, w)
		if err != nil {
			t.Fatal(err)
		}
		w.Close(true)
		session.lease.BeginRun()
		ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
		_ = session.lease.AcquireWrite(ctx)
		cancel()
		session.lease.EndRun()
		session.jobs.Close()
		if began != gen {
			t.Fatalf("generation %d: wait notices = %d, want %d", gen, began, gen)
		}
	}
}
