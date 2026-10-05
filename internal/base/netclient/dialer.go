package netclient

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/proxy"
)

// StreamDialer opens a raw TCP stream under the same proxy policy netclient
// applies to HTTP. It is the dial seam for non-HTTP protocols (SSH) so a
// user's configured proxy is honored consistently.
type StreamDialer interface {
	DialContext(ctx context.Context, network, addr string) (net.Conn, error)
}

// DialerFunc adapts a function to StreamDialer.
type DialerFunc func(ctx context.Context, network, addr string) (net.Conn, error)

// DialContext implements StreamDialer.
func (f DialerFunc) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	return f(ctx, network, addr)
}

// NewStreamDialer builds a StreamDialer for spec. Off/env/auto with no
// applicable proxy dial directly; socks5/socks5h dial through the SOCKS proxy;
// http/https dial through an HTTP CONNECT tunnel. DirectHosts and NoProxy are
// honored via the shared proxy resolution.
func NewStreamDialer(spec ProxySpec) (StreamDialer, error) {
	pf, err := proxyFunc(spec)
	if err != nil {
		return nil, err
	}
	base := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
	if pf == nil {
		return DialerFunc(base.DialContext), nil
	}
	return DialerFunc(func(ctx context.Context, network, addr string) (net.Conn, error) {
		// proxyFunc keys off a request URL; synthesize one for the target so
		// scheme-agnostic TCP dials reuse the exact NoProxy/DirectHosts logic.
		host, _, splitErr := net.SplitHostPort(addr)
		if splitErr != nil {
			host = addr
		}
		probe := &http.Request{URL: &url.URL{Scheme: "https", Host: host}}
		pu, perr := pf(probe)
		if perr != nil {
			return nil, perr
		}
		if pu == nil {
			return base.DialContext(ctx, network, addr)
		}
		switch strings.ToLower(pu.Scheme) {
		case "socks5", "socks5h":
			return dialSOCKS5(ctx, pu, base, network, addr)
		case "http", "https":
			return dialHTTPConnect(ctx, pu, base, addr)
		default:
			return nil, fmt.Errorf("netclient: unsupported proxy scheme %q for stream dial", pu.Scheme)
		}
	}), nil
}

func dialSOCKS5(ctx context.Context, pu *url.URL, fwd *net.Dialer, network, addr string) (net.Conn, error) {
	var auth *proxy.Auth
	if pu.User != nil {
		pw, _ := pu.User.Password()
		auth = &proxy.Auth{User: pu.User.Username(), Password: pw}
	}
	d, err := proxy.SOCKS5("tcp", pu.Host, auth, fwd)
	if err != nil {
		return nil, err
	}
	if cd, ok := d.(proxy.ContextDialer); ok {
		return cd.DialContext(ctx, network, addr)
	}
	return d.Dial(network, addr)
}

// dialHTTPConnect opens a CONNECT tunnel through an http/https proxy. For an
// https proxy the connection to the proxy itself must be TLS: the CONNECT
// request (including Proxy-Authorization credentials) is sent inside that TLS
// session, not in cleartext. The SSH client then speaks its own protocol over
// the established stream, so only the tunnel handshake lives here.
func dialHTTPConnect(ctx context.Context, pu *url.URL, base *net.Dialer, target string) (net.Conn, error) {
	conn, err := base.DialContext(ctx, "tcp", pu.Host)
	if err != nil {
		return nil, err
	}
	if strings.EqualFold(pu.Scheme, "https") {
		host := pu.Hostname()
		tconn := tls.Client(conn, &tls.Config{ServerName: host})
		hsCtx := ctx
		if _, ok := ctx.Deadline(); !ok {
			var cancel context.CancelFunc
			hsCtx, cancel = context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
		}
		if herr := tconn.HandshakeContext(hsCtx); herr != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("netclient: TLS handshake to https proxy %s: %w", pu.Host, herr)
		}
		conn = tconn
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	req := &http.Request{
		Method: http.MethodConnect,
		URL:    &url.URL{Opaque: target},
		Host:   target,
		Header: make(http.Header),
	}
	if pu.User != nil {
		pw, _ := pu.User.Password()
		req.SetBasicAuth(pu.User.Username(), pw)
		req.Header.Set("Proxy-Authorization", req.Header.Get("Authorization"))
		req.Header.Del("Authorization")
	}
	if err := req.Write(conn); err != nil {
		conn.Close()
		return nil, err
	}
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, req)
	if err != nil {
		conn.Close()
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		conn.Close()
		return nil, fmt.Errorf("netclient: proxy CONNECT to %s failed: %s", target, resp.Status)
	}
	// Clear the handshake deadline; the caller manages timeouts thereafter.
	_ = conn.SetDeadline(time.Time{})
	if br.Buffered() > 0 {
		// A well-behaved proxy sends nothing before the tunnel opens; if it
		// did, wrap so buffered bytes aren't lost.
		return &bufferedConn{Conn: conn, r: br}, nil
	}
	return conn, nil
}

type bufferedConn struct {
	net.Conn
	r *bufio.Reader
}

func (b *bufferedConn) Read(p []byte) (int, error) { return b.r.Read(p) }
