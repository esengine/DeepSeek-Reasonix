// Package sysproxy resolves the OS-level proxy (Windows system/PAC settings)
// for a target URL. ForURL returns nil on platforms without system-proxy
// support or when no proxy applies, so callers fall back to direct/env.
package sysproxy

import (
	"net/url"
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
		if i := strings.IndexByte(f, '='); i >= 0 {
			if strings.EqualFold(f[:i], scheme) {
				return hostProxyURL(f[i+1:])
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
// matches dotless (intranet) hosts; a leading "*" is a suffix wildcard.
func bypassed(host, bypass string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return false
	}
	for _, e := range splitList(bypass) {
		e = strings.ToLower(e)
		switch {
		case e == "<local>":
			if !strings.Contains(host, ".") {
				return true
			}
		case strings.HasPrefix(e, "*"):
			if strings.HasSuffix(host, strings.TrimPrefix(e, "*")) {
				return true
			}
		case host == e:
			return true
		}
	}
	return false
}
