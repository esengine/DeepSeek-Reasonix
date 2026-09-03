package main

import (
	"log/slog"
	"strings"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/provider"
)

func (a *App) recordModelSwitchTiming(tabID string, timing *modelSwitchTiming, started time.Time, result *error) {
	timing.Total = time.Since(started)
	if *result != nil {
		timing.Outcome = "failed"
	} else {
		timing.Outcome = "ok"
	}
	slog.Debug(
		"desktop: model switch timing",
		"tab", tabID,
		"outcome", timing.Outcome,
		"total_ms", timing.Total.Milliseconds(),
		"lock_wait_ms", timing.LockWait.Milliseconds(),
		"prepare_ms", timing.Prepare.Milliseconds(),
		"config_ms", timing.Config.Milliseconds(),
		"snapshot_ms", timing.Snapshot.Milliseconds(),
		"build_ms", timing.Build.Milliseconds(),
		"lease_resume_ms", timing.LeaseAndResume.Milliseconds(),
		"swap_persist_ms", timing.SwapAndPersist.Milliseconds(),
	)
	if a.modelSwitchTimingHook != nil {
		a.modelSwitchTimingHook(*timing)
	}
}

func (a *App) ModelsForTab(tabID string) []ModelInfo {
	if cur, ok := a.remoteTabCurrentModel(tabID); ok {
		if !a.remoteTabLocalProxy(tabID) {
			if infos, err := a.remoteServeModelsForTab(tabID, cur); err == nil {
				return infos
			}
			return []ModelInfo{}
		}
		return a.remoteProxyModelCatalog(cur)
	}
	a.mu.RLock()
	curModel := ""
	workspaceRoot := ""
	var ctrl control.SessionAPI
	if tab := a.tabByIDLocked(tabID); tab != nil {
		curModel = tab.model
		workspaceRoot = tab.WorkspaceRoot
		ctrl = tab.Ctrl
	}
	a.mu.RUnlock()
	return a.desktopModelCatalog(curModel, workspaceRoot, ctrl)
}

func (a *App) remoteProxyModelCatalog(curModel string) []ModelInfo {
	cfg, err := config.Load()
	if err != nil {
		return []ModelInfo{}
	}
	current, ok := cfg.ResolveModel(curModel)
	if !ok {
		return []ModelInfo{}
	}
	kind := strings.TrimSpace(current.Kind)
	if kind == "" {
		kind = "openai"
	}
	canonical := current.Name + "/" + current.Model
	out := []ModelInfo{}
	for i := range cfg.Providers {
		entry := &cfg.Providers[i]
		entryKind := strings.TrimSpace(entry.Kind)
		if entryKind == "" {
			entryKind = "openai"
		}
		if !strings.EqualFold(entryKind, kind) || !modelProviderAccessAllowed(cfg.Desktop.ProviderAccess, entry.Name) || !entry.Configured() {
			continue
		}
		if strings.TrimSpace(entry.AccountID) != "" {
			account, ok := config.ProviderAccountForEntry(cfg, *entry)
			if ok && (account.Retired || !account.IsEnabled()) && !strings.HasPrefix(curModel, entry.Name+"/") {
				continue
			}
		}
		for _, model := range entry.ChatModelList() {
			ref := entry.Name + "/" + model
			out = append(out, ModelInfo{Ref: ref, Provider: entry.Name, Model: model, Current: ref == canonical})
		}
	}
	return out
}

func (a *App) desktopModelCatalog(curModel, workspaceRoot string, ctrl control.SessionAPI) []ModelInfo {
	// A cold extension catalog fetch can block on a sidecar RPC, so read it
	// before loading config and without holding App.mu.
	var extensionCatalog []provider.Descriptor
	if ctrl != nil {
		extensionCatalog = ctrl.ProviderCatalog()
	}
	cfg, err := config.LoadForRoot(workspaceRoot)
	if err != nil {
		return []ModelInfo{}
	}
	requestedRef := strings.TrimSpace(curModel)
	if requestedRef == "" {
		requestedRef = strings.TrimSpace(cfg.DefaultModel)
		curModel = requestedRef
	}
	if entry, ok := cfg.ResolveModel(curModel); ok {
		curModel = entry.Name + "/" + entry.Model
	}
	out := []ModelInfo{}
	for i := range cfg.Providers {
		p := &cfg.Providers[i]
		if !modelProviderAccessAllowed(cfg.Desktop.ProviderAccess, p.Name) || !p.Configured() {
			continue
		}
		account, accountKnown := config.ProviderAccountForEntry(cfg, *p)
		if accountKnown && (account.Retired || !account.IsEnabled()) && !strings.HasPrefix(curModel, p.Name+"/") {
			continue
		}
		for _, m := range p.ChatModelList() {
			ref := p.Name + "/" + m
			out = append(out, ModelInfo{
				Ref: ref, Provider: p.Name, Model: m, Current: ref == curModel,
				ProviderGroup: account.ProviderID, AccountID: account.ID, AccountLabel: account.Label, AccountDefault: account.Default,
			})
		}
	}
	appendLegacyAccountFamilyModels(&out, cfg, requestedRef)
	return mergeExtensionModelInfos(out, extensionCatalog, curModel)
}

func appendLegacyAccountFamilyModels(out *[]ModelInfo, cfg *config.Config, requestedRef string) {
	if out == nil || cfg == nil {
		return
	}
	family, model, ok := strings.Cut(strings.TrimSpace(requestedRef), "/")
	if !ok || family == "" {
		return
	}
	entry, found := cfg.ResolveModel(requestedRef)
	if !found {
		return
	}
	account, ok := config.ProviderAccountForEntry(cfg, *entry)
	if !ok || account.ProviderID == family || !modelProviderAccessAllowed(cfg.Desktop.ProviderAccess, entry.Name) {
		return
	}
	family = account.ProviderID
	for _, item := range *out {
		if item.Ref == family+"/"+model {
			return
		}
	}
	if model == "" {
		return
	}
	*out = append(*out, ModelInfo{Ref: family + "/" + model, Provider: family, Model: model, Current: strings.TrimSpace(requestedRef) == family+"/"+model, ProviderGroup: account.ProviderID, AccountID: account.ID, AccountLabel: account.Label, AccountDefault: account.Default})
}
