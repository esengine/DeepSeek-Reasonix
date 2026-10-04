package serve

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"reasonix/internal/assembly/boot"
	"reasonix/internal/contract/config"
	"reasonix/internal/session/control"
)

// switchEffort persists a new reasoning-effort level for the active provider and
// rebuilds via switchModel (which serializes on bindMu).
func (s *Server) switchEffort(ctx context.Context, level string) error {
	cur := s.ctl()
	cfg, err := runtimeConfig(cur)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	ref := currentModelRef(cur)
	entry, ok := cfg.ResolveModel(ref)
	if !ok {
		return refusal(http.StatusConflict, "effort.no_provider",
			fmt.Errorf("cannot resolve current provider %q", ref), nil)
	}
	capability := config.EffortCapabilityForEntry(entry)
	if !capability.Supported {
		return refusal(http.StatusBadRequest, "effort.not_configurable",
			fmt.Errorf("%s declares no reasoning-effort levels; give it one with reasoning_protocol or supported_efforts in the provider's config block", entry.Name),
			map[string]any{"provider": entry.Name})
	}
	effort, err := config.NormalizeEffort(entry, level)
	if err != nil {
		return refusal(http.StatusBadRequest, "effort.unsupported_level", err,
			map[string]any{"provider": entry.Name, "level": level, "levels": strings.Join(capability.Levels, " | ")})
	}
	targetRef := entry.Name + "/" + entry.Model
	// Compare the request-level effort, not the picker spelling. "auto" and an
	// explicit level can resolve to the same provider request without rebuilding.
	targetEntry := *entry
	targetEntry.Effort = effort
	identity := boot.ResolveProviderBuildIdentity(&targetEntry, cfg.NetworkProxySpec(), nil)
	target := control.RuntimeSelection{
		ModelRef:            targetRef,
		Effort:              identity.Effort,
		ProviderFingerprint: identity.Fingerprint,
	}
	alreadyRunning := runtimeSelectionMatches(cur, target)
	if !alreadyRunning && controllerHasActiveRuntimeWork(cur) {
		return busyErr("busy.change_effort", "cannot change effort while active work or background jobs are running")
	}
	editPath := config.UserConfigPath()
	if editPath == "" {
		return fmt.Errorf("no config file found")
	}
	// Lock only the load-modify-save cycle; switchModel below rebuilds the
	// controller and must not hold the config edit lock.
	if err := func() error {
		unlock := config.LockUserConfigEdits()
		defer unlock()
		edit := config.LoadForEdit(editPath)
		if err := applyEffortEdit(edit, entry, effort); err != nil {
			return err
		}
		if err := edit.SaveTo(editPath); err != nil {
			return fmt.Errorf("save config: %w", err)
		}
		return nil
	}(); err != nil {
		return err
	}
	if alreadyRunning {
		return nil
	}
	return s.switchModel(ctx, targetRef)
}

// applyEffortEdit writes effort onto entry within edit, mirroring CLI/desktop
// SetEffort: upsert the provider when the user config has no block for it yet.
// It changes nothing else; endpoint field support is the provider contract's call.
func applyEffortEdit(edit *config.Config, entry *config.ProviderEntry, effort string) error {
	if _, ok := edit.Provider(entry.Name); !ok {
		if err := edit.UpsertProvider(*entry); err != nil {
			return err
		}
	}
	return edit.SetProviderEffort(entry.Name, effort)
}
