package boot

import (
	"context"
	"strings"

	"reasonix/internal/cosplay"
	"reasonix/internal/nilutil"
	"reasonix/internal/provider"
)

// providerBackend adapts a streaming provider.Provider to the cosplay
// ModelBackend surface (one non-streaming completion). The base request
// carries the selected model; each Complete() call swaps in fresh
// system+user messages so model-backed test generation and repair are
// stateless single turns.
type providerBackend struct {
	p   provider.Provider
	req provider.Request
}

// Complete implements cosplay.ModelBackend.
func (b providerBackend) Complete(ctx context.Context, system, user string, maxTokens int) (string, error) {
	if nilutil.IsNil(b.p) {
		return "", context.Canceled
	}
	req := b.req
	req.Messages = []provider.Message{
		{Role: provider.RoleSystem, Content: system},
		{Role: provider.RoleUser, Content: user},
	}
	if maxTokens > 0 {
		req.MaxTokens = maxTokens
	}
	chunks, err := b.p.Stream(ctx, req)
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	for ch := range chunks {
		if ch.Type == provider.ChunkText {
			sb.WriteString(ch.Text)
		}
	}
	return sb.String(), nil
}

// newModelBackend builds a cosplay.ModelBackend from the executor provider,
// or nil when no provider is available (then the offline template path is
// used). maxTokens<=0 leaves the provider default.
func newModelBackend(p provider.Provider, base provider.Request) cosplay.ModelBackend {
	if nilutil.IsNil(p) {
		return nil
	}
	return providerBackend{p: p, req: base}
}
