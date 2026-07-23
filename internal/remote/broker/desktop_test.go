package broker

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/provider"
	"reasonix/internal/remote/protocol"
	"reasonix/internal/rpcwire"
)

func TestDesktopBrokerUnavailableUntilActivatedAndAfterClose(t *testing.T) {
	var catalogCalls atomic.Int32
	conn := rpcwire.NewConn(strings.NewReader(""), io.Discard, rpcwire.Options{})
	d, err := Attach(conn, Options{
		Catalog: func(context.Context, map[string]struct{}) ([]protocol.BrokerProviderDescriptor, error) {
			catalogCalls.Add(1)
			return nil, nil
		},
		Open: func(context.Context, string, string, provider.Request) (<-chan provider.Chunk, error) {
			t.Fatal("open called before activation")
			return nil, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.handleCatalog(context.Background(), json.RawMessage(`{}`)); rpcCode(err) != ErrorNotReady {
		t.Fatalf("pre-activation catalog error = %v", err)
	}
	if catalogCalls.Load() != 0 {
		t.Fatal("catalog source ran before activation")
	}
	if err := d.Activate(); err != nil {
		t.Fatal(err)
	}
	d.Close()
	d.Close()
	if err := d.Activate(); err == nil {
		t.Fatal("closed Broker reactivated")
	}
}

func TestDesktopBrokerAuthorizationMustSucceedBeforeActivation(t *testing.T) {
	var allow atomic.Bool
	conn := rpcwire.NewConn(strings.NewReader(""), io.Discard, rpcwire.Options{})
	d, err := Attach(conn, Options{
		Catalog: func(context.Context, map[string]struct{}) ([]protocol.BrokerProviderDescriptor, error) {
			return nil, nil
		},
		Open: func(context.Context, string, string, provider.Request) (<-chan provider.Chunk, error) {
			return nil, nil
		},
		Authorize: func() error {
			if !allow.Load() {
				return errors.New("peer identity mismatch")
			}
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Activate(); err == nil {
		t.Fatal("Broker activated before peer authorization")
	}
	if _, err := d.handleCatalog(context.Background(), json.RawMessage(`{}`)); rpcCode(err) != ErrorNotReady {
		t.Fatalf("catalog after rejected authorization error = %v, want not ready", err)
	}
	allow.Store(true)
	if err := d.Activate(); err != nil {
		t.Fatalf("Activate after peer authorization: %v", err)
	}
	if _, err := d.handleCatalog(context.Background(), json.RawMessage(`{}`)); err != nil {
		t.Fatalf("catalog after authorization: %v", err)
	}
}

func TestDescriptorFromProviderPreservesCompactionAndPricingMetadata(t *testing.T) {
	descriptor := DescriptorFromProvider(
		"local/model", "Local", "model", stubProvider{}, []string{"high"}, "high", true,
		1_000_000, &provider.Pricing{CacheHit: 0.1, Input: 1.25, Output: 4.5, Currency: "$"},
	)
	if descriptor.ContextWindow != 1_000_000 || descriptor.PricingCurrency != "$" ||
		descriptor.CacheHitPerMillion != 0.1 || descriptor.InputPerMillion != 1.25 || descriptor.OutputPerMillion != 4.5 {
		t.Fatalf("descriptor metadata = %+v", descriptor)
	}
}

func TestDesktopBrokerNeverReturnsProviderErrorDetails(t *testing.T) {
	const secret = "sk-provider-secret-canary"
	conn := rpcwire.NewConn(strings.NewReader(""), io.Discard, rpcwire.Options{})
	d, err := Attach(conn, Options{
		Catalog: func(context.Context, map[string]struct{}) ([]protocol.BrokerProviderDescriptor, error) {
			return nil, errors.New("Authorization: Bearer " + secret)
		},
		Open: func(context.Context, string, string, provider.Request) (<-chan provider.Chunk, error) {
			return nil, errors.New("endpoint=https://secret.invalid key=" + secret)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Activate(); err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	_, catalogErr := d.handleCatalog(context.Background(), json.RawMessage(`{}`))
	openRaw, _ := json.Marshal(protocol.BrokerStreamOpenParams{
		StreamID: "s", ProviderRef: "p",
		Request: protocol.BrokerProviderRequestFromProvider(provider.Request{}),
	})
	_, openErr := d.handleStreamOpen(context.Background(), openRaw)
	for _, got := range []error{catalogErr, openErr} {
		if got == nil || strings.Contains(got.Error(), secret) || strings.Contains(got.Error(), "secret.invalid") {
			t.Fatalf("unsafe Broker error: %v", got)
		}
	}
}

func TestDesktopBrokerConcurrentCloseIsIdempotent(t *testing.T) {
	conn := rpcwire.NewConn(strings.NewReader(""), io.Discard, rpcwire.Options{})
	d, err := Attach(conn, Options{
		Catalog: func(context.Context, map[string]struct{}) ([]protocol.BrokerProviderDescriptor, error) {
			return nil, nil
		},
		Open: func(context.Context, string, string, provider.Request) (<-chan provider.Chunk, error) {
			return make(chan provider.Chunk), nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Activate(); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d.Close()
		}()
	}
	wg.Wait()
	if err := d.Activate(); err == nil {
		t.Fatal("closed Broker reactivated")
	}
}

func rpcCode(err error) int {
	var rpcErr *rpcwire.RPCError
	if errors.As(err, &rpcErr) {
		return rpcErr.Code
	}
	return 0
}

type pipePair struct {
	clientR io.Reader
	clientW io.Writer
	hostR   io.Reader
	hostW   io.Writer
}

func newPipePair() pipePair {
	c2hR, c2hW := io.Pipe()
	h2cR, h2cW := io.Pipe()
	return pipePair{
		clientR: h2cR,
		clientW: c2hW,
		hostR:   c2hR,
		hostW:   h2cW,
	}
}

type stubProvider struct {
	chunks []provider.Chunk
}

func (s stubProvider) Name() string { return "stub" }
func (s stubProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, len(s.chunks))
	go func() {
		defer close(ch)
		for _, c := range s.chunks {
			select {
			case <-ctx.Done():
				return
			case ch <- c:
			}
		}
	}()
	return ch, nil
}
func (s stubProvider) RequiresToolCallReasoning() bool { return true }

func TestDesktopBrokerCatalogAndStreamRoundTrip(t *testing.T) {
	pipes := newPipePair()
	desktopConn := rpcwire.NewConn(pipes.clientR, pipes.clientW, rpcwire.Options{
		Name: "desktop", StrictJSONRPC: true, MaxInboundBytes: 1 << 20, MaxOutboundBytes: 1 << 20,
	})
	hostConn := rpcwire.NewConn(pipes.hostR, pipes.hostW, rpcwire.Options{
		Name: "host", StrictJSONRPC: true, MaxInboundBytes: 1 << 20, MaxOutboundBytes: 1 << 20,
	})

	gotChunks := map[int64]provider.Chunk{}
	var end protocol.BrokerStreamEndParams
	var mu sync.Mutex
	hostConn.HandleNotify(string(protocol.MethodBrokerStreamChunk), func(ctx context.Context, params json.RawMessage) {
		var p protocol.BrokerStreamChunkParams
		if err := json.Unmarshal(params, &p); err != nil {
			t.Errorf("chunk params: %v", err)
			return
		}
		if p.StreamID != "s1" {
			t.Errorf("chunk stream = %q, want s1", p.StreamID)
			return
		}
		mu.Lock()
		gotChunks[p.Seq] = p.Chunk.ProviderChunk()
		mu.Unlock()
	})
	hostConn.HandleNotify(string(protocol.MethodBrokerStreamEnd), func(ctx context.Context, params json.RawMessage) {
		mu.Lock()
		defer mu.Unlock()
		_ = json.Unmarshal(params, &end)
	})

	prov := stubProvider{chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "hi"},
		{Type: provider.ChunkText, Text: " there"},
	}}
	gotEffort := ""
	d, err := Attach(desktopConn, Options{
		Catalog: func(ctx context.Context, allowed map[string]struct{}) ([]protocol.BrokerProviderDescriptor, error) {
			return []protocol.BrokerProviderDescriptor{
				DescriptorFromProvider("deepseek/chat", "DeepSeek", "chat", prov, nil, "", false, 128_000, &provider.Pricing{CacheHit: 0.1, Input: 1, Output: 2, Currency: "$"}),
			}, nil
		},
		Open: func(ctx context.Context, ref, effort string, req provider.Request) (<-chan provider.Chunk, error) {
			if ref != "deepseek/chat" {
				t.Fatalf("ref = %q", ref)
			}
			gotEffort = effort
			// Request must round-trip byte-equivalent for cache safety.
			return prov.Stream(ctx, req)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Activate(); err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go desktopConn.Serve(ctx)
	go hostConn.Serve(ctx)

	// Catalog.
	raw, err := hostConn.Request(ctx, string(protocol.MethodBrokerCatalog), protocol.BrokerCatalogParams{})
	if err != nil {
		t.Fatalf("catalog: %v", err)
	}
	var cat protocol.BrokerCatalogResult
	if err := json.Unmarshal(raw, &cat); err != nil {
		t.Fatal(err)
	}
	if len(cat.Providers) != 1 || !cat.Providers[0].ToolCallReasoning {
		t.Fatalf("catalog = %+v", cat.Providers)
	}

	// Stream with a structured request that must survive JSON marshal.
	req := provider.Request{
		Messages: []provider.Message{{Role: provider.RoleUser, Content: "hello"}},
	}
	openRaw, err := hostConn.Request(ctx, string(protocol.MethodBrokerStreamOpen), protocol.BrokerStreamOpenParams{
		StreamID: "s1", ProviderRef: "deepseek/chat", Effort: "high", Request: protocol.BrokerProviderRequestFromProvider(req),
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	var open protocol.BrokerStreamOpenResult
	if err := json.Unmarshal(openRaw, &open); err != nil || !open.Accepted {
		t.Fatalf("open result = %s err=%v", openRaw, err)
	}
	if gotEffort != "high" {
		t.Fatalf("effort = %q, want high", gotEffort)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(gotChunks)
		ended := end.StreamID == "s1"
		mu.Unlock()
		if n >= 2 && ended {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(gotChunks) != 2 {
		t.Fatalf("chunks = %d", len(gotChunks))
	}
	if gotChunks[1].Text+gotChunks[2].Text != "hi there" {
		t.Fatalf("text = %q%q", gotChunks[1].Text, gotChunks[2].Text)
	}
	if end.Interrupted || end.Error != "" || end.LastSeq != 2 {
		t.Fatalf("end = %+v", end)
	}
}

func TestDesktopBrokerCancelsStream(t *testing.T) {
	pipes := newPipePair()
	desktopConn := rpcwire.NewConn(pipes.clientR, pipes.clientW, rpcwire.Options{
		Name: "desktop", StrictJSONRPC: true, MaxInboundBytes: 1 << 20, MaxOutboundBytes: 1 << 20,
	})
	hostConn := rpcwire.NewConn(pipes.hostR, pipes.hostW, rpcwire.Options{
		Name: "host", StrictJSONRPC: true, MaxInboundBytes: 1 << 20, MaxOutboundBytes: 1 << 20,
	})

	started := make(chan struct{})
	d, err := Attach(desktopConn, Options{
		Catalog: func(context.Context, map[string]struct{}) ([]protocol.BrokerProviderDescriptor, error) {
			return nil, nil
		},
		Open: func(ctx context.Context, ref, _ string, req provider.Request) (<-chan provider.Chunk, error) {
			ch := make(chan provider.Chunk)
			go func() {
				defer close(ch)
				close(started)
				<-ctx.Done()
			}()
			return ch, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Activate(); err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go desktopConn.Serve(ctx)
	go hostConn.Serve(ctx)

	if _, err := hostConn.Request(ctx, string(protocol.MethodBrokerStreamOpen), protocol.BrokerStreamOpenParams{
		StreamID: "c1", ProviderRef: "stub/m", Request: protocol.BrokerProviderRequestFromProvider(provider.Request{Messages: []provider.Message{{Role: provider.RoleUser, Content: "x"}}}),
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("stream did not start")
	}
	raw, err := hostConn.Request(ctx, string(protocol.MethodBrokerStreamCancel), protocol.BrokerStreamCancelParams{StreamID: "c1"})
	if err != nil {
		t.Fatal(err)
	}
	var res protocol.BrokerStreamCancelResult
	if err := json.Unmarshal(raw, &res); err != nil || !res.Cancelled {
		t.Fatalf("cancel = %s", raw)
	}
}
