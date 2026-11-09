package serve

import (
	"context"
	"errors"
	"testing"
	"time"

	"reasonix/internal/assembly/boot"
	"reasonix/internal/contract/event"
	"reasonix/internal/session/control"
)

func (f *dispatchFixture) replaceRuntime(c *control.Controller, action string, sink event.Sink) *control.Controller {
	f.t.Helper()
	var srv *Server
	for _, rt := range f.hub.localRuntimes() {
		if rt.Server.Controller() == c {
			srv = rt.Server
		}
	}
	if srv == nil {
		f.t.Fatal("sender has no runtime")
	}
	key := c.PeerKey()
	deadline := time.Now().Add(5 * time.Second)
	for controllerHasActiveRuntimeWork(c) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if controllerHasActiveRuntimeWork(c) {
		f.t.Fatal("session did not finish its queued peer messages")
	}
	build := func() *control.Controller {
		next := control.New(control.Options{
			WorkspaceRoot: c.WorkspaceRoot(), SessionDir: c.SessionDir(), ModelRef: "test/next", Sink: sink,
			Peers: control.PeersOptions{Dir: f.peersDir}, Posture: control.PostureEvidence{Home: f.home},
		})
		f.t.Cleanup(next.Close)
		return next
	}
	srv.buildController = func(context.Context, string) (*control.Controller, error) { return build(), nil }
	srv.rebuildController = func(_ context.Context, old *control.Controller, _ string) (*control.Controller, error) {
		next := build()
		return next, boot.ApplyRuntimeMigration(next, old, boot.CaptureRuntimeMigration(old))
	}
	var err error
	if action == "model-switch" {
		err = srv.switchModel(f.t.Context(), "test/next")
	} else {
		err = srv.reloadExtensions(f.t.Context())
	}
	if err != nil {
		f.t.Fatalf("%s: %v", action, err)
	}
	next := srv.Controller().(*control.Controller)
	if next == c || next.PeerKey() != key {
		f.t.Fatalf("%s did not replace the runtime of the same session", action)
	}
	return next
}

func TestDispatchAndRepliesSurviveRuntimeReplacement(t *testing.T) {
	for _, action := range []string{"model-switch", "extension-reload"} {
		t.Run(action, func(t *testing.T) {
			f := newDispatchFixture(t, dispatchOn)
			f.project("billing", true)
			parent := f.sender(f.project("alpha", true))
			first, err := parent.DispatchToProject(t.Context(), "billing", "first task")
			if err != nil {
				t.Fatal(err)
			}
			child := f.paneOf(first.Key)
			child.RefreshPeers()
			parent = f.replaceRuntime(parent, action, event.Discard)
			again, err := parent.DispatchToProject(t.Context(), "billing", "second task")
			if err != nil || again.Key != first.Key || f.opened != 1 {
				t.Fatalf("dispatch after replacement = %+v, %v; opened %d panes", again, err, f.opened)
			}
			child.RefreshPeers()
			if held := child.HeldPeerMessages(); len(held) != 0 {
				t.Fatalf("the paired task now needs approval: %+v", held)
			}
			child = f.replaceRuntime(child, action, event.Discard)
			if !child.OpenedByDispatch() || child.DispatchDepth() != 1 {
				t.Fatal("receiver lost its dispatch identity or depth")
			}
			if _, err := child.DispatchToProject(t.Context(), "alpha", "nested"); !errors.Is(err, control.ErrDispatchNested) {
				t.Fatalf("receiver lost its nesting limit: %v", err)
			}
			if _, err := child.SendPeerMessage(parent.PeerKey(), "completed"); err != nil {
				t.Fatal(err)
			}
			parent.RefreshPeers()
			if held := parent.HeldPeerMessages(); len(held) != 0 {
				t.Fatalf("the paired reply now needs approval: %+v", held)
			}
			f.wantReleasedByDispatch(parent.PeerKey(), child.PeerKey())
		})
	}
}

func TestOpeningProjectsStillAsksAfterRuntimeReplacement(t *testing.T) {
	for _, action := range []string{"model-switch", "extension-reload"} {
		t.Run(action, func(t *testing.T) {
			f := newDispatchFixture(t, dispatchOn)
			c := f.sender(f.project("alpha", true))
			asks := make(chan event.Approval, 1)
			c = f.replaceRuntime(c, action, event.FuncSink(func(e event.Event) {
				if e.Kind == event.ApprovalRequest {
					asks <- e.Approval
				}
			}))
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			result := make(chan error, 1)
			path := folder(t, "new-project")
			go func() { _, err := c.OpenProjects(ctx, []string{path}); result <- err }()
			select {
			case ask := <-asks:
				c.Approve(ask.ID, false, false, false)
			case err := <-result:
				t.Fatalf("project opening returned before asking: %v", err)
			case <-ctx.Done():
				t.Fatal("project opening did not ask")
			}
			if err := <-result; !errors.Is(err, control.ErrOpenProjectsDeclined) {
				t.Fatalf("declined opening = %v", err)
			}
		})
	}
}
