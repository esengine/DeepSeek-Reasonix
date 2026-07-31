// Copyright 2024 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Infrastructure for testing ClientConn.RoundTrip.
// Put actual tests in transport_test.go.

package http2_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"reflect"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	. "golang.org/x/net/http2"
	"golang.org/x/net/http2/hpack"
	"golang.org/x/net/internal/gate"
)

// TestTestClientConn demonstrates usage of testClientConn.
func TestTestClientConn(t *testing.T) { synctestTest(t, testTestClientConn) }
func testTestClientConn(t testing.TB) {
	// newTestClientConn creates a *ClientConn and surrounding test infrastructure.
	tc := newTestClientConn(t)

	// tc.greet reads the client's initial SETTINGS and WINDOW_UPDATE frames,
	// and sends a SETTINGS frame to the client.
	//
	// Additional settings may be provided as optional parameters to greet.
	tc.greet()

	// Request bodies must either be constant (bytes.Buffer, strings.Reader)
	// or created with newRequestBody.
	body := tc.newRequestBody()
	body.writeBytes(10)         // 10 arbitrary bytes...
	body.closeWithError(io.EOF) // ...followed by EOF.

	// tc.roundTrip calls RoundTrip, but does not wait for it to return.
	// It returns a testRoundTrip.
	req, _ := http.NewRequest("PUT", "https://dummy.tld/", body)
	rt := tc.roundTrip(req)

	// Note: rt.streamID can give the stream ID, but only with http2legacy.
	streamID := uint32(1)

	// tc has a number of methods to check for expected frames sent.
	// Here, we look for headers and the request body.
	tc.wantHeaders(wantHeader{
		streamID:  streamID,
		endStream: false,
		header: http.Header{
			":authority": []string{"dummy.tld"},
			":method":    []string{"PUT"},
			":path":      []string{"/"},
		},
	})
	// Expect 10 bytes of request body in DATA frames.
	tc.wantData(wantData{
		streamID:  streamID,
		endStream: true,
		size:      10,
		multiple:  true,
	})

	// tc.writeHeaders sends a HEADERS frame back to the client.
	tc.writeHeaders(HeadersFrameParam{
		StreamID:   streamID,
		EndHeaders: true,
		EndStream:  true,
		BlockFragment: tc.makeHeaderBlockFragment(
			":status", "200",
		),
	})

	// Now that we've received headers, RoundTrip has finished.
	// testRoundTrip has various methods to examine the response,
	// or to fetch the response and/or error returned by RoundTrip
	rt.wantStatus(200)
	rt.wantBody(nil)
}

func TestTestTransport(t *testing.T) {
	synctestSubtest(t, "nethttp", func(t testing.TB) {
		testTestTransport(t, roundTripNetHTTP)
	})
	synctestSubtest(t, "xnethttp2", func(t testing.TB) {
		testTestTransport(t, roundTripXNetHTTP2)
	})
}
func testTestTransport(t testing.TB, mode roundTripTestMode) {
	tt := newTestTransport(t)

	req := Must(http.NewRequest("GET", "https://dummy.tld/", nil))
	rt := tt.roundTrip(req)
	tc := tt.getConn()
	tc.wantFrameType(FrameSettings)
	tc.wantFrameType(FrameWindowUpdate)

	tc.wantHeaders(wantHeader{
		streamID:  1,
		endStream: true,
		header: http.Header{
			":authority": []string{"dummy.tld"},
			":method":    []string{"GET"},
			":path":      []string{"/"},
		},
	})

	tc.writeSettings()
	tc.writeSettingsAck()
	tc.wantFrameType(FrameSettings) // acknowledgement
	tc.writeHeaders(HeadersFrameParam{
		StreamID:   1,
		EndHeaders: true,
		EndStream:  true,
		BlockFragment: tc.makeHeaderBlockFragment(
			":status", "200",
		),
	})

	rt.wantStatus(200)
}

// A testClientConn allows testing ClientConn.RoundTrip against a fake server.
//
// A test using testClientConn consists of:
//   - actions on the client (calling RoundTrip, making data available to Request.Body);
//   - validation of frames sent by the client to the server; and
//   - providing frames from the server to the client.
//
// testClientConn manages synchronization, so tests can generally be written as
// a linear sequence of actions and validations without additional synchronization.
type testClientConn struct {
	t testing.TB

	tr  *Transport
	fr  *Framer
	cc  *ClientConn
	cc1 *httpClientConn
	testConnFramer

	encbuf bytes.Buffer
	enc    *hpack.Encoder

	netconn    *synctestNetConn
	connReader *nonblockingReader
}

func newTestClientConnFromNetConn(tt *testTransport, nc net.Conn) *testClientConn {
	tc := &testClientConn{
		t:  tt.t,
		tr: tt.tr,
	}

	var writer io.Writer
	var reader io.Reader
	if tt.useTLS {
		tlsConfig := testTLSServerConfig.Clone()
		tlsConfig.NextProtos = []string{"h2"}
		tlsConn := tls.Server(nc, tlsConfig)
		reader = tlsConn
		writer = tlsConn
	} else {
		reader = nc
		writer = nc
	}
	tc.connReader = newNonblockingReader(reader)

	tc.netconn = nc.(*synctestNetConn)
	tc.enc = hpack.NewEncoder(&tc.encbuf)
	tc.fr = NewFramer(writer, tc.connReader)
	tc.testConnFramer = testConnFramer{
		t:   tt.t,
		fr:  tc.fr,
		dec: hpack.NewDecoder(InitialHeaderTableSize, nil),
	}
	tc.fr.SetMaxReadFrameSize(10 << 20)
	tt.t.Cleanup(func() {
		tc.closeWrite()
	})

	return tc
}

func (tc *testClientConn) readClientPreface() {
	tc.t.Helper()
	// Read the client's HTTP/2 preface, sent prior to any HTTP/2 frames.
	synctest.Wait()
	buf := make([]byte, len(ClientPreface))
	if _, err := io.ReadFull(tc.connReader, buf); err != nil {
		tc.t.Fatalf("reading preface: %v", err)
	}
	if !bytes.Equal(buf, []byte(ClientPreface)) {
		tc.t.Fatalf("client preface: %q, want %q", buf, ClientPreface)
	}
}

// hasFrame reports whether a frame is available to be read.
func (tc *testClientConn) hasFrame() bool {
	synctest.Wait()
	return tc.connReader.buf.Len() > 0
}

// isClosed reports whether the peer has closed the connection.
func (tc *testClientConn) isClosed() bool {
	synctest.Wait()
	return tc.netconn.IsClosedByPeer()
}

// closeWrite causes the net.Conn used by the ClientConn to return a error
// from Read calls.
func (tc *testClientConn) closeWrite() {
	tc.netconn.Close()
}

// closeWrite causes the net.Conn used by the ClientConn to return a error
// from Write calls.
func (tc *testClientConn) closeWriteWithError(err error) {
	tc.netconn.loc.setReadError(io.EOF)
	tc.netconn.loc.setWriteError(err)
}

// testRequestBody is a Request.Body for use in tests.
type testRequestBody struct {
	tc   *testClientConn
	gate gate.Gate

	// At most one of buf or bytes can be set at any given time:
	buf   bytes.Buffer // specific bytes to read from the body
	bytes int          // body contains this many arbitrary bytes

	err error // read error (comes after any available bytes)
}

func (tc *testClientConn) newRequestBody() *testRequestBody {
	b := &testRequestBody{
		tc:   tc,
		gate: gate.New(false),
	}
	return b
}

func (b *testRequestBody) unlock() {
	b.gate.Unlock(b.buf.Len() > 0 || b.bytes > 0 || b.err != nil)
}

// Read is called by the ClientConn to read from a request body.
func (b *testRequestBody) Read(p []byte) (n int, _ error) {
	if err := b.gate.WaitAndLock(context.Background()); err != nil {
		return 0, err
	}
	defer b.unlock()
	switch {
	case b.buf.Len() > 0:
		return b.buf.Read(p)
	case b.bytes > 0:
		if len(p) > b.bytes {
			p = p[:b.bytes]
		}
		b.bytes -= len(p)
		for i := range p {
			p[i] = 'A'
		}
		return len(p), nil
	default:
		return 0, b.err
	}
}

// Close is called by the ClientConn when it is done reading from a request body.
func (b *testRequestBody) Close() error {
	return nil
}

// writeBytes adds n arbitrary bytes to the body.
func (b *testRequestBody) writeBytes(n int) {
	defer synctest.Wait()
	b.gate.Lock()
	defer b.unlock()
	b.bytes += n
	b.checkWrite()
	synctest.Wait()
}

// Write adds bytes to the body.
func (b *testRequestBody) Write(p []byte) (int, error) {
	defer synctest.Wait()
	b.gate.Lock()
	defer b.unlock()
	n, err := b.buf.Write(p)
	b.checkWrite()
	return n, err
}

func (b *testRequestBody) checkWrite() {
	if b.bytes > 0 && b.buf.Len() > 0 {
		b.tc.t.Fatalf("can't interleave Write and writeBytes on request body")
	}
	if b.err != nil {
		b.tc.t.Fatalf("can't write to request body after closeWithError")
	}
}

// closeWithError sets an error which will be returned by Read.
func (b *testRequestBody) closeWithError(err error) {
	defer synctest.Wait()
	b.gate.Lock()
	defer b.unlock()
	b.err = err
}

// roundTrip starts a RoundTrip call.
//
// (Note that the RoundTrip won't complete until response headers are received,
// the request times out, or some other terminal condition is reached.)
func (tc *testClientConn) roundTrip(req *http.Request) *testRoundTrip {
	rt := &testRoundTrip{}
	rt.do(tc.t, req, func(req *http.Request) (*http.Response, error) {
		// doRoundTrip will pick a way to make a RoundTrip call
		// depending on the Go version and whether we're
		// wrapping the net/http implementation or using the one in this package.
		return tc.doRoundTrip(req, func(streamID uint32) {
			rt.id.Store(streamID)
		})
	})
	return rt
}

func newTestRoundTrip(t testing.TB, req *http.Request, f func(*http.Request) (*http.Response, error)) *testRoundTrip {
	rt := &testRoundTrip{}
	rt.do(t, req, f)
	return rt
}

func (rt *testRoundTrip) do(t testing.TB, req *http.Request, f func(*http.Request) (*http.Response, error)) {
	if rt.t != nil {
		t.Fatal("testRoundTrip can only be used once")
	}
	ctx, cancel := context.WithCancel(req.Context())
	req = req.WithContext(ctx)
	rt.t = t
	rt.donec = make(chan struct{})
	rt.cancel = cancel
	go func() {
		defer close(rt.donec)
		rt.resp, rt.respErr = f(req)
	}()
	synctest.Wait()

	t.Cleanup(func() {
		rt.cancel()
		if !rt.done() {
			return
		}
		res, _ := rt.result()
		if res != nil {
			res.Body.Close()
		}
	})
}

func (tc *testClientConn) greet(settings ...Setting) {
	tc.wantFrameType(FrameSettings)
	tc.wantFrameType(FrameWindowUpdate)
	tc.writeSettings(settings...)
	tc.writeSettingsAck()
	tc.wantFrameType(FrameSettings) // acknowledgement
}

// makeHeaderBlockFragment encodes headers in a form suitable for inclusion
// in a HEADERS or CONTINUATION frame.
//
// It takes a list of alternating names and values.
func (tc *testClientConn) makeHeaderBlockFragment(s ...string) []byte {
	if len(s)%2 != 0 {
		tc.t.Fatalf("uneven list of header name/value pairs")
	}
	tc.encbuf.Reset()
	for i := 0; i < len(s); i += 2 {
		tc.enc.WriteField(hpack.HeaderField{Name: s[i], Value: s[i+1]})
	}
	return tc.encbuf.Bytes()
}

// testRoundTrip manages a RoundTrip in progress.
type testRoundTrip struct {
	t       testing.TB
	resp    *http.Response
	respErr error
	donec   chan struct{}
	id      atomic.Uint32
	cancel  context.CancelFunc
}

// streamID returns the HTTP/2 stream ID of the request.
func (rt *testRoundTrip) streamID() uint32 {
	synctest.Wait()
	id := rt.id.Load()
	if id == 0 {
		panic("stream ID unknown")
	}
	return id
}

// done reports whether RoundTrip has returned.
func (rt *testRoundTrip) done() bool {
	synctest.Wait()
	select {
	case <-rt.donec:
		return true
	default:
		return false
	}
}

// result returns the result of the RoundTrip.
func (rt *testRoundTrip) result() (*http.Response, error) {
	t := rt.t
	t.Helper()
	synctest.Wait()
	select {
	case <-rt.donec:
	default:
		t.Fatalf("RoundTrip is not done; want it to be")
	}
	return rt.resp, rt.respErr
}

// response returns the response of a successful RoundTrip.
// If the RoundTrip unexpectedly failed, it calls t.Fatal.
func (rt *testRoundTrip) response() *http.Response {
	t := rt.t
	t.Helper()
	resp, err := rt.result()
	if err != nil {
		t.Fatalf("RoundTrip returned unexpected error: %v", rt.respErr)
	}
	if resp == nil {
		t.Fatalf("RoundTrip returned nil *Response and nil error")
	}
	return resp
}

// err returns the (possibly nil) error result of RoundTrip.
func (rt *testRoundTrip) err() error {
	t := rt.t
	t.Helper()
	_, err := rt.result()
	return err
}

// wantStatus indicates the expected response StatusCode.
func (rt *testRoundTrip) wantStatus(want int) {
	t := rt.t
	t.Helper()
	if got := rt.response().StatusCode; got != want {
		t.Fatalf("got response status %v, want %v", got, want)
	}
}

// readBody reads the contents of the response body.
func (rt *testRoundTrip) readBody() ([]byte, error) {
	t := rt.t
	t.Helper()
	return io.ReadAll(rt.response().Body)
}

// wantBody indicates the expected response body.
// (Note that this consumes the body.)
func (rt *testRoundTrip) wantBody(want []byte) {
	t := rt.t
	t.Helper()
	got, err := rt.readBody()
	if err != nil {
		t.Fatalf("unexpected error reading response body: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("unexpected response body:\ngot:  %q\nwant: %q", got, want)
	}
}

// wantHeaders indicates the expected response headers.
func (rt *testRoundTrip) wantHeaders(want http.Header) {
	t := rt.t
	t.Helper()
	res := rt.response()
	if diff := diffHeaders(res.Header, want); diff != "" {
		t.Fatalf("unexpected response headers:\n%v", diff)
	}
}

// wantTrailers indicates the expected response trailers.
func (rt *testRoundTrip) wantTrailers(want http.Header) {
	t := rt.t
	t.Helper()
	res := rt.response()
	if diff := diffHeaders(res.Trailer, want); diff != "" {
		t.Fatalf("unexpected response trailers:\n%v", diff)
	}
}

func diffHeaders(got, want http.Header) string {
	// nil and 0-length non-nil are equal.
	if len(got) == 0 && len(want) == 0 {
		return ""
	}
	// We could do a more sophisticated diff here.
	// DeepEqual is good enough for now.
	if reflect.DeepEqual(got, want) {
		return ""
	}
	return fmt.Sprintf("got:  %v\nwant: %v", got, want)
}

// roundTripTestMode selects which RoundTrip API a test uses.
type roundTripTestMode int

const (
	// roundTripNetHTTP uses net/http.Transport.RoundTrip or
	// net/http.ClientConn.RoundTrip:
	//
	//	t1 := http.Transport{}
	//	t2 := ConfigureTransports(t1)
	//	resp, err := t1.RoundTrip(req)
	//
	roundTripNetHTTP = roundTripTestMode(iota)

	// roundTripXNetHTTP2 uses x/net/http2.Transport.RoundTrip or
	// x/net/http2.ClientConn.RoundTrip:
	//
	//	t2 := http2.Transport{}
	//	resp, err := t2.RoundTrip(req)
	roundTripXNetHTTP2
)

// A testTransport allows testing Transport.RoundTrip against fake servers.
// Tests that aren't specifically exercising RoundTrip's retry loop or connection pooling
// should use testClientConn instead.
type testTransport struct {
	t    testing.TB
	tr   *Transport
	tr1  *http.Transport
	li   *synctestNetListener
	mode roundTripTestMode

	ccMu    sync.Mutex
	ccqueue []*testClientConn
	ccs     map[*synctestNetConn]*testClientConn

	ccpending []*testPendingClientConn

	useTLS bool
}

type testPendingClientConn struct {
	nc *synctestNetConn
	cc *ClientConn
	tc *testClientConn
}

func newTestTransport(t testing.TB, opts ...any) *testTransport {
	tt := &testTransport{
		t:    t,
		li:   newSynctestNetListener(),
		ccs:  make(map[*synctestNetConn]*testClientConn),
		mode: roundTripXNetHTTP2,
	}

	for _, o := range opts {
		switch o := o.(type) {
		case roundTripTestMode:
			tt.mode = o
		}
	}

	var (
		tr  *Transport
		tr1 *http.Transport
	)
	switch tt.mode {
	case roundTripXNetHTTP2:
		tr = &Transport{
			DialTLSContext: func(ctx context.Context, network, address string, tlsConf *tls.Config) (net.Conn, error) {
				if tt.useTLS {
					return tls.Client(tt.li.newConn(), tlsConf), nil
				}
				return tt.li.newConn(), nil
			},
			TLSClientConfig: testTLSClientConfig,
			AllowHTTP:       true,
		}
		tr1 = tr.TestTransport()
	case roundTripNetHTTP:
		tr1 = &http.Transport{}
	}

	tr1.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		return tt.li.newConn(), nil
	}
	tr1.TLSClientConfig = testTLSClientConfig
	tr1.Protocols = &http.Protocols{}
	tr1.Protocols.SetHTTP2(true)
	tr1.Protocols.SetUnencryptedHTTP2(true)
	t.Cleanup(tr1.CloseIdleConnections)

	if tt.mode == roundTripNetHTTP {
		var err error
		tr, err = ConfigureTransports(tr1)
		if err != nil {
			t.Fatal(err)
		}
	}

	for _, o := range opts {
		switch o := o.(type) {
		case func(*http.Transport):
			o(tr.TestTransport())
		case func(*Transport):
			o(tr)
		case *Transport:
			tr = o
		case roundTripTestMode:
			tt.mode = o
		case nil:
		default:
			t.Fatalf("unsupported option %T", o)
		}
	}
	tt.tr = tr
	tt.tr1 = tr.TestTransport()

	go tt.accept()

	// Install internal hooks when using the HTTP/2 implementation in this package.
	// This is a no-op when wrapping net/http's imeplementation.
	tt.maybeAddNewClientConnHook()

	t.Cleanup(func() {
		tt.li.Close()
		synctest.Wait()
		if len(tt.ccqueue) > 0 {
			t.Fatalf("%v test ClientConns created, but not examined by test", len(tt.ccqueue))
		}
	})

	return tt
}

func (tt *testTransport) addPending(nc *synctestNetConn, cc *ClientConn, tc *testClientConn) {
	tt.ccMu.Lock()
	defer tt.ccMu.Unlock()

	for i, p := range tt.ccpending {
		if p.nc != nc {
			break
		}
		if p.tc != nil {
			p.tc.cc = cc
		} else if tc != nil {
			tc.cc = p.cc
		} else {
			panic("found matching ccpending for conn with no tc")
		}
		tt.ccpending = slices.Delete(tt.ccpending, i, i+1)
		return
	}

	tt.ccpending = append(tt.ccpending, &testPendingClientConn{
		nc: nc,
		cc: cc,
		tc: tc,
	})
}

func (tt *testTransport) accept() {
	for {
		nc, err := tt.li.Accept()
		if err != nil {
			return
		}
		tc := newTestClientConnFromNetConn(tt, nc)
		tt.addPending(nc.(*synctestNetConn), nil, tc)
		tt.ccqueue = append(tt.ccqueue, tc)
	}
}

func (tt *testTransport) hasConn() bool {
	return len(tt.ccqueue) > 0
}

func (tt *testTransport) getConn() *testClientConn {
	tt.t.Helper()
	synctest.Wait()
	tt.ccMu.Lock()
	if len(tt.ccqueue) == 0 {
		tt.ccMu.Unlock()
		tt.t.Fatalf("no new ClientConns created; wanted one")
	}
	tc := tt.ccqueue[0]
	tt.ccqueue = tt.ccqueue[1:]
	tt.ccMu.Unlock()
	tc.readClientPreface()
	return tc
}

func (tt *testTransport) roundTrip(req *http.Request) *testRoundTrip {
	ctx, cancel := context.WithCancel(req.Context())
	req = req.WithContext(ctx)
	rt := &testRoundTrip{
		t:      tt.t,
		donec:  make(chan struct{}),
		cancel: cancel,
	}

	go func() {
		defer close(rt.donec)
		switch tt.mode {
		case roundTripXNetHTTP2:
			rt.resp, rt.respErr = tt.tr.RoundTrip(req)
		case roundTripNetHTTP:
			rt.resp, rt.respErr = tt.tr1.RoundTrip(req)
		}
	}()
	synctest.Wait()

	tt.t.Cleanup(func() {
		rt.cancel()
		if !rt.done() {
			return
		}
		res, _ := rt.result()
		if res != nil {
			res.Body.Close()
		}
	})

	return rt
}

type nonblockingReader struct {
	mu    sync.Mutex
	buf   bytes.Buffer
	err   error
	waitc chan struct{}
	stopc chan struct{}
}

func newNonblockingReader(reader io.Reader) *nonblockingReader {
	r := &nonblockingReader{}
	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := reader.Read(buf)
			r.mu.Lock()
			if n > 0 {
				r.buf.Write(buf[:n])
			}
			if err != nil {
				r.err = err
			}
			if r.waitc != nil {
				close(r.waitc)
				r.waitc = nil
			}
			stopc := r.stopc
			r.mu.Unlock()
			if err != nil {
				return
			}
			if stopc != nil {
				<-stopc
			}
		}
	}()
	return r
}

func (r *nonblockingReader) Read(p []byte) (n int, err error) {
	synctest.Wait()
	r.mu.Lock()
	defer r.mu.Unlock()
	n, err = r.buf.Read(p)
	if err == io.EOF {
		if r.err != nil {
			err = r.err
		} else {
			err = errWouldBlock
		}
	}
	return n, err
}

func (r *nonblockingReader) waitForData(t testing.TB) time.Duration {
	t.Helper()
	synctest.Wait()
	waitc := func() chan struct{} {
		r.mu.Lock()
		defer r.mu.Unlock()
		if r.buf.Len() > 0 || r.err != nil {
			return nil
		}
		if r.waitc == nil {
			r.waitc = make(chan struct{})
		}
		return r.waitc
	}()
	if waitc == nil {
		return 0
	}
	start := time.Now()
	select {
	case <-waitc:
	case <-time.After(1 * time.Hour):
		t.Fatalf("waited an hour for connection data, saw none")
	}
	return time.Since(start)
}

func (r *nonblockingReader) stop() {
	synctest.Wait()
	if r.stopc != nil {
		panic("stopping stopped reader")
	}
	r.stopc = make(chan struct{})
}

func (r *nonblockingReader) start() {
	synctest.Wait()
	if r.stopc == nil {
		panic("starting started reader")
	}
	stopc := r.stopc
	r.stopc = nil
	close(stopc)
}

var errWouldBlock = errors.New("would block")
