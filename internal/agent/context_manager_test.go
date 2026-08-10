package agent

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

type failingSummaryProvider struct{ calls int }

func (p *failingSummaryProvider) Name() string { return "failing-summary" }

func (p *failingSummaryProvider) Stream(context.Context, provider.Request) (<-chan provider.Chunk, error) {
	p.calls++
	ch := make(chan provider.Chunk, 1)
	ch <- provider.Chunk{Type: provider.ChunkError, Err: errors.New("summary unavailable")}
	close(ch)
	return ch, nil
}

func TestContextManagerPersistsAndRestoresBlockedFailureFingerprint(t *testing.T) {
	messages := []provider.Message{
		{Role: provider.RoleSystem, Content: "system"},
		{Role: provider.RoleUser, Content: strings.Repeat("old task ", 500)},
		{Role: provider.RoleAssistant, Content: strings.Repeat("old work ", 500)},
		{Role: provider.RoleUser, Content: "current"},
		{Role: provider.RoleAssistant, Content: "tail"},
	}
	path := filepath.Join(t.TempDir(), "session.jsonl")
	newAgent := func(p *failingSummaryProvider) *Agent {
		a := New(p, tool.NewRegistry(), &Session{Messages: append([]provider.Message(nil), messages...)}, Options{
			ContextWindow: 100, RecentKeep: 2, WorkspaceID: "workspace", ModelRef: "model",
		}, event.Discard)
		a.BindSessionPath(path, true)
		return a
	}

	firstProvider := &failingSummaryProvider{}
	first := newAgent(firstProvider)
	policy := ContextPreparePolicy{Trigger: CompactionTriggerPressure, ObservedInputTokens: 80}
	if _, err := first.contextManager().Prepare(context.Background(), policy); err != nil {
		t.Fatalf("soft-threshold failure should persist blocked state without rejecting this request: %v", err)
	}
	if firstProvider.calls != 2 { // summarizeWithRetry makes two bounded attempts
		t.Fatalf("summary calls = %d, want 2", firstProvider.calls)
	}
	if first.compactionState.BlockedInputHash == "" {
		t.Fatal("failed summary did not persist a blocked input hash")
	}
	if _, err := first.contextManager().Prepare(context.Background(), policy); err != nil {
		t.Fatal(err)
	}
	if firstProvider.calls != 2 {
		t.Fatalf("same in-memory fingerprint retried summary: calls=%d", firstProvider.calls)
	}

	resumedProvider := &failingSummaryProvider{}
	resumed := newAgent(resumedProvider)
	if resumed.compactionState.BlockedInputHash == "" {
		t.Fatal("blocked-only v3 sidecar was not restored")
	}
	if _, err := resumed.contextManager().Prepare(context.Background(), policy); err != nil {
		t.Fatal(err)
	}
	if resumedProvider.calls != 0 {
		t.Fatalf("resumed blocked fingerprint retried summary %d times", resumedProvider.calls)
	}
}

func TestStrictAlternatingRolesStillConvergesBeforeSampling(t *testing.T) {
	sess := &Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "system"},
		{Role: provider.RoleUser, Content: "old request"},
		{Role: provider.RoleAssistant, Content: strings.Repeat("old work ", 400)},
		{Role: provider.RoleUser, Content: "recent request"},
		{Role: provider.RoleAssistant, Content: "recent response"},
	}}
	a := New(&fakeProvider{reply: "old work summarized"}, tool.NewRegistry(), sess, Options{
		ContextWindow: 200, RecentKeep: 2, StrictAlternatingRoles: true,
	}, event.Discard)

	prepared, err := a.prepareSamplingRequest(context.Background())
	if err != nil {
		t.Fatalf("prepareSamplingRequest: %v", err)
	}
	if got := a.currentProjectionVersion(); got != 1 {
		t.Fatalf("projection version = %d, want pressure fold", got)
	}
	if len(prepared.req.Messages) >= len(sess.Snapshot()) {
		t.Fatalf("strict request did not converge: %+v", prepared.req.Messages)
	}
	for i := 1; i < len(prepared.req.Messages); i++ {
		if prepared.req.Messages[i-1].Role == prepared.req.Messages[i].Role {
			t.Fatalf("strict request has adjacent %s roles: %+v", prepared.req.Messages[i].Role, prepared.req.Messages)
		}
	}
}
