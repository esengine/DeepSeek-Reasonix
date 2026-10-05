package control

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/observe"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/safety/permission"
	"reasonix/internal/state/sessionstore"
)

// namedReader is a read-only tool under a chosen name that records its runs.
type namedReader struct {
	name string
	runs *int
}

func (r namedReader) Name() string        { return r.name }
func (r namedReader) Description() string { return "reads" }
func (r namedReader) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`)
}
func (r namedReader) ReadOnly() bool { return true }
func (r namedReader) Execute(context.Context, json.RawMessage) (string, error) {
	*r.runs++
	return "CONTENTS", nil
}

// requestLog is a scripted provider that also keeps what it was sent.
type requestLog struct {
	scriptedTurns
	reqs []provider.Request
}

func (l *requestLog) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	l.reqs = append(l.reqs, req)
	return l.scriptedTurns.Stream(ctx, req)
}

func (l *requestLog) toolResult(id string) string {
	for _, req := range l.reqs {
		for _, m := range req.Messages {
			if m.Role == provider.RoleTool && m.ToolCallID == id {
				return m.Content
			}
		}
	}
	return ""
}

// observeController wires the controller the way boot does, around a registry
// deliberately wider than the ceiling: the gate and the asker must hold when
// the registry does not.
func observeController(t *testing.T, policy permission.Policy, turns [][]provider.Chunk) (*Controller, *observe.Ledger, *recordingWriter, *int, *requestLog) {
	t.Helper()
	writer := &recordingWriter{}
	runs := 0
	reg := tool.NewRegistry()
	reg.Add(writer)
	reg.Add(namedReader{"read_file", &runs})
	reg.Add(namedReader{"remember", &runs})
	reg.Add(agent.NewAskTool())
	prov := &requestLog{scriptedTurns: scriptedTurns{turns: turns}}
	ag := agent.New(prov, reg, sessionstore.NewSession(""), agent.Options{}, event.Discard)
	ledger := observe.NewLedger(nil)
	c := New(Options{
		Runner: ag, Executor: ag, Policy: policy, Sink: event.Discard,
		Observe: &ObserveRun{Posture: observe.New(false), Pending: ledger, Context: observe.RunContext{ScheduleID: "s", TriggerID: "t"}},
	})
	t.Cleanup(c.Close)
	return c, ledger, writer, &runs, prov
}

func TestObserveGateParksWhatNeedsAPersonAndNeverRunsIt(t *testing.T) {
	policy := permission.New("allow", []string{"write_file", "remember"}, []string{"read_file(secret.txt)"}, nil)
	c, ledger, writer, runs, _ := observeController(t, policy, [][]provider.Chunk{
		toolCallTurn("c-read", "read_file", `{"path":"secret.txt"}`),
		toolCallTurn("c-remember", "remember", `{"note":"x"}`),
		toolCallTurn("c-write", "write_file", `{"path":"a.txt"}`),
		textTurn("done"),
	})
	c.EnableInteractiveApproval()
	c.SetToolApprovalMode(ToolApprovalYolo)
	c.ApplyHeadlessApprovalMode(ToolApprovalYolo)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := c.Run(ctx, "go"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if *runs != 0 || len(writer.paths) != 0 {
		t.Fatalf("a refused call ran: reader/remember runs=%d writes=%v", *runs, writer.paths)
	}
	parked := ledger.List()
	if len(parked) != 2 {
		t.Fatalf("parked = %+v, want the ask-rule read and the fresh-human tool", parked)
	}
	if parked[0].Source != "read_file" || parked[0].Risk != observe.RiskLow || parked[1].Source != "remember" || parked[1].Risk != observe.RiskHigh {
		t.Fatalf("records = %+v", parked)
	}
	if mode := c.ToolApprovalMode(); mode != ToolApprovalReadOnly {
		t.Fatalf("approval mode = %q after a frontend tried to change it", mode)
	}
}

func TestObserveAskIsNeverAnswered(t *testing.T) {
	askArgs := `{"questions":[{"header":"H","question":"Which?","options":[{"label":"A"},{"label":"B"}]}]}`
	c, ledger, _, _, prov := observeController(t, permission.New("allow", nil, nil, nil), [][]provider.Chunk{
		toolCallTurn("c-ask", "ask", askArgs),
		textTurn("done"),
	})
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := c.Run(ctx, "decide"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := ledger.List(); len(got) != 1 || got[0].Kind != observe.KindAsk || !got[0].Untrusted {
		t.Fatalf("parked = %+v", got)
	}
	got := prov.toolResult("c-ask")
	if !strings.Contains(got, "pending decision p1") || strings.Contains(got, "answered") || strings.Contains(got, "dismissed") {
		t.Fatalf("the model was told %q, want the question parked and unanswered", got)
	}
}

func TestParkingAskerReportsParkedByIdentity(t *testing.T) {
	ledger := observe.NewLedger(nil)
	answers, err := parkingAsker{sink: ledger}.Ask(context.Background(), []event.AskQuestion{{Header: "H", Prompt: "Which?", Options: []event.AskOption{{Label: "A"}, {Label: "B"}}}})
	if answers != nil || !errors.Is(err, observe.ErrParked) || !strings.Contains(err.Error(), "p1") {
		t.Fatalf("Ask = %v, %v, want no answers and ErrParked naming p1", answers, err)
	}
}

type failingSink struct{}

func (failingSink) Park(observe.Pending) (observe.Pending, error) {
	return observe.Pending{}, errors.New("disk full")
}

func TestUnrecordedRequestsAreStillRefused(t *testing.T) {
	answers, err := parkingAsker{sink: failingSink{}}.Ask(context.Background(), []event.AskQuestion{{Prompt: "Which?"}})
	if answers != nil || err == nil || errors.Is(err, observe.ErrParked) {
		t.Fatalf("Ask = %v, %v, want an unanswered error that does not claim to be parked", answers, err)
	}
	gate := newObserveGate(permission.New("ask", nil, []string{"read_file"}, nil), failingSink{}, nil)
	v, verr := gate.Verdict(context.Background(), "read_file", json.RawMessage(`{"path":"a"}`), true)
	if verr != nil || v.Allow || v.Code != permission.RefusalUnattended {
		t.Fatalf("verdict = %+v, %v", v, verr)
	}
}

func TestObserveGateIgnoresSessionAllowAndModes(t *testing.T) {
	policy := permission.New("allow", []string{"read_file", "write_file"}, []string{"read_file"}, nil).WithSessionAllow([]string{"read_file"})
	gate := newObserveGate(policy, observe.NewLedger(nil), nil)
	if v, _ := gate.Verdict(context.Background(), "read_file", json.RawMessage(`{"path":"a"}`), true); v.Allow {
		t.Fatalf("a session allow answered an ask rule: %+v", v)
	}
	if v, _ := gate.Verdict(context.Background(), "write_file", json.RawMessage(`{"path":"a"}`), false); v.Allow || v.Code != permission.RefusalReadOnly {
		t.Fatalf("a writer under a blanket allow: %+v", v)
	}
	if !gate.DeniesWriters() {
		t.Fatal("the observe gate must report that it denies writers")
	}
}

// captureSink keeps what a store would be handed.
type captureSink struct{ got []observe.Pending }

func (c *captureSink) Park(p observe.Pending) (observe.Pending, error) {
	c.got = append(c.got, p)
	p.ID = "p1"
	return p, nil
}

func TestParkedTextIsCleanedBeforeItReachesAnyStore(t *testing.T) {
	sink := &captureSink{}
	dirty := "x\u202ey\u200bz\x00\U000e0041"
	_, _ = parkingAsker{sink: sink}.Ask(context.Background(), []event.AskQuestion{{Header: "H", Prompt: "Which" + dirty, Options: []event.AskOption{{Label: "A" + dirty}, {Label: "B"}}}})
	gate := newObserveGate(permission.New("ask", nil, []string{"read_file"}, nil), sink, nil)
	_, _ = gate.Verdict(context.Background(), "read_file", json.RawMessage(`{"path":"a`+strings.ReplaceAll(dirty, "\x00", "")+`"}`), true)
	if len(sink.got) != 2 {
		t.Fatalf("stored %d records", len(sink.got))
	}
	for _, p := range sink.got {
		for _, r := range p.Summary + p.Detail {
			if r == 0x202e || r == 0x200b || r == 0 || r >= 0xe0000 {
				t.Fatalf("a hidden character reached the store: %+v", p)
			}
		}
	}
}
