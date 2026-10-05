package delegation

import (
	"context"
	"encoding/json"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/runtime/writeclaim"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
)

// The other half of the same rule: a run nobody is waiting on in front of must
// not stop for an answer. Several of them can be in flight at once, and a
// question from one of many has no reader who knows which asked it.
func TestABackgroundRunKeepsTheHeadlessFallback(t *testing.T) {
	asker := &recordingAsker{}
	reg := tool.NewRegistry()
	reg.Add(agent.NewAskTool())
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{toolCallChunk("1", "ask", `{"questions":[{"header":"Direction","question":"Which path?","options":[{"label":"Keep going"},{"label":"Stop"}]}]}`), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}},
	}}
	task := NewTaskTool(prov, nil, reg, 20, 0, 0, 0, 0.0, "", "sys", nil, 0, "", "", nil).
		WithTranscripts(mustSubagentStore(t), testenv.TempDir(t), "base", "high").
		WithScheduler(writeclaim.NewSubagentScheduler(4, 4))

	// What fleet items and parallel tasks build: the same call context with the
	// asker deliberately dropped.
	ctx := agent.WithCallContext(context.Background(), "item-1", event.Discard, nil, false)
	args, err := json.Marshal(map[string]string{"prompt": "Decide which path to take."})
	if err != nil {
		t.Fatal(err)
	}
	out, err := task.Execute(ctx, args)
	if err != nil {
		t.Fatalf("task: %v", err)
	}
	if len(asker.questions) != 0 {
		t.Fatalf("a run with no asker on its context reached one anyway: %v", asker.questions)
	}
	if strings.TrimSpace(out) == "" {
		t.Fatal("the run produced nothing")
	}
}

// A delegated run is the parent blocked on one child, so exactly one question
// can be outstanding and there is a person waiting on the other end of it.
// Reported as the ask tool never reaching the user in Studio: the child was
// built with no asker and answered itself with the headless fallback, which the
// model then repeated at the user as a fault to fix.
func TestADelegatedRunReachesTheAskerTheParentWasGiven(t *testing.T) {
	asker := &recordingAsker{}
	reg := tool.NewRegistry()
	reg.Add(agent.NewAskTool())
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{toolCallChunk("1", "ask", `{"questions":[{"header":"Direction","question":"Which path?","options":[{"label":"Keep going"},{"label":"Stop"}]}]}`), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}},
	}}
	task := NewTaskTool(prov, nil, reg, 20, 0, 0, 0, 0.0, "", "sys", nil, 0, "", "", nil).
		WithTranscripts(mustSubagentStore(t), testenv.TempDir(t), "base", "high").
		WithScheduler(writeclaim.NewSubagentScheduler(4, 4))

	ctx := agent.WithCallContext(context.Background(), "call-1", event.Discard, asker, false)
	args, err := json.Marshal(map[string]string{"prompt": "Decide which path to take."})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := task.Execute(ctx, args); err != nil {
		t.Fatalf("task: %v", err)
	}
	if len(asker.questions) != 1 {
		t.Fatalf("the asker was reached %d times, want 1: a delegated ask answered itself", len(asker.questions))
	}
	if asker.questions[0].Prompt != "Which path?" {
		t.Fatalf("question = %q, want the child's own", asker.questions[0].Prompt)
	}
}
