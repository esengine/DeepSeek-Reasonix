// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: © 2015 LabStack LLC and Echo contributors

package middleware

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"sync"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"golang.org/x/net/websocket"
)

// Assert expected with url.EscapedPath method to obtain the path.
func TestProxy(t *testing.T) {
	// Setup
	t1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "target 1")
	}))
	defer t1.Close()
	url1, _ := url.Parse(t1.URL)
	t2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "target 2")
	}))
	defer t2.Close()
	url2, _ := url.Parse(t2.URL)
	targets := []*ProxyTarget{
		{
			Name: "target 1",
			URL:  url1,
		},
		{
			Name: "target 2",
			URL:  url2,
		},
	}
	rb := NewRandomBalancer(nil)
	// must add targets:
	for _, target := range targets {
		assert.True(t, rb.AddTarget(target))
	}

	// must ignore duplicates:
	for _, target := range targets {
		assert.False(t, rb.AddTarget(target))
	}

	// Random
	e := echo.New()
	e.Use(Proxy(rb))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	body := rec.Body.String()
	expected := map[string]bool{
		"target 1": true,
		"target 2": true,
	}
	assert.Condition(t, func() bool {
		return expected[body]
	})

	for _, target := range targets {
		assert.True(t, rb.RemoveTarget(target.Name))
	}

	assert.False(t, rb.RemoveTarget("unknown target"))

	// Round-robin
	rrb := NewRoundRobinBalancer(targets)
	e = echo.New()
	e.Use(Proxy(rrb))

	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	body = rec.Body.String()
	assert.Equal(t, "target 1", body)

	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	body = rec.Body.String()
	assert.Equal(t, "target 2", body)

	// ModifyResponse
	e = echo.New()
	e.Use(ProxyWithConfig(ProxyConfig{
		Balancer: rrb,
		ModifyResponse: func(res *http.Response) error {
			res.Body = io.NopCloser(bytes.NewBuffer([]byte("modified")))
			res.Header.Set("X-Modified", "1")
			return nil
		},
	}))

	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	assert.Equal(t, "modified", rec.Body.String())
	assert.Equal(t, "1", rec.Header().Get("X-Modified"))

	// ProxyTarget is set in context
	contextObserver := func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) (err error) {
			next(c)
			assert.Contains(t, targets, c.Get("target"), "target is not set in context")
			return nil
		}
	}
	rrb1 := NewRoundRobinBalancer(targets)

	e = echo.New()
	e.Use(contextObserver)
	e.Use(Proxy(rrb1))
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)
}

type testProvider struct {
	commonBalancer
	target *ProxyTarget
	err    error
}

func (p *testProvider) Next(c echo.Context) *ProxyTarget {
	return &ProxyTarget{}
}

func (p *testProvider) NextTarget(c echo.Context) (*ProxyTarget, error) {
	return p.target, p.err
}

func TestTargetProvider(t *testing.T) {
	t1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "target 1")
	}))
	defer t1.Close()
	url1, _ := url.Parse(t1.URL)

	e := echo.New()
	tp := &testProvider{}
	tp.target = &ProxyTarget{Name: "target 1", URL: url1}
	e.Use(Proxy(tp))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	e.ServeHTTP(rec, req)
	body := rec.Body.String()
	assert.Equal(t, "target 1", body)
}

func TestFailNextTarget(t *testing.T) {
	url1, err := url.Parse("http://dummy:8080")
	assert.Nil(t, err)

	e := echo.New()
	tp := &testProvider{}
	tp.target = &ProxyTarget{Name: "target 1", URL: url1}
	tp.err = echo.NewHTTPError(http.StatusInternalServerError, "method could not select target")

	e.Use(Proxy(tp))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	e.ServeHTTP(rec, req)
	body := rec.Body.String()
	assert.Equal(t, "{\"message\":\"method could not select target\"}\n", body)
}

func TestProxyRealIPHeader(t *testing.T) {
	// Setup
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer upstream.Close()
	url, _ := url.Parse(upstream.URL)
	rrb := NewRoundRobinBalancer([]*ProxyTarget{{Name: "upstream", URL: url}})
	e := echo.New()
	e.Use(Proxy(rrb))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	remoteAddrIP, _, _ := net.SplitHostPort(req.RemoteAddr)
	realIPHeaderIP := "203.0.113.1"
	extractedRealIP := "203.0.113.10"
	tests := []*struct {
		hasRealIPheader bool
		hasIPExtractor  bool
		expectedXRealIP string
	}{
		{false, false, remoteAddrIP},
		{false, true, extractedRealIP},
		{true, false, realIPHeaderIP},
		{true, true, extractedRealIP},
	}

	for _, tt := range tests {
		if tt.hasRealIPheader {
			req.Header.Set(echo.HeaderXRealIP, realIPHeaderIP)
		} else {
			req.Header.Del(echo.HeaderXRealIP)
		}
		if tt.hasIPExtractor {
			e.IPExtractor = func(*http.Request) string {
				return extractedRealIP
			}
		} else {
			e.IPExtractor = nil
		}
		e.ServeHTTP(rec, req)
		assert.Equal(t, tt.expectedXRealIP, req.Header.Get(echo.HeaderXRealIP), "hasRealIPheader: %t / hasIPExtractor: %t", tt.hasRealIPheader, tt.hasIPExtractor)
	}
}

func TestProxyRewrite(t *testing.T) {
	var testCases = []struct {
		whenPath         string
		expectProxiedURI string
		expectStatus     int
	}{
		{
			whenPath:         "/api/users",
			expectProxiedURI: "/users",
			expectStatus:     http.StatusOK,
		},
		{
			whenPath:         "/js/main.js",
			expectProxiedURI: "/public/javascripts/main.js",
			expectStatus:     http.StatusOK,
		},
		{
			whenPath:         "/old",
			expectProxiedURI: "/new",
			expectStatus:     http.StatusOK,
		},
		{
			whenPath:         "/users/jack/orders/1",
			expectProxiedURI: "/user/jack/order/1",
			expectStatus:     http.StatusOK,
		},
		{
			whenPath:         "/user/jill/order/T%2FcO4lW%2Ft%2FVp%2F",
			expectProxiedURI: "/user/jill/order/T%2FcO4lW%2Ft%2FVp%2F",
			expectStatus:     http.StatusOK,
		},
		{ // ` ` (space) is encoded by httpClient to `%20` when doing request to Echo. `%20` should not be double escaped when proxying request
			whenPath:         "/api/new users",
			expectProxiedURI: "/new%20users",
			expectStatus:     http.StatusOK,
		},
		{ // query params should be proxied and not be modified
			whenPath:         "/api/users?limit=10",
			expectProxiedURI: "/users?limit=10",
			expectStatus:     http.StatusOK,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.whenPath, func(t *testing.T) {
			receivedRequestURI := make(chan string, 1)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// RequestURI is the unmodified request-target of the Request-Line (RFC 7230, Section 3.1.1) as sent by the client to a server
				// we need unmodified target to see if we are encoding/decoding the url in addition to rewrite/replace logic
				// if original request had `%2F` we should not magically decode it to `/` as it would change what was requested
				receivedRequestURI <- r.RequestURI
			}))
			defer upstream.Close()
			serverURL, _ := url.Parse(upstream.URL)
			rrb := NewRoundRobinBalancer([]*ProxyTarget{{Name: "upstream", URL: serverURL}})

			// Rewrite
			e := echo.New()
			e.Use(ProxyWithConfig(ProxyConfig{
				Balancer: rrb,
				Rewrite: map[string]string{
					"/old":              "/new",
					"/api/*":            "/$1",
					"/js/*":             "/public/javascripts/$1",
					"/users/*/orders/*": "/user/$1/order/$2",
				},
			}))

			targetURL, _ := serverURL.Parse(tc.whenPath)
			req := httptest.NewRequest(http.MethodGet, targetURL.String(), nil)
			rec := httptest.NewRecorder()

			e.ServeHTTP(rec, req)

			assert.Equal(t, tc.expectStatus, rec.Code)
			actualRequestURI := <-receivedRequestURI
			assert.Equal(t, tc.expectProxiedURI, actualRequestURI)
		})
	}
}

func TestProxyRewriteRegex(t *testing.T) {
	// Setup
	receivedRequestURI := make(chan string, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// RequestURI is the unmodified request-target of the Request-Line (RFC 7230, Section 3.1.1) as sent by the client to a server
		// we need unmodified target to see if we are encoding/decoding the url in addition to rewrite/replace logic
		// if original request had `%2F` we should not magically decode it to `/` as it would change what was requested
		receivedRequestURI <- r.RequestURI
	}))
	defer upstream.Close()
	tmpUrL, _ := url.Parse(upstream.URL)
	rrb := NewRoundRobinBalancer([]*ProxyTarget{{Name: "upstream", URL: tmpUrL}})

	// Rewrite
	e := echo.New()
	e.Use(ProxyWithConfig(ProxyConfig{
		Balancer: rrb,
		Rewrite: map[string]string{
			"^/a/*":     "/v1/$1",
			"^/b/*/c/*": "/v2/$2/$1",
			"^/c/*/*":   "/v3/$2",
		},
		RegexRewrite: map[*regexp.Regexp]string{
			regexp.MustCompile("^/x/.+?/(.*)"):   "/v4/$1",
			regexp.MustCompile("^/y/(.+?)/(.*)"): "/v5/$2/$1",
		},
	}))

	testCases := []struct {
		requestPath string
		statusCode  int
		expectPath  string
	}{
		{"/unmatched", http.StatusOK, "/unmatched"},
		{"/a/test", http.StatusOK, "/v1/test"},
		{"/b/foo/c/bar/baz", http.StatusOK, "/v2/bar/baz/foo"},
		{"/c/ignore/test", http.StatusOK, "/v3/test"},
		{"/c/ignore1/test/this", http.StatusOK, "/v3/test/this"},
		{"/x/ignore/test", http.StatusOK, "/v4/test"},
		{"/y/foo/bar", http.StatusOK, "/v5/bar/foo"},
		// NB: fragment is not added by golang httputil.NewSingleHostReverseProxy implementation
		// $2 = `bar?q=1#frag`, $1 = `foo`. replaced uri = `/v5/bar?q=1#frag/foo` but httputil.NewSingleHostReverseProxy does not send `#frag/foo` (currently)
		{"/y/foo/bar?q=1#frag", http.StatusOK, "/v5/bar?q=1"},
	}

	for _, tc := range testCases {
		t.Run(tc.requestPath, func(t *testing.T) {
			targetURL, _ := url.Parse(tc.requestPath)
			req := httptest.NewRequest(http.MethodGet, targetURL.String(), nil)
			rec := httptest.NewRecorder()

			e.ServeHTTP(rec, req)

			actualRequestURI := <-receivedRequestURI
			assert.Equal(t, tc.expectPath, actualRequestURI)
			assert.Equal(t, tc.statusCode, rec.Code)
		})
	}
}

func TestProxyError(t *testing.T) {
	// Setup
	url1, _ := url.Parse("http://127.0.0.1:27121")
	url2, _ := url.Parse("http://127.0.0.1:27122")

	targets := []*ProxyTarget{
		{
			Name: "target 1",
			URL:  url1,
		},
		{
			Name: "target 2",
			URL:  url2,
		},
	}
	rb := NewRandomBalancer(nil)
	// must add targets:
	for _, target := range targets {
		assert.True(t, rb.AddTarget(target))
	}

	// must ignore duplicates:
	for _, target := range targets {
		assert.False(t, rb.AddTarget(target))
	}

	// Random
	e := echo.New()
	e.Use(Proxy(rb))
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	// Remote unreachable
	rec := httptest.NewRecorder()
	req.URL.Path = "/api/users"
	e.ServeHTTP(rec, req)
	assert.Equal(t, "/api/users", req.URL.Path)
	assert.Equal(t, http.StatusBadGateway, rec.Code)
}

func TestProxyRetries(t *testing.T) {

	newServer := func(res int) (*url.URL, *httptest.Server) {
		server := httptest.NewServer(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(res)
			}),
		)
		targetURL, _ := url.Parse(server.URL)
		return targetURL, server
	}

	targetURL, server := newServer(http.StatusOK)
	defer server.Close()
	goodTarget := &ProxyTarget{
		Name: "Good",
		URL:  targetURL,
	}

	targetURL, server = newServer(http.StatusBadRequest)
	defer server.Close()
	goodTargetWith40X := &ProxyTarget{
		Name: "Good with 40X",
		URL:  targetURL,
	}

	targetURL, _ = url.Parse("http://127.0.0.1:27121")
	badTarget := &ProxyTarget{
		Name: "Bad",
		URL:  targetURL,
	}

	alwaysRetryFilter := func(c echo.Context, e error) bool { return true }
	neverRetryFilter := func(c echo.Context, e error) bool { return false }

	testCases := []struct {
		name             string
		retryCount       int
		retryFilters     []func(c echo.Context, e error) bool
		targets          []*ProxyTarget
		expectedResponse int
	}{
		{
			name: "retry count 0 does not attempt retry on fail",
			targets: []*ProxyTarget{
				badTarget,
				goodTarget,
			},
			expectedResponse: http.StatusBadGateway,
		},
		{
			name:       "retry count 1 does not attempt retry on success",
			retryCount: 1,
			targets: []*ProxyTarget{
				goodTarget,
			},
			expectedResponse: http.StatusOK,
		},
		{
			name:       "retry count 1 does retry on handler return true",
			retryCount: 1,
			retryFilters: []func(c echo.Context, e error) bool{
				alwaysRetryFilter,
			},
			targets: []*ProxyTarget{
				badTarget,
				goodTarget,
			},
			expectedResponse: http.StatusOK,
		},
		{
			name:       "retry count 1 does not retry on handler return false",
			retryCount: 1,
			retryFilters: []func(c echo.Context, e error) bool{
				neverRetryFilter,
			},
			targets: []*ProxyTarget{
				badTarget,
				goodTarget,
			},
			expectedResponse: http.StatusBadGateway,
		},
		{
			name:       "retry count 2 returns error when no more retries left",
			retryCount: 2,
			retryFilters: []func(c echo.Context, e error) bool{
				alwaysRetryFilter,
				alwaysRetryFilter,
			},
			targets: []*ProxyTarget{
				badTarget,
				badTarget,
				badTarget,
				goodTarget, //Should never be reached as only 2 retries
			},
			expectedResponse: http.StatusBadGateway,
		},
		{
			name:       "retry count 2 returns error when retries left but handler returns false",
			retryCount: 3,
			retryFilters: []func(c echo.Context, e error) bool{
				alwaysRetryFilter,
				alwaysRetryFilter,
				neverRetryFilter,
			},
			targets: []*ProxyTarget{
				badTarget,
				badTarget,
				badTarget,
				goodTarget, //Should never be reached as retry handler returns false on 2nd check
			},
			expectedResponse: http.StatusBadGateway,
		},
		{
			name:       "retry count 3 succeeds",
			retryCount: 3,
			retryFilters: []func(c echo.Context, e error) bool{
				alwaysRetryFilter,
				alwaysRetryFilter,
				alwaysRetryFilter,
			},
			targets: []*ProxyTarget{
				badTarget,
				badTarget,
				badTarget,
				goodTarget,
			},
			expectedResponse: http.StatusOK,
		},
		{
			name:       "40x responses are not retried",
			retryCount: 1,
			targets: []*ProxyTarget{
				goodTargetWith40X,
				goodTarget,
			},
			expectedResponse: http.StatusBadRequest,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {

			retryFilterCall := 0
			retryFilter := func(c echo.Context, e error) bool {
				if len(tc.retryFilters) == 0 {
					assert.FailNow(t, fmt.Sprintf("unexpected calls, %d, to retry handler", retryFilterCall))
				}

				retryFilterCall++

				nextRetryFilter := tc.retryFilters[0]
				tc.retryFilters = tc.retryFilters[1:]

				return nextRetryFilter(c, e)
			}

			e := echo.New()
			e.Use(ProxyWithConfig(
				ProxyConfig{
					Balancer:    NewRoundRobinBalancer(tc.targets),
					RetryCount:  tc.retryCount,
					RetryFilter: retryFilter,
				},
			))

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			rec := httptest.NewRecorder()

			e.ServeHTTP(rec, req)

			assert.Equal(t, tc.expectedResponse, rec.Code)
			if len(tc.retryFilters) > 0 {
				assert.FailNow(t, fmt.Sprintf("expected %d more retry handler calls", len(tc.retryFilters)))
			}
		})
	}
}

func TestProxyRetryWithBackendTimeout(t *testing.T) {

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = time.Millisecond * 500

	timeoutBackend := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(1 * time.Second)
			w.WriteHeader(404)
		}),
	)
	defer timeoutBackend.Close()

	timeoutTargetURL, _ := url.Parse(timeoutBackend.URL)
	goodBackend := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(200)
		}),
	)
	defer goodBackend.Close()

	goodTargetURL, _ := url.Parse(goodBackend.URL)
	e := echo.New()
	e.Use(ProxyWithConfig(
		ProxyConfig{
			Transport: transport,
			Balancer: NewRoundRobinBalancer([]*ProxyTarget{
				{
					Name: "Timeout",
					URL:  timeoutTargetURL,
				},
				{
					Name: "Good",
					URL:  goodTargetURL,
				},
			}),
			RetryCount: 1,
		},
	))

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)
			assert.Equal(t, 200, rec.Code)
		}()
	}

	wg.Wait()

}

func TestProxyErrorHandler(t *testing.T) {

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	goodURL, _ := url.Parse(server.URL)
	defer server.Close()
	goodTarget := &ProxyTarget{
		Name: "Good",
		URL:  goodURL,
	}

	badURL, _ := url.Parse("http://127.0.0.1:27121")
	badTarget := &ProxyTarget{
		Name: "Bad",
		URL:  badURL,
	}

	transformedError := errors.New("a new error")

	testCases := []struct {
		name             string
		target           *ProxyTarget
		errorHandler     func(c echo.Context, e error) error
		expectFinalError func(t *testing.T, err error)
	}{
		{
			name:   "Error handler not invoked when request success",
			target: goodTarget,
			errorHandler: func(c echo.Context, e error) error {
				assert.FailNow(t, "error handler should not be invoked")
				return e
			},
		},
		{
			name:   "Error handler invoked when request fails",
			target: badTarget,
			errorHandler: func(c echo.Context, e error) error {
				httpErr, ok := e.(*echo.HTTPError)
				assert.True(t, ok, "expected http error to be passed to handler")
				assert.Equal(t, http.StatusBadGateway, httpErr.Code, "expected http bad gateway error to be passed to handler")
				return transformedError
			},
			expectFinalError: func(t *testing.T, err error) {
				assert.Equal(t, transformedError, err, "transformed error not returned from proxy")
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			e := echo.New()
			e.Use(ProxyWithConfig(
				ProxyConfig{
					Balancer:     NewRoundRobinBalancer([]*ProxyTarget{tc.target}),
					ErrorHandler: tc.errorHandler,
				},
			))

			errorHandlerCalled := false
			e.HTTPErrorHandler = func(err error, c echo.Context) {
				errorHandlerCalled = true
				tc.expectFinalError(t, err)
				e.DefaultHTTPErrorHandler(err, c)
			}

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			rec := httptest.NewRecorder()

			e.ServeHTTP(rec, req)

			if !errorHandlerCalled && tc.expectFinalError != nil {
				t.Fatalf("error handler was not called")
			}

		})
	}
}

func TestClientCancelConnectionResultsHTTPCode499(t *testing.T) {
	var timeoutStop sync.WaitGroup
	timeoutStop.Add(1)
	HTTPTarget := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		timeoutStop.Wait() // wait until we have canceled the request
		w.WriteHeader(http.StatusOK)
	}))
	defer HTTPTarget.Close()
	targetURL, _ := url.Parse(HTTPTarget.URL)
	target := &ProxyTarget{
		Name: "target",
		URL:  targetURL,
	}
	rb := NewRandomBalancer(nil)
	assert.True(t, rb.AddTarget(target))
	e := echo.New()
	e.Use(Proxy(rb))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	ctx, cancel := context.WithCancel(req.Context())
	req = req.WithContext(ctx)
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()
	e.ServeHTTP(rec, req)
	timeoutStop.Done()
	assert.Equal(t, 499, rec.Code)
}

// Assert balancer with empty targets does return `nil` on `Next()`
func TestProxyBalancerWithNoTargets(t *testing.T) {
	rb := NewRandomBalancer(nil)
	assert.Nil(t, rb.Next(nil))

	rrb := NewRoundRobinBalancer([]*ProxyTarget{})
	assert.Nil(t, rrb.Next(nil))
}

type testContextKey string

type customBalancer struct {
	target *ProxyTarget
}

func (b *customBalancer) AddTarget(target *ProxyTarget) bool {
	return false
}

func (b *customBalancer) RemoveTarget(name string) bool {
	return false
}

func (b *customBalancer) Next(c echo.Context) *ProxyTarget {
	ctx := context.WithValue(c.Request().Context(), testContextKey("FROM_BALANCER"), "CUSTOM_BALANCER")
	c.SetRequest(c.Request().WithContext(ctx))
	return b.target
}

func TestModifyResponseUseContext(t *testing.T) {
	server := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("OK"))
		}),
	)
	defer server.Close()

	targetURL, _ := url.Parse(server.URL)
	e := echo.New()
	e.Use(ProxyWithConfig(
		ProxyConfig{
			Balancer: &customBalancer{
				target: &ProxyTarget{
					Name: "tst",
					URL:  targetURL,
				},
			},
			RetryCount: 1,
			ModifyResponse: func(res *http.Response) error {
				val := res.Request.Context().Value(testContextKey("FROM_BALANCER"))
				if valStr, ok := val.(string); ok {
					res.Header.Set("FROM_BALANCER", valStr)
				}
				return nil
			},
		},
	))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "OK", rec.Body.String())
	assert.Equal(t, "CUSTOM_BALANCER", rec.Header().Get("FROM_BALANCER"))
}

func createSimpleWebSocketServer(serveTLS bool) *httptest.Server {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wsHandler := func(conn *websocket.Conn) {
			defer conn.Close()
			for {
				var msg string
				err := websocket.Message.Receive(conn, &msg)
				if err != nil {
					return
				}
				// message back to the client
				websocket.Message.Send(conn, msg)
			}
		}
		websocket.Server{Handler: wsHandler}.ServeHTTP(w, r)
	})
	if serveTLS {
		return httptest.NewTLSServer(handler)
	}
	return httptest.NewServer(handler)
}

func createSimpleProxyServer(t *testing.T, srv *httptest.Server, serveTLS bool, toTLS bool) *httptest.Server {
	e := echo.New()

	if toTLS {
		// proxy to tls target
		tgtURL, _ := url.Parse(srv.URL)
		tgtURL.Scheme = "wss"
		balancer := NewRandomBalancer([]*ProxyTarget{{URL: tgtURL}})

		defaultTransport, ok := http.DefaultTransport.(*http.Transport)
		if !ok {
			t.Fatal("Default transport is not of type *http.Transport")
		}
		transport := defaultTransport.Clone()
		transport.TLSClientConfig = &tls.Config{
			InsecureSkipVerify: true,
		}
		e.Use(ProxyWithConfig(ProxyConfig{Balancer: balancer, Transport: transport}))
	} else {
		// proxy to non-TLS target
		tgtURL, _ := url.Parse(srv.URL)
		balancer := NewRandomBalancer([]*ProxyTarget{{URL: tgtURL}})
		e.Use(ProxyWithConfig(ProxyConfig{Balancer: balancer}))
	}

	if serveTLS {
		// serve proxy server with TLS
		ts := httptest.NewTLSServer(e)
		return ts
	}
	// serve proxy server without TLS
	ts := httptest.NewServer(e)
	return ts
}

// TestProxyWithConfigWebSocketNonTLS2NonTLS tests the proxy with non-TLS to non-TLS WebSocket connection.
func TestProxyWithConfigWebSocketNonTLS2NonTLS(t *testing.T) {
	/*
		Arrange
	*/
	// Create a WebSocket test server (non-TLS)
	srv := createSimpleWebSocketServer(false)
	defer srv.Close()

	// create proxy server (non-TLS to non-TLS)
	ts := createSimpleProxyServer(t, srv, false, false)
	defer ts.Close()

	tsURL, _ := url.Parse(ts.URL)
	tsURL.Scheme = "ws"
	tsURL.Path = "/"

	/*
		Act
	*/

	// Connect to the proxy WebSocket
	wsConn, err := websocket.Dial(tsURL.String(), "", "http://localhost/")
	assert.NoError(t, err)
	defer wsConn.Close()

	// Send message
	sendMsg := "Hello, Non TLS WebSocket!"
	err = websocket.Message.Send(wsConn, sendMsg)
	assert.NoError(t, err)

	/*
		Assert
	*/
	// Read response
	var recvMsg string
	err = websocket.Message.Receive(wsConn, &recvMsg)
	assert.NoError(t, err)
	assert.Equal(t, sendMsg, recvMsg)
}

// TestProxyWithConfigWebSocketTLS2TLS tests the proxy with TLS to TLS WebSocket connection.
func TestProxyWithConfigWebSocketTLS2TLS(t *testing.T) {
	/*
		Arrange
	*/
	// Create a WebSocket test server (TLS)
	srv := createSimpleWebSocketServer(true)
	defer srv.Close()

	// create proxy server (TLS to TLS)
	ts := createSimpleProxyServer(t, srv, true, true)
	defer ts.Close()

	tsURL, _ := url.Parse(ts.URL)
	tsURL.Scheme = "wss"
	tsURL.Path = "/"

	/*
		Act
	*/
	origin, err := url.Parse(ts.URL)
	assert.NoError(t, err)
	config := &websocket.Config{
		Location:  tsURL,
		Origin:    origin,
		TlsConfig: &tls.Config{InsecureSkipVerify: true}, // skip verify for testing
		Version:   websocket.ProtocolVersionHybi13,
	}
	wsConn, err := websocket.DialConfig(config)
	assert.NoError(t, err)
	defer wsConn.Close()

	// Send message
	sendMsg := "Hello, TLS to TLS WebSocket!"
	err = websocket.Message.Send(wsConn, sendMsg)
	assert.NoError(t, err)

	// Read response
	var recvMsg string
	err = websocket.Message.Receive(wsConn, &recvMsg)
	assert.NoError(t, err)
	assert.Equal(t, sendMsg, recvMsg)
}

// TestProxyWithConfigWebSocketNonTLS2TLS tests the proxy with non-TLS to TLS WebSocket connection.
func TestProxyWithConfigWebSocketNonTLS2TLS(t *testing.T) {
	/*
		Arrange
	*/

	// Create a WebSocket test server (TLS)
	srv := createSimpleWebSocketServer(true)
	defer srv.Close()

	// create proxy server (Non-TLS to TLS)
	ts := createSimpleProxyServer(t, srv, false, true)
	defer ts.Close()

	tsURL, _ := url.Parse(ts.URL)
	tsURL.Scheme = "ws"
	tsURL.Path = "/"

	/*
		Act
	*/
	// Connect to the proxy WebSocket
	wsConn, err := websocket.Dial(tsURL.String(), "", "http://localhost/")
	assert.NoError(t, err)
	defer wsConn.Close()

	// Send message
	sendMsg := "Hello, Non TLS to TLS WebSocket!"
	err = websocket.Message.Send(wsConn, sendMsg)
	assert.NoError(t, err)

	/*
		Assert
	*/
	// Read response
	var recvMsg string
	err = websocket.Message.Receive(wsConn, &recvMsg)
	assert.NoError(t, err)
	assert.Equal(t, sendMsg, recvMsg)
}

// TestProxyWithConfigWebSocketTLSToNoneTLS tests the proxy with TLS to non-TLS WebSocket connection. (TLS termination)
func TestProxyWithConfigWebSocketTLS2NonTLS(t *testing.T) {
	/*
		Arrange
	*/

	// Create a WebSocket test server (non-TLS)
	srv := createSimpleWebSocketServer(false)
	defer srv.Close()

	// create proxy server (TLS to non-TLS)
	ts := createSimpleProxyServer(t, srv, true, false)
	defer ts.Close()

	tsURL, _ := url.Parse(ts.URL)
	tsURL.Scheme = "wss"
	tsURL.Path = "/"

	/*
		Act
	*/
	origin, err := url.Parse(ts.URL)
	assert.NoError(t, err)
	config := &websocket.Config{
		Location:  tsURL,
		Origin:    origin,
		TlsConfig: &tls.Config{InsecureSkipVerify: true}, // skip verify for testing
		Version:   websocket.ProtocolVersionHybi13,
	}
	wsConn, err := websocket.DialConfig(config)
	assert.NoError(t, err)
	defer wsConn.Close()

	// Send message
	sendMsg := "Hello, TLS to NoneTLS WebSocket!"
	err = websocket.Message.Send(wsConn, sendMsg)
	assert.NoError(t, err)

	// Read response
	var recvMsg string
	err = websocket.Message.Receive(wsConn, &recvMsg)
	assert.NoError(t, err)
	assert.Equal(t, sendMsg, recvMsg)
}
