package boot

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
)

// issuerProvider answers with one model tool call, then finishes. It also
// reports a call the provider ran on its own side, which arrives as a result
// with no dispatch — that is the call's shape, not a hole in the record.
type issuerProvider struct {
	mu           sync.Mutex
	round        int
	providerSide bool
}

func (p *issuerProvider) Name() string { return "boot-tool-issuer" }

func (p *issuerProvider) Stream(context.Context, provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	i := p.round
	p.round++
	p.mu.Unlock()
	ch := make(chan provider.Chunk, 4)
	if i == 0 {
		if p.providerSide {
			ch <- provider.Chunk{Type: provider.ChunkProviderTool, Text: "two results",
				ToolCall: &provider.ToolCall{ID: "srv-1", Name: "web_search", Arguments: `{"q":"x"}`}}
		}
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{
			ID: "call-1", Name: "todo_write",
			Arguments: `{"todos":[{"content":"first","status":"in_progress"},{"content":"second","status":"pending"}]}`,
		}}
	} else {
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "done"}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

// issuerSink keeps every tool frame in arrival order, which is what a durable
// log holds and what a fold reads back.
type issuerSink struct {
	mu     sync.Mutex
	frames []event.Tool
}

func (s *issuerSink) Emit(e event.Event) {
	if e.Kind != event.ToolDispatch && e.Kind != event.ToolResult {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.frames = append(s.frames, e.Tool)
}

func (s *issuerSink) tools() []event.Tool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]event.Tool(nil), s.frames...)
}

func runIssuerTurn(t *testing.T, providerSide bool) []event.Tool {
	t.Helper()
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	// The kind registry is global and refuses a duplicate, so each test owns one.
	name := "boot-tool-issuer-" + t.Name()
	provider.Register(name, func(provider.Config) (provider.Provider, error) {
		return &issuerProvider{providerSide: providerSide}, nil
	})
	writeFile(t, dir, "reasonix.toml", fmt.Sprintf(`
default_model = "test-model"

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = %q
model = "x"
`, name))
	approveWorkspace(t, dir)
	sink := &issuerSink{}
	ctrl, err := Build(context.Background(), Options{Sink: sink})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	if err := ctrl.Run(context.Background(), "go"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	return sink.tools()
}

// Nothing downstream can re-derive who asked for a call: a host-minted one and
// a model one are the same shape on the wire. So the field has to survive the
// real assembly, not just the struct it is declared on.
func TestIssuerReachesTheSinkOnEveryToolFrame(t *testing.T) {
	frames := runIssuerTurn(t, false)
	if len(frames) == 0 {
		t.Fatal("the turn produced no tool frames")
	}
	for _, f := range frames {
		if f.Issuer == "" {
			t.Errorf("tool frame %q (%s) reached the sink with no issuer", f.ID, f.Name)
		}
	}
}

// A call is one thing across its lifetime. If its dispatch and its result
// disagree about who asked for it, a fold has to pick one, and whichever it
// picks is wrong half the time.
func TestIssuerIsStableAcrossACallsLifetime(t *testing.T) {
	seen := map[string]event.ToolIssuer{}
	for _, f := range runIssuerTurn(t, false) {
		if f.ID == "" {
			continue
		}
		if prior, ok := seen[f.ID]; ok && prior != f.Issuer {
			t.Fatalf("call %q: issuer changed %q -> %q across its frames", f.ID, prior, f.Issuer)
		}
		seen[f.ID] = f.Issuer
	}
	if seen["call-1"] != event.IssuedByModel {
		t.Fatalf("a call the model asked for = %q, want %q", seen["call-1"], event.IssuedByModel)
	}
}

// A provider-executed call reports a result and never a dispatch. Establishing
// it must not require inventing the dispatch it never had — and the absence of
// one must not be what says who ran it.
func TestProviderSideCallIsEstablishedByItsResultAlone(t *testing.T) {
	var dispatches, results int
	var issuer event.ToolIssuer
	var attempt string
	for _, f := range runIssuerTurn(t, true) {
		if f.ID != "srv-1" {
			continue
		}
		issuer, attempt = f.Issuer, f.AttemptID
		if f.Output != "" || f.Err != "" {
			results++
		} else {
			dispatches++
		}
	}
	if results == 0 {
		t.Fatal("the provider-side call produced no result frame")
	}
	if dispatches != 0 {
		t.Fatalf("the provider-side call produced %d dispatch frames; it has none to produce", dispatches)
	}
	if issuer != event.IssuedByProvider {
		t.Fatalf("a call the provider ran = %q, want %q", issuer, event.IssuedByProvider)
	}
	// The counterexample that keeps the two axes apart: this frame carries an
	// attempt id and was not issued by the model. Reading provenance off the
	// attempt id would call it the model's work.
	if attempt == "" {
		t.Fatal("the provider-side result carried no attempt id, so it cannot pin that " +
			"an attempt id does not mean the model issued the call")
	}
}
