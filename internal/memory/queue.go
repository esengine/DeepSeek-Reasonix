package memory

import (
	"context"
	"encoding/json"
	"strings"
)

// Queue receives a one-line note about a memory change a tool just made, so the
// controller can fold it into the current turn — taking effect this session
// without touching the cache-stable system prefix. The remember/forget tools
// read it from their call context the same way background tools read the job
// manager.
type Queue interface{ QueueMemory(note string) }

// maxNoteBodyRunes caps how much of a fact's body may ride the turn tail inside
// <memory-update> after a save. The body is already visible to the model as the
// content it just wrote (remember tool) or is one `memory` read away (desktop
// save), so beyond this point the note only needs enough to identify the fact.
// Short bodies are kept whole — the pre-existing behavior — so this only trims
// the pathological case where a max-size auto-memory (6000 runes) would
// otherwise inflate the next user turn by thousands of tokens.
const maxNoteBodyRunes = 400

// TrimMemoryNoteBody returns the portion of a fact body safe to embed in a
// turn-tail memory note. Rune-safe for CJK and other multi-byte text.
func TrimMemoryNoteBody(body string) string {
	runes := []rune(strings.TrimSpace(body))
	if len(runes) <= maxNoteBodyRunes {
		return string(runes)
	}
	return string(runes[:maxNoteBodyRunes]) + "…"
}

type autoMemoryWriteClaimer interface {
	ClaimAutoMemoryWrite(args json.RawMessage) bool
}

type queueKey struct{}

// WithQueue stamps q onto ctx for the remember/forget tools to find.
func WithQueue(ctx context.Context, q Queue) context.Context {
	return context.WithValue(ctx, queueKey{}, q)
}

// QueueFromContext returns the memory queue the agent stamped, if any.
func QueueFromContext(ctx context.Context) (Queue, bool) {
	q, ok := ctx.Value(queueKey{}).(Queue)
	return q, ok && q != nil
}

// ClaimAutoMemoryWriteFromContext consumes a host-issued create-only grant.
// Manual/approved writes have no claim and retain the legacy update behavior.
func ClaimAutoMemoryWriteFromContext(ctx context.Context, args json.RawMessage) bool {
	q, ok := QueueFromContext(ctx)
	if !ok {
		return false
	}
	claimer, ok := q.(autoMemoryWriteClaimer)
	return ok && claimer.ClaimAutoMemoryWrite(args)
}
