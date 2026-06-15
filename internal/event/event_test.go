package event

import (
	"sync"
	"testing"

	"reasonix/internal/evidence"
	"reasonix/internal/provider"
)

// --- Kind constants ---

func TestKindConstants(t *testing.T) {
	// Verify the iota sequence is stable and sequential.
	kinds := []Kind{
		TurnStarted, Reasoning, Text, Message, ToolDispatch, ToolResult,
		Usage, Notice, Phase, ApprovalRequest, AskRequest, TurnDone,
	}
	for i, k := range kinds {
		if int(k) != i {
			t.Errorf("Kind %d: got %d", i, int(k))
		}
	}
}

// --- Level constants ---

func TestLevelConstants(t *testing.T) {
	if LevelInfo != 0 {
		t.Errorf("LevelInfo = %d, want 0", LevelInfo)
	}
	if LevelWarn != 1 {
		t.Errorf("LevelWarn = %d, want 1", LevelWarn)
	}
}

// --- FuncSink ---

func TestFuncSinkEmit(t *testing.T) {
	var received Event
	fs := FuncSink(func(e Event) { received = e })
	e := Event{Kind: Text, Text: "hello"}
	fs.Emit(e)
	if received.Kind != Text || received.Text != "hello" {
		t.Errorf("FuncSink did not forward event: got %+v", received)
	}
}

func TestFuncSinkNilEmitIsNoop(t *testing.T) {
	var fs FuncSink

	fs.Emit(Event{Kind: Text, Text: "hello"})
}

type typedNilSink struct{}

func (*typedNilSink) Emit(Event) {}

func TestSyncTreatsTypedNilSinkAsDiscard(t *testing.T) {
	var base *typedNilSink

	Sync(base).Emit(Event{Kind: Text, Text: "hello"})
}

type readinessAuditRecorder struct {
	events []evidence.ReadinessAudit
}

func (r *readinessAuditRecorder) Emit(Event) {}

func (r *readinessAuditRecorder) RecordReadinessAudit(a evidence.ReadinessAudit) {
	r.events = append(r.events, a)
}

func TestSyncForwardsReadinessAuditReceipts(t *testing.T) {
	rec := &readinessAuditRecorder{}
	sink := Sync(rec)

	RecordReadinessAudit(sink, evidence.ReadinessAudit{
		Result:                 evidence.ReadinessBlocked,
		MissingProjectChecks:   1,
		CommandMismatchMissing: 1,
	})

	if len(rec.events) != 1 {
		t.Fatalf("readiness audit events = %d, want 1", len(rec.events))
	}
	if rec.events[0].Result != evidence.ReadinessBlocked || rec.events[0].MissingProjectChecks != 1 {
		t.Fatalf("readiness audit not forwarded through Sync: %+v", rec.events[0])
	}
}

// --- Discard ---

func TestDiscardSink(t *testing.T) {
	// Discard should accept any event without panic.
	Discard.Emit(Event{Kind: TurnStarted})
	Discard.Emit(Event{Kind: Text, Text: "discarded"})
	Discard.Emit(Event{Kind: TurnDone})
}

// --- Event struct field access ---

func TestEventFields(t *testing.T) {
	usage := &provider.Usage{PromptTokens: 100, CompletionTokens: 50}
	pricing := &provider.Pricing{Input: 2.0, Output: 10.0, Currency: "$"}

	e := Event{
		Kind:        Usage,
		Usage:       usage,
		Pricing:     pricing,
		SessionHit:  80,
		SessionMiss: 20,
	}
	if e.Kind != Usage {
		t.Errorf("Kind = %d, want %d", e.Kind, Usage)
	}
	if e.Usage.PromptTokens != 100 {
		t.Errorf("PromptTokens = %d, want 100", e.Usage.PromptTokens)
	}
	if e.Pricing.Currency != "$" {
		t.Errorf("Currency = %q, want $", e.Pricing.Currency)
	}
	if e.SessionHit != 80 || e.SessionMiss != 20 {
		t.Errorf("SessionHit=%d, SessionMiss=%d", e.SessionHit, e.SessionMiss)
	}
}

// --- Tool struct ---

func TestToolStruct(t *testing.T) {
	tool := Tool{
		ID:       "call-1",
		Name:     "bash",
		Args:     `{"command":"echo hi"}`,
		ReadOnly: false,
		Partial:  true,
		ParentID: "parent-1",
	}
	if tool.ID != "call-1" || tool.Name != "bash" {
		t.Errorf("unexpected tool: %+v", tool)
	}
	if !tool.Partial {
		t.Error("Partial should be true")
	}
	if tool.ParentID != "parent-1" {
		t.Errorf("ParentID = %q", tool.ParentID)
	}

	result := Tool{
		ID:        "call-1",
		Name:      "bash",
		Output:    "hi\n",
		Err:       "",
		Truncated: false,
	}
	if result.Output != "hi\n" {
		t.Errorf("Output = %q", result.Output)
	}
}

// --- Approval struct ---

func TestApprovalStruct(t *testing.T) {
	a := Approval{ID: "42", Tool: "bash", Subject: "rm -rf /"}
	if a.ID != "42" || a.Tool != "bash" || a.Subject != "rm -rf /" {
		t.Errorf("unexpected approval: %+v", a)
	}
}

// --- Ask / AskQuestion / AskOption / AskAnswer ---

func TestAskStructs(t *testing.T) {
	q := AskQuestion{
		ID:     "q1",
		Header: "Confirm",
		Prompt: "Are you sure?",
		Options: []AskOption{
			{Label: "Yes", Description: "Proceed"},
			{Label: "No", Description: "Cancel"},
		},
		Multi: false,
	}
	ask := Ask{
		ID:        "ask-1",
		Questions: []AskQuestion{q},
	}
	if len(ask.Questions) != 1 {
		t.Fatalf("questions count = %d", len(ask.Questions))
	}
	if ask.Questions[0].Options[0].Label != "Yes" {
		t.Errorf("first option = %q", ask.Questions[0].Options[0].Label)
	}

	ans := AskAnswer{QuestionID: "q1", Selected: []string{"Yes"}}
	if len(ans.Selected) != 1 || ans.Selected[0] != "Yes" {
		t.Errorf("answer = %+v", ans)
	}
}

// --- Multiple Emit via channel-backed sink ---

func TestChannelBackedSink(t *testing.T) {
	ch := make(chan Event, 8)
	sink := FuncSink(func(e Event) { ch <- e })

	events := []Event{
		{Kind: TurnStarted},
		{Kind: Text, Text: "hello"},
		{Kind: ToolDispatch, Tool: Tool{Name: "bash"}},
		{Kind: ToolResult, Tool: Tool{Output: "ok"}},
		{Kind: Usage, Usage: &provider.Usage{TotalTokens: 42}},
		{Kind: Notice, Level: LevelWarn, Text: "heads up"},
		{Kind: TurnDone},
	}
	for _, e := range events {
		sink.Emit(e)
	}

	for i, want := range events {
		got := <-ch
		if got.Kind != want.Kind {
			t.Errorf("event %d: Kind = %d, want %d", i, got.Kind, want.Kind)
		}
	}
}

// --- FuncSink forwards every concurrent Emit exactly once ---

// FuncSink.Emit forwards to the wrapped func with no synchronization of its own,
// so a concurrency-safe callback is the caller's responsibility (here a
// mutex-guarded counter). This verifies that N concurrent Emits produce exactly
// N forwarded calls, and under `go test -race` that the forwarding itself is
// race-free.
func TestFuncSinkForwardsEachConcurrentEmit(t *testing.T) {
	var mu sync.Mutex
	var count int
	sink := FuncSink(func(e Event) {
		mu.Lock()
		count++
		mu.Unlock()
	})
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sink.Emit(Event{Kind: Text})
		}()
	}
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	if count != 100 {
		t.Errorf("count = %d, want 100", count)
	}
}
