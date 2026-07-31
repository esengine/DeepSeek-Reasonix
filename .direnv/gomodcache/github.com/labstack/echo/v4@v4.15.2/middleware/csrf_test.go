// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: © 2015 LabStack LLC and Echo contributors

package middleware

import (
	"cmp"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
)

func TestCSRF_tokenExtractors(t *testing.T) {
	var testCases = []struct {
		name                    string
		whenTokenLookup         string
		whenCookieName          string
		givenCSRFCookie         string
		givenMethod             string
		givenQueryTokens        map[string][]string
		givenFormTokens         map[string][]string
		givenHeaderTokens       map[string][]string
		expectError             string
		expectToMiddlewareError string
	}{
		{
			name:            "ok, multiple token lookups sources, succeeds on last one",
			whenTokenLookup: "header:X-CSRF-Token,form:csrf",
			givenCSRFCookie: "token",
			givenMethod:     http.MethodPost,
			givenHeaderTokens: map[string][]string{
				echo.HeaderXCSRFToken: {"invalid_token"},
			},
			givenFormTokens: map[string][]string{
				"csrf": {"token"},
			},
		},
		{
			name:            "ok, token from POST form",
			whenTokenLookup: "form:csrf",
			givenCSRFCookie: "token",
			givenMethod:     http.MethodPost,
			givenFormTokens: map[string][]string{
				"csrf": {"token"},
			},
		},
		{
			name:            "ok, token from POST form, second token passes",
			whenTokenLookup: "form:csrf",
			givenCSRFCookie: "token",
			givenMethod:     http.MethodPost,
			givenFormTokens: map[string][]string{
				"csrf": {"invalid", "token"},
			},
		},
		{
			name:            "nok, invalid token from POST form",
			whenTokenLookup: "form:csrf",
			givenCSRFCookie: "token",
			givenMethod:     http.MethodPost,
			givenFormTokens: map[string][]string{
				"csrf": {"invalid_token"},
			},
			expectError: "code=403, message=invalid csrf token",
		},
		{
			name:            "nok, missing token from POST form",
			whenTokenLookup: "form:csrf",
			givenCSRFCookie: "token",
			givenMethod:     http.MethodPost,
			givenFormTokens: map[string][]string{},
			expectError:     "code=400, message=missing csrf token in the form parameter",
		},
		{
			name:            "ok, token from POST header",
			whenTokenLookup: "", // will use defaults
			givenCSRFCookie: "token",
			givenMethod:     http.MethodPost,
			givenHeaderTokens: map[string][]string{
				echo.HeaderXCSRFToken: {"token"},
			},
		},
		{
			name:            "nok, token from POST header, tokens limited to 1, second token would pass",
			whenTokenLookup: "header:" + echo.HeaderXCSRFToken,
			givenCSRFCookie: "token",
			givenMethod:     http.MethodPost,
			givenHeaderTokens: map[string][]string{
				echo.HeaderXCSRFToken: {"invalid", "token"},
			},
		},
		{
			name:            "nok, invalid token from POST header",
			whenTokenLookup: "header:" + echo.HeaderXCSRFToken,
			givenCSRFCookie: "token",
			givenMethod:     http.MethodPost,
			givenHeaderTokens: map[string][]string{
				echo.HeaderXCSRFToken: {"invalid_token"},
			},
			expectError: "code=403, message=invalid csrf token",
		},
		{
			name:              "nok, missing token from POST header",
			whenTokenLookup:   "header:" + echo.HeaderXCSRFToken,
			givenCSRFCookie:   "token",
			givenMethod:       http.MethodPost,
			givenHeaderTokens: map[string][]string{},
			expectError:       "code=400, message=missing csrf token in request header",
		},
		{
			name:            "ok, token from PUT query param",
			whenTokenLookup: "query:csrf-param",
			givenCSRFCookie: "token",
			givenMethod:     http.MethodPut,
			givenQueryTokens: map[string][]string{
				"csrf-param": {"token"},
			},
		},
		{
			name:            "nok, token from PUT query form, second token would pass",
			whenTokenLookup: "query:csrf",
			givenCSRFCookie: "token",
			givenMethod:     http.MethodPut,
			givenQueryTokens: map[string][]string{
				"csrf": {"invalid", "token"},
			},
		},
		{
			name:            "nok, invalid token from PUT query form",
			whenTokenLookup: "query:csrf",
			givenCSRFCookie: "token",
			givenMethod:     http.MethodPut,
			givenQueryTokens: map[string][]string{
				"csrf": {"invalid_token"},
			},
			expectError: "code=403, message=invalid csrf token",
		},
		{
			name:             "nok, missing token from PUT query form",
			whenTokenLookup:  "query:csrf",
			givenCSRFCookie:  "token",
			givenMethod:      http.MethodPut,
			givenQueryTokens: map[string][]string{},
			expectError:      "code=400, message=missing csrf token in the query string",
		},
		{
			name:                    "nok, invalid TokenLookup",
			whenTokenLookup:         "q",
			givenCSRFCookie:         "token",
			givenMethod:             http.MethodPut,
			givenQueryTokens:        map[string][]string{},
			expectToMiddlewareError: "extractor source for lookup could not be split into needed parts: q",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			e := echo.New()

			q := make(url.Values)
			for queryParam, values := range tc.givenQueryTokens {
				for _, v := range values {
					q.Add(queryParam, v)
				}
			}

			f := make(url.Values)
			for formKey, values := range tc.givenFormTokens {
				for _, v := range values {
					f.Add(formKey, v)
				}
			}

			var req *http.Request
			switch tc.givenMethod {
			case http.MethodGet:
				req = httptest.NewRequest(http.MethodGet, "/?"+q.Encode(), nil)
			case http.MethodPost, http.MethodPut:
				req = httptest.NewRequest(http.MethodPost, "/?"+q.Encode(), strings.NewReader(f.Encode()))
				req.Header.Add(echo.HeaderContentType, echo.MIMEApplicationForm)
			}

			for header, values := range tc.givenHeaderTokens {
				for _, v := range values {
					req.Header.Add(header, v)
				}
			}

			if tc.givenCSRFCookie != "" {
				req.Header.Set(echo.HeaderCookie, "_csrf="+tc.givenCSRFCookie)
			}

			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			config := CSRFConfig{
				TokenLookup: tc.whenTokenLookup,
				CookieName:  tc.whenCookieName,
			}
			csrf, err := config.ToMiddleware()
			if tc.expectToMiddlewareError != "" {
				assert.EqualError(t, err, tc.expectToMiddlewareError)
				return
			} else if err != nil {
				assert.NoError(t, err)
			}

			h := csrf(func(c echo.Context) error {
				return c.String(http.StatusOK, "test")
			})

			err = h(c)
			if tc.expectError != "" {
				assert.EqualError(t, err, tc.expectError)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestCSRFWithConfig(t *testing.T) {
	token := randomString(16)

	var testCases = []struct {
		name                 string
		givenConfig          *CSRFConfig
		whenMethod           string
		whenHeaders          map[string]string
		expectEmptyBody      bool
		expectMWError        string
		expectCookieContains string
		expectTokenInContext string
		expectErr            string
	}{
		{
			name:                 "ok, GET",
			whenMethod:           http.MethodGet,
			expectCookieContains: "_csrf",
			expectTokenInContext: "TESTTOKEN",
		},
		{
			name: "ok, POST valid token",
			whenHeaders: map[string]string{
				echo.HeaderCookie:     "_csrf=" + token,
				echo.HeaderXCSRFToken: token,
			},
			whenMethod:           http.MethodPost,
			expectCookieContains: "_csrf",
			expectTokenInContext: token,
		},
		{
			name:            "nok, POST without token",
			whenMethod:      http.MethodPost,
			expectEmptyBody: true,
			expectErr:       `code=400, message=missing csrf token in request header`,
		},
		{
			name:            "nok, POST empty token",
			whenHeaders:     map[string]string{echo.HeaderXCSRFToken: ""},
			whenMethod:      http.MethodPost,
			expectEmptyBody: true,
			expectErr:       `code=403, message=invalid csrf token`,
		},
		{
			name: "nok, invalid trusted origin in Config",
			givenConfig: &CSRFConfig{
				TrustedOrigins: []string{"http://example.com", "invalid"},
			},
			expectMWError: `trusted origin is missing scheme or host: invalid`,
		},
		{
			name: "ok, TokenLength",
			givenConfig: &CSRFConfig{
				TokenLength: 16,
			},
			whenMethod:           http.MethodGet,
			expectCookieContains: "_csrf",
			expectTokenInContext: "TESTTOKEN",
		},
		{
			name: "ok, unsafe method + SecFetchSite=same-origin passes",
			whenHeaders: map[string]string{
				echo.HeaderSecFetchSite: "same-origin",
			},
			whenMethod:           http.MethodPost,
			expectTokenInContext: "_echo_csrf_using_sec_fetch_site_",
		},
		{
			name: "ok, safe method + SecFetchSite=same-origin passes",
			whenHeaders: map[string]string{
				echo.HeaderSecFetchSite: "same-origin",
			},
			whenMethod:           http.MethodGet,
			expectTokenInContext: "_echo_csrf_using_sec_fetch_site_",
		},
		{
			name: "nok, unsafe method + SecFetchSite=same-cross blocked",
			whenHeaders: map[string]string{
				echo.HeaderSecFetchSite: "same-cross",
			},
			whenMethod:      http.MethodPost,
			expectEmptyBody: true,
			expectErr:       `code=403, message=cross-site request blocked by CSRF`,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			e := echo.New()

			req := httptest.NewRequest(cmp.Or(tc.whenMethod, http.MethodPost), "/", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			for key, value := range tc.whenHeaders {
				req.Header.Set(key, value)
			}

			config := CSRFConfig{}
			if tc.givenConfig != nil {
				config = *tc.givenConfig
			}
			if config.generator == nil {
				config.generator = func(_ uint8) string {
					return "TESTTOKEN"
				}
			}
			mw, err := config.ToMiddleware()
			if tc.expectMWError != "" {
				assert.EqualError(t, err, tc.expectMWError)
				return
			}
			assert.NoError(t, err)

			h := mw(func(c echo.Context) error {
				cToken := c.Get(cmp.Or(config.ContextKey, DefaultCSRFConfig.ContextKey))
				assert.Equal(t, tc.expectTokenInContext, cToken)
				return c.String(http.StatusOK, "test")
			})

			err = h(c)
			if tc.expectErr != "" {
				assert.EqualError(t, err, tc.expectErr)
			} else {
				assert.NoError(t, err)
			}

			expect := "test"
			if tc.expectEmptyBody {
				expect = ""
			}
			assert.Equal(t, expect, rec.Body.String())

			if tc.expectCookieContains != "" {
				assert.Contains(t, rec.Header().Get(echo.HeaderSetCookie), tc.expectCookieContains)
			}
		})
	}
}

func TestCSRF(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	csrf := CSRF()
	h := csrf(func(c echo.Context) error {
		return c.String(http.StatusOK, "test")
	})

	// Generate CSRF token
	h(c)
	assert.Contains(t, rec.Header().Get(echo.HeaderSetCookie), "_csrf")

}

func TestCSRFSetSameSiteMode(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	csrf := CSRFWithConfig(CSRFConfig{
		CookieSameSite: http.SameSiteStrictMode,
	})

	h := csrf(func(c echo.Context) error {
		return c.String(http.StatusOK, "test")
	})

	r := h(c)
	assert.NoError(t, r)
	assert.Regexp(t, "SameSite=Strict", rec.Header()["Set-Cookie"])
}

func TestCSRFWithoutSameSiteMode(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	csrf := CSRFWithConfig(CSRFConfig{})

	h := csrf(func(c echo.Context) error {
		return c.String(http.StatusOK, "test")
	})

	r := h(c)
	assert.NoError(t, r)
	assert.NotRegexp(t, "SameSite=", rec.Header()["Set-Cookie"])
}

func TestCSRFWithSameSiteDefaultMode(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	csrf := CSRFWithConfig(CSRFConfig{
		CookieSameSite: http.SameSiteDefaultMode,
	})

	h := csrf(func(c echo.Context) error {
		return c.String(http.StatusOK, "test")
	})

	r := h(c)
	assert.NoError(t, r)
	assert.NotRegexp(t, "SameSite=", rec.Header()["Set-Cookie"])
}

func TestCSRFWithSameSiteModeNone(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	csrf, err := CSRFConfig{
		CookieSameSite: http.SameSiteNoneMode,
	}.ToMiddleware()
	assert.NoError(t, err)

	h := csrf(func(c echo.Context) error {
		return c.String(http.StatusOK, "test")
	})

	r := h(c)
	assert.NoError(t, r)
	assert.Regexp(t, "SameSite=None", rec.Header()["Set-Cookie"])
	assert.Regexp(t, "Secure", rec.Header()["Set-Cookie"])
}

func TestCSRFConfig_skipper(t *testing.T) {
	var testCases = []struct {
		name          string
		whenSkip      bool
		expectCookies int
	}{
		{
			name:          "do skip",
			whenSkip:      true,
			expectCookies: 0,
		},
		{
			name:          "do not skip",
			whenSkip:      false,
			expectCookies: 1,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			e := echo.New()
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			csrf := CSRFWithConfig(CSRFConfig{
				Skipper: func(c echo.Context) bool {
					return tc.whenSkip
				},
			})

			h := csrf(func(c echo.Context) error {
				return c.String(http.StatusOK, "test")
			})

			r := h(c)
			assert.NoError(t, r)
			cookie := rec.Header()["Set-Cookie"]
			assert.Len(t, cookie, tc.expectCookies)
		})
	}
}

func TestCSRFErrorHandling(t *testing.T) {
	cfg := CSRFConfig{
		ErrorHandler: func(err error, c echo.Context) error {
			return echo.NewHTTPError(http.StatusTeapot, "error_handler_executed")
		},
	}

	e := echo.New()
	e.POST("/", func(c echo.Context) error {
		return c.String(http.StatusNotImplemented, "should not end up here")
	})

	e.Use(CSRFWithConfig(cfg))

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	res := httptest.NewRecorder()
	e.ServeHTTP(res, req)

	assert.Equal(t, http.StatusTeapot, res.Code)
	assert.Equal(t, "{\"message\":\"error_handler_executed\"}\n", res.Body.String())
}

func TestCSRFConfig_checkSecFetchSiteRequest(t *testing.T) {
	var testCases = []struct {
		name             string
		givenConfig      CSRFConfig
		whenMethod       string
		whenSecFetchSite string
		whenOrigin       string
		expectAllow      bool
		expectErr        string
	}{
		{
			name:             "ok, unsafe POST, no SecFetchSite is not blocked",
			givenConfig:      CSRFConfig{},
			whenMethod:       http.MethodPost,
			whenSecFetchSite: "",
			expectAllow:      false, // should fall back to token CSRF
		},
		{
			name:             "ok, safe GET + same-origin passes",
			givenConfig:      CSRFConfig{},
			whenMethod:       http.MethodGet,
			whenSecFetchSite: "same-origin",
			expectAllow:      true,
		},
		{
			name:             "ok, safe GET + none passes",
			givenConfig:      CSRFConfig{},
			whenMethod:       http.MethodGet,
			whenSecFetchSite: "none",
			expectAllow:      true,
		},
		{
			name:             "ok, safe GET + same-site passes",
			givenConfig:      CSRFConfig{},
			whenMethod:       http.MethodGet,
			whenSecFetchSite: "same-site",
			expectAllow:      true,
		},
		{
			name:             "ok, safe GET + cross-site passes",
			givenConfig:      CSRFConfig{},
			whenMethod:       http.MethodGet,
			whenSecFetchSite: "cross-site",
			expectAllow:      true,
		},
		{
			name:             "nok, unsafe POST + cross-site is blocked",
			givenConfig:      CSRFConfig{},
			whenMethod:       http.MethodPost,
			whenSecFetchSite: "cross-site",
			expectAllow:      false,
			expectErr:        `code=403, message=cross-site request blocked by CSRF`,
		},
		{
			name:             "nok, unsafe POST + same-site is blocked",
			givenConfig:      CSRFConfig{},
			whenMethod:       http.MethodPost,
			whenSecFetchSite: "same-site",
			expectAllow:      false,
		},
		{
			name:             "ok, unsafe POST + same-origin passes",
			givenConfig:      CSRFConfig{},
			whenMethod:       http.MethodPost,
			whenSecFetchSite: "same-origin",
			expectAllow:      true,
		},
		{
			name:             "ok, unsafe POST + none passes",
			givenConfig:      CSRFConfig{},
			whenMethod:       http.MethodPost,
			whenSecFetchSite: "none",
			expectAllow:      true,
		},
		{
			name:             "ok, unsafe PUT + same-origin passes",
			givenConfig:      CSRFConfig{},
			whenMethod:       http.MethodPut,
			whenSecFetchSite: "same-origin",
			expectAllow:      true,
		},
		{
			name:             "ok, unsafe PUT + none passes",
			givenConfig:      CSRFConfig{},
			whenMethod:       http.MethodPut,
			whenSecFetchSite: "none",
			expectAllow:      true,
		},
		{
			name:             "ok, unsafe DELETE + same-origin passes",
			givenConfig:      CSRFConfig{},
			whenMethod:       http.MethodDelete,
			whenSecFetchSite: "same-origin",
			expectAllow:      true,
		},
		{
			name:             "ok, unsafe PATCH + same-origin passes",
			givenConfig:      CSRFConfig{},
			whenMethod:       http.MethodPatch,
			whenSecFetchSite: "same-origin",
			expectAllow:      true,
		},
		{
			name:             "nok, unsafe PUT + cross-site is blocked",
			givenConfig:      CSRFConfig{},
			whenMethod:       http.MethodPut,
			whenSecFetchSite: "cross-site",
			expectAllow:      false,
			expectErr:        `code=403, message=cross-site request blocked by CSRF`,
		},
		{
			name:             "nok, unsafe PUT + same-site is blocked",
			givenConfig:      CSRFConfig{},
			whenMethod:       http.MethodPut,
			whenSecFetchSite: "same-site",
			expectAllow:      false,
		},
		{
			name:             "nok, unsafe DELETE + cross-site is blocked",
			givenConfig:      CSRFConfig{},
			whenMethod:       http.MethodDelete,
			whenSecFetchSite: "cross-site",
			expectAllow:      false,
			expectErr:        `code=403, message=cross-site request blocked by CSRF`,
		},
		{
			name:             "nok, unsafe DELETE + same-site is blocked",
			givenConfig:      CSRFConfig{},
			whenMethod:       http.MethodDelete,
			whenSecFetchSite: "same-site",
			expectAllow:      false,
		},
		{
			name:             "nok, unsafe PATCH + cross-site is blocked",
			givenConfig:      CSRFConfig{},
			whenMethod:       http.MethodPatch,
			whenSecFetchSite: "cross-site",
			expectAllow:      false,
			expectErr:        `code=403, message=cross-site request blocked by CSRF`,
		},
		{
			name:             "ok, safe HEAD + same-origin passes",
			givenConfig:      CSRFConfig{},
			whenMethod:       http.MethodHead,
			whenSecFetchSite: "same-origin",
			expectAllow:      true,
		},
		{
			name:             "ok, safe HEAD + cross-site passes",
			givenConfig:      CSRFConfig{},
			whenMethod:       http.MethodHead,
			whenSecFetchSite: "cross-site",
			expectAllow:      true,
		},
		{
			name:             "ok, safe OPTIONS + cross-site passes",
			givenConfig:      CSRFConfig{},
			whenMethod:       http.MethodOptions,
			whenSecFetchSite: "cross-site",
			expectAllow:      true,
		},
		{
			name:             "ok, safe TRACE + cross-site passes",
			givenConfig:      CSRFConfig{},
			whenMethod:       http.MethodTrace,
			whenSecFetchSite: "cross-site",
			expectAllow:      true,
		},
		{
			name: "ok, unsafe POST + cross-site + matching trusted origin passes",
			givenConfig: CSRFConfig{
				TrustedOrigins: []string{"https://trusted.example.com"},
			},
			whenMethod:       http.MethodPost,
			whenSecFetchSite: "cross-site",
			whenOrigin:       "https://trusted.example.com",
			expectAllow:      true,
		},
		{
			name: "ok, unsafe POST + same-site + matching trusted origin passes",
			givenConfig: CSRFConfig{
				TrustedOrigins: []string{"https://trusted.example.com"},
			},
			whenMethod:       http.MethodPost,
			whenSecFetchSite: "same-site",
			whenOrigin:       "https://trusted.example.com",
			expectAllow:      true,
		},
		{
			name: "nok, unsafe POST + cross-site + non-matching origin is blocked",
			givenConfig: CSRFConfig{
				TrustedOrigins: []string{"https://trusted.example.com"},
			},
			whenMethod:       http.MethodPost,
			whenSecFetchSite: "cross-site",
			whenOrigin:       "https://evil.example.com",
			expectAllow:      false,
			expectErr:        `code=403, message=cross-site request blocked by CSRF`,
		},
		{
			name: "ok, unsafe POST + cross-site + case-insensitive trusted origin match passes",
			givenConfig: CSRFConfig{
				TrustedOrigins: []string{"https://trusted.example.com"},
			},
			whenMethod:       http.MethodPost,
			whenSecFetchSite: "cross-site",
			whenOrigin:       "https://TRUSTED.example.com",
			expectAllow:      true,
		},
		{
			name: "ok, unsafe POST + same-origin + trusted origins configured but not matched passes",
			givenConfig: CSRFConfig{
				TrustedOrigins: []string{"https://trusted.example.com"},
			},
			whenMethod:       http.MethodPost,
			whenSecFetchSite: "same-origin",
			whenOrigin:       "https://different.example.com",
			expectAllow:      true,
		},
		{
			name: "nok, unsafe POST + cross-site + empty origin + trusted origins configured is blocked",
			givenConfig: CSRFConfig{
				TrustedOrigins: []string{"https://trusted.example.com"},
			},
			whenMethod:       http.MethodPost,
			whenSecFetchSite: "cross-site",
			whenOrigin:       "",
			expectAllow:      false,
			expectErr:        `code=403, message=cross-site request blocked by CSRF`,
		},
		{
			name: "ok, unsafe POST + cross-site + multiple trusted origins, second one matches",
			givenConfig: CSRFConfig{
				TrustedOrigins: []string{"https://first.example.com", "https://second.example.com"},
			},
			whenMethod:       http.MethodPost,
			whenSecFetchSite: "cross-site",
			whenOrigin:       "https://second.example.com",
			expectAllow:      true,
		},
		{
			name: "ok, unsafe POST + same-site + custom func allows",
			givenConfig: CSRFConfig{
				AllowSecFetchSiteFunc: func(c echo.Context) (bool, error) {
					return true, nil
				},
			},
			whenMethod:       http.MethodPost,
			whenSecFetchSite: "same-site",
			expectAllow:      true,
		},
		{
			name: "ok, unsafe POST + cross-site + custom func allows",
			givenConfig: CSRFConfig{
				AllowSecFetchSiteFunc: func(c echo.Context) (bool, error) {
					return true, nil
				},
			},
			whenMethod:       http.MethodPost,
			whenSecFetchSite: "cross-site",
			expectAllow:      true,
		},
		{
			name: "nok, unsafe POST + same-site + custom func returns custom error",
			givenConfig: CSRFConfig{
				AllowSecFetchSiteFunc: func(c echo.Context) (bool, error) {
					return false, echo.NewHTTPError(http.StatusTeapot, "custom error from func")
				},
			},
			whenMethod:       http.MethodPost,
			whenSecFetchSite: "same-site",
			expectAllow:      false,
			expectErr:        `code=418, message=custom error from func`,
		},
		{
			name: "nok, unsafe POST + cross-site + custom func returns false with nil error",
			givenConfig: CSRFConfig{
				AllowSecFetchSiteFunc: func(c echo.Context) (bool, error) {
					return false, nil
				},
			},
			whenMethod:       http.MethodPost,
			whenSecFetchSite: "cross-site",
			expectAllow:      false,
			expectErr:        "", // custom func returns nil error, so no error expected
		},
		{
			name:             "nok, unsafe POST + invalid Sec-Fetch-Site value treated as cross-site",
			givenConfig:      CSRFConfig{},
			whenMethod:       http.MethodPost,
			whenSecFetchSite: "invalid-value",
			expectAllow:      false,
			expectErr:        `code=403, message=cross-site request blocked by CSRF`,
		},
		{
			name: "ok, unsafe POST + cross-site + trusted origin takes precedence over custom func",
			givenConfig: CSRFConfig{
				TrustedOrigins: []string{"https://trusted.example.com"},
				AllowSecFetchSiteFunc: func(c echo.Context) (bool, error) {
					return false, echo.NewHTTPError(http.StatusTeapot, "should not be called")
				},
			},
			whenMethod:       http.MethodPost,
			whenSecFetchSite: "cross-site",
			whenOrigin:       "https://trusted.example.com",
			expectAllow:      true,
		},
		{
			name: "nok, unsafe POST + cross-site + trusted origin not matched, custom func blocks",
			givenConfig: CSRFConfig{
				TrustedOrigins: []string{"https://trusted.example.com"},
				AllowSecFetchSiteFunc: func(c echo.Context) (bool, error) {
					return false, echo.NewHTTPError(http.StatusTeapot, "custom block")
				},
			},
			whenMethod:       http.MethodPost,
			whenSecFetchSite: "cross-site",
			whenOrigin:       "https://evil.example.com",
			expectAllow:      false,
			expectErr:        `code=418, message=custom block`,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.whenMethod, "/", nil)
			if tc.whenSecFetchSite != "" {
				req.Header.Set(echo.HeaderSecFetchSite, tc.whenSecFetchSite)
			}
			if tc.whenOrigin != "" {
				req.Header.Set(echo.HeaderOrigin, tc.whenOrigin)
			}

			res := httptest.NewRecorder()
			e := echo.New()
			c := e.NewContext(req, res)

			allow, err := tc.givenConfig.checkSecFetchSiteRequest(c)

			assert.Equal(t, tc.expectAllow, allow)
			if tc.expectErr != "" {
				assert.EqualError(t, err, tc.expectErr)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
