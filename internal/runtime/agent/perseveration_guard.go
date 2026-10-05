package agent

import (
	"bytes"
	"context"
	"fmt"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/state/sessionstore"
)

// perseverationAbort is the stream state the opt-in cut path needs to close out
// an aborted attempt. collect carries the accumulated stream state, so the
// fields here are only what the cut adds to it.
type perseverationAbort struct {
	ctx             context.Context
	sink            event.Sink
	finishReasoning func() (string, string)
	collect         func(string, error) streamedTurn
	reasoningBytes  int
	thoughtMs       int64
}

// perseverationStep watches one streamed chunk and does what the guard's
// configuration asks: nothing, a report through the progress-watch channel, or
// a cut whose aborted turn it returns for the caller to adopt.
func (a *Agent) perseverationStep(guards perseverationGuards, chunk provider.Chunk, sink event.Sink, detected *bool, abort perseverationAbort) *streamedTurn {
	if !a.perseverationGuard || !guards.forChunk(chunk.Type).observe(chunk.Text) {
		return nil
	}
	if !a.perseverationCut() {
		*detected = true
		a.emitPerseverationNotice(sink)
		return nil
	}
	aborted := a.abortStreamOnPerseveration(abort)
	return &aborted
}

// abortStreamOnPerseveration closes out a stream the guard cut: it records
// best-effort usage, lets extensions rule on the terminal, and returns a clean
// abort whose calls never execute.
func (a *Agent) abortStreamOnPerseveration(s perseverationAbort) streamedTurn {
	base := s.collect("", nil)
	stored, _ := s.finishReasoning()
	usage := bestEffortStreamUsage(base.usage, len(base.text), s.reasoningBytes, "interrupted")
	usage = provider.UsageWithRequestAttemptCount(s.ctx, usage)
	// provider.response rules on the aborted terminal before it is persisted,
	// exactly as a clean terminal is.
	finalText, finalReasoning, signature, _, usage, err := a.interceptProviderResponse(
		s.ctx, base.text, stored, base.signature, base.calls, usage)
	if err != nil {
		return streamedTurn{partialToolStarted: base.partialToolStarted, partialCalls: base.partialCalls, maxArgChars: base.maxArgChars, err: err}
	}
	// Collapse the loop in what is shown and stored alike, so the transcript
	// follows the trim instead of silently diverging from it.
	text := trimPerseverationTail(finalText)
	reasoning := trimPerseverationTail(finalReasoning)
	reasoningID, reasoningStatus := base.reasoningID, base.reasoningStatus
	if reasoning != finalReasoning {
		// Provider-bound metadata must not attach to reasoning that was replaced.
		signature, reasoningID, reasoningStatus = "", "", ""
	}
	emitAssistantMessage(s.sink, text, reasoning, s.thoughtMs)
	// Calls from an aborted stream never execute.
	return streamedTurn{
		text: text, reasoning: reasoning, signature: signature, thoughtMs: s.thoughtMs,
		reasoningID: reasoningID, reasoningStatus: reasoningStatus, usage: usage,
		partialCalls: base.partialCalls, maxArgChars: base.maxArgChars, perseverationAborted: true,
	}
}

// perseverationKeepRepetitions is how many repetitions of the loop phrase are
// kept after the phrase itself when the tail is trimmed: the phrase and its last
// repetition, so the retry shows what was looped on and that it repeated.
const perseverationKeepRepetitions = 1

// trimPerseverationTail collapses a degenerate loop at the end of s to the
// phrase and its last repetition, so an opt-in retry resubmits evidence of the
// loop instead of the whole thing. A buffer with no such loop is unchanged.
func trimPerseverationTail(s string) string {
	if s == "" {
		return s
	}
	g := newPerseverationGuard()
	g.tail = []byte(s)
	unit, _ := g.trailingLoop()
	if unit <= 0 {
		return s
	}
	// The detected unit is the shortest block the scan may call a loop; its own
	// fundamental period is the phrase actually repeated, so keep the phrase and
	// its last repetition counted back through the buffer.
	phrase := fundamentalPeriod([]byte(s[len(s)-unit:]))
	block := s[len(s)-phrase:]
	repeats := 1
	for i := len(s) - 2*phrase; i >= 0; i -= phrase {
		if s[i:i+phrase] != block {
			break
		}
		repeats++
	}
	keep := 1 + perseverationKeepRepetitions
	if repeats < keep {
		return s
	}
	return s[:len(s)-(repeats-keep)*phrase]
}

// fundamentalPeriod returns the shortest period of block, which divides its
// length. A block that does not repeat within itself is its own period.
func fundamentalPeriod(block []byte) int {
	n := len(block)
	for d := 1; d <= n; d++ {
		if n%d != 0 {
			continue
		}
		periodic := true
		for i := d; i < n; i++ {
			if block[i] != block[i%d] {
				periodic = false
				break
			}
		}
		if periodic {
			return d
		}
	}
	return n
}

// perseverationRetryMessage is the host nudge appended before an opt-in retry.
// It is not the previous prompt verbatim — resending that would reproduce the
// loop — and it tells the user why the attempt restarted. attempt is the
// consecutive-retry ordinal (1 for the first retry).
func perseverationRetryMessage(attempt int) string {
	return fmt.Sprintf("\n[retrying (%d) avoiding perseveration]\n", attempt)
}

// perseverationLoopNotice is the fallback English text for the
// perseveration_loop notice; a frontend localizes it by the code.
func perseverationLoopNotice() string {
	return "The assistant got stuck repeating the same text; the response was cut short. Try again, add guidance, or switch provider/model."
}

// resolvePerseverationGuard maps the optional option onto the guard's state.
// Detection is on by default: an undefined (nil) budget reports a detected loop
// without cutting it. A negative value disables the guard entirely, which is how
// a provider opts out of the global default; zero or more reports, and a
// positive value also cuts the stream and nudges a retry.
func resolvePerseverationGuard(configured *int) (enabled bool, retries int) {
	if configured == nil {
		return true, 0
	}
	if *configured < 0 {
		return false, 0
	}
	return true, *configured
}

// Loop-unit detection bounds: these three fully determine sensitivity, so the
// smallest positive is 1 KiB of a byte-identical block. They are a deliberate
// tradeoff, not a tuning target — lowering them (a shorter minLoopPeriod, fewer
// repeats, a finer scan) fires sooner but starts tripping legitimate output,
// and a false positive is the worse trade, so the guard fires late and rarely yet reliably.
const (
	minLoopPeriod = 128
	maxLoopPeriod = 2048
	loopRepeats   = 8
)

// scanIntervalBytes amortises the O(maxLoopPeriod) periodicity scan: observe
// runs it at most once per this many appended bytes (trimming the tail on the
// same cadence) instead of on every streamed delta. One minimum span, so the
// first scan lands exactly when the smallest positive can first exist.
const scanIntervalBytes = minLoopPeriod * loopRepeats

// perseverationCut reports whether the opt-in cut path is enabled. Off by
// default: when the guard runs at all, detection only reports the loop.
func (a *Agent) perseverationCut() bool {
	return a.perseverationGuard && a.perseverationMaxRetries > 0
}

// settlePerseveration handles a detected or cut generation loop after the round
// is committed. The opt-in cut path nudges and retries, and an aborted stream's
// calls never reach the tool round; a detection-only run was already reported
// through the progress-watch channel, and the user's pause setting can end it
// here. A clean round resets the strike count; cont=false ends the run with err.
func (a *Agent) settlePerseveration(state *turnRuntime, streamed streamedTurn) (cont bool, err error) {
	if streamed.perseverationAborted {
		return a.handlePerseverationAbort(state)
	}
	if streamed.perseverationDetected {
		if a.progressWatchConfig().Pause {
			a.emitTurnShadows(a.turn.turnInput, true)
			return false, newPerseverationPause("progress_watch.pause",
				"the model got stuck repeating the same text", "turn off progress_watch.pause")
		}
	} else {
		state.perseverationStrikes = 0
	}
	return true, nil
}

// callsToCommit returns the tool calls to persist on the assistant turn. A
// detected loop under the user's pause setting ends the run before they can run,
// so the turn carries none: a committed call with no result is a pairing the
// next request has to backfill.
func (a *Agent) callsToCommit(streamed streamedTurn, calls []provider.ToolCall) []provider.ToolCall {
	if streamed.perseverationDetected && a.progressWatchConfig().Pause {
		return nil
	}
	return calls
}

// perseverationRetryNotice is the user-visible interjection that marks where the
// loop was collapsed and a retry began. The model sees the nudge through the
// host-marked steer path instead, so the two are deliberately separate strings.
func perseverationRetryNotice(attempt int) string {
	return fmt.Sprintf("[retrying (%d) avoiding perseveration]", attempt)
}

// handlePerseverationAbort decides what happens after the opt-in cut path ends
// a stream: below the retry budget it appends a host-marked steer nudge, shows
// the user the interjection, and continues the loop, and once the budget is spent
// it stops with a perseveration pause so the run does not read as a clean answer.
// cont=true keeps the tool loop running.
func (a *Agent) handlePerseverationAbort(state *turnRuntime) (cont bool, err error) {
	if state.perseverationStrikes < a.perseverationMaxRetries {
		state.perseverationStrikes++
		nudge := perseverationRetryMessage(state.perseverationStrikes)
		a.sess.conversation.Add(provider.Message{
			Role:         provider.RoleUser,
			Content:      a.withTurnPreferences(sessionstore.MidTurnSteerMessage(nudge, true)),
			HostAuthored: true,
		})
		a.svc.sink.Emit(event.Event{Kind: event.Steer, Text: nudge, HostAuthored: true})
		a.svc.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo,
			Code: event.NoticeCodePerseverationRetry, Text: perseverationRetryNotice(state.perseverationStrikes)})
		return true, nil
	}
	a.svc.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn,
		Code: event.NoticeCodePerseverationLoop, Text: perseverationLoopNotice()})
	return false, newPerseverationPause("progress_watch.perseveration_retries",
		"the model got stuck repeating the same text and the retry budget is spent", "raise progress_watch.perseveration_retries")
}

// perseverationGuard detects perseveration (a model stuck echoing one block):
// the model emits a short block of text or reasoning over and over without
// calling a tool or stopping, which no other guard observes — tool-round guards
// need tool calls, and the liveness watchdog only measures silence. It watches the
// rolling tail of one prose channel and reports on the rising edge.
type perseverationGuard struct {
	tail         []byte
	window       int // bytes of history examined for periodicity
	scanInterval int // appended bytes between periodicity scans (amortisation)
	sinceScan    int // appended bytes since the last scan
	minPeriod    int // shortest block that can count as a loop unit
	maxPeriod    int // longest block that can count as a loop unit
	minRepeats   int // identical repeats required before firing
	lastTripped  bool
}

func newPerseverationGuard() *perseverationGuard {
	return &perseverationGuard{
		// Exactly loopRepeats full maxLoopPeriod blocks: the scan can only reach
		// maxLoopPeriod once the tail is this long, so a smaller window would
		// silently make the largest unit undetectable.
		window:       maxLoopPeriod * loopRepeats,
		scanInterval: scanIntervalBytes,
		minPeriod:    minLoopPeriod,
		maxPeriod:    maxLoopPeriod,
		minRepeats:   loopRepeats,
	}
}

// perseverationGuards holds one guard per prose channel. Reasoning and answer
// deltas are judged on separate buffers so answer text interleaved between
// thinking deltas cannot break their periodicity and mask a thinking loop.
type perseverationGuards struct {
	reasoning *perseverationGuard
	text      *perseverationGuard
}

func newPerseverationGuards() perseverationGuards {
	return perseverationGuards{reasoning: newPerseverationGuard(), text: newPerseverationGuard()}
}

// forChunk returns the guard watching chunk's prose channel, or nil for chunk
// types that carry no prose (tool calls, usage, control).
func (g perseverationGuards) forChunk(t provider.ChunkType) *perseverationGuard {
	switch t {
	case provider.ChunkReasoning:
		return g.reasoning
	case provider.ChunkText:
		return g.text
	default:
		return nil
	}
}

// observe appends one streamed delta and reports whether the accumulated tail
// just became a short block repeated enough times to be degenerate. It reports
// on the rising edge only: a scan that trips again while the previous scan also
// tripped says nothing new, and the next scan that does not trip re-arms it. The
// periodicity scan and the tail trim both run on the scanInterval cadence.
func (g *perseverationGuard) observe(delta string) bool {
	if g == nil || delta == "" {
		return false
	}
	g.tail = append(g.tail, delta...)
	if len(g.tail) > g.window+g.scanInterval {
		g.tail = append(g.tail[:0], g.tail[len(g.tail)-g.window:]...)
	}
	g.sinceScan += len(delta)
	if g.sinceScan < g.scanInterval {
		return false
	}
	g.sinceScan = 0
	unit, _ := g.trailingLoop()
	tripped := unit > 0
	rising := tripped && !g.lastTripped
	g.lastTripped = tripped
	return rising
}

// trailingLoop reports the smallest block that repeats contiguously at the end
// of the tail at least minRepeats times, together with its repeat count, or
// (0, 0) when the tail is not a degenerate loop. The smallest qualifying
// period wins; a unit whose own fundamental period is below minPeriod is still
// caught, at its smallest multiple that clears minPeriod.
func (g *perseverationGuard) trailingLoop() (unit, repeats int) {
	n := len(g.tail)
	limit := min(g.maxPeriod, n/g.minRepeats)
	for period := g.minPeriod; period <= limit; period++ {
		if repeats = g.trailingRepeats(period); repeats >= g.minRepeats {
			return period, repeats
		}
	}
	return 0, 0
}

// trailingRepeats counts how many times the final period-length block repeats
// contiguously at the end of the tail.
func (g *perseverationGuard) trailingRepeats(period int) int {
	n := len(g.tail)
	block := g.tail[n-period:]
	count := 1
	for i := n - 2*period; i >= 0; i -= period {
		if !bytes.Equal(g.tail[i:i+period], block) {
			break
		}
		count++
	}
	return count
}
