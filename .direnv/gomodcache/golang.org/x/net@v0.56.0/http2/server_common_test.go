// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package http2_test

import (
	"bytes"
	"crypto/tls"
	"flag"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	. "golang.org/x/net/http2"
	"golang.org/x/net/http2/hpack"
)

var stderrVerbose = flag.Bool("stderr_verbose", false, "Mirror verbosity to stderr, unbuffered")

func stderrv() io.Writer {
	if *stderrVerbose {
		return os.Stderr
	}

	return io.Discard
}

type safeBuffer struct {
	b bytes.Buffer
	m sync.Mutex
}

func (sb *safeBuffer) Write(d []byte) (int, error) {
	sb.m.Lock()
	defer sb.m.Unlock()
	return sb.b.Write(d)
}

func (sb *safeBuffer) Bytes() []byte {
	sb.m.Lock()
	defer sb.m.Unlock()
	return sb.b.Bytes()
}

func (sb *safeBuffer) Len() int {
	sb.m.Lock()
	defer sb.m.Unlock()
	return sb.b.Len()
}

type serverTester struct {
	cc           net.Conn // client conn
	t            testing.TB
	h1server     *http.Server
	h2server     *Server
	serverLogBuf safeBuffer // logger for httptest.Server
	logFilter    []string   // substrings to filter out
	scMu         sync.Mutex // guards sc
	sc           *ServerConn
	testConnFramer

	callsMu sync.Mutex
	calls   []*serverHandlerCall

	// If http2debug!=2, then we capture Frame debug logs that will be written
	// to t.Log after a test fails. The read and write logs use separate locks
	// and buffers so we don't accidentally introduce synchronization between
	// the read and write goroutines, which may hide data races.
	frameReadLogMu   sync.Mutex
	frameReadLogBuf  bytes.Buffer
	frameWriteLogMu  sync.Mutex
	frameWriteLogBuf bytes.Buffer

	// writing headers:
	headerBuf bytes.Buffer
	hpackEnc  *hpack.Encoder
}

type twriter struct {
	t  testing.TB
	st *serverTester // optional
}

func (w twriter) Write(p []byte) (n int, err error) {
	if w.st != nil {
		ps := string(p)
		for _, phrase := range w.st.logFilter {
			if strings.Contains(ps, phrase) {
				return len(p), nil // no logging
			}
		}
	}
	w.t.Logf("%s", p)
	return len(p), nil
}

type serverTesterOpt string

var (
	optFramerReuseFrames = serverTesterOpt("frame_reuse_frames")

	// optNoConn indicates that newServerTester should not create a connection.
	// The test will create its own.
	optNoConn = serverTesterOpt("no_conn")

	optQuiet = func(server *http.Server) {
		server.ErrorLog = log.New(io.Discard, "", 0)
	}
)

func newServerTester(t testing.TB, handler http.HandlerFunc, opts ...interface{}) *serverTester {
	t.Helper()

	h1server := &http.Server{}
	h2server := &Server{}
	tlsState := tls.ConnectionState{
		Version:            tls.VersionTLS13,
		ServerName:         "go.dev",
		CipherSuite:        tls.TLS_AES_128_GCM_SHA256,
		NegotiatedProtocol: "h2",
	}
	noConn := false
	for _, opt := range opts {
		switch v := opt.(type) {
		case func(*Server):
			v(h2server)
		case func(*http.Server):
			v(h1server)
		case func(*tls.ConnectionState):
			v(&tlsState)
		case serverTesterOpt:
			switch v {
			case optNoConn:
				noConn = true
			default:
				t.Fatalf("unhandled option %v", v)
			}
		default:
			t.Fatalf("unknown newServerTester option type %T", v)
		}
	}
	ConfigureServer(h1server, h2server)

	cli, srv := synctestNetPipe()
	cli.SetReadDeadline(time.Now())
	cli.autoWait = true
	t.Cleanup(func() {
		cli.Close()
		srv.Close()
	})

	st := &serverTester{
		t:        t,
		cc:       cli,
		h1server: h1server,
		h2server: h2server,
	}
	st.hpackEnc = hpack.NewEncoder(&st.headerBuf)
	if h1server.ErrorLog == nil {
		h1server.ErrorLog = log.New(io.MultiWriter(stderrv(), twriter{t: t, st: st}, &st.serverLogBuf), "", log.LstdFlags)
	}

	if handler == nil {
		handler = serverTesterHandler{st}.ServeHTTP
	}

	t.Cleanup(func() {
		st.Close()
		time.Sleep(GoAwayTimeout) // give server time to shut down
	})

	if noConn {
		return st
	}

	var connc chan *ServerConn
	if !wrappedAPI {
		connc = make(chan *ServerConn)
		h2server.TestSetNewConnFunc(func(sc *ServerConn) {
			connc <- sc
			close(connc) // panic if we unexpectedly get a second conn
		})
	}

	go func() {
		h2server.ServeConn(&netConnWithConnectionState{
			Conn:  srv,
			state: tlsState,
		}, &ServeConnOpts{
			Handler:    handler,
			BaseConfig: h1server,
		})
	}()

	if !wrappedAPI {
		st.sc = <-connc
	}

	st.testConnFramer = testConnFramer{
		t:   t,
		fr:  NewFramer(st.cc, st.cc),
		dec: hpack.NewDecoder(InitialHeaderTableSize, nil),
	}
	synctest.Wait()
	return st
}

// testServerConn is a single connection to a server.
type testServerConn struct {
	t       testing.TB
	netconn *synctestNetConn
	testConnFramer
}

func newTestServerConn(t testing.TB, nc *synctestNetConn) *testServerConn {
	tc := &testServerConn{
		t:       t,
		netconn: nc,
	}
	tc.testConnFramer = testConnFramer{
		t:   t,
		fr:  NewFramer(nc, nc),
		dec: hpack.NewDecoder(InitialHeaderTableSize, nil),
	}
	return tc
}

func (tc *testServerConn) writePreface() {
	tc.t.Helper()
	n, err := tc.netconn.Write([]byte(ClientPreface))
	if err != nil {
		tc.t.Fatalf("Error writing client preface: %v", err)
	}
	if n != len(ClientPreface) {
		tc.t.Fatalf("Writing client preface, wrote %d bytes; want %d", n, len(ClientPreface))
	}
}

func (tc *testServerConn) greet() {
	tc.t.Helper()
	tc.writePreface()
	tc.wantFrameType(FrameSettings)
	tc.wantFrameType(FrameWindowUpdate)
	tc.writeSettings()
	tc.wantFrameType(FrameSettings) // ACK
	tc.writeSettingsAck()
}

type netConnWithConnectionState struct {
	net.Conn
	state tls.ConnectionState
}

func (c *netConnWithConnectionState) ConnectionState() tls.ConnectionState {
	return c.state
}

type serverTesterHandler struct {
	st *serverTester
}

func (h serverTesterHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	call := &serverHandlerCall{
		w:   w,
		req: req,
		ch:  make(chan func()),
	}
	h.st.t.Cleanup(call.exit)
	h.st.callsMu.Lock()
	h.st.calls = append(h.st.calls, call)
	h.st.callsMu.Unlock()
	for f := range call.ch {
		f()
	}
}

// serverHandlerCall is a call to the server handler's ServeHTTP method.
type serverHandlerCall struct {
	w         http.ResponseWriter
	req       *http.Request
	closeOnce sync.Once
	ch        chan func()
}

// do executes f in the handler's goroutine.
func (call *serverHandlerCall) do(f func(http.ResponseWriter, *http.Request)) {
	donec := make(chan struct{})
	call.ch <- func() {
		defer close(donec)
		f(call.w, call.req)
	}
	<-donec
}

// exit causes the handler to return.
func (call *serverHandlerCall) exit() {
	call.closeOnce.Do(func() {
		close(call.ch)
	})
}

func (st *serverTester) Close() {
	if st.t.Failed() {
		st.frameReadLogMu.Lock()
		if st.frameReadLogBuf.Len() > 0 {
			st.t.Logf("Framer read log:\n%s", st.frameReadLogBuf.String())
		}
		st.frameReadLogMu.Unlock()

		st.frameWriteLogMu.Lock()
		if st.frameWriteLogBuf.Len() > 0 {
			st.t.Logf("Framer write log:\n%s", st.frameWriteLogBuf.String())
		}
		st.frameWriteLogMu.Unlock()

		// If we failed already (and are likely in a Fatal,
		// unwindowing), force close the connection, so the
		// httptest.Server doesn't wait forever for the conn
		// to close.
		if st.cc != nil {
			st.cc.Close()
		}
	}
	if st.cc != nil {
		st.cc.Close()
	}
	log.SetOutput(os.Stderr)
}

// greet initiates the client's HTTP/2 connection into a state where
// frames may be sent.
func (st *serverTester) greet() {
	st.t.Helper()
	st.greetAndCheckSettings(func(Setting) error { return nil })
}

func (st *serverTester) greetAndCheckSettings(checkSetting func(s Setting) error) {
	st.t.Helper()
	st.writePreface()
	st.writeSettings()
	st.sync()
	readFrame[*SettingsFrame](st.t, st).ForeachSetting(checkSetting)
	st.writeSettingsAck()

	// The initial WINDOW_UPDATE and SETTINGS ACK can come in any order.
	var gotSettingsAck bool
	var gotWindowUpdate bool

	for i := 0; i < 2; i++ {
		f := st.readFrame()
		if f == nil {
			st.t.Fatal("wanted a settings ACK and window update, got none")
		}
		switch f := f.(type) {
		case *SettingsFrame:
			if !f.Header().Flags.Has(FlagSettingsAck) {
				st.t.Fatal("Settings Frame didn't have ACK set")
			}
			gotSettingsAck = true

		case *WindowUpdateFrame:
			if f.FrameHeader.StreamID != 0 {
				st.t.Fatalf("WindowUpdate StreamID = %d; want 0", f.FrameHeader.StreamID)
			}
			gotWindowUpdate = true

		default:
			st.t.Fatalf("Wanting a settings ACK or window update, received a %T", f)
		}
	}

	if !gotSettingsAck {
		st.t.Fatalf("Didn't get a settings ACK")
	}
	if !gotWindowUpdate {
		st.t.Fatalf("Didn't get a window update")
	}
}

func (st *serverTester) writePreface() {
	n, err := st.cc.Write([]byte(ClientPreface))
	if err != nil {
		st.t.Fatalf("Error writing client preface: %v", err)
	}
	if n != len(ClientPreface) {
		st.t.Fatalf("Writing client preface, wrote %d bytes; want %d", n, len(ClientPreface))
	}
}

func (st *serverTester) encodeHeaderField(k, v string) {
	err := st.hpackEnc.WriteField(hpack.HeaderField{Name: k, Value: v})
	if err != nil {
		st.t.Fatalf("HPACK encoding error for %q/%q: %v", k, v, err)
	}
}

// encodeHeaderRaw is the magic-free version of encodeHeader.
// It takes 0 or more (k, v) pairs and encodes them.
func (st *serverTester) encodeHeaderRaw(headers ...string) []byte {
	if len(headers)%2 == 1 {
		panic("odd number of kv args")
	}
	st.headerBuf.Reset()
	for len(headers) > 0 {
		k, v := headers[0], headers[1]
		st.encodeHeaderField(k, v)
		headers = headers[2:]
	}
	return st.headerBuf.Bytes()
}

// encodeHeader encodes headers and returns their HPACK bytes. headers
// must contain an even number of key/value pairs. There may be
// multiple pairs for keys (e.g. "cookie").  The :method, :path, and
// :scheme headers default to GET, / and https. The :authority header
// defaults to st.ts.Listener.Addr().
func (st *serverTester) encodeHeader(headers ...string) []byte {
	if len(headers)%2 == 1 {
		panic("odd number of kv args")
	}

	st.headerBuf.Reset()
	defaultAuthority := st.authority()

	if len(headers) == 0 {
		// Fast path, mostly for benchmarks, so test code doesn't pollute
		// profiles when we're looking to improve server allocations.
		st.encodeHeaderField(":method", "GET")
		st.encodeHeaderField(":scheme", "https")
		st.encodeHeaderField(":authority", defaultAuthority)
		st.encodeHeaderField(":path", "/")
		return st.headerBuf.Bytes()
	}

	if len(headers) == 2 && headers[0] == ":method" {
		// Another fast path for benchmarks.
		st.encodeHeaderField(":method", headers[1])
		st.encodeHeaderField(":scheme", "https")
		st.encodeHeaderField(":authority", defaultAuthority)
		st.encodeHeaderField(":path", "/")
		return st.headerBuf.Bytes()
	}

	pseudoCount := map[string]int{}
	keys := []string{":method", ":scheme", ":authority", ":path"}
	vals := map[string][]string{
		":method":    {"GET"},
		":scheme":    {"https"},
		":authority": {defaultAuthority},
		":path":      {"/"},
	}
	for len(headers) > 0 {
		k, v := headers[0], headers[1]
		headers = headers[2:]
		if _, ok := vals[k]; !ok {
			keys = append(keys, k)
		}
		if strings.HasPrefix(k, ":") {
			pseudoCount[k]++
			if pseudoCount[k] == 1 {
				vals[k] = []string{v}
			} else {
				// Allows testing of invalid headers w/ dup pseudo fields.
				vals[k] = append(vals[k], v)
			}
		} else {
			vals[k] = append(vals[k], v)
		}
	}
	for _, k := range keys {
		for _, v := range vals[k] {
			st.encodeHeaderField(k, v)
		}
	}
	return st.headerBuf.Bytes()
}

// bodylessReq1 writes a HEADERS frames with StreamID 1 and EndStream and EndHeaders set.
func (st *serverTester) bodylessReq1(headers ...string) {
	st.writeHeaders(HeadersFrameParam{
		StreamID:      1, // clients send odd numbers
		BlockFragment: st.encodeHeader(headers...),
		EndStream:     true,
		EndHeaders:    true,
	})
}

// sync waits for all goroutines to idle.
func (st *serverTester) sync() {
	synctest.Wait()
}

// advance advances synthetic time by a duration.
func (st *serverTester) advance(d time.Duration) {
	time.Sleep(d)
	synctest.Wait()
}

func (st *serverTester) authority() string {
	return "dummy.tld"
}

func (st *serverTester) addLogFilter(phrase string) {
	st.logFilter = append(st.logFilter, phrase)
}

func (st *serverTester) nextHandlerCall() *serverHandlerCall {
	st.t.Helper()
	synctest.Wait()
	st.callsMu.Lock()
	defer st.callsMu.Unlock()
	if len(st.calls) == 0 {
		st.t.Fatal("expected server handler call, got none")
	}
	call := st.calls[0]
	st.calls = st.calls[1:]
	return call
}
