// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: © 2015 LabStack LLC and Echo contributors

package middleware

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"
	"unsafe"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
)

func TestLoggerDefaultMW(t *testing.T) {
	var testCases = []struct {
		name           string
		whenHeader     map[string]string
		whenStatusCode int
		whenResponse   string
		whenError      error
		expect         string
	}{
		{
			name:           "ok, status 200",
			whenStatusCode: http.StatusOK,
			whenResponse:   "test",
			expect:         `{"time":"2020-04-28T01:26:40Z","id":"","remote_ip":"192.0.2.1","host":"example.com","method":"GET","uri":"/","user_agent":"","status":200,"error":"","latency":1,"latency_human":"1µs","bytes_in":0,"bytes_out":4}` + "\n",
		},
		{
			name:           "ok, status 300",
			whenStatusCode: http.StatusTemporaryRedirect,
			whenResponse:   "test",
			expect:         `{"time":"2020-04-28T01:26:40Z","id":"","remote_ip":"192.0.2.1","host":"example.com","method":"GET","uri":"/","user_agent":"","status":307,"error":"","latency":1,"latency_human":"1µs","bytes_in":0,"bytes_out":4}` + "\n",
		},
		{
			name:      "ok, handler error = status 500",
			whenError: errors.New("error"),
			expect:    `{"time":"2020-04-28T01:26:40Z","id":"","remote_ip":"192.0.2.1","host":"example.com","method":"GET","uri":"/","user_agent":"","status":500,"error":"error","latency":1,"latency_human":"1µs","bytes_in":0,"bytes_out":36}` + "\n",
		},
		{
			name:      "error with invalid UTF-8 sequences",
			whenError: errors.New("invalid data: \xFF\xFE"),
			expect:    `{"time":"2020-04-28T01:26:40Z","id":"","remote_ip":"192.0.2.1","host":"example.com","method":"GET","uri":"/","user_agent":"","status":500,"error":"invalid data: \ufffd\ufffd","latency":1,"latency_human":"1µs","bytes_in":0,"bytes_out":36}` + "\n",
		},
		{
			name:      "error with JSON special characters (quotes and backslashes)",
			whenError: errors.New(`error with "quotes" and \backslash`),
			expect:    `{"time":"2020-04-28T01:26:40Z","id":"","remote_ip":"192.0.2.1","host":"example.com","method":"GET","uri":"/","user_agent":"","status":500,"error":"error with \"quotes\" and \\backslash","latency":1,"latency_human":"1µs","bytes_in":0,"bytes_out":36}` + "\n",
		},
		{
			name:      "error with control characters (newlines and tabs)",
			whenError: errors.New("error\nwith\nnewlines\tand\ttabs"),
			expect:    `{"time":"2020-04-28T01:26:40Z","id":"","remote_ip":"192.0.2.1","host":"example.com","method":"GET","uri":"/","user_agent":"","status":500,"error":"error\nwith\nnewlines\tand\ttabs","latency":1,"latency_human":"1µs","bytes_in":0,"bytes_out":36}` + "\n",
		},
		{
			name:           "ok, remote_ip from X-Real-Ip header",
			whenHeader:     map[string]string{echo.HeaderXRealIP: "127.0.0.1"},
			whenStatusCode: http.StatusOK,
			whenResponse:   "test",
			expect:         `{"time":"2020-04-28T01:26:40Z","id":"","remote_ip":"127.0.0.1","host":"example.com","method":"GET","uri":"/","user_agent":"","status":200,"error":"","latency":1,"latency_human":"1µs","bytes_in":0,"bytes_out":4}` + "\n",
		},
		{
			name:           "ok, remote_ip from X-Forwarded-For header",
			whenHeader:     map[string]string{echo.HeaderXForwardedFor: "127.0.0.1"},
			whenStatusCode: http.StatusOK,
			whenResponse:   "test",
			expect:         `{"time":"2020-04-28T01:26:40Z","id":"","remote_ip":"127.0.0.1","host":"example.com","method":"GET","uri":"/","user_agent":"","status":200,"error":"","latency":1,"latency_human":"1µs","bytes_in":0,"bytes_out":4}` + "\n",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			e := echo.New()
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if len(tc.whenHeader) > 0 {
				for k, v := range tc.whenHeader {
					req.Header.Add(k, v)
				}
			}

			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			DefaultLoggerConfig.timeNow = func() time.Time { return time.Unix(1588037200, 0).UTC() }
			h := Logger()(func(c echo.Context) error {
				if tc.whenError != nil {
					return tc.whenError
				}
				return c.String(tc.whenStatusCode, tc.whenResponse)
			})
			buf := new(bytes.Buffer)
			e.Logger.SetOutput(buf)

			err := h(c)
			assert.NoError(t, err)

			result := buf.String()
			// handle everchanging latency numbers
			result = regexp.MustCompile(`"latency":\d+,`).ReplaceAllString(result, `"latency":1,`)
			result = regexp.MustCompile(`"latency_human":"[^"]+"`).ReplaceAllString(result, `"latency_human":"1µs"`)

			assert.Equal(t, tc.expect, result)
		})
	}
}

func TestLoggerWithLoggerConfig(t *testing.T) {
	// to handle everchanging latency numbers
	jsonLatency := map[string]*regexp.Regexp{
		`"latency":1,`:          regexp.MustCompile(`"latency":\d+,`),
		`"latency_human":"1µs"`: regexp.MustCompile(`"latency_human":"[^"]+"`),
	}

	form := make(url.Values)
	form.Set("csrf", "token")
	form.Add("multiple", "1")
	form.Add("multiple", "2")

	var testCases = []struct {
		name           string
		givenConfig    LoggerConfig
		whenURI        string
		whenMethod     string
		whenHost       string
		whenPath       string
		whenRoute      string
		whenProto      string
		whenRequestURI string
		whenHeader     map[string]string
		whenFormValues url.Values
		whenStatusCode int
		whenResponse   string
		whenError      error
		whenReplacers  map[string]*regexp.Regexp
		expect         string
	}{
		{
			name: "ok, skipper",
			givenConfig: LoggerConfig{
				Skipper: func(c echo.Context) bool { return true },
			},
			expect: ``,
		},
		{ // this is an example how format that does not seem to be JSON is not currently escaped
			name:        "ok, NON json string is not escaped: method",
			givenConfig: LoggerConfig{Format: `method:"${method}"`},
			whenMethod:  `","method":":D"`,
			expect:      `method:"","method":":D""`,
		},
		{
			name:        "ok, json string escape: method",
			givenConfig: LoggerConfig{Format: `{"method":"${method}"}`},
			whenMethod:  `","method":":D"`,
			expect:      `{"method":"\",\"method\":\":D\""}`,
		},
		{
			name:        "ok, json string escape: id",
			givenConfig: LoggerConfig{Format: `{"id":"${id}"}`},
			whenHeader:  map[string]string{echo.HeaderXRequestID: `\"127.0.0.1\"`},
			expect:      `{"id":"\\\"127.0.0.1\\\""}`,
		},
		{
			name:        "ok, json string escape: remote_ip",
			givenConfig: LoggerConfig{Format: `{"remote_ip":"${remote_ip}"}`},
			whenHeader:  map[string]string{echo.HeaderXForwardedFor: `\"127.0.0.1\"`},
			expect:      `{"remote_ip":"\\\"127.0.0.1\\\""}`,
		},
		{
			name:        "ok, json string escape: host",
			givenConfig: LoggerConfig{Format: `{"host":"${host}"}`},
			whenHost:    `\"127.0.0.1\"`,
			expect:      `{"host":"\\\"127.0.0.1\\\""}`,
		},
		{
			name:        "ok, json string escape: path",
			givenConfig: LoggerConfig{Format: `{"path":"${path}"}`},
			whenPath:    `\","` + "\n",
			expect:      `{"path":"\\\",\"\n"}`,
		},
		{
			name:        "ok, json string escape: route",
			givenConfig: LoggerConfig{Format: `{"route":"${route}"}`},
			whenRoute:   `\","` + "\n",
			expect:      `{"route":"\\\",\"\n"}`,
		},
		{
			name:        "ok, json string escape: proto",
			givenConfig: LoggerConfig{Format: `{"protocol":"${protocol}"}`},
			whenProto:   `\","` + "\n",
			expect:      `{"protocol":"\\\",\"\n"}`,
		},
		{
			name:        "ok, json string escape: referer",
			givenConfig: LoggerConfig{Format: `{"referer":"${referer}"}`},
			whenHeader:  map[string]string{"Referer": `\","` + "\n"},
			expect:      `{"referer":"\\\",\"\n"}`,
		},
		{
			name:        "ok, json string escape: user_agent",
			givenConfig: LoggerConfig{Format: `{"user_agent":"${user_agent}"}`},
			whenHeader:  map[string]string{"User-Agent": `\","` + "\n"},
			expect:      `{"user_agent":"\\\",\"\n"}`,
		},
		{
			name:        "ok, json string escape: bytes_in",
			givenConfig: LoggerConfig{Format: `{"bytes_in":"${bytes_in}"}`},
			whenHeader:  map[string]string{echo.HeaderContentLength: `\","` + "\n"},
			expect:      `{"bytes_in":"\\\",\"\n"}`,
		},
		{
			name:        "ok, json string escape: query param",
			givenConfig: LoggerConfig{Format: `{"query":"${query:test}"}`},
			whenURI:     `/?test=1","`,
			expect:      `{"query":"1\",\""}`,
		},
		{
			name:        "ok, json string escape: header",
			givenConfig: LoggerConfig{Format: `{"header":"${header:referer}"}`},
			whenHeader:  map[string]string{"referer": `\","` + "\n"},
			expect:      `{"header":"\\\",\"\n"}`,
		},
		{
			name:           "ok, json string escape: form",
			givenConfig:    LoggerConfig{Format: `{"csrf":"${form:csrf}"}`},
			whenMethod:     http.MethodPost,
			whenFormValues: url.Values{"csrf": {`token","`}},
			expect:         `{"csrf":"token\",\""}`,
		},
		{
			name: "nok, json string escape: cookie - will not accept invalid chars",
			// net/cookie.go: validCookieValueByte function allows these byte in cookie value
			// only `0x20 <= b && b < 0x7f && b != '"' && b != ';' && b != '\\'`
			givenConfig: LoggerConfig{Format: `{"cookie":"${cookie:session}"}`},
			whenHeader:  map[string]string{"Cookie": `_ga=GA1.2.000000000.0000000000; session=test\n`},
			expect:      `{"cookie":""}`,
		},
		{
			name:           "ok, format time_unix",
			givenConfig:    LoggerConfig{Format: `${time_unix}`},
			whenStatusCode: http.StatusOK,
			whenResponse:   "test",
			expect:         `1588037200`,
		},
		{
			name:           "ok, format time_unix_milli",
			givenConfig:    LoggerConfig{Format: `${time_unix_milli}`},
			whenStatusCode: http.StatusOK,
			whenResponse:   "test",
			expect:         `1588037200000`,
		},
		{
			name:           "ok, format time_unix_micro",
			givenConfig:    LoggerConfig{Format: `${time_unix_micro}`},
			whenStatusCode: http.StatusOK,
			whenResponse:   "test",
			expect:         `1588037200000000`,
		},
		{
			name:           "ok, format time_unix_nano",
			givenConfig:    LoggerConfig{Format: `${time_unix_nano}`},
			whenStatusCode: http.StatusOK,
			whenResponse:   "test",
			expect:         `1588037200000000000`,
		},
		{
			name:           "ok, format time_rfc3339",
			givenConfig:    LoggerConfig{Format: `${time_rfc3339}`},
			whenStatusCode: http.StatusOK,
			whenResponse:   "test",
			expect:         `2020-04-28T01:26:40Z`,
		},
		{
			name:           "ok, status 200",
			whenStatusCode: http.StatusOK,
			whenResponse:   "test",
			whenReplacers:  jsonLatency,
			expect:         `{"time":"2020-04-28T01:26:40Z","id":"","remote_ip":"192.0.2.1","host":"example.com","method":"GET","uri":"/","user_agent":"","status":200,"error":"","latency":1,"latency_human":"1µs","bytes_in":0,"bytes_out":4}` + "\n",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			e := echo.New()

			req := httptest.NewRequest(http.MethodGet, cmp.Or(tc.whenURI, "/"), nil)
			if tc.whenFormValues != nil {
				req = httptest.NewRequest(http.MethodGet, cmp.Or(tc.whenURI, "/"), strings.NewReader(tc.whenFormValues.Encode()))
				req.Header.Add(echo.HeaderContentType, echo.MIMEApplicationForm)
			}

			for k, v := range tc.whenHeader {
				req.Header.Add(k, v)
			}
			if tc.whenHost != "" {
				req.Host = tc.whenHost
			}
			if tc.whenMethod != "" {
				req.Method = tc.whenMethod
			}
			if tc.whenProto != "" {
				req.Proto = tc.whenProto
			}
			if tc.whenRequestURI != "" {
				req.RequestURI = tc.whenRequestURI
			}
			if tc.whenPath != "" {
				req.URL.Path = tc.whenPath
			}
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			if tc.whenFormValues != nil {
				c.FormValue("to trigger form parsing")
			}
			if tc.whenRoute != "" {
				c.SetPath(tc.whenRoute)
			}

			config := tc.givenConfig
			if config.timeNow == nil {
				config.timeNow = func() time.Time { return time.Unix(1588037200, 0).UTC() }
			}
			buf := new(bytes.Buffer)
			if config.Output == nil {
				e.Logger.SetOutput(buf)
			}

			h := LoggerWithConfig(config)(func(c echo.Context) error {
				if tc.whenError != nil {
					return tc.whenError
				}
				return c.String(cmp.Or(tc.whenStatusCode, http.StatusOK), cmp.Or(tc.whenResponse, "test"))
			})

			err := h(c)
			assert.NoError(t, err)

			result := buf.String()

			for replaceTo, replacer := range tc.whenReplacers {
				result = replacer.ReplaceAllString(result, replaceTo)
			}

			assert.Equal(t, tc.expect, result)
		})
	}
}

func TestLoggerTemplate(t *testing.T) {
	buf := new(bytes.Buffer)

	e := echo.New()
	e.Use(LoggerWithConfig(LoggerConfig{
		Format: `{"time":"${time_rfc3339_nano}","id":"${id}","remote_ip":"${remote_ip}","host":"${host}","user_agent":"${user_agent}",` +
			`"method":"${method}","uri":"${uri}","status":${status}, "latency":${latency},` +
			`"latency_human":"${latency_human}","bytes_in":${bytes_in}, "path":"${path}", "route":"${route}", "referer":"${referer}",` +
			`"bytes_out":${bytes_out},"ch":"${header:X-Custom-Header}", "protocol":"${protocol}"` +
			`"us":"${query:username}", "cf":"${form:username}", "session":"${cookie:session}"}` + "\n",
		Output: buf,
	}))

	e.GET("/users/:id", func(c echo.Context) error {
		return c.String(http.StatusOK, "Header Logged")
	})

	req := httptest.NewRequest(http.MethodGet, "/users/1?username=apagano-param&password=secret", nil)
	req.RequestURI = "/"
	req.Header.Add(echo.HeaderXRealIP, "127.0.0.1")
	req.Header.Add("Referer", "google.com")
	req.Header.Add("User-Agent", "echo-tests-agent")
	req.Header.Add("X-Custom-Header", "AAA-CUSTOM-VALUE")
	req.Header.Add("X-Request-ID", "6ba7b810-9dad-11d1-80b4-00c04fd430c8")
	req.Header.Add("Cookie", "_ga=GA1.2.000000000.0000000000; session=ac08034cd216a647fc2eb62f2bcf7b810")
	req.Form = url.Values{
		"username": []string{"apagano-form"},
		"password": []string{"secret-form"},
	}

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	cases := map[string]bool{
		"apagano-param":                        true,
		"apagano-form":                         true,
		"AAA-CUSTOM-VALUE":                     true,
		"BBB-CUSTOM-VALUE":                     false,
		"secret-form":                          false,
		"hexvalue":                             false,
		"GET":                                  true,
		"127.0.0.1":                            true,
		"\"path\":\"/users/1\"":                true,
		"\"route\":\"/users/:id\"":             true,
		"\"uri\":\"/\"":                        true,
		"\"status\":200":                       true,
		"\"bytes_in\":0":                       true,
		"google.com":                           true,
		"echo-tests-agent":                     true,
		"6ba7b810-9dad-11d1-80b4-00c04fd430c8": true,
		"ac08034cd216a647fc2eb62f2bcf7b810":    true,
	}

	for token, present := range cases {
		assert.True(t, strings.Contains(buf.String(), token) == present, "Case: "+token)
	}
}

func TestLoggerCustomTimestamp(t *testing.T) {
	buf := new(bytes.Buffer)
	customTimeFormat := "2006-01-02 15:04:05.00000"
	e := echo.New()
	e.Use(LoggerWithConfig(LoggerConfig{
		Format: `{"time":"${time_custom}","id":"${id}","remote_ip":"${remote_ip}","host":"${host}","user_agent":"${user_agent}",` +
			`"method":"${method}","uri":"${uri}","status":${status}, "latency":${latency},` +
			`"latency_human":"${latency_human}","bytes_in":${bytes_in}, "path":"${path}", "referer":"${referer}",` +
			`"bytes_out":${bytes_out},"ch":"${header:X-Custom-Header}",` +
			`"us":"${query:username}", "cf":"${form:username}", "session":"${cookie:session}"}` + "\n",
		CustomTimeFormat: customTimeFormat,
		Output:           buf,
	}))

	e.GET("/", func(c echo.Context) error {
		return c.String(http.StatusOK, "custom time stamp test")
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	var objs map[string]*json.RawMessage
	if err := json.Unmarshal(buf.Bytes(), &objs); err != nil {
		panic(err)
	}
	loggedTime := *(*string)(unsafe.Pointer(objs["time"]))
	_, err := time.Parse(customTimeFormat, loggedTime)
	assert.Error(t, err)
}

func TestLoggerCustomTagFunc(t *testing.T) {
	e := echo.New()
	buf := new(bytes.Buffer)
	e.Use(LoggerWithConfig(LoggerConfig{
		Format: `{"method":"${method}",${custom}}` + "\n",
		CustomTagFunc: func(c echo.Context, buf *bytes.Buffer) (int, error) {
			return buf.WriteString(`"tag":"my-value"`)
		},
		Output: buf,
	}))

	e.GET("/", func(c echo.Context) error {
		return c.String(http.StatusOK, "custom time stamp test")
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	assert.Equal(t, `{"method":"GET","tag":"my-value"}`+"\n", buf.String())
}

func BenchmarkLoggerWithConfig_withoutMapFields(b *testing.B) {
	e := echo.New()

	buf := new(bytes.Buffer)
	mw := LoggerWithConfig(LoggerConfig{
		Format: `{"time":"${time_rfc3339_nano}","id":"${id}","remote_ip":"${remote_ip}","host":"${host}","user_agent":"${user_agent}",` +
			`"method":"${method}","uri":"${uri}","status":${status}, "latency":${latency},` +
			`"latency_human":"${latency_human}","bytes_in":${bytes_in}, "path":"${path}", "referer":"${referer}",` +
			`"bytes_out":${bytes_out}, "protocol":"${protocol}"}` + "\n",
		Output: buf,
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
		buf.Reset()
	}
}

func BenchmarkLoggerWithConfig_withMapFields(b *testing.B) {
	e := echo.New()

	buf := new(bytes.Buffer)
	mw := LoggerWithConfig(LoggerConfig{
		Format: `{"time":"${time_rfc3339_nano}","id":"${id}","remote_ip":"${remote_ip}","host":"${host}","user_agent":"${user_agent}",` +
			`"method":"${method}","uri":"${uri}","status":${status}, "latency":${latency},` +
			`"latency_human":"${latency_human}","bytes_in":${bytes_in}, "path":"${path}", "referer":"${referer}",` +
			`"bytes_out":${bytes_out},"ch":"${header:X-Custom-Header}", "protocol":"${protocol}"` +
			`"us":"${query:username}", "cf":"${form:csrf}", "Referer2":"${header:Referer}"}` + "\n",
		Output: buf,
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
		buf.Reset()
	}
}
