//go:build live

package openai

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"reasonix/internal/provider"
)

// TestLiveADCMode is an env-gated end-to-end probe of the "adc" auth mode
// against a real OpenAI-compatible endpoint that accepts ADC bearers —
// Vertex AI's OpenAI endpoint:
//
//	REASONIX_ADC_BASE_URL=https://aiplatform.googleapis.com/v1/projects/<p>/locations/global/endpoints/openapi \
//	REASONIX_ADC_MODEL=google/gemini-3.6-flash \
//	go test -tags live ./internal/provider/openai/ -run TestLiveADCMode -v -count=1
//
// On GCE the metadata server supplies credentials with no static key anywhere.
func TestLiveADCMode(t *testing.T) {
	base := os.Getenv("REASONIX_ADC_BASE_URL")
	if base == "" {
		t.Skip("REASONIX_ADC_BASE_URL not set — skipping live ADC probe")
	}
	model := os.Getenv("REASONIX_ADC_MODEL")
	if model == "" {
		model = "google/gemini-2.5-flash"
	}
	p, err := New(provider.Config{
		Name:    "vertex-adc",
		BaseURL: base,
		Model:   model,
		Extra:   map[string]any{"auth": "adc"},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	ch, err := p.Stream(ctx, provider.Request{
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "Reply with exactly: ADC-LIVE-OK"}},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	var text string
	for evt := range ch {
		if evt.Err != nil {
			t.Fatalf("stream error: %v", evt.Err)
		}
		text += evt.Text
	}
	if !strings.Contains(text, "ADC-LIVE-OK") {
		t.Fatalf("reply = %q, want it to contain ADC-LIVE-OK", text)
	}
}
