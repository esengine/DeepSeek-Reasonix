package main

import "reasonix/internal/runtimeapi"

func (a *App) remotePluginsV1(tabID string) ([]PluginView, error) {
	catalog, err := a.remoteSessionCatalogV1(tabID)
	if err != nil {
		return nil, err
	}
	return projectRemotePlugins(catalog.Plugins), nil
}

func projectRemotePlugins(items []runtimeapi.PluginCatalogItem) []PluginView {
	out := make([]PluginView, len(items))
	for index, item := range items {
		// The frozen SessionCatalog exposes only identity, display name and
		// enabled state. Never populate Root, Source, commands, hooks, MCP
		// transport details or verification paths from Desktop-local state.
		out[index] = PluginView{Name: item.Name, Enabled: item.Enabled}
	}
	return out
}
