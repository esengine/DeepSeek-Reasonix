// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: © 2015 LabStack LLC and Echo contributors

package middleware

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
)

func TestCORS(t *testing.T) {
	var testCases = []struct {
		name             string
		givenMW          echo.MiddlewareFunc
		whenMethod       string
		whenHeaders      map[string]string
		expectHeaders    map[string]string
		notExpectHeaders map[string]string
	}{
		{
			name:          "ok, wildcard origin",
			whenHeaders:   map[string]string{echo.HeaderOrigin: "localhost"},
			expectHeaders: map[string]string{echo.HeaderAccessControlAllowOrigin: "*"},
		},
		{
			name:             "ok, wildcard AllowedOrigin with no Origin header in request",
			notExpectHeaders: map[string]string{echo.HeaderAccessControlAllowOrigin: ""},
		},
		{
			name: "ok, invalid pattern is ignored",
			givenMW: CORSWithConfig(CORSConfig{
				AllowOrigins: []string{
					"\xff", // Invalid UTF-8 makes regexp.Compile to error
					"*.example.com",
				},
			}),
			whenMethod:    http.MethodOptions,
			whenHeaders:   map[string]string{echo.HeaderOrigin: "http://aaa.example.com"},
			expectHeaders: map[string]string{echo.HeaderAccessControlAllowOrigin: "http://aaa.example.com"},
		},
		{
			name: "ok, specific AllowOrigins and AllowCredentials",
			givenMW: CORSWithConfig(CORSConfig{
				AllowOrigins:     []string{"localhost"},
				AllowCredentials: true,
				MaxAge:           3600,
			}),
			whenHeaders: map[string]string{echo.HeaderOrigin: "localhost"},
			expectHeaders: map[string]string{
				echo.HeaderAccessControlAllowOrigin:      "localhost",
				echo.HeaderAccessControlAllowCredentials: "true",
			},
		},
		{
			name: "ok, preflight request with matching origin for `AllowOrigins`",
			givenMW: CORSWithConfig(CORSConfig{
				AllowOrigins:     []string{"localhost"},
				AllowCredentials: true,
				MaxAge:           3600,
			}),
			whenMethod: http.MethodOptions,
			whenHeaders: map[string]string{
				echo.HeaderOrigin:      "localhost",
				echo.HeaderContentType: echo.MIMEApplicationJSON,
			},
			expectHeaders: map[string]string{
				echo.HeaderAccessControlAllowOrigin:      "localhost",
				echo.HeaderAccessControlAllowMethods:     "GET,HEAD,PUT,PATCH,POST,DELETE",
				echo.HeaderAccessControlAllowCredentials: "true",
				echo.HeaderAccessControlMaxAge:           "3600",
			},
		},
		{
			name: "ok, preflight request when `Access-Control-Max-Age` is set",
			givenMW: CORSWithConfig(CORSConfig{
				AllowOrigins:     []string{"localhost"},
				AllowCredentials: true,
				MaxAge:           1,
			}),
			whenMethod: http.MethodOptions,
			whenHeaders: map[string]string{
				echo.HeaderOrigin:      "localhost",
				echo.HeaderContentType: echo.MIMEApplicationJSON,
			},
			expectHeaders: map[string]string{
				echo.HeaderAccessControlMaxAge: "1",
			},
		},
		{
			name: "ok, preflight request when `Access-Control-Max-Age` is set to 0 - not to cache response",
			givenMW: CORSWithConfig(CORSConfig{
				AllowOrigins:     []string{"localhost"},
				AllowCredentials: true,
				MaxAge:           -1, // forces `Access-Control-Max-Age: 0`
			}),
			whenMethod: http.MethodOptions,
			whenHeaders: map[string]string{
				echo.HeaderOrigin:      "localhost",
				echo.HeaderContentType: echo.MIMEApplicationJSON,
			},
			expectHeaders: map[string]string{
				echo.HeaderAccessControlMaxAge: "0",
			},
		},
		{
			name: "ok, CORS check are skipped",
			givenMW: CORSWithConfig(CORSConfig{
				AllowOrigins:     []string{"localhost"},
				AllowCredentials: true,
				Skipper: func(c echo.Context) bool {
					return true
				},
			}),
			whenMethod: http.MethodOptions,
			whenHeaders: map[string]string{
				echo.HeaderOrigin:      "localhost",
				echo.HeaderContentType: echo.MIMEApplicationJSON,
			},
			notExpectHeaders: map[string]string{
				echo.HeaderAccessControlAllowOrigin:      "localhost",
				echo.HeaderAccessControlAllowMethods:     "GET,HEAD,PUT,PATCH,POST,DELETE",
				echo.HeaderAccessControlAllowCredentials: "true",
				echo.HeaderAccessControlMaxAge:           "3600",
			},
		},
		{
			name: "ok, preflight request with wildcard `AllowOrigins` and `AllowCredentials` true",
			givenMW: CORSWithConfig(CORSConfig{
				AllowOrigins:     []string{"*"},
				AllowCredentials: true,
				MaxAge:           3600,
			}),
			whenMethod: http.MethodOptions,
			whenHeaders: map[string]string{
				echo.HeaderOrigin:      "localhost",
				echo.HeaderContentType: echo.MIMEApplicationJSON,
			},
			expectHeaders: map[string]string{
				echo.HeaderAccessControlAllowOrigin:      "*", // Note: browsers will ignore and complain about responses having `*`
				echo.HeaderAccessControlAllowMethods:     "GET,HEAD,PUT,PATCH,POST,DELETE",
				echo.HeaderAccessControlAllowCredentials: "true",
				echo.HeaderAccessControlMaxAge:           "3600",
			},
		},
		{
			name: "ok, preflight request with wildcard `AllowOrigins` and `AllowCredentials` false",
			givenMW: CORSWithConfig(CORSConfig{
				AllowOrigins:     []string{"*"},
				AllowCredentials: false, // important for this testcase
				MaxAge:           3600,
			}),
			whenMethod: http.MethodOptions,
			whenHeaders: map[string]string{
				echo.HeaderOrigin:      "localhost",
				echo.HeaderContentType: echo.MIMEApplicationJSON,
			},
			expectHeaders: map[string]string{
				echo.HeaderAccessControlAllowOrigin:  "*",
				echo.HeaderAccessControlAllowMethods: "GET,HEAD,PUT,PATCH,POST,DELETE",
				echo.HeaderAccessControlMaxAge:       "3600",
			},
			notExpectHeaders: map[string]string{
				echo.HeaderAccessControlAllowCredentials: "",
			},
		},
		{
			name: "ok, INSECURE preflight request with wildcard `AllowOrigins` and `AllowCredentials` true",
			givenMW: CORSWithConfig(CORSConfig{
				AllowOrigins:                             []string{"*"},
				AllowCredentials:                         true,
				UnsafeWildcardOriginWithAllowCredentials: true, // important for this testcase
				MaxAge:                                   3600,
			}),
			whenMethod: http.MethodOptions,
			whenHeaders: map[string]string{
				echo.HeaderOrigin:      "localhost",
				echo.HeaderContentType: echo.MIMEApplicationJSON,
			},
			expectHeaders: map[string]string{
				echo.HeaderAccessControlAllowOrigin:      "localhost", // This could end up as cross-origin attack
				echo.HeaderAccessControlAllowMethods:     "GET,HEAD,PUT,PATCH,POST,DELETE",
				echo.HeaderAccessControlAllowCredentials: "true",
				echo.HeaderAccessControlMaxAge:           "3600",
			},
		},
		{
			name: "ok, preflight request with Access-Control-Request-Headers",
			givenMW: CORSWithConfig(CORSConfig{
				AllowOrigins: []string{"*"},
			}),
			whenMethod: http.MethodOptions,
			whenHeaders: map[string]string{
				echo.HeaderOrigin:                      "localhost",
				echo.HeaderContentType:                 echo.MIMEApplicationJSON,
				echo.HeaderAccessControlRequestHeaders: "Special-Request-Header",
			},
			expectHeaders: map[string]string{
				echo.HeaderAccessControlAllowOrigin:  "*",
				echo.HeaderAccessControlAllowHeaders: "Special-Request-Header",
				echo.HeaderAccessControlAllowMethods: "GET,HEAD,PUT,PATCH,POST,DELETE",
			},
		},
		{
			name: "ok, preflight request with `AllowOrigins` which allow all subdomains aaa with *",
			givenMW: CORSWithConfig(CORSConfig{
				AllowOrigins: []string{"http://*.example.com"},
			}),
			whenMethod:    http.MethodOptions,
			whenHeaders:   map[string]string{echo.HeaderOrigin: "http://aaa.example.com"},
			expectHeaders: map[string]string{echo.HeaderAccessControlAllowOrigin: "http://aaa.example.com"},
		},
		{
			name: "ok, preflight request with `AllowOrigins` which allow all subdomains bbb with *",
			givenMW: CORSWithConfig(CORSConfig{
				AllowOrigins: []string{"http://*.example.com"},
			}),
			whenMethod:    http.MethodOptions,
			whenHeaders:   map[string]string{echo.HeaderOrigin: "http://bbb.example.com"},
			expectHeaders: map[string]string{echo.HeaderAccessControlAllowOrigin: "http://bbb.example.com"},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			e := echo.New()

			mw := CORS()
			if tc.givenMW != nil {
				mw = tc.givenMW
			}
			h := mw(func(c echo.Context) error {
				return nil
			})

			method := http.MethodGet
			if tc.whenMethod != "" {
				method = tc.whenMethod
			}
			req := httptest.NewRequest(method, "/", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			for k, v := range tc.whenHeaders {
				req.Header.Set(k, v)
			}

			err := h(c)

			assert.NoError(t, err)
			header := rec.Header()
			for k, v := range tc.expectHeaders {
				assert.Equal(t, v, header.Get(k), "header: `%v` should be `%v`", k, v)
			}
			for k, v := range tc.notExpectHeaders {
				if v == "" {
					assert.Len(t, header.Values(k), 0, "header: `%v` should not be set", k)
				} else {
					assert.NotEqual(t, v, header.Get(k), "header: `%v` should not be `%v`", k, v)
				}
			}
		})
	}
}

func Test_allowOriginScheme(t *testing.T) {
	tests := []struct {
		domain, pattern string
		expected        bool
	}{
		{
			domain:   "http://example.com",
			pattern:  "http://example.com",
			expected: true,
		},
		{
			domain:   "https://example.com",
			pattern:  "https://example.com",
			expected: true,
		},
		{
			domain:   "http://example.com",
			pattern:  "https://example.com",
			expected: false,
		},
		{
			domain:   "https://example.com",
			pattern:  "http://example.com",
			expected: false,
		},
	}

	e := echo.New()
	for _, tt := range tests {
		req := httptest.NewRequest(http.MethodOptions, "/", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		req.Header.Set(echo.HeaderOrigin, tt.domain)
		cors := CORSWithConfig(CORSConfig{
			AllowOrigins: []string{tt.pattern},
		})
		h := cors(echo.NotFoundHandler)
		h(c)

		if tt.expected {
			assert.Equal(t, tt.domain, rec.Header().Get(echo.HeaderAccessControlAllowOrigin))
		} else {
			assert.NotContains(t, rec.Header(), echo.HeaderAccessControlAllowOrigin)
		}
	}
}

func Test_allowOriginSubdomain(t *testing.T) {
	tests := []struct {
		domain, pattern string
		expected        bool
	}{
		{
			domain:   "http://aaa.example.com",
			pattern:  "http://*.example.com",
			expected: true,
		},
		{
			domain:   "http://bbb.aaa.example.com",
			pattern:  "http://*.example.com",
			expected: true,
		},
		{
			domain:   "http://bbb.aaa.example.com",
			pattern:  "http://*.aaa.example.com",
			expected: true,
		},
		{
			domain:   "http://aaa.example.com:8080",
			pattern:  "http://*.example.com:8080",
			expected: true,
		},

		{
			domain:   "http://fuga.hoge.com",
			pattern:  "http://*.example.com",
			expected: false,
		},
		{
			domain:   "http://ccc.bbb.example.com",
			pattern:  "http://*.aaa.example.com",
			expected: false,
		},
		{
			domain: `http://1234567890.1234567890.1234567890.1234567890.1234567890.1234567890.1234567890.1234567890.1234567890.1234567890\
		  .1234567890.1234567890.1234567890.1234567890.1234567890.1234567890.1234567890.1234567890.1234567890.1234567890\
		  .1234567890.1234567890.1234567890.1234567890.1234567890.1234567890.1234567890.1234567890.1234567890.1234567890\
		  .1234567890.1234567890.1234567890.1234567890.1234567890.1234567890.1234567890.1234567890.1234567890.1234567890.example.com`,
			pattern:  "http://*.example.com",
			expected: false,
		},
		{
			domain:   `http://1234567890.1234567890.1234567890.1234567890.1234567890.1234567890.1234567890.1234567890.1234567890.1234567890.1234567890.1234567890.1234567890.1234567890.1234567890.1234567890.1234567890.1234567890.1234567890.1234567890.1234567890.1234567890.1234567890.1234567890.1234567890.1234567890.1234567890.1234567890.1234567890.1234567890.1234567890.1234567890.1234567890.1234567890.1234567890.1234567890.1234567890.1234567890.1234567890.1234567890.example.com`,
			pattern:  "http://*.example.com",
			expected: false,
		},
		{
			domain:   "http://ccc.bbb.example.com",
			pattern:  "http://example.com",
			expected: false,
		},
		{
			domain:   "https://prod-preview--aaa.bbb.com",
			pattern:  "https://*--aaa.bbb.com",
			expected: true,
		},
		{
			domain:   "http://ccc.bbb.example.com",
			pattern:  "http://*.example.com",
			expected: true,
		},
		{
			domain:   "http://ccc.bbb.example.com",
			pattern:  "http://foo.[a-z]*.example.com",
			expected: false,
		},
	}

	e := echo.New()
	for _, tt := range tests {
		req := httptest.NewRequest(http.MethodOptions, "/", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		req.Header.Set(echo.HeaderOrigin, tt.domain)
		cors := CORSWithConfig(CORSConfig{
			AllowOrigins: []string{tt.pattern},
		})
		h := cors(echo.NotFoundHandler)
		h(c)

		if tt.expected {
			assert.Equal(t, tt.domain, rec.Header().Get(echo.HeaderAccessControlAllowOrigin))
		} else {
			assert.NotContains(t, rec.Header(), echo.HeaderAccessControlAllowOrigin)
		}
	}
}

func TestCORSWithConfig_AllowMethods(t *testing.T) {
	var testCases = []struct {
		name            string
		allowOrigins    []string
		allowContextKey string

		whenOrigin       string
		whenAllowMethods []string

		expectAllow                     string
		expectAccessControlAllowMethods string
	}{
		{
			name:             "custom AllowMethods, preflight, no origin, sets only allow header from context key",
			allowContextKey:  "OPTIONS, GET",
			whenAllowMethods: []string{http.MethodGet, http.MethodHead},
			whenOrigin:       "",
			expectAllow:      "OPTIONS, GET",
		},
		{
			name:             "default AllowMethods, preflight, no origin, no allow header in context key and in response",
			allowContextKey:  "",
			whenAllowMethods: nil,
			whenOrigin:       "",
			expectAllow:      "",
		},
		{
			name:                            "custom AllowMethods, preflight, existing origin, sets both headers different values",
			allowContextKey:                 "OPTIONS, GET",
			whenAllowMethods:                []string{http.MethodGet, http.MethodHead},
			whenOrigin:                      "http://google.com",
			expectAllow:                     "OPTIONS, GET",
			expectAccessControlAllowMethods: "GET,HEAD",
		},
		{
			name:                            "default AllowMethods, preflight, existing origin, sets both headers",
			allowContextKey:                 "OPTIONS, GET",
			whenAllowMethods:                nil,
			whenOrigin:                      "http://google.com",
			expectAllow:                     "OPTIONS, GET",
			expectAccessControlAllowMethods: "OPTIONS, GET",
		},
		{
			name:                            "default AllowMethods, preflight, existing origin, no allows, sets only CORS allow methods",
			allowContextKey:                 "",
			whenAllowMethods:                nil,
			whenOrigin:                      "http://google.com",
			expectAllow:                     "",
			expectAccessControlAllowMethods: "GET,HEAD,PUT,PATCH,POST,DELETE",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			e := echo.New()
			e.GET("/test", func(c echo.Context) error {
				return c.String(http.StatusOK, "OK")
			})

			cors := CORSWithConfig(CORSConfig{
				AllowOrigins: tc.allowOrigins,
				AllowMethods: tc.whenAllowMethods,
			})

			req := httptest.NewRequest(http.MethodOptions, "/test", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			req.Header.Set(echo.HeaderOrigin, tc.whenOrigin)
			if tc.allowContextKey != "" {
				c.Set(echo.ContextKeyHeaderAllow, tc.allowContextKey)
			}

			h := cors(echo.NotFoundHandler)
			h(c)

			assert.Equal(t, tc.expectAllow, rec.Header().Get(echo.HeaderAllow))
			assert.Equal(t, tc.expectAccessControlAllowMethods, rec.Header().Get(echo.HeaderAccessControlAllowMethods))
		})
	}
}

func TestCorsHeaders(t *testing.T) {
	tests := []struct {
		name              string
		originDomain      string
		method            string
		allowedOrigin     string
		expected          bool
		expectStatus      int
		expectAllowHeader string
	}{
		{
			name:          "non-preflight request, allow any origin, missing origin header = no CORS logic done",
			originDomain:  "",
			allowedOrigin: "*",
			method:        http.MethodGet,
			expected:      false,
			expectStatus:  http.StatusOK,
		},
		{
			name:          "non-preflight request, allow any origin, specific origin domain",
			originDomain:  "http://example.com",
			allowedOrigin: "*",
			method:        http.MethodGet,
			expected:      true,
			expectStatus:  http.StatusOK,
		},
		{
			name:          "non-preflight request, allow specific origin, missing origin header = no CORS logic done",
			originDomain:  "", // Request does not have Origin header
			allowedOrigin: "http://example.com",
			method:        http.MethodGet,
			expected:      false,
			expectStatus:  http.StatusOK,
		},
		{
			name:          "non-preflight request, allow specific origin, different origin header = CORS logic failure",
			originDomain:  "http://bar.com",
			allowedOrigin: "http://example.com",
			method:        http.MethodGet,
			expected:      false,
			expectStatus:  http.StatusOK,
		},
		{
			name:          "non-preflight request, allow specific origin, matching origin header = CORS logic done",
			originDomain:  "http://example.com",
			allowedOrigin: "http://example.com",
			method:        http.MethodGet,
			expected:      true,
			expectStatus:  http.StatusOK,
		},
		{
			name:              "preflight, allow any origin, missing origin header = no CORS logic done",
			originDomain:      "", // Request does not have Origin header
			allowedOrigin:     "*",
			method:            http.MethodOptions,
			expected:          false,
			expectStatus:      http.StatusNoContent,
			expectAllowHeader: "OPTIONS, GET, POST",
		},
		{
			name:              "preflight, allow any origin, existing origin header = CORS logic done",
			originDomain:      "http://example.com",
			allowedOrigin:     "*",
			method:            http.MethodOptions,
			expected:          true,
			expectStatus:      http.StatusNoContent,
			expectAllowHeader: "OPTIONS, GET, POST",
		},
		{
			name:              "preflight, allow any origin, missing origin header = no CORS logic done",
			originDomain:      "", // Request does not have Origin header
			allowedOrigin:     "http://example.com",
			method:            http.MethodOptions,
			expected:          false,
			expectStatus:      http.StatusNoContent,
			expectAllowHeader: "OPTIONS, GET, POST",
		},
		{
			name:              "preflight, allow specific origin, different origin header = no CORS logic done",
			originDomain:      "http://bar.com",
			allowedOrigin:     "http://example.com",
			method:            http.MethodOptions,
			expected:          false,
			expectStatus:      http.StatusNoContent,
			expectAllowHeader: "OPTIONS, GET, POST",
		},
		{
			name:              "preflight, allow specific origin, matching origin header = CORS logic done",
			originDomain:      "http://example.com",
			allowedOrigin:     "http://example.com",
			method:            http.MethodOptions,
			expected:          true,
			expectStatus:      http.StatusNoContent,
			expectAllowHeader: "OPTIONS, GET, POST",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := echo.New()

			e.Use(CORSWithConfig(CORSConfig{
				AllowOrigins: []string{tc.allowedOrigin},
				//AllowCredentials: true,
				//MaxAge:           3600,
			}))

			e.GET("/", func(c echo.Context) error {
				return c.String(http.StatusOK, "OK")
			})
			e.POST("/", func(c echo.Context) error {
				return c.String(http.StatusCreated, "OK")
			})

			req := httptest.NewRequest(tc.method, "/", nil)
			rec := httptest.NewRecorder()

			if tc.originDomain != "" {
				req.Header.Set(echo.HeaderOrigin, tc.originDomain)
			}

			// we run through whole Echo handler chain to see how CORS works with Router OPTIONS handler
			e.ServeHTTP(rec, req)

			assert.Equal(t, echo.HeaderOrigin, rec.Header().Get(echo.HeaderVary))
			assert.Equal(t, tc.expectAllowHeader, rec.Header().Get(echo.HeaderAllow))
			assert.Equal(t, tc.expectStatus, rec.Code)

			expectedAllowOrigin := ""
			if tc.allowedOrigin == "*" {
				expectedAllowOrigin = "*"
			} else {
				expectedAllowOrigin = tc.originDomain
			}
			switch {
			case tc.expected && tc.method == http.MethodOptions:
				assert.Contains(t, rec.Header(), echo.HeaderAccessControlAllowMethods)
				assert.Equal(t, expectedAllowOrigin, rec.Header().Get(echo.HeaderAccessControlAllowOrigin))

				assert.Equal(t, 3, len(rec.Header()[echo.HeaderVary]))

			case tc.expected && tc.method == http.MethodGet:
				assert.Equal(t, expectedAllowOrigin, rec.Header().Get(echo.HeaderAccessControlAllowOrigin))
				assert.Equal(t, 1, len(rec.Header()[echo.HeaderVary])) // Vary: Origin
			default:
				assert.NotContains(t, rec.Header(), echo.HeaderAccessControlAllowOrigin)
				assert.Equal(t, 1, len(rec.Header()[echo.HeaderVary])) // Vary: Origin
			}
		})

	}
}

func Test_allowOriginFunc(t *testing.T) {
	returnTrue := func(origin string) (bool, error) {
		return true, nil
	}
	returnFalse := func(origin string) (bool, error) {
		return false, nil
	}
	returnError := func(origin string) (bool, error) {
		return true, errors.New("this is a test error")
	}

	allowOriginFuncs := []func(origin string) (bool, error){
		returnTrue,
		returnFalse,
		returnError,
	}

	const origin = "http://example.com"

	e := echo.New()
	for _, allowOriginFunc := range allowOriginFuncs {
		req := httptest.NewRequest(http.MethodOptions, "/", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		req.Header.Set(echo.HeaderOrigin, origin)
		cors := CORSWithConfig(CORSConfig{
			AllowOriginFunc: allowOriginFunc,
		})
		h := cors(echo.NotFoundHandler)
		err := h(c)

		expected, expectedErr := allowOriginFunc(origin)
		if expectedErr != nil {
			assert.Equal(t, expectedErr, err)
			assert.Equal(t, "", rec.Header().Get(echo.HeaderAccessControlAllowOrigin))
			continue
		}

		if expected {
			assert.Equal(t, origin, rec.Header().Get(echo.HeaderAccessControlAllowOrigin))
		} else {
			assert.Equal(t, "", rec.Header().Get(echo.HeaderAccessControlAllowOrigin))
		}
	}
}
