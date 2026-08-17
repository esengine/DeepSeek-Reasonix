package openai

import (
	"testing"

	"reasonix/internal/provider"
)

func TestSharedWindowOutputBudgetCapability(t *testing.T) {
	deepseek := &client{deepseek: true, maxOutputTokens: 32 * 1024}
	if !deepseek.SharesContextWindow() || deepseek.OutputBudget() != 32*1024 {
		t.Fatalf("DeepSeek capability = shared:%v budget:%d", deepseek.SharesContextWindow(), deepseek.OutputBudget())
	}
	openai := &client{maxOutputTokens: 32 * 1024}
	if openai.SharesContextWindow() {
		t.Fatal("ordinary OpenAI mode must keep its independent output ceiling")
	}
}

func TestOfficialDeepSeekPolicyOmitsWhenSafe(t *testing.T) {
	p, err := New(provider.Config{Name: "ds", BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-pro"})
	if err != nil {
		t.Fatal(err)
	}
	got := p.(provider.ContextBudgetPolicyProvider).ContextBudgetPolicy()
	if got.WindowMode != provider.ContextWindowShared || got.AutoOutputTokens != provider.DeepSeekMaxOutputTokens || got.LimitMode != provider.OutputLimitOmitWhenSafe {
		t.Fatalf("official DeepSeek policy = %+v", got)
	}
	req := p.(*client).buildRequest(provider.Request{})
	if req.MaxTokens != 0 {
		t.Fatalf("safe official DeepSeek should omit max_tokens, got %d", req.MaxTokens)
	}
	clipped := p.(*client).buildRequest(provider.Request{MaxTokens: 229_502})
	if clipped.MaxTokens != 229_502 {
		t.Fatalf("clipped official DeepSeek max_tokens = %d", clipped.MaxTokens)
	}
}

func TestOpenCodeGoChatPolicySendsGenericMaxTokens(t *testing.T) {
	p, err := New(provider.Config{Name: "og", BaseURL: "https://opencode.ai/zen/go/v1", Model: "kimi-k3"})
	if err != nil {
		t.Fatal(err)
	}
	got := p.(provider.ContextBudgetPolicyProvider).ContextBudgetPolicy()
	if got.WindowMode != provider.ContextWindowShared || got.MaxOutputTokens != 131_072 || got.LimitMode != provider.OutputLimitAlways {
		t.Fatalf("OpenCode Go kimi-k3 policy = %+v", got)
	}
	req := p.(*client).buildRequest(provider.Request{MaxTokens: 131_072})
	if req.MaxTokens != 131_072 || req.MaxCompletionTokens != 0 {
		t.Fatalf("OpenCode Go Kimi must keep generic max_tokens: %+v", req)
	}
}

func TestOfficialKimiK3KeepsMaxCompletionTokens(t *testing.T) {
	p, err := New(provider.Config{
		Name: "kimi", BaseURL: "https://api.moonshot.cn/v1", Model: "kimi-k3",
		Extra: map[string]any{"reasoning_protocol": "kimi-k3", "max_output_tokens": 131_072},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := p.(provider.ContextBudgetPolicyProvider).ContextBudgetPolicy()
	if got.WindowMode != provider.ContextWindowShared || got.AutoOutputTokens != 131_072 {
		t.Fatalf("official Kimi K3 policy = %+v", got)
	}
	req := p.(*client).buildRequest(provider.Request{MaxTokens: 131_072})
	if req.MaxTokens != 0 || req.MaxCompletionTokens != 131_072 {
		t.Fatalf("official Kimi K3 wire = max_tokens %d max_completion_tokens %d", req.MaxTokens, req.MaxCompletionTokens)
	}
}
