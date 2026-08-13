package control

import (
	"context"
	"errors"
	"testing"

	"reasonix/internal/provider"
)

// fakeTitleProvider streams a fixed text (or error) for topic-title tests.
type fakeTitleProvider struct {
	out string
	err error
}

func (p *fakeTitleProvider) Name() string { return "fake-title" }

func (p *fakeTitleProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, 2)
	if p.err != nil {
		close(ch)
		return ch, p.err
	}
	if p.out != "" {
		ch <- provider.Chunk{Type: provider.ChunkText, Text: p.out}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func titleTestController(p provider.Provider) *Controller {
	return New(Options{
		ModelRef: "fake/fake-title",
		ProviderResolver: &provider.StaticResolver{
			Descriptors: []provider.Descriptor{{Ref: "fake/fake-title"}},
			Providers:   map[string]provider.Provider{"fake/fake-title": p},
		},
	})
}

func TestGenerateTopicTitle(t *testing.T) {
	ctrl := titleTestController(&fakeTitleProvider{out: `"Fix the login bug in auth.go"`})
	title, err := ctrl.GenerateTopicTitle(context.Background(), "please fix the login bug in auth.go, users cannot log in")
	if err != nil {
		t.Fatalf("GenerateTopicTitle: %v", err)
	}
	if title != "Fix the login bug in auth.go" {
		t.Fatalf("title = %q, want %q", title, "Fix the login bug in auth.go")
	}
}

func TestGenerateTopicTitleTrimsQuotesAndWhitespace(t *testing.T) {
	ctrl := titleTestController(&fakeTitleProvider{out: "  \n  “Refactor the payment module” \n"})
	title, err := ctrl.GenerateTopicTitle(context.Background(), "refactor the payment module")
	if err != nil {
		t.Fatalf("GenerateTopicTitle: %v", err)
	}
	if title != "Refactor the payment module" {
		t.Fatalf("title = %q, want %q", title, "Refactor the payment module")
	}
}

func TestGenerateTopicTitleBoundedLength(t *testing.T) {
	long := "This is a very long title that the model should never produce because we ask for a short one, but just in case it does we truncate it at the topic title budget and add an ellipsis without leaving a dangling comma"
	ctrl := titleTestController(&fakeTitleProvider{out: long})
	title, err := ctrl.GenerateTopicTitle(context.Background(), "anything")
	if err != nil {
		t.Fatalf("GenerateTopicTitle: %v", err)
	}
	if len([]rune(title)) > topicTitleMaxRunes+1 {
		t.Fatalf("title too long: %d runes (cap %d + ellipsis): %q", len([]rune(title)), topicTitleMaxRunes, title)
	}
}

func TestGenerateTopicTitleEmptyTranscript(t *testing.T) {
	ctrl := titleTestController(&fakeTitleProvider{out: "irrelevant"})
	if _, err := ctrl.GenerateTopicTitle(context.Background(), "   "); err == nil {
		t.Fatal("expected error for empty transcript")
	}
}

func TestGenerateTopicTitleNoProvider(t *testing.T) {
	ctrl := New(Options{ModelRef: "fake/fake-title"}) // no resolver
	if _, err := ctrl.GenerateTopicTitle(context.Background(), "some conversation"); err == nil {
		t.Fatal("expected error when no provider resolver is configured")
	}
}

func TestGenerateTopicTitleNoModelRef(t *testing.T) {
	ctrl := New(Options{ProviderResolver: &provider.StaticResolver{}})
	if _, err := ctrl.GenerateTopicTitle(context.Background(), "some conversation"); err == nil {
		t.Fatal("expected error when no model ref is configured")
	}
}

func TestGenerateTopicTitleProviderEmptyOutput(t *testing.T) {
	ctrl := titleTestController(&fakeTitleProvider{out: ""})
	if _, err := ctrl.GenerateTopicTitle(context.Background(), "some conversation"); err == nil {
		t.Fatal("expected error when provider returns empty output")
	}
}

func TestGenerateTopicTitleProviderStreamError(t *testing.T) {
	ctrl := titleTestController(&fakeTitleProvider{err: errors.New("boom")})
	if _, err := ctrl.GenerateTopicTitle(context.Background(), "some conversation"); err == nil {
		t.Fatal("expected error when provider stream fails")
	}
}

func TestCleanTopicTitle(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{`"Hello"`, "Hello"},
		{"  spaced   out  ", "spaced out"},
		{"Trailing comma,", "Trailing comma,"},
		{"Trailing, and stuff.", "Trailing, and stuff."},
		{"…", "…"},
	}
	for _, c := range cases {
		if got := cleanTopicTitle(c.in); got != c.want {
			t.Errorf("cleanTopicTitle(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
