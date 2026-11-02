package provider

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestProtocolCompatibilityPreservesSiblingAndProviderScope(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Hold") != "" {
			w.(http.Flusher).Flush()
			<-release
			_, _ = io.WriteString(w, "sibling complete")
			return
		}
		if r.ProtoMajor == 2 {
			panic(http.ErrAbortHandler)
		}
		_, _ = io.WriteString(w, "ok")
	}))
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()
	var once sync.Once
	defer once.Do(func() { close(release) })
	c := compatibilityTestClient(t, server)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	sibling, err := SendOnce(ctx, c, SendOptions{}, func(ctx context.Context) (*http.Request, error) {
		r, _ := http.NewRequestWithContext(ctx, http.MethodPost, server.URL, nil)
		r.Header.Set("X-Hold", "true")
		return r, nil
	})
	if err != nil || sibling.ProtoMajor != 2 {
		t.Fatalf("sibling=%v err=%v", sibling, err)
	}
	defer sibling.Body.Close()
	var reused bool
	traceCtx := httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{GotConn: func(i httptrace.GotConnInfo) { reused = i.Reused }})
	_, err = compatibilityPost(traceCtx, c, server.URL)
	if HTTP2TransportCode(err) != "INTERNAL_ERROR" || !reused {
		t.Fatalf("failure did not share sibling connection: reused=%t err=%v", reused, err)
	}
	_, err = compatibilityPost(ctx, c, server.URL)
	if HTTP2TransportCode(err) != "INTERNAL_ERROR" {
		t.Fatalf("one INTERNAL_ERROR switched protocol: %v", err)
	}
	resp, err := compatibilityPost(ctx, c, server.URL)
	if err != nil || resp.ProtoMajor != 1 {
		t.Fatalf("compatibility failed: %v", err)
	}
	resp.Body.Close()
	// Reconstructed clients share hints, but another configured provider does not.
	other := WithProtocolCompatibility(server.Client(), RequestIdentity{Provider: "other", Protocol: "openai"})
	other.Transport.(*protocolTransport).hints = c.Transport.(*protocolTransport).hints
	defer other.CloseIdleConnections()
	_, err = compatibilityPost(ctx, other, server.URL)
	if HTTP2TransportCode(err) != "INTERNAL_ERROR" {
		t.Fatalf("compatibility leaked across providers: %v", err)
	}
	rebuilt := WithProtocolCompatibility(server.Client(), RequestIdentity{Provider: t.Name(), Protocol: "openai"})
	rebuilt.Transport.(*protocolTransport).hints = c.Transport.(*protocolTransport).hints
	defer rebuilt.CloseIdleConnections()
	resp, err = compatibilityPost(ctx, rebuilt, server.URL)
	if err != nil || resp.ProtoMajor != 1 {
		t.Fatalf("provider reconstruction lost hint: %v", err)
	}
	resp.Body.Close()
	once.Do(func() { close(release) })
	b, err := io.ReadAll(sibling.Body)
	if err != nil || string(b) != "sibling complete" {
		t.Fatalf("fallback interrupted multiplexed sibling: %s %v", b, err)
	}
}

func TestProtocolCompatibilityProxyChangesDoNotInheritHints(t *testing.T) {
	// Even a failed route must be keyed by the actual selected proxy, not a
	// stale config label. This resolver alternates direct and a dead proxy.
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic(http.ErrAbortHandler)
	}))
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()
	c := compatibilityTestClient(t, server)
	primeCompatibility(t, c, server.URL)
	tr := c.Transport.(*protocolTransport)
	dead, _ := url.Parse("http://127.0.0.1:1")
	tr.proxy = func(*http.Request) (*url.URL, error) { return dead, nil }
	var mu sync.Mutex
	var reason string
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	ctx = WithRequestObserver(ctx, func(o RequestObservation) { mu.Lock(); reason = o.ProtocolFallbackReason; mu.Unlock() })
	_, err := compatibilityPost(ctx, c, server.URL)
	if err == nil {
		t.Fatal("expected dead proxy failure")
	}
	mu.Lock()
	defer mu.Unlock()
	if reason != "" {
		t.Fatal("direct connection's hint leaked into proxy path")
	}
}

func TestProtocolCompatibilityCancellationDoesNotPoisonHint(t *testing.T) {
	hints := freshProtocolHints()
	key := [32]byte{}
	now := time.Now()
	hints.failedHTTP2(key, "PROTOCOL_ERROR", hints.begin(), now)
	h := hints.selectHTTP1(key, now)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	tr := &protocolTransport{hints: hints}
	tr.failed(ctx, key, h, hints.begin(), ctx.Err())
	if hints.selectHTTP1(key, now) == nil {
		t.Fatal("caller cancellation disabled compatibility")
	}
	// Cancellation without HTTP/2 evidence must not install a new hint.
	tr.failed(ctx, [32]byte{1}, nil, hints.begin(), ctx.Err())
	if len(hints.entries) != 1 {
		t.Fatal("caller cancellation created a hint")
	}
}

func TestProtocolCompatibilityUserCancelIsNotRetried(t *testing.T) {
	var calls atomic.Int32
	arrived, release := make(chan struct{}), make(chan struct{})
	s := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if r.ProtoMajor == 2 {
			panic(http.ErrAbortHandler)
		}
		if n == 3 {
			close(arrived)
			<-release
		}
		_, _ = io.WriteString(w, "ok")
	}))
	s.EnableHTTP2 = true
	s.StartTLS()
	defer s.Close()
	defer close(release)
	c := compatibilityTestClient(t, s)
	primeCompatibility(t, c, s.URL)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	ctx = WithRequestAttemptCounter(ctx)
	go func() {
		select {
		case <-arrived:
			cancel()
		case <-ctx.Done():
		}
	}()
	_, err := compatibilityPost(ctx, c, s.URL)
	if !errors.Is(err, context.Canceled) || RequestAttemptCount(ctx) != 1 || calls.Load() != 3 {
		t.Fatalf("cancel was retried or misclassified: calls=%d err=%v", calls.Load(), err)
	}
	resp, err := compatibilityPost(t.Context(), c, s.URL)
	if err != nil || resp.ProtoMajor != 1 || calls.Load() != 4 {
		t.Fatalf("cancellation poisoned hint: calls=%d err=%v", calls.Load(), err)
	}
	resp.Body.Close()
}
