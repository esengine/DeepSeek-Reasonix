// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: © 2015 LabStack LLC and Echo contributors

package middleware

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"net"
	"net/http"

	"github.com/labstack/echo/v4"
)

// BodyDumpConfig defines the config for BodyDump middleware.
type BodyDumpConfig struct {
	// Skipper defines a function to skip middleware.
	Skipper Skipper

	// Handler receives request and response payload.
	// Required.
	Handler BodyDumpHandler
}

// BodyDumpHandler receives the request and response payload.
type BodyDumpHandler func(echo.Context, []byte, []byte)

type bodyDumpResponseWriter struct {
	io.Writer
	http.ResponseWriter
}

// DefaultBodyDumpConfig is the default BodyDump middleware config.
var DefaultBodyDumpConfig = BodyDumpConfig{
	Skipper: DefaultSkipper,
}

// BodyDump returns a BodyDump middleware.
//
// BodyDump middleware captures the request and response payload and calls the
// registered handler.
func BodyDump(handler BodyDumpHandler) echo.MiddlewareFunc {
	c := DefaultBodyDumpConfig
	c.Handler = handler
	return BodyDumpWithConfig(c)
}

// BodyDumpWithConfig returns a BodyDump middleware with config.
// See: `BodyDump()`.
func BodyDumpWithConfig(config BodyDumpConfig) echo.MiddlewareFunc {
	// Defaults
	if config.Handler == nil {
		panic("echo: body-dump middleware requires a handler function")
	}
	if config.Skipper == nil {
		config.Skipper = DefaultBodyDumpConfig.Skipper
	}

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) (err error) {
			if config.Skipper(c) {
				return next(c)
			}

			// Request
			reqBody := []byte{}
			if c.Request().Body != nil {
				var readErr error
				reqBody, readErr = io.ReadAll(c.Request().Body)
				if readErr != nil {
					return readErr
				}
			}
			c.Request().Body = io.NopCloser(bytes.NewBuffer(reqBody)) // Reset

			// Response
			resBody := new(bytes.Buffer)
			mw := io.MultiWriter(c.Response().Writer, resBody)
			writer := &bodyDumpResponseWriter{Writer: mw, ResponseWriter: c.Response().Writer}
			c.Response().Writer = writer

			if err = next(c); err != nil {
				c.Error(err)
			}

			// Callback
			config.Handler(c, reqBody, resBody.Bytes())

			return
		}
	}
}

func (w *bodyDumpResponseWriter) WriteHeader(code int) {
	w.ResponseWriter.WriteHeader(code)
}

func (w *bodyDumpResponseWriter) Write(b []byte) (int, error) {
	return w.Writer.Write(b)
}

func (w *bodyDumpResponseWriter) Flush() {
	err := http.NewResponseController(w.ResponseWriter).Flush()
	if err != nil && errors.Is(err, http.ErrNotSupported) {
		panic(errors.New("response writer flushing is not supported"))
	}
}

func (w *bodyDumpResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return http.NewResponseController(w.ResponseWriter).Hijack()
}

func (w *bodyDumpResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
