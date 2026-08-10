package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"reasonix/internal/config"
	"reasonix/internal/control"
)

type providerRemovalPlan struct {
	config      *config.Config
	root        string
	fingerprint string
	targets     []string
	fallbackRef string
}

type providerRemovalTab struct {
	id            string
	ctrl          control.SessionAPI
	readOnly      bool
	retargetModel bool
}

// DeleteProvider removes a provider and retargets open idle tabs that used it.
func (a *App) DeleteProvider(name string) error {
	return a.deleteProviderAndRetargetTabs(name)
}

// RemoveProviderAccess hides one provider access card. The plural form keeps
// official provider profiles represented by a single card failure-atomic.
func (a *App) RemoveProviderAccess(name string) error {
	return a.RemoveProviderAccesses([]string{name})
}

func (a *App) RemoveProviderAccesses(rawNames []string) error {
	names := uniqueNonEmptyStrings(rawNames)
	if len(names) == 0 {
		return fmt.Errorf("remove provider access: provider list is empty")
	}
	cfg, _, err := a.loadDesktopUserConfigForView()
	if err != nil {
		return err
	}
	officialKind := ""
	for _, name := range names {
		p, ok := cfg.Provider(name)
		if !ok {
			return fmt.Errorf("remove provider access: provider %q not found", name)
		}
		kind := officialProviderKindFromEntry(*p)
		if kind == "" {
			if len(names) == 1 {
				return a.deleteProviderAndRetargetTabs(name)
			}
			return fmt.Errorf("remove provider access: custom provider %q cannot be removed as part of a group", name)
		}
		if officialKind != "" && kind != officialKind {
			return fmt.Errorf("remove provider access: providers do not belong to one official group")
		}
		officialKind = kind
	}
	return a.removeBuiltInProviderAccessAndRetargetTabs(names)
}

func validateOfficialProviderRemoval(c *config.Config, names []string) error {
	officialKind := ""
	for _, name := range names {
		p, ok := c.Provider(name)
		if !ok {
			return fmt.Errorf("remove provider access: provider %q not found", name)
		}
		kind := officialProviderKindFromEntry(*p)
		if kind == "" {
			return fmt.Errorf("remove provider access: provider %q is no longer an official provider", name)
		}
		if officialKind != "" && kind != officialKind {
			return fmt.Errorf("remove provider access: providers do not belong to one official group")
		}
		officialKind = kind
	}
	return nil
}

func lockProviderRemovalState() (func(), error) {
	unlockConfig := config.LockUserConfigEdits()
	unlockCredentials, err := config.LockUserCredentialEdits()
	if err != nil {
		unlockConfig()
		return nil, err
	}
	return func() {
		unlockCredentials()
		unlockConfig()
	}, nil
}

func (a *App) loadProviderRemovalConfigForEdit(root string) (*config.Config, string, error) {
	cfg, path, err := a.loadDesktopUserConfigForEditForRoot(root)
	if err != nil {
		return nil, "", err
	}
	for i := range cfg.Providers {
		cfg.Providers[i].ResolveAPIKeyForRoot(root)
	}
	return cfg, path, nil
}

// providerRemovalStateFingerprint covers every config or credential-store
// value used to classify a removal, choose its fallback, and resolve model
// references. The keyed digest remains process-local and contains no raw secret.
func providerRemovalStateFingerprint(c *config.Config, credentialsRevision string) string {
	h := hmac.New(sha256.New, providerStateFingerprintKey)
	write := func(value string) {
		_, _ = fmt.Fprintf(h, "%d:", len(value))
		_, _ = h.Write([]byte(value))
	}
	write("provider-removal-state-v2")
	write(credentialsRevision)
	write(c.DefaultModel)
	write(c.Agent.PlannerModel)
	write(c.Agent.SubagentModel)
	for _, name := range c.Desktop.ProviderAccess {
		write(name)
	}
	skills := make([]string, 0, len(c.Agent.SubagentModels))
	for skill := range c.Agent.SubagentModels {
		skills = append(skills, skill)
	}
	sort.Strings(skills)
	for _, skill := range skills {
		write(skill)
		write(c.Agent.SubagentModels[skill])
	}
	for i := range c.Providers {
		p := &c.Providers[i]
		write(p.Name)
		write(p.Kind)
		write(p.BaseURL)
		write(p.APIKeyEnv)
		write(fmt.Sprintf("%t", p.Configured()))
		write(p.Model)
		write(p.Default)
		for _, model := range p.Models {
			write(model)
		}
	}
	return string(h.Sum(nil))
}

func officialProviderRemovalTargets(names []string) []string {
	targets := append([]string(nil), names...)
	for _, name := range names {
		switch canonical := config.CanonicalDesktopOfficialProviderName(name); canonical {
		case "deepseek":
			targets = append(targets, canonical, "deepseek-flash", "deepseek-pro")
		default:
			targets = append(targets, canonical)
		}
	}
	return uniqueNonEmptyStrings(targets)
}

func providerAccessFallbackRef(c *config.Config, names []string) string {
	removed := providerAccessSet(names)
	for _, candidate := range c.Desktop.ProviderAccess {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" || removed[candidate] {
			continue
		}
		p, ok := c.Provider(candidate)
		if ok && p.Configured() && len(p.ModelList()) > 0 {
			return p.Name + "/" + p.DefaultModel()
		}
	}
	return ""
}

func providerRefMatchesAny(c *config.Config, ref string, names []string) bool {
	for _, name := range names {
		if desktopModelRefsProvider(c, ref, name) {
			return true
		}
	}
	return false
}

func retargetProviderReferences(c *config.Config, names []string, fallbackRef string) {
	fallbackRef = strings.TrimSpace(fallbackRef)
	if providerRefMatchesAny(c, c.DefaultModel, names) {
		if fallbackRef != "" {
			c.DefaultModel = fallbackRef
		}
	}
	if providerRefMatchesAny(c, c.Agent.PlannerModel, names) {
		c.Agent.PlannerModel = fallbackRef
	}
	if providerRefMatchesAny(c, c.Agent.SubagentModel, names) {
		c.Agent.SubagentModel = fallbackRef
	}
	for skill, ref := range c.Agent.SubagentModels {
		if providerRefMatchesAny(c, ref, names) {
			if fallbackRef == "" {
				delete(c.Agent.SubagentModels, skill)
			} else {
				c.Agent.SubagentModels[skill] = fallbackRef
			}
		}
	}
}

func providerReferencesAny(c *config.Config, names []string) bool {
	if providerRefMatchesAny(c, c.DefaultModel, names) ||
		providerRefMatchesAny(c, c.Agent.PlannerModel, names) ||
		providerRefMatchesAny(c, c.Agent.SubagentModel, names) {
		return true
	}
	for _, ref := range c.Agent.SubagentModels {
		if providerRefMatchesAny(c, ref, names) {
			return true
		}
	}
	return false
}

// providerAuxiliaryReferencesAny reports whether a controller's planner or
// sub-agent wiring was assembled from a provider being removed. Unlike
// default_model, these references affect the whole runtime regardless of the
// tab's selected chat model.
func providerAuxiliaryReferencesAny(c *config.Config, names []string) bool {
	if providerRefMatchesAny(c, c.Agent.PlannerModel, names) ||
		providerRefMatchesAny(c, c.Agent.SubagentModel, names) {
		return true
	}
	for _, ref := range c.Agent.SubagentModels {
		if providerRefMatchesAny(c, ref, names) {
			return true
		}
	}
	return false
}

func (a *App) planProviderRemoval(names []string, official bool) (providerRemovalPlan, error) {
	root := a.activeWorkspaceRoot()
	unlock, err := lockProviderRemovalState()
	if err != nil {
		return providerRemovalPlan{}, err
	}
	defer unlock()
	cfg, _, err := a.loadProviderRemovalConfigForEdit(root)
	if err != nil {
		return providerRemovalPlan{}, err
	}
	if official {
		if err := validateOfficialProviderRemoval(cfg, names); err != nil {
			return providerRemovalPlan{}, err
		}
	} else {
		p, ok := cfg.Provider(names[0])
		if !ok {
			return providerRemovalPlan{}, fmt.Errorf("remove provider: %q not found", names[0])
		}
		if isOfficialBuiltInProvider(*p) {
			return providerRemovalPlan{}, fmt.Errorf("remove provider: %q is now an official provider; retry", names[0])
		}
	}
	targets := names
	fallbackRef := providerAccessFallbackRef(cfg, targets)
	if official {
		targets = officialProviderRemovalTargets(names)
		fallbackRef = providerAccessFallbackRef(cfg, targets)
	}
	return providerRemovalPlan{
		config: cfg, root: root, targets: targets, fallbackRef: fallbackRef,
		fingerprint: providerRemovalStateFingerprint(cfg, providerCredentialsRevision()),
	}, nil
}

func (a *App) affectedProviderRemovalTabs(plan providerRemovalPlan, label, action string) ([]providerRemovalTab, bool, error) {
	a.mu.RLock()
	for _, tab := range a.detachedSessions {
		if tab != nil && tab.Ctrl != nil {
			a.mu.RUnlock()
			return nil, false, fmt.Errorf("background session is still using %q; reopen or close it before %s", label, action)
		}
	}
	visibleTabs := make([]*WorkspaceTab, 0, len(a.tabs))
	for _, id := range a.orderedTabIDsLocked() {
		if tab := a.tabs[id]; tab != nil {
			visibleTabs = append(visibleTabs, tab)
		}
	}
	a.mu.RUnlock()
	affected := make([]providerRemovalTab, 0, len(visibleTabs))
	referencesRemoved := providerReferencesAny(plan.config, plan.targets)
	for _, tab := range visibleTabs {
		snap := a.tabRuntimeSnapshot(tab)
		root := strings.TrimSpace(snap.workspaceRoot)
		if root == "" {
			root = "."
		}
		effective, err := config.LoadForRootReadOnly(root)
		if err != nil {
			return nil, false, fmt.Errorf("load workspace config before %s: %w", action, err)
		}
		ref := snap.model
		if strings.TrimSpace(ref) == "" {
			ref = effective.DefaultModel
		}
		retargetModel := providerRefMatchesAny(effective, ref, plan.targets)
		referencesRemoved = referencesRemoved || retargetModel || providerAuxiliaryReferencesAny(effective, plan.targets)
		if controllerHasActiveRuntimeWork(snap.ctrl) {
			return nil, false, fmt.Errorf("finish or cancel active work using %q before %s", label, action)
		}
		if snap.ctrl == nil && !retargetModel {
			// A controller-less placeholder has no stale runtime. Its first
			// build will read the updated global and project configuration.
			continue
		}
		// Provider configuration is user-global. Rebuild every visible live
		// controller so a non-active workspace cannot retain a project-level
		// planner or sub-agent reference that the active workspace does not use.
		affected = append(affected, providerRemovalTab{
			id: tab.ID, ctrl: snap.ctrl, readOnly: snap.readOnly, retargetModel: retargetModel,
		})
	}
	return affected, referencesRemoved, nil
}

func snapshotProviderRemovalTabs(affected []providerRemovalTab, label, action string) error {
	for _, item := range affected {
		if item.ctrl == nil || item.readOnly {
			continue
		}
		if err := item.ctrl.Snapshot(); err != nil {
			slog.Warn("desktop: snapshot before provider removal failed", "tab", item.id, "provider", label, "action", action, "err", err)
			return fmt.Errorf("save current session before %s: %w", action, err)
		}
	}
	return nil
}

func validateProviderRemovalFingerprint(fresh *config.Config, planned string) error {
	if providerRemovalStateFingerprint(fresh, providerCredentialsRevision()) != planned {
		return fmt.Errorf("provider configuration or credentials changed while removing access; retry")
	}
	return nil
}

func (a *App) commitOfficialProviderRemoval(plan providerRemovalPlan, names []string) (string, error) {
	unlock, err := lockProviderRemovalState()
	if err != nil {
		return "", err
	}
	defer unlock()
	fresh, path, err := a.loadProviderRemovalConfigForEdit(plan.root)
	if err != nil {
		return "", err
	}
	if err := validateOfficialProviderRemoval(fresh, names); err != nil {
		return "", fmt.Errorf("provider configuration changed while removing access; retry: %w", err)
	}
	if err := validateProviderRemovalFingerprint(fresh, plan.fingerprint); err != nil {
		return "", err
	}
	fallbackRef := providerAccessFallbackRef(fresh, plan.targets)
	retargetProviderReferences(fresh, plan.targets, fallbackRef)
	removeProviderAccess(fresh, plan.targets...)
	return fallbackRef, fresh.SaveTo(path)
}

func (a *App) commitCustomProviderRemoval(plan providerRemovalPlan, name string) (string, error) {
	unlock, err := lockProviderRemovalState()
	if err != nil {
		return "", err
	}
	defer unlock()
	fresh, path, err := a.loadProviderRemovalConfigForEdit(plan.root)
	if err != nil {
		return "", err
	}
	p, ok := fresh.Provider(name)
	if !ok {
		return "", fmt.Errorf("provider configuration changed while removing %q; retry", name)
	}
	if isOfficialBuiltInProvider(*p) {
		return "", fmt.Errorf("provider configuration changed while removing %q; it is now an official provider; retry", name)
	}
	if err := validateProviderRemovalFingerprint(fresh, plan.fingerprint); err != nil {
		return "", err
	}
	fallbackRef := providerAccessFallbackRef(fresh, plan.targets)
	// Config.RemoveProvider has a compatibility fallback across every configured
	// provider. Settings access removal is narrower: hidden providers must not
	// silently become the new default after restart. Retarget first so the
	// persisted config and every rebuilt tab use the same visible provider. Keep
	// the historical provider-only persisted form while the runtime uses the
	// exact provider/model reference returned below.
	persistedFallback := fallbackRef
	if providerName, _, ok := strings.Cut(fallbackRef, "/"); ok {
		persistedFallback = providerName
	}
	retargetProviderReferences(fresh, plan.targets, persistedFallback)
	if err := fresh.RemoveProvider(name); err != nil {
		return "", err
	}
	removeProviderAccess(fresh, name)
	return fallbackRef, fresh.SaveTo(path)
}

func (a *App) applyProviderRemovalRuntime(affected []providerRemovalTab, fallbackRef, setting string) error {
	if len(affected) == 0 {
		if err := a.rebuildActiveSettingRuntimeMutationLocked(setting); err != nil {
			if _, ok := a.deferredRebuildWarning(setting, err); ok {
				return nil
			}
			return err
		}
		return nil
	}
	reset := make([]providerRemovalTab, 0, len(affected))
	var rebuildErrs []error
	if a.ctx != nil {
		for _, item := range affected {
			tab := a.tabByID(item.id)
			if tab == nil || a.controllerForTab(tab) != item.ctrl {
				continue
			}
			if item.ctrl == nil {
				reset = append(reset, item)
				continue
			}
			// lockRuntimeTurnGates already holds this tab's turnStartMu, and the
			// outer runtime mutation owns runtimeRebuildMu plus admission. Reuse
			// the settings build-and-swap core without reacquiring either lock so
			// the old controller remains usable if replacement construction fails.
			modelOverride := ""
			if item.retargetModel {
				modelOverride = fallbackRef
			}
			if err := a.rebuildSettingTurnLockedWithModel(setting, tab, modelOverride, true, false); err != nil {
				if _, ok := a.deferredRebuildWarningForTab(setting, err, tab); ok {
					continue
				}
				rebuildErrs = append(rebuildErrs, err)
			}
		}
	} else {
		reset = affected
	}
	for _, item := range reset {
		if item.ctrl != nil {
			item.ctrl.Close()
		}
	}
	rebuildTabs, releasedHostKeys := a.resetProviderRemovalTabs(reset, fallbackRef)
	for _, key := range releasedHostKeys {
		a.releaseSharedHost(key)
	}
	for _, tab := range rebuildTabs {
		go a.buildTabController(tab)
	}
	return errors.Join(rebuildErrs...)
}

func (a *App) resetProviderRemovalTabs(affected []providerRemovalTab, fallbackRef string) ([]*WorkspaceTab, []string) {
	var rebuildTabs []*WorkspaceTab
	var releasedHostKeys []string
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, item := range affected {
		tab := a.tabs[item.id]
		if tab == nil || tab.Ctrl != item.ctrl {
			continue
		}
		tab.Ctrl = nil
		if key := takeTabSharedHostKey(tab); key != "" {
			releasedHostKeys = append(releasedHostKeys, key)
		}
		a.supersedeTabBuildLocked(tab)
		if item.retargetModel {
			tab.model = fallbackRef
			tab.Label = fallbackRef
		}
		clearTabStartupError(tab)
		tab.Ready = a.ctx == nil
		if a.ctx == nil {
			a.setSessionRuntimePhaseLocked(tab, sessionRuntimeFailed, fmt.Errorf("desktop runtime is not started"))
			continue
		}
		a.setSessionRuntimePhaseLocked(tab, sessionRuntimeStarting, nil)
		rebuildTabs = append(rebuildTabs, tab)
	}
	a.saveTabsLocked()
	return rebuildTabs, releasedHostKeys
}

func (a *App) removeBuiltInProviderAccessAndRetargetTabs(names []string) error {
	defer a.lockRuntimeMutation("remove-provider-access")()
	releaseGates, err := a.lockRuntimeTurnGates("provider access", nil)
	if err != nil {
		return err
	}
	defer releaseGates()

	plan, err := a.planProviderRemoval(names, true)
	if err != nil {
		return err
	}
	label := strings.Join(names, ", ")
	affected, referencesRemoved, err := a.affectedProviderRemovalTabs(plan, label, "removing the provider access")
	if err != nil {
		return err
	}
	if plan.fallbackRef == "" && referencesRemoved {
		return fmt.Errorf("remove provider access: %q is in use and no other configured provider exists", label)
	}
	if len(affected) == 0 {
		if err := a.ensureActiveTabRebuildAllowed("provider access"); err != nil {
			return err
		}
	}
	if err := snapshotProviderRemovalTabs(affected, label, "removing provider access"); err != nil {
		return err
	}
	fallbackRef, err := a.commitOfficialProviderRemoval(plan, names)
	if err != nil {
		return err
	}
	return a.applyProviderRemovalRuntime(affected, fallbackRef, "provider access")
}

func (a *App) deleteProviderAndRetargetTabs(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("remove provider: empty provider name")
	}
	defer a.lockRuntimeMutation("delete-provider")()
	releaseGates, err := a.lockRuntimeTurnGates("provider", nil)
	if err != nil {
		return err
	}
	defer releaseGates()

	plan, err := a.planProviderRemoval([]string{name}, false)
	if err != nil {
		return err
	}
	affected, referencesRemoved, err := a.affectedProviderRemovalTabs(plan, name, "deleting the provider")
	if err != nil {
		return err
	}
	if plan.fallbackRef == "" && referencesRemoved {
		return fmt.Errorf("remove provider: %q is in use and no other configured provider exists", name)
	}
	if len(affected) == 0 {
		if err := a.ensureActiveTabRebuildAllowed("provider"); err != nil {
			return err
		}
	}
	if err := snapshotProviderRemovalTabs(affected, name, "deleting provider"); err != nil {
		return err
	}
	fallbackRef, err := a.commitCustomProviderRemoval(plan, name)
	if err != nil {
		return err
	}
	return a.applyProviderRemovalRuntime(affected, fallbackRef, "provider")
}
