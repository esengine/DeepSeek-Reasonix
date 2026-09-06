package control

import (
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/agent/testutil"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

type manualProtocolProvider struct {
	*testutil.MockProvider
	entered, release chan struct{}
	calls            atomic.Int32
}

func (p *manualProtocolProvider) RequiresAssistantReasoning() bool { return true }
func (p *manualProtocolProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	if p.calls.Add(1) == 2 && p.entered != nil {
		close(p.entered)
		<-p.release
	}
	return p.MockProvider.Stream(ctx, req)
}
func TestProtocolRecoveryControllerDurabilityAndConcurrentAdmission(t *testing.T) {
	p := &manualProtocolProvider{MockProvider: testutil.NewMock("strict", testutil.ErrorTurn(&provider.APIError{Status: 400, Body: `{"model":"deepseek"}`}), testutil.Turn{Text: "done"}), entered: make(chan struct{}), release: make(chan struct{})}
	session := agent.NewSession("system")
	session.Add(provider.Message{Role: provider.RoleAssistant, Content: "earlier", ReasoningContent: "proof"})
	a := agent.New(p, tool.NewRegistry(), session, agent.Options{}, event.Discard)
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	c := New(Options{Runner: a, Executor: a, SessionDir: dir, SessionPath: path, Sink: event.Discard})
	defer c.Close()
	if err := c.RunTurn(context.Background(), "next"); err == nil {
		t.Fatal("expected opaque failure")
	}
	action := c.PendingProtocolRecovery()
	if action == nil {
		t.Fatal("missing recovery token")
	}
	loaded, err := agent.LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	var pending bool
	for _, m := range loaded.Snapshot() {
		r, ok := provider.DecodeProtocolRecovery(m.ProtocolRecovery)
		pending = pending || ok && r.State == "pending"
	}
	if !pending {
		t.Fatal("pending not persisted")
	}
	done := make(chan error, 1)
	go func() { done <- c.RunProtocolRecoveryWithAdmission(context.Background(), action.ID, "", nil) }()
	<-p.entered
	loaded, err = agent.LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	var consumed bool
	for _, m := range loaded.Snapshot() {
		r, ok := provider.DecodeProtocolRecovery(m.ProtocolRecovery)
		consumed = consumed || ok && r.State == "consumed"
	}
	if !consumed {
		t.Fatal("request started before durable consumption")
	}
	if err := c.RunProtocolRecoveryWithAdmission(context.Background(), action.ID, "", nil); err == nil {
		t.Fatal("concurrent duplicate admitted")
	}
	close(p.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := c.RunProtocolRecoveryWithAdmission(context.Background(), action.ID, "", nil); !errors.Is(err, agent.ErrProtocolRecoveryUnavailable) {
		t.Fatalf("duplicate=%v", err)
	}
	if p.calls.Load() != 2 {
		t.Fatal("duplicate provider invocation")
	}
}
func TestParseProtocolRecoveryCommand(t *testing.T) {
	id, guidance, ok := ParseProtocolRecoveryCommand("/recover-context token keep completed work")
	if !ok || id != "token" || guidance != "keep completed work" {
		t.Fatalf("%q %q %v", id, guidance, ok)
	}
	if _, _, ok := ParseProtocolRecoveryCommand("/recover-contextual"); ok {
		t.Fatal("ambiguous command accepted")
	}
}

func TestProtocolRecoveryCancelledBeforeAdmissionKeepsToken(t *testing.T) {
	p := &manualProtocolProvider{MockProvider: testutil.NewMock("strict", testutil.ErrorTurn(&provider.APIError{Status: 400, Body: `{"model":"deepseek"}`}))}
	session := agent.NewSession("system")
	session.Add(provider.Message{Role: provider.RoleAssistant, Content: "earlier", ReasoningContent: "proof"})
	a := agent.New(p, tool.NewRegistry(), session, agent.Options{}, event.Discard)
	c := New(Options{Runner: a, Executor: a, Sink: event.Discard})
	defer c.Close()
	_ = c.RunTurn(context.Background(), "next")
	action := c.PendingProtocolRecovery()
	if action == nil {
		t.Fatal("no token")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_ = c.RunProtocolRecoveryWithAdmission(ctx, action.ID, "", nil)
	if p.calls.Load() != 1 || c.PendingProtocolRecovery() == nil {
		t.Fatal("cancelled preparation consumed action or called provider")
	}
}
