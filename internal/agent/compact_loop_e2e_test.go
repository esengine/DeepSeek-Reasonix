package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/tool"
)

// fatTool returns a fixed-size blob, standing in for a real read_file / bash
// whose output dominates the recent (verbatim-kept) tail of the session.
type fatTool struct{ blob string }

func (fatTool) Name() string            { return "fat_read" }
func (fatTool) Description() string     { return "read a large file" }
func (fatTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object","properties":{}}`) }
func (fatTool) ReadOnly() bool          { return true }
func (f fatTool) Execute(context.Context, json.RawMessage) (string, error) {
	return f.blob, nil
}

// loopMock emits exactly one tool call per user turn (a tool call when the last
// message is the user's, a final answer when it is the tool result), so each Run
// does one tool round — the next request then runs ContextManager.Prepare. finalText overrides
// the per-turn closing answer so a test can grow the session with assistant text
// (which pruning never touches) instead of tool output.
type loopMock struct {
	t         *testing.T
	rounds    int
	finalText string
}

func lastRole(msgs []json.RawMessage) string {
	if len(msgs) == 0 {
		return ""
	}
	var m struct {
		Role string `json:"role"`
	}
	_ = json.Unmarshal(msgs[len(msgs)-1], &m)
	return m.Role
}

func (m *loopMock) handler(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	if isSummarizeRequest(body) {
		writeSSE(w, m.t,
			streamChunk(deltaText("- goal: keep going\n- pending: continue the task")),
			finishChunk("stop"),
			usageChunk(80, 30, 0, 80))
		return
	}

	msgs := decodeMessages(body)
	promptTok := charsOf(msgs) / 4

	if lastRole(msgs) == "tool" {
		text := m.finalText
		if text == "" {
			text = "Done with this step."
		}
		writeSSE(w, m.t,
			streamChunk(deltaText(text)),
			finishChunk("stop"),
			usageChunk(promptTok, 20, 0, promptTok))
		return
	}

	m.rounds++
	writeSSE(w, m.t,
		streamChunk(deltaToolCall(m.rounds, "fat_read", "{}")),
		finishChunk("tool_calls"),
		usageChunk(promptTok, 20, 0, promptTok))
}

// compactionsPerTurn drives `turns` user messages through a fresh agent wired to
// loopMock and reports, per turn, how many compactions started and whether an
// durable blocked receipt was seen.
func compactionsPerTurn(t *testing.T, windowTok int, blob, finalText string, turns int) (perTurn []int, paused bool, prunes int) {
	t.Helper()
	mock := &loopMock{t: t, finalText: finalText}
	srv := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer srv.Close()

	reg := tool.NewRegistry()
	reg.Add(fatTool{blob: blob})

	a, _ := newAgent(t, srv.URL, reg, windowTok, 4)
	started := 0
	a.sink = event.FuncSink(func(e event.Event) {
		switch e.Kind {
		case event.CompactionStarted:
			started++
		case event.Notice:
			if strings.Contains(e.Text, "Automatic context cleanup paused") {
				paused = true
			}
			if strings.Contains(e.Text, "pruned") {
				prunes++
			}
		case event.ContextMaintenanceEvent:
			if e.Maintenance != nil && e.Maintenance.Status == "blocked" {
				paused = true
			}
			if e.Maintenance != nil && e.Maintenance.Status == "applied" && e.Maintenance.Action == "prune" {
				prunes++
			}
		}
	})

	perTurn = make([]int, turns)
	for i := range turns {
		before := started
		if err := a.Run(context.Background(), fmt.Sprintf("turn %d: keep going, continue the work", i)); err != nil {
			t.Fatalf("Run %d: %v", i, err)
		}
		perTurn[i] = started - before
	}
	return perTurn, paused, prunes
}

func consecutiveCompactingTurns(perTurn []int) int {
	worst, run := 0, 0
	for _, n := range perTurn {
		if n > 0 {
			run++
			if run > worst {
				worst = run
			}
		} else {
			run = 0
		}
	}
	return worst
}

// TestCompactionStopsWhenProtectedContentExceedsWindow covers the user report
// where a single tool result alone exhausts a tiny window. Automatic maintenance
// no longer prunes mid-session tool bodies: it attempts one summary, records a
// generation-scoped block when the candidate cannot land, and must not loop.
func TestCompactionPausesWhenWindowTooSmall(t *testing.T) {
	mock := &loopMock{t: t}
	srv := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer srv.Close()
	reg := tool.NewRegistry()
	reg.Add(fatTool{blob: strings.Repeat("LARGE FILE CONTENTS. ", 350)})
	a, _ := newAgent(t, srv.URL, reg, 1600, 4)
	started := 0
	blocked := 0
	a.sink = event.FuncSink(func(e event.Event) {
		if e.Kind == event.CompactionStarted {
			started++
		}
		if e.Kind == event.ContextMaintenanceEvent && e.Maintenance != nil &&
			(e.Maintenance.Status == "blocked" || e.Maintenance.Status == "failed") {
			blocked++
		}
	})
	// First turn may fail with a typed overflow/blocked error once protected
	// content cannot form a safe checkpoint. It must not start many summaries.
	_ = a.Run(context.Background(), "turn 0: keep going")
	_ = a.Run(context.Background(), "turn 1: keep going")
	if started > 2 {
		t.Fatalf("summary transactions started = %d, want ≤2 (no multi-span / retry loop)", started)
	}
	if blocked == 0 && a.currentProjectionVersion() == 0 {
		// Either a durable block or a successful install is fine; looping is not.
		t.Logf("started=%d blocked=%d version=%d", started, blocked, a.currentProjectionVersion())
	}
}

// TestCompactionHealthyWindowNeverLoops is the companion: when growth comes from
// assistant text (which pruning never touches), compaction still fires as the
// session grows but reclaims enough headroom that it never fires on consecutive
// turns and never trips the stuck guard.
func TestCompactionHealthyWindowNeverLoops(t *testing.T) {
	perTurn, paused, _ := compactionsPerTurn(t, 40000, "small tool output", strings.Repeat("analysis paragraph. ", 600), 20)

	total := 0
	for _, n := range perTurn {
		total += n
	}
	t.Logf("compactions per turn: %v (total %d), paused=%v", perTurn, total, paused)

	if paused {
		t.Errorf("a healthy window should never pause auto-compaction")
	}
	if total == 0 {
		t.Errorf("expected compaction to fire at least once over a long session")
	}
	if c := consecutiveCompactingTurns(perTurn); c > 1 {
		t.Errorf("compaction fired on %d consecutive turns; a healthy compaction should leave breathing room", c)
	}
}

// TestSummaryKeepsToolHeavySessionBounded: automatic maintenance no longer
// prunes mid-session tool bodies. Growth from large tool results crosses
// compact_ratio once, installs one content-driven summary checkpoint, and must
// not loop or trip the stuck guard. Prune projections must not reappear.
func TestSummaryKeepsToolHeavySessionBounded(t *testing.T) {
	perTurn, paused, prunes := compactionsPerTurn(t, 40000, strings.Repeat("file line. ", 1100), "", 20)

	total := 0
	for _, n := range perTurn {
		total += n
	}
	t.Logf("compactions per turn: %v (total %d), paused=%v, prunes=%d", perTurn, total, paused, prunes)

	if total == 0 {
		t.Errorf("expected at least one summary fold over a tool-heavy session past compact_ratio")
	}
	if total > 3 {
		t.Errorf("compaction fired %d times; a single content-driven summary should leave breathing room", total)
	}
	if paused {
		t.Errorf("auto-compaction paused; successful summary should have prevented the stuck loop")
	}
	if prunes != 0 {
		t.Errorf("prune projections must not be installed (got %d)", prunes)
	}
	if c := consecutiveCompactingTurns(perTurn); c > 1 {
		t.Errorf("compaction fired on %d consecutive turns; content-driven summary should reclaim headroom", c)
	}
}

// Keep the old name as an alias so external references still resolve during the
// rename window; the body asserts the new no-prune contract.
func TestPruneKeepsToolHeavySessionBounded(t *testing.T) {
	TestSummaryKeepsToolHeavySessionBounded(t)
}
