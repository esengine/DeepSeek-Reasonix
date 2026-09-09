package main

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/extension/providerext"
)

var errModelSettingsSuperseded = errors.New("model settings changed while building the session")

type tabModelApplicationState struct {
	startupRetry bool
	failure      *modelSettingsApplyFailure
}

type modelSettingsApplyFailure struct {
	controller control.SessionAPI
	revision   string
	message    string
}

type modelSettingsSnapshot interface {
	ModelSettingsState() (applied, desired string, err error)
}

func modelSettingsNeedApply(ctrl control.SessionAPI) (bool, error) {
	snapshot, ok := ctrl.(modelSettingsSnapshot)
	if !ok {
		return false, nil
	}
	applied, desired, err := snapshot.ModelSettingsState()
	return applied != desired, err
}

// Model writes own only persisted configuration. Runtime admission applies the
// latest effective snapshot before accepting the next run, including when the
// write came from another process. Saving never requires a visible session.
func (a *App) applyModelConfigChange(mutate func(*config.Config) error) error {
	_, err := a.applyModelConfigChangeWithWarning("model settings", mutate)
	return err
}

func (a *App) applyModelConfigChangeWithWarning(setting string, mutate func(*config.Config) error) (string, error) {
	return a.applyModelConfigChangeWithSave(setting, mutate, nil)
}

func (a *App) applyModelConfigChangeWithSave(setting string, mutate func(*config.Config) error, save func(*config.Config, string) error) (string, error) {
	err := func() error {
		unlock := config.LockUserConfigEdits()
		defer unlock()
		unlockCredentials, err := config.LockUserCredentialEdits()
		if err != nil {
			return err
		}
		defer unlockCredentials()
		cfg, path, err := a.loadDesktopUserConfigForEdit()
		if err != nil {
			return err
		}
		baseline := cfg.ModelSettingsBaseline()
		defer cfg.CleanupStagedModelCredentialsLocked(path)
		if err := mutate(cfg); err != nil {
			return err
		}
		if save == nil {
			return cfg.SaveModelSettingsTo(path, baseline)
		}
		return save(cfg, path)
	}()
	if err != nil {
		return "", err
	}
	a.modelSettingsSaved(setting)
	return "", nil
}

func (a *App) modelSettingsSaved(setting string) {
	a.mu.RLock()
	count, active := len(a.tabs), a.activeTabID != ""
	var retry []*WorkspaceTab
	for _, tab := range a.tabs {
		if tab != nil && tab.Ctrl == nil && tab.modelApplication.startupRetry {
			retry = append(retry, tab)
		}
	}
	a.mu.RUnlock()
	slog.Debug("model settings persisted", "setting", setting, "visibleSessions", count, "hasActiveSession", active)
	a.refreshActiveTabMetaExtras()
	if a.ctx != nil {
		for _, tab := range retry {
			a.scheduleDeferredStartupBuild(tab.ID)
		}
	}
}

// refreshTabModelSettings runs outside the turn admission read lock. The
// existing rebuild owner protects build/swap; no second scheduling authority.
func (a *App) refreshTabModelSettings(tab *WorkspaceTab) error {
	a.runtimeRebuildMu.Lock()
	defer a.runtimeRebuildMu.Unlock()
	tab.turnStartMu.Lock()
	defer tab.turnStartMu.Unlock()
	for {
		if err := a.reqCtx().Err(); err != nil {
			return err
		}
		current := a.controllerForTab(tab)
		snapshot, ok := current.(modelSettingsSnapshot)
		if !ok {
			return nil
		}
		applied, attempted, err := snapshot.ModelSettingsState()
		if err != nil {
			return fmt.Errorf("read saved model settings: %w", err)
		}
		if applied == attempted {
			return nil
		}
		if err := a.rebuildSettingTurnLocked("saved model settings", tab, false, true); err != nil {
			if errors.Is(err, errModelSettingsSuperseded) {
				continue
			}
			if current != nil {
				a.mu.Lock()
				if a.ownsRuntimeTabLocked(tab) && tab.Ctrl == current {
					tab.modelApplication.failure = &modelSettingsApplyFailure{current, attempted, modelSettingsIssue("apply_failed", err).Message}
				}
				a.mu.Unlock()
			}
			return fmt.Errorf("model settings were saved but this session could not apply them: %w", err)
		}
		return nil
	}
}

// Preserve a selected model while it is still accessible; a removed selection
// may only fall back to an explicitly available and configured Desktop model.
func resolveModelSettingsRuntime(c *config.Config, model string) (string, error) {
	if providerext.PluginRefOwner(model) != "" {
		return model, nil
	}
	if p, ok := c.ResolveModel(model); ok && modelProviderAccessAllowed(c.Desktop.ProviderAccess, p.Name) && p.Configured() {
		return p.Name + "/" + p.Model, nil
	}
	if ref := resolveNewSessionModel(c); strings.TrimSpace(ref) != "" {
		if p, ok := c.ResolveModel(ref); ok && modelProviderAccessAllowed(c.Desktop.ProviderAccess, p.Name) && p.Configured() {
			return p.Name + "/" + p.Model, nil
		}
	}
	return "", fmt.Errorf("no configured model is available; choose a model in Settings before starting another run")
}

func (a *App) sessionPathForSettingsRebuild(tab *WorkspaceTab) string {
	if path := a.reconciledSessionPathForTab(tab); path != "" {
		return path
	}
	return a.currentSessionPathFor(tab)
}

func validateModelSettingsReplacement(ctrl, previous control.SessionAPI) error {
	if stale, err := modelSettingsNeedApply(ctrl); err != nil || stale {
		if ctrl != previous {
			ctrl.Close()
		}
		if err != nil {
			return err
		}
		return errModelSettingsSuperseded
	}
	return nil
}
