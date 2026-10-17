package config

import (
	"fmt"
	"strconv"
	"strings"
)

// renderNetworkResolvers writes the `dns` and `doh` lines of the [network] tab.
// Each is shown with an example when unset, so the shape is discoverable without
// the docs.
func renderNetworkResolvers(b *strings.Builder, c *Config) {
	if len(c.Network.DNSServers) > 0 {
		quoted := make([]string, 0, len(c.Network.DNSServers))
		for _, s := range c.Network.DNSServers {
			quoted = append(quoted, strconv.Quote(s))
		}
		fmt.Fprintf(b, "dns        = [%s]   # resolvers to use instead of the system's\n", strings.Join(quoted, ", "))
	} else {
		b.WriteString("# dns        = [\"223.5.5.5\", \"1.1.1.1\"]   # resolvers to use instead of the system's\n")
	}
	if c.Network.DoH != "" {
		fmt.Fprintf(b, "doh        = %q   # DNS over HTTPS; takes precedence over dns above\n", c.Network.DoH)
	} else {
		b.WriteString("# doh        = \"https://1.1.1.1/dns-query\"   # encrypted DNS; wins over `dns`\n")
	}
}
