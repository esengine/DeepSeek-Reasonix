package sidecar

import (
	"fmt"
	"strings"

	"reasonix/internal/extension/protocol"
	"reasonix/internal/pluginpkg"
)

func (c *Client) initializeParams() protocol.InitializeParams {
	return protocol.InitializeParams{
		ProtocolVersion:         protocol.ProtocolVersion,
		ProtocolID:              protocol.ProtocolID,
		Manifest:                c.manifestExpectation(),
		Session:                 c.session,
		DependencySchemaVersion: protocol.DependencySchemaVersion,
		Capabilities: protocol.HostCapabilities{
			ContentRefs:             true,
			UIHost:                  c.uiHost,
			ProtocolVersion:         protocol.ProtocolVersion,
			DependencySchemaVersion: protocol.DependencySchemaVersion,
		},
	}
}

func (c *Client) manifestExpectation() protocol.ManifestExpectation {
	rt := c.rt
	expectation := protocol.ManifestExpectation{
		Intercepts:   append([]string(nil), rt.Intercepts...),
		Replaces:     append([]string(nil), rt.Replaces...),
		Capabilities: append([]string(nil), rt.Capabilities...),
		Requires:     requirementWires(c.requires),
		Provides:     capabilityWires(c.provides),
	}
	for _, provided := range c.provides {
		switch strings.ToLower(strings.TrimSpace(provided.Kind)) {
		case "provider":
			expectation.Providers = append(expectation.Providers, capabilityAddress(provided))
		case "uiaction":
			expectation.UIActions = append(expectation.UIActions, strings.TrimSpace(provided.ID))
		}
	}
	return expectation
}

func capabilityAddress(ref pluginpkg.CapabilityRef) string {
	return strings.TrimSuffix(strings.TrimSpace(ref.Namespace), "/") + "/" + strings.TrimPrefix(strings.TrimSpace(ref.ID), "/")
}

func capabilityWires(refs []pluginpkg.CapabilityRef) []protocol.CapabilityWire {
	out := make([]protocol.CapabilityWire, 0, len(refs))
	for _, ref := range refs {
		out = append(out, protocol.CapabilityWire{
			Namespace:  strings.TrimSpace(ref.Namespace),
			Kind:       strings.TrimSpace(ref.Kind),
			ID:         strings.TrimSpace(ref.ID),
			Version:    strings.TrimSpace(ref.Version),
			SchemaHash: strings.TrimSpace(ref.SchemaHash),
		})
	}
	return out
}

func requirementWires(refs []pluginpkg.CapabilityRef) []protocol.RequirementWire {
	out := make([]protocol.RequirementWire, 0, len(refs))
	for _, ref := range refs {
		out = append(out, protocol.RequirementWire{
			Namespace:    strings.TrimSpace(ref.Namespace),
			Kind:         strings.TrimSpace(ref.Kind),
			ID:           strings.TrimSpace(ref.ID),
			Version:      strings.TrimSpace(ref.Version),
			SchemaHash:   strings.TrimSpace(ref.SchemaHash),
			VersionRange: strings.TrimSpace(ref.VersionRange),
			Optional:     ref.Optional,
		})
	}
	return out
}

// validateProvidesCeiling rejects handshake Provides entries that are not a
// subset of the manifest capability ceiling (by namespace/kind/id).
func validateProvidesCeiling(manifest []pluginpkg.CapabilityRef, wire []protocol.CapabilityWire) error {
	if len(wire) == 0 {
		return nil
	}
	if len(manifest) == 0 {
		// No v2 provides list: fall back to runtime.capabilities string checks above.
		return nil
	}
	allowed := map[string]bool{}
	for _, p := range manifest {
		key := strings.TrimSpace(p.Namespace) + "/" + strings.TrimSpace(p.Kind) + "/" + strings.TrimSpace(p.ID)
		allowed[key] = true
	}
	for _, w := range wire {
		key := strings.TrimSpace(w.Namespace) + "/" + strings.TrimSpace(w.Kind) + "/" + strings.TrimSpace(w.ID)
		if !allowed[key] {
			return fmt.Errorf("handshake provides %q is outside the manifest provides ceiling", key)
		}
	}
	return nil
}
