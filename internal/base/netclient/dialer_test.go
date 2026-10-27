package netclient

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
)

// The target is never dialed directly in these tests: the proxy plays the far
// end, and a name off loopback keeps the proxy policy from exempting it.
const streamTarget = "target.example:22"

// fakeProxy accepts one connection on a loopback port and hands it to serve.
func fakeProxy(t *testing.T, serve func(net.Conn)) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(testenv.Budget(t)))
		serve(conn)
	}()
	return ln.Addr().String()
}

func streamProxySpec(t *testing.T, typ, addr string) ProxySpec {
	t.Helper()
	spec := structuredProxySpec(t, typ, typ+"://"+addr)
	spec.Username, spec.Password = "alice", "s3cr:t"
	return spec
}

func dialStream(t *testing.T, spec ProxySpec) (net.Conn, error) {
	t.Helper()
	d, err := NewStreamDialer(spec)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), testenv.Budget(t))
	defer cancel()
	return d.DialContext(ctx, "tcp", streamTarget)
}

// answerPing is the far end of a tunnel: "ping" in, "pong" out.
func answerPing(conn net.Conn, r io.Reader) {
	if _, err := io.ReadFull(r, make([]byte, 4)); err == nil {
		_, _ = conn.Write([]byte("pong"))
	}
}

func pingThrough(t *testing.T, conn net.Conn) {
	t.Helper()
	_ = conn.SetDeadline(time.Now().Add(testenv.Budget(t)))
	if _, err := conn.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, 4)
	if _, err := io.ReadFull(conn, got); err != nil || string(got) != "pong" {
		t.Fatalf("stream through the proxy read %q (%v), want pong", got, err)
	}
}

func TestStreamDialTunnelsThroughAnHTTPProxyWithProxyCredentials(t *testing.T) {
	seen := make(chan *http.Request, 1)
	addr := fakeProxy(t, func(conn net.Conn) {
		r := bufio.NewReader(conn)
		req, err := http.ReadRequest(r)
		if err != nil {
			return
		}
		seen <- req
		_, _ = conn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
		answerPing(conn, r)
	})
	conn, err := dialStream(t, streamProxySpec(t, "http", addr))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	req := <-seen
	if req.Method != http.MethodConnect || req.RequestURI != streamTarget || req.Host != streamTarget {
		t.Fatalf("proxy got %s %s (Host %s), want CONNECT %s", req.Method, req.RequestURI, req.Host, streamTarget)
	}
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("alice:s3cr:t"))
	if got := req.Header.Get("Proxy-Authorization"); got != want {
		t.Fatalf("Proxy-Authorization = %q, want %q", got, want)
	}
	if got := req.Header.Get("Authorization"); got != "" {
		t.Fatalf("the proxy's credentials also went out as Authorization (%q), which a proxy passes on to the target", got)
	}
	pingThrough(t, conn)
}

func TestStreamDialRefusedByTheProxyOpensNoStream(t *testing.T) {
	addr := fakeProxy(t, func(conn net.Conn) {
		if _, err := http.ReadRequest(bufio.NewReader(conn)); err == nil {
			_, _ = conn.Write([]byte("HTTP/1.1 407 Proxy Authentication Required\r\nContent-Length: 0\r\n\r\n"))
		}
	})
	conn, err := dialStream(t, streamProxySpec(t, "http", addr))
	if err == nil {
		conn.Close()
		t.Fatal("a 407 from the proxy was handed back as an open stream")
	}
}

func TestStreamDialSendsNothingInCleartextToAnHTTPSProxy(t *testing.T) {
	first := make(chan []byte, 1)
	addr := fakeProxy(t, func(conn net.Conn) {
		buf := make([]byte, 4096)
		n, _ := conn.Read(buf)
		first <- buf[:n]
	})
	conn, err := dialStream(t, streamProxySpec(t, "https", addr))
	if err == nil {
		conn.Close()
		t.Fatal("an https proxy that never finished TLS opened a stream")
	}
	got := <-first
	if len(got) == 0 || got[0] != 0x16 {
		t.Fatalf("first bytes to an https proxy = %q, want a TLS handshake record", got)
	}
	secret := base64.StdEncoding.EncodeToString([]byte("alice:s3cr:t"))
	for _, leak := range []string{"CONNECT", streamTarget, "s3cr:t", secret} {
		if bytes.Contains(got, []byte(leak)) {
			t.Fatalf("%q crossed to the https proxy in cleartext", leak)
		}
	}
}

func TestStreamDialThroughASOCKS5ProxyAuthenticatesAndNamesTheTarget(t *testing.T) {
	type socksRequest struct {
		user, pass, host string
		port             uint16
	}
	seen := make(chan socksRequest, 1)
	addr := fakeProxy(t, func(conn net.Conn) {
		r := bufio.NewReader(conn)
		read := func(n int) []byte {
			b := make([]byte, n)
			if _, err := io.ReadFull(r, b); err != nil {
				return nil
			}
			return b
		}
		readLen := func() []byte {
			n := read(1)
			if n == nil {
				return nil
			}
			return read(int(n[0]))
		}
		hello := read(2)
		if hello == nil || hello[0] != 5 || !bytes.Contains(read(int(hello[1])), []byte{2}) {
			return
		}
		_, _ = conn.Write([]byte{5, 2})
		var got socksRequest
		if ver := read(1); ver == nil || ver[0] != 1 {
			return
		}
		got.user, got.pass = string(readLen()), string(readLen())
		_, _ = conn.Write([]byte{1, 0})
		head := read(4)
		if head == nil || head[1] != 1 || head[3] != 3 {
			return
		}
		got.host = string(readLen())
		if port := read(2); port != nil {
			got.port = binary.BigEndian.Uint16(port)
		}
		seen <- got
		_, _ = conn.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0})
		answerPing(conn, r)
	})
	conn, err := dialStream(t, streamProxySpec(t, "socks5", addr))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	got := <-seen
	if got != (socksRequest{user: "alice", pass: "s3cr:t", host: "target.example", port: 22}) {
		t.Fatalf("SOCKS5 proxy got %+v, want alice's credentials and target.example:22 by name", got)
	}
	pingThrough(t, conn)
}
