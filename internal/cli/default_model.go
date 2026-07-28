package cli

import (
	"fmt"
	"strings"

	"reasonix/internal/config"
)

// resolveModelForCLI picks the model a CLI subcommand should boot on. It
// mirrors desktopNewSessionModel on the desktop side (#6999) so a
// default_model that resolves but has no API key in the current environment
// no longer hard-fails every command on "missing env X_API_KEY" (issue
// #6996). A keyless default silently falls through to the first provider
// with a configured key, matching the runtime chat path's behavior.
//
// Semantics:
//
//   - explicitRef == "": the caller is using the configured default. If the
//     default resolves and is configured, it wins. Otherwise the first
//     provider with a configured key wins. When no provider is configured
//     at all the raw default is returned with fallback=false so the
//     existing missing-key banner / boot-time error still tells the user
//     which API key to set.
//
//   - explicitRef != "": the caller asked for this exact ref (--model flag,
//     ACP session param, etc.). The ref must resolve AND be configured.
//     There is no silent fallback — explicit choices that misfire must
//     fail loudly so the user is not quietly rerouted onto a model they
//     did not ask for.
func resolveModelForCLI(explicitRef string, cfg *config.Config) (ref string, fallback bool, err error) {
	explicitRef = strings.TrimSpace(explicitRef)
	if explicitRef != "" {
		entry, ok := cfg.ResolveModel(explicitRef)
		if !ok {
			return "", false, fmt.Errorf("unknown model %q", explicitRef)
		}
		if !entry.Configured() {
			return "", false, fmt.Errorf("provider %q requires %s", explicitRef, entry.APIKeyEnv)
		}
		return entry.Name + "/" + entry.Model, false, nil
	}
	def := strings.TrimSpace(cfg.DefaultModel)
	if def != "" {
		if entry, ok := cfg.ResolveModel(def); ok && entry.Configured() {
			return def, false, nil
		}
	}
	for i := range cfg.Providers {
		p := &cfg.Providers[i]
		if len(p.ModelList()) == 0 || !p.Configured() {
			continue
		}
		return p.Name + "/" + p.DefaultModel(), true, nil
	}
	// No configured provider and no configured default: hand the raw default
	// back unchanged. Downstream code (boot.Build or the TUI banner) surfaces
	// the missing-key message from this ref, so the user still gets a useful
	// hint about what to fix. An empty default is also returned so callers
	// that distinguish "no model" from "keyless model" keep working.
	return def, false, nil
}
