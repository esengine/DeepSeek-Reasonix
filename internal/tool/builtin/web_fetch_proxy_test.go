package builtin

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/netclient"
)

func startCONNECTProxy(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				br := bufio.NewReader(c)
				req, err := http.ReadRequest(br)
				if err != nil {
					return
				}
				if req.Method != http.MethodConnect {
					return
				}
				targetConn, err := net.DialTimeout("tcp", req.Host, 5*time.Second)
				if err != nil {
					c.Write([]byte("HTTP/1.1 502 Bad Gateway\r\n\r\n"))
					return
				}
				defer targetConn.Close()
				c.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
				go func() { io.Copy(targetConn, c) }()
				io.Copy(c, targetConn)
			}(conn)
		}
	}()
	return fmt.Sprintf("http://%s", listener.Addr().String())
}

func startRespondingCONNECTProxy(t *testing.T, respond func(hit int32, req *http.Request) *http.Response) (string, *int32) {
	t.Helper()
	var hits int32
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				br := bufio.NewReader(c)
				req, err := http.ReadRequest(br)
				if err != nil || req.Method != http.MethodConnect {
					return
				}
				hit := atomic.AddInt32(&hits, 1)
				c.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
				req, err = http.ReadRequest(br)
				if err != nil {
					return
				}
				_ = respond(hit, req).Write(c)
			}(conn)
		}
	}()
	return fmt.Sprintf("http://%s", listener.Addr().String()), &hits
}

func textProxyResponse(req *http.Request, statusCode int, status string, body string) *http.Response {
	resp := &http.Response{
		StatusCode: statusCode,
		Status:     status,
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
	resp.Header.Set("Content-Type", "text/plain")
	resp.ContentLength = int64(len(body))
	return resp
}

func startInterceptingCONNECTProxy(t *testing.T, body string) (string, *int32) {
	t.Helper()
	return startRespondingCONNECTProxy(t, func(_ int32, req *http.Request) *http.Response {
		return textProxyResponse(req, http.StatusOK, "200 OK", body)
	})
}

func startRedirectingCONNECTProxy(t *testing.T, location string) (string, *int32) {
	t.Helper()
	return startRespondingCONNECTProxy(t, func(hit int32, req *http.Request) *http.Response {
		if hit == 1 {
			resp := textProxyResponse(req, http.StatusFound, "302 Found", "")
			resp.Header.Set("Location", location)
			return resp
		}
		return textProxyResponse(req, http.StatusOK, "200 OK", "proxied after redirect")
	})
}

func startTestHTTPServer(t *testing.T, body string) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	u := fmt.Sprintf("http://%s/", listener.Addr().String())
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte(body))
	})
	server := &http.Server{Handler: mux}
	t.Cleanup(func() { server.Close() })
	go server.Serve(listener)
	return u
}

func TestWebFetchUsesEnvProxyAtRequestTime(t *testing.T) {
	t.Setenv("http_proxy", "")
	t.Setenv("HTTPS_PROXY", "")
	t.Setenv("https_proxy", "")
	t.Setenv("NO_PROXY", "")
	t.Setenv("no_proxy", "")

	proxyURL, proxyHits := startInterceptingCONNECTProxy(t, "from env proxy")
	wf := webFetch{proxySpec: netclient.ProxySpec{Mode: netclient.ModeEnv}}
	t.Setenv("HTTP_PROXY", proxyURL)

	args, _ := json.Marshal(map[string]string{"url": "http://service.test/resource"})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := wf.Execute(ctx, args)
	if err != nil {
		t.Fatalf("webFetch.Execute through env proxy: %v", err)
	}
	if !strings.Contains(result, "from env proxy") {
		t.Fatalf("expected env proxy response, got: %s", result)
	}
	if got := atomic.LoadInt32(proxyHits); got != 1 {
		t.Fatalf("proxy hits = %d, want 1", got)
	}
}

func TestWebFetchProxySpecHonorsNoProxy(t *testing.T) {
	targetURL := startTestHTTPServer(t, "direct no_proxy")
	target, err := url.Parse(targetURL)
	if err != nil {
		t.Fatalf("parse target URL: %v", err)
	}
	proxyURL, proxyHits := startInterceptingCONNECTProxy(t, "from proxy")
	wf := webFetch{
		proxySpec: netclient.ProxySpec{
			Mode:    netclient.ModeCustom,
			URL:     proxyURL,
			NoProxy: target.Hostname(),
		},
	}
	args, _ := json.Marshal(map[string]string{"url": targetURL})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := wf.Execute(ctx, args)
	if err != nil {
		t.Fatalf("webFetch.Execute with no_proxy: %v", err)
	}
	if !strings.Contains(result, "direct no_proxy") {
		t.Fatalf("expected direct response, got: %s", result)
	}
	if got := atomic.LoadInt32(proxyHits); got != 0 {
		t.Fatalf("proxy hits = %d, want 0", got)
	}
}

func TestWebFetchRechecksProxySpecAfterRedirect(t *testing.T) {
	targetURL := startTestHTTPServer(t, "redirect target direct")
	target, err := url.Parse(targetURL)
	if err != nil {
		t.Fatalf("parse target URL: %v", err)
	}
	proxyURL, proxyHits := startRedirectingCONNECTProxy(t, targetURL)
	wf := webFetch{
		proxySpec: netclient.ProxySpec{
			Mode:    netclient.ModeCustom,
			URL:     proxyURL,
			NoProxy: target.Hostname(),
		},
	}
	args, _ := json.Marshal(map[string]string{"url": "http://service.test/start"})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := wf.Execute(ctx, args)
	if err != nil {
		t.Fatalf("webFetch.Execute with redirect: %v", err)
	}
	if !strings.Contains(result, "redirect target direct") {
		t.Fatalf("expected redirected request to bypass proxy, got: %s", result)
	}
	if got := atomic.LoadInt32(proxyHits); got != 1 {
		t.Fatalf("proxy hits = %d, want 1", got)
	}
}

func TestWebFetchThroughCONNECTProxy(t *testing.T) {
	targetURL := startTestHTTPServer(t, "hello from target")
	proxyURL := startCONNECTProxy(t)
	t.Logf("proxy: %s  target: %s", proxyURL, targetURL)

	wf := webFetch{proxySpec: netclient.ProxySpec{Mode: netclient.ModeCustom, URL: proxyURL}}
	args, _ := json.Marshal(map[string]string{"url": targetURL})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := wf.Execute(ctx, args)
	if err != nil {
		t.Fatalf("webFetch.Execute through proxy: %v", err)
	}
	if !strings.Contains(result, "hello from target") {
		t.Errorf("expected 'hello from target', got: %s", result)
	}
}

func TestWebFetchWithoutProxy(t *testing.T) {
	targetURL := startTestHTTPServer(t, "direct fetch OK")
	wf := webFetch{proxySpec: netclient.ProxySpec{Mode: netclient.ModeOff}}
	args, _ := json.Marshal(map[string]string{"url": targetURL})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := wf.Execute(ctx, args)
	if err != nil {
		t.Fatalf("webFetch without proxy: %v", err)
	}
	if !strings.Contains(result, "direct fetch OK") {
		t.Errorf("expected 'direct fetch OK', got: %s", result)
	}
}

func TestSSRFStillBlocksPrivateThroughProxy(t *testing.T) {
	proxyURL := startCONNECTProxy(t)
	wf := webFetch{proxySpec: netclient.ProxySpec{Mode: netclient.ModeCustom, URL: proxyURL}}

	blocked := []string{
		"http://169.254.169.254/latest/meta-data",
		"http://10.0.0.1/",
		"http://192.168.1.1/",
	}
	for _, u := range blocked {
		t.Run(u, func(t *testing.T) {
			args, _ := json.Marshal(map[string]string{"url": u})
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, err := wf.Execute(ctx, args)
			if err == nil {
				t.Error("expected SSRF error, got nil")
			} else {
				t.Logf("blocked: %v", err)
			}
		})
	}
}

func TestProxyBasicAuthURLParsing(t *testing.T) {
	u, _ := url.Parse("http://user:pass@127.0.0.1:7897")
	if u.User == nil {
		t.Fatal("expected user info")
	}
	if u.User.Username() != "user" {
		t.Errorf("username = %q", u.User.Username())
	}
	pass, _ := u.User.Password()
	if pass != "pass" {
		t.Errorf("password = %q", pass)
	}
}

func TestWebFetchSOCKS5Proxy(t *testing.T) {
	wf := webFetch{proxySpec: netclient.ProxySpec{Mode: netclient.ModeCustom, URL: "socks5://127.0.0.1:1"}}
	args, _ := json.Marshal(map[string]string{"url": "https://example.com"})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := wf.Execute(ctx, args)
	if err == nil {
		t.Log("unexpected success (no SOCKS5 running)")
	} else {
		t.Logf("expected error (no SOCKS5 server): %v", err)
	}
}

func TestSSRFBlocksPrivateTargetThroughSOCKS5(t *testing.T) {
	wf := webFetch{proxySpec: netclient.ProxySpec{Mode: netclient.ModeCustom, URL: "socks5://127.0.0.1:1080"}}
	for _, u := range []string{"http://169.254.169.254/latest/meta-data", "http://10.0.0.1/", "http://192.168.1.1/"} {
		t.Run(u, func(t *testing.T) {
			args, _ := json.Marshal(map[string]string{"url": u})
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_, err := wf.Execute(ctx, args)
			if err == nil || !strings.Contains(err.Error(), "refusing to fetch internal address") {
				t.Errorf("want SSRF block for %s, got %v", u, err)
			}
		})
	}
}

func TestSOCKS5ProxyOnPrivateAddressNotSSRFBlocked(t *testing.T) {
	// A SOCKS proxy commonly lives on a private/LAN address; the SSRF guard must
	// not reject the proxy itself. Reaching the (absent) proxy fails, but never
	// with an SSRF "internal address" error.
	wf := webFetch{proxySpec: netclient.ProxySpec{Mode: netclient.ModeCustom, URL: "socks5://10.0.0.1:1080"}}
	args, _ := json.Marshal(map[string]string{"url": "https://example.com"})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := wf.Execute(ctx, args)
	if err != nil && strings.Contains(err.Error(), "refusing to fetch internal address") {
		t.Fatalf("proxy on private address was SSRF-blocked: %v", err)
	}
}
