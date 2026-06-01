package cli

import (
	"fmt"
	"strings"

	"reasonix/internal/config"
	"reasonix/internal/i18n"
)

// runProvidersSubcommand handles "/providers" (list), "/providers add <preset>"
// (add from ProviderPresets), and "/providers remove <name>" (remove from config).
// Add writes to reasonix.toml so the provider is available on the next /model
// switch; remove drops it.
func (m *chatTUI) runProvidersSubcommand(input string) {
	args := tokenizeArgs(input) // args[0] == "/providers"
	if len(args) < 2 {
		m.showProviders()
		return
	}
	switch args[1] {
	case "list", "ls":
		m.showProviders()
	case "add":
		if len(args) < 3 {
			m.showAvailablePresets()
			return
		}
		m.addProviderFromPreset(args[2])
	case "remove", "rm":
		if len(args) < 3 {
			m.notice("usage: /providers remove <name>")
			return
		}
		m.removeProvider(args[2])
	default:
		m.notice("unknown /providers subcommand — try: /providers, /providers add, /providers remove")
	}
}

// showProviders lists the currently configured providers and their models.
func (m *chatTUI) showProviders() {
	cfg, err := config.Load()
	if err != nil || len(cfg.Providers) == 0 {
		m.notice(i18n.M.ListProvidersNone)
		return
	}
	var b strings.Builder
	b.WriteString(dim("  · " + i18n.M.ListProvidersHeader + "\n"))
	for _, p := range cfg.Providers {
		for _, model := range p.ModelList() {
			ref := p.Name + "/" + model
			marker := "  "
			if ref == m.modelRef {
				marker = accent("› ")
			}
			fmt.Fprintf(&b, "%s%s\n", marker, ref)
		}
	}
	m.notice(strings.TrimRight(b.String(), "\n"))
}

// showAvailablePresets lists ProviderPresets that aren't yet in the config.
func (m *chatTUI) showAvailablePresets() {
	presets := config.ProviderPresets()
	cfg, _ := config.Load()
	existing := map[string]bool{}
	if cfg != nil {
		for _, p := range cfg.Providers {
			existing[p.Name] = true
		}
	}
	var b strings.Builder
	b.WriteString(dim("  · available presets (/providers add <name>)\n"))
	for _, p := range presets {
		if existing[p.Name] {
			continue // skip already-added presets
		}
		fmt.Fprintf(&b, "  %s — %s\n", bold(p.Name), strings.Join(p.ModelList(), ", "))
	}
	out := strings.TrimRight(b.String(), "\n")
	if out == "" {
		m.notice("all presets already added")
		return
	}
	m.notice(out)
}

// addProviderFromPreset looks up a preset by name, adds it to the config, saves,
// and prompts for the API key if missing.
func (m *chatTUI) addProviderFromPreset(name string) {
	presets := config.ProviderPresets()
	var preset *config.ProviderEntry
	for i := range presets {
		if presets[i].Name == name {
			preset = &presets[i]
			break
		}
	}
	if preset == nil {
		m.notice(fmt.Sprintf("unknown preset %q — use /providers add to see available", name))
		return
	}
	cfg, err := config.Load()
	if err != nil {
		m.notice("providers: " + err.Error())
		return
	}
	if _, exists := cfg.Provider(name); exists {
		m.notice(fmt.Sprintf("%s is already configured", name))
		return
	}
	if err := cfg.UpsertProvider(*preset); err != nil {
		m.notice("providers add: " + err.Error())
		return
	}
	if err := cfg.Save(); err != nil {
		m.notice("providers add: " + err.Error())
		return
	}
	m.notice(fmt.Sprintf("Added %s (%s) — use /model %s/%s to switch",
		name, strings.Join(preset.ModelList(), ", "), name, preset.DefaultModel()))

	// Prompt for API key if not set.
	if preset.APIKeyEnv != "" {
		if err := cfg.Validate(name + "/" + preset.DefaultModel()); err != nil {
			m.notice(fmt.Sprintf("  %s not set — add it to .env or export it", preset.APIKeyEnv))
		}
	}
}

// removeProvider removes a provider from the config and saves.
func (m *chatTUI) removeProvider(name string) {
	cfg, err := config.Load()
	if err != nil {
		m.notice("providers: " + err.Error())
		return
	}
	if err := cfg.RemoveProvider(name); err != nil {
		m.notice("providers remove: " + err.Error())
		return
	}
	if err := cfg.Save(); err != nil {
		m.notice("providers remove: " + err.Error())
		return
	}
	m.notice(fmt.Sprintf("Removed %s", name))
}
