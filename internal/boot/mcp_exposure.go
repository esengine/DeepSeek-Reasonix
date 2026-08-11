package boot

import (
	"fmt"
	"strings"

	"reasonix/internal/plugin"
)

// mcpExposureMode is deliberately an internal boot decision rather than a
// user-facing setting. The provider-visible tool list must be chosen once per
// session so connecting a server later cannot churn the prompt-cache prefix.
type mcpExposureMode string

const (
	mcpExposurePerTool    mcpExposureMode = "per-tool"
	mcpExposureCapability mcpExposureMode = "use-capability"
)

const (
	// Keep small MCP setups direct: the model already benefits from seeing the
	// concrete schema and avoids an extra list/inspect round trip.
	autoMCPToolThreshold = 16

	// A schema payload larger than 16 KiB is already a material fraction of the
	// tools prefix for the small/medium context windows Reasonix supports. Larger
	// context windows scale this budget proportionally below.
	autoMCPMinSchemaBytes             = 16 * 1024
	autoMCPContextFractionDenominator = 20 // 5% of context bytes

	// A cache miss hides the eventual tool count. Keep a bounded estimate for
	// diagnostics, but always use the proxy for an unknown server: exposing it
	// directly would let its background handshake mutate the provider-visible
	// schema after boot.
	autoMCPUnknownToolsEstimate  = 8
	autoMCPUnknownSchemaEstimate = 2 * 1024
)

type mcpExposureDecision struct {
	Mode                 mcpExposureMode
	KnownTools           int
	EstimatedTools       int
	KnownSchemaBytes     int
	EstimatedSchemaBytes int
	UnknownServers       int
	SchemaBudget         int
	Reason               string
}

func (d mcpExposureDecision) useCapability() bool {
	return d.Mode == mcpExposureCapability
}

func (d mcpExposureDecision) notice() string {
	if !d.useCapability() {
		return ""
	}
	return fmt.Sprintf(
		"MCP tool surface is large or not yet known; using use_capability automatically (%d known tools, %d estimated tools, %d estimated schema bytes, %d-byte budget, %d uncached servers).",
		d.KnownTools, d.EstimatedTools, d.EstimatedSchemaBytes, d.SchemaBudget, d.UnknownServers,
	)
}

// chooseMCPExposure decides the provider-visible MCP surface from boot-time
// schema information. It never consults a user setting: the ordinary path is
// zero-configuration and the result is captured for the whole session.
//
// cachedTools and cacheKeyOK come from capability.LoadCachedToolsForSpecs. A
// mismatched or missing cache is not trusted as a concrete schema, but it still
// contributes a bounded estimate for diagnostics and threshold explanations.
func chooseMCPExposure(specs []plugin.Spec, cachedTools map[string][]plugin.CachedTool, cacheKeyOK map[string]bool, contextWindow int) mcpExposureDecision {
	decision := mcpExposureDecision{
		Mode:         mcpExposurePerTool,
		SchemaBudget: mcpSchemaBudget(contextWindow),
	}
	seen := map[string]bool{}
	for _, spec := range specs {
		name := strings.TrimSpace(spec.Name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		tools := cachedTools[name]
		if len(tools) == 0 || !cacheKeyOK[name] {
			decision.UnknownServers++
			continue
		}
		decision.KnownTools += len(tools)
		for _, cached := range tools {
			decision.KnownSchemaBytes += mcpCachedToolBytes(cached)
		}
	}
	decision.EstimatedTools = decision.KnownTools + decision.UnknownServers*autoMCPUnknownToolsEstimate
	decision.EstimatedSchemaBytes = decision.KnownSchemaBytes + decision.UnknownServers*autoMCPUnknownSchemaEstimate
	if decision.UnknownServers > 0 {
		decision.Mode = mcpExposureCapability
		decision.Reason = "one or more MCP schema caches are unavailable or stale"
	} else if decision.EstimatedTools >= autoMCPToolThreshold {
		decision.Mode = mcpExposureCapability
		decision.Reason = "estimated MCP tool count exceeds the automatic threshold"
	} else if decision.EstimatedSchemaBytes >= decision.SchemaBudget {
		decision.Mode = mcpExposureCapability
		decision.Reason = "estimated MCP schema size exceeds the automatic budget"
	}
	return decision
}

func mcpSchemaBudget(contextWindow int) int {
	budget := autoMCPMinSchemaBytes
	if contextWindow <= 0 {
		return budget
	}
	// Schema bytes are a rough proxy for the serialized tools prefix. Scale the
	// automatic budget with the provider context window, but never lower the
	// conservative floor above.
	scaled := int64(contextWindow) * 4 / autoMCPContextFractionDenominator
	if scaled > int64(budget) {
		budget = int(scaled)
	}
	return budget
}

func mcpCachedToolBytes(cached plugin.CachedTool) int {
	// Include the fields that materially appear in provider tool definitions,
	// plus framing for name/description/JSON properties. This is intentionally
	// an estimate; the mode only needs a stable, conservative threshold.
	return len(cached.Name) + len(cached.Description) + len(cached.Schema) + 64
}
