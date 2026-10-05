package serve

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/contract/eventwire"
	"reasonix/internal/contract/provider"
	"reasonix/internal/session/control"
)

// traceFixturePath is where the desktop's reducer parity test reads these from.
// The frames have to come from the production assembly or that test proves only
// that a hand-written array round-trips through a reducer.
const traceFixturePath = "../../../desktop/frontend-next/src/e2e/fixtures/trace_readings.json"

// traceFixture is one turn as both readings, with the run-to-run noise removed
// so the file is stable enough to commit and diff.
type traceFixture struct {
	Name   string            `json:"name"`
	Live   []eventwire.Event `json:"live"`
	Replay []eventwire.Event `json:"replay"`
}

// stabilize removes what differs between two identical runs: host-local attempt
// and barrier ids become ordinals, clocks and sizes go to zero, and tool bodies
// drop out — they carry the temp paths this harness runs in. What is left is
// the structure both readings are compared on.
func stabilize(in []eventwire.Event) []eventwire.Event {
	rounds, barriers := map[string]string{}, map[string]string{}
	name := func(m map[string]string, prefix, id string) string {
		if id == "" {
			return ""
		}
		if v, ok := m[id]; ok {
			return v
		}
		v := fmt.Sprintf("%s%d", prefix, len(m)+1)
		m[id] = v
		return v
	}
	out := make([]eventwire.Event, 0, len(in))
	for _, e := range in {
		e.Seq = 0
		if e.StreamAttempt != nil {
			sa := *e.StreamAttempt
			sa.ID = name(rounds, "r", sa.ID)
			e.StreamAttempt = &sa
		}
		if e.Approval != nil {
			a := *e.Approval
			a.ID = name(barriers, "b", a.ID)
			a.Subject = ""
			e.Approval = &a
		}
		if e.Ask != nil {
			a := *e.Ask
			a.ID = name(barriers, "b", a.ID)
			e.Ask = &a
		}
		if e.DecisionReceipt != nil {
			r := *e.DecisionReceipt
			r.ID = name(barriers, "b", r.ID)
			e.DecisionReceipt = &r
		}
		if e.Tool != nil {
			t := *e.Tool
			t.AttemptID = name(rounds, "r", t.AttemptID)
			t.Args, t.Output, t.Err, t.Diff = "", "", "", ""
			t.DurationMs, t.ContextTokens, t.StartedAt, t.EndedAt, t.ArgChars = 0, 0, 0, 0, 0
			t.Added, t.Removed = 0, 0
			e.Tool = &t
		}
		if e.WorkspaceLease != nil {
			l := *e.WorkspaceLease
			l.WaitedMs, l.HeldMs, l.IdleMs = 0, 0, 0 // measured durations, not turn structure
			e.WorkspaceLease = &l
		}
		if e.Usage != nil {
			continue // billing numbers, not turn structure
		}
		if e.Completion != nil || e.Receipt != nil || e.Maintenance != nil {
			continue // verdicts about the turn, not the facts it is made of
		}
		out = append(out, e)
	}
	return out
}

// traceFixtureCases are the fixtures both parity layers share, so the desktop
// reducer is fed exactly the turns the wire gate proved.
func traceFixtureCases() map[string]traceCase {
	return map[string]traceCase{
		"single_model": {input: "go", executor: [][]provider.Chunk{
			{think("weighing it"), say("first"), callTodo("call-a", "a")},
			{say("second"), callTodo("call-b", "b")},
			{say("third")},
		}},
		"dual_model": {
			input:    control.PlannerRouteMarker + " build it",
			planner:  [][]provider.Chunk{{say("here is the plan")}},
			executor: [][]provider.Chunk{{say("running it"), callTodo("call-x", "x")}, {say("done")}},
		},
		"provider_result_only": {input: "go", executor: [][]provider.Chunk{
			{provider.Chunk{Type: provider.ChunkProviderTool, Text: "two results",
				ToolCall: &provider.ToolCall{ID: "srv-1", Name: "web_search", Arguments: `{"q":"x"}`}},
				say("summarised")},
		}},
		"approval_inside_call": {input: "go", interactive: true, executor: [][]provider.Chunk{
			{say("writing"), provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{
				ID: "call-write", Name: "write_file", Arguments: `{"path":"note.txt","content":"hello"}`,
			}}},
			{say("done")},
		}},
	}
}

// The desktop reducer parity test reads a committed file, so this is what stops
// it from being checked against a shape the kernel stopped producing. Run with
// REASONIX_UPDATE_TRACE_FIXTURES=1 to regenerate after a deliberate change.
func TestTraceFixturesMatchWhatTheKernelProduces(t *testing.T) {
	cases := traceFixtureCases()
	names := make([]string, 0, len(cases))
	for name := range cases {
		names = append(names, name)
	}
	fresh := make(map[string]traceFixture, len(cases))
	for _, name := range names {
		tc := cases[name]
		t.Run(name, func(t *testing.T) {
			r := runTrace(t, tc)
			assertParity(t, r)
			fresh[name] = traceFixture{Name: name, Live: stabilize(r.live), Replay: stabilize(r.replay)}
		})
	}
	if t.Failed() {
		return
	}
	body, err := json.MarshalIndent(fresh, "", " ")
	if err != nil {
		t.Fatalf("encode fixtures: %v", err)
	}
	body = append(body, '\n')
	if os.Getenv("REASONIX_UPDATE_TRACE_FIXTURES") != "" {
		if err := os.MkdirAll(filepath.Dir(traceFixturePath), 0o755); err != nil {
			t.Fatalf("fixture dir: %v", err)
		}
		if err := os.WriteFile(traceFixturePath, body, 0o644); err != nil {
			t.Fatalf("write fixtures: %v", err)
		}
		return
	}
	have, err := os.ReadFile(traceFixturePath)
	if err != nil {
		t.Fatalf("read fixtures: %v\nregenerate with REASONIX_UPDATE_TRACE_FIXTURES=1", err)
	}
	if strings.TrimSpace(string(have)) != strings.TrimSpace(string(body)) {
		t.Fatalf("the committed readings are not what the kernel produces now.\n" +
			"The desktop reducer parity test reads them, so a stale file would check\n" +
			"that reducer against a shape nothing emits. Regenerate with\n" +
			"  REASONIX_UPDATE_TRACE_FIXTURES=1 go test ./internal/frontend/serve/ -run TestTraceFixtures")
	}
}
