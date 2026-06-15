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
// does one tool round — the step that triggers maybeCompact. finalText overrides
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
// auto-compaction-paused notice was seen.
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
			if strings.Contains(e.Text, "Auto-compaction paused") {
				paused = true
			}
			if strings.Contains(e.Text, "pruned") {
				prunes++
			}
		}
	})

	perTurn = make([]int, turns)
	for i := 0; i < turns; i++ {
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

// TestCompactionPausesWhenWindowTooSmall covers the user report: a tool output
// that alone exceeds the trigger used to make every "continue" turn re-compact
// forever. The stuck guard now caps it — at most two compactions, then a paused
// notice — instead of looping turn after turn.
func TestCompactionPausesWhenWindowTooSmall(t *testing.T) {
	// One fat_read result (~1750 tok) exceeds the 0.8×1600 trigger on its own.
	perTurn, paused, _ := compactionsPerTurn(t, 1600, strings.Repeat("LARGE FILE CONTENTS. ", 350), "", 8)

	total := 0
	for _, n := range perTurn {
		total += n
	}
	t.Logf("compactions per turn: %v (total %d), paused=%v", perTurn, total, paused)

	if total > 2 {
		t.Errorf("compacted %d times; the stuck guard should cap it at ≤2, not loop", total)
	}
	if !paused {
		t.Errorf("expected an auto-compaction-paused notice")
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

// TestPruneKeepsToolHeavySessionBounded: when growth comes from tool results,
// pruning alone keeps the prompt under the trigger for the whole session — the
// paid summarize call never happens and the stuck guard never trips. 20 turns of
// ~3k-token blobs would otherwise cross 0.8×40000 around turn 11.
func TestPruneKeepsToolHeavySessionBounded(t *testing.T) {
	perTurn, paused, prunes := compactionsPerTurn(t, 40000, strings.Repeat("file line. ", 1100), "", 20)

	total := 0
	for _, n := range perTurn {
		total += n
	}
	t.Logf("compactions per turn: %v (total %d), paused=%v, prunes=%d", perTurn, total, paused, prunes)

	if total != 0 {
		t.Errorf("compaction fired %d times; pruning should keep a tool-heavy session bounded without folding", total)
	}
	if paused {
		t.Errorf("auto-compaction paused; pruning should have prevented the stuck loop entirely")
	}
	if prunes == 0 {
		t.Errorf("expected at least one prune pass over a tool-heavy session")
	}
}
