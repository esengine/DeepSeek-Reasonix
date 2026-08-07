package provider_test

import (
	"context"
	"errors"
	"testing"

	"reasonix/internal/provider"
	"reasonix/internal/provider/responses"
)

type unsupportedCompactor struct{}

func (unsupportedCompactor) Name() string { return "unsupported" }
func (unsupportedCompactor) Stream(context.Context, provider.Request) (<-chan provider.Chunk, error) {
	return nil, errors.New("not implemented")
}
func (unsupportedCompactor) Compact(context.Context, provider.CompactionRequest) (provider.CompactionResult, error) {
	return provider.CompactionResult{}, provider.ErrCompactionUnsupported
}

func TestNativeCompactorCapabilityMiss(t *testing.T) {
	var p provider.Provider = unsupportedCompactor{}
	nc, ok := provider.AsNativeCompactor(p)
	if !ok {
		t.Fatal("expected NativeCompactor")
	}
	_, err := nc.Compact(context.Background(), provider.CompactionRequest{
		Messages:       []provider.Message{{Role: provider.RoleUser, Content: "hi"}},
		PromptCacheKey: "ws|sess|model",
		SessionID:      "sess",
	})
	if !errors.Is(err, provider.ErrCompactionUnsupported) {
		t.Fatalf("err = %v, want ErrCompactionUnsupported", err)
	}
}

func TestResponsesCompactionCapabilities(t *testing.T) {
	p := responses.New(responses.Config{
		Name:    "deepseek",
		BaseURL: "https://api.deepseek.com",
		Model:   "deepseek-v4-flash",
	})
	cc, ok := provider.AsCompactionCapabler(p)
	if !ok {
		t.Fatal("responses client must expose CompactionCapabler")
	}
	caps := cc.CompactionCapabilities()
	if caps.NativeCompaction {
		t.Fatal("DeepSeek Responses must not claim native compaction yet")
	}
	if caps.CompactionOutputTokens != 4096 {
		t.Fatalf("CompactionOutputTokens = %d, want 4096", caps.CompactionOutputTokens)
	}
	if caps.MaxOutputTokens != provider.DefaultHighOutputTokens {
		t.Fatalf("MaxOutputTokens = %d, want %d", caps.MaxOutputTokens, provider.DefaultHighOutputTokens)
	}

	unknown := responses.New(responses.Config{
		Name:    "compat",
		BaseURL: "https://example.invalid/v1",
		Model:   "mystery",
	})
	ucaps := unknown.(provider.CompactionCapabler).CompactionCapabilities()
	if ucaps.MaxOutputTokens != 0 || ucaps.CompactionOutputTokens != 0 {
		t.Fatalf("unknown gateway must not inherit large defaults: %+v", ucaps)
	}
}

func TestCompactionResultValid(t *testing.T) {
	if (provider.CompactionResult{}).Valid() {
		t.Fatal("empty result must be invalid")
	}
	if !(provider.CompactionResult{Summary: "x"}).Valid() {
		t.Fatal("summary-only result must be valid")
	}
	if !(provider.CompactionResult{Projection: []provider.Message{{Role: provider.RoleUser, Content: "y"}}}).Valid() {
		t.Fatal("projection-only result must be valid")
	}
}
