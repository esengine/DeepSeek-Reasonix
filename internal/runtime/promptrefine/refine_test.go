package promptrefine

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

// What reaches the model is the draft and the recent turns fenced as material,
// and what comes back is billed to its own source and stripped of wrapping.
func TestARewriteReadsTheDraftAndRecentTurnsAndBillsItsOwnSource(t *testing.T) {
	prov := &fakeProvider{answer: "```\n修复登录循环：在 auth/session.go 里……\n```"}
	var billed []event.Event
	r := New(prov, nil, "deepseek/flash", event.FuncSink(func(e event.Event) { billed = append(billed, e) }))

	out, err := r.Refine(context.Background(), Input{
		Draft:     "  修一下那个bug  ",
		Workspace: "reasonix",
		Recent: []Turn{
			{Role: "user", Text: "oldest, dropped by the turn limit"},
			{Role: "user", Text: "登录后一直跳回登录页"},
			{Role: "assistant", Text: "问题在 auth/session.go 的过期判断"},
			{Role: "user", Text: "好的"},
			{Role: "assistant", Text: "要我改吗？"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out != "修复登录循环：在 auth/session.go 里……" {
		t.Fatalf("out = %q, want the fence taken off", out)
	}
	user := prov.got[1].Content
	for _, want := range []string{"<workspace>reasonix</workspace>", "[assistant] 问题在 auth/session.go", "<draft>\n修一下那个bug\n</draft>"} {
		if !strings.Contains(user, want) {
			t.Errorf("evidence lacks %q:\n%s", want, user)
		}
	}
	if strings.Contains(user, "oldest") {
		t.Errorf("read more turns than it keeps:\n%s", user)
	}
	if len(billed) != 1 || billed[0].UsageSource != event.UsageSourcePromptRefine {
		t.Fatalf("usage = %+v, want one prompt-refine event", billed)
	}
}

func TestADraftTheRefinerCannotTakeSaysWhy(t *testing.T) {
	r := New(&fakeProvider{answer: "  "}, nil, "", nil)
	cases := []struct {
		r     *Refiner
		draft string
		want  error
	}{
		{nil, "x", ErrUnavailable},
		{r, "   ", ErrEmpty},
		{r, strings.Repeat("长", MaxDraftBytes), ErrTooLong},
		{r, "fine", ErrNoAnswer},
	}
	for _, c := range cases {
		if _, err := c.r.Refine(context.Background(), Input{Draft: c.draft}); !errors.Is(err, c.want) {
			t.Errorf("draft %.10q = %v, want %v", c.draft, err, c.want)
		}
	}
}

func TestCleanTakesOffOnlyWhatWrapsTheWholeAnswer(t *testing.T) {
	for in, want := range map[string]string{
		`"do the thing"`:             "do the thing",
		"「改一下」":                      "改一下",
		`"a" and "b"`:                `"a" and "b"`,
		"```go\nfmt.Println()\n```":  "fmt.Println()",
		"run `go test` then ship it": "run `go test` then ship it",
	} {
		if got := clean(in); got != want {
			t.Errorf("clean(%q) = %q, want %q", in, got, want)
		}
	}
}
