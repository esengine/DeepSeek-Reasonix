package netclient

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCustomProxyBuildsSocks5URL(t *testing.T) {
	pf, err := proxyFunc(ProxySpec{
		Mode:     "custom",
		Type:     "socks5",
		Server:   "127.0.0.1",
		Port:     7890,
		Username: "user",
		Password: "secret",
	})
	if err != nil {
		t.Fatalf("proxyFunc: %v", err)
	}
	got, err := pf(&http.Request{URL: mustURL("https://api.deepseek.com/chat/completions")})
	if err != nil {
		t.Fatalf("proxy lookup: %v", err)
	}
	if got.Scheme != "socks5" || got.Host != "127.0.0.1:7890" {
		t.Fatalf("proxy URL = %s, want socks5://127.0.0.1:7890", got)
	}
	if pass, ok := got.User.Password(); !ok || pass != "secret" {
		t.Fatalf("proxy password not preserved")
	}
}

func TestCustomProxyHonorsNoProxy(t *testing.T) {
	pf, err := proxyFunc(ProxySpec{
		Mode:    "custom",
		URL:     "http://proxy.example.com:8080",
		NoProxy: "api.deepseek.com",
	})
	if err != nil {
		t.Fatalf("proxyFunc: %v", err)
	}
	got, err := pf(&http.Request{URL: mustURL("https://api.deepseek.com/chat/completions")})
	if err != nil {
		t.Fatalf("proxy lookup: %v", err)
	}
	if got != nil {
		t.Fatalf("NoProxy host should bypass proxy, got %s", got)
	}
}

func TestDirectHostsBypassProxy(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://proxy.example.com:8080")
	t.Setenv("NO_PROXY", "")
	pf, err := proxyFunc(ProxySpec{Mode: "auto", DirectHosts: []string{"token-plan-cn.xiaomimimo.com"}})
	if err != nil {
		t.Fatalf("proxyFunc: %v", err)
	}

	got, err := pf(&http.Request{URL: mustURL("https://token-plan-cn.xiaomimimo.com/v1/chat")})
	if err != nil {
		t.Fatalf("direct-host lookup: %v", err)
	}
	if got != nil {
		t.Fatalf("a direct host should bypass the proxy, got %s", got)
	}

	other, err := pf(&http.Request{URL: mustURL("https://example.com/x")})
	if err != nil {
		t.Fatalf("other lookup: %v", err)
	}
	if other == nil || other.Host != "proxy.example.com:8080" {
		t.Fatalf("non-direct host should still use the env proxy, got %v", other)
	}
}

func TestNoDirectHostsKeepsEveryoneProxied(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://proxy.example.com:8080")
	t.Setenv("NO_PROXY", "")
	pf, err := proxyFunc(ProxySpec{Mode: "env"}) // no DirectHosts → nothing special-cased
	if err != nil {
		t.Fatalf("proxyFunc: %v", err)
	}
	got, err := pf(&http.Request{URL: mustURL("https://token-plan-cn.xiaomimimo.com/v1/chat")})
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if got == nil || got.Host != "proxy.example.com:8080" {
		t.Fatalf("without DirectHosts the host must go through the proxy, got %v", got)
	}
}

func TestOffProxyDisablesProxy(t *testing.T) {
	pf, err := proxyFunc(ProxySpec{Mode: "off"})
	if err != nil {
		t.Fatalf("proxyFunc: %v", err)
	}
	if pf != nil {
		t.Fatal("off mode should return nil proxy func")
	}
}

func TestSummaryRedactsPassword(t *testing.T) {
	got := Summary(ProxySpec{
		Mode:     "custom",
		Type:     "socks5",
		Server:   "proxy.example.com",
		Port:     1080,
		Username: "user",
		Password: "secret",
	})
	if got != "custom (socks5://user@proxy.example.com:1080)" {
		t.Fatalf("Summary = %q", got)
	}
}

func TestHTTPClientProxyModesAffectRequests(t *testing.T) {
	var targetHits int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&targetHits, 1)
		_, _ = io.WriteString(w, "target")
	}))
	t.Cleanup(target.Close)
	targetAddr := strings.TrimPrefix(target.URL, "http://")

	var envProxyHits int32
	envProxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&envProxyHits, 1)
		if got, want := r.URL.String(), "http://service.test/resource"; got != want {
			t.Errorf("env proxy request URL = %q, want %q", got, want)
		}
		_, _ = io.WriteString(w, "env-proxy")
	}))
	t.Cleanup(envProxy.Close)

	var customProxyHits int32
	customProxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&customProxyHits, 1)
		if got, want := r.URL.String(), "http://service.test/resource"; got != want {
			t.Errorf("custom proxy request URL = %q, want %q", got, want)
		}
		_, _ = io.WriteString(w, "custom-proxy")
	}))
	t.Cleanup(customProxy.Close)

	// Windows env vars are case-insensitive, so HTTP_PROXY and http_proxy are the
	// same var — set the intended value last or the empty clear wipes it.
	t.Setenv("http_proxy", "")
	t.Setenv("HTTPS_PROXY", "")
	t.Setenv("https_proxy", "")
	t.Setenv("NO_PROXY", "")
	t.Setenv("no_proxy", "")
	t.Setenv("HTTP_PROXY", envProxy.URL)

	tests := []struct {
		name           string
		spec           ProxySpec
		wantBody       string
		wantTargetHits int32
		wantEnvHits    int32
		wantCustomHits int32
	}{
		{
			name:        "auto uses environment proxy",
			spec:        ProxySpec{Mode: ModeAuto},
			wantBody:    "env-proxy",
			wantEnvHits: 1,
		},
		{
			name:        "env uses environment proxy",
			spec:        ProxySpec{Mode: ModeEnv},
			wantBody:    "env-proxy",
			wantEnvHits: 1,
		},
		{
			name:           "custom ignores environment proxy",
			spec:           ProxySpec{Mode: ModeCustom, URL: customProxy.URL},
			wantBody:       "custom-proxy",
			wantCustomHits: 1,
		},
		{
			name:           "custom no_proxy bypasses proxy",
			spec:           ProxySpec{Mode: ModeCustom, URL: customProxy.URL, NoProxy: "service.test"},
			wantBody:       "target",
			wantTargetHits: 1,
		},
		{
			name:           "off bypasses environment proxy",
			spec:           ProxySpec{Mode: ModeOff},
			wantBody:       "target",
			wantTargetHits: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			atomic.StoreInt32(&targetHits, 0)
			atomic.StoreInt32(&envProxyHits, 0)
			atomic.StoreInt32(&customProxyHits, 0)

			client := mappedClient(t, tt.spec, "service.test:80", targetAddr)
			resp, err := client.Get("http://service.test/resource")
			if err != nil {
				t.Fatalf("GET: %v", err)
			}
			defer resp.Body.Close()
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("read body: %v", err)
			}
			if string(body) != tt.wantBody {
				t.Fatalf("body = %q, want %q", body, tt.wantBody)
			}
			if got := atomic.LoadInt32(&targetHits); got != tt.wantTargetHits {
				t.Fatalf("target hits = %d, want %d", got, tt.wantTargetHits)
			}
			if got := atomic.LoadInt32(&envProxyHits); got != tt.wantEnvHits {
				t.Fatalf("env proxy hits = %d, want %d", got, tt.wantEnvHits)
			}
			if got := atomic.LoadInt32(&customProxyHits); got != tt.wantCustomHits {
				t.Fatalf("custom proxy hits = %d, want %d", got, tt.wantCustomHits)
			}
		})
	}
}

func TestStructuredProxyTypesAffectRequests(t *testing.T) {
	httpProxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.URL.String(), "http://service.test/resource"; got != want {
			t.Errorf("HTTP proxy request URL = %q, want %q", got, want)
		}
		_, _ = io.WriteString(w, "http-proxy")
	}))
	t.Cleanup(httpProxy.Close)

	httpsProxy := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.URL.String(), "http://service.test/resource"; got != want {
			t.Errorf("HTTPS proxy request URL = %q, want %q", got, want)
		}
		_, _ = io.WriteString(w, "https-proxy")
	}))
	t.Cleanup(httpsProxy.Close)

	socks5Proxy := newSocksHTTPProxy(t)
	socks5hProxy := newSocksHTTPProxy(t)

	tests := []struct {
		name     string
		spec     ProxySpec
		tlsProxy *httptest.Server
		wantBody string
	}{
		{
			name:     "http",
			spec:     structuredProxySpec(t, "http", httpProxy.URL),
			wantBody: "http-proxy",
		},
		{
			name:     "https",
			spec:     structuredProxySpec(t, "https", httpsProxy.URL),
			tlsProxy: httpsProxy,
			wantBody: "https-proxy",
		},
		{
			name:     "socks5",
			spec:     structuredProxySpec(t, "socks5", "http://"+socks5Proxy.addr),
			wantBody: "socks-proxy",
		},
		{
			name:     "socks5h",
			spec:     structuredProxySpec(t, "socks5h", "http://"+socks5hProxy.addr),
			wantBody: "socks-proxy",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := mappedTransport(t, tt.spec, "service.test:80", "127.0.0.1:1")
			if tt.tlsProxy != nil {
				proxyTransport := tt.tlsProxy.Client().Transport.(*http.Transport)
				tr.TLSClientConfig = proxyTransport.TLSClientConfig
			}
			client := &http.Client{Transport: tr, Timeout: 2 * time.Second}
			resp, err := client.Get("http://service.test/resource")
			if err != nil {
				t.Fatalf("GET: %v", err)
			}
			defer resp.Body.Close()
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("read body: %v", err)
			}
			if string(body) != tt.wantBody {
				t.Fatalf("body = %q, want %q", body, tt.wantBody)
			}
		})
	}

	if got := atomic.LoadInt32(&socks5Proxy.hits); got != 1 {
		t.Fatalf("socks5 proxy hits = %d, want 1", got)
	}
	if got := atomic.LoadInt32(&socks5hProxy.hits); got != 1 {
		t.Fatalf("socks5h proxy hits = %d, want 1", got)
	}
}

func TestHTTPSRequestsRespectProxyModes(t *testing.T) {
	var targetHits int32
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&targetHits, 1)
		w.Header().Set("Connection", "close")
		_, _ = io.WriteString(w, "https-target")
	}))
	t.Cleanup(target.Close)
	targetAddr := strings.TrimPrefix(target.URL, "https://")

	var proxyHits int32
	proxy := newConnectProxy(t, targetAddr, &proxyHits)
	t.Cleanup(proxy.Close)

	// Set HTTPS_PROXY last: on Windows it and https_proxy are the same var.
	t.Setenv("HTTP_PROXY", "")
	t.Setenv("http_proxy", "")
	t.Setenv("https_proxy", "")
	t.Setenv("NO_PROXY", "")
	t.Setenv("no_proxy", "")
	t.Setenv("HTTPS_PROXY", proxy.URL)

	tests := []struct {
		name          string
		spec          ProxySpec
		wantProxyHits int32
	}{
		{
			name:          "auto uses HTTPS_PROXY",
			spec:          ProxySpec{Mode: ModeAuto},
			wantProxyHits: 1,
		},
		{
			name:          "custom proxies HTTPS requests",
			spec:          ProxySpec{Mode: ModeCustom, URL: proxy.URL},
			wantProxyHits: 1,
		},
		{
			name: "custom no_proxy bypasses HTTPS proxy",
			spec: ProxySpec{Mode: ModeCustom, URL: proxy.URL, NoProxy: "service.test"},
		},
		{
			name: "off bypasses HTTPS_PROXY",
			spec: ProxySpec{Mode: ModeOff},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			atomic.StoreInt32(&targetHits, 0)
			atomic.StoreInt32(&proxyHits, 0)

			tr := mappedTransport(t, tt.spec, "service.test:443", targetAddr)
			tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
			client := &http.Client{Transport: tr, Timeout: 2 * time.Second}
			resp, err := client.Get("https://service.test/resource")
			if err != nil {
				t.Fatalf("GET: %v", err)
			}
			defer resp.Body.Close()
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("read body: %v", err)
			}
			if string(body) != "https-target" {
				t.Fatalf("body = %q, want https-target", body)
			}
			if got := atomic.LoadInt32(&targetHits); got != 1 {
				t.Fatalf("target hits = %d, want 1", got)
			}
			if got := atomic.LoadInt32(&proxyHits); got != tt.wantProxyHits {
				t.Fatalf("CONNECT proxy hits = %d, want %d", got, tt.wantProxyHits)
			}
		})
	}
}

func structuredProxySpec(t *testing.T, typ, rawURL string) ProxySpec {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse proxy URL: %v", err)
	}
	host, portText, err := net.SplitHostPort(u.Host)
	if err != nil {
		t.Fatalf("split proxy host: %v", err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("parse proxy port: %v", err)
	}
	return ProxySpec{Mode: ModeCustom, Type: typ, Server: host, Port: port}
}

func mappedClient(t *testing.T, spec ProxySpec, fromAddr, toAddr string) *http.Client {
	t.Helper()
	tr := mappedTransport(t, spec, fromAddr, toAddr)
	return &http.Client{Transport: tr, Timeout: 2 * time.Second}
}

func mappedTransport(t *testing.T, spec ProxySpec, fromAddr, toAddr string) *http.Transport {
	t.Helper()
	tr, err := NewTransport(spec, TransportOptions{})
	if err != nil {
		t.Fatalf("NewTransport: %v", err)
	}
	t.Cleanup(tr.CloseIdleConnections)
	dialer := &net.Dialer{Timeout: time.Second}
	tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if addr == fromAddr {
			addr = toAddr
		}
		return dialer.DialContext(ctx, network, addr)
	}
	return tr
}

type socksHTTPProxy struct {
	addr string
	hits int32
}

func newSocksHTTPProxy(t *testing.T) *socksHTTPProxy {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen socks proxy: %v", err)
	}
	p := &socksHTTPProxy{addr: ln.Addr().String()}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go p.handle(conn)
		}
	}()
	return p
}

func (p *socksHTTPProxy) handle(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	r := bufio.NewReader(conn)

	header := make([]byte, 2)
	if _, err := io.ReadFull(r, header); err != nil || header[0] != 5 {
		return
	}
	methods := make([]byte, int(header[1]))
	if _, err := io.ReadFull(r, methods); err != nil {
		return
	}
	if _, err := conn.Write([]byte{5, 0}); err != nil {
		return
	}

	req := make([]byte, 4)
	if _, err := io.ReadFull(r, req); err != nil || req[0] != 5 || req[1] != 1 {
		return
	}
	if !p.readSocksAddr(r, req[3]) {
		return
	}
	atomic.AddInt32(&p.hits, 1)
	if _, err := conn.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0}); err != nil {
		return
	}

	httpReq, err := http.ReadRequest(r)
	if err != nil {
		return
	}
	_ = httpReq.Body.Close()
	_, _ = conn.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 11\r\nConnection: close\r\n\r\nsocks-proxy"))
}

func (p *socksHTTPProxy) readSocksAddr(r *bufio.Reader, atyp byte) bool {
	switch atyp {
	case 1:
		if _, err := io.ReadFull(r, make([]byte, net.IPv4len)); err != nil {
			return false
		}
	case 3:
		size, err := r.ReadByte()
		if err != nil {
			return false
		}
		if _, err := io.ReadFull(r, make([]byte, int(size))); err != nil {
			return false
		}
	case 4:
		if _, err := io.ReadFull(r, make([]byte, net.IPv6len)); err != nil {
			return false
		}
	default:
		return false
	}
	port := make([]byte, 2)
	if _, err := io.ReadFull(r, port); err != nil {
		return false
	}
	return binary.BigEndian.Uint16(port) != 0
}

func newConnectProxy(t *testing.T, targetAddr string, hits *int32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect {
			t.Errorf("proxy method = %s, want CONNECT", r.Method)
			http.Error(w, "CONNECT required", http.StatusMethodNotAllowed)
			return
		}
		atomic.AddInt32(hits, 1)
		clientConn, _, err := http.NewResponseController(w).Hijack()
		if err != nil {
			t.Errorf("hijack CONNECT: %v", err)
			return
		}
		defer clientConn.Close()
		targetConn, err := net.DialTimeout("tcp", targetAddr, time.Second)
		if err != nil {
			t.Errorf("dial CONNECT target: %v", err)
			return
		}
		defer targetConn.Close()
		if _, err := clientConn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n")); err != nil {
			return
		}
		go func() {
			_, _ = io.Copy(targetConn, clientConn)
		}()
		_, _ = io.Copy(clientConn, targetConn)
	}))
}

func mustURL(s string) *url.URL {
	u, err := url.Parse(s)
	if err != nil {
		panic(err)
	}
	return u
}
