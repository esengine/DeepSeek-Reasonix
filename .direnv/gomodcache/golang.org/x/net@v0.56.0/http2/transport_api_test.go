// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// This file tests the exported, user-facing parts of Transport.
//
// These tests verify that Transport behavior is consistent when using either the
// HTTP/2 implementation in this package (x/net/http2), or when using the
// implementation in net/http/internal/http2.

package http2_test

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"golang.org/x/net/http2"
)

func synctestTestRoundTrip(t *testing.T, f func(t *testing.T, mode roundTripTestMode)) {
	t.Run("netHTTP", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			f(t, roundTripNetHTTP)
		})
	})
	t.Run("xNetHTTP2", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			f(t, roundTripXNetHTTP2)
		})
	})
}

// TestAPITransportDial tests the Transport.Dial, Transport.DialTLS,
// and Transport.TLSClientConfig fields.
func TestAPITransportDial(t *testing.T) {
	t.Run("DialTLS/http", func(t *testing.T) {
		testAPITransportDial(t, "DialTLS", "http")
	})
	t.Run("DialTLS/https", func(t *testing.T) {
		testAPITransportDial(t, "DialTLS", "https")
	})
	t.Run("DialTLSContext/http", func(t *testing.T) {
		testAPITransportDial(t, "DialTLSContext", "http")
	})
	t.Run("DialTLSContext/https", func(t *testing.T) {
		testAPITransportDial(t, "DialTLSContext", "https")
	})
}
func testAPITransportDial(t *testing.T, name, proto string) {
	synctestTestRoundTrip(t, func(t *testing.T, mode roundTripTestMode) {
		const serverName = "server.tld"
		var (
			dialCalled             = false
			dialServerName         = ""
			verifyConnectionCalled = false
			tt                     *testTransport
		)
		dialFunc := func(network, address string, tlsConf *tls.Config) (net.Conn, error) {
			dialCalled = true
			dialServerName = tlsConf.ServerName
			if got, want := tlsConf.NextProtos, []string{"h2"}; !slices.Equal(got, want) {
				t.Errorf("tls.Config.NextProtos = %q, want %q", got, want)
			}
			conn := tt.li.newConn()
			switch proto {
			case "http":
				return conn, nil
			case "https":
				tlsConn := tls.Client(conn, tlsConf)
				if err := tlsConn.Handshake(); err != nil {
					t.Errorf("client TLS handshake: %v", err)
				}
				return tlsConn, nil
			default:
				panic("unknown proto: " + proto)
			}
		}
		tt = newTestTransport(t, mode, func(tr2 *http2.Transport) {
			tr2.TLSClientConfig = &tls.Config{
				InsecureSkipVerify: true,
				VerifyConnection: func(tls.ConnectionState) error {
					// We check to see if the Transport's TLSClientConfig
					// was used by observing if VerifyConnection was called.
					verifyConnectionCalled = true
					return nil
				},
			}

			switch name {
			case "DialTLS":
				tr2.DialTLS = dialFunc
				tr2.DialTLSContext = nil
			case "DialTLSContext":
				tr2.DialTLS = nil
				tr2.DialTLSContext = func(ctx context.Context, network, address string, tlsConf *tls.Config) (net.Conn, error) {
					return dialFunc(network, address, tlsConf)
				}
			default:
				panic("unknown func: " + name)
			}
			if proto == "http" {
				tr2.AllowHTTP = true
			}
		})
		if proto == "https" {
			tt.useTLS = true
		}

		req, _ := http.NewRequest("GET", proto+"://"+serverName+"/", nil)
		_ = tt.roundTrip(req)
		tc := tt.getConn()
		tc.wantFrameType(http2.FrameSettings)

		// When we use http.Transport.RoundTrip, it handles the dial and ignores
		// the http2.Transport's dialer.
		wantDialCalled := mode == roundTripXNetHTTP2
		if dialCalled != wantDialCalled {
			t.Errorf("Transport.%v called: %v, want %v", name, dialCalled, wantDialCalled)
		}

		// If the VerifyConnection hook is called, this indicates that we
		// correctly used the http2.Transport.TLSClientConfig.
		if got, want := verifyConnectionCalled, (proto == "https" && wantDialCalled); got != want {
			t.Errorf("TLSConfig.VerifyConnection called: %v, want %v", got, want)
		}

		// If the dial function is called, it should be provided with a *tls.Config
		// with the ServerName filled in correctly.
		if dialCalled && dialServerName != serverName {
			t.Errorf("TLSConfig.ServerName = %q, want %q", dialServerName, serverName)
		}
	})
}

// TestAPITransportDisableCompression tests the Transport.DisableCompression field.
func TestAPITransportDisableCompression(t *testing.T) {
	for _, disable := range []bool{true, false} {
		t.Run(fmt.Sprint(disable), func(t *testing.T) {
			synctestTestRoundTrip(t, func(t *testing.T, mode roundTripTestMode) {
				tc := newTestClientConn(t, mode, func(tr2 *http2.Transport) {
					tr2.DisableCompression = disable
				})
				tc.greet()

				req, _ := http.NewRequest("PUT", "https://dummy.tld/", nil)
				_ = tc.roundTrip(req)

				var want []string
				if !disable {
					want = []string{"gzip"}
				}
				tc.wantHeaders(wantHeader{
					streamID:  1,
					endStream: true,
					header: http.Header{
						"accept-encoding": want,
					},
				})
			})
		})
	}
}

// TestAPITransportAllowHTTPOff tests the Transport.AllowHTTP field.
// (It only tests AllowHTTP = false, since most other tests use AllowHTTP = true.)
func TestAPITransportAllowHTTPOff(t *testing.T) {
	synctestTestRoundTrip(t, func(t *testing.T, mode roundTripTestMode) {
		tt := newTestTransport(t, mode, func(tr2 *http2.Transport) {
			tr2.AllowHTTP = false
		})

		req, _ := http.NewRequest("GET", "http://dummy.tld/", nil)
		rt := tt.roundTrip(req)
		switch mode {
		case roundTripNetHTTP:
			// net/http.Transport doesn't respect http2.Transport.AllowHTTP.
			// When using a net/http Transport, unencrypted HTTP/2 is allowed when
			// the transport Protocols contains UnencryptedHTTP2.
			tc := tt.getConn()
			tc.wantFrameType(http2.FrameSettings)
			tc.wantFrameType(http2.FrameWindowUpdate)
			tc.wantFrameType(http2.FrameHeaders)
		case roundTripXNetHTTP2:
			// x/net/http.Transport only permits http URLs when AllowHTTP is false.
			err := rt.err()
			want := "unencrypted HTTP/2 not enabled"
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Errorf("RoundTrip = %v; want %q", err, want)
			}
		}
	})
}

// TestAPITransportMaxHeaderListSize tests the Transport.MaxHeaderListSize field.
func TestAPITransportMaxHeaderListSize(t *testing.T) {
	synctestTestRoundTrip(t, func(t *testing.T, mode roundTripTestMode) {
		const size = 20000
		tc := newTestClientConn(t, mode, func(tr2 *http2.Transport) {
			tr2.MaxHeaderListSize = size
		})
		tc.wantSettings(map[http2.SettingID]uint32{
			http2.SettingMaxHeaderListSize: size,
		})
	})
}

// TestAPITransportMaxDecoderHeaderTableSize tests the Transport.MaxDecoderHeaderTableSize field.
func TestAPITransportMaxDecoderHeaderTableSize(t *testing.T) {
	synctestTestRoundTrip(t, func(t *testing.T, mode roundTripTestMode) {
		const size = 10000
		tc := newTestClientConn(t, mode, func(tr2 *http2.Transport) {
			tr2.MaxDecoderHeaderTableSize = size
		})
		tc.wantSettings(map[http2.SettingID]uint32{
			http2.SettingHeaderTableSize: size,
		})
	})
}

// TestAPITransportMaxEncoderHeaderTableSize should go here,
// but it's difficult to verify the effects of the encoder table.

// TestAPITransportMaxReadFrameSize tests the Transport.MaxReadFrameSize field.
func TestAPITransportMaxReadFrameSize(t *testing.T) {
	synctestTestRoundTrip(t, func(t *testing.T, mode roundTripTestMode) {
		const size = 20000
		tc := newTestClientConn(t, mode, func(tr2 *http2.Transport) {
			tr2.MaxReadFrameSize = size
		})
		tc.wantSettings(map[http2.SettingID]uint32{
			http2.SettingMaxFrameSize: size,
		})
	})
}

// TestAPITransportStrictMaxConcurrentStreamsEnabled tests the
// Transport.StrictMaxConcurrentStreams field.
func TestAPITransportStrictMaxConcurrentStreamsEnabled(t *testing.T) {
	synctestTestRoundTrip(t, func(t *testing.T, mode roundTripTestMode) {
		tt := newTestTransport(t, mode, func(tr2 *http2.Transport) {
			tr2.StrictMaxConcurrentStreams = true
		})

		// Request 1: Sent on a new connection.
		// We observe MaxConcurrentStreams = 1.
		req1, _ := http.NewRequest("GET", "http://dummy.tld/1", nil)
		rt1 := tt.roundTrip(req1)

		tc1 := tt.getConn()
		tc1.wantFrameType(http2.FrameSettings)
		tc1.wantFrameType(http2.FrameWindowUpdate)
		tc1.wantHeaders(wantHeader{
			streamID:  1,
			endStream: true,
			header: http.Header{
				":authority": []string{"dummy.tld"},
				":method":    []string{"GET"},
				":path":      []string{"/1"},
			},
		})

		tc1.writeSettings(http2.Setting{
			ID:  http2.SettingMaxConcurrentStreams,
			Val: 1,
		})
		tc1.wantFrameType(http2.FrameSettings)

		// Request 2: Blocks, because request 1 is consuming the stream
		// concurrency slot.
		req2, _ := http.NewRequest("GET", "http://dummy.tld/2", nil)
		_ = tt.roundTrip(req2)
		tc1.wantIdle()

		// Send a response to request 1.
		// Request 2 can now be sent.
		tc1.writeHeaders(http2.HeadersFrameParam{
			StreamID:   1,
			EndHeaders: true,
			EndStream:  true,
			BlockFragment: tc1.makeHeaderBlockFragment(
				":status", "200",
			),
		})
		rt1.wantStatus(200)
		tc1.wantHeaders(wantHeader{
			streamID:  3,
			endStream: true,
			header: http.Header{
				":authority": []string{"dummy.tld"},
				":method":    []string{"GET"},
				":path":      []string{"/2"},
			},
		})
	})
}

// TestAPITransportStrictMaxConcurrentStreamsDisabled tests the
// Transport.StrictMaxConcurrentStreams field.
func TestAPITransportStrictMaxConcurrentStreamsDisabled(t *testing.T) {
	synctestTestRoundTrip(t, func(t *testing.T, mode roundTripTestMode) {
		tt := newTestTransport(t, mode, func(tr2 *http2.Transport) {
			tr2.StrictMaxConcurrentStreams = false
		})

		// Request 1: Sent on a new connection.
		// We observe MaxConcurrentStreams = 1.
		req1, _ := http.NewRequest("GET", "http://dummy.tld/1", nil)
		_ = tt.roundTrip(req1)

		tc1 := tt.getConn()
		tc1.wantFrameType(http2.FrameSettings)
		tc1.wantFrameType(http2.FrameWindowUpdate)
		tc1.wantHeaders(wantHeader{
			streamID:  1,
			endStream: true,
			header: http.Header{
				":authority": []string{"dummy.tld"},
				":method":    []string{"GET"},
				":path":      []string{"/1"},
			},
		})

		tc1.writeSettings(http2.Setting{
			ID:  http2.SettingMaxConcurrentStreams,
			Val: 1,
		})
		tc1.wantFrameType(http2.FrameSettings)

		// Request 2: Sent on a new connection, because request 1 is consuming the
		// first connection's stream concurrency slot.
		req2, _ := http.NewRequest("GET", "http://dummy.tld/2", nil)
		_ = tt.roundTrip(req2)
		tc2 := tt.getConn()
		tc2.wantFrameType(http2.FrameSettings)
		tc2.wantFrameType(http2.FrameWindowUpdate)
		tc2.wantHeaders(wantHeader{
			streamID:  1,
			endStream: true,
			header: http.Header{
				":authority": []string{"dummy.tld"},
				":method":    []string{"GET"},
				":path":      []string{"/2"},
			},
		})
	})
}

// TestAPITransportIdleConnTimeout tests the Transport.IdleConnTimeout field.
func TestAPITransportIdleConnTimeout(t *testing.T) {
	synctestTestRoundTrip(t, func(t *testing.T, mode roundTripTestMode) {
		const idleConnTimeout = 3 * time.Second
		tc := newTestClientConn(t, mode, func(tr2 *http2.Transport) {
			tr2.IdleConnTimeout = idleConnTimeout
		})
		tc.greet()
		tc.wantIdle()

		closeDelay := tc.connReader.waitForData(t)
		tc.wantClosed()
		if got, want := closeDelay, idleConnTimeout; got != want {
			t.Errorf("time until close: %v, want %v", got, want)
		}
	})
}

// TestAPITransportPingTimeout tests the
// Transport.ReadIdleTimeout and Transport.PingTimeout fields.
func TestAPITransportPingTimeout(t *testing.T) {
	synctestTestRoundTrip(t, func(t *testing.T, mode roundTripTestMode) {
		const readIdleTimeout = 3 * time.Second
		const pingTimeout = 5 * time.Second
		tc := newTestClientConn(t, mode, func(tr2 *http2.Transport) {
			tr2.ReadIdleTimeout = readIdleTimeout
			tr2.PingTimeout = pingTimeout
		})
		tc.greet()
		tc.wantIdle()

		// PING is sent after ReadIdleTimeout.
		pingDelay := tc.connReader.waitForData(t)
		tc.wantFrameType(http2.FramePing)
		if got, want := pingDelay, readIdleTimeout; got != want {
			t.Errorf("time until PING: %v, want %v", got, want)
		}

		// Connection is closed after PingTimeout.
		closeDelay := tc.connReader.waitForData(t)
		tc.wantClosed()
		if got, want := closeDelay, pingTimeout; got != want {
			t.Errorf("time after PING until close: %v, want %v", got, want)
		}
	})
}

// TestAPITransportWriteByteTimeout tests the Transport.WriteByteTimeout field.
func TestAPITransportWriteByteTimeout(t *testing.T) {
	synctestTestRoundTrip(t, func(t *testing.T, mode roundTripTestMode) {
		const writeByteTimeout = 3 * time.Second
		tt := newTestTransport(t, mode, func(tr2 *http2.Transport) {
			tr2.WriteByteTimeout = writeByteTimeout
		})

		req1, _ := http.NewRequest("GET", "http://dummy.tld/1", nil)
		_ = tt.roundTrip(req1)

		tc := tt.getConn()
		tc.wantFrameType(http2.FrameSettings)
		tc.wantFrameType(http2.FrameWindowUpdate)
		tc.wantFrameType(http2.FrameHeaders)

		// Block writes (past the first byte).
		// Just sleeping for WriteByteTimeout shouldn't do anything.
		tc.connReader.stop()
		tc.netconn.SetReadBufferSize(1)
		time.Sleep(2 * writeByteTimeout)
		tc.wantIdle()

		// Sending a new request will fail after the write timeout.
		//
		// We need to sleep for 2*writeByteTimeout, since we'll read
		// 2 bytes during the first timeout period. (We set a 1-byte buffer,
		// the smallest the test conn permits, which still allows for writing a byte.)
		req2, _ := http.NewRequest("GET", "http://dummy.tld/", nil)
		_ = tt.roundTrip(req2)
		time.Sleep(2 * writeByteTimeout)
		synctest.Wait()

		// Drain the partial request from the conn,
		// after which we can observe that it has been closed.
		tc.connReader.start()
		io.Copy(io.Discard, tc.connReader)
		tc.wantClosed()
	})
}

// TestAPITransportCountError tests the Transport.CountError field.
func TestAPITransportCountError(t *testing.T) {
	synctestTestRoundTrip(t, func(t *testing.T, mode roundTripTestMode) {
		countError := 0
		tc := newTestClientConn(t, mode, func(tr2 *http2.Transport) {
			tr2.CountError = func(errType string) {
				countError++
			}
		})
		tc.greet()

		tc.netconn.Close()
		synctest.Wait()
		if countError != 1 {
			t.Errorf("after connection error: CountError called %v times, want 1", countError)
		}
	})
}

type testClientConnPool struct {
	t        *testing.T
	li       *synctestNetListener
	tr2      *http2.Transport
	wantReq  *http.Request
	wantDead *http2.ClientConn
	conns    []*http2.ClientConn
	tlsConf  *tls.Config
}

func (p *testClientConnPool) GetClientConn(req *http.Request, addr string) (cc *http2.ClientConn, err error) {
	if p.wantReq == nil {
		p.t.Errorf("unexpected call to ClientConnPool.GetClientConn")
	}
	conn := net.Conn(p.li.newConn())
	if req.URL.Scheme == "https" {
		conn = tls.Client(conn, p.tlsConf)
	}
	cc, err = p.tr2.NewClientConn(conn)
	if cc != nil {
		p.conns = append(p.conns, cc)
	}
	p.wantReq = nil
	return cc, err
}

func (p *testClientConnPool) MarkDead(cc *http2.ClientConn) {
	if p.wantDead == nil {
		p.t.Errorf("unexpected call to ClientConnPool.MarkDead")
	}
	p.wantDead = nil
}

func (p *testClientConnPool) check() {
	p.t.Helper()
	synctest.Wait()
	if p.wantReq != nil {
		p.t.Errorf("wanted call to ClientConnPool.GetClientConn, got none")
	}
	if p.wantDead != nil {
		p.t.Errorf("wanted call to ClientConnPool.MarkDead, got none")
	}
}

// TestAPITransportConnPool tests the Transport.ConnPool field.
func TestAPITransportConnPool(t *testing.T) {
	synctestTestRoundTrip(t, func(t *testing.T, mode roundTripTestMode) {
		const idleConnTimeout = 3 * time.Second
		pool := &testClientConnPool{
			t: t,
		}
		tt := newTestTransport(t, mode, func(tr2 *http2.Transport) {
			tr2.ConnPool = pool
		})
		tt.useTLS = true
		pool.tr2 = tt.tr
		pool.li = tt.li
		pool.tlsConf = testTLSClientConfig.Clone()
		pool.tlsConf.NextProtos = []string{"h2"}

		// Send a request. The pool creates a new connection.
		req1, _ := http.NewRequest("GET", "https://dummy.tld/", nil)
		pool.wantReq = req1
		rt := tt.roundTrip(req1)
		pool.check()
		if len(pool.conns) != 1 {
			t.Fatalf("expected pool to create 1 conn, got %v", len(pool.conns))
		}

		// This is the connection created by the ClientConnPool.
		tc1 := tt.getConn()
		tc1.wantFrameType(http2.FrameSettings)
		tc1.wantFrameType(http2.FrameWindowUpdate)
		tc1.wantFrameType(http2.FrameHeaders)

		tc1.writeSettings()
		tc1.wantFrameType(http2.FrameSettings) // ACK

		tc1.writeHeaders(http2.HeadersFrameParam{
			StreamID:   1,
			EndHeaders: true,
			EndStream:  true,
			BlockFragment: tc1.makeHeaderBlockFragment(
				":status", "200",
			),
		})
		rt.wantStatus(200)

		// ClientConnPool.MarkDead is called when the connection closes.
		pool.wantDead = pool.conns[0]
		tc1.netconn.Close()
		pool.check()
	})
}

// TestAPITransportNewClientConn tests the Transport.NewClientConn method.
func TestAPITransportNewClientConn(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tt := newTestTransport(t, roundTripXNetHTTP2, func(tr2 *http2.Transport) {
			// ClientConnState.LastIdle is only set when there is an idle timer.
			tr2.IdleConnTimeout = 10 * time.Second
		})

		nc := tt.li.newConn()
		cc, err := tt.tr.NewClientConn(nc)
		if err != nil {
			t.Fatalf("NewClientConn: %v", err)
		}

		tc1 := tt.getConn()
		tc1.wantFrameType(http2.FrameSettings)
		tc1.wantFrameType(http2.FrameWindowUpdate)
		tc1.writeSettings(http2.Setting{
			ID:  http2.SettingMaxConcurrentStreams,
			Val: 1,
		})
		tc1.wantFrameType(http2.FrameSettings) // ACK

		synctest.Wait()
		wantClientConnState(t, cc.State(), http2.ClientConnState{
			MaxConcurrentStreams: 1,
		})

		if got, want := cc.CanTakeNewRequest(), true; got != want {
			t.Errorf("cc.CanTakeNewRequest() = %v, want %v", got, want)
		}
		if got, want := cc.ReserveNewRequest(), true; got != want {
			t.Errorf("cc.ReserveNewRequest() = %v, want %v", got, want)
		}
		// Reservation has consumed the one concurrency slot.
		if got, want := cc.CanTakeNewRequest(), false; got != want {
			t.Errorf("cc.CanTakeNewRequest() = %v, want %v (sole request slot reserved)", got, want)
		}
		wantClientConnState(t, cc.State(), http2.ClientConnState{
			StreamsReserved:      1,
			MaxConcurrentStreams: 1,
		})

		// Consume the reservation by sending a request.
		req1, _ := http.NewRequest("GET", "https://dummy.tld/", nil)
		rt1 := newTestRoundTrip(t, req1, cc.RoundTrip)
		tc1.wantFrameType(http2.FrameHeaders)
		wantClientConnState(t, cc.State(), http2.ClientConnState{
			StreamsActive:        1,
			MaxConcurrentStreams: 1,
		})

		tc1.writeHeaders(http2.HeadersFrameParam{
			StreamID:   1,
			EndHeaders: true,
			EndStream:  true,
			BlockFragment: tc1.makeHeaderBlockFragment(
				":status", "200",
			),
		})
		rt1.wantStatus(200)
		if got, want := cc.CanTakeNewRequest(), true; got != want {
			t.Errorf("cc.CanTakeNewRequest() = %v, want %v", got, want)
		}
		wantClientConnState(t, cc.State(), http2.ClientConnState{
			MaxConcurrentStreams: 1,
			LastIdle:             time.Now(),
		})

		cc.SetDoNotReuse()
		wantClientConnState(t, cc.State(), http2.ClientConnState{
			Closing:              true,
			MaxConcurrentStreams: 1,
			LastIdle:             time.Now(),
		})
		req2, _ := http.NewRequest("GET", "https://dummy.tld/", nil)
		rt2 := newTestRoundTrip(t, req2, cc.RoundTrip)
		if rt2.err() == nil {
			t.Fatalf("RoundTrip after SetDoNotReuse: succeeded, want error")
		}

		if err := cc.Close(); err != nil {
			t.Errorf("cc.Close() = %v, want nil", err)
		}
		wantClientConnState(t, cc.State(), http2.ClientConnState{
			Closing:              true,
			Closed:               true,
			MaxConcurrentStreams: 1,
			LastIdle:             time.Now(),
		})
	})
}

// TestAPITransportClientConnPending tests the ClientConnState.Pending state.
func TestAPITransportClientConnPending(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tt := newTestTransport(t, roundTripXNetHTTP2, func(tr2 *http2.Transport) {
			tr2.StrictMaxConcurrentStreams = true
		})

		nc := tt.li.newConn()
		cc, err := tt.tr.NewClientConn(nc)
		if err != nil {
			t.Fatalf("NewClientConn: %v", err)
		}

		tc1 := tt.getConn()
		tc1.wantFrameType(http2.FrameSettings)
		tc1.wantFrameType(http2.FrameWindowUpdate)
		tc1.writeSettings(http2.Setting{
			ID:  http2.SettingMaxConcurrentStreams,
			Val: 1,
		})
		tc1.wantFrameType(http2.FrameSettings) // ACK

		synctest.Wait()
		wantClientConnState(t, cc.State(), http2.ClientConnState{
			MaxConcurrentStreams: 1,
		})

		// Send a request, consuming the concurrency slot.
		req1, _ := http.NewRequest("GET", "https://dummy.tld/", nil)
		rt1 := newTestRoundTrip(t, req1, cc.RoundTrip)
		tc1.wantFrameType(http2.FrameHeaders)
		wantClientConnState(t, cc.State(), http2.ClientConnState{
			MaxConcurrentStreams: 1,
			StreamsActive:        1,
		})

		// Send another request, which enters the Pending state.
		req2, _ := http.NewRequest("GET", "https://dummy.tld/", nil)
		_ = newTestRoundTrip(t, req2, cc.RoundTrip)
		tc1.wantIdle()
		wantClientConnState(t, cc.State(), http2.ClientConnState{
			MaxConcurrentStreams: 1,
			StreamsActive:        1,
			StreamsPending:       1,
		})

		// First request completes, second starts.
		tc1.writeHeaders(http2.HeadersFrameParam{
			StreamID:   1,
			EndHeaders: true,
			EndStream:  true,
			BlockFragment: tc1.makeHeaderBlockFragment(
				":status", "200",
			),
		})
		rt1.wantStatus(200)
		tc1.wantFrameType(http2.FrameHeaders)
		wantClientConnState(t, cc.State(), http2.ClientConnState{
			MaxConcurrentStreams: 1,
			StreamsActive:        1,
		})
	})
}

// TestAPIClientConnPing tests the ClientConn.Ping method.
func TestAPIClientConnPing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tt := newTestTransport(t, roundTripXNetHTTP2)

		nc := tt.li.newConn()
		cc, err := tt.tr.NewClientConn(nc)
		if err != nil {
			t.Fatalf("NewClientConn: %v", err)
		}

		tc1 := tt.getConn()
		tc1.wantFrameType(http2.FrameSettings)
		tc1.wantFrameType(http2.FrameWindowUpdate)
		tc1.writeSettings(http2.Setting{
			ID:  http2.SettingMaxConcurrentStreams,
			Val: 1,
		})
		tc1.wantFrameType(http2.FrameSettings) // ACK
		tc1.wantIdle()

		// Ping with successful response.
		pingDone := false
		var pingErr error
		go func() {
			pingErr = cc.Ping(context.Background())
			pingDone = true
		}()
		synctest.Wait()
		fr := readFrame[*http2.PingFrame](t, tc1)
		if pingDone {
			t.Fatalf("cc.Ping() = %v; want to still be running", pingErr)
		}
		tc1.writePing(true, fr.Data)
		synctest.Wait()
		if !pingDone {
			t.Fatalf("cc.Ping() still running; want to be done")
		}
		if pingErr != nil {
			t.Fatalf("cc.Ping() = %v; want nil", pingErr)
		}

		// Ping with no response.
		const timeout = 1 * time.Second
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		start := time.Now()
		if err := cc.Ping(ctx); err == nil {
			t.Fatalf("cc.Ping() = nil; want error")
		}
		if got, want := time.Since(start), timeout; got != want {
			t.Fatalf("cc.Ping() returned after %v, want %v", got, want)
		}
	})
}

// TestAPIClientConnShutdown tests the ClientConn.Shutdown method.
// Shutdown returns after the last request on the connection completes.
func TestAPIClientConnShutdownSuccess(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tt := newTestTransport(t, roundTripXNetHTTP2)

		nc := tt.li.newConn()
		cc, err := tt.tr.NewClientConn(nc)
		if err != nil {
			t.Fatalf("NewClientConn: %v", err)
		}

		tc1 := tt.getConn()
		tc1.greet()

		// Start a request.
		req1, _ := http.NewRequest("GET", "https://dummy.tld/", nil)
		rt1 := newTestRoundTrip(t, req1, cc.RoundTrip)
		tc1.wantFrameType(http2.FrameHeaders)

		// Start shutdown.
		var shutdownErr error
		shutdownDone := false
		go func() {
			shutdownErr = cc.Shutdown(context.Background())
			shutdownDone = true
		}()

		synctest.Wait()
		if shutdownDone {
			t.Fatalf("cc.Shutdown() = %v; want still running with req in flight", err)
		}
		if rt1.done() {
			t.Fatalf("RoundTrip finished; want still running")
		}

		// Server terminates the outstanding request, connection shuts down.
		tc1.writeRSTStream(1, http2.ErrCodeCancel)
		synctest.Wait()

		if !shutdownDone {
			t.Fatalf("cc.Shutdown() still running; want to have returned")
		}
		if shutdownErr != nil {
			t.Fatalf("cc.Shutdown() = %v; want nil", err)
		}
		if err := rt1.err(); err == nil {
			t.Fatalf("RoundTrip succeeded; want error")
		}

		// We might send a GOAWAY frame before closing.
		if fr := tc1.readFrame(); fr != nil {
			if _, ok := fr.(*http2.GoAwayFrame); !ok {
				t.Fatalf("read frame %v; want GOAWAY or nothing", fr)
			}
		}
		tc1.wantClosed()
	})
}

// TestAPIClientConnShutdown tests the ClientConn.Shutdown method.
// Shutdown's context expires before the last request on the connection completes.
func TestAPIClientConnShutdownFailure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tt := newTestTransport(t, roundTripXNetHTTP2)

		nc := tt.li.newConn()
		cc, err := tt.tr.NewClientConn(nc)
		if err != nil {
			t.Fatalf("NewClientConn: %v", err)
		}

		tc1 := tt.getConn()
		tc1.greet()

		// Start a request.
		req1, _ := http.NewRequest("GET", "https://dummy.tld/", nil)
		_ = newTestRoundTrip(t, req1, cc.RoundTrip)
		tc1.wantFrameType(http2.FrameHeaders)

		// Shutdown's context expires before the request completes.
		const timeout = 1 * time.Second
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		start := time.Now()
		if err := cc.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("cc.Shutdown() = %v; want DeadlineExceeded", err)
		}
		if got, want := time.Since(start), timeout; got != want {
			t.Fatalf("cc.Shutdown() returned after %v, want %v", got, want)
		}

		// We might send a GOAWAY frame before closing.
		if fr := tc1.readFrame(); fr != nil {
			if _, ok := fr.(*http2.GoAwayFrame); !ok {
				t.Fatalf("read frame %v; want GOAWAY or nothing", fr)
			}
		}
		tc1.wantIdle()
	})
}

func wantClientConnState(t *testing.T, a, b http2.ClientConnState) {
	t.Helper()
	if got, want := a.Closed, b.Closed; got != want {
		t.Errorf("ClientConnState.Closed = %v, want %v", got, want)
	}
	if got, want := a.Closing, b.Closing; got != want {
		t.Errorf("ClientConnState.Closing = %v, want %v", got, want)
	}
	if got, want := a.StreamsActive, b.StreamsActive; got != want {
		t.Errorf("ClientConnState.StreamsActive = %v, want %v", got, want)
	}
	if got, want := a.StreamsReserved, b.StreamsReserved; got != want {
		t.Errorf("ClientConnState.StreamsReserved = %v, want %v", got, want)
	}
	if got, want := a.StreamsPending, b.StreamsPending; got != want {
		t.Errorf("ClientConnState.StreamsPending = %v, want %v", got, want)
	}
	if got, want := a.MaxConcurrentStreams, b.MaxConcurrentStreams; got != want {
		t.Errorf("ClientConnState.MaxConcurrentStreams = %v, want %v", got, want)
	}
	if got, want := a.LastIdle, b.LastIdle; !got.Equal(want) {
		t.Errorf("ClientConnState.LastIdle = %v, want %v", got, want)
	}
}
