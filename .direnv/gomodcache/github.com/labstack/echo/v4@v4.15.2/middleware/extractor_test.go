// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: © 2015 LabStack LLC and Echo contributors

package middleware

import (
	"bytes"
	"fmt"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

type pathParam struct {
	name  string
	value string
}

func setPathParams(c echo.Context, params []pathParam) {
	names := make([]string, 0, len(params))
	values := make([]string, 0, len(params))
	for _, pp := range params {
		names = append(names, pp.name)
		values = append(values, pp.value)
	}
	c.SetParamNames(names...)
	c.SetParamValues(values...)
}

func TestCreateExtractors(t *testing.T) {
	var testCases = []struct {
		name              string
		givenRequest      func() *http.Request
		givenPathParams   []pathParam
		whenLoopups       string
		expectValues      []string
		expectCreateError string
		expectError       string
	}{
		{
			name: "ok, header",
			givenRequest: func() *http.Request {
				req := httptest.NewRequest(http.MethodGet, "/", nil)
				req.Header.Set(echo.HeaderAuthorization, "Bearer token")
				return req
			},
			whenLoopups:  "header:Authorization:Bearer ",
			expectValues: []string{"token"},
		},
		{
			name: "ok, form",
			givenRequest: func() *http.Request {
				f := make(url.Values)
				f.Set("name", "Jon Snow")

				req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(f.Encode()))
				req.Header.Add(echo.HeaderContentType, echo.MIMEApplicationForm)
				return req
			},
			whenLoopups:  "form:name",
			expectValues: []string{"Jon Snow"},
		},
		{
			name: "ok, cookie",
			givenRequest: func() *http.Request {
				req := httptest.NewRequest(http.MethodGet, "/", nil)
				req.Header.Set(echo.HeaderCookie, "_csrf=token")
				return req
			},
			whenLoopups:  "cookie:_csrf",
			expectValues: []string{"token"},
		},
		{
			name: "ok, param",
			givenPathParams: []pathParam{
				{name: "id", value: "123"},
			},
			whenLoopups:  "param:id",
			expectValues: []string{"123"},
		},
		{
			name: "ok, query",
			givenRequest: func() *http.Request {
				req := httptest.NewRequest(http.MethodGet, "/?id=999", nil)
				return req
			},
			whenLoopups:  "query:id",
			expectValues: []string{"999"},
		},
		{
			name:              "nok, invalid lookup",
			whenLoopups:       "query",
			expectCreateError: "extractor source for lookup could not be split into needed parts: query",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			e := echo.New()

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tc.givenRequest != nil {
				req = tc.givenRequest()
			}
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			if tc.givenPathParams != nil {
				setPathParams(c, tc.givenPathParams)
			}

			extractors, err := CreateExtractors(tc.whenLoopups)
			if tc.expectCreateError != "" {
				assert.EqualError(t, err, tc.expectCreateError)
				return
			}
			assert.NoError(t, err)

			for _, e := range extractors {
				values, eErr := e(c)
				assert.Equal(t, tc.expectValues, values)
				if tc.expectError != "" {
					assert.EqualError(t, eErr, tc.expectError)
					return
				}
				assert.NoError(t, eErr)
			}
		})
	}
}

func TestValuesFromHeader(t *testing.T) {
	exampleRequest := func(req *http.Request) {
		req.Header.Set(echo.HeaderAuthorization, "basic dXNlcjpwYXNzd29yZA==")
	}

	var testCases = []struct {
		name            string
		givenRequest    func(req *http.Request)
		whenName        string
		whenValuePrefix string
		expectValues    []string
		expectError     string
	}{
		{
			name:            "ok, single value",
			givenRequest:    exampleRequest,
			whenName:        echo.HeaderAuthorization,
			whenValuePrefix: "basic ",
			expectValues:    []string{"dXNlcjpwYXNzd29yZA=="},
		},
		{
			name:            "ok, single value, case insensitive",
			givenRequest:    exampleRequest,
			whenName:        echo.HeaderAuthorization,
			whenValuePrefix: "Basic ",
			expectValues:    []string{"dXNlcjpwYXNzd29yZA=="},
		},
		{
			name: "ok, multiple value",
			givenRequest: func(req *http.Request) {
				req.Header.Set(echo.HeaderAuthorization, "basic dXNlcjpwYXNzd29yZA==")
				req.Header.Add(echo.HeaderAuthorization, "basic dGVzdDp0ZXN0")
			},
			whenName:        echo.HeaderAuthorization,
			whenValuePrefix: "basic ",
			expectValues:    []string{"dXNlcjpwYXNzd29yZA==", "dGVzdDp0ZXN0"},
		},
		{
			name:            "ok, empty prefix",
			givenRequest:    exampleRequest,
			whenName:        echo.HeaderAuthorization,
			whenValuePrefix: "",
			expectValues:    []string{"basic dXNlcjpwYXNzd29yZA=="},
		},
		{
			name: "nok, no matching due different prefix",
			givenRequest: func(req *http.Request) {
				req.Header.Set(echo.HeaderAuthorization, "basic dXNlcjpwYXNzd29yZA==")
				req.Header.Add(echo.HeaderAuthorization, "basic dGVzdDp0ZXN0")
			},
			whenName:        echo.HeaderAuthorization,
			whenValuePrefix: "Bearer ",
			expectError:     errHeaderExtractorValueInvalid.Error(),
		},
		{
			name: "nok, no matching due different prefix",
			givenRequest: func(req *http.Request) {
				req.Header.Set(echo.HeaderAuthorization, "basic dXNlcjpwYXNzd29yZA==")
				req.Header.Add(echo.HeaderAuthorization, "basic dGVzdDp0ZXN0")
			},
			whenName:        echo.HeaderWWWAuthenticate,
			whenValuePrefix: "",
			expectError:     errHeaderExtractorValueMissing.Error(),
		},
		{
			name:            "nok, no headers",
			givenRequest:    nil,
			whenName:        echo.HeaderAuthorization,
			whenValuePrefix: "basic ",
			expectError:     errHeaderExtractorValueMissing.Error(),
		},
		{
			name: "ok, prefix, cut values over extractorLimit",
			givenRequest: func(req *http.Request) {
				for i := 1; i <= 25; i++ {
					req.Header.Add(echo.HeaderAuthorization, fmt.Sprintf("basic %v", i))
				}
			},
			whenName:        echo.HeaderAuthorization,
			whenValuePrefix: "basic ",
			expectValues: []string{
				"1", "2", "3", "4", "5", "6", "7", "8", "9", "10",
				"11", "12", "13", "14", "15", "16", "17", "18", "19", "20",
			},
		},
		{
			name: "ok, cut values over extractorLimit",
			givenRequest: func(req *http.Request) {
				for i := 1; i <= 25; i++ {
					req.Header.Add(echo.HeaderAuthorization, fmt.Sprintf("%v", i))
				}
			},
			whenName:        echo.HeaderAuthorization,
			whenValuePrefix: "",
			expectValues: []string{
				"1", "2", "3", "4", "5", "6", "7", "8", "9", "10",
				"11", "12", "13", "14", "15", "16", "17", "18", "19", "20",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			e := echo.New()

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tc.givenRequest != nil {
				tc.givenRequest(req)
			}
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			extractor := valuesFromHeader(tc.whenName, tc.whenValuePrefix)

			values, err := extractor(c)
			assert.Equal(t, tc.expectValues, values)
			if tc.expectError != "" {
				assert.EqualError(t, err, tc.expectError)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValuesFromQuery(t *testing.T) {
	var testCases = []struct {
		name           string
		givenQueryPart string
		whenName       string
		expectValues   []string
		expectError    string
	}{
		{
			name:           "ok, single value",
			givenQueryPart: "?id=123&name=test",
			whenName:       "id",
			expectValues:   []string{"123"},
		},
		{
			name:           "ok, multiple value",
			givenQueryPart: "?id=123&id=456&name=test",
			whenName:       "id",
			expectValues:   []string{"123", "456"},
		},
		{
			name:           "nok, missing value",
			givenQueryPart: "?id=123&name=test",
			whenName:       "nope",
			expectError:    errQueryExtractorValueMissing.Error(),
		},
		{
			name: "ok, cut values over extractorLimit",
			givenQueryPart: "?name=test" +
				"&id=1&id=2&id=3&id=4&id=5&id=6&id=7&id=8&id=9&id=10" +
				"&id=11&id=12&id=13&id=14&id=15&id=16&id=17&id=18&id=19&id=20" +
				"&id=21&id=22&id=23&id=24&id=25",
			whenName: "id",
			expectValues: []string{
				"1", "2", "3", "4", "5", "6", "7", "8", "9", "10",
				"11", "12", "13", "14", "15", "16", "17", "18", "19", "20",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			e := echo.New()

			req := httptest.NewRequest(http.MethodGet, "/"+tc.givenQueryPart, nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			extractor := valuesFromQuery(tc.whenName)

			values, err := extractor(c)
			assert.Equal(t, tc.expectValues, values)
			if tc.expectError != "" {
				assert.EqualError(t, err, tc.expectError)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValuesFromParam(t *testing.T) {
	examplePathParams := []pathParam{
		{name: "id", value: "123"},
		{name: "gid", value: "456"},
		{name: "gid", value: "789"},
	}
	examplePathParams20 := make([]pathParam, 0)
	for i := 1; i < 25; i++ {
		examplePathParams20 = append(examplePathParams20, pathParam{name: "id", value: fmt.Sprintf("%v", i)})
	}

	var testCases = []struct {
		name            string
		givenPathParams []pathParam
		whenName        string
		expectValues    []string
		expectError     string
	}{
		{
			name:            "ok, single value",
			givenPathParams: examplePathParams,
			whenName:        "id",
			expectValues:    []string{"123"},
		},
		{
			name:            "ok, multiple value",
			givenPathParams: examplePathParams,
			whenName:        "gid",
			expectValues:    []string{"456", "789"},
		},
		{
			name:            "nok, no values",
			givenPathParams: nil,
			whenName:        "nope",
			expectValues:    nil,
			expectError:     errParamExtractorValueMissing.Error(),
		},
		{
			name:            "nok, no matching value",
			givenPathParams: examplePathParams,
			whenName:        "nope",
			expectValues:    nil,
			expectError:     errParamExtractorValueMissing.Error(),
		},
		{
			name:            "ok, cut values over extractorLimit",
			givenPathParams: examplePathParams20,
			whenName:        "id",
			expectValues: []string{
				"1", "2", "3", "4", "5", "6", "7", "8", "9", "10",
				"11", "12", "13", "14", "15", "16", "17", "18", "19", "20",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			e := echo.New()

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			if tc.givenPathParams != nil {
				setPathParams(c, tc.givenPathParams)
			}

			extractor := valuesFromParam(tc.whenName)

			values, err := extractor(c)
			assert.Equal(t, tc.expectValues, values)
			if tc.expectError != "" {
				assert.EqualError(t, err, tc.expectError)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValuesFromCookie(t *testing.T) {
	exampleRequest := func(req *http.Request) {
		req.Header.Set(echo.HeaderCookie, "_csrf=token")
	}

	var testCases = []struct {
		name         string
		givenRequest func(req *http.Request)
		whenName     string
		expectValues []string
		expectError  string
	}{
		{
			name:         "ok, single value",
			givenRequest: exampleRequest,
			whenName:     "_csrf",
			expectValues: []string{"token"},
		},
		{
			name: "ok, multiple value",
			givenRequest: func(req *http.Request) {
				req.Header.Add(echo.HeaderCookie, "_csrf=token")
				req.Header.Add(echo.HeaderCookie, "_csrf=token2")
			},
			whenName:     "_csrf",
			expectValues: []string{"token", "token2"},
		},
		{
			name:         "nok, no matching cookie",
			givenRequest: exampleRequest,
			whenName:     "xxx",
			expectValues: nil,
			expectError:  errCookieExtractorValueMissing.Error(),
		},
		{
			name:         "nok, no cookies at all",
			givenRequest: nil,
			whenName:     "xxx",
			expectValues: nil,
			expectError:  errCookieExtractorValueMissing.Error(),
		},
		{
			name: "ok, cut values over extractorLimit",
			givenRequest: func(req *http.Request) {
				for i := 1; i < 25; i++ {
					req.Header.Add(echo.HeaderCookie, fmt.Sprintf("_csrf=%v", i))
				}
			},
			whenName: "_csrf",
			expectValues: []string{
				"1", "2", "3", "4", "5", "6", "7", "8", "9", "10",
				"11", "12", "13", "14", "15", "16", "17", "18", "19", "20",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			e := echo.New()

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tc.givenRequest != nil {
				tc.givenRequest(req)
			}
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			extractor := valuesFromCookie(tc.whenName)

			values, err := extractor(c)
			assert.Equal(t, tc.expectValues, values)
			if tc.expectError != "" {
				assert.EqualError(t, err, tc.expectError)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValuesFromForm(t *testing.T) {
	examplePostFormRequest := func(mod func(v *url.Values)) *http.Request {
		f := make(url.Values)
		f.Set("name", "Jon Snow")
		f.Set("emails[]", "jon@labstack.com")
		if mod != nil {
			mod(&f)
		}

		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(f.Encode()))
		req.Header.Add(echo.HeaderContentType, echo.MIMEApplicationForm)

		return req
	}
	exampleGetFormRequest := func(mod func(v *url.Values)) *http.Request {
		f := make(url.Values)
		f.Set("name", "Jon Snow")
		f.Set("emails[]", "jon@labstack.com")
		if mod != nil {
			mod(&f)
		}

		req := httptest.NewRequest(http.MethodGet, "/?"+f.Encode(), nil)
		return req
	}

	exampleMultiPartFormRequest := func(mod func(w *multipart.Writer)) *http.Request {
		var b bytes.Buffer
		w := multipart.NewWriter(&b)
		w.WriteField("name", "Jon Snow")
		w.WriteField("emails[]", "jon@labstack.com")
		if mod != nil {
			mod(w)
		}

		fw, _ := w.CreateFormFile("upload", "my.file")
		fw.Write([]byte(`<div>hi</div>`))
		w.Close()

		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(b.String()))
		req.Header.Add(echo.HeaderContentType, w.FormDataContentType())

		return req
	}

	var testCases = []struct {
		name         string
		givenRequest *http.Request
		whenName     string
		expectValues []string
		expectError  string
	}{
		{
			name:         "ok, POST form, single value",
			givenRequest: examplePostFormRequest(nil),
			whenName:     "emails[]",
			expectValues: []string{"jon@labstack.com"},
		},
		{
			name: "ok, POST form, multiple value",
			givenRequest: examplePostFormRequest(func(v *url.Values) {
				v.Add("emails[]", "snow@labstack.com")
			}),
			whenName:     "emails[]",
			expectValues: []string{"jon@labstack.com", "snow@labstack.com"},
		},
		{
			name: "ok, POST multipart/form, multiple value",
			givenRequest: exampleMultiPartFormRequest(func(w *multipart.Writer) {
				w.WriteField("emails[]", "snow@labstack.com")
			}),
			whenName:     "emails[]",
			expectValues: []string{"jon@labstack.com", "snow@labstack.com"},
		},
		{
			name:         "ok, GET form, single value",
			givenRequest: exampleGetFormRequest(nil),
			whenName:     "emails[]",
			expectValues: []string{"jon@labstack.com"},
		},
		{
			name: "ok, GET form, multiple value",
			givenRequest: examplePostFormRequest(func(v *url.Values) {
				v.Add("emails[]", "snow@labstack.com")
			}),
			whenName:     "emails[]",
			expectValues: []string{"jon@labstack.com", "snow@labstack.com"},
		},
		{
			name:         "nok, POST form, value missing",
			givenRequest: examplePostFormRequest(nil),
			whenName:     "nope",
			expectError:  errFormExtractorValueMissing.Error(),
		},
		{
			name: "ok, cut values over extractorLimit",
			givenRequest: examplePostFormRequest(func(v *url.Values) {
				for i := 1; i < 25; i++ {
					v.Add("id[]", fmt.Sprintf("%v", i))
				}
			}),
			whenName: "id[]",
			expectValues: []string{
				"1", "2", "3", "4", "5", "6", "7", "8", "9", "10",
				"11", "12", "13", "14", "15", "16", "17", "18", "19", "20",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			e := echo.New()

			req := tc.givenRequest
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			extractor := valuesFromForm(tc.whenName)

			values, err := extractor(c)
			assert.Equal(t, tc.expectValues, values)
			if tc.expectError != "" {
				assert.EqualError(t, err, tc.expectError)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
