package agent

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func TestAgentReusesPreacceptedUserAndPreservesEditMetadata(t *testing.T) {
	prov := &mockProvider{name: "executor", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "done"},
		{Type: provider.ChunkDone},
	}}
	sess := NewSession("system")
	sess.Add(provider.Message{
		Role:     provider.RoleUser,
		Content:  "stable display text",
		Images:   []string{"data:image/png;base64,b2xk"},
		Edited:   true,
		Original: "original prompt",
	})
	a := New(prov, tool.NewRegistry(), sess, Options{ReasoningLanguage: "en", ResponseLanguage: "zh"}, event.Discard)
	persistCalls := 0
	a.SetAcceptedTurnPersistHook(func() error {
		persistCalls++
		return errors.New("preaccepted turns must not run the append hook")
	})
	images := []string{"data:image/png;base64,bmV3"}
	ctx := WithUserImages(WithAcceptedTurn(context.Background(), sess, 1), images)

	if err := a.Run(ctx, "implement the accepted turn"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if persistCalls != 0 {
		t.Fatalf("accepted-turn persist hook calls = %d, want 0 for a preaccepted user", persistCalls)
	}
	msgs := sess.Snapshot()
	if got := len(msgs); got != 3 {
		t.Fatalf("messages = %d, want system + reused user + assistant", got)
	}
	user := msgs[1]
	if user.Role != provider.RoleUser || !strings.Contains(user.Content, "implement the accepted turn") {
		t.Fatalf("reused user = %+v, want final model input", user)
	}
	if got := lastUser(prov.requests[0]); got != user.Content {
		t.Fatalf("provider user = %q, persisted user = %q; want the same final input", got, user.Content)
	}
	if !user.Edited || user.Original != "original prompt" {
		t.Fatalf("edit metadata was replaced: Edited=%v Original=%q", user.Edited, user.Original)
	}
	if len(user.Images) != 1 || user.Images[0] != images[0] {
		t.Fatalf("reused images = %v, want %v", user.Images, images)
	}
	images[0] = "mutated by caller"
	if got := sess.Snapshot()[1].Images[0]; got != "data:image/png;base64,bmV3" {
		t.Fatalf("accepted images alias caller storage: %q", got)
	}
	if !sess.NeedsRewriteSave() {
		t.Fatal("accepted user replacement did not mark the durable transcript for rewrite")
	}
	if !HasAcceptedTurn(ctx) {
		t.Fatal("accepted-turn top-level marker disappeared after consumption")
	}
	if index, ok := AcceptedTurnMessageIndex(ctx); !ok || index != 1 {
		t.Fatalf("AcceptedTurnMessageIndex = (%d, %v), want (1, true)", index, ok)
	}
}

func TestAcceptedTurnObserverRunsBeforeSessionRewriteExactlyOnce(t *testing.T) {
	prov := &mockProvider{name: "executor", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "done"},
		{Type: provider.ChunkDone},
	}}
	sess := NewSession("system")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "stable display"})
	a := New(prov, tool.NewRegistry(), sess, Options{}, event.Discard)
	ctx := WithAcceptedTurn(context.Background(), sess, 1)
	observerEntered := make(chan string, 1)
	releaseObserver := make(chan struct{})
	var observerCalls atomic.Int32
	if !OnAcceptedTurnReuse(ctx, func(content string) {
		observerCalls.Add(1)
		observerEntered <- content
		<-releaseObserver
	}) {
		t.Fatal("OnAcceptedTurnReuse did not register")
	}

	runDone := make(chan error, 1)
	go func() { runDone <- a.Run(ctx, "final model input") }()
	observedContent := <-observerEntered
	if !strings.Contains(observedContent, "final model input") {
		t.Fatalf("observer content = %q, want final provider-visible input", observedContent)
	}
	if got := sess.Snapshot()[1].Content; got != "stable display" {
		t.Fatalf("Session rewrote before observer completed: %q", got)
	}
	close(releaseObserver)
	if err := <-runDone; err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := observerCalls.Load(); got != 1 {
		t.Fatalf("observer calls = %d, want exactly 1", got)
	}
	if got := sess.Snapshot()[1].Content; !strings.Contains(got, "final model input") {
		t.Fatalf("Session content after observer = %q, want final input", got)
	}

	// The top-level context remains marked, but its ref is consumed. A later
	// synthetic Agent round appends normally and must not replay the observer.
	if err := a.Run(ctx, "synthetic continuation"); err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if got := observerCalls.Load(); got != 1 {
		t.Fatalf("observer calls after synthetic continuation = %d, want 1", got)
	}
}

func TestAcceptedTurnObserverIgnoresMismatchAndControllerConsume(t *testing.T) {
	t.Run("session mismatch", func(t *testing.T) {
		executorSession := NewSession("executor-system")
		executorSession.Add(provider.Message{Role: provider.RoleUser, Content: "stable display"})
		ctx := WithAcceptedTurn(context.Background(), executorSession, 1)
		var calls atomic.Int32
		if !OnAcceptedTurnReuse(ctx, func(string) { calls.Add(1) }) {
			t.Fatal("OnAcceptedTurnReuse did not register")
		}
		plannerProvider := &mockProvider{name: "planner", chunks: []provider.Chunk{
			{Type: provider.ChunkText, Text: "plan"},
			{Type: provider.ChunkDone},
		}}
		planner := NewReadOnlyAgent(plannerProvider, tool.NewRegistry(), NewSession("planner-system"), Options{}, event.Discard)
		if err := planner.Run(ctx, "research"); err != nil {
			t.Fatalf("planner Run: %v", err)
		}
		if got := calls.Load(); got != 0 {
			t.Fatalf("observer calls after Session mismatch = %d, want 0", got)
		}

		executorProvider := &mockProvider{name: "executor", chunks: []provider.Chunk{
			{Type: provider.ChunkText, Text: "done"},
			{Type: provider.ChunkDone},
		}}
		executor := New(executorProvider, tool.NewRegistry(), executorSession, Options{}, event.Discard)
		if err := executor.Run(ctx, "execute"); err != nil {
			t.Fatalf("executor Run: %v", err)
		}
		if got := calls.Load(); got != 1 {
			t.Fatalf("observer calls after executor reuse = %d, want 1", got)
		}
	})

	t.Run("Controller consume", func(t *testing.T) {
		sess := NewSession("system")
		sess.Add(provider.Message{Role: provider.RoleUser, Content: "stable display"})
		ctx := WithAcceptedTurn(context.Background(), sess, 1)
		var calls atomic.Int32
		if !OnAcceptedTurnReuse(ctx, func(string) { calls.Add(1) }) {
			t.Fatal("OnAcceptedTurnReuse did not register")
		}
		if _, ok := ConsumeAcceptedTurn(ctx, sess); !ok {
			t.Fatal("ConsumeAcceptedTurn returned false")
		}
		if got := calls.Load(); got != 0 {
			t.Fatalf("observer calls after Controller consume = %d, want 0", got)
		}
		if OnAcceptedTurnReuse(ctx, func(string) { calls.Add(1) }) {
			t.Fatal("registered observer on an already consumed ref")
		}
	})

	if OnAcceptedTurnReuse(context.Background(), func(string) {}) {
		t.Fatal("registered observer without an accepted ref")
	}
}

func TestAcceptedTurnSessionMismatchLeavesRefForExecutor(t *testing.T) {
	executorSession := NewSession("executor-system")
	executorSession.Add(provider.Message{Role: provider.RoleUser, Content: "accepted display"})
	ctx := WithAcceptedTurn(context.Background(), executorSession, 1)

	plannerProvider := &mockProvider{name: "planner", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "plan"},
		{Type: provider.ChunkDone},
	}}
	plannerSession := NewSession("planner-system")
	planner := NewReadOnlyAgent(plannerProvider, tool.NewRegistry(), plannerSession, Options{}, event.Discard)
	if err := planner.Run(ctx, "research the task"); err != nil {
		t.Fatalf("planner Run: %v", err)
	}
	if got := executorSession.Snapshot()[1].Content; got != "accepted display" {
		t.Fatalf("planner consumed executor ref: executor user = %q", got)
	}
	if got := countMessagesWithRole(plannerSession.Snapshot(), provider.RoleUser); got != 1 {
		t.Fatalf("planner users = %d, want its ordinary appended user", got)
	}

	executorProvider := &mockProvider{name: "executor", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "done"},
		{Type: provider.ChunkDone},
	}}
	executor := New(executorProvider, tool.NewRegistry(), executorSession, Options{}, event.Discard)
	if err := executor.Run(ctx, "execute the task"); err != nil {
		t.Fatalf("executor Run: %v", err)
	}
	executorMessages := executorSession.Snapshot()
	if got := countMessagesWithRole(executorMessages, provider.RoleUser); got != 1 {
		t.Fatalf("executor users = %d, want exactly the preaccepted user", got)
	}
	if got := executorMessages[1].Content; !strings.Contains(got, "execute the task") {
		t.Fatalf("executor did not consume the retained ref: %q", got)
	}
}

func TestCoordinatorNoOpReusesPreacceptedUser(t *testing.T) {
	planner := &mockProvider{name: "planner", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "The implementation is already complete.\n[no_changes]"},
		{Type: provider.ChunkDone},
	}}
	executorProvider := &mockProvider{name: "executor", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "must not run"},
		{Type: provider.ChunkDone},
	}}
	executorSession := NewSession("executor-system")
	executorSession.Add(provider.Message{
		Role:     provider.RoleUser,
		Content:  "accepted display",
		Edited:   true,
		Original: "before edit",
	})
	executor := New(executorProvider, tool.NewRegistry(), executorSession, Options{}, event.Discard)
	// A tool-enabled planner runs through its own Agent with the same ctx. Its
	// different Session must leave the executor's ref untouched for the no-op
	// persistence path below.
	coord := NewCoordinator(planner, NewSession("planner-system"), nil, tool.NewRegistry(), Options{}, executor, 0, event.Discard, nil)
	ctx := WithAcceptedTurn(context.Background(), executorSession, 1)

	if err := coord.Run(ctx, "check whether the implementation is complete"); err != nil {
		t.Fatalf("Coordinator Run: %v", err)
	}
	if got := len(executorProvider.requests); got != 0 {
		t.Fatalf("executor requests = %d, want planner no-op", got)
	}
	msgs := executorSession.Snapshot()
	if got := countMessagesWithRole(msgs, provider.RoleUser); got != 1 {
		t.Fatalf("executor users = %d, want one preaccepted user without a ghost duplicate", got)
	}
	if got := len(msgs); got != 3 {
		t.Fatalf("executor messages = %d, want system + reused user + planner conclusion", got)
	}
	if !strings.Contains(msgs[1].Content, "check whether the implementation is complete") {
		t.Fatalf("reused no-op user = %q, want original task", msgs[1].Content)
	}
	if !msgs[1].Edited || msgs[1].Original != "before edit" {
		t.Fatalf("no-op reuse lost edit metadata: %+v", msgs[1])
	}
	if msgs[2].Role != provider.RoleAssistant || !strings.Contains(msgs[2].Content, noChangesMarker) {
		t.Fatalf("planner conclusion = %+v", msgs[2])
	}
}

func TestCoordinatorStoppedPlannerPathsReusePreacceptedUser(t *testing.T) {
	tests := []struct {
		name      string
		plan      string
		configure func(*Coordinator)
	}{
		{
			name: "approval declined",
			plan: "Rewrite auth.\n" + plannerRequiresApprovalMarker,
			configure: func(c *Coordinator) {
				c.SetPlannerPlanApprover(&coordinatorApprovalGate{allow: false})
			},
		},
		{
			name: "decision unanswered",
			plan: "Choose storage.\n<planner-ask>\nquestion: Which database?\noption: sqlite\noption: postgres\n</planner-ask>",
			configure: func(c *Coordinator) {
				c.SetPlannerUserDecisionAsker(&coordinatorDecisionGate{answer: ""})
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			planner := &mockProvider{name: "planner", chunks: []provider.Chunk{
				{Type: provider.ChunkText, Text: test.plan},
				{Type: provider.ChunkDone},
			}}
			executorProvider := &mockProvider{name: "executor", chunks: []provider.Chunk{
				{Type: provider.ChunkText, Text: "must not run"},
				{Type: provider.ChunkDone},
			}}
			sess := NewSession("executor-system")
			sess.Add(provider.Message{Role: provider.RoleUser, Content: "accepted display"})
			executor := New(executorProvider, tool.NewRegistry(), sess, Options{}, event.Discard)
			coord := NewCoordinator(planner, NewSession("planner-system"), nil, nil, Options{}, executor, 0, event.Discard, nil)
			test.configure(coord)

			if err := coord.Run(WithAcceptedTurn(context.Background(), sess, 1), "original task"); err != nil {
				t.Fatalf("Coordinator Run: %v", err)
			}
			if got := len(executorProvider.requests); got != 0 {
				t.Fatalf("executor requests = %d, want stopped planner path", got)
			}
			msgs := sess.Snapshot()
			if got := countMessagesWithRole(msgs, provider.RoleUser); got != 1 {
				t.Fatalf("executor users = %d, want one preaccepted user", got)
			}
			if len(msgs) != 3 || msgs[2].Role != provider.RoleAssistant {
				t.Fatalf("persisted stopped turn = %+v, want system/user/assistant", msgs)
			}
			if !strings.Contains(msgs[1].Content, "original task") {
				t.Fatalf("reused user = %q, want original task", msgs[1].Content)
			}
		})
	}
}

func TestConsumedAcceptedTurnFallsBackToOrdinaryAppend(t *testing.T) {
	prov := &mockProvider{name: "executor", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "done"},
		{Type: provider.ChunkDone},
	}}
	sess := NewSession("system")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "accepted display"})
	a := New(prov, tool.NewRegistry(), sess, Options{}, event.Discard)
	ctx := WithAcceptedTurn(context.Background(), sess, 1)

	if err := a.Run(ctx, "first model round"); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	if err := a.Run(ctx, "synthetic continuation"); err != nil {
		t.Fatalf("second Run: %v", err)
	}
	msgs := sess.Snapshot()
	if got := countMessagesWithRole(msgs, provider.RoleUser); got != 2 {
		t.Fatalf("users = %d, want reused top-level user + appended synthetic user", got)
	}
	if !strings.Contains(msgs[1].Content, "first model round") {
		t.Fatalf("first accepted user was rewritten twice: %q", msgs[1].Content)
	}
	if !strings.Contains(msgs[3].Content, "synthetic continuation") {
		t.Fatalf("synthetic continuation was not appended: %+v", msgs)
	}
	if !HasAcceptedTurn(ctx) {
		t.Fatal("top-level accepted marker must remain visible to Controller internal rounds")
	}
}

func TestControllerCanConsumeAcceptedTurnBeforeSyntheticAgentRound(t *testing.T) {
	sess := NewSession("system")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "accepted display"})
	ctx := WithAcceptedTurn(context.Background(), sess, 1)
	index, ok := ConsumeAcceptedTurn(ctx, sess)
	if !ok || index != 1 {
		t.Fatalf("ConsumeAcceptedTurn = (%d, %v), want (1, true)", index, ok)
	}
	if _, ok := ConsumeAcceptedTurn(ctx, sess); ok {
		t.Fatal("accepted turn was consumed more than once")
	}
	if !HasAcceptedTurn(ctx) {
		t.Fatal("top-level accepted marker disappeared after Controller consumption")
	}

	prov := &mockProvider{name: "executor", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "done"},
		{Type: provider.ChunkDone},
	}}
	a := New(prov, tool.NewRegistry(), sess, Options{}, event.Discard)
	if err := a.Run(ctx, "goal continuation"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	msgs := sess.Snapshot()
	if got := countMessagesWithRole(msgs, provider.RoleUser); got != 2 {
		t.Fatalf("users = %d, want Controller-owned accepted user + synthetic append", got)
	}
	if got := msgs[1].Content; got != "accepted display" {
		t.Fatalf("Controller-consumed user was rewritten by later Agent.Run: %q", got)
	}
}

func TestAgentWithoutAcceptedTurnKeepsAppendAndPersistBehavior(t *testing.T) {
	prov := &mockProvider{name: "local", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "done"},
		{Type: provider.ChunkDone},
	}}
	sess := NewSession("system")
	a := New(prov, tool.NewRegistry(), sess, Options{}, event.Discard)
	persistCalls := 0
	a.SetAcceptedTurnPersistHook(func() error {
		persistCalls++
		return nil
	})

	if err := a.Run(context.Background(), "ordinary local input"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if persistCalls != 1 {
		t.Fatalf("persist hook calls = %d, want 1 for ordinary append behavior", persistCalls)
	}
	msgs := sess.Snapshot()
	if got := countMessagesWithRole(msgs, provider.RoleUser); got != 1 {
		t.Fatalf("users = %d, want one ordinarily appended user", got)
	}
	if !strings.Contains(msgs[1].Content, "ordinary local input") {
		t.Fatalf("ordinary appended user = %q", msgs[1].Content)
	}
	if HasAcceptedTurn(context.Background()) {
		t.Fatal("plain context unexpectedly reports an accepted turn")
	}
}

func countMessagesWithRole(messages []provider.Message, role provider.Role) int {
	count := 0
	for _, message := range messages {
		if message.Role == role {
			count++
		}
	}
	return count
}
