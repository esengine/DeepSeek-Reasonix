// Package redirectguard decides which redirects a download may follow.
//
// A client that authenticates and then follows a redirect anywhere is how an
// artifact arrives from a host nobody vouched for. Three downloads needed that
// judgement and grew three copies of it, one of which was never wired to the
// client it was written for; this is the one they share.
package redirectguard

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// ErrRefused identifies a redirect that was not followed, so a caller can tell
// one from a transport failure. The message names the rule that stopped it and
// is for whoever reads the log.
var ErrRefused = errors.New("redirect refused")

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
