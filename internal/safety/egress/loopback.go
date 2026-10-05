package egress

import (
	"net"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

var proxyEnvKeys = []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"}

// LoopbackProxyPorts lists the loopback ports of the proxies this host is set
// to use, from the environment and the OS proxy settings. A command that can
// reach loopback could hand its traffic to one of them instead of to us.
func LoopbackProxyPorts(getenv func(string) string) []int {
	var hostports []string
	for _, k := range proxyEnvKeys {
		if v := strings.TrimSpace(getenv(k)); v != "" {
			hostports = append(hostports, proxyHostPort(v))
		}
	}
	hostports = append(hostports, systemProxies()...)
	var ports []int
	for _, hp := range hostports {
		host, portText, err := net.SplitHostPort(hp)
		if err != nil || !isLoopbackHost(host) {
			continue
		}
		if port, err := strconv.Atoi(portText); err == nil && port > 0 && !slices.Contains(ports, port) {
			ports = append(ports, port)
		}
	}
	slices.Sort(ports)
	return ports
}

// proxyHostPort reads host:port from a proxy variable, which may omit the
// scheme; a missing port is the scheme's default.
func proxyHostPort(v string) string {
	if !strings.Contains(v, "://") {
		v = "http://" + v
	}
	u, err := url.Parse(v)
	if err != nil {
		return ""
	}
	if u.Port() != "" {
		return u.Host
	}
	port := "80"
	if strings.HasPrefix(u.Scheme, "socks") {
		port = "1080"
	}
	return net.JoinHostPort(u.Hostname(), port)
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// parseScutilProxies reads the enabled proxies out of `scutil --proxy`, a
// dictionary of "<Kind>Enable", "<Kind>Proxy" and "<Kind>Port" keys.
func parseScutilProxies(out string) []string {
	keys := map[string]string{}
	for line := range strings.SplitSeq(out, "\n") {
		k, v, ok := strings.Cut(line, " : ")
		if ok {
			keys[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	var hostports []string
	for _, kind := range []string{"HTTP", "HTTPS", "SOCKS"} {
		if keys[kind+"Enable"] == "1" && keys[kind+"Proxy"] != "" && keys[kind+"Port"] != "" {
			hostports = append(hostports, net.JoinHostPort(keys[kind+"Proxy"], keys[kind+"Port"]))
		}
	}
	return hostports
}
