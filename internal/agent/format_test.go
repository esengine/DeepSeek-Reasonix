package agent

import (
	"context"
	"testing"
)

func TestResponseFormatContextRoundTrip(t *testing.T) {
	ctx := WithResponseFormat(context.Background(), "json_object")
	if got := responseFormatFromContext(ctx); got != "json_object" {
		t.Fatalf("format = %q, want json_object", got)
	}
	// Empty format is a no-op: context stays byte-identical (nil read).
	if got := responseFormatFromContext(WithResponseFormat(ctx, "  ")); got != "json_object" {
		t.Fatalf("empty format must be ignored, got %q", got)
	}
	if got := responseFormatFromContext(context.Background()); got != "" {
		t.Fatalf("default ctx format = %q, want empty", got)
	}
	if got := responseFormatFromRequest(ctx); got == nil || got.Type != "json_object" {
		t.Fatalf("request format = %#v, want json_object", got)
	}
	if got := responseFormatFromRequest(context.Background()); got != nil {
		t.Fatalf("request format must be nil on plain ctx, got %#v", got)
	}
}
