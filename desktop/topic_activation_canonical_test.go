package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"reasonix/internal/control"
	"reasonix/internal/session"
)

func canonicalTopicHistoryReady(t *testing.T, app *App, tabID string) session.HistoryWindowPage {
	t.Helper()
	req := session.HistoryWindowRequest{Anchor: "newest", Limit: 32}
	page, err := app.SessionHistoryWindowForTab(tabID, req)
	if err != nil {
		t.Fatal(err)
	}
	if page.Status == "preparing" {
		query, ref, err := app.canonicalSessionQuery(tabID)
		if err != nil {
			t.Fatal(err)
		}
		// TitleMessages joins this query's locator preparation, so completion
		// supplies the readiness boundary without polling filesystem speed.
		if _, err := query.TitleMessages(t.Context(), ref, 1); err != nil {
			t.Fatal(err)
		}
		page, err = app.SessionHistoryWindowForTab(tabID, req)
		if err != nil {
			t.Fatal(err)
		}
	}
	if page.Status != "ready" || len(page.Messages) == 0 {
		t.Fatalf("history is not readable: status=%s messages=%d", page.Status, len(page.Messages))
	}
	return page
}

func TestCanonicalSidebarActivationReplacesRemoteSurface(t *testing.T) {
	app, source, target, root, _ := canonicalWorkspaceOpenFixture(t)
	app.readyHook = func() {}
	installNoopRuntimeEvents(app, source.sink)
	events := newActivationEventRecorder(app)
	t.Cleanup(func() { app.shutdown(context.Background()) })

	remoteCtx, cancelRemote := context.WithCancel(context.Background())
	app.remoteTabMu.Lock()
	app.remoteTabs = map[string]*remoteTab{
		"remote": {id: "remote", ref: RemoteTabRef{HostID: "box", Workspace: "/work"}, state: "ready", cancel: cancelRemote},
	}
	app.remoteTabLayout = remoteTabLayoutState{activeID: "remote", order: []string{"remote"}, stripOrder: []string{"remote"}}
	app.remoteTabMu.Unlock()

	ref := target.Ref()
	ticket, err := app.StartTopicActivation(TopicActivationRequest{
		Selector: &SessionSelector{Ref: &ref}, Scope: "project", WorkspaceRoot: root,
		TopicID: "existing-topic", SessionPath: sessionRoute(ref.SessionID), RequestID: "sidebar-local",
	})
	if err != nil {
		t.Fatal(err)
	}
	if ticket.Meta.SessionID != ref.SessionID || ticket.Meta.WorkspaceRoot != root {
		t.Fatalf("selected local session = %+v, want %s in %s", ticket.Meta, ref.SessionID, root)
	}
	if tabs := app.ListTabs(); len(tabs) == 0 {
		t.Fatal("local tab disappeared after canonical sidebar activation")
	} else {
		for _, tab := range tabs {
			if tab.Active != (tab.ID == ticket.TabID) {
				t.Fatalf("tab activity after canonical sidebar activation = %+v", tabs)
			}
		}
	}
	events.waitFor(t, activationEventFor(ticket.RequestID, "ready"))
	flushActivationCompletions(app)
	if tabs := app.ListTabs(); len(tabs) != 1 || tabs[0].ID != ticket.TabID || !tabs[0].Active {
		t.Fatalf("tabs after local activation = %+v", tabs)
	}
	select {
	case <-remoteCtx.Done():
	default:
		t.Fatal("remote tab remained attached after selecting a local session")
	}
}

func TestCanonicalTopicActivationPublishesReadableHistoryBeforeRuntime(t *testing.T) {
	app, source, target, root, workspace := canonicalWorkspaceOpenFixture(t)
	app.readyHook = func() {}
	installNoopRuntimeEvents(app, source.sink)
	events := newActivationEventRecorder(app)
	gate := newTabBuildGate(app)
	t.Cleanup(func() { app.shutdown(context.Background()) })
	t.Cleanup(gate.releaseAll)
	app.runtimeRebuildMu.Lock()
	unlock := sync.OnceFunc(app.runtimeRebuildMu.Unlock)
	defer unlock()

	type result struct {
		ticket TopicActivationTicket
		err    error
	}
	done := make(chan result, 1)
	ref := target.Ref()
	go func() {
		ticket, err := app.StartTopicActivation(TopicActivationRequest{
			Selector: &SessionSelector{Ref: &ref}, Scope: "project", WorkspaceRoot: source.WorkspaceRoot,
			SessionPath: sessionRoute(ref.SessionID), RequestID: "canonical-history",
		})
		done <- result{ticket, err}
	}()
	var ticket TopicActivationTicket
	select {
	case got := <-done:
		if got.err != nil {
			t.Fatal(got.err)
		}
		ticket = got.ticket
	case <-time.After(5 * time.Second):
		unlock()
		<-done
		t.Fatal("canonical history activation waited for the runtime rebuild mutex before publishing its identity")
	}
	gate.waitEntered(t, ticket.TabID)
	if ticket.Meta.SessionID != ref.SessionID || ticket.Meta.WorkspaceID != workspace || ticket.Meta.WorkspaceRoot != root || ticket.Meta.Ready {
		t.Fatalf("cold canonical ticket = %+v", ticket.Meta)
	}
	page := canonicalTopicHistoryReady(t, app, ticket.TabID)
	if page.Messages[len(page.Messages)-1].MessageID != "session-B-user" {
		t.Fatal("history read returned another session's messages")
	}
	events.waitFor(t, activationEventFor("canonical-history", "starting"))
	app.mu.RLock()
	sourcePreserved := app.tabs[source.ID] == source && source.Ctrl != nil && source.SessionID == "session-A"
	app.mu.RUnlock()
	if !sourcePreserved {
		t.Fatal("source runtime was replaced before target startup completed")
	}
	unlock()
	gate.release(ticket.TabID)
	events.waitFor(t, activationEventFor("canonical-history", "ready"))
	app.mu.RLock()
	controller := app.tabs[ticket.TabID].Ctrl
	app.mu.RUnlock()
	bound, ok := controller.(control.IdentityLifecycle).SessionRef()
	if !ok || bound != ref {
		t.Fatalf("startup bound a different session: %+v, want %+v", bound, ref)
	}
}

func TestCanonicalTopicOpenInactivePublishesIdentityWithoutChangingVisibleSession(t *testing.T) {
	app, source, target, root, workspace := canonicalWorkspaceOpenFixture(t)
	app.readyHook = func() {}
	installNoopRuntimeEvents(app, source.sink)
	gate := newTabBuildGate(app)
	t.Cleanup(func() { app.shutdown(context.Background()) })
	t.Cleanup(gate.releaseAll)
	if err := app.workspaceRegistry().EnsureSessionTopic(t.Context(), target.Ref().SessionID, "target-topic", "Target title"); err != nil {
		t.Fatal(err)
	}
	meta, err := app.openProjectTabInactive(root, "target-topic")
	if err != nil {
		t.Fatal(err)
	}
	gate.waitEntered(t, meta.ID)
	if meta.SessionID != target.Ref().SessionID || meta.WorkspaceID != workspace || meta.TopicTitle != "Target title" || meta.Ready {
		t.Fatalf("inactive canonical metadata = %+v", meta)
	}
	app.mu.RLock()
	active := app.activeTabID
	app.mu.RUnlock()
	if active != source.ID {
		t.Fatal("inactive history open changed the visible session")
	}
	canonicalTopicHistoryReady(t, app, meta.ID)
}

func TestCanonicalTopicActivationSupersededBuildKeepsRunningSource(t *testing.T) {
	app, source, target, root, _ := canonicalWorkspaceOpenFixture(t)
	app.readyHook = func() {}
	active := &activeNavigationController{
		SessionAPI: source.Ctrl, IdentityLifecycle: source.Ctrl.(control.IdentityLifecycle),
		status: control.RuntimeStatus{Running: true},
	}
	source.Ctrl = active
	installNoopRuntimeEvents(app, source.sink)
	events := newActivationEventRecorder(app)
	gate := newTabBuildGate(app)
	t.Cleanup(func() { app.shutdown(context.Background()) })
	t.Cleanup(gate.releaseAll)
	first, err := app.StartTopicActivation(TopicActivationRequest{
		Scope: "project", WorkspaceRoot: root, SessionPath: sessionRoute(target.Ref().SessionID), RequestID: "first",
	})
	if err != nil {
		t.Fatal(err)
	}
	gate.waitEntered(t, first.TabID)
	app.mu.RLock()
	buildDone := app.tabs[first.TabID].buildDone
	app.mu.RUnlock()
	second, err := app.StartTopicActivation(TopicActivationRequest{
		Scope: "project", WorkspaceRoot: source.WorkspaceRoot, SessionPath: sessionRoute(source.SessionID), RequestID: "second",
	})
	if err != nil {
		t.Fatal(err)
	}
	events.waitFor(t, activationEventFor("first", "cancelled"))
	events.waitFor(t, activationEventFor("second", "ready"))
	gate.release(first.TabID)
	select {
	case <-buildDone:
	case <-time.After(5 * time.Second):
		t.Fatal("superseded build did not stop")
	}
	app.mu.RLock()
	current := app.tabs[app.activeTabID]
	correct := current == source && current.Ctrl == active && second.TabID == source.ID
	app.mu.RUnlock()
	if !correct || active.closed {
		t.Fatal("late history build displaced or closed the running source")
	}
}

func TestCanonicalTopicActivationKeepsHistoryReadableWhenWriterUnavailable(t *testing.T) {
	app, source, target, root, _ := canonicalWorkspaceOpenFixture(t)
	app.readyHook = func() {}
	installNoopRuntimeEvents(app, source.sink)
	events := newActivationEventRecorder(app)
	t.Cleanup(func() { app.shutdown(context.Background()) })
	if err := app.desktopSessionService("").Close(t.Context(), target.Ref()); err != nil {
		t.Fatal(err)
	}
	other, err := session.NewService(localDesktopHostID, session.NewFilesystemPersistence(app.desktopSessions.root))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = other.Shutdown(context.Background()) })
	lease, err := other.Open(t.Context(), target.Ref())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lease.Release(context.Background()) })
	ticket, err := app.StartTopicActivation(TopicActivationRequest{
		Scope: "project", WorkspaceRoot: root, SessionPath: sessionRoute(target.Ref().SessionID), RequestID: "writer-busy",
	})
	if err != nil {
		t.Fatalf("writer ownership blocked history navigation: %v", err)
	}
	events.waitFor(t, activationEventFor("writer-busy", "failed"))
	canonicalTopicHistoryReady(t, app, ticket.TabID)
	app.mu.RLock()
	tab := app.tabs[ticket.TabID]
	correct := tab != nil && tab.SessionID == target.Ref().SessionID && tab.StartupErr != "" && !tab.Ready
	app.mu.RUnlock()
	if !correct {
		t.Fatal("runtime failure lost the target identity or failure state")
	}
}

func TestCanonicalTopicOpenRejectsConflictingWorkspaceBeforePublishing(t *testing.T) {
	app, source, target, root, workspaceID := canonicalWorkspaceOpenFixture(t)
	state, err := app.workspaceRegistry().Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	workspace := state.Workspaces[workspaceID]
	workspace.Root = source.WorkspaceRoot
	state.Workspaces[workspaceID] = workspace
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(app.workspaceRegistry().Path(), data, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = app.OpenTopicSession("project", root, "", sessionRoute(target.Ref().SessionID))
	if !errors.Is(err, errSessionWorkspaceConflict) {
		t.Fatalf("conflicting canonical workspace accepted: %v", err)
	}
	if len(app.tabs) != 1 || app.activeTabID != source.ID || source.SessionID != "session-A" || !source.Ready {
		t.Fatal("failed identity validation changed the visible session")
	}
}

func TestCanonicalTopicActivationReattachesRunningSourceWithoutRebuild(t *testing.T) {
	app, source, target, root, _ := canonicalWorkspaceOpenFixture(t)
	app.readyHook = func() {}
	active := &activeNavigationController{
		SessionAPI: source.Ctrl, IdentityLifecycle: source.Ctrl.(control.IdentityLifecycle),
		status: control.RuntimeStatus{Running: true},
	}
	source.Ctrl = active
	installNoopRuntimeEvents(app, source.sink)
	events := newActivationEventRecorder(app)
	t.Cleanup(func() { app.shutdown(context.Background()) })
	sourceID, sourceRoot := source.SessionID, source.WorkspaceRoot
	first, err := app.StartTopicActivation(TopicActivationRequest{
		Scope: "project", WorkspaceRoot: root, SessionPath: sessionRoute(target.Ref().SessionID), RequestID: "target",
	})
	if err != nil {
		t.Fatal(err)
	}
	events.waitFor(t, activationEventFor("target", "ready"))
	// Complete pruning before the return click so this covers detached reuse,
	// not merely reuse of a source that is still in the visible tabs map.
	app.singleSurfaceMu.Lock()
	_, err = app.keepOnlyVisibleTab(first.TabID)
	app.singleSurfaceMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	app.mu.RLock()
	_, visible := app.tabs[source.ID]
	app.mu.RUnlock()
	if visible || active.closed {
		t.Fatal("running source was not preserved as a detached runtime")
	}
	second, err := app.StartTopicActivation(TopicActivationRequest{
		Scope: "project", WorkspaceRoot: sourceRoot, SessionPath: sessionRoute(sourceID), RequestID: "return",
	})
	if err != nil {
		t.Fatal(err)
	}
	events.waitFor(t, activationEventFor("return", "ready"))
	app.mu.RLock()
	current := app.tabs[second.TabID]
	correct := current != nil && current.Ctrl == active && current.SessionID == sourceID
	app.mu.RUnlock()
	if !correct || active.closed {
		t.Fatal("returning to running history replaced its controller")
	}
}
