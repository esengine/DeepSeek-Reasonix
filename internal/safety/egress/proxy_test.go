package egress

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func TestPolicyDecide(t *testing.T) {
	p := Policy{Allow: []string{"github.com", "*.npmjs.org", "PyPI.org.", "10.0.0.5"}, Deny: []string{"gist.github.com", "*.evil.npmjs.org"}}
	cases := map[string]Reason{
		"github.com":            "",
		"GitHub.com.":           "",
		"api.github.com":        ReasonNotAllowed,
		"gist.github.com":       ReasonDenied,
		"registry.npmjs.org":    "",
		"npmjs.org":             ReasonNotAllowed,
		"a.evil.npmjs.org":      ReasonDenied,
		"pypi.org":              "",
		"example.com":           ReasonNotAllowed,
		"":                      ReasonNotAllowed,
		"10.0.0.5":              "",
		"[10.0.0.5]":            "",
		"notgithub.com":         ReasonNotAllowed,
		"github.com.attack.net": ReasonNotAllowed,
	}
	for host, want := range cases {
		if got := p.Decide(host); got != want {
			t.Errorf("Decide(%q) = %q, want %q", host, got, want)
		}
	}
	if got := (Policy{}).Decide("github.com"); got != ReasonNotAllowed {
		t.Fatalf("an empty allow list admitted a host: %q", got)
	}
}

func TestForbiddenAddress(t *testing.T) {
	own := net.ParseIP("192.168.7.20")
	local := func() []net.IP { return []net.IP{own} }
	cases := map[string]bool{
		"127.0.0.1":          true,
		"::1":                true,
		"0.0.0.0":            true,
		"169.254.169.254":    true,
		"100.100.100.200":    true,
		"168.63.129.16":      true,
		"fd00:ec2::254":      true,
		"::ffff:127.0.0.1":   true,
		"64:ff9b::7f00:1":    true,
		"2002:7f00:1::":      true,
		"224.0.0.1":          true,
		"192.168.7.20":       true,
		"10.0.0.1":           false,
		"192.168.7.21":       false,
		"93.184.216.34":      false,
		"2606:4700::6810:84": false,
	}
	for s, want := range cases {
		if got := forbiddenAddress(net.ParseIP(s), local); got != want {
			t.Errorf("forbiddenAddress(%s) = %v, want %v", s, got, want)
		}
	}
}

// harness runs a proxy whose resolver answers from names and whose dialer
// lands every public address on backend, recording what it was asked for.
type harness struct {
	proxy *Proxy
	mu    sync.Mutex
	dials []string
}

func newHarness(t *testing.T, policy Policy, opts Options, names map[string]string, backend string, passthrough ...string) *harness {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	h := &harness{}
	p := newProxy(policy, opts, ln)
	p.local = func() []net.IP { return nil }
	p.lookup = func(_ context.Context, host string) ([]net.IP, error) {
		ip, ok := names[host]
		if !ok {
			return nil, errors.New("no such host")
		}
		return []net.IP{net.ParseIP(ip)}, nil
	}
	p.dial = func(ctx context.Context, network, addr string) (net.Conn, error) {
		h.mu.Lock()
		h.dials = append(h.dials, addr)
		h.mu.Unlock()
		target := backend
		if slices.Contains(passthrough, addr) {
			target = addr
		}
		return (&net.Dialer{}).DialContext(ctx, network, target)
	}
	go func() { _ = p.srv.Serve(ln) }()
	t.Cleanup(func() { _ = p.Close() })
	h.proxy = p
	return h
}

func (h *harness) client(token string) *http.Client {
	u, _ := url.Parse("http://" + token + "@" + h.proxy.Addr())
	return &http.Client{Transport: &http.Transport{
		Proxy:           http.ProxyURL(u),
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // test backend
	}}
}

func (h *harness) dialed() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.dials)
}

func get(t *testing.T, c *http.Client, target string) (*http.Response, string) {
	t.Helper()
	resp, err := c.Get(target)
	if err != nil {
		t.Fatalf("GET %s: %v", target, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp, string(body)
}

var names = map[string]string{"allowed.test": "93.184.216.34", "other.test": "93.184.216.35", "sneaky.test": "127.0.0.1"}

func TestForwardAdmitsAllowedAndRecordsRefusals(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Proxy-Authorization") != "" {
			t.Error("the proxy forwarded its own credentials upstream")
		}
		_, _ = io.WriteString(w, "hello "+r.Host+r.URL.Path)
	}))
	defer backend.Close()
	h := newHarness(t, Policy{Allow: []string{"allowed.test", "sneaky.test"}}, Options{}, names, backend.Listener.Addr().String())
	c := h.client("cmd1")

	resp, body := get(t, c, "http://allowed.test/x")
	if resp.StatusCode != http.StatusOK || body != "hello allowed.test/x" {
		t.Fatalf("allowed = %d %q", resp.StatusCode, body)
	}
	if got := h.dialed(); !slices.Equal(got, []string{"93.184.216.34:80"}) {
		t.Fatalf("dialed %v, want the checked address", got)
	}
	resp, _ = get(t, c, "http://other.test/")
	if resp.StatusCode != http.StatusForbidden || resp.Header.Get("X-Proxy-Error") != string(ReasonNotAllowed) {
		t.Fatalf("other = %d %q", resp.StatusCode, resp.Header.Get("X-Proxy-Error"))
	}
	resp, _ = get(t, c, "http://sneaky.test/")
	if resp.StatusCode != http.StatusForbidden || resp.Header.Get("X-Proxy-Error") != string(ReasonAddress) {
		t.Fatalf("sneaky = %d %q", resp.StatusCode, resp.Header.Get("X-Proxy-Error"))
	}
	get(t, c, "http://other.test/again")
	want := []Denial{{"other.test", ReasonNotAllowed}, {"sneaky.test", ReasonAddress}}
	if got := h.proxy.TakeDenials("cmd1"); !slices.Equal(got, want) {
		t.Fatalf("denials = %v, want %v", got, want)
	}
	if got := h.proxy.TakeDenials("cmd1"); len(got) != 0 {
		t.Fatalf("denials were not taken: %v", got)
	}
	if got := h.proxy.TakeDenials("cmd2"); len(got) != 0 {
		t.Fatalf("another command was charged: %v", got)
	}
}

func TestConnectTunnelsOnlyAllowedHosts(t *testing.T) {
	backend := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "secure")
	}))
	defer backend.Close()
	h := newHarness(t, Policy{Allow: []string{"allowed.test"}}, Options{}, names, backend.Listener.Addr().String())
	c := h.client("cmd1")

	resp, body := get(t, c, "https://allowed.test/")
	if resp.StatusCode != http.StatusOK || body != "secure" {
		t.Fatalf("tunnel = %d %q", resp.StatusCode, body)
	}
	if got := h.dialed(); !slices.Equal(got, []string{"93.184.216.34:443"}) {
		t.Fatalf("dialed %v", got)
	}
	if _, err := c.Get("https://other.test/"); err == nil || !strings.Contains(err.Error(), "Forbidden") {
		t.Fatalf("CONNECT to a disallowed host: %v", err)
	}
	if got := h.proxy.TakeDenials("cmd1"); !slices.Equal(got, []Denial{{"other.test", ReasonNotAllowed}}) {
		t.Fatalf("denials = %v", got)
	}
}

// Through an upstream the name is handed on even when this host cannot
// resolve it, and the upstream's credentials go to the upstream alone.
func TestConnectChainsThroughUpstream(t *testing.T) {
	backend := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "via upstream")
	}))
	defer backend.Close()
	var seen []string
	var mu sync.Mutex
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Method+" "+r.Host+" "+r.Header.Get("Proxy-Authorization"))
		mu.Unlock()
		remote, err := net.Dial("tcp", backend.Listener.Addr().String())
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		client, rw, _ := http.NewResponseController(w).Hijack()
		_, _ = rw.WriteString("HTTP/1.1 200 OK\r\n\r\n")
		_ = rw.Flush()
		go func() { _, _ = io.Copy(remote, rw) }()
		_, _ = io.Copy(client, bufio.NewReader(remote))
		_ = client.Close()
	}))
	defer upstream.Close()
	up, _ := url.Parse("http://u:p@" + upstream.Listener.Addr().String())
	h := newHarness(t, Policy{Allow: []string{"unresolvable.test"}}, Options{Upstream: http.ProxyURL(up)}, names, "", up.Host)

	resp, body := get(t, h.client("cmd1"), "https://unresolvable.test/")
	if resp.StatusCode != http.StatusOK || body != "via upstream" {
		t.Fatalf("chained = %d %q", resp.StatusCode, body)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 1 || seen[0] != "CONNECT unresolvable.test:443 Basic dTpw" {
		t.Fatalf("upstream saw %q", seen)
	}
}

func TestUnsupportedUpstreamIsReported(t *testing.T) {
	up, _ := url.Parse("https://127.0.0.1:1080")
	h := newHarness(t, Policy{Allow: []string{"allowed.test"}}, Options{Upstream: http.ProxyURL(up)}, names, "")
	resp, body := get(t, h.client("cmd1"), "http://allowed.test/")
	if resp.StatusCode != http.StatusBadGateway || !strings.Contains(body, "https") {
		t.Fatalf("https upstream = %d %q", resp.StatusCode, body)
	}
}

// A SOCKS5 upstream is handed the name, and the policy still applies first.
func TestSOCKSUpstreamCarriesAllowedTraffic(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "socks "+r.Host)
	}))
	defer backend.Close()
	socks, requested := fakeSOCKS5(t, backend.Listener.Addr().String())
	up, _ := url.Parse("socks5h://" + socks)
	h := newHarness(t, Policy{Allow: []string{"unresolvable.test"}}, Options{Upstream: http.ProxyURL(up)}, names, "", socks)
	c := h.client("cmd1")

	resp, body := get(t, c, "http://unresolvable.test/")
	if resp.StatusCode != http.StatusOK || body != "socks unresolvable.test" {
		t.Fatalf("forward via socks = %d %q", resp.StatusCode, body)
	}
	if got := requested(); !slices.Equal(got, []string{"unresolvable.test:80"}) {
		t.Fatalf("socks upstream was asked for %v", got)
	}
	if resp, _ := get(t, c, "http://other.test/"); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("a disallowed host went through the socks upstream: %d", resp.StatusCode)
	}
}

// fakeSOCKS5 is a no-auth SOCKS5 server that connects every request to
// backend and records the name it was asked for.
func fakeSOCKS5(t *testing.T, backend string) (string, func() []string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	var mu sync.Mutex
	var seen []string
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 262)
				if _, err := io.ReadFull(c, buf[:2]); err != nil {
					return
				}
				if _, err := io.ReadFull(c, buf[:buf[1]]); err != nil {
					return
				}
				_, _ = c.Write([]byte{5, 0})
				if _, err := io.ReadFull(c, buf[:5]); err != nil || buf[3] != 3 {
					return
				}
				n := int(buf[4])
				if _, err := io.ReadFull(c, buf[:n+2]); err != nil {
					return
				}
				name := net.JoinHostPort(string(buf[:n]), strconv.Itoa(int(buf[n])<<8|int(buf[n+1])))
				mu.Lock()
				seen = append(seen, name)
				mu.Unlock()
				remote, err := net.Dial("tcp", backend)
				if err != nil {
					return
				}
				defer remote.Close()
				_, _ = c.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0})
				go func() { _, _ = io.Copy(remote, c) }()
				_, _ = io.Copy(c, remote)
			}(c)
		}
	}()
	return ln.Addr().String(), func() []string {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(seen)
	}
}

func TestEnvCarriesTheTokenAndBypassesOnlyLoopback(t *testing.T) {
	h := newHarness(t, Policy{}, Options{}, nil, "")
	tok := NewToken()
	if len(tok) != 24 || tok == NewToken() {
		t.Fatalf("token %q is not a fresh 12-byte hex name", tok)
	}
	env := h.proxy.Env(tok)
	want := "HTTPS_PROXY=http://" + tok + "@" + h.proxy.Addr()
	if !slices.Contains(env, want) || !slices.Contains(env, "NO_PROXY=localhost,127.0.0.1,::1") || !slices.Contains(env, "no_proxy=localhost,127.0.0.1,::1") {
		t.Fatalf("env = %q", env)
	}
	if len(env) != len(EnvKeys) {
		t.Fatalf("env has %d entries, EnvKeys %d", len(env), len(EnvKeys))
	}
}

func TestLoopbackProxyPorts(t *testing.T) {
	env := map[string]string{
		"HTTPS_PROXY": "http://127.0.0.1:7890",
		"http_proxy":  "localhost:8118",
		"ALL_PROXY":   "socks5://[::1]",
		"HTTP_PROXY":  "http://corp-proxy.example:3128",
	}
	got := LoopbackProxyPorts(func(k string) string { return env[k] })
	for _, want := range []int{1080, 7890, 8118} {
		if !slices.Contains(got, want) {
			t.Fatalf("ports = %v, missing %d", got, want)
		}
	}
	if slices.Contains(got, 3128) {
		t.Fatalf("a remote proxy's port was closed: %v", got)
	}
}

func TestParseScutilProxies(t *testing.T) {
	out := `<dictionary> {
  ExceptionsList : <array> {
    0 : 127.0.0.1
  }
  HTTPEnable : 1
  HTTPPort : 7897
  HTTPProxy : 127.0.0.1
  HTTPSEnable : 0
  HTTPSPort : 7898
  HTTPSProxy : 127.0.0.1
  SOCKSEnable : 1
  SOCKSPort : 7899
  SOCKSProxy : 127.0.0.1
}`
	if got := parseScutilProxies(out); !slices.Equal(got, []string{"127.0.0.1:7897", "127.0.0.1:7899"}) {
		t.Fatalf("scutil proxies = %v", got)
	}
}

// A host outside the list is put to the command's asker once, however many
// connections ask for it; denied hosts and other commands are never asked.
func TestAskPutsUnlistedHostsToAPerson(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	defer backend.Close()
	names := map[string]string{"asked.test": "93.184.216.34", "refused.test": "93.184.216.35", "broken.test": "93.184.216.36", "gist.test": "93.184.216.37"}
	h := newHarness(t, Policy{Allow: []string{"allowed.test"}, Deny: []string{"gist.test"}}, Options{}, names, backend.Listener.Addr().String())
	var mu sync.Mutex
	var asked []string
	h.proxy.Ask("cmd1", func(host string) (bool, error) {
		mu.Lock()
		asked = append(asked, host)
		mu.Unlock()
		switch host {
		case "asked.test":
			return true, nil
		case "broken.test":
			return false, errors.New("nobody is there")
		}
		return false, nil
	})
	c := h.client("cmd1")
	var wg sync.WaitGroup
	for range 3 {
		wg.Go(func() {
			if resp, _ := get(t, c, "http://asked.test/"); resp.StatusCode != http.StatusOK {
				t.Errorf("approved host = %d", resp.StatusCode)
			}
		})
	}
	wg.Wait()
	for host, want := range map[string]Reason{"refused.test": ReasonDeclined, "broken.test": ReasonNotAllowed, "gist.test": ReasonDenied} {
		if resp, _ := get(t, c, "http://"+host+"/"); resp.Header.Get("X-Proxy-Error") != string(want) {
			t.Errorf("%s = %q, want %q", host, resp.Header.Get("X-Proxy-Error"), want)
		}
	}
	if resp, _ := get(t, h.client("cmd2"), "http://asked.test/"); resp.Header.Get("X-Proxy-Error") != string(ReasonNotAllowed) {
		t.Errorf("another command rode cmd1's answer: %q", resp.Header.Get("X-Proxy-Error"))
	}
	mu.Lock()
	defer mu.Unlock()
	slices.Sort(asked)
	if !slices.Equal(asked, []string{"asked.test", "broken.test", "refused.test"}) {
		t.Fatalf("asked %v, want each unlisted host once and no denied host", asked)
	}
	h.proxy.TakeDenials("cmd1")
	if resp, _ := get(t, c, "http://asked.test/"); resp.Header.Get("X-Proxy-Error") != string(ReasonNotAllowed) {
		t.Fatalf("an ended token kept its asker: %q", resp.Header.Get("X-Proxy-Error"))
	}
}

// A sandbox in its own network namespace reaches the proxy on its socket, under
// the same policy and the same per-command accounting.
func TestListenUnixServesTheSamePolicy(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no sandbox reaches the proxy on a socket here")
	}
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "via socket")
	}))
	defer backend.Close()
	h := newHarness(t, Policy{Allow: []string{"allowed.test"}}, Options{}, names, backend.Listener.Addr().String())
	dir, err := os.MkdirTemp("/tmp", "egs")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sock := filepath.Join(dir, "sub", "p.sock")
	if err := h.proxy.ListenUnix(sock); err != nil {
		t.Fatal(err)
	}
	if h.proxy.SocketPath() != sock {
		t.Fatalf("SocketPath = %q", h.proxy.SocketPath())
	}
	if info, err := os.Stat(sock); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("socket mode = %v, %v", info, err)
	}
	u, _ := url.Parse("http://cmd9@proxy.invalid")
	c := &http.Client{Transport: &http.Transport{
		Proxy: http.ProxyURL(u),
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", sock)
		},
	}}
	if resp, body := get(t, c, "http://allowed.test/"); resp.StatusCode != http.StatusOK || body != "via socket" {
		t.Fatalf("allowed via socket = %d %q", resp.StatusCode, body)
	}
	get(t, c, "http://other.test/")
	if got := h.proxy.TakeDenials("cmd9"); !slices.Equal(got, []Denial{{"other.test", ReasonNotAllowed}}) {
		t.Fatalf("denials via socket = %v", got)
	}
	_ = h.proxy.Close()
	if _, err := os.Stat(sock); !os.IsNotExist(err) {
		t.Fatalf("Close left the socket behind: %v", err)
	}
}
