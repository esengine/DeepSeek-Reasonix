package control

import (
	"fmt"
	"runtime"
	"slices"
	"strings"

	"reasonix/internal/agent"
	"reasonix/internal/permissionpreset"
	"reasonix/internal/sandbox"
)

// SessionGrantSummary is a transport-safe description of an in-memory grant.
// Grants remain session-local and are never converted to persistent rules.
type SessionGrantSummary struct {
	Scope  string `json:"scope"`
	Target string `json:"target"`
}

type PermissionCapabilities struct {
	Backend           string   `json:"backend"`
	Enforcement       string   `json:"enforcement"`
	SupportedPresets  []string `json:"supportedPresets"`
	UnavailableReason string   `json:"unavailableReason,omitempty"`
	WriteIsolation    string   `json:"writeIsolation,omitempty"`
	ReadIsolation     string   `json:"readIsolation,omitempty"`
	NetworkIsolation  string   `json:"networkIsolation,omitempty"`
}

// PermissionSnapshot is the sole user-facing permission state for a session.
type PermissionSnapshot struct {
	SessionID     string                 `json:"sessionId"`
	Generation    uint64                 `json:"generation"`
	Revision      uint64                 `json:"revision"`
	Preset        string                 `json:"preset"`
	WorkspaceRoot string                 `json:"workspaceRoot"`
	Grants        []SessionGrantSummary  `json:"grants"`
	Capabilities  PermissionCapabilities `json:"capabilities"`
}

func platformPermissionCapabilities() PermissionCapabilities {
	return permissionCapabilitiesForPlatform(runtime.GOOS, sandbox.Available(), sandbox.UnavailableMessage())
}

func permissionCapabilitiesForPlatform(goos string, available bool, unavailableReason string) PermissionCapabilities {
	backend := "none"
	writeIsolation := ""
	readIsolation := ""
	networkIsolation := ""
	switch goos {
	case "darwin":
		backend = "seatbelt"
		writeIsolation = "seatbelt-filesystem"
		readIsolation = "seatbelt-filesystem"
		networkIsolation = "seatbelt-network"
	case "linux":
		backend = "bubblewrap"
		writeIsolation = "bubblewrap-mount-namespace"
		readIsolation = "bubblewrap-mount-namespace"
		networkIsolation = "bubblewrap-network-namespace"
	case "windows":
		backend = "windows-write-restricted+appcontainer"
		writeIsolation = "write-restricted-capability-sid"
		readIsolation = "forbid-read+appcontainer-direct-tools"
		networkIsolation = "appcontainer-direct-tools-only"
	}
	if available {
		enforcement := "full"
		if goos == "windows" {
			enforcement = "partial"
		}
		return PermissionCapabilities{
			Backend: backend, Enforcement: enforcement,
			SupportedPresets: []string{string(permissionpreset.ReadOnly), string(permissionpreset.WorkspaceWrite), string(permissionpreset.DangerFullAccess)},
			WriteIsolation:   writeIsolation, ReadIsolation: readIsolation, NetworkIsolation: networkIsolation,
		}
	}
	return PermissionCapabilities{
		Backend: backend, Enforcement: "unavailable",
		SupportedPresets:  []string{string(permissionpreset.DangerFullAccess)},
		UnavailableReason: unavailableReason,
	}
}

func (c *Controller) PermissionSnapshot() PermissionSnapshot {
	auth := c.SessionAuthorizations()
	grants := make([]SessionGrantSummary, 0, len(auth.Grants)+len(auth.WriteRoots)+len(auth.PlanModeReadOnlyCommands))
	for _, target := range auth.Grants {
		grants = append(grants, SessionGrantSummary{Scope: "tool", Target: target})
	}
	for _, target := range auth.WriteRoots {
		grants = append(grants, SessionGrantSummary{Scope: "directory", Target: target})
	}
	for _, target := range auth.PlanModeReadOnlyCommands {
		grants = append(grants, SessionGrantSummary{Scope: "command-prefix", Target: target})
	}
	return PermissionSnapshot{
		SessionID: agent.BranchID(c.SessionPath()), Generation: c.runtimeGeneration,
		Revision: c.permissionRevision.Load(), Preset: c.ToolApprovalMode(),
		WorkspaceRoot: strings.TrimSpace(c.workspaceRoot), Grants: grants,
		Capabilities: platformPermissionCapabilities(),
	}
}

// SetPermissionPreset applies a compare-and-set update. A stale UI or remote
// reply cannot mutate a newer permission generation.
func (c *Controller) SetPermissionPreset(preset string, expectedRevision uint64) (PermissionSnapshot, []string, error) {
	c.permissionMu.Lock()
	defer c.permissionMu.Unlock()
	current := c.permissionRevision.Load()
	if expectedRevision != current {
		return c.PermissionSnapshot(), nil, fmt.Errorf("permission revision changed: have %d, expected %d", current, expectedRevision)
	}
	raw := strings.ToLower(strings.TrimSpace(preset))
	if !permissionpreset.Valid(raw) {
		return c.PermissionSnapshot(), nil, fmt.Errorf("permission preset must be read-only, workspace-write, or danger-full-access")
	}
	capabilities := platformPermissionCapabilities()
	if !slices.Contains(capabilities.SupportedPresets, raw) {
		return c.PermissionSnapshot(), nil, fmt.Errorf("permission preset %q is unavailable: %s", raw, capabilities.UnavailableReason)
	}
	drained := c.applyToolApprovalModeLocked(raw)
	return c.PermissionSnapshot(), drained, nil
}

// RevokeSessionGrant removes one exact in-memory authorization. Revocation is
// compare-and-set protected; the new revision is published atomically with the
// removal before in-flight work and background processes are stopped.
func (c *Controller) RevokeSessionGrant(scope, target string, expectedRevision uint64) (PermissionSnapshot, error) {
	if c == nil {
		return PermissionSnapshot{}, fmt.Errorf("controller is nil")
	}
	c.permissionMu.Lock()
	defer c.permissionMu.Unlock()
	current := c.permissionRevision.Load()
	if expectedRevision != current {
		return c.PermissionSnapshot(), fmt.Errorf("permission revision changed: have %d, expected %d", current, expectedRevision)
	}
	c.promptResolveMu.Lock()
	removed := false
	switch strings.TrimSpace(scope) {
	case "directory":
		if c.writeAccess.roots != nil {
			removed = c.writeAccess.roots.RevokeSession(target)
		}
	case "tool", "command-prefix":
		removed = c.approval.revokeSessionAuthorization(scope, target)
	default:
		c.promptResolveMu.Unlock()
		return c.PermissionSnapshot(), fmt.Errorf("unknown session grant scope %q", scope)
	}
	if !removed {
		c.promptResolveMu.Unlock()
		return c.PermissionSnapshot(), fmt.Errorf("session grant was not found")
	}
	c.permissionRevision.Add(1)
	c.promptResolveMu.Unlock()
	if c.Running() {
		c.Cancel()
	}
	for _, job := range c.Jobs() {
		c.CancelJob(job.ID)
	}
	return c.PermissionSnapshot(), nil
}
