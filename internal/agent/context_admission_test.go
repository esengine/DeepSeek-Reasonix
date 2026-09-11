package agent

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

type policyWindowProvider struct {
	sharedWindowTestProvider
	policy provider.ContextBudgetPolicy
}

func (p *policyWindowProvider) ContextBudgetPolicy() provider.ContextBudgetPolicy { return p.policy }

func TestAdmitOutputBudgetClipsIssue8909AndScreenshot(t *testing.T) {
	cases := []struct {
		name      string
		prompt    int
		requested int
		want      int
	}{
		{name: "issue8909", prompt: 810_882, requested: 354_469, want: 229_502},
		{name: "screenshot", prompt: 917_189, requested: 245_760, want: 123_195},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prov := &sharedWindowTestProvider{budget: tc.requested, shared: true}
			a := &Agent{
				agentConfig: agentConfig{contextWindow: 1_048_576},
				svc:         agentServices{prov: prov},
				sess:        sessionRuntime{output: outputBudgetState{outputBudget: tc.requested}},
			}
			msgs := []provider.Message{{Role: provider.RoleUser, Content: strings.Repeat("x", 3_000_000)}}
			a.setPromptTokenCalibration(tc.prompt, requestCalibrationShapeOf(provider.Request{Messages: msgs}))
			adm, err := a.admitOutputBudget(provider.Request{Messages: msgs, MaxTokens: tc.requested})
			if err != nil {
				t.Fatal(err)
			}
			if !adm.Clipped || adm.EffectiveOutputTokens != tc.want || adm.PhysicalRemaining != tc.want {
				t.Fatalf("adm=%+v, want clipped %d", adm, tc.want)
			}
		})
	}
}

func TestAdmitOutputBudgetUsesOfficialAutoWhenConfigIsZero(t *testing.T) {
	prov := &policyWindowProvider{sharedWindowTestProvider: sharedWindowTestProvider{shared: true}, policy: provider.ContextBudgetPolicy{
		WindowMode:       provider.ContextWindowShared,
		AutoOutputTokens: provider.DeepSeekMaxOutputTokens,
		MaxOutputTokens:  provider.DeepSeekMaxOutputTokens,
		LimitMode:        provider.OutputLimitOmitWhenSafe,
	}}
	a := &Agent{agentConfig: agentConfig{contextWindow: 1_048_576}, svc: agentServices{prov: prov}}
	msgs := []provider.Message{{Role: provider.RoleUser, Content: strings.Repeat("x", 3_000_000)}}
	a.setPromptTokenCalibration(810_882, requestCalibrationShapeOf(provider.Request{Messages: msgs}))
	req := provider.Request{Messages: msgs, MaxTokens: 0}
	if err := a.applyAdmissionToRequest(&req); err != nil {
		t.Fatal(err)
	}
	if req.MaxTokens != 229_502 {
		t.Fatalf("auto official clip = %d, want 229502", req.MaxTokens)
	}
}

func TestAdmitOutputBudgetOmitsWhenSafeHasRoom(t *testing.T) {
	prov := &policyWindowProvider{policy: provider.ContextBudgetPolicy{
		WindowMode:       provider.ContextWindowShared,
		AutoOutputTokens: provider.DeepSeekMaxOutputTokens,
		MaxOutputTokens:  provider.DeepSeekMaxOutputTokens,
		LimitMode:        provider.OutputLimitOmitWhenSafe,
	}}
	a := &Agent{agentConfig: agentConfig{contextWindow: 1_048_576}, svc: agentServices{prov: prov}}
	req := provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}}}
	if err := a.applyAdmissionToRequest(&req); err != nil {
		t.Fatal(err)
	}
	if req.MaxTokens != 0 {
		t.Fatalf("safe omit injected %d", req.MaxTokens)
	}
}

func TestAdmitOutputBudgetAlwaysSendsOpenCodeLimit(t *testing.T) {
	prov := &policyWindowProvider{policy: provider.ContextBudgetPolicy{
		WindowMode:       provider.ContextWindowShared,
		AutoOutputTokens: 131_072,
		MaxOutputTokens:  131_072,
		LimitMode:        provider.OutputLimitAlways,
	}}
	a := &Agent{agentConfig: agentConfig{contextWindow: 1_048_576}, svc: agentServices{prov: prov}}
	req := provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "hi"}}, MaxTokens: 0}
	if err := a.applyAdmissionToRequest(&req); err != nil {
		t.Fatal(err)
	}
	if req.MaxTokens != 131_072 {
		t.Fatalf("OpenCode always send = %d, want 131072", req.MaxTokens)
	}
}

func TestAdmitOutputBudgetNegativeOmitsInsteadOfInject(t *testing.T) {
	prov := &policyWindowProvider{policy: provider.ContextBudgetPolicy{
		WindowMode:       provider.ContextWindowShared,
		AutoOutputTokens: provider.DeepSeekMaxOutputTokens,
		LimitMode:        provider.OutputLimitOmitWhenSafe,
	}}
	a := &Agent{agentConfig: agentConfig{contextWindow: 1_048_576}, svc: agentServices{prov: prov}}
	msgs := []provider.Message{{Role: provider.RoleUser, Content: strings.Repeat("x", 3_000_000)}}
	a.setPromptTokenCalibration(810_882, requestCalibrationShapeOf(provider.Request{Messages: msgs}))
	_, err := a.admitOutputBudget(provider.Request{Messages: msgs, MaxTokens: -1})
	if !errors.Is(err, ErrCompactionRequired) {
		t.Fatalf("negative omit err = %v, want compaction", err)
	}
}

func TestCompactRatioIndependentOfOutputBudgetAndLearnedWindow(t *testing.T) {
	a := &Agent{
		svc:         agentServices{prov: &sharedWindowTestProvider{budget: 131_072, shared: true}},
		agentConfig: agentConfig{contextWindow: 128_000, compactRatio: defaultCompactRatio},
	}
	want := int(float64(128_000) * defaultCompactRatio)
	if got := a.compactTrigger(); got != want {
		t.Fatalf("trigger = %d, want %d", got, want)
	}
	a.sess.output.learned.Store(&learnedContextBudget{windowTokens: 64_000})
	if got := a.compactTrigger(); got != int(float64(64_000)*defaultCompactRatio) {
		t.Fatalf("learned trigger = %d", got)
	}
	a.sess.output.learned.Store(&learnedContextBudget{windowTokens: 64_000})
	if a.compactRatio != defaultCompactRatio {
		t.Fatal("compact_ratio must stay user-owned")
	}
}

func TestGuardedSummaryUsesSharedPolicyWhenAutoBudgetIsZero(t *testing.T) {
	prov := &policyWindowProvider{policy: provider.ContextBudgetPolicy{WindowMode: provider.ContextWindowShared, LimitMode: provider.OutputLimitOmitWhenSafe}}
	a := &Agent{agentConfig: agentConfig{contextWindow: 100_000}, svc: agentServices{prov: prov, sink: event.Discard}}
	fold := []provider.Message{{Role: provider.RoleUser, Content: strings.Repeat("字", 80_000)}}
	if budget := a.summaryInputBudget(""); budget <= 0 {
		t.Fatalf("shared auto-zero summary budget = %d", budget)
	}
	if got := a.guardedSummaryInputTokens(fold); got <= 0 {
		t.Fatalf("guarded tokens = %d", got)
	}
}

func TestLearnedWindowIsolatedPerAgent(t *testing.T) {
	first := &Agent{agentConfig: agentConfig{contextWindow: 1_048_576}}
	second := &Agent{agentConfig: agentConfig{contextWindow: 1_048_576}}
	first.learnContextBudget(200_000, 0, false)
	if first.effectiveContextWindow() != 200_000 {
		t.Fatalf("first window = %d", first.effectiveContextWindow())
	}
	if second.effectiveContextWindow() != 1_048_576 {
		t.Fatalf("second agent inherited learned window %d", second.effectiveContextWindow())
	}
}

func TestZeroConfigWindowUsesLearned(t *testing.T) {
	a := &Agent{}
	a.learnContextBudget(262_144, 0, false)
	if a.effectiveContextWindow() != 262_144 {
		t.Fatalf("zero config window = %d", a.effectiveContextWindow())
	}
}

func TestForkCaptureForwardsContextBudgetPolicy(t *testing.T) {
	t.Setenv("REASONIX_EXPERIMENT_FORK_CAPTURE_DIR", t.TempDir())
	inner := &policyWindowProvider{policy: provider.ContextBudgetPolicy{
		WindowMode: provider.ContextWindowShared, AutoOutputTokens: 384_000, LimitMode: provider.OutputLimitOmitWhenSafe,
	}}
	a := New(inner, tool.NewRegistry(), NewSession(""), Options{}, event.Discard)
	got := provider.ResolveContextBudgetPolicy(a.svc.prov)
	if got.AutoOutputTokens != 384_000 || got.WindowMode != provider.ContextWindowShared {
		t.Fatalf("wrapped policy = %+v", got)
	}
}

func TestFreezeProviderRequestOwnsNestedServerSearchData(t *testing.T) {
	req := provider.Request{Messages: []provider.Message{{
		Role: provider.RoleAssistant,
		ServerSearch: []provider.ServerSearchCall{{
			ID:      "search-1",
			Results: []provider.ServerSearchHit{{Title: "original", URL: "https://example.test/original"}},
			Raw:     json.RawMessage(`{"value":"original"}`),
		}},
	}}}
	frozen := freezeProviderRequest(req)
	req.Messages[0].ServerSearch[0].Results[0].Title = "mutated"
	req.Messages[0].ServerSearch[0].Raw[0] = '['

	got := frozen.Messages[0].ServerSearch[0]
	if got.Results[0].Title != "original" || string(got.Raw) != `{"value":"original"}` {
		t.Fatalf("frozen server search shared mutable data: %+v", got)
	}
}
