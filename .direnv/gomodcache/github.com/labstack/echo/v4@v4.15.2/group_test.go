// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: © 2015 LabStack LLC and Echo contributors

package echo

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TODO: Fix me
func TestGroup(t *testing.T) {
	g := New().Group("/group")
	h := func(Context) error { return nil }
	g.CONNECT("/", h)
	g.DELETE("/", h)
	g.GET("/", h)
	g.HEAD("/", h)
	g.OPTIONS("/", h)
	g.PATCH("/", h)
	g.POST("/", h)
	g.PUT("/", h)
	g.TRACE("/", h)
	g.Any("/", h)
	g.Match([]string{http.MethodGet, http.MethodPost}, "/", h)
	g.Static("/static", "/tmp")
	g.File("/walle", "_fixture/images//walle.png")
}

func TestGroupFile(t *testing.T) {
	e := New()
	g := e.Group("/group")
	g.File("/walle", "_fixture/images/walle.png")
	expectedData, err := os.ReadFile("_fixture/images/walle.png")
	assert.Nil(t, err)
	req := httptest.NewRequest(http.MethodGet, "/group/walle", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, expectedData, rec.Body.Bytes())
}

func TestGroupRouteMiddleware(t *testing.T) {
	// Ensure middleware slices are not re-used
	e := New()
	g := e.Group("/group")
	h := func(Context) error { return nil }
	m1 := func(next HandlerFunc) HandlerFunc {
		return func(c Context) error {
			return next(c)
		}
	}
	m2 := func(next HandlerFunc) HandlerFunc {
		return func(c Context) error {
			return next(c)
		}
	}
	m3 := func(next HandlerFunc) HandlerFunc {
		return func(c Context) error {
			return next(c)
		}
	}
	m4 := func(next HandlerFunc) HandlerFunc {
		return func(c Context) error {
			return c.NoContent(404)
		}
	}
	m5 := func(next HandlerFunc) HandlerFunc {
		return func(c Context) error {
			return c.NoContent(405)
		}
	}
	g.Use(m1, m2, m3)
	g.GET("/404", h, m4)
	g.GET("/405", h, m5)

	c, _ := request(http.MethodGet, "/group/404", e)
	assert.Equal(t, 404, c)
	c, _ = request(http.MethodGet, "/group/405", e)
	assert.Equal(t, 405, c)
}

func TestGroupRouteMiddlewareWithMatchAny(t *testing.T) {
	// Ensure middleware and match any routes do not conflict
	e := New()
	g := e.Group("/group")
	m1 := func(next HandlerFunc) HandlerFunc {
		return func(c Context) error {
			return next(c)
		}
	}
	m2 := func(next HandlerFunc) HandlerFunc {
		return func(c Context) error {
			return c.String(http.StatusOK, c.Path())
		}
	}
	h := func(c Context) error {
		return c.String(http.StatusOK, c.Path())
	}
	g.Use(m1)
	g.GET("/help", h, m2)
	g.GET("/*", h, m2)
	g.GET("", h, m2)
	e.GET("unrelated", h, m2)
	e.GET("*", h, m2)

	_, m := request(http.MethodGet, "/group/help", e)
	assert.Equal(t, "/group/help", m)
	_, m = request(http.MethodGet, "/group/help/other", e)
	assert.Equal(t, "/group/*", m)
	_, m = request(http.MethodGet, "/group/404", e)
	assert.Equal(t, "/group/*", m)
	_, m = request(http.MethodGet, "/group", e)
	assert.Equal(t, "/group", m)
	_, m = request(http.MethodGet, "/other", e)
	assert.Equal(t, "/*", m)
	_, m = request(http.MethodGet, "/", e)
	assert.Equal(t, "/*", m)

}

func TestGroup_RouteNotFound(t *testing.T) {
	var testCases = []struct {
		name        string
		whenURL     string
		expectRoute interface{}
		expectCode  int
	}{
		{
			name:        "404, route to static not found handler /group/a/c/xx",
			whenURL:     "/group/a/c/xx",
			expectRoute: "GET /group/a/c/xx",
			expectCode:  http.StatusNotFound,
		},
		{
			name:        "404, route to path param not found handler /group/a/:file",
			whenURL:     "/group/a/echo.exe",
			expectRoute: "GET /group/a/:file",
			expectCode:  http.StatusNotFound,
		},
		{
			name:        "404, route to any not found handler /group/*",
			whenURL:     "/group/b/echo.exe",
			expectRoute: "GET /group/*",
			expectCode:  http.StatusNotFound,
		},
		{
			name:        "200, route /group/a/c/df to /group/a/c/df",
			whenURL:     "/group/a/c/df",
			expectRoute: "GET /group/a/c/df",
			expectCode:  http.StatusOK,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			e := New()
			g := e.Group("/group")

			okHandler := func(c Context) error {
				return c.String(http.StatusOK, c.Request().Method+" "+c.Path())
			}
			notFoundHandler := func(c Context) error {
				return c.String(http.StatusNotFound, c.Request().Method+" "+c.Path())
			}

			g.GET("/", okHandler)
			g.GET("/a/c/df", okHandler)
			g.GET("/a/b*", okHandler)
			g.PUT("/*", okHandler)

			g.RouteNotFound("/a/c/xx", notFoundHandler)  // static
			g.RouteNotFound("/a/:file", notFoundHandler) // param
			g.RouteNotFound("/*", notFoundHandler)       // any

			req := httptest.NewRequest(http.MethodGet, tc.whenURL, nil)
			rec := httptest.NewRecorder()

			e.ServeHTTP(rec, req)

			assert.Equal(t, tc.expectCode, rec.Code)
			assert.Equal(t, tc.expectRoute, rec.Body.String())
		})
	}
}

func TestGroup_RouteNotFoundWithMiddleware(t *testing.T) {
	var testCases = []struct {
		name           string
		givenCustom404 bool
		whenURL        string
		expectBody     interface{}
		expectCode     int
	}{
		{
			name:           "ok, custom 404 handler is called with middleware",
			givenCustom404: true,
			whenURL:        "/group/test3",
			expectBody:     "GET /group/*",
			expectCode:     http.StatusNotFound,
		},
		{
			name:           "ok, default group 404 handler is called with middleware",
			givenCustom404: false,
			whenURL:        "/group/test3",
			expectBody:     "{\"message\":\"Not Found\"}\n",
			expectCode:     http.StatusNotFound,
		},
		{
			name:           "ok, (no slash) default group 404 handler is called with middleware",
			givenCustom404: false,
			whenURL:        "/group",
			expectBody:     "{\"message\":\"Not Found\"}\n",
			expectCode:     http.StatusNotFound,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {

			okHandler := func(c Context) error {
				return c.String(http.StatusOK, c.Request().Method+" "+c.Path())
			}
			notFoundHandler := func(c Context) error {
				return c.String(http.StatusNotFound, c.Request().Method+" "+c.Path())
			}

			e := New()
			e.GET("/test1", okHandler)
			e.RouteNotFound("/*", notFoundHandler)

			g := e.Group("/group")
			g.GET("/test1", okHandler)

			middlewareCalled := false
			g.Use(func(next HandlerFunc) HandlerFunc {
				return func(c Context) error {
					middlewareCalled = true
					return next(c)
				}
			})
			if tc.givenCustom404 {
				g.RouteNotFound("/*", notFoundHandler)
			}

			req := httptest.NewRequest(http.MethodGet, tc.whenURL, nil)
			rec := httptest.NewRecorder()

			e.ServeHTTP(rec, req)

			assert.True(t, middlewareCalled)
			assert.Equal(t, tc.expectCode, rec.Code)
			assert.Equal(t, tc.expectBody, rec.Body.String())
		})
	}
}
