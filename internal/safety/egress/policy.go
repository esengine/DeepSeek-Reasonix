package egress

import (
	"net"
	"strings"
)

// Reason is why a connection was refused. It crosses to the client as the
// X-Proxy-Error header and to the model through the host's attribution.
type Reason string

const (
	// ReasonNotAllowed is a host no allow pattern names.
	ReasonNotAllowed Reason = "blocked-by-allowlist"
	// ReasonDenied is a host a deny pattern names; deny outranks allow.
	ReasonDenied Reason = "blocked-by-denylist"
	// ReasonAddress is an allowed name that resolves only to loopback,
	// link-local, metadata or this host's own addresses.
	ReasonAddress Reason = "blocked-by-address"
	// ReasonDeclined is a host outside the list a person was asked about and
	// refused.
	ReasonDeclined Reason = "declined-by-user"
)

// Policy is the domain list. A pattern is an exact host name, or "*.name" for
// every name beneath it but not name itself. Matching is case-insensitive and
// ignores a trailing root dot. An empty Allow admits nothing.
type Policy struct {
	Allow []string
	Deny  []string
}

// Decide returns the refusal for host, or "" when the policy admits it.
func (p Policy) Decide(host string) Reason {
	host = normalizeHost(host)
	if host == "" {
		return ReasonNotAllowed
	}
	if matchesAny(host, p.Deny) {
		return ReasonDenied
	}
	if !matchesAny(host, p.Allow) {
		return ReasonNotAllowed
	}
	return ""
}

func matchesAny(host string, patterns []string) bool {
	for _, raw := range patterns {
		pattern := normalizeHost(raw)
		if suffix, ok := strings.CutPrefix(pattern, "*."); ok {
			if suffix != "" && strings.HasSuffix(host, "."+suffix) {
				return true
			}
			continue
		}
		if pattern != "" && pattern == host {
			return true
		}
	}
	return false
}

func normalizeHost(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	host = strings.TrimSuffix(host, ".")
	if ip := net.ParseIP(strings.Trim(host, "[]")); ip != nil {
		return ip.String()
	}
	return host
}
