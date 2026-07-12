package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/boot"
	"reasonix/internal/botruntime"
	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/provider"
	"reasonix/internal/sandbox"
)

// settings_app.go is the desktop Settings panel's command surface: it reads the
// resolved config and applies edits through internal/config/edit.go (the
// purpose-built mutation API), then rebuilds the controller so the change takes
// effect live — the same snapshot→reload→resume pattern as SetModel. Secrets are
// the exception: they go to Reasonix's global .env (upsertDotEnv), since config
// stores only the env-var name, not the key.

// --- read ---

type ProviderView struct {
	Name              string                      `json:"name"`
	BuiltIn           bool                        `json:"builtIn"`
	Added             bool                        `json:"added"`
	Kind              string                      `json:"kind"`
	BaseURL           string                      `json:"baseUrl"`
	ChatURL           string                      `json:"chatUrl"`
	Models            []string                    `json:"models"`
	VisionModels      []string                    `json:"visionModels"`
	VisionModelsSet   bool                        `json:"visionModelsConfigured"`
	ModelsURL         string                      `json:"modelsUrl"`
	Default           string                      `json:"default"`
	APIKeyEnv         string                      `json:"apiKeyEnv"`
	Headers           map[string]string           `json:"headers"`
	ExtraBody         map[string]any              `json:"extraBody"`
	AuthHeader        bool                        `json:"authHeader"`
	KeySet            bool                        `json:"keySet"` // the env var currently resolves to a non-empty value
	RequiresKey       bool                        `json:"requiresKey"`
	Configured        bool                        `json:"configured"` // selectable: either key is present or no key is required
	KeySource         string                      `json:"keySource,omitempty"`
	KeySourcePath     string                      `json:"keySourcePath,omitempty"`
	BalanceURL        string                      `json:"balanceUrl"`
	ContextWindow     int                         `json:"contextWindow"`
	ReasoningProtocol string                      `json:"reasoningProtocol"`
	Thinking          string                      `json:"thinking"`
	SupportedEfforts  []string                    `json:"supportedEfforts"`
	DefaultEffort     string                      `json:"defaultEffort"`
	ModelOverrides    []ProviderModelOverrideView `json:"modelOverrides"`
}

type ProviderPresetView struct {
	ID                  string   `json:"id"`
	Label               string   `json:"label"`
	Description         string   `json:"description"`
	KeyEnv              string   `json:"keyEnv"`
	ProviderNames       []string `json:"providerNames"`
	Models              []string `json:"models"`
	Added               bool     `json:"added"`
	Status              string   `json:"status"`
	StatusProviderNames []string `json:"statusProviderNames"`
	KeySet              bool     `json:"keySet"`
	RequiresKey         bool     `json:"requiresKey"`
	Configured          bool     `json:"configured"`
	KeySource           string   `json:"keySource,omitempty"`
	KeySourcePath       string   `json:"keySourcePath,omitempty"`
}

const (
	providerPresetStatusAvailable         = "available"
	providerPresetStatusInstalled         = "installed"
	providerPresetStatusInstalledModified = "installed_modified"
	providerPresetStatusNameConflict      = "name_conflict"
	providerPresetStatusSimilarExisting   = "similar_existing"
)

type ProviderModelOverrideView struct {
	Model             string   `json:"model"`
	ReasoningProtocol string   `json:"reasoningProtocol"`
	Thinking          string   `json:"thinking"`
	SupportedEfforts  []string `json:"supportedEfforts"`
	DefaultEffort     string   `json:"defaultEffort"`
	Vision            *bool    `json:"vision"`
}

type PermissionsView struct {
	Mode  string   `json:"mode"`
	Allow []string `json:"allow"`
	Ask   []string `json:"ask"`
	Deny  []string `json:"deny"`
}

type SandboxView struct {
	Bash                   string   `json:"bash"`
	Network                bool     `json:"network"`
	WorkspaceRoot          string   `json:"workspaceRoot"`
	AllowWrite             []string `json:"allowWrite"`
	EffectiveWorkspaceRoot string   `json:"effectiveWorkspaceRoot"`
	EffectiveWriteRoots    []string `json:"effectiveWriteRoots"`
	Shell                  string   `json:"shell"` // [tools.shell] prefer: auto|bash|powershell|pwsh
	EffectiveShell         string   `json:"effectiveShell,omitempty"`
}

type NetworkProxyView struct {
	Type     string `json:"type"`
	Server   string `json:"server"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type NetworkView struct {
	ProxyMode string           `json:"proxyMode"`
	ProxyURL  string           `json:"proxyUrl"`
	NoProxy   string           `json:"noProxy"`
	Proxy     NetworkProxyView `json:"proxy"`
}

type AgentView struct {
	Temperature       float64 `json:"temperature"`
	MaxSteps          int     `json:"maxSteps"`
	PlannerMaxSteps   int     `json:"plannerMaxSteps"`
	MaxSubagentDepth  int     `json:"maxSubagentDepth"`
	SystemPrompt      string  `json:"systemPrompt"`
	ColdResumePrune   bool    `json:"coldResumePrune"`
	ReasoningLanguage string  `json:"reasoningLanguage"`
}

type BotAllowlistView struct {
	Enabled         bool     `json:"enabled"`
	AllowAll        bool     `json:"allowAll"`
	QQUsers         []string `json:"qqUsers"`
	FeishuUsers     []string `json:"feishuUsers"`
	WeixinUsers     []string `json:"weixinUsers"`
	QQApprovers     []string `json:"qqApprovers"`
	FeishuApprovers []string `json:"feishuApprovers"`
	WeixinApprovers []string `json:"weixinApprovers"`
	QQAdmins        []string `json:"qqAdmins"`
	FeishuAdmins    []string `json:"feishuAdmins"`
	WeixinAdmins    []string `json:"weixinAdmins"`
	QQGroups        []string `json:"qqGroups"`
	FeishuGroups    []string `json:"feishuGroups"`
	WeixinGroups    []string `json:"weixinGroups"`
}

type BotAccessView struct {
	Enabled        bool     `json:"enabled"`
	AllowAll       bool     `json:"allowAll"`
	PairingEnabled bool     `json:"pairingEnabled"`
	Users          []string `json:"users"`
	Groups         []string `json:"groups"`
	Approvers      []string `json:"approvers"`
	Admins         []string `json:"admins"`
}

type BotSelfUserIDsView struct {
	QQ     []string `json:"qq"`
	Feishu []string `json:"feishu"`
	Weixin []string `json:"weixin"`
}

type BotPairingView struct {
	Enabled               bool `json:"enabled"`
	RequestTTLMinutes     int  `json:"requestTtlMinutes"`
	MaxPendingPerPlatform int  `json:"maxPendingPerPlatform"`
}

type BotControlView struct {
	Enabled  bool   `json:"enabled"`
	Addr     string `json:"addr"`
	TokenEnv string `json:"tokenEnv"`
}

type BotRouteView struct {
	ConnectionID     string `json:"connectionId"`
	Platform         string `json:"platform"`
	ChatType         string `json:"chatType"`
	ChatID           string `json:"chatId"`
	UserID           string `json:"userId"`
	ThreadID         string `json:"threadId"`
	Model            string `json:"model"`
	ToolApprovalMode string `json:"toolApprovalMode"`
	WorkspaceRoot    string `json:"workspaceRoot"`
}

type QQBotView struct {
	Enabled          bool          `json:"enabled"`
	AppID            string        `json:"appId"`
	AppSecretEnv     string        `json:"appSecretEnv"`
	SecretSet        bool          `json:"secretSet"`
	Sandbox          bool          `json:"sandbox"`
	Model            string        `json:"model"`
	ToolApprovalMode string        `json:"toolApprovalMode"`
	WorkspaceRoot    string        `json:"workspaceRoot"`
	Access           BotAccessView `json:"access"`
}

type FeishuBotView struct {
	Enabled           bool   `json:"enabled"`
	Domain            string `json:"domain"`
	AppID             string `json:"appId"`
	AppSecretEnv      string `json:"appSecretEnv"`
	SecretSet         bool   `json:"secretSet"`
	VerificationToken string `json:"verificationToken"`
	Mode              string `json:"mode"`
	WebhookPort       int    `json:"webhookPort"`
	RequireMention    bool   `json:"requireMention"`
}

type WeixinBotView struct {
	Enabled   bool   `json:"enabled"`
	AccountID string `json:"accountId"`
	TokenEnv  string `json:"tokenEnv"`
	TokenSet  bool   `json:"tokenSet"`
	APIBase   string `json:"apiBase"`
}

type BotSettingsView struct {
	Enabled            bool                `json:"enabled"`
	Model              string              `json:"model"`
	ToolApprovalMode   string              `json:"toolApprovalMode"`
	MaxSteps           int                 `json:"maxSteps"`
	DebounceMs         int                 `json:"debounceMs"`
	QueueMode          string              `json:"queueMode"`
	QueueCap           int                 `json:"queueCap"`
	QueueDrop          string              `json:"queueDrop"`
	IgnoreSelfMessages bool                `json:"ignoreSelfMessages"`
	SelfUserIDs        BotSelfUserIDsView  `json:"selfUserIds"`
	Control            BotControlView      `json:"control"`
	Pairing            BotPairingView      `json:"pairing"`
	Routes             []BotRouteView      `json:"routes"`
	Allowlist          BotAllowlistView    `json:"allowlist"`
	QQ                 QQBotView           `json:"qq"`
	Feishu             FeishuBotView       `json:"feishu"`
	Weixin             WeixinBotView       `json:"weixin"`
	Connections        []BotConnectionView `json:"connections"`
}

// SettingsView is the whole Settings panel payload.
type SettingsView struct {
	DefaultModel            string               `json:"defaultModel"`
	PlannerModel            string               `json:"plannerModel"`
	SubagentModel           string               `json:"subagentModel"`
	SubagentEffort          string               `json:"subagentEffort"`
	AutoPlan                string               `json:"autoPlan"`
	Providers               []ProviderView       `json:"providers"`
	OfficialProviders       []ProviderView       `json:"officialProviders"`
	ProviderPresets         []ProviderPresetView `json:"providerPresets"`
	Permissions             PermissionsView      `json:"permissions"`
	Sandbox                 SandboxView          `json:"sandbox"`
	Network                 NetworkView          `json:"network"`
	Agent                   AgentView            `json:"agent"`
	Bot                     BotSettingsView      `json:"bot"`
	DesktopLanguage         string               `json:"desktopLanguage"`
	DesktopLayoutStyle      string               `json:"desktopLayoutStyle"`
	DesktopTheme            string               `json:"desktopTheme"`
	DesktopThemeStyle       string               `json:"desktopThemeStyle"`
	CloseBehavior           string               `json:"closeBehavior"`
	DisplayMode             string               `json:"displayMode"`
	StatusBarStyle          string               `json:"statusBarStyle"`
	StatusBarItems          []string             `json:"statusBarItems"`
	DefaultToolApprovalMode string               `json:"defaultToolApprovalMode"`
	CheckUpdates            bool                 `json:"checkUpdates"`
	Telemetry               bool                 `json:"telemetry"`
	Metrics                 bool                 `json:"metrics"`
	MemoryCompiler          bool                 `json:"memoryCompilerEnabled"`
	ExpandThinking          bool                 `json:"expandThinking"`
	ConfigPath              string               `json:"configPath"`
	// ProviderKinds lists the provider implementations the kernel actually
	// registered (provider.Kinds()), so the editor's "kind" picker offers only
	// kinds that resolve — selecting an unregistered one would fail the rebuild.
	ProviderKinds []string `json:"providerKinds"`
	// AutoApproveTools is the live YOLO/full-access state (runtime-only, not from
	// config), so the panel's toggle reflects whether tool approvals are currently
	// being skipped this session.
	AutoApproveTools bool `json:"autoApproveTools"`
	// Bypass is the legacy JSON key for the same live state.
	Bypass bool `json:"bypass"`
}

// DesktopStartupSettingsView is the lightweight Settings subset needed during
// frontend startup. It deliberately excludes providers and credential state so
// slow keychain/env resolution stays off the first-render path.
type DesktopStartupSettingsView struct {
	Bot                BotSettingsView `json:"bot"`
	DesktopLanguage    string          `json:"desktopLanguage"`
	DesktopLayoutStyle string          `json:"desktopLayoutStyle"`
	DesktopTheme       string          `json:"desktopTheme"`
	DesktopThemeStyle  string          `json:"desktopThemeStyle"`
	DisplayMode        string          `json:"displayMode"`
	StatusBarStyle     string          `json:"statusBarStyle"`
	StatusBarItems     []string        `json:"statusBarItems"`
	CheckUpdates       bool            `json:"checkUpdates"`
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func nonNilStringMap(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}

func nonNilAnyMap(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}

func providerModelOverridesForView(overrides map[string]config.ProviderModelOverride, models []string) []ProviderModelOverrideView {
	if len(overrides) == 0 {
		return []ProviderModelOverrideView{}
	}
	modelSet := map[string]bool{}
	for _, model := range models {
		modelSet[model] = true
	}
	keys := make([]string, 0, len(overrides))
	for model := range overrides {
		model = strings.TrimSpace(model)
		if model == "" {
			continue
		}
		if len(modelSet) > 0 && !modelSet[model] {
			continue
		}
		keys = append(keys, model)
	}
	sort.Strings(keys)
	out := make([]ProviderModelOverrideView, 0, len(keys))
	for _, model := range keys {
		ov := overrides[model]
		out = append(out, ProviderModelOverrideView{
			Model:             model,
			ReasoningProtocol: ov.ReasoningProtocol,
			SupportedEfforts:  nonNil(ov.SupportedEfforts),
			DefaultEffort:     ov.DefaultEffort,
			Vision:            ov.Vision,
		})
	}
	return out
}

func providerModelOverridesForSave(overrides []ProviderModelOverrideView, models []string) map[string]config.ProviderModelOverride {
	if len(overrides) == 0 {
		return nil
	}
	modelSet := map[string]bool{}
	for _, model := range models {
		modelSet[model] = true
	}
	out := map[string]config.ProviderModelOverride{}
	for _, item := range overrides {
		model := strings.TrimSpace(item.Model)
		if model == "" || (len(modelSet) > 0 && !modelSet[model]) {
			continue
		}
		ov := config.ProviderModelOverride{
			ReasoningProtocol: strings.TrimSpace(item.ReasoningProtocol),
			SupportedEfforts:  nonNil(item.SupportedEfforts),
			DefaultEffort:     strings.TrimSpace(item.DefaultEffort),
			Vision:            item.Vision,
		}
		if strings.TrimSpace(ov.ReasoningProtocol) == "" && len(ov.SupportedEfforts) == 0 && strings.TrimSpace(ov.DefaultEffort) == "" && ov.Vision == nil {
			continue
		}
		out[model] = ov
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func providerRemovalFallbackRef(c *config.Config, name string) string {
	for i := range c.Providers {
		p := &c.Providers[i]
		if p.Name == name || !p.Configured() || len(p.ModelList()) == 0 {
			continue
		}
		return p.Name + "/" + p.DefaultModel()
	}
	return ""
}

func desktopModelRefsProvider(c *config.Config, ref, name string) bool {
	if config.ModelRefsProvider(ref, name) {
		return true
	}
	if e, ok := c.ResolveModel(ref); ok {
		return e.Name == name
	}
	return false
}

func officialProviderHost(baseURL string) string {
	u, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

func officialProviderKindFromEntry(p config.ProviderEntry) string {
	host := officialProviderHost(p.BaseURL)
	switch config.CanonicalDesktopOfficialProviderName(p.Name) {
	case "deepseek":
		if host == "api.deepseek.com" {
			return "deepseek"
		}
	}
	return ""
}

func isOfficialBuiltInProvider(p config.ProviderEntry) bool {
	return officialProviderKindFromEntry(p) != ""
}

func providerAccessSet(names []string) map[string]bool {
	out := map[string]bool{}
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name != "" {
			out[name] = true
		}
	}
	return out
}

func addProviderAccess(c *config.Config, names ...string) {
	seen := providerAccessSet(c.Desktop.ProviderAccess)
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			continue
		}
		c.Desktop.ProviderAccess = append(c.Desktop.ProviderAccess, name)
		seen[name] = true
	}
}

func removeProviderAccess(c *config.Config, names ...string) {
	remove := providerAccessSet(names)
	if len(remove) == 0 {
		return
	}
	out := c.Desktop.ProviderAccess[:0]
	for _, name := range c.Desktop.ProviderAccess {
		if !remove[name] {
			out = append(out, name)
		}
	}
	c.Desktop.ProviderAccess = out
}

func providerViewFromEntry(p config.ProviderEntry, builtIn, added bool) ProviderView {
	return providerViewFromEntryForRoot(p, builtIn, added, ".")
}

func providerViewFromEntryForRoot(p config.ProviderEntry, builtIn, added bool, root string) ProviderView {
	return providerViewFromEntryForRootWithResolver(p, builtIn, added, root, nil)
}

func providerViewFromEntryForRootWithResolver(p config.ProviderEntry, builtIn, added bool, root string, resolver *config.CredentialResolver) ProviderView {
	models := p.ChatModelList()
	visionModels := p.VisionModels
	visionModelsSet := p.Vision || p.VisionModels != nil
	if p.Vision {
		visionModels = models
	}
	if resolver == nil {
		resolver = config.NewCredentialResolverForRoot(root)
	}
	key := resolver.ResolveGlobalFirst(p.APIKeyEnv)
	requiresKey := p.RequiresAPIKey()
	return ProviderView{
		Name: p.Name, BuiltIn: builtIn, Added: added, Kind: p.Kind, BaseURL: p.BaseURL, ChatURL: p.ChatURL,
		Models: nonNil(models), VisionModels: nonNil(providerVisionModels(models, visionModels)), VisionModelsSet: visionModelsSet, ModelsURL: p.ModelsURL, Default: p.DefaultModel(),
		APIKeyEnv:         p.APIKeyEnv,
		Headers:           nonNilStringMap(p.Headers),
		ExtraBody:         nonNilAnyMap(p.ExtraBody),
		AuthHeader:        p.AuthHeader,
		KeySet:            key.Set,
		RequiresKey:       requiresKey,
		Configured:        !requiresKey || key.Set,
		KeySource:         key.Source.Label,
		KeySourcePath:     key.Source.Path,
		BalanceURL:        p.BalanceURL,
		ContextWindow:     p.ContextWindow,
		ReasoningProtocol: p.ReasoningProtocol,
		Thinking:          providerThinkingForSettings(p.Thinking),
		SupportedEfforts:  nonNil(p.SupportedEfforts),
		DefaultEffort:     p.DefaultEffort,
		ModelOverrides:    providerModelOverridesForView(p.ModelOverrides, models),
	}
}

func providerThinkingForSettings(thinking string) string {
	normalized := strings.ToLower(strings.TrimSpace(thinking))
	switch normalized {
	case "enabled", "disabled", "adaptive":
		return normalized
	default:
		return ""
	}
}

func officialProviderViews(added map[string]bool, pricingLanguage string) []ProviderView {
	return officialProviderViewsForRoot(added, pricingLanguage, ".")
}

func officialProviderViewsForRoot(added map[string]bool, pricingLanguage, root string) []ProviderView {
	return officialProviderViewsForRootWithResolver(added, pricingLanguage, root, nil)
}

func officialProviderViewsForRootWithResolver(added map[string]bool, pricingLanguage, root string, resolver *config.CredentialResolver) []ProviderView {
	var out []ProviderView
	if resolver == nil {
		resolver = config.NewCredentialResolverForRoot(root)
	}
	for _, kind := range []string{"deepseek"} {
		entries, _, err := officialProviderTemplate(kind, pricingLanguage)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			out = append(out, providerViewFromEntryForRootWithResolver(entry, true, added[entry.Name], root, resolver))
		}
	}
	return out
}

func providerPresetViewsForRootWithResolver(cfg *config.Config, root string, resolver *config.CredentialResolver) []ProviderPresetView {
	if resolver == nil {
		resolver = config.NewCredentialResolverForRoot(root)
	}
	presets := config.CuratedProviderPresets()
	out := make([]ProviderPresetView, 0, len(presets))
	for _, preset := range presets {
		keyEnv := strings.TrimSpace(preset.KeyEnv)
		names := make([]string, 0, len(preset.Entries))
		models := make([]string, 0)
		modelSeen := map[string]bool{}
		requiresKey := false
		for _, entry := range preset.Entries {
			if keyEnv == "" {
				keyEnv = strings.TrimSpace(entry.APIKeyEnv)
			}
			if entry.RequiresAPIKey() {
				requiresKey = true
			}
			name := strings.TrimSpace(entry.Name)
			if name != "" {
				names = append(names, name)
			}
			for _, model := range chatProviderModels(entry.ChatModelList()) {
				if modelSeen[model] {
					continue
				}
				modelSeen[model] = true
				models = append(models, model)
			}
		}
		key := config.CredentialResolution{}
		if keyEnv != "" {
			key = resolver.ResolveGlobalFirst(keyEnv)
		}
		status, statusNames := classifyProviderPresetStatus(cfg, preset)
		added := status == providerPresetStatusInstalled || status == providerPresetStatusInstalledModified || status == providerPresetStatusNameConflict
		out = append(out, ProviderPresetView{
			ID:                  preset.ID,
			Label:               preset.Label,
			Description:         preset.Description,
			KeyEnv:              keyEnv,
			ProviderNames:       nonNil(names),
			Models:              nonNil(models),
			Added:               added,
			Status:              status,
			StatusProviderNames: nonNil(statusNames),
			KeySet:              key.Set,
			RequiresKey:         requiresKey,
			Configured:          !requiresKey || key.Set,
			KeySource:           key.Source.Label,
			KeySourcePath:       key.Source.Path,
		})
	}
	return out
}

func classifyProviderPresetStatus(cfg *config.Config, preset config.ProviderPreset) (string, []string) {
	if cfg == nil {
		return providerPresetStatusAvailable, nil
	}
	installed := make([]string, 0)
	modified := make([]string, 0)
	conflicts := make([]string, 0)
	similar := make([]string, 0)
	presetID := strings.TrimSpace(preset.ID)
	for _, entry := range preset.Entries {
		name := strings.TrimSpace(entry.Name)
		if name == "" {
			continue
		}
		existing, ok := cfg.Provider(name)
		if !ok {
			continue
		}
		if providerEntryMatchesPreset(*existing, entry, presetID) {
			installed = append(installed, name)
		} else if providerEntryUsesPresetID(*existing, presetID) {
			modified = append(modified, name)
		} else {
			conflicts = append(conflicts, name)
		}
	}
	if len(conflicts) > 0 {
		return providerPresetStatusNameConflict, uniqueNonEmptyStrings(conflicts)
	}
	if len(modified) > 0 {
		return providerPresetStatusInstalledModified, uniqueNonEmptyStrings(modified)
	}
	if len(installed) > 0 {
		return providerPresetStatusInstalled, uniqueNonEmptyStrings(installed)
	}
	for i := range cfg.Providers {
		existing := cfg.Providers[i]
		existingName := strings.TrimSpace(existing.Name)
		if existingName == "" {
			continue
		}
		for _, entry := range preset.Entries {
			if existingName == strings.TrimSpace(entry.Name) {
				continue
			}
			if providerEntrySimilarToPreset(existing, entry, presetID) {
				similar = append(similar, existingName)
				break
			}
		}
	}
	if len(similar) > 0 {
		return providerPresetStatusSimilarExisting, uniqueNonEmptyStrings(similar)
	}
	return providerPresetStatusAvailable, nil
}

func providerEntryMatchesPreset(existing, preset config.ProviderEntry, presetID string) bool {
	if strings.TrimSpace(existing.PresetID) != "" {
		if providerEntryUsesPresetID(existing, presetID) {
			return providerEntryCoreMatches(existing, preset)
		}
		return false
	}
	return providerEntryCoreMatches(existing, preset)
}

func providerEntrySimilarToPreset(existing, preset config.ProviderEntry, presetID string) bool {
	if providerEntryUsesPresetID(existing, presetID) {
		return true
	}
	return providerEntryCoreMatches(existing, preset)
}

func providerEntryUsesPresetID(existing config.ProviderEntry, presetID string) bool {
	presetID = strings.TrimSpace(presetID)
	return presetID != "" && strings.TrimSpace(existing.PresetID) == presetID
}

func providerEntryCoreMatches(existing, preset config.ProviderEntry) bool {
	return strings.EqualFold(strings.TrimSpace(existing.Kind), strings.TrimSpace(preset.Kind)) &&
		normalizeProviderURL(existing.BaseURL) == normalizeProviderURL(preset.BaseURL) &&
		strings.TrimSpace(existing.ChatURL) == strings.TrimSpace(preset.ChatURL) &&
		strings.TrimSpace(existing.APIKeyEnv) == strings.TrimSpace(preset.APIKeyEnv) &&
		existing.AuthHeader == preset.AuthHeader
}

func normalizeProviderURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err == nil && u.Scheme != "" && u.Host != "" {
		u.Scheme = strings.ToLower(u.Scheme)
		u.Host = strings.ToLower(u.Host)
		u.Path = strings.TrimRight(u.Path, "/")
		u.RawPath = ""
		u.RawQuery = ""
		u.Fragment = ""
		return strings.TrimRight(u.String(), "/")
	}
	return strings.TrimRight(raw, "/")
}

func uniqueNonEmptyStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

func officialProviderAddedSet(cfg *config.Config) map[string]bool {
	out := map[string]bool{}
	if cfg == nil {
		return out
	}
	access := providerAccessSet(cfg.Desktop.ProviderAccess)
	for i := range cfg.Providers {
		p := cfg.Providers[i]
		if !access[p.Name] {
			continue
		}
		if kind := officialProviderKindFromEntry(p); kind != "" {
			out[kind] = true
		}
	}
	return out
}

func desktopStartupSettingsFromConfig(cfg *config.Config) DesktopStartupSettingsView {
	if cfg == nil {
		return DesktopStartupSettingsView{
			Bot:                botSettingsView(config.BotConfig{}),
			DesktopLayoutStyle: "workbench",
			DesktopTheme:       "auto",
			DesktopThemeStyle:  "graphite",
			DisplayMode:        "standard",
			StatusBarStyle:     "text",
			StatusBarItems:     config.DefaultDesktopStatusBarItems(),
			CheckUpdates:       true,
		}
	}
	return DesktopStartupSettingsView{
		Bot:                botSettingsView(cfg.Bot),
		DesktopLanguage:    cfg.DesktopLanguage(),
		DesktopLayoutStyle: cfg.DesktopLayoutStyle(),
		DesktopTheme:       cfg.DesktopTheme(),
		DesktopThemeStyle:  cfg.DesktopThemeStyle(),
		DisplayMode:        cfg.DesktopDisplayMode(),
		StatusBarStyle:     cfg.DesktopStatusBarStyle(),
		StatusBarItems:     cfg.DesktopStatusBarItems(),
		CheckUpdates:       cfg.DesktopCheckUpdates(),
	}
}

// DesktopStartupSettings returns only the desktop chrome preferences needed at
// app startup. Keep provider/key status in Settings(), where the Settings panel
// actually needs it.
func (a *App) DesktopStartupSettings() DesktopStartupSettingsView {
	cfg, _, err := a.loadDesktopUserConfigForView()
	if err != nil {
		return desktopStartupSettingsFromConfig(nil)
	}
	return desktopStartupSettingsFromConfig(cfg)
}

// Settings returns the current configuration for the Settings panel.
func (a *App) Settings() SettingsView {
	cfg, cfgPath, err := a.loadDesktopUserConfigForView()
	if err != nil {
		return SettingsView{
			Providers:         []ProviderView{},
			OfficialProviders: officialProviderViews(map[string]bool{}, ""),
			ProviderPresets:   providerPresetViewsForRootWithResolver(nil, a.activeWorkspaceRoot(), nil),
			ProviderKinds:     nonNil(provider.Kinds()),
			Permissions: PermissionsView{
				Mode:  "ask",
				Allow: []string{},
				Ask:   []string{},
				Deny:  []string{},
			},
			Sandbox:                 SandboxView{Bash: config.Default().BashMode(), AllowWrite: []string{}, EffectiveWriteRoots: []string{}, Shell: "auto", EffectiveShell: sandboxEffectiveShellView(sandbox.ResolveShell("", "", nil))},
			Agent:                   AgentView{PlannerMaxSteps: 0, MaxSubagentDepth: agent.DefaultMaxSubagentDepth, ColdResumePrune: true, ReasoningLanguage: "auto"},
			Bot:                     botSettingsView(config.BotConfig{}),
			AutoPlan:                "off",
			DesktopLayoutStyle:      "workbench",
			DesktopTheme:            "auto",
			DesktopThemeStyle:       "graphite",
			CloseBehavior:           "background",
			DisplayMode:             "standard",
			StatusBarStyle:          "text",
			StatusBarItems:          config.DefaultDesktopStatusBarItems(),
			DefaultToolApprovalMode: "auto",
			CheckUpdates:            true,
			Telemetry:               true,
			Metrics:                 true,
			MemoryCompiler:          true,
			ExpandThinking:          false,
		}
	}
	ctrl := a.activeCtrl()
	bash := cfg.BashMode()
	shell := cfg.Tools.Shell.Prefer
	if shell == "" {
		shell = "auto"
	}
	root := a.activeWorkspaceRoot()
	writeRoots := cfg.WriteRootsForRoot(root)
	effectiveWorkspaceRoot := ""
	if len(writeRoots) > 0 {
		effectiveWorkspaceRoot = writeRoots[0]
	}
	effectiveShell := sandbox.ResolveShell(cfg.Tools.Shell.Prefer, cfg.Tools.Shell.Path, nil)
	v := SettingsView{
		DefaultModel:      cfg.DefaultModel,
		PlannerModel:      cfg.Agent.PlannerModel,
		SubagentModel:     cfg.Agent.SubagentModel,
		SubagentEffort:    cfg.Agent.SubagentEffort,
		AutoPlan:          desktopAutoPlanMode(cfg.Agent.AutoPlan),
		Providers:         []ProviderView{},
		OfficialProviders: []ProviderView{},
		ProviderPresets:   []ProviderPresetView{},
		Permissions: PermissionsView{
			Mode:  orDefault(cfg.Permissions.Mode, "ask"),
			Allow: nonNil(cfg.Permissions.Allow),
			Ask:   nonNil(cfg.Permissions.Ask),
			Deny:  nonNil(cfg.Permissions.Deny),
		},
		Sandbox: SandboxView{
			Bash: bash, Network: cfg.Sandbox.Network,
			WorkspaceRoot: cfg.Sandbox.WorkspaceRoot, AllowWrite: nonNil(cfg.Sandbox.AllowWrite),
			EffectiveWorkspaceRoot: effectiveWorkspaceRoot, EffectiveWriteRoots: nonNil(writeRoots),
			Shell: shell, EffectiveShell: sandboxEffectiveShellView(effectiveShell),
		},
		Network: NetworkView{
			ProxyMode: cfg.NetworkProxyMode(),
			ProxyURL:  cfg.Network.ProxyURL,
			NoProxy:   cfg.Network.NoProxy,
			Proxy: NetworkProxyView{
				Type:     orDefault(cfg.Network.Proxy.Type, "socks5"),
				Server:   cfg.Network.Proxy.Server,
				Port:     cfg.Network.Proxy.Port,
				Username: cfg.Network.Proxy.Username,
				Password: cfg.Network.Proxy.Password,
			},
		},
		Agent:                   AgentView{Temperature: cfg.Agent.Temperature, MaxSteps: cfg.Agent.MaxSteps, PlannerMaxSteps: cfg.Agent.PlannerMaxSteps, MaxSubagentDepth: desktopMaxSubagentDepth(cfg.Agent.MaxSubagentDepth), SystemPrompt: cfg.Agent.SystemPrompt, ColdResumePrune: cfg.ColdResumePruneEnabled(), ReasoningLanguage: cfg.ReasoningLanguage()},
		Bot:                     botSettingsView(cfg.Bot),
		DesktopLanguage:         cfg.DesktopLanguage(),
		DesktopLayoutStyle:      cfg.DesktopLayoutStyle(),
		DesktopTheme:            cfg.DesktopTheme(),
		DesktopThemeStyle:       cfg.DesktopThemeStyle(),
		CloseBehavior:           cfg.DesktopCloseBehavior(),
		DisplayMode:             cfg.DesktopDisplayMode(),
		StatusBarStyle:          cfg.DesktopStatusBarStyle(),
		StatusBarItems:          cfg.DesktopStatusBarItems(),
		DefaultToolApprovalMode: cfg.DesktopDefaultToolApprovalMode(),
		CheckUpdates:            cfg.DesktopCheckUpdates(),
		Telemetry:               cfg.DesktopTelemetry(),
		Metrics:                 cfg.DesktopMetrics(),
		MemoryCompiler:          cfg.MemoryCompilerEnabled(),
		ExpandThinking:          cfg.Desktop.ExpandThinking,
		ConfigPath:              cfgPath,
		ProviderKinds:           nonNil(provider.Kinds()),
		AutoApproveTools:        ctrl != nil && ctrl.AutoApproveTools(),
		Bypass:                  ctrl != nil && ctrl.AutoApproveTools(),
	}
	added := providerAccessSet(cfg.Desktop.ProviderAccess)
	resolver := config.NewCredentialResolverForRoot(root)
	v.OfficialProviders = officialProviderViewsForRootWithResolver(officialProviderAddedSet(cfg), cfg.DeepSeekOfficialPricingLanguage(), root, resolver)
	v.ProviderPresets = providerPresetViewsForRootWithResolver(cfg, root, resolver)
	for i := range cfg.Providers {
		p := &cfg.Providers[i]
		v.Providers = append(v.Providers, providerViewFromEntryForRootWithResolver(*p, isOfficialBuiltInProvider(*p), added[p.Name], root, resolver))
	}
	return v
}

func sandboxEffectiveShellView(sh sandbox.Shell) string {
	if sh.Kind == sandbox.ShellPowerShell {
		if sh.SupportsChaining() {
			return "pwsh"
		}
		return "powershell"
	}
	path := strings.ToLower(strings.ReplaceAll(sh.Path, "\\", "/"))
	if strings.Contains(path, "/git/") && strings.HasSuffix(path, "bash.exe") {
		return "git-bash"
	}
	return "bash"
}

func botSettingsView(b config.BotConfig) BotSettingsView {
	mode := strings.TrimSpace(b.Feishu.Mode)
	if mode == "" {
		mode = "webhook"
	}
	return BotSettingsView{
		Enabled:            b.Enabled,
		Model:              b.Model,
		ToolApprovalMode:   normalizeBotConnectionToolApprovalMode(b.ToolApprovalMode),
		MaxSteps:           b.MaxSteps,
		DebounceMs:         b.DebounceMs,
		QueueMode:          b.QueueMode,
		QueueCap:           b.QueueCap,
		QueueDrop:          b.QueueDrop,
		IgnoreSelfMessages: b.IgnoreSelfMessages,
		SelfUserIDs: BotSelfUserIDsView{
			QQ:     nonNil(b.SelfUserIDs.QQ),
			Feishu: nonNil(b.SelfUserIDs.Feishu),
			Weixin: nonNil(b.SelfUserIDs.Weixin),
		},
		Control: BotControlView{
			Enabled:  b.Control.Enabled,
			Addr:     b.Control.Addr,
			TokenEnv: b.Control.TokenEnv,
		},
		Pairing: BotPairingView{
			Enabled:               b.Pairing.Enabled,
			RequestTTLMinutes:     b.Pairing.RequestTTLMinutes,
			MaxPendingPerPlatform: b.Pairing.MaxPendingPerPlatform,
		},
		Routes: botRouteViews(b.Routes),
		Allowlist: BotAllowlistView{
			Enabled:         b.Allowlist.Enabled,
			AllowAll:        b.Allowlist.AllowAll,
			QQUsers:         nonNil(b.Allowlist.QQUsers),
			FeishuUsers:     nonNil(b.Allowlist.FeishuUsers),
			WeixinUsers:     nonNil(b.Allowlist.WeixinUsers),
			QQApprovers:     nonNil(b.Allowlist.QQApprovers),
			FeishuApprovers: nonNil(b.Allowlist.FeishuApprovers),
			WeixinApprovers: nonNil(b.Allowlist.WeixinApprovers),
			QQAdmins:        nonNil(b.Allowlist.QQAdmins),
			FeishuAdmins:    nonNil(b.Allowlist.FeishuAdmins),
			WeixinAdmins:    nonNil(b.Allowlist.WeixinAdmins),
			QQGroups:        nonNil(b.Allowlist.QQGroups),
			FeishuGroups:    nonNil(b.Allowlist.FeishuGroups),
			WeixinGroups:    nonNil(b.Allowlist.WeixinGroups),
		},
		QQ: QQBotView{
			Enabled:          b.QQ.Enabled,
			AppID:            b.QQ.AppID,
			AppSecretEnv:     b.QQ.AppSecretEnv,
			SecretSet:        strings.TrimSpace(b.QQ.AppSecretEnv) != "" && os.Getenv(b.QQ.AppSecretEnv) != "",
			Sandbox:          b.QQ.Sandbox,
			Model:            b.QQ.Model,
			ToolApprovalMode: normalizeBotConnectionToolApprovalMode(b.QQ.ToolApprovalMode),
			WorkspaceRoot:    b.QQ.WorkspaceRoot,
			Access:           botAccessViewFromConfig(b.QQ.Access),
		},
		Feishu: FeishuBotView{
			Enabled:           b.Feishu.Enabled,
			Domain:            orDefault(strings.TrimSpace(b.Feishu.Domain), "feishu"),
			AppID:             b.Feishu.AppID,
			AppSecretEnv:      b.Feishu.AppSecretEnv,
			SecretSet:         strings.TrimSpace(b.Feishu.AppSecretEnv) != "" && os.Getenv(b.Feishu.AppSecretEnv) != "",
			VerificationToken: b.Feishu.VerificationToken,
			Mode:              mode,
			WebhookPort:       b.Feishu.WebhookPort,
			RequireMention:    b.Feishu.RequireMention,
		},
		Weixin: WeixinBotView{
			Enabled:   b.Weixin.Enabled,
			AccountID: b.Weixin.AccountID,
			TokenEnv:  b.Weixin.TokenEnv,
			TokenSet:  strings.TrimSpace(b.Weixin.TokenEnv) != "" && os.Getenv(b.Weixin.TokenEnv) != "",
			APIBase:   b.Weixin.APIBase,
		},
		Connections: botConnectionViews(b.Connections),
	}
}

func orDefault(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}

func botRouteViews(routes []config.BotRouteConfig) []BotRouteView {
	if len(routes) == 0 {
		return []BotRouteView{}
	}
	out := make([]BotRouteView, 0, len(routes))
	for _, route := range routes {
		out = append(out, BotRouteView{
			ConnectionID:     route.ConnectionID,
			Platform:         route.Platform,
			ChatType:         route.ChatType,
			ChatID:           route.ChatID,
			UserID:           route.UserID,
			ThreadID:         route.ThreadID,
			Model:            route.Model,
			ToolApprovalMode: normalizeBotConnectionToolApprovalMode(route.ToolApprovalMode),
			WorkspaceRoot:    route.WorkspaceRoot,
		})
	}
	return out
}

func botRouteConfigs(routes []BotRouteView) []config.BotRouteConfig {
	if len(routes) == 0 {
		return nil
	}
	out := make([]config.BotRouteConfig, 0, len(routes))
	for _, route := range routes {
		cfg := config.BotRouteConfig{
			ConnectionID:     strings.TrimSpace(route.ConnectionID),
			Platform:         strings.TrimSpace(route.Platform),
			ChatType:         strings.TrimSpace(route.ChatType),
			ChatID:           strings.TrimSpace(route.ChatID),
			UserID:           strings.TrimSpace(route.UserID),
			ThreadID:         strings.TrimSpace(route.ThreadID),
			Model:            strings.TrimSpace(route.Model),
			ToolApprovalMode: normalizeBotConnectionToolApprovalMode(route.ToolApprovalMode),
			WorkspaceRoot:    strings.TrimSpace(route.WorkspaceRoot),
		}
		if cfg.ConnectionID == "" && cfg.Platform == "" && cfg.ChatType == "" && cfg.ChatID == "" && cfg.UserID == "" && cfg.ThreadID == "" &&
			cfg.Model == "" && cfg.ToolApprovalMode == "" && cfg.WorkspaceRoot == "" {
			continue
		}
		out = append(out, cfg)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func botAccessViewFromConfig(access config.BotAccessConfig) BotAccessView {
	return BotAccessView{
		Enabled:        access.Enabled,
		AllowAll:       access.AllowAll,
		PairingEnabled: access.PairingEnabled,
		Users:          nonNil(access.Users),
		Groups:         nonNil(access.Groups),
		Approvers:      nonNil(access.Approvers),
		Admins:         nonNil(access.Admins),
	}
}

func botAccessConfigFromView(access BotAccessView) config.BotAccessConfig {
	return config.BotAccessConfig{
		Enabled:        access.Enabled,
		AllowAll:       access.AllowAll,
		PairingEnabled: access.PairingEnabled,
		Users:          trimList(access.Users),
		Groups:         trimList(access.Groups),
		Approvers:      trimList(access.Approvers),
		Admins:         trimList(access.Admins),
	}
}

func botDomainOrDefault(domain string) string {
	if strings.EqualFold(strings.TrimSpace(domain), "lark") {
		return "lark"
	}
	return "feishu"
}

// --- apply (write config, then rebuild the controller so it's live) ---

// applyConfigChange mutates the user-global config and rebuilds the controller so
// the change takes effect this session. Desktop settings such as providers and
// keys are account-level, not per-project: writing them to the global config
// rather than the cwd's reasonix.toml is what lets them survive a workspace switch.
func (a *App) applyConfigChange(mutate func(*config.Config) error) error {
	_, err := a.applyConfigChangeWithWarning("settings", mutate)
	return err
}

func (a *App) applyConfigChangeWithWarning(setting string, mutate func(*config.Config) error) (string, error) {
	if err := a.ensureActiveTabRebuildAllowed(setting); err != nil {
		return "", err
	}
	if err := func() error {
		// Serialize the load-modify-save against other in-process config editors
		// (bot auto-session persistence, applyConfigOnly) so neither drops the
		// other's fields. rebuild() runs after unlocking — it does slow work and
		// must not hold the config edit lock.
		unlock := config.LockUserConfigEdits()
		defer unlock()
		cfg, path, err := a.loadDesktopUserConfigForEdit()
		if err != nil {
			return err
		}
		if err := mutate(cfg); err != nil {
			return err
		}
		return cfg.SaveTo(path)
	}(); err != nil {
		return "", err
	}
	if err := a.rebuildSetting(setting); err != nil {
		if warning, ok := a.deferredRebuildWarning(setting, err); ok {
			return warning, nil
		}
		return "", err
	}
	return "", nil
}

func (a *App) applyConfigOnly(mutate func(*config.Config) error) error {
	unlock := config.LockUserConfigEdits()
	defer unlock()
	cfg, path, err := a.loadDesktopUserConfigForEdit()
	if err != nil {
		return err
	}
	if err := mutate(cfg); err != nil {
		return err
	}
	return cfg.SaveTo(path)
}

func (a *App) ensureActiveTabRebuildAllowed(setting string) error {
	tab := a.activeTab()
	if tab == nil {
		if a.ctx == nil {
			return nil
		}
		return fmt.Errorf("no active tab")
	}
	if controllerHasActiveRuntimeWork(a.controllerForTab(tab)) {
		return rebuildControllerActiveWorkError(setting)
	}
	return nil
}

func (a *App) ensureLiveControllersRuntimeMutationAllowed(setting string) error {
	a.mu.RLock()
	defer a.mu.RUnlock()
	for _, tab := range a.tabs {
		if tab == nil {
			continue
		}
		if controllerHasActiveRuntimeWork(tab.Ctrl) {
			return rebuildControllerActiveWorkError(setting)
		}
	}
	return nil
}

func (a *App) deferredRebuildWarning(setting string, err error) (string, bool) {
	if err == nil || !errors.Is(err, agent.ErrSessionLeaseHeld) {
		return "", false
	}
	setting = strings.TrimSpace(setting)
	if setting == "" {
		setting = "settings"
	}
	userErr := userFacingSessionLeaseError(setting, err)
	warning := fmt.Sprintf("%s saved, but the current session could not refresh yet: %s", setting, userErr.Error())
	slog.Warn("desktop: deferred settings rebuild", "setting", setting, "err", err)
	// Bind both the warning and the retry to the tab whose refresh failed (the
	// rebuild acts on the active tab), so a tab switch right after the failure
	// cannot misroute the notice or the deferred rebuild.
	if tab := a.activeTab(); tab != nil {
		a.warnForTab(tab.ID, warning)
		a.scheduleDeferredRebuild(tab.ID, setting)
	}
	return warning, true
}

func appendSettingsWarning(existing, warning string) string {
	existing = strings.TrimSpace(existing)
	warning = strings.TrimSpace(warning)
	if existing == "" {
		return warning
	}
	if warning == "" {
		return existing
	}
	return existing + "\n" + warning
}

// loadDesktopUserConfigForEdit loads the user config for a write path and
// persists any pending one-time legacy migrations (provider-access normalize,
// legacy bot-config move) to disk as part of the load.
//
// Contract: the caller must already hold config.LockUserConfigEdits() across
// its whole load→mutate→SaveTo cycle, so the migration write-back cannot race
// other in-process config editors. This helper must never acquire that lock
// itself: applyConfigChange/applyConfigOnly (and every other caller) invoke it
// with the lock held, so an inner acquire would self-deadlock. Read-only
// callers must use loadDesktopUserConfigForView (or its WithCredentials
// variant), which never writes to disk.
func (a *App) loadDesktopUserConfigForEdit() (*config.Config, string, error) {
	userPath := config.UserConfigPath()
	if userPath == "" {
		return nil, "", fmt.Errorf("cannot resolve user config directory")
	}
	if _, err := os.Stat(userPath); err == nil {
		cfg := config.LoadForEdit(userPath)
		if err := normalizeLegacyDesktopProviderAccessForSettings(cfg, userPath); err != nil {
			return nil, "", err
		}
		if err := a.migrateLegacyBotConfigToUser(cfg, userPath); err != nil {
			return nil, "", err
		}
		return cfg, userPath, nil
	}
	cfg := config.LoadForEdit(userPath)
	legacyPath := config.SourcePathForRoot(a.activeWorkspaceRoot())
	if legacyPath == "" || sameConfigPath(legacyPath, userPath) {
		if err := normalizeLegacyDesktopProviderAccessForSettings(cfg, userPath); err != nil {
			return nil, "", err
		}
		return cfg, userPath, nil
	}
	legacyCfg := config.LoadForEdit(legacyPath)
	if err := normalizeLegacyDesktopProviderAccessForSettings(legacyCfg, legacyPath); err != nil {
		return nil, "", err
	}
	legacyCfg.ConfigVersion = config.Default().ConfigVersion
	if err := migrateLegacyBotConfigToUser(cfg, legacyCfg, userPath); err != nil {
		return nil, "", err
	}
	return legacyCfg, userPath, nil
}

// loadDesktopUserConfigForView loads the user config for read-only callers.
// Contract: it never writes to disk, so it is safe without
// config.LockUserConfigEdits(). Legacy migrations (provider-access normalize,
// legacy bot-config merge) are applied to the returned copy in memory only;
// the on-disk file migrates the first time a locked write path runs
// loadDesktopUserConfigForEdit. Credentials (Reasonix global .env) are not
// loaded; callers that hand the config to a runtime resolving secrets from the
// process env must use loadDesktopUserConfigForViewWithCredentials.
func (a *App) loadDesktopUserConfigForView() (*config.Config, string, error) {
	return a.loadDesktopUserConfigReadOnly(config.LoadForEditWithoutCredentials)
}

// loadDesktopUserConfigForViewWithCredentials is loadDesktopUserConfigForView
// plus credential resolution: like config.LoadForEdit it loads Reasonix's
// global .env into the process env. Use it for read-only loads whose result
// feeds a runtime that resolves env-based secrets — the bot runtime
// (app-secret/control-token envs) and MCP server connects. It still never
// writes to disk.
func (a *App) loadDesktopUserConfigForViewWithCredentials() (*config.Config, string, error) {
	return a.loadDesktopUserConfigReadOnly(config.LoadForEdit)
}

// loadDesktopUserConfigReadOnly is the shared pure-read loader behind the View
// variants: same shape as loadDesktopUserConfigForEdit, but every legacy
// migration stays in memory (zero SaveTo).
func (a *App) loadDesktopUserConfigReadOnly(load func(string) *config.Config) (*config.Config, string, error) {
	userPath := config.UserConfigPath()
	if userPath == "" {
		return nil, "", fmt.Errorf("cannot resolve user config directory")
	}
	if _, err := os.Stat(userPath); err == nil {
		cfg := load(userPath)
		normalizeLegacyDesktopProviderAccessInMemory(cfg, userPath)
		legacyPath := config.SourcePathForRoot(a.activeWorkspaceRoot())
		if legacyPath != "" && !sameConfigPath(legacyPath, userPath) {
			mergeLegacyBotConfigInMemory(cfg, load(legacyPath))
		}
		return cfg, userPath, nil
	}
	cfg := load(userPath)
	legacyPath := config.SourcePathForRoot(a.activeWorkspaceRoot())
	if legacyPath == "" || sameConfigPath(legacyPath, userPath) {
		normalizeLegacyDesktopProviderAccessInMemory(cfg, userPath)
		return cfg, userPath, nil
	}
	// The user config does not exist yet: serve the legacy config as the view.
	// It already carries any legacy bot config, so no merge is needed; the
	// write path creates the migrated user file later.
	legacyCfg := load(legacyPath)
	normalizeLegacyDesktopProviderAccessInMemory(legacyCfg, legacyPath)
	legacyCfg.ConfigVersion = config.Default().ConfigVersion
	return legacyCfg, userPath, nil
}

// migrateLegacyBotConfigToUser (method) is the write-path legacy bot-config
// migration against the active workspace's legacy config file. Callers must
// hold config.LockUserConfigEdits() (see loadDesktopUserConfigForEdit).
func (a *App) migrateLegacyBotConfigToUser(userCfg *config.Config, userPath string) error {
	if userCfg == nil {
		return nil
	}
	legacyPath := config.SourcePathForRoot(a.activeWorkspaceRoot())
	if legacyPath == "" || sameConfigPath(legacyPath, userPath) {
		return nil
	}
	legacyCfg := config.LoadForEdit(legacyPath)
	return migrateLegacyBotConfigToUser(userCfg, legacyCfg, userPath)
}

// migrateLegacyBotConfigToUser is the write-path variant: it merges the legacy
// bot config in memory and persists the result to userPath. Callers must hold
// config.LockUserConfigEdits() (see loadDesktopUserConfigForEdit). Read paths
// use mergeLegacyBotConfigInMemory instead.
func migrateLegacyBotConfigToUser(userCfg, legacyCfg *config.Config, userPath string) error {
	if !mergeLegacyBotConfigInMemory(userCfg, legacyCfg) {
		return nil
	}
	if err := userCfg.SaveTo(userPath); err != nil {
		return fmt.Errorf("migrate legacy bot config: %w", err)
	}
	return nil
}

// mergeLegacyBotConfigInMemory copies the legacy bot config onto userCfg when
// the user config has none of its own. It never touches disk; it reports
// whether userCfg changed (i.e. whether a write path should persist it).
func mergeLegacyBotConfigInMemory(userCfg, legacyCfg *config.Config) bool {
	if userCfg == nil || legacyCfg == nil || desktopBotConfigConfigured(userCfg.Bot) {
		return false
	}
	if !desktopBotConfigConfigured(legacyCfg.Bot) {
		return false
	}
	userCfg.Bot = legacyCfg.Bot
	return true
}

func desktopBotConfigConfigured(bot config.BotConfig) bool {
	defaults := config.Default().Bot
	if bot.Enabled || strings.TrimSpace(bot.Model) != "" || len(bot.Connections) > 0 {
		return true
	}
	if (bot.MaxSteps != 0 && bot.MaxSteps != defaults.MaxSteps) ||
		(bot.DebounceMs != 0 && bot.DebounceMs != defaults.DebounceMs) ||
		(strings.TrimSpace(bot.QueueMode) != "" && bot.QueueMode != defaults.QueueMode) ||
		(bot.QueueCap != 0 && bot.QueueCap != defaults.QueueCap) ||
		(strings.TrimSpace(bot.QueueDrop) != "" && bot.QueueDrop != defaults.QueueDrop) ||
		bot.IgnoreSelfMessages != defaults.IgnoreSelfMessages ||
		bot.Pairing.Enabled != defaults.Pairing.Enabled ||
		(bot.Pairing.RequestTTLMinutes != 0 && bot.Pairing.RequestTTLMinutes != defaults.Pairing.RequestTTLMinutes) ||
		(bot.Pairing.MaxPendingPerPlatform != 0 && bot.Pairing.MaxPendingPerPlatform != defaults.Pairing.MaxPendingPerPlatform) ||
		bot.Control.Enabled != defaults.Control.Enabled ||
		(strings.TrimSpace(bot.Control.Addr) != "" && bot.Control.Addr != defaults.Control.Addr) ||
		(strings.TrimSpace(bot.Control.TokenEnv) != "" && bot.Control.TokenEnv != defaults.Control.TokenEnv) ||
		len(bot.Routes) > 0 ||
		len(bot.SelfUserIDs.QQ)+len(bot.SelfUserIDs.Feishu)+len(bot.SelfUserIDs.Weixin) > 0 {
		return true
	}
	if bot.Allowlist.AllowAll ||
		len(bot.Allowlist.QQUsers)+len(bot.Allowlist.FeishuUsers)+len(bot.Allowlist.WeixinUsers) > 0 ||
		len(bot.Allowlist.QQApprovers)+len(bot.Allowlist.FeishuApprovers)+len(bot.Allowlist.WeixinApprovers) > 0 ||
		len(bot.Allowlist.QQAdmins)+len(bot.Allowlist.FeishuAdmins)+len(bot.Allowlist.WeixinAdmins) > 0 ||
		len(bot.Allowlist.QQGroups)+len(bot.Allowlist.FeishuGroups)+len(bot.Allowlist.WeixinGroups) > 0 {
		return true
	}
	if bot.QQ.Enabled ||
		strings.TrimSpace(bot.QQ.AppID) != "" ||
		bot.QQ.AppSecretEnv != defaults.QQ.AppSecretEnv ||
		bot.QQ.Sandbox != defaults.QQ.Sandbox ||
		strings.TrimSpace(bot.QQ.Model) != "" ||
		strings.TrimSpace(bot.QQ.ToolApprovalMode) != "" ||
		strings.TrimSpace(bot.QQ.WorkspaceRoot) != "" ||
		botruntime.BotAccessActive(bot.QQ.Access) {
		return true
	}
	if bot.Feishu.Enabled ||
		strings.TrimSpace(bot.Feishu.AppID) != "" ||
		bot.Feishu.Domain != defaults.Feishu.Domain ||
		bot.Feishu.AppSecretEnv != defaults.Feishu.AppSecretEnv ||
		strings.TrimSpace(bot.Feishu.VerificationToken) != "" ||
		bot.Feishu.Mode != defaults.Feishu.Mode ||
		bot.Feishu.WebhookPort != defaults.Feishu.WebhookPort ||
		bot.Feishu.RequireMention != defaults.Feishu.RequireMention {
		return true
	}
	if bot.Weixin.Enabled ||
		bot.Weixin.AccountID != defaults.Weixin.AccountID ||
		bot.Weixin.TokenEnv != defaults.Weixin.TokenEnv ||
		bot.Weixin.APIBase != defaults.Weixin.APIBase {
		return true
	}
	return false
}

// normalizeLegacyDesktopProviderAccessForSettings is the write-path variant:
// it normalizes in memory and persists the migrated form to path. Callers must
// hold config.LockUserConfigEdits() (see loadDesktopUserConfigForEdit). Read
// paths use normalizeLegacyDesktopProviderAccessInMemory instead.
func normalizeLegacyDesktopProviderAccessForSettings(cfg *config.Config, path string) error {
	if !normalizeLegacyDesktopProviderAccessInMemory(cfg, path) {
		return nil
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return cfg.SaveTo(path)
}

// normalizeLegacyDesktopProviderAccessInMemory seeds cfg.Desktop.ProviderAccess
// from configs written before Settings tracked explicit provider access. It
// never touches disk; it reports whether cfg now carries a normalized list
// that the file at path does not declare (i.e. whether a write path should
// persist it).
func normalizeLegacyDesktopProviderAccessInMemory(cfg *config.Config, path string) bool {
	if cfg == nil || len(cfg.Desktop.ProviderAccess) > 0 || configDeclaresProviderAccess(path) {
		return false
	}
	config.NormalizeLegacyDesktopProviderAccess(cfg)
	return len(cfg.Desktop.ProviderAccess) > 0 && strings.TrimSpace(path) != ""
}

func configDeclaresProviderAccess(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	body, err := readFileUTF8(path)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(body), "\n") {
		if before, _, ok := strings.Cut(line, "#"); ok {
			line = before
		}
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "provider_access") {
			rest := strings.TrimSpace(strings.TrimPrefix(line, "provider_access"))
			return strings.HasPrefix(rest, "=")
		}
	}
	return false
}

func (a *App) activeWorkspaceRoot() string {
	tab := a.activeTab()
	if tab != nil {
		a.reconcileTabWithPinnedSessionMeta(tab)
		if strings.TrimSpace(tab.WorkspaceRoot) != "" {
			return tab.WorkspaceRoot
		}
	}
	return "."
}

func (a *App) saveProviderCredential(apiKeyEnv, value string) (string, error) {
	apiKeyEnv = strings.TrimSpace(apiKeyEnv)
	value = strings.TrimSpace(value)
	if err := upsertDotEnv(apiKeyEnv, value); err != nil {
		return "", err
	}
	return providerCredentialSourceNotice(apiKeyEnv, value), nil
}

func providerCredentialSourceNotice(apiKeyEnv, value string) string {
	return ""
}

func projectConfigPathForRoot(root string) string {
	if strings.TrimSpace(root) == "" || root == "." {
		return "reasonix.toml"
	}
	return filepath.Join(root, "reasonix.toml")
}

func sameConfigPath(a, b string) bool {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if a == "" || b == "" {
		return false
	}
	aAbs, aErr := filepath.Abs(a)
	bAbs, bErr := filepath.Abs(b)
	if aErr == nil && bErr == nil {
		return filepath.Clean(aAbs) == filepath.Clean(bAbs)
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

// rebuild builds a replacement controller from the (just-changed) config and
// swaps it in only after the target session lease is available. The old
// controller stays usable if the rebuild fails.
func (a *App) rebuild() error {
	return a.rebuildSetting("settings")
}

func (a *App) rebuildSetting(setting string) error {
	if a.ctx == nil {
		return nil
	}
	// Serialize with SetModelForTab and the deferred-rebuild retry loop: two
	// concurrent build+swap sequences on the same tab leak the first-swapped
	// controller and double-close the old one.
	a.runtimeRebuildMu.Lock()
	defer a.runtimeRebuildMu.Unlock()
	return a.rebuildSettingLocked(setting)
}

// rebuildSettingLocked is rebuildSetting's body; callers must already hold
// runtimeRebuildMu. The deferred-rebuild retry loop calls this directly because
// it takes the lock across its lease probe.
func (a *App) rebuildSettingLocked(setting string) error {
	if a.ctx == nil {
		return nil
	}
	tab := a.activeTab()
	if tab == nil {
		return fmt.Errorf("no active tab")
	}
	if controllerHasActiveRuntimeWork(a.controllerForTab(tab)) {
		return rebuildControllerActiveWorkError(setting)
	}
	if err := a.ensureTabControllerWorkspace(tab); err != nil {
		return err
	}
	prevPath := a.reconciledSessionPathForTab(tab)
	if prevPath == "" {
		prevPath = a.currentSessionPathFor(tab)
	}
	if a.controllerForTab(tab) == nil && prevPath != "" && a.attachExistingSessionRuntime(tab, prevPath, a.ctx) {
		prevPath = a.reconciledSessionPathForTab(tab)
		if prevPath == "" {
			prevPath = a.currentSessionPathFor(tab)
		}
	}
	if controllerHasActiveRuntimeWork(a.controllerForTab(tab)) {
		return rebuildControllerActiveWorkError(setting)
	}
	if err := a.ensureTabControllerWorkspace(tab); err != nil {
		return err
	}

	var carried []provider.Message
	oldCtrl := a.controllerForTab(tab)
	if oldCtrl != nil {
		if prevPath == "" {
			prevPath = oldCtrl.SessionPath()
		}
		if err := a.ensureTabSessionLeaseForRebuild(tab, prevPath, setting); err != nil {
			return err
		}
		if err := a.snapshotTabForAction(tab, "rebuilding settings"); err != nil {
			return err
		}
		prevPath = sessionPathAfterSnapshot(oldCtrl, prevPath)
		carried = oldCtrl.History()
	}
	snap := a.tabRuntimeSnapshot(tab)
	model := snap.model
	if cfg, err := config.LoadForRoot(snap.workspaceRoot); err == nil {
		if resolved, fallback, ok := cfg.ResolveModelWithFallback(model); ok {
			if fallback && strings.TrimSpace(model) != "" {
				a.noticeForTab(tab.ID, fmt.Sprintf("model %q is no longer available; switched to %s", model, resolved))
			}
			model = resolved
		}
	}
	sharedHost := a.lookupSharedHost(snap.sharedHostKey)
	ctrl, err := boot.Build(a.bootContext(), boot.Options{
		Model: model, RequireKey: false,
		Sink:                     snap.sink,
		WorkspaceRoot:            snap.workspaceRoot,
		SessionDir:               sessionDirForSnapshot(snap),
		EffortOverride:           cloneStringPtr(snap.effort),
		TokenMode:                snap.currentTokenMode(),
		SharedHost:               sharedHost,
		CleanupPendingReconciler: reconcileDesktopCleanupPending,
		SessionRecoveryMeta:      a.tabSessionRecoveryMeta(tab),
		OnSessionRecovered:       a.handleTabSessionRecovered(tab),
	})
	if err != nil {
		if oldCtrl == nil {
			leaseHeld := false
			a.mu.Lock()
			leaseHeld = setTabStartupError(tab, err)
			tab.Ready = true
			a.mu.Unlock()
			if leaseHeld {
				a.scheduleDeferredStartupBuild(tab.ID)
			}
			a.emitReady(a.ctx)
		}
		return err
	}
	a.bindControllerDisplayRecorder(ctrl)
	ctrl.EnableInteractiveApproval()
	applyTabModeToController(ctrl, snap.mode)
	// applyTabModeToController only encodes plan+yolo from tab.mode.
	// Apply the explicit toolApprovalMode (ask/auto/yolo) afterwards so
	// "auto" is not lost — otherwise rebuild would silently downgrade
	// auto to ask (#5424 regression).
	if mode := strings.TrimSpace(snap.toolApprovalMode); mode != "" {
		applyTabToolApprovalModeToController(ctrl, mode)
	}
	path := agent.ContinueSessionPath(prevPath, ctrl.SessionDir(), ctrl.Label())
	if err := a.ensureTabSessionLeaseForRebuild(tab, path, setting); err != nil {
		ctrl.Close()
		return err
	}
	resumeWithFreshSystemPrompt(ctrl, carried, path)
	if oldCtrl != nil {
		oldCtrl.Close()
	}
	a.persistTabSessionPath(tab, path)
	a.mu.Lock()
	if current := a.tabs[tab.ID]; current != tab {
		a.mu.Unlock()
		ctrl.Close()
		tab.releaseSessionLease()
		return fmt.Errorf("tab %q changed while rebuilding settings; retry", tab.ID)
	}
	tab.Ctrl = ctrl
	tab.model = model
	tab.Label = ctrl.Label()
	clearTabStartupError(tab)
	tab.Ready = true
	// Supersede any in-flight startup build: it would otherwise finish later,
	// pass its generation check, and overwrite the controller just installed.
	a.supersedeTabBuildLocked(tab)
	a.saveTabsLocked()
	a.mu.Unlock()
	a.clearDeferredRebuild(tab.ID)
	a.emitReady(a.ctx)
	return nil
}

// SetDefaultModel sets the config default and switches the live model to it.
func (a *App) SetDefaultModel(ref string) error {
	tab := a.activeTab()
	if tab == nil {
		return fmt.Errorf("no active tab")
	}
	// applyConfigChange ends in rebuild(), which reads tab.model to pick the
	// runtime model — the new ref must be visible on the tab before that runs.
	a.mu.Lock()
	prev := tab.model
	tab.model = ref
	a.mu.Unlock()
	if err := a.applyConfigChange(func(c *config.Config) error {
		resolved, err := selectableDesktopModelRef(c, ref)
		if err != nil {
			return err
		}
		c.DefaultModel = resolved
		a.mu.Lock()
		tab.model = resolved
		a.mu.Unlock()
		return nil
	}); err != nil {
		a.mu.Lock()
		tab.model = prev
		a.mu.Unlock()
		return err
	}
	return nil
}

// SetPlannerModel sets (or, with "", clears) the two-model planner.
func (a *App) SetPlannerModel(ref string) error {
	return a.applyConfigChange(func(c *config.Config) error {
		if ref != "" {
			resolved, err := selectableDesktopModelRef(c, ref)
			if err != nil {
				return err
			}
			ref = resolved
		}
		c.Agent.PlannerModel = ref
		return nil
	})
}

// SetSubagentModel sets (or clears) the default model used by subagent entry points.
func (a *App) SetSubagentModel(ref string) error {
	return a.applyConfigChange(func(c *config.Config) error {
		ref = strings.TrimSpace(ref)
		if ref != "" {
			resolved, err := selectableDesktopModelRef(c, ref)
			if err != nil {
				return err
			}
			ref = resolved
		}
		c.Agent.SubagentModel = ref
		return nil
	})
}

func selectableDesktopModelRef(c *config.Config, ref string) (string, error) {
	entry, ok := c.ResolveModel(ref)
	if !ok {
		return "", fmt.Errorf("unknown model %q", ref)
	}
	if !modelProviderAccessAllowed(providerAccessSet(c.Desktop.ProviderAccess), entry.Name) {
		return "", fmt.Errorf("model %q is not available because provider %q is not added", ref, entry.Name)
	}
	if !entry.Configured() {
		return "", fmt.Errorf("model %q is not available because provider %q has no key", ref, entry.Name)
	}
	return entry.Name + "/" + entry.Model, nil
}

// SetSubagentEffort sets (or clears) the default effort used by subagent entry points.
func (a *App) SetSubagentEffort(level string) error {
	return a.applyConfigChange(func(c *config.Config) error {
		level = strings.TrimSpace(level)
		if level == "" || level == "auto" {
			c.Agent.SubagentEffort = ""
			return nil
		}
		model := strings.TrimSpace(c.Agent.SubagentModel)
		if model == "" {
			model = c.DefaultModel
		}
		entry, ok := c.ResolveModel(model)
		if !ok {
			return fmt.Errorf("unknown subagent model %q", model)
		}
		effort, err := config.NormalizeEffort(entry, level)
		if err != nil {
			return err
		}
		c.Agent.SubagentEffort = effort
		return nil
	})
}

// deleteSubagentOverrideAliases removes every underscore/hyphen alias entry
// for name (boot.SubagentModelKeys — the same key set runtime dispatch
// reads). Deleting only the exact key would leave a legacy alias entry (e.g.
// `security_review` for the security-review skill) silently active.
func deleteSubagentOverrideAliases(overrides map[string]string, name string) {
	for _, key := range boot.SubagentModelKeys(name) {
		delete(overrides, key)
	}
}

// SetSubagentProfileModel sets (or clears) a per-name model override for a
// subagent — the only way to influence a built-in subagent's model in the
// Subagents settings page, since built-ins have no editable frontmatter file
// to carry a `model:` line. Writes into the same cfg.Agent.SubagentModels map
// internal/boot's subagentModelRef already reads at dispatch time. Set and
// clear both sweep the underscore/hyphen alias keys so a legacy alias entry
// can neither shadow the new value nor survive a clear.
func (a *App) SetSubagentProfileModel(name, ref string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("name is required")
	}
	return a.applyConfigChange(func(c *config.Config) error {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			deleteSubagentOverrideAliases(c.Agent.SubagentModels, name)
			return nil
		}
		resolved, err := selectableDesktopModelRef(c, ref)
		if err != nil {
			return err
		}
		if c.Agent.SubagentModels == nil {
			c.Agent.SubagentModels = map[string]string{}
		}
		deleteSubagentOverrideAliases(c.Agent.SubagentModels, name)
		c.Agent.SubagentModels[name] = resolved
		return nil
	})
}

// SetSubagentProfileEffort sets (or clears) a per-name effort override. See
// SetSubagentProfileModel.
func (a *App) SetSubagentProfileEffort(name, level string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("name is required")
	}
	return a.applyConfigChange(func(c *config.Config) error {
		level = strings.TrimSpace(level)
		if level == "" || level == "auto" {
			deleteSubagentOverrideAliases(c.Agent.SubagentEfforts, name)
			return nil
		}
		// Validate against the model the override will actually apply to:
		// the alias-aware per-name model override first, then the global
		// subagent default, then the session default.
		model := subagentOverrideFor(c.Agent.SubagentModels, name)
		if model == "" {
			model = strings.TrimSpace(c.Agent.SubagentModel)
		}
		if model == "" {
			model = c.DefaultModel
		}
		entry, ok := c.ResolveModel(model)
		if !ok {
			return fmt.Errorf("unknown subagent model %q", model)
		}
		effort, err := config.NormalizeEffort(entry, level)
		if err != nil {
			return err
		}
		if c.Agent.SubagentEfforts == nil {
			c.Agent.SubagentEfforts = map[string]string{}
		}
		deleteSubagentOverrideAliases(c.Agent.SubagentEfforts, name)
		c.Agent.SubagentEfforts[name] = effort
		return nil
	})
}

func desktopMaxSubagentDepth(depth int) int {
	if depth <= 0 {
		return agent.DefaultMaxSubagentDepth
	}
	if depth == 1 {
		return 1
	}
	return agent.DefaultMaxSubagentDepth
}

// SetMaxSubagentDepth controls whether first-layer subagents may delegate once more.
func (a *App) SetMaxSubagentDepth(depth int) error {
	return a.applyConfigChange(func(c *config.Config) error {
		c.Agent.MaxSubagentDepth = desktopMaxSubagentDepth(depth)
		return nil
	})
}

// SetAutoPlan updates the automatic plan-mode gate (off|on).
func (a *App) SetAutoPlan(mode string) error {
	if err := a.ensureLiveControllersRuntimeMutationAllowed("auto-plan"); err != nil {
		return err
	}
	var cfg *config.Config
	// Lock only the load-modify-save cycle; the live-controller fan-out and the
	// optional rebuild below are slow and must not hold the config edit lock.
	if err := func() error {
		unlock := config.LockUserConfigEdits()
		defer unlock()
		loaded, path, err := a.loadDesktopUserConfigForEdit()
		if err != nil {
			return err
		}
		if err := loaded.SetAutoPlan(mode); err != nil {
			return err
		}
		if err := loaded.SaveTo(path); err != nil {
			return err
		}
		cfg = loaded
		return nil
	}(); err != nil {
		return err
	}
	a.applyAutoPlanToLiveControllers(cfg.Agent.AutoPlan)
	if desktopAutoPlanMode(cfg.Agent.AutoPlan) == "on" && strings.TrimSpace(cfg.Agent.AutoPlanClassifier) != "" {
		if err := a.rebuild(); err != nil {
			if _, ok := a.deferredRebuildWarning("auto-plan", err); ok {
				return nil
			}
			return err
		}
	}
	return nil
}

// SetDefaultToolApprovalMode updates the global Ask/Auto/YOLO default used only
// for newly-created desktop sessions. Existing tabs keep their persisted mode.
func (a *App) SetDefaultToolApprovalMode(mode string) error {
	return a.applyConfigOnly(func(c *config.Config) error {
		return c.SetDesktopDefaultToolApprovalMode(mode)
	})
}

// SetMemoryCompilerEnabled toggles the Memory v5 execution compiler.
func (a *App) SetMemoryCompilerEnabled(enabled bool) error {
	// Lock only the load-modify-save cycle; the live-controller fan-out below
	// must not hold the config edit lock.
	if err := func() error {
		unlock := config.LockUserConfigEdits()
		defer unlock()
		cfg, path, err := a.loadDesktopUserConfigForEdit()
		if err != nil {
			return err
		}
		if err := cfg.SetMemoryCompilerEnabled(enabled); err != nil {
			return err
		}
		return cfg.SaveTo(path)
	}(); err != nil {
		return err
	}
	a.applyMemoryCompilerToLiveControllers(enabled)
	return nil
}

func (a *App) applyMemoryCompilerToLiveControllers(enabled bool) {
	if a == nil {
		return
	}
	var controllers []memoryCompilerTarget
	a.mu.RLock()
	for _, id := range a.orderedTabIDsLocked() {
		tab := a.tabs[id]
		if tab == nil || tab.Ctrl == nil {
			continue
		}
		controllers = append(controllers, tab.Ctrl)
	}
	a.mu.RUnlock()
	applyMemoryCompilerToControllers(enabled, controllers)
}

type memoryCompilerTarget interface {
	SetMemoryCompilerEnabled(enabled bool)
}

func applyMemoryCompilerToControllers(enabled bool, controllers []memoryCompilerTarget) {
	for _, ctrl := range controllers {
		if ctrl != nil {
			ctrl.SetMemoryCompilerEnabled(enabled)
		}
	}
}

func desktopAutoPlanMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "on", "ask":
		return "on"
	default:
		return "off"
	}
}

func (a *App) applyAutoPlanToLiveControllers(fallback string) {
	type liveTab struct {
		root string
		ctrl control.SessionAPI
	}
	var tabs []liveTab
	a.mu.RLock()
	for _, tab := range a.tabs {
		if tab != nil && tab.Ctrl != nil {
			tabs = append(tabs, liveTab{root: tab.WorkspaceRoot, ctrl: tab.Ctrl})
		}
	}
	a.mu.RUnlock()
	for _, tab := range tabs {
		mode := fallback
		if cfg, err := config.LoadForRoot(tab.root); err == nil {
			mode = cfg.Agent.AutoPlan
		}
		tab.ctrl.SetAutoPlan(mode)
	}
}

func officialProviderTemplate(kind, pricingLanguage string) ([]config.ProviderEntry, string, error) {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "deepseek", "deepseek-official":
		return []config.ProviderEntry{{
			Name:          "deepseek",
			Kind:          "openai",
			BaseURL:       "https://api.deepseek.com",
			Models:        []string{"deepseek-v4-flash", "deepseek-v4-pro"},
			Default:       "deepseek-v4-flash",
			APIKeyEnv:     "DEEPSEEK_API_KEY",
			BalanceURL:    "https://api.deepseek.com/user/balance",
			ContextWindow: 1_000_000,
			Prices:        config.DeepSeekV4PricesForLanguage(pricingLanguage),
		}}, "DEEPSEEK_API_KEY", nil
	default:
		return nil, "", fmt.Errorf("unknown official provider template %q", kind)
	}
}

func chatProviderModels(models []string) []string {
	out := make([]string, 0, len(models))
	seen := map[string]bool{}
	for _, model := range models {
		model = strings.TrimSpace(model)
		if model == "" || seen[model] || !config.IsLikelyChatModel(model) {
			continue
		}
		seen[model] = true
		out = append(out, model)
	}
	return out
}

func providerVisionModels(models, visionModels []string) []string {
	enabled := map[string]bool{}
	for _, model := range models {
		enabled[model] = true
	}
	out := make([]string, 0, len(visionModels))
	for _, model := range chatProviderModels(visionModels) {
		if enabled[model] {
			out = append(out, model)
		}
	}
	return out
}

func providerDefaultForModels(currentDefault string, models []string) string {
	currentDefault = strings.TrimSpace(currentDefault)
	if currentDefault != "" {
		for _, model := range models {
			if model == currentDefault {
				return currentDefault
			}
		}
	}
	if len(models) > 0 {
		return models[0]
	}
	return ""
}

func saveProviderConfig(c *config.Config, p ProviderView) error {
	if c == nil {
		return fmt.Errorf("config is nil")
	}
	e := config.ProviderEntry{Name: p.Name}
	for i := range c.Providers {
		if c.Providers[i].Name == p.Name {
			e = c.Providers[i]
			break
		}
	}
	e.Name = p.Name
	e.Kind = p.Kind
	e.BaseURL = p.BaseURL
	e.ChatURL = strings.TrimSpace(p.ChatURL)
	e.ModelsURL = strings.TrimSpace(p.ModelsURL)
	e.APIKeyEnv = p.APIKeyEnv
	e.Headers = p.Headers
	e.ExtraBody = p.ExtraBody
	e.AuthHeader = p.AuthHeader
	e.BalanceURL = strings.TrimSpace(p.BalanceURL)
	e.ContextWindow = p.ContextWindow
	e.ReasoningProtocol = p.ReasoningProtocol
	e.Thinking = providerThinkingForSettings(p.Thinking)
	e.SupportedEfforts = p.SupportedEfforts
	e.DefaultEffort = p.DefaultEffort
	e.Model = ""
	e.Models = nil
	e.Default = ""
	e.VisionModels = nil
	models := chatProviderModels(p.Models)
	if len(models) > 0 {
		e.Model = models[0] // also satisfies validateProvider's model requirement
		e.Models = models
		e.ModelOverrides = providerModelOverridesForSave(p.ModelOverrides, models)
		if p.VisionModelsSet || len(p.VisionModels) > 0 {
			e.Vision = false
			e.VisionModels = providerVisionModels(models, p.VisionModels)
		}
		if len(models) > 1 {
			e.Default = providerDefaultForModels(p.Default, models)
		}
	} else {
		e.Vision = false
		e.VisionModels = nil
		e.ModelOverrides = nil
	}
	if err := c.UpsertProvider(e); err != nil {
		return err
	}
	addProviderAccess(c, p.Name)
	return nil
}

// SaveProvider adds or updates a provider. Enabled models are persisted through
// `models` even when only one model is selected, while `model` remains populated
// in-memory for validation/back-compat. The shared key/endpoint live on the entry.
func (a *App) SaveProvider(p ProviderView) error {
	return a.applyConfigChange(func(c *config.Config) error {
		return saveProviderConfig(c, p)
	})
}

// SaveProviderWithKey saves a custom provider and its credential as one settings
// transaction, then rebuilds once after both are visible to the runtime.
func (a *App) SaveProviderWithKey(p ProviderView, key string) (string, error) {
	apiKeyEnv := strings.TrimSpace(p.APIKeyEnv)
	if apiKeyEnv == "" {
		return "", fmt.Errorf("this provider has no api_key_env set")
	}
	if err := a.ensureActiveTabRebuildAllowed("provider"); err != nil {
		return "", err
	}
	warning, err := a.saveProviderCredential(apiKeyEnv, key)
	if err != nil {
		return "", err
	}
	if err := func() error {
		unlock := config.LockUserConfigEdits()
		defer unlock()
		cfg, path, err := a.loadDesktopUserConfigForEdit()
		if err != nil {
			return err
		}
		if err := saveProviderConfig(cfg, p); err != nil {
			return err
		}
		return cfg.SaveTo(path)
	}(); err != nil {
		return "", err
	}
	if err := a.rebuildSetting("provider"); err != nil {
		if rebuildWarning, ok := a.deferredRebuildWarning("provider", err); ok {
			return appendSettingsWarning(warning, rebuildWarning), nil
		}
		return "", err
	}
	return warning, nil
}

// AddOfficialProviderAccess adds one curated desktop provider template to the
// Settings > Model > Access list. The runtime default providers still exist
// independently; this only records the user's explicit access setup.
func (a *App) AddOfficialProviderAccess(kind, key string) (string, error) {
	// Read-only pre-read (pricing language); the actual write happens inside
	// applyConfigChange below, under the config edit lock.
	cfg, _, err := a.loadDesktopUserConfigForView()
	if err != nil {
		return "", err
	}
	entries, keyEnv, err := officialProviderTemplate(kind, cfg.DeepSeekOfficialPricingLanguage())
	if err != nil {
		return "", err
	}
	if err := a.ensureActiveTabRebuildAllowed("provider access"); err != nil {
		return "", err
	}
	keyWarning := ""
	if strings.TrimSpace(key) != "" && keyEnv != "" {
		var err error
		keyWarning, err = a.saveProviderCredential(keyEnv, key)
		if err != nil {
			return "", err
		}
	}
	rebuildWarning, err := a.applyConfigChangeWithWarning("provider access", func(c *config.Config) error {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			if err := c.UpsertProvider(e); err != nil {
				return err
			}
			names = append(names, e.Name)
		}
		addProviderAccess(c, names...)
		return nil
	})
	if err != nil {
		return "", err
	}
	return appendSettingsWarning(keyWarning, rebuildWarning), nil
}

// AddProviderPresetAccess installs one editable custom-provider preset. Unlike
// official built-ins, these entries are saved as normal providers so users can
// tweak endpoints, model lists, and capability overrides after the one-click
// setup path.
func (a *App) AddProviderPresetAccess(id, key string) (string, error) {
	preset, ok := config.CuratedProviderPreset(id)
	if !ok {
		return "", fmt.Errorf("unknown provider preset %q", id)
	}
	if len(preset.Entries) == 0 {
		return "", fmt.Errorf("provider preset %q has no provider entries", id)
	}
	if err := a.ensureActiveTabRebuildAllowed("provider access"); err != nil {
		return "", err
	}
	// Read-only duplicate-name pre-check; applyConfigChange re-checks under the
	// config edit lock before writing.
	cfg, _, err := a.loadDesktopUserConfigForView()
	if err != nil {
		return "", err
	}
	if existing := existingProviderNames(cfg, preset.Entries); len(existing) > 0 {
		return "", providerPresetAlreadyAddedError(preset.ID, existing)
	}
	keyEnv := strings.TrimSpace(preset.KeyEnv)
	if keyEnv == "" {
		for _, e := range preset.Entries {
			if keyEnv = strings.TrimSpace(e.APIKeyEnv); keyEnv != "" {
				break
			}
		}
	}
	keyWarning := ""
	if strings.TrimSpace(key) != "" && keyEnv != "" {
		var err error
		keyWarning, err = a.saveProviderCredential(keyEnv, key)
		if err != nil {
			return "", err
		}
	}
	rebuildWarning, err := a.applyConfigChangeWithWarning("provider access", func(c *config.Config) error {
		if existing := existingProviderNames(c, preset.Entries); len(existing) > 0 {
			return providerPresetAlreadyAddedError(preset.ID, existing)
		}
		names := make([]string, 0, len(preset.Entries))
		for _, e := range preset.Entries {
			if err := c.UpsertProvider(e); err != nil {
				return err
			}
			names = append(names, e.Name)
		}
		addProviderAccess(c, names...)
		return nil
	})
	if err != nil {
		return "", err
	}
	return appendSettingsWarning(keyWarning, rebuildWarning), nil
}

// ResetProviderPresetAccess intentionally overwrites same-name provider entries
// with the curated preset template. It only mutates config; provider secrets stay
// in Reasonix home .env under whichever api_key_env the resulting preset uses.
func (a *App) ResetProviderPresetAccess(id string) error {
	preset, ok := config.CuratedProviderPreset(id)
	if !ok {
		return fmt.Errorf("unknown provider preset %q", id)
	}
	if len(preset.Entries) == 0 {
		return fmt.Errorf("provider preset %q has no provider entries", id)
	}
	if err := a.ensureActiveTabRebuildAllowed("provider access"); err != nil {
		return err
	}
	// Read-only existence pre-check; applyConfigChange re-checks under the
	// config edit lock before writing.
	cfg, _, err := a.loadDesktopUserConfigForView()
	if err != nil {
		return err
	}
	if existing := existingProviderNames(cfg, preset.Entries); len(existing) == 0 {
		return providerPresetNoExistingProviderError(preset.ID)
	}
	return a.applyConfigChange(func(c *config.Config) error {
		if existing := existingProviderNames(c, preset.Entries); len(existing) == 0 {
			return providerPresetNoExistingProviderError(preset.ID)
		}
		names := make([]string, 0, len(preset.Entries))
		for _, e := range preset.Entries {
			if err := c.UpsertProvider(e); err != nil {
				return err
			}
			names = append(names, e.Name)
		}
		addProviderAccess(c, names...)
		return nil
	})
}

func existingProviderNames(c *config.Config, entries []config.ProviderEntry) []string {
	if c == nil || len(entries) == 0 {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := strings.TrimSpace(entry.Name)
		if name == "" {
			continue
		}
		if _, ok := c.Provider(name); ok {
			names = append(names, name)
		}
	}
	return names
}

func providerPresetAlreadyAddedError(id string, names []string) error {
	return fmt.Errorf("provider preset %q cannot be added because provider name(s) already exist: %s; edit, rename, or remove the existing provider before adding it again", id, strings.Join(names, ", "))
}

func providerPresetNoExistingProviderError(id string) error {
	return fmt.Errorf("provider preset %q cannot be reset because no same-name provider exists; add the preset instead", id)
}

// FetchProviderModels probes the provider's OpenAI-compatible model-list
// endpoint and returns the available model IDs. This is a settings-only helper:
// it never touches chat request serialization or provider-visible prompt data.
func (a *App) FetchProviderModels(p ProviderView) ([]string, error) {
	e := config.ProviderEntry{
		Name:       p.Name,
		Kind:       p.Kind,
		BaseURL:    p.BaseURL,
		ModelsURL:  strings.TrimSpace(p.ModelsURL),
		APIKeyEnv:  p.APIKeyEnv,
		Headers:    p.Headers,
		AuthHeader: p.AuthHeader,
	}
	e.ResolveAPIKeyForRoot(a.activeWorkspaceRoot())
	ctx, cancel := context.WithTimeout(a.reqCtx(), 15*time.Second)
	defer cancel()
	models, err := e.FetchModels(ctx)
	if err != nil {
		return []string{}, err
	}
	return nonNil(chatProviderModels(models)), nil
}

// DeleteProvider removes a provider and retargets open idle tabs that used it.
func (a *App) DeleteProvider(name string) error {
	return a.deleteProviderAndRetargetTabs(name)
}

// RemoveProviderAccess hides a provider from Settings > Model > Access and from
// settings model pickers. Built-in provider entries remain in the runtime config
// for back-compat, but visible defaults and idle tabs are retargeted away from
// the removed access entry when another accessed provider is available. Custom
// providers are deleted outright.
func (a *App) RemoveProviderAccess(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("remove provider access: empty provider name")
	}
	// Read-only dispatch check (built-in vs custom); the removal paths below
	// reload and write under the config edit lock.
	cfg, _, err := a.loadDesktopUserConfigForView()
	if err != nil {
		return err
	}
	if p, ok := cfg.Provider(name); ok && isOfficialBuiltInProvider(*p) {
		return a.removeBuiltInProviderAccessAndRetargetTabs(name)
	}
	return a.deleteProviderAndRetargetTabs(name)
}

type providerRemovalTab struct {
	id       string
	ctrl     control.SessionAPI
	readOnly bool
}

func providerAccessFallbackRef(c *config.Config, name string) string {
	name = strings.TrimSpace(name)
	for _, candidate := range c.Desktop.ProviderAccess {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" || candidate == name {
			continue
		}
		p, ok := c.Provider(candidate)
		if !ok || len(p.ModelList()) == 0 {
			continue
		}
		return p.Name + "/" + p.DefaultModel()
	}
	return ""
}

func retargetProviderReferences(c *config.Config, name, fallbackRef string) {
	if strings.TrimSpace(fallbackRef) == "" {
		return
	}
	if desktopModelRefsProvider(c, c.DefaultModel, name) {
		c.DefaultModel = fallbackRef
	}
	if desktopModelRefsProvider(c, c.Agent.PlannerModel, name) {
		c.Agent.PlannerModel = fallbackRef
	}
	if desktopModelRefsProvider(c, c.Agent.SubagentModel, name) {
		c.Agent.SubagentModel = fallbackRef
	}
	for skill, ref := range c.Agent.SubagentModels {
		if desktopModelRefsProvider(c, ref, name) {
			c.Agent.SubagentModels[skill] = fallbackRef
		}
	}
}

func (a *App) removeBuiltInProviderAccessAndRetargetTabs(name string) error {
	// This first load is a read-only planning copy (fallback ref + affected-tab
	// scan); it loads credentials because the fallback choice depends on which
	// providers resolve a key. The saved edit below reloads under the config
	// edit lock so the slow snapshot work in between cannot widen the
	// read-modify-write window.
	cfg, _, err := a.loadDesktopUserConfigForViewWithCredentials()
	if err != nil {
		return err
	}
	fallbackRef := providerAccessFallbackRef(cfg, name)

	var affected []providerRemovalTab
	if fallbackRef != "" {
		a.mu.RLock()
		for _, id := range a.orderedTabIDsLocked() {
			tab := a.tabs[id]
			if tab == nil {
				continue
			}
			ref := tab.model
			if strings.TrimSpace(ref) == "" {
				ref = cfg.DefaultModel
			}
			if !desktopModelRefsProvider(cfg, ref, name) {
				continue
			}
			if controllerHasActiveRuntimeWork(tab.Ctrl) {
				a.mu.RUnlock()
				return fmt.Errorf("finish or cancel active work using %q before removing the provider access", name)
			}
			affected = append(affected, providerRemovalTab{id: id, ctrl: tab.Ctrl, readOnly: tab.ReadOnly})
		}
		a.mu.RUnlock()
	}

	if len(affected) == 0 {
		if err := a.ensureActiveTabRebuildAllowed("provider access"); err != nil {
			return err
		}
	}
	for _, item := range affected {
		if item.ctrl != nil && !item.readOnly {
			if err := item.ctrl.Snapshot(); err != nil {
				slog.Warn("desktop: snapshot before removing provider access failed", "tab", item.id, "provider", name, "err", err)
				return fmt.Errorf("save current session before removing provider access: %w", err)
			}
		}
	}
	// Reload-modify-save under the config edit lock: the pre-save snapshots
	// above are slow and must not hold the lock, so mutate a fresh copy here
	// instead of the stale planning copy loaded before them.
	if err := func() error {
		unlock := config.LockUserConfigEdits()
		defer unlock()
		fresh, path, err := a.loadDesktopUserConfigForEdit()
		if err != nil {
			return err
		}
		retargetProviderReferences(fresh, name, fallbackRef)
		removeProviderAccess(fresh, name)
		return fresh.SaveTo(path)
	}(); err != nil {
		return err
	}
	if len(affected) == 0 {
		if err := a.rebuild(); err != nil {
			if _, ok := a.deferredRebuildWarning("provider access", err); ok {
				return nil
			}
			return err
		}
		return nil
	}
	for _, item := range affected {
		if item.ctrl != nil {
			item.ctrl.Close()
		}
	}

	var rebuildTabs []*WorkspaceTab
	a.mu.Lock()
	for _, item := range affected {
		tab := a.tabs[item.id]
		if tab == nil {
			continue
		}
		if tab.Ctrl != item.ctrl {
			// The tab swapped controllers while we worked off-lock; nil-ing the
			// replacement would leak it. Leave the new runtime alone.
			continue
		}
		tab.Ctrl = nil
		// Supersede any in-flight startup build: it was planned against the
		// removed provider and would otherwise finish later, pass its
		// generation check, and reinstall a controller for it.
		a.supersedeTabBuildLocked(tab)
		tab.model = fallbackRef
		tab.Label = fallbackRef
		clearTabStartupError(tab)
		tab.Ready = a.ctx == nil
		if a.ctx != nil {
			rebuildTabs = append(rebuildTabs, tab)
		}
	}
	a.saveTabsLocked()
	a.mu.Unlock()

	for _, tab := range rebuildTabs {
		go a.buildTabController(tab)
	}
	return nil
}

func (a *App) deleteProviderAndRetargetTabs(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("remove provider: empty provider name")
	}
	// Read-only planning copy (with credentials — the fallback choice depends
	// on which providers resolve a key); the saved edit below reloads under the
	// config edit lock (see removeBuiltInProviderAccessAndRetargetTabs).
	cfg, _, err := a.loadDesktopUserConfigForViewWithCredentials()
	if err != nil {
		return err
	}
	fallbackRef := providerRemovalFallbackRef(cfg, name)

	var affected []providerRemovalTab
	a.mu.RLock()
	for _, id := range a.orderedTabIDsLocked() {
		tab := a.tabs[id]
		if tab == nil {
			continue
		}
		ref := tab.model
		if strings.TrimSpace(ref) == "" {
			ref = cfg.DefaultModel
		}
		if !desktopModelRefsProvider(cfg, ref, name) {
			continue
		}
		if controllerHasActiveRuntimeWork(tab.Ctrl) {
			a.mu.RUnlock()
			return fmt.Errorf("finish or cancel active work using %q before deleting the provider", name)
		}
		affected = append(affected, providerRemovalTab{id: id, ctrl: tab.Ctrl, readOnly: tab.ReadOnly})
	}
	a.mu.RUnlock()

	if len(affected) > 0 && fallbackRef == "" {
		return fmt.Errorf("remove provider: %q is used by open tabs and no other configured provider exists", name)
	}
	if len(affected) == 0 {
		if err := a.ensureActiveTabRebuildAllowed("provider"); err != nil {
			return err
		}
	}
	for _, item := range affected {
		if item.ctrl != nil && !item.readOnly {
			if err := item.ctrl.Snapshot(); err != nil {
				slog.Warn("desktop: snapshot before deleting provider failed", "tab", item.id, "provider", name, "err", err)
				return fmt.Errorf("save current session before deleting provider: %w", err)
			}
		}
	}
	// Reload-modify-save under the config edit lock; the snapshots above ran
	// off-lock against the stale planning copy.
	if err := func() error {
		unlock := config.LockUserConfigEdits()
		defer unlock()
		fresh, path, err := a.loadDesktopUserConfigForEdit()
		if err != nil {
			return err
		}
		if err := fresh.RemoveProvider(name); err != nil {
			return err
		}
		removeProviderAccess(fresh, name)
		return fresh.SaveTo(path)
	}(); err != nil {
		return err
	}

	if len(affected) == 0 {
		if err := a.rebuild(); err != nil {
			if _, ok := a.deferredRebuildWarning("provider", err); ok {
				return nil
			}
			return err
		}
		return nil
	}
	for _, item := range affected {
		if item.ctrl != nil {
			item.ctrl.Close()
		}
	}

	var rebuildTabs []*WorkspaceTab
	a.mu.Lock()
	for _, item := range affected {
		tab := a.tabs[item.id]
		if tab == nil {
			continue
		}
		if tab.Ctrl != item.ctrl {
			// The tab swapped controllers while we worked off-lock; nil-ing the
			// replacement would leak it. Leave the new runtime alone.
			continue
		}
		tab.Ctrl = nil
		// Supersede any in-flight startup build: it was planned against the
		// removed provider and would otherwise finish later, pass its
		// generation check, and reinstall a controller for it.
		a.supersedeTabBuildLocked(tab)
		tab.model = fallbackRef
		tab.Label = fallbackRef
		clearTabStartupError(tab)
		tab.Ready = a.ctx == nil
		if a.ctx != nil {
			rebuildTabs = append(rebuildTabs, tab)
		}
	}
	a.saveTabsLocked()
	a.mu.Unlock()

	for _, tab := range rebuildTabs {
		go a.buildTabController(tab)
	}
	return nil
}

// SetProviderKey writes a secret to Reasonix's global .env under the given
// env-var name (the one a provider's api_key_env points at) and rebuilds so it
// resolves immediately.
func (a *App) SetProviderKey(apiKeyEnv, value string) (string, error) {
	if strings.TrimSpace(apiKeyEnv) == "" {
		return "", fmt.Errorf("this provider has no api_key_env set")
	}
	if err := a.ensureActiveTabRebuildAllowed("provider key"); err != nil {
		return "", err
	}
	warning, err := a.saveProviderCredential(apiKeyEnv, value)
	if err != nil {
		return "", err
	}
	if err := a.ensureProviderAccessForKey(apiKeyEnv); err != nil {
		return "", err
	}
	if err := a.rebuildSetting("provider key"); err != nil {
		if rebuildWarning, ok := a.deferredRebuildWarning("provider key", err); ok {
			return appendSettingsWarning(warning, rebuildWarning), nil
		}
		return "", err
	}
	return warning, nil
}

// SaveProviderKey writes a provider secret without rebuilding the chat runtime.
// It is used by settings probes that need credentials only for a model-list
// request; explicit "save key" actions still call SetProviderKey.
func (a *App) SaveProviderKey(apiKeyEnv, value string) (string, error) {
	if strings.TrimSpace(apiKeyEnv) == "" {
		return "", fmt.Errorf("this provider has no api_key_env set")
	}
	return a.saveProviderCredential(apiKeyEnv, value)
}

func (a *App) ensureProviderAccessForKey(apiKeyEnv string) error {
	apiKeyEnv = strings.TrimSpace(apiKeyEnv)
	if apiKeyEnv == "" {
		return nil
	}
	// Pure load-modify-save on the user config; the caller (SetProviderKey)
	// rebuilds after we return, outside the config edit lock.
	unlock := config.LockUserConfigEdits()
	defer unlock()
	cfg, path, err := a.loadDesktopUserConfigForEdit()
	if err != nil {
		return err
	}
	access := providerAccessSet(cfg.Desktop.ProviderAccess)
	changed := false
	addAccess := func(name string) {
		if name == "" || access[name] {
			return
		}
		addProviderAccess(cfg, name)
		access[name] = true
		changed = true
	}
	for i := range cfg.Providers {
		p := cfg.Providers[i]
		if strings.TrimSpace(p.APIKeyEnv) != apiKeyEnv {
			continue
		}
		if len(p.ModelList()) == 0 {
			continue
		}
		if isOfficialBuiltInProvider(p) {
			addAccess(config.CanonicalDesktopOfficialProviderName(p.Name))
		} else {
			addAccess(strings.TrimSpace(p.Name))
		}
	}
	if !changed && apiKeyEnv == "DEEPSEEK_API_KEY" {
		entries, _, err := officialProviderTemplate("deepseek", cfg.DeepSeekOfficialPricingLanguage())
		if err != nil {
			return err
		}
		for _, e := range entries {
			if err := cfg.UpsertProvider(e); err != nil {
				return err
			}
			addAccess(e.Name)
		}
	}
	if !changed {
		return nil
	}
	return cfg.SaveTo(path)
}

// ClearProviderKey removes a provider secret from Reasonix's global .env
// and rebuilds so the provider immediately becomes unauthenticated.
func (a *App) ClearProviderKey(apiKeyEnv string) error {
	if strings.TrimSpace(apiKeyEnv) == "" {
		return fmt.Errorf("this provider has no api_key_env set")
	}
	if err := a.ensureActiveTabRebuildAllowed("provider key"); err != nil {
		return err
	}
	if err := removeDotEnv(apiKeyEnv); err != nil {
		return err
	}
	if err := a.rebuildSetting("provider key"); err != nil {
		if _, ok := a.deferredRebuildWarning("provider key", err); ok {
			return nil
		}
		return err
	}
	return nil
}

// SetPermissionMode sets the writer-fallback mode (ask|allow|deny).
func (a *App) SetPermissionMode(mode string) error {
	return a.applyConfigChange(func(c *config.Config) error { return c.SetPermissionMode(mode) })
}

// AddPermissionRule appends a rule to the allow/ask/deny list.
func (a *App) AddPermissionRule(list, rule string) error {
	return a.applyConfigChange(func(c *config.Config) error { return c.AddPermissionRule(list, rule) })
}

// RemovePermissionRule drops a rule from the allow/ask/deny list.
func (a *App) RemovePermissionRule(list, rule string) error {
	return a.applyConfigChange(func(c *config.Config) error {
		_, err := c.RemovePermissionRule(list, rule)
		return err
	})
}

// ReloadSettings rebuilds the active controller from the current config without
// changing any config file. It lets manual config.toml edits take effect.
func (a *App) ReloadSettings() error {
	if err := a.ensureActiveTabRebuildAllowed("settings"); err != nil {
		return err
	}
	if err := a.rebuild(); err != nil {
		// The on-disk config already diverged from the runtime; retry the
		// refresh once the other window releases the session lease.
		if _, ok := a.deferredRebuildWarning("settings", err); ok {
			return nil
		}
		return err
	}
	return nil
}

// SetSandbox updates the bash sandbox mode, network egress, and write roots.
func (a *App) SetSandbox(bash string, network bool, workspaceRoot string, allowWrite []string, shell string) error {
	return a.applyConfigChange(func(c *config.Config) error {
		c.Sandbox.Bash = bash
		c.Sandbox.Network = network
		c.Sandbox.WorkspaceRoot = strings.TrimSpace(workspaceRoot)
		c.Sandbox.AllowWrite = trimList(allowWrite)
		c.Tools.Shell.Prefer = strings.TrimSpace(shell)
		return nil
	})
}

// SetNetwork updates ordinary outbound proxy settings.
func (a *App) SetNetwork(n NetworkView) error {
	return a.applyConfigChange(func(c *config.Config) error {
		return c.SetNetwork(config.NetworkConfig{
			ProxyMode: n.ProxyMode,
			ProxyURL:  n.ProxyURL,
			NoProxy:   n.NoProxy,
			Proxy: config.NetworkProxyConfig{
				Type:     n.Proxy.Type,
				Server:   n.Proxy.Server,
				Port:     n.Proxy.Port,
				Username: n.Proxy.Username,
				Password: n.Proxy.Password,
			},
		})
	})
}

func (a *App) SetBotSettings(b BotSettingsView) error {
	err := a.applyConfigOnly(func(c *config.Config) error {
		c.Bot.Enabled = b.Enabled
		c.Bot.Model = strings.TrimSpace(b.Model)
		c.Bot.ToolApprovalMode = normalizeBotConnectionToolApprovalMode(b.ToolApprovalMode)
		c.Bot.MaxSteps = b.MaxSteps
		c.Bot.DebounceMs = b.DebounceMs
		c.Bot.QueueMode = strings.TrimSpace(b.QueueMode)
		c.Bot.QueueCap = b.QueueCap
		c.Bot.QueueDrop = strings.TrimSpace(b.QueueDrop)
		c.Bot.IgnoreSelfMessages = b.IgnoreSelfMessages
		c.Bot.SelfUserIDs = config.BotSelfUserIDs{
			QQ:     trimList(b.SelfUserIDs.QQ),
			Feishu: trimList(b.SelfUserIDs.Feishu),
			Weixin: trimList(b.SelfUserIDs.Weixin),
		}
		c.Bot.Control = config.BotControlConfig{
			Enabled:  b.Control.Enabled,
			Addr:     strings.TrimSpace(b.Control.Addr),
			TokenEnv: strings.TrimSpace(b.Control.TokenEnv),
		}
		c.Bot.Pairing = config.BotPairingConfig{
			Enabled:               b.Pairing.Enabled,
			RequestTTLMinutes:     b.Pairing.RequestTTLMinutes,
			MaxPendingPerPlatform: b.Pairing.MaxPendingPerPlatform,
		}
		c.Bot.Routes = botRouteConfigs(b.Routes)
		c.Bot.Allowlist = config.BotAllowlist{
			Enabled:         b.Allowlist.Enabled,
			AllowAll:        b.Allowlist.AllowAll,
			QQUsers:         trimList(b.Allowlist.QQUsers),
			FeishuUsers:     trimList(b.Allowlist.FeishuUsers),
			WeixinUsers:     trimList(b.Allowlist.WeixinUsers),
			QQApprovers:     trimList(b.Allowlist.QQApprovers),
			FeishuApprovers: trimList(b.Allowlist.FeishuApprovers),
			WeixinApprovers: trimList(b.Allowlist.WeixinApprovers),
			QQAdmins:        trimList(b.Allowlist.QQAdmins),
			FeishuAdmins:    trimList(b.Allowlist.FeishuAdmins),
			WeixinAdmins:    trimList(b.Allowlist.WeixinAdmins),
			QQGroups:        trimList(b.Allowlist.QQGroups),
			FeishuGroups:    trimList(b.Allowlist.FeishuGroups),
			WeixinGroups:    trimList(b.Allowlist.WeixinGroups),
		}
		c.Bot.QQ = config.QQBotConfig{
			Enabled:          b.QQ.Enabled,
			AppID:            strings.TrimSpace(b.QQ.AppID),
			AppSecretEnv:     strings.TrimSpace(b.QQ.AppSecretEnv),
			Sandbox:          b.QQ.Sandbox,
			Model:            strings.TrimSpace(b.QQ.Model),
			ToolApprovalMode: normalizeBotConnectionToolApprovalMode(b.QQ.ToolApprovalMode),
			WorkspaceRoot:    strings.TrimSpace(b.QQ.WorkspaceRoot),
			Access:           botAccessConfigFromView(b.QQ.Access),
		}
		c.Bot.Feishu = config.FeishuBotConfig{
			Enabled:            b.Feishu.Enabled,
			Domain:             botDomainOrDefault(b.Feishu.Domain),
			AppID:              strings.TrimSpace(b.Feishu.AppID),
			AppSecretEnv:       strings.TrimSpace(b.Feishu.AppSecretEnv),
			VerificationToken:  strings.TrimSpace(b.Feishu.VerificationToken),
			Mode:               strings.TrimSpace(b.Feishu.Mode),
			WebhookPort:        b.Feishu.WebhookPort,
			RequireMention:     b.Feishu.RequireMention,
			OutboundMediaRoots: append([]string(nil), c.Bot.Feishu.OutboundMediaRoots...),
		}
		c.Bot.Weixin = config.WeixinBotConfig{
			Enabled:   b.Weixin.Enabled,
			AccountID: strings.TrimSpace(b.Weixin.AccountID),
			TokenEnv:  strings.TrimSpace(b.Weixin.TokenEnv),
			APIBase:   strings.TrimRight(strings.TrimSpace(b.Weixin.APIBase), "/"),
		}
		c.Bot.Connections = botConnectionConfigs(b.Connections)
		return nil
	})
	if err == nil {
		a.refreshBotRuntimeAsync()
	}
	return err
}

// SetBotConnectionToolApprovalMode updates a single connection's tool approval
// mode without restarting the bot gateway. Only the connection's mode field is
// persisted; existing sessions on the running gateway are updated in-place.
func (a *App) SetBotConnectionToolApprovalMode(connID, mode string) error {
	connID = strings.TrimSpace(connID)
	mode = normalizeBotConnectionToolApprovalMode(mode)
	runtimeConnID := connID
	err := a.applyConfigOnly(func(c *config.Config) error {
		for i := range c.Bot.Connections {
			candidateRuntimeID := botruntime.ConnectionRuntimeID(c.Bot.Connections[i])
			if candidateRuntimeID == "" {
				candidateRuntimeID = strings.TrimSpace(c.Bot.Connections[i].ID)
			}
			if c.Bot.Connections[i].ID == connID || candidateRuntimeID == connID {
				c.Bot.Connections[i].ToolApprovalMode = mode
				c.Bot.Connections[i].UpdatedAt = time.Now().UTC().Format(time.RFC3339)
				runtimeConnID = candidateRuntimeID
				return nil
			}
		}
		return fmt.Errorf("connection %q not found", connID)
	})
	if err != nil {
		return err
	}
	if a.botRuntime != nil {
		a.botRuntime.updateConnectionToolApprovalMode(runtimeConnID, mode)
	}
	return nil
}

func (a *App) SetBotSecret(envName, value string) error {
	envName = strings.TrimSpace(envName)
	if envName == "" {
		return fmt.Errorf("bot secret env name is empty")
	}
	if err := upsertDotEnv(envName, value); err != nil {
		return err
	}
	a.refreshBotRuntimeAsync()
	return nil
}

func (a *App) ClearBotSecret(envName string) error {
	envName = strings.TrimSpace(envName)
	if envName == "" {
		return fmt.Errorf("bot secret env name is empty")
	}
	if err := removeDotEnv(envName); err != nil {
		return err
	}
	a.refreshBotRuntimeAsync()
	return nil
}

// SetCloseBehavior updates desktop-only window close behavior without rebuilding
// the active controller. It must stay out of provider-visible prompt/request data.
func (a *App) SetCloseBehavior(mode string) error {
	return a.applyConfigOnly(func(c *config.Config) error { return c.SetDesktopCloseBehavior(mode) })
}

// SetDisplayMode updates the transcript display mode. UI-only, no rebuild needed.
func (a *App) SetDisplayMode(mode string) error {
	return a.applyConfigOnly(func(c *config.Config) error { return c.SetDesktopDisplayMode(mode) })
}

// SetStatusBarStyle updates the desktop status bar metric label style. UI-only,
// no rebuild needed.
func (a *App) SetStatusBarStyle(style string) error {
	return a.applyConfigOnly(func(c *config.Config) error { return c.SetDesktopStatusBarStyle(style) })
}

// SetStatusBarItems updates the ordered visible desktop status bar items.
// UI-only, no rebuild needed.
func (a *App) SetStatusBarItems(items []string) error {
	return a.applyConfigOnly(func(c *config.Config) error { return c.SetDesktopStatusBarItems(items) })
}

// SetDesktopLanguage updates the desktop UI language and the user-level response
// language preference used by model-facing desktop sessions.
func (a *App) SetDesktopLanguage(lang string) error {
	responseLanguage := ""
	if err := a.applyConfigOnly(func(c *config.Config) error {
		if err := c.SetDesktopLanguage(lang); err != nil {
			return err
		}
		if err := c.SetLanguage(lang); err != nil {
			return err
		}
		responseLanguage = c.ResponseLanguage()
		return nil
	}); err != nil {
		return err
	}
	a.updateTrayLocale(lang)
	a.applyResponseLanguageToLiveControllers(responseLanguage)
	return nil
}

// SetTrayLocale mirrors the resolved desktop UI language into the native tray
// menu. It is runtime-only; the persisted preference remains [desktop].language.
func (a *App) SetTrayLocale(locale string) error {
	if locale != "zh" {
		locale = "en"
	}
	a.updateTrayLocale(locale)
	return nil
}

// SetDesktopAppearance updates only desktop theme preferences. It does not
// rebuild the active controller and must stay out of provider-visible requests.
func (a *App) SetDesktopAppearance(theme, style string) error {
	return a.applyConfigOnly(func(c *config.Config) error { return c.SetDesktopAppearance(theme, style) })
}

// SetDesktopLayoutStyle updates only the desktop layout style. It does not
// rebuild the active controller and must stay out of provider-visible requests.
func (a *App) SetDesktopLayoutStyle(style string) error {
	normalized := ""
	if err := a.applyConfigOnly(func(c *config.Config) error {
		if err := c.SetDesktopLayoutStyle(style); err != nil {
			return err
		}
		normalized = c.DesktopLayoutStyle()
		return nil
	}); err != nil {
		return err
	}
	if singleSurfaceLayoutStyle(normalized) {
		return a.applySingleSurfaceTabPolicy()
	}
	return nil
}

// SetDesktopCheckUpdates updates only the desktop startup update-check
// preference. Manual checks in Settings are unaffected.
func (a *App) SetDesktopCheckUpdates(enabled bool) error {
	return a.applyConfigOnly(func(c *config.Config) error { return c.SetDesktopCheckUpdates(enabled) })
}

// SetDesktopTelemetry sets whether the desktop sends the anonymous launch ping.
func (a *App) SetDesktopTelemetry(enabled bool) error {
	return a.applyConfigOnly(func(c *config.Config) error { return c.SetDesktopTelemetry(enabled) })
}

// SetDesktopMetrics sets whether the desktop sends aggregate desktop metrics,
// starting or stopping the live aggregator so the toggle takes effect immediately.
func (a *App) SetDesktopMetrics(enabled bool) error {
	if err := a.applyConfigOnly(func(c *config.Config) error { return c.SetDesktopMetrics(enabled) }); err != nil {
		return err
	}
	switch {
	case enabled && a.metrics.Load() == nil && version != "dev":
		a.metrics.Store(newMetricsAggregator(config.MemoryUserDir()))
		if cfg, err := config.Load(); err == nil {
			a.recordSettingsMetricsSnapshot(cfg)
		}
	case !enabled:
		a.metrics.Store(nil)
	}
	return nil
}

// SetExpandThinking sets whether reasoning text is expanded by default on
// the desktop. It is desktop-only and does not rebuild the controller.
func (a *App) SetExpandThinking(on bool) error {
	return a.applyConfigOnly(func(c *config.Config) error { return c.SetExpandThinking(on) })
}

// MigrateDesktopPreferences imports old browser-local desktop preferences into
// the user config once. Existing [desktop] values win so stale localStorage never
// overwrites an explicit config edit.
func (a *App) MigrateDesktopPreferences(language, theme, style string) error {
	return a.applyConfigOnly(func(c *config.Config) error {
		if strings.TrimSpace(c.Desktop.Language) == "" {
			if err := c.SetDesktopLanguage(language); err != nil {
				return err
			}
		}
		if strings.TrimSpace(c.Desktop.Theme) == "" && strings.TrimSpace(c.Desktop.ThemeStyle) == "" {
			if err := c.SetDesktopAppearance(theme, style); err != nil {
				return err
			}
		}
		return nil
	})
}

// SetAgentParams updates sampling temperature, optional step guards, and the
// base system prompt.
func (a *App) SetAgentParams(temperature float64, maxSteps int, plannerMaxSteps int, systemPrompt string) error {
	return a.applyConfigChange(func(c *config.Config) error {
		c.Agent.Temperature = temperature
		c.Agent.MaxSteps = maxSteps
		c.Agent.PlannerMaxSteps = plannerMaxSteps
		c.Agent.SystemPrompt = systemPrompt
		return nil
	})
}

func (a *App) SetColdResumePrune(enabled bool) error {
	return a.applyConfigChange(func(c *config.Config) error { return c.SetColdResumePrune(enabled) })
}

func (a *App) SetReasoningLanguage(lang string) error {
	if err := a.ensureLiveControllersRuntimeMutationAllowed("reasoning language"); err != nil {
		return err
	}
	var cfg *config.Config
	// Lock only the load-modify-save cycle; the live-controller fan-out below
	// must not hold the config edit lock.
	if err := func() error {
		unlock := config.LockUserConfigEdits()
		defer unlock()
		loaded, path, err := a.loadDesktopUserConfigForEdit()
		if err != nil {
			return err
		}
		if err := loaded.SetReasoningLanguage(lang); err != nil {
			return err
		}
		if err := loaded.SaveTo(path); err != nil {
			return err
		}
		cfg = loaded
		return nil
	}(); err != nil {
		return err
	}
	a.applyReasoningLanguageToLiveControllers(cfg.ReasoningLanguage())
	return nil
}

func (a *App) applyReasoningLanguageToLiveControllers(fallback string) {
	type liveTab struct {
		root string
		ctrl control.SessionAPI
	}
	var tabs []liveTab
	a.mu.RLock()
	for _, tab := range a.tabs {
		if tab != nil && tab.Ctrl != nil {
			tabs = append(tabs, liveTab{root: tab.WorkspaceRoot, ctrl: tab.Ctrl})
		}
	}
	a.mu.RUnlock()
	for _, tab := range tabs {
		mode := fallback
		if cfg, err := config.LoadForRoot(tab.root); err == nil {
			mode = cfg.ReasoningLanguage()
		}
		tab.ctrl.SetReasoningLanguage(mode)
	}
}

func (a *App) applyResponseLanguageToLiveControllers(fallback string) {
	type liveTab struct {
		root string
		ctrl control.SessionAPI
	}
	var tabs []liveTab
	a.mu.RLock()
	for _, tab := range a.tabs {
		if tab != nil && tab.Ctrl != nil {
			tabs = append(tabs, liveTab{root: tab.WorkspaceRoot, ctrl: tab.Ctrl})
		}
	}
	a.mu.RUnlock()
	for _, tab := range tabs {
		mode := fallback
		if cfg, err := config.LoadForRoot(tab.root); err == nil {
			mode = cfg.ResponseLanguage()
		}
		tab.ctrl.SetResponseLanguage(mode)
	}
}

// trimList drops blank entries from a string slice (and returns a non-nil slice).
func trimList(in []string) []string {
	out := []string{}
	for _, s := range in {
		if t := strings.TrimSpace(s); t != "" {
			out = append(out, t)
		}
	}
	return out
}
