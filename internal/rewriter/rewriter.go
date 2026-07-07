package rewriter

import (
	"context"
	"strings"
	"time"

	"reasonix/internal/provider"
)

// Rewriter defines the lightweight model rewrite interface.
// Short user inputs are rewritten into structured prompts to improve
// the main model's prefix-cache hit rate — the rewritten text feeds
// the same cache-stable system prompt, so the prefix stays unchanged.
type Rewriter interface {
	Rewrite(ctx context.Context, input string) (string, error)
}

// Config configures the lightweight rewriter.
type Config struct {
	Enabled   bool          `toml:"enabled"`
	Model     string        `toml:"model"`      // provider/model reference; empty disables rewriter
	Timeout   time.Duration `toml:"timeout"`    // per-call deadline; 0 = 3s
	MaxLength int           `toml:"max_length"` // inputs longer than this skip rewrite; 0 = 500 runes
}

// ProviderRewriter is the default implementation: calls a lightweight provider.
type ProviderRewriter struct {
	prov   provider.Provider
	cfg    Config
	prompt string
}

func NewProviderRewriter(prov provider.Provider, cfg Config) *ProviderRewriter {
	if cfg.Timeout == 0 {
		cfg.Timeout = 2 * time.Second
	}
	if cfg.MaxLength == 0 {
		cfg.MaxLength = 500
	}
	return &ProviderRewriter{
		prov: prov,
		cfg:  cfg,
		prompt: `Rewrite the user's input into a clear, structured instruction for an AI assistant. ` +
			`Keep the same meaning, but make it precise and actionable. ` +
			`Output only the rewritten text, no extra commentary.`,
	}
}

func (r *ProviderRewriter) Rewrite(ctx context.Context, input string) (string, error) {
	if !r.cfg.Enabled {
		return input, nil
	}
	if len([]rune(input)) > r.cfg.MaxLength {
		return input, nil
	}
	ctx, cancel := context.WithTimeout(ctx, r.cfg.Timeout)
	defer cancel()

	req := provider.Request{
		Messages: []provider.Message{
			{Role: provider.RoleSystem, Content: r.prompt},
			{Role: provider.RoleUser, Content: input},
		},
		Temperature: provider.OptionalTemperature(0.3),
	}
	ch, err := r.prov.Stream(ctx, req)
	if err != nil {
		return input, nil // silent fallback
	}
	var result strings.Builder
	for chunk := range ch {
		if chunk.Type == provider.ChunkText {
			result.WriteString(chunk.Text)
		}
	}
	rewritten := strings.TrimSpace(result.String())
	if rewritten == "" {
		return input, nil
	}
	return rewritten, nil
}
