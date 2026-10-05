package anthropic

import "testing"

func TestSharedWindowOutputBudgetCapability(t *testing.T) {
	deepseek := &client{deepseek: true, defaultMaxTokens: 32 * 1024}
	if !deepseek.SharesContextWindow() || deepseek.OutputBudget() != 32*1024 {
		t.Fatalf("DeepSeek capability = shared:%v budget:%d", deepseek.SharesContextWindow(), deepseek.OutputBudget())
	}
	anthropic := &client{defaultMaxTokens: 16 * 1024}
	if anthropic.SharesContextWindow() {
		t.Fatal("native Anthropic mode must keep its independent output ceiling")
	}
}
