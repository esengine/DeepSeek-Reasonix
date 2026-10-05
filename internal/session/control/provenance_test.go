package control

import (
	"context"
	"strings"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/safety/permission"
	"reasonix/internal/state/sessionstore"
)

// An answer given on a phone is the phone's on its receipt, and reaches the
// model exactly as the same answer from the window would.
func TestADevicesAnswerIsRecordedOnItsReceipt(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(agent.NewAskTool())
	prov := &recordingProvider{streams: [][]provider.Chunk{
		toolCallTurn("a1", "ask", askQuestionArgs),
		textTurn("Done."),
	}}
	ag := agent.New(prov, reg, sessionstore.NewSession(""), agent.Options{}, event.Discard)

	asked := make(chan event.Ask, 1)
	receipts := make(chan *provider.DecisionReceipt, 2)
	c := New(Options{
		Runner:   ag,
		Executor: ag,
		Policy:   permission.New("ask", nil, nil, nil),
		Sink: event.FuncSink(func(e event.Event) {
			switch {
			case e.Kind == event.AskRequest:
				asked <- e.Ask
			case e.Kind == event.Notice && e.DecisionReceipt != nil:
				receipts <- e.DecisionReceipt
			}
		}),
	})
	c.EnableInteractiveApproval()

	via := &provider.Via{Device: "dev-d", Ordinal: 5}
	go func() {
		a := <-asked
		c.AnswerQuestionFrom(a.ID, []event.AskAnswer{{QuestionID: a.Questions[0].ID, Selected: []string{"B"}}}, via)
	}()
	if err := c.runOneTurn(context.Background(), orchestratedTurn{input: "pick one", raw: "pick one"}); err != nil {
		t.Fatalf("runOneTurn: %v", err)
	}

	select {
	case r := <-receipts:
		if r.Kind != "ask" || r.Via == nil || *r.Via != *via {
			t.Fatalf("receipt %+v, want the ask attributed to device 5", r)
		}
	default:
		t.Fatal("no receipt was recorded for the answer")
	}
	if result := lastToolResult(prov); !strings.Contains(result, "B") {
		t.Fatalf("the device picked B and the model was told %q", result)
	}
}
