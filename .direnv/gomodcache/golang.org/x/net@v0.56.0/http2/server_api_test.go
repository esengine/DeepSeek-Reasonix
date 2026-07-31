// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// This file tests the exported, user-facing parts of Server.
//
// These tests verify that Server behavior is consistent when using either the
// HTTP/2 implementation in this package (x/net/http2), or when using the
// implementation in net/http/internal/http2.

package http2_test

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/synctest"
	"time"

	"golang.org/x/net/http2"
)

// TestAPIServerMaxConcurrentStreams tests the Server.MaxConcurrentStreams field.
func TestAPIServerMaxConcurrentStreams(t *testing.T) {
	synctestTest(t, testAPIServerMaxConcurrentStreams)
}
func testAPIServerMaxConcurrentStreams(t testing.TB) {
	const maxConcurrentStreams = 10
	st := newServerTester(t, func(w http.ResponseWriter, r *http.Request) {},
		func(s *http2.Server) {
			s.MaxConcurrentStreams = maxConcurrentStreams
		})

	st.wantSettings(map[http2.SettingID]uint32{
		http2.SettingMaxConcurrentStreams: maxConcurrentStreams,
	})
}

// TestAPIServerMaxDecoderHeaderTableSize tests the Server.MaxDecoderHeaderTableSize field.
func TestAPIServerMaxDecoderHeaderTableSize(t *testing.T) {
	synctestTest(t, testAPIServerMaxDecoderHeaderTableSize)
}
func testAPIServerMaxDecoderHeaderTableSize(t testing.TB) {
	const maxDecoderHeaderTableSize = 10000
	st := newServerTester(t, func(w http.ResponseWriter, r *http.Request) {},
		func(s *http2.Server) {
			s.MaxDecoderHeaderTableSize = maxDecoderHeaderTableSize
		})
	st.wantSettings(map[http2.SettingID]uint32{
		http2.SettingHeaderTableSize: maxDecoderHeaderTableSize,
	})
}

// TestAPIServerMaxEncoderHeaderTableSize should go here,
// but it's difficult to verify the effects of the encoder table.

// TestAPIServerMaxReadFrameSize tests the Server.MaxDecoderReadFrameSize field.
func TestAPIServerMaxReadFrameSize(t *testing.T) {
	synctestTest(t, testAPIServerMaxReadFrameSize)
}
func testAPIServerMaxReadFrameSize(t testing.TB) {
	const maxReadFrameSize = 20000
	st := newServerTester(t, func(w http.ResponseWriter, r *http.Request) {},
		func(s *http2.Server) {
			s.MaxReadFrameSize = maxReadFrameSize
		})
	st.wantSettings(map[http2.SettingID]uint32{
		http2.SettingMaxFrameSize: maxReadFrameSize,
	})
}

// TestAPIServerPermitProhibitedCipherSuites tests the Server.PermitProhibitedCipherSuites field.
func TestAPIServerPermitProhibitedCipherSuites(t *testing.T) {
	synctestTest(t, testAPIServerPermitProhibitedCipherSuites)
}
func testAPIServerPermitProhibitedCipherSuites(t testing.TB) {
	prohibitedCipher := func(state *tls.ConnectionState) {
		const cipher_TLS_NULL_WITH_NULL_NULL uint16 = 0x0000
		state.CipherSuite = cipher_TLS_NULL_WITH_NULL_NULL
	}

	// Default: Connection rejected.
	st1 := newServerTester(t, func(w http.ResponseWriter, r *http.Request) {},
		prohibitedCipher,
	)
	st1.wantGoAway(0, http2.ErrCodeInadequateSecurity)
	st1.wantClosed()

	// PermitProhibitedCipherSuites set: Connection accepted.
	st2 := newServerTester(t, func(w http.ResponseWriter, r *http.Request) {},
		prohibitedCipher,
		func(s *http2.Server) {
			s.PermitProhibitedCipherSuites = true
		})
	st2.greet()
}

// TestAPIServerIdleTimeout tests the
// Server.ReadIdleTimeout and Server.IdleTimeout fields.
func TestAPIServerIdleTimeout(t *testing.T) {
	synctestTest(t, testAPIServerIdleTimeout)
}
func testAPIServerIdleTimeout(t testing.TB) {
	const idleTimeout = 3 * time.Second
	st := newServerTester(t, func(w http.ResponseWriter, r *http.Request) {},
		func(s *http2.Server) {
			s.IdleTimeout = idleTimeout
		})
	st.greet()
	st.wantIdle()

	// Connection does not close before IdleTimeout.
	time.Sleep(idleTimeout - time.Nanosecond)
	st.wantIdle()

	// Connection is closed after IdleTimeout.
	time.Sleep(time.Nanosecond)
	st.wantGoAway(0, http2.ErrCodeNo)
}

// TestAPIServerPingTimeout tests the
// Server.ReadIdleTimeout and Server.PingTimeout fields.
func TestAPIServerPingTimeout(t *testing.T) {
	synctestTest(t, testAPIServerPingTimeout)
}
func testAPIServerPingTimeout(t testing.TB) {
	const readIdleTimeout = 3 * time.Second
	const pingTimeout = 5 * time.Second
	st := newServerTester(t, func(w http.ResponseWriter, r *http.Request) {},
		func(s *http2.Server) {
			s.ReadIdleTimeout = readIdleTimeout
			s.PingTimeout = pingTimeout
		})
	st.greet()
	st.wantIdle()

	// PING is sent after ReadIdleTimeout.
	time.Sleep(readIdleTimeout - time.Nanosecond)
	st.wantIdle()
	time.Sleep(time.Nanosecond)
	st.wantFrameType(http2.FramePing)

	// Connection is closed after PingTimeout.
	time.Sleep(pingTimeout - time.Nanosecond)
	st.wantIdle()
	time.Sleep(time.Nanosecond)
	st.wantClosed()
}

// TestAPIServerWriteByteTimeout tests the Server.WriteByteTimeout field.
func TestAPIServerWriteByteTimeout(t *testing.T) {
	synctestTest(t, testAPIServerWriteByteTimeout)
}
func testAPIServerWriteByteTimeout(t testing.TB) {
	const writeByteTimeout = 3 * time.Second
	st := newServerTester(t, func(w http.ResponseWriter, r *http.Request) {},
		func(s *http2.Server) {
			s.WriteByteTimeout = writeByteTimeout
		})
	st.greet()
	st.wantIdle()

	st.cc.(*synctestNetConn).SetReadBufferSize(1)

	st.writeHeaders(http2.HeadersFrameParam{
		StreamID:      1,
		BlockFragment: st.encodeHeader(),
		EndStream:     true, // no DATA frames
		EndHeaders:    true,
	})
	time.Sleep(writeByteTimeout - time.Nanosecond)
	st.wantFrameType(http2.FrameHeaders)

	st.writeHeaders(http2.HeadersFrameParam{
		StreamID:      3,
		BlockFragment: st.encodeHeader(),
		EndStream:     true, // no DATA frames
		EndHeaders:    true,
	})
	time.Sleep(2 * writeByteTimeout)
	synctest.Wait()

	// Drain the partial response frame from the conn,
	// after which we can observe that it has been closed.
	st.wantClosed()
}

// TestAPIServerMaxUploadBufferPerConnection tests the Server.MaxUploadBufferPerConnection field.
func TestAPIServerMaxUploadBufferPerConnection(t *testing.T) {
	synctestTest(t, testAPIServerMaxUploadBufferPerConnection)
}
func testAPIServerMaxUploadBufferPerConnection(t testing.TB) {
	const maxUploadBufferPerConnection = http2.InitialWindowSize + 10000
	st := newServerTester(t, func(w http.ResponseWriter, r *http.Request) {},
		func(s *http2.Server) {
			s.MaxUploadBufferPerConnection = maxUploadBufferPerConnection
		})
	st.writePreface()
	st.wantFrameType(http2.FrameSettings)
	st.wantWindowUpdate(0, maxUploadBufferPerConnection-http2.InitialWindowSize)
}

// TestAPIServerMaxUploadBufferPerStream tests the Server.MaxUploadBufferPerStream field.
func TestAPIServerMaxUploadBufferPerStream(t *testing.T) {
	synctestTest(t, testAPIServerMaxUploadBufferPerStream)
}
func testAPIServerMaxUploadBufferPerStream(t testing.TB) {
	const maxUploadBufferPerStream = 10000
	st := newServerTester(t, func(w http.ResponseWriter, r *http.Request) {},
		func(s *http2.Server) {
			s.MaxUploadBufferPerStream = maxUploadBufferPerStream
		})
	st.wantSettings(map[http2.SettingID]uint32{
		http2.SettingInitialWindowSize: maxUploadBufferPerStream,
	})
}

// TestAPIServerCountError tests the Server.CountError field.
func TestAPIServerCountError(t *testing.T) {
	synctestTest(t, testAPIServerCountError)
}
func testAPIServerCountError(t testing.TB) {
	countError := 0
	st := newServerTester(t, func(w http.ResponseWriter, r *http.Request) {},
		func(s *http2.Server) {
			s.CountError = func(errType string) {
				countError++
			}
		})
	st.greet()

	st.writeHeaders(http2.HeadersFrameParam{
		StreamID:      2, // invalid stream ID
		BlockFragment: st.encodeHeader(),
		EndStream:     true, // no DATA frames
		EndHeaders:    true,
	})
	synctest.Wait()
	if countError != 1 {
		t.Errorf("after connection error: CountError called %v times, want 1", countError)
	}
}

// TestAPIServeConnOptsContext tests the ServeConnOpts.Context field.
func TestAPIServeConnOptsContext(t *testing.T) {
	synctestTest(t, testAPIServeConnOptsContext)
}
func testAPIServeConnOptsContext(t testing.TB) {
	st := newServerTester(t, nil, optNoConn)

	cli, srv := synctestNetPipe()
	t.Cleanup(func() {
		cli.Close()
		srv.Close()
	})

	type testContextKey struct{}
	type testContextValue struct{}
	baseCtx := context.WithValue(t.Context(), testContextKey{}, testContextValue{})

	go func() {
		st.h2server.ServeConn(srv, &http2.ServeConnOpts{
			Context: baseCtx,
			Handler: serverTesterHandler{st},
		})
	}()

	tc := newTestServerConn(t, cli)
	tc.greet()

	tc.writeHeaders(http2.HeadersFrameParam{
		StreamID:      1, // invalid stream ID
		BlockFragment: st.encodeHeader(),
		EndStream:     true, // no DATA frames
		EndHeaders:    true,
	})
	call := st.nextHandlerCall()

	callCtx := call.req.Context()
	if v := callCtx.Value(testContextKey{}); v != (testContextValue{}) {
		t.Errorf("handler context does not inherit from server base context")
	}
	if got, want := callCtx.Value(http.LocalAddrContextKey), srv.LocalAddr(); got != want {
		t.Errorf("handler context LocalAddrContextKey = %v, want %v", got, want)
	}
}

// TestAPIServeConnOptsBaseConfig tests the ServeConnOpts.BaseConfig field.
func TestAPIServeConnOptsBaseConfig(t *testing.T) {
	synctestTest(t, testAPIServeConnOptsBaseConfig)
}
func testAPIServeConnOptsBaseConfig(t testing.TB) {
	st := newServerTester(t, nil, optNoConn)

	cli, srv := synctestNetPipe()
	t.Cleanup(func() {
		cli.Close()
		srv.Close()
	})

	const maxReadFrameSize = 20000
	go func() {
		st.h2server.ServeConn(srv, &http2.ServeConnOpts{
			BaseConfig: &http.Server{
				HTTP2: &http.HTTP2Config{
					MaxReadFrameSize: maxReadFrameSize,
				},
			},
			Handler: serverTesterHandler{st},
		})
	}()

	tc := newTestServerConn(t, cli)
	tc.wantSettings(map[http2.SettingID]uint32{
		http2.SettingMaxFrameSize: maxReadFrameSize,
	})
}

// TestAPIServeConnUpgrade tests using Server.ServeConn to serve an Upgrade: h2c connection.
//
// Upgrade: h2c is deprecated in current RFCs and we regret attempting to support it
// (our support was never good, and never actually tested because Transport doesn't
// support it at all), but it's there, so we support it for now at least.
func TestAPIServeConnUpgrade(t *testing.T) {
	synctestTest(t, testAPIServeConnUpgrade)
}
func testAPIServeConnUpgrade(t testing.TB) {
	st := newServerTester(t, nil, optNoConn)

	cli, srv := synctestNetPipe()
	t.Cleanup(func() {
		cli.Close()
		srv.Close()
	})

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "h2c")
	go func() {
		st.h2server.ServeConn(srv, &http2.ServeConnOpts{
			UpgradeRequest: req,
			Settings:       []byte{},
			Handler:        serverTesterHandler{st},
		})
	}()

	cli.SetReadDeadline(time.Now())
	cli.autoWait = true
	tc := newTestServerConn(t, cli)
	tc.greet()

	call := st.nextHandlerCall()
	call.w.Header().Set("X-Header", "header")
	call.w.WriteHeader(404)
	call.w.Header().Set(http.TrailerPrefix+"X-Trailer", "trailer")
	call.w.Write([]byte("body"))
	call.exit()
	tc.wantHeaders(wantHeader{
		streamID:  1,
		endStream: false,
		header: http.Header{
			":status":  []string{"404"},
			"x-header": []string{"header"},
		},
	})
	tc.wantData(wantData{
		streamID:  1,
		endStream: false,
		data:      []byte("body"),
	})
	tc.wantHeaders(wantHeader{
		streamID:  1,
		endStream: true,
		header: http.Header{
			"x-trailer": []string{"trailer"},
		},
	})
	tc.wantIdle()

	tc.writeHeaders(http2.HeadersFrameParam{
		StreamID:      3,
		BlockFragment: st.encodeHeader(),
		EndStream:     true,
		EndHeaders:    true,
	})
	call = st.nextHandlerCall()
	call.exit()
	tc.wantHeaders(wantHeader{
		streamID:  3,
		endStream: true,
		header: http.Header{
			":status": []string{"200"},
		},
	})
}

// TestAPIServeConnOptsSawClientPreface tests the ServeConnOpts.SawClientPreface field.
func TestAPIServeConnOptsSawClientPreface(t *testing.T) {
	synctestTest(t, testAPIServeConnOptsSawClientPreface)
}
func testAPIServeConnOptsSawClientPreface(t testing.TB) {
	st := newServerTester(t, nil, optNoConn)

	cli, srv := synctestNetPipe()
	t.Cleanup(func() {
		cli.Close()
		srv.Close()
	})

	const maxReadFrameSize = 20000
	go func() {
		st.h2server.ServeConn(srv, &http2.ServeConnOpts{
			SawClientPreface: true,
			Handler:          serverTesterHandler{st},
		})
	}()

	tc := newTestServerConn(t, cli)
	tc.wantFrameType(http2.FrameSettings)
	tc.wantFrameType(http2.FrameWindowUpdate)
	tc.writeSettings()
	tc.wantFrameType(http2.FrameSettings) // ACK
	tc.writeSettingsAck()
}
