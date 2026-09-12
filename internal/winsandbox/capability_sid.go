package winsandbox

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
)

type capabilityPurpose string

const (
	capabilityWorkspace   capabilityPurpose = "workspace-write"
	capabilitySessionTemp capabilityPurpose = "session-temp-write"
	capabilityHashDomain                    = "reasonix/windows-write-capability/v1"
)

// deriveCapabilitySID returns an unforgeable-in-practice Windows capability
// identity for one canonical directory and purpose. The SID is authority-local
// (S-1-4) and gains power only where an ACL explicitly names it.
func deriveCapabilitySID(purpose capabilityPurpose, canonicalPath string) string {
	first, second := capabilitySubauthorities(purpose, canonicalPath)
	return fmt.Sprintf("S-1-4-%d-%d", first, second)
}

func capabilitySubauthorities(purpose capabilityPurpose, canonicalPath string) (uint32, uint32) {
	digest := sha256.Sum256([]byte(capabilityHashDomain + "\x00" + string(purpose) + "\x00" + canonicalPath))
	const modulus = uint32(1<<30 - 1)
	first := binary.LittleEndian.Uint32(digest[0:4])%modulus + 1
	second := binary.LittleEndian.Uint32(digest[4:8])%modulus + 1
	return first, second
}
