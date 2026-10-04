package commitmsg

import (
	"context"
	"errors"
	"strings"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
)

type fakeProvider struct {
	answer string
	got    []provider.Message
}

func (f *fakeProvider) Name() string { return "fake" }

func (f *fakeProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	f.got = req.Messages
	ch := make(chan provider.Chunk, 2)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: f.answer}
	ch <- provider.Chunk{Type: provider.ChunkUsage, Usage: &provider.Usage{PromptTokens: 10, CompletionTokens: 5}}
	close(ch)
	return ch, nil
}

func TestProposalReadsFilesDiffAndStyleAndBillsItsOwnSource(t *testing.T) {
	prov := &fakeProvider{answer: "```\nfeat(auth): add session expiry\n```"}
	var billed []event.Event
	g := New(prov, nil, "deepseek/flash", event.FuncSink(func(e event.Event) { billed = append(billed, e) }))

	out, err := g.Generate(context.Background(), Input{
		Files:  []File{{Path: "auth/session.go", Status: "M"}},
		Diff:   "+func Expire() {}\n+api_key = \"abcd1234efgh5678ijkl9012\"\n",
		Recent: []string{"fix(auth): loop on login"},
	})
	if err != nil || out != "feat(auth): add session expiry" {
		t.Fatalf("out = %q err = %v", out, err)
	}
	user := prov.got[1].Content
	for _, want := range []string{"M auth/session.go", "+func Expire()", "fix(auth): loop on login"} {
		if !strings.Contains(user, want) {
			t.Errorf("evidence lacks %q:\n%s", want, user)
		}
	}
	if strings.Contains(user, "abcd1234efgh5678ijkl9012") {
		t.Errorf("a credential-shaped value reached the model:\n%s", user)
	}
	if len(billed) != 1 || billed[0].UsageSource != event.UsageSourceCommitMessage {
		t.Fatalf("usage = %+v, want one commit-message event", billed)
	}
}

func TestOversizedDiffIsClippedAndSaysSo(t *testing.T) {
	prov := &fakeProvider{answer: "chore: x"}
	g := New(prov, nil, "", nil)
	if _, err := g.Generate(context.Background(), Input{Diff: strings.Repeat("+x\n", maxDiffBytes)}); err != nil {
		t.Fatal(err)
	}
	if user := prov.got[1].Content; len(user) > maxDiffBytes+1024 || !strings.Contains(user, "[diff truncated]") {
		t.Fatalf("evidence is %d bytes", len(user))
	}
}

func TestUnavailableAndEmptyAnswerAreTyped(t *testing.T) {
	if _, err := (*Generator)(nil).Generate(context.Background(), Input{}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("nil generator: %v", err)
	}
	if _, err := New(&fakeProvider{answer: " "}, nil, "", nil).Generate(context.Background(), Input{}); !errors.Is(err, ErrNoAnswer) {
		t.Fatalf("blank answer: %v", err)
	}
}

func TestDraftLosesControlSequencesAndAttributionTrailers(t *testing.T) {
	answer := "feat: add x\x1b]8;;http://evil\x07 \u202egpj\n\nWhy it was needed.\n\nCo-authored-by: Someone <s@x.io>\nSigned-off-by: A <a@x.io>"
	out, err := New(&fakeProvider{answer: answer}, nil, "", nil).Generate(context.Background(), Input{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(out, "\x1b\x07\u202e") || strings.Contains(out, "Co-authored-by") || strings.Contains(out, "Signed-off-by") {
		t.Fatalf("out = %q", out)
	}
	if !strings.Contains(out, "Why it was needed.") {
		t.Fatalf("the body was dropped: %q", out)
	}
	if one, _ := New(&fakeProvider{answer: "fix: x"}, nil, "", nil).Generate(context.Background(), Input{}); one != "fix: x" {
		t.Fatalf("a lone subject was taken for trailers: %q", one)
	}
}
