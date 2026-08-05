package agent

import (
	"context"
	"strings"

	"reasonix/internal/provider"
)

// responseFormatContextKey carries a per-turn structured-output request
// through Run's context (same pattern as runStepLimitContextKey). The agent
// reads it when building the provider.Request so a turn can ask for
// json_object output without changing the agent's persistent state.
type responseFormatContextKey struct{}

// WithResponseFormat attaches a structured-output format to a turn context.
// Empty format is a no-op returning ctx unchanged (byte-stable default path).
func WithResponseFormat(ctx context.Context, format string) context.Context {
	if strings.TrimSpace(format) == "" {
		return ctx
	}
	return context.WithValue(ctx, responseFormatContextKey{}, strings.TrimSpace(format))
}

// responseFormatFromContext returns the turn-scoped structured-output format
// (e.g. "json_object") or "" when none was requested.
func responseFormatFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	f, _ := ctx.Value(responseFormatContextKey{}).(string)
	return f
}

// ResponseFormatFromRequest is the exported form of responseFormatFromRequest:
// control tests assert the turn-bound format actually reaches the agent
// request path (review #7234 — format bound to turn, not global slot).
func ResponseFormatFromRequest(ctx context.Context) *provider.ResponseFormat {
	return responseFormatFromRequest(ctx)
}

// responseFormatFromRequest returns the turn-scoped structured-output format
// (e.g. "json_object") as a Request field, or nil when none was requested
// (nil keeps the wire byte-stable for prompt caching).
func responseFormatFromRequest(ctx context.Context) *provider.ResponseFormat {
	if f := responseFormatFromContext(ctx); f != "" {
		return &provider.ResponseFormat{Type: f}
	}
	return nil
}
