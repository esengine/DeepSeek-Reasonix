package rewriter

import (
    "context"
    "testing"
    "time"

    "reasonix/internal/provider"
)

// mockProvider 实现 provider.Provider 接口
type mockProvider struct {
    response string
    err      error
}

func (m *mockProvider) Name() string {
    return "mock"
}

func (m *mockProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
    ch := make(chan provider.Chunk, 1)
    if m.err != nil {
        close(ch)
        return ch, m.err
    }
    ch <- provider.Chunk{Type: provider.ChunkText, Text: m.response}
    close(ch)
    return ch, nil
}

// 如果 provider 接口还有其他方法，比如 Capabilities()，请补上
// func (m *mockProvider) Capabilities() provider.Capabilities { return provider.Capabilities{} }

func TestRewriterRewrite(t *testing.T) {
    mockProv := &mockProvider{response: "rewritten: test"}
    cfg := Config{Enabled: true, Timeout: 1 * time.Second, MaxLength: 100}
    rw := NewProviderRewriter(mockProv, cfg)

    result, err := rw.Rewrite(context.Background(), "do something")
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if result != "rewritten: test" {
        t.Errorf("expected 'rewritten: test', got %q", result)
    }
}

func TestRewriterDisabled(t *testing.T) {
    cfg := Config{Enabled: false}
    rw := NewProviderRewriter(nil, cfg)
    input := "hello"
    result, err := rw.Rewrite(context.Background(), input)
    if err != nil {
        t.Fatal(err)
    }
    if result != input {
        t.Errorf("expected %q, got %q", input, result)
    }
}

func TestRewriterMaxLength(t *testing.T) {
    cfg := Config{Enabled: true, MaxLength: 5}
    rw := NewProviderRewriter(nil, cfg)
    longInput := "this is too long"
    result, _ := rw.Rewrite(context.Background(), longInput)
    if result != longInput {
        t.Errorf("expected original, got %q", result)
    }
}