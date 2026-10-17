package netdns

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"sync"
	"testing"
	"time"
)

// The parse is the part that runs on a machine that is not a phone: everything
// beyond it is a `net.Resolver` the standard library already tests.
func TestParse(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"empty", "", nil},
		{"whitespace only", "  \t\n ", nil},
		{"one v4", "10.0.0.1", []string{"10.0.0.1:53"}},
		{"comma separated", "10.0.0.1,8.8.8.8", []string{"10.0.0.1:53", "8.8.8.8:53"}},
		{"spaces and semicolons", "10.0.0.1; 8.8.8.8", []string{"10.0.0.1:53", "8.8.8.8:53"}},
		{"explicit port", "10.0.0.1:5353", []string{"10.0.0.1:5353"}},
		{"one v6", "fd00::1", []string{"[fd00::1]:53"}},
		{"two v6", "fd00::1,fd00::2", []string{"[fd00::1]:53", "[fd00::2]:53"}},
		// A trailing zone is how Android reports a link-local server.
		{"v6 with zone", "fe80::1%wlan0", []string{"[fe80::1%wlan0]:53"}},
		{"mixed garbage", "10.0.0.1,,not-an-address,8.8.8.8", []string{"10.0.0.1:53", "8.8.8.8:53"}},
		{"newlines", "10.0.0.1\n8.8.8.8\n", []string{"10.0.0.1:53", "8.8.8.8:53"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Parse(tc.in)
			if len(got) == 0 && len(tc.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Parse(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// A malformed variable must not take resolution down with it.
func TestParseDropsGarbage(t *testing.T) {
	if servers := Parse("this is not an address"); len(servers) != 0 {
		t.Fatalf("Parse kept %v, want none", servers)
	}
}

// Install must be inert without servers, or a desktop build would replace a
// working system resolver with an empty one.
func TestInstallIsInertWithoutServers(t *testing.T) {
	before := net.DefaultResolver
	Install(nil)
	if net.DefaultResolver != before {
		t.Fatal("Install replaced the resolver with no servers configured")
	}
}

// The command's init reads the environment and hands the servers in; this is the
// same chain, so a variable that names nothing must still be inert.
func TestInstallIsInertWithAnEmptyVariable(t *testing.T) {
	t.Setenv(EnvVar, "")
	before := net.DefaultResolver
	Install(Parse(os.Getenv(EnvVar)))
	if net.DefaultResolver != before {
		t.Fatal("Install replaced the resolver for an empty variable")
	}
}

// With servers named, Install takes over — a silent no-op here is the bug this
// package exists to fix.
func TestInstallTakesOverWhenServersAreNamed(t *testing.T) {
	t.Setenv(EnvVar, "10.0.0.1")
	before := net.DefaultResolver
	t.Cleanup(func() { net.DefaultResolver = before })
	Install(Parse(os.Getenv(EnvVar)))
	if net.DefaultResolver == before {
		t.Fatal("Install left the default resolver in place with servers configured")
	}
	if !net.DefaultResolver.PreferGo {
		t.Fatal("the installed resolver must prefer the Go path: this build has no cgo resolver")
	}
}

// The setting reads as encrypted DNS, so anything that would send a query in the
// clear is refused rather than used as written.
func TestValidateEndpoint(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    string
		refused bool
	}{
		{"unset", "", "", false},
		{"https", "https://1.1.1.1/dns-query", "https://1.1.1.1/dns-query", false},
		{"trimmed", "  https://1.1.1.1/dns-query  ", "https://1.1.1.1/dns-query", false},
		{"hostname", "https://dns.example/dns-query", "https://dns.example/dns-query", false},
		{"cleartext", "http://1.1.1.1/dns-query", "", true},
		{"no scheme", "1.1.1.1/dns-query", "", true},
		{"no host", "https:///dns-query", "", true},
		{"credentials", "https://user:pw@dns.example/q", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ValidateEndpoint(tc.in)
			if tc.refused {
				if err == nil {
					t.Fatalf("ValidateEndpoint(%q) = %q, want a refusal", tc.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ValidateEndpoint(%q): %v", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("ValidateEndpoint(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// dialDoh opens the connection a DoH resolver would use, so these cases exercise
// the client it builds rather than one the test assembled.
func dialDoh(t *testing.T, endpoint string) net.Conn {
	t.Helper()
	conn, err := dohResolver(endpoint, nil, nil).Dial(context.Background(), "udp", "")
	if err != nil {
		t.Fatalf("dial %s: %v", endpoint, err)
	}
	return conn
}

// A query goes out as RFC 8484's POST form and the answer comes back whole.
func TestDohConnExchangesAQuery(t *testing.T) {
	seen := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", dohMediaType)
		_, _ = w.Write(answerQuestion(body))
	}))
	defer srv.Close()

	conn := dialDoh(t, srv.URL+"/dns-query")
	query, id := queryPacket()
	if _, err := conn.Write(frame(query)); err != nil {
		t.Fatalf("write: %v", err)
	}
	if ct := <-seen; ct != dohMediaType {
		t.Fatalf("content type = %q, want %q", ct, dohMediaType)
	}
	got := make([]byte, 1024)
	n, err := conn.Read(got)
	if err != nil && !errors.Is(err, io.EOF) {
		t.Fatalf("read: %v", err)
	}
	// The resolver reads the first two bytes as a size, so the framing matters.
	if n < 2 || int(binary.BigEndian.Uint16(got[:2])) != n-2 {
		t.Fatalf("reply is not length-prefixed: %d bytes", n)
	}
	if got[2] != id[0] || got[3] != id[1] {
		t.Fatalf("reply id %02x%02x, query id %02x%02x", got[2], got[3], id[0], id[1])
	}
}

func TestDohConnRefusesANonOKAnswer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not yours", http.StatusForbidden)
	}))
	defer srv.Close()

	conn := dialDoh(t, srv.URL+"/dns-query")
	query, _ := queryPacket()
	if _, err := conn.Write(frame(query)); err == nil {
		t.Fatal("a 403 was accepted as an answer")
	}
}

// A huge body must be bounded: a hostile or broken endpoint cannot be allowed to
// decide how much memory a phone spends on one lookup.
func TestDohConnBoundsAHugeAnswer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_, _ = w.Write(append(answerQuestion(body), make([]byte, 200<<10)...))
	}))
	defer srv.Close()

	conn := dialDoh(t, srv.URL+"/dns-query")
	query, _ := queryPacket()
	if _, err := conn.Write(frame(query)); err != nil {
		t.Fatalf("write: %v", err)
	}
	total := 0
	buf := make([]byte, 4096)
	for {
		n, err := conn.Read(buf)
		total += n
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("read: %v", err)
		}
	}
	if total > 64<<10+2 {
		t.Fatalf("read %d bytes from a 200 KiB answer, want the 64 KiB cap", total)
	}
}

// A redirect would move the exchange to an endpoint whose certificate this
// setting never named.
func TestDohConnRefusesARedirect(t *testing.T) {
	var reached bool
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		reached = true
	}))
	defer target.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer srv.Close()

	conn := dialDoh(t, srv.URL+"/dns-query")
	query, _ := queryPacket()
	if _, err := conn.Write(frame(query)); err == nil {
		t.Fatal("a redirect was followed")
	}
	if reached {
		t.Fatal("the redirect target was reached")
	}
}

// One connection carries one query, so a reply whose transaction id differs is
// another lookup's answer and must not be handed back as this one's.
func TestDohConnRejectsAnAnswerToAnotherQuery(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		reply := answerQuestion(body)
		reply[0] ^= 0xFF
		_, _ = w.Write(reply)
	}))
	defer srv.Close()

	conn := dialDoh(t, srv.URL+"/dns-query")
	query, _ := queryPacket()
	if _, err := conn.Write(frame(query)); err == nil {
		t.Fatal("an answer to another query was accepted")
	}
}

// A hostname endpoint must not be resolved by the resolver it is replacing:
// after install that resolver answers by querying this endpoint, so the lookup
// would need its own answer first. The address is pinned through the resolver
// that was in place before, which is what this dialer is handed.
func TestEndpointDialerPinsAHostname(t *testing.T) {
	bootstrap := resolverAt(localDNS(t))

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	_, port, _ := net.SplitHostPort(ln.Addr().String())

	dial := endpointDialer("https://endpoint.example:"+port+"/dns-query", bootstrap)
	if dial == nil {
		t.Fatal("a hostname endpoint got no dialer, so it would resolve itself")
	}
	go func() {
		conn, err := ln.Accept()
		if err == nil {
			_ = conn.Close()
		}
	}()
	conn, err := dial(context.Background(), "tcp", "endpoint.example:"+port)
	if err != nil {
		t.Fatalf("dial through the pinned address: %v", err)
	}
	_ = conn.Close()
}

// An address needs no lookup, and with no resolver to look one up there is
// nothing to pin: the default dialer is right in both cases.
func TestEndpointDialerLeavesAnAddressAlone(t *testing.T) {
	if dial := endpointDialer("https://1.1.1.1/dns-query", resolverAt("127.0.0.1:53")); dial != nil {
		t.Fatal("an IP-literal endpoint was given a dialer it does not need")
	}
	if dial := endpointDialer("https://dns.example/dns-query", nil); dial != nil {
		t.Fatal("a hostname endpoint was given a dialer with no resolver to pin it through")
	}
}

// localDNS answers every A query with 127.0.0.1 and returns its own address.
func localDNS(t *testing.T) string {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("local dns: %v", err)
	}
	t.Cleanup(func() { _ = pc.Close() })
	go func() {
		buf := make([]byte, 512)
		for {
			n, from, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			if _, err := pc.WriteTo(answerQuestion(buf[:n]), from); err != nil {
				return
			}
		}
	}()
	return pc.LocalAddr().String()
}

// resolverAt is a resolver that sends every query to one server.
func resolverAt(addr string) *net.Resolver {
	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "udp", addr)
		},
	}
}

// answerQuestion is a reply to the query's own question: the loopback address.
// It parses the question section rather than echoing the rest of the packet,
// because Go's resolver puts an EDNS OPT record in the additional section and a
// response that echoes it while claiming no additional records does not parse.
func answerQuestion(query []byte) []byte {
	if len(query) < headerLen {
		return nil
	}
	i := headerLen
	for i < len(query) {
		l := int(query[i])
		if l == 0 {
			i++
			break
		}
		if l&0xC0 == 0xC0 {
			i += 2
			break
		}
		i += 1 + l
	}
	i += 4 // qtype + qclass
	if i > len(query) {
		return nil
	}
	out := []byte{
		query[0], query[1], // the query's own id
		0x81, 0x80, // response, recursion desired and available
		0x00, 0x01, // one question
		0x00, 0x01, // one answer
		0x00, 0x00, 0x00, 0x00, // no authority, no additional
	}
	out = append(out, query[headerLen:i]...) // the question, as asked
	out = append(out,
		0xC0, 0x0C, // the name, at the question
		0x00, 0x01, 0x00, 0x01, // type A, class IN
		0x00, 0x00, 0x00, 0x3C, // ttl 60
		0x00, 0x04, // rdlength
		127, 0, 0, 1, // rdata
	)
	return out
}

// resetInstall gives one test the process's first install, and puts the resolver
// back afterwards, so tests cannot stand in for each other.
func resetInstall(t *testing.T) {
	t.Helper()
	beforeResolver, beforeOnce := net.DefaultResolver, installOnce
	installOnce = sync.Once{}
	t.Cleanup(func() {
		net.DefaultResolver, installOnce = beforeResolver, beforeOnce
	})
}

// A second build with the same setting must not replace the resolver: other
// panes are resolving through it, and the write is the race.
func TestInstallKeepsTheResolverForAnUnchangedSetting(t *testing.T) {
	resetInstall(t)
	InstallConfigured([]string{"10.0.0.1"}, "", nil)
	first := net.DefaultResolver
	InstallConfigured([]string{"10.0.0.1"}, "", nil)
	if net.DefaultResolver != first {
		t.Fatal("the same servers replaced the resolver a second time")
	}
}

// With DoH the same mistake is worse than a racy write: each build would wrap
// the previous resolver as its bootstrap, one layer per pane.
func TestInstallDoesNotWrapTheSameEndpointTwice(t *testing.T) {
	resetInstall(t)
	InstallConfigured(nil, "https://dns.example/dns-query", nil)
	first := net.DefaultResolver
	InstallConfigured(nil, "https://dns.example/dns-query", nil)
	if net.DefaultResolver != first {
		t.Fatal("the same endpoint wrapped the previous resolver")
	}
}

// Lookups run while another pane builds with the same setting. The second build
// must not write: the resolver the first one installed is in use, and the
// standard library reads net.DefaultResolver without synchronisation, so a write
// here is a race no mutex on this side can close. Run under -race.
func TestRepeatedInstallDoesNotWriteWhileLookupsRun(t *testing.T) {
	resetInstall(t)
	server := localDNS(t)
	InstallConfigured([]string{server}, "", nil)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 20; i++ {
			InstallConfigured([]string{server}, "", nil)
		}
	}()
	for i := 0; i < 20; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_, _ = net.DefaultResolver.LookupHost(ctx, "example.test")
		cancel()
	}
	<-done
}
