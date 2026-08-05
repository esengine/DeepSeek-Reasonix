package protocol

import "encoding/json"

// Lifecycle DTOs: initialize handshake, initialized notification, graceful
// shutdown, event observation, and resource change notification.

// InitializeParams is the host's opening handshake. The sidecar must answer
// with InitializeResult before any other method runs.
type InitializeParams struct {
	ProtocolVersion string              `json:"protocolVersion" validate:"nonempty"`
	ProtocolID      string              `json:"protocolId" validate:"nonempty"`
	Manifest        ManifestExpectation `json:"manifest"`
	Session         SessionContext      `json:"session"`
	Capabilities    HostCapabilities    `json:"capabilities"`
}

// ManifestExpectation is what the host will accept from this extension,
// derived from its installed manifest. The sidecar must not use anything
// outside this set; doing so fails with capability_not_declared.
type ManifestExpectation struct {
	Intercepts   []string `json:"intercepts,omitempty"`
	Replaces     []string `json:"replaces,omitempty"`
	Providers    []string `json:"providers,omitempty"`
	UIActions    []string `json:"uiActions,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
}

// SessionContext identifies the session the extension serves.
type SessionContext struct {
	SessionID     string `json:"sessionId" validate:"nonempty"`
	WorkspaceRoot string `json:"workspaceRoot" validate:"nonempty"`
	Generation    uint64 `json:"generation"`
}

// HostCapabilities tells the sidecar what this host supports.
type HostCapabilities struct {
	ContentRefs     bool       `json:"contentRefs"`
	UIHost          UIHostKind `json:"uiHost"`
	ProtocolVersion string     `json:"protocolVersion" validate:"nonempty"`
}

// InitializeResult is the sidecar's handshake answer: its identity plus the
// contributions it actually activated for this session.
type InitializeResult struct {
	ProtocolVersion    string               `json:"protocolVersion" validate:"nonempty"`
	Name               string               `json:"name" validate:"nonempty"`
	Version            string               `json:"version" validate:"nonempty"`
	Subscriptions      []string             `json:"subscriptions,omitempty"`
	Replaces           []string             `json:"replaces,omitempty"`
	Providers          []ProviderDescriptor `json:"providers,omitempty"`
	UIActions          []UIActionDecl       `json:"uiActions,omitempty"`
	StateSchemaVersion int                  `json:"stateSchemaVersion" validate:"min=0"`
}

// InitializedParams carries no payload; the notification only signals the
// sidecar may start receiving intercepts and events.
type InitializedParams struct{}

// ShutdownParams requests a graceful stop. The sidecar must answer within
// TimeoutMillis or the host reports shutdown_timeout and kills the process.
type ShutdownParams struct {
	TimeoutMillis int `json:"timeoutMillis" validate:"min=0"`
}

// ShutdownResult acknowledges the shutdown request.
type ShutdownResult struct {
	Accepted bool `json:"accepted"`
}

// EventParams is the fire-and-forget observation of one of the 17 hook
// points. Unlike extension/intercept, the extension's answer (if any) is
// discarded and cannot change host behavior. When Payload exceeds
// ExternalizeFieldBytes it travels as null and Externalized carries its
// content-ref descriptor.
type EventParams struct {
	Event        InterceptEvent      `json:"event"`
	Payload      json.RawMessage     `json:"payload" externalizable:"true"`
	Externalized []ExternalizedField `json:"externalized,omitempty"`
}

// ResourcesChangedParams notifies the extension that watched resources
// (skills, commands, prompts, themes, …) changed on disk.
type ResourcesChangedParams struct {
	Paths []string `json:"paths"`
}
