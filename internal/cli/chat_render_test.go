package cli

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/textarea"

	"reasonix/internal/event"
)

// newTestChatTUI builds a chatTUI with just the pieces the streaming/commit and
// completion paths need, for unit tests that don't run the bubbletea loop.
func newTestChatTUI() chatTUI {
	commit := []string{}
	ti := textarea.New()
	configureChatTextarea(&ti)
	ti.SetWidth(80)
	shellIdx := map[string]int{}
	shellOut := map[string]string{}
	shellExp := map[string]bool{}
	return chatTUI{
		input:                ti,
		width:                80,
		statusLineCount:      2,
		submittedInputCursor: -1,
		queueEditCursor:      -1,
		nextPasteID:          1,
		reasoningLineIdx:     -1,
		reasoningTextIdx:     -1,
		answerIdx:            -1,
		toolStreamIdx:        -1,
		reasoning:            &strings.Builder{},
		pending:              &strings.Builder{},
		pendingCommit:        &commit,
		renderer:             newMarkdownRenderer(80),
		shellOutputs:         shellOut,
		shellExpanded:        shellExp,
		shellTranscriptIdx:   shellIdx,
		toolLineCountByID:    map[string]int{},
	}
}

func TestCacheRateLabelKeepsTwoDecimals(t *testing.T) {
	if got := cacheRateLabel("turn hit %s", 998, 1000); got != "turn hit 99.80%" {
		t.Fatalf("cacheRateLabel = %q, want turn hit 99.80%%", got)
	}
	if got := cacheRateLabel("avg %s", 1, 3); got != "avg 33.33%" {
		t.Fatalf("cacheRateLabel = %q, want avg 33.33%%", got)
	}
	if got := cacheRateLabel("avg %s", 1, 0); got != "" {
		t.Fatalf("cacheRateLabel with zero denominator = %q, want empty", got)
	}
}

// TestIngestSeparatesReasoningFromAnswer proves the thinking marker plus its live
// text appear as reasoning streams, collapse to a "thought for Ns" summary (the
// streamed text removed) when the answer begins, and the answer commits as its
// own distinct entry.
func TestIngestSeparatesReasoningFromAnswer(t *testing.T) {
	m := newTestChatTUI()

	m.ingestEvent(event.Event{Kind: event.Reasoning, Text: "…reasoning…"}) // thinking → marker + live text
	if len(m.transcript) != 2 || !strings.Contains(m.transcript[0], "thinking") {
		t.Fatalf("thinking marker should appear at once, transcript=%v", m.transcript)
	}
	if !strings.Contains(m.transcript[1], "…reasoning…") {
		t.Fatalf("reasoning text should stream live below the marker, transcript=%v", m.transcript)
	}

	m.ingestEvent(event.Event{Kind: event.Text, Text: "Hello answer"}) // answer begins → block collapses
	if len(m.transcript) != 1 || !strings.Contains(m.transcript[0], "thought for") {
		t.Fatalf("block should collapse to a duration summary, transcript=%v", m.transcript)
	}
	if strings.Contains(strings.Join(m.transcript, "\n"), "…reasoning…") {
		t.Fatalf("collapsed reasoning text should be removed, transcript=%v", m.transcript)
	}
	if m.pending.String() != "Hello answer" {
		t.Errorf("answer should be live in pending, got %q", m.pending.String())
	}
	if m.reasoning.Len() != 0 {
		t.Errorf("reasoning buffer should be cleared after commit")
	}

	m.commitPending() // turn end
	if len(m.transcript) != 2 || !strings.Contains(m.transcript[1], "Hello") {
		t.Fatalf("answer should commit as a separate entry, transcript=%v", m.transcript)
	}
}

// TestVerboseReasoningInsertsTextUnderSummary proves /verbose mode keeps the full
// thinking text, placed beneath the collapsed duration summary.
func TestVerboseReasoningInsertsTextUnderSummary(t *testing.T) {
	m := newTestChatTUI()
	m.showReasoning = true

	m.ingestEvent(event.Event{Kind: event.Reasoning, Text: "step one "})
	m.ingestEvent(event.Event{Kind: event.Reasoning, Text: "step two"})
	m.ingestEvent(event.Event{Kind: event.Text, Text: "Answer"}) // closes the block

	if len(m.transcript) != 2 {
		t.Fatalf("verbose block should be summary + text, transcript=%v", m.transcript)
	}
	if !strings.Contains(m.transcript[0], "thought for") {
		t.Errorf("first line should be the duration summary, got %q", m.transcript[0])
	}
	if !strings.Contains(m.transcript[1], "step one") || !strings.Contains(m.transcript[1], "step two") {
		t.Errorf("verbose text should appear under the summary, got %q", m.transcript[1])
	}
}

// TestIngestEventFlushesAnswer confirms an event line (e.g. a tool dispatch)
// finalizes the answer streamed before it, preserving order in scrollback.
func TestIngestEventFlushesAnswer(t *testing.T) {
	m := newTestChatTUI()
	m.ingestEvent(event.Event{Kind: event.Text, Text: "partial answer "})
	m.ingestEvent(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{Name: "read_file", Args: `{"path":"x"}`}})
	// answer, then a blank spacer, then the tool line.
	if n := len(*m.pendingCommit); n != 3 {
		t.Fatalf("answer + spacer + event line should be three commits, got %d: %v", n, *m.pendingCommit)
	}
	if !strings.Contains((*m.pendingCommit)[0], "partial answer") {
		t.Errorf("first commit should be the buffered answer, got %q", (*m.pendingCommit)[0])
	}
	if strings.TrimSpace((*m.pendingCommit)[1]) != "" {
		t.Errorf("second commit should be a blank spacer, got %q", (*m.pendingCommit)[1])
	}
	if !strings.Contains((*m.pendingCommit)[2], "Read(x)") {
		t.Errorf("third commit should be the tool card, got %q", (*m.pendingCommit)[2])
	}
	if m.pending.Len() != 0 {
		t.Errorf("answer buffer should be drained after the event line")
	}
}

// TestStreamAnswerFlushesCompletedParagraphs proves a multi-paragraph answer
// appears chunk by chunk: a closed paragraph renders to scrollback while the
// still-streaming one stays buffered, and turn end flushes the remainder.
func TestStreamAnswerFlushesCompletedParagraphs(t *testing.T) {
	m := newTestChatTUI()

	m.ingestEvent(event.Event{Kind: event.Text, Text: "First paragraph.\n\nSecond para "})
	if m.answerIdx < 0 {
		t.Fatalf("a completed paragraph should open a streamed answer block")
	}
	joined := strings.Join(m.transcript, "\n")
	if !strings.Contains(joined, "First paragraph.") {
		t.Errorf("completed paragraph should be on screen, transcript=%v", m.transcript)
	}
	if strings.Contains(joined, "Second para") {
		t.Errorf("the still-streaming paragraph must stay buffered, transcript=%v", m.transcript)
	}

	m.ingestEvent(event.Event{Kind: event.Text, Text: "is done now."})
	m.ingestEvent(event.Event{Kind: event.Message})
	final := strings.Join(m.transcript, "\n")
	if !strings.Contains(final, "First paragraph.") || !strings.Contains(final, "Second para is done now.") {
		t.Errorf("turn end should flush the whole answer, transcript=%v", m.transcript)
	}
	if m.pending.Len() != 0 || m.answerIdx != -1 {
		t.Errorf("answer state should reset after commit, pending=%d idx=%d", m.pending.Len(), m.answerIdx)
	}
}

// TestFlushableMarkdownPrefixKeepsOpenFence proves a blank line inside an unclosed
// fenced code block is not a flush boundary — the half-written block stays buffered
// so it never renders mangled, while prose before the fence does flush.
func TestFlushableMarkdownPrefixKeepsOpenFence(t *testing.T) {
	open := "intro line\n\n```go\nfunc f() {\n\n\t// still typing"
	if got := flushableMarkdownPrefix(open); got != "intro line" {
		t.Errorf("open fence: flushable prefix = %q, want %q", got, "intro line")
	}

	closed := "```go\ncode\n\nmore\n```\n\ntrailing"
	if got := flushableMarkdownPrefix(closed); got != "```go\ncode\n\nmore\n```" {
		t.Errorf("closed fence: flushable prefix = %q", got)
	}

	if got := flushableMarkdownPrefix("no boundary yet"); got != "" {
		t.Errorf("no blank line should flush nothing, got %q", got)
	}
}

// TestToolProgressStreamsThenCollapses proves a running tool's output streams
// live under its card via the ⎿ connector, then collapses to a line-count
// summary when the result lands.
func TestToolProgressStreamsThenCollapses(t *testing.T) {
	m := newTestChatTUI()
	m.ingestEvent(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: "b1", Name: "bash", Args: `{"command":"go test ./..."}`}})
	m.ingestEvent(event.Event{Kind: event.ToolProgress, Tool: event.Tool{ID: "b1", Output: "ok pkg/a\n"}})
	m.ingestEvent(event.Event{Kind: event.ToolProgress, Tool: event.Tool{ID: "b1", Output: "ok pkg/b\n"}})

	joined := strings.Join(m.transcript, "\n")
	if !strings.Contains(joined, "ok pkg/a") || !strings.Contains(joined, "ok pkg/b") {
		t.Fatalf("live output should be visible while running:\n%s", joined)
	}
	if !strings.Contains(joined, "⎿") {
		t.Fatalf("live output should use the ⎿ connector:\n%s", joined)
	}

	m.ingestEvent(event.Event{Kind: event.ToolResult, Tool: event.Tool{ID: "b1", Name: "bash", Output: "ok pkg/a\nok pkg/b\n"}})
	joined = strings.Join(m.transcript, "\n")
	if strings.Contains(joined, "ok pkg/a") {
		t.Fatalf("output should collapse after completion:\n%s", joined)
	}
	if !strings.Contains(joined, "2 lines") {
		t.Fatalf("collapsed block should summarize the line count:\n%s", joined)
	}
}

// TestToolWorkingLineThenClears proves a dispatched tool that streams no output
// (e.g. codegraph_context) shows a live "working · Ns" line so it doesn't look
// frozen, and that the line clears on the result instead of collapsing to
// "0 lines".
func TestToolWorkingLineThenClears(t *testing.T) {
	m := newTestChatTUI()
	m.ingestEvent(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: "c1", Name: "codegraph_context", Args: `{"q":"x"}`}})

	m.tickToolRunning() // one elapsed tick fills the placeholder
	joined := strings.Join(m.transcript, "\n")
	if !strings.Contains(joined, "⎿") || !strings.Contains(joined, "working") {
		t.Fatalf("a running tool should show a 'working' progress line:\n%s", joined)
	}

	m.ingestEvent(event.Event{Kind: event.ToolResult, Tool: event.Tool{ID: "c1", Name: "codegraph_context"}})
	joined = strings.Join(m.transcript, "\n")
	if strings.Contains(joined, "working") {
		t.Fatalf("working line should clear after the result:\n%s", joined)
	}
	if strings.Contains(joined, "0 lines") {
		t.Fatalf("a no-output tool must not collapse to '0 lines':\n%s", joined)
	}
	if m.toolStreamIdx != -1 {
		t.Fatalf("tool block should be closed after the result, idx=%d", m.toolStreamIdx)
	}
}

// TestConsecutiveToolCallsKeepMarkersUnderOwnCard is a regression test for
// back-to-back Bash tool calls. Before the fix, the late ToolProgress for
// the first tool (already superseded in the controller by a second
// ToolDispatch) appended a fresh live block at the end of the transcript
// under the *second* tool's card. Both "⎿" markers then stacked at the
// end, hiding which run produced which output. The fix threads the
// transcript slot through shellTranscriptIdx so each tool's live block
// stays directly under its own card regardless of the dispatch/progress
// arrival order.
func TestConsecutiveToolCallsKeepMarkersUnderOwnCard(t *testing.T) {
	m := newTestChatTUI()
	// First bash: dispatched and gets one progress chunk before the second
	// bash is dispatched, mirroring the model's parallel-tool-call pattern.
	// The "shell-" prefix ensures streamToolOutput accumulates into
	// shellOutputs, which collapseShellSlot uses to recover the line count
	// after the live state has been reset by the second beginToolRunning.
	m.ingestEvent(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: "shell-1", Name: "bash", Args: `{"command":"git status"}`}})
	m.ingestEvent(event.Event{Kind: event.ToolProgress, Tool: event.Tool{ID: "shell-1", Output: "On branch main-v2\n"}})
	// Second bash dispatched before the first finishes; this switches
	// m.toolStreamID to "shell-2" and resets the live streaming state.
	m.ingestEvent(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: "shell-2", Name: "bash", Args: `{"command":"git branch -a"}`}})
	// The second bash also streams one chunk of output so its collapse
	// produces a real ⎿ marker (not the zero-output blank fallback).
	m.ingestEvent(event.Event{Kind: event.ToolProgress, Tool: event.Tool{ID: "shell-2", Output: "* main-v2\n"}})
	// Late progress for the FIRST bash — the path that previously stacked
	// its marker under the second card.
	m.ingestEvent(event.Event{Kind: event.ToolProgress, Tool: event.Tool{ID: "shell-1", Output: "nothing to commit\n"}})
	// Now finish both; each should collapse in place under its own card.
	m.ingestEvent(event.Event{Kind: event.ToolResult, Tool: event.Tool{ID: "shell-1", Name: "bash", Output: "On branch main-v2\nnothing to commit\n"}})
	m.ingestEvent(event.Event{Kind: event.ToolResult, Tool: event.Tool{ID: "shell-2", Name: "bash", Output: "* main-v2\n"}})

	// Locate each tool's card. With the fix the transcript is exactly
	// [card1, marker1, "", card2, marker2] — 5 lines, one marker per
	// card. Without the fix the late progress overwrites the last slot
	// in place (or appends), so the first card's slot is left holding
	// only the first live chunk, and both markers end up at the tail.
	transcript := m.transcript
	idx1, idx2 := -1, -1
	for i, ln := range transcript {
		if idx1 == -1 && strings.Contains(ln, "git status") {
			idx1 = i
		}
		if idx2 == -1 && strings.Contains(ln, "git branch -a") {
			idx2 = i
		}
	}
	if idx1 < 0 || idx2 < 0 || idx2 <= idx1 {
		t.Fatalf("expected two bash cards in dispatch order, got idx1=%d idx2=%d\n%s", idx1, idx2, strings.Join(transcript, "\n"))
	}

	// Each card must be followed by its own ⎿-prefixed marker slot —
	// not just "some marker somewhere after the second card".
	for _, pair := range []struct {
		card string
		idx  int
	}{
		{card: "git status", idx: idx1},
		{card: "git branch -a", idx: idx2},
	} {
		next := transcript[pair.idx+1]
		if !strings.Contains(next, "⎿") {
			t.Fatalf("%q's marker should be at transcript[%d] with the ⎿ connector, got %q\nfull transcript:\n%s",
				pair.card, pair.idx+1, next, strings.Join(transcript, "\n"))
		}
	}

	// The first card's marker must reflect the full output of the first
	// run ("On branch main-v2" AND "nothing to commit"), not just the
	// first chunk. The bug left only the pre-late-progress chunk in
	// transcript[idx1+1], so the second line would be missing.
	marker1 := transcript[idx1+1]
	if !strings.Contains(marker1, "On branch main-v2") || !strings.Contains(marker1, "nothing to commit") {
		t.Fatalf("first card's marker should preview the full output of shell-1, got %q", marker1)
	}
}

// TestRepeatedShellCommandDoesNotAccumulateOutput is the regression test for a
// re-run of the same "!" command (e.g. !pwd three times). RunShell derives a
// stable id from the command text ("shell-pwd"), so streamToolOutput kept
// appending each run's output onto the previous run's in m.shellOutputs[id];
// beginToolRunning now clears the entry so each run starts from a clean slate.
func TestRepeatedShellCommandDoesNotAccumulateOutput(t *testing.T) {
	m := newTestChatTUI()
	const id = "shell-pwd"
	const out = "/home/user/project\n"

	for range 3 {
		m.ingestEvent(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: id, Name: "bash", Args: `{"command":"pwd"}`}})
		m.ingestEvent(event.Event{Kind: event.ToolProgress, Tool: event.Tool{ID: id, Output: out}})
		m.ingestEvent(event.Event{Kind: event.ToolResult, Tool: event.Tool{ID: id, Name: "bash", Output: out}})
	}

	if got := m.shellOutputs[id]; got != out {
		t.Fatalf("a re-run must not accumulate prior output: shellOutputs[%q] = %q, want %q", id, got, out)
	}
}

// TestConsecutiveNonShellToolsDoNotRenderNegativeLineCount is the regression
// test for the review-blocking case. The original fix to back-to-back shell
// tools records every dispatched id in shellTranscriptIdx so a late
// ToolProgress/Result can land in the correct slot. But for non-shell-
// prefixed tools (e.g. read_file) the streaming state belongs to whichever
// id is current and the accumulator (shellOutputs) is never populated, so
// the late path's "n" stayed at -1 and the final else branch rendered
// "⎿ -1 lines". The fix in collapseShellSlot guards n < 0 by clearing the
// slot — a deliberate blank-line fallback rather than a misleading
// negative count.
func TestConsecutiveNonShellToolsDoNotRenderNegativeLineCount(t *testing.T) {
	m := newTestChatTUI()
	// Two back-to-back read_file tools; the first result lands AFTER
	// the second dispatch (the model dispatched them in parallel and
	// the first one finished last). This is the path the PR reviewer
	// identified as the blocker.
	m.ingestEvent(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: "read_file-1", Name: "read_file", Args: `{"path":"a.txt"}`}})
	m.ingestEvent(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: "read_file-2", Name: "read_file", Args: `{"path":"b.txt"}`}})
	// Late ToolResult for the FIRST tool — this used to render "-1 lines"
	// under the first card.
	m.ingestEvent(event.Event{Kind: event.ToolResult, Tool: event.Tool{ID: "read_file-1", Name: "read_file", Output: "a.txt contents"}})
	m.ingestEvent(event.Event{Kind: event.ToolResult, Tool: event.Tool{ID: "read_file-2", Name: "read_file", Output: "b.txt contents"}})

	transcript := m.transcript
	// The "-1 lines" bug surfaced literally as that text, so assert its
	// absence first as a clear regression marker.
	if joined := strings.Join(transcript, "\n"); strings.Contains(joined, "-1 lines") {
		t.Fatalf("transcript must not contain a negative line count:\n%s", joined)
	}
	// And the more general contract: no slot under a card should claim
	// a non-positive line count either.
	for _, line := range transcript {
		if strings.Contains(line, "0 lines") || strings.Contains(line, "-1 lines") {
			t.Fatalf("non-shell tool marker should be blank, got %q\nfull transcript:\n%s",
				line, strings.Join(transcript, "\n"))
		}
	}
}

func TestTodoPanelKeepsLastSuccessfulTodoWrite(t *testing.T) {
	m := newTestChatTUI()
	initial := `{"todos":[{"content":"Sync main-v2","status":"in_progress"},{"content":"Push origin","status":"pending"}]}`
	failed := `{"todos":[{"content":"Sync main-v2","status":"completed"},{"content":"Push origin","status":"in_progress"}]}`

	m.ingestEvent(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: "todo-1", Name: "todo_write", Args: initial}})
	m.ingestEvent(event.Event{Kind: event.ToolResult, Tool: event.Tool{ID: "todo-1", Name: "todo_write", Args: initial, Output: "Todos updated"}})
	if m.todoArgs != initial {
		t.Fatalf("todoArgs after successful result = %q, want initial args", m.todoArgs)
	}

	m.ingestEvent(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: "todo-2", Name: "todo_write", Args: failed}})
	m.ingestEvent(event.Event{Kind: event.ToolResult, Tool: event.Tool{ID: "todo-2", Name: "todo_write", Args: failed, Err: "missing complete_step"}})
	if m.todoArgs != initial {
		t.Fatalf("failed todo_write must not replace the panel: got %q, want %q", m.todoArgs, initial)
	}
}

// TestToolProgressTailCap proves the live block only keeps the last
// toolStreamTailLines lines so a chatty build doesn't flood scrollback.
func TestToolProgressTailCap(t *testing.T) {
	m := newTestChatTUI()
	m.ingestEvent(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: "b1", Name: "bash", Args: `{"command":"x"}`}})
	for i := 0; i < toolStreamTailLines+5; i++ {
		m.ingestEvent(event.Event{Kind: event.ToolProgress, Tool: event.Tool{ID: "b1", Output: "line" + string(rune('A'+i)) + "\n"}})
	}
	block := m.transcript[m.toolStreamIdx]
	if got := strings.Count(block, "\n") + 1; got > toolStreamTailLines {
		t.Fatalf("live block kept %d lines, want <= %d:\n%s", got, toolStreamTailLines, block)
	}
	if strings.Contains(block, "lineA") {
		t.Fatalf("oldest line should have scrolled out of the tail:\n%s", block)
	}
}

// TestReasoningViewBounded proves the live thinking view stays bounded under a
// long stream — the fix for the O(n²)/multi-GB re-render of the full thought.
func TestReasoningViewBounded(t *testing.T) {
	m := newTestChatTUI()
	for i := 0; i < 5000; i++ {
		m.ingestEvent(event.Event{Kind: event.Reasoning, Text: "some thinking text token "})
	}
	if len(m.reasoningView) > reasoningViewMax {
		t.Fatalf("reasoningView unbounded: %d > %d", len(m.reasoningView), reasoningViewMax)
	}
	if c := strings.Count(m.transcript[m.reasoningTextIdx], "\n") + 1; c > reasoningTailLines {
		t.Fatalf("live reasoning block kept %d lines, want <= %d", c, reasoningTailLines)
	}
}
