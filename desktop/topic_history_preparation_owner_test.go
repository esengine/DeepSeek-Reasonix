package main

import (
	"context"
	"sync"
	"testing"

	"reasonix/internal/session"
)

func TestCanonicalInactiveHistoryPreparationWaitsForItsOwnerWithoutRuntime(t *testing.T) {
	app, source, target, root, _ := canonicalWorkspaceOpenFixture(t)
	app.readyHook = func() {}
	installNoopRuntimeEvents(app, source.sink)
	gate := newTabBuildGate(app)
	t.Cleanup(func() { app.shutdown(context.Background()) })
	t.Cleanup(gate.releaseAll)
	release, err := app.historyMaintenance.Foreground(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(release)
	if err := app.workspaceRegistry().EnsureSessionTopic(t.Context(), target.Ref().SessionID, "owner-gated-topic", "History owner"); err != nil {
		t.Fatal(err)
	}
	meta, err := app.openProjectTabInactive(root, "owner-gated-topic")
	if err != nil {
		t.Fatal(err)
	}
	gate.waitEntered(t, meta.ID)
	request := session.HistoryWindowRequest{Anchor: "newest", Limit: 32}
	page, err := app.SessionHistoryWindowForTab(meta.ID, request)
	if err != nil || page.Status != "preparing" {
		t.Fatalf("history before admission: %+v, %v", page, err)
	}
	query, ref, err := app.canonicalSessionQuery(meta.ID)
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	wait := &topicHistoryOwnerWaitContext{Context: t.Context(), entered: entered}
	done := make(chan struct{})
	var prepareErr error
	go func() {
		_, prepareErr = query.TitleMessages(wait, ref, 1)
		close(done)
	}()
	t.Cleanup(func() { release(); <-done })
	<-entered
	select {
	case <-done:
		t.Fatalf("history barrier completed while admission was held: %v", prepareErr)
	default:
	}
	release()
	<-done
	if prepareErr != nil {
		t.Fatal(prepareErr)
	}
	page, err = app.SessionHistoryWindowForTab(meta.ID, request)
	if err != nil || page.Status != "ready" || len(page.Messages) == 0 || page.Messages[len(page.Messages)-1].MessageID != "session-B-user" {
		t.Fatalf("history after preparation: %+v, %v", page, err)
	}
	app.mu.RLock()
	defer app.mu.RUnlock()
	if app.activeTabID != source.ID || app.tabs[meta.ID].Ctrl != nil || app.tabs[meta.ID].Ready {
		t.Fatal("history preparation started the gated runtime or changed the visible session")
	}
}

type topicHistoryOwnerWaitContext struct {
	context.Context
	entered chan struct{}
	once    sync.Once
}

func (c *topicHistoryOwnerWaitContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.entered) })
	return c.Context.Done()
}
