package openai

import "testing"

func TestUnknownDeepSeekVisionRequiresDeclaration(t *testing.T) {
	for _, model := range []string{"deepseek-v5-vision", "future-vision"} {
		if DeepSeekImageInputAllowed(true, "", model, false, false) {
			t.Fatalf("%s inferred image support without declaration", model)
		}
		if !DeepSeekImageInputAllowed(true, "", model, true, true) {
			t.Fatalf("%s ignored explicit image declaration", model)
		}
	}
	if !DeepSeekImageInputAllowed(true, "", "deepseek-flash", false, false) {
		t.Fatal("known multimodal model required a declaration")
	}
	if DeepSeekImageInputAllowed(true, "", "deepseek-v4-pro", true, true) {
		t.Fatal("known text-only model accepted images")
	}
}
