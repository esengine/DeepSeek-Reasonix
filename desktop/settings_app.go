package main

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

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

// read

type ProviderView struct {
	Name                        string                      `json:"name"`
	BuiltIn                     bool                        `json:"builtIn"`
	Added                       bool                        `json:"added"`
	Kind                        string                      `json:"kind"`
	BaseURL                     string                      `json:"baseUrl"`
	ChatURL                     string                      `json:"chatUrl"`
	Models                      []string                    `json:"models"`
	VisionModels                []string                    `json:"visionModels"`
	VisionModelsSet             bool                        `json:"visionModelsConfigured"`
	VisionCapability            string                      `json:"visionCapability,omitempty"`
	ModelsURL                   string                      `json:"modelsUrl"`
	Default                     string                      `json:"default"`
	APIKeyEnv                   string                      `json:"apiKeyEnv"`
	Headers                     map[string]string           `json:"headers"`
	ExtraBody                   map[string]any              `json:"extraBody"`
	AuthHeader                  bool                        `json:"authHeader"`
	KeySet                      bool                        `json:"keySet"` // the env var currently resolves to a non-empty value
	RequiresKey                 bool                        `json:"requiresKey"`
	Configured                  bool                        `json:"configured"` // selectable: either key is present or no key is required
	KeySource                   string                      `json:"keySource,omitempty"`
	KeySourcePath               string                      `json:"keySourcePath,omitempty"`
	BalanceURL                  string                      `json:"balanceUrl"`
	ContextWindow               int                         `json:"contextWindow"`
	ReasoningProtocol           string                      `json:"reasoningProtocol"`
	Thinking                    string                      `json:"thinking"`
	WebSearch                   bool                        `json:"webSearch"`
	ServerWebSearchCapability   bool                        `json:"serverWebSearchCapability"`
	SupportedEfforts            []string                    `json:"supportedEfforts"`
	DefaultEffort               string                      `json:"defaultEffort"`
	ModelOverrides              []ProviderModelOverrideView `json:"modelOverrides"`
	RecommendedUpgradeAvailable bool                        `json:"recommendedUpgradeAvailable,omitempty"`
	// ModelCatalogFingerprint is an opaque digest of the provider identity and
	// current model selection. Background discovery must compare it while holding
	// the config edit lock before applying a narrow catalog-only update.
	ModelCatalogFingerprint string `json:"modelCatalogFingerprint"`
}

type ProviderModelCatalogUpdate struct {
	Name                string   `json:"name"`
	ExpectedFingerprint string   `json:"expectedFingerprint"`
	Models              []string `json:"models"`
	Default             string   `json:"default"`
	VisionModels        []string `json:"visionModels"`
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
	ContextWindow     int      `json:"contextWindow,omitempty"`
	MaxOutputTokens   int      `json:"maxOutputTokens,omitempty"`
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
	Temperature            float64 `json:"temperature"`
	MaxSteps               int     `json:"maxSteps"`
	PlannerMaxSteps        int     `json:"plannerMaxSteps"`
	MaxSubagentDepth       int     `json:"maxSubagentDepth"`
	MaxSubagentConcurrency int     `json:"maxSubagentConcurrency"`
	MaxParallelWriters     int     `json:"maxParallelWriters"`
	SystemPrompt           string  `json:"systemPrompt"`
	ColdResumePrune        bool    `json:"coldResumePrune"`
	ReasoningLanguage      string  `json:"reasoningLanguage"`
	CompactRatio           float64 `json:"compactRatio,omitempty"`
	EffectiveCompactRatio  float64 `json:"effectiveCompactRatio,omitempty"`
	CompactRatioOverridden bool    `json:"compactRatioOverridden,omitempty"`
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
	DesktopCurrency         string               `json:"desktopCurrency"`
	DesktopLayoutStyle      string               `json:"desktopLayoutStyle"`
	DesktopTheme            string               `json:"desktopTheme"`
	DesktopThemeStyle       string               `json:"desktopThemeStyle"`
	DesktopTerminalTheme    string               `json:"desktopTerminalTheme,omitempty"`
	CloseBehavior           string               `json:"closeBehavior"`
	DisplayMode             string               `json:"displayMode"`
	StatusBarStyle          string               `json:"statusBarStyle"`
	StatusBarItems          []string             `json:"statusBarItems"`
	DefaultToolApprovalMode string               `json:"defaultToolApprovalMode"`

	CheckUpdates      bool   `json:"checkUpdates"`
	UpdateChannel     string `json:"updateChannel"`
	Telemetry         bool   `json:"telemetry"`
	Metrics           bool   `json:"metrics"`
	ExpandThinking    bool   `json:"expandThinking"`
	ConversationWidth string `json:"conversationWidth,omitempty"`
	ConfigPath        string `json:"configPath"`
	// ShadowedByPath is the workspace reasonix.toml that outranks the file this
	// panel writes, so an edit here can be overridden with nothing on screen to
	// explain it (#4333). Empty when the panel's file is the one in effect.
	ShadowedByPath string `json:"shadowedByPath,omitempty"`
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
	Bot                  BotSettingsView `json:"bot"`
	DesktopLanguage      string          `json:"desktopLanguage"`
	DesktopLayoutStyle   string          `json:"desktopLayoutStyle"`
	DesktopTheme         string          `json:"desktopTheme"`
	DesktopThemeStyle    string          `json:"desktopThemeStyle"`
	DesktopTerminalTheme string          `json:"desktopTerminalTheme,omitempty"`
	DisplayMode          string          `json:"displayMode"`
	StatusBarStyle       string          `json:"statusBarStyle"`
	StatusBarItems       []string        `json:"statusBarItems"`
	CheckUpdates         bool            `json:"checkUpdates"`
	UpdateChannel        string          `json:"updateChannel"`
	ConversationWidth    string          `json:"conversationWidth,omitempty"`
	// ConfigWarnings are non-blocking notices when user/project config was
	// recovered in memory (last-known-good or defaults) without rewriting files.
	ConfigWarnings []string `json:"configWarnings,omitempty"`
	ConfigPath     string   `json:"configPath,omitempty"`
}

// shadowingConfigPath returns the config file that outranks writePath for the
// workspace at root, or "" when writePath is the one in effect. A project
// reasonix.toml beats the user config, so settings written here would otherwise
// look ignored (#4333).
func shadowingConfigPath(writePath, root string) string {
	effective := config.SourcePathForRoot(root)
	if effective == "" || samePath(effective, writePath) {
		return ""
	}
	if abs, err := filepath.Abs(effective); err == nil {
		return abs
	}
	return effective
}

func samePath(a, b string) bool {
	absA, errA := filepath.Abs(a)
	absB, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return a == b
	}
	if runtime.GOOS == "windows" {
		return strings.EqualFold(filepath.Clean(absA), filepath.Clean(absB))
	}
	return filepath.Clean(absA) == filepath.Clean(absB)
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

func providerCredentialsRevision() string {
	return config.CredentialStoreRevision()
}

var providerStateFingerprintKey = func() []byte {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		panic(fmt.Sprintf("initialize provider state fingerprint key: %v", err))
	}
	return key
}()

func providerModelCatalogFingerprint(p config.ProviderEntry) string {
	return providerModelCatalogFingerprintForCredentials(p, providerCredentialsRevision())
}

func providerModelCatalogFingerprintForCredentials(p config.ProviderEntry, credentialsRevision string) string {
	// This token crosses the Wails boundary, so key the digest instead of exposing
	// a reusable hash of header or credential-store metadata to the frontend.
	h := hmac.New(sha256.New, providerStateFingerprintKey)
	write := func(value string) {
		_, _ = fmt.Fprintf(h, "%d:", len(value))
		_, _ = h.Write([]byte(value))
	}
	write("provider-model-catalog-v1")
	write("name")
	write(p.Name)
	write("kind")
	write(p.Kind)
	write("base_url")
	write(p.BaseURL)
	write("models_url")
	write(p.ModelsURL)
	write("api_key_env")
	write(p.APIKeyEnv)
	write("credentials_revision")
	write(credentialsRevision)
	write("auth_header")
	write(fmt.Sprintf("%t", p.AuthHeader))
	keys := make([]string, 0, len(p.Headers))
	for key := range p.Headers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	write("headers")
	write(fmt.Sprintf("%d", len(keys)))
	for _, key := range keys {
		write(key)
		write(p.Headers[key])
	}
	write("model")
	write(p.Model)
	write("models")
	write(fmt.Sprintf("%d", len(p.Models)))
	for _, model := range p.Models {
		write(model)
	}
	write("default")
	write(p.Default)
	write("vision")
	write(fmt.Sprintf("%t", p.Vision))
	write("vision_models")
	write(fmt.Sprintf("%d", len(p.VisionModels)))
	for _, model := range p.VisionModels {
		write(model)
	}
	return fmt.Sprintf("%x", h.Sum(nil))
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
			ContextWindow:     ov.ContextWindow,
			MaxOutputTokens:   ov.MaxOutputTokens,
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
			ContextWindow:     max(item.ContextWindow, 0),
			MaxOutputTokens:   item.MaxOutputTokens,
		}
		if strings.TrimSpace(ov.ReasoningProtocol) == "" && len(ov.SupportedEfforts) == 0 && strings.TrimSpace(ov.DefaultEffort) == "" && ov.Vision == nil && ov.ContextWindow == 0 && ov.MaxOutputTokens == 0 {
			continue
		}
		out[model] = ov
	}
	if len(out) == 0 {
		return nil
	}
	return out
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
	return providerViewFromEntryForRootWithResolverAndCredentials(p, builtIn, added, root, resolver, providerCredentialsRevision())
}

func providerViewFromEntryForRootWithResolverAndCredentials(p config.ProviderEntry, builtIn, added bool, root string, resolver *config.CredentialResolver, credentialsRevision string) ProviderView {
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
	visionCapability := "configurable"
	if !config.CanConfigureVision(&p) {
		visionCapability = "unsupported"
	}
	return ProviderView{
		Name: p.Name, BuiltIn: builtIn, Added: added, Kind: p.Kind, BaseURL: p.BaseURL, ChatURL: p.ChatURL,
		Models: nonNil(models), VisionModels: nonNil(providerVisionModels(models, visionModels)), VisionModelsSet: visionModelsSet, VisionCapability: visionCapability, ModelsURL: p.ModelsURL, Default: p.DefaultModel(),
		APIKeyEnv:                   p.APIKeyEnv,
		Headers:                     nonNilStringMap(p.Headers),
		ExtraBody:                   nonNilAnyMap(p.ExtraBody),
		AuthHeader:                  p.AuthHeader,
		KeySet:                      key.Set,
		RequiresKey:                 requiresKey,
		Configured:                  !requiresKey || key.Set,
		KeySource:                   key.Source.Label,
		KeySourcePath:               key.Source.Path,
		BalanceURL:                  p.BalanceURL,
		ContextWindow:               p.ContextWindow,
		ReasoningProtocol:           p.ReasoningProtocol,
		Thinking:                    providerThinkingForSettings(p.Thinking),
		WebSearch:                   config.EffectiveWebSearch(&p),
		ServerWebSearchCapability:   config.HasServerWebSearchCapability(&p),
		SupportedEfforts:            nonNil(p.SupportedEfforts),
		DefaultEffort:               p.DefaultEffort,
		ModelOverrides:              providerModelOverridesForView(p.ModelOverrides, models),
		RecommendedUpgradeAvailable: config.CanUpgradeDeepSeekProviderProtocol(&p),
		ModelCatalogFingerprint:     providerModelCatalogFingerprintForCredentials(p, credentialsRevision),
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
	credentialsRevision := providerCredentialsRevision()
	for _, kind := range []string{"deepseek"} {
		entries, _, err := officialProviderTemplate(kind, pricingLanguage)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			out = append(out, providerViewFromEntryForRootWithResolverAndCredentials(entry, true, added[entry.Name], root, resolver, credentialsRevision))
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
			Bot:                  botSettingsView(config.BotConfig{}),
			DesktopLayoutStyle:   "workbench",
			DesktopTheme:         "auto",
			DesktopThemeStyle:    "graphite",
			DesktopTerminalTheme: "auto",
			DisplayMode:          "standard",
			StatusBarStyle:       "text",
			StatusBarItems:       config.DefaultDesktopStatusBarItems(),
			CheckUpdates:         true,
			UpdateChannel:        "stable",
			ConversationWidth:    "standard",
		}
	}
	return DesktopStartupSettingsView{
		Bot:                  botSettingsView(cfg.Bot),
		DesktopLanguage:      cfg.DesktopLanguage(),
		DesktopLayoutStyle:   cfg.DesktopLayoutStyle(),
		DesktopTheme:         cfg.DesktopTheme(),
		DesktopThemeStyle:    cfg.DesktopThemeStyle(),
		DesktopTerminalTheme: cfg.DesktopTerminalTheme(),
		DisplayMode:          cfg.DesktopDisplayMode(),
		StatusBarStyle:       cfg.DesktopStatusBarStyle(),
		StatusBarItems:       cfg.DesktopStatusBarItems(),
		CheckUpdates:         cfg.DesktopCheckUpdates(),
		UpdateChannel:        cfg.DesktopUpdateChannel(),
		ConversationWidth:    cfg.DesktopConversationWidth(),
		ConfigWarnings:       cfg.LoadWarnings(),
		ConfigPath:           config.UserConfigPath(),
	}
}

// DesktopStartupSettings returns only the desktop chrome preferences needed at
// app startup. Keep provider/key status in Settings(), where the Settings panel
// actually needs it.
func (a *App) DesktopStartupSettings() DesktopStartupSettingsView {
	// Prefer the resilient workspace load so config warnings surface on first paint.
	if cfg, err := config.LoadForRootReadOnly(a.activeWorkspaceRoot()); err == nil {
		view := desktopStartupSettingsFromConfig(cfg)
		view.ConfigWarnings = cfg.LoadWarnings()
		view.ConfigPath = config.UserConfigPath()
		return view
	}
	cfg, path, err := a.loadDesktopUserConfigForView()
	if err != nil {
		view := desktopStartupSettingsFromConfig(nil)
		view.ConfigWarnings = []string{
			"user configuration could not be loaded; using built-in defaults. Run: reasonix doctor repair",
		}
		view.ConfigPath = config.UserConfigPath()
		return view
	}
	view := desktopStartupSettingsFromConfig(cfg)
	view.ConfigPath = path
	return view
}

// OpenUserConfigPath reveals the user config file in the system file manager.
func (a *App) OpenUserConfigPath() error {
	path := config.UserConfigPath()
	if path == "" {
		return fmt.Errorf("user config path is unavailable")
	}
	// Reveal the parent directory when the file does not exist yet so the user
	// can still find where config.toml should live.
	if _, err := os.Stat(path); err != nil {
		return a.RevealPath(filepath.Dir(path))
	}
	return a.RevealPath(path)
}

// ReloadUserConfig reloads configuration for the active workspace after the
// user fixes a broken file. Non-fatal load warnings remain visible when present.
func (a *App) ReloadUserConfig() (DesktopStartupSettingsView, error) {
	return a.DesktopStartupSettings(), nil
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
			Sandbox: SandboxView{Bash: config.Default().BashMode(), AllowWrite: []string{}, EffectiveWriteRoots: []string{}, Shell: "auto", EffectiveShell: sandboxEffectiveShellView(sandbox.ResolveShell("", "", nil))},
			Agent: AgentView{
				PlannerMaxSteps:        0,
				MaxSubagentDepth:       agent.DefaultMaxSubagentDepth,
				MaxSubagentConcurrency: agent.DefaultMaxSubagentConcurrency,
				MaxParallelWriters:     agent.DefaultMaxParallelWriters,
				ColdResumePrune:        true,
				ReasoningLanguage:      "auto",
				CompactRatio:           config.Default().Agent.CompactRatio,
				EffectiveCompactRatio:  config.Default().Agent.CompactRatio,
			},
			Bot:                     botSettingsView(config.BotConfig{}),
			AutoPlan:                "off",
			DesktopLayoutStyle:      "workbench",
			DesktopTheme:            "auto",
			DesktopThemeStyle:       "graphite",
			DesktopTerminalTheme:    "auto",
			CloseBehavior:           "background",
			DisplayMode:             "standard",
			StatusBarStyle:          "text",
			StatusBarItems:          config.DefaultDesktopStatusBarItems(),
			DefaultToolApprovalMode: "auto",
			CheckUpdates:            true,
			UpdateChannel:           "stable",
			Telemetry:               true,
			Metrics:                 true,
			ExpandThinking:          false,
			ConversationWidth:       "standard",
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
		AutoPlan:          "off", // deprecated JSON compatibility for older frontends
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
		Agent: AgentView{
			Temperature:            cfg.Agent.Temperature,
			MaxSteps:               cfg.Agent.MaxSteps,
			PlannerMaxSteps:        cfg.Agent.PlannerMaxSteps,
			MaxSubagentDepth:       desktopMaxSubagentDepth(cfg.Agent.MaxSubagentDepth),
			MaxSubagentConcurrency: desktopSubagentConcurrency(cfg.Agent.MaxSubagentConcurrency),
			MaxParallelWriters:     desktopParallelWriters(cfg.Agent.MaxParallelWriters, cfg.Agent.MaxSubagentConcurrency),
			SystemPrompt:           cfg.Agent.SystemPrompt,
			ColdResumePrune:        cfg.ColdResumePruneEnabled(),
			ReasoningLanguage:      cfg.ReasoningLanguage(),
			CompactRatio:           cfg.Agent.CompactRatio,
			EffectiveCompactRatio:  cfg.Agent.CompactRatio,
		},
		Bot:                     botSettingsView(cfg.Bot),
		DesktopLanguage:         cfg.DesktopLanguage(),
		DesktopCurrency:         cfg.DesktopCurrency(),
		DesktopLayoutStyle:      cfg.DesktopLayoutStyle(),
		DesktopTheme:            cfg.DesktopTheme(),
		DesktopThemeStyle:       cfg.DesktopThemeStyle(),
		DesktopTerminalTheme:    cfg.DesktopTerminalTheme(),
		CloseBehavior:           cfg.DesktopCloseBehavior(),
		DisplayMode:             cfg.DesktopDisplayMode(),
		StatusBarStyle:          cfg.DesktopStatusBarStyle(),
		StatusBarItems:          cfg.DesktopStatusBarItems(),
		DefaultToolApprovalMode: cfg.DesktopDefaultToolApprovalMode(),
		CheckUpdates:            cfg.DesktopCheckUpdates(),
		UpdateChannel:           cfg.DesktopUpdateChannel(),
		Telemetry:               cfg.DesktopTelemetry(),
		Metrics:                 cfg.DesktopMetrics(),
		ExpandThinking:          cfg.Desktop.ExpandThinking,
		ConversationWidth:       cfg.DesktopConversationWidth(),
		ConfigPath:              cfgPath,
		ShadowedByPath:          shadowingConfigPath(cfgPath, root),
		ProviderKinds:           nonNil(provider.Kinds()),
		AutoApproveTools:        ctrl != nil && ctrl.AutoApproveTools(),
		Bypass:                  ctrl != nil && ctrl.AutoApproveTools(),
	}
	if ctrl != nil {
		if effective := ctrl.CompactRatio(); effective > 0 {
			v.Agent.EffectiveCompactRatio = effective
			v.Agent.CompactRatioOverridden = math.Abs(effective-v.Agent.CompactRatio) > 0.0001
		}
	}
	added := providerAccessSet(cfg.Desktop.ProviderAccess)
	resolver := config.NewCredentialResolverForRoot(root)
	credentialsRevision := providerCredentialsRevision()
	v.OfficialProviders = officialProviderViewsForRootWithResolver(officialProviderAddedSet(cfg), a.desktopOfficialPricingLanguage(cfg), root, resolver)
	v.ProviderPresets = providerPresetViewsForRootWithResolver(cfg, root, resolver)
	for i := range cfg.Providers {
		p := &cfg.Providers[i]
		providerView := providerViewFromEntryForRootWithResolverAndCredentials(*p, isOfficialBuiltInProvider(*p), added[p.Name], root, resolver, credentialsRevision)
		providerView.RecommendedUpgradeAvailable = providerView.RecommendedUpgradeAvailable && config.CanUpgradeDeepSeekProviderProtocolUserConfig(p.Name)
		v.Providers = append(v.Providers, providerView)
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

// apply (write config, then rebuild the controller so it's live)

// applyConfigChange mutates the user-global config and rebuilds the controller so
// the change takes effect this session. Desktop settings such as providers and
// keys are account-level, not per-project: writing them to the global config
// rather than the cwd's reasonix.toml is what lets them survive a workspace switch.
func (a *App) applyConfigChange(mutate func(*config.Config) error) error {
	_, err := a.applyConfigChangeWithWarning("settings", mutate)
	return err
}

// applySkillConfigChange edits the config file that owns the selected [skills]
// field. Project skill settings shadow the global setting at runtime, so
// writing only the user config would make the UI appear to save while the
// active project continued using its old value.
func (a *App) applySkillConfigChange(field, setting string, mutate func(*config.Config) error) error {
	return a.applySkillConfigChangeForFields([]string{field}, setting, mutate)
}

func (a *App) applySkillConfigChangeForFields(fields []string, setting string, mutate func(*config.Config) error) error {
	workspaceRoot := a.activeWorkspaceRoot()
	projectPath := config.SourcePathForRoot(workspaceRoot)
	projectOwned := strings.TrimSpace(projectPath) != "" && !config.IsUserConfigPath(projectPath)
	if projectOwned {
		projectOwned = slices.ContainsFunc(fields, func(field string) bool {
			return config.ConfigFileDefinesSkillKey(projectPath, field)
		})
	}
	if !projectOwned {
		return a.applyConfigChange(mutate)
	}
	if err := a.ensureActiveTabRebuildAllowed(setting); err != nil {
		return err
	}
	if err := func() error {
		unlock, err := config.LockConfigFileEdits(projectPath)
		if err != nil {
			return err
		}
		defer unlock()
		cfg, err := config.LoadForEditWithoutCredentialsReadOnlyStrict(projectPath)
		if err != nil {
			return err
		}
		if err := mutate(cfg); err != nil {
			return err
		}
		for _, field := range fields {
			if err := cfg.KeepProjectSkillKey(field); err != nil {
				return err
			}
		}
		return cfg.SaveTo(projectPath)
	}(); err != nil {
		return err
	}
	if err := a.rebuildSetting(setting); err != nil {
		if _, ok := a.deferredRebuildWarning(setting, err); ok {
			return nil
		}
		return err
	}
	return nil
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

// applyGlobalProviderConfigChange persists a provider-wide setting and refreshes
// every visible runtime while runtime admission and all turn gates are frozen.
// Detached runtimes cannot participate in the failure-atomic build-and-swap, so
// reject before writing instead of leaving one session on stale provider state.
func (a *App) applyGlobalProviderConfigChange(setting, operation, detachedAction string, mutate func(*config.Config) error) error {
	defer a.lockRuntimeMutation(operation)()
	releaseGates, err := a.lockRuntimeTurnGates(setting, nil)
	if err != nil {
		return err
	}
	defer releaseGates()
	tabs, err := a.visibleTabsForGlobalRuntimeMutation(detachedAction)
	if err != nil {
		return err
	}
	if err := func() error {
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
		return err
	}

	var rebuildErrs []error
	for _, tab := range tabs {
		if a.controllerForTab(tab) == nil {
			// A startup placeholder has no stale provider runtime. Its eventual
			// build reads the just-persisted config.
			continue
		}
		if err := a.rebuildSettingTurnLocked(setting, tab, true, false); err != nil {
			if _, ok := a.deferredRebuildWarningForTab(setting, err, tab); ok {
				continue
			}
			rebuildErrs = append(rebuildErrs, err)
		}
	}
	return errors.Join(rebuildErrs...)
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
	if err := rebuildControllerActiveWorkErrorFor(a.controllerForTab(tab), setting); err != nil {
		return err
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
		if err := rebuildControllerActiveWorkErrorFor(tab.Ctrl, setting); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) deferredRebuildWarning(setting string, err error) (string, bool) {
	return a.deferredRebuildWarningForTab(setting, err, a.activeTab())
}

func (a *App) deferredRebuildWarningForTab(setting string, err error, tab *WorkspaceTab) (string, bool) {
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
	// Bind both the warning and the retry to the tab whose refresh failed, so a
	// tab switch or a multi-tab mutation cannot misroute either one.
	if tab != nil {
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

// loadDesktopUserConfigForEdit loads the user config for a write path. Pending
// legacy migrations are assembled in memory and reach disk through the locked
// user-config save, never by rewriting a project file as a side effect.
//
// Contract: the caller must already hold config.LockUserConfigEdits() across
// its whole load→mutate→SaveTo cycle, so the migration write-back cannot race
// other in-process config editors. This helper must never acquire that lock
// itself: applyConfigChange/applyConfigOnly (and every other caller) invoke it
// with the lock held, so an inner acquire would self-deadlock. Read-only
// callers must use loadDesktopUserConfigForView (or its WithCredentials
// variant), which never writes to disk.
func (a *App) loadDesktopUserConfigForEdit() (*config.Config, string, error) {
	return a.loadDesktopUserConfigForEditForRoot(a.activeWorkspaceRoot())
}

func (a *App) loadDesktopUserConfigForEditForRoot(root string) (*config.Config, string, error) {
	userPath := config.UserConfigPath()
	if userPath == "" {
		return nil, "", fmt.Errorf("cannot resolve user config directory")
	}
	if _, err := os.Stat(userPath); err == nil {
		cfg, err := config.LoadForEditReadOnlyStrict(userPath)
		if err != nil {
			return nil, "", err
		}
		if err := normalizeLegacyDesktopProviderAccessForSettings(cfg, userPath); err != nil {
			return nil, "", err
		}
		if err := a.migrateLegacyBotConfigToUserForRoot(root, cfg, userPath); err != nil {
			return nil, "", err
		}
		return cfg, userPath, nil
	}
	cfg, err := config.LoadForEditReadOnlyStrict(userPath)
	if err != nil {
		return nil, "", err
	}
	legacyPath := config.SourcePathForRoot(root)
	if legacyPath == "" || sameConfigPath(legacyPath, userPath) {
		if err := normalizeLegacyDesktopProviderAccessForSettings(cfg, userPath); err != nil {
			return nil, "", err
		}
		return cfg, userPath, nil
	}
	legacyCfg, err := config.LoadForEditReadOnlyStrict(legacyPath)
	if err != nil {
		return nil, "", err
	}
	normalizeLegacyDesktopProviderAccessInMemory(legacyCfg, legacyPath)
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
	return a.loadDesktopUserConfigForViewForRoot(a.activeWorkspaceRoot())
}

func (a *App) loadDesktopUserConfigForViewForRoot(root string) (*config.Config, string, error) {
	return a.loadDesktopUserConfigReadOnlyForRoot(root, config.LoadForEditWithoutCredentialsReadOnlyStrict)
}

// loadDesktopUserConfigForViewWithCredentials is loadDesktopUserConfigForView
// plus credential resolution: like config.LoadForEdit it loads Reasonix's
// global .env into the process env. Use it for read-only loads whose result
// feeds a runtime that resolves env-based secrets — the bot runtime
// (app-secret/control-token envs) and MCP server connects. It still never
// writes to disk.
func (a *App) loadDesktopUserConfigForViewWithCredentials() (*config.Config, string, error) {
	return a.loadDesktopUserConfigForViewWithCredentialsForRoot(a.activeWorkspaceRoot())
}

func (a *App) loadDesktopUserConfigForViewWithCredentialsForRoot(root string) (*config.Config, string, error) {
	return a.loadDesktopUserConfigReadOnlyForRoot(root, config.LoadForEditReadOnlyStrict)
}

// loadDesktopUserConfigReadOnlyForRoot is the shared pure-read loader behind
// the View variants: same shape as loadDesktopUserConfigForEdit, but every
// legacy migration stays in memory (zero SaveTo) and resolves from root.
func (a *App) loadDesktopUserConfigReadOnlyForRoot(root string, load func(string) (*config.Config, error)) (*config.Config, string, error) {
	userPath := config.UserConfigPath()
	if userPath == "" {
		return nil, "", fmt.Errorf("cannot resolve user config directory")
	}
	if _, err := os.Stat(userPath); err == nil {
		cfg, err := load(userPath)
		if err != nil {
			return nil, "", err
		}
		normalizeLegacyDesktopProviderAccessInMemory(cfg, userPath)
		legacyPath := config.SourcePathForRoot(root)
		if legacyPath != "" && !sameConfigPath(legacyPath, userPath) {
			legacyCfg, err := load(legacyPath)
			if err != nil {
				return nil, "", err
			}
			mergeLegacyBotConfigInMemory(cfg, legacyCfg)
		}
		return cfg, userPath, nil
	}
	cfg, err := load(userPath)
	if err != nil {
		return nil, "", err
	}
	legacyPath := config.SourcePathForRoot(root)
	if legacyPath == "" || sameConfigPath(legacyPath, userPath) {
		normalizeLegacyDesktopProviderAccessInMemory(cfg, userPath)
		return cfg, userPath, nil
	}
	// The user config does not exist yet: serve the legacy config as the view.
	// It already carries any legacy bot config, so no merge is needed; the
	// write path creates the migrated user file later.
	legacyCfg, err := load(legacyPath)
	if err != nil {
		return nil, "", err
	}
	normalizeLegacyDesktopProviderAccessInMemory(legacyCfg, legacyPath)
	legacyCfg.ConfigVersion = config.Default().ConfigVersion
	return legacyCfg, userPath, nil
}

// migrateLegacyBotConfigToUserForRoot is the write-path legacy bot-config
// migration against an explicit workspace's legacy config file. Callers must
// hold config.LockUserConfigEdits() (see loadDesktopUserConfigForEdit).
func (a *App) migrateLegacyBotConfigToUserForRoot(root string, userCfg *config.Config, userPath string) error {
	if userCfg == nil {
		return nil
	}
	legacyPath := config.SourcePathForRoot(root)
	if legacyPath == "" || sameConfigPath(legacyPath, userPath) {
		return nil
	}
	legacyCfg, err := config.LoadForEditReadOnlyStrict(legacyPath)
	if err != nil {
		return err
	}
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
	for line := range strings.SplitSeq(string(body), "\n") {
		if before, _, ok := strings.Cut(line, "#"); ok {
			line = before
		}
		line = strings.TrimSpace(line)
		if after, ok := strings.CutPrefix(line, "provider_access"); ok {
			rest := strings.TrimSpace(after)
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
	err := a.rebuildSettingLocked(setting)
	a.runtimeRebuildMu.Unlock()
	return err
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
	tab.turnStartMu.Lock()
	defer tab.turnStartMu.Unlock()
	return a.rebuildSettingTurnLocked(setting, tab, false, false)
}

// rebuildSettingTurnLocked is rebuildSettingLocked's body; callers must hold
// runtimeRebuildMu and the passed tab's turnStartMu. admissionHeld is true for
// MCP lifecycle callers that also hold runtimeAdmissionMu's write side.
// reload selects the stage-3b runtime-reload build path (boot.Rebuild migrates
// the session) instead of the legacy boot.Build + manual migration; everything
// else — active-work guards, workspace prep, lease moves, swap, close-after-
// swap, fence — is shared.
func (a *App) rebuildSettingTurnLocked(setting string, tab *WorkspaceTab, admissionHeld bool, reload bool) error {
	return a.rebuildSettingTurnLockedWithModel(setting, tab, "", admissionHeld, reload)
}

// rebuildSettingTurnLockedWithModel optionally builds the replacement for a
// target model without changing tab.model before the swap. Provider removal
// uses this to remain failure-atomic: a failed fallback build leaves both the
// old controller and its visible model identity untouched.
func (a *App) rebuildSettingTurnLockedWithModel(setting string, tab *WorkspaceTab, modelOverride string, admissionHeld bool, reload bool) error {
	if a.ctx == nil {
		return nil
	}
	if err := rebuildControllerActiveWorkErrorFor(a.controllerForTab(tab), setting); err != nil {
		return err
	}
	ensureWorkspace := a.ensureTabControllerWorkspace
	if admissionHeld {
		ensureWorkspace = a.ensureTabControllerWorkspaceAdmissionHeld
	}
	if err := ensureWorkspace(tab); err != nil {
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
	if err := rebuildControllerActiveWorkErrorFor(a.controllerForTab(tab), setting); err != nil {
		return err
	}
	if err := ensureWorkspace(tab); err != nil {
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
	runtime := snap.normalizedRuntime()
	model := snap.model
	if override := strings.TrimSpace(modelOverride); override != "" {
		model = override
	}
	if cfg, err := config.LoadForRoot(snap.workspaceRoot); err == nil {
		if resolved, fallback, ok := cfg.ResolveModelWithFallback(model); ok {
			if fallback && strings.TrimSpace(model) != "" {
				a.noticeForTab(tab.ID, fmt.Sprintf("model %q is no longer available; switched to %s", model, resolved))
			}
			model = resolved
		}
	}
	ctrl, restoredRuntime, path, err := a.buildSettingReplacementController(tab, snap, runtime, model, prevPath, setting, oldCtrl, carried, reload)
	if err != nil {
		if oldCtrl == nil {
			leaseHeld := false
			a.mu.Lock()
			leaseHeld = setTabStartupError(tab, err)
			tab.Ready = false
			if leaseHeld {
				a.setSessionRuntimePhaseLocked(tab, sessionRuntimeLeaseBlocked, err)
			} else {
				a.setSessionRuntimePhaseLocked(tab, sessionRuntimeFailed, err)
			}
			a.mu.Unlock()
			if leaseHeld {
				a.scheduleDeferredStartupBuild(tab.ID)
			}
			a.emitReady(a.ctx)
		}
		return err
	}
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
	applyNormalizedRuntimeToTabLocked(tab, restoredRuntime)
	clearTabStartupError(tab)
	tab.Ready = true
	// Supersede any in-flight startup build: it would otherwise finish later,
	// pass its generation check, and overwrite the controller just installed.
	a.supersedeTabBuildLocked(tab)
	a.saveTabsLocked()
	a.mu.Unlock()
	// True subgraph rebuilds reuse the same controller pointer — never Close it.
	if oldCtrl != nil && oldCtrl != ctrl {
		oldCtrl.Close()
	}
	a.persistTabSessionPath(tab, path)
	if setting == "currency" {
		a.repriceTabUsageForCurrentCurrency(tab)
	}
	a.clearDeferredRebuild(tab.ID)
	a.notifyTabRuntimeRebuilt(tab)
	a.emitReady(a.ctx)
	return nil
}

// buildSettingReplacementController builds the replacement controller for
// rebuildSettingTurnLocked and migrates the session onto it, returning the
// controller, the runtime posture actually restored, and the session path it
// bound. reload=false is the legacy settings path (boot.Build plus the
// desktop's manual migration); reload=true is the stage-3b runtime reload,
// routing build and migration through boot.Rebuild so history, approval mode
// and grants, plan/goal state, and lifecycle move inside the boot layer. The
// caller owns the swap, closing the old controller after the swap, and the
// post-swap persistence.
func (a *App) buildSettingReplacementController(tab *WorkspaceTab, snap tabRuntimeSnapshot, runtime normalizedTabRuntime, model, prevPath, setting string, oldCtrl control.SessionAPI, carried []provider.Message, reload bool) (control.SessionAPI, normalizedTabRuntime, string, error) {
	opts := boot.Options{
		Model: model, RequireKey: false,
		RuntimeReload:            boot.RuntimeReload{ForceFullRebuild: reload},
		AutoPricingCurrency:      a.desktopAutoPricingCurrency(),
		StatsSource:              "desktop",
		Sink:                     snap.sink,
		WorkspaceRoot:            snap.workspaceRoot,
		SessionDir:               sessionDirForSnapshot(snap),
		EffortOverride:           cloneStringPtr(snap.effort),
		TokenMode:                runtime.tokenMode,
		SharedHost:               a.lookupSharedHost(snap.sharedHostKey),
		CleanupPendingReconciler: reconcileDesktopCleanupPending,
		SubagentParentLive:       a.subagentParentProbeForBuild(tab),
		SessionRecoveryMeta:      a.tabSessionRecoveryMeta(tab),
		OnSessionRecovered:       a.handleTabSessionRecovered(tab),
	}
	if reload && oldCtrl != nil {
		old, ok := oldCtrl.(*control.Controller)
		if !ok {
			return nil, normalizedTabRuntime{}, "", fmt.Errorf("reload runtime: controller is %T, want *control.Controller", oldCtrl)
		}
		res, err := rebuildTabRuntime(a, tab, old, opts)
		if err != nil {
			return nil, normalizedTabRuntime{}, "", err
		}
		ctrl := res.Controller
		a.bindControllerDisplayRecorder(ctrl)
		// boot.Rebuild migrated history (same session file, fresh system
		// prompt spliced), approval mode and grants, plan/goal state, and
		// lifecycle. The interactive approval gate and the plan/yolo tab
		// mode are desktop wiring Rebuild deliberately leaves out — the
		// mode re-apply also restores yolo, which Rebuild does not carry.
		ctrl.EnableInteractiveApproval()
		applyTabModeToController(ctrl, runtime.tabMode())
		// Same path Rebuild pinned internally (identical inputs), recomputed
		// for the lease move and the post-swap persistence.
		path := agent.ContinueSessionPath(prevPath, ctrl.SessionDir(), ctrl.Label())
		if err := a.ensureTabSessionLeaseForRebuild(tab, path, setting); err != nil {
			ctrl.Close()
			return nil, normalizedTabRuntime{}, "", err
		}
		restoredRuntime, err := normalizeRestoredControllerRuntime(ctrl, runtime)
		if err != nil {
			ctrl.Close()
			return nil, normalizedTabRuntime{}, "", err
		}
		return ctrl, restoredRuntime, path, nil
	}
	// Same-session rebuild without the full boot.Rebuild path still must keep
	// the private temporary directory (Issue #7575).
	if old, ok := oldCtrl.(*control.Controller); ok && old != nil && opts.SessionTemp == nil {
		opts.SessionTemp = old.SessionTemp()
	}
	ctrl, err := boot.Build(a.bootContext(), opts)
	if err != nil {
		return nil, normalizedTabRuntime{}, "", err
	}
	a.bindControllerDisplayRecorder(ctrl)
	configureControllerRuntime(ctrl, oldCtrl, runtime)
	path := agent.ContinueSessionPath(prevPath, ctrl.SessionDir(), ctrl.Label())
	if err := a.ensureTabSessionLeaseForRebuild(tab, path, setting); err != nil {
		ctrl.Close()
		return nil, normalizedTabRuntime{}, "", err
	}
	restoredRuntime, err := resumeControllerRuntimeWithMessages(ctrl, carried, path, runtime)
	if err != nil {
		ctrl.Close()
		return nil, normalizedTabRuntime{}, "", err
	}
	return ctrl, restoredRuntime, path, nil
}

// runtimeReloadSettingLabel is the settings-style label used in busy/lease
// error text and notices for an explicit runtime reload.
const runtimeReloadSettingLabel = "runtime reload"

// ReloadRuntime rebuilds the tab's agent runtime in place — tools, skills,
// commands, hooks, providers, and MCP servers are re-discovered from the
// current config — while the session carries over (transcript, approval
// grants, goal/recovery state, shared plugin Host) via boot.Rebuild. Active
// work or a held lease queues exactly one reload on the deferred-rebuild
// loop, which runs it once the tab is idle; a failure keeps the old
// controller fully usable.
func (a *App) ReloadRuntime(tabID string) error {
	if a.ctx == nil {
		return nil
	}
	tab := a.tabByID(tabID)
	if tab == nil || tab.ID != tabID {
		return fmt.Errorf("unknown tab %q", tabID)
	}
	// Same serialization as rebuildSetting: two build+swap sequences on the
	// same tab must not interleave.
	a.runtimeRebuildMu.Lock()
	err := a.reloadRuntimeTurnLocked(tab)
	a.runtimeRebuildMu.Unlock()
	if err == nil {
		return nil
	}
	var busy *rebuildBusyError
	if errors.As(err, &busy) || errors.Is(err, agent.ErrSessionLeaseHeld) {
		// Queue exactly one reload per tab (the pending map coalesces
		// duplicates); the loop retries once the work finishes or the lease
		// clears.
		a.scheduleDeferredRebuild(tab.ID, deferredRuntimeReloadLabel)
		a.noticeForTab(tab.ID, "runtime reload queued: will run when the current work finishes")
		return nil
	}
	return err
}

// reloadRuntimeTurnLocked runs the in-place runtime reload for tab; callers
// hold runtimeRebuildMu (the deferred-rebuild retry loop also drives it).
func (a *App) reloadRuntimeTurnLocked(tab *WorkspaceTab) error {
	if a.ctx == nil {
		return nil
	}
	tab.turnStartMu.Lock()
	defer tab.turnStartMu.Unlock()
	return a.rebuildSettingTurnLocked(runtimeReloadSettingLabel, tab, false, true)
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
	if !modelProviderAccessAllowed(c.Desktop.ProviderAccess, entry.Name) {
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

func desktopSubagentConcurrency(n int) int {
	total, _ := agent.NormalizeConcurrencyLimits(n, 0)
	return total
}

func desktopParallelWriters(writers, total int) int {
	_, w := agent.NormalizeConcurrencyLimits(total, writers)
	return w
}

// SetMaxSubagentConcurrency sets the session-wide sub-agent concurrency cap (1–32).
func (a *App) SetMaxSubagentConcurrency(n int) error {
	return a.applyConfigChange(func(c *config.Config) error {
		total, writers := agent.NormalizeConcurrencyLimits(n, c.Agent.MaxParallelWriters)
		c.Agent.MaxSubagentConcurrency = total
		c.Agent.MaxParallelWriters = writers
		return nil
	})
}

// SetMaxParallelWriters sets the concurrent writer cap (1–32, ≤ total concurrency).
func (a *App) SetMaxParallelWriters(n int) error {
	return a.applyConfigChange(func(c *config.Config) error {
		total, writers := agent.NormalizeConcurrencyLimits(c.Agent.MaxSubagentConcurrency, n)
		c.Agent.MaxSubagentConcurrency = total
		c.Agent.MaxParallelWriters = writers
		return nil
	})
}

// SetAutoPlan is retained for older frontend bundles. Automatic plan mode is
// retired, so "off" is an idempotent compatibility call and enabling it is
// rejected without mutating user configuration or live controllers.
func (a *App) SetAutoPlan(mode string) error {
	return config.Default().SetAutoPlan(mode)
}

// SetDefaultToolApprovalMode updates the global Ask/Auto/YOLO default used only
// for newly-created desktop sessions. Existing tabs keep their persisted mode.
func (a *App) SetDefaultToolApprovalMode(mode string) error {
	return a.applyConfigOnly(func(c *config.Config) error {
		return c.SetDesktopDefaultToolApprovalMode(mode)
	})
}

// SetDefaultAutoRecoveryCheckpoint is retained as a no-op Wails surface for
// older generated frontends. Auto Guard is always built into Auto.
func (a *App) SetDefaultAutoRecoveryCheckpoint(_ bool) error { return nil }

func officialProviderTemplate(kind, pricingLanguage string) ([]config.ProviderEntry, string, error) {
	webSearchEnabled := true
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "deepseek", "deepseek-official":
		return []config.ProviderEntry{{
			Name:          "deepseek",
			Kind:          "anthropic",
			BaseURL:       "https://api.deepseek.com/anthropic",
			Models:        []string{"deepseek-v4-flash", "deepseek-v4-pro"},
			Default:       "deepseek-v4-flash",
			APIKeyEnv:     "DEEPSEEK_API_KEY",
			BalanceURL:    "https://api.deepseek.com/user/balance",
			Thinking:      "enabled",
			WebSearch:     &webSearchEnabled,
			ContextWindow: 1_000_000,
			Prices:        config.DeepSeekV4PricesForLanguage(pricingLanguage),
			ModelOverrides: map[string]config.ProviderModelOverride{
				"deepseek-v4-flash": {SupportedEfforts: []string{"disabled", "low", "high", "max"}, DefaultEffort: "high"},
				"deepseek-v4-pro":   {SupportedEfforts: []string{"disabled", "high", "max"}, DefaultEffort: "high"},
			},
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
		if slices.Contains(models, currentDefault) {
			return currentDefault
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
	existing := false
	for i := range c.Providers {
		if c.Providers[i].Name == p.Name {
			e = c.Providers[i]
			existing = true
			break
		}
	}
	original := e
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
	// Settings exposes this switch only for verified endpoints. Preserve an
	// existing advanced override, but never carry an official default to a new URL.
	if config.IsOfficialDeepSeekWebSearchEndpoint(&e) {
		enabled := p.WebSearch
		e.WebSearch = &enabled
	} else if !config.SupportsServerWebSearch(&e) || !existing || config.IsOfficialDeepSeekWebSearchEndpoint(&original) {
		e.WebSearch = nil
	}
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

// SetProviderWebSearch updates every provider represented by one Settings
// access card in a single config transaction. Legacy DeepSeek aliases can
// remain separate when their custom transport fields differ, so changing only
// the first profile would leave the grouped control in a contradictory state.
func (a *App) SetProviderWebSearch(names []string, enabled bool) error {
	return a.applyGlobalProviderConfigChange(
		"DeepSeek server-side web search",
		"set-provider-web-search",
		"changing DeepSeek server-side web search",
		func(c *config.Config) error {
			seen := make(map[string]bool, len(names))
			providers := make([]*config.ProviderEntry, 0, len(names))
			for _, rawName := range names {
				name := strings.TrimSpace(rawName)
				if name == "" || seen[name] {
					continue
				}
				seen[name] = true
				entry, ok := c.Provider(name)
				if !ok {
					return fmt.Errorf("provider %q not found", name)
				}
				if !config.IsOfficialDeepSeekWebSearchEndpoint(entry) {
					return fmt.Errorf("provider %q does not support configurable server-side web search", name)
				}
				providers = append(providers, entry)
			}
			if len(providers) == 0 {
				return fmt.Errorf("provider list is empty")
			}
			for _, entry := range providers {
				value := enabled
				entry.WebSearch = &value
			}
			return nil
		})
}

func providerModelOverridesForCatalog(overrides map[string]config.ProviderModelOverride, models []string) map[string]config.ProviderModelOverride {
	if len(overrides) == 0 {
		return nil
	}
	allowed := make(map[string]bool, len(models))
	for _, model := range models {
		allowed[model] = true
	}
	filtered := make(map[string]config.ProviderModelOverride, len(overrides))
	for model, override := range overrides {
		if allowed[model] {
			filtered[model] = override
		}
	}
	if len(filtered) == 0 {
		return nil
	}
	return filtered
}

func applyProviderModelCatalogUpdate(c *config.Config, update ProviderModelCatalogUpdate, credentialsRevision string) (bool, error) {
	if c == nil {
		return false, fmt.Errorf("config is nil")
	}
	current, ok := c.Provider(strings.TrimSpace(update.Name))
	if !ok || strings.TrimSpace(update.ExpectedFingerprint) == "" ||
		providerModelCatalogFingerprintForCredentials(*current, credentialsRevision) != strings.TrimSpace(update.ExpectedFingerprint) {
		return false, nil
	}
	models := chatProviderModels(update.Models)
	if len(models) == 0 {
		return false, fmt.Errorf("provider %q model catalog is empty", update.Name)
	}

	next := *current
	visionConfigured := next.Vision || next.VisionModels != nil
	next.Model = models[0] // keep validation/back-compat populated
	next.Models = models
	next.Default = ""
	if len(models) > 1 {
		next.Default = providerDefaultForModels(update.Default, models)
	}
	next.Vision = false
	if visionConfigured {
		next.VisionModels = providerVisionModels(models, update.VisionModels)
	} else {
		next.VisionModels = nil
	}
	next.ModelOverrides = providerModelOverridesForCatalog(next.ModelOverrides, models)
	if config.ProviderEntriesConfigEqual(*current, next) {
		return false, nil
	}
	if err := c.UpsertProvider(next); err != nil {
		return false, err
	}
	return true, nil
}

// SaveProviderModelCatalogs applies only model-catalog fields. Each update is
// compared against the provider snapshot that launched discovery while the
// config edit lock is held, so an older async completion cannot overwrite newer
// provider edits. Stale updates are skipped rather than treated as failures.
func (a *App) SaveProviderModelCatalogs(updates []ProviderModelCatalogUpdate) ([]string, error) {
	if len(updates) == 0 {
		return []string{}, nil
	}
	if err := a.ensureActiveTabRebuildAllowed("provider model catalogs"); err != nil {
		return []string{}, err
	}
	applied := make([]string, 0, len(updates))
	if err := func() error {
		unlock := config.LockUserConfigEdits()
		defer unlock()
		cfg, path, err := a.loadDesktopUserConfigForEdit()
		if err != nil {
			return err
		}
		observedCredentialsRevision := providerCredentialsRevision()
		if a.providerCatalogBeforeCredentialLockHook != nil {
			a.providerCatalogBeforeCredentialLockHook(observedCredentialsRevision)
		}
		unlockCredentials, err := config.LockUserCredentialEdits()
		if err != nil {
			return err
		}
		defer unlockCredentials()
		// Re-read while holding the same lock as every Reasonix credential
		// writer, then keep that lock through the config commit. A rotation that
		// won the race therefore invalidates the request fingerprint.
		credentialsRevision := providerCredentialsRevision()
		for _, update := range updates {
			changed, err := applyProviderModelCatalogUpdate(cfg, update, credentialsRevision)
			if err != nil {
				return err
			}
			if changed {
				applied = append(applied, strings.TrimSpace(update.Name))
			}
		}
		if len(applied) == 0 {
			return nil
		}
		return cfg.SaveTo(path)
	}(); err != nil {
		return []string{}, err
	}
	if len(applied) == 0 {
		return applied, nil
	}
	if err := a.rebuildSetting("provider model catalogs"); err != nil {
		if _, ok := a.deferredRebuildWarning("provider model catalogs", err); ok {
			return applied, nil
		}
		return []string{}, err
	}
	return applied, nil
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

// UpgradeDeepSeekProviderAccess applies the explicit Settings action for an
// official legacy OpenAI entry. The config package performs a narrow raw-TOML
// edit so unrelated and future fields are not lost to a full config render.
func (a *App) UpgradeDeepSeekProviderAccess(name string) (string, error) {
	const setting = "DeepSeek provider protocol"
	defer a.lockRuntimeMutation("upgrade-deepseek-provider")()
	releaseGates, err := a.lockRuntimeTurnGates(setting, nil)
	if err != nil {
		return "", err
	}
	defer releaseGates()
	visibleTabs, err := a.visibleTabsForGlobalRuntimeMutation("upgrading the DeepSeek provider protocol")
	if err != nil {
		return "", err
	}

	changed, err := config.UpgradeDeepSeekProviderProtocolUserConfig(name)
	if err != nil {
		return "", err
	}
	if !changed {
		return "", fmt.Errorf("DeepSeek provider %q is not eligible for the recommended protocol upgrade", name)
	}
	warning := ""
	var rebuildErrs []error
	for _, tab := range visibleTabs {
		if a.controllerForTab(tab) == nil {
			// A visible startup placeholder has no stale provider runtime. Its
			// eventual build will read the upgraded config directly.
			continue
		}
		if err := a.rebuildSettingTurnLocked(setting, tab, true, false); err != nil {
			if deferred, ok := a.deferredRebuildWarningForTab(setting, err, tab); ok {
				if warning == "" {
					warning = deferred
				}
				continue
			}
			// The protocol is user-global and already persisted. Keep refreshing
			// sibling tabs so one workspace-specific boot failure cannot leave
			// every later runtime pinned to the old protocol.
			rebuildErrs = append(rebuildErrs, err)
		}
	}
	return warning, errors.Join(rebuildErrs...)
}

// visibleTabsForGlobalRuntimeUpgrade returns every visible runtime that must
// observe a user-global provider protocol change. A detached controller cannot
// use rebuildSettingTurnLocked because it is intentionally absent from a.tabs;
// reject before mutating config so it never remains silently pinned to the old
// protocol. runtime mutation admission and all turn gates are held by callers.
func (a *App) visibleTabsForGlobalRuntimeMutation(detachedAction string) ([]*WorkspaceTab, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	for _, tab := range a.detachedSessions {
		if tab != nil && tab.Ctrl != nil {
			return nil, fmt.Errorf("background session is still open; reopen or close it before %s", detachedAction)
		}
	}
	tabs := make([]*WorkspaceTab, 0, len(a.tabs))
	for _, id := range a.orderedTabIDsLocked() {
		if tab := a.tabs[id]; tab != nil {
			tabs = append(tabs, tab)
		}
	}
	return tabs, nil
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

// FetchAllProviderModels fetches model lists for all providers in a single
// batch. Models are fetched concurrently (up to 4 parallel requests) and
// returned as a map keyed by provider name. Errors for individual providers
// are recorded as nil entries; callers should handle missing keys.
func (a *App) FetchAllProviderModels(providers []ProviderView) map[string][]string {
	results := make(map[string][]string, len(providers))
	var mu sync.Mutex
	g, ctx := errgroup.WithContext(a.reqCtx())
	g.SetLimit(4)
	root := a.activeWorkspaceRoot()
	for i := range providers {
		p := providers[i]
		g.Go(func() error {
			e := config.ProviderEntry{
				Name:       p.Name,
				Kind:       p.Kind,
				BaseURL:    p.BaseURL,
				ModelsURL:  strings.TrimSpace(p.ModelsURL),
				APIKeyEnv:  p.APIKeyEnv,
				Headers:    p.Headers,
				AuthHeader: p.AuthHeader,
			}
			e.ResolveAPIKeyForRoot(root)
			ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
			defer cancel()
			models, err := e.FetchModels(ctx)
			if err != nil {
				// Omit failed providers so the frontend can retry them through
				// the cached single-provider path without emitting JSON null.
				return nil
			}
			mu.Lock()
			defer mu.Unlock()
			results[p.Name] = nonNil(chatProviderModels(models))
			return nil
		})
	}
	_ = g.Wait()
	return results
}

// rebuildActiveSettingRuntimeMutationLocked refreshes the active controller
// while lockRuntimeMutation and all runtime turn gates are held.
func (a *App) rebuildActiveSettingRuntimeMutationLocked(setting string) error {
	tab := a.activeTab()
	if tab == nil {
		if a.ctx == nil {
			return nil
		}
		return fmt.Errorf("no active tab")
	}
	return a.rebuildSettingTurnLocked(setting, tab, true, false)
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
	pricingChanged := false
	if cfg, _, err := a.loadDesktopUserConfigForView(); err == nil && cfg.DesktopCurrency() == "" {
		targetCurrency := a.desktopAutoPricingCurrency()
		switch strings.ToLower(strings.TrimSpace(lang)) {
		case "zh":
			targetCurrency = "CNY"
		case "en":
			targetCurrency = "USD"
		}
		pricingChanged = a.desktopEffectivePricingCurrency(cfg) != targetCurrency
	}
	mutate := func(c *config.Config) error {
		if err := c.SetDesktopLanguage(lang); err != nil {
			return err
		}
		if err := c.SetLanguage(lang); err != nil {
			return err
		}
		responseLanguage = c.ResponseLanguage()
		return nil
	}
	var err error
	if pricingChanged {
		_, err = a.applyConfigChangeWithWarning("currency", mutate)
	} else {
		err = a.applyConfigOnly(mutate)
	}
	if err != nil {
		return err
	}
	if pricingChanged {
		a.scheduleCurrencyRefreshForOtherTabs()
	}
	if strings.TrimSpace(lang) != "" && !strings.EqualFold(strings.TrimSpace(lang), "auto") {
		a.setDesktopLocale(lang)
	}
	a.updateTrayLocale(lang)
	a.applyResponseLanguageToLiveControllers(responseLanguage)
	return nil
}

// SetDesktopCurrency updates the official pricing region independently from UI
// language. Rebuild the active controller so subsequent usage carries the new
// currency and regional rates through the existing structured cost fields.
func (a *App) SetDesktopCurrency(currency string) error {
	_, err := a.applyConfigChangeWithWarning("currency", func(c *config.Config) error {
		return c.SetDesktopCurrency(currency)
	})
	if err == nil {
		a.scheduleCurrencyRefreshForOtherTabs()
	}
	return err
}

func (a *App) scheduleCurrencyRefreshForOtherTabs() {
	if a == nil || a.ctx == nil {
		return
	}
	a.mu.RLock()
	activeID := a.activeTabID
	tabIDs := make([]string, 0, len(a.tabs))
	for id, tab := range a.tabs {
		if id != activeID && tab != nil && tab.Ctrl != nil && !tab.removed {
			tabIDs = append(tabIDs, id)
		}
	}
	a.mu.RUnlock()
	for _, id := range tabIDs {
		a.scheduleDeferredRebuild(id, "currency")
	}
}

func (a *App) scheduleCurrencyRefreshForAllTabs() {
	if a == nil {
		return
	}
	a.mu.RLock()
	tabIDs := make([]string, 0, len(a.tabs))
	for id, tab := range a.tabs {
		if tab != nil && tab.Ctrl != nil && !tab.removed {
			tabIDs = append(tabIDs, id)
		}
	}
	a.mu.RUnlock()
	for _, id := range tabIDs {
		a.scheduleDeferredRebuild(id, "currency")
	}
}

func (a *App) desktopPricingFollowsDetectedLocale() bool {
	cfg, _, err := a.loadDesktopUserConfigForView()
	return err == nil && cfg.DesktopPricingFollowsDetectedLocale()
}

func (a *App) desktopEffectivePricingCurrency(cfg *config.Config) string {
	if cfg == nil {
		return a.desktopAutoPricingCurrency()
	}
	if cfg.DesktopPricingFollowsDetectedLocale() {
		return a.desktopAutoPricingCurrency()
	}
	return cfg.DeepSeekOfficialPricingCurrency()
}

func (a *App) desktopOfficialPricingLanguage(cfg *config.Config) string {
	if a.desktopEffectivePricingCurrency(cfg) == "CNY" {
		return "zh"
	}
	return "en"
}

// SetTrayLocale mirrors the resolved desktop UI language into the native tray
// menu. It is runtime-only; the persisted preference remains [desktop].language.
func (a *App) SetTrayLocale(locale string) error {
	previousCurrency := a.desktopAutoPricingCurrency()
	a.setDesktopLocale(locale)
	pricingCurrencyChanged := previousCurrency != a.desktopAutoPricingCurrency()
	trayLocale := "en"
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(locale)), "zh") {
		trayLocale = "zh"
	}
	a.updateTrayLocale(trayLocale)
	if pricingCurrencyChanged && a.desktopPricingFollowsDetectedLocale() {
		a.scheduleCurrencyRefreshForAllTabs()
		a.kickDeferredRebuildRetry()
	}
	a.emitProjectTreeChanged()
	return nil
}

// SetDesktopAppearance updates only desktop theme preferences. It does not
// rebuild the active controller and must stay out of provider-visible requests.
func (a *App) SetDesktopAppearance(theme, style string) error {
	return a.applyConfigOnly(func(c *config.Config) error { return c.SetDesktopAppearance(theme, style) })
}

// SetDesktopTerminalTheme updates only the integrated terminal colours. It is
// applied live by the frontend and does not rebuild the active controller.
func (a *App) SetDesktopTerminalTheme(theme string) error {
	return a.applyConfigOnly(func(c *config.Config) error { return c.SetDesktopTerminalTheme(theme) })
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

// SetDesktopUpdateChannel is retained for older Wails clients. The config layer
// clears the retired preference and every updater request uses Stable.
func (a *App) SetDesktopUpdateChannel(channel string) error {
	return a.applyConfigOnly(func(c *config.Config) error { return c.SetDesktopUpdateChannel(channel) })
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

// SetDesktopConversationWidth sets the max transcript width preference.
// standard = 960px fixed; full = 90% of the parent, with a 960px floor. Pure config-only.
func (a *App) SetDesktopConversationWidth(width string) error {
	return a.applyConfigOnly(func(c *config.Config) error { return c.SetDesktopConversationWidth(width) })
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

// SetAgentParams updates sampling temperature and the base system prompt. The
// step arguments remain in the Wails contract for older frontends, but are
// retired and deliberately normalized to automatic execution.
func (a *App) SetAgentParams(temperature float64, maxSteps int, plannerMaxSteps int, systemPrompt string) error {
	return a.applyConfigChange(func(c *config.Config) error {
		c.Agent.Temperature = temperature
		c.Agent.MaxSteps = 0
		c.Agent.PlannerMaxSteps = 0
		c.Agent.SystemPrompt = systemPrompt
		return nil
	})
}

func (a *App) SetColdResumePrune(enabled bool) error {
	return a.applyConfigChange(func(c *config.Config) error { return c.SetColdResumePrune(enabled) })
}

func (a *App) SetCompactRatio(ratio float64) error {
	_, err := a.applyConfigChangeWithWarning("context compaction threshold", func(c *config.Config) error {
		return c.SetCompactRatio(ratio)
	})
	return err
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
