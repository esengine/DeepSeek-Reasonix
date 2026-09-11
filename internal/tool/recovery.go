package tool

import (
	"context"
	"encoding/json"
)

// EffectInspection is evidence from the tool's authoritative sink. Absent is
// retry-safe only when Fenced proves an older attempt can no longer commit.
type EffectInspection struct {
	State   string // present | absent | unknown
	Fenced  bool
	Summary string
}

// EffectVerifier is optional. Unsupported tools remain unknown; generic shell
// commands and arbitrary MCP tools are never assumed idempotent.
type EffectVerifier interface {
	RecoveryScope() string // stable sink/account/resource identity, independent of a retry
	InspectEffect(context.Context, string, json.RawMessage) (EffectInspection, error)
}

type recoveryKey struct{}

// RecoveryIdempotencyKey is stable across explicit retries of one action.
func RecoveryIdempotencyKey(ctx context.Context) string {
	v, _ := ctx.Value(recoveryKey{}).(string)
	return v
}
func WithRecoveryIdempotencyKey(ctx context.Context, key string) context.Context {
	return context.WithValue(ctx, recoveryKey{}, key)
}
