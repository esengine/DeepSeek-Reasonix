// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: © 2015 LabStack LLC and Echo contributors

package middleware

import (
	"encoding/base64"
	"net/http"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"
)

// BasicAuthConfig defines the config for BasicAuth middleware.
type BasicAuthConfig struct {
	// Skipper defines a function to skip middleware.
	Skipper Skipper

	// Validator is a function to validate BasicAuth credentials.
	// Required.
	Validator BasicAuthValidator

	// Realm is a string to define realm attribute of BasicAuth.
	// Default value "Restricted".
	Realm string
}

// BasicAuthValidator defines a function to validate BasicAuth credentials.
// The function should return a boolean indicating whether the credentials are valid,
// and an error if any error occurs during the validation process.
type BasicAuthValidator func(string, string, echo.Context) (bool, error)

const (
	basic        = "basic"
	defaultRealm = "Restricted"
)

// DefaultBasicAuthConfig is the default BasicAuth middleware config.
var DefaultBasicAuthConfig = BasicAuthConfig{
	Skipper: DefaultSkipper,
	Realm:   defaultRealm,
}

// BasicAuth returns an BasicAuth middleware.
//
// For valid credentials it calls the next handler.
// For missing or invalid credentials, it sends "401 - Unauthorized" response.
func BasicAuth(fn BasicAuthValidator) echo.MiddlewareFunc {
	c := DefaultBasicAuthConfig
	c.Validator = fn
	return BasicAuthWithConfig(c)
}

// BasicAuthWithConfig returns an BasicAuth middleware with config.
// See `BasicAuth()`.
func BasicAuthWithConfig(config BasicAuthConfig) echo.MiddlewareFunc {
	// Defaults
	if config.Validator == nil {
		panic("echo: basic-auth middleware requires a validator function")
	}
	if config.Skipper == nil {
		config.Skipper = DefaultBasicAuthConfig.Skipper
	}
	if config.Realm == "" {
		config.Realm = defaultRealm
	}

	// Pre-compute the quoted realm for WWW-Authenticate header (RFC 7617)
	quotedRealm := strconv.Quote(config.Realm)

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if config.Skipper(c) {
				return next(c)
			}

			auth := c.Request().Header.Get(echo.HeaderAuthorization)
			l := len(basic)

			if len(auth) > l+1 && strings.EqualFold(auth[:l], basic) {
				// Invalid base64 shouldn't be treated as error
				// instead should be treated as invalid client input
				b, err := base64.StdEncoding.DecodeString(auth[l+1:])
				if err != nil {
					return echo.NewHTTPError(http.StatusBadRequest).SetInternal(err)
				}

				cred := string(b)
				user, pass, ok := strings.Cut(cred, ":")
				if ok {
					// Verify credentials
					valid, err := config.Validator(user, pass, c)
					if err != nil {
						return err
					} else if valid {
						return next(c)
					}
				}
			}

			// Need to return `401` for browsers to pop-up login box.
			// Realm is case-insensitive, so we can use "basic" directly. See RFC 7617.
			c.Response().Header().Set(echo.HeaderWWWAuthenticate, basic+" realm="+quotedRealm)
			return echo.ErrUnauthorized
		}
	}
}
