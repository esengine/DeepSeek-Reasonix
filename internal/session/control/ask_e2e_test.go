package control

import (
	"context"
	"reasonix/internal/state/sessionstore"
	"strings"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/safety/permission"
)

// askQuestionArgs is the model calling `ask` with one two-option question.
const askQuestionArgs = `{"questions":[{"header":"Lib","question":"Which one?",` +
	`"options":[{"label":"A"},{"label":"B"}]}]}`

// lastToolResult is what the ask returned, read where it reaches the model: the
// only place that tells an answer from the fallback the tool returns when it
// finds no asker on the call context.
func lastToolResult(p *recordingProvider) string {
	if len(p.requests) == 0 {
		return ""
	}
	out := ""
	for _, m := range p.requests[len(p.requests)-1].Messages {
		if m.Role == provider.RoleTool {
			out = m.Content
		}
	}
	return out
}

// The ask tool reaches the user through one wire and one only: the Asker that
// EnableInteractiveApproval installs on the executor. Nothing asserted the wire
// was live — so an assembly that stopped installing it answers every question
// with the headless model-assumption fallback, deciding for the user instead of
// asking, and no gate says a word (#9509).
func TestAskReachesTheUserOnceInteractiveApprovalIsOn(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(agent.NewAskTool())

	prov := &recordingProvider{streams: [][]provider.Chunk{
		toolCallTurn("a1", "ask", askQuestionArgs),
		textTurn("Done."),
	}}
	ag := agent.New(prov, reg, sessionstore.NewSession(""), agent.Options{}, event.Discard)

	asked := make(chan event.Ask, 1)
	c := New(Options{
		Runner:   ag,
		Executor: ag,
		Policy:   permission.New("ask", nil, nil, nil),
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.AskRequest {
				asked <- e.Ask
			}
		}),
	})
	c.EnableInteractiveApproval()

	go func() {
		a := <-asked
		c.AnswerQuestion(a.ID, []event.AskAnswer{{QuestionID: a.Questions[0].ID, Selected: []string{"B"}}})
	}()

	if err := c.runOneTurn(context.Background(), orchestratedTurn{input: "pick one", raw: "pick one"}); err != nil {
		t.Fatalf("runOneTurn: %v", err)
	}

	result := lastToolResult(prov)
	if result == "" {
		t.Fatal("no tool result reached the model, so the ask never returned one")
	}
	if strings.Contains(result, "unresolved") {
		t.Fatalf("ask left the decision unresolved while a user was connected: %s", result)
	}
	if !strings.Contains(result, "B") {
		t.Fatalf("the user picked B and the model was told %q", result)
	}
}

// The headless half of the same contract: with no interactive approval wired
// there is nobody to answer, and the run must say so in the result rather than
// blocking on a question no one will ever see.
func TestAskLeavesTheDecisionUnresolvedWhenNoAskerIsWired(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(agent.NewAskTool())

	prov := &recordingProvider{streams: [][]provider.Chunk{
		toolCallTurn("a1", "ask", askQuestionArgs),
		textTurn("Done."),
	}}
	ag := agent.New(prov, reg, sessionstore.NewSession(""), agent.Options{}, event.Discard)

	c := New(Options{
		Runner:   ag,
		Executor: ag,
		Policy:   permission.New("ask", nil, nil, nil),
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.AskRequest {
				t.Error("a headless run raised a question nobody can answer")
			}
		}),
	})

	if err := c.runOneTurn(context.Background(), orchestratedTurn{input: "pick one", raw: "pick one"}); err != nil {
		t.Fatalf("runOneTurn: %v", err)
	}
	if result := lastToolResult(prov); !strings.Contains(result, "unresolved") || !strings.Contains(result, "conclude_blocked") {
		t.Fatalf("a headless ask returned %q, want the decision left unresolved", result)
	}
}

// Approval posture is about tool side effects. It cannot manufacture user
// intent, so no mode answers a question on the user's behalf — the point of
// asking is that the answer is not the agent's to produce, and auto-approving
// writes says nothing about which option the user wants.
func TestNoApprovalPostureAnswersAQuestionForTheUser(t *testing.T) {
	for _, mode := range []string{ToolApprovalAuto, ToolApprovalYolo} {
		t.Run(mode, func(t *testing.T) {
			reg := tool.NewRegistry()
			reg.Add(agent.NewAskTool())
			prov := &recordingProvider{streams: [][]provider.Chunk{
				toolCallTurn("a1", "ask", askQuestionArgs),
				textTurn("Done."),
			}}
			ag := agent.New(prov, reg, sessionstore.NewSession(""), agent.Options{}, event.Discard)

			asked := make(chan event.Ask, 1)
			c := New(Options{
				Runner:   ag,
				Executor: ag,
				Policy:   permission.New("ask", nil, nil, nil),
				Sink: event.FuncSink(func(e event.Event) {
					if e.Kind == event.AskRequest {
						asked <- e.Ask
					}
				}),
			})
			defer c.Close()
			c.EnableInteractiveApproval()
			c.SetToolApprovalMode(mode)

			// Closed before answering, so the turn cannot finish ahead of it.
			userAsked := make(chan struct{})
			go func() {
				a := <-asked
				close(userAsked)
				c.AnswerQuestion(a.ID, []event.AskAnswer{{QuestionID: a.Questions[0].ID, Selected: []string{"B"}}})
			}()

			if err := c.runOneTurn(context.Background(), orchestratedTurn{input: "pick one", raw: "pick one"}); err != nil {
				t.Fatalf("runOneTurn: %v", err)
			}
			select {
			case <-userAsked:
			default:
				t.Fatalf("%s answered the question itself; the user was never asked", mode)
			}
			if result := lastToolResult(prov); !strings.Contains(result, "B") {
				t.Fatalf("%s: the user picked B and the model was told %q", mode, result)
			}
		})
	}
}
