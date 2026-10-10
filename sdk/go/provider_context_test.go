package extension

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

type contextProvider struct {
	*scriptProvider
	started chan context.Context
}

func (p *contextProvider) Stream(ctx context.Context, req StreamRequest) (<-chan StreamChunk, error) {
	p.started <- ctx
	return p.scriptProvider.Stream(ctx, req)
}

func TestProviderStreamContextEndsWithStream(t *testing.T) {
	for _, mode := range []string{"clean", "error", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			chunks := make(chan StreamChunk, 2)
			chunks <- TextChunk("partial")
			switch mode {
			case "clean":
				close(chunks)
			case "error":
				chunks <- ErrorChunk("upstream unavailable")
			}
			provider := &contextProvider{
				scriptProvider: &scriptProvider{makeChannel: func(StreamRequest) <-chan StreamChunk { return chunks }},
				started:        make(chan context.Context, 1),
			}
			host, _ := startFakeHost(t, providerHandler(), Options{Provider: provider})
			host.handshake(t)
			resp := host.request(MethodExtensionProviderStreamOpen, openStreamRequest("lifetime"))
			if resp.Err != nil {
				t.Fatalf("open: %+v", resp.Err)
			}
			ctx := <-provider.started
			if mode == "cancel" {
				host.nextNotification(MethodExtensionProviderStreamChunk)
				resp = host.request(MethodExtensionProviderStreamCancel, StreamCancelParams{StreamID: "lifetime"})
				if resp.Err != nil {
					t.Fatalf("cancel: %+v", resp.Err)
				}
			}
			end := host.waitStreamEnd()
			if end.LastSeq != 1 || end.Interrupted != (mode == "cancel") || (end.Error != "") != (mode == "error") {
				t.Fatalf("end = %+v", end)
			}
			select {
			case <-ctx.Done():
				if !errors.Is(ctx.Err(), context.Canceled) {
					t.Fatalf("stream context error = %v", ctx.Err())
				}
			case <-time.After(time.Second):
				t.Fatal("provider context remains active after stream/end")
			}
			resp = host.request(MethodExtensionProviderCatalog, ProviderCatalogParams{})
			if resp.Err != nil {
				t.Fatalf("connection did not survive stream completion: %+v", resp.Err)
			}
			_, ends := host.streamNotifications()
			if len(ends) != 1 {
				t.Fatalf("stream/end count = %d", len(ends))
			}
			var catalog ProviderCatalogResult
			if err := json.Unmarshal(resp.Result, &catalog); err != nil || catalog.Providers == nil || len(catalog.Providers) != 0 {
				t.Fatalf("catalog = %+v, err = %v", catalog, err)
			}
		})
	}
}
