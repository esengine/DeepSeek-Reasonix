// Freshness classification for saved facts: how a fact ages (volatility or
// the legacy type default), when it hard-expires, and what renews its clock.
package memory

import (
	"strings"
	"time"
)

const (
	FreshnessFresh   = "fresh"
	FreshnessCurrent = "current"
	FreshnessStale   = "stale"
	FreshnessExpired = "expired" // past an explicit expires_at; excluded from automatic recall
)

// Volatility is how fast a fact ages, orthogonal to Type: a project fact may
// be a release branch that dies in days or a README location that holds for
// years. Unset falls back to the legacy type-based windows.
type Volatility string

const (
	VolatilityEvergreen Volatility = "evergreen" // never ages
	VolatilityStable    Volatility = "stable"    // 90 days fresh / 365 current
	VolatilityVolatile  Volatility = "volatile"  // 7 days fresh / 30 current
)

// NormalizeVolatility validates a persisted or requested volatility. Empty and
// unknown values return "" (unset) so the type-based default applies.
func NormalizeVolatility(s string) Volatility {
	switch Volatility(strings.ToLower(strings.TrimSpace(s))) {
	case VolatilityEvergreen:
		return VolatilityEvergreen
	case VolatilityStable:
		return VolatilityStable
	case VolatilityVolatile:
		return VolatilityVolatile
	}
	return ""
}

// freshnessWindows resolves the (fresh, current) aging windows: an explicit
// volatility is self-describing and wins; unset falls back to the type
// defaults that predate the field.
func freshnessWindows(m Memory) (fresh, current time.Duration, evergreen bool) {
	switch NormalizeVolatility(string(m.Volatility)) {
	case VolatilityEvergreen:
		return 0, 0, true
	case VolatilityStable:
		return 90 * 24 * time.Hour, 365 * 24 * time.Hour, false
	case VolatilityVolatile:
		return 7 * 24 * time.Hour, 30 * 24 * time.Hour, false
	}
	switch NormalizeType(string(m.Type)) {
	case TypeReference:
		return 14 * 24 * time.Hour, 45 * 24 * time.Hour, false
	case TypeUser, TypeFeedback:
		return 90 * 24 * time.Hour, 365 * 24 * time.Hour, false
	default:
		return 30 * 24 * time.Hour, 180 * 24 * time.Hour, false
	}
}

// FreshnessFor exposes the freshness classification shared by automatic
// recall, /memory, and diagnostic surfaces. An explicit expiry is a hard
// boundary; otherwise the clock runs from the last content change or the last
// explicit verification, whichever is newer.
func FreshnessFor(fact Memory, now time.Time) string {
	return memoryFreshness(fact, now)
}

func memoryFreshness(m Memory, now time.Time) string {
	if !m.ExpiresAt.IsZero() && now.After(m.ExpiresAt) {
		return FreshnessExpired
	}
	updated := m.UpdatedAt
	if updated.IsZero() {
		updated = m.CreatedAt
	}
	if m.LastVerifiedAt.After(updated) {
		updated = m.LastVerifiedAt
	}
	if updated.IsZero() || updated.After(now) {
		return FreshnessCurrent
	}
	fresh, current, evergreen := freshnessWindows(m)
	if evergreen {
		return FreshnessFresh
	}
	age := now.Sub(updated)
	if age <= fresh {
		return FreshnessFresh
	}
	if age <= current {
		return FreshnessCurrent
	}
	return FreshnessStale
}
