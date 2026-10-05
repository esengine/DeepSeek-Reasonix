package externalurl

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"strconv"
	"strings"

	"golang.org/x/net/idna"
)

var errHost = errors.New("invalid external URL host")

// Host interprets an HTTP URL's literal hostname without resolving or fetching it.
// Numeric IPv4 spellings follow the browser URL standard rather than net/url.
func Host(raw string) (name string, local bool, err error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Opaque != "" {
		return "", false, errHost
	}
	host := u.Hostname()
	if addr, e := netip.ParseAddr(host); e == nil && addr.Is6() && addr.Zone() == "" {
		name = addr.String()
		if addr.Is4In6() {
			bytes := addr.As16()
			name = fmt.Sprintf("::ffff:%x:%x", binary.BigEndian.Uint16(bytes[12:14]), binary.BigEndian.Uint16(bytes[14:16]))
		}
		return "[" + name + "]", localAddress(addr), nil
	}
	profile := idna.New(idna.MapForLookup(), idna.StrictDomainName(false), idna.ValidateLabels(false), idna.VerifyDNSLength(false), idna.Transitional(false))
	name, err = profile.ToASCII(host)
	if err != nil || name == "" || strings.ContainsAny(name, "#%/:<>?@[\\]^|") {
		return "", false, errHost
	}
	name = strings.ToLower(name)
	parts := strings.Split(strings.TrimSuffix(name, "."), ".")
	last := parts[len(parts)-1]
	_, numberErr := ipv4Number(last)
	if numberErr == nil || allDigits(last) {
		addr, e := browserIPv4(parts)
		if e != nil {
			return "", false, e
		}
		return addr.String(), localAddress(addr), nil
	}
	bare := strings.TrimSuffix(name, ".")
	return name, bare == "localhost" || strings.HasSuffix(bare, ".localhost") || bare == "local" || strings.HasSuffix(bare, ".local"), nil
}

func localAddress(addr netip.Addr) bool {
	addr = addr.Unmap()
	return addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() || addr.IsUnspecified()
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func ipv4Number(s string) (uint64, error) {
	if s == "" {
		return 0, errHost
	}
	base := 10
	if strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X") {
		s, base = s[2:], 16
	} else if len(s) >= 2 && s[0] == '0' {
		s, base = s[1:], 8
	}
	if s == "" {
		return 0, nil
	}
	return strconv.ParseUint(s, base, 32)
}

func browserIPv4(parts []string) (netip.Addr, error) {
	if len(parts) > 4 {
		return netip.Addr{}, errHost
	}
	var value uint64
	for i, part := range parts {
		n, err := ipv4Number(part)
		if err != nil {
			return netip.Addr{}, errHost
		}
		if i == len(parts)-1 {
			if n >= uint64(1)<<(8*(5-len(parts))) {
				return netip.Addr{}, errHost
			}
			value += n
		} else {
			if n > 255 {
				return netip.Addr{}, errHost
			}
			value += n << (8 * (3 - i))
		}
	}
	return netip.AddrFrom4([4]byte{byte(value >> 24), byte(value >> 16), byte(value >> 8), byte(value)}), nil
}
