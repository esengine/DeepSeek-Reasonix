// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: © 2015 LabStack LLC and Echo contributors

package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
)

func TestRequestID(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	handler := func(c echo.Context) error {
		return c.String(http.StatusOK, "test")
	}

	rid := RequestIDWithConfig(RequestIDConfig{})
	h := rid(handler)
	h(c)
	assert.Len(t, rec.Header().Get(echo.HeaderXRequestID), 32)

	// Custom generator and handler
	customID := "customGenerator"
	calledHandler := false
	rid = RequestIDWithConfig(RequestIDConfig{
		Generator: func() string { return customID },
		RequestIDHandler: func(_ echo.Context, id string) {
			calledHandler = true
			assert.Equal(t, customID, id)
		},
	})
	h = rid(handler)
	h(c)
	assert.Equal(t, rec.Header().Get(echo.HeaderXRequestID), "customGenerator")
	assert.True(t, calledHandler)
}

func TestRequestID_IDNotAltered(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Add(echo.HeaderXRequestID, "<sample-request-id>")

	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	handler := func(c echo.Context) error {
		return c.String(http.StatusOK, "test")
	}

	rid := RequestIDWithConfig(RequestIDConfig{})
	h := rid(handler)
	_ = h(c)
	assert.Equal(t, rec.Header().Get(echo.HeaderXRequestID), "<sample-request-id>")
}

func TestRequestIDConfigDifferentHeader(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	handler := func(c echo.Context) error {
		return c.String(http.StatusOK, "test")
	}

	rid := RequestIDWithConfig(RequestIDConfig{TargetHeader: echo.HeaderXCorrelationID})
	h := rid(handler)
	h(c)
	assert.Len(t, rec.Header().Get(echo.HeaderXCorrelationID), 32)

	// Custom generator and handler
	customID := "customGenerator"
	calledHandler := false
	rid = RequestIDWithConfig(RequestIDConfig{
		Generator:    func() string { return customID },
		TargetHeader: echo.HeaderXCorrelationID,
		RequestIDHandler: func(_ echo.Context, id string) {
			calledHandler = true
			assert.Equal(t, customID, id)
		},
	})
	h = rid(handler)
	h(c)
	assert.Equal(t, rec.Header().Get(echo.HeaderXCorrelationID), "customGenerator")
	assert.True(t, calledHandler)
}
