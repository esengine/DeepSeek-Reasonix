package agent

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"reasonix/internal/state/sessionstore"
	"strings"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/model/openai"
)

// oneTurnMock keeps a single turn going for rounds tool calls before answering,
// so the context passes the trigger without a second user message arriving.
type oneTurnMock struct {
	t      *testing.T
	rounds int
	seen   int
	peak   int
}

func (m *oneTurnMock) handler(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	if isSummarizeRequest(body) {
		writeSSE(w, m.t, streamChunk(deltaText("## Digest\n- digest")), finishChunk("stop"), usageChunk(80, 30, 0, 80))
		return
	}
	promptTok := charsOf(decodeMessages(body)) / 4
	m.peak = max(m.peak, promptTok)
	if m.seen >= m.rounds {
		writeSSE(w, m.t, streamChunk(deltaText("done")), finishChunk("stop"), usageChunk(promptTok, 20, 0, promptTok))
		return
	}
	m.seen++
	writeSSE(w, m.t, streamChunk(deltaToolCall(m.seen, "fat_read", "{}")), finishChunk("tool_calls"), usageChunk(promptTok, 20, 0, promptTok))
}

// A verbatim tail wider than the trigger folds nothing, forever: the session
// crosses the threshold with every message still held, so maintenance runs each
// round and finds an empty fold region. It reached production as "a low
// compact_ratio disables compaction outright" — on a 1M window every ratio
// under ~0.19 put the trigger below the 96K tail.
func TestTailBudgetYieldsToLowTrigger(t *testing.T) {
	for _, tc := range []struct {
		name   string
		window int
		ratio  float64
	}{
		{"tail below trigger", 20000, 0.25},
		{"wide window, low ratio", 1000000, 0.005},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mock := &oneTurnMock{t: t, rounds: 12}
			srv := httptest.NewServer(http.HandlerFunc(mock.handler))
			defer srv.Close()
			reg := tool.NewRegistry()
			reg.Add(fatTool{blob: strings.Repeat("FILE CONTENTS LINE. ", 1000)})
			prov, err := openai.New(provider.Config{
				Name: "deepseek", BaseURL: srv.URL, Model: "deepseek-reasoner", APIKey: "test",
				Extra: map[string]any{"api_key_env": "DEEPSEEK_API_KEY"},
			})
			if err != nil {
				t.Fatalf("provider: %v", err)
			}
			a := New(prov, reg, sessionstore.NewSession(systemPrompt), Options{
				Temperature: 0, ContextWindow: tc.window, CompactRatio: tc.ratio,
				RecentKeep: 4, MaxSteps: 40,
			}, &collectSink{})
			started := 0
			a.svc.sink = event.FuncSink(func(e event.Event) {
				if e.Kind == event.CompactionStarted {
					started++
				}
			})
			if err := a.Run(context.Background(), "one turn: keep reading until done"); err != nil {
				t.Fatalf("run: %v", err)
			}
			if trigger, tail := a.window().compactTrigger(), a.window().recentTailBudget(); tail > trigger/2 {
				t.Fatalf("tail %d leaves no fold region under trigger %d", tail, trigger)
			}
			if mock.peak <= a.window().compactTrigger() {
				t.Fatalf("peak prompt %d never reached trigger %d; the case proves nothing", mock.peak, a.window().compactTrigger())
			}
			if started == 0 {
				t.Fatalf("context passed the trigger (peak %d > %d) but nothing folded", mock.peak, a.window().compactTrigger())
			}
		})
	}
}
