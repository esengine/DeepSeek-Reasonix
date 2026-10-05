package serve

import (
	"context"
	"reasonix/internal/state/sessionstore"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/safety/permission"
	"reasonix/internal/session/control"
)

// askingProvider calls the ask tool once and then finishes.
type askingProvider struct{ turn int }

func (*askingProvider) Name() string { return "asking" }

func (p *askingProvider) Stream(context.Context, provider.Request) (<-chan provider.Chunk, error) {
	turn := p.turn
	p.turn++
	ch := make(chan provider.Chunk, 2)
	if turn == 0 {
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{
			ID: "ask-1", Name: "ask",
			Arguments: `{"questions":[{"header":"Lib","question":"Which one?","options":[{"label":"A"},{"label":"B"}]}]}`,
		}}
	} else {
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "done"}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

// The desktop window adopted its first pane and served it through Handler,
// never through RunGracefulListener, so every ask in the conversation that pane
// carried answered itself with the headless fallback while a person sat in
// front of it. A pane opened later went through Open and worked, which read as
// "only new sessions can ask" (#9509, #9527).
func TestHubAdoptPublishesLocalRuntimeWithInteractiveWiring(t *testing.T) {
	dir := testenv.TempDir(t)
	reg := tool.NewRegistry()
	reg.Add(agent.NewAskTool())
	executor := agent.New(&askingProvider{}, reg, sessionstore.NewSession(""), agent.Options{MaxSteps: 4}, event.Discard)

	bc := NewBroadcaster()
	asked := make(chan event.Ask, 1)
	results := make(chan string, 8)
	// Read the events on the way to the broadcaster: a Frame carries them
	// encoded, and the subject here is what the kernel emitted.
	watch := event.FuncSink(func(e event.Event) {
		switch e.Kind {
		case event.AskRequest:
			asked <- e.Ask
		case event.ToolResult:
			results <- e.Tool.Output
		}
		bc.Emit(e)
	})
	ctrl := control.New(control.Options{
		Runner:     executor,
		Executor:   executor,
		Sink:       watch,
		Policy:     permission.New("ask", nil, nil, nil),
		SessionDir: dir,
	})
	srv := New(ctrl, bc, config.ServeConfig{})

	// Adopted, and served through the handler: no listener, which is the whole
	// of what the desktop host does differently.
	hub := NewHub(HubOptions{})
	rt, err := hub.Adopt(srv, bc)
	if err != nil {
		t.Fatalf("adopt: %v", err)
	}
	t.Cleanup(func() { _ = hub.Close(rt.ID) })

	go func() {
		a := <-asked
		ctrl.AnswerQuestion(a.ID, []event.AskAnswer{{QuestionID: a.Questions[0].ID, Selected: []string{"B"}}})
	}()

	if err := ctrl.RunTurn(context.Background(), "pick one"); err != nil {
		t.Fatalf("RunTurn: %v", err)
	}

	// The event is the proof the asker was reached: the fallback returns a
	// result without ever raising one.
	var out string
	for len(results) > 0 {
		if got := <-results; strings.TrimSpace(got) != "" {
			out = got
		}
	}
	if strings.Contains(out, "No interactive user answered") {
		t.Fatalf("an adopted pane answered its own question: %s", out)
	}
	if !strings.Contains(out, "B") {
		t.Fatalf("the user picked B and the model was told %q", out)
	}
}

// The remote half of the same rule: a remote pane has no Server and no
// Controller here, because the machine driving it owns those decisions.
// Publishing one must not reach for a controller that is not there.
func TestHubPublishLeavesRemoteRuntimesAlone(t *testing.T) {
	h := NewHub(HubOptions{})
	rt := &Runtime{ID: "r-remote", remote: &remoteBinding{ep: RemoteEndpoint{Addr: "127.0.0.1:1", Token: "t"}}}
	h.publish(rt) // would panic on rt.Server if the invariant ignored Local()
	if rt.handler == nil {
		t.Fatal("a published remote runtime got no handler")
	}
	if got := len(h.Runtimes()); got != 1 {
		t.Fatalf("hub holds %d runtimes, want the one published", got)
	}
}
