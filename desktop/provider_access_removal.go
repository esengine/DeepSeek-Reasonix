package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"

	"reasonix/internal/config"
)

type providerRemovalPlan struct {
	config      *config.Config
	root        string
	fingerprint string
	targets     []string
	fallbackRef string
}

// DeleteProvider commits removal; each runtime resolves its fallback next run.
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
	if len(names) > 1 && isAtomicCustomProviderGroup(cfg, names) {
		return a.deleteProvidersAndRetargetTabs(names)
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

// isAtomicCustomProviderGroup identifies custom provider families that are
// represented by one settings card but persisted as multiple routes.
// Keep this allowlist narrow: RemoveProviderAccesses intentionally rejects
// arbitrary custom-provider batches so callers cannot accidentally delete
// unrelated endpoints in one operation.
func isAtomicCustomProviderGroup(c *config.Config, names []string) bool {
	if c == nil || len(names) < 2 {
		return false
	}
	group := ""
	for _, name := range names {
		current := customProviderGroupKey(name)
		if current == "" || (group != "" && current != group) {
			return false
		}
		p, ok := c.Provider(name)
		if !ok || isOfficialBuiltInProvider(*p) {
			return false
		}
		group = current
	}
	return group != ""
}

func customProviderGroupKey(name string) string {
	switch strings.TrimSpace(name) {
	case "opencode-go", "opencode-go-anthropic", "opencode-go-responses",
		"opencode-go-deepseek-anthropic", "opencode-go-deepseek-responses":
		return "opencode-go"
	default:
		return ""
	}
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
	write("provider-removal-state-v3")
	write(credentialsRevision)
	write(c.DefaultModel)
	write(c.Agent.PlannerModel)
	write(c.Agent.VisionModel)
	write(c.Agent.GuardianModel)
	write(c.Agent.RecoveryModel)
	write(c.Agent.SubagentModel)
	write(c.Bot.Model)
	write(c.Bot.QQ.Model)
	write(c.Bot.Dingtalk.Model)
	for i := range c.Bot.Routes {
		write(c.Bot.Routes[i].Model)
	}
	for i := range c.Bot.Connections {
		write(c.Bot.Connections[i].Model)
	}
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
		c.DefaultModel = fallbackRef
	}
	if providerRefMatchesAny(c, c.Agent.PlannerModel, names) {
		c.Agent.PlannerModel = fallbackRef
	}
	if providerRefMatchesAny(c, c.Agent.VisionModel, names) {
		c.Agent.VisionModel = ""
	}
	if providerRefMatchesAny(c, c.Agent.GuardianModel, names) {
		c.Agent.GuardianModel = fallbackRef
	}
	if providerRefMatchesAny(c, c.Agent.RecoveryModel, names) {
		c.Agent.RecoveryModel = fallbackRef
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
	if providerRefMatchesAny(c, c.Bot.Model, names) {
		c.Bot.Model = fallbackRef
	}
	if providerRefMatchesAny(c, c.Bot.QQ.Model, names) {
		c.Bot.QQ.Model = fallbackRef
	}
	if providerRefMatchesAny(c, c.Bot.Dingtalk.Model, names) {
		c.Bot.Dingtalk.Model = fallbackRef
	}
	for i := range c.Bot.Routes {
		if providerRefMatchesAny(c, c.Bot.Routes[i].Model, names) {
			c.Bot.Routes[i].Model = fallbackRef
		}
	}
	for i := range c.Bot.Connections {
		if providerRefMatchesAny(c, c.Bot.Connections[i].Model, names) {
			c.Bot.Connections[i].Model = fallbackRef
		}
	}
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
		if len(names) > 1 && !isAtomicCustomProviderGroup(cfg, names) {
			return providerRemovalPlan{}, fmt.Errorf("remove provider: custom provider group is not supported")
		}
		for _, name := range names {
			p, ok := cfg.Provider(name)
			if !ok {
				return providerRemovalPlan{}, fmt.Errorf("remove provider: %q not found", name)
			}
			if isOfficialBuiltInProvider(*p) {
				return providerRemovalPlan{}, fmt.Errorf("remove provider: %q is now an official provider; retry", name)
			}
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
	baseline := fresh.ModelSettingsBaseline()
	fallbackRef := providerAccessFallbackRef(fresh, plan.targets)
	retargetProviderReferences(fresh, plan.targets, fallbackRef)
	removeProviderAccess(fresh, plan.targets...)
	return fallbackRef, fresh.SaveModelSettingsTo(path, baseline)
}

func (a *App) commitCustomProviderRemovals(plan providerRemovalPlan) (string, error) {
	unlock, err := lockProviderRemovalState()
	if err != nil {
		return "", err
	}
	defer unlock()
	fresh, path, err := a.loadProviderRemovalConfigForEdit(plan.root)
	if err != nil {
		return "", err
	}
	for _, name := range plan.targets {
		p, ok := fresh.Provider(name)
		if !ok {
			return "", fmt.Errorf("provider configuration changed while removing %q; retry", name)
		}
		if isOfficialBuiltInProvider(*p) {
			return "", fmt.Errorf("provider configuration changed while removing %q; it is now an official provider; retry", name)
		}
	}
	if err := validateProviderRemovalFingerprint(fresh, plan.fingerprint); err != nil {
		return "", err
	}
	baseline := fresh.ModelSettingsBaseline()
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
	for _, name := range plan.targets {
		if err := fresh.RemoveProvider(name); err != nil {
			return "", err
		}
	}
	removeProviderAccess(fresh, plan.targets...)
	return fallbackRef, fresh.SaveModelSettingsTo(path, baseline)
}

func (a *App) removeBuiltInProviderAccessAndRetargetTabs(names []string) error {
	plan, err := a.planProviderRemoval(names, true)
	if err != nil {
		return err
	}
	if _, err := a.commitOfficialProviderRemoval(plan, names); err != nil {
		return err
	}
	a.modelSettingsSaved("provider access")
	return nil
}

func (a *App) deleteProviderAndRetargetTabs(name string) error {
	return a.deleteProvidersAndRetargetTabs([]string{name})
}

func (a *App) deleteProvidersAndRetargetTabs(rawNames []string) error {
	names := uniqueNonEmptyStrings(rawNames)
	if len(names) == 0 {
		return fmt.Errorf("remove provider: empty provider name")
	}
	plan, err := a.planProviderRemoval(names, false)
	if err != nil {
		return err
	}
	if _, err := a.commitCustomProviderRemovals(plan); err != nil {
		return err
	}
	a.modelSettingsSaved("provider")
	return nil
}
