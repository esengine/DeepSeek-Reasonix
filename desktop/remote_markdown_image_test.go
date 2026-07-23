package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/netclient"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestRemoteMarkdownImageUsesReasonixProxySpec(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\nproxy-image")
	wantSpec := netclient.ProxySpec{Mode: netclient.ModeCustom, URL: "socks5://127.0.0.1:10808"}
	var gotSpec netclient.ProxySpec
	var gotRequest *http.Request
	factory := func(spec netclient.ProxySpec) (*http.Client, error) {
		gotSpec = spec
		return &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			gotRequest = req
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(bytes.NewReader(png)),
				Request:    req,
			}, nil
		})}, nil
	}

	req := httptest.NewRequest(http.MethodGet, remoteMarkdownImagePath+"?url="+url.QueryEscape("https://images.example.com/pixel.png"), nil)
	rec := httptest.NewRecorder()
	serveRemoteMarkdownImage(rec, req, wantSpec, factory)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %q", rec.Code, rec.Body.String())
	}
	if !reflect.DeepEqual(gotSpec, wantSpec) {
		t.Fatalf("proxy spec = %#v, want %#v", gotSpec, wantSpec)
	}
	if gotRequest == nil || gotRequest.URL.String() != "https://images.example.com/pixel.png" {
		t.Fatalf("remote request = %v", gotRequest)
	}
	if got := gotRequest.Header.Get("Accept"); !strings.Contains(got, "image/png") {
		t.Fatalf("Accept = %q", got)
	}
	if got := rec.Header().Get("Content-Type"); got != "image/png" {
		t.Fatalf("Content-Type = %q", got)
	}
	if rec.Body.String() != string(png) {
		t.Fatalf("body mismatch: %q", rec.Body.String())
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q", got)
	}
}

func TestRemoteMarkdownImageTraversesConfiguredHTTPProxy(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\nproxied")
	var proxyCalled atomic.Bool
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyCalled.Store(true)
		if r.Method != http.MethodConnect || r.Host != "93.184.216.34:80" {
			t.Errorf("proxy request = %s %s, want CONNECT to vetted IP", r.Method, r.Host)
			http.Error(w, "CONNECT required", http.StatusMethodNotAllowed)
			return
		}
		conn, rw, err := http.NewResponseController(w).Hijack()
		if err != nil {
			t.Errorf("hijack proxy connection: %v", err)
			return
		}
		defer conn.Close()
		if _, err := rw.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
			return
		}
		if err := rw.Flush(); err != nil {
			return
		}
		tunneled, err := http.ReadRequest(rw.Reader)
		if err != nil {
			t.Errorf("read tunneled request: %v", err)
			return
		}
		defer tunneled.Body.Close()
		if tunneled.Host != "images.example.invalid" || tunneled.URL.Path != "/pixel.png" {
			t.Errorf("tunneled request = host %q path %q", tunneled.Host, tunneled.URL.Path)
		}
		if !tunneled.Close {
			t.Error("single-use image transport kept the proxy tunnel alive")
		}
		_, _ = rw.WriteString("HTTP/1.1 200 OK\r\nContent-Type: image/png\r\nContent-Length: " + fmt.Sprint(len(png)) + "\r\nConnection: close\r\n\r\n")
		_, _ = rw.Write(png)
		_ = rw.Flush()
	}))
	defer proxy.Close()

	spec := netclient.ProxySpec{Mode: netclient.ModeCustom, URL: proxy.URL}
	req := httptest.NewRequest(http.MethodGet, remoteMarkdownImagePath+"?url="+url.QueryEscape("http://images.example.invalid/pixel.png"), nil)
	rec := httptest.NewRecorder()
	serveRemoteMarkdownImage(rec, req, spec, func(spec netclient.ProxySpec) (*http.Client, error) {
		return newRemoteMarkdownImageClientWithLookup(spec, func(context.Context, string) ([]net.IPAddr, error) {
			return []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}}, nil
		})
	})

	if rec.Code != http.StatusOK || !proxyCalled.Load() || rec.Body.String() != string(png) {
		t.Fatalf("configured proxy was not used: status=%d called=%v body=%q", rec.Code, proxyCalled.Load(), rec.Body.String())
	}
}

func TestRemoteMarkdownImageHTTPSConnectPinsVettedIP(t *testing.T) {
	var proxyCalled atomic.Bool
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyCalled.Store(true)
		if r.Method != http.MethodConnect || r.Host != "93.184.216.34:443" {
			t.Errorf("HTTPS proxy request = %s %s, want CONNECT to vetted IP", r.Method, r.Host)
		}
		http.Error(w, "test stops before target TLS", http.StatusBadGateway)
	}))
	defer proxy.Close()

	spec := netclient.ProxySpec{Mode: netclient.ModeCustom, URL: proxy.URL}
	req := httptest.NewRequest(http.MethodGet, remoteMarkdownImagePath+"?url="+url.QueryEscape("https://images.example.invalid/pixel.png"), nil)
	rec := httptest.NewRecorder()
	serveRemoteMarkdownImage(rec, req, spec, func(spec netclient.ProxySpec) (*http.Client, error) {
		return newRemoteMarkdownImageClientWithLookup(spec, func(context.Context, string) ([]net.IPAddr, error) {
			return []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}}, nil
		})
	})

	if rec.Code != http.StatusBadGateway || !proxyCalled.Load() {
		t.Fatalf("HTTPS proxy status=%d called=%v", rec.Code, proxyCalled.Load())
	}
}

func TestRemoteMarkdownImageTraversesConfiguredSOCKSProxyWithVettedIP(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\nsocks-proxied")
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	proxyResult := make(chan error, 1)
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			proxyResult <- acceptErr
			return
		}
		defer conn.Close()
		reader := bufio.NewReader(conn)
		header := make([]byte, 2)
		if _, err := io.ReadFull(reader, header); err != nil || header[0] != 5 {
			proxyResult <- fmt.Errorf("read SOCKS greeting: %w", err)
			return
		}
		methods := make([]byte, int(header[1]))
		if _, err := io.ReadFull(reader, methods); err != nil {
			proxyResult <- err
			return
		}
		if _, err := conn.Write([]byte{5, 0}); err != nil {
			proxyResult <- err
			return
		}
		requestHeader := make([]byte, 4)
		if _, err := io.ReadFull(reader, requestHeader); err != nil || requestHeader[0] != 5 || requestHeader[1] != 1 || requestHeader[3] != 1 {
			proxyResult <- fmt.Errorf("SOCKS target was not an IPv4 CONNECT: header=%v err=%v", requestHeader, err)
			return
		}
		ipBytes := make([]byte, net.IPv4len)
		portBytes := make([]byte, 2)
		if _, err := io.ReadFull(reader, ipBytes); err != nil {
			proxyResult <- err
			return
		}
		if _, err := io.ReadFull(reader, portBytes); err != nil {
			proxyResult <- err
			return
		}
		if target := net.JoinHostPort(net.IP(ipBytes).String(), fmt.Sprint(binary.BigEndian.Uint16(portBytes))); target != "93.184.216.34:80" {
			proxyResult <- fmt.Errorf("SOCKS target = %s, want vetted IP", target)
			return
		}
		if _, err := conn.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0}); err != nil {
			proxyResult <- err
			return
		}
		tunneled, err := http.ReadRequest(reader)
		if err != nil {
			proxyResult <- err
			return
		}
		defer tunneled.Body.Close()
		if tunneled.Host != "images.example.invalid" || tunneled.URL.Path != "/pixel.png" || !tunneled.Close {
			proxyResult <- fmt.Errorf("tunneled request host=%q path=%q close=%v", tunneled.Host, tunneled.URL.Path, tunneled.Close)
			return
		}
		if _, err := fmt.Fprintf(conn, "HTTP/1.1 200 OK\r\nContent-Type: image/png\r\nContent-Length: %d\r\nConnection: close\r\n\r\n", len(png)); err != nil {
			proxyResult <- err
			return
		}
		if _, err := conn.Write(png); err != nil {
			proxyResult <- err
			return
		}
		proxyResult <- nil
	}()

	spec := netclient.ProxySpec{Mode: netclient.ModeCustom, URL: "socks5h://" + listener.Addr().String()}
	req := httptest.NewRequest(http.MethodGet, remoteMarkdownImagePath+"?url="+url.QueryEscape("http://images.example.invalid/pixel.png"), nil)
	rec := httptest.NewRecorder()
	serveRemoteMarkdownImage(rec, req, spec, func(spec netclient.ProxySpec) (*http.Client, error) {
		return newRemoteMarkdownImageClientWithLookup(spec, func(context.Context, string) ([]net.IPAddr, error) {
			return []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}}, nil
		})
	})
	select {
	case proxyErr := <-proxyResult:
		if proxyErr != nil {
			t.Fatal(proxyErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("SOCKS proxy did not receive the remote image request")
	}
	if rec.Code != http.StatusOK || rec.Body.String() != string(png) {
		t.Fatalf("SOCKS proxy status=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestRemoteMarkdownImageProxyRejectsPrivateResolution(t *testing.T) {
	var proxyCalled atomic.Bool
	proxy := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		proxyCalled.Store(true)
	}))
	defer proxy.Close()

	spec := netclient.ProxySpec{Mode: netclient.ModeCustom, URL: proxy.URL}
	req := httptest.NewRequest(http.MethodGet, remoteMarkdownImagePath+"?url="+url.QueryEscape("http://rebind.example.test/pixel.png"), nil)
	rec := httptest.NewRecorder()
	serveRemoteMarkdownImage(rec, req, spec, func(spec netclient.ProxySpec) (*http.Client, error) {
		return newRemoteMarkdownImageClientWithLookup(spec, func(context.Context, string) ([]net.IPAddr, error) {
			return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, nil
		})
	})

	if rec.Code != http.StatusBadGateway || proxyCalled.Load() {
		t.Fatalf("private proxy target status=%d proxyCalled=%v", rec.Code, proxyCalled.Load())
	}
}

func TestResolveRemoteMarkdownImageAddressesRejectsAnyPrivateResolution(t *testing.T) {
	_, err := resolveRemoteMarkdownImageAddresses(context.Background(), "rebind.example.test", func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{
			{IP: net.ParseIP("93.184.216.34")},
			{IP: net.ParseIP("169.254.169.254")},
		}, nil
	})
	if err == nil || !strings.Contains(err.Error(), "non-public") {
		t.Fatalf("mixed public/private resolution error = %v", err)
	}
}

func TestRemoteMarkdownImageProxyURLDefaults(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want string
	}{
		{raw: "//proxy.example.test", want: "http://proxy.example.test:80"},
		{raw: "https://proxy.example.test", want: "https://proxy.example.test:443"},
		{raw: "socks5h://proxy.example.test", want: "socks5h://proxy.example.test:1080"},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			parsed, err := url.Parse(tc.raw)
			if err != nil {
				t.Fatal(err)
			}
			got, err := normalizedRemoteMarkdownImageProxyURL(parsed)
			if err != nil {
				t.Fatal(err)
			}
			if got.String() != tc.want {
				t.Fatalf("normalized proxy = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRemoteMarkdownImageRoundTripperPinsDirectDialAndResolvesRouteOnce(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "direct-image")
	}))
	defer target.Close()
	targetAddress := strings.TrimPrefix(target.URL, "http://")

	var proxyCalls atomic.Int32
	var dialedAddress atomic.Value
	rt := remoteMarkdownImageRoundTripper{
		proxyFor: func(*http.Request) (*url.URL, error) {
			proxyCalls.Add(1)
			return nil, nil
		},
		lookupIP: func(context.Context, string) ([]net.IPAddr, error) {
			return []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}}, nil
		},
		dialerForProxy: func(proxyURL *url.URL) (netclient.StreamDialer, error) {
			if proxyURL != nil {
				t.Fatalf("unexpected proxy URL: %v", proxyURL)
			}
			return netclient.DialerFunc(func(ctx context.Context, network, address string) (net.Conn, error) {
				dialedAddress.Store(address)
				return (&net.Dialer{}).DialContext(ctx, network, targetAddress)
			}), nil
		},
		options: netclient.TransportOptions{DialTimeout: time.Second},
	}
	req, err := http.NewRequest(http.MethodGet, "http://images.example.com/pixel.png", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if string(body) != "direct-image" || proxyCalls.Load() != 1 || dialedAddress.Load() != "93.184.216.34:80" {
		t.Fatalf("body=%q proxyCalls=%d dialed=%v", body, proxyCalls.Load(), dialedAddress.Load())
	}
}

func TestRemoteMarkdownImageRejectsUnsafeTargets(t *testing.T) {
	for _, raw := range []string{
		"",
		"file:///tmp/secret.png",
		"http://localhost/image.png",
		"http://127.0.0.1/image.png",
		"http://10.0.0.1/image.png",
		"http://169.254.169.254/latest/meta-data",
		"http://100.100.100.200/latest/meta-data",
		"http://255.255.255.255/image.png",
		"http://router.local/image.png",
		"https://user:pass@images.example.com/image.png",
	} {
		t.Run(raw, func(t *testing.T) {
			if _, err := validateRemoteMarkdownImageURL(raw); err == nil {
				t.Fatalf("unsafe URL accepted: %q", raw)
			}
		})
	}
	if got, err := validateRemoteMarkdownImageURL("https://images.example.com/a.png#section"); err != nil || got != "https://images.example.com/a.png" {
		t.Fatalf("public URL = %q, %v", got, err)
	}
	if _, err := validateRemoteMarkdownImageURL("https://[2001:4860:4860::8888]/a.png"); err != nil {
		t.Fatalf("public IPv6 URL rejected: %v", err)
	}
}

func TestRemoteMarkdownImageRejectsNonImagesAndOversizedBodies(t *testing.T) {
	for _, tc := range []struct {
		name string
		body []byte
		want int
	}{
		{name: "html", body: []byte("<!doctype html><script>alert(1)</script>"), want: http.StatusUnsupportedMediaType},
		{name: "oversized", body: bytes.Repeat([]byte{'x'}, remoteMarkdownImageMaxBytes+1), want: http.StatusBadGateway},
	} {
		t.Run(tc.name, func(t *testing.T) {
			factory := func(netclient.ProxySpec) (*http.Client, error) {
				return &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusOK,
						Header:     make(http.Header),
						Body:       io.NopCloser(bytes.NewReader(tc.body)),
						Request:    req,
					}, nil
				})}, nil
			}
			req := httptest.NewRequest(http.MethodGet, remoteMarkdownImagePath+"?url="+url.QueryEscape("https://images.example.com/image"), nil)
			rec := httptest.NewRecorder()
			serveRemoteMarkdownImage(rec, req, netclient.ProxySpec{Mode: netclient.ModeCustom, URL: "http://127.0.0.1:10808"}, factory)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d; body = %q", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

func TestRemoteMarkdownImageSanitizesSVG(t *testing.T) {
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg" onload="steal()">
<style>@import url(https://evil.example/style.css);</style>
<script>alert(1)</script>
<foreignObject><iframe src="https://evil.example/"></iframe></foreignObject>
<image href="https://evil.example/pixel.png" />
<use href="#safe-shape" />
<rect id="safe-shape" width="10" height="10" fill="url(#paint)" style="color:red" />
</svg>`)
	factory := func(netclient.ProxySpec) (*http.Client, error) {
		return &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"image/svg+xml"}},
				Body:       io.NopCloser(bytes.NewReader(svg)),
				Request:    req,
			}, nil
		})}, nil
	}
	req := httptest.NewRequest(http.MethodGet, remoteMarkdownImagePath+"?url="+url.QueryEscape("https://images.example.com/badge.svg"), nil)
	rec := httptest.NewRecorder()
	serveRemoteMarkdownImage(rec, req, netclient.ProxySpec{Mode: netclient.ModeCustom, URL: "http://127.0.0.1:10808"}, factory)

	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/svg+xml" {
		t.Fatalf("SVG status=%d type=%q body=%q", rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
	}
	got := rec.Body.String()
	for _, forbidden := range []string{"<script", "<style", "foreignObject", "iframe", "onload", "evil.example"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("sanitized SVG still contains %q: %s", forbidden, got)
		}
	}
	for _, preserved := range []string{`href="#safe-shape"`, `fill="url(#paint)"`, `style="color:red"`} {
		if !strings.Contains(got, preserved) {
			t.Fatalf("sanitized SVG dropped %q: %s", preserved, got)
		}
	}
}

func TestRemoteMarkdownImageSanitizesValidSVGPrologs(t *testing.T) {
	tests := []struct {
		name string
		body []byte
	}{
		{name: "UTF-8 BOM", body: append([]byte{0xef, 0xbb, 0xbf}, []byte(`<svg xmlns="http://www.w3.org/2000/svg"><rect width="1" height="1" /></svg>`)...)},
		{name: "leading comment", body: []byte(`<!-- exported by a diagram tool --><svg xmlns="http://www.w3.org/2000/svg"><rect width="1" height="1" /></svg>`)},
		{name: "DOCTYPE", body: []byte(`<!DOCTYPE svg><svg xmlns="http://www.w3.org/2000/svg"><rect width="1" height="1" /></svg>`)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sanitized, ok := sanitizeRemoteMarkdownSVG(tt.body)
			if !ok || !bytes.Contains(sanitized, []byte("<svg")) || !bytes.Contains(sanitized, []byte("<rect")) {
				t.Fatalf("valid SVG rejected: ok=%v body=%q", ok, sanitized)
			}
			if bytes.Contains(sanitized, []byte("DOCTYPE")) || bytes.Contains(sanitized, []byte("exported")) {
				t.Fatalf("SVG prolog was not removed: %q", sanitized)
			}
		})
	}
}

func TestRemoteMarkdownImageRejectsNonSVGXML(t *testing.T) {
	if sanitized, ok := sanitizeRemoteMarkdownSVG([]byte(`<?xml version="1.0"?><html></html>`)); ok {
		t.Fatalf("non-SVG XML accepted: %q", sanitized)
	}
}

func TestRemoteMarkdownImageMiddlewarePassesOtherPaths(t *testing.T) {
	app := NewApp()
	called := false
	handler := app.remoteMarkdownImageMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/index.html", nil))
	if !called || rec.Code != http.StatusNoContent {
		t.Fatalf("unrelated request was not passed through: called=%v status=%d", called, rec.Code)
	}
}

func TestRemoteMarkdownImageOnlyAllowsGet(t *testing.T) {
	called := false
	factory := func(netclient.ProxySpec) (*http.Client, error) {
		called = true
		return &http.Client{}, nil
	}
	req := httptest.NewRequest(http.MethodPost, remoteMarkdownImagePath+"?url="+url.QueryEscape("https://images.example.com/image.png"), nil)
	rec := httptest.NewRecorder()
	serveRemoteMarkdownImage(rec, req, netclient.ProxySpec{}, factory)
	if rec.Code != http.StatusMethodNotAllowed || called {
		t.Fatalf("POST status=%d factoryCalled=%v", rec.Code, called)
	}
}
