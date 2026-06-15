// Package netclient builds HTTP clients and proxy resolvers that share Reasonix's
// user-facing proxy settings. web_fetch reuses the resolver while keeping its own
// dial-time SSRF guard.
package netclient

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/http/httpproxy"

	"reasonix/internal/sysproxy"
)

const (
	ModeAuto   = "auto"
	ModeEnv    = "env"
	ModeCustom = "custom"
	ModeOff    = "off"
)

// ProxySpec is the resolved proxy configuration used by network clients. URL is
// an advanced override; otherwise Type/Server/Port/Credentials are composed into a
// proxy URL. NoProxy is honored for custom proxies. DirectHosts always bypass the
// proxy in every mode (the caller derives them, e.g. from no_proxy providers).
type ProxySpec struct {
	Mode        string
	URL         string
	NoProxy     string
	Type        string
	Server      string
	Port        int
	Username    string
	Password    string
	DirectHosts []string
}

// TransportOptions lets callers keep their existing network timeouts while
// sharing proxy behavior. ForceIPv4 pins the dialer to tcp4 — the desktop updater
// uses it to retry over IPv4 when an IPv6 route (CN → Cloudflare) resets mid-transfer.
type TransportOptions struct {
	DialTimeout           time.Duration
	KeepAlive             time.Duration
	TLSHandshakeTimeout   time.Duration
	ResponseHeaderTimeout time.Duration
	ForceIPv4             bool
}

// NormalizeMode maps empty and unknown modes to auto, preserving a fail-open
// default for older configs.
func NormalizeMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case ModeEnv:
		return ModeEnv
	case ModeCustom:
		return ModeCustom
	case ModeOff:
		return ModeOff
	default:
		return ModeAuto
	}
}

// Validate reports whether spec can be used. Non-custom modes have no required
// fields; custom needs either a complete URL or a structured server+port.
func Validate(spec ProxySpec) error {
	_, err := proxyFunc(spec)
	return err
}

// ProxyFunc returns the per-request proxy resolver for spec.
func ProxyFunc(spec ProxySpec) (func(*http.Request) (*url.URL, error), error) {
	return proxyFunc(spec)
}

// NewHTTPClient returns an HTTP client with Reasonix proxy settings applied.
func NewHTTPClient(spec ProxySpec, opts TransportOptions) (*http.Client, error) {
	tr, err := NewTransport(spec, opts)
	if err != nil {
		return nil, err
	}
	return &http.Client{Transport: tr}, nil
}

// NewTransport clones net/http's default transport and overlays the requested
// proxy and timeout knobs. Cloning preserves defaults such as HTTP/2 support,
// connection pooling, and environment-proxy behavior for auto/env modes.
func NewTransport(spec ProxySpec, opts TransportOptions) (*http.Transport, error) {
	tr := defaultTransport()
	proxy, err := proxyFunc(spec)
	if err != nil {
		return nil, err
	}
	tr.Proxy = proxy
	if opts.DialTimeout != 0 || opts.KeepAlive != 0 || opts.ForceIPv4 {
		d := &net.Dialer{Timeout: opts.DialTimeout, KeepAlive: opts.KeepAlive}
		if opts.ForceIPv4 {
			// Default-transport dialer uses 30s/30s; keep those when no explicit
			// timeout is set so forcing IPv4 doesn't drop the dial deadline.
			if d.Timeout == 0 {
				d.Timeout = 30 * time.Second
			}
			if d.KeepAlive == 0 {
				d.KeepAlive = 30 * time.Second
			}
			tr.DialContext = func(ctx context.Context, _, addr string) (net.Conn, error) {
				return d.DialContext(ctx, "tcp4", addr)
			}
		} else {
			tr.DialContext = d.DialContext
		}
	}
	if opts.TLSHandshakeTimeout != 0 {
		tr.TLSHandshakeTimeout = opts.TLSHandshakeTimeout
	}
	if opts.ResponseHeaderTimeout != 0 {
		tr.ResponseHeaderTimeout = opts.ResponseHeaderTimeout
	}
	return tr, nil
}

// Summary returns a redacted, user-facing description for diagnostics.
func Summary(spec ProxySpec) string {
	switch NormalizeMode(spec.Mode) {
	case ModeOff:
		return "off (direct)"
	case ModeEnv:
		return "env"
	case ModeCustom:
		u, err := customProxyURL(spec)
		if err != nil {
			return "custom (invalid)"
		}
		return "custom (" + redactURL(u) + ")"
	default:
		return "auto (env)"
	}
}

func defaultTransport() *http.Transport {
	if base, ok := http.DefaultTransport.(*http.Transport); ok {
		return base.Clone()
	}
	return &http.Transport{Proxy: http.ProxyFromEnvironment}
}

func proxyFunc(spec ProxySpec) (func(*http.Request) (*url.URL, error), error) {
	base, err := baseProxyFunc(spec)
	if err != nil {
		return nil, err
	}
	return withDirectHosts(base, spec.DirectHosts), nil
}

func baseProxyFunc(spec ProxySpec) (func(*http.Request) (*url.URL, error), error) {
	switch NormalizeMode(spec.Mode) {
	case ModeOff:
		return nil, nil
	case ModeCustom:
		u, err := customProxyURL(spec)
		if err != nil {
			return nil, err
		}
		cfg := &httpproxy.Config{
			HTTPProxy:  u.String(),
			HTTPSProxy: u.String(),
			NoProxy:    strings.TrimSpace(spec.NoProxy),
		}
		pf := cfg.ProxyFunc()
		return func(req *http.Request) (*url.URL, error) { return pf(req.URL) }, nil
	case ModeEnv:
		return environmentProxyFunc(), nil
	default:
		return autoProxyFunc(), nil
	}
}

// withDirectHosts makes the listed hosts (and their subdomains) bypass the proxy
// in every mode. The caller decides which hosts are direct — netclient stays
// provider-agnostic. A China-only endpoint reached through a foreign-exit proxy
// resets the TLS handshake (SSL_ERROR_SYSCALL, #2803), so its provider marks it.
func withDirectHosts(pf func(*http.Request) (*url.URL, error), hosts []string) func(*http.Request) (*url.URL, error) {
	if pf == nil || len(hosts) == 0 {
		return pf
	}
	norm := make([]string, 0, len(hosts))
	for _, h := range hosts {
		if h = strings.ToLower(strings.TrimSpace(h)); h != "" {
			norm = append(norm, h)
		}
	}
	return func(req *http.Request) (*url.URL, error) {
		host := strings.ToLower(req.URL.Hostname())
		for _, h := range norm {
			if host == h || strings.HasSuffix(host, "."+h) {
				return nil, nil
			}
		}
		return pf(req)
	}
}

func environmentProxyFunc() func(*http.Request) (*url.URL, error) {
	cfg := httpproxy.FromEnvironment()
	pf := cfg.ProxyFunc()
	return func(req *http.Request) (*url.URL, error) { return pf(req.URL) }
}

// autoProxyFunc honors environment proxy vars first, then falls back to the OS
// system proxy (Windows IE/PAC/WPAD) so corporate Windows machines work without
// any manual HTTP_PROXY setup. Non-Windows resolves to env-only.
func autoProxyFunc() func(*http.Request) (*url.URL, error) {
	pf := httpproxy.FromEnvironment().ProxyFunc()
	return func(req *http.Request) (*url.URL, error) {
		if u, err := pf(req.URL); err != nil || u != nil {
			return u, err
		}
		return sysproxy.ForURL(req.URL)
	}
}

func customProxyURL(spec ProxySpec) (*url.URL, error) {
	if raw := strings.TrimSpace(spec.URL); raw != "" {
		u, err := url.Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("network proxy_url: %w", err)
		}
		if err := validateProxyURL(u); err != nil {
			return nil, err
		}
		return u, nil
	}
	typ := strings.ToLower(strings.TrimSpace(spec.Type))
	if typ == "" {
		typ = "http"
	}
	switch typ {
	case "http", "https", "socks5", "socks5h":
	default:
		return nil, fmt.Errorf("network proxy type %q: must be http|https|socks5|socks5h", spec.Type)
	}
	server := strings.TrimSpace(spec.Server)
	if server == "" {
		return nil, fmt.Errorf("network proxy server is required when proxy_mode = custom")
	}
	if spec.Port <= 0 || spec.Port > 65535 {
		return nil, fmt.Errorf("network proxy port must be 1..65535")
	}
	u := &url.URL{Scheme: typ, Host: net.JoinHostPort(server, strconv.Itoa(spec.Port))}
	if spec.Username != "" {
		if spec.Password != "" {
			u.User = url.UserPassword(spec.Username, spec.Password)
		} else {
			u.User = url.User(spec.Username)
		}
	}
	return u, nil
}

func validateProxyURL(u *url.URL) error {
	switch strings.ToLower(u.Scheme) {
	case "http", "https", "socks5", "socks5h":
	default:
		return fmt.Errorf("network proxy_url scheme %q: must be http|https|socks5|socks5h", u.Scheme)
	}
	if u.Hostname() == "" {
		return fmt.Errorf("network proxy_url host is required")
	}
	return nil
}

func redactURL(u *url.URL) string {
	cp := *u
	if cp.User != nil {
		if name := cp.User.Username(); name != "" {
			cp.User = url.User(name)
		} else {
			cp.User = nil
		}
	}
	return cp.String()
}
