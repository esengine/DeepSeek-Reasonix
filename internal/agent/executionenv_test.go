package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/tool"
)

type countingAsker struct {
	asked int
}

func (a *countingAsker) Ask(context.Context, []event.AskQuestion) ([]event.AskAnswer, error) {
	a.asked++
	return []event.AskAnswer{{QuestionID: "q1", Selected: []string{"yes"}}}, nil
}

const askArgs = `{"questions":[{"question":"ship it?","header":"ship","options":[{"label":"yes"},{"label":"no"}]}]}`

// The point of the parameter is that omitting the capability is visible. A tool
// reached through ExecuteEnv must use what it was handed and must not fall back
// to whatever an ambient context happens to carry.
func TestAskToolUsesTheEnvAskerNotTheContext(t *testing.T) {
	fromEnv, fromContext := &countingAsker{}, &countingAsker{}
	ctx := installCallEnv(context.Background(), tool.ExecutionEnv{Asker: fromContext})

	out, err := (&AskTool{}).ExecuteEnv(ctx, tool.ExecutionEnv{Asker: fromEnv}, json.RawMessage(askArgs))
	if err != nil {
		t.Fatalf("ExecuteEnv: %v", err)
	}
	if fromEnv.asked != 1 {
		t.Errorf("env asker consulted %d times, want 1", fromEnv.asked)
	}
	if fromContext.asked != 0 {
		t.Error("the context asker was consulted; the parameter must win, or it records nothing")
	}
	if strings.TrimSpace(out) == "" {
		t.Error("no answer rendered")
	}
}

// A nil Asker is the headless case and must stay a stated fallback rather than
// an error, because an autonomous run has no user to answer.
func TestAskToolWithoutAnAskerSaysSoInsteadOfFailing(t *testing.T) {
	out, err := (&AskTool{}).ExecuteEnv(context.Background(), tool.ExecutionEnv{}, json.RawMessage(askArgs))
	if err != nil {
		t.Fatalf("ExecuteEnv: %v", err)
	}
	if !strings.Contains(out, "model-assumption fallback") {
		t.Errorf("output = %q, want the provenance stated so the model does not read it as a user choice", out)
	}
}

// The legacy path has to keep working while tools migrate one at a time, so the
// context the agent installs still reaches a tool through plain Execute.
func TestAskToolLegacyExecuteStillReadsTheInstalledContext(t *testing.T) {
	asker := &countingAsker{}
	ctx := installCallEnv(context.Background(), tool.ExecutionEnv{Asker: asker})

	if _, err := (&AskTool{}).Execute(ctx, json.RawMessage(askArgs)); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if asker.asked != 1 {
		t.Errorf("installed asker consulted %d times, want 1", asker.asked)
	}
}

// installCallEnv is the only place the legacy keys are written. If a second
// writer appears, a tool can be handed one env and read another.
func TestInstalledEnvMatchesWhatTheToolLayerWasGiven(t *testing.T) {
	sink := event.Discard
	env := tool.ExecutionEnv{
		Call:     tool.CallIdentity{ID: "call-7"},
		Sink:     sink,
		Asker:    &countingAsker{},
		PlanMode: true,
	}
	ctx := installCallEnv(context.Background(), env)

	id, gotSink, gotAsker, ok := CallContext(ctx)
	if !ok {
		t.Fatal("CallContext found nothing after installCallEnv")
	}
	// event.Sink is not comparable, so identity is checked on what is: a nil
	// sink here would mean a tool loses the card it should nest under.
	if id != env.Call.ID || gotAsker != env.Asker || gotSink == nil {
		t.Errorf("installed call context = (%q, sink!=nil:%t, %v), want it to match the env", id, gotSink != nil, gotAsker)
	}
	if !PlanModeFromContext(ctx) {
		t.Error("plan mode did not survive the install; it travels under a second key for tools that cannot import this package")
	}
}
