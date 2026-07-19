package serve

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/control"
)

func newListenerTestServer(t *testing.T) *Server {
	t.Helper()
	t.Setenv("REASONIX_HOME", t.TempDir())
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{
		Sink:       bc,
		Label:      "listener-test",
		SessionDir: t.TempDir(),
	})
	t.Cleanup(func() { ctrl.Close() })
	return New(ctrl, bc, config.ServeConfig{})
}

func waitForHTTP(t *testing.T, addr string) {
	t.Helper()
	client := &http.Client{Timeout: 2 * time.Second}
	var lastErr error
	for i := 0; i < 100; i++ {
		resp, err := client.Get("http://" + addr + "/assets/logo-wordmark.svg")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("GET logo = %d, want 200", resp.StatusCode)
			}
			return
		}
		lastErr = err
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("server never came up on %s: %v", addr, lastErr)
}

// RunGracefulListener must serve on the caller-supplied listener so callers
// that need the real bound address (--addr 127.0.0.1:0 with --port-file) can
// listen first, record ln.Addr(), then hand the listener over.
func TestRunGracefulListenerServesOnProvidedListener(t *testing.T) {
	srv := newListenerTestServer(t)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.RunGracefulListener(ctx, ln) }()

	waitForHTTP(t, ln.Addr().String())

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("RunGracefulListener returned a shutdown error: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("RunGracefulListener did not return after ctx cancel")
	}
}

// RunGraceful must keep its historical contract (bind from the addr string
// itself) now that it delegates to RunGracefulListener.
func TestRunGracefulStillListensFromAddr(t *testing.T) {
	srv := newListenerTestServer(t)

	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("probe listen: %v", err)
	}
	addr := probe.Addr().String()
	probe.Close()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.RunGraceful(ctx, addr) }()

	waitForHTTP(t, addr)

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("RunGraceful returned a shutdown error: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("RunGraceful did not return after ctx cancel")
	}
}
