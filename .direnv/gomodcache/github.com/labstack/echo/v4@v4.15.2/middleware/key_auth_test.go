// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: © 2015 LabStack LLC and Echo contributors

package middleware

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
)

func testKeyValidator(key string, c echo.Context) (bool, error) {
	switch key {
	case "valid-key":
		return true, nil
	case "error-key":
		return false, errors.New("some user defined error")
	default:
		return false, nil
	}
}

func TestKeyAuth(t *testing.T) {
	handlerCalled := false
	handler := func(c echo.Context) error {
		handlerCalled = true
		return c.String(http.StatusOK, "test")
	}
	middlewareChain := KeyAuth(testKeyValidator)(handler)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(echo.HeaderAuthorization, "Bearer valid-key")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := middlewareChain(c)

	assert.NoError(t, err)
	assert.True(t, handlerCalled)
}

func TestKeyAuthWithConfig(t *testing.T) {
	var testCases = []struct {
		name                string
		givenRequestFunc    func() *http.Request
		givenRequest        func(req *http.Request)
		whenConfig          func(conf *KeyAuthConfig)
		expectHandlerCalled bool
		expectError         string
	}{
		{
			name: "ok, defaults, key from header",
			givenRequest: func(req *http.Request) {
				req.Header.Set(echo.HeaderAuthorization, "Bearer valid-key")
			},
			expectHandlerCalled: true,
		},
		{
			name: "ok, custom skipper",
			givenRequest: func(req *http.Request) {
				req.Header.Set(echo.HeaderAuthorization, "Bearer error-key")
			},
			whenConfig: func(conf *KeyAuthConfig) {
				conf.Skipper = func(context echo.Context) bool {
					return true
				}
			},
			expectHandlerCalled: true,
		},
		{
			name: "nok, defaults, invalid key from header, Authorization: Bearer",
			givenRequest: func(req *http.Request) {
				req.Header.Set(echo.HeaderAuthorization, "Bearer invalid-key")
			},
			expectHandlerCalled: false,
			expectError:         "code=401, message=Unauthorized, internal=invalid key",
		},
		{
			name: "nok, defaults, invalid scheme in header",
			givenRequest: func(req *http.Request) {
				req.Header.Set(echo.HeaderAuthorization, "Bear valid-key")
			},
			expectHandlerCalled: false,
			expectError:         "code=400, message=invalid key in the request header",
		},
		{
			name:                "nok, defaults, missing header",
			givenRequest:        func(req *http.Request) {},
			expectHandlerCalled: false,
			expectError:         "code=400, message=missing key in request header",
		},
		{
			name: "ok, custom key lookup from multiple places, query and header",
			givenRequest: func(req *http.Request) {
				req.URL.RawQuery = "key=invalid-key"
				req.Header.Set("API-Key", "valid-key")
			},
			whenConfig: func(conf *KeyAuthConfig) {
				conf.KeyLookup = "query:key,header:API-Key"
			},
			expectHandlerCalled: true,
		},
		{
			name: "ok, custom key lookup, header",
			givenRequest: func(req *http.Request) {
				req.Header.Set("API-Key", "valid-key")
			},
			whenConfig: func(conf *KeyAuthConfig) {
				conf.KeyLookup = "header:API-Key"
			},
			expectHandlerCalled: true,
		},
		{
			name: "nok, custom key lookup, missing header",
			givenRequest: func(req *http.Request) {
			},
			whenConfig: func(conf *KeyAuthConfig) {
				conf.KeyLookup = "header:API-Key"
			},
			expectHandlerCalled: false,
			expectError:         "code=400, message=missing key in request header",
		},
		{
			name: "ok, custom key lookup, query",
			givenRequest: func(req *http.Request) {
				q := req.URL.Query()
				q.Add("key", "valid-key")
				req.URL.RawQuery = q.Encode()
			},
			whenConfig: func(conf *KeyAuthConfig) {
				conf.KeyLookup = "query:key"
			},
			expectHandlerCalled: true,
		},
		{
			name: "nok, custom key lookup, missing query param",
			whenConfig: func(conf *KeyAuthConfig) {
				conf.KeyLookup = "query:key"
			},
			expectHandlerCalled: false,
			expectError:         "code=400, message=missing key in the query string",
		},
		{
			name: "ok, custom key lookup, form",
			givenRequestFunc: func() *http.Request {
				req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("key=valid-key"))
				req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
				return req
			},
			whenConfig: func(conf *KeyAuthConfig) {
				conf.KeyLookup = "form:key"
			},
			expectHandlerCalled: true,
		},
		{
			name: "nok, custom key lookup, missing key in form",
			givenRequestFunc: func() *http.Request {
				req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("xxx=valid-key"))
				req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
				return req
			},
			whenConfig: func(conf *KeyAuthConfig) {
				conf.KeyLookup = "form:key"
			},
			expectHandlerCalled: false,
			expectError:         "code=400, message=missing key in the form",
		},
		{
			name: "ok, custom key lookup, cookie",
			givenRequest: func(req *http.Request) {
				req.AddCookie(&http.Cookie{
					Name:  "key",
					Value: "valid-key",
				})
				q := req.URL.Query()
				q.Add("key", "valid-key")
				req.URL.RawQuery = q.Encode()
			},
			whenConfig: func(conf *KeyAuthConfig) {
				conf.KeyLookup = "cookie:key"
			},
			expectHandlerCalled: true,
		},
		{
			name: "nok, custom key lookup, missing cookie param",
			whenConfig: func(conf *KeyAuthConfig) {
				conf.KeyLookup = "cookie:key"
			},
			expectHandlerCalled: false,
			expectError:         "code=400, message=missing key in cookies",
		},
		{
			name: "nok, custom errorHandler, error from extractor",
			whenConfig: func(conf *KeyAuthConfig) {
				conf.KeyLookup = "header:token"
				conf.ErrorHandler = func(err error, context echo.Context) error {
					httpError := echo.NewHTTPError(http.StatusTeapot, "custom")
					httpError.Internal = err
					return httpError
				}
			},
			expectHandlerCalled: false,
			expectError:         "code=418, message=custom, internal=missing key in request header",
		},
		{
			name: "nok, custom errorHandler, error from validator",
			givenRequest: func(req *http.Request) {
				req.Header.Set(echo.HeaderAuthorization, "Bearer error-key")
			},
			whenConfig: func(conf *KeyAuthConfig) {
				conf.ErrorHandler = func(err error, context echo.Context) error {
					httpError := echo.NewHTTPError(http.StatusTeapot, "custom")
					httpError.Internal = err
					return httpError
				}
			},
			expectHandlerCalled: false,
			expectError:         "code=418, message=custom, internal=some user defined error",
		},
		{
			name: "nok, defaults, error from validator",
			givenRequest: func(req *http.Request) {
				req.Header.Set(echo.HeaderAuthorization, "Bearer error-key")
			},
			whenConfig:          func(conf *KeyAuthConfig) {},
			expectHandlerCalled: false,
			expectError:         "code=401, message=Unauthorized, internal=some user defined error",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			handlerCalled := false
			handler := func(c echo.Context) error {
				handlerCalled = true
				return c.String(http.StatusOK, "test")
			}
			config := KeyAuthConfig{
				Validator: testKeyValidator,
			}
			if tc.whenConfig != nil {
				tc.whenConfig(&config)
			}
			middlewareChain := KeyAuthWithConfig(config)(handler)

			e := echo.New()
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tc.givenRequestFunc != nil {
				req = tc.givenRequestFunc()
			}
			if tc.givenRequest != nil {
				tc.givenRequest(req)
			}
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			err := middlewareChain(c)

			assert.Equal(t, tc.expectHandlerCalled, handlerCalled)
			if tc.expectError != "" {
				assert.EqualError(t, err, tc.expectError)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestKeyAuthWithConfig_panicsOnInvalidLookup(t *testing.T) {
	assert.PanicsWithError(
		t,
		"extractor source for lookup could not be split into needed parts: a",
		func() {
			handler := func(c echo.Context) error {
				return c.String(http.StatusOK, "test")
			}
			KeyAuthWithConfig(KeyAuthConfig{
				Validator: testKeyValidator,
				KeyLookup: "a",
			})(handler)
		},
	)
}

func TestKeyAuthWithConfig_panicsOnEmptyValidator(t *testing.T) {
	assert.PanicsWithValue(
		t,
		"echo: key-auth middleware requires a validator function",
		func() {
			handler := func(c echo.Context) error {
				return c.String(http.StatusOK, "test")
			}
			KeyAuthWithConfig(KeyAuthConfig{
				Validator: nil,
			})(handler)
		},
	)
}

func TestKeyAuthWithConfig_ContinueOnIgnoredError(t *testing.T) {
	var testCases = []struct {
		name                       string
		whenContinueOnIgnoredError bool
		givenKey                   string
		expectStatus               int
		expectBody                 string
	}{
		{
			name:                       "no error handler is called",
			whenContinueOnIgnoredError: true,
			givenKey:                   "valid-key",
			expectStatus:               http.StatusTeapot,
			expectBody:                 "",
		},
		{
			name:                       "ContinueOnIgnoredError is false and error handler is called for missing token",
			whenContinueOnIgnoredError: false,
			givenKey:                   "",
			// empty response with 200. This emulates previous behaviour when error handler swallowed the error
			expectStatus: http.StatusOK,
			expectBody:   "",
		},
		{
			name:                       "error handler is called for missing token",
			whenContinueOnIgnoredError: true,
			givenKey:                   "",
			expectStatus:               http.StatusTeapot,
			expectBody:                 "public-auth",
		},
		{
			name:                       "error handler is called for invalid token",
			whenContinueOnIgnoredError: true,
			givenKey:                   "x.x.x",
			expectStatus:               http.StatusUnauthorized,
			expectBody:                 "{\"message\":\"Unauthorized\"}\n",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			e := echo.New()

			e.GET("/", func(c echo.Context) error {
				testValue, _ := c.Get("test").(string)
				return c.String(http.StatusTeapot, testValue)
			})

			e.Use(KeyAuthWithConfig(KeyAuthConfig{
				Validator: testKeyValidator,
				ErrorHandler: func(err error, c echo.Context) error {
					if _, ok := err.(*ErrKeyAuthMissing); ok {
						c.Set("test", "public-auth")
						return nil
					}
					return echo.ErrUnauthorized
				},
				KeyLookup:              "header:X-API-Key",
				ContinueOnIgnoredError: tc.whenContinueOnIgnoredError,
			}))

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tc.givenKey != "" {
				req.Header.Set("X-API-Key", tc.givenKey)
			}
			res := httptest.NewRecorder()

			e.ServeHTTP(res, req)

			assert.Equal(t, tc.expectStatus, res.Code)
			assert.Equal(t, tc.expectBody, res.Body.String())
		})
	}
}
