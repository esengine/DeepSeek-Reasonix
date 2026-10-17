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

// DNS over HTTPS: a plain resolver on a tampered path is answered by the
// tamperer, so the query goes inside TLS to a named endpoint instead. The conn
// below is shaped by Go's resolver, which speaks the stream framing to it.

// dohConn is one connection's worth of DoH exchanges, driven by the resolver: it
// is written for the sequence the resolver performs — write one query, read one
// answer, close — rather than as a general stream.
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
	if err := checkAnswer(c.endpoint, query, reply); err != nil {
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

// checkAnswer refuses a reply that is not the answer to this query: the
// transaction id must match and the QR bit must say response. One connection
// carries one query at a time, so a mismatch means a confused or hostile
// endpoint is feeding one lookup another's answer.
func checkAnswer(endpoint string, query, reply []byte) error {
	if len(reply) < headerLen {
		return fmt.Errorf("dns over https: %s answered with %d bytes, which is not a DNS message", endpoint, len(reply))
	}
	if reply[0] != query[0] || reply[1] != query[1] {
		return fmt.Errorf("dns over https: %s answered transaction %02x%02x, not the query's %02x%02x",
			endpoint, reply[0], reply[1], query[0], query[1])
	}
	if reply[2]&0x80 == 0 {
		return fmt.Errorf("dns over https: %s answered with a query, not a response", endpoint)
	}
	return nil
}

// exchange performs one DoH lookup. The wire format is RFC 8484's POST form: the
// query goes as the request body under a content type that says what it is, and
// the response body is the message with no envelope around it.
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
	// in DNS. Bounding the read keeps a broken endpoint from filling memory.
	reply, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return nil, err
	}
	return reply, nil
}

const dohMediaType = "application/dns-message"

// dohResolver builds a resolver that sends every lookup to one DoH endpoint. One
// endpoint and no list: this is a setting the person typed, so falling over to a
// different name server behind their back would answer with a resolver they did
// not choose. A failure is reported, and the next setting is theirs to change.
func dohResolver(endpoint string, bootstrap *net.Resolver, proxy func(*http.Request) (*url.URL, error)) *net.Resolver {
	client := &http.Client{
		// The query is already inside TLS, and a redirect would move the
		// exchange to an endpoint whose certificate this setting never named.
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			// The same egress the rest of the kernel uses. A DoH query that
			// went direct from a machine whose only way out is a proxy would
			// fail for a reason nothing on screen names.
			Proxy:       proxy,
			DialContext: endpointDialer(endpoint, bootstrap),
			// A fresh connection per lookup would pay a TCP and TLS handshake
			// for every name resolved. Kept small: this is a phone.
			MaxIdleConns:        4,
			MaxIdleConnsPerHost: 2,
			IdleConnTimeout:     90 * time.Second,
		},
	}
	return &net.Resolver{
		// Without it `net.Resolver` looks for the cgo resolver this build does
		// not have.
		PreferGo: true,
		Dial: func(context.Context, string, string) (net.Conn, error) {
			// The address the resolver passes is a name server it read from the
			// environment; it is ignored because the point of this path is not
			// to ask what the machine was told to ask.
			return &dohConn{endpoint: endpoint, client: client}, nil
		},
	}
}

// endpointDialer connects to the endpoint's host without asking this resolver:
// once installed, the process default resolves that name by querying this very
// endpoint, so a hostname would recurse. The address is looked up once through
// the resolver this one replaced, then pinned; TLS still sees the hostname. Nil
// is right for an IP literal, and when there is no resolver to look one up with.
func endpointDialer(endpoint string, bootstrap *net.Resolver) func(context.Context, string, string) (net.Conn, error) {
	u, err := url.Parse(endpoint)
	if err != nil || bootstrap == nil {
		return nil
	}
	host := u.Hostname()
	if host == "" || net.ParseIP(host) != nil {
		return nil
	}
	port := u.Port()
	if port == "" {
		port = "443"
	}
	var (
		mu   sync.Mutex
		addr string
	)
	return func(ctx context.Context, network, _ string) (net.Conn, error) {
		mu.Lock()
		pinned := addr
		mu.Unlock()
		if pinned == "" {
			ips, err := bootstrap.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, fmt.Errorf("dns over https: cannot resolve the endpoint %s: %w", host, err)
			}
			if len(ips) == 0 {
				return nil, fmt.Errorf("dns over https: no address for the endpoint %s", host)
			}
			// IPv4 where there is one: the missing route on a phone that sends
			// someone looking for this setting is usually the v6 one.
			chosen := ips[0].IP
			for _, ip := range ips {
				if ip.IP.To4() != nil {
					chosen = ip.IP
					break
				}
			}
			pinned = net.JoinHostPort(chosen.String(), port)
			mu.Lock()
			addr = pinned
			mu.Unlock()
		}
		dialer := net.Dialer{Timeout: dialTimeout}
		return dialer.DialContext(ctx, network, pinned)
	}
}

// frame prefixes a DNS message with the two-byte length the stream framing
// expects. Getting this wrong makes a working endpoint look malformed, because
// the resolver reads the first two bytes as a size.
func frame(msg []byte) []byte {
	out := make([]byte, 2+len(msg))
	binary.BigEndian.PutUint16(out, uint16(len(msg)))
	copy(out[2:], msg)
	return out
}

// unframe is the other direction, and it tolerates a message that is not
// prefixed: refusing to answer a bare one would be a failure with no cause a
// reader could find.
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
