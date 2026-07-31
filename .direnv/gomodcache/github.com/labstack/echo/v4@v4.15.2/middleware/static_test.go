// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: © 2015 LabStack LLC and Echo contributors

package middleware

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
)

func TestStatic(t *testing.T) {
	var testCases = []struct {
		name                 string
		givenConfig          *StaticConfig
		givenAttachedToGroup string
		whenURL              string
		expectContains       string
		expectLength         string
		expectCode           int
	}{
		{
			name:           "ok, serve index with Echo message",
			whenURL:        "/",
			expectCode:     http.StatusOK,
			expectContains: "<title>Echo</title>",
		},
		{
			name:         "ok, serve file from subdirectory",
			whenURL:      "/images/walle.png",
			expectCode:   http.StatusOK,
			expectLength: "219885",
		},
		{
			name: "ok, when html5 mode serve index for any static file that does not exist",
			givenConfig: &StaticConfig{
				Root:  "../_fixture",
				HTML5: true,
			},
			whenURL:        "/random",
			expectCode:     http.StatusOK,
			expectContains: "<title>Echo</title>",
		},
		{
			name: "ok, serve index as directory index listing files directory",
			givenConfig: &StaticConfig{
				Root:   "../_fixture/certs",
				Browse: true,
			},
			whenURL:        "/",
			expectCode:     http.StatusOK,
			expectContains: "cert.pem",
		},
		{
			name: "ok, serve directory index with IgnoreBase and browse",
			givenConfig: &StaticConfig{
				Root:       "../_fixture/_fixture/", // <-- last `_fixture/` is overlapping with group path and needs to be ignored
				IgnoreBase: true,
				Browse:     true,
			},
			givenAttachedToGroup: "/_fixture",
			whenURL:              "/_fixture/",
			expectCode:           http.StatusOK,
			expectContains:       `<a class="file" href="README.md">README.md</a>`,
		},
		{
			name: "ok, serve file with IgnoreBase",
			givenConfig: &StaticConfig{
				Root:       "../_fixture/_fixture/", // <-- last `_fixture/` is overlapping with group path and needs to be ignored
				IgnoreBase: true,
				Browse:     true,
			},
			givenAttachedToGroup: "/_fixture",
			whenURL:              "/_fixture/README.md",
			expectCode:           http.StatusOK,
			expectContains:       "This directory is used for the static middleware test",
		},
		{
			name:           "nok, file not found",
			whenURL:        "/none",
			expectCode:     http.StatusNotFound,
			expectContains: "{\"message\":\"Not Found\"}\n",
		},
		{
			name:           "nok, do not allow directory traversal (backslash - windows separator)",
			whenURL:        `/..\\middleware/basic_auth.go`,
			expectCode:     http.StatusNotFound,
			expectContains: "{\"message\":\"Not Found\"}\n",
		},
		{
			name:           "nok,do not allow directory traversal (slash - unix separator)",
			whenURL:        `/../middleware/basic_auth.go`,
			expectCode:     http.StatusNotFound,
			expectContains: "{\"message\":\"Not Found\"}\n",
		},
		{
			name:           "nok, URL encoded path traversal (single encoding)",
			whenURL:        "/%2e%2e%2fmiddleware/basic_auth.go",
			expectCode:     http.StatusNotFound,
			expectContains: "{\"message\":\"Not Found\"}\n",
		},
		{
			name:           "nok, URL encoded path traversal (double encoding)",
			whenURL:        "/%252e%252e%252fmiddleware/basic_auth.go",
			expectCode:     http.StatusNotFound,
			expectContains: "{\"message\":\"Not Found\"}\n",
		},
		{
			name:           "nok, URL encoded path traversal (mixed encoding)",
			whenURL:        "/%2e%2e/middleware/basic_auth.go",
			expectCode:     http.StatusNotFound,
			expectContains: "{\"message\":\"Not Found\"}\n",
		},
		{
			name:           "nok, backslash URL encoded",
			whenURL:        "/..%5c..%5cmiddleware/basic_auth.go",
			expectCode:     http.StatusNotFound,
			expectContains: "{\"message\":\"Not Found\"}\n",
		},
		//{ // Disabled: returns 404 under Windows
		//	name:           "nok, null byte injection",
		//	whenURL:        "/index.html%00.jpg",
		//	expectCode:     http.StatusInternalServerError,
		//	expectContains: "{\"message\":\"Internal Server Error\"}\n",
		//},
		{
			name:           "nok, mixed backslash and forward slash traversal",
			whenURL:        "/..\\../middleware/basic_auth.go",
			expectCode:     http.StatusNotFound,
			expectContains: "{\"message\":\"Not Found\"}\n",
		},
		{
			name:           "nok, trailing dots (Windows edge case)",
			whenURL:        "/../middleware/basic_auth.go...",
			expectCode:     http.StatusNotFound,
			expectContains: "{\"message\":\"Not Found\"}\n",
		},
		{
			name:           "ok, do not serve file, when a handler took care of the request",
			whenURL:        "/regular-handler",
			expectCode:     http.StatusOK,
			expectContains: "ok",
		},
		{
			name: "nok, when html5 fail if the index file does not exist",
			givenConfig: &StaticConfig{
				Root:  "../_fixture",
				HTML5: true,
				Index: "missing.html",
			},
			whenURL:    "/random",
			expectCode: http.StatusInternalServerError,
		},
		{
			name: "ok, serve from http.FileSystem",
			givenConfig: &StaticConfig{
				Root:       "_fixture",
				Filesystem: http.Dir(".."),
			},
			whenURL:        "/",
			expectCode:     http.StatusOK,
			expectContains: "<title>Echo</title>",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			e := echo.New()

			config := StaticConfig{Root: "../_fixture"}
			if tc.givenConfig != nil {
				config = *tc.givenConfig
			}
			middlewareFunc := StaticWithConfig(config)
			if tc.givenAttachedToGroup != "" {
				// middleware is attached to group
				subGroup := e.Group(tc.givenAttachedToGroup, middlewareFunc)
				// group without http handlers (routes) does not do anything.
				// Request is matched against http handlers (routes) that have group middleware attached to them
				subGroup.GET("", echo.NotFoundHandler)
				subGroup.GET("/*", echo.NotFoundHandler)
			} else {
				// middleware is on root level
				e.Use(middlewareFunc)
				e.GET("/regular-handler", func(c echo.Context) error {
					return c.String(http.StatusOK, "ok")
				})
			}

			req := httptest.NewRequest(http.MethodGet, tc.whenURL, nil)
			rec := httptest.NewRecorder()

			e.ServeHTTP(rec, req)

			assert.Equal(t, tc.expectCode, rec.Code)
			if tc.expectContains != "" {
				responseBody := rec.Body.String()
				assert.Contains(t, responseBody, tc.expectContains)
			}
			if tc.expectLength != "" {
				assert.Equal(t, rec.Header().Get(echo.HeaderContentLength), tc.expectLength)
			}
		})
	}
}

func TestStatic_GroupWithStatic(t *testing.T) {
	var testCases = []struct {
		name                 string
		givenGroup           string
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
			givenRoot:            "../_fixture/images",
			whenURL:              "/group/images/walle.png",
			expectStatus:         http.StatusOK,
			expectBodyStartsWith: string([]byte{0x89, 0x50, 0x4e, 0x47}),
		},
		{
			name:                 "No file",
			givenPrefix:          "/images",
			givenRoot:            "../_fixture/scripts",
			whenURL:              "/group/images/bolt.png",
			expectStatus:         http.StatusNotFound,
			expectBodyStartsWith: "{\"message\":\"Not Found\"}\n",
		},
		{
			name:                 "Directory not found (no trailing slash)",
			givenPrefix:          "/images",
			givenRoot:            "../_fixture/images",
			whenURL:              "/group/images/",
			expectStatus:         http.StatusNotFound,
			expectBodyStartsWith: "{\"message\":\"Not Found\"}\n",
		},
		{
			name:                 "Directory redirect",
			givenPrefix:          "/",
			givenRoot:            "../_fixture",
			whenURL:              "/group/folder",
			expectStatus:         http.StatusMovedPermanently,
			expectHeaderLocation: "/group/folder/",
			expectBodyStartsWith: "",
		},
		{
			name:                 "Directory redirect",
			givenPrefix:          "/",
			givenRoot:            "../_fixture",
			whenURL:              "/group/folder%2f..",
			expectStatus:         http.StatusMovedPermanently,
			expectHeaderLocation: "/group/folder/../",
			expectBodyStartsWith: "",
		},
		{
			name:                 "Prefixed directory 404 (request URL without slash)",
			givenGroup:           "_fixture",
			givenPrefix:          "/folder/", // trailing slash will intentionally not match "/folder"
			givenRoot:            "../_fixture",
			whenURL:              "/_fixture/folder", // no trailing slash
			expectStatus:         http.StatusNotFound,
			expectBodyStartsWith: "{\"message\":\"Not Found\"}\n",
		},
		{
			name:                 "Prefixed directory redirect (without slash redirect to slash)",
			givenGroup:           "_fixture",
			givenPrefix:          "/folder", // no trailing slash shall match /folder and /folder/*
			givenRoot:            "../_fixture",
			whenURL:              "/_fixture/folder", // no trailing slash
			expectStatus:         http.StatusMovedPermanently,
			expectHeaderLocation: "/_fixture/folder/",
			expectBodyStartsWith: "",
		},
		{
			name:                 "Directory with index.html",
			givenPrefix:          "/",
			givenRoot:            "../_fixture",
			whenURL:              "/group/",
			expectStatus:         http.StatusOK,
			expectBodyStartsWith: "<!doctype html>",
		},
		{
			name:                 "Prefixed directory with index.html (prefix ending with slash)",
			givenPrefix:          "/assets/",
			givenRoot:            "../_fixture",
			whenURL:              "/group/assets/",
			expectStatus:         http.StatusOK,
			expectBodyStartsWith: "<!doctype html>",
		},
		{
			name:                 "Prefixed directory with index.html (prefix ending without slash)",
			givenPrefix:          "/assets",
			givenRoot:            "../_fixture",
			whenURL:              "/group/assets/",
			expectStatus:         http.StatusOK,
			expectBodyStartsWith: "<!doctype html>",
		},
		{
			name:                 "Sub-directory with index.html",
			givenPrefix:          "/",
			givenRoot:            "../_fixture",
			whenURL:              "/group/folder/",
			expectStatus:         http.StatusOK,
			expectBodyStartsWith: "<!doctype html>",
		},
		{
			name:                 "do not allow directory traversal (backslash - windows separator)",
			givenPrefix:          "/",
			givenRoot:            "../_fixture/",
			whenURL:              `/group/..\\middleware/basic_auth.go`,
			expectStatus:         http.StatusNotFound,
			expectBodyStartsWith: "{\"message\":\"Not Found\"}\n",
		},
		{
			name:                 "do not allow directory traversal (slash - unix separator)",
			givenPrefix:          "/",
			givenRoot:            "../_fixture/",
			whenURL:              `/group/../middleware/basic_auth.go`,
			expectStatus:         http.StatusNotFound,
			expectBodyStartsWith: "{\"message\":\"Not Found\"}\n",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			e := echo.New()
			group := "/group"
			if tc.givenGroup != "" {
				group = tc.givenGroup
			}
			g := e.Group(group)
			g.Static(tc.givenPrefix, tc.givenRoot)

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
				assert.Equal(t, tc.expectHeaderLocation, rec.Header().Get(echo.HeaderLocation))
			} else {
				_, ok := rec.Result().Header[echo.HeaderLocation]
				assert.False(t, ok)
			}
		})
	}
}

func TestStatic_CustomFS(t *testing.T) {
	var testCases = []struct {
		name           string
		filesystem     fs.FS
		root           string
		whenURL        string
		expectContains string
		expectCode     int
	}{
		{
			name:           "ok, serve index with Echo message",
			whenURL:        "/",
			filesystem:     os.DirFS("../_fixture"),
			expectCode:     http.StatusOK,
			expectContains: "<title>Echo</title>",
		},

		{
			name:           "ok, serve index with Echo message",
			whenURL:        "/_fixture/",
			filesystem:     os.DirFS(".."),
			expectCode:     http.StatusOK,
			expectContains: "<title>Echo</title>",
		},
		{
			name:    "ok, serve file from map fs",
			whenURL: "/file.txt",
			filesystem: fstest.MapFS{
				"file.txt": &fstest.MapFile{Data: []byte("file.txt is ok")},
			},
			expectCode:     http.StatusOK,
			expectContains: "file.txt is ok",
		},
		{
			name:       "nok, missing file in map fs",
			whenURL:    "/file.txt",
			expectCode: http.StatusNotFound,
			filesystem: fstest.MapFS{
				"file2.txt": &fstest.MapFile{Data: []byte("file2.txt is ok")},
			},
		},
		{
			name:    "nok, file is not a subpath of root",
			whenURL: `/../../secret.txt`,
			root:    "/nested/folder",
			filesystem: fstest.MapFS{
				"secret.txt": &fstest.MapFile{Data: []byte("this is a secret")},
			},
			expectCode: http.StatusNotFound,
		},
		{
			name:       "nok, backslash is forbidden",
			whenURL:    `/..\..\secret.txt`,
			expectCode: http.StatusNotFound,
			root:       "/nested/folder",
			filesystem: fstest.MapFS{
				"secret.txt": &fstest.MapFile{Data: []byte("this is a secret")},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			e := echo.New()

			config := StaticConfig{
				Root:       ".",
				Filesystem: http.FS(tc.filesystem),
			}

			if tc.root != "" {
				config.Root = tc.root
			}

			middlewareFunc := StaticWithConfig(config)
			e.Use(middlewareFunc)

			req := httptest.NewRequest(http.MethodGet, tc.whenURL, nil)
			rec := httptest.NewRecorder()

			e.ServeHTTP(rec, req)

			assert.Equal(t, tc.expectCode, rec.Code)
			if tc.expectContains != "" {
				responseBody := rec.Body.String()
				assert.Contains(t, responseBody, tc.expectContains)
			}
		})
	}
}
