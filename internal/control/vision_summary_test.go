package control

import (
	"context"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

type visionSummaryTestProvider struct{}

func (visionSummaryTestProvider) Name() string { return "vision-test" }

func (visionSummaryTestProvider) Stream(ctx context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
	out := make(chan provider.Chunk, 2)
	go func() {
		defer close(out)
		select {
		case out <- provider.Chunk{Type: provider.ChunkText, Text: "a chart with OCR: Revenue 42"}:
		case <-ctx.Done():
			return
		}
		out <- provider.Chunk{Type: provider.ChunkDone}
	}()
	return out, nil
}

func TestPrepareVisionTurnSummarizesTextOnlyInputAndKeepsRawPrompt(t *testing.T) {
	c := &Controller{
		modelRef:    "text/text-model",
		visionModel: "vision/vision-model",
		visionProviderResolver: func(string) (provider.Provider, error) {
			return visionSummaryTestProvider{}, nil
		},
		sink: event.Discard,
	}
	ctx := context.Background()
	got, ctx, err := c.prepareVisionTurn(ctx, "请看这张图", []string{"data:image/png;base64,AA=="})
	if err != nil {
		t.Fatalf("prepareVisionTurn: %v", err)
	}
	if got == "请看这张图" || !contains(got, "<reasonix-image-context") || !contains(got, "Revenue 42") {
		t.Fatalf("prepared input = %q", got)
	}
	if summary := agent.VisionSummaryFromContext(ctx); summary == nil || summary.ModelRef != "vision/vision-model" {
		t.Fatalf("summary context = %+v", summary)
	}
}

func TestCachedVisionSummaryIsReused(t *testing.T) {
	session := agent.NewSession("system")
	calls := 0
	c := &Controller{
		modelRef:    "text/text-model",
		visionModel: "vision/vision-model",
		executor:    agent.New(nil, nil, session, agent.Options{}, event.Discard),
		visionProviderResolver: func(string) (provider.Provider, error) {
			calls++
			return visionSummaryTestProvider{}, nil
		},
		sink: event.Discard,
	}
	images := []string{"data:image/png;base64,AA=="}
	first, ctx, err := c.prepareVisionTurn(context.Background(), "first", images)
	if err != nil {
		t.Fatalf("first prepare: %v", err)
	}
	summary := agent.VisionSummaryFromContext(ctx)
	session.Add(provider.Message{Role: provider.RoleUser, Content: first, RawContent: "first", VisionSummary: summary})
	second, _, err := c.prepareVisionTurn(context.Background(), "follow-up", images)
	if err != nil {
		t.Fatalf("second prepare: %v", err)
	}
	if calls != 1 || !contains(second, "Revenue 42") {
		t.Fatalf("calls=%d second=%q", calls, second)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
