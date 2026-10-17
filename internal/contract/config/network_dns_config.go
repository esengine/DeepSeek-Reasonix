package config

import (
	"strings"

	"reasonix/internal/base/netdns"
)

// NetworkResolverConfig holds the name-server settings. It is a separate type so
// its fields and their comments live beside the code that reads them, rather
// than growing `config.go` past its file ceiling.
type NetworkResolverConfig struct {
	// DNSServers names the resolvers to use instead of the system's, most
	// useful first. An entry is a `host` or `host:port`.
	DNSServers []string `toml:"dns"`
	// DoH is a DNS-over-HTTPS endpoint, e.g. "https://1.1.1.1/dns-query". When
	// set it takes precedence over DNSServers.
	DoH string `toml:"doh"`
}

// NetworkDoH is the DNS-over-HTTPS endpoint the person configured, or "".
//
// `${VAR}` expands from the process environment for the same reason the proxy
// fields do, and the value is trimmed because a TOML string spelled across lines
// arrives with whitespace around it.
func (c *Config) NetworkDoH() string {
	return strings.TrimSpace(ExpandVars(c.Network.DoH))
}

// NetworkDNS is the resolver list the person configured, or nil. Entries are
// trimmed and empties dropped, so a trailing comma is not a resolver that fails
// to answer.
func (c *Config) NetworkDNS() []string {
	out := make([]string, 0, len(c.Network.DNSServers))
	for _, raw := range c.Network.DNSServers {
		if host := strings.TrimSpace(ExpandVars(raw)); host != "" {
			out = append(out, host)
		}
	}
	return out
}

// normalizeNetworkResolver refuses a DoH endpoint that would not be encrypted:
// `http://` reads as encrypted DNS while sending every query in the clear, so it
// is dropped and said through the load warnings rather than left to fail at the
// first lookup. A rejected `doh` falls back to `dns`.
func normalizeNetworkResolver(c *Config) {
	if c == nil {
		return
	}
	raw := strings.TrimSpace(c.Network.DoH)
	if raw == "" {
		return
	}
	if _, err := netdns.ValidateEndpoint(ExpandVars(raw)); err != nil {
		c.addLoadWarning(err.Error() + "; the DoH endpoint was ignored")
		c.Network.DoH = ""
	}
}
