package provider

import (
	"context"
	"encoding/json"
	"testing"
)

// --- SanitizeToolPairing ---

// toolIDsAnswered reports whether every assistant tool_call id has a following
// tool message answering it — the contract the OpenAI/DeepSeek API enforces.
func toolIDsAnswered(msgs []Message) bool {
	answered := map[string]bool{}
	for _, m := range msgs {
		if m.Role == RoleTool {
			answered[m.ToolCallID] = true
		}
	}
	for _, m := range msgs {
		for _, tc := range m.ToolCalls {
			if !answered[tc.ID] {
				return false
			}
		}
	}
	return true
}

func TestSanitizeToolPairingBackfillsDanglingCall(t *testing.T) {
	in := []Message{
		{Role: RoleUser, Content: "list files"},
		{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "c1", Name: "ls"}}},
		{Role: RoleUser, Content: "never mind"},
	}
	out := SanitizeToolPairing(in)
	if !toolIDsAnswered(out) {
		t.Fatalf("dangling tool_call left unanswered: %+v", out)
	}
	// The backfilled result sits right after the assistant turn, keyed to its id.
	if out[2].Role != RoleTool || out[2].ToolCallID != "c1" {
		t.Fatalf("expected a backfilled tool result for c1 at index 2, got %+v", out[2])
	}
}

func TestSanitizeToolPairingKeepsCallOrderAndMultiple(t *testing.T) {
	in := []Message{
		{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "a"}, {ID: "b"}, {ID: "c"}}},
		{Role: RoleTool, ToolCallID: "b", Content: "B"}, // out of order, c missing
		{Role: RoleTool, ToolCallID: "a", Content: "A"},
	}
	out := SanitizeToolPairing(in)
	if !toolIDsAnswered(out) {
		t.Fatalf("not all calls answered: %+v", out)
	}
	gotOrder := []string{out[1].ToolCallID, out[2].ToolCallID, out[3].ToolCallID}
	want := []string{"a", "b", "c"}
	for i := range want {
		if gotOrder[i] != want[i] {
			t.Fatalf("tool results out of call order: got %v want %v", gotOrder, want)
		}
	}
}

func TestSanitizeToolPairingDropsOrphanToolMessage(t *testing.T) {
	in := []Message{
		{Role: RoleUser, Content: "hi"},
		{Role: RoleTool, ToolCallID: "ghost", Content: "leftover"}, // no preceding call
		{Role: RoleAssistant, Content: "hello"},
	}
	out := SanitizeToolPairing(in)
	for _, m := range out {
		if m.Role == RoleTool {
			t.Fatalf("orphan tool message survived: %+v", out)
		}
	}
	if len(out) != 2 {
		t.Fatalf("want 2 messages after dropping the orphan, got %d: %+v", len(out), out)
	}
}

func TestSanitizeToolPairingLeavesWellFormedUnchanged(t *testing.T) {
	in := []Message{
		{Role: RoleSystem, Content: "sys"},
		{Role: RoleUser, Content: "q"},
		{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "c1", Name: "ls"}}},
		{Role: RoleTool, ToolCallID: "c1", Name: "ls", Content: "main.go"},
		{Role: RoleAssistant, Content: "done"},
	}
	out := SanitizeToolPairing(in)
	if len(out) != len(in) {
		t.Fatalf("well-formed history changed length: %d -> %d", len(in), len(out))
	}
	if &out[0] != &in[0] {
		t.Fatalf("well-formed history should return the input slice without allocating")
	}
	for i := range in {
		if out[i].Role != in[i].Role || out[i].Content != in[i].Content || out[i].ToolCallID != in[i].ToolCallID {
			t.Fatalf("well-formed message %d mutated: %+v -> %+v", i, in[i], out[i])
		}
	}
}

func TestNormalizeSessionMessagesPreservesStandaloneToolMessage(t *testing.T) {
	in := []Message{
		{Role: RoleSystem, Content: "sys"},
		{Role: RoleUser, Content: "run it"},
		{Role: RoleTool, ToolCallID: "c1", Name: "bash", Content: "large output"},
	}
	out := NormalizeSessionMessages(in)
	if len(out) != len(in) {
		t.Fatalf("session normalization changed length: %d -> %d", len(in), len(out))
	}
	if &out[0] != &in[0] {
		t.Fatalf("session-safe orphan tool should keep the input slice unchanged")
	}
	if out[2].Role != RoleTool || out[2].Content != "large output" {
		t.Fatalf("standalone tool message was not preserved: %+v", out)
	}
}

func TestNormalizeSessionMessagesPreservesExtraToolResult(t *testing.T) {
	in := []Message{
		{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "c1", Name: "bash"}}},
		{Role: RoleTool, ToolCallID: "c1", Name: "bash", Content: "ok"},
		{Role: RoleTool, ToolCallID: "ghost", Name: "bash", Content: "saved extra output"},
	}
	out := NormalizeSessionMessages(in)
	if len(out) != len(in) {
		t.Fatalf("session normalization changed length: %d -> %d", len(in), len(out))
	}
	if out[2].ToolCallID != "ghost" || out[2].Content != "saved extra output" {
		t.Fatalf("extra stored tool result was not preserved: %+v", out)
	}
	wire := SanitizeToolPairing(in)
	if len(wire) != 2 {
		t.Fatalf("wire sanitize should still drop the extra orphan result, got %+v", wire)
	}
}

func TestSanitizeToolPairingClosesTruncatedArgs(t *testing.T) {
	cases := []struct{ in, want string }{
		{`{`, `{}`},
		{`{"time": 2`, `{"time": 2}`},
		{`{"command": "ls -la`, `{"command": "ls -la"}`},
		{`{"a": 1,`, `{"a": 1}`},
		{`{"a":`, `{"a":null}`},
		{`{"path": "C:\\tmp\`, `{"path": "C:\\tmp"}`},
		{`{"items": [1, 2`, `{"items": [1, 2]}`},
		{`total garbage`, `{}`},
		{`{"ok": true}`, `{"ok": true}`},
		{``, ``},
	}
	for _, c := range cases {
		in := []Message{
			{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "c1", Name: "bash", Arguments: c.in}}},
			{Role: RoleTool, ToolCallID: "c1", Content: "r"},
		}
		out := SanitizeToolPairing(in)
		if got := out[0].ToolCalls[0].Arguments; got != c.want {
			t.Errorf("args %q repaired to %q, want %q", c.in, got, c.want)
		}
		if in[0].ToolCalls[0].Arguments != c.in {
			t.Errorf("stored history mutated for %q: %q", c.in, in[0].ToolCalls[0].Arguments)
		}
	}
}

func TestBackfillToolCallNamesByID(t *testing.T) {
	calls := []ToolCall{{ID: "c1"}, {ID: "c2", Name: "grep"}}
	results := []Message{
		{Role: RoleTool, ToolCallID: "c2", Name: "grep"},
		{Role: RoleTool, ToolCallID: "c1", Name: "ls"}, // returned out of call order
	}
	out := backfillToolCallNames(calls, results)
	if out[0].Name != "ls" {
		t.Fatalf("empty name not backfilled by id: got %q want ls", out[0].Name)
	}
	if out[1].Name != "grep" {
		t.Fatalf("non-empty name clobbered: got %q want grep", out[1].Name)
	}
	if calls[0].Name != "" {
		t.Fatalf("input slice mutated: %+v", calls)
	}
}

func TestBackfillToolCallNamesPositional(t *testing.T) {
	// Empty ids defeat idDistinct, so names pair by position instead.
	calls := []ToolCall{{}, {}}
	results := []Message{{Role: RoleTool, Name: "ls"}, {Role: RoleTool, Name: "cat"}}
	out := backfillToolCallNames(calls, results)
	if out[0].Name != "ls" || out[1].Name != "cat" {
		t.Fatalf("positional backfill wrong: %+v", out)
	}
}

func TestBackfillToolCallNamesUnpairedStaysEmpty(t *testing.T) {
	out := backfillToolCallNames([]ToolCall{{ID: "c1"}}, nil)
	if out[0].Name != "" {
		t.Fatalf("unpaired call should keep its empty name, got %q", out[0].Name)
	}
}

func TestBackfillToolCallNamesNoEmptyReturnsInput(t *testing.T) {
	calls := []ToolCall{{ID: "c1", Name: "ls"}, {ID: "c2", Name: "grep"}}
	out := backfillToolCallNames(calls, []Message{{Role: RoleTool, ToolCallID: "c1", Name: "x"}})
	if &out[0] != &calls[0] {
		t.Fatalf("no empty names: want the input slice back without copying")
	}
}

func TestSanitizeToolPairingBackfillsEmptyName(t *testing.T) {
	in := []Message{
		{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "c1"}}}, // old session: name lost
		{Role: RoleTool, ToolCallID: "c1", Name: "ls", Content: "main.go"},
	}
	out := SanitizeToolPairing(in)
	if out[0].ToolCalls[0].Name != "ls" {
		t.Fatalf("empty tool-call name not backfilled on replay: %+v", out[0].ToolCalls)
	}
	if in[0].ToolCalls[0].Name != "" {
		t.Fatalf("stored history mutated: %+v", in[0].ToolCalls)
	}
}

func TestSanitizeToolPairingBackfillsMissingToolResultName(t *testing.T) {
	in := []Message{
		{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "c1", Name: "ls"}}},
		{Role: RoleTool, ToolCallID: "c1", Content: "main.go"},
	}
	out := SanitizeToolPairing(in)
	if out[1].Name != "ls" {
		t.Fatalf("missing tool result name not backfilled: %+v", out[1])
	}
	if in[1].Name != "" {
		t.Fatalf("stored history mutated: %+v", in[1])
	}
}

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

func TestPricingCostFallsBackToPromptTokensAsMiss(t *testing.T) {
	p := &Pricing{Input: 2.0, Output: 10.0}
	u := &Usage{PromptTokens: 500_000, CompletionTokens: 100_000}
	if got := p.Cost(u); got != 2.0 {
		t.Errorf("Cost = %f, want 2.0", got)
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

func TestPricingSymbolNormalizesCurrencyCodes(t *testing.T) {
	cases := []struct {
		currency string
		want     string
	}{
		{currency: "USD", want: "$"},
		{currency: "dollars", want: "$"},
		{currency: "CNY", want: "¥"},
		{currency: "￥", want: "¥"},
		{currency: "EUR", want: "€"},
		{currency: "₹", want: "₹"},
		{currency: "aud", want: "AUD "},
		{currency: "A$", want: "A$"},
		{currency: "HK$", want: "HK$"},
		{currency: "楼", want: "¥"},
	}
	for _, tc := range cases {
		p := &Pricing{Currency: tc.currency}
		if got := p.Symbol(); got != tc.want {
			t.Errorf("Currency %q Symbol() = %q, want %q", tc.currency, got, tc.want)
		}
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

func TestAuthErrorBodyStaysOutOfError(t *testing.T) {
	// Body carries the server's reason for display layers to extract, but it
	// must never leak into Error(): servers echo masked key fragments in auth
	// bodies, and the ambient string flows into logs and traces.
	e := &AuthError{Provider: "relay", Status: 401, Body: `{"error":{"message":"Your api key: ****ae54 has expired"}}`}
	if e.Body == "" {
		t.Fatal("Body should carry the server's reason")
	}
	if msg := e.Error(); contains(msg, "ae54") || contains(msg, "{") {
		t.Errorf("AuthError.Error() must not include body content: %s", msg)
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

func TestNewRejectsTypedNilProvider(t *testing.T) {
	kind := "test-typed-nil-__" + t.Name()
	Register(kind, func(cfg Config) (Provider, error) {
		var p *mockProvider
		return p, nil
	})

	_, err := New(kind, Config{})
	if err == nil {
		t.Fatal("New should reject typed nil provider")
	}
	if !contains(err.Error(), "returned nil provider") {
		t.Fatalf("New error = %v, want returned nil provider", err)
	}
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
	types := []ChunkType{ChunkText, ChunkReasoning, ChunkToolCallStart, ChunkToolCallArgsDelta, ChunkToolCall, ChunkUsage, ChunkDone, ChunkError}
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
