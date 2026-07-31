// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: © 2015 LabStack LLC and Echo contributors

package echo

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"text/template"
	"time"

	"github.com/labstack/gommon/log"
	"github.com/stretchr/testify/assert"
)

type Template struct {
	templates *template.Template
}

var testUser = user{1, "Jon Snow"}

func BenchmarkAllocJSONP(b *testing.B) {
	e := New()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(userJSON))
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec).(*context)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		c.JSONP(http.StatusOK, "callback", testUser)
	}
}

func BenchmarkAllocJSON(b *testing.B) {
	e := New()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(userJSON))
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec).(*context)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		c.JSON(http.StatusOK, testUser)
	}
}

func BenchmarkAllocXML(b *testing.B) {
	e := New()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(userJSON))
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec).(*context)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		c.XML(http.StatusOK, testUser)
	}
}

func BenchmarkRealIPForHeaderXForwardFor(b *testing.B) {
	c := context{request: &http.Request{
		Header: http.Header{HeaderXForwardedFor: []string{"127.0.0.1, 127.0.1.1, "}},
	}}
	for i := 0; i < b.N; i++ {
		c.RealIP()
	}
}

func (t *Template) Render(w io.Writer, name string, data interface{}, c Context) error {
	return t.templates.ExecuteTemplate(w, name, data)
}

func TestContextEcho(t *testing.T) {
	e := New()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(userJSON))
	rec := httptest.NewRecorder()

	c := e.NewContext(req, rec).(*context)

	assert.Equal(t, e, c.Echo())
}

func TestContextRequest(t *testing.T) {
	e := New()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(userJSON))
	rec := httptest.NewRecorder()

	c := e.NewContext(req, rec).(*context)

	assert.NotNil(t, c.Request())
	assert.Equal(t, req, c.Request())
}

func TestContextResponse(t *testing.T) {
	e := New()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(userJSON))
	rec := httptest.NewRecorder()

	c := e.NewContext(req, rec).(*context)

	assert.NotNil(t, c.Response())
}

func TestContextRenderTemplate(t *testing.T) {
	e := New()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(userJSON))
	rec := httptest.NewRecorder()

	c := e.NewContext(req, rec).(*context)

	tmpl := &Template{
		templates: template.Must(template.New("hello").Parse("Hello, {{.}}!")),
	}
	c.echo.Renderer = tmpl
	err := c.Render(http.StatusOK, "hello", "Jon Snow")
	if assert.NoError(t, err) {
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "Hello, Jon Snow!", rec.Body.String())
	}
}

func TestContextRenderErrorsOnNoRenderer(t *testing.T) {
	e := New()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(userJSON))
	rec := httptest.NewRecorder()

	c := e.NewContext(req, rec).(*context)

	c.echo.Renderer = nil
	assert.Error(t, c.Render(http.StatusOK, "hello", "Jon Snow"))
}

func TestContextJSON(t *testing.T) {
	e := New()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(userJSON))
	c := e.NewContext(req, rec).(*context)

	err := c.JSON(http.StatusOK, user{1, "Jon Snow"})
	if assert.NoError(t, err) {
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, MIMEApplicationJSON, rec.Header().Get(HeaderContentType))
		assert.Equal(t, userJSON+"\n", rec.Body.String())
	}
}

func TestContextJSONErrorsOut(t *testing.T) {
	e := New()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(userJSON))
	c := e.NewContext(req, rec).(*context)

	err := c.JSON(http.StatusOK, make(chan bool))
	assert.EqualError(t, err, "json: unsupported type: chan bool")
}

func TestContextJSONPrettyURL(t *testing.T) {
	e := New()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/?pretty", nil)
	c := e.NewContext(req, rec).(*context)

	err := c.JSON(http.StatusOK, user{1, "Jon Snow"})
	if assert.NoError(t, err) {
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, MIMEApplicationJSON, rec.Header().Get(HeaderContentType))
		assert.Equal(t, userJSONPretty+"\n", rec.Body.String())
	}
}

func TestContextJSONPretty(t *testing.T) {
	e := New()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	c := e.NewContext(req, rec).(*context)

	err := c.JSONPretty(http.StatusOK, user{1, "Jon Snow"}, "  ")
	if assert.NoError(t, err) {
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, MIMEApplicationJSON, rec.Header().Get(HeaderContentType))
		assert.Equal(t, userJSONPretty+"\n", rec.Body.String())
	}
}

func TestContextJSONWithEmptyIntent(t *testing.T) {
	e := New()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	c := e.NewContext(req, rec).(*context)

	u := user{1, "Jon Snow"}
	emptyIndent := ""
	buf := new(bytes.Buffer)

	enc := json.NewEncoder(buf)
	enc.SetIndent(emptyIndent, emptyIndent)
	_ = enc.Encode(u)
	err := c.json(http.StatusOK, user{1, "Jon Snow"}, emptyIndent)
	if assert.NoError(t, err) {
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, MIMEApplicationJSON, rec.Header().Get(HeaderContentType))
		assert.Equal(t, buf.String(), rec.Body.String())
	}
}

func TestContextJSONP(t *testing.T) {
	e := New()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	c := e.NewContext(req, rec).(*context)

	callback := "callback"
	err := c.JSONP(http.StatusOK, callback, user{1, "Jon Snow"})
	if assert.NoError(t, err) {
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, MIMEApplicationJavaScriptCharsetUTF8, rec.Header().Get(HeaderContentType))
		assert.Equal(t, callback+"("+userJSON+"\n);", rec.Body.String())
	}
}

func TestContextJSONBlob(t *testing.T) {
	e := New()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	c := e.NewContext(req, rec).(*context)

	data, err := json.Marshal(user{1, "Jon Snow"})
	assert.NoError(t, err)
	err = c.JSONBlob(http.StatusOK, data)
	if assert.NoError(t, err) {
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, MIMEApplicationJSON, rec.Header().Get(HeaderContentType))
		assert.Equal(t, userJSON, rec.Body.String())
	}
}

func TestContextJSONPBlob(t *testing.T) {
	e := New()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	c := e.NewContext(req, rec).(*context)

	callback := "callback"
	data, err := json.Marshal(user{1, "Jon Snow"})
	assert.NoError(t, err)
	err = c.JSONPBlob(http.StatusOK, callback, data)
	if assert.NoError(t, err) {
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, MIMEApplicationJavaScriptCharsetUTF8, rec.Header().Get(HeaderContentType))
		assert.Equal(t, callback+"("+userJSON+");", rec.Body.String())
	}
}

func TestContextXML(t *testing.T) {
	e := New()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	c := e.NewContext(req, rec).(*context)

	err := c.XML(http.StatusOK, user{1, "Jon Snow"})
	if assert.NoError(t, err) {
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, MIMEApplicationXMLCharsetUTF8, rec.Header().Get(HeaderContentType))
		assert.Equal(t, xml.Header+userXML, rec.Body.String())
	}
}

func TestContextXMLPrettyURL(t *testing.T) {
	e := New()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/?pretty", nil)
	c := e.NewContext(req, rec).(*context)

	err := c.XML(http.StatusOK, user{1, "Jon Snow"})
	if assert.NoError(t, err) {
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, MIMEApplicationXMLCharsetUTF8, rec.Header().Get(HeaderContentType))
		assert.Equal(t, xml.Header+userXMLPretty, rec.Body.String())
	}
}

func TestContextXMLPretty(t *testing.T) {
	e := New()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	c := e.NewContext(req, rec).(*context)

	err := c.XMLPretty(http.StatusOK, user{1, "Jon Snow"}, "  ")
	if assert.NoError(t, err) {
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, MIMEApplicationXMLCharsetUTF8, rec.Header().Get(HeaderContentType))
		assert.Equal(t, xml.Header+userXMLPretty, rec.Body.String())
	}
}

func TestContextXMLBlob(t *testing.T) {
	e := New()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	c := e.NewContext(req, rec).(*context)

	data, err := xml.Marshal(user{1, "Jon Snow"})
	assert.NoError(t, err)
	err = c.XMLBlob(http.StatusOK, data)
	if assert.NoError(t, err) {
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, MIMEApplicationXMLCharsetUTF8, rec.Header().Get(HeaderContentType))
		assert.Equal(t, xml.Header+userXML, rec.Body.String())
	}
}

func TestContextXMLWithEmptyIntent(t *testing.T) {
	e := New()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	c := e.NewContext(req, rec).(*context)

	u := user{1, "Jon Snow"}
	emptyIndent := ""
	buf := new(bytes.Buffer)

	enc := xml.NewEncoder(buf)
	enc.Indent(emptyIndent, emptyIndent)
	_ = enc.Encode(u)
	err := c.xml(http.StatusOK, user{1, "Jon Snow"}, emptyIndent)
	if assert.NoError(t, err) {
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, MIMEApplicationXMLCharsetUTF8, rec.Header().Get(HeaderContentType))
		assert.Equal(t, xml.Header+buf.String(), rec.Body.String())
	}
}

type responseWriterErr struct {
}

func (responseWriterErr) Header() http.Header {
	return http.Header{}
}

func (responseWriterErr) Write([]byte) (int, error) {
	return 0, errors.New("responseWriterErr")
}

func (responseWriterErr) WriteHeader(statusCode int) {
}

func TestContextXMLError(t *testing.T) {
	e := New()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/?pretty", nil)
	c := e.NewContext(req, rec).(*context)
	c.response.Writer = responseWriterErr{}

	err := c.XML(http.StatusOK, make(chan bool))
	assert.EqualError(t, err, "responseWriterErr")
}

func TestContextString(t *testing.T) {
	e := New()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/?pretty", nil)
	c := e.NewContext(req, rec).(*context)

	err := c.String(http.StatusOK, "Hello, World!")
	if assert.NoError(t, err) {
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, MIMETextPlainCharsetUTF8, rec.Header().Get(HeaderContentType))
		assert.Equal(t, "Hello, World!", rec.Body.String())
	}
}

func TestContextHTML(t *testing.T) {
	e := New()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/?pretty", nil)
	c := e.NewContext(req, rec).(*context)

	err := c.HTML(http.StatusOK, "Hello, <strong>World!</strong>")
	if assert.NoError(t, err) {
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, MIMETextHTMLCharsetUTF8, rec.Header().Get(HeaderContentType))
		assert.Equal(t, "Hello, <strong>World!</strong>", rec.Body.String())
	}
}

func TestContextStream(t *testing.T) {
	e := New()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/?pretty", nil)
	c := e.NewContext(req, rec).(*context)

	r := strings.NewReader("response from a stream")
	err := c.Stream(http.StatusOK, "application/octet-stream", r)
	if assert.NoError(t, err) {
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "application/octet-stream", rec.Header().Get(HeaderContentType))
		assert.Equal(t, "response from a stream", rec.Body.String())
	}
}

func TestContextAttachment(t *testing.T) {
	var testCases = []struct {
		name         string
		whenName     string
		expectHeader string
	}{
		{
			name:         "ok",
			whenName:     "walle.png",
			expectHeader: `attachment; filename="walle.png"`,
		},
		{
			name:         "ok, escape quotes in malicious filename",
			whenName:     `malicious.sh"; \"; dummy=.txt`,
			expectHeader: `attachment; filename="malicious.sh\"; \\\"; dummy=.txt"`,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			e := New()
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			c := e.NewContext(req, rec).(*context)

			err := c.Attachment("_fixture/images/walle.png", tc.whenName)
			if assert.NoError(t, err) {
				assert.Equal(t, tc.expectHeader, rec.Header().Get(HeaderContentDisposition))

				assert.Equal(t, http.StatusOK, rec.Code)
				assert.Equal(t, 219885, rec.Body.Len())
			}
		})
	}
}

func TestContextInline(t *testing.T) {
	var testCases = []struct {
		name         string
		whenName     string
		expectHeader string
	}{
		{
			name:         "ok",
			whenName:     "walle.png",
			expectHeader: `inline; filename="walle.png"`,
		},
		{
			name:         "ok, escape quotes in malicious filename",
			whenName:     `malicious.sh"; \"; dummy=.txt`,
			expectHeader: `inline; filename="malicious.sh\"; \\\"; dummy=.txt"`,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			e := New()
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			c := e.NewContext(req, rec).(*context)

			err := c.Inline("_fixture/images/walle.png", tc.whenName)
			if assert.NoError(t, err) {
				assert.Equal(t, tc.expectHeader, rec.Header().Get(HeaderContentDisposition))

				assert.Equal(t, http.StatusOK, rec.Code)
				assert.Equal(t, 219885, rec.Body.Len())
			}
		})
	}
}

func TestContextNoContent(t *testing.T) {
	e := New()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/?pretty", nil)
	c := e.NewContext(req, rec).(*context)

	c.NoContent(http.StatusOK)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestContextError(t *testing.T) {
	e := New()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/?pretty", nil)
	c := e.NewContext(req, rec).(*context)

	c.Error(errors.New("error"))
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.True(t, c.Response().Committed)
}

func TestContextReset(t *testing.T) {
	e := New()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	c := e.NewContext(req, rec).(*context)

	c.SetParamNames("foo")
	c.SetParamValues("bar")
	c.Set("foe", "ban")
	c.query = url.Values(map[string][]string{"fon": {"baz"}})

	c.Reset(req, httptest.NewRecorder())

	assert.Len(t, c.ParamValues(), 0)
	assert.Len(t, c.ParamNames(), 0)
	assert.Len(t, c.Path(), 0)
	assert.Len(t, c.QueryParams(), 0)
	assert.Len(t, c.store, 0)
}

func TestContext_JSON_CommitsCustomResponseCode(t *testing.T) {
	e := New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec).(*context)
	err := c.JSON(http.StatusCreated, user{1, "Jon Snow"})

	if assert.NoError(t, err) {
		assert.Equal(t, http.StatusCreated, rec.Code)
		assert.Equal(t, MIMEApplicationJSON, rec.Header().Get(HeaderContentType))
		assert.Equal(t, userJSON+"\n", rec.Body.String())
	}
}

func TestContext_JSON_DoesntCommitResponseCodePrematurely(t *testing.T) {
	e := New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec).(*context)
	err := c.JSON(http.StatusCreated, map[string]float64{"a": math.NaN()})

	if assert.Error(t, err) {
		assert.False(t, c.response.Committed)
	}
}

func TestContextCookie(t *testing.T) {
	e := New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	theme := "theme=light"
	user := "user=Jon Snow"
	req.Header.Add(HeaderCookie, theme)
	req.Header.Add(HeaderCookie, user)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec).(*context)

	// Read single
	cookie, err := c.Cookie("theme")
	if assert.NoError(t, err) {
		assert.Equal(t, "theme", cookie.Name)
		assert.Equal(t, "light", cookie.Value)
	}

	// Read multiple
	for _, cookie := range c.Cookies() {
		switch cookie.Name {
		case "theme":
			assert.Equal(t, "light", cookie.Value)
		case "user":
			assert.Equal(t, "Jon Snow", cookie.Value)
		}
	}

	// Write
	cookie = &http.Cookie{
		Name:     "SSID",
		Value:    "Ap4PGTEq",
		Domain:   "labstack.com",
		Path:     "/",
		Expires:  time.Now(),
		Secure:   true,
		HttpOnly: true,
	}
	c.SetCookie(cookie)
	assert.Contains(t, rec.Header().Get(HeaderSetCookie), "SSID")
	assert.Contains(t, rec.Header().Get(HeaderSetCookie), "Ap4PGTEq")
	assert.Contains(t, rec.Header().Get(HeaderSetCookie), "labstack.com")
	assert.Contains(t, rec.Header().Get(HeaderSetCookie), "Secure")
	assert.Contains(t, rec.Header().Get(HeaderSetCookie), "HttpOnly")
}

func TestContextPath(t *testing.T) {
	e := New()
	r := e.Router()

	handler := func(c Context) error { return c.String(http.StatusOK, "OK") }

	r.Add(http.MethodGet, "/users/:id", handler)
	c := e.NewContext(nil, nil)
	r.Find(http.MethodGet, "/users/1", c)

	assert.Equal(t, "/users/:id", c.Path())

	r.Add(http.MethodGet, "/users/:uid/files/:fid", handler)
	c = e.NewContext(nil, nil)
	r.Find(http.MethodGet, "/users/1/files/1", c)
	assert.Equal(t, "/users/:uid/files/:fid", c.Path())
}

func TestContextPathParam(t *testing.T) {
	e := New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	c := e.NewContext(req, nil)

	// ParamNames
	c.SetParamNames("uid", "fid")
	assert.EqualValues(t, []string{"uid", "fid"}, c.ParamNames())

	// ParamValues
	c.SetParamValues("101", "501")
	assert.EqualValues(t, []string{"101", "501"}, c.ParamValues())

	// Param
	assert.Equal(t, "501", c.Param("fid"))
	assert.Equal(t, "", c.Param("undefined"))
}

func TestContextGetAndSetParam(t *testing.T) {
	e := New()
	r := e.Router()
	r.Add(http.MethodGet, "/:foo", func(Context) error { return nil })
	req := httptest.NewRequest(http.MethodGet, "/:foo", nil)
	c := e.NewContext(req, nil)
	c.SetParamNames("foo")

	// round-trip param values with modification
	paramVals := c.ParamValues()
	assert.EqualValues(t, []string{""}, c.ParamValues())
	paramVals[0] = "bar"
	c.SetParamValues(paramVals...)
	assert.EqualValues(t, []string{"bar"}, c.ParamValues())

	// shouldn't explode during Reset() afterwards!
	assert.NotPanics(t, func() {
		c.Reset(nil, nil)
	})
}

func TestContextSetParamNamesEchoMaxParam(t *testing.T) {
	e := New()
	assert.Equal(t, 0, *e.maxParam)

	expectedOneParam := []string{"one"}
	expectedTwoParams := []string{"one", "two"}
	expectedThreeParams := []string{"one", "two", ""}

	{
		c := e.AcquireContext()
		c.SetParamNames("1", "2")
		c.SetParamValues(expectedTwoParams...)
		assert.Equal(t, 0, *e.maxParam) // has not been changed
		assert.EqualValues(t, expectedTwoParams, c.ParamValues())
		e.ReleaseContext(c)
	}

	{
		c := e.AcquireContext()
		c.SetParamNames("1", "2", "3")
		c.SetParamValues(expectedThreeParams...)
		assert.Equal(t, 0, *e.maxParam) // has not been changed
		assert.EqualValues(t, expectedThreeParams, c.ParamValues())
		e.ReleaseContext(c)
	}

	{ // values is always same size as names length
		c := e.NewContext(nil, nil)
		c.SetParamValues([]string{"one", "two"}...) // more values than names should be ok
		c.SetParamNames("1")
		assert.Equal(t, 0, *e.maxParam) // has not been changed
		assert.EqualValues(t, expectedOneParam, c.ParamValues())
	}

	e.GET("/:id", handlerFunc)
	assert.Equal(t, 1, *e.maxParam) // has not been changed

	{
		c := e.NewContext(nil, nil)
		c.SetParamValues([]string{"one", "two"}...)
		c.SetParamNames("1")
		assert.Equal(t, 1, *e.maxParam) // has not been changed
		assert.EqualValues(t, expectedOneParam, c.ParamValues())
	}
}

func TestContextFormValue(t *testing.T) {
	f := make(url.Values)
	f.Set("name", "Jon Snow")
	f.Set("email", "jon@labstack.com")

	e := New()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(f.Encode()))
	req.Header.Add(HeaderContentType, MIMEApplicationForm)
	c := e.NewContext(req, nil)

	// FormValue
	assert.Equal(t, "Jon Snow", c.FormValue("name"))
	assert.Equal(t, "jon@labstack.com", c.FormValue("email"))

	// FormParams
	params, err := c.FormParams()
	if assert.NoError(t, err) {
		assert.Equal(t, url.Values{
			"name":  []string{"Jon Snow"},
			"email": []string{"jon@labstack.com"},
		}, params)
	}

	// Multipart FormParams error
	req = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(f.Encode()))
	req.Header.Add(HeaderContentType, MIMEMultipartForm)
	c = e.NewContext(req, nil)
	params, err = c.FormParams()
	assert.Nil(t, params)
	assert.Error(t, err)
}

func TestContextQueryParam(t *testing.T) {
	q := make(url.Values)
	q.Set("name", "Jon Snow")
	q.Set("email", "jon@labstack.com")
	req := httptest.NewRequest(http.MethodGet, "/?"+q.Encode(), nil)
	e := New()
	c := e.NewContext(req, nil)

	// QueryParam
	assert.Equal(t, "Jon Snow", c.QueryParam("name"))
	assert.Equal(t, "jon@labstack.com", c.QueryParam("email"))

	// QueryParams
	assert.Equal(t, url.Values{
		"name":  []string{"Jon Snow"},
		"email": []string{"jon@labstack.com"},
	}, c.QueryParams())
}

func TestContextFormFile(t *testing.T) {
	e := New()
	buf := new(bytes.Buffer)
	mr := multipart.NewWriter(buf)
	w, err := mr.CreateFormFile("file", "test")
	if assert.NoError(t, err) {
		w.Write([]byte("test"))
	}
	mr.Close()
	req := httptest.NewRequest(http.MethodPost, "/", buf)
	req.Header.Set(HeaderContentType, mr.FormDataContentType())
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	f, err := c.FormFile("file")
	if assert.NoError(t, err) {
		assert.Equal(t, "test", f.Filename)
	}
}

func TestContextMultipartForm(t *testing.T) {
	e := New()
	buf := new(bytes.Buffer)
	mw := multipart.NewWriter(buf)
	mw.WriteField("name", "Jon Snow")
	fileContent := "This is a test file"
	w, err := mw.CreateFormFile("file", "test.txt")
	if assert.NoError(t, err) {
		w.Write([]byte(fileContent))
	}
	mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/", buf)
	req.Header.Set(HeaderContentType, mw.FormDataContentType())
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	f, err := c.MultipartForm()
	if assert.NoError(t, err) {
		assert.NotNil(t, f)

		files := f.File["file"]
		if assert.Len(t, files, 1) {
			file := files[0]
			assert.Equal(t, "test.txt", file.Filename)
			assert.Equal(t, int64(len(fileContent)), file.Size)
		}
	}
}

func TestContextRedirect(t *testing.T) {
	e := New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	assert.Equal(t, nil, c.Redirect(http.StatusMovedPermanently, "http://labstack.github.io/echo"))
	assert.Equal(t, http.StatusMovedPermanently, rec.Code)
	assert.Equal(t, "http://labstack.github.io/echo", rec.Header().Get(HeaderLocation))
	assert.Error(t, c.Redirect(310, "http://labstack.github.io/echo"))
}

func TestContextStore(t *testing.T) {
	var c Context = new(context)
	c.Set("name", "Jon Snow")
	assert.Equal(t, "Jon Snow", c.Get("name"))
}

func BenchmarkContext_Store(b *testing.B) {
	e := &Echo{}

	c := &context{
		echo: e,
	}

	for n := 0; n < b.N; n++ {
		c.Set("name", "Jon Snow")
		if c.Get("name") != "Jon Snow" {
			b.Fail()
		}
	}
}

func TestContextHandler(t *testing.T) {
	e := New()
	r := e.Router()
	b := new(bytes.Buffer)

	r.Add(http.MethodGet, "/handler", func(Context) error {
		_, err := b.Write([]byte("handler"))
		return err
	})
	c := e.NewContext(nil, nil)
	r.Find(http.MethodGet, "/handler", c)
	err := c.Handler()(c)
	assert.Equal(t, "handler", b.String())
	assert.NoError(t, err)
}

func TestContext_SetHandler(t *testing.T) {
	var c Context = new(context)

	assert.Nil(t, c.Handler())

	c.SetHandler(func(c Context) error {
		return nil
	})
	assert.NotNil(t, c.Handler())
}

func TestContext_Path(t *testing.T) {
	path := "/pa/th"

	var c Context = new(context)

	c.SetPath(path)
	assert.Equal(t, path, c.Path())
}

type validator struct{}

func (*validator) Validate(i interface{}) error {
	return nil
}

func TestContext_Validate(t *testing.T) {
	e := New()
	c := e.NewContext(nil, nil)

	assert.Error(t, c.Validate(struct{}{}))

	e.Validator = &validator{}
	assert.NoError(t, c.Validate(struct{}{}))
}

func TestContext_QueryString(t *testing.T) {
	e := New()

	queryString := "query=string&var=val"

	req := httptest.NewRequest(http.MethodGet, "/?"+queryString, nil)
	c := e.NewContext(req, nil)

	assert.Equal(t, queryString, c.QueryString())
}

func TestContext_Request(t *testing.T) {
	var c Context = new(context)

	assert.Nil(t, c.Request())

	req := httptest.NewRequest(http.MethodGet, "/path", nil)
	c.SetRequest(req)

	assert.Equal(t, req, c.Request())
}

func TestContext_Scheme(t *testing.T) {
	var testCases = []struct {
		name         string
		givenIsTLS   bool
		givenHeaders http.Header
		expect       string
	}{
		{
			name:         "defaults to http without TLS or headers",
			givenIsTLS:   false,
			givenHeaders: nil,
			expect:       "http",
		},
		{
			name:         "returns https when TLS is enabled",
			givenIsTLS:   true,
			givenHeaders: nil,
			expect:       "https",
		},
		{
			name:       "TLS takes precedence over forwarded proto",
			givenIsTLS: true,
			givenHeaders: http.Header{
				HeaderXForwardedProto: []string{"http"},
			},
			expect: "https",
		},
		{
			name:       "uses X-Forwarded-Proto http",
			givenIsTLS: false,
			givenHeaders: http.Header{
				HeaderXForwardedProto: []string{"http"},
			},
			expect: "http",
		},
		{
			name:       "uses X-Forwarded-Proto https",
			givenIsTLS: false,
			givenHeaders: http.Header{
				HeaderXForwardedProto: []string{"https"},
			},
			expect: "https",
		},
		{
			name:       "X-Forwarded-Proto is case insensitive",
			givenIsTLS: false,
			givenHeaders: http.Header{
				HeaderXForwardedProto: []string{"HTTPS"},
			},
			expect: "HTTPS",
		},
		{
			name:       "uses X-Forwarded-Proto ws",
			givenIsTLS: false,
			givenHeaders: http.Header{
				HeaderXForwardedProto: []string{"ws"},
			},
			expect: "ws",
		},
		{
			name:       "uses X-Forwarded-Proto wss",
			givenIsTLS: false,
			givenHeaders: http.Header{
				HeaderXForwardedProto: []string{"wss"},
			},
			expect: "wss",
		},
		{
			name:       "ignores invalid X-Forwarded-Proto and uses X-Forwarded-Protocol",
			givenIsTLS: false,
			givenHeaders: http.Header{
				HeaderXForwardedProto:    []string{"ftp"},
				HeaderXForwardedProtocol: []string{"https"},
			},
			expect: "https",
		},
		{
			name:       "uses X-Forwarded-Protocol",
			givenIsTLS: false,
			givenHeaders: http.Header{
				HeaderXForwardedProtocol: []string{"https"},
			},
			expect: "https",
		},
		{
			name:       "X-Forwarded-Proto takes precedence over X-Forwarded-Protocol",
			givenIsTLS: false,
			givenHeaders: http.Header{
				HeaderXForwardedProto:    []string{"http"},
				HeaderXForwardedProtocol: []string{"https"},
			},
			expect: "http",
		},
		{
			name:       "uses X-Forwarded-Ssl on",
			givenIsTLS: false,
			givenHeaders: http.Header{
				HeaderXForwardedSsl: []string{"on"},
			},
			expect: "https",
		},
		{
			name:       "X-Forwarded-Ssl on is case sensitive",
			givenIsTLS: false,
			givenHeaders: http.Header{
				HeaderXForwardedSsl: []string{"ON"},
			},
			expect: "http",
		},
		{
			name:       "X-Forwarded-Protocol takes precedence over X-Forwarded-Ssl",
			givenIsTLS: false,
			givenHeaders: http.Header{
				HeaderXForwardedProtocol: []string{"http"},
				HeaderXForwardedSsl:      []string{"on"},
			},
			expect: "http",
		},
		{
			name:       "uses X-Url-Scheme",
			givenIsTLS: false,
			givenHeaders: http.Header{
				HeaderXUrlScheme: []string{"https"},
			},
			expect: "https",
		},
		{
			name:       "X-Forwarded-Ssl takes precedence over X-Url-Scheme",
			givenIsTLS: false,
			givenHeaders: http.Header{
				HeaderXForwardedSsl: []string{"on"},
				HeaderXUrlScheme:    []string{"http"},
			},
			expect: "https",
		},
		{
			name:       "ignores invalid forwarded headers and falls back to http",
			givenIsTLS: false,
			givenHeaders: http.Header{
				HeaderXForwardedProto:    []string{"ftp"},
				HeaderXForwardedProtocol: []string{"smtp"},
				HeaderXForwardedSsl:      []string{"off"},
				HeaderXUrlScheme:         []string{"file"},
			},
			expect: "http",
		},
		{
			name:       "ignores empty forwarded proto and uses X-Url-Scheme",
			givenIsTLS: false,
			givenHeaders: http.Header{
				HeaderXForwardedProto: []string{""},
				HeaderXUrlScheme:      []string{"https"},
			},
			expect: "https",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tc.givenHeaders != nil {
				req.Header = tc.givenHeaders
			}
			e := New()
			c := e.NewContext(req, nil)
			if tc.givenIsTLS {
				c.Request().TLS = &tls.ConnectionState{}
			}

			assert.Equal(t, tc.expect, c.Scheme())
		})
	}
}

func TestContext_IsWebSocket(t *testing.T) {
	tests := []struct {
		c  Context
		ws assert.BoolAssertionFunc
	}{
		{
			&context{
				request: &http.Request{
					Header: http.Header{HeaderUpgrade: []string{"websocket"}},
				},
			},
			assert.True,
		},
		{
			&context{
				request: &http.Request{
					Header: http.Header{HeaderUpgrade: []string{"Websocket"}},
				},
			},
			assert.True,
		},
		{
			&context{
				request: &http.Request{},
			},
			assert.False,
		},
		{
			&context{
				request: &http.Request{
					Header: http.Header{HeaderUpgrade: []string{"other"}},
				},
			},
			assert.False,
		},
	}

	for i, tt := range tests {
		t.Run(fmt.Sprintf("test %d", i+1), func(t *testing.T) {
			tt.ws(t, tt.c.IsWebSocket())
		})
	}
}

func TestContext_Bind(t *testing.T) {
	e := New()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(userJSON))
	c := e.NewContext(req, nil)
	u := new(user)

	req.Header.Add(HeaderContentType, MIMEApplicationJSON)
	err := c.Bind(u)
	assert.NoError(t, err)
	assert.Equal(t, &user{1, "Jon Snow"}, u)
}

func TestContext_Logger(t *testing.T) {
	e := New()
	c := e.NewContext(nil, nil)

	log1 := c.Logger()
	assert.NotNil(t, log1)

	log2 := log.New("echo2")
	c.SetLogger(log2)
	assert.Equal(t, log2, c.Logger())

	// Resetting the context returns the initial logger
	c.Reset(nil, nil)
	assert.Equal(t, log1, c.Logger())
}

func TestContext_RealIP(t *testing.T) {
	tests := []struct {
		c Context
		s string
	}{
		{
			&context{
				request: &http.Request{
					Header: http.Header{HeaderXForwardedFor: []string{"127.0.0.1, 127.0.1.1, "}},
				},
			},
			"127.0.0.1",
		},
		{
			&context{
				request: &http.Request{
					Header: http.Header{HeaderXForwardedFor: []string{"127.0.0.1,127.0.1.1"}},
				},
			},
			"127.0.0.1",
		},
		{
			&context{
				request: &http.Request{
					Header: http.Header{HeaderXForwardedFor: []string{"127.0.0.1"}},
				},
			},
			"127.0.0.1",
		},
		{
			&context{
				request: &http.Request{
					Header: http.Header{HeaderXForwardedFor: []string{"[2001:db8:85a3:8d3:1319:8a2e:370:7348], 2001:db8::1, "}},
				},
			},
			"2001:db8:85a3:8d3:1319:8a2e:370:7348",
		},
		{
			&context{
				request: &http.Request{
					Header: http.Header{HeaderXForwardedFor: []string{"[2001:db8:85a3:8d3:1319:8a2e:370:7348],[2001:db8::1]"}},
				},
			},
			"2001:db8:85a3:8d3:1319:8a2e:370:7348",
		},
		{
			&context{
				request: &http.Request{
					Header: http.Header{HeaderXForwardedFor: []string{"2001:db8:85a3:8d3:1319:8a2e:370:7348"}},
				},
			},
			"2001:db8:85a3:8d3:1319:8a2e:370:7348",
		},
		{
			&context{
				request: &http.Request{
					Header: http.Header{
						"X-Real-Ip": []string{"192.168.0.1"},
					},
				},
			},
			"192.168.0.1",
		},
		{
			&context{
				request: &http.Request{
					Header: http.Header{
						"X-Real-Ip": []string{"[2001:db8::1]"},
					},
				},
			},
			"2001:db8::1",
		},

		{
			&context{
				request: &http.Request{
					RemoteAddr: "89.89.89.89:1654",
				},
			},
			"89.89.89.89",
		},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.s, tt.c.RealIP())
	}
}
