// Package netdns points name resolution at servers given in the environment.
//
// It exists because of Android, though it is compiled everywhere so that its
// parsing can be tested on a normal machine.
//
// Go's own source says why, in `net/conf.go`:
//
//	// DNS requests don't work on Android, so prefer the cgo resolver.
//	// Issue #10714.
//	case "android":
//	    return true
//
// The cgo resolver is how a normal Android binary resolves names: it calls
// `getaddrinfo`, which talks to the `netd` daemon and gets the servers of the
// network that is actually up. This kernel is built with CGO_ENABLED=0 — that is
// what makes it one static file with no runtime on the device — so there is no
// cgo resolver to prefer, and Go falls back to reading `/etc/resolv.conf`.
//
// Android has no `/etc/resolv.conf`. Its DNS servers live in `netd`, keyed by
// network, and are not written to a file a process can read. With the file
// missing, Go's `dnsReadConfig` takes its `err != nil` branch and falls back to
// `defaultNS`, which is `127.0.0.1:53` and `[::1]:53` — where nothing listens on
// a phone. Every lookup fails, so every HTTPS request fails, and the first
// screen of a new install reports `probe: unreachable` for every provider,
// including the official ones.
//
// So the servers are handed in. The Android shell reads them from
// `ConnectivityManager` — the same source the system uses — and passes them in
// `REASONIX_DNS`. Nothing here runs when that variable is absent, so a desktop
// build, a server install and the test suite are all unaffected.
package netdns

import (
	"context"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// EnvVar is where the caller puts the servers, separated by commas, spaces,
// semicolons or newlines.
const EnvVar = "REASONIX_DNS"

const (
	// dialTimeout bounds opening the connection a lookup will be sent on.
	dialTimeout = 5 * time.Second
	// reachTimeout bounds one server's reachability check. A local resolver
	// answers in single-digit milliseconds and a public one in tens; a whole
	// second is already generous, and a server that has not answered by then is
	// not the one to spend a lookup's deadline on.
	reachTimeout = time.Second
	// aliveTTL is how long a reachability result is reused. Long enough that
	// the check is not paid on every lookup, short enough that moving between
	// Wi-Fi and mobile data is noticed.
	aliveTTL = 30 * time.Second
)

// Install points the default resolver at the servers in EnvVar, and is a no-op
// when none are named.
//
// Called from `init` in the command's package rather than by a caller: the
// resolver has to be in place before anything opens a socket, and the order of
// an import graph is not something a caller can promise — a provider probe can
// start before `main` has finished its setup.
//
// Deliberately not guarded by `sync.Once`. It reads the environment on every
// call, so a caller that changes the servers and installs again gets the new
// ones; a once would make the second call a silent no-op and leave a kernel
// resolving through servers that are gone.
func Install() {
	servers := Parse(os.Getenv(EnvVar))
	if len(servers) == 0 {
		return
	}
	net.DefaultResolver = resolver(servers)
}

// InstallConfigured points name resolution at what the person asked for, and
// is what makes the setting in `[network]` mean anything.
//
// DoH wins when both are set, and that is a decision rather than an accident:
// the two are not equal answers to the same question. A bare `dns` server is
// faster and still fails wherever the path itself is tampered with — encrypted
// resolution is the only setting that works there — so a person who has filled
// in both has said what they want the stronger one to be.
//
// `proxy` is the same one the rest of the kernel's traffic uses. A DoH query is
// an HTTPS request like any other, and a machine whose only egress is a proxy
// would otherwise resolve by a route it has no access to: the setting would look
// broken for a reason nothing on screen names. Nil means no proxy, which is what
// a host with none configured wants.
//
// An empty call changes nothing, which is what keeps a desktop install, a
// server install and the test suite on the system's own resolver.
func InstallConfigured(servers []string, doh string, proxy func(*http.Request) (*url.URL, error)) {
	if endpoint := strings.TrimSpace(doh); endpoint != "" {
		net.DefaultResolver = dohResolver(endpoint, proxy)
		return
	}
	if all := Parse(strings.Join(servers, ",")); len(all) > 0 {
		net.DefaultResolver = resolver(all)
	}
}

// resolver builds the resolver for a set of `host:port` servers.
//
// PreferGo is required: `net.Resolver` would otherwise try the cgo path this
// build does not have, and answer every lookup with a "cannot use cgo" error.
//
// Which server a lookup goes to is decided by `aliveCache`, not by dialling.
// Dialling cannot decide it: a UDP `Dial` only creates a socket and sets its
// default destination, so it succeeds against a server that will never answer —
// and a lookup sent down that connection then waits out the resolver's whole
// deadline before failing, with the next server never tried.
//
// Measured, with a black-hole server listed first and a working one second —
// `REASONIX_DNS=10.255.255.1,223.5.5.5` — a probe took 20.0s and answered
// `provider.probe.timeout`. Reversing the two made the same probe finish in
// 2.1s. The address list is the shell's to order, but the kernel cannot assume
// the first entry is the one that works, because a phone reports its servers in
// the order the system happens to hold them.
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

// aliveCache remembers which server last answered.
//
// The check is a real query — see `answers` — because nothing cheaper tells a
// server that is down from one that is up, and the cost of guessing wrong is
// the whole lookup deadline.
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

// answers reports whether a server replies at all.
//
// The query is for a name under `.invalid`, which RFC 6761 reserves never to
// exist, so the reply is an NXDOMAIN rather than a record. That is deliberate:
// what is being asked is "is anything there", not "does this resolve" — and a
// server that is up answers an NXDOMAIN just as promptly as an address.
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
	// The transaction id, so a late answer to an earlier query is not taken
	// for this one's.
	return reply[0] == id[0] && reply[1] == id[1]
}

// headerLen is a DNS message's fixed header; a reply shorter than this is not
// one.
const headerLen = 12

// queryPacket builds an A query for a name that cannot exist, and returns the
// id it used so the caller can match the reply.
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
	for _, label := range strings.Split(name, ".") {
		b = append(b, byte(len(label)))
		b = append(b, label...)
	}
	b = append(b, 0x00)       // end of the name
	b = append(b, 0x00, 0x01) // type A
	b = append(b, 0x00, 0x01) // class IN
	return b, id
}

// dialNetwork narrows the network to what a resolver connection can be.
//
// The caller passes "udp", "udp4", "tcp" and so on. Android's resolver answers
// ordinary lookups on UDP only, and a phone's firewall drops a stray TCP attempt
// rather than refusing it, so a TCP try would cost a full timeout before the
// next server was reached.
func dialNetwork(network string) string {
	if strings.HasPrefix(network, "tcp") {
		return "tcp"
	}
	return "udp"
}

// Parse turns a caller's answer into `host:port` servers, dropping anything
// that is not an address: a malformed variable must not make every lookup fail
// with an error that describes the resolver instead of the setting.
//
// Exported because the shape is worth testing on a machine that is not a phone.
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
		// `net.JoinHostPort` brackets an IPv6 address itself, but only when the
		// address has no port yet; a bare "fd00::1" would otherwise be read as
		// host "fd00" and port ":1".
		if _, _, err := net.SplitHostPort(host); err != nil {
			host = net.JoinHostPort(strings.Trim(host, "[]"), "53")
		}
		if _, err := net.ResolveUDPAddr("udp", host); err != nil {
			continue
		}
		out = append(out, host)
	}
	return out
}
