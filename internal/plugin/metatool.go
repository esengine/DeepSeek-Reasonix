// Historical: the run_mcp meta-tool was a single dispatcher that replaced
// every per-tool "mcp__<server>__<tool>" top-level entry. It was removed
// because it bypassed the MCP security boundary (direct c.call instead of
// remoteTool.Execute), used name-based instead of spec-based identity
// (unsafe in a shared Host), broke lazy startup (KickSpawns started all
// handshakes eagerly), and had an unstable Description (rebuilt every call
// from live ServerNames).
//
// The meta-tool mode now reuses the existing use_capability proxy, which
// already provides stable schema, list/inspect/call, on-demand connection,
// spec-based identity, CallResolver security flow, and destructive/readOnly
// review. See internal/agent/usecapability.go and boot.go's capProxy
// registration (gated by cfg.MCPMetaToolEnabled()).
//
// This file retains only the env-only shim so mcp-surface-dump and tests
// can flip the mode via REASONIX_MCP_META_TOOL without loading config.
package plugin

import (
	"os"
	"strings"
)

// MetaToolEnabled reports whether meta-tool mode is enabled, checking ONLY
// the REASONIX_MCP_META_TOOL env var. It is the env-only shim kept for
// mcp-surface-dump and direct package use; the production boot path uses
// config.MCPMetaToolEnabled() instead, which resolves [tools] meta_tool
// config first and treats this env var as an override. Recognized spellings:
// 1/true/yes/on enables, 0/false/no/off disables, unset/unknown = false.
//
// When enabled, boot.go hides the per-tool mcp__ surface and registers
// use_capability as the single MCP entry point — NOT a run_mcp dispatcher.
func MetaToolEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("REASONIX_MCP_META_TOOL"))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}
