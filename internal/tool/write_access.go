package tool

import "encoding/json"

// WriteAccessDeclaration is the host-local write-directory request a tool
// exposes from its structured arguments. It is not part of the provider
// protocol.
type WriteAccessDeclaration struct {
	Directories   []string
	Justification string
	// RequestedPreset is a stable, tool-declared per-call escalation request.
	// The host remains authoritative and may reject it before execution.
	RequestedPreset string
	DenialID        string
}

// WriteAccessDeclarer is implemented by built-in tools that can name the local
// directories a call needs to write. MCP and custom tools do not implement it.
type WriteAccessDeclarer interface {
	DeclareWriteAccess(args json.RawMessage) (WriteAccessDeclaration, error)
}
