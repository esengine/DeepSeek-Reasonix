package main

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"

	"reasonix/internal/control"
)

// A real controller delivers TurnDone while its admission gate is still held.
// The project tree must subsequently receive the settled state, without the
// user opening another tab or causing a catalog reload.
func TestRuntimeStateCompletedTurnPublishesIdleProjectTree(t *testing.T) {
	isolateDesktopUserDirs(t)
	app := NewApp()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	app.ctx = ctx
	snapshots := make(chan ProjectTreeRuntimeSnapshot, 64)
	app.runtimeEvents.emit = func(_ context.Context, name string, payload ...any) {
		if name == "project-tree:runtime-changed" {
			snapshots <- payload[0].(ProjectTreeRuntimeSnapshot)
		}
	}
	sink := &tabEventSink{tabID: "runtime-regression", app: app}
	runner := &blockingRunner{started: make(chan struct{}), release: make(chan struct{})}
	releaseRunner := sync.OnceFunc(func() { close(runner.release) })
	ctrl := control.New(control.Options{
		Runner: runner, SessionDir: t.TempDir(), Label: "runtime regression", Sink: sink,
	})
	defer ctrl.Close()
	defer releaseRunner()
	tab := &WorkspaceTab{
		ID: sink.tabID, Scope: "global", TopicID: "runtime-regression-topic",
		Ctrl: ctrl, Ready: true, sink: sink, disabledMCP: map[string]ServerView{},
	}
	// Hold the independent metadata-save lane busy: runtime completion must
	// not depend on an autosave/catalog refresh happening to repair its state.
	// scheduleTabSnapshot will only mark saveAgain while this lane is occupied.
	tab.saving = true
	app.tabs[tab.ID] = tab
	app.tabOrder = []string{tab.ID}
	app.activeTabID = tab.ID
	ctrl.Submit("complete the isolated test turn")
	select {
	case <-runner.started:
	case <-time.After(5 * time.Second):
		t.Fatal("isolated runner did not start")
	}
	// Observe execution before allowing completion, so a publisher that
	// intentionally coalesces intermediate snapshots cannot hide the start.
	startDeadline := time.NewTimer(5 * time.Second)
	defer startDeadline.Stop()
started:
	for {
		select {
		case snapshot := <-snapshots:
			if len(snapshot.Topics) == 1 && snapshot.Topics[0].Node.Running {
				break started
			}
		case <-startDeadline.C:
			t.Fatal("running controller never published its activity")
		}
	}
	releaseRunner()
	waitNotRunning(t, ctrl)

	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	var last ProjectTreeRuntimeSnapshot
	for {
		select {
		case last = <-snapshots:
			if len(last.Topics) != 1 {
				continue
			}
			node := last.Topics[0].Node
			if node.Running || node.Status != "" {
				continue
			}
			fresh := app.GetProjectTreeRuntimeSnapshot()
			if last.Revision == fresh.Revision && !reflect.DeepEqual(last, fresh) {
				t.Fatalf("one revision identifies different runtime snapshots: published=%+v read=%+v", last, fresh)
			}
			status := ctrl.RuntimeStatus()
			if status.Running || status.PendingPrompt || status.BackgroundJobs != 0 {
				t.Fatalf("idle publication disagrees with controller: %+v", status)
			}
			return
		case <-deadline.C:
			t.Fatalf("completed controller never published idle: controller=%+v last=%+v fresh=%+v", ctrl.RuntimeStatus(), last, app.GetProjectTreeRuntimeSnapshot())
		}
	}
}
