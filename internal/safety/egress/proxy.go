package egress

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/proxy"
)

// ErrUpstreamScheme is an upstream proxy the egress proxy cannot chain
// through; HTTP and SOCKS5 proxies are supported.
var ErrUpstreamScheme = errors.New("egress: upstream proxy scheme not supported")

const (
	dialTimeout      = 30 * time.Second
	maxDenialsPerCmd = 32
)

// Options configures a Proxy.
type Options struct {
	// Upstream answers which proxy the host itself would use for a request,
	// the way http.ProxyFromEnvironment does. Nil dials every target directly.
	Upstream func(*http.Request) (*url.URL, error)
}

// Denial is one refused connection, recorded against the command whose token
// the client presented.
type Denial struct {
	Host   string
	Reason Reason
}

// Proxy is a loopback HTTP proxy that admits only what its Policy allows.
type Proxy struct {
	policy   Policy
	upstream func(*http.Request) (*url.URL, error)
	ln       net.Listener
	srv      *http.Server
	socket   string

	lookup func(ctx context.Context, host string) ([]net.IP, error)
	dial   func(ctx context.Context, network, addr string) (net.Conn, error)
	local  func() []net.IP

	mu      sync.Mutex
	denials map[string][]Denial
	askers  map[string]*asker
	tunnels map[net.Conn]struct{}
}

// asker puts one command's out-of-list hosts to a person, one question at a
// time, and remembers each answer for the rest of that command.
type asker struct {
	mu      sync.Mutex
	ask     func(host string) (bool, error)
	answers map[string]Reason
}

func (a *asker) decide(host string) Reason {
	a.mu.Lock()
	defer a.mu.Unlock()
	if r, ok := a.answers[host]; ok {
		return r
	}
	allowed, err := a.ask(host)
	r := Reason("")
	switch {
	case err != nil:
		r = ReasonNotAllowed
	case !allowed:
		r = ReasonDeclined
	}
	a.answers[host] = r
	return r
}

// Start listens on an ephemeral loopback port and serves until Close.
func Start(policy Policy, opts Options) (*Proxy, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	p := newProxy(policy, opts, ln)
	go func() { _ = p.srv.Serve(ln) }()
	return p, nil
}

func newProxy(policy Policy, opts Options, ln net.Listener) *Proxy {
	d := &net.Dialer{Timeout: dialTimeout}
	p := &Proxy{
		policy:   policy,
		upstream: opts.Upstream,
		ln:       ln,
		lookup: func(ctx context.Context, host string) ([]net.IP, error) {
			return net.DefaultResolver.LookupIP(ctx, "ip", host)
		},
		dial:    d.DialContext,
		local:   interfaceAddresses,
		denials: map[string][]Denial{},
		askers:  map[string]*asker{},
		tunnels: map[net.Conn]struct{}{},
	}
	p.srv = &http.Server{Handler: p, ReadHeaderTimeout: dialTimeout}
	return p
}

// Addr is the host:port clients are pointed at.
func (p *Proxy) Addr() string { return p.ln.Addr().String() }

// Port is the loopback port the OS sandbox leaves open.
func (p *Proxy) Port() int { return p.ln.Addr().(*net.TCPAddr).Port }

// ListenUnix also serves on a Unix socket at path, for a sandbox whose own
// network namespace cannot reach this host's loopback. Only this user may
// connect to it.
func (p *Proxy) ListenUnix(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	_ = os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = ln.Close()
		return err
	}
	p.mu.Lock()
	p.socket = path
	p.mu.Unlock()
	go func() { _ = p.srv.Serve(ln) }()
	return nil
}

// SocketPath is where ListenUnix serves, or "".
func (p *Proxy) SocketPath() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.socket
}

// Close stops the listeners and every tunnel still open through them.
func (p *Proxy) Close() error {
	err := p.srv.Close()
	if socket := p.SocketPath(); socket != "" {
		_ = os.Remove(socket)
	}
	p.mu.Lock()
	for c := range p.tunnels {
		_ = c.Close()
	}
	p.mu.Unlock()
	return err
}

// NewToken names one command's traffic. It rides the proxy URL as userinfo,
// which clients return as Proxy-Authorization on every request.
func NewToken() string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// EnvKeys are the variables Env sets, for callers that must clear inherited ones.
var EnvKeys = []string{
	"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy",
	"NO_PROXY", "no_proxy", "npm_config_proxy", "npm_config_https_proxy",
	"YARN_HTTP_PROXY", "YARN_HTTPS_PROXY",
}

// Env points a command's clients at the proxy under token. NO_PROXY names
// loopback alone: the sandbox leaves loopback open, and any other host that
// bypassed the proxy would find direct egress shut.
func (p *Proxy) Env(token string) []string {
	u := "http://" + token + "@" + p.Addr()
	out := make([]string, 0, len(EnvKeys))
	for _, k := range EnvKeys {
		v := u
		if strings.EqualFold(k, "NO_PROXY") {
			v = "localhost,127.0.0.1,::1"
		}
		out = append(out, k+"="+v)
	}
	return out
}

// Ask lets a host outside the allow list be put to a person for the command
// holding token, instead of refused outright. Denied hosts are never asked.
func (p *Proxy) Ask(token string, ask func(host string) (bool, error)) {
	if token == "" || ask == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.askers[token] = &asker{ask: ask, answers: map[string]Reason{}}
}

// TakeDenials returns what was refused under token and ends the token.
func (p *Proxy) TakeDenials(token string) []Denial {
	p.mu.Lock()
	defer p.mu.Unlock()
	d := p.denials[token]
	delete(p.denials, token)
	delete(p.askers, token)
	return d
}

// decide applies the policy, putting a host outside the list to the command's
// asker when it has one.
func (p *Proxy) decide(token, host string) Reason {
	reason := p.policy.Decide(host)
	if reason != ReasonNotAllowed {
		return reason
	}
	p.mu.Lock()
	a := p.askers[token]
	p.mu.Unlock()
	if a == nil {
		return reason
	}
	return a.decide(normalizeHost(host))
}

// Refusals renders TakeDenials for the sandbox's EgressRoute.
func (p *Proxy) Refusals(token string) []string {
	denials := p.TakeDenials(token)
	out := make([]string, 0, len(denials))
	for _, d := range denials {
		out = append(out, d.Host+" ("+string(d.Reason)+")")
	}
	return out
}

func (p *Proxy) record(token, host string, reason Reason) {
	p.mu.Lock()
	defer p.mu.Unlock()
	list := p.denials[token]
	for _, d := range list {
		if d.Host == host && d.Reason == reason {
			return
		}
	}
	if len(list) < maxDenialsPerCmd {
		p.denials[token] = append(list, Denial{Host: host, Reason: reason})
	}
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	token := proxyToken(r.Header.Get("Proxy-Authorization"))
	if r.Method == http.MethodConnect {
		p.serveConnect(w, r, token)
		return
	}
	if !r.URL.IsAbs() {
		http.Error(w, "reasonix egress proxy: absolute URL required", http.StatusBadRequest)
		return
	}
	p.serveForward(w, r, token)
}

// admit decides host:port and returns where to dial. Through an upstream the
// name is passed on, since the upstream resolves it; the local lookup still
// refuses a name this host sees resolving only to forbidden addresses.
func (p *Proxy) admit(ctx context.Context, token, host, port string, upstream *url.URL) (string, Reason, error) {
	if reason := p.decide(token, host); reason != "" {
		return "", reason, nil
	}
	if ip := net.ParseIP(strings.Trim(host, "[]")); ip != nil {
		if forbiddenAddress(ip, p.local) {
			return "", ReasonAddress, nil
		}
		return net.JoinHostPort(ip.String(), port), "", nil
	}
	ips, err := p.lookup(ctx, host)
	if err != nil {
		if upstream != nil {
			return net.JoinHostPort(host, port), "", nil
		}
		return "", "", err
	}
	for _, ip := range ips {
		if forbiddenAddress(ip, p.local) {
			continue
		}
		if upstream != nil {
			return net.JoinHostPort(host, port), "", nil
		}
		return net.JoinHostPort(ip.String(), port), "", nil
	}
	return "", ReasonAddress, nil
}

func (p *Proxy) refuse(w http.ResponseWriter, token, host string, reason Reason) {
	p.record(token, host, reason)
	w.Header().Set("X-Proxy-Error", string(reason))
	http.Error(w, fmt.Sprintf("reasonix sandbox: egress to %s refused (%s)", host, reason), http.StatusForbidden)
}

func (p *Proxy) upstreamFor(target *url.URL) (*url.URL, error) {
	if p.upstream == nil {
		return nil, nil
	}
	u, err := p.upstream(&http.Request{Method: http.MethodGet, URL: target, Header: http.Header{}})
	if err != nil || u == nil {
		return nil, err
	}
	switch u.Scheme {
	case "http", "socks5", "socks5h":
		return u, nil
	}
	return nil, fmt.Errorf("%w: %s", ErrUpstreamScheme, u.Scheme)
}

func isSOCKS(u *url.URL) bool { return u != nil && strings.HasPrefix(u.Scheme, "socks5") }

// dialSOCKS reaches addr through a SOCKS5 upstream, which is handed the name
// so it resolves it the way it would for any other client.
func (p *Proxy) dialSOCKS(ctx context.Context, up *url.URL, addr string) (net.Conn, error) {
	var auth *proxy.Auth
	if up.User != nil {
		pass, _ := up.User.Password()
		auth = &proxy.Auth{User: up.User.Username(), Password: pass}
	}
	d, err := proxy.SOCKS5("tcp", up.Host, auth, contextDialer(p.dial))
	if err != nil {
		return nil, err
	}
	return d.(proxy.ContextDialer).DialContext(ctx, "tcp", addr)
}

type contextDialer func(ctx context.Context, network, addr string) (net.Conn, error)

func (d contextDialer) Dial(network, addr string) (net.Conn, error) {
	return d(context.Background(), network, addr)
}

func (d contextDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	return d(ctx, network, addr)
}

func (p *Proxy) serveConnect(w http.ResponseWriter, r *http.Request, token string) {
	host, port, err := net.SplitHostPort(r.Host)
	if err != nil {
		host, port = r.Host, "443"
	}
	up, err := p.upstreamFor(&url.URL{Scheme: "https", Host: net.JoinHostPort(host, port)})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	addr, reason, err := p.admit(r.Context(), token, host, port, up)
	switch {
	case reason != "":
		p.refuse(w, token, host, reason)
		return
	case err != nil:
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	remote, buffered, err := p.connectTo(r.Context(), addr, up)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	client, rw, err := http.NewResponseController(w).Hijack()
	if err != nil {
		_ = remote.Close()
		return
	}
	_, _ = rw.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
	if rw.Flush() != nil {
		_ = remote.Close()
		_ = client.Close()
		return
	}
	p.tunnel(client, rw.Reader, remote, buffered)
}

// connectTo opens a byte stream to addr, through the upstream when there is
// one. The reader carries whatever the upstream sent past its CONNECT reply.
func (p *Proxy) connectTo(ctx context.Context, addr string, up *url.URL) (net.Conn, io.Reader, error) {
	if up == nil || isSOCKS(up) {
		var c net.Conn
		var err error
		if up == nil {
			c, err = p.dial(ctx, "tcp", addr)
		} else {
			c, err = p.dialSOCKS(ctx, up, addr)
		}
		return c, c, err
	}
	c, err := p.dial(ctx, "tcp", up.Host)
	if err != nil {
		return nil, nil, err
	}
	req := &http.Request{Method: http.MethodConnect, URL: &url.URL{Opaque: addr}, Host: addr, Header: http.Header{}}
	if up.User != nil {
		pass, _ := up.User.Password()
		req.Header.Set("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(up.User.Username()+":"+pass)))
	}
	if err := req.Write(c); err != nil {
		_ = c.Close()
		return nil, nil, err
	}
	br := bufio.NewReader(c)
	resp, err := http.ReadResponse(br, req)
	if err != nil {
		_ = c.Close()
		return nil, nil, err
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_ = c.Close()
		return nil, nil, fmt.Errorf("upstream proxy refused CONNECT %s: %s", addr, resp.Status)
	}
	return c, br, nil
}

func (p *Proxy) tunnel(client net.Conn, fromClient io.Reader, remote net.Conn, fromRemote io.Reader) {
	p.mu.Lock()
	p.tunnels[client], p.tunnels[remote] = struct{}{}, struct{}{}
	p.mu.Unlock()
	var wg sync.WaitGroup
	wg.Add(2)
	pipe := func(dst net.Conn, src io.Reader) {
		defer wg.Done()
		_, _ = io.Copy(dst, src)
		_ = client.Close()
		_ = remote.Close()
	}
	go pipe(remote, fromClient)
	go pipe(client, fromRemote)
	wg.Wait()
	p.mu.Lock()
	delete(p.tunnels, client)
	delete(p.tunnels, remote)
	p.mu.Unlock()
}

var hopHeaders = []string{
	"Proxy-Authorization", "Proxy-Connection", "Connection", "Keep-Alive",
	"TE", "Trailer", "Transfer-Encoding", "Upgrade",
}

func (p *Proxy) serveForward(w http.ResponseWriter, r *http.Request, token string) {
	host, port := r.URL.Hostname(), r.URL.Port()
	if port == "" {
		port = "80"
		if r.URL.Scheme == "https" {
			port = "443"
		}
	}
	up, err := p.upstreamFor(r.URL)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	addr, reason, err := p.admit(r.Context(), token, host, port, up)
	switch {
	case reason != "":
		p.refuse(w, token, host, reason)
		return
	case err != nil:
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			switch {
			case isSOCKS(up):
				return p.dialSOCKS(ctx, up, addr)
			case up != nil:
				return p.dial(ctx, network, up.Host)
			}
			return p.dial(ctx, network, addr)
		},
		DisableKeepAlives: true,
	}
	if up != nil && !isSOCKS(up) {
		transport.Proxy = http.ProxyURL(up)
	}
	out := r.Clone(r.Context())
	out.RequestURI = ""
	for _, h := range hopHeaders {
		out.Header.Del(h)
	}
	resp, err := transport.RoundTrip(out)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	for _, h := range hopHeaders {
		resp.Header.Del(h)
	}
	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

// proxyToken reads the username of a Basic Proxy-Authorization header.
func proxyToken(header string) string {
	raw, ok := strings.CutPrefix(header, "Basic ")
	if !ok {
		return ""
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	user, _, _ := strings.Cut(string(decoded), ":")
	return user
}
