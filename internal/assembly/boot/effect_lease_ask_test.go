package boot

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/session/control"
)

const leaseAskQuestions = `{"questions":[{"header":"h","question":"which?","options":[{"label":"a"},{"label":"b"}]}]}`

// leaseAskProvider writes a file, asks the user, then finishes. holding
// records whether the session's claim was held when the answer reached the
// model.
type leaseAskProvider struct {
	path    string
	noAsk   bool
	holding func() bool

	mu      sync.Mutex
	heldAt  bool
	reached bool
}

func (*leaseAskProvider) Name() string { return "lease-ask-effect" }

func (p *leaseAskProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	results := 0
	for _, m := range req.Messages {
		if m.Role == provider.RoleTool {
			results++
		}
	}
	ch := make(chan provider.Chunk, 2)
	switch results {
	case 0:
		args, _ := json.Marshal(map[string]string{"path": p.path, "content": "fixture\n"})
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "w", Name: "write_file", Arguments: string(args)}}
	case 1:
		if p.noAsk {
			p.mu.Lock()
			p.reached = true
			p.mu.Unlock()
			ch <- provider.Chunk{Type: provider.ChunkText, Text: "done"}
			break
		}
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "q", Name: "ask", Arguments: leaseAskQuestions}}
	default:
		p.mu.Lock()
		p.reached, p.heldAt = true, p.holding != nil && p.holding()
		p.mu.Unlock()
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "done"}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

type askSink struct{ asked chan event.Ask }

func (s askSink) Emit(e event.Event) {
	if e.Kind == event.AskRequest {
		select {
		case s.asked <- e.Ask:
		default:
		}
	}
}

func TestEffectLeaseIsFreeWhileTheTurnWaitsOnAnAsk(t *testing.T) {
	t.Parallel()
	home := statedBootHome(t)
	root := testenv.TempDir(t)
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "reasonix.toml", `
default_model = "test-model"
[codegraph]
enabled = false
[[providers]]
name = "test-model"
kind = "lease-ask-effect"
model = "x"
`)
	approveWorkspaceAt(t, home, root)
	first := &leaseAskProvider{path: "shared.txt"}
	second := &leaseAskProvider{path: "shared.txt", noAsk: true}
	current := first
	provider.Register("lease-ask-effect", func(provider.Config) (provider.Provider, error) { return current, nil })
	asked := askSink{asked: make(chan event.Ask, 4)}
	build := func(sink event.Sink) *control.Controller {
		c, err := Build(context.Background(), Options{Home: home, WorkspaceRoot: root, Sink: sink})
		if err != nil {
			t.Fatal(err)
		}
		c.EnableInteractiveApproval()
		c.SetToolApprovalMode(control.ToolApprovalYolo)
		t.Cleanup(func() { c.Close() })
		return c
	}
	a := build(asked)
	first.holding = func() bool { return a.WorkspaceLeaseState().Acquired }
	current = second
	b := build(event.Discard)

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	doneA := make(chan struct{})
	go func() { defer close(doneA); _ = a.Run(ctx, "write then ask") }()

	var ask event.Ask
	select {
	case ask = <-asked.asked:
	case <-ctx.Done():
		t.Fatal("the first session never asked")
	}
	if a.WorkspaceLeaseState().Acquired {
		t.Fatal("the session kept the workspace write claim while waiting on the user")
	}
	bctx, bcancel := context.WithTimeout(ctx, 5*time.Second)
	defer bcancel()
	err := b.Run(bctx, "write the same file")
	second.mu.Lock()
	wrote := second.reached
	second.mu.Unlock()
	if !wrote {
		t.Fatalf("another session could not write the same path during the ask: %v", err)
	}

	a.AnswerQuestion(ask.ID, []event.AskAnswer{{QuestionID: ask.Questions[0].ID, Selected: []string{"a"}}})
	select {
	case <-doneA:
	case <-ctx.Done():
		t.Fatal("the first session never finished after being answered")
	}
	first.mu.Lock()
	defer first.mu.Unlock()
	if !first.reached || !first.heldAt {
		t.Fatalf("answer reached the model with the claim held = %v (reached %v), want it taken again first", first.heldAt, first.reached)
	}
}
