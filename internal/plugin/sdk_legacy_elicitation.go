package plugin

import (
	"context"
	"encoding/json"
	"sync"

	"reasonix/internal/mcpinteraction"
)

type legacyElicitationCall struct {
	ctx    context.Context
	broker mcpinteraction.Broker
}

func (t *sdkSessionTransport) invokeManaged(ctx context.Context, managed *managedMCPSession, method string, params any) (json.RawMessage, error) {
	unregister := t.registerLegacyElicitationCall(ctx, managed.protocol, method)
	defer unregister()
	return invokeSDKMethod(ctx, managed.session, method, params)
}

// registerLegacyElicitationCall covers the push-style elicitation/create flow
// used before 2026-07-28. Legacy JSON-RPC requests arrive on the session
// context, not the originating tools/call context, so every active legacy call
// is tracked. The handler only routes a decision when exactly one call is in
// flight; ambiguity cancels instead of crossing tabs or headless callers.
func (t *sdkSessionTransport) registerLegacyElicitationCall(ctx context.Context, protocol, method string) func() {
	if method != "tools/call" || protocol == "" || protocol >= "2026-07-28" {
		return func() {}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	t.legacyElicitationMu.Lock()
	t.legacyElicitationNext++
	id := t.legacyElicitationNext
	if t.legacyElicitation == nil {
		t.legacyElicitation = map[uint64]legacyElicitationCall{}
	}
	t.legacyElicitation[id] = legacyElicitationCall{ctx: ctx, broker: mcpinteraction.FromContext(ctx)}
	t.legacyElicitationMu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			t.legacyElicitationMu.Lock()
			delete(t.legacyElicitation, id)
			t.legacyElicitationMu.Unlock()
		})
	}
}

func (t *sdkSessionTransport) unambiguousLegacyElicitation() (mcpinteraction.Broker, context.Context, bool) {
	t.legacyElicitationMu.Lock()
	defer t.legacyElicitationMu.Unlock()
	if len(t.legacyElicitation) != 1 {
		return nil, nil, false
	}
	for _, call := range t.legacyElicitation {
		if call.broker == nil || call.ctx == nil || call.ctx.Err() != nil {
			return nil, nil, false
		}
		return call.broker, call.ctx, true
	}
	return nil, nil, false
}
