package openai

import (
	"context"
	"errors"
	"testing"

	"reasonix/internal/provider"
)

func newTestClient(t *testing.T, model string, extra map[string]any) *client {
	t.Helper()
	cfg := provider.Config{Name: "p", BaseURL: "https://api.deepseek.com", Model: model, APIKey: "k", Extra: extra}
	p, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return p.(*client)
}

func TestEffortOverrideDeepSeekFlash(t *testing.T) {
	c := newTestClient(t, "deepseek-v4-flash", map[string]any{"reasoning_protocol": "deepseek"})
	if got := c.buildRequest(provider.Request{}).ReasoningEffort; got != "high" {
		t.Fatalf("default reasoning_effort = %q, want high", got)
	}
	if got := c.buildRequest(provider.Request{EffortOverride: "low"}).ReasoningEffort; got != "low" {
		t.Fatalf("override low: reasoning_effort = %q, want low", got)
	}
	assertRejectedEffort(t, c, "medium")
	request := c.buildRequest(provider.Request{EffortOverride: "disabled"})
	if request.ReasoningEffort != "" || request.Thinking == nil || request.Thinking.Type != "disabled" {
		t.Fatalf("disabled override = thinking:%#v effort:%q, want thinking disabled and no effort", request.Thinking, request.ReasoningEffort)
	}
}

func TestEffortOverrideDeepSeekNonFlashRejectsLow(t *testing.T) {
	c := newTestClient(t, "deepseek-v4", map[string]any{"reasoning_protocol": "deepseek"})
	assertRejectedEffort(t, c, "low")
	if got := c.buildRequest(provider.Request{EffortOverride: "max"}).ReasoningEffort; got != "max" {
		t.Fatalf("max is in the official DeepSeek vocabulary, got %q", got)
	}
}

func TestEffortOverrideDeepSeekProSupportsLow(t *testing.T) {
	c := newTestClient(t, "deepseek-v4-pro", map[string]any{"reasoning_protocol": "deepseek"})
	if got := c.buildRequest(provider.Request{EffortOverride: "low"}).ReasoningEffort; got != "low" {
		t.Fatalf("Pro low reasoning_effort = %q, want low", got)
	}
}

func TestEffortOverrideHonorsSupportedEfforts(t *testing.T) {
	c := newTestClient(t, "deepseek-v4", map[string]any{
		"reasoning_protocol": "deepseek",
		"effort":             "high",
		"supported_efforts":  []string{"low", "high", "disabled"},
	})
	if got := c.buildRequest(provider.Request{EffortOverride: "low"}).ReasoningEffort; got != "low" {
		t.Fatalf("declared vocabulary must admit low, got %q", got)
	}
	assertRejectedEffort(t, c, "max")
	request := c.buildRequest(provider.Request{EffortOverride: "disabled"})
	if request.ReasoningEffort != "" || request.Thinking == nil || request.Thinking.Type != "disabled" {
		t.Fatalf("disabled override = thinking:%#v effort:%q, want thinking disabled and no effort", request.Thinking, request.ReasoningEffort)
	}
}

func assertRejectedEffort(t *testing.T, c *client, id string) {
	t.Helper()
	_, err := c.Stream(context.Background(), provider.Request{EffortOverride: id})
	var unsupported *provider.UnsupportedReasoningEffort
	if !errors.As(err, &unsupported) {
		t.Fatalf("expected rejection before I/O for %q, got %v", id, err)
	}
}
func TestEffortOverrideRejectedByBinaryThinkingKnobs(t *testing.T) {
	for _, url := range []string{"https://api.minimaxi.com/v1", "https://open.bigmodel.cn/api/paas/v4", "https://api.longcat.chat/v1"} {
		p, err := New(provider.Config{Name: "test", BaseURL: url, Model: "model"})
		if err != nil {
			t.Fatal(err)
		}
		c := p.(*client)
		assertRejectedEffort(t, c, "low")
		out := c.buildRequest(provider.Request{EffortOverride: "disabled"})
		if out.Thinking == nil || out.Thinking.Type != "disabled" || out.ReasoningEffort != "" {
			t.Fatalf("disabled wire: %+v", out)
		}
	}
}
func TestEffortOverrideRejectedWithoutDepthVocabulary(t *testing.T) {
	c := &client{model: "unknown"}
	assertRejectedEffort(t, c, "low")
	disabled := newTestClient(t, "deepseek-v4", map[string]any{"reasoning_protocol": "deepseek", "thinking": "disabled"})
	assertRejectedEffort(t, disabled, "high")
}
