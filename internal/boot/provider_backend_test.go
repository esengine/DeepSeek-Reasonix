package boot

import (
	"context"
	"strings"
	"testing"

	"reasonix/internal/provider"
)

// fakeStreamProvider returns a canned stream of text chunks.
type fakeStreamProvider struct {
	chunks []string
	model  string
}

func (f *fakeStreamProvider) Name() string { return "fake" }

func (f *fakeStreamProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk)
	go func() {
		defer close(ch)
		for _, t := range f.chunks {
			select {
			case <-ctx.Done():
				return
			case ch <- provider.Chunk{Type: provider.ChunkText, Text: t}:
			}
		}
	}()
	return ch, nil
}

func TestProviderBackendComplete(t *testing.T) {
	p := &fakeStreamProvider{chunks: []string{"hello ", "world", ""}}
	b := providerBackend{p: p}
	got, err := b.Complete(context.Background(), "sys", "user", 64)
	if err != nil {
		t.Fatal(err)
	}
	if got != "hello world" {
		t.Errorf("Complete = %q, want %q", got, "hello world")
	}
}

func TestNewModelBackendNilProvider(t *testing.T) {
	if b := newModelBackend(nil, provider.Request{}); b != nil {
		t.Errorf("newModelBackend(nil) = %v, want nil", b)
	}
}

func TestProviderBackendSendsMessages(t *testing.T) {
	var captured provider.Request
	p := &capturingProvider{onStream: func(req provider.Request) {
		captured = req
	}}
	b := providerBackend{p: p}
	_, _ = b.Complete(context.Background(), "SYS", "USR", 128)
	if len(captured.Messages) != 2 {
		t.Fatalf("want 2 messages, got %d", len(captured.Messages))
	}
	if captured.Messages[0].Role != provider.RoleSystem || captured.Messages[0].Content != "SYS" {
		t.Errorf("system message wrong: %+v", captured.Messages[0])
	}
	if captured.Messages[1].Role != provider.RoleUser || captured.Messages[1].Content != "USR" {
		t.Errorf("user message wrong: %+v", captured.Messages[1])
	}
	if captured.MaxTokens != 128 {
		t.Errorf("MaxTokens = %d, want 128", captured.MaxTokens)
	}
}

// capturingProvider records the request and returns an empty stream.
type capturingProvider struct {
	onStream func(provider.Request)
}

func (c *capturingProvider) Name() string { return "capture" }

func (c *capturingProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	c.onStream(req)
	ch := make(chan provider.Chunk)
	close(ch)
	return ch, nil
}

var _ = strings.TrimSpace // keep import if unused later
