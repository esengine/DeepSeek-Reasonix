package builtin

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"reasonix/internal/base/netclient"
)

func TestBlockedFetchIP(t *testing.T) {
	blocked := []string{
		"169.254.169.254",      // cloud metadata (link-local)
		"10.1.2.3",             // RFC1918
		"172.16.5.6",           // RFC1918
		"192.168.1.1",          // RFC1918
		"0.0.0.0",              // unspecified
		"fe80::1",              // IPv6 link-local
		"fc00::1",              // IPv6 unique-local
		"::ffff:10.0.0.1",      // IPv4-mapped private
		"100.100.100.200",      // Alibaba Cloud metadata (CGNAT)
		"100.64.0.1",           // RFC 6598 shared space
		"::ffff:100.100.100.1", // IPv4-mapped CGNAT
	}
	for _, s := range blocked {
		if !blockedFetchIP(net.ParseIP(s)) {
			t.Errorf("%s should be blocked", s)
		}
	}
	allowed := []string{"8.8.8.8", "1.1.1.1", "127.0.0.1", "::1", "93.184.216.34"}
	for _, s := range allowed {
		if blockedFetchIP(net.ParseIP(s)) {
			t.Errorf("%s should be allowed", s)
		}
	}
}

// TestWebFetchAllowsLoopback proves the guard doesn't break normal fetches: a
// loopback dev server (httptest binds 127.0.0.1) stays reachable.
func TestWebFetchAllowsLoopback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("hello from localhost"))
	}))
	defer srv.Close()

	args, _ := json.Marshal(map[string]any{"url": srv.URL})
	out, err := (webFetch{proxySpec: netclient.ProxySpec{Mode: netclient.ModeOff}}).Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("loopback fetch should succeed, got %v", err)
	}
	if !strings.Contains(out, "hello from localhost") {
		t.Fatalf("body missing: %q", out)
	}
}

// TestWebFetchRefusesLinkLocal proves a fetch aimed at the cloud-metadata
// endpoint is refused at dial time (no packet leaves the host).
func TestWebFetchRefusesLinkLocal(t *testing.T) {
	args, _ := json.Marshal(map[string]any{"url": "http://169.254.169.254/latest/meta-data/"})
	_, err := (webFetch{proxySpec: netclient.ProxySpec{Mode: netclient.ModeOff}}).Execute(context.Background(), args)
	if err == nil {
		t.Fatal("fetch to 169.254.169.254 should be refused")
	}
	if !strings.Contains(err.Error(), "169.254.169.254") {
		t.Fatalf("error should name the refused address, got %v", err)
	}
}

func stubFetchLookup(t *testing.T, addrs ...string) {
	t.Helper()
	prev := lookupFetchHost
	t.Cleanup(func() { lookupFetchHost = prev })
	lookupFetchHost = func(context.Context, string) ([]net.IPAddr, error) {
		out := make([]net.IPAddr, 0, len(addrs))
		for _, a := range addrs {
			out = append(out, net.IPAddr{IP: net.ParseIP(a)})
		}
		return out, nil
	}
}

// A fake-ip resolver (Clash, mihomo) answers a public host with a unique-local
// AAAA beside a routable A. The blocked address is skipped, not fatal.
func TestWebFetchDialsAllowedAddressBesideBlockedOne(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("reached through the allowed address"))
	}))
	defer srv.Close()
	_, port, _ := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	stubFetchLookup(t, "fdfe:dcba:9876::152", "127.0.0.1")

	args, _ := json.Marshal(map[string]any{"url": "http://api-docs.example:" + port + "/"})
	out, err := (webFetch{proxySpec: netclient.ProxySpec{Mode: netclient.ModeOff}}).Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("fetch should use the allowed address, got %v", err)
	}
	if !strings.Contains(out, "reached through the allowed address") {
		t.Fatalf("body missing: %q", out)
	}
}

func TestWebFetchRefusesHostWithOnlyBlockedAddresses(t *testing.T) {
	stubFetchLookup(t, "fdfe:dcba:9876::152", "10.0.0.7")
	args, _ := json.Marshal(map[string]any{"url": "http://internal.example/"})
	_, err := (webFetch{proxySpec: netclient.ProxySpec{Mode: netclient.ModeOff}}).Execute(context.Background(), args)
	if err == nil || !strings.Contains(err.Error(), "refusing to fetch internal address") {
		t.Fatalf("want SSRF refusal, got %v", err)
	}
}

func TestFetchGuardJudgesTheIPv4ATransitionAddressCarries(t *testing.T) {
	for _, in := range []string{"64:ff9b::a9fe:a9fe", "2002:a9fe:a9fe::1", "64:ff9b::a00:1", "64:ff9b::7f00:1", "2002:7f00:1::"} {
		if !internalForRemote(net.ParseIP(in)) {
			t.Errorf("%s reaches an internal address through a transition form but was let through", in)
		}
	}
	if internalForRemote(net.ParseIP("64:ff9b::808:808")) {
		t.Error("a transition form carrying a public address must stay reachable")
	}
}
