package boot

import (
	"strings"

	"reasonix/internal/config"
	"reasonix/internal/plugin"
)

// capabilityServerInventory adds explicitly supplied host-session servers to
// the capability runtime and marks only those additions enabled.
func capabilityServerInventory(configEntries []config.PluginEntry, root string, opts PluginSpecOptions, hostSession []plugin.Spec, enabled map[string]bool) ([]config.PluginEntry, []plugin.Spec) {
	for _, spec := range hostSession {
		if name := strings.TrimSpace(spec.Name); name != "" {
			enabled[name] = true
		}
	}
	configSpecs := PluginSpecsForRootWithOptions(configEntries, root, opts)
	return mergeHostSessionCapabilitySpecs(configEntries, configSpecs, hostSession)
}

// mergeHostSessionCapabilitySpecs makes the host-session endpoint authoritative
// on a name collision and drops the shadowed config entry with its stale state.
func mergeHostSessionCapabilitySpecs(configEntries []config.PluginEntry, configSpecs, hostSession []plugin.Spec) ([]config.PluginEntry, []plugin.Spec) {
	if len(hostSession) == 0 {
		return configEntries, configSpecs
	}
	shadowed := make(map[string]bool, len(hostSession))
	for _, spec := range hostSession {
		if name := strings.TrimSpace(spec.Name); name != "" {
			shadowed[name] = true
		}
	}

	specs := make([]plugin.Spec, 0, len(configSpecs)+len(hostSession))
	for _, spec := range configSpecs {
		if !shadowed[strings.TrimSpace(spec.Name)] {
			specs = append(specs, spec)
		}
	}
	specs = append(specs, hostSession...)

	entries := make([]config.PluginEntry, 0, len(configEntries))
	for _, entry := range configEntries {
		if !shadowed[strings.TrimSpace(entry.Name)] {
			entries = append(entries, entry)
		}
	}
	return entries, specs
}
