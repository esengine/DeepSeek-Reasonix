package agent

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/evidence"
	"reasonix/internal/instruction"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
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

func toolResult(s *Session, name string) string {
	for _, m := range s.Messages {
		if m.Role == provider.RoleTool && m.Name == name {
			return m.Content
		}
	}
	return ""
}

func lastToolResult(s *Session, name string) string {
	var result string
	for _, m := range s.Messages {
		if m.Role == provider.RoleTool && m.Name == name {
			result = m.Content
		}
	}
	return result
}

func toolResultByID(s *Session, id string) string {
	for _, m := range s.Messages {
		if m.Role == provider.RoleTool && m.ToolCallID == id {
			return m.Content
		}
	}
	return ""
}

func toolResults(s *Session, name string) []string {
	var results []string
	for _, m := range s.Messages {
		if m.Role == provider.RoleTool && m.Name == name {
			results = append(results, m.Content)
		}
	}
	return results
}

func sessionHasUserMessageContaining(s *Session, needle string) bool {
	for _, m := range s.Messages {
		if m.Role == provider.RoleUser && strings.Contains(m.Content, needle) {
			return true
		}
	}
	return false
}

type readinessAuditSink struct {
	events []evidence.ReadinessAudit
}

func (s *readinessAuditSink) Emit(event.Event) {}

func (s *readinessAuditSink) RecordReadinessAudit(a evidence.ReadinessAudit) {
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

	a := New(prov, reg, NewSession(""), Options{}, event.Discard)
	if err := a.Run(context.Background(), "run the suite and sign the step off"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if got := toolResult(a.session, "complete_step"); !strings.Contains(got, "host-verified 1") {
		t.Fatalf("complete_step result = %q, want it host-verified from the bash receipt", got)
	}
}

func TestDeliveryProfileEnforcesAcceptanceReviewVerificationAndSignoff(t *testing.T) {
	reg := evidenceRegistry()
	reg.Add(fakeTool{name: "read_file", readOnly: true})

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

	a := New(prov, reg, NewSession(""), Options{DeliveryProfile: true}, event.Discard)
	if err := a.Run(context.Background(), "implement main"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := toolResult(a.session, "write_file"); !strings.Contains(got, "delivery-first mode requires acceptance criteria") {
		t.Fatalf("first write result = %q, want delivery acceptance gate", got)
	}
	if !sessionHasUserMessageContaining(a.session, "<delivery-runtime>") {
		t.Fatal("delivery runtime marker was not injected into the turn tail")
	}
	if got := lastToolResult(a.session, "complete_step"); !strings.Contains(got, "signed off") {
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

func TestDeliveryProfileRetriesFinalAnswerUntilReviewExists(t *testing.T) {
	reg := evidenceRegistry()
	reg.Add(fakeTool{name: "read_file", readOnly: true})
	prov := &scriptedProvider{name: "delivery", turns: [][]provider.Chunk{
		{toolCallChunk("criteria", "todo_write", `{"todos":[{"content":"Ship main","status":"in_progress"}]}`), {Type: provider.ChunkDone}},
		{toolCallChunk("write", "write_file", `{"path":"main.go"}`), {Type: provider.ChunkDone}},
		{toolCallChunk("verify", "bash", `{"command":"go test ./..."}`), {Type: provider.ChunkDone}},
		{toolCallChunk("signoff", "complete_step", `{"step":"Ship main","result":"implemented","evidence":[{"kind":"verification","summary":"tests pass","command":"go test ./..."}]}`), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "done too early"}, {Type: provider.ChunkDone}},
		{toolCallChunk("review", "read_file", `{"path":"main.go"}`), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "done before renewed signoff"}, {Type: provider.ChunkDone}},
		{toolCallChunk("renewed-signoff", "complete_step", `{"step":"Ship main","result":"implemented, reviewed, and verified","evidence":[{"kind":"verification","summary":"tests pass","command":"go test ./..."}]}`), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "done after review and signoff"}, {Type: provider.ChunkDone}},
	}}
	sink := &readinessAuditSink{}
	a := New(prov, reg, NewSession(""), Options{DeliveryProfile: true}, sink)
	if err := a.Run(context.Background(), "implement main"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if prov.call != 9 {
		t.Fatalf("provider calls = %d, want review followed by renewed sign-off", prov.call)
	}
	if !sessionHasUserMessageContaining(a.session, "inspect the changed result") {
		t.Fatal("missing host retry explaining the delivery review requirement")
	}
	if len(sink.events) < 3 || sink.events[0].MissingReview != 1 || sink.events[1].MissingVerification != 1 || sink.events[len(sink.events)-1].Result != evidence.ReadinessAllowed {
		t.Fatalf("readiness audits = %+v, want review block, renewed-signoff block, then allowed", sink.events)
	}
}

func TestDeliveryProfileRejectsTextOnlyImplementationClaim(t *testing.T) {
	reg := evidenceRegistry()
	reg.Add(fakeTool{name: "read_file", readOnly: true})
	prov := &scriptedProvider{name: "delivery", turns: [][]provider.Chunk{
		{{Type: provider.ChunkText, Text: "implemented"}, {Type: provider.ChunkDone}},
		{toolCallChunk("criteria", "todo_write", `{"todos":[{"content":"Implement main","status":"in_progress"}]}`), {Type: provider.ChunkDone}},
		{toolCallChunk("write", "write_file", `{"path":"main.go"}`), {Type: provider.ChunkDone}},
		{toolCallChunk("review", "read_file", `{"path":"main.go"}`), {Type: provider.ChunkDone}},
		{toolCallChunk("verify", "bash", `{"command":"go test ./..."}`), {Type: provider.ChunkDone}},
		{toolCallChunk("signoff", "complete_step", `{"step":"Implement main","result":"implemented","evidence":[{"kind":"verification","summary":"tests pass","command":"go test ./..."}]}`), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "implemented with evidence"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, reg, NewSession(""), Options{DeliveryProfile: true}, event.Discard)
	if err := a.Run(context.Background(), "implement main"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !sessionHasUserMessageContaining(a.session, "no successful mutation was observed") {
		t.Fatal("text-only implementation claim was not rejected by the delivery gate")
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
	a := New(prov, reg, NewSession(""), Options{DeliveryProfile: true}, event.Discard)
	if err := a.Run(context.Background(), "run tests"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := toolResult(a.session, "bash"); !strings.Contains(got, "delivery-first mode requires acceptance criteria") {
		t.Fatalf("first bash result = %q, want acceptance gate", got)
	}
	if got := lastToolResult(a.session, "complete_step"); !strings.Contains(got, "signed off") {
		t.Fatalf("complete_step result = %q, want successful command-only sign-off", got)
	}
}

func TestDeliveryProfileBlocksMixedVerificationBeforeItBecomesMutation(t *testing.T) {
	reg := evidenceRegistry()
	prov := &scriptedProvider{name: "delivery", turns: [][]provider.Chunk{
		{toolCallChunk("criteria", "todo_write", `{"todos":[{"content":"Check snake","status":"in_progress"}]}`), {Type: provider.ChunkDone}},
		{toolCallChunk("mixed", "bash", `{"command":"python3 -c 'open(\"/tmp/snake_check.js\",\"w\").write(\"x\")' && node --check /tmp/snake_check.js"}`), {Type: provider.ChunkDone}},
		{toolCallChunk("safe", "bash", `{"command":"tail -n +2 snake.js | head -n 20 | node --check -"}`), {Type: provider.ChunkDone}},
		{toolCallChunk("signoff", "complete_step", `{"step":"Check snake","result":"syntax valid","evidence":[{"kind":"verification","summary":"syntax valid","command":"tail -n +2 snake.js | head -n 20 | node --check -"}]}`), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "checked"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, reg, NewSession(""), Options{DeliveryProfile: true}, event.Discard)
	if err := a.Run(context.Background(), "check the snake game"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := toolResult(a.session, "bash"); !strings.Contains(got, "mixes a verification check") {
		t.Fatalf("mixed command result = %q, want pre-execution split guidance", got)
	}
	if _, ok := a.evidence.LatestSuccessfulMutationIndex(); ok {
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
	a := New(prov, reg, NewSession(""), Options{DeliveryProfile: true}, event.Discard)
	if err := a.Run(context.Background(), "check the snake game"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := toolResultByID(a.session, "masked"); !strings.Contains(got, "masks the verifier's exit status") {
		t.Fatalf("masked command result = %q, want precise exit-status guidance", got)
	}
	if _, ok := a.evidence.LatestSuccessfulMutationIndex(); ok {
		t.Fatal("blocked masked verifier must not become a successful mutation")
	}
}

func TestDeliveryProfileBlocksOpaqueInlineInterpreterBeforeItBecomesMutation(t *testing.T) {
	reg := evidenceRegistry()
	prov := &scriptedProvider{name: "delivery", turns: [][]provider.Chunk{
		{toolCallChunk("criteria", "todo_write", `{"todos":[{"content":"Check snake","status":"in_progress"}]}`), {Type: provider.ChunkDone}},
		{toolCallChunk("opaque", "bash", `{"command":"node -e 'require(\"fs\").readFileSync(\"snake.html\")'"}`), {Type: provider.ChunkDone}},
		{toolCallChunk("safe", "bash", `{"command":"tail -n +2 snake.js | head -n 20 | node --check -"}`), {Type: provider.ChunkDone}},
		{toolCallChunk("signoff", "complete_step", `{"step":"Check snake","result":"syntax valid","evidence":[{"kind":"verification","summary":"syntax valid","command":"tail -n +2 snake.js | head -n 20 | node --check -"}]}`), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "checked"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, reg, NewSession(""), Options{DeliveryProfile: true}, event.Discard)
	if err := a.Run(context.Background(), "check the snake game"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := toolResultByID(a.session, "opaque"); !strings.Contains(got, "cannot audit inline interpreter source") {
		t.Fatalf("opaque command result = %q, want pre-execution audit guidance", got)
	}
	if _, ok := a.evidence.LatestSuccessfulMutationIndex(); ok {
		t.Fatal("blocked inline interpreter must not become a successful mutation")
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
	a := New(prov, reg, NewSession(""), Options{DeliveryProfile: true}, event.Discard)
	if err := a.Run(context.Background(), "implement main and incorporate review fixes"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := toolResultByID(a.session, "late-write"); !strings.Contains(got, "current in_progress todo") {
		t.Fatalf("late mutation result = %q, want active-todo gate", got)
	}
	if got := toolResultByID(a.session, "retry-write"); strings.HasPrefix(got, "blocked:") || strings.HasPrefix(got, "error:") {
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
	a := New(prov, reg, NewSession(""), Options{DeliveryProfile: true}, event.Discard)
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
	reg.Add(fakeTool{name: "write_file", readOnly: false})
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

	a := New(prov, reg, NewSession(""), Options{
		ProjectChecks: []instruction.VerifyCheck{{Command: "go test ./...", SourcePath: "AGENTS.md", Line: 3}},
	}, event.Discard)
	if err := a.Run(context.Background(), "edit and verify"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	got := toolResult(a.session, "complete_step")
	if !strings.Contains(got, "project checks 1") {
		t.Fatalf("complete_step result = %q, want project check verified from same batch", got)
	}
}

func TestFinalReadinessAllowsFinalAnswerWithoutWriter(t *testing.T) {
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, tool.NewRegistry(), NewSession(""), Options{
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
	reg.Add(fakeTool{name: "write_file", readOnly: false})
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{
			toolCallChunk("c1", "write_file", `{"path":"changed.go","content":"package main"}`),
			{Type: provider.ChunkDone},
		},
		{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, reg, NewSession(""), Options{}, event.Discard)

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
		a := New(prov, tool.NewRegistry(), NewSession(""), Options{
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
		reg.Add(fakeTool{name: "write_file", readOnly: false})
		prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
			{
				toolCallChunk("c1", "write_file", `{"path":"changed.go","content":"package main"}`),
				{Type: provider.ChunkDone},
			},
			{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}},
		}}
		sink := &readinessAuditSink{}
		a := New(prov, reg, NewSession(""), Options{}, sink)

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
	reg.Add(fakeTool{name: "write_file", readOnly: false})
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
	a := New(prov, reg, NewSession(""), Options{
		ProjectChecks: []instruction.VerifyCheck{{Command: "go test ./...", SourcePath: "AGENTS.md", Line: 3}},
	}, event.Discard)

	if err := a.Run(context.Background(), "edit and finish"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if prov.call != 4 {
		t.Fatalf("provider calls = %d, want final answer to be retried after readiness block", prov.call)
	}
	if !sessionHasUserMessageContaining(a.session, "final-answer readiness") {
		t.Fatal("missing synthetic readiness retry message")
	}
	if got := lastToolResult(a.session, "bash"); !strings.Contains(got, "bash done") {
		t.Fatalf("bash tool result = %q, want command rerun after block", got)
	}
}

func TestFinalReadinessAuditRecordsBlockAndRecovery(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "write_file", readOnly: false})
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
	a := New(prov, reg, NewSession(""), Options{
		ProjectChecks: []instruction.VerifyCheck{{Command: "go test ./...", SourcePath: "AGENTS.md", Line: 3}},
	}, sink)

	if err := a.Run(context.Background(), "edit and finish"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(sink.events) != 2 {
		t.Fatalf("readiness audit events = %d, want 2: %+v", len(sink.events), sink.events)
	}
	blocked := sink.events[0]
	if blocked.Result != evidence.ReadinessBlocked || blocked.MissingProjectChecks != 1 || blocked.CommandMismatchMissing != 1 {
		t.Fatalf("blocked audit = %+v, want missing project check command", blocked)
	}
	recovered := sink.events[1]
	if recovered.Result != evidence.ReadinessAllowed || !recovered.Recovered {
		t.Fatalf("recovery audit = %+v, want allowed recovered", recovered)
	}
}

func TestFinalReadinessRejectsProjectCheckBeforeWriter(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "bash", readOnly: false})
	reg.Add(fakeTool{name: "write_file", readOnly: false})
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
	a := New(prov, reg, NewSession(""), Options{
		ProjectChecks: []instruction.VerifyCheck{{Command: "go test ./...", SourcePath: "AGENTS.md", Line: 3}},
	}, event.Discard)

	if err := a.Run(context.Background(), "verify before edit, then finish"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if prov.call != 4 {
		t.Fatalf("provider calls = %d, want pre-write check rejected and retried", prov.call)
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
	reg.Add(fakeTool{name: "write_file", readOnly: false})
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
	a := New(prov, reg, NewSession(""), Options{}, event.Discard)

	if err := a.Run(context.Background(), "edit with todo and finish"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if prov.call != 4 {
		t.Fatalf("provider calls = %d, want final answer to wait for complete_step", prov.call)
	}
	if got := lastToolResult(a.session, "complete_step"); !strings.Contains(got, "signed off") {
		t.Fatalf("complete_step result = %q, want successful sign-off", got)
	}
}

func TestFinalReadinessStopsAfterRepeatedBlocks(t *testing.T) {
	todoWrite, ok := tool.LookupBuiltin("todo_write")
	if !ok {
		t.Fatal("todo_write builtin not registered")
	}
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "write_file", readOnly: false})
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
	a := New(prov, reg, NewSession(""), Options{}, event.Discard)

	err := a.Run(context.Background(), "edit with todo and never sign off")
	if err == nil {
		t.Fatal("expected repeated readiness blocks to stop the run")
	}
	if !strings.Contains(err.Error(), "final-answer readiness") {
		t.Fatalf("error = %v, want final-answer readiness", err)
	}
	if prov.call != 4 {
		t.Fatalf("provider calls = %d, want three blocked final answers after writer turn", prov.call)
	}
}

func TestFinalReadinessPermissionLoopGuardAllowsBlockedFinal(t *testing.T) {
	todoWrite, ok := tool.LookupBuiltin("todo_write")
	if !ok {
		t.Fatal("todo_write builtin not registered")
	}
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "write_file", readOnly: false})
	reg.Add(fakeTool{name: "bash", readOnly: false})
	reg.Add(todoWrite)
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{
			toolCallChunk("w1", "write_file", `{"path":"changed.go","content":"package main"}`),
			toolCallChunk("t1", "todo_write", `{"todos":[{"content":"Edit code","status":"in_progress"}]}`),
			{Type: provider.ChunkDone},
		},
		{{Type: provider.ChunkText, Text: "premature final"}, {Type: provider.ChunkDone}},
		{toolCallChunk("b1", "bash", `{"command":"go test ./..."}`), {Type: provider.ChunkDone}},
		{toolCallChunk("b2", "bash", `{"command":"git status --short"}`), {Type: provider.ChunkDone}},
		{toolCallChunk("b3", "bash", `{"command":"ls -la"}`), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "blocked by permission"}, {Type: provider.ChunkDone}},
	}}
	sink, notices := noticeRecorder()
	a := New(prov, reg, NewSession(""), Options{
		Gate: &stubGate{deny: map[string]bool{"bash": true}},
	}, sink)

	if err := a.Run(context.Background(), "edit with todo, then hit bash permission blocks"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if prov.call != 6 {
		t.Fatalf("provider calls = %d, want readiness retry, three blocked bash calls, then final", prov.call)
	}
	if !sessionHasUserMessageContaining(a.session, "final-answer readiness") {
		t.Fatal("missing synthetic readiness retry message")
	}
	if got := lastToolResult(a.session, "bash"); !strings.Contains(got, "[loop guard]") {
		t.Fatalf("last bash result = %q, want permission loop guard", got)
	}
	if got := toolResults(a.session, "bash"); len(got) != stormBreakThreshold {
		t.Fatalf("bash results = %d, want exactly %d blocked attempts", len(got), stormBreakThreshold)
	}
	if len(*notices) == 0 {
		t.Fatal("loop guard should emit a user-facing notice")
	}
}

// TestFinalReadinessPermissionLoopGuardAllowsBlockedFinalForBatch pins the
// multi-call variant: the guard text lands on the batch's FIRST result, so any
// detection keyed to the latest tool message misses it. The loop-guard pass is
// host state and must let the model report the blocker regardless of where in
// the batch the guard text sits.
func TestFinalReadinessPermissionLoopGuardAllowsBlockedFinalForBatch(t *testing.T) {
	todoWrite, ok := tool.LookupBuiltin("todo_write")
	if !ok {
		t.Fatal("todo_write builtin not registered")
	}
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "write_file", readOnly: false})
	reg.Add(fakeTool{name: "bash", readOnly: false})
	reg.Add(todoWrite)
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{
			toolCallChunk("w1", "write_file", `{"path":"changed.go","content":"package main"}`),
			toolCallChunk("t1", "todo_write", `{"todos":[{"content":"Edit code","status":"in_progress"}]}`),
			{Type: provider.ChunkDone},
		},
		{{Type: provider.ChunkText, Text: "premature final"}, {Type: provider.ChunkDone}},
		{
			toolCallChunk("b1a", "bash", `{"command":"go test ./..."}`),
			toolCallChunk("b1b", "bash", `{"command":"go vet ./..."}`),
			{Type: provider.ChunkDone},
		},
		{
			toolCallChunk("b2a", "bash", `{"command":"git status --short"}`),
			toolCallChunk("b2b", "bash", `{"command":"git diff --stat"}`),
			{Type: provider.ChunkDone},
		},
		{
			toolCallChunk("b3a", "bash", `{"command":"ls -la"}`),
			toolCallChunk("b3b", "bash", `{"command":"pwd"}`),
			{Type: provider.ChunkDone},
		},
		{{Type: provider.ChunkText, Text: "blocked by permission"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, reg, NewSession(""), Options{
		Gate: &stubGate{deny: map[string]bool{"bash": true}},
	}, event.Discard)

	if err := a.Run(context.Background(), "edit with todo, then hit batched bash permission blocks"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if prov.call != 6 {
		t.Fatalf("provider calls = %d, want readiness retry, three blocked batches, then final", prov.call)
	}
	results := toolResults(a.session, "bash")
	if len(results) != 2*stormBreakThreshold {
		t.Fatalf("bash results = %d, want %d blocked attempts across three batches", len(results), 2*stormBreakThreshold)
	}
	if !strings.Contains(results[len(results)-2], "[loop guard]") {
		t.Fatalf("first result of the guarded batch should carry the loop guard, got: %q", results[len(results)-2])
	}
	if strings.Contains(results[len(results)-1], "[loop guard]") {
		t.Fatalf("last result of the guarded batch should stay untouched (the pass must not depend on it), got: %q", results[len(results)-1])
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
	a := New(prov, reg, NewSession(""), Options{}, event.Discard)

	if err := a.Run(context.Background(), "create a todo list only"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if prov.call != 2 {
		t.Fatalf("provider calls = %d, want 2 without readiness retry", prov.call)
	}
	if got := lastToolResult(a.session, "todo_write"); !strings.Contains(got, "Todos updated") {
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
	a := New(prov, reg, NewSession(""), Options{}, event.Discard)

	if err := a.Run(context.Background(), "read context and only draft a todo list"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if prov.call != 2 {
		t.Fatalf("provider calls = %d, want 2 without readiness retry", prov.call)
	}
	if got := lastToolResult(a.session, "todo_write"); !strings.Contains(got, "Todos updated") {
		t.Fatalf("todo_write result = %q, want successful todo update", got)
	}
}

func TestFinalReadinessAuditRecordsTerminalError(t *testing.T) {
	todoWrite, ok := tool.LookupBuiltin("todo_write")
	if !ok {
		t.Fatal("todo_write builtin not registered")
	}
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "write_file", readOnly: false})
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
	a := New(prov, reg, NewSession(""), Options{}, sink)

	err := a.Run(context.Background(), "edit with todo and never sign off")
	if err == nil {
		t.Fatal("expected repeated readiness blocks to stop the run")
	}
	if len(sink.events) != 3 {
		t.Fatalf("readiness audit events = %d, want 3: %+v", len(sink.events), sink.events)
	}
	last := sink.events[len(sink.events)-1]
	if last.Result != evidence.ReadinessErrored || last.IncompleteTodos == 0 {
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

	a := New(prov, reg, NewSession(""), Options{}, event.Discard)
	if err := a.Run(context.Background(), "vet the tree and sign off"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	got := toolResult(a.session, "complete_step")
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

	a := New(prov, reg, NewSession(""), Options{}, event.Discard)
	if err := a.Run(context.Background(), "update todos then sign off the wrong step"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	got := toolResult(a.session, "complete_step")
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

	a := New(prov, reg, NewSession(""), Options{}, event.Discard)
	if err := a.Run(context.Background(), "complete the todo with a sign-off first"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if got := lastToolResult(a.session, "todo_write"); !strings.Contains(got, "Todos updated") {
		t.Fatalf("final todo_write result = %q, want update accepted", got)
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

	a := New(prov, reg, NewSession(""), Options{}, event.Discard)
	if err := a.Run(context.Background(), "complete the todo without a sign-off"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	results := toolResults(a.session, "todo_write")
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

	a := New(prov, reg, NewSession(""), Options{}, event.Discard)
	if err := a.Run(context.Background(), "recover after a failed complete_step"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	stepResult := lastToolResult(a.session, "complete_step")
	if !strings.Contains(stepResult, "no matching successful receipt") {
		t.Fatalf("complete_step result = %q, want the sign-off attempt to fail first", stepResult)
	}
	if !strings.Contains(stepResult, `python \"script.py\"`) {
		t.Fatalf("complete_step result = %q, want the self-correction hint to include the real command", stepResult)
	}
	if strings.Contains(stepResult, "todo_write") {
		t.Fatalf("complete_step result = %q, want command hints without todo tool noise", stepResult)
	}
	if got := lastToolResult(a.session, "todo_write"); !strings.Contains(got, "1 completed") {
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

	a := New(prov, reg, NewSession(""), Options{}, event.Discard)
	if err := a.Run(context.Background(), "recover from a rejected batch todo update"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	stepResults := toolResults(a.session, "complete_step")
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

	a := New(prov, reg, NewSession(""), Options{}, event.Discard)
	if err := a.Run(context.Background(), "attempt completion after a failed sign-off"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	results := toolResults(a.session, "todo_write")
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

	a := New(prov, reg, NewSession(""), Options{}, event.Discard)
	if err := a.Run(context.Background(), "try to reuse a numeric sign-off for another todo"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	results := toolResults(a.session, "todo_write")
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

	a := New(prov, reg, NewSession(""), Options{}, event.Discard)
	if err := a.Run(context.Background(), "complete the signed todo after reordering it"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	results := toolResults(a.session, "todo_write")
	if len(results) != 2 || !strings.Contains(results[1], "completed after unfinished") {
		t.Fatalf("reordered todo_write results = %v, want serial-order rejection", results)
	}
	for i, todo := range a.CanonicalTodoState() {
		if todo.Status != "completed" {
			t.Fatalf("canonical todo %d = %+v, want completed after serial recovery", i+1, todo)
		}
	}
}
