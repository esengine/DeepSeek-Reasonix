// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: © 2015 LabStack LLC and Echo contributors

package echo

import (
	"bytes"
	stdContext "context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/http2"
)

type user struct {
	ID   int    `json:"id" xml:"id" form:"id" query:"id" param:"id" header:"id"`
	Name string `json:"name" xml:"name" form:"name" query:"name" param:"name" header:"name"`
}

const (
	userJSON                    = `{"id":1,"name":"Jon Snow"}`
	usersJSON                   = `[{"id":1,"name":"Jon Snow"}]`
	userXML                     = `<user><id>1</id><name>Jon Snow</name></user>`
	userForm                    = `id=1&name=Jon Snow`
	invalidContent              = "invalid content"
	userJSONInvalidType         = `{"id":"1","name":"Jon Snow"}`
	userXMLConvertNumberError   = `<user><id>Number one</id><name>Jon Snow</name></user>`
	userXMLUnsupportedTypeError = `<user><>Number one</><name>Jon Snow</name></user>`
)

const userJSONPretty = `{
  "id": 1,
  "name": "Jon Snow"
}`

const userXMLPretty = `<user>
  <id>1</id>
  <name>Jon Snow</name>
</user>`

var dummyQuery = url.Values{"dummy": []string{"useless"}}

func TestEcho(t *testing.T) {
	e := New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Router
	assert.NotNil(t, e.Router())

	// DefaultHTTPErrorHandler
	e.DefaultHTTPErrorHandler(errors.New("error"), c)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestEchoStatic(t *testing.T) {
	var testCases = []struct {
		name                 string
		givenPrefix          string
		givenRoot            string
		whenURL              string
		expectStatus         int
		expectHeaderLocation string
		expectBodyStartsWith string
	}{
		{
			name:                 "ok",
			givenPrefix:          "/images",
			givenRoot:            "_fixture/images",
			whenURL:              "/images/walle.png",
			expectStatus:         http.StatusOK,
			expectBodyStartsWith: string([]byte{0x89, 0x50, 0x4e, 0x47}),
		},
		{
			name:                 "ok with relative path for root points to directory",
			givenPrefix:          "/images",
			givenRoot:            "./_fixture/images",
			whenURL:              "/images/walle.png",
			expectStatus:         http.StatusOK,
			expectBodyStartsWith: string([]byte{0x89, 0x50, 0x4e, 0x47}),
		},
		{
			name:                 "No file",
			givenPrefix:          "/images",
			givenRoot:            "_fixture/scripts",
			whenURL:              "/images/bolt.png",
			expectStatus:         http.StatusNotFound,
			expectBodyStartsWith: "{\"message\":\"Not Found\"}\n",
		},
		{
			name:                 "Directory",
			givenPrefix:          "/images",
			givenRoot:            "_fixture/images",
			whenURL:              "/images/",
			expectStatus:         http.StatusNotFound,
			expectBodyStartsWith: "{\"message\":\"Not Found\"}\n",
		},
		{
			name:                 "Directory Redirect",
			givenPrefix:          "/",
			givenRoot:            "_fixture",
			whenURL:              "/folder",
			expectStatus:         http.StatusMovedPermanently,
			expectHeaderLocation: "/folder/",
			expectBodyStartsWith: "",
		},
		{
			name:                 "Directory Redirect with non-root path",
			givenPrefix:          "/static",
			givenRoot:            "_fixture",
			whenURL:              "/static",
			expectStatus:         http.StatusMovedPermanently,
			expectHeaderLocation: "/static/",
			expectBodyStartsWith: "",
		},
		{
			name:                 "Prefixed directory 404 (request URL without slash)",
			givenPrefix:          "/folder/", // trailing slash will intentionally not match "/folder"
			givenRoot:            "_fixture",
			whenURL:              "/folder", // no trailing slash
			expectStatus:         http.StatusNotFound,
			expectBodyStartsWith: "{\"message\":\"Not Found\"}\n",
		},
		{
			name:                 "Prefixed directory redirect (without slash redirect to slash)",
			givenPrefix:          "/folder", // no trailing slash shall match /folder and /folder/*
			givenRoot:            "_fixture",
			whenURL:              "/folder", // no trailing slash
			expectStatus:         http.StatusMovedPermanently,
			expectHeaderLocation: "/folder/",
			expectBodyStartsWith: "",
		},
		{
			name:                 "Directory with index.html",
			givenPrefix:          "/",
			givenRoot:            "_fixture",
			whenURL:              "/",
			expectStatus:         http.StatusOK,
			expectBodyStartsWith: "<!doctype html>",
		},
		{
			name:                 "Prefixed directory with index.html (prefix ending with slash)",
			givenPrefix:          "/assets/",
			givenRoot:            "_fixture",
			whenURL:              "/assets/",
			expectStatus:         http.StatusOK,
			expectBodyStartsWith: "<!doctype html>",
		},
		{
			name:                 "Prefixed directory with index.html (prefix ending without slash)",
			givenPrefix:          "/assets",
			givenRoot:            "_fixture",
			whenURL:              "/assets/",
			expectStatus:         http.StatusOK,
			expectBodyStartsWith: "<!doctype html>",
		},
		{
			name:                 "Sub-directory with index.html",
			givenPrefix:          "/",
			givenRoot:            "_fixture",
			whenURL:              "/folder/",
			expectStatus:         http.StatusOK,
			expectBodyStartsWith: "<!doctype html>",
		},
		{
			name:                 "do not allow directory traversal (backslash - windows separator)",
			givenPrefix:          "/",
			givenRoot:            "_fixture/",
			whenURL:              `/..\\middleware/basic_auth.go`,
			expectStatus:         http.StatusNotFound,
			expectBodyStartsWith: "{\"message\":\"Not Found\"}\n",
		},
		{
			name:                 "do not allow directory traversal (slash - unix separator)",
			givenPrefix:          "/",
			givenRoot:            "_fixture/",
			whenURL:              `/../middleware/basic_auth.go`,
			expectStatus:         http.StatusNotFound,
			expectBodyStartsWith: "{\"message\":\"Not Found\"}\n",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			e := New()
			e.Static(tc.givenPrefix, tc.givenRoot)
			req := httptest.NewRequest(http.MethodGet, tc.whenURL, nil)
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)
			assert.Equal(t, tc.expectStatus, rec.Code)
			body := rec.Body.String()
			if tc.expectBodyStartsWith != "" {
				assert.True(t, strings.HasPrefix(body, tc.expectBodyStartsWith))
			} else {
				assert.Equal(t, "", body)
			}

			if tc.expectHeaderLocation != "" {
				assert.Equal(t, tc.expectHeaderLocation, rec.Result().Header["Location"][0])
			} else {
				_, ok := rec.Result().Header["Location"]
				assert.False(t, ok)
			}
		})
	}
}

func TestEchoStaticRedirectIndex(t *testing.T) {
	e := New()

	// HandlerFunc
	e.Static("/static", "_fixture")

	errCh := make(chan error)

	go func() {
		errCh <- e.Start(":0")
	}()

	err := waitForServerStart(e, errCh, false)
	assert.NoError(t, err)

	addr := e.ListenerAddr().String()
	if resp, err := http.Get("http://" + addr + "/static"); err == nil { // http.Get follows redirects by default
		defer func(Body io.ReadCloser) {
			err := Body.Close()
			if err != nil {
				assert.Fail(t, err.Error())
			}
		}(resp.Body)
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		if body, err := io.ReadAll(resp.Body); err == nil {
			assert.Equal(t, true, strings.HasPrefix(string(body), "<!doctype html>"))
		} else {
			assert.Fail(t, err.Error())
		}

	} else {
		assert.NoError(t, err)
	}

	if err := e.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestEchoFile(t *testing.T) {
	var testCases = []struct {
		name             string
		givenPath        string
		givenFile        string
		whenPath         string
		expectCode       int
		expectStartsWith string
	}{
		{
			name:             "ok",
			givenPath:        "/walle",
			givenFile:        "_fixture/images/walle.png",
			whenPath:         "/walle",
			expectCode:       http.StatusOK,
			expectStartsWith: string([]byte{0x89, 0x50, 0x4e}),
		},
		{
			name:             "ok with relative path",
			givenPath:        "/",
			givenFile:        "./go.mod",
			whenPath:         "/",
			expectCode:       http.StatusOK,
			expectStartsWith: "module github.com/labstack/echo/v",
		},
		{
			name:             "nok file does not exist",
			givenPath:        "/",
			givenFile:        "./this-file-does-not-exist",
			whenPath:         "/",
			expectCode:       http.StatusNotFound,
			expectStartsWith: "{\"message\":\"Not Found\"}\n",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			e := New() // we are using echo.defaultFS instance
			e.File(tc.givenPath, tc.givenFile)

			c, b := request(http.MethodGet, tc.whenPath, e)
			assert.Equal(t, tc.expectCode, c)

			if len(b) > len(tc.expectStartsWith) {
				b = b[:len(tc.expectStartsWith)]
			}
			assert.Equal(t, tc.expectStartsWith, b)
		})
	}
}

func TestEchoMiddleware(t *testing.T) {
	e := New()
	buf := new(bytes.Buffer)

	e.Pre(func(next HandlerFunc) HandlerFunc {
		return func(c Context) error {
			assert.Empty(t, c.Path())
			buf.WriteString("-1")
			return next(c)
		}
	})

	e.Use(func(next HandlerFunc) HandlerFunc {
		return func(c Context) error {
			buf.WriteString("1")
			return next(c)
		}
	})

	e.Use(func(next HandlerFunc) HandlerFunc {
		return func(c Context) error {
			buf.WriteString("2")
			return next(c)
		}
	})

	e.Use(func(next HandlerFunc) HandlerFunc {
		return func(c Context) error {
			buf.WriteString("3")
			return next(c)
		}
	})

	// Route
	e.GET("/", func(c Context) error {
		return c.String(http.StatusOK, "OK")
	})

	c, b := request(http.MethodGet, "/", e)
	assert.Equal(t, "-1123", buf.String())
	assert.Equal(t, http.StatusOK, c)
	assert.Equal(t, "OK", b)
}

func TestEchoMiddlewareError(t *testing.T) {
	e := New()
	e.Use(func(next HandlerFunc) HandlerFunc {
		return func(c Context) error {
			return errors.New("error")
		}
	})
	e.GET("/", NotFoundHandler)
	c, _ := request(http.MethodGet, "/", e)
	assert.Equal(t, http.StatusInternalServerError, c)
}

func TestEchoHandler(t *testing.T) {
	e := New()

	// HandlerFunc
	e.GET("/ok", func(c Context) error {
		return c.String(http.StatusOK, "OK")
	})

	c, b := request(http.MethodGet, "/ok", e)
	assert.Equal(t, http.StatusOK, c)
	assert.Equal(t, "OK", b)
}

func TestEchoWrapHandler(t *testing.T) {
	e := New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	h := WrapHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte("test"))
		if err != nil {
			assert.Fail(t, err.Error())
		}
	}))
	if assert.NoError(t, h(c)) {
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "test", rec.Body.String())
	}
}

func TestEchoWrapMiddleware(t *testing.T) {
	e := New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	buf := new(bytes.Buffer)
	mw := WrapMiddleware(func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			buf.Write([]byte("mw"))
			h.ServeHTTP(w, r)
		})
	})
	h := mw(func(c Context) error {
		return c.String(http.StatusOK, "OK")
	})
	if assert.NoError(t, h(c)) {
		assert.Equal(t, "mw", buf.String())
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "OK", rec.Body.String())
	}
}

func TestEchoConnect(t *testing.T) {
	e := New()
	testMethod(t, http.MethodConnect, "/", e)
}

func TestEchoDelete(t *testing.T) {
	e := New()
	testMethod(t, http.MethodDelete, "/", e)
}

func TestEchoGet(t *testing.T) {
	e := New()
	testMethod(t, http.MethodGet, "/", e)
}

func TestEchoHead(t *testing.T) {
	e := New()
	testMethod(t, http.MethodHead, "/", e)
}

func TestEchoOptions(t *testing.T) {
	e := New()
	testMethod(t, http.MethodOptions, "/", e)
}

func TestEchoPatch(t *testing.T) {
	e := New()
	testMethod(t, http.MethodPatch, "/", e)
}

func TestEchoPost(t *testing.T) {
	e := New()
	testMethod(t, http.MethodPost, "/", e)
}

func TestEchoPut(t *testing.T) {
	e := New()
	testMethod(t, http.MethodPut, "/", e)
}

func TestEchoTrace(t *testing.T) {
	e := New()
	testMethod(t, http.MethodTrace, "/", e)
}

func TestEchoAny(t *testing.T) { // JFC
	e := New()
	e.Any("/", func(c Context) error {
		return c.String(http.StatusOK, "Any")
	})
}

func TestEchoMatch(t *testing.T) { // JFC
	e := New()
	e.Match([]string{http.MethodGet, http.MethodPost}, "/", func(c Context) error {
		return c.String(http.StatusOK, "Match")
	})
}

func TestEchoURL(t *testing.T) {
	e := New()
	static := func(Context) error { return nil }
	getUser := func(Context) error { return nil }
	getAny := func(Context) error { return nil }
	getFile := func(Context) error { return nil }

	e.GET("/static/file", static)
	e.GET("/users/:id", getUser)
	e.GET("/documents/*", getAny)
	g := e.Group("/group")
	g.GET("/users/:uid/files/:fid", getFile)

	assert.Equal(t, "/static/file", e.URL(static))
	assert.Equal(t, "/users/:id", e.URL(getUser))
	assert.Equal(t, "/users/1", e.URL(getUser, "1"))
	assert.Equal(t, "/users/1", e.URL(getUser, "1"))
	assert.Equal(t, "/documents/foo.txt", e.URL(getAny, "foo.txt"))
	assert.Equal(t, "/documents/*", e.URL(getAny))
	assert.Equal(t, "/group/users/1/files/:fid", e.URL(getFile, "1"))
	assert.Equal(t, "/group/users/1/files/1", e.URL(getFile, "1", "1"))
}

func TestEchoRoutes(t *testing.T) {
	e := New()
	routes := []*Route{
		{http.MethodGet, "/users/:user/events", ""},
		{http.MethodGet, "/users/:user/events/public", ""},
		{http.MethodPost, "/repos/:owner/:repo/git/refs", ""},
		{http.MethodPost, "/repos/:owner/:repo/git/tags", ""},
	}
	for _, r := range routes {
		e.Add(r.Method, r.Path, func(c Context) error {
			return c.String(http.StatusOK, "OK")
		})
	}

	if assert.Equal(t, len(routes), len(e.Routes())) {
		for _, r := range e.Routes() {
			found := false
			for _, rr := range routes {
				if r.Method == rr.Method && r.Path == rr.Path {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("Route %s %s not found", r.Method, r.Path)
			}
		}
	}
}

func TestEchoRoutesHandleAdditionalHosts(t *testing.T) {
	e := New()
	domain2Router := e.Host("domain2.router.com")
	routes := []*Route{
		{http.MethodGet, "/users/:user/events", ""},
		{http.MethodGet, "/users/:user/events/public", ""},
		{http.MethodPost, "/repos/:owner/:repo/git/refs", ""},
		{http.MethodPost, "/repos/:owner/:repo/git/tags", ""},
	}
	for _, r := range routes {
		domain2Router.Add(r.Method, r.Path, func(c Context) error {
			return c.String(http.StatusOK, "OK")
		})
	}
	e.Add(http.MethodGet, "/api", func(c Context) error {
		return c.String(http.StatusOK, "OK")
	})

	domain2Routes := e.Routers()["domain2.router.com"].Routes()

	assert.Len(t, domain2Routes, len(routes))
	for _, r := range domain2Routes {
		found := false
		for _, rr := range routes {
			if r.Method == rr.Method && r.Path == rr.Path {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Route %s %s not found", r.Method, r.Path)
		}
	}
}

func TestEchoRoutesHandleDefaultHost(t *testing.T) {
	e := New()
	routes := []*Route{
		{http.MethodGet, "/users/:user/events", ""},
		{http.MethodGet, "/users/:user/events/public", ""},
		{http.MethodPost, "/repos/:owner/:repo/git/refs", ""},
		{http.MethodPost, "/repos/:owner/:repo/git/tags", ""},
	}
	for _, r := range routes {
		e.Add(r.Method, r.Path, func(c Context) error {
			return c.String(http.StatusOK, "OK")
		})
	}
	e.Host("subdomain.mysite.site").Add(http.MethodGet, "/api", func(c Context) error {
		return c.String(http.StatusOK, "OK")
	})

	defaultRouterRoutes := e.Routes()
	assert.Len(t, defaultRouterRoutes, len(routes))
	for _, r := range defaultRouterRoutes {
		found := false
		for _, rr := range routes {
			if r.Method == rr.Method && r.Path == rr.Path {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Route %s %s not found", r.Method, r.Path)
		}
	}
}

func TestEchoServeHTTPPathEncoding(t *testing.T) {
	e := New()
	e.GET("/with/slash", func(c Context) error {
		return c.String(http.StatusOK, "/with/slash")
	})
	e.GET("/:id", func(c Context) error {
		return c.String(http.StatusOK, c.Param("id"))
	})

	var testCases = []struct {
		name         string
		whenURL      string
		expectURL    string
		expectStatus int
	}{
		{
			name:         "url with encoding is not decoded for routing",
			whenURL:      "/with%2Fslash",
			expectURL:    "with%2Fslash", // `%2F` is not decoded to `/` for routing
			expectStatus: http.StatusOK,
		},
		{
			name:         "url without encoding is used as is",
			whenURL:      "/with/slash",
			expectURL:    "/with/slash",
			expectStatus: http.StatusOK,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.whenURL, nil)
			rec := httptest.NewRecorder()

			e.ServeHTTP(rec, req)

			assert.Equal(t, tc.expectStatus, rec.Code)
			assert.Equal(t, tc.expectURL, rec.Body.String())
		})
	}
}

func TestEchoHost(t *testing.T) {
	okHandler := func(c Context) error { return c.String(http.StatusOK, http.StatusText(http.StatusOK)) }
	teapotHandler := func(c Context) error { return c.String(http.StatusTeapot, http.StatusText(http.StatusTeapot)) }
	acceptHandler := func(c Context) error { return c.String(http.StatusAccepted, http.StatusText(http.StatusAccepted)) }
	teapotMiddleware := MiddlewareFunc(func(next HandlerFunc) HandlerFunc { return teapotHandler })

	e := New()
	e.GET("/", acceptHandler)
	e.GET("/foo", acceptHandler)

	ok := e.Host("ok.com")
	ok.GET("/", okHandler)
	ok.GET("/foo", okHandler)

	teapot := e.Host("teapot.com")
	teapot.GET("/", teapotHandler)
	teapot.GET("/foo", teapotHandler)

	middle := e.Host("middleware.com", teapotMiddleware)
	middle.GET("/", okHandler)
	middle.GET("/foo", okHandler)

	var testCases = []struct {
		name         string
		whenHost     string
		whenPath     string
		expectBody   string
		expectStatus int
	}{
		{
			name:         "No Host Root",
			whenHost:     "",
			whenPath:     "/",
			expectBody:   http.StatusText(http.StatusAccepted),
			expectStatus: http.StatusAccepted,
		},
		{
			name:         "No Host Foo",
			whenHost:     "",
			whenPath:     "/foo",
			expectBody:   http.StatusText(http.StatusAccepted),
			expectStatus: http.StatusAccepted,
		},
		{
			name:         "OK Host Root",
			whenHost:     "ok.com",
			whenPath:     "/",
			expectBody:   http.StatusText(http.StatusOK),
			expectStatus: http.StatusOK,
		},
		{
			name:         "OK Host Foo",
			whenHost:     "ok.com",
			whenPath:     "/foo",
			expectBody:   http.StatusText(http.StatusOK),
			expectStatus: http.StatusOK,
		},
		{
			name:         "Teapot Host Root",
			whenHost:     "teapot.com",
			whenPath:     "/",
			expectBody:   http.StatusText(http.StatusTeapot),
			expectStatus: http.StatusTeapot,
		},
		{
			name:         "Teapot Host Foo",
			whenHost:     "teapot.com",
			whenPath:     "/foo",
			expectBody:   http.StatusText(http.StatusTeapot),
			expectStatus: http.StatusTeapot,
		},
		{
			name:         "Middleware Host",
			whenHost:     "middleware.com",
			whenPath:     "/",
			expectBody:   http.StatusText(http.StatusTeapot),
			expectStatus: http.StatusTeapot,
		},
		{
			name:         "Middleware Host Foo",
			whenHost:     "middleware.com",
			whenPath:     "/foo",
			expectBody:   http.StatusText(http.StatusTeapot),
			expectStatus: http.StatusTeapot,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.whenPath, nil)
			req.Host = tc.whenHost
			rec := httptest.NewRecorder()

			e.ServeHTTP(rec, req)

			assert.Equal(t, tc.expectStatus, rec.Code)
			assert.Equal(t, tc.expectBody, rec.Body.String())
		})
	}
}

func TestEchoGroup(t *testing.T) {
	e := New()
	buf := new(bytes.Buffer)
	e.Use(MiddlewareFunc(func(next HandlerFunc) HandlerFunc {
		return func(c Context) error {
			buf.WriteString("0")
			return next(c)
		}
	}))
	h := func(c Context) error {
		return c.NoContent(http.StatusOK)
	}

	//--------
	// Routes
	//--------

	e.GET("/users", h)

	// Group
	g1 := e.Group("/group1")
	g1.Use(func(next HandlerFunc) HandlerFunc {
		return func(c Context) error {
			buf.WriteString("1")
			return next(c)
		}
	})
	g1.GET("", h)

	// Nested groups with middleware
	g2 := e.Group("/group2")
	g2.Use(func(next HandlerFunc) HandlerFunc {
		return func(c Context) error {
			buf.WriteString("2")
			return next(c)
		}
	})
	g3 := g2.Group("/group3")
	g3.Use(func(next HandlerFunc) HandlerFunc {
		return func(c Context) error {
			buf.WriteString("3")
			return next(c)
		}
	})
	g3.GET("", h)

	request(http.MethodGet, "/users", e)
	assert.Equal(t, "0", buf.String())

	buf.Reset()
	request(http.MethodGet, "/group1", e)
	assert.Equal(t, "01", buf.String())

	buf.Reset()
	request(http.MethodGet, "/group2/group3", e)
	assert.Equal(t, "023", buf.String())
}

func TestEchoNotFound(t *testing.T) {
	e := New()
	req := httptest.NewRequest(http.MethodGet, "/files", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestEcho_RouteNotFound(t *testing.T) {
	var testCases = []struct {
		name        string
		whenURL     string
		expectRoute interface{}
		expectCode  int
	}{
		{
			name:        "404, route to static not found handler /a/c/xx",
			whenURL:     "/a/c/xx",
			expectRoute: "GET /a/c/xx",
			expectCode:  http.StatusNotFound,
		},
		{
			name:        "404, route to path param not found handler /a/:file",
			whenURL:     "/a/echo.exe",
			expectRoute: "GET /a/:file",
			expectCode:  http.StatusNotFound,
		},
		{
			name:        "404, route to any not found handler /*",
			whenURL:     "/b/echo.exe",
			expectRoute: "GET /*",
			expectCode:  http.StatusNotFound,
		},
		{
			name:        "200, route /a/c/df to /a/c/df",
			whenURL:     "/a/c/df",
			expectRoute: "GET /a/c/df",
			expectCode:  http.StatusOK,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			e := New()

			okHandler := func(c Context) error {
				return c.String(http.StatusOK, c.Request().Method+" "+c.Path())
			}
			notFoundHandler := func(c Context) error {
				return c.String(http.StatusNotFound, c.Request().Method+" "+c.Path())
			}

			e.GET("/", okHandler)
			e.GET("/a/c/df", okHandler)
			e.GET("/a/b*", okHandler)
			e.PUT("/*", okHandler)

			e.RouteNotFound("/a/c/xx", notFoundHandler)  // static
			e.RouteNotFound("/a/:file", notFoundHandler) // param
			e.RouteNotFound("/*", notFoundHandler)       // any

			req := httptest.NewRequest(http.MethodGet, tc.whenURL, nil)
			rec := httptest.NewRecorder()

			e.ServeHTTP(rec, req)

			assert.Equal(t, tc.expectCode, rec.Code)
			assert.Equal(t, tc.expectRoute, rec.Body.String())
		})
	}
}

func TestEchoMethodNotAllowed(t *testing.T) {
	e := New()

	e.GET("/", func(c Context) error {
		return c.String(http.StatusOK, "Echo!")
	})
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
	assert.Equal(t, "OPTIONS, GET", rec.Header().Get(HeaderAllow))
}

func TestEchoContext(t *testing.T) {
	e := New()
	c := e.AcquireContext()
	assert.IsType(t, new(context), c)
	e.ReleaseContext(c)
}

func waitForServerStart(e *Echo, errChan <-chan error, isTLS bool) error {
	ctx, cancel := stdContext.WithTimeout(stdContext.Background(), 200*time.Millisecond)
	defer cancel()

	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			var addr net.Addr
			if isTLS {
				addr = e.TLSListenerAddr()
			} else {
				addr = e.ListenerAddr()
			}
			if addr != nil && strings.Contains(addr.String(), ":") {
				return nil // was started
			}
		case err := <-errChan:
			if err == http.ErrServerClosed {
				return nil
			}
			return err
		}
	}
}

func TestEchoStart(t *testing.T) {
	e := New()
	errChan := make(chan error)

	go func() {
		err := e.Start(":0")
		if err != nil {
			errChan <- err
		}
	}()

	err := waitForServerStart(e, errChan, false)
	assert.NoError(t, err)

	assert.NoError(t, e.Close())
}

func TestEcho_StartTLS(t *testing.T) {
	var testCases = []struct {
		name        string
		addr        string
		certFile    string
		keyFile     string
		expectError string
	}{
		{
			name: "ok",
			addr: ":0",
		},
		{
			name:        "nok, invalid certFile",
			addr:        ":0",
			certFile:    "not existing",
			expectError: "open not existing: no such file or directory",
		},
		{
			name:        "nok, invalid keyFile",
			addr:        ":0",
			keyFile:     "not existing",
			expectError: "open not existing: no such file or directory",
		},
		{
			name:        "nok, failed to create cert out of certFile and keyFile",
			addr:        ":0",
			keyFile:     "_fixture/certs/cert.pem", // we are passing cert instead of key
			expectError: "tls: found a certificate rather than a key in the PEM for the private key",
		},
		{
			name:        "nok, invalid tls address",
			addr:        "nope",
			expectError: "listen tcp: address nope: missing port in address",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			e := New()
			errChan := make(chan error)

			go func() {
				certFile := "_fixture/certs/cert.pem"
				if tc.certFile != "" {
					certFile = tc.certFile
				}
				keyFile := "_fixture/certs/key.pem"
				if tc.keyFile != "" {
					keyFile = tc.keyFile
				}

				err := e.StartTLS(tc.addr, certFile, keyFile)
				if err != nil {
					errChan <- err
				}
			}()

			err := waitForServerStart(e, errChan, true)
			if tc.expectError != "" {
				if _, ok := err.(*os.PathError); ok {
					assert.Error(t, err) // error messages for unix and windows are different. so test only error type here
				} else {
					assert.EqualError(t, err, tc.expectError)
				}
			} else {
				assert.NoError(t, err)
			}

			assert.NoError(t, e.Close())
		})
	}
}

func TestEchoStartTLSAndStart(t *testing.T) {
	// We test if Echo and listeners work correctly when Echo is simultaneously attached to HTTP and HTTPS server
	e := New()
	e.GET("/", func(c Context) error {
		return c.String(http.StatusOK, "OK")
	})

	errTLSChan := make(chan error)
	go func() {
		certFile := "_fixture/certs/cert.pem"
		keyFile := "_fixture/certs/key.pem"
		err := e.StartTLS("localhost:", certFile, keyFile)
		if err != nil {
			errTLSChan <- err
		}
	}()

	err := waitForServerStart(e, errTLSChan, true)
	assert.NoError(t, err)
	defer func() {
		if err := e.Shutdown(stdContext.Background()); err != nil {
			t.Error(err)
		}
	}()

	// check if HTTPS works (note: we are using self signed certs so InsecureSkipVerify=true)
	client := &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}}
	res, err := client.Get("https://" + e.TLSListenerAddr().String())
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, res.StatusCode)

	errChan := make(chan error)
	go func() {
		err := e.Start("localhost:")
		if err != nil {
			errChan <- err
		}
	}()
	err = waitForServerStart(e, errChan, false)
	assert.NoError(t, err)

	// now we are serving both HTTPS and HTTP listeners. see if HTTP works in addition to HTTPS
	res, err = http.Get("http://" + e.ListenerAddr().String())
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, res.StatusCode)

	// see if HTTPS works after HTTP listener is also added
	res, err = client.Get("https://" + e.TLSListenerAddr().String())
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, res.StatusCode)
}

func TestEchoStartTLSByteString(t *testing.T) {
	cert, err := os.ReadFile("_fixture/certs/cert.pem")
	require.NoError(t, err)
	key, err := os.ReadFile("_fixture/certs/key.pem")
	require.NoError(t, err)

	testCases := []struct {
		cert        interface{}
		key         interface{}
		expectedErr error
		name        string
	}{
		{
			cert:        "_fixture/certs/cert.pem",
			key:         "_fixture/certs/key.pem",
			expectedErr: nil,
			name:        `ValidCertAndKeyFilePath`,
		},
		{
			cert:        cert,
			key:         key,
			expectedErr: nil,
			name:        `ValidCertAndKeyByteString`,
		},
		{
			cert:        cert,
			key:         1,
			expectedErr: ErrInvalidCertOrKeyType,
			name:        `InvalidKeyType`,
		},
		{
			cert:        0,
			key:         key,
			expectedErr: ErrInvalidCertOrKeyType,
			name:        `InvalidCertType`,
		},
		{
			cert:        0,
			key:         1,
			expectedErr: ErrInvalidCertOrKeyType,
			name:        `InvalidCertAndKeyTypes`,
		},
	}

	for _, test := range testCases {
		test := test
		t.Run(test.name, func(t *testing.T) {
			e := New()
			e.HideBanner = true

			errChan := make(chan error)

			go func() {
				errChan <- e.StartTLS(":0", test.cert, test.key)
			}()

			err := waitForServerStart(e, errChan, true)
			if test.expectedErr != nil {
				assert.EqualError(t, err, test.expectedErr.Error())
			} else {
				assert.NoError(t, err)
			}

			assert.NoError(t, e.Close())
		})
	}
}

func TestEcho_StartAutoTLS(t *testing.T) {
	var testCases = []struct {
		name        string
		addr        string
		expectError string
	}{
		{
			name: "ok",
			addr: ":0",
		},
		{
			name:        "nok, invalid address",
			addr:        "nope",
			expectError: "listen tcp: address nope: missing port in address",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			e := New()
			errChan := make(chan error)

			go func() {
				errChan <- e.StartAutoTLS(tc.addr)
			}()

			err := waitForServerStart(e, errChan, true)
			if tc.expectError != "" {
				assert.EqualError(t, err, tc.expectError)
			} else {
				assert.NoError(t, err)
			}

			assert.NoError(t, e.Close())
		})
	}
}

func TestEcho_StartH2CServer(t *testing.T) {
	var testCases = []struct {
		name        string
		addr        string
		expectError string
	}{
		{
			name: "ok",
			addr: ":0",
		},
		{
			name:        "nok, invalid address",
			addr:        "nope",
			expectError: "listen tcp: address nope: missing port in address",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			e := New()
			e.Debug = true
			h2s := &http2.Server{}

			errChan := make(chan error)
			go func() {
				err := e.StartH2CServer(tc.addr, h2s)
				if err != nil {
					errChan <- err
				}
			}()

			err := waitForServerStart(e, errChan, false)
			if tc.expectError != "" {
				assert.EqualError(t, err, tc.expectError)
			} else {
				assert.NoError(t, err)
			}

			assert.NoError(t, e.Close())
		})
	}
}

func testMethod(t *testing.T, method, path string, e *Echo) {
	p := reflect.ValueOf(path)
	h := reflect.ValueOf(func(c Context) error {
		return c.String(http.StatusOK, method)
	})
	i := interface{}(e)
	reflect.ValueOf(i).MethodByName(method).Call([]reflect.Value{p, h})
	_, body := request(method, path, e)
	assert.Equal(t, method, body)
}

func request(method, path string, e *Echo) (int, string) {
	req := httptest.NewRequest(method, path, nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func TestHTTPError(t *testing.T) {
	t.Run("non-internal", func(t *testing.T) {
		err := NewHTTPError(http.StatusBadRequest, map[string]interface{}{
			"code": 12,
		})

		assert.Equal(t, "code=400, message=map[code:12]", err.Error())
	})

	t.Run("internal and SetInternal", func(t *testing.T) {
		err := NewHTTPError(http.StatusBadRequest, map[string]interface{}{
			"code": 12,
		})
		err.SetInternal(errors.New("internal error"))
		assert.Equal(t, "code=400, message=map[code:12], internal=internal error", err.Error())
	})

	t.Run("internal and WithInternal", func(t *testing.T) {
		err := NewHTTPError(http.StatusBadRequest, map[string]interface{}{
			"code": 12,
		})
		err = err.WithInternal(errors.New("internal error"))
		assert.Equal(t, "code=400, message=map[code:12], internal=internal error", err.Error())
	})
}

func TestHTTPError_Unwrap(t *testing.T) {
	t.Run("non-internal", func(t *testing.T) {
		err := NewHTTPError(http.StatusBadRequest, map[string]interface{}{
			"code": 12,
		})

		assert.Nil(t, errors.Unwrap(err))
	})

	t.Run("unwrap internal and SetInternal", func(t *testing.T) {
		err := NewHTTPError(http.StatusBadRequest, map[string]interface{}{
			"code": 12,
		})
		err.SetInternal(errors.New("internal error"))
		assert.Equal(t, "internal error", errors.Unwrap(err).Error())
	})

	t.Run("unwrap internal and WithInternal", func(t *testing.T) {
		err := NewHTTPError(http.StatusBadRequest, map[string]interface{}{
			"code": 12,
		})
		err = err.WithInternal(errors.New("internal error"))
		assert.Equal(t, "internal error", errors.Unwrap(err).Error())
	})
}

type customError struct {
	s string
}

func (ce *customError) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf(`{"x":"%v"}`, ce.s)), nil
}

func (ce *customError) Error() string {
	return ce.s
}

func TestDefaultHTTPErrorHandler(t *testing.T) {
	var testCases = []struct {
		name       string
		givenDebug bool
		whenPath   string
		expectCode int
		expectBody string
	}{
		{
			name:       "with Debug=true plain response contains error message",
			givenDebug: true,
			whenPath:   "/plain",
			expectCode: http.StatusInternalServerError,
			expectBody: "{\n  \"error\": \"an error occurred\",\n  \"message\": \"Internal Server Error\"\n}\n",
		},
		{
			name:       "with Debug=true special handling for HTTPError",
			givenDebug: true,
			whenPath:   "/badrequest",
			expectCode: http.StatusBadRequest,
			expectBody: "{\n  \"error\": \"code=400, message=Invalid request\",\n  \"message\": \"Invalid request\"\n}\n",
		},
		{
			name:       "with Debug=true complex errors are serialized to pretty JSON",
			givenDebug: true,
			whenPath:   "/servererror",
			expectCode: http.StatusInternalServerError,
			expectBody: "{\n  \"code\": 33,\n  \"error\": \"stackinfo\",\n  \"message\": \"Something bad happened\"\n}\n",
		},
		{
			name:       "with Debug=true if the body is already set HTTPErrorHandler should not add anything to response body",
			givenDebug: true,
			whenPath:   "/early-return",
			expectCode: http.StatusOK,
			expectBody: "OK",
		},
		{
			name:       "with Debug=true internal error should be reflected in the message",
			givenDebug: true,
			whenPath:   "/internal-error",
			expectCode: http.StatusBadRequest,
			expectBody: "{\n  \"error\": \"code=400, message=Bad Request, internal=internal error message body\",\n  \"message\": \"Bad Request\"\n}\n",
		},
		{
			name:       "with Debug=false the error response is shortened",
			whenPath:   "/plain",
			expectCode: http.StatusInternalServerError,
			expectBody: "{\"message\":\"Internal Server Error\"}\n",
		},
		{
			name:       "with Debug=false the error response is shortened",
			whenPath:   "/badrequest",
			expectCode: http.StatusBadRequest,
			expectBody: "{\"message\":\"Invalid request\"}\n",
		},
		{
			name:       "with Debug=false No difference for error response with non plain string errors",
			whenPath:   "/servererror",
			expectCode: http.StatusInternalServerError,
			expectBody: "{\"code\":33,\"error\":\"stackinfo\",\"message\":\"Something bad happened\"}\n",
		},
		{
			name:       "with Debug=false when httpError contains an error",
			whenPath:   "/error-in-httperror",
			expectCode: http.StatusBadRequest,
			expectBody: "{\"message\":\"error in httperror\"}\n",
		},
		{
			name:       "with Debug=false when httpError contains an error",
			whenPath:   "/customerror-in-httperror",
			expectCode: http.StatusBadRequest,
			expectBody: "{\"x\":\"custom error msg\"}\n",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			e := New()
			e.Debug = tc.givenDebug // With Debug=true plain response contains error message

			e.Any("/plain", func(c Context) error {
				return errors.New("an error occurred")
			})

			e.Any("/badrequest", func(c Context) error { // and special handling for HTTPError
				return NewHTTPError(http.StatusBadRequest, "Invalid request")
			})

			e.Any("/servererror", func(c Context) error { // complex errors are serialized to pretty JSON
				return NewHTTPError(http.StatusInternalServerError, map[string]interface{}{
					"code":    33,
					"message": "Something bad happened",
					"error":   "stackinfo",
				})
			})

			// if the body is already set HTTPErrorHandler should not add anything to response body
			e.Any("/early-return", func(c Context) error {
				err := c.String(http.StatusOK, "OK")
				if err != nil {
					assert.Fail(t, err.Error())
				}
				return errors.New("ERROR")
			})

			// internal error should be reflected in the message
			e.GET("/internal-error", func(c Context) error {
				err := errors.New("internal error message body")
				return NewHTTPError(http.StatusBadRequest).SetInternal(err)
			})

			e.GET("/error-in-httperror", func(c Context) error {
				return NewHTTPError(http.StatusBadRequest, errors.New("error in httperror"))
			})

			e.GET("/customerror-in-httperror", func(c Context) error {
				return NewHTTPError(http.StatusBadRequest, &customError{s: "custom error msg"})
			})

			c, b := request(http.MethodGet, tc.whenPath, e)
			assert.Equal(t, tc.expectCode, c)
			assert.Equal(t, tc.expectBody, b)
		})
	}
}

func TestEchoClose(t *testing.T) {
	e := New()
	errCh := make(chan error)

	go func() {
		errCh <- e.Start(":0")
	}()

	err := waitForServerStart(e, errCh, false)
	assert.NoError(t, err)

	if err := e.Close(); err != nil {
		t.Fatal(err)
	}

	assert.NoError(t, e.Close())

	err = <-errCh
	assert.Equal(t, err.Error(), "http: Server closed")
}

func TestEchoShutdown(t *testing.T) {
	e := New()
	errCh := make(chan error)

	go func() {
		errCh <- e.Start(":0")
	}()

	err := waitForServerStart(e, errCh, false)
	assert.NoError(t, err)

	if err := e.Close(); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := stdContext.WithTimeout(stdContext.Background(), 10*time.Second)
	defer cancel()
	assert.NoError(t, e.Shutdown(ctx))

	err = <-errCh
	assert.Equal(t, err.Error(), "http: Server closed")
}

var listenerNetworkTests = []struct {
	test    string
	network string
	address string
}{
	{"tcp ipv4 address", "tcp", "127.0.0.1:1323"},
	{"tcp ipv6 address", "tcp", "[::1]:1323"},
	{"tcp4 ipv4 address", "tcp4", "127.0.0.1:1323"},
	{"tcp6 ipv6 address", "tcp6", "[::1]:1323"},
}

func supportsIPv6() bool {
	addrs, _ := net.InterfaceAddrs()
	for _, addr := range addrs {
		// Check if any interface has local IPv6 assigned
		if strings.Contains(addr.String(), "::1") {
			return true
		}
	}
	return false
}

func TestEchoListenerNetwork(t *testing.T) {
	hasIPv6 := supportsIPv6()
	for _, tt := range listenerNetworkTests {
		if !hasIPv6 && strings.Contains(tt.address, "::") {
			t.Skip("Skipping testing IPv6 for " + tt.address + ", not available")
			continue
		}
		t.Run(tt.test, func(t *testing.T) {
			e := New()
			e.ListenerNetwork = tt.network

			// HandlerFunc
			e.GET("/ok", func(c Context) error {
				return c.String(http.StatusOK, "OK")
			})

			errCh := make(chan error)

			go func() {
				errCh <- e.Start(tt.address)
			}()

			err := waitForServerStart(e, errCh, false)
			assert.NoError(t, err)

			if resp, err := http.Get(fmt.Sprintf("http://%s/ok", tt.address)); err == nil {
				defer func(Body io.ReadCloser) {
					err := Body.Close()
					if err != nil {
						assert.Fail(t, err.Error())
					}
				}(resp.Body)
				assert.Equal(t, http.StatusOK, resp.StatusCode)

				if body, err := io.ReadAll(resp.Body); err == nil {
					assert.Equal(t, "OK", string(body))
				} else {
					assert.Fail(t, err.Error())
				}

			} else {
				assert.Fail(t, err.Error())
			}

			if err := e.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestEchoListenerNetworkInvalid(t *testing.T) {
	e := New()
	e.ListenerNetwork = "unix"

	// HandlerFunc
	e.GET("/ok", func(c Context) error {
		return c.String(http.StatusOK, "OK")
	})

	assert.Equal(t, ErrInvalidListenerNetwork, e.Start(":1323"))
}

func TestEcho_OnAddRouteHandler(t *testing.T) {
	type rr struct {
		host       string
		route      Route
		handler    HandlerFunc
		middleware []MiddlewareFunc
	}
	dummyHandler := func(Context) error { return nil }
	e := New()

	added := make([]rr, 0)
	e.OnAddRouteHandler = func(host string, route Route, handler HandlerFunc, middleware []MiddlewareFunc) {
		added = append(added, rr{
			host:       host,
			route:      route,
			handler:    handler,
			middleware: middleware,
		})
	}

	e.GET("/static", dummyHandler)
	e.Host("domain.site").GET("/static/*", dummyHandler, func(next HandlerFunc) HandlerFunc {
		return func(c Context) error {
			return next(c)
		}
	})

	assert.Len(t, added, 2)

	assert.Equal(t, "", added[0].host)
	assert.Equal(t, Route{Method: http.MethodGet, Path: "/static", Name: "github.com/labstack/echo/v4.TestEcho_OnAddRouteHandler.func1"}, added[0].route)
	assert.Len(t, added[0].middleware, 0)

	assert.Equal(t, "domain.site", added[1].host)
	assert.Equal(t, Route{Method: http.MethodGet, Path: "/static/*", Name: "github.com/labstack/echo/v4.TestEcho_OnAddRouteHandler.func1"}, added[1].route)
	assert.Len(t, added[1].middleware, 1)
}

func TestEchoReverse(t *testing.T) {
	var testCases = []struct {
		name          string
		whenRouteName string
		whenParams    []interface{}
		expect        string
	}{
		{
			name:          "ok, not existing path returns empty url",
			whenRouteName: "not-existing",
			expect:        "",
		},
		{
			name:          "ok,static with no params",
			whenRouteName: "/static",
			expect:        "/static",
		},
		{
			name:          "ok,static with non existent param",
			whenRouteName: "/static",
			whenParams:    []interface{}{"missing param"},
			expect:        "/static",
		},
		{
			name:          "ok, wildcard with no params",
			whenRouteName: "/static/*",
			expect:        "/static/*",
		},
		{
			name:          "ok, wildcard with params",
			whenRouteName: "/static/*",
			whenParams:    []interface{}{"foo.txt"},
			expect:        "/static/foo.txt",
		},
		{
			name:          "ok, single param without param",
			whenRouteName: "/params/:foo",
			expect:        "/params/:foo",
		},
		{
			name:          "ok, single param with param",
			whenRouteName: "/params/:foo",
			whenParams:    []interface{}{"one"},
			expect:        "/params/one",
		},
		{
			name:          "ok, multi param without params",
			whenRouteName: "/params/:foo/bar/:qux",
			expect:        "/params/:foo/bar/:qux",
		},
		{
			name:          "ok, multi param with one param",
			whenRouteName: "/params/:foo/bar/:qux",
			whenParams:    []interface{}{"one"},
			expect:        "/params/one/bar/:qux",
		},
		{
			name:          "ok, multi param with all params",
			whenRouteName: "/params/:foo/bar/:qux",
			whenParams:    []interface{}{"one", "two"},
			expect:        "/params/one/bar/two",
		},
		{
			name:          "ok, multi param + wildcard with all params",
			whenRouteName: "/params/:foo/bar/:qux/*",
			whenParams:    []interface{}{"one", "two", "three"},
			expect:        "/params/one/bar/two/three",
		},
		{
			name:          "ok, backslash is not escaped",
			whenRouteName: "/backslash",
			whenParams:    []interface{}{"test"},
			expect:        `/a\b/test`,
		},
		{
			name:          "ok, escaped colon verbs",
			whenRouteName: "/params:customVerb",
			whenParams:    []interface{}{"PATCH"},
			expect:        `/params:PATCH`,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			e := New()
			dummyHandler := func(Context) error { return nil }

			e.GET("/static", dummyHandler).Name = "/static"
			e.GET("/static/*", dummyHandler).Name = "/static/*"
			e.GET("/params/:foo", dummyHandler).Name = "/params/:foo"
			e.GET("/params/:foo/bar/:qux", dummyHandler).Name = "/params/:foo/bar/:qux"
			e.GET("/params/:foo/bar/:qux/*", dummyHandler).Name = "/params/:foo/bar/:qux/*"
			e.GET("/a\\b/:x", dummyHandler).Name = "/backslash"
			e.GET("/params\\::customVerb", dummyHandler).Name = "/params:customVerb"

			assert.Equal(t, tc.expect, e.Reverse(tc.whenRouteName, tc.whenParams...))
		})
	}
}

func TestEchoReverseHandleHostProperly(t *testing.T) {
	dummyHandler := func(Context) error { return nil }

	e := New()

	// routes added to the default router are different form different hosts
	e.GET("/static", dummyHandler).Name = "default-host /static"
	e.GET("/static/*", dummyHandler).Name = "xxx"

	// different host
	h := e.Host("the_host")
	h.GET("/static", dummyHandler).Name = "host2 /static"
	h.GET("/static/v2/*", dummyHandler).Name = "xxx"

	assert.Equal(t, "/static", e.Reverse("default-host /static"))
	// when actual route does not have params and we provide some to Reverse we should get that route url back
	assert.Equal(t, "/static", e.Reverse("default-host /static", "missing param"))

	host2Router := e.Routers()["the_host"]
	assert.Equal(t, "/static", host2Router.Reverse("host2 /static"))
	assert.Equal(t, "/static", host2Router.Reverse("host2 /static", "missing param"))

	assert.Equal(t, "/static/v2/*", host2Router.Reverse("xxx"))
	assert.Equal(t, "/static/v2/foo.txt", host2Router.Reverse("xxx", "foo.txt"))

}

func TestEcho_ListenerAddr(t *testing.T) {
	e := New()

	addr := e.ListenerAddr()
	assert.Nil(t, addr)

	errCh := make(chan error)
	go func() {
		errCh <- e.Start(":0")
	}()

	err := waitForServerStart(e, errCh, false)
	assert.NoError(t, err)
}

func TestEcho_TLSListenerAddr(t *testing.T) {
	cert, err := os.ReadFile("_fixture/certs/cert.pem")
	require.NoError(t, err)
	key, err := os.ReadFile("_fixture/certs/key.pem")
	require.NoError(t, err)

	e := New()

	addr := e.TLSListenerAddr()
	assert.Nil(t, addr)

	errCh := make(chan error)
	go func() {
		errCh <- e.StartTLS(":0", cert, key)
	}()

	err = waitForServerStart(e, errCh, true)
	assert.NoError(t, err)
}

func TestEcho_StartServer(t *testing.T) {
	cert, err := os.ReadFile("_fixture/certs/cert.pem")
	require.NoError(t, err)
	key, err := os.ReadFile("_fixture/certs/key.pem")
	require.NoError(t, err)
	certs, err := tls.X509KeyPair(cert, key)
	require.NoError(t, err)

	var testCases = []struct {
		name        string
		addr        string
		TLSConfig   *tls.Config
		expectError string
	}{
		{
			name: "ok",
			addr: ":0",
		},
		{
			name:      "ok, start with TLS",
			addr:      ":0",
			TLSConfig: &tls.Config{Certificates: []tls.Certificate{certs}},
		},
		{
			name:        "nok, invalid address",
			addr:        "nope",
			expectError: "listen tcp: address nope: missing port in address",
		},
		{
			name:        "nok, invalid tls address",
			addr:        "nope",
			TLSConfig:   &tls.Config{InsecureSkipVerify: true},
			expectError: "listen tcp: address nope: missing port in address",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			e := New()
			e.Debug = true

			server := new(http.Server)
			server.Addr = tc.addr
			if tc.TLSConfig != nil {
				server.TLSConfig = tc.TLSConfig
			}

			errCh := make(chan error)
			go func() {
				errCh <- e.StartServer(server)
			}()

			err := waitForServerStart(e, errCh, tc.TLSConfig != nil)
			if tc.expectError != "" {
				assert.EqualError(t, err, tc.expectError)
			} else {
				assert.NoError(t, err)
			}
			assert.NoError(t, e.Close())
		})
	}
}

func benchmarkEchoRoutes(b *testing.B, routes []*Route) {
	e := New()
	req := httptest.NewRequest("GET", "/", nil)
	u := req.URL
	w := httptest.NewRecorder()

	b.ReportAllocs()

	// Add routes
	for _, route := range routes {
		e.Add(route.Method, route.Path, func(c Context) error {
			return nil
		})
	}

	// Find routes
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, route := range routes {
			req.Method = route.Method
			u.Path = route.Path
			e.ServeHTTP(w, req)
		}
	}
}

func BenchmarkEchoStaticRoutes(b *testing.B) {
	benchmarkEchoRoutes(b, staticRoutes)
}

func BenchmarkEchoStaticRoutesMisses(b *testing.B) {
	benchmarkEchoRoutes(b, staticRoutes)
}

func BenchmarkEchoGitHubAPI(b *testing.B) {
	benchmarkEchoRoutes(b, gitHubAPI)
}

func BenchmarkEchoGitHubAPIMisses(b *testing.B) {
	benchmarkEchoRoutes(b, gitHubAPI)
}

func BenchmarkEchoParseAPI(b *testing.B) {
	benchmarkEchoRoutes(b, parseAPI)
}
