package main

import (
	"strings"

	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/pluginpkg"
)

// SlashArgItem is one sub-command / argument suggestion for the composer's slash
// menu (the part after the command word). Mirrors the CLI's arg completion via
// the shared control.SlashArgItems, so desktop and CLI offer the same hints.
type SlashArgItem struct {
	Label   string `json:"label"`
	Insert  string `json:"insert"`
	Hint    string `json:"hint"`
	Descend bool   `json:"descend"`
}

// SlashArgsResult carries the suggestions plus the byte offset in the input where
// the current token begins, so the composer replaces just that token.
type SlashArgsResult struct {
	Items []SlashArgItem `json:"items"`
	From  int            `json:"from"`
}

// SlashArgs completes the arguments of a management slash command (/mcp, /model,
// /skill, /hooks) for the composer — the same logic the chat TUI uses. Empty
// Items means the input has no structured arguments to complete.
func (a *App) SlashArgs(input string) SlashArgsResult {
	a.mu.RLock()
	ctrl := a.activeCtrlLocked()
	model := ""
	tabID := ""
	if tab := a.activeTabLocked(); tab != nil {
		model = tab.model
		tabID = tab.ID
	}
	a.mu.RUnlock()
	if ctrl == nil {
		return SlashArgsResult{Items: []SlashArgItem{}}
	}
	data := control.ArgData{
		Skills:          ctrl.Skills(),
		DisabledSkills:  ctrl.DisabledSkills(),
		ConfiguredMCP:   ctrl.ConfiguredMCPNames(),
		DisconnectedMCP: ctrl.DisconnectedMCPNames(),
		CurrentModel:    model,
	}
	if fields := strings.Fields(input); len(fields) > 0 && fields[0] == "/effort" {
		if effort := a.EffortForTab(tabID); effort.Supported {
			data.EffortLevels = append([]string(nil), effort.Levels...)
		}
	}
	if names, err := pluginpkg.InstalledNames(config.ReasonixHomeDir()); err == nil {
		data.PluginNames = names
	}
	seen := map[string]bool{}
	for _, m := range a.Models() {
		data.ModelRefs = append(data.ModelRefs, m.Ref)
		if m.Provider != "" && !seen[m.Provider] {
			seen[m.Provider] = true
			data.ProviderNames = append(data.ProviderNames, m.Provider)
		}
		if m.Current {
			data.CurrentProvider = m.Provider
		}
	}
	if h := ctrl.Host(); h != nil {
		data.ServerNames = h.ServerNames()
	}
	data.MemoryRefs, data.MemoryArchives = control.MemoryCompletionData(ctrl.Memory())
	items, from := control.SlashArgItems(input, data)
	// Non-nil so it serializes as a JSON array, never null — the frontend filters
	// over it directly.
	out := SlashArgsResult{Items: []SlashArgItem{}, From: from}
	for _, it := range items {
		out.Items = append(out.Items, SlashArgItem{Label: it.Label, Insert: it.Insert, Hint: it.Hint, Descend: it.Descend})
	}
	return out
}
