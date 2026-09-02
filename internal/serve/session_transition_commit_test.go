package serve

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/eventwire"
	"reasonix/internal/provider"
)

func TestSessionTransitionPublishesRouteOnlyAfterControllerCommit(t *testing.T) {
	bc := NewBroadcaster()
	tag := newSessionTagSink(bc)
	dir := t.TempDir()
	initialPath := filepath.Join(dir, "old.jsonl")
	sess := agent.NewSession("sys")
	exec := agent.New(nil, nil, sess, agent.Options{}, tag)
	ctrl := control.New(control.Options{Runner: exec, Executor: exec, Sink: tag, SessionDir: dir, SessionPath: initialPath})
	oldPath := agent.CanonicalSessionPath(ctrl.SessionPath())
	tag.SetPath(oldPath)
	bc.SetCurrentSession(oldPath)

	srv := New(ctrl, bc, config.ServeConfig{})
	srv.RegisterSessionTag(ctrl, tag)
	handler := srv.sessionTransitionHandler(ctrl, nil)
	ctrl.SetOnSessionTransition(func(info control.SessionTransitionInfo) error {
		if err := handler(info); err != nil {
			return err
		}
		// This models output produced after transition preparation but before
		// ClearSession swaps its executor and session path.
		tag.Emit(event.Event{Kind: event.Notice, Text: "pre-commit hook output"})
		return nil
	})

	all, stop := bc.SubscribeAll()
	defer stop()
	if err := ctrl.ClearSession(); err != nil {
		t.Fatal(err)
	}
	newPath := agent.CanonicalSessionPath(ctrl.SessionPath())
	if newPath == oldPath {
		t.Fatal("clear did not rotate the session path")
	}
	tag.Emit(event.Event{Kind: event.Notice, Text: "post-commit output"})

	var before, after eventwire.Event
	if err := json.Unmarshal(<-all, &before); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(<-all, &after); err != nil {
		t.Fatal(err)
	}
	if before.SessionPath != oldPath || !before.SessionCurrent {
		t.Fatalf("pre-commit frame route = %q current=%v, want old foreground %q", before.SessionPath, before.SessionCurrent, oldPath)
	}
	if after.SessionPath != newPath || !after.SessionCurrent {
		t.Fatalf("post-commit frame route = %q current=%v, want new foreground %q", after.SessionPath, after.SessionCurrent, newPath)
	}
}

func TestBranchTransitionPublishesMustDeliverRoute(t *testing.T) {
	bc := NewBroadcaster()
	tag := newSessionTagSink(bc)
	dir := t.TempDir()
	initialPath := filepath.Join(dir, "old.jsonl")
	sess := agent.NewSession("")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "branch me"})
	exec := agent.New(nil, nil, sess, agent.Options{}, tag)
	ctrl := control.New(control.Options{Runner: exec, Executor: exec, Sink: tag, SessionDir: dir, SessionPath: initialPath})
	defer ctrl.Close()
	tag.SetPath(initialPath)
	bc.SetCurrentSession(initialPath)

	srv := New(ctrl, bc, config.ServeConfig{})
	srv.RegisterSessionTag(ctrl, tag)
	ctrl.SetOnSessionTransition(srv.sessionTransitionHandler(ctrl, nil))
	all, stop := bc.SubscribeAll()
	defer stop()
	for range subscriberBufferSize - subscriberPriorityReserve {
		bc.Emit(event.Event{Kind: event.Text, Text: "delta", SessionPath: initialPath})
	}
	for range subscriberPriorityReserve {
		bc.Emit(event.Event{Kind: event.Notice, Text: "priority", SessionPath: initialPath})
	}
	if got := len(all); got != subscriberBufferSize {
		t.Fatalf("saturated subscriber length = %d, want %d", got, subscriberBufferSize)
	}

	newPath, err := ctrl.Branch("child")
	if err != nil {
		t.Fatal(err)
	}
	newPath = agent.CanonicalSessionPath(newPath)
	found := false
	for len(all) > 0 {
		var frame eventwire.Event
		if err := json.Unmarshal(<-all, &frame); err != nil {
			t.Fatal(err)
		}
		if frame.Kind == "session_changed" && frame.SessionPath == newPath && frame.SessionCurrent {
			found = true
		}
	}
	if !found {
		t.Fatalf("saturated subscriber lost branch route to %q", newPath)
	}
}

func TestForegroundRecoveryPublishesMustDeliverRoute(t *testing.T) {
	bc := NewBroadcaster()
	tag := newSessionTagSink(bc)
	dir := t.TempDir()
	initialPath := filepath.Join(dir, "old.jsonl")
	recoveryPath := filepath.Join(dir, "old-recovery.jsonl")
	ctrl := control.New(control.Options{SessionPath: initialPath})
	defer ctrl.Close()
	tag.SetPath(initialPath)
	bc.SetCurrentSession(initialPath)

	srv := New(ctrl, bc, config.ServeConfig{})
	srv.RegisterSessionTag(ctrl, tag)
	all, stop := bc.SubscribeAll()
	defer stop()
	for range subscriberBufferSize - subscriberPriorityReserve {
		bc.Emit(event.Event{Kind: event.Text, Text: "delta", SessionPath: initialPath})
	}
	for range subscriberPriorityReserve {
		bc.Emit(event.Event{Kind: event.Notice, Text: "priority", SessionPath: initialPath})
	}
	if got := len(all); got != subscriberBufferSize {
		t.Fatalf("saturated subscriber length = %d, want %d", got, subscriberBufferSize)
	}

	srv.publishRecoveredControllerRoute(ctrl, recoveryPath)
	recoveryPath = agent.CanonicalSessionPath(recoveryPath)
	found := false
	for len(all) > 0 {
		var frame eventwire.Event
		if err := json.Unmarshal(<-all, &frame); err != nil {
			t.Fatal(err)
		}
		if frame.Kind == "session_changed" && frame.SessionPath == recoveryPath && frame.SessionCurrent {
			found = true
		}
	}
	if !found {
		t.Fatalf("saturated subscriber lost foreground recovery route to %q", recoveryPath)
	}
}
