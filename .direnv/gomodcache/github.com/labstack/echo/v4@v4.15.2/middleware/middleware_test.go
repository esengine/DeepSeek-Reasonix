// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: © 2015 LabStack LLC and Echo contributors

package middleware

import (
	"bufio"
	"errors"
	"github.com/stretchr/testify/assert"
	"net"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
)

func TestRewriteURL(t *testing.T) {
	var testCases = []struct {
		whenURL       string
		expectPath    string
		expectRawPath string
		expectQuery   string
		expectErr     string
	}{
		{
			whenURL:       "http://localhost:8080/old",
			expectPath:    "/new",
			expectRawPath: "",
		},
		{ // encoded `ol%64` (decoded `old`) should not be rewritten to `/new`
			whenURL:       "/ol%64", // `%64` is decoded `d`
			expectPath:    "/old",
			expectRawPath: "/ol%64",
		},
		{
			whenURL:       "http://localhost:8080/users/+_+/orders/___++++?test=1",
			expectPath:    "/user/+_+/order/___++++",
			expectRawPath: "",
			expectQuery:   "test=1",
		},
		{
			whenURL:       "http://localhost:8080/users/%20a/orders/%20aa",
			expectPath:    "/user/ a/order/ aa",
			expectRawPath: "",
		},
		{
			whenURL:       "http://localhost:8080/%47%6f%2f?test=1",
			expectPath:    "/Go/",
			expectRawPath: "/%47%6f%2f",
			expectQuery:   "test=1",
		},
		{
			whenURL:       "/users/jill/orders/T%2FcO4lW%2Ft%2FVp%2F",
			expectPath:    "/user/jill/order/T/cO4lW/t/Vp/",
			expectRawPath: "/user/jill/order/T%2FcO4lW%2Ft%2FVp%2F",
		},
		{ // do nothing, replace nothing
			whenURL:       "http://localhost:8080/user/jill/order/T%2FcO4lW%2Ft%2FVp%2F",
			expectPath:    "/user/jill/order/T/cO4lW/t/Vp/",
			expectRawPath: "/user/jill/order/T%2FcO4lW%2Ft%2FVp%2F",
		},
		{
			whenURL:       "http://localhost:8080/static",
			expectPath:    "/static/path",
			expectRawPath: "",
			expectQuery:   "role=AUTHOR&limit=1000",
		},
		{
			whenURL:       "/static",
			expectPath:    "/static/path",
			expectRawPath: "",
			expectQuery:   "role=AUTHOR&limit=1000",
		},
	}

	rules := map[*regexp.Regexp]string{
		regexp.MustCompile("^/old$"):                      "/new",
		regexp.MustCompile("^/users/(.*?)/orders/(.*?)$"): "/user/$1/order/$2",
		regexp.MustCompile("^/static$"):                   "/static/path?role=AUTHOR&limit=1000",
	}

	for _, tc := range testCases {
		t.Run(tc.whenURL, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.whenURL, nil)

			err := rewriteURL(rules, req)

			if tc.expectErr != "" {
				assert.EqualError(t, err, tc.expectErr)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tc.expectPath, req.URL.Path)       // Path field is stored in decoded form: /%47%6f%2f becomes /Go/.
			assert.Equal(t, tc.expectRawPath, req.URL.RawPath) // RawPath, an optional field which only gets set if the default encoding is different from Path.
			assert.Equal(t, tc.expectQuery, req.URL.RawQuery)
		})
	}
}

type testResponseWriterNoFlushHijack struct {
}

func (w *testResponseWriterNoFlushHijack) WriteHeader(statusCode int) {
}

func (w *testResponseWriterNoFlushHijack) Write([]byte) (int, error) {
	return 0, nil
}

func (w *testResponseWriterNoFlushHijack) Header() http.Header {
	return nil
}

type testResponseWriterUnwrapper struct {
	unwrapCalled int
	rw           http.ResponseWriter
}

func (w *testResponseWriterUnwrapper) WriteHeader(statusCode int) {
}

func (w *testResponseWriterUnwrapper) Write([]byte) (int, error) {
	return 0, nil
}

func (w *testResponseWriterUnwrapper) Header() http.Header {
	return nil
}

func (w *testResponseWriterUnwrapper) Unwrap() http.ResponseWriter {
	w.unwrapCalled++
	return w.rw
}

type testResponseWriterUnwrapperHijack struct {
	testResponseWriterUnwrapper
}

func (w *testResponseWriterUnwrapperHijack) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return nil, nil, errors.New("can hijack")
}
