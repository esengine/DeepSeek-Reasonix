package sidecar

import (
	"reasonix/internal/extension/protocol"
)

// NewProbeClient is a host-test seam: non-process Client with declared
// providers for stage/commit router-identity checks. Not a production path
// (use StartClient / StartPackagesWithPlan; real streams use process fakes).
func NewProbeClient(pluginID string, providers []protocol.ProviderDescriptor, router StreamRouter) *Client {
	if router == nil {
		router = dropStreamRouter{pluginID: pluginID}
	}
	return &Client{
		pluginID: pluginID,
		initResult: protocol.InitializeResult{
			ProtocolVersion:    protocol.ProtocolVersion,
			Name:               pluginID,
			Version:            "0.0.0-probe",
			Providers:          append([]protocol.ProviderDescriptor(nil), providers...),
			StateSchemaVersion: 0,
		},
		streams:     router,
		serveExited: make(chan struct{}),
		state:       handshakeReady,
	}
}

// StreamRouter returns the installed provider stream router (test identity only).
func (c *Client) StreamRouter() StreamRouter {
	if c == nil {
		return nil
	}
	return c.streamRouter()
}
