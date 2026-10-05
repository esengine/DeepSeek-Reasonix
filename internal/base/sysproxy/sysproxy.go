// Package sysproxy resolves the OS-level proxy (Windows system/PAC settings)
// for a target URL. ForURL returns nil on platforms without system-proxy
// support or when no proxy applies, so callers fall back to direct/env.
package sysproxy

import (
	"net/netip"
	"net/url"
	"slices"
	"strings"
)

func splitList(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		return r == ';' || r == ' ' || r == '\t' || r == '\n' || r == '\r'
	})
}

// parseProxyList picks a proxy from a WinHTTP/IE proxy string for scheme. The
// string is either "host:port" (all protocols) or "http=h:p;https=h:p" form.
func parseProxyList(list, scheme string) *url.URL {
	var fallback string
	for _, f := range splitList(list) {
		if before, after, ok := strings.Cut(f, "="); ok {
			if strings.EqualFold(before, scheme) {
				return hostProxyURL(after)
			}
			continue
		}
		if fallback == "" {
			fallback = f
		}
	}
	if fallback != "" {
		return hostProxyURL(fallback)
	}
	return nil
}

func hostProxyURL(hostport string) *url.URL {
	hostport = strings.TrimSpace(hostport)
	if i := strings.Index(hostport, "://"); i >= 0 {
		hostport = hostport[i+3:]
	}
	if hostport == "" {
		return nil
	}
	return &url.URL{Scheme: "http", Host: hostport}
}

// bypassed reports whether host matches a WinINET proxy-bypass entry. "<local>"
// matches dotless (intranet) hosts, and "*" is a wildcard anywhere in an entry
// ("127.*", "*.corp.local"). Loopback is bypassed unless "<-loopback>" is listed,
// as WinINET does.
func bypassed(host, bypass string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return false
	}
	entries := splitList(strings.ToLower(bypass))
	if isLoopback(host) && !slices.Contains(entries, "<-loopback>") {
		return true
	}
	for _, e := range entries {
		if e == "<local>" {
			if !strings.Contains(host, ".") {
				return true
			}
			continue
		}
		if wildcardMatch(strings.Trim(e, "[]"), host) {
			return true
		}
	}
	return false
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip, err := netip.ParseAddr(host)
	return err == nil && ip.IsLoopback()
}

// wildcardMatch reports whether s matches pattern, where "*" matches any run of
// characters and every other character matches itself.
func wildcardMatch(pattern, s string) bool {
	parts := strings.Split(pattern, "*")
	if len(parts) == 1 {
		return pattern == s
	}
	if !strings.HasPrefix(s, parts[0]) {
		return false
	}
	s = s[len(parts[0]):]
	last := parts[len(parts)-1]
	for _, part := range parts[1 : len(parts)-1] {
		i := strings.Index(s, part)
		if i < 0 {
			return false
		}
		s = s[i+len(part):]
	}
	return len(s) >= len(last) && strings.HasSuffix(s, last)
}
