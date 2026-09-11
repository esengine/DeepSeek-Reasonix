package main

import (
	"context"
	"testing"
	"time"
)

func TestSessionRecoveredEventPrecedesImmediateCatalogRevision(t *testing.T) {
	events := make(chan string, 4)
	app := &App{ctx: context.Background()}
	app.runtimeEvents.emit = func(_ context.Context, name string, _ ...any) {
		events <- name
	}
	app.projectTreeChangedHook = func() {
		app.emitProjectTreeChangedV2(1, []string{""}, "test-immediate-revision")
	}

	app.emitSessionRecoveredAndRefresh("", sessionRecoveryEvent{RecoveryPath: "/s/fork.jsonl", TopicID: "topic"})

	next := func() string {
		t.Helper()
		select {
		case name := <-events:
			return name
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for runtime event")
			return ""
		}
	}
	if got := next(); got != "session:recovered" {
		t.Fatalf("first event = %q, want session:recovered", got)
	}
	if got := next(); got != "project-tree:changed-v2" {
		t.Fatalf("second event = %q, want project-tree:changed-v2", got)
	}
}
