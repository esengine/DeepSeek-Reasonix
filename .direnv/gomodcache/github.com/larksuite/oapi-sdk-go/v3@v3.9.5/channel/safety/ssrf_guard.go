package safety

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strings"
)

var blockedV4Blocks []*net.IPNet

func init() {
	cidrs := []string{
		"0.0.0.0/8",       // 0.0.0.0/8
		"10.0.0.0/8",      // 10.0.0.0/8
		"127.0.0.0/8",     // 127.0.0.0/8
		"169.254.0.0/16",  // 169.254.0.0/16
		"172.16.0.0/12",   // 172.16.0.0/12
		"192.168.0.0/16",  // 192.168.0.0/16
		"100.64.0.0/10",   // 100.64.0.0/10 (CGNAT)
		"192.0.0.0/24",    // 192.0.0.0/24
		"192.0.2.0/24",    // 192.0.2.0/24
		"198.18.0.0/15",   // 198.18.0.0/15
		"198.51.100.0/24", // 198.51.100.0/24
		"203.0.113.0/24",  // 203.0.113.0/24
		"224.0.0.0/4",     // 224.0.0.0/4 (multicast)
		"240.0.0.0/4",     // 240.0.0.0/4 (reserved)
	}
	for _, c := range cidrs {
		_, block, err := net.ParseCIDR(c)
		if err == nil {
			blockedV4Blocks = append(blockedV4Blocks, block)
		}
	}
}

// SsrfGuardOptions provides options for SSRF guard.
type SsrfGuardOptions struct {
	Allowlist []string
}

// AssertPublicURL validates if a URL is safe to be fetched.
func AssertPublicURL(ctx context.Context, u string, opts *SsrfGuardOptions) error {
	parsed, err := url.Parse(u)
	if err != nil {
		return fmt.Errorf("ssrf_blocked: invalid url")
	}

	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("ssrf_blocked: protocol %s", parsed.Scheme)
	}

	host := parsed.Hostname()
	if opts != nil {
		for _, allowed := range opts.Allowlist {
			if host == allowed {
				return nil
			}
		}
	}

	var ips []net.IP
	ip := net.ParseIP(host)
	if ip != nil {
		ips = append(ips, ip)
	} else {
		resolved, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
		if err != nil {
			return fmt.Errorf("ssrf_blocked: dns lookup failed: %w", err)
		}
		ips = resolved
	}

	for _, ip := range ips {
		if ip.To4() != nil {
			if ipv4Blocked(ip.To4()) {
				return fmt.Errorf("ssrf_blocked: %s", ip.String())
			}
		} else {
			if ipv6Blocked(ip) {
				return fmt.Errorf("ssrf_blocked: %s", ip.String())
			}
		}
	}

	return nil
}

func ipv4Blocked(ip net.IP) bool {
	for _, block := range blockedV4Blocks {
		if block.Contains(ip) {
			return true
		}
	}
	return false
}

func ipv6Blocked(ip net.IP) bool {
	if ip.IsLoopback() {
		return true
	}
	if ip.IsUnspecified() {
		return true
	}
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}
	if ip.IsPrivate() { // Includes fc00::/7
		return true
	}
	if ip.IsMulticast() {
		return true
	}

	// Handle IPv4-mapped IPv6 addresses (::ffff:a.b.c.d)
	if ip4 := ip.To4(); ip4 != nil {
		// If it's a valid IPv4 embedded, To4() returns the 4-byte representation
		// Wait, net.IP.To4() returns non-nil for IPv4-mapped IPv6.
		// If it's an IPv4-mapped IPv6, the above loop wouldn't treat it as IPv6 because To4() != nil.
		// But just in case:
		return ipv4Blocked(ip4)
	}

	lower := strings.ToLower(ip.String())
	if strings.HasPrefix(lower, "fe80:") {
		return true
	}
	if strings.HasPrefix(lower, "fc") || strings.HasPrefix(lower, "fd") {
		return true
	}
	if strings.HasPrefix(lower, "ff") {
		return true
	}
	return false
}
