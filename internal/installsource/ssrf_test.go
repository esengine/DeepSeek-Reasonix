package installsource

import (
	"bufio"
	"context"
	"net"
	"net/http"
	"net/url"
	"sync"
	"testing"
	"time"
)

// recordingProxy is a dummy HTTP proxy: it accepts absolute-form requests,
// records the destination the client asked it to reach, and answers 200. It
// never contacts the destination, so tests can assert what would have been
// forwarded without touching a real internal service.
type recordingProxy struct {
	mu    sync.Mutex
	dests []string

	listener net.Listener
}

func newRecordingProxy(t *testing.T) *recordingProxy {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	p := &recordingProxy{listener: l}
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			go p.serve(conn)
		}
	}()
	t.Cleanup(func() { _ = l.Close() })
	return p
}

func (p *recordingProxy) serve(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	req, err := http.ReadRequest(bufio.NewReader(conn))
	if err != nil {
		return
	}
	if req.URL != nil && req.URL.IsAbs() {
		p.mu.Lock()
		p.dests = append(p.dests, req.URL.String())
		p.mu.Unlock()
	}
	_, _ = conn.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 2\r\nConnection: close\r\n\r\nok"))
}

func (p *recordingProxy) destinations() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.dests...)
}

func (p *recordingProxy) URL() string {
	return "http://" + p.listener.Addr().String()
}

func TestSSRFGuardRejectsBlockedIPLiteralThroughProxy(t *testing.T) {
	proxy := newRecordingProxy(t)
	transport := &http.Transport{Proxy: func(*http.Request) (*url.URL, error) {
		return url.Parse(proxy.URL())
	}}
	client := ssrfGuardClient(&http.Client{Transport: transport, Timeout: 5 * time.Second})

	for _, target := range []string{
		"http://169.254.169.254/latest/meta-data/",
		"http://10.0.0.1/mcp",
		"http://192.168.0.1/mcp",
		"http://100.64.0.1/mcp",
	} {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, target, nil)
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		resp, err := client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			t.Fatalf("request to %s through proxy succeeded; want SSRF refusal", target)
		}
	}
	if got := proxy.destinations(); len(got) != 0 {
		t.Fatalf("proxy was asked to reach %v; want the guard to refuse before forwarding", got)
	}
}

func TestSSRFGuardRejectsBlockedIPLiteralDirect(t *testing.T) {
	client := ssrfGuardClient(&http.Client{Timeout: 2 * time.Second})
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://169.254.169.254/latest/meta-data/", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if _, err := client.Do(req); err == nil {
		t.Fatal("direct request to blocked IP succeeded; want SSRF refusal")
	}
}

func TestSSRFGuardAllowsLoopbackThroughProxy(t *testing.T) {
	proxy := newRecordingProxy(t)
	transport := &http.Transport{Proxy: func(*http.Request) (*url.URL, error) {
		return url.Parse(proxy.URL())
	}}
	client := ssrfGuardClient(&http.Client{Transport: transport, Timeout: 5 * time.Second})

	// Loopback is deliberately allowed by this guard; the request must reach
	// the proxy carrying its destination intact.
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://127.0.0.1:8799/mcp", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("loopback request through proxy: %v", err)
	}
	_ = resp.Body.Close()
	if got := proxy.destinations(); len(got) != 1 || got[0] != "http://127.0.0.1:8799/mcp" {
		t.Fatalf("proxy destinations = %v; want exactly the loopback target", got)
	}
}
