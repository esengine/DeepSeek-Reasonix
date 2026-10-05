package provider

import (
	"net/http"
	"strings"
	"sync/atomic"
)

var clientVersion atomic.Pointer[string]

// SetClientVersion records the running build's version for ClientUserAgent.
// Hosts call it once at start; "" reverts to "dev".
func SetClientVersion(v string) {
	v = strings.TrimSpace(v)
	clientVersion.Store(&v)
}

// ClientVersion is the version SetClientVersion recorded, or "dev".
func ClientVersion() string {
	if p := clientVersion.Load(); p != nil && *p != "" {
		return *p
	}
	return "dev"
}

// ClientUserAgent is the identity a model request presents to the endpoint and
// any gateway in front of it: Reasonix/<version>.
func ClientUserAgent() string {
	return "Reasonix/" + ClientVersion()
}

// ApplyClientIdentity sets ClientUserAgent unless a User-Agent is already set,
// so a configured custom header or an endpoint-specific identity wins.
func ApplyClientIdentity(req *http.Request) {
	if req != nil && req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", ClientUserAgent())
	}
}
