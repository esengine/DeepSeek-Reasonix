package extension

import (
	"strings"
	"sync"
)

// MessageSendGuard prevents duplicate irreversible message-send receipts for
// the same (generation, messageID) pair within a process.
type MessageSendGuard struct {
	mu   sync.Mutex
	seen map[string]struct{}
}

// DefaultMessageSendGuard belongs to the compatibility runtime owner.
var DefaultMessageSendGuard = DefaultRuntimeOwner.Messages

// NewMessageSendGuard returns an empty guard.
func NewMessageSendGuard() *MessageSendGuard {
	return &MessageSendGuard{seen: make(map[string]struct{})}
}

// TryRecord returns true when this is the first observation of messageID for
// gen and records it. Second calls return false (duplicate send protection).
func (g *MessageSendGuard) TryRecord(gen uint64, messageID string) bool {
	if g == nil || messageID == "" {
		return true
	}
	key := itoaU64(gen) + ":" + messageID
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, ok := g.seen[key]; ok {
		return false
	}
	g.seen[key] = struct{}{}
	return true
}

// Forget removes a message key when its matching receipt leaves retention.
func (g *MessageSendGuard) Forget(gen uint64, messageID string) {
	if g == nil || messageID == "" {
		return
	}
	g.mu.Lock()
	delete(g.seen, itoaU64(gen)+":"+messageID)
	g.mu.Unlock()
}

// ForgetReceipt keeps dedup retention aligned with ReceiptStore eviction.
func (g *MessageSendGuard) ForgetReceipt(r EffectReceipt) {
	const prefix = "message-sent:"
	if g == nil || !strings.HasPrefix(r.ID, prefix) {
		return
	}
	messageID := r.Component
	if messageID == "" {
		messageID = strings.TrimPrefix(r.ID, prefix)
		messageID = strings.TrimSuffix(messageID, "#gen-"+itoaU64(r.Generation))
	}
	g.Forget(r.Generation, messageID)
}

// RecordMessageSentOnce records an irreversible message-send receipt only on
// the first observation of (generation, messageID).
func RecordMessageSentOnce(generation uint64, messageID, owner string) bool {
	return RuntimeOwnerOrDefault(nil).RecordMessageSentOnce(generation, messageID, owner)
}
