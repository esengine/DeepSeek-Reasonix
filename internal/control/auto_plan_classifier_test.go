package control

import (
	"context"
	"strings"
	"testing"

	"reasonix/internal/provider"
)

type classifierProvider struct {
	text string
	err  error
	req  provider.Request
}

func (p *classifierProvider) Name() string { return "classifier" }

func (p *classifierProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.req = req
	if p.err != nil {
		return nil, p.err
	}
	ch := make(chan provider.Chunk, 1)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: p.text}
	close(ch)
	return ch, nil
}

func TestProviderAutoPlanClassifierParsesJSON(t *testing.T) {
	p := &classifierProvider{text: "```json\n{\"needs_plan\":true,\"reason\":\"multi-file\"}\n```"}
	c := NewProviderAutoPlanClassifier(p)

	needsPlan, reason, err := c.NeedsPlan(context.Background(), "implement feature", 1)
	if err != nil {
		t.Fatalf("NeedsPlan error: %v", err)
	}
	if !needsPlan || reason != "multi-file" {
		t.Fatalf("NeedsPlan = (%v,%q), want (true,multi-file)", needsPlan, reason)
	}
	if len(p.req.Messages) != 2 || p.req.Messages[0].Role != provider.RoleSystem {
		t.Fatalf("request messages = %+v", p.req.Messages)
	}
	if !strings.Contains(p.req.Messages[0].Content, "plan-first workflow") || strings.Contains(strings.ToLower(p.req.Messages[0].Content), "read-only planning") {
		t.Fatalf("classifier prompt must describe workflow, not a read-only boundary: %q", p.req.Messages[0].Content)
	}
	if p.req.MaxTokens != 80 || p.req.Temperature == nil || *p.req.Temperature != 0 {
		t.Fatalf("request limits = max %d temp %v, want 80/ptr(0)", p.req.MaxTokens, p.req.Temperature)
	}
	if !strings.Contains(p.req.Messages[1].Content, "heuristic_score=1") {
		t.Fatalf("user message missing score: %q", p.req.Messages[1].Content)
	}
}

func TestProviderAutoPlanClassifierRejectsBadJSON(t *testing.T) {
	p := &classifierProvider{text: "needs plan"}
	c := NewProviderAutoPlanClassifier(p)

	if _, _, err := c.NeedsPlan(context.Background(), "x", 1); err == nil {
		t.Fatal("NeedsPlan should reject non-JSON response")
	}
}

func TestProviderAutoPlanClassifierRequiresNeedsPlan(t *testing.T) {
	p := &classifierProvider{text: `{"reason":"missing decision"}`}
	c := NewProviderAutoPlanClassifier(p)

	if _, _, err := c.NeedsPlan(context.Background(), "x", 1); err == nil {
		t.Fatal("NeedsPlan should reject JSON without needs_plan")
	}
}

func TestProviderAutoPlanClassifierNilReceiverReturnsError(t *testing.T) {
	var c *ProviderAutoPlanClassifier

	if _, _, err := c.NeedsPlan(context.Background(), "x", 1); err == nil {
		t.Fatal("NeedsPlan should reject nil classifier")
	}
}
