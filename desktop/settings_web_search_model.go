package main

import (
	"fmt"
	"strings"

	"reasonix/internal/config"
)

// SetWebSearchModel uses the same admission, locking and rebuild lifecycle as
// other model assignments. A running controller is never mutated in place.
func (a *App) SetWebSearchModel(ref string) error {
	_, err := a.applyModelConfigChangeWithSave("web search model", func(c *config.Config) error {
		return setWebSearchModelConfig(c, ref)
	}, func(c *config.Config, path string) error { return c.SaveWebSearchModelTo(path) })
	return err
}

func (a *App) populateWebSearchSettings(v *SettingsView, cfg *config.Config, root string) {
	v.WebSearchModels = []string{}
	for _, p := range cfg.Providers {
		if !modelProviderAccessAllowed(cfg.Desktop.ProviderAccess, p.Name) {
			continue
		}
		for _, model := range p.ModelList() {
			ref := p.Name + "/" + model
			if _, err := cfg.ResolveWebSearchModel(ref); err == nil {
				v.WebSearchModels = append(v.WebSearchModels, ref)
			}
		}
	}
	effective := cfg
	path := config.SourcePathForRoot(root)
	if !config.IsUserConfigPath(path) && config.ConfigFileDefinesWebSearchModel(path) {
		if loaded, err := config.LoadForRootReadOnly(root); err == nil {
			effective = loaded
			v.WebSearchModelOverridden = true
		}
	}
	v.EffectiveWebSearchModel = effective.Agent.WebSearchModel
	current, _ := effective.ResolveModel(effective.DefaultModel)
	result := effective.ResolveWebSearch(current)
	v.WebSearchModelStatus, v.WebSearchModelReason = result.Status, result.Reason
	if ref := strings.TrimSpace(effective.Agent.WebSearchModel); ref != "" && !strings.EqualFold(ref, "auto") {
		name, _, _ := strings.Cut(ref, "/")
		if !modelProviderAccessAllowed(effective.Desktop.ProviderAccess, name) {
			v.WebSearchModelStatus = "invalid"
			v.WebSearchModelReason = "Search connection is not added"
		}
	}
}

func setWebSearchModelConfig(c *config.Config, ref string) error {
	ref = strings.TrimSpace(ref)
	if ref != "" && !strings.EqualFold(ref, "auto") {
		entry, err := c.ResolveWebSearchModel(ref)
		if err != nil {
			return err
		}
		if !modelProviderAccessAllowed(c.Desktop.ProviderAccess, entry.Name) {
			return fmt.Errorf("search connection is not added")
		}
	}
	return c.SetWebSearchModel(ref)
}
