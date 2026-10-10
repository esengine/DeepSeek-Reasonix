// Package redirectguard decides which redirects a download may follow.
//
// A client that authenticates and then follows a redirect anywhere is how an
// artifact arrives from a host nobody vouched for. Three downloads needed that
// judgement and grew three copies of it, one of which was never wired to the
// client it was written for; this is the one they share.
package redirectguard

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// ErrRefused identifies a redirect that was not followed, so a caller can tell
// one from a transport failure. The message names the rule that stopped it and
// is for whoever reads the log.
var ErrRefused error = refused{}

// refused is a failure no retry can fix: the same request goes to the same place.
type refused struct{}

func (refused) Error() string   { return "redirect refused" }
func (refused) Permanent() bool { return true }

// OriginLeft is the refusal of a redirect to another host. From and To are
// host[:port] only, so the message is safe to show and carries no path or query.
type OriginLeft struct{ From, To string }

func (e *OriginLeft) Error() string {
	return fmt.Sprintf("redirect refused: %s redirected to %s; if you trust %s, set the address to %s", e.From, e.To, e.To, e.To)
}

func (e *OriginLeft) Unwrap() error { return ErrRefused }

// maxHops is Go's own default, restated because a CheckRedirect replaces that
// default outright: without a limit here a chain never stops.
const maxHops = 10

// Follow returns a CheckRedirect that follows a redirect only while it stays
// HTTPS, carries no credentials and no port, and lands on one of hosts. A host
// is an exact name, or a ".suffix" matching any name beneath it.
func Follow(hosts ...string) func(*http.Request, []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if len(via) >= maxHops {
			return fmt.Errorf("%w: stopped after %d hops", ErrRefused, maxHops)
		}
		if req == nil || req.URL == nil {
			return fmt.Errorf("%w: no target URL", ErrRefused)
		}
		target := req.URL
		if !strings.EqualFold(target.Scheme, "https") {
			return fmt.Errorf("%w: not HTTPS: %q", ErrRefused, target.String())
		}
		if target.User != nil {
			return fmt.Errorf("%w: carries credentials: %q", ErrRefused, target.String())
		}
		if target.Hostname() == "" {
			return fmt.Errorf("%w: no hostname: %q", ErrRefused, target.String())
		}
		// A port is refused rather than matched: release hosts answer on 443,
		// and an allowed name on another port is a different endpoint.
		if target.Port() != "" || !permitted(target.Hostname(), hosts) {
			return fmt.Errorf("%w: untrusted host %q", ErrRefused, target.Host)
		}
		return nil
	}
}

// StayOnOrigin returns a CheckRedirect for a request to an address the user
// chose, which may be HTTP or carry a port. A redirect is followed only on the
// host the chain started on (same port, or the default-port HTTP to HTTPS
// upgrade), never downgrades HTTPS and carries no credentials.
func StayOnOrigin() func(*http.Request, []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if len(via) >= maxHops {
			return fmt.Errorf("%w: stopped after %d hops", ErrRefused, maxHops)
		}
		if req == nil || req.URL == nil || len(via) == 0 || via[0].URL == nil {
			return fmt.Errorf("%w: no origin to stay on", ErrRefused)
		}
		origin, target := via[0].URL, req.URL
		if target.User != nil {
			return fmt.Errorf("%w: carries credentials: %q", ErrRefused, target.String())
		}
		if strings.EqualFold(origin.Scheme, "https") && !strings.EqualFold(target.Scheme, "https") {
			return fmt.Errorf("%w: downgrades HTTPS: %q", ErrRefused, target.String())
		}
		if !sameOrigin(origin, target) {
			return &OriginLeft{From: origin.Host, To: target.Host}
		}
		return nil
	}
}

func sameOrigin(origin, target *url.URL) bool {
	if !strings.EqualFold(strings.TrimSuffix(origin.Hostname(), "."), strings.TrimSuffix(target.Hostname(), ".")) {
		return false
	}
	from, to := effectivePort(origin), effectivePort(target)
	return from == to || (from == "80" && to == "443" && !strings.EqualFold(origin.Scheme, "https"))
}

func effectivePort(u *url.URL) string {
	if p := u.Port(); p != "" {
		return p
	}
	if strings.EqualFold(u.Scheme, "https") {
		return "443"
	}
	return "80"
}

// permitted compares the way DNS does: case-insensitively, and with the root
// label a resolver accepts but a string comparison would not.
func permitted(host string, hosts []string) bool {
	host = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	if host == "" {
		return false
	}
	for _, allowed := range hosts {
		allowed = strings.ToLower(allowed)
		if strings.HasPrefix(allowed, ".") {
			if strings.HasSuffix(host, allowed) {
				return true
			}
			continue
		}
		if host == allowed {
			return true
		}
	}
	return false
}
