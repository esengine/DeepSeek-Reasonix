package agent

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/evidence"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func planTodos() []evidence.TodoItem {
	return []evidence.TodoItem{
		{Content: "Wire the parser", Status: "completed", StepID: "plan_step_01"},
		{Content: "Add the tests", Status: "in_progress", StepID: "plan_step_02"},
		{Content: "Ship it", Status: "pending", StepID: "plan_step_03"},
	}
}

func TestTodoCitationPrefersTheStableID(t *testing.T) {
	if got := evidence.TodoCitation("plan_step_02", 2, "Add the tests"); got != "[plan_step_02] Add the tests" {
		t.Fatalf("citation = %q, want the stable id", got)
	}
	// A list that never carried ids still has to name its items somehow.
	if got := evidence.TodoCitation("", 2, "Add the tests"); got != "2) Add the tests" {
		t.Fatalf("citation = %q, want the ordinal fallback", got)
	}
}

func todoWriteMessage(ids ...string) provider.Message {
	items := make([]string, 0, len(ids))
	for _, id := range ids {
		items = append(items, `{"step_id":"`+id+`"}`)
	}
	args := `{"todos":[` + strings.Join(items, ",") + `]}`
	return provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{
		{Name: "todo_write", Arguments: args},
	}}
}

// Owed is a fact about the request being built, not a memory of an earlier one.
// A view that still shows the ids is owed nothing; the same host state against
// a view that lost them is owed the note, however many requests ago the model
// last read them.
func TestTodoIdentityTailIsOwedByTheViewNotByHistory(t *testing.T) {
	a := New(&scriptedProvider{name: "p"}, tool.NewRegistry(), NewSession(""), Options{}, event.Discard)
	a.ReplaceTodoState(planTodos())

	readable := []provider.Message{todoWriteMessage("plan_step_01", "plan_step_02", "plan_step_03")}
	if got := a.withTodoIdentityTail(readable); len(got) != 1 {
		t.Fatalf("appended %d messages to a view that already shows the ids", len(got)-1)
	}

	folded := []provider.Message{{Role: provider.RoleUser, Content: "summary of earlier work"}}
	for range 2 {
		got := a.withTodoIdentityTail(folded)
		if len(got) != 2 {
			t.Fatalf("appended %d messages, want the note every time the view cannot show the ids", len(got)-1)
		}
		for _, id := range []string{"plan_step_01", "plan_step_02", "plan_step_03"} {
			if !strings.Contains(got[1].Content, "["+id+"]") {
				t.Fatalf("note = %q, want it to carry %s", got[1].Content, id)
			}
		}
	}
}

// The case that killed the remembered-shown design: identity moves with no fold
// in between. A request that showed A may not suppress B.
func TestTodoIdentityTailFollowsTheHostAcrossARewrite(t *testing.T) {
	a := New(&scriptedProvider{name: "p"}, tool.NewRegistry(), NewSession(""), Options{}, event.Discard)
	a.ReplaceTodoState(planTodos())
	folded := []provider.Message{{Role: provider.RoleUser, Content: "summary of earlier work"}}
	if got := a.withTodoIdentityTail(folded); !strings.Contains(got[1].Content, "[plan_step_03]") {
		t.Fatalf("first request did not carry the original identity: %q", got[1].Content)
	}

	a.ReplaceTodoState([]evidence.TodoItem{
		{Content: "Wire the parser", Status: "completed", StepID: "plan_step_01"},
		{Content: "Add the tests", Status: "in_progress", StepID: "plan_step_02"},
		{Content: "Ship the rename", Status: "pending", StepID: "plan_step_09"},
	})
	got := a.withTodoIdentityTail(folded)
	if len(got) != 2 {
		t.Fatalf("appended %d messages after the list changed, want one note", len(got)-1)
	}
	if !strings.Contains(got[1].Content, "[plan_step_09]") {
		t.Fatalf("note = %q, want the identity the host holds now", got[1].Content)
	}
	if strings.Contains(got[1].Content, "[plan_step_03]") {
		t.Fatalf("note = %q, want no id the host has dropped", got[1].Content)
	}
}

func TestNoProjectionWithoutStableIDs(t *testing.T) {
	a := New(&scriptedProvider{name: "p"}, tool.NewRegistry(), NewSession(""), Options{}, event.Discard)
	a.ReplaceTodoState([]evidence.TodoItem{{Content: "Do the thing", Status: "in_progress"}})

	// Nothing to re-project: an ordinal is not an identity the host owns.
	folded := []provider.Message{{Role: provider.RoleUser, Content: "summary of earlier work"}}
	if got := a.withTodoIdentityTail(folded); len(got) != 1 {
		t.Fatalf("appended %d messages for a list with no ids", len(got)-1)
	}
}

// TestTodoWriteResultNamesTheSignableStepID is the renderer half: the model is
// asked for a step_id, so a step_id has to come back with the list it wrote.
func TestTodoWriteResultNamesTheSignableStepID(t *testing.T) {
	todoWrite, ok := tool.LookupBuiltin("todo_write")
	if !ok {
		t.Fatal("todo_write builtin not registered")
	}
	reg := tool.NewRegistry()
	reg.Add(todoWrite)

	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{toolCallChunk("c1", "todo_write", `{"todos":[
			{"content":"Wire the parser","status":"in_progress","activeForm":"Wiring the parser","step_id":"plan_step_01"},
			{"content":"Add the tests","status":"pending","activeForm":"Adding the tests","step_id":"plan_step_02"}
		]}`), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}},
	}}

	a := New(prov, reg, NewSession(""), Options{}, event.Discard)
	if err := a.Run(context.Background(), "plan the work"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	got := toolResult(a.sess.conversation, "todo_write")
	if !strings.Contains(got, "plan_step_01") {
		t.Fatalf("todo_write result = %q, want the in_progress item's step_id", got)
	}
}

// TestRealFoldLeavesTheStepIDsReadable drives actual compaction and asserts on
// the request, which is where the ids have to be readable: the round the fold
// feeds is already frozen when the next turn starts, so an id restored a round
// later is one sign-off too late. The installed projection is history and does
// not carry them — that is the point of deriving the note per request.
func TestRealFoldLeavesTheStepIDsReadable(t *testing.T) {
	mock := &loopMock{t: t}
	srv := httptest.NewServer(http.HandlerFunc(mock.handler))
	defer srv.Close()

	reg := tool.NewRegistry()
	reg.Add(fatTool{blob: strings.Repeat("file line. ", 1100)})
	a, _ := newAgent(t, srv.URL, reg, 40000, 4)
	a.ReplaceTodoState(planTodos())

	for i := range 20 {
		if err := a.Run(context.Background(), fmt.Sprintf("turn %d: keep going", i)); err != nil {
			t.Fatalf("Run %d: %v", i, err)
		}
	}

	a.sess.compactionMu.Lock()
	projected := a.sess.compactionState.Projection.Messages
	a.sess.compactionMu.Unlock()
	if len(projected) == 0 {
		t.Fatal("no fold was installed; this test asserts nothing without one")
	}
	visible := a.modelVisibleMessages()
	for _, id := range []string{"plan_step_01", "plan_step_02", "plan_step_03"} {
		if !messagesMentionID(visible, id) {
			t.Fatalf("the request after the fold cannot cite %s", id)
		}
	}
	if messagesMentionID(projected, "plan_step_02") && !messagesMentionID(a.modelVisibleHistory(), "plan_step_02") {
		t.Fatal("the frozen body carries host step state; it is history, not host state")
	}
}
