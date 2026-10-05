package agent

import (
	"fmt"
	"reasonix/internal/contract/hostaudit"

	"reasonix/internal/contract/event"
	"reasonix/internal/safety/evidence"
)

// stallNoticeAge is when a check that will not move becomes worth saying out
// loud. Two re-failures can be one fix landing short; by the third the turn has
// spent three rounds of reasoning without the check reporting anything
// different, and only the person watching can price continuing.
const stallNoticeAge = 3

// observeOutcomeShadow scores one round's receipts by outcome — information
// gathered, verifications run, state transitions, unverified change carried —
// and offers the sample to any sink collecting them. It still decides nothing
// about what the turn may do; the one number it reads back is said to the user,
// never to the model, so no gate and no prompt byte moves with it.
func (a *Agent) observeOutcomeShadow(cancelled bool, receiptMark int) {
	if cancelled || a.task.ledger == nil {
		return
	}
	if a.task.outcome == nil {
		a.task.outcome = evidence.NewOutcomeTracker()
	}
	sample := a.task.outcome.ScoreRound(a.task.ledger.ReceiptsSince(receiptMark))
	event.RecordOutcomeProgress(a.svc.sink, sample)
	a.noticeStalledVerification(sample)
}

// noticeStalledVerification says once that the turn is spending itself on a
// check that has stopped moving. Only the round the check actually re-failed on
// can cross the threshold — the age stands still through the edits in between —
// so this fires on that one round, and a different check getting stuck later
// starts a streak that has to climb again.
func (a *Agent) noticeStalledVerification(sample hostaudit.OutcomeSample) {
	if sample.Stall == 0 || sample.StallAge != stallNoticeAge {
		return
	}
	a.svc.sink.Emit(event.Event{
		Kind: event.Notice, Level: event.LevelWarn, Code: event.NoticeCodeVerificationStalled,
		// About the run rather than the conversation: it is the window's to say
		// beside the composer, not a card in the transcript.
		Audience: event.NoticeAudienceOperator,
		Text:     fmt.Sprintf("The same check has failed %d rounds running and still reports the same thing.", sample.StallAge),
		Detail: fmt.Sprintf("verification stalled: %d rounds, %d change(s) landed against it without moving it",
			sample.StallAge, sample.StallMutations),
	})
}

// ledgerMark is the receipt count to score a round against, or zero before any
// ledger exists.
func (a *Agent) ledgerMark() int {
	if a.task.ledger == nil {
		return 0
	}
	return a.task.ledger.Len()
}
