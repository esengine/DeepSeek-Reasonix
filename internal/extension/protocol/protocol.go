// Package protocol is the frozen Extension Protocol v2 wire contract between
// the Reasonix host and out-of-process extension sidecars. It is a public
// protocol: sidecars are written against this package's generated JSON Schema
// and its compatibility hash, not against Reasonix internals.
//
// Stability contract: within major version 2, only optional fields, new enum
// values, and new methods may be added. Existing required fields, method
// names, directions, limits, error reasons, and semantics never change. Any
// such change requires a new major protocol version.
//
// The sidecar process is the "extension" peer; Reasonix is the "host" peer.
// Directions are named from the host's point of view: host_to_extension_*
// flows from Reasonix to the sidecar, extension_to_host_* flows back.
package protocol

import (
	"fmt"
	"strconv"
)

// ProtocolID is the immutable identity string peers exchange during the
// initialize handshake. It is also the generated schema document's $id.
const ProtocolID = "reasonix.extension.v2"

// ProtocolMajor is the frozen major version of this protocol build.
const ProtocolMajor = 2

// ProtocolVersion is the wire string form of ProtocolMajor carried in the
// initialize handshake.
const ProtocolVersion = "2"

// NoResult is the result placeholder for notifications, which carry no
// response payload.
type NoResult struct{}

// CompareProtocolVersion validates a peer's handshake identity for the
// extension protocol: the protocol ID must match ProtocolID exactly and the
// peer's major version must equal ProtocolMajor. Mismatches return the frozen
// unsupported_version or protocol_error *ProtocolError so transports can
// answer the handshake with a structured error instead of an ad-hoc string.
func CompareProtocolVersion(peerID, peerVersion string) error {
	if peerID != ProtocolID {
		return MustProtocolError(ErrUnsupportedVersion)
	}
	major, err := strconv.Atoi(peerVersion)
	if err != nil {
		return MustProtocolError(ErrProtocolError)
	}
	if major != ProtocolMajor {
		return MustProtocolError(ErrUnsupportedVersion)
	}
	return nil
}

// HandshakeIdentity is the SchemaHash-bearing identity line a peer may log or
// compare after a successful CompareProtocolVersion check. Two peers with
// equal schema hashes run byte-identical contracts.
func HandshakeIdentity() string {
	return fmt.Sprintf("%s major=%d schema=%s", ProtocolID, ProtocolMajor, SchemaHash())
}
