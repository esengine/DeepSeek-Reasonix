package dispatch

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"reasonix/internal/extension/protocol"
)

// recordedCall captures one intercept or event call a fake client received.
type recordedCall struct {
	event   protocol.InterceptEvent
	payload json.RawMessage
}

// fakeClient is a scriptable dispatch.Client for tests. Nil hooks answer
// continue (intercept) or success (notify).
type fakeClient struct {
	mu          sync.Mutex
	interceptFn func(event protocol.InterceptEvent, payload json.RawMessage) (protocol.InterceptResult, error)
	notifyFn    func(event protocol.InterceptEvent, payload json.RawMessage) error
	intercepts  []recordedCall
	notifies    []recordedCall
}

func (f *fakeClient) Intercept(_ context.Context, event protocol.InterceptEvent, payload json.RawMessage, _ time.Duration) (protocol.InterceptResult, error) {
	f.mu.Lock()
	f.intercepts = append(f.intercepts, recordedCall{event: event, payload: append(json.RawMessage(nil), payload...)})
	fn := f.interceptFn
	f.mu.Unlock()
	if fn == nil {
		return protocol.InterceptResult{Decision: protocol.DecisionContinue}, nil
	}
	return fn(event, payload)
}

func (f *fakeClient) TryNotifyEvent(event protocol.InterceptEvent, payload json.RawMessage) error {
	f.mu.Lock()
	f.notifies = append(f.notifies, recordedCall{event: event, payload: append(json.RawMessage(nil), payload...)})
	fn := f.notifyFn
	f.mu.Unlock()
	if fn == nil {
		return nil
	}
	return fn(event, payload)
}

func (f *fakeClient) interceptCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.intercepts)
}

func (f *fakeClient) notifyCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.notifies)
}

// observedPayloads returns copies of every payload Intercept was called with.
func (f *fakeClient) observedPayloads() []json.RawMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]json.RawMessage, len(f.intercepts))
	for i, call := range f.intercepts {
		out[i] = call.payload
	}
	return out
}

// warnRecorder collects Options.Warn messages, safe for concurrent use.
type warnRecorder struct {
	mu   sync.Mutex
	msgs []string
}

func (w *warnRecorder) warn(msg string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.msgs = append(w.msgs, msg)
}

func (w *warnRecorder) count() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.msgs)
}

func (w *warnRecorder) contains(substr string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, msg := range w.msgs {
		if strings.Contains(msg, substr) {
			return true
		}
	}
	return false
}
