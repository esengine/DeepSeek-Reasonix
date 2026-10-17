// Package netdns points name resolution at servers the caller names.
//
// It exists because of Android: Go's own source prefers a cgo resolver there
// ("DNS requests don't work on Android", issue #10714), and a CGO_ENABLED=0
// build has none, so it reads `/etc/resolv.conf` — a file Android does not have
// — and falls back to `127.0.0.1:53`, where nothing listens. The servers are
// handed in instead, in `[network]` or from `REASONIX_DNS`. Nothing here runs
// when neither names one.
package netdns

import (
	"context"
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"time"
)

// EnvVar is where the caller puts the servers, separated by commas, spaces,
// semicolons or newlines.
const EnvVar = "REASONIX_DNS"

const (
	// defaultPort is the port a bare name server address takes.
	defaultPort = 53
	// dialTimeout bounds opening the connection a lookup will be sent on.
	dialTimeout = 5 * time.Second
	// reachTimeout bounds one server's reachability check: a local resolver
	// answers in single-digit milliseconds and a public one in tens.
	reachTimeout = time.Second
	// aliveTTL is how long a reachability result is reused — long enough that
	// the check is not paid on every lookup, short enough to notice a change.
	aliveTTL = 30 * time.Second
)

// Install points the default resolver at the servers named, and does nothing
// when none are. The caller reads the environment and parses it, because a
// build bound to a home other than the process's must be told what to ask
// rather than infer it.
func Install(servers []string) {
	if len(servers) == 0 {
		return
	}
	net.DefaultResolver = resolver(servers)
}

// InstallConfigured points name resolution at what the person asked for, which
// is what makes the `[network]` setting mean anything. DoH wins when both are
// set: a bare server is faster but still fails where the path is tampered with.
// An empty call changes nothing, which keeps every other install on the
// system's own resolver.
func InstallConfigured(servers []string, doh string, proxy func(*http.Request) (*url.URL, error)) {
	if endpoint := strings.TrimSpace(doh); endpoint != "" {
		// The resolver about to be replaced is the one that can still resolve
		// the endpoint's own hostname: after the swap, anything asking the
		// process default would be asking this resolver to resolve itself.
		net.DefaultResolver = dohResolver(endpoint, net.DefaultResolver, proxy)
		return
	}
	if all := Parse(strings.Join(servers, ",")); len(all) > 0 {
		net.DefaultResolver = resolver(all)
	}
}

// ValidateEndpoint checks a DoH setting and returns it trimmed. Encrypted
// resolution is the point of the setting, so anything that would send a query
// in the clear is refused rather than used: a scheme that is not `https`, a URL
// with no host, userinfo that does not belong in a resolver. An empty setting
// is not an error — it means the option is unset.
func ValidateEndpoint(raw string) (string, error) {
	endpoint := strings.TrimSpace(raw)
	if endpoint == "" {
		return "", nil
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("doh: %w", err)
	}
	switch {
	case u.Scheme != "https":
		return "", fmt.Errorf("doh: scheme %q is not https; a cleartext endpoint would defeat the setting", u.Scheme)
	case u.Host == "":
		return "", fmt.Errorf("doh: %q names no host", endpoint)
	case u.User != nil:
		return "", fmt.Errorf("doh: %q carries credentials; the endpoint must not", endpoint)
	}
	return endpoint, nil
}

// resolver builds the resolver for a set of `host:port` servers. PreferGo is
// required: without it `net.Resolver` looks for the cgo resolver this build
// does not have and reports "cannot use cgo" for every lookup.
func resolver(servers []string) *net.Resolver {
	alive := &aliveCache{}
	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			// The address being looked up rides on the connection, not in this
			// call, which is why one Dial serves every lookup.
			netw := dialNetwork(network)
			server := alive.pick(ctx, netw, servers)
			dialer := net.Dialer{Timeout: dialTimeout}
			return dialer.DialContext(ctx, netw, server)
		},
	}
}

// aliveCache remembers which server last answered. Nothing cheaper than a real
// query tells a server that is down from one that is up, and guessing wrong
// costs the whole lookup deadline.
type aliveCache struct {
	mu     sync.Mutex
	server string
	at     time.Time
}

// pick returns the server to send a lookup to.
func (c *aliveCache) pick(ctx context.Context, network string, servers []string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if time.Since(c.at) < aliveTTL {
		// A positive answer is reused, and so is a negative one — a network
		// where nothing answers must not pay the timeout on every lookup.
		if c.server != "" {
			return c.server
		}
		return servers[0]
	}
	for _, server := range servers {
		if answers(ctx, network, server) {
			c.server, c.at = server, time.Now()
			return server
		}
	}
	// Nothing answered. Hand back the first and let the lookup produce its own
	// error, which names the address; one invented here would not.
	c.server, c.at = "", time.Now()
	return servers[0]
}

// answers reports whether a server replies at all. The query is for a name
// under `.invalid`, which RFC 6761 reserves never to exist, so any live server
// answers an NXDOMAIN as promptly as a record.
func answers(ctx context.Context, network, server string) bool {
	dialer := net.Dialer{Timeout: reachTimeout}
	conn, err := dialer.DialContext(ctx, network, server)
	if err != nil {
		return false
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(reachTimeout)); err != nil {
		return false
	}
	query, id := queryPacket()
	// DNS over TCP frames every message with a two-byte length; over UDP the
	// message stands alone. Getting this wrong makes a working server look dead.
	if strings.HasPrefix(network, "tcp") {
		framed := make([]byte, 2+len(query))
		framed[0], framed[1] = byte(len(query)>>8), byte(len(query))
		copy(framed[2:], query)
		query = framed
	}
	if _, err := conn.Write(query); err != nil {
		return false
	}
	reply := make([]byte, 512)
	n, err := conn.Read(reply)
	if err != nil || n < headerLen {
		return false
	}
	// The transaction id, so a late answer to an earlier query is not taken for
	// this one's.
	return reply[0] == id[0] && reply[1] == id[1]
}

// headerLen is a DNS message's fixed header; a reply shorter than this is not
// one.
const headerLen = 12

// queryPacket builds an A query for a name that cannot exist, and returns the id
// it used so the caller can match the reply.
func queryPacket() ([]byte, [2]byte) {
	var id [2]byte
	value := uint16(rand.Intn(1 << 16))
	id[0], id[1] = byte(value>>8), byte(value)
	// `.invalid` is reserved by RFC 6761: no server may answer it with a
	// record, and none may refuse to answer it on that ground either.
	const name = "reachability.probe.invalid"
	b := make([]byte, 0, 16+len(name)+1)
	b = append(b, id[0], id[1])
	b = append(b, 0x01, 0x00) // standard query, recursion desired
	b = append(b, 0x00, 0x01) // one question
	b = append(b, 0x00, 0x00) // no answers
	b = append(b, 0x00, 0x00) // no authority records
	b = append(b, 0x00, 0x00) // no additional records
	for label := range strings.SplitSeq(name, ".") {
		b = append(b, byte(len(label)))
		b = append(b, label...)
	}
	b = append(b, 0x00)       // end of the name
	b = append(b, 0x00, 0x01) // type A
	b = append(b, 0x00, 0x01) // class IN
	return b, id
}

// dialNetwork narrows the network to what a resolver connection can be. Android
// answers ordinary lookups on UDP only, and a phone's firewall drops a stray TCP
// attempt rather than refusing it, so a TCP try costs a full timeout.
func dialNetwork(network string) string {
	if strings.HasPrefix(network, "tcp") {
		return "tcp"
	}
	return "udp"
}

// Parse turns a caller's answer into `host:port` servers, dropping anything that
// is not an address. Parsing runs at boot, before the resolver this setting
// installs exists, so an entry has to be an address: a name would have to be
// resolved by the resolver being configured. Dropping what is malformed keeps
// an error from describing the resolver instead of the setting.
func Parse(raw string) []string {
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ' ' || r == ';' || r == '\n' || r == '\t' || r == '|'
	})
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		host := strings.TrimSpace(field)
		if host == "" {
			continue
		}
		// A bare address takes the default port; `netip` reads the two forms
		// without a lookup, and brackets a v6 address itself, with its zone.
		if addr, err := netip.ParseAddr(host); err == nil {
			out = append(out, netip.AddrPortFrom(addr, defaultPort).String())
			continue
		}
		if addrPort, err := netip.ParseAddrPort(host); err == nil {
			out = append(out, addrPort.String())
		}
	}
	return out
}
