package agent

import (
	"fmt"
	"os"

	"reasonix/internal/contract/event"
	"reasonix/internal/safety/evidence"
)

// ProgressWatch says when a run has stopped producing effects the host can
// observe. What it produces is said to the person watching; nothing reaches
// the model, no call is refused, and only Pause — the user's own setting — ends
// a run, through the same resumable stop a spent budget uses.
type ProgressWatch struct {
	Rounds        int  // idle tool rounds in a row before a stall; <=0 watches none
	TokenMultiple int  // input since the last effect reaching this many windows; <=0 is off
	Pause         bool // end the run resumably instead of only saying so
}

// PauseKindNoProgress is RunPauseInfo.Kind for a run the watch paused.
const PauseKindNoProgress = "no_progress"

// PauseKindPerseveration is RunPauseInfo.Kind for a run paused because the model
// was stuck repeating itself (a perseveration loop).
const PauseKindPerseveration = "perseveration"

// SetProgressWatch replaces the watch for the next round onward. A settings
// change reaches a running turn without a rebuild.
func (a *Agent) SetProgressWatch(w ProgressWatch) { a.progressWatch.Store(&w) }

func (a *Agent) progressWatchConfig() ProgressWatch {
	if w := a.progressWatch.Load(); w != nil {
		return *w
	}
	return ProgressWatch{}
}

// progressRun is one Run's watch. It lives in turnRuntime, so a user message —
// which is what starts a Run — is what zeroes it.
type progressRun struct {
	effects *evidence.EffectWatch
	idle    int
	stalled bool
	// spentAtEffect is the run's input tokens when it last did something
	// observable; the backstop counts only what was spent after it.
	spentAtEffect int
}

// progressMark is where a round starts in the two records its effects land in.
type progressMark struct {
	receipts int
	todo     int
}

func (a *Agent) markProgressRound() progressMark {
	return progressMark{receipts: a.ledgerMark(), todo: a.task.todoRevs.progress}
}

// settleProgressRound scores the round that just ran and reports the watch when
// it is stalled, or when it stops being so. The error is non-nil only when the
// user's pause setting asks for the run to end here.
func (a *Agent) settleProgressRound(state *turnRuntime, mark progressMark) error {
	cfg := a.progressWatchConfig()
	if cfg.Rounds <= 0 && cfg.TokenMultiple <= 0 {
		return nil
	}
	w := &state.watch
	// Without a ledger nothing is observed, and an unobserved round is not an
	// idle one: the round judgement stays silent rather than guess.
	if a.task.ledger != nil {
		if w.effects == nil {
			w.effects = evidence.NewEffectWatch()
		}
		effect := w.effects.RoundHadEffect(a.task.ledger.ReceiptsSince(mark.receipts), a.progressExists())
		if effect || a.task.todoRevs.progress != mark.todo {
			w.idle, w.spentAtEffect = 0, state.budget.promptTokens
		} else if cfg.Rounds > 0 {
			w.idle++
		}
	}
	report := a.progressReport(cfg, w.idle, state.budget.promptTokens-w.spentAtEffect)
	if !report.Stalled && !w.stalled {
		return nil
	}
	w.stalled = report.Stalled
	report.Pausing = report.Stalled && cfg.Pause
	a.svc.sink.Emit(event.Event{Kind: event.ProgressWatchEvent, ProgressWatch: &report})
	if !report.Pausing {
		return nil
	}
	a.emitTurnShadows(a.turn.turnInput, true)
	return newNoProgressPause(report)
}

// emitPerseverationNotice reports a detected degenerate generation loop through
// the progress-watch channel. The guard calls it on the rising edge of a
// detection, so a loop that keeps tripping says so once and stays quiet until a
// scan that does not trip re-arms it. Nothing reaches the model.
func (a *Agent) emitPerseverationNotice(sink event.Sink) {
	cfg := a.progressWatchConfig()
	w := &a.turn.watch
	report := event.ProgressWatch{
		Stalled: true, Cause: event.ProgressWatchCausePerseveration,
		IdleRounds: w.idle, RoundLimit: max(cfg.Rounds, 0), Pausing: cfg.Pause,
	}
	// w.stalled lets the next clean round clear the notice through the same
	// report the round and token causes use.
	w.stalled = true
	sink.Emit(event.Event{Kind: event.ProgressWatchEvent, ProgressWatch: &report})
}

func (a *Agent) progressReport(cfg ProgressWatch, idle, promptTokens int) event.ProgressWatch {
	report := event.ProgressWatch{
		IdleRounds: idle, RoundLimit: max(cfg.Rounds, 0),
		PromptTokens: promptTokens, TokenMultiple: max(cfg.TokenMultiple, 0),
	}
	if window := a.ContextWindow(); window > 0 && cfg.TokenMultiple > 0 {
		report.TokenLimit = window * cfg.TokenMultiple
	}
	switch {
	case cfg.Rounds > 0 && idle >= cfg.Rounds:
		report.Cause = event.ProgressWatchCauseRounds
	case report.TokenLimit > 0 && promptTokens >= report.TokenLimit:
		report.Cause = event.ProgressWatchCauseTokens
	}
	report.Stalled = report.Cause != ""
	return report
}

// progressExists stats a shell operand against the workspace the host observes
// effects in, which is where the shell tool runs.
func (a *Agent) progressExists() evidence.Exists {
	return evidence.ExistsUnder(a.observeRoot, func(p string) bool {
		_, err := os.Stat(p)
		return err == nil
	})
}

// noProgressPause ends a Run the user asked to have paused once it stalls. The
// work is saved and the next message continues it with the watch zeroed.
type noProgressPause struct {
	limit  int
	key    string
	detail string
}

func newNoProgressPause(r event.ProgressWatch) *noProgressPause {
	if r.Cause == event.ProgressWatchCauseTokens {
		return &noProgressPause{limit: r.TokenMultiple, key: "progress_watch.token_multiple", detail: fmt.Sprintf(
			"%d input tokens since the run last did anything observable, %d times the model's context window", r.PromptTokens, r.TokenMultiple)}
	}
	return &noProgressPause{limit: r.RoundLimit, key: "progress_watch.rounds", detail: fmt.Sprintf(
		"%d tool rounds in a row changed no file, moved no check or task step, read nothing new and brought back no delegated result", r.IdleRounds)}
}

func (e *noProgressPause) Error() string {
	return fmt.Sprintf("paused: %s — the work so far is saved; send another message to continue, or turn off progress_watch.pause", e.detail)
}

// perseverationPause ends a Run because the model kept repeating itself. Key
// names the setting that stopped it — the user's pause switch, or a spent retry
// budget — so a host points at the knob that actually ended the run instead of
// the other one. The work is saved and the next message continues it.
type perseverationPause struct {
	key    string
	detail string
	hint   string
}

func newPerseverationPause(key, detail, hint string) *perseverationPause {
	return &perseverationPause{key: key, detail: detail, hint: hint}
}

func (e *perseverationPause) Error() string {
	return fmt.Sprintf("paused: %s — the work so far is saved; send another message to continue, or %s", e.detail, e.hint)
}
