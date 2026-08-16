package control

import (
	"context"
	"errors"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
)

type sessionTitleProviderStub struct {
	out      string
	err      error
	requests []provider.Request
}

func (p *sessionTitleProviderStub) Name() string { return "session-title-stub" }

func (p *sessionTitleProviderStub) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.requests = append(p.requests, req)
	if p.err != nil {
		return nil, p.err
	}
	ch := make(chan provider.Chunk, 2)
	if p.out != "" {
		ch <- provider.Chunk{Type: provider.ChunkText, Text: p.out}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func sessionTitleTestController(prov provider.Provider, sink event.Sink) *Controller {
	return New(Options{
		ModelRef: "test/title-model",
		Sink:     sink,
		ProviderResolver: &provider.StaticResolver{
			Descriptors: []provider.Descriptor{{Ref: "test/title-model"}},
			Providers:   map[string]provider.Provider{"test/title-model": prov},
		},
	})
}

func TestGenerateSessionTitleUsesBoundedNoToolRequest(t *testing.T) {
	prov := &sessionTitleProviderStub{out: "  “Fix login redirect loop”  "}
	ctrl := sessionTitleTestController(prov, event.Discard)
	title, err := ctrl.GenerateSessionTitle(context.Background(), "User: login redirects forever")
	if err != nil {
		t.Fatalf("GenerateSessionTitle: %v", err)
	}
	if title != "Fix login redirect loop" {
		t.Fatalf("title = %q", title)
	}
	if len(prov.requests) != 1 {
		t.Fatalf("requests = %d", len(prov.requests))
	}
	req := prov.requests[0]
	if len(req.Tools) != 0 || req.MaxTokens != 128 || len(req.Messages) != 2 {
		t.Fatalf("request = %+v", req)
	}
	if req.Messages[0].Content != sessionTitleSystemPrompt {
		t.Fatal("unexpected system prompt")
	}
}

func TestGenerateSessionTitleBoundsTranscriptAndOutput(t *testing.T) {
	prov := &sessionTitleProviderStub{out: strings.Repeat("long title ", 20)}
	ctrl := sessionTitleTestController(prov, event.Discard)
	title, err := ctrl.GenerateSessionTitle(context.Background(), strings.Repeat("界", sessionTitleMaxTranscriptRunes+100))
	if err != nil {
		t.Fatalf("GenerateSessionTitle: %v", err)
	}
	if got := len([]rune(prov.requests[0].Messages[1].Content)); got != sessionTitleMaxTranscriptRunes {
		t.Fatalf("transcript runes = %d", got)
	}
	if got := len([]rune(title)); got > sessionTitleMaxRunes+1 {
		t.Fatalf("title runes = %d: %q", got, title)
	}
}

func TestGenerateSessionTitleRejectsMissingInputsAndProviderFailures(t *testing.T) {
	if _, err := sessionTitleTestController(&sessionTitleProviderStub{}, event.Discard).GenerateSessionTitle(context.Background(), " "); err == nil {
		t.Fatal("empty transcript should fail")
	}
	if _, err := New(Options{ModelRef: "test/title-model"}).GenerateSessionTitle(context.Background(), "hello"); err == nil {
		t.Fatal("missing resolver should fail")
	}
	ctrl := sessionTitleTestController(&sessionTitleProviderStub{err: errors.New("boom")}, event.Discard)
	if _, err := ctrl.GenerateSessionTitle(context.Background(), "hello"); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("provider error = %v", err)
	}
}

func TestCleanSessionTitle(t *testing.T) {
	tests := map[string]string{
		`"Hello"`:               "Hello",
		"  spaced   out  ":      "spaced out",
		"Trailing punctuation!": "Trailing punctuation!",
		"‘中文会话标题’":              "中文会话标题",
	}
	for input, want := range tests {
		if got := cleanSessionTitle(input); got != want {
			t.Errorf("cleanSessionTitle(%q) = %q, want %q", input, got, want)
		}
	}
}
