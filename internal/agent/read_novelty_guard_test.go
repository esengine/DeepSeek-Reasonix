package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// grepNoveltyProbe mirrors the real grep tool's receipt shape: read-only,
// path-scoped, and carrying its question in an argument rather than the path.
type grepNoveltyProbe struct{}

func (grepNoveltyProbe) Name() string        { return "grep" }
func (grepNoveltyProbe) Description() string { return "search" }
func (grepNoveltyProbe) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"pattern":{"type":"string"},"path":{"type":"string"}},"required":["pattern"]}`)
}
func (grepNoveltyProbe) ReadOnly() bool { return true }
func (grepNoveltyProbe) Execute(context.Context, json.RawMessage) (string, error) {
	return "internal/agent/agent.go:120: match\n", nil
}

// runNoveltyRounds executes one call per round and returns the guard text each
// round received, indexed from round 1.
func runNoveltyRounds(t *testing.T, calls []provider.ToolCall) []string {
	t.Helper()
	reg := tool.NewRegistry()
	reg.Add(readProbe{})
	reg.Add(grepNoveltyProbe{})
	a := New(nil, reg, NewSession(""), Options{}, event.Discard)
	a.resetTurnEvidence()

	fired := make([]string, len(calls)+1)
	for i, call := range calls {
		batch := a.executeBatch(context.Background(), &a.turn, []provider.ToolCall{call})
		if out := batch.results[0]; strings.Contains(out, "[progress guard]") {
			fired[i+1] = out[strings.Index(out, "[progress guard]"):]
		}
	}
	return fired
}

func assertNoStopTier(t *testing.T, fired []string, what string) {
	t.Helper()
	for round, text := range fired {
		if strings.Contains(text, "produce your final answer now") {
			t.Errorf("%s: round %d was told it produced no new evidence and ordered to answer", what, round)
		}
	}
}

// Ten searches of one package, a different pattern each time. Every round
// returns information the turn did not have; keying novelty on the path alone
// scored nine of them as repeats and stopped the turn at round 7.
func TestGrepWithNewPatternsNoLongerTripsTheGuard(t *testing.T) {
	var calls []provider.ToolCall
	for i, p := range []string{"Compose", "Receipt", "Ledger", "progressGuard", "applyEBM", "Delivery", "ToolCall", "Session", "compact", "readiness"} {
		calls = append(calls, provider.ToolCall{
			ID: fmt.Sprintf("g%d", i), Name: "grep",
			Arguments: fmt.Sprintf(`{"pattern":%q,"path":"internal/agent"}`, p),
		})
	}
	assertNoStopTier(t, runNoveltyRounds(t, calls), "ten distinct searches")
}

// Paging through one long file: distinct windows, one path.
func TestPagingOneFileNoLongerTripsTheGuard(t *testing.T) {
	var calls []provider.ToolCall
	for i := range 10 {
		calls = append(calls, provider.ToolCall{
			ID: fmt.Sprintf("r%d", i), Name: "read_file",
			Arguments: fmt.Sprintf(`{"path":"internal/agent/agent.go","offset":%d,"limit":300}`, i*300),
		})
	}
	assertNoStopTier(t, runNoveltyRounds(t, calls), "paging one file")
}

// The discriminator stays intact: re-asking the identical question is still a
// repeat, and the ladder still escalates on it.
func TestIdenticalReadsStillEscalate(t *testing.T) {
	var calls []provider.ToolCall
	for i := range progressStopStreak + 2 {
		calls = append(calls, provider.ToolCall{
			ID: fmt.Sprintf("s%d", i), Name: "read_file", Arguments: `{"path":"same.go"}`,
		})
	}
	fired := runNoveltyRounds(t, calls)
	stopped := false
	for _, text := range fired {
		if strings.Contains(text, "produce your final answer now") {
			stopped = true
		}
	}
	if !stopped {
		t.Fatal("re-reading one path must still reach the stop tier")
	}
}
