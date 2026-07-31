// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: © 2015 LabStack LLC and Echo contributors

package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
)

func TestRewriteAfterRouting(t *testing.T) {
	e := echo.New()
	// middlewares added with `Use()` are executed after routing is done and do not affect which route handler is matched
	e.Use(RewriteWithConfig(RewriteConfig{
		Rules: map[string]string{
			"/old":              "/new",
			"/api/*":            "/$1",
			"/js/*":             "/public/javascripts/$1",
			"/users/*/orders/*": "/user/$1/order/$2",
		},
	}))
	e.GET("/public/*", func(c echo.Context) error {
		return c.String(http.StatusOK, c.Param("*"))
	})
	e.GET("/*", func(c echo.Context) error {
		return c.String(http.StatusOK, c.Param("*"))
	})

	var testCases = []struct {
		whenPath             string
		expectRoutePath      string
		expectRequestPath    string
		expectRequestRawPath string
	}{
		{
			whenPath:             "/api/users",
			expectRoutePath:      "api/users",
			expectRequestPath:    "/users",
			expectRequestRawPath: "",
		},
		{
			whenPath:             "/js/main.js",
			expectRoutePath:      "js/main.js",
			expectRequestPath:    "/public/javascripts/main.js",
			expectRequestRawPath: "",
		},
		{
			whenPath:             "/users/jack/orders/1",
			expectRoutePath:      "users/jack/orders/1",
			expectRequestPath:    "/user/jack/order/1",
			expectRequestRawPath: "",
		},
		{ // no rewrite rule matched. already encoded URL should not be double encoded or changed in any way
			whenPath:             "/user/jill/order/T%2FcO4lW%2Ft%2FVp%2F",
			expectRoutePath:      "user/jill/order/T%2FcO4lW%2Ft%2FVp%2F",
			expectRequestPath:    "/user/jill/order/T/cO4lW/t/Vp/", // this is equal to `url.Parse(tc.whenPath)` result
			expectRequestRawPath: "/user/jill/order/T%2FcO4lW%2Ft%2FVp%2F",
		},
		{ // just rewrite but do not touch encoding. already encoded URL should not be double encoded
			whenPath:             "/users/jill/orders/T%2FcO4lW%2Ft%2FVp%2F",
			expectRoutePath:      "users/jill/orders/T%2FcO4lW%2Ft%2FVp%2F",
			expectRequestPath:    "/user/jill/order/T/cO4lW/t/Vp/", // this is equal to `url.Parse(tc.whenPath)` result
			expectRequestRawPath: "/user/jill/order/T%2FcO4lW%2Ft%2FVp%2F",
		},
		{ // ` ` (space) is encoded by httpClient to `%20` when doing request to Echo. `%20` should not be double escaped or changed in any way when rewriting request
			whenPath:             "/api/new users",
			expectRoutePath:      "api/new users",
			expectRequestPath:    "/new users",
			expectRequestRawPath: "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.whenPath, func(t *testing.T) {
			target, _ := url.Parse(tc.whenPath)
			req := httptest.NewRequest(http.MethodGet, target.String(), nil)
			rec := httptest.NewRecorder()

			e.ServeHTTP(rec, req)

			assert.Equal(t, http.StatusOK, rec.Code)
			assert.Equal(t, tc.expectRoutePath, rec.Body.String())
			assert.Equal(t, tc.expectRequestPath, req.URL.Path)
			assert.Equal(t, tc.expectRequestRawPath, req.URL.RawPath)
		})
	}
}

// Issue #1086
func TestEchoRewritePreMiddleware(t *testing.T) {
	e := echo.New()
	r := e.Router()

	// Rewrite old url to new one
	// middlewares added with `Pre()` are executed before routing is done and therefore change which handler matches
	e.Pre(Rewrite(map[string]string{
		"/old": "/new",
	},
	))

	// Route
	r.Add(http.MethodGet, "/new", func(c echo.Context) error {
		return c.NoContent(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/old", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	assert.Equal(t, "/new", req.URL.EscapedPath())
	assert.Equal(t, http.StatusOK, rec.Code)
}

// Issue #1143
func TestRewriteWithConfigPreMiddleware_Issue1143(t *testing.T) {
	e := echo.New()
	r := e.Router()

	// middlewares added with `Pre()` are executed before routing is done and therefore change which handler matches
	e.Pre(RewriteWithConfig(RewriteConfig{
		Rules: map[string]string{
			"/api/*/mgmt/proj/*/agt": "/api/$1/hosts/$2",
			"/api/*/mgmt/proj":       "/api/$1/eng",
		},
	}))

	r.Add(http.MethodGet, "/api/:version/hosts/:name", func(c echo.Context) error {
		return c.String(http.StatusOK, "hosts")
	})
	r.Add(http.MethodGet, "/api/:version/eng", func(c echo.Context) error {
		return c.String(http.StatusOK, "eng")
	})

	for i := 0; i < 100; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/mgmt/proj/test/agt", nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		assert.Equal(t, "/api/v1/hosts/test", req.URL.EscapedPath())
		assert.Equal(t, http.StatusOK, rec.Code)

		defer rec.Result().Body.Close()
		bodyBytes, _ := io.ReadAll(rec.Result().Body)
		assert.Equal(t, "hosts", string(bodyBytes))
	}
}

// Issue #1573
func TestEchoRewriteWithCaret(t *testing.T) {
	e := echo.New()

	e.Pre(RewriteWithConfig(RewriteConfig{
		Rules: map[string]string{
			"^/abc/*": "/v1/abc/$1",
		},
	}))

	rec := httptest.NewRecorder()

	var req *http.Request

	req = httptest.NewRequest(http.MethodGet, "/abc/test", nil)
	e.ServeHTTP(rec, req)
	assert.Equal(t, "/v1/abc/test", req.URL.Path)

	req = httptest.NewRequest(http.MethodGet, "/v1/abc/test", nil)
	e.ServeHTTP(rec, req)
	assert.Equal(t, "/v1/abc/test", req.URL.Path)

	req = httptest.NewRequest(http.MethodGet, "/v2/abc/test", nil)
	e.ServeHTTP(rec, req)
	assert.Equal(t, "/v2/abc/test", req.URL.Path)
}

// Verify regex used with rewrite
func TestEchoRewriteWithRegexRules(t *testing.T) {
	e := echo.New()

	e.Pre(RewriteWithConfig(RewriteConfig{
		Rules: map[string]string{
			"^/a/*":     "/v1/$1",
			"^/b/*/c/*": "/v2/$2/$1",
			"^/c/*/*":   "/v3/$2",
		},
		RegexRules: map[*regexp.Regexp]string{
			regexp.MustCompile("^/x/.+?/(.*)"):   "/v4/$1",
			regexp.MustCompile("^/y/(.+?)/(.*)"): "/v5/$2/$1",
		},
	}))

	var rec *httptest.ResponseRecorder
	var req *http.Request

	testCases := []struct {
		requestPath string
		expectPath  string
	}{
		{"/unmatched", "/unmatched"},
		{"/a/test", "/v1/test"},
		{"/b/foo/c/bar/baz", "/v2/bar/baz/foo"},
		{"/c/ignore/test", "/v3/test"},
		{"/c/ignore1/test/this", "/v3/test/this"},
		{"/x/ignore/test", "/v4/test"},
		{"/y/foo/bar", "/v5/bar/foo"},
	}

	for _, tc := range testCases {
		t.Run(tc.requestPath, func(t *testing.T) {
			req = httptest.NewRequest(http.MethodGet, tc.requestPath, nil)
			rec = httptest.NewRecorder()
			e.ServeHTTP(rec, req)
			assert.Equal(t, tc.expectPath, req.URL.EscapedPath())
		})
	}
}

// Ensure correct escaping as defined in replacement (issue #1798)
func TestEchoRewriteReplacementEscaping(t *testing.T) {
	e := echo.New()

	// NOTE: these are incorrect regexps as they do not factor in that URI we are replacing could contain ? (query) and # (fragment) parts
	// so in reality they append query and fragment part as `$1` matches everything after that prefix
	e.Pre(RewriteWithConfig(RewriteConfig{
		Rules: map[string]string{
			"^/a/*": "/$1?query=param",
			"^/b/*": "/$1;part#one",
		},
		RegexRules: map[*regexp.Regexp]string{
			regexp.MustCompile("^/x/(.*)"): "/$1?query=param",
			regexp.MustCompile("^/y/(.*)"): "/$1;part#one",
			regexp.MustCompile("^/z/(.*)"): "/$1?test=1#escaped%20test",
		},
	}))

	var rec *httptest.ResponseRecorder
	var req *http.Request

	testCases := []struct {
		requestPath string
		expect      string
	}{
		{"/unmatched", "/unmatched"},
		{"/a/test", "/test?query=param"},
		{"/b/foo/bar", "/foo/bar;part#one"},
		{"/x/test", "/test?query=param"},
		{"/y/foo/bar", "/foo/bar;part#one"},
		{"/z/foo/b%20ar", "/foo/b%20ar?test=1#escaped%20test"},
		{"/z/foo/b%20ar?nope=1#yes", "/foo/b%20ar?nope=1#yes?test=1%23escaped%20test"}, // example of appending
	}

	for _, tc := range testCases {
		t.Run(tc.requestPath, func(t *testing.T) {
			req = httptest.NewRequest(http.MethodGet, tc.requestPath, nil)
			rec = httptest.NewRecorder()
			e.ServeHTTP(rec, req)
			assert.Equal(t, tc.expect, req.URL.String())
		})
	}
}
