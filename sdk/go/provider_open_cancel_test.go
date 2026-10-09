package extension

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

type openingProvider struct {
	entered       chan context.Context
	release       chan struct{}
	channelOnStop bool
}

func (*openingProvider) Catalog(context.Context) ([]ProviderDescriptor, error) {
	return nil, nil
}

func (p *openingProvider) Stream(ctx context.Context, _ StreamRequest) (<-chan StreamChunk, error) {
	p.entered <- ctx
	select {
	case <-ctx.Done():
		if !p.channelOnStop {
			return nil, ctx.Err()
		}
	case <-p.release:
	}
	return make(chan StreamChunk), nil
}

func TestProviderCancelDuringStreamOpen(t *testing.T) {
	for _, channelOnStop := range []bool{false, true} {
		name := "error"
		if channelOnStop {
			name = "channel"
		}
		t.Run(name, func(t *testing.T) {
			p := &openingProvider{entered: make(chan context.Context, 1), release: make(chan struct{}), channelOnStop: channelOnStop}
			host, _ := startFakeHost(t, providerHandler(), Options{Provider: p})
			host.handshake(t)
			_, opened := host.startRequest(MethodExtensionProviderStreamOpen, openStreamRequest("opening"))
			var streamCtx context.Context
			select {
			case streamCtx = <-p.entered:
			case <-time.After(time.Second):
				t.Fatal("provider did not receive the stream open")
			}
			resp := host.request(MethodExtensionProviderStreamCancel, StreamCancelParams{StreamID: "opening"})
			var cancelled StreamCancelResult
			if err := json.Unmarshal(resp.Result, &cancelled); err != nil || !cancelled.Cancelled {
				t.Errorf("cancel during open = %+v, error=%+v, decode=%v", cancelled, resp.Err, err)
			}
			select {
			case <-streamCtx.Done():
			case <-time.After(200 * time.Millisecond):
				t.Error("processed cancel did not cancel the opening provider context")
			}
			close(p.release)
			select {
			case resp = <-opened:
			case <-time.After(time.Second):
				t.Fatal("stream open did not finish")
			}
			if !channelOnStop && streamCtx.Err() != nil {
				if resp.Err == nil {
					t.Fatal("provider's cancelled open should return its existing error response")
				}
				host.request(MethodExtensionProviderCatalog, ProviderCatalogParams{})
				return
			}
			if resp.Err != nil {
				t.Fatalf("channel open failed: %+v", resp.Err)
			}
			if streamCtx.Err() == nil {
				host.request(MethodExtensionProviderStreamCancel, StreamCancelParams{StreamID: "opening"})
			}
			end := host.waitStreamEnd()
			chunks, ends := host.streamNotifications()
			if !end.Interrupted || end.LastSeq != 0 || len(chunks) != 0 || len(ends) != 1 {
				t.Fatalf("cancelled open end=%+v chunks=%d ends=%d", end, len(chunks), len(ends))
			}
		})
	}
}

type streamOpenFunc func(context.Context, StreamRequest) (<-chan StreamChunk, error)

func (streamOpenFunc) Catalog(context.Context) ([]ProviderDescriptor, error) { return nil, nil }

func (f streamOpenFunc) Stream(ctx context.Context, req StreamRequest) (<-chan StreamChunk, error) {
	return f(ctx, req)
}

func TestFailedProviderOpenReleasesStreamID(t *testing.T) {
	for _, failure := range []string{"error", "nil channel", "panic"} {
		t.Run(failure, func(t *testing.T) {
			var calls atomic.Int64
			p := streamOpenFunc(func(context.Context, StreamRequest) (<-chan StreamChunk, error) {
				if calls.Add(1) == 1 {
					switch failure {
					case "error":
						return nil, errors.New("fixture open failed")
					case "nil channel":
						return nil, nil
					case "panic":
						panic("fixture open panic")
					}
				}
				return make(chan StreamChunk), nil
			})
			host, _ := startFakeHost(t, providerHandler(), Options{Provider: p})
			host.handshake(t)
			resp := host.request(MethodExtensionProviderStreamOpen, openStreamRequest("retry"))
			if resp.Err == nil {
				t.Fatal("expected the first open to fail")
			}
			resp = host.request(MethodExtensionProviderStreamCancel, StreamCancelParams{StreamID: "retry"})
			var cancelled StreamCancelResult
			if err := json.Unmarshal(resp.Result, &cancelled); err != nil || cancelled.Cancelled {
				t.Fatalf("failed stream still registered: %+v, decode=%v", cancelled, err)
			}
			resp = host.request(MethodExtensionProviderStreamOpen, openStreamRequest("retry"))
			if resp.Err != nil {
				t.Fatalf("failed stream id not released: %+v", resp.Err)
			}
			host.request(MethodExtensionProviderStreamCancel, StreamCancelParams{StreamID: "retry"})
			if end := host.waitStreamEnd(); !end.Interrupted {
				t.Fatalf("retry end=%+v", end)
			}
		})
	}
}
