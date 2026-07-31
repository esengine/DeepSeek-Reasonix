// Copyright 2024 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package http3

import (
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/netip"
	"net/url"
	"reflect"
	"slices"
	"strconv"
	"testing"
	"testing/synctest"
	"time"

	"golang.org/x/net/internal/quic/quicwire"
	"golang.org/x/net/quic"
)

// requestHeader is a helper function to make sure that all required
// pseudo-headers exist in an http.Header used for a request. Per
// https://www.rfc-editor.org/rfc/rfc9114.html#name-request-pseudo-header-field:
// "All HTTP/3 requests MUST include exactly one value for the :method,
// :scheme, and :path pseudo-header fields, unless the request is a CONNECT
// request;"
func requestHeader(h http.Header) http.Header {
	minimalHeader := http.Header{
		":method": {"GET"},
		":scheme": {"https"},
		":path":   {"/"},
	}
	maps.Copy(minimalHeader, h)
	return minimalHeader
}

func TestServerReceivePushStream(t *testing.T) {
	// "[...] if a server receives a client-initiated push stream,
	// this MUST be treated as a connection error of type H3_STREAM_CREATION_ERROR."
	// https://www.rfc-editor.org/rfc/rfc9114.html#section-6.2.2-3
	synctest.Test(t, func(t *testing.T) {
		ts := newTestServer(t, nil)
		tc := ts.connect()
		tc.newStream(streamTypePush)
		tc.wantClosed("invalid client-created push stream", errH3StreamCreationError)
	})
}

func TestServerCancelPushForUnsentPromise(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ts := newTestServer(t, nil)
		tc := ts.connect()
		tc.greet()

		const pushID = 100
		tc.control.writeVarint(int64(frameTypeCancelPush))
		tc.control.writeVarint(int64(quicwire.SizeVarint(pushID)))
		tc.control.writeVarint(pushID)
		tc.control.Flush()

		tc.wantClosed("client canceled never-sent push ID", errH3IDError)
	})
}

func TestServerHeader(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ts := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := w.Header()
			for key, values := range r.Header {
				for _, value := range values {
					header.Add(key, value)
				}
			}
			w.WriteHeader(204)
		}))
		tc := ts.connect()
		tc.greet()

		reqStream := tc.newStream(streamTypeRequest)
		reqStream.writeHeaders(requestHeader(http.Header{
			"header-from-client": {"that", "should", "be", "echoed"},
		}))
		reqStream.wantSomeHeaders(http.Header{
			":status":            {"204"},
			"Header-From-Client": {"that", "should", "be", "echoed"},
		})
		reqStream.wantClosed("request is complete")
	})
}

func TestServerHeaderSnapshot(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ts := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Test-Header", "original")
			w.WriteHeader(200)
			w.Header().Set("X-Test-Header", "modified")
			w.Write([]byte("body"))
		}))
		tc := ts.connect()
		tc.greet()

		reqStream := tc.newStream(streamTypeRequest)
		reqStream.writeHeaders(requestHeader(nil))
		reqStream.wantSomeHeaders(http.Header{
			":status":       {"200"},
			"X-Test-Header": {"original"},
		})
		reqStream.wantData([]byte("body"))
		reqStream.wantClosed("request is complete")
	})
}

func TestServerHeaderInvalid(t *testing.T) {
	tests := []struct {
		name      string
		header    http.Header
		wantError bool
	}{
		{
			name:      "header name with control character",
			header:    http.Header{"name\nevilinjection": {"Value"}},
			wantError: true,
		},
		{
			name:      "header name with uppercase character",
			header:    http.Header{"nAme": {"Value"}},
			wantError: true,
		},
		{
			name:      "pseudo-header name with control character",
			header:    http.Header{":path\nevilinjection": {"Value"}},
			wantError: true,
		},
		{
			name:      "pseudo-header name with uppercase character",
			header:    http.Header{":meThod": {"Value"}},
			wantError: true,
		},
		{
			name:      "header value with control character",
			header:    http.Header{"name": {"Value\nEvilInjection"}},
			wantError: true,
		},
		{
			name:      "pseudo-header value with control character",
			header:    http.Header{":method": {"Value\nEvilInjection"}},
			wantError: true,
		},
		{
			name:      "connection header name",
			header:    http.Header{"connection": {"foo"}},
			wantError: true,
		},
		{
			name:      "keep-alive header name",
			header:    http.Header{"Keep-Alive": {"foo"}},
			wantError: true,
		},
		{
			name:      "proxy-connection header name",
			header:    http.Header{"proxy-connection": {"foo"}},
			wantError: true,
		},
		{
			name:      "transfer-encoding header name",
			header:    http.Header{"transfer-encoding": {"foo"}},
			wantError: true,
		},
		{
			name:      "upgrade header name",
			header:    http.Header{"upgrade": {"foo"}},
			wantError: true,
		},
		{
			name:      "te header name",
			header:    http.Header{"te": {"foo"}},
			wantError: true,
		},
		{
			name:      "te header name with trailers value",
			header:    http.Header{"te": {"trailers"}},
			wantError: false,
		},
	}
	for _, tt := range tests {
		synctestSubtest(t, tt.name, func(t *testing.T) {
			body := []byte("some data")
			ts := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Write(body)
			}))
			tc := ts.connect()
			tc.greet()

			reqStream := tc.newStream(streamTypeRequest)
			reqStream.writeHeadersRaw(requestHeader(tt.header))

			if tt.wantError {
				reqStream.wantError(quic.StreamErrorCode(errH3MessageError))
			} else {
				reqStream.wantHeaders(nil)
				reqStream.wantData(body)
				reqStream.wantClosed("request is complete")
			}
		})
	}
}

func TestServerPseudoHeader(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ts := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Pseudo-headers from client request should populate a specific
			// field in http.Request, and should not be part of http.Request.Header.
			if len(r.Header) != 0 {
				t.Errorf("got %v, want request header to be empty", r.Header)
			}
			if r.Method != "GET" {
				t.Errorf("got %v, want GET method", r.Method)
			}
			if r.Host != "fake.tld:1234" {
				t.Errorf("got %v, want fake.tld:1234", r.Host)
			}
			wantURL := &url.URL{
				Path:     "/some/path",
				RawQuery: "query=value&query2=value2#fragment",
			}
			if !reflect.DeepEqual(r.URL, wantURL) {
				t.Errorf("got %v, want URL to be %v", r.URL, wantURL)
			}

			// Conversely, server should not be able to set pseudo-headers by
			// writing to the ResponseWriter's Header.
			header := w.Header()
			header.Add(":status", "123")
			w.WriteHeader(321)
		}))
		tc := ts.connect()
		tc.greet()

		reqStream := tc.newStream(streamTypeRequest)
		reqStream.writeHeaders(http.Header{
			":method":    {"GET"},
			":authority": {"fake.tld:1234"},
			":scheme":    {"https"},
			":path":      {"/some/path?query=value&query2=value2#fragment"},
		})
		reqStream.wantSomeHeaders(http.Header{":status": {"321"}})
		reqStream.wantClosed("request is complete")

		reqStream = tc.newStream(streamTypeRequest)
		reqStream.writeHeaders(http.Header{}) // Missing pseudo-header.
		reqStream.wantError(quic.StreamErrorCode(errH3MessageError))
	})
}

func TestServerPseudoHeaderCount(t *testing.T) {
	tests := []struct {
		name      string
		header    http.Header
		wantError bool
	}{
		{
			name: "missing method pseudo-header",
			header: http.Header{
				":scheme":    {"https"},
				":path":      {"/"},
				":authority": {"fake.tld:1234"},
			},
			wantError: true,
		},
		{
			name: "valid pseudo-headers for non-CONNECT request",
			header: http.Header{
				":method": {"GET"},
				":scheme": {"https"},
				":path":   {"/"},
			},
			wantError: false,
		},
		{
			name: "extraneous pseudo-headers for non-CONNECT request",
			header: http.Header{
				":method": {"GET", "GET"}, // Duplicate :method.
				":scheme": {"https"},
				":path":   {"/"},
			},
			wantError: true,
		},
		{
			name: "missing pseudo-headers for non-CONNECT request",
			header: http.Header{
				":method": {"GET", "GET"},
				":path":   {"/"},
			},
			wantError: true,
		},
		{
			name: "valid pseudo-headers for CONNECT request",
			header: http.Header{
				":method":    {"CONNECT"},
				":authority": {"fake.tld:1234"},
			},
			wantError: false,
		},
		{
			name: "extraneous pseudo-headers for CONNECT request",
			header: http.Header{
				":method":    {"CONNECT"},
				":authority": {"fake.tld:1234"},
				":path":      {"/"}, // :path should be omitted.
			},
			wantError: true,
		},
		{
			name: "missing pseudo-headers for CONNECT request",
			header: http.Header{
				":method": {"CONNECT"},
			},
			wantError: true,
		},
	}
	for _, tt := range tests {
		synctestSubtest(t, tt.name, func(t *testing.T) {
			body := []byte("some data")
			ts := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Write(body)
			}))
			tc := ts.connect()
			tc.greet()

			reqStream := tc.newStream(streamTypeRequest)
			reqStream.writeHeaders(tt.header)

			if tt.wantError {
				reqStream.wantError(quic.StreamErrorCode(errH3MessageError))
			} else {
				reqStream.wantHeaders(nil)
				reqStream.wantData(body)
				reqStream.wantClosed("request is complete")
			}
		})
	}
}

func TestServerInvalidHeader(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ts := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Add("valid-name", "valid value")
			// Invalid headers are skipped.
			w.Header().Add("invalid name with spaces", "some value")
			w.Header().Add("some-name", "invalid value with \n")
			w.Header().Add("valid-name-2", "valid value 2")
			w.WriteHeader(200)
		}))
		tc := ts.connect()
		tc.greet()

		reqStream := tc.newStream(streamTypeRequest)
		reqStream.writeHeaders(requestHeader(nil))
		reqStream.wantSomeHeaders(http.Header{
			":status":      {"200"},
			"Valid-Name":   {"valid value"},
			"Valid-Name-2": {"valid value 2"},
		})
		reqStream.wantClosed("request is complete")
	})
}

func TestServerInvalidStatus(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		gotpanic := make(chan bool)
		ts := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer close(gotpanic)
			defer func() {
				if e := recover(); e != nil {
					got := fmt.Sprintf("%T, %v", e, e)
					want := "string, invalid WriteHeader code 0"
					if got != want {
						t.Errorf("unexpected panic value:\n got: %v\nwant: %v\n", got, want)
					}
					gotpanic <- true
					// Set an explicit 503. This also tests that the
					// WriteHeader call panics before it recorded that an
					// explicit value was set.
					w.WriteHeader(503)

					// Verify that writing invalid status will not panic if a
					// status is already set anyways.
					w.WriteHeader(0)
				}
			}()
			w.WriteHeader(0) // Invalid. Will panic.
		}))
		tc := ts.connect()
		tc.greet()

		reqStream := tc.newStream(streamTypeRequest)
		reqStream.writeHeaders(requestHeader(nil))
		if !<-gotpanic {
			t.Error("expected panic in handler")
		}
		synctest.Wait()
		reqStream.wantSomeHeaders(http.Header{
			":status": {"503"},
		})
		reqStream.wantClosed("request is complete")
	})
}

func TestServerBody(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ts := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatal(err)
			}
			w.Write([]byte(r.URL.Path)) // Implicitly calls w.WriteHeader(200).
			w.Write(body)
		}))
		tc := ts.connect()
		tc.greet()

		reqStream := tc.newStream(streamTypeRequest)
		reqStream.writeHeaders(requestHeader(nil))
		bodyContent := []byte("some body content that should be echoed")
		reqStream.writeData(bodyContent)
		reqStream.stream.stream.CloseWrite()
		reqStream.wantSomeHeaders(http.Header{":status": {"200"}})
		// Small multiple calls to Write will be coalesced into one DATA frame.
		reqStream.wantData(append([]byte("/"), bodyContent...))
		reqStream.wantClosed("request is complete")
	})
}

func TestServerHeadResponseNoBody(t *testing.T) {
	bodyContent := []byte("response body that will not be sent for HEAD requests")
	synctest.Test(t, func(t *testing.T) {
		ts := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write(bodyContent)
		}))
		tc := ts.connect()
		tc.greet()

		reqStream := tc.newStream(streamTypeRequest)
		reqStream.writeHeaders(requestHeader(nil))
		reqStream.wantSomeHeaders(http.Header{":status": {"200"}})
		reqStream.wantData(bodyContent)
		reqStream.wantClosed("request is complete")

		reqStream = tc.newStream(streamTypeRequest)
		reqStream.writeHeaders(requestHeader(http.Header{":method": {http.MethodHead}}))
		reqStream.wantSomeHeaders(http.Header{":status": {"200"}})
		reqStream.wantClosed("request is complete")
	})
}

func TestServerShutdownGoaway(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ts := newTestServer(t, nil)

		tc := ts.connect()
		tc.greet()
		tc.wantNotClosed("after initial connection handshake")

		requestCount := int64(5)
		for range requestCount {
			tc.newStream(streamTypeRequest).writeHeaders(requestHeader(nil))
		}

		control := tc.wantStream(streamTypeControl)
		control.wantSettings(nil)

		shutdownComplete := make(chan any)
		go func() {
			ts.s.shutdown(t.Context())
			shutdownComplete <- struct{}{}
		}()
		control.wantGoaway((requestCount - 1) * 4) // Request stream ID goes from 0, 4, 8, ...
		<-shutdownComplete
	})
}

func TestServerHandlerEmpty(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ts := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Empty handler should return a 200 OK
		}))
		tc := ts.connect()
		tc.greet()

		reqStream := tc.newStream(streamTypeRequest)
		reqStream.writeHeaders(requestHeader(nil))
		reqStream.wantSomeHeaders(http.Header{":status": {"200"}})
		reqStream.wantClosed("request is complete")
	})
}

func TestServerHandlerFlushing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ts := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(time.Second)
			w.Write([]byte("first"))

			time.Sleep(time.Second)
			w.Write([]byte("second"))
			w.(http.Flusher).Flush()

			time.Sleep(time.Second)
			w.Write([]byte("third"))
		}))
		tc := ts.connect()
		tc.greet()

		reqStream := tc.newStream(streamTypeRequest)
		reqStream.writeHeaders(requestHeader(nil))
		respBody := make([]byte, 100)

		time.Sleep(time.Second)
		synctest.Wait()
		if n, err := reqStream.Read(respBody); err == nil {
			t.Errorf("got %v bytes read, want no message yet", n)
		}

		time.Sleep(time.Second)
		synctest.Wait()
		if _, err := reqStream.Read(respBody); err != nil {
			t.Errorf("failed to read partial response from server, got err: %v", err)
		}

		time.Sleep(time.Second)
		synctest.Wait()
		if _, err := reqStream.Read(respBody); err != io.EOF {
			t.Errorf("got err %v, want EOF", err)
		}
		reqStream.wantClosed("request is complete")
	})
}

func TestServerHandlerStreaming(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		stream := make(chan string)
		ts := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Flushing when we have not written anything yet implicitly calls
			// w.WriteHeader(200).
			w.(http.Flusher).Flush()
			for str := range stream {
				w.Write([]byte(str))
				w.(http.Flusher).Flush()
			}
		}))
		tc := ts.connect()
		tc.greet()

		reqStream := tc.newStream(streamTypeRequest)
		reqStream.writeHeaders(requestHeader(nil))
		reqStream.wantSomeHeaders(http.Header{":status": {"200"}})

		for _, data := range []string{"a", "bunch", "of", "things", "to", "stream"} {
			stream <- data
			reqStream.wantData([]byte(data))
		}
		close(stream)
		reqStream.wantClosed("request is complete")
	})
}

func TestServerHandlerTrimsContentBody(t *testing.T) {
	tests := []struct {
		name                      string
		declaredContentLen        int
		declaredInvalidContentLen bool
		actualContentLen          int
		wantTrimmed               bool
	}{
		{
			name:               "declared accurate content length",
			declaredContentLen: 100,
			actualContentLen:   100,
		},
		{
			name:               "declared larger content length",
			declaredContentLen: 100,
			actualContentLen:   10,
		},
		{
			name:               "declared smaller content length",
			declaredContentLen: 10,
			actualContentLen:   100,
			wantTrimmed:        true,
		},
		{
			name:                      "declared invalid content length",
			declaredInvalidContentLen: true,
			actualContentLen:          100,
		},
	}

	for _, tt := range tests {
		wantWrittenLen := min(tt.actualContentLen, tt.declaredContentLen)
		if tt.declaredInvalidContentLen {
			wantWrittenLen = tt.actualContentLen
		}
		synctestSubtest(t, tt.name, func(t *testing.T) {
			ts := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Length", strconv.Itoa(tt.declaredContentLen))
				if tt.declaredInvalidContentLen {
					w.Header().Set("Content-Length", "not a number, should be ignored")
				}
				var written int
				var lastErr error
				for range tt.actualContentLen {
					n, err := w.Write([]byte("a"))
					written += n
					lastErr = err
				}
				if tt.wantTrimmed != (lastErr != nil) {
					t.Errorf("got %v error when writing response body, even though wantTrimmed is %v", lastErr, tt.wantTrimmed)
				}
				if written != wantWrittenLen {
					t.Errorf("got %v bytes written by the server, want %v bytes", written, wantWrittenLen)
				}
			}))
			tc := ts.connect()
			tc.greet()

			reqStream := tc.newStream(streamTypeRequest)
			reqStream.writeHeaders(requestHeader(nil))
			reqStream.wantHeaders(nil)
			reqStream.wantData(slices.Repeat([]byte("a"), wantWrittenLen))
			reqStream.wantClosed("request is complete")
		})
	}
}

func TestServerExpect100Continue(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		streamIdle := make(chan bool)
		ts := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Expect: 100-continue header should not be accessible from the
			// server handler.
			if len(r.Header) > 0 {
				t.Errorf("got %v, want request header to be empty", r.Header)
			}
			// Reading the body will cause the server to call w.WriteHeader(100).
			<-streamIdle
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatal(err)
			}
			// Implicitly calls w.WriteHeader(200) since non-1XX status code
			// has been sent yet so far.
			w.Write(body)
		}))
		tc := ts.connect()
		tc.greet()

		// Client sends an Expect: 100-continue request.
		reqStream := tc.newStream(streamTypeRequest)
		reqStream.writeHeaders(requestHeader(http.Header{
			"expect": {"100-continue"},
		}))

		reqStream.wantIdle("stream is idle until server sends an HTTP 100 status")
		streamIdle <- true
		// Wait until server responds with HTTP status 100 before sending the
		// body.
		reqStream.wantSomeHeaders(http.Header{":status": {"100"}})
		body := []byte("body that will be echoed back if we get status 100")
		reqStream.writeData(body)
		reqStream.stream.stream.CloseWrite()

		// Receive the server's response after sending the body.
		reqStream.wantSomeHeaders(http.Header{":status": {"200"}})
		reqStream.wantData(body)
		reqStream.wantClosed("request is complete")
	})
}

func TestServerExpect100ContinueRejected(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rejectBody := []byte("not allowed")
		ts := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(403)
			w.Write(rejectBody)
		}))
		tc := ts.connect()
		tc.greet()

		// Client sends an Expect: 100-continue request.
		reqStream := tc.newStream(streamTypeRequest)
		reqStream.writeHeaders(requestHeader(http.Header{
			"expect": {"100-continue"},
		}))

		// Server rejects it.
		reqStream.wantSomeHeaders(http.Header{":status": {"403"}})
		reqStream.wantData(rejectBody)
		reqStream.wantClosed("request is complete")
	})
}

func TestServerNoExpect100ContinueAfterNormalResponse(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ts := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(200)
			w.(http.Flusher).Flush()
			// This should not cause an HTTP 100 status to be sent since we
			// have sent an HTTP 200 response already.
			io.ReadAll(r.Body)
		}))
		tc := ts.connect()
		tc.greet()

		// Client sends an Expect: 100-continue request.
		reqStream := tc.newStream(streamTypeRequest)
		reqStream.writeHeaders(requestHeader(http.Header{
			"expect": {"100-continue"},
		}))
		// Client sends a body prematurely. This should not happen, unless a
		// client misbehaves. We do so here anyways so the server handler can
		// read the request body without hanging, which would normally cause an
		// HTTP 100 to be sent.
		reqStream.writeData([]byte("some body"))
		reqStream.stream.stream.CloseWrite()

		// Verify that no HTTP 100 was sent.
		reqStream.wantSomeHeaders(http.Header{":status": {"200"}})
		reqStream.wantClosed("request is complete")
	})
}

func TestServerHandlerReadReqWithNoBody(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		serverBody := []byte("hello from server!")
		ts := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, err := io.ReadAll(r.Body); err != nil {
				t.Errorf("got %v err when reading from an empty request body, want nil", err)
			}
			w.Write(serverBody)
		}))
		tc := ts.connect()
		tc.greet()

		// Case 1: we know that there is no body / DATA frame because the
		// client closes the write direction of the stream.
		reqStream := tc.newStream(streamTypeRequest)
		reqStream.writeHeaders(requestHeader(nil))
		reqStream.stream.stream.CloseWrite()
		reqStream.wantSomeHeaders(http.Header{":status": {"200"}})
		reqStream.wantData(serverBody)
		reqStream.wantClosed("request is complete")

		// Case 2: we know that there is no body / DATA frame because the
		// client indicates a Content-Length of 0.
		reqStream = tc.newStream(streamTypeRequest)
		reqStream.writeHeaders(requestHeader(http.Header{
			"content-length": {"0"},
		}))
		reqStream.wantSomeHeaders(http.Header{":status": {"200"}})
		reqStream.wantData(serverBody)
		reqStream.wantClosed("request is complete")
	})
}

func TestServerHandlerReadTrailer(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		body := []byte("some body")
		ts := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			wantTrailer := http.Header{
				"Client-Trailer-A": nil,
				"Client-Trailer-B": nil,
			}
			if !reflect.DeepEqual(r.Trailer, wantTrailer) {
				t.Errorf("got %v; want trailer to be %v before reading the body", r.Trailer, wantTrailer)
			}
			if _, err := io.ReadAll(r.Body); err != nil {
				t.Fatal(err)
			}
			wantTrailer = http.Header{
				"Client-Trailer-A": {"valuea"},
				"Client-Trailer-B": {"valueb"},
			}
			if !reflect.DeepEqual(r.Trailer, wantTrailer) {
				t.Errorf("got %v; want trailer to be %v after reading the body", r.Trailer, wantTrailer)
			}
			w.WriteHeader(200)
		}))
		tc := ts.connect()
		tc.greet()

		reqStream := tc.newStream(streamTypeRequest)
		reqStream.writeHeaders(requestHeader(http.Header{
			"trailer": {"Client-Trailer-A, Client-Trailer-B"},
		}))
		reqStream.writeData(body)
		reqStream.writeHeaders(http.Header{
			"Client-Trailer-A": {"valuea"},
			"Client-Trailer-B": {"valueb"},
			// Server should not accept undeclared trailers.
			"Undeclared-Trailer": {"undeclared"},
		})
		reqStream.wantHeaders(nil)
		reqStream.wantClosed("request is complete")
	})
}

func TestServerHandlerReadTrailerNoBody(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ts := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			wantTrailer := http.Header{
				"Client-Trailer-A": nil,
				"Client-Trailer-B": nil,
			}
			if !reflect.DeepEqual(r.Trailer, wantTrailer) {
				t.Errorf("got %v; want trailer to be %v before reading the body", r.Trailer, wantTrailer)
			}
			if _, err := io.ReadAll(r.Body); err != nil {
				t.Fatal(err)
			}
			wantTrailer = http.Header{
				"Client-Trailer-A": {"valuea"},
				"Client-Trailer-B": {"valueb"},
			}
			if !reflect.DeepEqual(r.Trailer, wantTrailer) {
				t.Errorf("got %v; want trailer to be %v after reading the body", r.Trailer, wantTrailer)
			}
			w.WriteHeader(200)
		}))
		tc := ts.connect()
		tc.greet()

		reqStream := tc.newStream(streamTypeRequest)
		reqStream.writeHeaders(requestHeader(http.Header{
			"trailer":        {"Client-Trailer-A, Client-Trailer-B"},
			"content-length": {"0"},
		}))
		reqStream.writeHeaders(http.Header{
			"Client-Trailer-A": {"valuea"},
			"Client-Trailer-B": {"valueb"},
			// Server should not accept undeclared trailers.
			"Undeclared-Trailer": {"undeclared"},
		})
		reqStream.wantHeaders(nil)
		reqStream.wantClosed("request is complete")
	})
}

func TestServerHandlerWriteTrailer(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		body := []byte("some body")
		ts := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Trailer", "server-trailer-a, server-trailer-b") // Trailer header will be canonicalized.
			w.Header().Add("Trailer", "Server-Trailer-C")

			w.Write(body)

			w.Header().Set("server-trailer-a", "valuea") // Trailer header will be canonicalized.
			w.Header().Set("Server-Trailer-C", "valuec") // skipping B
			// Server should not send undeclared trailers, unless it has the
			// magic "Trailer:" prefix.
			w.Header().Set("Server-Trailer-Not-Declared", "should be omitted")
			w.Header().Set("Trailer:Undeclared-Trailer-Exception", "should be sent")
		}))
		tc := ts.connect()
		tc.greet()

		reqStream := tc.newStream(streamTypeRequest)
		reqStream.writeHeaders(requestHeader(nil))
		reqStream.wantSomeHeaders(http.Header{
			":status": {"200"},
			"Trailer": {"Server-Trailer-A, Server-Trailer-B, Server-Trailer-C"},
		})
		reqStream.wantData(body)
		reqStream.wantSomeHeaders(http.Header{
			"Server-Trailer-A":             {"valuea"},
			"Server-Trailer-C":             {"valuec"},
			"Undeclared-Trailer-Exception": {"should be sent"},
		})
		reqStream.wantClosed("request is complete")
	})
}

func TestServerHandlerWriteTrailerNoBody(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ts := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Trailer", "server-trailer-a, server-trailer-b") // Trailer header will be canonicalized.
			w.Header().Add("Trailer", "Server-Trailer-C")

			w.(http.Flusher).Flush()

			w.Header().Set("server-trailer-a", "valuea") // Trailer header will be canonicalized.
			w.Header().Set("Server-Trailer-C", "valuec") // skipping B
			// Server should not send undeclared trailers without "Trailer:"
			// prefix.
			w.Header().Set("Server-Trailer-Not-Declared", "should be omitted")
			w.Header().Set("Trailer:undeclared-trailer-exception", "should be sent")
		}))
		tc := ts.connect()
		tc.greet()

		reqStream := tc.newStream(streamTypeRequest)
		reqStream.writeHeaders(requestHeader(nil))
		reqStream.wantSomeHeaders(http.Header{
			":status": {"200"},
			"Trailer": {"Server-Trailer-A, Server-Trailer-B, Server-Trailer-C"},
		})
		reqStream.wantSomeHeaders(http.Header{
			"Server-Trailer-A":             {"valuea"},
			"Server-Trailer-C":             {"valuec"},
			"Undeclared-Trailer-Exception": {"should be sent"},
		})
		reqStream.wantClosed("request is complete")
	})
}

func TestServerInfersHeaders(t *testing.T) {
	tests := []struct {
		name            string
		flushedEarly    bool
		responseStatus  int
		does100Continue bool
		declaredHeader  http.Header
		want            http.Header
	}{
		{
			name:           "infers undeclared headers",
			responseStatus: 200,
			declaredHeader: http.Header{
				"Some-Other-Header": {"some value"},
			},
			want: http.Header{
				"Date":              {"Sat, 01 Jan 2000 00:00:00 GMT"}, // Synctest starting time.
				"Content-Type":      {"text/html; charset=utf-8"},
				"Some-Other-Header": {"some value"},
			},
		},
		{
			name:           "does not write over declared header",
			responseStatus: 200,
			declaredHeader: http.Header{
				"Date":              {"some date"},
				"Content-Type":      {"some content type"},
				"Some-Other-Header": {"some value"},
			},
			want: http.Header{
				"Date":              {"some date"},
				"Content-Type":      {"some content type"},
				"Some-Other-Header": {"some value"},
			},
		},
		{
			name:           "does not infer content type for response with no body",
			responseStatus: 304, // 304 status response has no body.
			declaredHeader: http.Header{
				"Some-Other-Header": {"some value"},
			},
			want: http.Header{
				"Date":              {"Sat, 01 Jan 2000 00:00:00 GMT"}, // Synctest starting time.
				"Some-Other-Header": {"some value"},
			},
		},
		{
			// See golang.org/issue/31753.
			name:           "does not infer content type for response with declared content encoding",
			responseStatus: 200,
			declaredHeader: http.Header{
				"Content-Encoding":  {"some encoding"},
				"Some-Other-Header": {"some value"},
			},
			want: http.Header{
				"Date":              {"Sat, 01 Jan 2000 00:00:00 GMT"}, // Synctest starting time.
				"Content-Encoding":  {"some encoding"},
				"Some-Other-Header": {"some value"},
			},
		},
		{
			name:           "does not infer content type when header is flushed before body is written",
			responseStatus: 200,
			flushedEarly:   true,
			declaredHeader: http.Header{
				"Some-Other-Header": {"some value"},
			},
			want: http.Header{
				"Date":              {"Sat, 01 Jan 2000 00:00:00 GMT"}, // Synctest starting time.
				"Some-Other-Header": {"some value"},
			},
		},
		{
			name:            "infers header for the header that comes after 100 continue",
			responseStatus:  200,
			does100Continue: true,
			declaredHeader: http.Header{
				"Some-Other-Header": {"some value"},
			},
			want: http.Header{
				"Date":              {"Sat, 01 Jan 2000 00:00:00 GMT"}, // Synctest starting time.
				"Content-Type":      {"text/html; charset=utf-8"},
				"Some-Other-Header": {"some value"},
			},
		},
	}

	for _, tt := range tests {
		synctestSubtest(t, tt.name, func(t *testing.T) {
			body := []byte("<html>some html content</html>")
			streamIdle := make(chan bool)
			ts := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tt.does100Continue {
					<-streamIdle
					io.ReadAll(r.Body)
				}
				for name, values := range tt.declaredHeader {
					for _, value := range values {
						w.Header().Add(name, value)
					}
				}
				w.WriteHeader(tt.responseStatus)
				if tt.flushedEarly {
					w.(http.Flusher).Flush()
				}
				// Write the body one byte at a time. To confirm that body
				// writes are buffered and that Content-Type will not be
				// wrongly identified as text/plain rather than text/html.
				for _, b := range body {
					w.Write([]byte{b})
				}
			}))
			tc := ts.connect()
			tc.greet()

			reqStream := tc.newStream(streamTypeRequest)

			if tt.does100Continue {
				reqStream.writeHeaders(requestHeader(http.Header{
					"expect": {"100-continue"},
				}))
				reqStream.wantIdle("stream is idle until server sends an HTTP 100 status")
				streamIdle <- true
				reqStream.wantHeaders(http.Header{":status": {"100"}})
			}

			reqStream.writeHeaders(requestHeader(nil))
			tt.want.Add(":status", strconv.Itoa(tt.responseStatus))
			reqStream.wantHeaders(tt.want)
			if responseCanHaveBody(tt.responseStatus) {
				reqStream.wantData(body)
			}
			reqStream.wantClosed("request is complete")
		})
	}
}

func TestServerBuffersBodyWrite(t *testing.T) {
	tests := []struct {
		name      string
		bodyLen   int
		writeSize int
		flushes   bool
	}{
		{
			name:      "buffers small body content",
			bodyLen:   defaultBodyBufferCap * 10,
			writeSize: 5,
			flushes:   false,
		},
		{
			name:      "does not buffer large body content",
			bodyLen:   defaultBodyBufferCap * 10,
			writeSize: defaultBodyBufferCap * 2,
			flushes:   false,
		},
		{
			name:      "does not buffer flushed body content",
			bodyLen:   defaultBodyBufferCap * 10,
			writeSize: 10,
			flushes:   true,
		},
	}
	for _, tt := range tests {
		synctestSubtest(t, tt.name, func(t *testing.T) {
			ts := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				for n := 0; n < tt.bodyLen; n += tt.writeSize {
					data := slices.Repeat([]byte("a"), min(tt.writeSize, tt.bodyLen-n))
					n, err := w.Write(data)
					if err != nil {
						t.Fatal(err)
					}
					if n != len(data) {
						t.Errorf("got %v bytes when writing in server handler, want %v", n, len(data))
					}
					if tt.flushes {
						w.(http.Flusher).Flush()
					}
				}
			}))
			tc := ts.connect()
			tc.greet()

			reqStream := tc.newStream(streamTypeRequest)
			reqStream.writeHeaders(requestHeader(nil))
			reqStream.wantHeaders(nil)
			switch {
			case tt.writeSize > defaultBodyBufferCap:
				// After using the buffer once, it is no longer used since the
				// writeSize is larger than the buffer.
				for n := 0; n < tt.bodyLen; n += tt.writeSize {
					reqStream.wantData(slices.Repeat([]byte("a"), min(tt.writeSize, tt.bodyLen-n)))
				}
			case tt.flushes:
				for n := 0; n < tt.bodyLen; n += tt.writeSize {
					reqStream.wantData(slices.Repeat([]byte("a"), min(tt.writeSize, tt.bodyLen-n)))
				}
			case tt.writeSize <= defaultBodyBufferCap:
				dataLen := defaultBodyBufferCap + tt.writeSize - (defaultBodyBufferCap % tt.writeSize)
				for n := 0; n < tt.bodyLen; n += dataLen {
					reqStream.wantData(slices.Repeat([]byte("a"), min(dataLen, tt.bodyLen-n)))
				}
			}
			reqStream.wantClosed("request is complete")
		})
	}
}

func TestServer103EarlyHints(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		body := []byte("some body")
		ts := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()

			h.Add("Content-Length", "123") // Must be ignored
			h.Add("Link", "</style.css>; rel=preload; as=style")
			h.Add("Link", "</script.js>; rel=preload; as=script")
			w.WriteHeader(http.StatusEarlyHints)

			h.Add("Link", "</foo.js>; rel=preload; as=script")
			w.WriteHeader(http.StatusEarlyHints)

			w.Write(body)                        // Implicitly sends status 200.
			w.WriteHeader(http.StatusEarlyHints) // Should be a no-op.
		}))
		tc := ts.connect()
		tc.greet()

		reqStream := tc.newStream(streamTypeRequest)
		reqStream.writeHeaders(requestHeader(nil))
		reqStream.wantHeaders(http.Header{
			":status": {"103"},
			"Link": {
				"</style.css>; rel=preload; as=style",
				"</script.js>; rel=preload; as=script",
			},
		})
		reqStream.wantHeaders(http.Header{
			":status": {"103"},
			"Link": {
				"</style.css>; rel=preload; as=style",
				"</script.js>; rel=preload; as=script",
				"</foo.js>; rel=preload; as=script",
			},
		})
		reqStream.wantSomeHeaders(http.Header{
			":status":        {"200"},
			"Content-Length": {"123"},
		})
		reqStream.wantData(body)
		reqStream.wantClosed("request is complete")
	})
}

func TestServer304NotModified(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ts := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotModified)
			if _, err := w.Write([]byte("body should not be allowed")); !errors.Is(err, http.ErrBodyNotAllowed) {
				t.Errorf("got %v error when calling Write after WriteHeader(304), want %v error", err, http.ErrBodyNotAllowed)
			}
		}))
		tc := ts.connect()
		tc.greet()

		reqStream := tc.newStream(streamTypeRequest)
		reqStream.writeHeaders(requestHeader(nil))
		reqStream.wantSomeHeaders(http.Header{":status": {"304"}})
		reqStream.wantClosed("request is complete")
	})
}

func TestServerInvalidPathHeader(t *testing.T) {
	for _, test := range []struct {
		name string
		path string
	}{{
		name: "empty",
		path: "",
	}, {
		name: "invalid char",
		path: "\x00",
	}, {
		name: "absolute url",
		path: "https://example.com/",
	}} {
		synctestSubtest(t, test.name, func(t *testing.T) {
			ts := newTestServer(t, nil)
			tc := ts.connect()
			tc.greet()

			reqStream := tc.newStream(streamTypeRequest)
			reqStream.writeHeaders(requestHeader(http.Header{
				":path": []string{test.path},
			}))
			reqStream.wantError(quic.StreamErrorCode(errH3MessageError))
		})
	}
}

func TestServerOptionsMethod(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ts := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		tc := ts.connect()
		tc.greet()

		reqStream := tc.newStream(streamTypeRequest)
		reqStream.writeHeaders(requestHeader(http.Header{
			":method": []string{"OPTIONS"},
			":path":   []string{"*"},
		}))
		reqStream.wantSomeHeaders(http.Header{
			":status": {"200"},
		})
	})
}

type testServer struct {
	t  testing.TB
	s  *server
	tn testNet
	*testQUICEndpoint

	addr netip.AddrPort
}

type testQUICEndpoint struct {
	t testing.TB
	e *quic.Endpoint
}

type testServerConn struct {
	ts *testServer

	*testQUICConn
	control *testQUICStream
}

func newTestServer(t testing.TB, handler http.Handler) *testServer {
	t.Helper()
	ts := &testServer{
		t: t,
		s: &server{
			config: &quic.Config{
				TLSConfig: testTLSConfig,
			},
			handler: handler,
		},
	}
	e := ts.tn.newQUICEndpoint(t, ts.s.config)
	ts.addr = e.LocalAddr()
	go ts.s.serve(e)
	return ts
}

func (ts *testServer) connect() *testServerConn {
	ts.t.Helper()
	config := &quic.Config{TLSConfig: testTLSConfig}
	e := ts.tn.newQUICEndpoint(ts.t, nil)
	qconn, err := e.Dial(ts.t.Context(), "udp", ts.addr.String(), config)
	if err != nil {
		ts.t.Fatal(err)
	}
	tc := &testServerConn{
		ts:           ts,
		testQUICConn: newTestQUICConn(ts.t, qconn),
	}
	synctest.Wait()
	return tc
}

// greet performs initial connection handshaking with the server.
func (tc *testServerConn) greet() {
	// Client creates a control stream.
	tc.control = tc.newStream(streamTypeControl)
	tc.control.writeVarint(int64(frameTypeSettings))
	tc.control.writeVarint(0) // size
	tc.control.Flush()
	synctest.Wait()
}
