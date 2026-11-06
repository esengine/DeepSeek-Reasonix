package agent

import (
	"context"
	"reasonix/internal/state/sessionstore"
	"reflect"
	"strings"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
)

func TestSummarizeAtProjectionBoundaryReportsFoldedAnchor(t *testing.T) {
	sess := &sessionstore.Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "old boundary"},
		{Role: provider.RoleAssistant, Content: strings.Repeat("old work ", 200)},
		{Role: provider.RoleUser, Content: "retained boundary"},
		{Role: provider.RoleAssistant, Content: "retained answer"},
	}}
	before := sess.Snapshot()
	a := New(&fakeProvider{reply: "old work summarized"}, tool.NewRegistry(), sess, Options{}, event.Discard)

	if err := a.SummarizeUpTo(context.Background(), 3); err != nil {
		t.Fatalf("initial SummarizeUpTo: %v", err)
	}
	version := a.sess.win.compactionState.Projection.ProjectionVersion
	err := a.SummarizeFrom(context.Background(), 1)
	if err == nil || !strings.Contains(err.Error(), "no longer present in the model context") {
		t.Fatalf("folded-boundary error = %v", err)
	}
	if a.sess.win.compactionState.Projection.ProjectionVersion != version {
		t.Fatal("folded-boundary retry replaced the projection")
	}
	if !reflect.DeepEqual(sess.Snapshot(), before) {
		t.Fatal("folded-boundary retry changed canonical history")
	}
}

func TestSummarizeAtProjectionBoundaryReportsNoSavings(t *testing.T) {
	sess := &sessionstore.Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "tiny"},
		{Role: provider.RoleUser, Content: "keep boundary"},
	}}
	a := New(&fakeProvider{reply: strings.Repeat("long summary ", 30)}, tool.NewRegistry(), sess, Options{}, event.Discard)

	err := a.SummarizeUpTo(context.Background(), 2)
	if err == nil || !strings.Contains(err.Error(), "compressed context would not be smaller") {
		t.Fatalf("no-savings error = %v", err)
	}
	if len(a.sess.win.compactionState.Projection.Messages) != 0 {
		t.Fatal("no-savings positional compression installed a projection")
	}
}
