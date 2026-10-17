package boot

import (
	"fmt"

	"reasonix/internal/contract/agentcomposition"
	"reasonix/internal/contract/config"
	"reasonix/internal/ext/pluginpkg"
)

func (b *builder) validateCompositionAvailability() error {
	if err := b.prompt.skillStore.ValidateSnapshot(); err != nil {
		return fmt.Errorf("%w: %w", agentcomposition.ErrUnavailable, err)
	}
	if err := b.tools.host.CompositionContractError(); err != nil {
		return fmt.Errorf("%w: %w", agentcomposition.ErrUnavailable, err)
	}
	for _, entry := range b.cfg.Plugins {
		decision, _, err := config.DefaultActivationStore().ServerDecision(entry, b.root)
		if err != nil {
			return fmt.Errorf("%w: %w", agentcomposition.ErrUnavailable, err)
		}
		if decision == config.ActivationDisabled {
			return agentcomposition.Unavailable(entry.Name, "MCP authorization was revoked.")
		}
	}
	installed, _ := pluginpkg.LoadInstalled(b.roots.Home())
	return verifySelected(b.opts.Composition.Resolved.Extensions, installed, func(p pluginpkg.InstalledPackage) string { return p.Installed.Name })
}
