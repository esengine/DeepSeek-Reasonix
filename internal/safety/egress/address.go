package egress

import (
	"net"
	"sync"
)

// metadataNets are cloud instance-metadata endpoints outside the link-local
// range: Alibaba, Azure's wire server, and the AWS IPv6 endpoint.
var metadataNets = mustCIDRs("100.100.100.200/32", "168.63.129.16/32", "fd00:ec2::/32")

var (
	nat64Net  = mustCIDRs("64:ff9b::/96")[0]
	sixToFour = mustCIDRs("2002::/16")[0]
)

// forbiddenAddress reports whether dialing ip would reach this machine, its
// link, or a metadata service. An IPv6 form that embeds an IPv4 address is
// judged by the address it carries. Private ranges stay reachable: a company
// registry is a legitimate place to fetch from.
func forbiddenAddress(ip net.IP, local func() []net.IP) bool {
	for _, candidate := range embedded(ip) {
		if candidate.IsLoopback() || candidate.IsUnspecified() || candidate.IsLinkLocalUnicast() ||
			candidate.IsLinkLocalMulticast() || candidate.IsInterfaceLocalMulticast() || candidate.IsMulticast() {
			return true
		}
		for _, n := range metadataNets {
			if n.Contains(candidate) {
				return true
			}
		}
		for _, own := range local() {
			if own.Equal(candidate) {
				return true
			}
		}
	}
	return false
}

func embedded(ip net.IP) []net.IP {
	out := []net.IP{ip}
	if v4 := ip.To4(); v4 != nil {
		return append(out, v4)
	}
	if ip16 := ip.To16(); ip16 != nil {
		if nat64Net.Contains(ip16) {
			out = append(out, net.IP(ip16[12:16]))
		}
		if sixToFour.Contains(ip16) {
			out = append(out, net.IP(ip16[2:6]))
		}
	}
	return out
}

// interfaceAddresses lists this host's own addresses once per process, so an
// interface brought up afterwards is not in it.
var interfaceAddresses = sync.OnceValue(func() []net.IP {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	out := make([]net.IP, 0, len(addrs))
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok {
			out = append(out, n.IP)
		}
	}
	return out
})

func mustCIDRs(cidrs ...string) []*net.IPNet {
	out := make([]*net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			panic(err)
		}
		out = append(out, n)
	}
	return out
}
