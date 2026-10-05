package agent

import (
	"context"
	"errors"
	"reasonix/internal/contract/hostaudit"
	"reasonix/internal/state/sessionstore"
	"reflect"
	"slices"
	"strings"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/safety/evidence"
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

type readinessAuditSink struct {
	events []hostaudit.ReadinessAudit
}

func (s *readinessAuditSink) Emit(event.Event) {}

func (s *readinessAuditSink) RecordReadinessAudit(a hostaudit.ReadinessAudit) {
	s.events = append(s.events, a)
}

// TestEvidenceFlowEndToEnd drives a full Run(): turn 1 runs bash then signs the
// step off citing that exact command; complete_step must see the host receipt
// recorded earlier in the same batch and report it host-verified.
func TestEvidenceFlowEndToEnd(t *testing.T) {
	completeStep, ok := tool.LookupBuiltin("complete_step")
	if !ok {
		t.Fatal("complete_step builtin not registered")
	}
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "bash", readOnly: false})
	reg.Add(completeStep)

	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{
			toolCallChunk("c1", "bash", `{"command":"go test ./..."}`),
			toolCallChunk("c2", "complete_step", `{
				"step":"Run the suite",
				"result":"tests pass",
				"evidence":[{"kind":"verification","summary":"go test ./... passed","command":"go test ./..."}]
			}`),
			{Type: provider.ChunkDone},
		},
		{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}},
	}}

	a := New(prov, reg, sessionstore.NewSession(""), Options{}, event.Discard)
	if err := a.Run(context.Background(), "run the suite and sign the step off"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if got := toolResult(a.sess.conversation, "complete_step"); !strings.Contains(got, "host-verified 1") {
		t.Fatalf("complete_step result = %q, want it host-verified from the bash receipt", got)
	}
}

func TestDeliveryProfileEnforcesAcceptanceReviewVerificationAndSignoff(t *testing.T) {
	reg := evidenceRegistry()
	reg.Add(fakeTool{name: "read_file", readOnly: true})
	// Keep review available so this ordinary production change exercises the
	// Medium-risk host-proof alternative instead of the minimal-registry bypass.
	reg.Add(fakeTool{name: "review", readOnly: true})

	prov := &scriptedProvider{name: "delivery", turns: [][]provider.Chunk{
		{toolCallChunk("blocked-write", "write_file", `{"path":"main.go","content":"package main"}`), {Type: provider.ChunkDone}},
		{toolCallChunk("criteria", "todo_write", `{"todos":[{"content":"Ship main","status":"in_progress","activeForm":"Shipping main"}]}`), {Type: provider.ChunkDone}},
		{toolCallChunk("write", "write_file", `{"path":"main.go","content":"package main"}`), {Type: provider.ChunkDone}},
		{toolCallChunk("review", "read_file", `{"path":"main.go"}`), {Type: provider.ChunkDone}},
		{toolCallChunk("verify", "bash", `{"command":"go test ./..."}`), {Type: provider.ChunkDone}},
		{toolCallChunk("signoff", "complete_step", `{
			"step":"Ship main",
			"result":"main is implemented and verified",
			"evidence":[
				{"kind":"diff","summary":"main implementation added","paths":["main.go"]},
				{"kind":"verification","summary":"tests pass","command":"go test ./..."}
			]
		}`), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "delivered"}, {Type: provider.ChunkDone}},
	}}

	a := New(prov, reg, sessionstore.NewSession(""), Options{DeliveryProfile: true}, event.Discard)
	if err := a.Run(context.Background(), "implement main"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := toolResult(a.sess.conversation, "write_file"); !strings.Contains(got, "delivery-first mode requires acceptance criteria") {
		t.Fatalf("first write result = %q, want delivery acceptance gate", got)
	}
	if !sessionHasUserMessageContaining(a.sess.conversation, "<execution-policy") {
		t.Fatal("execution-policy marker was not injected into the turn tail")
	}
	if got := lastToolResult(a.sess.conversation, "complete_step"); !strings.Contains(got, "signed off") {
		t.Fatalf("complete_step result = %q, want successful sign-off", got)
	}
	firstSystem := systemMessageContent(prov.requests[0])
	firstTools := prov.requests[0].Tools
	for i, req := range prov.requests[1:] {
		if got := systemMessageContent(req); got != firstSystem {
			t.Fatalf("delivery request %d changed the cache-stable system prompt", i+2)
		}
		if !reflect.DeepEqual(req.Tools, firstTools) {
			t.Fatalf("delivery request %d changed provider-visible tool schemas", i+2)
		}
	}
}

func systemMessageContent(req provider.Request) string {
	for _, msg := range req.Messages {
		if msg.Role == provider.RoleSystem {
			return msg.Content
		}
	}
	return ""
}

func TestDeliveryProfileRequiresReviewBeforeFinalAnswer(t *testing.T) {
	reg := evidenceRegistry()
	reg.Add(fakeTool{name: "read_file", readOnly: true})
	prov := &scriptedProvider{name: "delivery", turns: [][]provider.Chunk{
		{toolCallChunk("criteria", "todo_write", `{"todos":[{"content":"Ship main","status":"in_progress"}]}`), {Type: provider.ChunkDone}},
		{toolCallChunk("write", "write_file", `{"path":"main.go"}`), {Type: provider.ChunkDone}},
		{toolCallChunk("verify", "bash", `{"command":"go test ./..."}`), {Type: provider.ChunkDone}},
		{toolCallChunk("signoff", "complete_step", `{"step":"Ship main","result":"implemented","evidence":[{"kind":"verification","summary":"tests pass","command":"go test ./..."}]}`), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "done too early"}, {Type: provider.ChunkDone}},
		{toolCallChunk("review", "read_file", `{"path":"main.go"}`), {Type: provider.ChunkDone}},
		{toolCallChunk("renewed-signoff", "complete_step", `{"step":"Ship main","result":"implemented, reviewed, and verified","evidence":[{"kind":"verification","summary":"tests pass","command":"go test ./..."}]}`), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "done after review and signoff"}, {Type: provider.ChunkDone}},
	}}
	sink := &readinessAuditSink{}
	a := New(prov, reg, sessionstore.NewSession(""), Options{DeliveryProfile: true}, sink)
	ctx := deliveryGoalContext("goal-review", "implement main")
	// The first final answer fails immediately (no readiness retries); the
	// scoped follow-up adds the missing review and renews the sign-off.
	if err := a.Run(ctx, "implement main"); !readinessBlocked(err) {
		t.Fatalf("first Run err = %v, want FinalReadinessError for the missing review", err)
	}
	// The debt here is inspection of what the turn changed, not a structured
	// review report — a distinction the single review counter could not make.
	if len(sink.events) != 1 || sink.events[0].Result != hostaudit.ReadinessErrored || sink.events[0].MissingPathInspection == 0 {
		t.Fatalf("readiness audits = %+v, want one errored audit owing changed-path inspection", sink.events)
	}
	if err := a.Run(ctx, "finish the goal"); err != nil {
		t.Fatalf("follow-up Run: %v", err)
	}
	if len(sink.events) != 2 || sink.events[len(sink.events)-1].Result != hostaudit.ReadinessAllowed {
		t.Fatalf("readiness audits = %+v, want a final allowed audit", sink.events)
	}
}
func TestDeliveryProfileCommandOnlyActionRequiresCriteriaAndSignoff(t *testing.T) {
	reg := evidenceRegistry()
	prov := &scriptedProvider{name: "delivery", turns: [][]provider.Chunk{
		{toolCallChunk("blocked-test", "bash", `{"command":"go test ./..."}`), {Type: provider.ChunkDone}},
		{toolCallChunk("criteria", "todo_write", `{"todos":[{"content":"Run tests","status":"in_progress"}]}`), {Type: provider.ChunkDone}},
		{toolCallChunk("verify", "bash", `{"command":"go test ./..."}`), {Type: provider.ChunkDone}},
		{toolCallChunk("signoff", "complete_step", `{"step":"Run tests","result":"tests pass","evidence":[{"kind":"verification","summary":"tests pass","command":"go test ./..."}]}`), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "tests pass"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, reg, sessionstore.NewSession(""), Options{DeliveryProfile: true}, event.Discard)
	if err := a.Run(context.Background(), "run tests"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := toolResult(a.sess.conversation, "bash"); !strings.Contains(got, "delivery-first mode requires acceptance criteria") {
		t.Fatalf("first bash result = %q, want acceptance gate", got)
	}
	if got := lastToolResult(a.sess.conversation, "complete_step"); !strings.Contains(got, "signed off") {
		t.Fatalf("complete_step result = %q, want successful command-only sign-off", got)
	}
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

// Handing source to an interpreter is a debt, not a refusal. Running it was
// never the problem; what a later check cannot do is say what it changed, so a
// green suite leaves the debt exactly where it was.
func TestOpaqueInterpreterRunsButACheckDoesNotProveItsUnknownEffects(t *testing.T) {
	reg := evidenceRegistry()
	reg.Add(fakeTool{name: "read_file", readOnly: true})
	prov := &scriptedProvider{name: "delivery", turns: [][]provider.Chunk{
		{toolCallChunk("criteria", "todo_write", `{"todos":[{"content":"Check snake","status":"in_progress"}]}`), {Type: provider.ChunkDone}},
		{toolCallChunk("opaque", "bash", `{"command":"node -e 'require(\"fs\").writeFileSync(\"snake.html\",\"x\")'"}`), {Type: provider.ChunkDone}},
		{toolCallChunk("review", "read_file", `{"path":"snake.js"}`), {Type: provider.ChunkDone}},
		{toolCallChunk("safe", "bash", `{"command":"go test ./..."}`), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "checked"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, reg, sessionstore.NewSession(""), Options{DeliveryProfile: true}, event.Discard)
	err := a.Run(context.Background(), "check the snake game")

	if got := toolResultByID(a.sess.conversation, "opaque"); strings.Contains(got, "cannot audit inline interpreter source") {
		t.Fatalf("opaque command result = %q, want it to have run", got)
	}
	var readiness *FinalReadinessError
	if !errors.As(err, &readiness) || !slices.Contains(readiness.Missing, "mutation") {
		t.Fatalf("Run err = %v, want the mutation debt to survive the check", err)
	}
}

// The same call with nothing checking after it: Delivery may not report done
// while the debt stands, and the reason has to name it rather than the shape.
func TestDeliveryWillNotFinishOwingAnOpaqueMutation(t *testing.T) {
	reg := evidenceRegistry()
	prov := &scriptedProvider{name: "delivery", turns: [][]provider.Chunk{
		{toolCallChunk("criteria", "todo_write", `{"todos":[{"content":"Check snake","status":"in_progress"}]}`), {Type: provider.ChunkDone}},
		{toolCallChunk("opaque", "bash", `{"command":"node -e 'require(\"fs\").writeFileSync(\"snake.html\",\"x\")'"}`), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, reg, sessionstore.NewSession(""), Options{DeliveryProfile: true}, event.Discard)
	err := a.Run(context.Background(), "write the snake game")
	var readiness *FinalReadinessError
	if !errors.As(err, &readiness) {
		t.Fatalf("Run err = %v, want a readiness refusal while the debt stands", err)
	}
	if !slices.Contains(readiness.Missing, "mutation") {
		t.Fatalf("missing = %v, want the mutation debt named", readiness.Missing)
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
	a := New(prov, reg, sessionstore.NewSession(""), Options{DeliveryProfile: true}, event.Discard)
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

func TestEvidenceFlowEnforcesProjectChecksAfterWrite(t *testing.T) {
	completeStep, ok := tool.LookupBuiltin("complete_step")
	if !ok {
		t.Fatal("complete_step builtin not registered")
	}
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "write_file", readOnly: false, writesPaths: true})
	reg.Add(fakeTool{name: "bash", readOnly: false})
	reg.Add(completeStep)

	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{
			toolCallChunk("c1", "write_file", `{"path":"changed.go","content":"package main"}`),
			toolCallChunk("c2", "bash", `{"command":"go test ./..."}`),
			toolCallChunk("c3", "complete_step", `{
				"step":"Edit code",
				"result":"changed.go updated",
				"evidence":[{"kind":"diff","summary":"updated code","paths":["changed.go"]}]
			}`),
			{Type: provider.ChunkDone},
		},
		{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}},
	}}

	a := New(prov, reg, sessionstore.NewSession(""), Options{
		ProjectChecks: []instruction.VerifyCheck{{Command: "go test ./...", SourcePath: "AGENTS.md", Line: 3}},
	}, event.Discard)
	if err := a.Run(context.Background(), "edit and verify"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	got := toolResult(a.sess.conversation, "complete_step")
	if !strings.Contains(got, "project checks 1") {
		t.Fatalf("complete_step result = %q, want project check verified from same batch", got)
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

func TestFinalReadinessAuditSkipsWhenGateDoesNotApply(t *testing.T) {
	t.Run("no writer", func(t *testing.T) {
		prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
			{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}},
		}}
		sink := &readinessAuditSink{}
		a := New(prov, tool.NewRegistry(), sessionstore.NewSession(""), Options{
			ProjectChecks: []instruction.VerifyCheck{{Command: "go test ./...", SourcePath: "AGENTS.md", Line: 3}},
		}, sink)

		if err := a.Run(context.Background(), "inspect only"); err != nil {
			t.Fatalf("Run: %v", err)
		}
		if len(sink.events) != 0 {
			t.Fatalf("readiness audit events = %d, want 0: %+v", len(sink.events), sink.events)
		}
	})

	t.Run("writer without checks or todo", func(t *testing.T) {
		reg := tool.NewRegistry()
		reg.Add(fakeTool{name: "write_file", readOnly: false, writesPaths: true})
		prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
			{
				toolCallChunk("c1", "write_file", `{"path":"changed.go","content":"package main"}`),
				{Type: provider.ChunkDone},
			},
			{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}},
		}}
		sink := &readinessAuditSink{}
		a := New(prov, reg, sessionstore.NewSession(""), Options{}, sink)

		if err := a.Run(context.Background(), "simple edit"); err != nil {
			t.Fatalf("Run: %v", err)
		}
		if len(sink.events) != 0 {
			t.Fatalf("readiness audit events = %d, want 0: %+v", len(sink.events), sink.events)
		}
	})
}

func TestFinalReadinessBlocksUntilProjectCheckRunsAfterWriter(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "write_file", readOnly: false, writesPaths: true})
	reg.Add(fakeTool{name: "bash", readOnly: false})
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{
			toolCallChunk("c1", "write_file", `{"path":"changed.go","content":"package main"}`),
			{Type: provider.ChunkDone},
		},
		{{Type: provider.ChunkText, Text: "premature"}, {Type: provider.ChunkDone}},
		{
			toolCallChunk("c2", "bash", `{"command":"go test ./..."}`),
			{Type: provider.ChunkDone},
		},
		{{Type: provider.ChunkText, Text: "verified done"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, reg, sessionstore.NewSession(""), Options{
		ProjectChecks: []instruction.VerifyCheck{{Command: "go test ./...", SourcePath: "AGENTS.md", Line: 3}},
	}, event.Discard)
	ctx := deliveryGoalContext("goal-checks", "edit and finish")

	// The premature final answer fails immediately; the scoped follow-up runs
	// the required project check after the preserved write and passes.
	if err := a.Run(ctx, "edit and finish"); !readinessBlocked(err) {
		t.Fatalf("premature Run err = %v, want FinalReadinessError", err)
	}
	if prov.call != 2 {
		t.Fatalf("provider calls = %d, want writer turn + one blocked final answer (no retries)", prov.call)
	}
	if err := a.Run(ctx, "finish"); err != nil {
		t.Fatalf("verified Run: %v", err)
	}
	if got := lastToolResult(a.sess.conversation, "bash"); !strings.Contains(got, "bash done") {
		t.Fatalf("bash tool result = %q, want command run after the block", got)
	}
}

func TestFinalReadinessAuditRecordsBlockAndRecovery(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "write_file", readOnly: false, writesPaths: true})
	reg.Add(fakeTool{name: "bash", readOnly: false})
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{
			toolCallChunk("c1", "write_file", `{"path":"changed.go","content":"package main"}`),
			{Type: provider.ChunkDone},
		},
		{{Type: provider.ChunkText, Text: "premature"}, {Type: provider.ChunkDone}},
		{
			toolCallChunk("c2", "bash", `{"command":"go test ./..."}`),
			{Type: provider.ChunkDone},
		},
		{{Type: provider.ChunkText, Text: "verified done"}, {Type: provider.ChunkDone}},
	}}
	sink := &readinessAuditSink{}
	a := New(prov, reg, sessionstore.NewSession(""), Options{
		ProjectChecks: []instruction.VerifyCheck{{Command: "go test ./...", SourcePath: "AGENTS.md", Line: 3}},
	}, sink)
	ctx := deliveryGoalContext("goal-audit", "edit and finish")

	if err := a.Run(ctx, "edit and finish"); !readinessBlocked(err) {
		t.Fatalf("premature Run err = %v, want FinalReadinessError", err)
	}
	if len(sink.events) != 1 {
		t.Fatalf("readiness audit events = %d, want 1: %+v", len(sink.events), sink.events)
	}
	blocked := sink.events[0]
	if blocked.Result != hostaudit.ReadinessErrored || blocked.MissingProjectChecks != 1 {
		t.Fatalf("blocked audit = %+v, want missing project check command", blocked)
	}
	if err := a.Run(ctx, "finish"); err != nil {
		t.Fatalf("verified Run: %v", err)
	}
	recovered := sink.events[len(sink.events)-1]
	if recovered.Result != hostaudit.ReadinessAllowed || !recovered.Recovered {
		t.Fatalf("recovery audit = %+v, want allowed recovered", recovered)
	}
}

func TestFinalReadinessRejectsProjectCheckBeforeWriter(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "bash", readOnly: false})
	reg.Add(fakeTool{name: "write_file", readOnly: false, writesPaths: true})
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{
			toolCallChunk("c1", "bash", `{"command":"go test ./..."}`),
			toolCallChunk("c2", "write_file", `{"path":"changed.go","content":"package main"}`),
			{Type: provider.ChunkDone},
		},
		{{Type: provider.ChunkText, Text: "premature"}, {Type: provider.ChunkDone}},
		{
			toolCallChunk("c3", "bash", `{"command":"go test ./..."}`),
			{Type: provider.ChunkDone},
		},
		{{Type: provider.ChunkText, Text: "verified done"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, reg, sessionstore.NewSession(""), Options{
		ProjectChecks: []instruction.VerifyCheck{{Command: "go test ./...", SourcePath: "AGENTS.md", Line: 3}},
	}, event.Discard)
	ctx := deliveryGoalContext("goal-before-writer", "verify before edit, then finish")

	// The pre-writer check does not satisfy the after-writer requirement: the
	// final answer fails, and the scoped follow-up reruns the check.
	if err := a.Run(ctx, "verify before edit, then finish"); !readinessBlocked(err) {
		t.Fatalf("premature Run err = %v, want FinalReadinessError", err)
	}
	if err := a.Run(ctx, "finish"); err != nil {
		t.Fatalf("verified Run: %v", err)
	}
	if prov.call != 4 {
		t.Fatalf("provider calls = %d, want pre-write check, writer, blocked final, post-write check", prov.call)
	}
}

func TestFinalReadinessRequiresCompleteStepAfterWriterWhenTodoSeen(t *testing.T) {
	todoWrite, ok := tool.LookupBuiltin("todo_write")
	if !ok {
		t.Fatal("todo_write builtin not registered")
	}
	completeStep, ok := tool.LookupBuiltin("complete_step")
	if !ok {
		t.Fatal("complete_step builtin not registered")
	}
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "write_file", readOnly: false, writesPaths: true})
	reg.Add(todoWrite)
	reg.Add(completeStep)
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{
			toolCallChunk("c1", "write_file", `{"path":"changed.go","content":"package main"}`),
			toolCallChunk("c2", "todo_write", `{"todos":[{"content":"Edit code","status":"in_progress"}]}`),
			{Type: provider.ChunkDone},
		},
		{{Type: provider.ChunkText, Text: "premature"}, {Type: provider.ChunkDone}},
		{
			toolCallChunk("c3", "complete_step", `{
				"step":"Edit code",
				"result":"changed.go updated",
				"evidence":[{"kind":"diff","summary":"updated code","paths":["changed.go"]}]
			}`),
			toolCallChunk("c4", "todo_write", `{"todos":[{"content":"Edit code","status":"completed"}]}`),
			{Type: provider.ChunkDone},
		},
		{{Type: provider.ChunkText, Text: "signed off done"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, reg, sessionstore.NewSession(""), Options{}, event.Discard)
	ctx := deliveryGoalContext("goal-signoff", "edit with todo and finish")

	// The premature final answer fails immediately; the scoped follow-up signs
	// the step off with complete_step and passes.
	if err := a.Run(ctx, "edit with todo and finish"); !readinessBlocked(err) {
		t.Fatalf("premature Run err = %v, want FinalReadinessError", err)
	}
	if err := a.Run(ctx, "finish"); err != nil {
		t.Fatalf("signed-off Run: %v", err)
	}
	if got := lastToolResult(a.sess.conversation, "complete_step"); !strings.Contains(got, "signed off") {
		t.Fatalf("complete_step result = %q, want successful sign-off", got)
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

func TestFinalReadinessAuditRecordsTerminalError(t *testing.T) {
	todoWrite, ok := tool.LookupBuiltin("todo_write")
	if !ok {
		t.Fatal("todo_write builtin not registered")
	}
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "write_file", readOnly: false, writesPaths: true})
	reg.Add(todoWrite)
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{
			toolCallChunk("c1", "write_file", `{"path":"changed.go","content":"package main"}`),
			toolCallChunk("c2", "todo_write", `{"todos":[{"content":"Edit code","status":"in_progress"}]}`),
			{Type: provider.ChunkDone},
		},
		{{Type: provider.ChunkText, Text: "premature 1"}, {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "premature 2"}, {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "premature 3"}, {Type: provider.ChunkDone}},
	}}
	sink := &readinessAuditSink{}
	a := New(prov, reg, sessionstore.NewSession(""), Options{}, sink)

	err := a.Run(context.Background(), "edit with todo and never sign off")
	if err == nil {
		t.Fatal("expected the first readiness block to stop the run")
	}
	if len(sink.events) != 1 {
		t.Fatalf("readiness audit events = %d, want 1 (no retries): %+v", len(sink.events), sink.events)
	}
	last := sink.events[len(sink.events)-1]
	if last.Result != hostaudit.ReadinessErrored || last.IncompleteTodos == 0 {
		t.Fatalf("terminal audit = %+v, want errored with incomplete todos", last)
	}
}

// TestEvidenceFlowRejectsUncitedCommand proves the loop rejects a sign-off whose
// cited command was never run: bash ran "go test", complete_step cites "go vet".
func TestEvidenceFlowRejectsUncitedCommand(t *testing.T) {
	completeStep, ok := tool.LookupBuiltin("complete_step")
	if !ok {
		t.Fatal("complete_step builtin not registered")
	}
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "bash", readOnly: false})
	reg.Add(completeStep)

	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{
			toolCallChunk("c1", "bash", `{"command":"go test ./..."}`),
			toolCallChunk("c2", "complete_step", `{
				"step":"Vet the tree",
				"result":"vet is clean",
				"evidence":[{"kind":"verification","summary":"go vet passed","command":"go vet ./..."}]
			}`),
			{Type: provider.ChunkDone},
		},
		{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}},
	}}

	a := New(prov, reg, sessionstore.NewSession(""), Options{}, event.Discard)
	if err := a.Run(context.Background(), "vet the tree and sign off"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	got := toolResult(a.sess.conversation, "complete_step")
	if !strings.Contains(got, "has no matching successful receipt") {
		t.Fatalf("complete_step result = %q, want the uncited command rejected", got)
	}
	if strings.Contains(got, "host-verified") {
		t.Fatalf("uncited command should not verify, got %q", got)
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

func TestEvidenceFlowRecoversTodoCompletionAfterFailedCompleteStepWithProgress(t *testing.T) {
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
	reg.Add(fakeTool{name: "bash", readOnly: false})
	reg.Add(completeStep)

	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{
			toolCallChunk("c1", "todo_write", `{"todos":[{"content":"Run project script","status":"in_progress"}]}`),
			toolCallChunk("c2", "bash", `{"command":"python \"script.py\""}`),
			toolCallChunk("c3", "complete_step", `{
				"step":"Run project script",
				"result":"script ran",
				"evidence":[{"kind":"verification","summary":"script completed","command":"python other.py"}]
			}`),
			toolCallChunk("c4", "todo_write", `{"todos":[{"content":"Run project script","status":"completed"}]}`),
			{Type: provider.ChunkDone},
		},
		{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}},
	}}

	a := New(prov, reg, sessionstore.NewSession(""), Options{}, event.Discard)
	if err := a.Run(context.Background(), "recover after a failed complete_step"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	stepResult := lastToolResult(a.sess.conversation, "complete_step")
	if !strings.Contains(stepResult, "no matching successful receipt") {
		t.Fatalf("complete_step result = %q, want the sign-off attempt to fail first", stepResult)
	}
	if !strings.Contains(stepResult, `python \"script.py\"`) {
		t.Fatalf("complete_step result = %q, want the self-correction hint to include the real command", stepResult)
	}
	if strings.Contains(stepResult, "todo_write") {
		t.Fatalf("complete_step result = %q, want command hints without todo tool noise", stepResult)
	}
	if got := lastToolResult(a.sess.conversation, "todo_write"); !strings.Contains(got, "1 completed") {
		t.Fatalf("todo_write result = %q, want completion recovery accepted", got)
	}
}

func TestEvidenceFlowRecoversAfterBatchTodoCompletionRejection(t *testing.T) {
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
				{"content":"Port entity imports","status":"in_progress"},
				{"content":"Run build and tests","status":"pending"}
			]}`),
			toolCallChunk("c2", "todo_write", `{"todos":[
				{"content":"Port entity imports","status":"completed"},
				{"content":"Run build and tests","status":"completed"}
			]}`),
			{Type: provider.ChunkDone},
		},
		{
			toolCallChunk("c3", "complete_step", `{
				"step":"Port entity imports",
				"result":"entity imports ported",
				"evidence":[{"kind":"manual","summary":"checked manually"}]
			}`),
			toolCallChunk("c4", "complete_step", `{
				"step":"Run build and tests",
				"result":"build and tests ran",
				"evidence":[{"kind":"manual","summary":"checked manually"}]
			}`),
			{Type: provider.ChunkDone},
		},
		{
			toolCallChunk("c5", "complete_step", `{
				"step":"Run build and tests",
				"result":"build and tests ran",
				"evidence":[{"kind":"manual","summary":"checked manually"}]
			}`),
			{Type: provider.ChunkDone},
		},
		{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}},
	}}

	a := New(prov, reg, sessionstore.NewSession(""), Options{}, event.Discard)
	if err := a.Run(context.Background(), "recover from a rejected batch todo update"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	stepResults := toolResults(a.sess.conversation, "complete_step")
	if len(stepResults) < 3 {
		t.Fatalf("complete_step results = %v, want blocked batch sign-off and a retry", stepResults)
	}
	if got := stepResults[1]; !strings.Contains(got, "only one successful complete_step") {
		t.Fatalf("second batched complete_step result = %q, want serial-signoff block", got)
	}
	if got := stepResults[2]; !strings.Contains(got, "signed off") {
		t.Fatalf("next-round complete_step result = %q, want successful sign-off", got)
	}
	for i, todo := range a.CanonicalTodoState() {
		if todo.Status != "completed" {
			t.Fatalf("canonical todo %d = %+v, want completed", i+1, todo)
		}
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

// The debt has to reach the model while it can still act on it. The host owns
// the state either way — this only makes sure the model is not deciding its
// next action against a ledger it has not been shown.
func TestObligationDeltaReachesTheModelWithTheResultThatCausedIt(t *testing.T) {
	reg := evidenceRegistry()
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{toolCallChunk("criteria", "todo_write", `{"todos":[{"content":"Ship","status":"in_progress"}]}`), {Type: provider.ChunkDone}},
		{toolCallChunk("opaque", "bash", `{"command":"node -e 'require(\"fs\").writeFileSync(\"a.txt\",\"x\")'"}`), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "ok"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, reg, sessionstore.NewSession(""), Options{}, event.Discard)
	// How the turn ends is another gate's business; what matters here is what
	// the model was holding when it chose its next action.
	_ = a.Run(context.Background(), "write it")
	got := toolResultByID(a.sess.conversation, "opaque")
	if !strings.Contains(got, "host obligations changed") {
		t.Fatalf("result = %q, want the debt reported with the call that made it", got)
	}
	if !strings.Contains(got, string(evidence.ObligationUnprovenMutation)) {
		t.Fatalf("result = %q, want the unproven mutation named", got)
	}
	// The turn before it changed nothing, so nothing was owed to report.
	if plain := toolResultByID(a.sess.conversation, "criteria"); strings.Contains(plain, "host obligations changed") {
		t.Fatalf("todo_write result = %q, want no debt reported for a call that owed nothing", plain)
	}
}
