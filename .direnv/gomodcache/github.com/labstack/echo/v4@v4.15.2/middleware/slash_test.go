// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: © 2015 LabStack LLC and Echo contributors

package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
)

func TestAddTrailingSlashWithConfig(t *testing.T) {
	var testCases = []struct {
		whenURL        string
		whenMethod     string
		expectPath     string
		expectLocation []string
		expectStatus   int
	}{
		{
			whenURL:        "/add-slash",
			whenMethod:     http.MethodGet,
			expectPath:     "/add-slash",
			expectLocation: []string{`/add-slash/`},
		},
		{
			whenURL:        "/add-slash?key=value",
			whenMethod:     http.MethodGet,
			expectPath:     "/add-slash",
			expectLocation: []string{`/add-slash/?key=value`},
		},
		{
			whenURL:        "/",
			whenMethod:     http.MethodConnect,
			expectPath:     "/",
			expectLocation: nil,
			expectStatus:   http.StatusOK,
		},
		// cases for open redirect vulnerability
		{
			whenURL:        "http://localhost:1323/%5Cexample.com",
			expectPath:     `/\example.com`,
			expectLocation: []string{`/example.com/`},
		},
		{
			whenURL:        `http://localhost:1323/\example.com`,
			expectPath:     `/\example.com`,
			expectLocation: []string{`/example.com/`},
		},
		{
			whenURL:        `http://localhost:1323/\\%5C////%5C\\\example.com`,
			expectPath:     `/\\\////\\\\example.com`,
			expectLocation: []string{`/example.com/`},
		},
		{
			whenURL:        "http://localhost:1323//example.com",
			expectPath:     `//example.com`,
			expectLocation: []string{`/example.com/`},
		},
		{
			whenURL:        "http://localhost:1323/%5C%5C",
			expectPath:     `/\\`,
			expectLocation: []string{`/`},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.whenURL, func(t *testing.T) {
			e := echo.New()

			mw := AddTrailingSlashWithConfig(TrailingSlashConfig{
				RedirectCode: http.StatusMovedPermanently,
			})
			h := mw(func(c echo.Context) error {
				return nil
			})

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(tc.whenMethod, tc.whenURL, nil)
			c := e.NewContext(req, rec)

			err := h(c)
			assert.NoError(t, err)

			assert.Equal(t, tc.expectPath, req.URL.Path)
			assert.Equal(t, tc.expectLocation, rec.Header()[echo.HeaderLocation])
			if tc.expectStatus == 0 {
				assert.Equal(t, http.StatusMovedPermanently, rec.Code)
			} else {
				assert.Equal(t, tc.expectStatus, rec.Code)
			}
		})
	}
}

func TestAddTrailingSlash(t *testing.T) {
	var testCases = []struct {
		whenURL        string
		whenMethod     string
		expectPath     string
		expectLocation []string
	}{
		{
			whenURL:    "/add-slash",
			whenMethod: http.MethodGet,
			expectPath: "/add-slash/",
		},
		{
			whenURL:    "/add-slash?key=value",
			whenMethod: http.MethodGet,
			expectPath: "/add-slash/",
		},
		{
			whenURL:        "/",
			whenMethod:     http.MethodConnect,
			expectPath:     "/",
			expectLocation: nil,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.whenURL, func(t *testing.T) {
			e := echo.New()

			h := AddTrailingSlash()(func(c echo.Context) error {
				return nil
			})

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(tc.whenMethod, tc.whenURL, nil)
			c := e.NewContext(req, rec)

			err := h(c)
			assert.NoError(t, err)

			assert.Equal(t, tc.expectPath, req.URL.Path)
			assert.Equal(t, []string(nil), rec.Header()[echo.HeaderLocation])
			assert.Equal(t, http.StatusOK, rec.Code)
		})
	}
}

func TestRemoveTrailingSlashWithConfig(t *testing.T) {
	var testCases = []struct {
		whenURL        string
		whenMethod     string
		expectPath     string
		expectLocation []string
		expectStatus   int
	}{
		{
			whenURL:        "/remove-slash/",
			whenMethod:     http.MethodGet,
			expectPath:     "/remove-slash/",
			expectLocation: []string{`/remove-slash`},
		},
		{
			whenURL:        "/remove-slash/?key=value",
			whenMethod:     http.MethodGet,
			expectPath:     "/remove-slash/",
			expectLocation: []string{`/remove-slash?key=value`},
		},
		{
			whenURL:        "/",
			whenMethod:     http.MethodConnect,
			expectPath:     "/",
			expectLocation: nil,
			expectStatus:   http.StatusOK,
		},
		{
			whenURL:        "http://localhost",
			whenMethod:     http.MethodGet,
			expectPath:     "",
			expectLocation: nil,
			expectStatus:   http.StatusOK,
		},
		// cases for open redirect vulnerability
		{
			whenURL:        "http://localhost:1323/%5Cexample.com/",
			expectPath:     `/\example.com/`,
			expectLocation: []string{`/example.com`},
		},
		{
			whenURL:        `http://localhost:1323/\example.com/`,
			expectPath:     `/\example.com/`,
			expectLocation: []string{`/example.com`},
		},
		{
			whenURL:        `http://localhost:1323/\\%5C////%5C\\\example.com/`,
			expectPath:     `/\\\////\\\\example.com/`,
			expectLocation: []string{`/example.com`},
		},
		{
			whenURL:        "http://localhost:1323//example.com/",
			expectPath:     `//example.com/`,
			expectLocation: []string{`/example.com`},
		},
		{
			whenURL:        "http://localhost:1323/%5C%5C/",
			expectPath:     `/\\/`,
			expectLocation: []string{`/`},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.whenURL, func(t *testing.T) {
			e := echo.New()

			mw := RemoveTrailingSlashWithConfig(TrailingSlashConfig{
				RedirectCode: http.StatusMovedPermanently,
			})
			h := mw(func(c echo.Context) error {
				return nil
			})

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(tc.whenMethod, tc.whenURL, nil)
			c := e.NewContext(req, rec)

			err := h(c)
			assert.NoError(t, err)

			assert.Equal(t, tc.expectPath, req.URL.Path)
			assert.Equal(t, tc.expectLocation, rec.Header()[echo.HeaderLocation])
			if tc.expectStatus == 0 {
				assert.Equal(t, http.StatusMovedPermanently, rec.Code)
			} else {
				assert.Equal(t, tc.expectStatus, rec.Code)
			}
		})
	}
}

func TestRemoveTrailingSlash(t *testing.T) {
	var testCases = []struct {
		whenURL    string
		whenMethod string
		expectPath string
	}{
		{
			whenURL:    "/remove-slash/",
			whenMethod: http.MethodGet,
			expectPath: "/remove-slash",
		},
		{
			whenURL:    "/remove-slash/?key=value",
			whenMethod: http.MethodGet,
			expectPath: "/remove-slash",
		},
		{
			whenURL:    "/",
			whenMethod: http.MethodConnect,
			expectPath: "/",
		},
		{
			whenURL:    "http://localhost",
			whenMethod: http.MethodGet,
			expectPath: "",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.whenURL, func(t *testing.T) {
			e := echo.New()

			h := RemoveTrailingSlash()(func(c echo.Context) error {
				return nil
			})

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(tc.whenMethod, tc.whenURL, nil)
			c := e.NewContext(req, rec)

			err := h(c)
			assert.NoError(t, err)

			assert.Equal(t, tc.expectPath, req.URL.Path)
			assert.Equal(t, []string(nil), rec.Header()[echo.HeaderLocation])
			assert.Equal(t, http.StatusOK, rec.Code)
		})
	}
}
