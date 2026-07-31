// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: © 2015 LabStack LLC and Echo contributors

package middleware

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
)

func TestRequestLoggerOK(t *testing.T) {
	old := slog.Default()
	t.Cleanup(func() {
		slog.SetDefault(old)
	})

	buf := new(bytes.Buffer)
	slog.SetDefault(slog.New(slog.NewJSONHandler(buf, nil)))

	e := echo.New()
	e.Use(RequestLogger())

	e.POST("/test", func(c echo.Context) error {
		return c.String(http.StatusTeapot, "OK")
	})

	reader := strings.NewReader(`{"foo":"bar"}`)
	req := httptest.NewRequest(http.MethodPost, "/test", reader)
	req.Header.Set(echo.HeaderContentLength, strconv.Itoa(int(reader.Size())))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	req.Header.Set(echo.HeaderXRealIP, "8.8.8.8")
	req.Header.Set("User-Agent", "curl/7.68.0")

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	logAttrs := map[string]interface{}{}
	assert.NoError(t, json.Unmarshal(buf.Bytes(), &logAttrs))
	logAttrs["latency"] = 123
	logAttrs["time"] = "x"

	expect := map[string]interface{}{
		"level":      "INFO",
		"msg":        "REQUEST",
		"method":     "POST",
		"uri":        "/test",
		"status":     float64(418),
		"bytes_in":   "13",
		"host":       "example.com",
		"bytes_out":  float64(2),
		"user_agent": "curl/7.68.0",
		"remote_ip":  "8.8.8.8",
		"request_id": "",

		"time":    "x",
		"latency": 123,
	}
	assert.Equal(t, expect, logAttrs)
}

func TestRequestLoggerError(t *testing.T) {
	old := slog.Default()
	t.Cleanup(func() {
		slog.SetDefault(old)
	})

	buf := new(bytes.Buffer)
	slog.SetDefault(slog.New(slog.NewJSONHandler(buf, nil)))

	e := echo.New()
	e.Use(RequestLogger())

	e.GET("/test", func(c echo.Context) error {
		return errors.New("nope")
	})
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	logAttrs := map[string]interface{}{}
	assert.NoError(t, json.Unmarshal(buf.Bytes(), &logAttrs))
	logAttrs["latency"] = 123
	logAttrs["time"] = "x"

	expect := map[string]interface{}{
		"level":      "ERROR",
		"msg":        "REQUEST_ERROR",
		"method":     "GET",
		"uri":        "/test",
		"status":     float64(500),
		"bytes_in":   "",
		"host":       "example.com",
		"bytes_out":  float64(36.0),
		"user_agent": "",
		"remote_ip":  "192.0.2.1",
		"request_id": "",
		"error":      "nope",

		"latency": 123,
		"time":    "x",
	}
	assert.Equal(t, expect, logAttrs)
}

func TestRequestLoggerWithConfig(t *testing.T) {
	e := echo.New()

	var expect RequestLoggerValues
	e.Use(RequestLoggerWithConfig(RequestLoggerConfig{
		LogRoutePath: true,
		LogURI:       true,
		LogValuesFunc: func(c echo.Context, values RequestLoggerValues) error {
			expect = values
			return nil
		},
	}))

	e.GET("/test", func(c echo.Context) error {
		return c.String(http.StatusTeapot, "OK")
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusTeapot, rec.Code)
	assert.Equal(t, "/test", expect.RoutePath)
}

func TestRequestLoggerWithConfig_missingOnLogValuesPanics(t *testing.T) {
	assert.Panics(t, func() {
		RequestLoggerWithConfig(RequestLoggerConfig{
			LogValuesFunc: nil,
		})
	})
}

func TestRequestLogger_skipper(t *testing.T) {
	e := echo.New()

	loggerCalled := false
	e.Use(RequestLoggerWithConfig(RequestLoggerConfig{
		Skipper: func(c echo.Context) bool {
			return true
		},
		LogValuesFunc: func(c echo.Context, values RequestLoggerValues) error {
			loggerCalled = true
			return nil
		},
	}))

	e.GET("/test", func(c echo.Context) error {
		return c.String(http.StatusTeapot, "OK")
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusTeapot, rec.Code)
	assert.False(t, loggerCalled)
}

func TestRequestLogger_beforeNextFunc(t *testing.T) {
	e := echo.New()

	var myLoggerInstance int
	e.Use(RequestLoggerWithConfig(RequestLoggerConfig{
		BeforeNextFunc: func(c echo.Context) {
			c.Set("myLoggerInstance", 42)
		},
		LogValuesFunc: func(c echo.Context, values RequestLoggerValues) error {
			myLoggerInstance = c.Get("myLoggerInstance").(int)
			return nil
		},
	}))

	e.GET("/test", func(c echo.Context) error {
		return c.String(http.StatusTeapot, "OK")
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusTeapot, rec.Code)
	assert.Equal(t, 42, myLoggerInstance)
}

func TestRequestLogger_logError(t *testing.T) {
	e := echo.New()

	var actual RequestLoggerValues
	e.Use(RequestLoggerWithConfig(RequestLoggerConfig{
		LogError:  true,
		LogStatus: true,
		LogValuesFunc: func(c echo.Context, values RequestLoggerValues) error {
			actual = values
			return nil
		},
	}))

	e.GET("/test", func(c echo.Context) error {
		return echo.NewHTTPError(http.StatusNotAcceptable, "nope")
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotAcceptable, rec.Code)
	assert.Equal(t, http.StatusNotAcceptable, actual.Status)
	assert.EqualError(t, actual.Error, "code=406, message=nope")
}

func TestRequestLogger_HandleError(t *testing.T) {
	e := echo.New()

	var actual RequestLoggerValues
	e.Use(RequestLoggerWithConfig(RequestLoggerConfig{
		timeNow: func() time.Time {
			return time.Unix(1631045377, 0).UTC()
		},
		HandleError: true,
		LogError:    true,
		LogStatus:   true,
		LogValuesFunc: func(c echo.Context, values RequestLoggerValues) error {
			actual = values
			return nil
		},
	}))

	// to see if "HandleError" works we create custom error handler that uses its own status codes
	e.HTTPErrorHandler = func(err error, c echo.Context) {
		if c.Response().Committed {
			return
		}
		c.JSON(http.StatusTeapot, "custom error handler")
	}

	e.GET("/test", func(c echo.Context) error {
		return echo.NewHTTPError(http.StatusForbidden, "nope")
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusTeapot, rec.Code)

	expect := RequestLoggerValues{
		StartTime: time.Unix(1631045377, 0).UTC(),
		Status:    http.StatusTeapot,
		Error:     echo.NewHTTPError(http.StatusForbidden, "nope"),
	}
	assert.Equal(t, expect, actual)
}

func TestRequestLogger_LogValuesFuncError(t *testing.T) {
	e := echo.New()

	var expect RequestLoggerValues
	e.Use(RequestLoggerWithConfig(RequestLoggerConfig{
		LogError:  true,
		LogStatus: true,
		LogValuesFunc: func(c echo.Context, values RequestLoggerValues) error {
			expect = values
			return echo.NewHTTPError(http.StatusNotAcceptable, "LogValuesFuncError")
		},
	}))

	e.GET("/test", func(c echo.Context) error {
		return c.String(http.StatusTeapot, "OK")
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	// NOTE: when global error handler received error returned from middleware the status has already
	// been written to the client and response has been "committed" therefore global error handler does not do anything
	// and error that bubbled up in middleware chain will not be reflected in response code.
	assert.Equal(t, http.StatusTeapot, rec.Code)
	assert.Equal(t, http.StatusTeapot, expect.Status)
}

func TestRequestLogger_ID(t *testing.T) {
	var testCases = []struct {
		name            string
		whenFromRequest bool
		expect          string
	}{
		{
			name:            "ok, ID is provided from request headers",
			whenFromRequest: true,
			expect:          "123",
		},
		{
			name:            "ok, ID is from response headers",
			whenFromRequest: false,
			expect:          "321",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			e := echo.New()

			var expect RequestLoggerValues
			e.Use(RequestLoggerWithConfig(RequestLoggerConfig{
				LogRequestID: true,
				LogValuesFunc: func(c echo.Context, values RequestLoggerValues) error {
					expect = values
					return nil
				},
			}))

			e.GET("/test", func(c echo.Context) error {
				c.Response().Header().Set(echo.HeaderXRequestID, "321")
				return c.String(http.StatusTeapot, "OK")
			})

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			if tc.whenFromRequest {
				req.Header.Set(echo.HeaderXRequestID, "123")
			}
			rec := httptest.NewRecorder()

			e.ServeHTTP(rec, req)

			assert.Equal(t, http.StatusTeapot, rec.Code)
			assert.Equal(t, tc.expect, expect.RequestID)
		})
	}
}

func TestRequestLogger_headerIsCaseInsensitive(t *testing.T) {
	e := echo.New()

	var expect RequestLoggerValues
	mw := RequestLoggerWithConfig(RequestLoggerConfig{
		LogValuesFunc: func(c echo.Context, values RequestLoggerValues) error {
			expect = values
			return nil
		},
		LogHeaders: []string{"referer", "User-Agent"},
	})(func(c echo.Context) error {
		c.Request().Header.Set(echo.HeaderXRequestID, "123")
		c.FormValue("to force parse form")
		return c.String(http.StatusTeapot, "OK")
	})

	req := httptest.NewRequest(http.MethodGet, "/test?lang=en&checked=1&checked=2", nil)
	req.Header.Set("referer", "https://echo.labstack.com/")

	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := mw(c)

	assert.NoError(t, err)
	assert.Len(t, expect.Headers, 1)
	assert.Equal(t, []string{"https://echo.labstack.com/"}, expect.Headers["Referer"])
}

func TestRequestLogger_allFields(t *testing.T) {
	e := echo.New()

	isFirstNowCall := true
	var expect RequestLoggerValues
	mw := RequestLoggerWithConfig(RequestLoggerConfig{
		LogValuesFunc: func(c echo.Context, values RequestLoggerValues) error {
			expect = values
			return nil
		},
		LogLatency:       true,
		LogProtocol:      true,
		LogRemoteIP:      true,
		LogHost:          true,
		LogMethod:        true,
		LogURI:           true,
		LogURIPath:       true,
		LogRoutePath:     true,
		LogRequestID:     true,
		LogReferer:       true,
		LogUserAgent:     true,
		LogStatus:        true,
		LogError:         true,
		LogContentLength: true,
		LogResponseSize:  true,
		LogHeaders:       []string{"accept-encoding", "User-Agent"},
		LogQueryParams:   []string{"lang", "checked"},
		LogFormValues:    []string{"csrf", "multiple"},
		timeNow: func() time.Time {
			if isFirstNowCall {
				isFirstNowCall = false
				return time.Unix(1631045377, 0)
			}
			return time.Unix(1631045377+10, 0)
		},
	})(func(c echo.Context) error {
		c.Request().Header.Set(echo.HeaderXRequestID, "123")
		c.FormValue("to force parse form")
		return c.String(http.StatusTeapot, "OK")
	})

	f := make(url.Values)
	f.Set("csrf", "token")
	f.Set("multiple", "1")
	f.Add("multiple", "2")
	reader := strings.NewReader(f.Encode())
	req := httptest.NewRequest(http.MethodPost, "/test?lang=en&checked=1&checked=2", reader)
	req.Header.Set("Referer", "https://echo.labstack.com/")
	req.Header.Set("User-Agent", "curl/7.68.0")
	req.Header.Set(echo.HeaderContentLength, strconv.Itoa(int(reader.Size())))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
	req.Header.Set(echo.HeaderXRealIP, "8.8.8.8")

	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	c.SetPath("/test*")

	err := mw(c)

	assert.NoError(t, err)
	assert.Equal(t, time.Unix(1631045377, 0), expect.StartTime)
	assert.Equal(t, 10*time.Second, expect.Latency)
	assert.Equal(t, "HTTP/1.1", expect.Protocol)
	assert.Equal(t, "8.8.8.8", expect.RemoteIP)
	assert.Equal(t, "example.com", expect.Host)
	assert.Equal(t, http.MethodPost, expect.Method)
	assert.Equal(t, "/test?lang=en&checked=1&checked=2", expect.URI)
	assert.Equal(t, "/test", expect.URIPath)
	assert.Equal(t, "/test*", expect.RoutePath)
	assert.Equal(t, "123", expect.RequestID)
	assert.Equal(t, "https://echo.labstack.com/", expect.Referer)
	assert.Equal(t, "curl/7.68.0", expect.UserAgent)
	assert.Equal(t, 418, expect.Status)
	assert.Equal(t, nil, expect.Error)
	assert.Equal(t, "32", expect.ContentLength)
	assert.Equal(t, int64(2), expect.ResponseSize)

	assert.Len(t, expect.Headers, 1)
	assert.Equal(t, []string{"curl/7.68.0"}, expect.Headers["User-Agent"])

	assert.Len(t, expect.QueryParams, 2)
	assert.Equal(t, []string{"en"}, expect.QueryParams["lang"])
	assert.Equal(t, []string{"1", "2"}, expect.QueryParams["checked"])

	assert.Len(t, expect.FormValues, 2)
	assert.Equal(t, []string{"token"}, expect.FormValues["csrf"])
	assert.Equal(t, []string{"1", "2"}, expect.FormValues["multiple"])
}

func BenchmarkRequestLogger_withoutMapFields(b *testing.B) {
	e := echo.New()

	mw := RequestLoggerWithConfig(RequestLoggerConfig{
		Skipper: nil,
		LogValuesFunc: func(c echo.Context, values RequestLoggerValues) error {
			return nil
		},
		LogLatency:       true,
		LogProtocol:      true,
		LogRemoteIP:      true,
		LogHost:          true,
		LogMethod:        true,
		LogURI:           true,
		LogURIPath:       true,
		LogRoutePath:     true,
		LogRequestID:     true,
		LogReferer:       true,
		LogUserAgent:     true,
		LogStatus:        true,
		LogError:         true,
		LogContentLength: true,
		LogResponseSize:  true,
	})(func(c echo.Context) error {
		c.Request().Header.Set(echo.HeaderXRequestID, "123")
		return c.String(http.StatusTeapot, "OK")
	})

	req := httptest.NewRequest(http.MethodGet, "/test?lang=en", nil)
	req.Header.Set("Referer", "https://echo.labstack.com/")
	req.Header.Set("User-Agent", "curl/7.68.0")

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		mw(c)
	}
}

func BenchmarkRequestLogger_withMapFields(b *testing.B) {
	e := echo.New()

	mw := RequestLoggerWithConfig(RequestLoggerConfig{
		LogValuesFunc: func(c echo.Context, values RequestLoggerValues) error {
			return nil
		},
		LogLatency:       true,
		LogProtocol:      true,
		LogRemoteIP:      true,
		LogHost:          true,
		LogMethod:        true,
		LogURI:           true,
		LogURIPath:       true,
		LogRoutePath:     true,
		LogRequestID:     true,
		LogReferer:       true,
		LogUserAgent:     true,
		LogStatus:        true,
		LogError:         true,
		LogContentLength: true,
		LogResponseSize:  true,
		LogHeaders:       []string{"accept-encoding", "User-Agent"},
		LogQueryParams:   []string{"lang", "checked"},
		LogFormValues:    []string{"csrf", "multiple"},
	})(func(c echo.Context) error {
		c.Request().Header.Set(echo.HeaderXRequestID, "123")
		c.FormValue("to force parse form")
		return c.String(http.StatusTeapot, "OK")
	})

	f := make(url.Values)
	f.Set("csrf", "token")
	f.Add("multiple", "1")
	f.Add("multiple", "2")
	req := httptest.NewRequest(http.MethodPost, "/test?lang=en&checked=1&checked=2", strings.NewReader(f.Encode()))
	req.Header.Set("Referer", "https://echo.labstack.com/")
	req.Header.Set("User-Agent", "curl/7.68.0")
	req.Header.Add(echo.HeaderContentType, echo.MIMEApplicationForm)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		mw(c)
	}
}
