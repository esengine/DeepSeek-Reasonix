package agent

import (
	"context"
	"encoding/json"
	"errors"
	"reasonix/internal/state/sessionstore"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
)

type sharedWindowTestProvider struct {
	budget int
	shared bool
	policy provider.SharedWindowInputPolicy
	last   provider.Request
	calls  int
	finish string
}

func (*sharedWindowTestProvider) Name() string { return "shared-window-test" }

func (p *sharedWindowTestProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.last = req
	p.calls++
	ch := make(chan provider.Chunk, 3)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: "## Summary\nsummary"}
	if p.finish != "" {
		ch <- provider.Chunk{Type: provider.ChunkUsage, Usage: &provider.Usage{FinishReason: p.finish}}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

// Before any usage calibrates the session, the estimate compared against the
// context window must still be tokens. It used to be characters, which reads
// 3-4x high and compacted long before the configured ratio.
func TestEstimatedPromptTokensStayInTokenUnitBeforeCalibration(t *testing.T) {
	a := &Agent{agentConfig: agentConfig{contextWindow: 1_000_000}}
	cases := []struct {
		name           string
		text           string
		realish, upper int
	}{
		// DeepSeek bills Chinese near 0.6 tokens per han rune, English near 0.25
		// per character. The cold estimate may be conservative, never 3x.
		{"chinese", strings.Repeat("上下文压缩策略", 8_000), 33_600, 50_000},
		{"english", strings.Repeat("compact the context window ", 8_000), 54_000, 70_000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := a.window().estimatedPromptTokens([]provider.Message{{Role: provider.RoleUser, Content: tc.text}})
			if got < tc.realish/2 || got > tc.upper {
				t.Fatalf("cold estimate = %d tokens, want between %d and %d (real ~%d)",
					got, tc.realish/2, tc.upper, tc.realish)
			}
		})
	}
}

// Desktop rebinds sessions constantly — tab switches, forks, and the snapshot
// conflict path that adopts the newer disk transcript. Each rebind used to drop
// the calibration and put the next turn back on the cold estimate.
func TestSessionSwapKeepsPromptCalibration(t *testing.T) {
	a := &Agent{agentConfig: agentConfig{contextWindow: 200_000}}
	msgs := []provider.Message{{Role: provider.RoleUser, Content: strings.Repeat("字", 60_000)}}
	a.window().setPromptTokenCalibration(36_000, requestCalibrationShapeOf(provider.Request{Messages: msgs}))

	before := a.window().estimatedPromptTokens(msgs)
	a.SetSession(sessionstore.NewSession("system"))
	after := a.window().estimatedPromptTokens(msgs)

	if before != after {
		t.Fatalf("estimate moved across a session swap: %d -> %d", before, after)
	}
	if after > 40_000 {
		t.Fatalf("estimate = %d, want the calibrated ~36000 rather than a cold fallback", after)
	}
}

func (p *sharedWindowTestProvider) OutputBudget() int         { return p.budget }
func (p *sharedWindowTestProvider) SharesContextWindow() bool { return p.shared }

func (p *sharedWindowTestProvider) SharedWindowInputPolicy() provider.SharedWindowInputPolicy {
	return p.policy
}

func TestEffectiveOutputBudgetClipsSharedWindowRequest(t *testing.T) {
	prov := &sharedWindowTestProvider{budget: 128 * 1024, shared: true}
	a := &Agent{agentConfig: agentConfig{contextWindow: 1_048_576}, svc: agentServices{prov: prov}, sess: sessionRuntime{output: outputBudgetState{outputBudget: prov.budget}}}
	msgs := []provider.Message{{Role: provider.RoleUser, Content: strings.Repeat("字", 950_000)}}
	// Calibrate this session at one token per rune. The 950K prompt fits, but
	// not beside the provider's full 128K output default.
	a.sess.output.lastUsage.Store(&provider.Usage{PromptTokens: 950_000})
	a.window().setPromptTokenCalibration(950_000, requestCalibrationShapeOf(provider.Request{Messages: msgs}))

	got, clipped, err := a.window().effectiveOutputBudget(provider.Request{Messages: msgs})
	if err != nil {
		t.Fatalf("effectiveOutputBudget: %v", err)
	}
	if !clipped {
		t.Fatal("near-window request kept the provider's full output budget")
	}
	if got <= 0 || got >= prov.budget {
		t.Fatalf("clipped budget = %d, want 0 < budget < %d", got, prov.budget)
	}
	if got+950_000 > a.contextWindow-outputBudgetReserve {
		t.Fatalf("input + output = %d, exceeds reserved shared window %d", got+950_000, a.contextWindow-outputBudgetReserve)
	}
}

func TestCalibratedOutputBudgetIncludesReplayedReasoning(t *testing.T) {
	prov := &sharedWindowTestProvider{budget: 128 * 1024, shared: true}
	a := &Agent{agentConfig: agentConfig{contextWindow: 200_000}, svc: agentServices{prov: prov}, sess: sessionRuntime{output: outputBudgetState{outputBudget: prov.budget}}}
	previous := []provider.Message{{Role: provider.RoleUser, Content: strings.Repeat("x", 300_000)}}
	a.window().setPromptTokenCalibration(75_000, requestCalibrationShapeOf(provider.Request{Messages: previous}))
	current := append(previous, provider.Message{
		Role:             provider.RoleAssistant,
		ReasoningContent: strings.Repeat("r", 400_000),
		ToolCalls:        []provider.ToolCall{{ID: "call_1", Name: "bash", Arguments: `{}`}},
	})

	before := a.window().estimatedPromptTokens(previous)
	after := a.window().estimatedPromptTokens(current)
	if after < before+99_000 {
		t.Fatalf("400K replayed reasoning was not calibrated: before=%d after=%d", before, after)
	}
	budget, clipped, err := a.window().effectiveOutputBudget(provider.Request{Messages: current})
	if err != nil {
		t.Fatalf("effectiveOutputBudget: %v", err)
	}
	if !clipped || budget > 20_000 {
		t.Fatalf("replayed reasoning budget = %d clipped=%v, want a clipped budget <= 20000", budget, clipped)
	}
}

func TestCalibratedOutputBudgetKeepsCJKConservativeFloor(t *testing.T) {
	prov := &sharedWindowTestProvider{budget: 128 * 1024, shared: true}
	a := &Agent{agentConfig: agentConfig{contextWindow: 1_048_576}, svc: agentServices{prov: prov}, sess: sessionRuntime{output: outputBudgetState{outputBudget: prov.budget}}}
	previous := []provider.Message{{Role: provider.RoleUser, Content: strings.Repeat("x", 300_000)}}
	a.window().setPromptTokenCalibration(75_000, requestCalibrationShapeOf(provider.Request{Messages: previous}))
	// Enough unrepresented CJK that the reply no longer fits beside it: at the
	// corrected unit 430K runes leave most of a 1M window free.
	current := append(append([]provider.Message(nil), previous...), provider.Message{
		Role:             provider.RoleAssistant,
		ReasoningContent: strings.Repeat("字", 1_200_000),
		ToolCalls:        []provider.ToolCall{{ID: "call_1", Name: "bash", Arguments: `{}`}},
	})

	// The unrepresented CJK runes are priced at the cold rate: 3 bytes each at
	// ~4 chars per token, i.e. 0.75 tokens per rune against a real ~0.6.
	calibrated := a.window().estimatedPromptTokens(current)
	wantFloor := 75_000 + 1_200_000*3/4
	if calibrated < wantFloor {
		t.Fatalf("calibrated estimate %d fell below mixed-script safety floor %d", calibrated, wantFloor)
	}

	budget, clipped, err := a.window().effectiveOutputBudget(provider.Request{Messages: current})
	if err != nil {
		t.Fatalf("effectiveOutputBudget: %v", err)
	}
	if !clipped || budget <= 0 || budget >= prov.budget {
		t.Fatalf("mixed-script request budget = %d clipped=%v, want a clipped positive budget below %d", budget, clipped, prov.budget)
	}
}

func TestCalibrationIgnoresNonReplayableOrdinaryReasoning(t *testing.T) {
	a := &Agent{}
	previous := []provider.Message{
		{Role: provider.RoleUser, Content: strings.Repeat("x", 300_000)},
		{Role: provider.RoleAssistant, ReasoningContent: strings.Repeat("hidden", 150_000)},
	}
	a.window().setPromptTokenCalibration(75_000, requestCalibrationShapeOf(provider.Request{Messages: previous}))
	current := append(append([]provider.Message(nil), previous...), provider.Message{
		Role:             provider.RoleAssistant,
		ReasoningContent: strings.Repeat("r", 400_000),
		ToolCalls:        []provider.ToolCall{{ID: "call_1", Name: "bash", Arguments: `{}`}},
	})

	if got := a.window().estimatedPromptTokens(current); got < 160_000 {
		t.Fatalf("replayable reasoning estimate = %d, want ordinary local reasoning excluded from calibration denominator", got)
	}
}

func TestCalibratedResponsesBudgetIncludesNewOrdinaryReasoning(t *testing.T) {
	prov := &sharedWindowTestProvider{budget: 128 * 1024, shared: true,
		policy: provider.SharedWindowInputPolicy{ReplaysOrdinaryReasoning: true}}
	a := &Agent{agentConfig: agentConfig{contextWindow: 200_000}, svc: agentServices{prov: prov}, sess: sessionRuntime{output: outputBudgetState{outputBudget: prov.budget}}}
	previous := provider.Request{Messages: []provider.Message{{
		Role: provider.RoleUser, Content: strings.Repeat("x", 300_000),
	}}}
	a.window().setPromptTokenCalibration(75_000, a.window().requestCalibrationShape(previous))
	current := previous
	current.Messages = append(append([]provider.Message(nil), previous.Messages...), provider.Message{
		Role: provider.RoleAssistant, ReasoningContent: strings.Repeat("r", 400_000),
	})

	if got := a.window().estimatedRequestTokens(current); got < 174_000 {
		t.Fatalf("Responses ordinary reasoning estimate = %d, want newly replayed reasoning included", got)
	}
	if budget, clipped, err := a.window().effectiveOutputBudget(current); err != nil || !clipped || budget >= prov.budget {
		t.Fatalf("Responses ordinary reasoning budget = %d clipped=%v err=%v, want a clipped budget", budget, clipped, err)
	}
}

func TestCalibratedResponsesBudgetIncludesNewReplayItems(t *testing.T) {
	prov := &sharedWindowTestProvider{budget: 128 * 1024, shared: true,
		policy: provider.SharedWindowInputPolicy{ReplaysResponsesItems: true}}
	a := &Agent{agentConfig: agentConfig{contextWindow: 200_000}, svc: agentServices{prov: prov}, sess: sessionRuntime{output: outputBudgetState{outputBudget: prov.budget}}}
	previous := provider.Request{Messages: []provider.Message{{
		Role: provider.RoleUser, Content: strings.Repeat("x", 300_000),
	}}}
	a.window().setPromptTokenCalibration(75_000, a.window().requestCalibrationShape(previous))
	item := json.RawMessage(`{"id":"ws_1","type":"web_search_call","status":"completed","action":{"query":"` + strings.Repeat("q", 400_000) + `"}}`)
	current := previous
	current.Messages = append(append([]provider.Message(nil), previous.Messages...), provider.Message{
		Role: provider.RoleAssistant, ResponsesItems: []json.RawMessage{item},
	})

	if got := a.window().estimatedRequestTokens(current); got < 174_000 {
		t.Fatalf("Responses replay-item estimate = %d, want newly replayed item included", got)
	}
	if budget, clipped, err := a.window().effectiveOutputBudget(current); err != nil || !clipped || budget >= prov.budget {
		t.Fatalf("Responses replay-item budget = %d clipped=%v err=%v, want a clipped budget", budget, clipped, err)
	}
}

func TestCalibratedOutputBudgetCountsToolSchemasOnce(t *testing.T) {
	a := &Agent{}
	req := provider.Request{
		Messages: []provider.Message{{Role: provider.RoleUser, Content: strings.Repeat("x", 100_000)}},
		Tools: []provider.ToolSchema{{
			Name: "lookup", Description: strings.Repeat("y", 100_000), Parameters: []byte(`{"type":"object"}`),
		}},
	}
	a.window().setPromptTokenCalibration(60_000, requestCalibrationShapeOf(req))

	if got := a.window().estimatedRequestTokens(req); got != 60_000 {
		t.Fatalf("calibrated request tokens = %d, want tool schema counted once in 60000", got)
	}
}

func TestPrepareSamplingRequestClipsSharedWindowOutput(t *testing.T) {
	prov := &sharedWindowTestProvider{budget: 128 * 1024, shared: true}
	msgs := []provider.Message{{Role: provider.RoleUser, Content: strings.Repeat("字", 950_000)}}
	sess := sessionstore.NewSession("")
	sess.Replace(msgs)
	// compactRatio 2 disables auto maintenance for this output-clip test
	a := &Agent{agentConfig: agentConfig{contextWindow: 1_048_576, compactRatio: 2}, svc: agentServices{prov: prov, tools: tool.NewRegistry()},
		sess: sessionRuntime{conversation: sess, output: outputBudgetState{outputBudget: prov.budget}}}
	a.sess.output.lastUsage.Store(&provider.Usage{PromptTokens: 950_000})
	a.window().setPromptTokenCalibration(950_000, requestCalibrationShapeOf(provider.Request{Messages: msgs}))

	prepared, err := a.prepareSamplingRequest(context.Background())
	if err != nil {
		t.Fatalf("prepareSamplingRequest: %v", err)
	}
	if prepared.req.MaxTokens <= 0 || prepared.req.MaxTokens >= prov.budget {
		t.Fatalf("prepared MaxTokens = %d, want a clipped positive budget below %d", prepared.req.MaxTokens, prov.budget)
	}
}

func TestEffectiveOutputBudgetRejectsExhaustedSharedWindow(t *testing.T) {
	prov := &sharedWindowTestProvider{budget: 128 * 1024, shared: true}
	a := &Agent{agentConfig: agentConfig{contextWindow: 1_048_576}, svc: agentServices{prov: prov}, sess: sessionRuntime{output: outputBudgetState{outputBudget: prov.budget}}}
	msgs := []provider.Message{{Role: provider.RoleUser, Content: strings.Repeat("字", 1_045_000)}}
	a.sess.output.lastUsage.Store(&provider.Usage{PromptTokens: 1_045_000})
	a.window().setPromptTokenCalibration(1_045_000, requestCalibrationShapeOf(provider.Request{Messages: msgs}))

	_, _, err := a.window().effectiveOutputBudget(provider.Request{Messages: msgs})
	if !errors.Is(err, ErrCompactionRequired) {
		t.Fatalf("effectiveOutputBudget error = %v, want ErrCompactionRequired", err)
	}
}

func TestEffectiveOutputBudgetLeavesIndependentProviderUnchanged(t *testing.T) {
	prov := &sharedWindowTestProvider{budget: 128 * 1024, shared: false}
	a := &Agent{agentConfig: agentConfig{contextWindow: 1_048_576}, svc: agentServices{prov: prov}, sess: sessionRuntime{output: outputBudgetState{outputBudget: prov.budget}}}
	got, clipped, err := a.window().effectiveOutputBudget(provider.Request{
		Messages: []provider.Message{{Role: provider.RoleUser, Content: strings.Repeat("字", 950_000)}},
	})
	if err != nil || clipped || got != 0 {
		t.Fatalf("independent provider changed: budget=%d clipped=%v err=%v", got, clipped, err)
	}
}

func TestEffectiveOutputBudgetHonorsExplicitOmit(t *testing.T) {
	prov := &sharedWindowTestProvider{budget: 128 * 1024, shared: true}
	a := &Agent{agentConfig: agentConfig{contextWindow: 1_048_576}, svc: agentServices{prov: prov}, sess: sessionRuntime{output: outputBudgetState{outputBudget: prov.budget}}}
	got, clipped, err := a.window().effectiveOutputBudget(provider.Request{
		Messages:  []provider.Message{{Role: provider.RoleUser, Content: strings.Repeat("字", 950_000)}},
		MaxTokens: -1,
	})
	if err != nil || clipped || got != 0 {
		t.Fatalf("explicit omit changed: budget=%d clipped=%v err=%v", got, clipped, err)
	}
}

func TestSetSessionResetsPerTranscriptUsageState(t *testing.T) {
	a := &Agent{}
	a.sess.output.lastUsage.Store(&provider.Usage{PromptTokens: 200_000})
	active := requestCalibrationShape{requestChars: 900_000, compactChars: 850_000}
	a.sess.output.activeReqShape.Store(&active)
	a.window().setPromptTokenCalibration(200_000, requestCalibrationShape{requestChars: 1_000_000, compactChars: 950_000})
	a.SetSession(sessionstore.NewSession("new"))

	if got := a.sess.output.lastUsage.Load(); got != nil {
		t.Fatalf("lastUsage survived session switch: %+v", got)
	}
	if got := a.sess.output.activeReqShape.Load(); got != nil {
		t.Fatalf("activeReqShape survived session switch: %+v", got)
	}
	if got := a.sess.output.promptCalibration.Load(); got == nil {
		t.Fatal("promptCalibration was dropped on session switch; the tokenizer ratio outlives the transcript")
	}
}

func TestLatestUsagePairsWithActiveRequestSize(t *testing.T) {
	a := &Agent{}
	active := requestCalibrationShape{requestChars: 222, compactChars: 111, cjkRunes: 22, cjkBytes: 66}
	a.sess.output.activeReqShape.Store(&active)
	a.storeLatestRequestUsage(&provider.Usage{PromptTokens: 100})

	if got := a.sess.output.promptCalibration.Load(); got == nil || got.promptTokens != 100 || got.requestChars != 222 || got.compactChars != 111 || got.cjkRunes != 22 || got.cjkBytes != 66 {
		t.Fatalf("promptCalibration = %+v, want promptTokens=100 requestChars=222 compactChars=111 cjkRunes=22 cjkBytes=66", got)
	}
}

func TestEstimatedUsageDoesNotReplacePromptCalibration(t *testing.T) {
	a := &Agent{}
	active := requestCalibrationShape{requestChars: 200_000, compactChars: 100_000}
	a.sess.output.activeReqShape.Store(&active)
	a.window().setPromptTokenCalibration(50_000, requestCalibrationShape{requestChars: 100_000, compactChars: 80_000})

	a.storeLatestRequestUsage(&provider.Usage{
		PromptTokens: 10_000,
		TotalTokens:  10_100,
		Estimated:    true,
	})

	got := a.sess.output.promptCalibration.Load()
	if got == nil || got.promptTokens != 50_000 || got.requestChars != 100_000 || got.compactChars != 80_000 {
		t.Fatalf("estimated usage replaced provider calibration: %+v", got)
	}
	if latest := a.sess.output.lastUsage.Load(); latest == nil || !latest.Estimated {
		t.Fatalf("estimated usage was not retained for accounting: %+v", latest)
	}
}

func TestForkCaptureProviderPreservesOutputBudgetCapabilities(t *testing.T) {
	t.Setenv("REASONIX_EXPERIMENT_FORK_CAPTURE_DIR", testenv.TempDir(t))
	prov := &sharedWindowTestProvider{budget: 128 * 1024, shared: true,
		policy: provider.SharedWindowInputPolicy{ReplaysOrdinaryReasoning: true, ReplaysResponsesItems: true}}
	a := New(prov, tool.NewRegistry(), sessionstore.NewSession(""), Options{}, event.Discard)

	if !sharesContextWindow(a.svc.prov) {
		t.Fatal("fork capture wrapper erased shared-window output capability")
	}
	if got := outputBudgetOf(a.svc.prov); got != prov.budget {
		t.Fatalf("wrapped output budget = %d, want %d", got, prov.budget)
	}
	if got := sharedWindowInputPolicyOf(a.svc.prov); got != prov.policy {
		t.Fatalf("wrapped input policy = %+v, want %+v", got, prov.policy)
	}
}

func TestOutputBudgetKeepsAWindowProportionalMarginOnALargeWindow(t *testing.T) {
	prov := &sharedWindowTestProvider{budget: 65_536, shared: true}
	window := 1_048_576
	a := &Agent{agentConfig: agentConfig{contextWindow: window}, svc: agentServices{prov: prov}, sess: sessionRuntime{output: outputBudgetState{outputBudget: prov.budget}}}
	msgs := []provider.Message{{Role: provider.RoleUser, Content: strings.Repeat("字", 975_000)}}
	a.sess.output.lastUsage.Store(&provider.Usage{PromptTokens: 975_000})
	a.window().setPromptTokenCalibration(975_000, requestCalibrationShapeOf(provider.Request{Messages: msgs}))

	got, clipped, err := a.window().effectiveOutputBudget(provider.Request{Messages: msgs})
	if err != nil || !clipped {
		t.Fatalf("effectiveOutputBudget = %d clipped=%v err=%v, want a clipped budget", got, clipped, err)
	}
	// The real count was 1% above the estimate in the report this pins: input
	// and output must still fit the window with that much error.
	if actual := 975_000 + 975_000/100; actual+got > window {
		t.Fatalf("a prompt 1%% above its estimate plus the %d-token budget is %d, past the %d window", got, actual+got, window)
	}
}

// The prompt size at which the send-time output check refuses a request is the
// size at which a fold must free room. The fold's hard ceiling used to sit above
// it, so a prompt in between was neither folded nor sendable, and the person
// saw the refusal beside a fold that declined for being worth too little.
func TestTheFoldCeilingStopsWhereTheSendTimeOutputCheckStops(t *testing.T) {
	prov := &sharedWindowTestProvider{budget: 8 * 1024, shared: true}
	a := &Agent{agentConfig: agentConfig{contextWindow: 128_000}, svc: agentServices{prov: prov, sink: event.Discard}, sess: sessionRuntime{output: outputBudgetState{outputBudget: prov.budget}}}
	w := a.window()
	ceiling := w.hardInputCeiling()
	atCeiling := provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: strings.Repeat("x", 4)}}}
	if _, _, err := w.effectiveOutputBudget(atCeiling); err != nil {
		t.Fatalf("a tiny prompt is refused: %v", err)
	}
	if want := 128_000 - outputBudgetReserveFor(128_000); ceiling != want {
		t.Fatalf("hardInputCeiling = %d, want the window less the output reserve (%d)", ceiling, want)
	}
	prov.shared = false
	if got := w.hardInputCeiling(); got != 128_000-protocolReserveTokens {
		t.Fatalf("a provider whose reply does not share the window keeps the protocol reserve only, got %d", got)
	}
}
