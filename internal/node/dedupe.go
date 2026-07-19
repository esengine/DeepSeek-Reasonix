package node

import "sync"

// DedupeEntry stores the first successful response for a requestId.
type DedupeEntry struct {
	Response []byte
}

// RequestDedupe is a bounded map of requestId → first response.
// Retried mobile write commands must not re-execute side effects.
type RequestDedupe struct {
	mu      sync.Mutex
	max     int
	order   []string
	entries map[string]DedupeEntry
}

// NewRequestDedupe retains at most max entries (min 1).
func NewRequestDedupe(max int) *RequestDedupe {
	if max < 1 {
		max = 1
	}
	return &RequestDedupe{
		max:     max,
		entries: make(map[string]DedupeEntry),
	}
}

// Lookup returns a prior response when the request was already processed.
func (d *RequestDedupe) Lookup(requestID string) (DedupeEntry, bool) {
	if requestID == "" {
		return DedupeEntry{}, false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	e, ok := d.entries[requestID]
	return e, ok
}

// Remember stores the first response for requestID. Later Remember calls for
// the same id are ignored so concurrent retries cannot overwrite the winner.
func (d *RequestDedupe) Remember(requestID string, response []byte) {
	if requestID == "" {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, exists := d.entries[requestID]; exists {
		return
	}
	cp := make([]byte, len(response))
	copy(cp, response)
	d.entries[requestID] = DedupeEntry{Response: cp}
	d.order = append(d.order, requestID)
	for len(d.order) > d.max {
		old := d.order[0]
		d.order = d.order[1:]
		delete(d.entries, old)
	}
}
