package provider

import (
	"context"
	"encoding/json"
	"testing"
)

// --- Pricing.Cost ---

func TestPricingCostNil(t *testing.T) {
	var p *Pricing
	if got := p.Cost(&Usage{PromptTokens: 100}); got != 0 {
		t.Errorf("nil Pricing.Cost = %f, want 0", got)
	}
}

func TestPricingCostNilUsage(t *testing.T) {
	p := &Pricing{Input: 2.0, Output: 10.0}
	if got := p.Cost(nil); got != 0 {
		t.Errorf("nil Usage.Cost = %f, want 0", got)
	}
}

func TestPricingCostBothNil(t *testing.T) {
	var p *Pricing
	if got := p.Cost(nil); got != 0 {
		t.Errorf("both nil.Cost = %f, want 0", got)
	}
}

func TestPricingCostCalculation(t *testing.T) {
	p := &Pricing{
		CacheHit: 0.5,  // ¥0.5 per 1M cached tokens
		Input:    2.0,  // ¥2.0 per 1M uncached tokens
		Output:   10.0, // ¥10.0 per 1M completion tokens
	}
	u := &Usage{
		CacheHitTokens:   1_000_000,
		CacheMissTokens:  500_000,
		CompletionTokens: 200_000,
	}
	// Expected: (1M * 0.5 + 500K * 2.0 + 200K * 10.0) / 1M
	//         = (0.5 + 1.0 + 2.0) = 3.5
	got := p.Cost(u)
	if got != 3.5 {
		t.Errorf("Cost = %f, want 3.5", got)
	}
}

func TestPricingCostZeroTokens(t *testing.T) {
	p := &Pricing{Input: 2.0, Output: 10.0}
	u := &Usage{}
	if got := p.Cost(u); got != 0 {
		t.Errorf("zero tokens Cost = %f, want 0", got)
	}
}

// --- Pricing.Symbol ---

func TestPricingSymbolDefault(t *testing.T) {
	p := &Pricing{}
	if got := p.Symbol(); got != "¥" {
		t.Errorf("empty Currency.Symbol() = %q, want ¥", got)
	}
}

func TestPricingSymbolNil(t *testing.T) {
	var p *Pricing
	if got := p.Symbol(); got != "¥" {
		t.Errorf("nil.Symbol() = %q, want ¥", got)
	}
}

func TestPricingSymbolCustom(t *testing.T) {
	p := &Pricing{Currency: "$"}
	if got := p.Symbol(); got != "$" {
		t.Errorf("Symbol() = %q, want $", got)
	}
}

// --- AuthError ---

func TestAuthErrorWithKeyEnv(t *testing.T) {
	e := &AuthError{Provider: "deepseek", KeyEnv: "DEEPSEEK_API_KEY", Status: 401}
	msg := e.Error()
	for _, want := range []string{"deepseek", "DEEPSEEK_API_KEY", "401", "invalid or expired"} {
		if !contains(msg, want) {
			t.Errorf("AuthError.Error() missing %q: %s", want, msg)
		}
	}
}

func TestAuthErrorWithoutKeyEnv(t *testing.T) {
	e := &AuthError{Provider: "openai", Status: 403}
	msg := e.Error()
	if !contains(msg, "the API key") {
		t.Errorf("AuthError without KeyEnv should say 'the API key': %s", msg)
	}
	if !contains(msg, "403") {
		t.Errorf("AuthError should include status code 403: %s", msg)
	}
}

func TestAuthErrorImplementsError(t *testing.T) {
	var err error = &AuthError{Provider: "test", Status: 401}
	if err.Error() == "" {
		t.Error("AuthError.Error() should not be empty")
	}
}

// --- Registry ---

func TestRegistryKindsSorted(t *testing.T) {
	// The openai package self-registers via init(); we can't control that here
	// but we can verify Kinds() returns a sorted list.
	kinds := Kinds()
	for i := 1; i < len(kinds); i++ {
		if kinds[i-1] >= kinds[i] {
			t.Errorf("Kinds() not sorted: %v", kinds)
			break
		}
	}
}

func TestNewUnknownKind(t *testing.T) {
	_, err := New("nonexistent-kind-xyzzy", Config{})
	if err == nil {
		t.Fatal("expected error for unknown kind")
	}
	if !contains(err.Error(), "unknown kind") {
		t.Errorf("error should mention 'unknown kind': %v", err)
	}
}

func TestNewWithRegisteredKind(t *testing.T) {
	// Register a mock factory.
	Register("test-mock-__"+t.Name(), func(cfg Config) (Provider, error) {
		return nil, nil
	})
	// We can't easily unregister, but we can test it doesn't panic.
}

// --- Role constants ---

func TestRoleConstants(t *testing.T) {
	if RoleSystem != "system" {
		t.Errorf("RoleSystem = %q", RoleSystem)
	}
	if RoleUser != "user" {
		t.Errorf("RoleUser = %q", RoleUser)
	}
	if RoleAssistant != "assistant" {
		t.Errorf("RoleAssistant = %q", RoleAssistant)
	}
	if RoleTool != "tool" {
		t.Errorf("RoleTool = %q", RoleTool)
	}
}

// --- ChunkType constants ---

func TestChunkTypeConstants(t *testing.T) {
	types := []ChunkType{ChunkText, ChunkReasoning, ChunkToolCallStart, ChunkToolCall, ChunkUsage, ChunkDone, ChunkError}
	for i, ct := range types {
		if int(ct) != i {
			t.Errorf("ChunkType %d: got %d", i, int(ct))
		}
	}
}

// --- ToolSchema ---

func TestToolSchemaJSON(t *testing.T) {
	ts := ToolSchema{
		Name:        "bash",
		Description: "Run a shell command",
		Parameters:  json.RawMessage(`{"type":"object"}`),
	}
	b, err := json.Marshal(ts)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !contains(string(b), "bash") {
		t.Errorf("JSON missing name: %s", b)
	}
}

// helper
func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsStr(s, sub))
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// Ensure the Provider interface is satisfied by a minimal mock (compile-time check).
var _ Provider = (*mockProvider)(nil)

type mockProvider struct{}

func (m *mockProvider) Name() string { return "mock" }
func (m *mockProvider) Stream(ctx context.Context, req Request) (<-chan Chunk, error) {
	ch := make(chan Chunk, 1)
	ch <- Chunk{Type: ChunkDone}
	close(ch)
	return ch, nil
}

func TestMockProviderImplementsInterface(t *testing.T) {
	p := &mockProvider{}
	if p.Name() != "mock" {
		t.Errorf("Name = %q", p.Name())
	}
	ch, err := p.Stream(context.Background(), Request{})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	got := <-ch
	if got.Type != ChunkDone {
		t.Errorf("Chunk.Type = %d, want ChunkDone", got.Type)
	}
}
