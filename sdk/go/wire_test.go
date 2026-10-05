package extension

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// TestHandlerPoolSaturated verifies the bounded inbound concurrency: with all
// 32 handler slots occupied, the next request is answered -32099 (server
// busy) while the parked requests still complete afterwards.
func TestHandlerPoolSaturated(t *testing.T) {
	release := make(chan struct{})
	interceptors := map[string]InterceptorFunc{
		"tool.before": func(ctx context.Context, _ string, _ json.RawMessage) (*InterceptResult, error) {
			select {
			case <-release:
			case <-ctx.Done():
			}
			return Continue(), nil
		},
	}
	host, _ := startFakeHost(t, basicHandler(), Options{Interceptors: interceptors})
	host.handshake(t)

	channels := make([]chan hostResponse, 0, maxConcurrentHandlers+1)
	for i := 0; i < maxConcurrentHandlers+1; i++ {
		_, ch := host.startRequest(MethodExtensionIntercept, InterceptParams{
			Event: EventToolBefore, Seq: uint64(i + 1), Payload: json.RawMessage(`{}`),
		})
		channels = append(channels, ch)
	}

	// Exactly one request — the one finding no handler slot — is rejected
	// immediately; the rest stay parked on release.
	busyIdx := -1
	deadline := time.Now().Add(5 * time.Second)
	for busyIdx < 0 && time.Now().Before(deadline) {
		for i, ch := range channels {
			select {
			case resp := <-ch:
				if resp.Err == nil || resp.Err.Code != CodeServerBusy {
					t.Fatalf("request %d: unexpected early response %+v", i, resp)
				}
				busyIdx = i
			default:
			}
		}
		if busyIdx < 0 {
			time.Sleep(5 * time.Millisecond)
		}
	}
	if busyIdx < 0 {
		t.Fatal("no request was answered server-busy")
	}
	for i, ch := range channels {
		if i == busyIdx {
			continue
		}
		select {
		case resp := <-ch:
			t.Fatalf("request %d: expected parked, got %+v", i, resp)
		default:
		}
	}

	close(release)
	for i, ch := range channels {
		if i == busyIdx {
			continue
		}
		select {
		case resp := <-ch:
			if resp.Err != nil {
				t.Fatalf("parked request %d errored: %+v", i, resp.Err)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("parked request %d did not complete after release", i)
		}
	}
}
