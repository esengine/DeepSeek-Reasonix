package openai

import "testing"

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
