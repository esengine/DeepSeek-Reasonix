package main

import (
	"context"
	"testing"
	"time"

	"reasonix/internal/control"
	"reasonix/internal/event"
)

func TestReplayPendingPromptsForTabDoesNotReplaySiblingAsk(t *testing.T) {
	type askHarness struct {
		ctrl   *control.Controller
		events chan event.Ask
		cancel context.CancelFunc
		done   chan struct{}
	}
	newHarness := func(label string) askHarness {
		events := make(chan event.Ask, 4)
		ctrl := control.New(control.Options{
			Label: label,
			Sink: event.FuncSink(func(e event.Event) {
				if e.Kind == event.AskRequest {
					events <- e.Ask
				}
			}),
		})
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})
		go func() {
			defer close(done)
			_, _ = ctrl.Ask(ctx, []event.AskQuestion{{ID: "choice", Prompt: "Pick one"}})
		}()
		return askHarness{ctrl: ctrl, events: events, cancel: cancel, done: done}
	}
	waitAsk := func(label string, events <-chan event.Ask) event.Ask {
		t.Helper()
		select {
		case ask := <-events:
			return ask
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for %s ask", label)
			return event.Ask{}
		}
	}

	tabA := newHarness("tab-a")
	tabB := newHarness("tab-b")
	defer func() {
		tabA.cancel()
		tabB.cancel()
		<-tabA.done
		<-tabB.done
		tabA.ctrl.Close()
		tabB.ctrl.Close()
	}()
	waitAsk("initial tab-a", tabA.events)
	waitAsk("initial tab-b", tabB.events)

	app := NewApp()
	app.tabs = map[string]*WorkspaceTab{
		"tab-a": {ID: "tab-a", Ctrl: tabA.ctrl, Ready: true},
		"tab-b": {ID: "tab-b", Ctrl: tabB.ctrl, Ready: true},
	}
	app.tabOrder = []string{"tab-a", "tab-b"}
	app.activeTabID = "tab-a"

	app.ReplayPendingPromptsForTab("tab-b")
	if got := waitAsk("replayed tab-b", tabB.events); got.ID == "" {
		t.Fatal("tab-b replay returned an empty ask")
	}
	select {
	case got := <-tabA.events:
		t.Fatalf("scoped tab-b replay also emitted tab-a ask: %+v", got)
	default:
	}
}
