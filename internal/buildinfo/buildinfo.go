// Package buildinfo exposes the running build's identity so outbound HTTP
// requests can be attributed to Reasonix. The version is injected at build time
// via -ldflags "-X main.version=..." and handed to SetVersion once at startup;
// providers read UserAgent() when constructing requests.
package buildinfo

import (
	"strings"
	"sync/atomic"
)

// devVersion is the placeholder for source/test builds that carry no injected
// version (the same default as cmd/reasonix/main.go's `version` var).
const devVersion = "dev"

// version holds the running build's version. Stored atomically because it is
// set once at startup but read from every provider request goroutine.
var version atomic.Value

// SetVersion records the running build's version. An empty value (source builds,
// tests that never set it) is normalized to "dev". Intended to be called once,
// early in startup, before any request is issued.
func SetVersion(v string) {
	v = strings.TrimSpace(v)
	if v == "" {
		v = devVersion
	}
	version.Store(v)
}

// Version returns the running build's version, or "dev" if none was set.
func Version() string {
	if v, ok := version.Load().(string); ok && v != "" {
		return v
	}
	return devVersion
}

// UserAgent returns the identifying User-Agent for outbound Reasonix requests,
// e.g. "Reasonix/v1.2.3" (or "Reasonix/dev" for source builds).
func UserAgent() string {
	return "Reasonix/" + Version()
}
