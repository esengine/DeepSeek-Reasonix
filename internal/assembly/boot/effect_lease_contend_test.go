package boot

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/session/control"
)

// noticeProvider is leaseAskProvider that also keeps every user-role message
// the model saw once the ask was answered.
type noticeProvider struct {
	leaseAskProvider
	seenMu sync.Mutex
	seen   []string
}

func (p *noticeProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.seenMu.Lock()
	for _, m := range req.Messages {
		if m.Role == provider.RoleUser {
			p.seen = append(p.seen, m.Content)
		}
	}
	p.seenMu.Unlock()
	return p.leaseAskProvider.Stream(ctx, req)
}

type receiptSink struct {
	askSink
	receipts chan *event.CompletionReceipt
}

func (s receiptSink) Emit(e event.Event) {
	s.askSink.Emit(e)
	if e.Kind == event.TurnDone {
		select {
		case s.receipts <- e.Receipt:
		default:
		}
	}
}

func TestEffectContendedYieldTellsTheModelAndTheReceipt(t *testing.T) {
	t.Parallel()
	home := statedBootHome(t)
	root := testenv.TempDir(t)
	initEffectRepository(t, root)
	writeFile(t, root, "reasonix.toml", `
default_model = "test-model"
[codegraph]
enabled = false
[[providers]]
name = "test-model"
kind = "lease-contend-effect"
model = "x"
`)
	approveWorkspaceAt(t, home, root)
	first := &noticeProvider{leaseAskProvider: leaseAskProvider{path: "shared.txt"}}
	second := &leaseAskProvider{path: "shared.txt", noAsk: true}
	var current provider.Provider = first
	provider.Register("lease-contend-effect", func(provider.Config) (provider.Provider, error) { return current, nil })
	sink := receiptSink{askSink: askSink{asked: make(chan event.Ask, 4)}, receipts: make(chan *event.CompletionReceipt, 4)}
	build := func(s event.Sink) *control.Controller {
		c, err := Build(context.Background(), Options{Home: home, WorkspaceRoot: root, Sink: s})
		if err != nil {
			t.Fatal(err)
		}
		c.EnableInteractiveApproval()
		c.SetToolApprovalMode(control.ToolApprovalYolo)
		t.Cleanup(func() { c.Close() })
		return c
	}
	a := build(sink)
	current = second
	b := build(event.Discard)

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	a.Submit("write then ask")
	var ask event.Ask
	select {
	case ask = <-sink.asked:
	case <-ctx.Done():
		t.Fatal("the first session never asked")
	}
	err := b.Run(ctx, "write the same file")
	second.mu.Lock()
	wrote := second.reached
	second.mu.Unlock()
	if !wrote {
		t.Fatalf("the second session could not write during the ask: %v", err)
	}
	a.AnswerQuestion(ask.ID, []event.AskAnswer{{QuestionID: ask.Questions[0].ID, Selected: []string{"a"}}})

	select {
	case r := <-sink.receipts:
		if r == nil {
			t.Fatal("the turn ended with no receipt")
		}
		found := false
		for _, g := range r.Gaps {
			found = found || g.Kind == "workspace_claim_yielded"
		}
		if !found {
			t.Fatalf("receipt gaps = %+v, want workspace_claim_yielded", r.Gaps)
		}
	case <-ctx.Done():
		t.Fatal("the first session's turn never finished")
	}
	first.seenMu.Lock()
	notice := strings.Join(first.seen, "\n")
	first.seenMu.Unlock()
	if !strings.Contains(notice, "workspace.claim_contended") {
		t.Fatalf("the model was not told another session claimed its paths; user-role messages:\n%s", notice)
	}
}
