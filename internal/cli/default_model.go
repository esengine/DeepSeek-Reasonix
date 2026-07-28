package cli

import (
	"fmt"
	"strings"

	"reasonix/internal/config"
)

// resolveModelForCLI picks the chat model a CLI subcommand should boot on. A
// keyless default falls through to the first configured chat provider, while
// explicit model choices remain strict.
//
// Semantics:
//
//   - explicitRef == "": Config.ResolveNewSessionChatModel supplies the shared
//     default/fallback policy used by boot and desktop. Non-chat models are
//     never selected as a fallback.
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
	ref, fallback, _ = cfg.ResolveNewSessionChatModel()
	return ref, fallback, nil
}

// resolveServeModel keeps serve's implicit model scoped to the user config.
// A project reasonix.toml may configure the served workspace, but it must not
// replace the account-level model used for new serve sessions.
func resolveServeModel(modelName string) string {
	if strings.TrimSpace(modelName) != "" {
		return modelName
	}
	cfg := config.LoadForEdit(config.UserConfigPath())
	if resolved, _, ok := cfg.ResolveNewSessionChatModel(); ok {
		return resolved
	}
	return modelName
}
