// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: © 2015 LabStack LLC and Echo contributors

package middleware

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
)

func TestBodyLimit(t *testing.T) {
	e := echo.New()
	hw := []byte("Hello, World!")
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(hw))
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	h := func(c echo.Context) error {
		body, err := io.ReadAll(c.Request().Body)
		if err != nil {
			return err
		}
		return c.String(http.StatusOK, string(body))
	}

	// Based on content length (within limit)
	if assert.NoError(t, BodyLimit("2M")(h)(c)) {
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, hw, rec.Body.Bytes())
	}

	// Based on content length (overlimit)
	he := BodyLimit("2B")(h)(c).(*echo.HTTPError)
	assert.Equal(t, http.StatusRequestEntityTooLarge, he.Code)

	// Based on content read (within limit)
	req = httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(hw))
	req.ContentLength = -1
	rec = httptest.NewRecorder()
	c = e.NewContext(req, rec)
	if assert.NoError(t, BodyLimit("2M")(h)(c)) {
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "Hello, World!", rec.Body.String())
	}

	// Based on content read (overlimit)
	req = httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(hw))
	req.ContentLength = -1
	rec = httptest.NewRecorder()
	c = e.NewContext(req, rec)
	he = BodyLimit("2B")(h)(c).(*echo.HTTPError)
	assert.Equal(t, http.StatusRequestEntityTooLarge, he.Code)
}

func TestBodyLimitReader(t *testing.T) {
	hw := []byte("Hello, World!")

	config := BodyLimitConfig{
		Skipper: DefaultSkipper,
		Limit:   "2B",
		limit:   2,
	}
	reader := &limitedReader{
		BodyLimitConfig: config,
		reader:          io.NopCloser(bytes.NewReader(hw)),
	}

	// read all should return ErrStatusRequestEntityTooLarge
	_, err := io.ReadAll(reader)
	he := err.(*echo.HTTPError)
	assert.Equal(t, http.StatusRequestEntityTooLarge, he.Code)

	// reset reader and read two bytes must succeed
	bt := make([]byte, 2)
	reader.Reset(io.NopCloser(bytes.NewReader(hw)))
	n, err := reader.Read(bt)
	assert.Equal(t, 2, n)
	assert.Equal(t, nil, err)
}

func TestBodyLimitWithConfig_Skipper(t *testing.T) {
	e := echo.New()
	h := func(c echo.Context) error {
		body, err := io.ReadAll(c.Request().Body)
		if err != nil {
			return err
		}
		return c.String(http.StatusOK, string(body))
	}
	mw := BodyLimitWithConfig(BodyLimitConfig{
		Skipper: func(c echo.Context) bool {
			return true
		},
		Limit: "2B", // if not skipped this limit would make request to fail limit check
	})

	hw := []byte("Hello, World!")
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(hw))
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := mw(h)(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, hw, rec.Body.Bytes())
}

func TestBodyLimitWithConfig(t *testing.T) {
	var testCases = []struct {
		name        string
		givenLimit  string
		whenBody    []byte
		expectBody  []byte
		expectError string
	}{
		{
			name:        "ok, body is less than limit",
			givenLimit:  "10B",
			whenBody:    []byte("123456789"),
			expectBody:  []byte("123456789"),
			expectError: "",
		},
		{
			name:        "nok, body is more than limit",
			givenLimit:  "9B",
			whenBody:    []byte("1234567890"),
			expectBody:  []byte(nil),
			expectError: "code=413, message=Request Entity Too Large",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			e := echo.New()
			h := func(c echo.Context) error {
				body, err := io.ReadAll(c.Request().Body)
				if err != nil {
					return err
				}
				return c.String(http.StatusOK, string(body))
			}
			mw := BodyLimitWithConfig(BodyLimitConfig{
				Limit: tc.givenLimit,
			})

			req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(tc.whenBody))
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			err := mw(h)(c)
			if tc.expectError != "" {
				assert.EqualError(t, err, tc.expectError)
			} else {
				assert.NoError(t, err)
			}
			// not testing status as middlewares return error instead of committing it and OK cases are anyway 200
			assert.Equal(t, tc.expectBody, rec.Body.Bytes())
		})
	}
}

func TestBodyLimit_panicOnInvalidLimit(t *testing.T) {
	assert.PanicsWithError(
		t,
		"echo: invalid body-limit=",
		func() { BodyLimit("") },
	)
}
