package extension

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestInterceptBudgetIncludesExternalizedContent(t *testing.T) {
	for _, tc := range []struct {
		name    string
		timeout int
		failed  bool
	}{
		{"bounded", 20, true},
		{"unbounded", 0, false},
		{"shared budget", 1000, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := newContentStore()
			payload := `{"text":"` + strings.Repeat("x", ExternalizeFieldBytes+1) + `"}`
			_, descriptor := store.put(payload)
			var called atomic.Bool
			var firstRead atomic.Pointer[time.Time]
			var got string
			host, _ := startFakeHost(t, basicHandler(), Options{Interceptors: map[string]InterceptorFunc{
				"input.receive": func(ctx context.Context, _ string, raw json.RawMessage) (*InterceptResult, error) {
					if tc.timeout > 0 {
						deadline, ok := ctx.Deadline()
						started := firstRead.Load()
						if !ok || started == nil || deadline.After(started.Add(time.Duration(tc.timeout)*time.Millisecond)) {
							t.Error("callback received a new budget after content reading")
						}
					}
					got = string(raw)
					called.Store(true)
					return Continue(), nil
				},
			}})
			host.onRequest(MethodHostContentRead, func(params json.RawMessage) (any, *hostError) {
				started := time.Now()
				firstRead.CompareAndSwap(nil, &started)
				time.Sleep(100 * time.Millisecond)
				return store.handler(params)
			})
			host.handshake(t)
			resp := host.request(MethodExtensionIntercept, InterceptParams{
				Event: EventInputReceive, Seq: 1, Payload: json.RawMessage("null"), TimeoutMillis: tc.timeout,
				Externalized: []ExternalizedField{descriptor},
			})
			if tc.failed {
				if resp.Err == nil || resp.Err.Code != DomainErrorCode {
					t.Fatalf("expected content-read timeout, got %+v", resp)
				}
				data, _ := resp.Err.Data.(ProtocolErrorData)
				if data.Reason != ErrInterceptTimeout || called.Load() {
					t.Fatalf("reason = %q, callback called = %v", data.Reason, called.Load())
				}
				return
			}
			if resp.Err != nil || !called.Load() || got != payload {
				t.Fatalf("unbounded intercept = %+v, called = %v, bytes = %d", resp, called.Load(), len(got))
			}
		})
	}
}
