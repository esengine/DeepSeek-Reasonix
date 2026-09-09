package openai

import "testing"

func TestUnknownDeepSeekVisionRequiresDeclaration(t *testing.T) {
	for _, model := range []string{"deepseek-v4.1-flash-expires-on-0910", "future-vision"} {
		if DeepSeekImageInputAllowed(true, "", model, false, false) {
			t.Fatalf("%s inferred image support without declaration", model)
		}
		if !DeepSeekImageInputAllowed(true, "", model, true, true) {
			t.Fatalf("%s ignored explicit image declaration", model)
		}
	}
	if DeepSeekImageInputAllowed(true, "", "deepseek-v4-flash", true, true) {
		t.Fatal("known text-only model accepted images")
	}
}
