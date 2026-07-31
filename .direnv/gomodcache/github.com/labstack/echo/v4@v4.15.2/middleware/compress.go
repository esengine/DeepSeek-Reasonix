// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: © 2015 LabStack LLC and Echo contributors

package middleware

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"

	"github.com/labstack/echo/v4"
)

// GzipConfig defines the config for Gzip middleware.
type GzipConfig struct {
	// Skipper defines a function to skip middleware.
	Skipper Skipper

	// Gzip compression level.
	// Optional. Default value -1.
	Level int `yaml:"level"`

	// Length threshold before gzip compression is applied.
	// Optional. Default value 0.
	//
	// Most of the time you will not need to change the default. Compressing
	// a short response might increase the transmitted data because of the
	// gzip format overhead. Compressing the response will also consume CPU
	// and time on the server and the client (for decompressing). Depending on
	// your use case such a threshold might be useful.
	//
	// See also:
	// https://webmasters.stackexchange.com/questions/31750/what-is-recommended-minimum-object-size-for-gzip-performance-benefits
	MinLength int
}

type gzipResponseWriter struct {
	io.Writer
	http.ResponseWriter
	wroteHeader       bool
	wroteBody         bool
	minLength         int
	minLengthExceeded bool
	buffer            *bytes.Buffer
	code              int
}

const (
	gzipScheme = "gzip"
)

// DefaultGzipConfig is the default Gzip middleware config.
var DefaultGzipConfig = GzipConfig{
	Skipper:   DefaultSkipper,
	Level:     -1,
	MinLength: 0,
}

// Gzip returns a middleware which compresses HTTP response using gzip compression
// scheme.
func Gzip() echo.MiddlewareFunc {
	return GzipWithConfig(DefaultGzipConfig)
}

// GzipWithConfig return Gzip middleware with config.
// See: `Gzip()`.
func GzipWithConfig(config GzipConfig) echo.MiddlewareFunc {
	// Defaults
	if config.Skipper == nil {
		config.Skipper = DefaultGzipConfig.Skipper
	}
	if config.Level == 0 {
		config.Level = DefaultGzipConfig.Level
	}
	if config.MinLength < 0 {
		config.MinLength = DefaultGzipConfig.MinLength
	}

	pool := gzipCompressPool(config)
	bpool := bufferPool()

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if config.Skipper(c) {
				return next(c)
			}

			res := c.Response()
			res.Header().Add(echo.HeaderVary, echo.HeaderAcceptEncoding)
			if strings.Contains(c.Request().Header.Get(echo.HeaderAcceptEncoding), gzipScheme) {
				i := pool.Get()
				w, ok := i.(*gzip.Writer)
				if !ok {
					return echo.NewHTTPError(http.StatusInternalServerError, "invalid pool object")
				}
				rw := res.Writer
				w.Reset(rw)

				buf := bpool.Get().(*bytes.Buffer)
				buf.Reset()

				grw := &gzipResponseWriter{Writer: w, ResponseWriter: rw, minLength: config.MinLength, buffer: buf}
				defer func() {
					// There are different reasons for cases when we have not yet written response to the client and now need to do so.
					// a) handler response had only response code and no response body (ala 404 or redirects etc). Response code need to be written now.
					// b) body is shorter than our minimum length threshold and being buffered currently and needs to be written
					if !grw.wroteBody {
						if res.Header().Get(echo.HeaderContentEncoding) == gzipScheme {
							res.Header().Del(echo.HeaderContentEncoding)
						}
						if grw.wroteHeader {
							rw.WriteHeader(grw.code)
						}
						// We have to reset response to it's pristine state when
						// nothing is written to body or error is returned.
						// See issue #424, #407.
						res.Writer = rw
						w.Reset(io.Discard)
					} else if !grw.minLengthExceeded {
						// Write uncompressed response
						res.Writer = rw
						if grw.wroteHeader {
							grw.ResponseWriter.WriteHeader(grw.code)
						}
						grw.buffer.WriteTo(rw)
						w.Reset(io.Discard)
					}
					w.Close()
					bpool.Put(buf)
					pool.Put(w)
				}()
				res.Writer = grw
			}
			return next(c)
		}
	}
}

func (w *gzipResponseWriter) WriteHeader(code int) {
	w.Header().Del(echo.HeaderContentLength) // Issue #444

	w.wroteHeader = true

	// Delay writing of the header until we know if we'll actually compress the response
	w.code = code
}

func (w *gzipResponseWriter) Write(b []byte) (int, error) {
	if w.Header().Get(echo.HeaderContentType) == "" {
		w.Header().Set(echo.HeaderContentType, http.DetectContentType(b))
	}
	w.wroteBody = true

	if !w.minLengthExceeded {
		n, err := w.buffer.Write(b)

		if w.buffer.Len() >= w.minLength {
			w.minLengthExceeded = true

			// The minimum length is exceeded, add Content-Encoding header and write the header
			w.Header().Set(echo.HeaderContentEncoding, gzipScheme) // Issue #806
			if w.wroteHeader {
				w.ResponseWriter.WriteHeader(w.code)
			}

			return w.Writer.Write(w.buffer.Bytes())
		}

		return n, err
	}

	return w.Writer.Write(b)
}

func (w *gzipResponseWriter) Flush() {
	if !w.minLengthExceeded {
		// Enforce compression because we will not know how much more data will come
		w.minLengthExceeded = true
		w.Header().Set(echo.HeaderContentEncoding, gzipScheme) // Issue #806
		if w.wroteHeader {
			w.ResponseWriter.WriteHeader(w.code)
		}

		w.Writer.Write(w.buffer.Bytes())
	}

	if gw, ok := w.Writer.(*gzip.Writer); ok {
		gw.Flush()
	}
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}

func (w *gzipResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func (w *gzipResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return http.NewResponseController(w.ResponseWriter).Hijack()
}

func (w *gzipResponseWriter) Push(target string, opts *http.PushOptions) error {
	if p, ok := w.ResponseWriter.(http.Pusher); ok {
		return p.Push(target, opts)
	}
	return http.ErrNotSupported
}

func gzipCompressPool(config GzipConfig) sync.Pool {
	return sync.Pool{
		New: func() interface{} {
			w, err := gzip.NewWriterLevel(io.Discard, config.Level)
			if err != nil {
				return err
			}
			return w
		},
	}
}

func bufferPool() sync.Pool {
	return sync.Pool{
		New: func() interface{} {
			b := &bytes.Buffer{}
			return b
		},
	}
}
