// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: © 2015 LabStack LLC and Echo contributors

package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
)

func TestContextTimeoutSkipper(t *testing.T) {
	t.Parallel()
	m := ContextTimeoutWithConfig(ContextTimeoutConfig{
		Skipper: func(context echo.Context) bool {
			return true
		},
		Timeout: 10 * time.Millisecond,
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	e := echo.New()
	c := e.NewContext(req, rec)

	err := m(func(c echo.Context) error {
		if err := sleepWithContext(c.Request().Context(), time.Duration(20*time.Millisecond)); err != nil {
			return err
		}

		return errors.New("response from handler")
	})(c)

	// if not skipped we would have not returned error due context timeout logic
	assert.EqualError(t, err, "response from handler")
}

func TestContextTimeoutWithTimeout0(t *testing.T) {
	t.Parallel()
	assert.Panics(t, func() {
		ContextTimeout(time.Duration(0))
	})
}

func TestContextTimeoutErrorOutInHandler(t *testing.T) {
	t.Parallel()
	m := ContextTimeoutWithConfig(ContextTimeoutConfig{
		// Timeout has to be defined or the whole flow for timeout middleware will be skipped
		Timeout: 10 * time.Millisecond,
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	e := echo.New()
	c := e.NewContext(req, rec)

	rec.Code = 1 // we want to be sure that even 200 will not be sent
	err := m(func(c echo.Context) error {
		// this error must not be written to the client response. Middlewares upstream of timeout middleware must be able
		// to handle returned error and this can be done only then handler has not yet committed (written status code)
		// the response.
		return echo.NewHTTPError(http.StatusTeapot, "err")
	})(c)

	assert.Error(t, err)
	assert.EqualError(t, err, "code=418, message=err")
	assert.Equal(t, 1, rec.Code)
	assert.Equal(t, "", rec.Body.String())
}

func TestContextTimeoutSuccessfulRequest(t *testing.T) {
	t.Parallel()
	m := ContextTimeoutWithConfig(ContextTimeoutConfig{
		// Timeout has to be defined or the whole flow for timeout middleware will be skipped
		Timeout: 10 * time.Millisecond,
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	e := echo.New()
	c := e.NewContext(req, rec)

	err := m(func(c echo.Context) error {
		return c.JSON(http.StatusCreated, map[string]string{"data": "ok"})
	})(c)

	assert.NoError(t, err)
	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.Equal(t, "{\"data\":\"ok\"}\n", rec.Body.String())
}

func TestContextTimeoutTestRequestClone(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodPost, "/uri?query=value", strings.NewReader(url.Values{"form": {"value"}}.Encode()))
	req.AddCookie(&http.Cookie{Name: "cookie", Value: "value"})
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	m := ContextTimeoutWithConfig(ContextTimeoutConfig{
		// Timeout has to be defined or the whole flow for timeout middleware will be skipped
		Timeout: 1 * time.Second,
	})

	e := echo.New()
	c := e.NewContext(req, rec)

	err := m(func(c echo.Context) error {
		// Cookie test
		cookie, err := c.Request().Cookie("cookie")
		if assert.NoError(t, err) {
			assert.EqualValues(t, "cookie", cookie.Name)
			assert.EqualValues(t, "value", cookie.Value)
		}

		// Form values
		if assert.NoError(t, c.Request().ParseForm()) {
			assert.EqualValues(t, "value", c.Request().FormValue("form"))
		}

		// Query string
		assert.EqualValues(t, "value", c.Request().URL.Query()["query"][0])
		return nil
	})(c)

	assert.NoError(t, err)
}

func TestContextTimeoutWithDefaultErrorMessage(t *testing.T) {
	t.Parallel()

	timeout := 10 * time.Millisecond
	m := ContextTimeoutWithConfig(ContextTimeoutConfig{
		Timeout: timeout,
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	e := echo.New()
	c := e.NewContext(req, rec)

	err := m(func(c echo.Context) error {
		if err := sleepWithContext(c.Request().Context(), time.Duration(80*time.Millisecond)); err != nil {
			return err
		}
		return c.String(http.StatusOK, "Hello, World!")
	})(c)

	assert.IsType(t, &echo.HTTPError{}, err)
	assert.Error(t, err)
	assert.Equal(t, http.StatusServiceUnavailable, err.(*echo.HTTPError).Code)
	assert.Equal(t, "Service Unavailable", err.(*echo.HTTPError).Message)
}

func TestContextTimeoutCanHandleContextDeadlineOnNextHandler(t *testing.T) {
	t.Parallel()

	timeoutErrorHandler := func(err error, c echo.Context) error {
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				return &echo.HTTPError{
					Code:    http.StatusServiceUnavailable,
					Message: "Timeout! change me",
				}
			}
			return err
		}
		return nil
	}

	timeout := 50 * time.Millisecond
	m := ContextTimeoutWithConfig(ContextTimeoutConfig{
		Timeout:      timeout,
		ErrorHandler: timeoutErrorHandler,
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	e := echo.New()
	c := e.NewContext(req, rec)

	err := m(func(c echo.Context) error {
		// NOTE: Very short periods are not reliable for tests due to Go routine scheduling and the unpredictable order
		// for 1) request and 2) time goroutine. For most OS this works as expected, but MacOS seems most flaky.

		if err := sleepWithContext(c.Request().Context(), 100*time.Millisecond); err != nil {
			return err
		}

		// The Request Context should have a Deadline set by http.ContextTimeoutHandler
		if _, ok := c.Request().Context().Deadline(); !ok {
			assert.Fail(t, "No timeout set on Request Context")
		}
		return c.String(http.StatusOK, "Hello, World!")
	})(c)

	assert.IsType(t, &echo.HTTPError{}, err)
	assert.Error(t, err)
	assert.Equal(t, http.StatusServiceUnavailable, err.(*echo.HTTPError).Code)
	assert.Equal(t, "Timeout! change me", err.(*echo.HTTPError).Message)
}

func sleepWithContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)

	defer func() {
		_ = timer.Stop()
	}()

	select {
	case <-ctx.Done():
		return context.DeadlineExceeded
	case <-timer.C:
		return nil
	}
}
