package agent

import (
	"context"
	"reasonix/internal/state/sessionstore"
	"strings"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/state/instruction"
)

// scriptedProvider replays a distinct chunk set per Stream call, so a multi-turn
// Run() sees tool calls on turn 1 and a plain final answer on turn 2.
type scriptedProvider struct {
	name     string
	turns    [][]provider.Chunk
	call     int
	requests []provider.Request
}

func (s *scriptedProvider) Name() string { return s.name }

func (s *scriptedProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	s.requests = append(s.requests, req)
	i := s.call
	if i >= len(s.turns) {
		i = len(s.turns) - 1
	}
	s.call++
	ch := make(chan provider.Chunk, len(s.turns[i]))
	for _, c := range s.turns[i] {
		ch <- c
	}
	close(ch)
	return ch, nil
}

func toolCallChunk(id, name, args string) provider.Chunk {
	return provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: id, Name: name, Arguments: args}}
}

func toolResult(s *sessionstore.Session, name string) string {
	for _, m := range s.Messages {
		if m.Role == provider.RoleTool && m.Name == name {
			return m.Content
		}
	}
	return ""
}

func lastToolResult(s *sessionstore.Session, name string) string {
	var result string
	for _, m := range s.Messages {
		if m.Role == provider.RoleTool && m.Name == name {
			result = m.Content
		}
	}
	return result
}

func toolResultByID(s *sessionstore.Session, id string) string {
	for _, m := range s.Messages {
		if m.Role == provider.RoleTool && m.ToolCallID == id {
			return m.Content
		}
	}
	return ""
}

func toolResults(s *sessionstore.Session, name string) []string {
	var results []string
	for _, m := range s.Messages {
		if m.Role == provider.RoleTool && m.Name == name {
			results = append(results, m.Content)
		}
	}
	return results
}

func sessionHasUserMessageContaining(s *sessionstore.Session, needle string) bool {
	for _, m := range s.Messages {
		if m.Role != provider.RoleUser {
			continue
		}
		if strings.Contains(m.Content, needle) {
			return true
		}
		// Fallback: provider projection (RawContent stripped, Content kept).
		projected := provider.ModelMessages([]provider.Message{m})
		if len(projected) > 0 && strings.Contains(projected[0].Content, needle) {
			return true
		}
	}
	return false
}

func TestDeliveryProfileBlocksMixedVerificationBeforeItBecomesMutation(t *testing.T) {
	reg := evidenceRegistry()
	prov := &scriptedProvider{name: "delivery", turns: [][]provider.Chunk{
		{toolCallChunk("criteria", "todo_write", `{"todos":[{"content":"Check snake","status":"in_progress"}]}`), {Type: provider.ChunkDone}},
		// cp rather than an inline interpreter: delivery refuses `python3 -c` for
		// being unauditable before any shape block sees it, which would make this
		// a test of the wrong gate.
		{toolCallChunk("mixed", "bash", `{"command":"cp snake.js /tmp/snake_check.js && node --check /tmp/snake_check.js"}`), {Type: provider.ChunkDone}},
		{toolCallChunk("safe", "bash", `{"command":"tail -n +2 snake.js | head -n 20 | node --check -"}`), {Type: provider.ChunkDone}},
		{toolCallChunk("signoff", "complete_step", `{"step":"Check snake","result":"syntax valid","evidence":[{"kind":"verification","summary":"syntax valid","command":"tail -n +2 snake.js | head -n 20 | node --check -"}]}`), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "checked"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, reg, sessionstore.NewSession(""), Options{DeliveryProfile: true}, event.Discard)
	if err := a.Run(context.Background(), "check the snake game"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got := toolResult(a.sess.conversation, "bash")
	if !strings.Contains(got, "blocked:") {
		t.Fatalf("mixed command result = %q, want pre-execution split guidance", got)
	}
	// Naming the segment is the difference between one block and three: an
	// unnamed refusal makes the run rewrite whichever part it guesses.
	if !strings.Contains(got, "cp snake.js /tmp/snake_check.js") {
		t.Fatalf("delivery block = %q, want the offending segment named", got)
	}
	if _, ok := a.task.ledger.LatestSuccessfulMutationIndex(); ok {
		t.Fatal("blocked scratch-file verification must not become a successful mutation")
	}
}

func TestDeliveryProfileExplainsMaskedVerifierExitBeforeExecution(t *testing.T) {
	reg := evidenceRegistry()
	prov := &scriptedProvider{name: "delivery", turns: [][]provider.Chunk{
		{toolCallChunk("criteria", "todo_write", `{"todos":[{"content":"Check snake","status":"in_progress"}]}`), {Type: provider.ChunkDone}},
		{toolCallChunk("masked", "bash", `{"command":"tail -n +2 snake.js | head -n 20 | node --check -; echo \"EXIT: $?\""}`), {Type: provider.ChunkDone}},
		{toolCallChunk("safe", "bash", `{"command":"tail -n +2 snake.js | head -n 20 | node --check -"}`), {Type: provider.ChunkDone}},
		{toolCallChunk("signoff", "complete_step", `{"step":"Check snake","result":"syntax valid","evidence":[{"kind":"verification","summary":"syntax valid","command":"tail -n +2 snake.js | head -n 20 | node --check -"}]}`), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "checked"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, reg, sessionstore.NewSession(""), Options{DeliveryProfile: true}, event.Discard)
	if err := a.Run(context.Background(), "check the snake game"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := toolResultByID(a.sess.conversation, "masked"); !strings.Contains(got, "masks the verifier's exit status") {
		t.Fatalf("masked command result = %q, want precise exit-status guidance", got)
	}
	if _, ok := a.task.ledger.LatestSuccessfulMutationIndex(); ok {
		t.Fatal("blocked masked verifier must not become a successful mutation")
	}
}

func TestDeliveryProfileRequiresActiveTodoForLateMutation(t *testing.T) {
	reg := evidenceRegistry()
	reg.Add(fakeTool{name: "read_file", readOnly: true})
	prov := &scriptedProvider{name: "delivery", turns: [][]provider.Chunk{
		{toolCallChunk("criteria", "todo_write", `{"todos":[{"content":"Ship main","status":"in_progress"}]}`), {Type: provider.ChunkDone}},
		{toolCallChunk("write", "write_file", `{"path":"main.go","content":"package main"}`), {Type: provider.ChunkDone}},
		{toolCallChunk("review", "read_file", `{"path":"main.go"}`), {Type: provider.ChunkDone}},
		{toolCallChunk("verify", "bash", `{"command":"go test ./..."}`), {Type: provider.ChunkDone}},
		{toolCallChunk("signoff", "complete_step", `{"step":"Ship main","result":"done","evidence":[{"kind":"verification","summary":"tests pass","command":"go test ./..."}]}`), {Type: provider.ChunkDone}},
		{toolCallChunk("late-write", "write_file", `{"path":"main.go","content":"package main // late"}`), {Type: provider.ChunkDone}},
		{toolCallChunk("append-todo", "todo_write", `{"todos":[{"content":"Ship main","status":"completed"},{"content":"Apply review fix","status":"in_progress"}]}`), {Type: provider.ChunkDone}},
		{toolCallChunk("retry-write", "write_file", `{"path":"main.go","content":"package main // reviewed"}`), {Type: provider.ChunkDone}},
		{toolCallChunk("review-2", "read_file", `{"path":"main.go"}`), {Type: provider.ChunkDone}},
		{toolCallChunk("verify-2", "bash", `{"command":"go test ./..."}`), {Type: provider.ChunkDone}},
		{toolCallChunk("signoff-2", "complete_step", `{"step":"Apply review fix","result":"done","evidence":[{"kind":"verification","summary":"tests pass","command":"go test ./..."}]}`), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "delivered"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, reg, sessionstore.NewSession(""), Options{DeliveryProfile: true,
		CheckTargetAccess: allowTestFileAccess,
	}, event.Discard)
	if err := a.Run(context.Background(), "implement main and incorporate review fixes"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := toolResultByID(a.sess.conversation, "late-write"); !strings.Contains(got, "current in_progress todo") {
		t.Fatalf("late mutation result = %q, want active-todo gate", got)
	}
	if got := toolResultByID(a.sess.conversation, "retry-write"); strings.HasPrefix(got, "blocked:") || strings.HasPrefix(got, "error:") {
		t.Fatalf("mutation after appended active todo should run, got %q", got)
	}
}

func TestDeliveryProfileAllowsEvidenceBackedReadOnlyAnalysis(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "read_file", readOnly: true})
	prov := &scriptedProvider{name: "delivery", turns: [][]provider.Chunk{
		{toolCallChunk("read", "read_file", `{"path":"main.go"}`), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "analysis"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, reg, sessionstore.NewSession(""), Options{DeliveryProfile: true}, event.Discard)
	if err := a.Run(context.Background(), "analyze main.go"); err != nil {
		t.Fatalf("read-only analysis should not require mutation/sign-off: %v", err)
	}
}

func TestFinalReadinessAllowsFinalAnswerWithoutWriter(t *testing.T) {
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, tool.NewRegistry(), sessionstore.NewSession(""), Options{
		ProjectChecks: []instruction.VerifyCheck{{Command: "go test ./...", SourcePath: "AGENTS.md", Line: 3}},
	}, event.Discard)

	if err := a.Run(context.Background(), "inspect only"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if prov.call != 1 {
		t.Fatalf("provider calls = %d, want 1", prov.call)
	}
}

func TestFinalReadinessAllowsWriterWithoutChecksOrTodos(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "write_file", readOnly: false, writesPaths: true})
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{
			toolCallChunk("c1", "write_file", `{"path":"changed.go","content":"package main"}`),
			{Type: provider.ChunkDone},
		},
		{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, reg, sessionstore.NewSession(""), Options{}, event.Discard)

	if err := a.Run(context.Background(), "simple edit"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if prov.call != 2 {
		t.Fatalf("provider calls = %d, want 2", prov.call)
	}
}

func TestTodoWriteOnlyTurnMayEndWithIncompleteTodos(t *testing.T) {
	todoWrite, ok := tool.LookupBuiltin("todo_write")
	if !ok {
		t.Fatal("todo_write builtin not registered")
	}
	reg := tool.NewRegistry()
	reg.Add(todoWrite)
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{
			toolCallChunk("c1", "todo_write", `{"todos":[{"content":"Draft plan","status":"in_progress"},{"content":"Implement","status":"pending"}]}`),
			{Type: provider.ChunkDone},
		},
		{{Type: provider.ChunkText, Text: "here is the task list"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, reg, sessionstore.NewSession(""), Options{}, event.Discard)

	if err := a.Run(context.Background(), "create a todo list only"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if prov.call != 2 {
		t.Fatalf("provider calls = %d, want 2 without readiness retry", prov.call)
	}
	if got := lastToolResult(a.sess.conversation, "todo_write"); !strings.Contains(got, "Todos updated") {
		t.Fatalf("todo_write result = %q, want successful todo update", got)
	}
}

func TestReadOnlyContextAndTodoTurnMayEndWithIncompleteTodos(t *testing.T) {
	todoWrite, ok := tool.LookupBuiltin("todo_write")
	if !ok {
		t.Fatal("todo_write builtin not registered")
	}
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "read_file", readOnly: true})
	reg.Add(todoWrite)
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{
			toolCallChunk("c1", "read_file", `{"path":"README.md"}`),
			toolCallChunk("c2", "todo_write", `{"todos":[{"content":"Draft plan","status":"in_progress"},{"content":"Implement","status":"pending"}]}`),
			{Type: provider.ChunkDone},
		},
		{{Type: provider.ChunkText, Text: "I reviewed the context and wrote the list."}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, reg, sessionstore.NewSession(""), Options{}, event.Discard)

	if err := a.Run(context.Background(), "read context and only draft a todo list"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if prov.call != 2 {
		t.Fatalf("provider calls = %d, want 2 without readiness retry", prov.call)
	}
	if got := lastToolResult(a.sess.conversation, "todo_write"); !strings.Contains(got, "Todos updated") {
		t.Fatalf("todo_write result = %q, want successful todo update", got)
	}
}

func TestEvidenceFlowRejectsStepMissingFromTodoWrite(t *testing.T) {
	todoWrite, ok := tool.LookupBuiltin("todo_write")
	if !ok {
		t.Fatal("todo_write builtin not registered")
	}
	completeStep, ok := tool.LookupBuiltin("complete_step")
	if !ok {
		t.Fatal("complete_step builtin not registered")
	}
	reg := tool.NewRegistry()
	reg.Add(todoWrite)
	reg.Add(completeStep)

	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{
			toolCallChunk("c1", "todo_write", `{"todos":[{"content":"Add parser","status":"in_progress"}]}`),
			toolCallChunk("c2", "complete_step", `{
				"step":"Ship parser",
				"result":"step is complete",
				"evidence":[{"kind":"manual","summary":"checked manually"}]
			}`),
			toolCallChunk("c3", "complete_step", `{
				"step":"Add parser",
				"result":"parser added",
				"evidence":[{"kind":"manual","summary":"checked manually"}]
			}`),
			toolCallChunk("c4", "todo_write", `{"todos":[{"content":"Add parser","status":"completed"}]}`),
			{Type: provider.ChunkDone},
		},
		{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}},
	}}

	a := New(prov, reg, sessionstore.NewSession(""), Options{}, event.Discard)
	if err := a.Run(context.Background(), "update todos then sign off the wrong step"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	got := toolResult(a.sess.conversation, "complete_step")
	if !strings.Contains(got, "matching todo_write item") {
		t.Fatalf("complete_step result = %q, want todo-backed rejection", got)
	}
}

func TestEvidenceFlowAcceptsTodoCompletionAfterCompleteStep(t *testing.T) {
	todoWrite, ok := tool.LookupBuiltin("todo_write")
	if !ok {
		t.Fatal("todo_write builtin not registered")
	}
	completeStep, ok := tool.LookupBuiltin("complete_step")
	if !ok {
		t.Fatal("complete_step builtin not registered")
	}
	reg := tool.NewRegistry()
	reg.Add(todoWrite)
	reg.Add(completeStep)

	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{
			toolCallChunk("c1", "todo_write", `{"todos":[{"content":"Add parser","status":"in_progress"}]}`),
			toolCallChunk("c2", "complete_step", `{
				"step":"Add parser",
				"result":"parser added",
				"evidence":[{"kind":"manual","summary":"checked manually"}]
			}`),
			toolCallChunk("c3", "todo_write", `{"todos":[{"content":"Add parser","status":"completed"}]}`),
			{Type: provider.ChunkDone},
		},
		{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}},
	}}

	a := New(prov, reg, sessionstore.NewSession(""), Options{}, event.Discard)
	if err := a.Run(context.Background(), "complete the todo with a sign-off first"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// The sign-off advanced the list already, so the final call restates it
	// rather than changing anything. What this test is about is that it is
	// accepted at all — the completion-transition guard has its receipt.
	got := lastToolResult(a.sess.conversation, "todo_write")
	if strings.Contains(got, "complete_step receipt") || !strings.Contains(got, "1 completed") {
		t.Fatalf("final todo_write result = %q, want the completion accepted", got)
	}
}

func TestEvidenceFlowRejectsTodoCompletionWithoutCompleteStep(t *testing.T) {
	todoWrite, ok := tool.LookupBuiltin("todo_write")
	if !ok {
		t.Fatal("todo_write builtin not registered")
	}
	completeStep, ok := tool.LookupBuiltin("complete_step")
	if !ok {
		t.Fatal("complete_step builtin not registered")
	}
	reg := tool.NewRegistry()
	reg.Add(todoWrite)
	reg.Add(completeStep)

	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{
			toolCallChunk("c1", "todo_write", `{"todos":[{"content":"Add parser","status":"in_progress"}]}`),
			toolCallChunk("c2", "todo_write", `{"todos":[{"content":"Add parser","status":"completed"}]}`),
			toolCallChunk("c3", "complete_step", `{
				"step":"Add parser",
				"result":"parser added",
				"evidence":[{"kind":"manual","summary":"checked manually"}]
			}`),
			toolCallChunk("c4", "todo_write", `{"todos":[{"content":"Add parser","status":"completed"}]}`),
			{Type: provider.ChunkDone},
		},
		{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}},
	}}

	a := New(prov, reg, sessionstore.NewSession(""), Options{}, event.Discard)
	if err := a.Run(context.Background(), "complete the todo without a sign-off"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	results := toolResults(a.sess.conversation, "todo_write")
	if len(results) < 2 {
		t.Fatalf("todo_write results = %v, want the rejected completion result", results)
	}
	got := results[1]
	if !strings.Contains(got, "complete_step") {
		t.Fatalf("todo_write result = %q, want completion rejected until complete_step", got)
	}
}

func TestEvidenceFlowFailedCompleteStepDoesNotAuthorizeTodoCompletion(t *testing.T) {
	todoWrite, ok := tool.LookupBuiltin("todo_write")
	if !ok {
		t.Fatal("todo_write builtin not registered")
	}
	completeStep, ok := tool.LookupBuiltin("complete_step")
	if !ok {
		t.Fatal("complete_step builtin not registered")
	}
	reg := tool.NewRegistry()
	reg.Add(todoWrite)
	reg.Add(completeStep)

	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{
			toolCallChunk("c1", "todo_write", `{"todos":[{"content":"Add parser","status":"in_progress"}]}`),
			toolCallChunk("c2", "complete_step", `{
				"step":"Ship parser",
				"result":"parser shipped",
				"evidence":[{"kind":"manual","summary":"checked manually"}]
			}`),
			toolCallChunk("c3", "todo_write", `{"todos":[{"content":"Add parser","status":"completed"}]}`),
			toolCallChunk("c4", "complete_step", `{
				"step":"Add parser",
				"result":"parser added",
				"evidence":[{"kind":"manual","summary":"checked manually"}]
			}`),
			toolCallChunk("c5", "todo_write", `{"todos":[{"content":"Add parser","status":"completed"}]}`),
			{Type: provider.ChunkDone},
		},
		{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}},
	}}

	a := New(prov, reg, sessionstore.NewSession(""), Options{}, event.Discard)
	if err := a.Run(context.Background(), "attempt completion after a failed sign-off"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	results := toolResults(a.sess.conversation, "todo_write")
	if len(results) < 2 {
		t.Fatalf("todo_write results = %v, want the rejected completion result", results)
	}
	got := results[1]
	if !strings.Contains(got, "complete_step") {
		t.Fatalf("todo_write result = %q, want failed complete_step not to authorize completion", got)
	}
}

func TestEvidenceFlowRejectsReplacedTodoAfterNumericCompleteStep(t *testing.T) {
	todoWrite, ok := tool.LookupBuiltin("todo_write")
	if !ok {
		t.Fatal("todo_write builtin not registered")
	}
	completeStep, ok := tool.LookupBuiltin("complete_step")
	if !ok {
		t.Fatal("complete_step builtin not registered")
	}
	reg := tool.NewRegistry()
	reg.Add(todoWrite)
	reg.Add(completeStep)

	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{
			toolCallChunk("c1", "todo_write", `{"todos":[{"content":"Add parser","status":"in_progress"}]}`),
			toolCallChunk("c2", "complete_step", `{
				"step":"1",
				"result":"parser added",
				"evidence":[{"kind":"manual","summary":"checked manually"}]
			}`),
			toolCallChunk("c3", "todo_write", `{"todos":[{"content":"Ship parser","status":"completed"}]}`),
			toolCallChunk("c4", "todo_write", `{"todos":[{"content":"Add parser","status":"completed"}]}`),
			{Type: provider.ChunkDone},
		},
		{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}},
	}}

	a := New(prov, reg, sessionstore.NewSession(""), Options{}, event.Discard)
	if err := a.Run(context.Background(), "try to reuse a numeric sign-off for another todo"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	results := toolResults(a.sess.conversation, "todo_write")
	if len(results) < 2 {
		t.Fatalf("todo_write results = %v, want the rejected replacement result", results)
	}
	got := results[1]
	if !strings.Contains(got, "Ship parser") || !strings.Contains(got, "complete_step") {
		t.Fatalf("todo_write result = %q, want replaced todo rejected", got)
	}
}

func TestEvidenceFlowRejectsReorderedTodoAndRecoversSerially(t *testing.T) {
	todoWrite, ok := tool.LookupBuiltin("todo_write")
	if !ok {
		t.Fatal("todo_write builtin not registered")
	}
	completeStep, ok := tool.LookupBuiltin("complete_step")
	if !ok {
		t.Fatal("complete_step builtin not registered")
	}
	reg := tool.NewRegistry()
	reg.Add(todoWrite)
	reg.Add(completeStep)

	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{
			toolCallChunk("c1", "todo_write", `{"todos":[
				{"content":"Add parser","status":"in_progress"},
				{"content":"Write tests","status":"pending"}
			]}`),
			toolCallChunk("c2", "complete_step", `{
				"step":"1",
				"result":"parser added",
				"evidence":[{"kind":"manual","summary":"checked manually"}]
			}`),
			toolCallChunk("c3", "todo_write", `{"todos":[
				{"content":"Write tests","status":"pending"},
				{"content":"Add parser","status":"completed"}
			]}`),
			{Type: provider.ChunkDone},
		},
		{
			toolCallChunk("c4", "complete_step", `{
				"step":"Write tests",
				"result":"tests written",
				"evidence":[{"kind":"manual","summary":"checked manually"}]
			}`),
			{Type: provider.ChunkDone},
		},
		{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}},
	}}

	a := New(prov, reg, sessionstore.NewSession(""), Options{}, event.Discard)
	if err := a.Run(context.Background(), "complete the signed todo after reordering it"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	results := toolResults(a.sess.conversation, "todo_write")
	if len(results) != 2 || !strings.Contains(results[1], "completed after unfinished") {
		t.Fatalf("reordered todo_write results = %v, want serial-order rejection", results)
	}
	for i, todo := range a.CanonicalTodoState() {
		if todo.Status != "completed" {
			t.Fatalf("canonical todo %d = %+v, want completed after serial recovery", i+1, todo)
		}
	}
}
