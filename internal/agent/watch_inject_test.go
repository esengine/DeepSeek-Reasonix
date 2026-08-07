package agent

import (
	"context"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// watchKernel is a NavigatorKernel stub that returns canned watch events so
// the prompt-injection path can be tested without a real navigator.
type watchKernel struct {
	events []string
}

func (w watchKernel) ImplicitStateDigest() string { return "" }

func (w watchKernel) BeginAction(ctx context.Context, verb, args string) error { return nil }

func (w watchKernel) EndAction(ctx context.Context, verb, args, output string, toolErr error) (CorrectionBrief, error) {
	return CorrectionBrief{Strategy: "continue"}, nil
}

func (w watchKernel) PendingWatchEvents() []string { return w.events }

func TestPrepareSamplingRequestInjectsWatchEvents(t *testing.T) {
	sess := NewSession("sys")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "do the thing"})
	kernel := watchKernel{events: []string{"[env filesystem] modify a.go", "[env process] appear python"}}
	a := New(&fakeProvider{reply: "ok"}, tool.NewRegistry(), sess, Options{
		RecentKeep:  2,
		LongHorizon: true,
		Navigator:   kernel,
	}, event.Discard)

	prepared, err := a.prepareSamplingRequest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	msgs := prepared.req.Messages
	last := msgs[len(msgs)-1]
	if last.Role != provider.RoleUser {
		t.Fatalf("last message should be user, got %v", last.Role)
	}
	if !strings.Contains(last.Content, "Environment updates noticed since the last turn") {
		t.Errorf("user message lacks watch header: %q", last.Content)
	}
	if !strings.Contains(last.Content, "[env filesystem] modify a.go") {
		t.Errorf("user message lacks watch line: %q", last.Content)
	}
	// The session log is untouched — ephemeral injection only.
	if !strings.Contains(sess.Messages[len(sess.Messages)-1].Content, "do the thing") {
		t.Error("session message should remain unchanged")
	}
}

func TestPrepareSamplingRequestSkipsWithoutEvents(t *testing.T) {
	sess := NewSession("sys")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "hello"})
	a := New(&fakeProvider{reply: "ok"}, tool.NewRegistry(), sess, Options{
		RecentKeep:  2,
		LongHorizon: true,
		Navigator:   watchKernel{events: nil},
	}, event.Discard)
	prepared, err := a.prepareSamplingRequest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	msgs := prepared.req.Messages
	last := msgs[len(msgs)-1]
	if strings.Contains(last.Content, "Environment updates") {
		t.Errorf("no events must mean no injection, got %q", last.Content)
	}
}

func TestPrepareSamplingRequestSkipsWhenNotLongHorizon(t *testing.T) {
	sess := NewSession("sys")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "hello"})
	a := New(&fakeProvider{reply: "ok"}, tool.NewRegistry(), sess, Options{
		RecentKeep: 2,
		Navigator:  watchKernel{events: []string{"[env filesystem] modify a.go"}},
	}, event.Discard)
	prepared, err := a.prepareSamplingRequest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	msgs := prepared.req.Messages
	last := msgs[len(msgs)-1]
	if strings.Contains(last.Content, "Environment updates") {
		t.Errorf("long_horizon=false must not inject, got %q", last.Content)
	}
}
