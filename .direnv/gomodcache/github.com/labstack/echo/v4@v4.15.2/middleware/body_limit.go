// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: © 2015 LabStack LLC and Echo contributors

package middleware

import (
	"fmt"
	"io"
	"net/http"
	"sync"

	"github.com/labstack/echo/v4"
	"github.com/labstack/gommon/bytes"
)

// BodyLimitConfig defines the config for BodyLimit middleware.
type BodyLimitConfig struct {
	// Skipper defines a function to skip middleware.
	Skipper Skipper

	// Maximum allowed size for a request body, it can be specified
	// as `4x` or `4xB`, where x is one of the multiple from K, M, G, T or P.
	Limit string `yaml:"limit"`
	limit int64
}

type limitedReader struct {
	BodyLimitConfig
	reader io.ReadCloser
	read   int64
}

// DefaultBodyLimitConfig is the default BodyLimit middleware config.
var DefaultBodyLimitConfig = BodyLimitConfig{
	Skipper: DefaultSkipper,
}

// BodyLimit returns a BodyLimit middleware.
//
// BodyLimit middleware sets the maximum allowed size for a request body, if the
// size exceeds the configured limit, it sends "413 - Request Entity Too Large"
// response. The BodyLimit is determined based on both `Content-Length` request
// header and actual content read, which makes it super secure.
// Limit can be specified as `4x` or `4xB`, where x is one of the multiple from K, M,
// G, T or P.
func BodyLimit(limit string) echo.MiddlewareFunc {
	c := DefaultBodyLimitConfig
	c.Limit = limit
	return BodyLimitWithConfig(c)
}

// BodyLimitWithConfig returns a BodyLimit middleware with config.
// See: `BodyLimit()`.
func BodyLimitWithConfig(config BodyLimitConfig) echo.MiddlewareFunc {
	// Defaults
	if config.Skipper == nil {
		config.Skipper = DefaultBodyLimitConfig.Skipper
	}

	limit, err := bytes.Parse(config.Limit)
	if err != nil {
		panic(fmt.Errorf("echo: invalid body-limit=%s", config.Limit))
	}
	config.limit = limit
	pool := limitedReaderPool(config)

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if config.Skipper(c) {
				return next(c)
			}

			req := c.Request()

			// Based on content length
			if req.ContentLength > config.limit {
				return echo.ErrStatusRequestEntityTooLarge
			}

			// Based on content read
			r, ok := pool.Get().(*limitedReader)
			if !ok {
				return echo.NewHTTPError(http.StatusInternalServerError, "invalid pool object")
			}
			r.Reset(req.Body)
			defer pool.Put(r)
			req.Body = r

			return next(c)
		}
	}
}

func (r *limitedReader) Read(b []byte) (n int, err error) {
	n, err = r.reader.Read(b)
	r.read += int64(n)
	if r.read > r.limit {
		return n, echo.ErrStatusRequestEntityTooLarge
	}
	return
}

func (r *limitedReader) Close() error {
	return r.reader.Close()
}

func (r *limitedReader) Reset(reader io.ReadCloser) {
	r.reader = reader
	r.read = 0
}

func limitedReaderPool(c BodyLimitConfig) sync.Pool {
	return sync.Pool{
		New: func() interface{} {
			return &limitedReader{BodyLimitConfig: c}
		},
	}
}
