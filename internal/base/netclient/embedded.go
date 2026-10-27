package netclient

import "net"

var (
	nat64Prefix  = mustPrefix("64:ff9b::/96")
	sixToFour    = mustPrefix("2002::/16")
	ipv4Compat   = mustPrefix("::/96")
	teredoPrefix = mustPrefix("2001::/32")
)

func mustPrefix(s string) *net.IPNet {
	_, n, err := net.ParseCIDR(s)
	if err != nil {
		panic(err)
	}
	return n
}

// EmbeddedIPv4 is the IPv4 address an IPv6 transition form delivers to: NAT64,
// 6to4, IPv4-compatible and Teredo carry one, and a guard that judges only the
// IPv6 spelling lets the carried address through. nil when ip carries none; an
// IPv4-mapped address is returned as its IPv4 form.
func EmbeddedIPv4(ip net.IP) net.IP {
	if v4 := ip.To4(); v4 != nil {
		return v4
	}
	ip16 := ip.To16()
	if ip16 == nil {
		return nil
	}
	switch {
	case nat64Prefix.Contains(ip16), ipv4Compat.Contains(ip16):
		return net.IP(append([]byte(nil), ip16[12:16]...))
	case sixToFour.Contains(ip16):
		return net.IP(append([]byte(nil), ip16[2:6]...))
	case teredoPrefix.Contains(ip16):
		out := make([]byte, 4)
		for i := range out {
			out[i] = ip16[12+i] ^ 0xff
		}
		return net.IP(out)
	}
	return nil
}
