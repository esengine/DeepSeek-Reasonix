package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"

	"reasonix/internal/command"
	"reasonix/internal/control"
	"reasonix/internal/plugin"
	"reasonix/internal/remote/protocol"
	"reasonix/internal/skill"
)

type runtimeCapabilities interface {
	Host() *plugin.Host
	Commands() []command.Command
	Skills() []skill.Skill
	SlashSkills() []skill.Skill
	DisabledSkills() []skill.Skill
	ConfiguredMCPNames() []string
	DisconnectedMCPNames() []string
}

func buildSessionCatalog(ctx context.Context, ctrl SessionController) protocol.SessionCatalogResult {
	result := protocol.SessionCatalogResult{
		Revision: protocol.CatalogRevision("catalog_" + randomHex(8)),
		Commands: []protocol.CommandCatalogItem{}, MCPServers: []protocol.MCPServerCatalogItem{},
		Skills: []protocol.SkillCatalogItem{}, Plugins: []protocol.PluginCatalogItem{},
	}
	capabilities, ok := ctrl.(runtimeCapabilities)
	if !ok {
		return result
	}
	plugins := map[string]struct{}{}
	for _, command := range capabilities.Commands() {
		if command.Hidden {
			continue
		}
		result.Commands = append(result.Commands, protocol.CommandCatalogItem{Name: command.Name, Description: command.Description})
		if name := strings.TrimSpace(command.Plugin); name != "" {
			plugins[name] = struct{}{}
		}
	}
	for _, item := range capabilities.SlashSkills() {
		result.Skills = append(result.Skills, protocol.SkillCatalogItem{
			ID:   protocol.SkillID("skill_" + stableCatalogID(item.SlashName())),
			Name: item.SlashName(), Description: item.Description, Scope: string(item.Scope),
		})
		if name := strings.TrimSpace(item.Plugin); name != "" {
			plugins[name] = struct{}{}
		}
	}
	if host := capabilities.Host(); host != nil {
		for _, prompt := range host.Prompts() {
			result.Commands = append(result.Commands, protocol.CommandCatalogItem{Name: prompt.Name, Description: prompt.Description})
		}
		for _, name := range host.ServerNames() {
			toolCount := 0
			if tools, err := host.ToolsFor(ctx, name); err == nil {
				toolCount = len(tools)
			}
			result.MCPServers = append(result.MCPServers, protocol.MCPServerCatalogItem{Name: name, Available: true, ToolCount: toolCount})
		}
	}
	connected := make(map[string]bool, len(result.MCPServers))
	for _, server := range result.MCPServers {
		connected[server.Name] = true
	}
	for _, name := range capabilities.ConfiguredMCPNames() {
		if name = strings.TrimSpace(name); name != "" && !connected[name] {
			result.MCPServers = append(result.MCPServers, protocol.MCPServerCatalogItem{Name: name, Available: false, ToolCount: 0})
		}
	}
	for name := range plugins {
		result.Plugins = append(result.Plugins, protocol.PluginCatalogItem{ID: name, Name: name, Enabled: true})
	}
	sort.Slice(result.Commands, func(i, j int) bool { return result.Commands[i].Name < result.Commands[j].Name })
	sort.Slice(result.Skills, func(i, j int) bool { return result.Skills[i].Name < result.Skills[j].Name })
	sort.Slice(result.MCPServers, func(i, j int) bool { return result.MCPServers[i].Name < result.MCPServers[j].Name })
	sort.Slice(result.Plugins, func(i, j int) bool { return result.Plugins[i].Name < result.Plugins[j].Name })
	return result
}

func (s *Server) composerSlashArgs(p protocol.ComposerSlashArgsParams) (protocol.ComposerSlashArgsResult, error) {
	sess, err := s.sessionForQuery(p.ExpectedHostEpoch, p.Target, p.ExpectedRuntimeEpoch)
	if err != nil {
		return protocol.ComposerSlashArgsResult{}, err
	}
	capabilities, ok := sess.ctrl.(runtimeCapabilities)
	if !ok {
		return protocol.ComposerSlashArgsResult{Items: []protocol.SlashArgItem{}}, nil
	}
	data := control.ArgData{
		Skills: capabilities.Skills(), DisabledSkills: capabilities.DisabledSkills(),
		ConfiguredMCP: capabilities.ConfiguredMCPNames(), DisconnectedMCP: capabilities.DisconnectedMCPNames(),
		CurrentModel: sess.model,
	}
	pluginNames := map[string]struct{}{}
	for _, item := range capabilities.Commands() {
		if name := strings.TrimSpace(item.Plugin); name != "" {
			pluginNames[name] = struct{}{}
		}
	}
	for _, item := range capabilities.SlashSkills() {
		if name := strings.TrimSpace(item.Plugin); name != "" {
			pluginNames[name] = struct{}{}
		}
	}
	for name := range pluginNames {
		data.PluginNames = append(data.PluginNames, name)
	}
	seenProviders := map[string]bool{}
	for _, descriptor := range s.broker.Catalog() {
		data.ModelRefs = append(data.ModelRefs, descriptor.Ref)
		providerName := strings.TrimSpace(descriptor.DisplayName)
		if providerName == "" {
			providerName, _, _ = strings.Cut(descriptor.Ref, "/")
		}
		if providerName != "" && !seenProviders[providerName] {
			seenProviders[providerName] = true
			data.ProviderNames = append(data.ProviderNames, providerName)
		}
		if descriptor.Ref == sess.model {
			data.CurrentProvider = providerName
		}
	}
	if host := capabilities.Host(); host != nil {
		data.ServerNames = host.ServerNames()
	}
	sort.Strings(data.PluginNames)
	sort.Strings(data.ModelRefs)
	sort.Strings(data.ProviderNames)
	items, from := control.SlashArgItems(p.Input, data)
	result := protocol.ComposerSlashArgsResult{Items: []protocol.SlashArgItem{}, From: from}
	for _, item := range items {
		result.Items = append(result.Items, protocol.SlashArgItem{Label: item.Label, Insert: item.Insert, Hint: item.Hint, Descend: item.Descend})
	}
	return result, nil
}

func stableCatalogID(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:8])
}
