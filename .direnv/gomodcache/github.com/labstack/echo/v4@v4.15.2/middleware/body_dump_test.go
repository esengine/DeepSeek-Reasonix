// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: © 2015 LabStack LLC and Echo contributors

package middleware

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
)

func TestBodyDump(t *testing.T) {
	e := echo.New()
	hw := "Hello, World!"
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(hw))
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	h := func(c echo.Context) error {
		body, err := io.ReadAll(c.Request().Body)
		if err != nil {
			return err
		}
		return c.String(http.StatusOK, string(body))
	}

	requestBody := ""
	responseBody := ""
	mw := BodyDump(func(c echo.Context, reqBody, resBody []byte) {
		requestBody = string(reqBody)
		responseBody = string(resBody)
	})

	if assert.NoError(t, mw(h)(c)) {
		assert.Equal(t, requestBody, hw)
		assert.Equal(t, responseBody, hw)
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, hw, rec.Body.String())
	}

	// Must set default skipper
	BodyDumpWithConfig(BodyDumpConfig{
		Skipper: nil,
		Handler: func(c echo.Context, reqBody, resBody []byte) {
			requestBody = string(reqBody)
			responseBody = string(resBody)
		},
	})
}

func TestBodyDumpFails(t *testing.T) {
	e := echo.New()
	hw := "Hello, World!"
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(hw))
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	h := func(c echo.Context) error {
		return errors.New("some error")
	}

	mw := BodyDump(func(c echo.Context, reqBody, resBody []byte) {})

	if !assert.Error(t, mw(h)(c)) {
		t.FailNow()
	}

	assert.Panics(t, func() {
		mw = BodyDumpWithConfig(BodyDumpConfig{
			Skipper: nil,
			Handler: nil,
		})
	})

	assert.NotPanics(t, func() {
		mw = BodyDumpWithConfig(BodyDumpConfig{
			Skipper: func(c echo.Context) bool {
				return true
			},
			Handler: func(c echo.Context, reqBody, resBody []byte) {
			},
		})

		if !assert.Error(t, mw(h)(c)) {
			t.FailNow()
		}
	})
}

func TestBodyDumpResponseWriter_CanNotFlush(t *testing.T) {
	bdrw := bodyDumpResponseWriter{
		ResponseWriter: new(testResponseWriterNoFlushHijack), // this RW does not support flush
	}

	assert.PanicsWithError(t, "response writer flushing is not supported", func() {
		bdrw.Flush()
	})
}

func TestBodyDumpResponseWriter_CanFlush(t *testing.T) {
	trwu := testResponseWriterUnwrapperHijack{testResponseWriterUnwrapper: testResponseWriterUnwrapper{rw: httptest.NewRecorder()}}
	bdrw := bodyDumpResponseWriter{
		ResponseWriter: &trwu,
	}

	bdrw.Flush()
	assert.Equal(t, 1, trwu.unwrapCalled)
}

func TestBodyDumpResponseWriter_CanUnwrap(t *testing.T) {
	trwu := &testResponseWriterUnwrapper{rw: httptest.NewRecorder()}
	bdrw := bodyDumpResponseWriter{
		ResponseWriter: trwu,
	}

	result := bdrw.Unwrap()
	assert.Equal(t, trwu, result)
}

func TestBodyDumpResponseWriter_CanHijack(t *testing.T) {
	trwu := testResponseWriterUnwrapperHijack{testResponseWriterUnwrapper: testResponseWriterUnwrapper{rw: httptest.NewRecorder()}}
	bdrw := bodyDumpResponseWriter{
		ResponseWriter: &trwu, // this RW supports hijacking through unwrapping
	}

	_, _, err := bdrw.Hijack()
	assert.EqualError(t, err, "can hijack")
}

func TestBodyDumpResponseWriter_CanNotHijack(t *testing.T) {
	trwu := testResponseWriterUnwrapper{rw: httptest.NewRecorder()}
	bdrw := bodyDumpResponseWriter{
		ResponseWriter: &trwu, // this RW supports hijacking through unwrapping
	}

	_, _, err := bdrw.Hijack()
	assert.EqualError(t, err, "feature not supported")
}

func TestBodyDump_ReadError(t *testing.T) {
	e := echo.New()

	// Create a reader that fails during read
	failingReader := &failingReadCloser{
		data:     []byte("partial data"),
		failAt:   7, // Fail after 7 bytes
		failWith: errors.New("connection reset"),
	}

	req := httptest.NewRequest(http.MethodPost, "/", failingReader)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h := func(c echo.Context) error {
		// This handler should not be reached if body read fails
		body, _ := io.ReadAll(c.Request().Body)
		return c.String(http.StatusOK, string(body))
	}

	requestBodyReceived := ""
	mw := BodyDump(func(c echo.Context, reqBody, resBody []byte) {
		requestBodyReceived = string(reqBody)
	})

	err := mw(h)(c)

	// Verify error is propagated
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "connection reset")

	// Verify handler was not executed (callback wouldn't have received data)
	assert.Empty(t, requestBodyReceived)
}

// failingReadCloser is a helper type for testing read errors
type failingReadCloser struct {
	data     []byte
	pos      int
	failAt   int
	failWith error
}

func (f *failingReadCloser) Read(p []byte) (n int, err error) {
	if f.pos >= f.failAt {
		return 0, f.failWith
	}

	n = copy(p, f.data[f.pos:])
	f.pos += n

	if f.pos >= f.failAt {
		return n, f.failWith
	}

	return n, nil
}

func (f *failingReadCloser) Close() error {
	return nil
}
