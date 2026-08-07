package provider

import (
	"context"
	"errors"
	"time"
)

// ErrCompactionUnsupported is returned by a NativeCompactor when the provider
// or model has no native compaction endpoint. Agents treat it as a capability
// miss and fall back to ordinary summarization; it is not a request failure.
var ErrCompactionUnsupported = errors.New("provider: native compaction unsupported")

// NativeCompactor is an optional Provider capability. Providers that do not
// implement it are handled by the agent's summarize fallback.
type NativeCompactor interface {
	Compact(ctx context.Context, req CompactionRequest) (CompactionResult, error)
}

// CompactionRequest is a provider-neutral native compaction call.
type CompactionRequest struct {
	Messages        []Message
	Instructions    string
	MaxOutputTokens int
	PromptCacheKey  string
	SessionID       string
}

// CompactionResult is the provider-neutral outcome of a native compaction call.
// Projection and Summary must not both be empty.
type CompactionResult struct {
	Projection        []Message
	Summary           string
	Usage             *Usage
	ProviderRequestID string
}

// Valid reports whether the result carries a usable projection and/or summary.
func (r CompactionResult) Valid() bool {
	return len(r.Projection) > 0 || r.Summary != ""
}

// CompactionCapabilities describes model/provider compaction and cache limits.
// Defaults come only from an explicit vendor/model capability table; unknown
// compatible gateways must not inherit a large shared default.
type CompactionCapabilities struct {
	NativeCompaction       bool
	MaxOutputTokens        int
	CompactionOutputTokens int
	CacheTTL               time.Duration
}

// CompactionCapabler is an optional Provider surface for capability lookup.
type CompactionCapabler interface {
	CompactionCapabilities() CompactionCapabilities
}

// AsNativeCompactor returns p when it implements NativeCompactor.
func AsNativeCompactor(p Provider) (NativeCompactor, bool) {
	if p == nil {
		return nil, false
	}
	nc, ok := p.(NativeCompactor)
	return nc, ok
}

// AsCompactionCapabler returns p when it implements CompactionCapabler.
func AsCompactionCapabler(p Provider) (CompactionCapabler, bool) {
	if p == nil {
		return nil, false
	}
	cc, ok := p.(CompactionCapabler)
	return cc, ok
}
