package plugin

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// HostProfile is the semantic identity of a frontend's MCP client surface:
// which optional client capabilities every server connection declares. It is
// fixed when the Host is created and never changes for the Host's lifetime —
// capabilities are negotiated per connection at initialize time, so a live
// downgrade would require tearing every session down. Cache identity and the
// capability matrix both derive from the profile, never from SDK versions,
// wall-clock time, or negotiated results.
type HostProfile string

const (
	// HostProfileCore is the headless surface: bots and print-mode CLI. It
	// declares no optional interaction capabilities, matching the legacy
	// client byte-for-byte, so it keeps the v2 cache contract.
	HostProfileCore HostProfile = "core-v1"
	// HostProfileInteractive adds form and URL elicitation for frontends with
	// a human on the other end: the chat TUI and serve.
	HostProfileInteractive HostProfile = "interactive-v1"
	// HostProfileDesktopApps is the Desktop surface: elicitation plus the
	// stable MCP Apps 2026-01-26 ui extension.
	HostProfileDesktopApps HostProfile = "desktop-apps-2026-01-26-v1"
)

// AppsUIExtensionID is the client extension identifier for MCP Apps
// (ext-apps 2026-01-26). Declaring it tells servers this host can render
// text/html;profile=mcp-app resources inline.
const AppsUIExtensionID = "io.modelcontextprotocol/ui"

// AppsMimeType is the single MIME type the Desktop host accepts for app
// resources, matching the stable Apps specification.
const AppsMimeType = "text/html;profile=mcp-app"

// ProfileCapabilities is the set of optional client capabilities a profile
// declares. The JSON encoding is the profile's cache identity: two profiles
// that declare identical capabilities must share one cache identity, and any
// capability change must change the identity so stale tool catalogs written
// under the old negotiation can never be read under the new one.
type ProfileCapabilities struct {
	ElicitationForms bool `json:"elicitationForms,omitempty"`
	ElicitationURL   bool `json:"elicitationURL,omitempty"`
	AppsUI           bool `json:"appsUI,omitempty"`
}

// HostProfileForInteractive maps a human-in-the-loop flag onto the profile:
// interactive-v1 when a human can answer prompts, core-v1 otherwise.
func HostProfileForInteractive(interactive bool) HostProfile {
	if interactive {
		return HostProfileInteractive
	}
	return HostProfileCore
}

// String returns the wire form of the profile identifier.
func (p HostProfile) String() string { return string(p.Normalize()) }

// Capabilities returns what the profile declares to every server.
func (p HostProfile) Capabilities() ProfileCapabilities {
	switch p {
	case HostProfileInteractive:
		return ProfileCapabilities{ElicitationForms: true, ElicitationURL: true}
	case HostProfileDesktopApps:
		return ProfileCapabilities{ElicitationForms: true, ElicitationURL: true, AppsUI: true}
	default:
		return ProfileCapabilities{}
	}
}

// Normalize maps an unknown or empty profile onto core-v1 so a config typo can
// never silently widen the declared surface.
func (p HostProfile) Normalize() HostProfile {
	switch p {
	case HostProfileInteractive, HostProfileDesktopApps:
		return p
	default:
		return HostProfileCore
	}
}

// UsesEnhancedCache reports whether the profile declares anything beyond the
// legacy client surface. Such profiles cannot reuse the v2 cache: a server may
// return a different tools/list once it sees elicitation or ui extensions, so
// they read and write an isolated v3 cache instead.
func (p HostProfile) UsesEnhancedCache() bool {
	return p.Normalize() != HostProfileCore
}

// ProfileCacheHash is a stable, short digest of the declared capabilities.
// It appears in the enhanced cache filename (<slug>.host-<hash>.json) so
// caches written under different profiles never collide.
func (p HostProfile) ProfileCacheHash() string {
	caps := p.Capabilities()
	b, err := json.Marshal(caps)
	if err != nil {
		// ProfileCapabilities is three booleans; marshal cannot fail.
		panic(fmt.Sprintf("plugin: marshal profile capabilities: %v", err))
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:6])
}

// HostProfileOrder is the display order of the capability matrix layers.
var HostProfileOrder = []HostProfile{HostProfileCore, HostProfileInteractive, HostProfileDesktopApps}

// ProfileDisplayNames maps profiles to human labels for status surfaces.
func (p HostProfile) DisplayName() string {
	switch p {
	case HostProfileInteractive:
		return "Interactive Host"
	case HostProfileDesktopApps:
		return "Apps Host"
	default:
		return "Core Host"
	}
}
