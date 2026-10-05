package plugin

import (
	"errors"
	"fmt"
	"slices"
)

// supportedProtocolVersions are the MCP revisions this client can speak, newest
// first. The first is what initialize offers; a server may answer with any of
// them, and the session then runs on the one it chose.
var supportedProtocolVersions = []string{protocolVersion, "2025-06-18", "2025-03-26", "2024-11-05"}

// ErrUnsupportedProtocolVersion is a server answering initialize with a
// revision this client does not implement. The spec has the client disconnect
// rather than guess at a protocol it cannot read.
var ErrUnsupportedProtocolVersion = errors.New("MCP server chose a protocol version this client does not support")

// negotiatedVersion reads the server's initialize answer. An empty answer is a
// server older than the field, and runs on the oldest revision.
func negotiatedVersion(reply string) (string, error) {
	if reply == "" {
		return supportedProtocolVersions[len(supportedProtocolVersions)-1], nil
	}
	if slices.Contains(supportedProtocolVersions, reply) {
		return reply, nil
	}
	return "", fmt.Errorf("%w: %q", ErrUnsupportedProtocolVersion, reply)
}

// protocolVersioned is a transport that states the negotiated revision on each
// request: Streamable HTTP's MCP-Protocol-Version header. A server that gets
// no header assumes 2025-03-26, which is not what a newer session agreed.
type protocolVersioned interface {
	setProtocolVersion(version string)
}
