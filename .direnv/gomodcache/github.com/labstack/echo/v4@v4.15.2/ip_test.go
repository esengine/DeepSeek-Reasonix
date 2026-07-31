// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: © 2015 LabStack LLC and Echo contributors

package echo

import (
	"net"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

func mustParseCIDR(s string) *net.IPNet {
	_, IPNet, err := net.ParseCIDR(s)
	if err != nil {
		panic(err)
	}
	return IPNet
}

func TestIPChecker_TrustOption(t *testing.T) {
	var testCases = []struct {
		name         string
		givenOptions []TrustOption
		whenIP       string
		expect       bool
	}{
		{
			name: "ip is within trust range, trusts additional private IPV6 network",
			givenOptions: []TrustOption{
				TrustLoopback(false),
				TrustLinkLocal(false),
				TrustPrivateNet(false),
				// this is private IPv6 ip
				// CIDR Notation: 	2001:0db8:0000:0000:0000:0000:0000:0000/48
				// Address: 				2001:0db8:0000:0000:0000:0000:0000:0103
				// Range start: 		2001:0db8:0000:0000:0000:0000:0000:0000
				// Range end: 			2001:0db8:0000:ffff:ffff:ffff:ffff:ffff
				TrustIPRange(mustParseCIDR("2001:db8::103/48")),
			},
			whenIP: "2001:0db8:0000:0000:0000:0000:0000:0103",
			expect: true,
		},
		{
			name: "ip is within trust range, trusts additional private IPV6 network",
			givenOptions: []TrustOption{
				TrustIPRange(mustParseCIDR("2001:db8::103/48")),
			},
			whenIP: "2001:0db8:0000:0000:0000:0000:0000:0103",
			expect: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			checker := newIPChecker(tc.givenOptions)

			result := checker.trust(net.ParseIP(tc.whenIP))
			assert.Equal(t, tc.expect, result)
		})
	}
}

func TestTrustIPRange(t *testing.T) {
	var testCases = []struct {
		name       string
		givenRange string
		whenIP     string
		expect     bool
	}{
		{
			name: "ip is within trust range, IPV6 network range",
			// CIDR Notation: 2001:0db8:0000:0000:0000:0000:0000:0000/48
			// Address:       2001:0db8:0000:0000:0000:0000:0000:0103
			// Range start:   2001:0db8:0000:0000:0000:0000:0000:0000
			// Range end:     2001:0db8:0000:ffff:ffff:ffff:ffff:ffff
			givenRange: "2001:db8::103/48",
			whenIP:     "2001:0db8:0000:0000:0000:0000:0000:0103",
			expect:     true,
		},
		{
			name:       "ip is outside (upper bounds) of trust range, IPV6 network range",
			givenRange: "2001:db8::103/48",
			whenIP:     "2001:0db8:0001:0000:0000:0000:0000:0000",
			expect:     false,
		},
		{
			name:       "ip is outside (lower bounds) of trust range, IPV6 network range",
			givenRange: "2001:db8::103/48",
			whenIP:     "2001:0db7:ffff:ffff:ffff:ffff:ffff:ffff",
			expect:     false,
		},
		{
			name: "ip is within trust range, IPV4 network range",
			// CIDR Notation: 8.8.8.8/24
			// Address:       8.8.8.8
			// Range start:   8.8.8.0
			// Range end:     8.8.8.255
			givenRange: "8.8.8.0/24",
			whenIP:     "8.8.8.8",
			expect:     true,
		},
		{
			name: "ip is within trust range, IPV4 network range",
			// CIDR Notation: 8.8.8.8/24
			// Address:       8.8.8.8
			// Range start:   8.8.8.0
			// Range end:     8.8.8.255
			givenRange: "8.8.8.0/24",
			whenIP:     "8.8.8.8",
			expect:     true,
		},
		{
			name:       "ip is outside (upper bounds) of trust range, IPV4 network range",
			givenRange: "8.8.8.0/24",
			whenIP:     "8.8.9.0",
			expect:     false,
		},
		{
			name:       "ip is outside (lower bounds) of trust range, IPV4 network range",
			givenRange: "8.8.8.0/24",
			whenIP:     "8.8.7.255",
			expect:     false,
		},
		{
			name:       "public ip, trust everything in IPV4 network range",
			givenRange: "0.0.0.0/0",
			whenIP:     "8.8.8.8",
			expect:     true,
		},
		{
			name:       "internal ip, trust everything in IPV4 network range",
			givenRange: "0.0.0.0/0",
			whenIP:     "127.0.10.1",
			expect:     true,
		},
		{
			name:       "public ip, trust everything in IPV6 network range",
			givenRange: "::/0",
			whenIP:     "2a00:1450:4026:805::200e",
			expect:     true,
		},
		{
			name:       "internal ip, trust everything in IPV6 network range",
			givenRange: "::/0",
			whenIP:     "0:0:0:0:0:0:0:1",
			expect:     true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cidr := mustParseCIDR(tc.givenRange)
			checker := newIPChecker([]TrustOption{
				TrustLoopback(false),   // disable to avoid interference
				TrustLinkLocal(false),  // disable to avoid interference
				TrustPrivateNet(false), // disable to avoid interference

				TrustIPRange(cidr),
			})

			result := checker.trust(net.ParseIP(tc.whenIP))
			assert.Equal(t, tc.expect, result)
		})
	}
}

func TestTrustPrivateNet(t *testing.T) {
	var testCases = []struct {
		name   string
		whenIP string
		expect bool
	}{
		{
			name:   "do not trust public IPv4 address",
			whenIP: "8.8.8.8",
			expect: false,
		},
		{
			name:   "do not trust public IPv6 address",
			whenIP: "2a00:1450:4026:805::200e",
			expect: false,
		},

		{ // Class A: 10.0.0.0 — 10.255.255.255
			name:   "do not trust IPv4 just outside of class A (lower bounds)",
			whenIP: "9.255.255.255",
			expect: false,
		},
		{
			name:   "do not trust IPv4 just outside of class A (upper bounds)",
			whenIP: "11.0.0.0",
			expect: false,
		},
		{
			name:   "trust IPv4 of class A (lower bounds)",
			whenIP: "10.0.0.0",
			expect: true,
		},
		{
			name:   "trust IPv4 of class A (upper bounds)",
			whenIP: "10.255.255.255",
			expect: true,
		},

		{ // Class B: 172.16.0.0 — 172.31.255.255
			name:   "do not trust IPv4 just outside of class B (lower bounds)",
			whenIP: "172.15.255.255",
			expect: false,
		},
		{
			name:   "do not trust IPv4 just outside of class B (upper bounds)",
			whenIP: "172.32.0.0",
			expect: false,
		},
		{
			name:   "trust IPv4 of class B (lower bounds)",
			whenIP: "172.16.0.0",
			expect: true,
		},
		{
			name:   "trust IPv4 of class B (upper bounds)",
			whenIP: "172.31.255.255",
			expect: true,
		},

		{ // Class C: 192.168.0.0 — 192.168.255.255
			name:   "do not trust IPv4 just outside of class C (lower bounds)",
			whenIP: "192.167.255.255",
			expect: false,
		},
		{
			name:   "do not trust IPv4 just outside of class C (upper bounds)",
			whenIP: "192.169.0.0",
			expect: false,
		},
		{
			name:   "trust IPv4 of class C (lower bounds)",
			whenIP: "192.168.0.0",
			expect: true,
		},
		{
			name:   "trust IPv4 of class C (upper bounds)",
			whenIP: "192.168.255.255",
			expect: true,
		},

		{ // fc00::/7 address block = RFC 4193 Unique Local Addresses (ULA)
			// splits the address block in two equally sized halves, fc00::/8 and fd00::/8.
			// https://en.wikipedia.org/wiki/Unique_local_address
			name:   "trust IPv6 private address",
			whenIP: "fdfc:3514:2cb3:4bd5::",
			expect: true,
		},
		{
			name:   "do not trust IPv6 just out of /fd (upper bounds)",
			whenIP: "/fe00:0000:0000:0000:0000",
			expect: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			checker := newIPChecker([]TrustOption{
				TrustLoopback(false),  // disable to avoid interference
				TrustLinkLocal(false), // disable to avoid interference

				TrustPrivateNet(true),
			})

			result := checker.trust(net.ParseIP(tc.whenIP))
			assert.Equal(t, tc.expect, result)
		})
	}
}

func TestTrustLinkLocal(t *testing.T) {
	var testCases = []struct {
		name   string
		whenIP string
		expect bool
	}{
		{
			name:   "trust link local IPv4 address (lower bounds)",
			whenIP: "169.254.0.0",
			expect: true,
		},
		{
			name:   "trust link local  IPv4 address (upper bounds)",
			whenIP: "169.254.255.255",
			expect: true,
		},
		{
			name:   "do not trust link local IPv4 address (outside of lower bounds)",
			whenIP: "169.253.255.255",
			expect: false,
		},
		{
			name:   "do not trust link local  IPv4 address (outside of upper bounds)",
			whenIP: "169.255.0.0",
			expect: false,
		},
		{
			name:   "trust link local IPv6 address ",
			whenIP: "fe80::1",
			expect: true,
		},
		{
			name:   "do not trust link local IPv6 address ",
			whenIP: "fec0::1",
			expect: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			checker := newIPChecker([]TrustOption{
				TrustLoopback(false),   // disable to avoid interference
				TrustPrivateNet(false), // disable to avoid interference

				TrustLinkLocal(true),
			})

			result := checker.trust(net.ParseIP(tc.whenIP))
			assert.Equal(t, tc.expect, result)
		})
	}
}

func TestTrustLoopback(t *testing.T) {
	var testCases = []struct {
		name   string
		whenIP string
		expect bool
	}{
		{
			name:   "trust IPv4 as localhost",
			whenIP: "127.0.0.1",
			expect: true,
		},
		{
			name:   "trust IPv6 as localhost",
			whenIP: "::1",
			expect: true,
		},
		{
			name:   "do not trust public ip as localhost",
			whenIP: "8.8.8.8",
			expect: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			checker := newIPChecker([]TrustOption{
				TrustLinkLocal(false),  // disable to avoid interference
				TrustPrivateNet(false), // disable to avoid interference

				TrustLoopback(true),
			})

			result := checker.trust(net.ParseIP(tc.whenIP))
			assert.Equal(t, tc.expect, result)
		})
	}
}

func TestExtractIPDirect(t *testing.T) {
	var testCases = []struct {
		name        string
		whenRequest http.Request
		expectIP    string
	}{
		{
			name: "request has no headers, extracts IP from request remote addr",
			whenRequest: http.Request{
				RemoteAddr: "203.0.113.1:8080",
			},
			expectIP: "203.0.113.1",
		},
		{
			name: "remote addr is IP without port, extracts IP directly",
			whenRequest: http.Request{
				RemoteAddr: "203.0.113.1",
			},
			expectIP: "203.0.113.1",
		},
		{
			name: "remote addr is IPv6 without port, extracts IP directly",
			whenRequest: http.Request{
				RemoteAddr: "2001:db8::1",
			},
			expectIP: "2001:db8::1",
		},
		{
			name: "remote addr is IPv6 with port",
			whenRequest: http.Request{
				RemoteAddr: "[2001:db8::1]:8080",
			},
			expectIP: "2001:db8::1",
		},
		{
			name: "remote addr is invalid, returns empty string",
			whenRequest: http.Request{
				RemoteAddr: "invalid-ip-format",
			},
			expectIP: "",
		},
		{
			name: "request is from external IP has X-Real-Ip header, extractor still extracts IP from request remote addr",
			whenRequest: http.Request{
				Header: http.Header{
					HeaderXRealIP: []string{"203.0.113.10"},
				},
				RemoteAddr: "203.0.113.1:8080",
			},
			expectIP: "203.0.113.1",
		},
		{
			name: "request is from internal IP and has Real-IP header, extractor still extracts internal IP from request remote addr",
			whenRequest: http.Request{
				Header: http.Header{
					HeaderXRealIP: []string{"203.0.113.10"},
				},
				RemoteAddr: "127.0.0.1:8080",
			},
			expectIP: "127.0.0.1",
		},
		{
			name: "request is from external IP and has XFF + Real-IP header, extractor still extracts external IP from request remote addr",
			whenRequest: http.Request{
				Header: http.Header{
					HeaderXRealIP:       []string{"203.0.113.10"},
					HeaderXForwardedFor: []string{"192.0.2.106, 198.51.100.105, fc00::104, 2001:db8::103, 192.168.0.102, 169.254.0.101"},
				},
				RemoteAddr: "203.0.113.1:8080",
			},
			expectIP: "203.0.113.1",
		},
		{
			name: "request is from internal IP and has XFF + Real-IP header, extractor still extracts internal IP from request remote addr",
			whenRequest: http.Request{
				Header: http.Header{
					HeaderXRealIP:       []string{"127.0.0.1"},
					HeaderXForwardedFor: []string{"192.0.2.106, 198.51.100.105, fc00::104, 2001:db8::103, 192.168.0.102, 169.254.0.101"},
				},
				RemoteAddr: "127.0.0.1:8080",
			},
			expectIP: "127.0.0.1",
		},
		{
			name: "request is from external IP and has XFF header, extractor still extracts external IP from request remote addr",
			whenRequest: http.Request{
				Header: http.Header{
					HeaderXForwardedFor: []string{"192.0.2.106, 198.51.100.105, fc00::104, 2001:db8::103, 192.168.0.102, 169.254.0.101"},
				},
				RemoteAddr: "203.0.113.1:8080",
			},
			expectIP: "203.0.113.1",
		},
		{
			name: "request is from internal IP and has XFF header, extractor still extracts internal IP from request remote addr",
			whenRequest: http.Request{
				Header: http.Header{
					HeaderXForwardedFor: []string{"192.0.2.106, 198.51.100.105, fc00::104, 2001:db8::103, 192.168.0.102, 169.254.0.101"},
				},
				RemoteAddr: "127.0.0.1:8080",
			},
			expectIP: "127.0.0.1",
		},
		{
			name: "request is from internal IP and has INVALID XFF header, extractor still extracts internal IP from request remote addr",
			whenRequest: http.Request{
				Header: http.Header{
					HeaderXForwardedFor: []string{"this.is.broken.lol, 169.254.0.101"},
				},
				RemoteAddr: "127.0.0.1:8080",
			},
			expectIP: "127.0.0.1",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			extractedIP := ExtractIPDirect()(&tc.whenRequest)
			assert.Equal(t, tc.expectIP, extractedIP)
		})
	}
}

func TestExtractIPFromRealIPHeader(t *testing.T) {
	_, ipForRemoteAddrExternalRange, _ := net.ParseCIDR("203.0.113.0/24")
	_, ipv6ForRemoteAddrExternalRange, _ := net.ParseCIDR("2001:db8::/64")

	var testCases = []struct {
		name              string
		givenTrustOptions []TrustOption
		whenRequest       http.Request
		expectIP          string
	}{
		{
			name: "request has no headers, extracts IP from request remote addr",
			whenRequest: http.Request{
				RemoteAddr: "203.0.113.1:8080",
			},
			expectIP: "203.0.113.1",
		},
		{
			name: "request is from external IP has INVALID external X-Real-Ip header, extract IP from remote addr",
			whenRequest: http.Request{
				Header: http.Header{
					HeaderXRealIP: []string{"xxx.yyy.zzz.ccc"}, // <-- this is invalid
				},
				RemoteAddr: "203.0.113.1:8080",
			},
			expectIP: "203.0.113.1",
		},
		{
			name: "request is from external IP has valid + UNTRUSTED external X-Real-Ip header, extract IP from remote addr",
			givenTrustOptions: []TrustOption{ // case for "trust direct-facing proxy"
				TrustIPRange(ipForRemoteAddrExternalRange), // we trust external IP range "203.0.113.199/24"
			},
			whenRequest: http.Request{
				Header: http.Header{
					HeaderXRealIP: []string{"203.0.113.199"},
				},
				RemoteAddr: "8.8.8.8:8080", // <-- this is untrusted
			},
			expectIP: "8.8.8.8",
		},
		{
			name: "request is from external IP has valid + UNTRUSTED external X-Real-Ip header, extract IP from remote addr",
			givenTrustOptions: []TrustOption{ // case for "trust direct-facing proxy"
				TrustIPRange(ipv6ForRemoteAddrExternalRange), // we trust external IP range "203.0.113.199/24"
			},
			whenRequest: http.Request{
				Header: http.Header{
					HeaderXRealIP: []string{"[bc01:1010::9090:1888]"},
				},
				RemoteAddr: "[fe64:aa10::1]:8080", // <-- this is untrusted
			},
			expectIP: "fe64:aa10::1",
		},
		{
			name: "request is from external IP has valid + TRUSTED X-Real-Ip header, extract IP from X-Real-Ip header",
			givenTrustOptions: []TrustOption{ // case for "trust direct-facing proxy"
				TrustIPRange(ipForRemoteAddrExternalRange), // we trust external IP range "203.0.113.0/24"
			},
			whenRequest: http.Request{
				Header: http.Header{
					HeaderXRealIP: []string{"8.8.8.8"},
				},
				RemoteAddr: "203.0.113.1:8080",
			},
			expectIP: "8.8.8.8",
		},
		{
			name: "request is from external IP has valid + TRUSTED X-Real-Ip header, extract IP from X-Real-Ip header",
			givenTrustOptions: []TrustOption{ // case for "trust direct-facing proxy"
				TrustIPRange(ipv6ForRemoteAddrExternalRange), // we trust external IP range "2001:db8::/64"
			},
			whenRequest: http.Request{
				Header: http.Header{
					HeaderXRealIP: []string{"[fe64:db8::113:199]"},
				},
				RemoteAddr: "[2001:db8::113:1]:8080",
			},
			expectIP: "fe64:db8::113:199",
		},
		{
			name: "request is from external IP has XFF and valid + TRUSTED X-Real-Ip header, extract IP from X-Real-Ip header",
			givenTrustOptions: []TrustOption{ // case for "trust direct-facing proxy"
				TrustIPRange(ipForRemoteAddrExternalRange), // we trust external IP range "203.0.113.199/24"
			},
			whenRequest: http.Request{
				Header: http.Header{
					HeaderXRealIP:       []string{"8.8.8.8"},
					HeaderXForwardedFor: []string{"1.1.1.1 ,8.8.8.8"}, // <-- should not affect anything
				},
				RemoteAddr: "203.0.113.1:8080",
			},
			expectIP: "8.8.8.8",
		},
		{
			name: "request is from external IP has XFF and valid + TRUSTED X-Real-Ip header, extract IP from X-Real-Ip header",
			givenTrustOptions: []TrustOption{ // case for "trust direct-facing proxy"
				TrustIPRange(ipv6ForRemoteAddrExternalRange), // we trust external IP range "2001:db8::/64"
			},
			whenRequest: http.Request{
				Header: http.Header{
					HeaderXRealIP:       []string{"[fe64:db8::113:199]"},
					HeaderXForwardedFor: []string{"[feab:cde9::113:198], [fe64:db8::113:199]"}, // <-- should not affect anything
				},
				RemoteAddr: "[2001:db8::113:1]:8080",
			},
			expectIP: "fe64:db8::113:199",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			extractedIP := ExtractIPFromRealIPHeader(tc.givenTrustOptions...)(&tc.whenRequest)
			assert.Equal(t, tc.expectIP, extractedIP)
		})
	}
}

func TestExtractIPFromXFFHeader(t *testing.T) {
	_, ipForRemoteAddrExternalRange, _ := net.ParseCIDR("203.0.113.199/24")
	_, ipv6ForRemoteAddrExternalRange, _ := net.ParseCIDR("2001:db8::/64")

	var testCases = []struct {
		name              string
		givenTrustOptions []TrustOption
		whenRequest       http.Request
		expectIP          string
	}{
		{
			name: "request has no headers, extracts IP from request remote addr",
			whenRequest: http.Request{
				RemoteAddr: "203.0.113.1:8080",
			},
			expectIP: "203.0.113.1",
		},
		{
			name: "request has INVALID external XFF header, extract IP from remote addr",
			whenRequest: http.Request{
				Header: http.Header{
					HeaderXForwardedFor: []string{"xxx.yyy.zzz.ccc, 127.0.0.2"}, // <-- this is invalid
				},
				RemoteAddr: "127.0.0.1:8080",
			},
			expectIP: "127.0.0.1",
		},
		{
			name: "request trusts all IPs in XFF header, extract IP from furthest in XFF chain",
			whenRequest: http.Request{
				Header: http.Header{
					HeaderXForwardedFor: []string{"127.0.0.3, 127.0.0.2, 127.0.0.1"},
				},
				RemoteAddr: "127.0.0.1:8080",
			},
			expectIP: "127.0.0.3",
		},
		{
			name: "request trusts all IPs in XFF header, extract IP from furthest in XFF chain",
			whenRequest: http.Request{
				Header: http.Header{
					HeaderXForwardedFor: []string{"[fe80::3], [fe80::2], [fe80::1]"},
				},
				RemoteAddr: "[fe80::1]:8080",
			},
			expectIP: "fe80::3",
		},
		{
			name: "request is from external IP has valid + UNTRUSTED external XFF header, extract IP from remote addr",
			whenRequest: http.Request{
				Header: http.Header{
					HeaderXForwardedFor: []string{"203.0.113.199"}, // <-- this is untrusted
				},
				RemoteAddr: "203.0.113.1:8080",
			},
			expectIP: "203.0.113.1",
		},
		{
			name: "request is from external IP has valid + UNTRUSTED external XFF header, extract IP from remote addr",
			whenRequest: http.Request{
				Header: http.Header{
					HeaderXForwardedFor: []string{"[2001:db8::1]"}, // <-- this is untrusted
				},
				RemoteAddr: "[2001:db8::2]:8080",
			},
			expectIP: "2001:db8::2",
		},
		{
			name: "request is from external IP is valid and has some IPs TRUSTED XFF header, extract IP from XFF header",
			givenTrustOptions: []TrustOption{
				TrustIPRange(ipForRemoteAddrExternalRange), // we trust external IP range "203.0.113.199/24"
			},
			// from request its seems that request has been proxied through 6 servers.
			// 1) 203.0.1.100 (this is external IP set by 203.0.100.100 which we do not trust - could be spoofed)
			// 2) 203.0.100.100 (this is outside of our network but set by 203.0.113.199 which we trust to set correct IPs)
			// 3) 203.0.113.199 (we trust, for example maybe our proxy from some other office)
			// 4) 192.168.1.100 (internal IP, some internal upstream loadbalancer ala SSL offloading with F5 products)
			// 5) 127.0.0.1 (is proxy on localhost. maybe we have Nginx in front of our Echo instance doing some routing)
			whenRequest: http.Request{
				Header: http.Header{
					HeaderXForwardedFor: []string{"203.0.1.100, 203.0.100.100, 203.0.113.199, 192.168.1.100"},
				},
				RemoteAddr: "127.0.0.1:8080", // IP of proxy upstream of our APP
			},
			expectIP: "203.0.100.100", // this is first trusted IP in XFF chain
		},
		{
			name: "request is from external IP is valid and has some IPs TRUSTED XFF header, extract IP from XFF header",
			givenTrustOptions: []TrustOption{
				TrustIPRange(ipv6ForRemoteAddrExternalRange), // we trust external IP range "2001:db8::/64"
			},
			// from request its seems that request has been proxied through 6 servers.
			// 1) 2001:db8:1::1:100 (this is external IP set by 2001:db8:2::100:100 which we do not trust - could be spoofed)
			// 2) 2001:db8:2::100:100  (this is outside of our network but set by 2001:db8::113:199 which we trust to set correct IPs)
			// 3) 2001:db8::113:199 (we trust, for example maybe our proxy from some other office)
			// 4) fd12:3456:789a:1::1 (internal IP, some internal upstream loadbalancer ala SSL offloading with F5 products)
			// 5) fe80::1 (is proxy on localhost. maybe we have Nginx in front of our Echo instance doing some routing)
			whenRequest: http.Request{
				Header: http.Header{
					HeaderXForwardedFor: []string{"[2001:db8:1::1:100], [2001:db8:2::100:100], [2001:db8::113:199], [fd12:3456:789a:1::1]"},
				},
				RemoteAddr: "[fe80::1]:8080", // IP of proxy upstream of our APP
			},
			expectIP: "2001:db8:2::100:100", // this is first trusted IP in XFF chain
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			extractedIP := ExtractIPFromXFFHeader(tc.givenTrustOptions...)(&tc.whenRequest)
			assert.Equal(t, tc.expectIP, extractedIP)
		})
	}
}
