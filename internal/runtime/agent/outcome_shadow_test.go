package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/safety/evidence"
)

// Measuring the stall is worth nothing unless it reaches a person, and it has
// to reach them once: a warning repeated every round is one nobody reads, and
// one that never arrives is the ten minutes this was built for.
func TestStalledCheckIsSaidOnceAndOnlyToTheUser(t *testing.T) {
	ledger := evidence.NewLedger()
	var notices, everything []event.Event
	sink := event.FuncSink(func(e event.Event) {
		everything = append(everything, e)
		if e.Kind == event.Notice && e.Code == event.NoticeCodeVerificationStalled {
			notices = append(notices, e)
		}
	})
	a := &Agent{task: taskRuntime{ledger: ledger}, svc: agentServices{sink: sink}}

	failingCheck := func() evidence.Receipt {
		args, _ := json.Marshal(map[string]string{"command": "node --check game.js"})
		return evidence.ReceiptFromToolCall("bash", args, false, evidence.ToolFacts{})
	}
	edit := func() evidence.Receipt {
		return evidence.ReceiptFromToolCall("edit_file", json.RawMessage(`{"path":"game.js"}`), true, evidence.ToolFacts{})
	}
	round := func(r evidence.Receipt) {
		mark := a.ledgerMark()
		ledger.Record(r)
		a.observeOutcomeShadow(false, mark)
	}

	// The exported trajectory's shape: one check that fails, then edit and
	// re-check until the person watching gives up.
	round(failingCheck())
	for range 5 {
		round(edit())
		round(failingCheck())
	}

	if len(notices) != 1 {
		t.Fatalf("stall notices = %d, want exactly one across five repeats", len(notices))
	}
	if notices[0].Level != event.LevelWarn {
		t.Fatalf("stall notice level = %v, want warn", notices[0].Level)
	}
	// What the run is spending itself on is not something anyone in the
	// conversation said. Said as a chat message it arrived as a card with the
	// weight of a turn; the window has a place of its own for it.
	if notices[0].Audience != event.NoticeAudienceOperator {
		t.Fatalf("stall notice audience = %q, want operator", notices[0].Audience)
	}
	if !strings.Contains(notices[0].Detail, "3 rounds") || !strings.Contains(notices[0].Detail, "3 change") {
		t.Fatalf("stall notice detail = %q, want the rounds and the change that moved nothing", notices[0].Detail)
	}
	// It is an instrument, not a gate. The samples travel on the outcome-sink
	// capability, so an ordinary sink sees one thing and one thing only: a
	// notice, which no frontend turns into model input.
	for _, e := range everything {
		if e.Kind != event.Notice {
			t.Fatalf("outcome shadow emitted %v to an ordinary sink, want notices only", e.Kind)
		}
	}

	// A check that goes green ends the streak, so the next stuck check has to
	// earn its own warning rather than inherit this one's.
	passing := func() evidence.Receipt {
		args, _ := json.Marshal(map[string]string{"command": "node --check game.js"})
		return evidence.ReceiptFromToolCall("bash", args, true, evidence.ToolFacts{})
	}
	round(passing())
	round(failingCheck())
	round(failingCheck())
	if len(notices) != 1 {
		t.Fatalf("stall notices after the check moved = %d, want the streak restarted", len(notices))
	}
}
