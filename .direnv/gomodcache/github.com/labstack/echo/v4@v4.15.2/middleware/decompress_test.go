// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: © 2015 LabStack LLC and Echo contributors

package middleware

import (
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
)

func TestDecompress(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("test"))
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	// Skip if no Content-Encoding header
	h := Decompress()(func(c echo.Context) error {
		c.Response().Write([]byte("test")) // For Content-Type sniffing
		return nil
	})
	h(c)

	assert.Equal(t, "test", rec.Body.String())

	// Decompress
	body := `{"name": "echo"}`
	gz, _ := gzipString(body)
	req = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(string(gz)))
	req.Header.Set(echo.HeaderContentEncoding, GZIPEncoding)
	rec = httptest.NewRecorder()
	c = e.NewContext(req, rec)
	h(c)
	assert.Equal(t, GZIPEncoding, req.Header.Get(echo.HeaderContentEncoding))
	b, err := io.ReadAll(req.Body)
	assert.NoError(t, err)
	assert.Equal(t, body, string(b))
}

func TestDecompressDefaultConfig(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("test"))
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h := DecompressWithConfig(DecompressConfig{})(func(c echo.Context) error {
		c.Response().Write([]byte("test")) // For Content-Type sniffing
		return nil
	})
	h(c)

	assert.Equal(t, "test", rec.Body.String())

	// Decompress
	body := `{"name": "echo"}`
	gz, _ := gzipString(body)
	req = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(string(gz)))
	req.Header.Set(echo.HeaderContentEncoding, GZIPEncoding)
	rec = httptest.NewRecorder()
	c = e.NewContext(req, rec)
	h(c)
	assert.Equal(t, GZIPEncoding, req.Header.Get(echo.HeaderContentEncoding))
	b, err := io.ReadAll(req.Body)
	assert.NoError(t, err)
	assert.Equal(t, body, string(b))
}

func TestCompressRequestWithoutDecompressMiddleware(t *testing.T) {
	e := echo.New()
	body := `{"name":"echo"}`
	gz, _ := gzipString(body)
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(string(gz)))
	req.Header.Set(echo.HeaderContentEncoding, GZIPEncoding)
	rec := httptest.NewRecorder()
	e.NewContext(req, rec)
	e.ServeHTTP(rec, req)
	assert.Equal(t, GZIPEncoding, req.Header.Get(echo.HeaderContentEncoding))
	b, err := io.ReadAll(req.Body)
	assert.NoError(t, err)
	assert.NotEqual(t, b, body)
	assert.Equal(t, b, gz)
}

func TestDecompressNoContent(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(echo.HeaderContentEncoding, GZIPEncoding)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	h := Decompress()(func(c echo.Context) error {
		return c.NoContent(http.StatusNoContent)
	})
	if assert.NoError(t, h(c)) {
		assert.Equal(t, GZIPEncoding, req.Header.Get(echo.HeaderContentEncoding))
		assert.Empty(t, rec.Header().Get(echo.HeaderContentType))
		assert.Equal(t, 0, len(rec.Body.Bytes()))
	}
}

func TestDecompressErrorReturned(t *testing.T) {
	e := echo.New()
	e.Use(Decompress())
	e.GET("/", func(c echo.Context) error {
		return echo.ErrNotFound
	})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(echo.HeaderContentEncoding, GZIPEncoding)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Empty(t, rec.Header().Get(echo.HeaderContentEncoding))
}

func TestDecompressSkipper(t *testing.T) {
	e := echo.New()
	e.Use(DecompressWithConfig(DecompressConfig{
		Skipper: func(c echo.Context) bool {
			return c.Request().URL.Path == "/skip"
		},
	}))
	body := `{"name": "echo"}`
	req := httptest.NewRequest(http.MethodPost, "/skip", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentEncoding, GZIPEncoding)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	e.ServeHTTP(rec, req)
	assert.Equal(t, rec.Header().Get(echo.HeaderContentType), echo.MIMEApplicationJSON)
	reqBody, err := io.ReadAll(c.Request().Body)
	assert.NoError(t, err)
	assert.Equal(t, body, string(reqBody))
}

type TestDecompressPoolWithError struct {
}

func (d *TestDecompressPoolWithError) gzipDecompressPool() sync.Pool {
	return sync.Pool{
		New: func() interface{} {
			return errors.New("pool error")
		},
	}
}

func TestDecompressPoolError(t *testing.T) {
	e := echo.New()
	e.Use(DecompressWithConfig(DecompressConfig{
		Skipper:            DefaultSkipper,
		GzipDecompressPool: &TestDecompressPoolWithError{},
	}))
	body := `{"name": "echo"}`
	req := httptest.NewRequest(http.MethodPost, "/echo", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentEncoding, GZIPEncoding)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	e.ServeHTTP(rec, req)
	assert.Equal(t, GZIPEncoding, req.Header.Get(echo.HeaderContentEncoding))
	reqBody, err := io.ReadAll(c.Request().Body)
	assert.NoError(t, err)
	assert.Equal(t, body, string(reqBody))
	assert.Equal(t, rec.Code, http.StatusInternalServerError)
}

func BenchmarkDecompress(b *testing.B) {
	e := echo.New()
	body := `{"name": "echo"}`
	gz, _ := gzipString(body)
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(string(gz)))
	req.Header.Set(echo.HeaderContentEncoding, GZIPEncoding)

	h := Decompress()(func(c echo.Context) error {
		c.Response().Write([]byte(body)) // For Content-Type sniffing
		return nil
	})

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		// Decompress
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		h(c)
	}
}

func gzipString(body string) ([]byte, error) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)

	_, err := gz.Write([]byte(body))
	if err != nil {
		return nil, err
	}

	if err := gz.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}
