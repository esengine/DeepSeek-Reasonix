package pluginpkg

import (
	"maps"
	"slices"
	"strings"

	"reasonix/internal/base/secrets"
)

func (p Package) mcpServerRefs() []MCPServerRef {
	names := slices.Sorted(maps.Keys(p.Manifest.MCPServers))
	out := make([]MCPServerRef, 0, len(names))
	for _, name := range names {
		server := p.Manifest.MCPServers[name]
		out = append(out, MCPServerRef{
			Name:        name,
			DisplayName: firstNonEmpty(strings.TrimSpace(server.DisplayName), name),
			Description: strings.TrimSpace(server.Description),
			Transport:   pluginMCPTransport(server),
			Command:     secrets.RedactConfigValue("", strings.TrimSpace(server.Command)),
			URL:         secrets.RedactEndpoint(strings.TrimSpace(server.URL)),
			AutoStart:   server.AutoStart == nil || *server.AutoStart,
		})
	}
	return out
}
