package cli

import (
	"fmt"
	"strings"

	"reasonix/internal/config"
	"reasonix/internal/i18n"
)

// runProviderCommand handles "/provider": with no argument it lists the configured
// providers and marks the active one; "/provider <name>" switches to that
// provider's default model (or prompts the user to pick one when multiple models
// are configured).
func (m *chatTUI) runProviderCommand(input string) {
	args := tokenizeArgs(input) // args[0] == "/provider"
	if len(args) < 2 {
		m.showProviders()
		return
	}
	name := args[1]
	m.switchToProvider(name)
}

// showProviders lists all configured providers, marking the one backing the
// current model.
func (m *chatTUI) showProviders() {
	cfg, err := config.Load()
	if err != nil {
		m.notice("provider: " + err.Error())
		return
	}
	var lines []string
	lines = append(lines, viewHeader("%s", i18n.M.ProviderListHeader))

	// Determine the current provider name from m.modelRef ("provider/model").
	curProvider := ""
	if parts := strings.SplitN(m.modelRef, "/", 2); len(parts) == 2 {
		curProvider = parts[0]
	}

	for i := range cfg.Providers {
		p := &cfg.Providers[i]
		if !p.Configured() {
			continue
		}
		// Prefer chat models for display; fall back to the full model list
		// (which includes non-chat models like embeddings) for count/label.
		models := p.ChatModelList()
		if len(models) == 0 {
			models = p.ModelList()
		}

		status := ""
		if p.Name == curProvider {
			status = "  " + viewStatus("active")
		}
		modelLabel := fmt.Sprintf("%d models", len(models))
		if len(models) == 1 {
			modelLabel = models[0]
		}
		line := fmt.Sprintf("  %-16s  %-20s  %s%s", p.Name, modelLabel, dim(p.Kind), status)
		lines = append(lines, line)
	}
	lines = append(lines, viewHint(viewCompactText("switch with /provider <name>", m.width)))
	m.commitLine(strings.Join(lines, "\n"))
}

// switchToProvider switches the session to the named provider's default model.
// If the provider has multiple models, it shows an interactive picker (in the
// setup/CLI style) if running in a TTY, or falls back to a notice listing
// available models.
func (m *chatTUI) switchToProvider(name string) {
	cfg, err := config.Load()
	if err != nil {
		m.notice("provider: " + err.Error())
		return
	}
	var entry *config.ProviderEntry
	for i := range cfg.Providers {
		p := &cfg.Providers[i]
		if p.Name == name && p.Configured() {
			entry = p
			break
		}
	}
	if entry == nil {
		m.notice(fmt.Sprintf(i18n.M.ProviderUnknownFmt, name))
		return
	}

	// Determine current provider.
	curProvider := ""
	if parts := strings.SplitN(m.modelRef, "/", 2); len(parts) == 2 {
		curProvider = parts[0]
	}

	models := entry.ChatModelList()
	if len(models) == 0 {
		models = entry.ModelList()
	}
	if len(models) == 0 {
		m.notice(fmt.Sprintf(i18n.M.ProviderNoModelsFmt, name))
		return
	}

	// If only one model, switch directly.
	if len(models) == 1 {
		ref := entry.Name + "/" + models[0]
		if entry.Name == curProvider && models[0] == "" {
			m.notice(fmt.Sprintf(i18n.M.ProviderAlreadyOnFmt, name))
			return
		}
		m.runModelSubcommand("/model " + ref)
		return
	}

	// If already on this provider, just notify.
	if entry.Name == curProvider {
		m.notice(fmt.Sprintf(i18n.M.ProviderAlreadyOnFmt, name))
		return
	}

	// Multiple models — use the TUI notice to list them and tell the user to
	// pick one with /model. We don't launch raw-mode selectOne from inside the
	// bubbletea event loop because it would conflict with bubbletea's terminal
	// management. Instead, we show the available models and suggest /model.
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", viewHeader("%s", fmt.Sprintf(i18n.M.ProviderPickLabel, name)))
	for _, model := range models {
		fmt.Fprintf(&b, "  %s\n", model)
	}
	fmt.Fprintf(&b, "%s", viewHint(viewCompactText(fmt.Sprintf("switch with /model %s/<model>", entry.Name), m.width)))
	m.commitLine(strings.TrimRight(b.String(), "\n"))
}
