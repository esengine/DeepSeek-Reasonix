package extension

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestHostHelpersRespectCancelledContexts(t *testing.T) {
	for _, helper := range []struct {
		name   string
		method string
		call   func(context.Context, string) error
	}{
		{"publish", MethodHostUIPublish, func(ctx context.Context, _ string) error {
			return (HostUI{}).PublishStatus(ctx, "sess-1", 7, "status-1", UIStatusPayload{Label: "Ready"})
		}},
		{"prompt", MethodHostUIRequest, func(ctx context.Context, _ string) error {
			_, err := (HostUI{}).RequestConfirm(ctx, "sess-1", 7, "confirm-1", "Continue?")
			return err
		}},
		{"content", MethodHostContentRead, func(ctx context.Context, ref string) error {
			_, err := ReadContentRef(ctx, ref)
			return err
		}},
	} {
		for _, mode := range []string{"cancelled", "deadline", "active"} {
			t.Run(helper.name+"/"+mode, func(t *testing.T) {
				store := newContentStore()
				ref, _ := store.put("fixture content")
				callErr := make(chan error, 1)
				opts := Options{Interceptors: map[string]InterceptorFunc{
					"tool.before": func(ctx context.Context, _ string, _ json.RawMessage) (*InterceptResult, error) {
						if mode == "cancelled" {
							cancelled, cancel := context.WithCancel(ctx)
							cancel()
							ctx = cancelled
						} else if mode == "deadline" {
							<-ctx.Done()
						}
						callErr <- helper.call(ctx, ref)
						return Continue(), nil
					},
				}}
				host, _ := startFakeHost(t, basicHandler(), opts)
				host.onRequest(MethodHostUIPublish, func(json.RawMessage) (any, *hostError) { return UIPublishResult{Accepted: true}, nil })
				host.onRequest(MethodHostUIRequest, func(json.RawMessage) (any, *hostError) {
					return UIRequestResult{Values: map[string]any{"value": true}}, nil
				})
				host.onRequest(MethodHostContentRead, store.handler)
				host.handshake(t)
				timeout := 0
				if mode == "deadline" {
					timeout = 5
				}
				host.request(MethodExtensionIntercept, InterceptParams{Event: EventToolBefore, Seq: 1, Payload: json.RawMessage(`{}`), TimeoutMillis: timeout})
				err := <-callErr
				want := error(nil)
				requests := 1
				if mode == "cancelled" {
					want = context.Canceled
					requests = 0
				}
				if mode == "deadline" {
					want = context.DeadlineExceeded
					requests = 0
				}
				if !errors.Is(err, want) {
					t.Errorf("helper error = %v, want %v", err, want)
				}
				host.handlersMu.Lock()
				got := len(host.requestLog[helper.method])
				host.handlersMu.Unlock()
				if got != requests {
					t.Errorf("host received %d %s requests, want %d", got, helper.method, requests)
				}
			})
		}
	}
}
