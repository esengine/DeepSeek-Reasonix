package serve

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"reasonix/internal/assembly/boot"
	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/eventwire"
	"reasonix/internal/contract/provider"
	"reasonix/internal/session/control"
)

// traceScript is one stubbed model: round i answers with script[i]. It stands
// in for the provider only — everything downstream of it is the production
// assembly, which is the point of driving the fixtures this way.
type traceScript struct {
	mu     sync.Mutex
	round  int
	script [][]provider.Chunk
}

func (p *traceScript) Name() string { return "trace-script" }

func (p *traceScript) Stream(context.Context, provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	i := p.round
	p.round++
	p.mu.Unlock()
	var chunks []provider.Chunk
	if i < len(p.script) {
		chunks = p.script[i]
	} else {
		chunks = []provider.Chunk{{Type: provider.ChunkText, Text: "done"}}
	}
	ch := make(chan provider.Chunk, len(chunks)+1)
	for _, c := range chunks {
		ch <- c
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

// traceReading is one turn read twice: what a connected client saw as it
// happened, and what a cold reload reads back from the durable log.
type traceReading struct {
	live         []eventwire.Event
	replay       []eventwire.Event
	availability trajectoryAvailability
}

// traceCase is one fixture: what the executor answers, and — when the fixture
// is a two-model turn — what the planner answers first.
type traceCase struct {
	input    string
	executor [][]provider.Chunk
	planner  [][]provider.Chunk
	// interactive wires the host's question surfaces, which is what makes an
	// approval or an ask reachable at all: without them a tool call is either
	// auto-allowed or told to decide for itself, and no barrier ever lands.
	interactive bool
}

// runTrace drives one turn through control -> eventwire -> broadcaster ->
// wirelog -> the real /trajectory handler, and returns both readings.
func runTrace(t *testing.T, tc traceCase) traceReading {
	t.Helper()
	dir := testenv.TempDir(t)
	kind := "trace-script-" + t.Name()
	provider.Register(kind, func(provider.Config) (provider.Provider, error) {
		return &traceScript{script: tc.executor}, nil
	})
	agentBlock := "[agent]\nsystem_prompt = \"BASE\"\n"
	providers := "[[providers]]\nname = \"test-model\"\nkind = \"" + kind + "\"\nmodel = \"x\"\n"
	if tc.planner != nil {
		pkind := kind + "-planner"
		provider.Register(pkind, func(provider.Config) (provider.Provider, error) {
			return &traceScript{script: tc.planner}, nil
		})
		agentBlock += "planner_model = \"planner-model\"\n"
		providers += "\n[[providers]]\nname = \"planner-model\"\nkind = \"" + pkind + "\"\nmodel = \"p\"\n"
	}
	toml := "default_model = \"test-model\"\n\n" + agentBlock + "\n" + providers
	if err := os.WriteFile(filepath.Join(dir, "reasonix.toml"), []byte(toml), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	// The home this run states is where the person's approval would be.
	if _, err := config.RootsForHome(dir).ApproveWorkspacePrograms(dir); err != nil {
		t.Fatal(err)
	}

	bc := NewBroadcaster()
	// Subscribed before the turn and before the server: a client that connected
	// late is not the reading this compares against.
	ch, unsubscribe := bc.Subscribe()
	defer unsubscribe()
	var mu sync.Mutex
	var live []eventwire.Event
	barriers := make(chan eventwire.Event, 8)
	drained := make(chan struct{})
	turnDone := make(chan struct{})
	progress := make(chan struct{}, 1)
	go func() {
		defer close(drained)
		closed := false
		for frame := range ch {
			var e eventwire.Event
			if err := json.Unmarshal(frame.Data, &e); err != nil {
				continue
			}
			mu.Lock()
			live = append(live, e)
			mu.Unlock()
			select {
			case progress <- struct{}{}:
			default:
			}
			if tc.interactive && (e.Kind == "approval_request" || e.Kind == "ask_request") {
				select {
				case barriers <- e:
				default:
				}
			}
			if e.Kind == "turn_done" && !closed {
				closed = true
				close(turnDone)
			}
		}
	}()

	ctx := context.Background()
	ctrl, err := boot.Build(ctx, boot.Options{Sink: bc, Home: dir, WorkspaceRoot: dir})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	ctrl.SetFreshSessionPath(filepath.Join(dir, "sessions", "trace.jsonl"))
	if err := os.MkdirAll(filepath.Join(dir, "sessions"), 0o700); err != nil {
		t.Fatalf("session dir: %v", err)
	}
	// New attaches the wire log, so the durable record starts here.
	s := New(ctrl, bc, config.ServeConfig{})
	if tc.interactive {
		ctrl.EnableInteractiveApproval()
		go answerBarriers(ctrl, barriers)
	}

	// The serve frontend's own entry point, because the turn boundary is part of
	// what this compares: Run is synchronous and closes no turn, so a harness
	// built on it would be measuring a shape no frontend produces.
	ctrl.SubmitHTTP(tc.input)
	// A turn is hung when it goes silent, not when it is long; one that never
	// stops emitting is bounded by a total ceiling.
	quiet := testenv.Budget(t) / 2
	ceiling := 5 * quiet
	if deadline, ok := t.Deadline(); ok {
		ceiling = min(ceiling, time.Until(deadline)*9/10)
	}
	idle := time.NewTimer(quiet)
	defer idle.Stop()
	overall := time.NewTimer(ceiling)
	defer overall.Stop()
wait:
	for {
		select {
		case <-turnDone:
			break wait
		case <-progress:
			idle.Reset(quiet)
		case <-idle.C:
			t.Fatal("the turn went silent before it closed")
		case <-overall.C:
			t.Fatal("the turn kept emitting but never closed")
		}
	}

	view := settledTrajectory(t, s)
	unsubscribe()
	<-drained
	mu.Lock()
	defer mu.Unlock()
	replay := make([]eventwire.Event, 0, len(view.Events))
	for _, raw := range view.Events {
		var e eventwire.Event
		if err := json.Unmarshal(raw, &e); err != nil {
			t.Fatalf("replayed frame is not an event: %v", err)
		}
		replay = append(replay, e)
	}
	return traceReading{live: append([]eventwire.Event(nil), live...), replay: replay, availability: view.Availability}
}

// settledTrajectory reads /trajectory until the durable log stops growing. The
// log is written from a subscription, so it lands after the turn returns;
// waiting on the observable count rather than a sleep is what keeps this from
// comparing a reading against a log still being written.
func settledTrajectory(t *testing.T, s *Server) trajectoryView {
	t.Helper()
	deadline := time.Now().Add(testenv.Budget(t) / 4)
	var last trajectoryView
	stable := 0
	for time.Now().Before(deadline) {
		rec := httptest.NewRecorder()
		s.trajectory(rec, httptest.NewRequest(http.MethodGet, "/trajectory", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /trajectory = %d %s", rec.Code, rec.Body.String())
		}
		var view trajectoryView
		if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
			t.Fatalf("decode trajectory: %v", err)
		}
		if len(view.Events) == len(last.Events) && len(view.Events) > 0 {
			stable++
			if stable == 3 {
				return view
			}
		} else {
			stable = 0
		}
		last = view
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("the durable log never settled; last read had %d frames", len(last.Events))
	return last
}

// assertParity is the gate: the same turn, read live and read cold, yields the
// same structural facts. It compares what a partition would be computed from,
// never a partition — that is the thing under study, and comparing it here
// would only prove both readings ran the same algorithm.
func assertParity(t *testing.T, r traceReading) TurnTraceFacts {
	t.Helper()
	if r.availability != trajectoryComplete {
		t.Fatalf("the fixture's own trajectory is %q; a prefix cannot answer a parity question", r.availability)
	}
	liveFacts := traceFacts(r.live, true)
	replayFacts := traceFacts(r.replay, false)
	if d := liveFacts.diff(replayFacts); d != "" {
		t.Fatalf("live and cold replay disagree about what happened\n%s", d)
	}
	return liveFacts
}

func say(text string) provider.Chunk { return provider.Chunk{Type: provider.ChunkText, Text: text} }
func think(text string) provider.Chunk {
	return provider.Chunk{Type: provider.ChunkReasoning, Text: text}
}
func callTodo(id, content string) provider.Chunk {
	return provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{
		ID: id, Name: "todo_write",
		Arguments: `{"todos":[{"content":"` + content + `","status":"pending"}]}`,
	}}
}

// The shape the corpus said is the common one: a say, a call, a say, a call, a
// say. Every one of those facts has to survive the trip to disk — the deltas
// that carried the text do not, so the settled frame is the only thing a cold
// reload has to rebuild them from.
func TestSingleModelTurnReadsTheSameLiveAndCold(t *testing.T) {
	r := runTrace(t, traceCase{input: "go", executor: [][]provider.Chunk{
		{think("weighing it"), say("first"), callTodo("call-a", "a")},
		{say("second"), callTodo("call-b", "b")},
		{say("third")},
	}})
	facts := assertParity(t, r)
	if len(facts.Messages) != 3 {
		t.Fatalf("settled says = %d, want 3: %+v", len(facts.Messages), facts.Messages)
	}
	for i, m := range facts.Messages {
		if !m.HasText {
			t.Errorf("say %d survived the trip with no text", i)
		}
	}
	// The first round reasoned before it spoke. If a cold reload cannot say a
	// round reasoned, this fixture has to fail rather than pass by agreeing that
	// neither reading knows.
	if !facts.Messages[0].HasReasoning {
		t.Error("the round that reasoned reads as one that did not")
	}
	if len(facts.Calls) != 2 {
		t.Fatalf("tool calls = %d, want 2: %+v", len(facts.Calls), facts.Calls)
	}
	for _, c := range facts.Calls {
		if c.Issuer != "model" || !c.SawDispatch || !c.SawResult {
			t.Errorf("call %q = %+v, want a model-issued call with both halves", c.ID, c)
		}
	}
	if facts.Terminal != "explicit" {
		t.Fatalf("terminal = %q, want an explicit turn_done", facts.Terminal)
	}
}

// A two-model turn is where the producer axis earns its keep. The assertion is
// on the values, not on "the source changed": two readings that both got it
// wrong the same way would satisfy the weaker one.
func TestTwoModelTurnKeepsEachProducersOwnFacts(t *testing.T) {
	r := runTrace(t, traceCase{
		input:    control.PlannerRouteMarker + " build it",
		planner:  [][]provider.Chunk{{say("here is the plan")}},
		executor: [][]provider.Chunk{{say("running it"), callTodo("call-x", "x")}, {say("done")}},
	})
	facts := assertParity(t, r)
	var sources []string
	for _, m := range facts.Messages {
		sources = append(sources, m.Source)
	}
	if len(sources) < 2 || sources[0] != "planner" {
		t.Fatalf("settled say sources = %v, want the planner's first", sources)
	}
	if sources[len(sources)-1] != "executor" {
		t.Fatalf("settled say sources = %v, want the executor's last", sources)
	}
	if len(facts.Calls) != 1 {
		t.Fatalf("tool calls = %d, want 1: %+v", len(facts.Calls), facts.Calls)
	}
	if c := facts.Calls[0]; c.Source != "executor" || c.Issuer != "model" {
		t.Fatalf("the executor's call = source %q issuer %q, want executor/model", c.Source, c.Issuer)
	}
}

// The host advances a task list itself, and the frame it emits is shaped
// exactly like the model's own todo_write. Same turn, same shape, different
// issuer — which is the only thing that tells them apart on either reading.
func TestHostIssuedAndModelIssuedCallsStayApartInOneTurn(t *testing.T) {
	r := runTrace(t, traceCase{input: "go", executor: [][]provider.Chunk{
		{say("planning"), provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{
			ID: "call-list", Name: "todo_write",
			Arguments: `{"todos":[{"content":"read the file","status":"in_progress"},{"content":"then write","status":"pending"}]}`,
		}}},
		// The sign-off cites proof the session produced, so the read comes
		// first: an unbacked completion is rejected and the host advances
		// nothing, leaving this fixture nothing to tell apart.
		{provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{
			ID: "call-read", Name: "read_file", Arguments: `{"path":"reasonix.toml"}`,
		}}},
		{provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{
			ID: "call-signoff", Name: "complete_step",
			Arguments: `{"step":"read the file","result":"read it","evidence":[{"kind":"files","summary":"looked at it","paths":["reasonix.toml"]}]}`,
		}}},
		{say("done")},
	}})
	facts := assertParity(t, r)
	byIssuer := map[string][]string{}
	for _, c := range facts.Calls {
		byIssuer[c.Issuer] = append(byIssuer[c.Issuer], c.ID)
	}
	if len(byIssuer["model"]) == 0 {
		t.Fatalf("no model-issued calls in the turn: %+v", facts.Calls)
	}
	if len(byIssuer["host"]) == 0 {
		t.Fatalf("the host advanced no list, so this fixture proves nothing about "+
			"telling the two apart: %+v", facts.Calls)
	}
}

// A provider-side call has a result and no dispatch. A cold reload must read it
// as the call it is, not invent the dispatch it never had in order to make the
// two readings look alike.
func TestProviderSideCallSurvivesWithNoDispatchOnEitherReading(t *testing.T) {
	r := runTrace(t, traceCase{input: "go", executor: [][]provider.Chunk{
		{provider.Chunk{Type: provider.ChunkProviderTool, Text: "two results",
			ToolCall: &provider.ToolCall{ID: "srv-1", Name: "web_search", Arguments: `{"q":"x"}`}},
			say("summarised")},
	}})
	facts := assertParity(t, r)
	var found *CallFact
	for i := range facts.Calls {
		if facts.Calls[i].ID == "srv-1" {
			found = &facts.Calls[i]
		}
	}
	if found == nil {
		t.Fatalf("the provider-side call did not survive to either reading: %+v", facts.Calls)
	}
	if found.SawDispatch || !found.SawResult {
		t.Fatalf("provider-side call = %+v, want a result and no dispatch", *found)
	}
	if found.Issuer != "provider" {
		t.Fatalf("provider-side call issuer = %q, want provider", found.Issuer)
	}
}

// The turn identity is the anchor everything else hangs off. A reload that
// cannot say which authored turn it is looking at cannot attribute anything in
// it either.
func TestTurnIdentitySurvivesToTheColdReading(t *testing.T) {
	r := runTrace(t, traceCase{input: "go", executor: [][]provider.Chunk{{say("hi")}}})
	facts := assertParity(t, r)
	if facts.AuthoredTurn == nil || facts.MsgIndex == nil {
		t.Fatalf("the turn arrived without an identity: authored=%v msgIndex=%v",
			facts.AuthoredTurn, facts.MsgIndex)
	}
	if *facts.AuthoredTurn < 1 {
		t.Fatalf("authored turn = %d, want the conversation's first", *facts.AuthoredTurn)
	}
}

// answerBarriers plays the person: it allows what is asked and picks the first
// option offered. Which way it answers is not what this measures — that a
// barrier lands inside a call and both readings say so is.
func answerBarriers(ctrl *control.Controller, in <-chan eventwire.Event) {
	for e := range in {
		switch {
		case e.Approval != nil:
			ctrl.Approve(e.Approval.ID, true, false, false)
		case e.Ask != nil:
			answers := make([]event.AskAnswer, 0, len(e.Ask.Questions))
			for _, q := range e.Ask.Questions {
				if len(q.Options) == 0 {
					continue
				}
				answers = append(answers, event.AskAnswer{QuestionID: q.ID, Selected: []string{q.Options[0].Label}})
			}
			ctrl.AnswerQuestion(e.Ask.ID, answers)
		}
	}
}

// A barrier lands between a call's dispatch and its result: it interrupts one
// call rather than separating two. Both readings have to preserve that, because
// a partition built on the other reading splits a call in half.
func TestApprovalLandsInsideACallOnBothReadings(t *testing.T) {
	r := runTrace(t, traceCase{input: "go", interactive: true, executor: [][]provider.Chunk{
		{say("writing"), provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{
			ID: "call-write", Name: "write_file",
			Arguments: `{"path":"note.txt","content":"hello"}`,
		}}},
		{say("done")},
	}})
	facts := assertParity(t, r)
	var approvals int
	for _, i := range facts.Interruptions {
		if i.Kind != "approval_request" {
			continue
		}
		approvals++
		if i.InsideOf != "call-write" {
			t.Fatalf("the approval landed at %q, want inside call-write — a barrier "+
				"between two calls is a different fact from one interrupting one", i.InsideOf)
		}
	}
	if approvals == 0 {
		t.Fatalf("no approval barrier landed, so this fixture proves nothing: %+v", facts)
	}
}

// The ask tool is the model handing a decision back. It is a barrier the same
// way an approval is, and the same fact has to survive to the cold reading.
func TestAskLandsInsideACallOnBothReadings(t *testing.T) {
	r := runTrace(t, traceCase{input: "go", interactive: true, executor: [][]provider.Chunk{
		{provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{
			ID: "call-ask", Name: "ask",
			Arguments: `{"questions":[{"header":"Store","question":"Which store?","reason":"user_decision",` +
				`"options":[{"label":"sqlite"},{"label":"postgres"}]}]}`,
		}}},
		{say("going with sqlite")},
	}})
	facts := assertParity(t, r)
	var asks int
	for _, i := range facts.Interruptions {
		if i.Kind != "ask_request" {
			continue
		}
		asks++
		if i.InsideOf != "call-ask" {
			t.Fatalf("the ask landed at %q, want inside call-ask", i.InsideOf)
		}
	}
	if asks == 0 {
		t.Fatalf("no ask barrier landed, so this fixture proves nothing: %+v", facts)
	}
}

// The three destructive controls. Each removes one fact from the cold reading
// and requires the gate to notice: a parity test that passes without a field is
// a test that was never reading it.

func mapFrames(in []eventwire.Event, f func(*eventwire.Event)) []eventwire.Event {
	out := make([]eventwire.Event, len(in))
	copy(out, in)
	for i := range out {
		if out[i].Tool != nil {
			t := *out[i].Tool
			out[i].Tool = &t
		}
		f(&out[i])
	}
	return out
}

// Without the producer on the frame, a two-model turn reads as one producer —
// which is what every reader had before the field existed.
func TestParityFailsWhenTheColdReadingLosesTheProducer(t *testing.T) {
	r := runTrace(t, traceCase{
		input:    control.PlannerRouteMarker + " build it",
		planner:  [][]provider.Chunk{{say("here is the plan")}},
		executor: [][]provider.Chunk{{say("running it"), callTodo("call-x", "x")}, {say("done")}},
	})
	assertParity(t, r)
	blind := mapFrames(r.replay, func(e *eventwire.Event) { e.Source = "" })
	if d := traceFacts(r.live, true).diff(traceFacts(blind, false)); d == "" {
		t.Fatal("the cold reading lost every producer label and the gate said the readings agree")
	}
}

// Without the issuer, the host's own bookkeeping and the model's work are the
// same frame. This is P2-2a's field being load-bearing rather than merely sent.
func TestParityFailsWhenTheColdReadingLosesTheIssuer(t *testing.T) {
	r := runTrace(t, traceCase{input: "go", executor: [][]provider.Chunk{
		{say("planning"), provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{
			ID: "call-list", Name: "todo_write",
			Arguments: `{"todos":[{"content":"read the file","status":"in_progress"},{"content":"then write","status":"pending"}]}`,
		}}},
		{provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{
			ID: "call-read", Name: "read_file", Arguments: `{"path":"reasonix.toml"}`,
		}}},
		{provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{
			ID: "call-signoff", Name: "complete_step",
			Arguments: `{"step":"read the file","result":"read it","evidence":[{"kind":"files","summary":"looked at it","paths":["reasonix.toml"]}]}`,
		}}},
		{say("done")},
	}})
	assertParity(t, r)
	blind := mapFrames(r.replay, func(e *eventwire.Event) {
		if e.Tool != nil {
			e.Tool.Issuer = ""
		}
	})
	if d := traceFacts(r.live, true).diff(traceFacts(blind, false)); d == "" {
		t.Fatal("the cold reading lost every issuer and the gate said the readings agree")
	}
}

// A settled message that only seals, without materializing what was said, is
// what the record held before ba4cc4396. A connected client still has the
// deltas; a cold reload has nothing, and the gate has to see that.
func TestParityFailsWhenTheSettledSayStopsMaterializing(t *testing.T) {
	r := runTrace(t, traceCase{input: "go", executor: [][]provider.Chunk{
		{say("first"), callTodo("call-a", "a")},
		{say("second")},
	}})
	assertParity(t, r)
	sealed := mapFrames(r.replay, func(e *eventwire.Event) {
		if e.Kind == "message" {
			e.Text = ""
		}
	})
	if d := traceFacts(r.live, true).diff(traceFacts(sealed, false)); d == "" {
		t.Fatal("the cold reading lost every say's text and the gate said the readings agree")
	}
}
