package provider

import (
	"context"
	"strings"
	"testing"
	"time"
)

type auxiliaryScript struct {
	calls   int
	partial bool
}

func (*auxiliaryScript) Name() string { return "auxiliary-test" }
func (p *auxiliaryScript) Stream(context.Context, Request) (<-chan Chunk, error) {
	p.calls++
	if !p.partial && p.calls < 4 {
		return nil, &APIError{Status: 503}
	}
	ch := make(chan Chunk, 3)
	ch <- Chunk{Type: ChunkText, Text: "candidate"}
	ch <- Chunk{Type: ChunkUsage, Usage: &Usage{PromptTokens: 10, CompletionTokens: 1, TotalTokens: 11}}
	if !p.partial {
		ch <- Chunk{Type: ChunkDone}
	}
	close(ch)
	return ch, nil
}
func TestAuxiliaryRetriesShareFiniteBudgetAndDoNotLeakPartialText(t *testing.T) {
	for _, partial := range []bool{false, true} {
		p := &auxiliaryScript{partial: partial}
		var waits []time.Duration
		ctx := WithRecoverySleeper(context.Background(), func(_ context.Context, d time.Duration) bool { waits = append(waits, d); return true })
		ch, err := StreamAuxiliary(ctx, p, Request{})
		if err != nil {
			t.Fatal(err)
		}
		var text strings.Builder
		var usage *Usage
		var streamErr error
		for c := range ch {
			switch c.Type {
			case ChunkText:
				text.WriteString(c.Text)
			case ChunkUsage:
				usage = c.Usage
			case ChunkError:
				streamErr = c.Err
			}
		}
		if p.calls != 4 || len(waits) != 3 {
			t.Fatalf("calls=%d waits=%v", p.calls, waits)
		}
		for i, want := range []time.Duration{2 * time.Second, 4 * time.Second, 8 * time.Second} {
			if waits[i] != want {
				t.Fatalf("waits=%v", waits)
			}
		}
		if usage == nil || usage.RequestCount != 4 {
			t.Fatalf("usage=%+v", usage)
		}
		if partial {
			if text.Len() != 0 || streamErr == nil || usage.PromptTokens != 40 {
				t.Fatalf("partial text=%q err=%v usage=%+v", text.String(), streamErr, usage)
			}
		} else if text.String() != "candidate" || streamErr != nil || !usage.Unknown || usage.PromptTokens != 10 {
			t.Fatalf("text=%q err=%v usage=%+v", text.String(), streamErr, usage)
		}
	}
}
