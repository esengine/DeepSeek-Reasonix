package boot

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/state/sessionstore"
)

const failedSummaryWindow = 20000

// refusingRelay answers every summary request with a 400 and refuses a main
// request larger than its window with a prose-only 400, the way a relay that
// flattens upstream errors does.
type refusingRelay struct {
	mu        sync.Mutex
	sent      []int
	summaries int
}

func (r *refusingRelay) last() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.sent[len(r.sent)-1]
}

func (r *refusingRelay) Name() string { return "boot-refusing-relay" }

func (r *refusingRelay) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(req.Messages) > 0 && strings.Contains(req.Messages[0].Content, "compacting the earlier part") {
		r.summaries++
		return nil, &provider.APIError{Provider: "relay", Status: 400, Body: `{"error":{"message":"summarizer unavailable"}}`}
	}
	chars := len(req.Messages[0].Content)
	for _, m := range req.Messages[1:] {
		chars += len(m.Content)
	}
	tools, _ := json.Marshal(req.Tools)
	tokens := (chars + len(tools)) / 4
	r.sent = append(r.sent, tokens)
	if tokens > failedSummaryWindow {
		return nil, &provider.APIError{Provider: "relay", Status: 400, Body: `{"error":{"message":"upstream rejected the request"}}`}
	}
	ch := make(chan provider.Chunk, 2)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: "ok"}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

// After an automatic summary fails, no later request may exceed the window:
// the retry hold gives way before the ceiling, and a refusal the relay words
// without a code is still answered by folding.
func TestEffectFailedSummaryNeverLetsARequestPastTheWindow(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	relay := &refusingRelay{}
	kind := "boot-refusing-relay-" + t.Name()
	provider.Register(kind, func(provider.Config) (provider.Provider, error) { return relay, nil })
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "`+kind+`"
model = "x"
context_window = 20000
`)
	approveWorkspace(t, dir)

	ctrl, err := Build(context.Background(), Options{Sink: event.Discard})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	ctrl.SetSessionPath(sessionstore.NewSessionPath(ctrl.SessionDir(), ctrl.Label()))

	turn := func(i, tokens int) {
		t.Helper()
		if err := ctrl.Run(context.Background(), "go "+strings.Repeat("log line ", tokens*4/9)); err != nil {
			t.Fatalf("turn %d (requests so far %v, summaries %d): %v", i, relay.sent, relay.summaries, err)
		}
	}
	for i := 0; i == 0 || relay.last() < 12000; i++ {
		turn(i, 2500)
	}
	// Land the first request past the trigger by about 8%, where a failed
	// summary used to hold the retry until the window itself.
	const landing = 18400
	turn(90, landing-relay.last())
	turn(91, 1400)
	for i := 6; i < 10; i++ {
		turn(i, 900)
	}
	if relay.summaries == 0 {
		t.Fatal("no summary was ever attempted; the fixture never reached the trigger")
	}
	for i, tokens := range relay.sent {
		if tokens > failedSummaryWindow {
			t.Errorf("request %d carried ~%d tokens into a %d-token window (summaries attempted: %d)", i, tokens, failedSummaryWindow, relay.summaries)
		}
	}
}
