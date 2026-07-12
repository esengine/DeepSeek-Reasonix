package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"reasonix/internal/agent/testutil"
	"reasonix/internal/event"
	"reasonix/internal/memorycompiler"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

type toolCallReasoningRequiredProvider struct {
	*testutil.MockProvider
}

func (p toolCallReasoningRequiredProvider) RequiresToolCallReasoning() bool { return true }

func echoRegistry() *tool.Registry {
	reg := tool.NewRegistry()
	reg.Add(echoTool{})
	return reg
}

// TestRunMultiToolRoundEmptyIDsSurvivePairing drives the real loop through a turn
// that fans out two tool calls carrying no id (a gateway that streams by index),
// then asserts both results still pair back after SanitizeToolPairing — the repair
// that runs on every send. Keying on tool_call_id alone collapsed them into one,
// dropping a result from the model's context on the very next turn.
func TestRunMultiToolRoundEmptyIDsSurvivePairing(t *testing.T) {
	mp := testutil.NewMock("m",
		testutil.Turn{ToolCalls: []provider.ToolCall{
			{ID: "", Name: "echo", Arguments: `{"text":"alpha"}`},
			{ID: "", Name: "echo", Arguments: `{"text":"beta"}`},
		}},
		testutil.Turn{Text: "done"},
	)
	a := New(mp, echoRegistry(), NewSession(""), Options{}, event.Discard)
	if err := a.Run(context.Background(), "go"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	repaired := provider.SanitizeToolPairing(a.Session().Messages)
	var results []string
	for _, m := range repaired {
		if m.Role == provider.RoleTool {
			results = append(results, m.Content)
		}
	}
	if len(results) != 2 {
		t.Fatalf("want 2 tool results after pairing, got %d: %v", len(results), results)
	}
	if results[0] == results[1] {
		t.Fatalf("both results collapsed to %q — one was lost from the model's context", results[0])
	}
	if !strings.Contains(results[0], "alpha") || !strings.Contains(results[1], "beta") {
		t.Errorf("results lost their identity: %v", results)
	}
}

func TestRunPersistsCumulativeAssistantWorkDuration(t *testing.T) {
	mp := testutil.NewMock("m",
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "call-1", Name: "echo", Arguments: `{"text":"hello"}`}}},
		testutil.Turn{Text: "done"},
	)
	a := New(mp, echoRegistry(), NewSession(""), Options{}, event.Discard)
	if err := a.Run(context.Background(), "go"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	var durations []int64
	for _, message := range a.Session().Messages {
		if message.Role == provider.RoleAssistant {
			durations = append(durations, message.WorkDurationMs)
		}
	}
	if len(durations) != 2 {
		t.Fatalf("assistant durations = %v, want two rounds", durations)
	}
	if durations[0] <= 0 || durations[1] < durations[0] {
		t.Fatalf("assistant durations must be positive and cumulative: %v", durations)
	}
}

func TestRunSkipsMemoryCompilerForSyntheticTurn(t *testing.T) {
	rt := memorycompiler.New(t.TempDir())
	_, seed := rt.StartTurn(context.Background(), "fix a bug", nil)
	seed.RecordToolResults([]memorycompiler.ToolRecord{
		{Name: "bash", Error: "exit status 1"},
		{Name: "bash", Error: "exit status 1"},
	})
	seed.Finish(nil)

	mp := testutil.NewMock("m", testutil.Turn{Text: "done"})
	a := New(mp, echoRegistry(), NewSession(""), Options{MemoryCompiler: rt}, event.Discard)
	// A controller-injected synthetic turn (e.g. the goal-loop continuation)
	// marks the context so the compiler is bypassed; otherwise the echoed
	// contract re-injects every turn and spins the loop (#5342, #5329).
	ctx := WithMemoryCompilerSkip(context.Background())
	if err := a.Run(ctx, "continue work"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	user := lastUserMessage(t, mp.Requests())
	if strings.Contains(user.Content, "<memory-compiler-execution>") {
		t.Fatalf("synthetic turn was compiled into a contract:\n%s", user.Content)
	}
	if !strings.Contains(user.Content, "continue work") {
		t.Fatalf("synthetic turn text was lost:\n%s", user.Content)
	}
}

func lastUserMessage(t *testing.T, reqs []provider.Request) provider.Message {
	t.Helper()
	if len(reqs) == 0 {
		t.Fatal("no requests recorded")
	}
	var user provider.Message
	for _, msg := range reqs[0].Messages {
		if msg.Role == provider.RoleUser {
			user = msg
		}
	}
	return user
}

func TestRunUsesMemoryCompilerContractAsUserTurn(t *testing.T) {
	rt := memorycompiler.New(t.TempDir())
	_, seed := rt.StartTurn(context.Background(), "fix a bug", nil)
	seed.RecordToolResults([]memorycompiler.ToolRecord{
		{Name: "bash", Error: "exit status 1"},
		{Name: "bash", Error: "exit status 1"},
	})
	seed.Finish(nil)

	mp := testutil.NewMock("m", testutil.Turn{Text: "done"})
	a := New(mp, echoRegistry(), NewSession(""), Options{
		MemoryCompiler:          rt,
		MemoryCompilerVerbosity: MemoryCompilerVerbosityCompact,
	}, event.Discard)
	if err := a.Run(context.Background(), "continue work"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	reqs := mp.Requests()
	if len(reqs) != 1 {
		t.Fatalf("requests = %d, want 1", len(reqs))
	}
	var user provider.Message
	for _, msg := range reqs[0].Messages {
		if msg.Role == provider.RoleUser {
			user = msg
		}
	}
	if !strings.HasPrefix(user.Content, "<memory-compiler-execution>") {
		t.Fatalf("user turn was not replaced by compiled contract:\n%s", user.Content)
	}
	if strings.HasPrefix(user.Content, "continue work\n\n") {
		t.Fatalf("compiled contract was appended as a sidecar instead of replacing the turn:\n%s", user.Content)
	}
	if !strings.Contains(user.Content, `"source_event":"continue work"`) {
		t.Fatalf("compiled contract lost the source event:\n%s", user.Content)
	}
}

func TestRunMemoryCompilerDefaultsToObserveMode(t *testing.T) {
	rt := memorycompiler.New(t.TempDir())
	_, seed := rt.StartTurn(context.Background(), "fix a bug", nil)
	seed.RecordToolResults([]memorycompiler.ToolRecord{
		{Name: "bash", Output: "tests passed"},
	})
	seed.Finish(nil)

	mp := testutil.NewMock("m", testutil.Turn{Text: "done"})
	var stats []event.MemoryCompilerStats
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.MemoryCompilerStatsEvent && e.MemoryCompiler != nil {
			stats = append(stats, *e.MemoryCompiler)
		}
	})
	a := New(mp, echoRegistry(), NewSession(""), Options{MemoryCompiler: rt}, sink)
	if err := a.Run(context.Background(), "continue work"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	reqs := mp.Requests()
	if len(reqs) != 1 {
		t.Fatalf("requests = %d, want 1", len(reqs))
	}
	user := lastUserMessageFromRequest(t, reqs[0])
	if user.Content != "continue work" {
		t.Fatalf("observe mode should preserve raw user input, got:\n%s", user.Content)
	}
	if strings.Contains(user.Content, "<memory-compiler-execution>") {
		t.Fatalf("observe mode exposed Memory v5 contract:\n%s", user.Content)
	}
	if len(stats) != 1 {
		t.Fatalf("memory compiler stats events = %d, want 1", len(stats))
	}
	if stats[0].Injected || !stats[0].UsefulIR || stats[0].CompiledTokens != 0 {
		t.Fatalf("observe mode stats should be useful but not injected: %+v", stats[0])
	}
}

func TestRunCompilesMemoryGoalFromRawInputBeforeReasoningLanguage(t *testing.T) {
	rt := memorycompiler.New(t.TempDir())
	_, seed := rt.StartTurn(context.Background(), "fix a bug", nil)
	seed.RecordToolResults([]memorycompiler.ToolRecord{
		{Name: "bash", Error: "exit status 1"},
		{Name: "bash", Error: "exit status 1"},
	})
	seed.Finish(nil)

	mp := testutil.NewMock("m", testutil.Turn{Text: "done"})
	a := New(mp, echoRegistry(), NewSession(""), Options{
		MemoryCompiler:          rt,
		MemoryCompilerVerbosity: MemoryCompilerVerbosityCompact,
	}, event.Discard)
	a.SetReasoningLanguage("zh")
	if err := a.Run(context.Background(), "fix another bug"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	req := mp.Requests()[0]
	user := req.Messages[len(req.Messages)-1]
	if !strings.Contains(user.Content, `"source_event":"fix another bug"`) {
		t.Fatalf("compiled contract did not keep raw source event:\n%s", user.Content)
	}
	if strings.Contains(user.Content, `"source_event":"<reasoning-language>`) {
		t.Fatalf("reasoning language wrapper leaked into source event:\n%s", user.Content)
	}
	if !strings.Contains(user.Content, "<reasoning-language>") {
		t.Fatalf("reasoning language wrapper should still apply to final provider input:\n%s", user.Content)
	}
}

func TestRunCompilesMemorySourceFromUnexpandedContext(t *testing.T) {
	rt := memorycompiler.New(t.TempDir())
	_, seed := rt.StartTurn(context.Background(), "fix a bug", nil)
	seed.RecordToolResults([]memorycompiler.ToolRecord{
		{Name: "bash", Error: "exit status 1"},
		{Name: "bash", Error: "exit status 1"},
	})
	seed.Finish(nil)

	expanded := "Referenced context:\n\n<file path=\"auth.go\">\npackage main\nconst secret = true\n</file>\n\nfix @auth.go"
	raw := "fix @auth.go"
	mp := testutil.NewMock("m", testutil.Turn{Text: "done"})
	var stats []event.MemoryCompilerStats
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.MemoryCompilerStatsEvent && e.MemoryCompiler != nil {
			stats = append(stats, *e.MemoryCompiler)
		}
	})
	a := New(mp, echoRegistry(), NewSession(""), Options{
		MemoryCompiler:          rt,
		MemoryCompilerVerbosity: MemoryCompilerVerbosityCompact,
	}, sink)
	ctx := WithMemoryCompilerSourceInput(context.Background(), raw)
	if err := a.Run(ctx, expanded); err != nil {
		t.Fatalf("Run: %v", err)
	}

	req := mp.Requests()[0]
	user := req.Messages[len(req.Messages)-1]
	if !strings.Contains(user.Content, `"source_event":"fix @auth.go"`) {
		t.Fatalf("compiled contract did not use raw source event:\n%s", user.Content)
	}
	if strings.Contains(user.Content, "Referenced context:") || strings.Contains(user.Content, "const secret") {
		t.Fatalf("expanded reference context leaked into Memory v5 contract:\n%s", user.Content)
	}
	if len(stats) != 1 {
		t.Fatalf("memory compiler stats events = %d, want 1", len(stats))
	}
	if !stats[0].Injected || stats[0].CompiledTokens == 0 || stats[0].MemoryReferences == 0 {
		t.Fatalf("memory compiler stats did not quantify injected memory: %+v", stats[0])
	}
}

func TestRunDoesNotInjectMemoryCompilerContractForGreeting(t *testing.T) {
	rt := memorycompiler.New(t.TempDir())
	_, seed := rt.StartTurn(context.Background(), "fix a bug", nil)
	seed.RecordToolResults([]memorycompiler.ToolRecord{
		{Name: "bash", Error: "exit status 1"},
		{Name: "bash", Error: "exit status 1"},
	})
	seed.Finish(nil)

	mp := testutil.NewMock("m", testutil.Turn{Text: "hi"})
	var stats []event.MemoryCompilerStats
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.MemoryCompilerStatsEvent && e.MemoryCompiler != nil {
			stats = append(stats, *e.MemoryCompiler)
		}
	})
	a := New(mp, echoRegistry(), NewSession(""), Options{MemoryCompiler: rt}, sink)

	if err := a.Run(context.Background(), "hello"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	reqs := mp.Requests()
	if len(reqs) != 1 {
		t.Fatalf("requests = %d, want 1", len(reqs))
	}
	user := lastUserMessageFromRequest(t, reqs[0])
	if strings.Contains(user.Content, "<memory-compiler-execution>") {
		t.Fatalf("greeting was replaced by Memory v5 contract:\n%s", user.Content)
	}
	if user.Content != "hello" {
		t.Fatalf("greeting should stay raw user input, got:\n%s", user.Content)
	}
	// 更新后的行为：聊天输入完全跳过 Memory v5，所以不会有 stats 事件
	if len(stats) != 0 {
		t.Fatalf("memory compiler stats events = %d, want 0 (chat inputs skip Memory v5 entirely)", len(stats))
	}
}

func TestRunThrottlesMemoryCompilerInjectionButKeepsLearning(t *testing.T) {
	rt := memorycompiler.New(t.TempDir())
	_, seed := rt.StartTurn(context.Background(), "fix a bug", nil)
	seed.RecordToolResults([]memorycompiler.ToolRecord{
		{Name: "bash", Error: "exit status 1"},
		{Name: "bash", Error: "exit status 1"},
	})
	seed.Finish(nil)

	mp := testutil.NewMock("m",
		testutil.Turn{Text: "first done"},
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "echo-1", Name: "echo", Arguments: `{"text":"fresh throttled signal"}`}}},
		testutil.Turn{Text: "second done"},
	)
	var stats []event.MemoryCompilerStats
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.MemoryCompilerStatsEvent && e.MemoryCompiler != nil {
			stats = append(stats, *e.MemoryCompiler)
		}
	})
	a := New(mp, echoRegistry(), NewSession(""), Options{
		MemoryCompiler:          rt,
		MemoryCompilerVerbosity: MemoryCompilerVerbosityCompact,
	}, sink)

	if err := a.Run(context.Background(), "fix first prompt"); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	if err := a.Run(context.Background(), "fix second prompt"); err != nil {
		t.Fatalf("second Run: %v", err)
	}

	reqs := mp.Requests()
	if len(reqs) != 3 {
		t.Fatalf("requests = %d, want 3", len(reqs))
	}
	firstUser := lastUserMessageFromRequest(t, reqs[0])
	if !strings.Contains(firstUser.Content, "<memory-compiler-execution>") {
		t.Fatalf("first useful Memory v5 turn should inject contract:\n%s", firstUser.Content)
	}
	secondUser := lastUserMessageFromRequest(t, reqs[1])
	if strings.Contains(secondUser.Content, "<memory-compiler-execution>") {
		t.Fatalf("second quick turn should not inject Memory v5 contract:\n%s", secondUser.Content)
	}
	if secondUser.Content != "fix second prompt" {
		t.Fatalf("second quick turn should preserve raw user input, got:\n%s", secondUser.Content)
	}
	if len(stats) != 2 {
		t.Fatalf("memory compiler stats events = %d, want 2", len(stats))
	}
	if !stats[0].Injected || !stats[0].UsefulIR {
		t.Fatalf("first stats should report injected useful IR: %+v", stats[0])
	}
	if stats[1].Injected || !stats[1].UsefulIR || stats[1].CompiledTokens != 0 {
		t.Fatalf("second stats should report useful but non-injected IR: %+v", stats[1])
	}

	compiled, turn := rt.StartTurn(context.Background(), "inspect throttled learning", nil)
	if turn == nil {
		t.Fatal("StartTurn after throttled run returned nil turn")
	}
	defer turn.Finish(nil)
	if !strings.Contains(compiled, "echo succeeded") {
		t.Fatalf("throttled turn did not record tool results for learning:\n%s", compiled)
	}
}

func lastUserMessageFromRequest(t *testing.T, req provider.Request) provider.Message {
	t.Helper()
	var user provider.Message
	for _, msg := range req.Messages {
		if msg.Role == provider.RoleUser {
			user = msg
		}
	}
	if user.Role != provider.RoleUser {
		t.Fatal("request did not contain a user message")
	}
	return user
}

// TestRunCancelledMidStreamLeavesResumableSession proves a turn cancelled before
// the model answered leaves the session well-formed: the user message stands,
// nothing dangling, and the repaired history is sendable as-is on resume.
func TestRunCancelledMidStreamLeavesResumableSession(t *testing.T) {
	mp := testutil.NewMock("m", testutil.ErrorTurn(context.Canceled))
	a := New(mp, echoRegistry(), NewSession("sys"), Options{}, event.Discard)

	err := a.Run(context.Background(), "do the thing")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run should surface the cancellation, got %v", err)
	}

	repaired := provider.SanitizeToolPairing(a.Session().Messages)
	for i, m := range repaired {
		if m.Role == provider.RoleTool {
			t.Fatalf("a cancelled turn left a dangling tool message at %d: %+v", i, m)
		}
	}
	last := repaired[len(repaired)-1]
	if last.Role != provider.RoleUser || last.Content != "do the thing" {
		t.Errorf("the pending user message should survive a cancel, got %+v", last)
	}
}

func TestRunRecoversInterruptedStreamAfterPartialText(t *testing.T) {
	interrupted := &provider.StreamInterruptedError{Err: errors.New("deepseek-flash: read stream: unexpected EOF")}
	mp := testutil.NewMock("m",
		testutil.Turn{Text: "partial ", ChunkError: interrupted},
		testutil.Turn{Text: "continued"},
	)
	sink := &recordSink{}
	a := New(mp, echoRegistry(), NewSession(""), Options{}, sink)

	if err := a.Run(context.Background(), "go"); err != nil {
		t.Fatalf("Run should recover the interrupted stream, got %v", err)
	}
	if mp.CallCount() != 2 {
		t.Fatalf("provider calls = %d, want 2", mp.CallCount())
	}

	reqs := mp.Requests()
	if len(reqs) != 2 {
		t.Fatalf("recorded requests = %d, want 2", len(reqs))
	}
	second := reqs[1].Messages
	if len(second) < 3 {
		t.Fatalf("second request should include partial assistant and recovery prompt: %+v", second)
	}
	if second[len(second)-2].Role != provider.RoleAssistant || second[len(second)-2].Content != "partial " {
		t.Fatalf("partial assistant was not preserved before recovery: %+v", second)
	}
	if second[len(second)-1].Role != provider.RoleUser || !strings.Contains(second[len(second)-1].Content, "Do not repeat") {
		t.Fatalf("recovery prompt missing duplicate guard: %+v", second[len(second)-1])
	}

	var streamed strings.Builder
	for _, e := range sink.kinds(event.Text) {
		streamed.WriteString(e.Text)
	}
	if streamed.String() != "partial continued" {
		t.Fatalf("streamed text = %q, want %q", streamed.String(), "partial continued")
	}
	retries := sink.kinds(event.Retrying)
	if len(retries) != 1 || retries[0].RetryAttempt != 1 || retries[0].RetryMax != maxStreamRecoveries {
		t.Fatalf("retry events = %+v, want one stream recovery retry", retries)
	}
}

func TestRunRecoversRepeatedInterruptedStreams(t *testing.T) {
	interrupted := &provider.StreamInterruptedError{Err: errors.New("deepseek-flash: read stream: unexpected EOF")}
	mp := testutil.NewMock("m",
		testutil.Turn{Text: "first ", ChunkError: interrupted},
		testutil.Turn{Text: "second ", ChunkError: interrupted},
		testutil.Turn{Text: "done"},
	)
	sink := &recordSink{}
	a := New(mp, echoRegistry(), NewSession(""), Options{}, sink)

	if err := a.Run(context.Background(), "go"); err != nil {
		t.Fatalf("Run should recover repeated interrupted streams, got %v", err)
	}
	if mp.CallCount() != 3 {
		t.Fatalf("provider calls = %d, want 3", mp.CallCount())
	}

	var streamed strings.Builder
	for _, e := range sink.kinds(event.Text) {
		streamed.WriteString(e.Text)
	}
	if streamed.String() != "first second done" {
		t.Fatalf("streamed text = %q, want repeated partials plus final text", streamed.String())
	}
	retries := sink.kinds(event.Retrying)
	if len(retries) != 2 || retries[0].RetryAttempt != 1 || retries[1].RetryAttempt != 2 {
		t.Fatalf("retry events = %+v, want attempts 1 and 2", retries)
	}
	for _, retry := range retries {
		if retry.RetryMax != maxStreamRecoveries {
			t.Fatalf("retry max = %d, want %d", retry.RetryMax, maxStreamRecoveries)
		}
	}
}

func TestRunRecoversInterruptedPartialToolCallWithoutExecutingIt(t *testing.T) {
	interrupted := &provider.StreamInterruptedError{Err: errors.New("deepseek-flash: read stream: unexpected EOF")}
	mp := testutil.NewMock("m",
		testutil.Turn{Chunks: []provider.Chunk{
			{Type: provider.ChunkToolCallStart, ToolCall: &provider.ToolCall{ID: "c1", Name: "echo"}},
			{Type: provider.ChunkError, Err: interrupted},
		}},
		testutil.Turn{Text: "recovered"},
	)
	a := New(mp, echoRegistry(), NewSession(""), Options{}, event.Discard)

	if err := a.Run(context.Background(), "go"); err != nil {
		t.Fatalf("Run should recover the interrupted tool-call stream, got %v", err)
	}

	for _, m := range a.Session().Messages {
		if m.Role == provider.RoleTool {
			t.Fatalf("partial tool call should not have executed or produced a tool result: %+v", m)
		}
	}
	reqs := mp.Requests()
	second := reqs[1].Messages
	last := second[len(second)-1]
	if last.Role != provider.RoleUser || !strings.Contains(last.Content, "fresh complete tool call") {
		t.Fatalf("partial-tool recovery prompt missing fresh-call instruction: %+v", last)
	}
}

// TestRunWellFormedToolLoopRoundTrips is the happy-path baseline: a tool round
// then a final answer. The session must end with the assistant answer and pair
// cleanly (the repair is a no-op on well-formed histories).
func TestRunWellFormedToolLoopRoundTrips(t *testing.T) {
	mp := testutil.NewMock("m",
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "c1", Name: "echo", Arguments: `{"text":"hi"}`}}},
		testutil.Turn{Text: "all set"},
	)
	a := New(mp, echoRegistry(), NewSession(""), Options{}, event.Discard)
	if err := a.Run(context.Background(), "go"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	msgs := a.Session().Messages
	last := msgs[len(msgs)-1]
	if last.Role != provider.RoleAssistant || last.Content != "all set" {
		t.Fatalf("final message should be the assistant answer, got %+v", last)
	}
	before := len(msgs)
	if after := len(provider.SanitizeToolPairing(msgs)); after != before {
		t.Errorf("repair mutated a well-formed session: %d -> %d", before, after)
	}
}

// TestRunWarnsAndContinuesOnMissingToolCallReasoning: a DeepSeek thinking-mode
// tool_calls turn arriving without reasoning is a quality degradation, not a
// failure — the turn is saved, the loop continues to completion, and the user
// sees a single warn notice. Missing reasoning tends to repeat on every round
// once it starts (endpoint-conditional behavior, seen on the official API too),
// so later rounds with the same shape must stay silent instead of flooding the
// transcript (#6259). The wire layer keeps the replay valid by always
// serializing the reasoning_content key on such turns.
func TestRunWarnsAndContinuesOnMissingToolCallReasoning(t *testing.T) {
	mp := testutil.NewMock("deepseek-proxy",
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "c1", Name: "echo", Arguments: `{"text":"hi"}`}}},
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "c2", Name: "echo", Arguments: `{"text":"again"}`}}},
		testutil.Turn{Text: "done"},
	)
	sink := &recordSink{}
	a := New(toolCallReasoningRequiredProvider{mp}, echoRegistry(), NewSession(""), Options{}, sink)

	if err := a.Run(context.Background(), "go"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	var savedToolTurns int
	for _, m := range a.Session().Messages {
		if m.Role == provider.RoleAssistant && len(m.ToolCalls) > 0 {
			savedToolTurns++
		}
	}
	if savedToolTurns != 2 {
		t.Fatalf("tool-call turns saved = %d, want 2 despite missing reasoning, session=%+v", savedToolTurns, a.Session().Messages)
	}
	var warns int
	for _, e := range sink.kinds(event.Notice) {
		if e.Level == event.LevelWarn && strings.Contains(e.Text, "without reasoning_content") {
			warns++
		}
	}
	if warns != 1 {
		t.Fatalf("missing-reasoning warn notices = %d, want exactly 1 (first round warns, repeats stay silent)", warns)
	}
}

// TestSetSessionRearmsMissingToolCallReasoningWarn: the once-per-session dedupe
// is scoped to the conversation — swapping in a different session (resume/new)
// must re-arm the notice so the fresh conversation still gets its one warning.
func TestSetSessionRearmsMissingToolCallReasoningWarn(t *testing.T) {
	mp := testutil.NewMock("deepseek-proxy",
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "c1", Name: "echo", Arguments: `{"text":"hi"}`}}},
		testutil.Turn{Text: "done"},
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "c2", Name: "echo", Arguments: `{"text":"hi"}`}}},
		testutil.Turn{Text: "done again"},
	)
	sink := &recordSink{}
	a := New(toolCallReasoningRequiredProvider{mp}, echoRegistry(), NewSession(""), Options{}, sink)

	if err := a.Run(context.Background(), "go"); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	a.SetSession(NewSession(""))
	if err := a.Run(context.Background(), "go"); err != nil {
		t.Fatalf("second Run: %v", err)
	}
	var warns int
	for _, e := range sink.kinds(event.Notice) {
		if e.Level == event.LevelWarn && strings.Contains(e.Text, "without reasoning_content") {
			warns++
		}
	}
	if warns != 2 {
		t.Fatalf("warn notices across two sessions = %d, want 2 (SetSession re-arms the dedupe)", warns)
	}
}

func TestRunPreservesOriginalRequiredToolCallReasoningAcrossHook(t *testing.T) {
	mp := testutil.NewMock("deepseek-proxy",
		testutil.Turn{
			Reasoning: "original reasoning",
			ToolCalls: []provider.ToolCall{{
				ID: "c1", Name: "echo", Arguments: `{"text":"hi"}`,
			}},
		},
		testutil.Turn{Text: "done"},
	)
	h := &stubHooks{hasPostLLM: true, postLLMOut: "translated display"}
	a := New(toolCallReasoningRequiredProvider{mp}, echoRegistry(), NewSession(""), Options{Hooks: h}, event.Discard)

	if err := a.Run(context.Background(), "go"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	reqs := mp.Requests()
	if len(reqs) != 2 {
		t.Fatalf("provider calls = %d, want 2", len(reqs))
	}
	var toolCallAssistant provider.Message
	for _, m := range reqs[1].Messages {
		if m.Role == provider.RoleAssistant && len(m.ToolCalls) > 0 {
			toolCallAssistant = m
			break
		}
	}
	if toolCallAssistant.ReasoningContent != "original reasoning" {
		t.Fatalf("tool-call reasoning = %q, want original provider reasoning", toolCallAssistant.ReasoningContent)
	}
	if toolCallAssistant.ReasoningContent == "translated display" {
		t.Fatal("translated display text leaked into provider-visible tool-call reasoning")
	}
}

// TestClassifierSkipsMemoryV5ForChat 验证聊天输入跳过 Memory v5
func TestClassifierSkipsMemoryV5ForChat(t *testing.T) {
	rt := memorycompiler.New(t.TempDir())
	mp := testutil.NewMock("m", testutil.Turn{Text: "hello back"})
	var stats []event.MemoryCompilerStats
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.MemoryCompilerStatsEvent && e.MemoryCompiler != nil {
			stats = append(stats, *e.MemoryCompiler)
		}
	})
	// 使用默认启发式分类器（UseMemoryCompilerLLMClassification = false）
	a := New(mp, echoRegistry(), NewSession(""), Options{MemoryCompiler: rt}, sink)

	// 发送聊天输入
	if err := a.Run(context.Background(), "hello"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// 验证 Memory v5 完全没有启动
	if len(stats) != 0 {
		t.Fatalf("chat input should completely skip Memory v5, got %d stats events", len(stats))
	}

	// 验证用户输入未被修改
	reqs := mp.Requests()
	if len(reqs) != 1 {
		t.Fatalf("requests = %d, want 1", len(reqs))
	}
	user := lastUserMessageFromRequest(t, reqs[0])
	if user.Content != "hello" {
		t.Errorf("chat input should be unchanged, got: %s", user.Content)
	}
}

// TestClassifierUsesMemoryV5ForTask 验证任务输入使用 Memory v5
func TestClassifierUsesMemoryV5ForTask(t *testing.T) {
	rt := memorycompiler.New(t.TempDir())
	// 预先种入一些记忆让 Memory v5 有内容可编译
	_, seed := rt.StartTurn(context.Background(), "fix a bug", nil)
	seed.RecordToolResults([]memorycompiler.ToolRecord{
		{Name: "bash", Output: "test passed"},
	})
	seed.Finish(nil)

	mp := testutil.NewMock("m", testutil.Turn{Text: "fixed"})
	var stats []event.MemoryCompilerStats
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.MemoryCompilerStatsEvent && e.MemoryCompiler != nil {
			stats = append(stats, *e.MemoryCompiler)
		}
	})
	a := New(mp, echoRegistry(), NewSession(""), Options{
		MemoryCompiler:          rt,
		MemoryCompilerVerbosity: MemoryCompilerVerbosityCompact,
	}, sink)

	// 发送没有命令式动词但明显需要处理的问题描述
	if err := a.Run(context.Background(), "the auth isn't working"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// 验证 Memory v5 启动了
	if len(stats) != 1 {
		t.Fatalf("task input should start Memory v5, got %d stats events", len(stats))
	}

	// 验证 stats 正常
	if !stats[0].UsefulIR {
		t.Errorf("task should produce useful IR: %+v", stats[0])
	}
	reqs := mp.Requests()
	if len(reqs) != 1 {
		t.Fatalf("requests = %d, want 1", len(reqs))
	}
	user := lastUserMessageFromRequest(t, reqs[0])
	if !strings.Contains(user.Content, "<memory-compiler-execution>") {
		t.Fatalf("task input was not replaced by Memory v5 contract:\n%s", user.Content)
	}
}

type fixedTaskClassifier struct {
	isTask bool
}

func (f fixedTaskClassifier) IsTask(context.Context, string) (bool, error) {
	return f.isTask, nil
}

func TestTaskClassifierResultControlsMemoryV5Injection(t *testing.T) {
	rt := memorycompiler.New(t.TempDir())
	_, seed := rt.StartTurn(context.Background(), "fix a bug", nil)
	seed.RecordToolResults([]memorycompiler.ToolRecord{
		{Name: "bash", Output: "test passed"},
	})
	seed.Finish(nil)

	mp := testutil.NewMock("m", testutil.Turn{Text: "done"})
	a := New(mp, echoRegistry(), NewSession(""), Options{
		MemoryCompiler:          rt,
		MemoryCompilerVerbosity: MemoryCompilerVerbosityCompact,
	}, event.Discard)
	a.classifier = fixedTaskClassifier{isTask: true}

	if err := a.Run(context.Background(), "please look into this"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	reqs := mp.Requests()
	if len(reqs) != 1 {
		t.Fatalf("requests = %d, want 1", len(reqs))
	}
	user := lastUserMessageFromRequest(t, reqs[0])
	if !strings.Contains(user.Content, "<memory-compiler-execution>") {
		t.Fatalf("task classifier result did not allow Memory v5 injection:\n%s", user.Content)
	}
}
