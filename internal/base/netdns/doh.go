package netdns

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// This file adds DNS over HTTPS, and the reason it exists is the reason the
// rest of the package does: the answers a phone gets cannot be trusted.
//
// A plain UDP resolver on a tampered path is answered by the tamperer, not by
// the name's own server — that is what a poisoned `opencode.ai` resolving to a
// domestic address is — and moving to a different port-53 server does not help,
// because the interception is at the network, not at the server. DoH carries
// the query inside TLS to a named endpoint, so the answer comes from whoever
// holds that endpoint's certificate. It is the same defence the provider
// connection itself relies on, applied one layer earlier, and it is the only
// one that works when the path is the problem.
//
// The shape below is dictated by Go's resolver, which wants a `net.Conn` and
// then speaks one of two framings to it depending on whether the conn claims to
// be a `PacketConn`. Ours does not, so Go uses the stream framing: a query
// arrives length-prefixed, and the reply must leave that way. Doing the HTTPS
// exchange inside `Write`, and handing the answer back from `Read`, is what lets
// the standard resolver drive a protocol it has never heard of.

// dohConn is one connection's worth of DoH exchanges, driven by the resolver.
//
// It is written for the sequence the resolver actually performs — write one
// query, read one answer, close — rather than as a general stream, because
// pretending to be one would mean buffering an unbounded amount of state to
// preserve a property nothing uses.
type dohConn struct {
	endpoint string
	client   *http.Client

	mu     sync.Mutex
	reply  []byte
	offset int

	deadline time.Time
}

func (c *dohConn) Write(p []byte) (int, error) {
	query, err := unframe(p)
	if err != nil {
		return 0, err
	}
	reply, err := c.exchange(query)
	if err != nil {
		return 0, err
	}
	c.mu.Lock()
	c.reply, c.offset = frame(reply), 0
	c.mu.Unlock()
	return len(p), nil
}

func (c *dohConn) Read(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.offset >= len(c.reply) {
		return 0, io.EOF
	}
	n := copy(p, c.reply[c.offset:])
	c.offset += n
	return n, nil
}

func (c *dohConn) Close() error { return nil }

func (c *dohConn) LocalAddr() net.Addr  { return dohAddr(c.endpoint) }
func (c *dohConn) RemoteAddr() net.Addr { return dohAddr(c.endpoint) }

func (c *dohConn) SetDeadline(t time.Time) error {
	c.mu.Lock()
	c.deadline = t
	c.mu.Unlock()
	return nil
}
func (c *dohConn) SetReadDeadline(t time.Time) error  { return c.SetDeadline(t) }
func (c *dohConn) SetWriteDeadline(t time.Time) error { return c.SetDeadline(t) }

// exchange performs one DoH lookup. The wire format is RFC 8484's POST form:
// the query goes as the request body under a content type that says what it is,
// and the response body is the message with no envelope around it.
func (c *dohConn) exchange(query []byte) ([]byte, error) {
	ctx := context.Background()
	c.mu.Lock()
	deadline := c.deadline
	c.mu.Unlock()
	if !deadline.IsZero() {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, deadline)
		defer cancel()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(query))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", dohMediaType)
	req.Header.Set("Accept", dohMediaType)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		// The body of a refusal is the endpoint's own wording — a wrong path, a
		// rate limit, a subscription that lapsed — and reading it is the
		// difference between a fixable setting and an unexplained outage.
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("dns over https: %s answered %d: %s", c.endpoint, resp.StatusCode, strings.TrimSpace(string(detail)))
	}
	// A DNS answer is small; a megabyte of it is a server that is not answering
	// in DNS. Bounding the read keeps a hostile or broken endpoint from filling
	// memory on a phone.
	reply, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return nil, err
	}
	if len(reply) < headerLen {
		return nil, fmt.Errorf("dns over https: %s answered with %d bytes, which is not a DNS message", c.endpoint, len(reply))
	}
	return reply, nil
}

const dohMediaType = "application/dns-message"

// dohResolver builds a resolver that sends every lookup to one DoH endpoint.
//
// One endpoint and no list: unlike the shell's servers, this is a setting the
// person typed, so falling over to a different name server behind their back
// would be answering with a different resolver than the one they chose. A
// failure is reported instead, and the next setting is theirs to change.
//
// `proxy` is handed in rather than read from the environment, because the
// kernel's own proxy setting is the one that matters and it is not always in
// the environment: a custom proxy is a config entry, and a DoH query that
// ignored it would go direct from a machine whose only way out is that proxy.
func dohResolver(endpoint string, proxy func(*http.Request) (*url.URL, error)) *net.Resolver {
	client := &http.Client{
		// The DNS query is already inside TLS, and a redirect would move the
		// exchange to an endpoint whose certificate this setting never named.
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			// The same egress the rest of the kernel uses.
			Proxy: proxy,
			// A fresh connection per lookup would pay a TCP and TLS handshake
			// for every name resolved, which is the one thing DoH can be
			// accused of. Kept small: this is a phone.
			MaxIdleConns:        4,
			MaxIdleConnsPerHost: 2,
			IdleConnTimeout:     90 * time.Second,
		},
	}
	return &net.Resolver{
		// Required for the same reason as the plain path: without it
		// `net.Resolver` looks for the cgo resolver this build does not have.
		PreferGo: true,
		Dial: func(_ context.Context, _, _ string) (net.Conn, error) {
			// The address the resolver passes is a name server it read from the
			// environment; it is ignored on purpose, because the whole point of
			// this path is to not ask whatever the machine was told to ask.
			return &dohConn{endpoint: endpoint, client: client}, nil
		},
	}
}

// frame prefixes a DNS message with the two-byte length the stream framing
// expects. Getting this wrong makes a working endpoint look like a malformed
// one, because the resolver reads the first two bytes as a size.
func frame(msg []byte) []byte {
	out := make([]byte, 2+len(msg))
	binary.BigEndian.PutUint16(out, uint16(len(msg)))
	copy(out[2:], msg)
	return out
}

// unframe is the other direction, and it tolerates a message that is not
// prefixed: a resolver configured for the packet framing would send one bare,
// and refusing to answer it would be a failure with no cause a reader could
// find.
func unframe(b []byte) ([]byte, error) {
	if len(b) >= 2 {
		if size := int(binary.BigEndian.Uint16(b[:2])); size == len(b)-2 {
			return b[2:], nil
		}
	}
	if len(b) < headerLen {
		return nil, fmt.Errorf("dns over https: refusing to send %d bytes, which is not a DNS query", len(b))
	}
	return b, nil
}

type dohAddr string

func (a dohAddr) Network() string { return "https" }
func (a dohAddr) String() string  { return string(a) }
