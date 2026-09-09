package openai

import (
	"reasonix/internal/provider"
	"testing"
)

func TestDeepSeekBetaExplicitWireAlias(t *testing.T) {
	const alias = "DeepSeek-V4.1-Flash-Expires-On-0910"
	c := newTestClient(t, alias, nil)
	if got := c.buildRequest(provider.Request{}).Model; got != "deepseek-v4.1-flash-expires-on-0910" {
		t.Fatal(got)
	}
	if c.modelInfo.ID != alias {
		t.Fatal("model metadata identity changed")
	}
	if got := deepSeekChatWireModel("https://api.deepseek.com/chat/completions", alias); got != "deepseek-v4.1-flash-expires-on-0910" {
		t.Fatal(got)
	}
	for _, model := range []string{"Custom-Model", "DEEPSEEK-V4.1-FLASH-EXPIRES-ON-0910"} {
		if got := deepSeekChatWireModel("https://api.deepseek.com/chat/completions", model); got != model {
			t.Fatal(got)
		}
	}
	if got := deepSeekChatWireModel("https://gateway.example/v1/chat/completions", alias); got != alias {
		t.Fatal(got)
	}
}
