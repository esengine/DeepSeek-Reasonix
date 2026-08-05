package sidecar

import (
	"context"
	"sync"
	"testing"
	"time"

	"reasonix/internal/extension/protocol"
	"reasonix/internal/pluginpkg"
)

// recordingRouter captures routed provider stream notifications.
type recordingRouter struct {
	mu     sync.Mutex
	chunks []protocol.StreamChunkParams
	ends   []protocol.StreamEndParams
}

func (r *recordingRouter) RouteStreamChunk(p protocol.StreamChunkParams) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.chunks = append(r.chunks, p)
}

func (r *recordingRouter) RouteStreamEnd(p protocol.StreamEndParams) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ends = append(r.ends, p)
}

func (r *recordingRouter) counts() (int, int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.chunks), len(r.ends)
}

func openProviderStream(t *testing.T, client *Client, streamID string) {
	t.Helper()
	opened, err := client.ProviderStreamOpen(context.Background(), protocol.StreamOpenParams{
		StreamID:    streamID,
		ProviderRef: "plugin/fakeplugin/fake/x",
		Request:     protocol.ProviderRequest{Messages: []protocol.ProviderMessage{}, Tools: []protocol.ProviderToolSchema{}},
		SeqBase:     1,
	})
	if err != nil {
		t.Fatalf("ProviderStreamOpen: %v", err)
	}
	if !opened.Accepted {
		t.Fatal("ProviderStreamOpen was declined")
	}
}

// TestStreamRouterReceivesWireNotifications pins the stage 7 seam: inbound
// stream/chunk and stream/end notifications reach the installed router,
// decoded and addressed by stream ID.
func TestStreamRouterReceivesWireNotifications(t *testing.T) {
	recorder := &recordingRouter{}
	client := startFakeClient(t, func(rt *pluginpkg.RuntimeSpec) {
		rt.Env[fakeEnvMode] = "provider_stream"
	}, func(opts *ClientOptions) {
		opts.Streams = recorder
	})

	openProviderStream(t, client, "es_route")
	waitFor(t, "chunk and end routed", 5*time.Second, func() bool {
		chunks, ends := recorder.counts()
		return chunks == 1 && ends == 1
	})
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	if recorder.chunks[0].StreamID != "es_route" || recorder.chunks[0].Seq != 1 ||
		recorder.chunks[0].Chunk.Type != protocol.ChunkText || recorder.chunks[0].Chunk.Text != "wired" {
		t.Fatalf("routed chunk = %+v", recorder.chunks[0])
	}
	if recorder.ends[0].StreamID != "es_route" || recorder.ends[0].LastSeq != 1 {
		t.Fatalf("routed end = %+v", recorder.ends[0])
	}
}

// TestSetStreamRouterSwapsMidFlight: a router installed after start receives
// later notifications; the replaced one stops seeing them.
func TestSetStreamRouterSwapsMidFlight(t *testing.T) {
	first := &recordingRouter{}
	client := startFakeClient(t, func(rt *pluginpkg.RuntimeSpec) {
		rt.Env[fakeEnvMode] = "provider_stream"
	}, func(opts *ClientOptions) {
		opts.Streams = first
	})

	second := &recordingRouter{}
	client.SetStreamRouter(second)
	openProviderStream(t, client, "es_swapped")
	waitFor(t, "chunk routed to the swapped router", 5*time.Second, func() bool {
		chunks, _ := second.counts()
		return chunks == 1
	})
	if chunks, _ := first.counts(); chunks != 0 {
		t.Fatalf("replaced router saw %d chunks after the swap", chunks)
	}
}

func TestSetStreamRouterNilRestoresDropDefault(t *testing.T) {
	c := &Client{pluginID: "p", streams: dropStreamRouter{pluginID: "p"}}
	first := &recordingRouter{}
	c.SetStreamRouter(first)
	if c.streamRouter() != first {
		t.Fatal("SetStreamRouter did not install the router")
	}
	c.SetStreamRouter(nil)
	if _, ok := c.streamRouter().(dropStreamRouter); !ok {
		t.Fatalf("SetStreamRouter(nil) restored %T, want the drop default", c.streamRouter())
	}
}

// TestDisconnectedClosesWithServeLoop: the provider stream watchers' signal
// fires on an orderly shutdown too, so in-flight streams never hang.
func TestDisconnectedClosesWithServeLoop(t *testing.T) {
	client := startFakeClient(t, nil, nil)
	select {
	case <-client.Disconnected():
		t.Fatal("Disconnected closed on a live client")
	default:
	}
	if err := client.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	select {
	case <-client.Disconnected():
	case <-time.After(5 * time.Second):
		t.Fatal("Disconnected did not close after shutdown")
	}
}

func TestProviderCatalogRoundTrip(t *testing.T) {
	client := startFakeClient(t, nil, nil)
	providers, err := client.ProviderCatalog(context.Background())
	if err != nil {
		t.Fatalf("ProviderCatalog: %v", err)
	}
	if len(providers) != 0 {
		t.Fatalf("providers = %v, want the fake's empty catalog", providers)
	}
}

func TestProviderStreamCancelBestEffort(t *testing.T) {
	client := startFakeClient(t, nil, nil)
	// The fake answers {"cancelled":true}; the call must simply not wedge.
	client.ProviderStreamCancel("es_test")
}
