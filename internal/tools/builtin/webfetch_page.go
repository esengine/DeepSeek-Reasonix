package builtin

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"

	"reasonix/internal/base/netclient"
)

// ErrFetchInternalAddress is the refusal to reach an address on this machine's
// own network. It is a policy decision, not a site that failed to answer, so a
// caller never retries it somewhere else.
var ErrFetchInternalAddress = errors.New("refusing to fetch internal address")

// FetchPage is what a fetch found: the status the response carried, and the
// text the page reduced to. Text is empty when the body was.
type FetchPage struct {
	Status      int
	StatusText  string
	ContentType string
	Bytes       int
	Text        string
}

// Rendered is the page as web_fetch has always shown it to the model.
func (p FetchPage) Rendered() string {
	if p.Text == "" {
		return fmt.Sprintf("(empty body — status %s)", p.StatusText)
	}
	return fmt.Sprintf("status %s · %s · %d bytes\n\n", p.StatusText, contentTypeShort(p.ContentType), p.Bytes) + p.Text
}

// LocalFetcher is web_fetch's own fetch, from this machine, behind the SSRF
// guard.
type LocalFetcher struct{ fetcher webFetch }

func NewLocalFetcher(proxy netclient.ProxySpec) LocalFetcher {
	return LocalFetcher{fetcher: webFetch{proxySpec: proxy}}
}

func (f LocalFetcher) Fetch(ctx context.Context, rawURL string) (FetchPage, error) {
	return f.fetcher.fetch(ctx, rawURL)
}

// VetFetchURL decides whether a URL may be handed to a third party to fetch.
// Nothing that names this machine's own network leaves it: internal addresses,
// names that cannot be public by construction, and names that resolve to either.
// A public-shaped name this machine cannot resolve passes, since reaching what
// it cannot is why a remote fetch is asked at all.
func VetFetchURL(ctx context.Context, rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil {
		return errors.New("url must be an absolute http(s) address without credentials")
	}
	host := u.Hostname()
	if ip := net.ParseIP(host); ip == nil && !publicByConstruction(host) {
		return fmt.Errorf("%w %s", ErrFetchInternalAddress, host)
	}
	if ip := net.ParseIP(host); ip != nil {
		if internalForRemote(ip) {
			return fmt.Errorf("%w %s", ErrFetchInternalAddress, host)
		}
		return nil
	}
	addrs, err := lookupFetchHost(ctx, host)
	if err != nil {
		return nil
	}
	for _, a := range addrs {
		if internalForRemote(a.IP) {
			return fmt.Errorf("%w %s (resolves to %s)", ErrFetchInternalAddress, host, a.IP)
		}
	}
	return nil
}

func internalForRemote(ip net.IP) bool {
	if v4 := netclient.EmbeddedIPv4(ip); v4 != nil && v4.IsLoopback() {
		return true
	}
	return ip.IsLoopback() || blockedFetchIP(ip)
}

// nonPublicSuffixes are names the standards keep out of the public DNS:
// localhost, invalid, test, example (RFC 6761), local (6762), onion (7686),
// home.arpa (8375), internal (ICANN private use), and lan, the home-router
// suffix that is never delegated. Under one, an off-VPN host resolves nowhere.
var nonPublicSuffixes = []string{"localhost", "local", "internal", "lan", "home.arpa", "invalid", "test", "example", "onion"}

// publicByConstruction reports whether a hostname's shape allows it to be a
// public name: more than one label, and not under a special-use suffix.
func publicByConstruction(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	if !strings.Contains(host, ".") {
		return false
	}
	for _, suffix := range nonPublicSuffixes {
		if host == suffix || strings.HasSuffix(host, "."+suffix) {
			return false
		}
	}
	return true
}
