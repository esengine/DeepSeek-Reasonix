package browserhost

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"reasonix/internal/extensioncontract"
)

// Capability identity for the host-owned browser companion API.
const (
	CapabilityNamespace = "reasonix"
	CapabilityKind      = "browser"
	CapabilityID        = "companion"
	CapabilityVersion   = "1.0.0"
)

// schemaSeed is the stable DTO fingerprint for the five browser RPCs. It is
// intentionally independent of the full Extension Protocol schema hash so
// unrelated protocol additions never force browser plugins to reload.
const schemaSeed = `browser-capability-v1:
host/browser/tab/list:BrowserTabListParams->BrowserTabListResult
host/browser/tab/open:BrowserTabOpenParams->BrowserTabOpenResult
host/browser/tab/snapshot:BrowserTabSnapshotParams->BrowserTabSnapshotResult
host/browser/tab/wait:BrowserTabWaitParams->BrowserTabWaitResult
host/browser/tab/act:BrowserTabActParams->BrowserTabActResult
BrowserTab:{tabId,url,title,active,generation}
`

// SchemaHash is the published capability schema hash for reasonix/browser/companion.
func SchemaHash() string {
	sum := sha256.Sum256([]byte(schemaSeed))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Capability returns the synthetic host capability advertised when a frontend
// supplies a non-nil BrowserHost. Companion install/ready state does not change
// this capability — only API support does.
func Capability() extensioncontract.Capability {
	return extensioncontract.Capability{
		Key: extensioncontract.CapabilityKey{
			Namespace: CapabilityNamespace,
			Kind:      CapabilityKind,
			ID:        CapabilityID,
		},
		Version:    CapabilityVersion,
		SchemaHash: SchemaHash(),
	}
}

// CapabilityJSON is the wire form used in docs and doctor.
func CapabilityJSON() string {
	raw, err := json.Marshal(Capability())
	if err != nil {
		return ""
	}
	return string(raw)
}
