// Copyright 2025 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package httpcommon

import (
	"cmp"
	"context"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
)

func TestEncodeHeaders(t *testing.T) {
	type header struct {
		name  string
		value string
	}
	for _, test := range []struct {
		name               string
		in                 EncodeHeadersParam
		want               EncodeHeadersResult
		wantHeaders        []header
		disableCompression bool
	}{{
		name: "simple request",
		in: EncodeHeadersParam{
			Request: newReq(func() *http.Request {
				return must(http.NewRequest("GET", "https://example.tld/", nil))
			}),
			DefaultUserAgent: "default-user-agent",
		},
		want: EncodeHeadersResult{
			HasBody:     false,
			HasTrailers: false,
		},
		wantHeaders: []header{
			{":authority", "example.tld"},
			{":method", "GET"},
			{":path", "/"},
			{":scheme", "https"},
			{"accept-encoding", "gzip"},
			{"user-agent", "default-user-agent"},
		},
	}, {
		name: "host set from URL",
		in: EncodeHeadersParam{
			Request: newReq(func() *http.Request {
				req := must(http.NewRequest("GET", "https://example.tld/", nil))
				req.Host = ""
				req.URL.Host = "example.tld"
				return req
			}),
			DefaultUserAgent: "default-user-agent",
		},
		want: EncodeHeadersResult{
			HasBody:     false,
			HasTrailers: false,
		},
		wantHeaders: []header{
			{":authority", "example.tld"},
			{":method", "GET"},
			{":path", "/"},
			{":scheme", "https"},
			{"accept-encoding", "gzip"},
			{"user-agent", "default-user-agent"},
		},
	}, {
		name: "chunked transfer-encoding",
		in: EncodeHeadersParam{
			Request: newReq(func() *http.Request {
				req := must(http.NewRequest("GET", "https://example.tld/", nil))
				req.Header.Set("Transfer-Encoding", "chunked") // ignored
				return req
			}),
			DefaultUserAgent: "default-user-agent",
		},
		want: EncodeHeadersResult{
			HasBody:     false,
			HasTrailers: false,
		},
		wantHeaders: []header{
			{":authority", "example.tld"},
			{":method", "GET"},
			{":path", "/"},
			{":scheme", "https"},
			{"accept-encoding", "gzip"},
			{"user-agent", "default-user-agent"},
		},
	}, {
		name: "connection close",
		in: EncodeHeadersParam{
			Request: newReq(func() *http.Request {
				req := must(http.NewRequest("GET", "https://example.tld/", nil))
				req.Header.Set("Connection", "close")
				return req
			}),
			DefaultUserAgent: "default-user-agent",
		},
		want: EncodeHeadersResult{
			HasBody:     false,
			HasTrailers: false,
		},
		wantHeaders: []header{
			{":authority", "example.tld"},
			{":method", "GET"},
			{":path", "/"},
			{":scheme", "https"},
			{"accept-encoding", "gzip"},
			{"user-agent", "default-user-agent"},
		},
	}, {
		name: "connection keep-alive",
		in: EncodeHeadersParam{
			Request: newReq(func() *http.Request {
				req := must(http.NewRequest("GET", "https://example.tld/", nil))
				req.Header.Set("Connection", "keep-alive")
				return req
			}),
			DefaultUserAgent: "default-user-agent",
		},
		want: EncodeHeadersResult{
			HasBody:     false,
			HasTrailers: false,
		},
		wantHeaders: []header{
			{":authority", "example.tld"},
			{":method", "GET"},
			{":path", "/"},
			{":scheme", "https"},
			{"accept-encoding", "gzip"},
			{"user-agent", "default-user-agent"},
		},
	}, {
		name: "normal connect",
		in: EncodeHeadersParam{
			Request: newReq(func() *http.Request {
				return must(http.NewRequest("CONNECT", "https://example.tld/", nil))
			}),
			DefaultUserAgent: "default-user-agent",
		},
		want: EncodeHeadersResult{
			HasBody:     false,
			HasTrailers: false,
		},
		wantHeaders: []header{
			{":authority", "example.tld"},
			{":method", "CONNECT"},
			{"accept-encoding", "gzip"},
			{"user-agent", "default-user-agent"},
		},
	}, {
		name: "extended connect",
		in: EncodeHeadersParam{
			Request: newReq(func() *http.Request {
				req := must(http.NewRequest("CONNECT", "https://example.tld/", nil))
				req.Header.Set(":protocol", "foo")
				return req
			}),
			DefaultUserAgent: "default-user-agent",
		},
		want: EncodeHeadersResult{
			HasBody:     false,
			HasTrailers: false,
		},
		wantHeaders: []header{
			{":authority", "example.tld"},
			{":method", "CONNECT"},
			{":path", "/"},
			{":protocol", "foo"},
			{":scheme", "https"},
			{"accept-encoding", "gzip"},
			{"user-agent", "default-user-agent"},
		},
	}, {
		name: "trailers",
		in: EncodeHeadersParam{
			Request: newReq(func() *http.Request {
				req := must(http.NewRequest("GET", "https://example.tld/", nil))
				req.Trailer = make(http.Header)
				req.Trailer.Set("a", "1")
				req.Trailer.Set("b", "2")
				return req
			}),
			DefaultUserAgent: "default-user-agent",
		},
		want: EncodeHeadersResult{
			HasBody:     false,
			HasTrailers: true,
		},
		wantHeaders: []header{
			{":authority", "example.tld"},
			{":method", "GET"},
			{":path", "/"},
			{":scheme", "https"},
			{"accept-encoding", "gzip"},
			{"trailer", "A,B"},
			{"user-agent", "default-user-agent"},
		},
	}, {
		name: "override user-agent",
		in: EncodeHeadersParam{
			Request: newReq(func() *http.Request {
				req := must(http.NewRequest("GET", "https://example.tld/", nil))
				req.Header.Set("User-Agent", "GopherTron 9000")
				return req
			}),
			DefaultUserAgent: "default-user-agent",
		},
		want: EncodeHeadersResult{
			HasBody:     false,
			HasTrailers: false,
		},
		wantHeaders: []header{
			{":authority", "example.tld"},
			{":method", "GET"},
			{":path", "/"},
			{":scheme", "https"},
			{"accept-encoding", "gzip"},
			{"user-agent", "GopherTron 9000"},
		},
	}, {
		name: "disable user-agent",
		in: EncodeHeadersParam{
			Request: newReq(func() *http.Request {
				req := must(http.NewRequest("GET", "https://example.tld/", nil))
				req.Header["User-Agent"] = nil
				return req
			}),
			DefaultUserAgent: "default-user-agent",
		},
		want: EncodeHeadersResult{
			HasBody:     false,
			HasTrailers: false,
		},
		wantHeaders: []header{
			{":authority", "example.tld"},
			{":method", "GET"},
			{":path", "/"},
			{":scheme", "https"},
			{"accept-encoding", "gzip"},
		},
	}, {
		name: "ignore host header",
		in: EncodeHeadersParam{
			Request: newReq(func() *http.Request {
				req := must(http.NewRequest("GET", "https://example.tld/", nil))
				req.Header.Set("Host", "gophers.tld/") // ignored
				return req
			}),
			DefaultUserAgent: "default-user-agent",
		},
		want: EncodeHeadersResult{
			HasBody:     false,
			HasTrailers: false,
		},
		wantHeaders: []header{
			{":authority", "example.tld"},
			{":method", "GET"},
			{":path", "/"},
			{":scheme", "https"},
			{"accept-encoding", "gzip"},
			{"user-agent", "default-user-agent"},
		},
	}, {
		name: "crumble cookie header",
		in: EncodeHeadersParam{
			Request: newReq(func() *http.Request {
				req := must(http.NewRequest("GET", "https://example.tld/", nil))
				req.Header.Set("Cookie", "a=b; b=c; c=d")
				return req
			}),
			DefaultUserAgent: "default-user-agent",
		},
		want: EncodeHeadersResult{
			HasBody:     false,
			HasTrailers: false,
		},
		wantHeaders: []header{
			{":authority", "example.tld"},
			{":method", "GET"},
			{":path", "/"},
			{":scheme", "https"},
			{"accept-encoding", "gzip"},
			{"user-agent", "default-user-agent"},
			// Cookie header is split into separate header fields.
			{"cookie", "a=b"},
			{"cookie", "b=c"},
			{"cookie", "c=d"},
		},
	}, {
		name: "post with nil body",
		in: EncodeHeadersParam{
			Request: newReq(func() *http.Request {
				return must(http.NewRequest("POST", "https://example.tld/", nil))
			}),
			DefaultUserAgent: "default-user-agent",
		},
		want: EncodeHeadersResult{
			HasBody:     false,
			HasTrailers: false,
		},
		wantHeaders: []header{
			{":authority", "example.tld"},
			{":method", "POST"},
			{":path", "/"},
			{":scheme", "https"},
			{"accept-encoding", "gzip"},
			{"user-agent", "default-user-agent"},
			{"content-length", "0"},
		},
	}, {
		name: "post with NoBody",
		in: EncodeHeadersParam{
			Request: newReq(func() *http.Request {
				return must(http.NewRequest("POST", "https://example.tld/", http.NoBody))
			}),
			DefaultUserAgent: "default-user-agent",
		},
		want: EncodeHeadersResult{
			HasBody:     false,
			HasTrailers: false,
		},
		wantHeaders: []header{
			{":authority", "example.tld"},
			{":method", "POST"},
			{":path", "/"},
			{":scheme", "https"},
			{"accept-encoding", "gzip"},
			{"user-agent", "default-user-agent"},
			{"content-length", "0"},
		},
	}, {
		name: "post with Content-Length",
		in: EncodeHeadersParam{
			Request: newReq(func() *http.Request {
				type reader struct{ io.ReadCloser }
				req := must(http.NewRequest("POST", "https://example.tld/", reader{}))
				req.ContentLength = 10
				return req
			}),
			DefaultUserAgent: "default-user-agent",
		},
		want: EncodeHeadersResult{
			HasBody:     true,
			HasTrailers: false,
		},
		wantHeaders: []header{
			{":authority", "example.tld"},
			{":method", "POST"},
			{":path", "/"},
			{":scheme", "https"},
			{"accept-encoding", "gzip"},
			{"user-agent", "default-user-agent"},
			{"content-length", "10"},
		},
	}, {
		name: "post with unknown Content-Length",
		in: EncodeHeadersParam{
			Request: newReq(func() *http.Request {
				type reader struct{ io.ReadCloser }
				req := must(http.NewRequest("POST", "https://example.tld/", reader{}))
				return req
			}),
			DefaultUserAgent: "default-user-agent",
		},
		want: EncodeHeadersResult{
			HasBody:     true,
			HasTrailers: false,
		},
		wantHeaders: []header{
			{":authority", "example.tld"},
			{":method", "POST"},
			{":path", "/"},
			{":scheme", "https"},
			{"accept-encoding", "gzip"},
			{"user-agent", "default-user-agent"},
		},
	}, {
		name: "explicit accept-encoding",
		in: EncodeHeadersParam{
			Request: newReq(func() *http.Request {
				req := must(http.NewRequest("GET", "https://example.tld/", nil))
				req.Header.Set("Accept-Encoding", "deflate")
				return req
			}),
			DefaultUserAgent: "default-user-agent",
		},
		want: EncodeHeadersResult{
			HasBody:     false,
			HasTrailers: false,
		},
		wantHeaders: []header{
			{":authority", "example.tld"},
			{":method", "GET"},
			{":path", "/"},
			{":scheme", "https"},
			{"accept-encoding", "deflate"},
			{"user-agent", "default-user-agent"},
		},
	}, {
		name: "head request",
		in: EncodeHeadersParam{
			Request: newReq(func() *http.Request {
				return must(http.NewRequest("HEAD", "https://example.tld/", nil))
			}),
			DefaultUserAgent: "default-user-agent",
		},
		want: EncodeHeadersResult{
			HasBody:     false,
			HasTrailers: false,
		},
		wantHeaders: []header{
			{":authority", "example.tld"},
			{":method", "HEAD"},
			{":path", "/"},
			{":scheme", "https"},
			{"user-agent", "default-user-agent"},
		},
	}, {
		name: "range request",
		in: EncodeHeadersParam{
			Request: newReq(func() *http.Request {
				req := must(http.NewRequest("HEAD", "https://example.tld/", nil))
				req.Header.Set("Range", "bytes=0-10")
				return req
			}),
			DefaultUserAgent: "default-user-agent",
		},
		want: EncodeHeadersResult{
			HasBody:     false,
			HasTrailers: false,
		},
		wantHeaders: []header{
			{":authority", "example.tld"},
			{":method", "HEAD"},
			{":path", "/"},
			{":scheme", "https"},
			{"user-agent", "default-user-agent"},
			{"range", "bytes=0-10"},
		},
	}} {
		t.Run(test.name, func(t *testing.T) {
			var gotHeaders []header
			if IsRequestGzip(test.in.Request.Method, test.in.Request.Header, test.disableCompression) {
				test.in.AddGzipHeader = true
			}

			got, err := EncodeHeaders(context.Background(), test.in, func(name, value string) {
				gotHeaders = append(gotHeaders, header{name, value})
			})
			if err != nil {
				t.Fatalf("EncodeHeaders = %v", err)
			}
			if got.HasBody != test.want.HasBody {
				t.Errorf("HasBody = %v, want %v", got.HasBody, test.want.HasBody)
			}
			if got.HasTrailers != test.want.HasTrailers {
				t.Errorf("HasTrailers = %v, want %v", got.HasTrailers, test.want.HasTrailers)
			}
			cmpHeader := func(a, b header) int {
				return cmp.Or(
					cmp.Compare(a.name, b.name),
					cmp.Compare(a.value, b.value),
				)
			}
			slices.SortFunc(gotHeaders, cmpHeader)
			slices.SortFunc(test.wantHeaders, cmpHeader)
			if !slices.Equal(gotHeaders, test.wantHeaders) {
				t.Errorf("got headers:")
				for _, h := range gotHeaders {
					t.Errorf("  %v: %q", h.name, h.value)
				}
				t.Errorf("want headers:")
				for _, h := range test.wantHeaders {
					t.Errorf("  %v: %q", h.name, h.value)
				}
			}
		})
	}
}

func TestEncodeHeaderErrors(t *testing.T) {
	for _, test := range []struct {
		name string
		in   EncodeHeadersParam
		want string
	}{{
		name: "URL is nil",
		in: EncodeHeadersParam{
			Request: newReq(func() *http.Request {
				req := must(http.NewRequest("GET", "https://example.tld/", nil))
				req.URL = nil
				return req
			}),
		},
		want: "URL is nil",
	}, {
		name: "upgrade header is set",
		in: EncodeHeadersParam{
			Request: newReq(func() *http.Request {
				req := must(http.NewRequest("GET", "https://example.tld/", nil))
				req.Header.Set("Upgrade", "foo")
				return req
			}),
		},
		want: "Upgrade",
	}, {
		name: "unsupported transfer-encoding header",
		in: EncodeHeadersParam{
			Request: newReq(func() *http.Request {
				req := must(http.NewRequest("GET", "https://example.tld/", nil))
				req.Header.Set("Transfer-Encoding", "identity")
				return req
			}),
		},
		want: "Transfer-Encoding",
	}, {
		name: "unsupported connection header",
		in: EncodeHeadersParam{
			Request: newReq(func() *http.Request {
				req := must(http.NewRequest("GET", "https://example.tld/", nil))
				req.Header.Set("Connection", "x")
				return req
			}),
		},
		want: "Connection",
	}, {
		name: "invalid host",
		in: EncodeHeadersParam{
			Request: newReq(func() *http.Request {
				req := must(http.NewRequest("GET", "https://example.tld/", nil))
				req.Host = "\x00.tld"
				return req
			}),
		},
		want: "Host",
	}, {
		name: "protocol header is set",
		in: EncodeHeadersParam{
			Request: newReq(func() *http.Request {
				req := must(http.NewRequest("GET", "https://example.tld/", nil))
				req.Header.Set(":protocol", "foo")
				return req
			}),
		},
		want: ":protocol",
	}, {
		name: "invalid path",
		in: EncodeHeadersParam{
			Request: newReq(func() *http.Request {
				req := must(http.NewRequest("GET", "https://example.tld/", nil))
				req.URL.Path = "no_leading_slash"
				return req
			}),
		},
		want: "path",
	}, {
		name: "invalid header name",
		in: EncodeHeadersParam{
			Request: newReq(func() *http.Request {
				req := must(http.NewRequest("GET", "https://example.tld/", nil))
				req.Header.Set("x\ny", "foo")
				return req
			}),
		},
		want: "header",
	}, {
		name: "invalid header value",
		in: EncodeHeadersParam{
			Request: newReq(func() *http.Request {
				req := must(http.NewRequest("GET", "https://example.tld/", nil))
				req.Header.Set("x", "foo\nbar")
				return req
			}),
		},
		want: "header",
	}, {
		name: "invalid trailer",
		in: EncodeHeadersParam{
			Request: newReq(func() *http.Request {
				req := must(http.NewRequest("GET", "https://example.tld/", nil))
				req.Trailer = make(http.Header)
				req.Trailer.Set("x\ny", "foo")
				return req
			}),
		},
		want: "trailer",
	}, {
		name: "transfer-encoding trailer",
		in: EncodeHeadersParam{
			Request: newReq(func() *http.Request {
				req := must(http.NewRequest("GET", "https://example.tld/", nil))
				req.Trailer = make(http.Header)
				req.Trailer.Set("Transfer-Encoding", "chunked")
				return req
			}),
		},
		want: "Trailer",
	}, {
		name: "trailer trailer",
		in: EncodeHeadersParam{
			Request: newReq(func() *http.Request {
				req := must(http.NewRequest("GET", "https://example.tld/", nil))
				req.Trailer = make(http.Header)
				req.Trailer.Set("Trailer", "chunked")
				return req
			}),
		},
		want: "Trailer",
	}, {
		name: "content-length trailer",
		in: EncodeHeadersParam{
			Request: newReq(func() *http.Request {
				req := must(http.NewRequest("GET", "https://example.tld/", nil))
				req.Trailer = make(http.Header)
				req.Trailer.Set("Content-Length", "0")
				return req
			}),
		},
		want: "Trailer",
	}, {
		name: "too many headers",
		in: EncodeHeadersParam{
			Request: newReq(func() *http.Request {
				req := must(http.NewRequest("GET", "https://example.tld/", nil))
				req.Header.Set("X-Foo", strings.Repeat("x", 1000))
				return req
			}),
			PeerMaxHeaderListSize: 1000,
		},
		want: "limit",
	}} {
		t.Run(test.name, func(t *testing.T) {
			_, err := EncodeHeaders(context.Background(), test.in, func(name, value string) {})
			if err == nil {
				t.Fatalf("EncodeHeaders = nil, want %q", test.want)
			}
			if !strings.Contains(err.Error(), test.want) {
				t.Fatalf("EncodeHeaders = %q, want error containing %q", err, test.want)
			}
		})
	}
}

func newReq(f func() *http.Request) Request {
	req := f()
	contentLength := req.ContentLength
	if req.Body == nil || req.Body == http.NoBody {
		contentLength = 0
	} else if contentLength == 0 {
		contentLength = -1
	}
	return Request{
		Header:              req.Header,
		Trailer:             req.Trailer,
		URL:                 req.URL,
		Host:                req.Host,
		Method:              req.Method,
		ActualContentLength: contentLength,
	}
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}
