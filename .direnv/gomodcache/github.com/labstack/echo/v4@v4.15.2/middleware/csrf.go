// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: © 2015 LabStack LLC and Echo contributors

package middleware

import (
	"crypto/subtle"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
)

// CSRFUsingSecFetchSite is a context key for CSRF middleware what is set when the client browser is using Sec-Fetch-Site
// header and the request is deemed safe.
// It is a dummy token value that can be used to render CSRF token for form by handlers.
//
// We know that the client is using a browser that supports Sec-Fetch-Site header, so when the form is submitted in
// the future with this dummy token value it is OK. Although the request is safe, the template rendered by the
// handler may need this value to render CSRF token for form.
const CSRFUsingSecFetchSite = "_echo_csrf_using_sec_fetch_site_"

// CSRFConfig defines the config for CSRF middleware.
type CSRFConfig struct {
	// Skipper defines a function to skip middleware.
	Skipper Skipper

	// TrustedOrigin permits any request with `Sec-Fetch-Site` header whose `Origin` header
	// exactly matches the specified value.
	// Values should be formated as Origin header "scheme://host[:port]".
	//
	// See [Origin]: https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/Origin
	// See [Sec-Fetch-Site]: https://cheatsheetseries.owasp.org/cheatsheets/Cross-Site_Request_Forgery_Prevention_Cheat_Sheet.html#fetch-metadata-headers
	TrustedOrigins []string

	// AllowSecFetchSameSite allows custom behaviour for `Sec-Fetch-Site` requests that are about to
	// fail with CRSF error, to be allowed or replaced with custom error.
	// This function applies to `Sec-Fetch-Site` values:
	// - `same-site` 		same registrable domain (subdomain and/or different port)
	// - `cross-site`		request originates from different site
	// See [Sec-Fetch-Site]: https://cheatsheetseries.owasp.org/cheatsheets/Cross-Site_Request_Forgery_Prevention_Cheat_Sheet.html#fetch-metadata-headers
	AllowSecFetchSiteFunc func(c echo.Context) (bool, error)

	// TokenLength is the length of the generated token.
	TokenLength uint8 `yaml:"token_length"`
	// Optional. Default value 32.

	// TokenLookup is a string in the form of "<source>:<name>" or "<source>:<name>,<source>:<name>" that is used
	// to extract token from the request.
	// Optional. Default value "header:X-CSRF-Token".
	// Possible values:
	// - "header:<name>" or "header:<name>:<cut-prefix>"
	// - "query:<name>"
	// - "form:<name>"
	// Multiple sources example:
	// - "header:X-CSRF-Token,query:csrf"
	TokenLookup string `yaml:"token_lookup"`

	// Context key to store generated CSRF token into context.
	// Optional. Default value "csrf".
	ContextKey string `yaml:"context_key"`

	// Name of the CSRF cookie. This cookie will store CSRF token.
	// Optional. Default value "csrf".
	CookieName string `yaml:"cookie_name"`

	// Domain of the CSRF cookie.
	// Optional. Default value none.
	CookieDomain string `yaml:"cookie_domain"`

	// Path of the CSRF cookie.
	// Optional. Default value none.
	CookiePath string `yaml:"cookie_path"`

	// Max age (in seconds) of the CSRF cookie.
	// Optional. Default value 86400 (24hr).
	CookieMaxAge int `yaml:"cookie_max_age"`

	// Indicates if CSRF cookie is secure.
	// Optional. Default value false.
	CookieSecure bool `yaml:"cookie_secure"`

	// Indicates if CSRF cookie is HTTP only.
	// Optional. Default value false.
	CookieHTTPOnly bool `yaml:"cookie_http_only"`

	// Indicates SameSite mode of the CSRF cookie.
	// Optional. Default value SameSiteDefaultMode.
	CookieSameSite http.SameSite `yaml:"cookie_same_site"`

	// ErrorHandler defines a function which is executed for returning custom errors.
	ErrorHandler CSRFErrorHandler

	generator func(length uint8) string
}

// CSRFErrorHandler is a function which is executed for creating custom errors.
type CSRFErrorHandler func(err error, c echo.Context) error

// ErrCSRFInvalid is returned when CSRF check fails
var ErrCSRFInvalid = echo.NewHTTPError(http.StatusForbidden, "invalid csrf token")

// DefaultCSRFConfig is the default CSRF middleware config.
var DefaultCSRFConfig = CSRFConfig{
	Skipper:        DefaultSkipper,
	TokenLength:    32,
	TokenLookup:    "header:" + echo.HeaderXCSRFToken,
	ContextKey:     "csrf",
	CookieName:     "_csrf",
	CookieMaxAge:   86400,
	CookieSameSite: http.SameSiteDefaultMode,
}

// CSRF returns a Cross-Site Request Forgery (CSRF) middleware.
// See: https://en.wikipedia.org/wiki/Cross-site_request_forgery
func CSRF() echo.MiddlewareFunc {
	c := DefaultCSRFConfig
	return CSRFWithConfig(c)
}

// CSRFWithConfig returns a CSRF middleware with config.
// See `CSRF()`.
func CSRFWithConfig(config CSRFConfig) echo.MiddlewareFunc {
	return toMiddlewareOrPanic(config)
}

// ToMiddleware converts CSRFConfig to middleware or returns an error for invalid configuration
func (config CSRFConfig) ToMiddleware() (echo.MiddlewareFunc, error) {
	if config.Skipper == nil {
		config.Skipper = DefaultCSRFConfig.Skipper
	}
	if config.TokenLength == 0 {
		config.TokenLength = DefaultCSRFConfig.TokenLength
	}

	if config.TokenLookup == "" {
		config.TokenLookup = DefaultCSRFConfig.TokenLookup
	}
	if config.ContextKey == "" {
		config.ContextKey = DefaultCSRFConfig.ContextKey
	}
	if config.CookieName == "" {
		config.CookieName = DefaultCSRFConfig.CookieName
	}
	if config.CookieMaxAge == 0 {
		config.CookieMaxAge = DefaultCSRFConfig.CookieMaxAge
	}
	if config.CookieSameSite == http.SameSiteNoneMode {
		config.CookieSecure = true
	}
	if len(config.TrustedOrigins) > 0 {
		if vErr := validateOrigins(config.TrustedOrigins, "trusted origin"); vErr != nil {
			return nil, vErr
		}
		config.TrustedOrigins = append([]string(nil), config.TrustedOrigins...)
	}
	tokenGenerator := randomString
	if config.generator != nil {
		tokenGenerator = config.generator
	}

	extractors, cErr := CreateExtractors(config.TokenLookup)
	if cErr != nil {
		return nil, cErr
	}

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if config.Skipper(c) {
				return next(c)
			}

			// use the `Sec-Fetch-Site` header as part of a modern approach to CSRF protection
			allow, err := config.checkSecFetchSiteRequest(c)
			if err != nil {
				return err
			}
			if allow {
				return next(c)
			}

			// Fallback to legacy token based CSRF protection

			token := ""
			if k, err := c.Cookie(config.CookieName); err != nil {
				token = tokenGenerator(config.TokenLength)
			} else {
				token = k.Value // Reuse token
			}

			switch c.Request().Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
			default:
				// Validate token only for requests which are not defined as 'safe' by RFC7231
				var lastExtractorErr error
				var lastTokenErr error
			outer:
				for _, extractor := range extractors {
					clientTokens, err := extractor(c)
					if err != nil {
						lastExtractorErr = err
						continue
					}

					for _, clientToken := range clientTokens {
						if validateCSRFToken(token, clientToken) {
							lastTokenErr = nil
							lastExtractorErr = nil
							break outer
						}
						lastTokenErr = ErrCSRFInvalid
					}
				}
				var finalErr error
				if lastTokenErr != nil {
					finalErr = lastTokenErr
				} else if lastExtractorErr != nil {
					// ugly part to preserve backwards compatible errors. someone could rely on them
					if lastExtractorErr == errQueryExtractorValueMissing {
						lastExtractorErr = echo.NewHTTPError(http.StatusBadRequest, "missing csrf token in the query string")
					} else if lastExtractorErr == errFormExtractorValueMissing {
						lastExtractorErr = echo.NewHTTPError(http.StatusBadRequest, "missing csrf token in the form parameter")
					} else if lastExtractorErr == errHeaderExtractorValueMissing {
						lastExtractorErr = echo.NewHTTPError(http.StatusBadRequest, "missing csrf token in request header")
					} else {
						lastExtractorErr = echo.NewHTTPError(http.StatusBadRequest, lastExtractorErr.Error())
					}
					finalErr = lastExtractorErr
				}

				if finalErr != nil {
					if config.ErrorHandler != nil {
						return config.ErrorHandler(finalErr, c)
					}
					return finalErr
				}
			}

			// Set CSRF cookie
			cookie := new(http.Cookie)
			cookie.Name = config.CookieName
			cookie.Value = token
			if config.CookiePath != "" {
				cookie.Path = config.CookiePath
			}
			if config.CookieDomain != "" {
				cookie.Domain = config.CookieDomain
			}
			if config.CookieSameSite != http.SameSiteDefaultMode {
				cookie.SameSite = config.CookieSameSite
			}
			cookie.Expires = time.Now().Add(time.Duration(config.CookieMaxAge) * time.Second)
			cookie.Secure = config.CookieSecure
			cookie.HttpOnly = config.CookieHTTPOnly
			c.SetCookie(cookie)

			// Store token in the context
			c.Set(config.ContextKey, token)

			// Protect clients from caching the response
			c.Response().Header().Add(echo.HeaderVary, echo.HeaderCookie)

			return next(c)
		}
	}, nil
}

func validateCSRFToken(token, clientToken string) bool {
	return subtle.ConstantTimeCompare([]byte(token), []byte(clientToken)) == 1
}

var safeMethods = []string{http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace}

func (config CSRFConfig) checkSecFetchSiteRequest(c echo.Context) (bool, error) {
	// https://cheatsheetseries.owasp.org/cheatsheets/Cross-Site_Request_Forgery_Prevention_Cheat_Sheet.html#fetch-metadata-headers
	// Sec-Fetch-Site values are:
	// - `same-origin` 	exact origin match - allow always
	// - `same-site` 		same registrable domain (subdomain and/or different port) - block, unless explicitly trusted
	// - `cross-site`		request originates from different site - block, unless explicitly trusted
	// - `none`					direct navigation (URL bar, bookmark) - allow always
	secFetchSite := c.Request().Header.Get(echo.HeaderSecFetchSite)
	if secFetchSite == "" {
		return false, nil
	}

	if len(config.TrustedOrigins) > 0 {
		// trusted sites ala OAuth callbacks etc. should be let through
		origin := c.Request().Header.Get(echo.HeaderOrigin)
		if origin != "" {
			for _, trustedOrigin := range config.TrustedOrigins {
				if strings.EqualFold(origin, trustedOrigin) {
					return true, nil
				}
			}
		}
	}
	isSafe := slices.Contains(safeMethods, c.Request().Method)
	if !isSafe { // for state-changing request check SecFetchSite value
		isSafe = secFetchSite == "same-origin" || secFetchSite == "none"
	}

	if isSafe {
		// This helps handlers that support older token-based CSRF protection.
		// We know that the client is using a browser that supports Sec-Fetch-Site header, so when the form is submitted in
		// the future with this dummy token value it is OK. Although the request is safe, the template rendered by the
		// handler may need this value to render CSRF token for form.
		c.Set(config.ContextKey, CSRFUsingSecFetchSite)
		return true, nil
	}
	// we are here when request is state-changing and `cross-site` or `same-site`

	// Note: if you want to block `same-site` use config.TrustedOrigins or `config.AllowSecFetchSiteFunc`
	if config.AllowSecFetchSiteFunc != nil {
		return config.AllowSecFetchSiteFunc(c)
	}

	if secFetchSite == "same-site" {
		return false, nil // fall back to legacy token
	}
	return false, echo.NewHTTPError(http.StatusForbidden, "cross-site request blocked by CSRF")
}
