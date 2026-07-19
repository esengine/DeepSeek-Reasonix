package config

import (
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"reasonix/internal/fileutil"
	fileencoding "reasonix/internal/fileutil/encoding"
	"reasonix/internal/provider"
)

// Load builds the configuration: defaults, then user config, then project
// config, then MCP servers from Claude Code's .mcp.json, then (lowest priority)
// the v0.x ~/.reasonix/config.json's mcpServers. Provider api_key_env values
// resolve from Reasonix's global .env, not from project .env files.
func Load() (*Config, error) {
	return LoadForRoot(".")
}

// LoadForRoot builds the configuration with project files resolved from root
// instead of the current working directory. When root is "" or ".", it behaves
// like Load(). This is the workspace-aware entry point: desktop tabs use it so
// each project's reasonix.toml + .mcp.json are resolved independently without
// changing the process cwd, while provider keys stay rooted in Reasonix home.
//
// Note: LoadForRoot may rewrite legacy MCP `tier` lines on disk (see
// mergeRuntimeTOMLFile). Callers that must not mutate config files should use
// LoadForRootReadOnly instead.
func LoadForRoot(root string) (*Config, error) {
	return loadForRoot(root, true)
}

// LoadForRootReadOnly is like LoadForRoot but never writes config files: it skips
// on-disk legacy MCP tier migration. Prefer this for diagnostics, doctor, and
// other read-only inspection paths.
func LoadForRootReadOnly(root string) (*Config, error) {
	return loadForRoot(root, false)
}

func loadForRoot(root string, migrateOnDisk bool) (*Config, error) {
	root = resolveRoot(root)
	if SafeModeRequested() {
		return loadSafeModeForRoot(root), nil
	}
	expansionEnv := loadDotEnvForRoot(root)
	cfg := Default()
	cfg.setExpansionEnv(expansionEnv)
	cfg.CredentialsStore = credentialsStoreMode()

	projectTOML := "reasonix.toml"
	if root != "." {
		projectTOML = filepath.Join(root, "reasonix.toml")
	}

	mergeTOML := mergeFile
	if migrateOnDisk {
		mergeTOML = mergeRuntimeTOMLFile
	}

	var tomlSources []string
	userDefaultModelExplicit := false
	if uc := userConfigLoadPath(); uc != "" {
		tomlSources = append(tomlSources, uc)
		if err := mergeTOML(cfg, uc); err != nil {
			return nil, err
		}
		userDefaultModelExplicit = tomlFileDefinesKey(uc, "default_model")
	}
	userDefaultModel := cfg.DefaultModel
	globalSecrets := cfg.Secrets

	tomlSources = append(tomlSources, projectTOML)
	if err := mergeTOML(cfg, projectTOML); err != nil {
		return nil, err
	}
	// Secret protection is a user-global security control: a cloned repo's
	// reasonix.toml must not be able to flip on the workflow-breaking env/path
	// protections.
	cfg.Secrets = globalSecrets
	// TOML decoding replaces [[plugins]] wholesale, so cfg.Plugins now holds
	// only the last file's. Re-merge by name across all sources (later wins) so a
	// project reasonix.toml doesn't drop the global config's MCP servers.
	// mergeTOMLPlugins only reads files; it does not run on-disk migrations.
	plugins, err := mergeTOMLPlugins(tomlSources)
	if err != nil {
		return nil, err
	}
	cfg.Plugins = plugins
	if providers, providerSources, shadowedProjectProviders, ok, err := mergeTOMLProviders(tomlSources); err != nil {
		return nil, err
	} else if ok {
		cfg.Providers = providers
		cfg.providerSources = providerSources
		cfg.shadowedProjectProviders = shadowedProjectProviders
	}
	if access, ok, err := mergeTOMLProviderAccess(tomlSources); err != nil {
		return nil, err
	} else if ok {
		cfg.Desktop.ProviderAccess = access
	}

	// Claude Code's .mcp.json (project root) is read last and merged into
	// [[plugins]], so a server configured for Claude works here unchanged.
	// reasonix.toml wins on a name collision (see mergeMCPJSON).
	mcpFile := mcpJSONFile
	if root != "." {
		mcpFile = filepath.Join(root, mcpJSONFile)
	}
	entries, err := loadMCPJSON(mcpFile)
	if err != nil {
		return nil, err
	}
	for i := range entries {
		entries[i] = restrictProjectMCPApprovalPolicy(entries[i])
	}
	cfg.mergeMCPJSON(entries)

	// Lowest priority before the one-time v1.9.1 MCP migration: the v0.x
	// ~/.reasonix/config.json's mcpServers. Once the migration marker exists, the
	// current config is authoritative even when it is empty; reading the legacy
	// source again would resurrect servers the user removed from current config.
	if !mcpGlobalMigrationComplete() {
		cfg.mergeMCPJSON(loadLegacyMCP(legacyConfigPath()))
	}
	_ = mergeInstalledPluginPackages(cfg, root)
	normalizePluginCommandLines(cfg)
	normalizeLegacyEffort(cfg)
	cfg.ignoredLegacyStepLimits = normalizeLegacyAgentStepLimits(cfg)
	normalizeLegacyMCPTiers(cfg)
	normalizeLegacyStepFunBaseURLs(cfg)
	normalizeLegacyMimoCustomProviders(cfg)
	normalizeLegacyProviderModels(cfg)
	normalizeDesktopOfficialProviderAccess(cfg)
	normalizeOfficialDeepSeekModels(cfg)
	applyDeepSeekOfficialDefaultPricing(cfg)
	backfillDeepSeekOfficialPrices(cfg)
	normalizeEffortConfig(cfg)
	backfillDeepSeekPro(cfg)
	if userDefaultModelExplicit {
		restoreUnresolvableProjectDefaultModel(cfg, userDefaultModel)
	}
	cfg.Agent.AutoPlan = userAutoPlanMode()
	cfg.CredentialsStore = credentialsStoreMode()
	cfg.setExpansionEnv(expansionEnv)
	resolveProviderCredentialsForRoot(root, cfg)
	return cfg, nil
}

// SafeModeRequested reports whether this process should ignore user/project
// runtime extensions and boot from built-in defaults. The environment switch is
// intentionally process-local; it never rewrites user configuration.
func SafeModeRequested() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("REASONIX_SAFE_MODE"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func loadSafeModeForRoot(root string) *Config {
	cfg := Default()
	cfg.safeMode = true
	cfg.Plugins = nil
	cfg.Skills = SkillsConfig{}
	cfg.Bot.Enabled = false
	cfg.Bot.Connections = nil
	cfg.Bot.Routes = nil
	cfg.Statusline.Command = ""
	cfg.LSP.Enabled = false
	cfg.Desktop.CheckUpdates = safeModeBoolPtr(false)
	// A Safe Mode boot never reads the user's config, so it cannot see (and
	// must not override) a telemetry/metrics opt-out recorded there. Force
	// every reporting path off instead of inheriting the enabled defaults.
	cfg.Desktop.Telemetry = safeModeBoolPtr(false)
	cfg.Desktop.Metrics = safeModeBoolPtr(false)
	cfg.setExpansionEnv(nil)
	cfg.CredentialsStore = credentialsStoreMode()
	resolveProviderCredentialsForRoot(root, cfg)
	return cfg
}

// LoadRecoveryDefaultsForRoot returns the same built-in-only configuration used
// by Safe Mode without reading or migrating user/project TOML. Recovery tools use
// it to reach an explicitly selected built-in provider when configuration is
// malformed; provider credentials still resolve only from Reasonix's global
// credential store.
func LoadRecoveryDefaultsForRoot(root string) *Config {
	return loadSafeModeForRoot(root)
}

func safeModeBoolPtr(v bool) *bool { return &v }

func (c *Config) setExpansionEnv(env map[string]string) {
	if c == nil {
		return
	}
	c.expansionEnv = cloneStringMap(env)
	for i := range c.Plugins {
		c.Plugins[i].expansionEnv = c.expansionEnv
	}
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func userAutoPlanMode() string {
	cfg := Default()
	if uc := userConfigLoadPath(); uc != "" {
		_ = mergeFile(cfg, uc)
	}
	switch strings.ToLower(strings.TrimSpace(cfg.Agent.AutoPlan)) {
	case "on", "ask":
		return "on"
	default:
		return "off"
	}
}

// restoreUnresolvableProjectDefaultModel falls back to the user/global
// default_model when a project reasonix.toml overrides it with a reference no
// configured provider serves (#4218). Pre-v1.11 persistence paths (e.g. the
// "always allow" writer) full-rendered ./reasonix.toml and pinned the built-in
// default_model ("deepseek-flash") into it; once the user's [[providers]]
// replaced the built-in presets, that stale name resolved to nothing and boot
// hard-failed in every launch from that folder. In-memory only — the project
// file is untouched, and a project override that does resolve still wins. The
// ignored value is kept so boot can surface a notice.
//
// Callers must only invoke this when the user config explicitly defines
// default_model: falling back to the built-in default would silently mask a
// broken ref when the project file is the user's only config, and that case
// must keep the actionable boot error (TestBuildUnknownModelErrorIsActionable).
func restoreUnresolvableProjectDefaultModel(c *Config, userDefault string) {
	if c == nil {
		return
	}
	if c.DefaultModel == userDefault {
		return
	}
	if _, ok := c.ResolveModel(c.DefaultModel); ok {
		return
	}
	if _, ok := c.ResolveModel(userDefault); !ok {
		return
	}
	c.ignoredProjectDefaultModel = c.DefaultModel
	c.DefaultModel = userDefault
}

// tomlFileDefinesKey reports whether the TOML file at path explicitly defines
// the given top-level key. Missing or unparseable files report false.
func tomlFileDefinesKey(path string, key ...string) bool {
	var f Config
	meta, err := decodeTOMLFile(path, &f)
	if err != nil {
		return false
	}
	return meta.IsDefined(key...)
}

// backfillDeepSeekPro restores deepseek-pro for configs the pre-fix setup wizard
// wrote with only deepseek-v4-flash: a keyless /models probe used to drop the Pro
// SKU, leaving users unable to switch to it. In-memory only — the user's file is
// untouched. Narrowly scoped to the official DeepSeek endpoint (which is known to
// serve pro) so a custom flash-only deployment isn't given an entry that 404s.
func backfillDeepSeekPro(c *Config) {
	const flashModel, proModel = "deepseek-v4-flash", "deepseek-v4-pro"
	var flash *ProviderEntry
	for i := range c.Providers {
		p := &c.Providers[i]
		if p.Name == "deepseek-pro" {
			return
		}
		for _, m := range p.ModelList() {
			switch m {
			case proModel:
				return // pro already reachable
			case flashModel:
				if strings.Contains(p.BaseURL, "api.deepseek.com") {
					flash = p
				}
			}
		}
	}
	if flash == nil {
		return
	}
	// If the user has explicitly curated a model list for the flash provider
	// (e.g. unchecked pro in Settings), respect that choice and do not backfill.
	if len(flash.Models) > 0 {
		return
	}
	for _, bp := range Default().Providers {
		if bp.Name == "deepseek-pro" {
			bp.APIKeyEnv = flash.APIKeyEnv
			bp.Price = deepSeekV4PriceForModel(c.DeepSeekOfficialPricingLanguage(), proModel)
			c.Providers = append(c.Providers, bp)
			return
		}
	}
}

func backfillDeepSeekOfficialPrices(c *Config) {
	if c == nil {
		return
	}
	defaults := deepSeekV4PricesForConfig(c)
	for i := range c.Providers {
		p := &c.Providers[i]
		if officialProviderKind(p) != "deepseek" {
			continue
		}
		if p.Price != nil {
			continue
		}
		if p.Prices == nil {
			p.Prices = map[string]*provider.Pricing{}
		}
		for model, price := range defaults {
			if p.HasModel(model) && p.Prices[model] == nil {
				p.Prices[model] = clonePricing(price)
			}
		}
	}
}

func officialProviderKind(p *ProviderEntry) string {
	if p == nil {
		return ""
	}
	u, err := url.Parse(strings.TrimSpace(p.BaseURL))
	if err != nil {
		return ""
	}
	if strings.EqualFold(u.Hostname(), "api.deepseek.com") {
		return "deepseek"
	}
	return ""
}

func resolveRoot(root string) string {
	if root == "" || root == "." {
		return "."
	}
	return filepath.Clean(root)
}

// normalizeLegacyEffort migrates the retired DeepSeek effort="off" (the old
// /thinking off that disabled thinking) to the provider default, so a config
// written by an older version keeps loading instead of erroring on a value the
// provider no longer accepts.
func normalizeLegacyEffort(c *Config) {
	for i := range c.Providers {
		if strings.EqualFold(strings.TrimSpace(c.Providers[i].Effort), "off") {
			c.Providers[i].Effort = ""
		}
	}
}

// mergeTOMLPlugins merges [[plugins]] across TOML sources by name (later source wins).
func mergeTOMLPlugins(paths []string) ([]PluginEntry, error) {
	var merged []PluginEntry
	index := map[string]int{}
	userApprovalPolicies := map[string]PluginEntry{}
	for _, path := range paths {
		if _, err := os.Stat(path); err != nil {
			continue
		}
		var f Config
		if _, err := decodeTOMLFile(path, &f); err != nil {
			return nil, fmt.Errorf("config %s: %w", path, err)
		}
		for _, p := range f.Plugins {
			p, _ = NormalizePluginCommandLine(p)
			if isUserConfigPath(path) {
				p.Source = MCPSourceUserConfig
				userApprovalPolicies[p.Name] = p
			} else {
				p.Source = MCPSourceProjectConfig
				p = restrictProjectMCPApprovalPolicy(p)
				if userPolicy, ok := userApprovalPolicies[p.Name]; ok {
					p = inheritUserMCPApprovalPolicy(p, userPolicy)
				}
			}
			if i, ok := index[p.Name]; ok {
				merged[i] = p
				continue
			}
			index[p.Name] = len(merged)
			merged = append(merged, p)
		}
	}
	return merged, nil
}

func inheritUserMCPApprovalPolicy(project, user PluginEntry) PluginEntry {
	project.DefaultToolsApprovalMode = user.DefaultToolsApprovalMode
	project.Tools = cloneMCPToolPolicies(user.Tools)
	project.ApprovalsReviewer = user.ApprovalsReviewer
	return project
}

// restrictProjectMCPApprovalPolicy keeps repository-controlled configuration
// monotonic with the user's global permission posture. Project files may make
// MCP calls stricter, but cannot silently auto-approve writers or route a human
// prompt to an automated reviewer.
func restrictProjectMCPApprovalPolicy(p PluginEntry) PluginEntry {
	if strings.EqualFold(strings.TrimSpace(p.DefaultToolsApprovalMode), "approve") {
		p.DefaultToolsApprovalMode = "auto"
	}
	if len(p.Tools) > 0 {
		p.Tools = cloneMCPToolPolicies(p.Tools)
		for name, policy := range p.Tools {
			if strings.EqualFold(strings.TrimSpace(policy.ApprovalMode), "approve") {
				policy.ApprovalMode = "auto"
				p.Tools[name] = policy
			}
		}
	}
	if strings.EqualFold(strings.TrimSpace(p.ApprovalsReviewer), "auto_review") {
		p.ApprovalsReviewer = "user"
	}
	return p
}

func cloneMCPToolPolicies(in map[string]MCPToolPolicy) map[string]MCPToolPolicy {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]MCPToolPolicy, len(in))
	for name, policy := range in {
		out[name] = policy
	}
	return out
}

// mergeTOMLProviders merges [[providers]] across TOML sources by provider name.
// User-global providers win over same-named project providers; project providers
// only fill names the global config does not define. Keep official legacy aliases
// distinct here: they can carry different default models and effort capabilities,
// and the later desktop normalization layer handles canonical Settings access.
func mergeTOMLProviders(paths []string) ([]ProviderEntry, map[string]providerSourceScope, []ProviderEntry, bool, error) {
	var merged []ProviderEntry
	var shadowedProject []ProviderEntry
	index := map[string]int{}
	sources := map[string]providerSourceScope{}
	saw := false
	for _, path := range paths {
		if _, err := os.Stat(path); err != nil {
			continue
		}
		var f Config
		if _, err := decodeTOMLFile(path, &f); err != nil {
			return nil, nil, nil, false, fmt.Errorf("config %s: %w", path, err)
		}
		if len(f.Providers) == 0 {
			continue
		}
		saw = true
		source := providerSourceForPath(path)
		for _, p := range f.Providers {
			normalizeProviderEffortFields(&p)
			key := providerMergeKey(p)
			if i, ok := index[key]; ok {
				if sources[key] == providerSourceProject && source == providerSourceUser {
					shadowedProject = append(shadowedProject, merged[i])
					merged[i] = p
					sources[key] = source
				} else if sources[key] == providerSourceUser && source == providerSourceProject {
					shadowedProject = append(shadowedProject, p)
				}
				continue
			} else {
				index[key] = len(merged)
				merged = append(merged, p)
				sources[key] = source
			}
		}
	}
	return merged, sources, shadowedProject, saw, nil
}

func providerSourceForPath(path string) providerSourceScope {
	if isUserConfigPath(path) {
		return providerSourceUser
	}
	return providerSourceProject
}

func providerMergeKey(p ProviderEntry) string {
	return strings.TrimSpace(p.Name)
}

// mergeTOMLProviderAccess merges desktop.provider_access across TOML sources so
// project desktop settings do not hide account-level providers from the desktop
// model switcher.
func mergeTOMLProviderAccess(paths []string) ([]string, bool, error) {
	var merged []string
	seen := map[string]bool{}
	saw := false
	for _, path := range paths {
		if _, err := os.Stat(path); err != nil {
			continue
		}
		var f Config
		meta, err := decodeTOMLFile(path, &f)
		if err != nil {
			return nil, false, fmt.Errorf("config %s: %w", path, err)
		}
		if !meta.IsDefined("desktop", "provider_access") {
			continue
		}
		saw = true
		for _, name := range f.Desktop.ProviderAccess {
			name = strings.TrimSpace(name)
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			merged = append(merged, name)
		}
	}
	return merged, saw, nil
}

// ConfigFileDeclarations contains provider settings explicitly declared by one
// TOML file, without defaults or values inherited from another scope.
type ConfigFileDeclarations struct {
	ProviderNames                 []string
	DesktopProviderAccessDeclared bool
}

// InspectConfigFileDeclarations returns the provider-related fields explicitly
// present in one TOML file. It deliberately does not include built-in defaults
// or values inherited from another config scope.
func InspectConfigFileDeclarations(path string) (ConfigFileDeclarations, error) {
	var declarations ConfigFileDeclarations
	path = strings.TrimSpace(path)
	if path == "" {
		return declarations, nil
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return declarations, nil
		}
		return declarations, err
	}
	var f Config
	meta, err := decodeTOMLFile(path, &f)
	if err != nil {
		return declarations, fmt.Errorf("config %s: %w", path, err)
	}
	seen := make(map[string]bool, len(f.Providers))
	for _, provider := range f.Providers {
		name := strings.TrimSpace(provider.Name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		declarations.ProviderNames = append(declarations.ProviderNames, name)
	}
	declarations.DesktopProviderAccessDeclared = meta.IsDefined("desktop", "provider_access")
	return declarations, nil
}

// DesktopProviderAccessDeclared reports whether path explicitly declares
// desktop.provider_access. It distinguishes omission from an intentional [].
func DesktopProviderAccessDeclared(path string) (bool, error) {
	declarations, err := InspectConfigFileDeclarations(path)
	return declarations.DesktopProviderAccessDeclared, err
}

// LoadForEdit returns a config to seed the `reasonix setup` wizard when reconfiguring:
// the built-in defaults with the file at path (if present) decoded on top, so a
// reconfigure preserves the user's existing providers and agent settings instead
// of resetting to defaults. Reasonix's global .env is loaded so api_key_env
// resolution works while the wizard decides which keys are still missing.
func LoadForEdit(path string) *Config {
	return loadForEdit(path, true, true)
}

// LoadForEditReadOnlyStrict is the error-returning commit-time variant. It must
// not fall back to defaults when another writer leaves malformed TOML, because
// saving that fallback would overwrite the user's recoverable file.
func LoadForEditReadOnlyStrict(path string) (*Config, error) {
	return loadForEditStrict(path, true, false)
}

// ValidateFile parses one TOML config in isolation without loading credentials,
// applying migrations, or writing the file. A missing file is valid.
func ValidateFile(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	cfg := Default()
	if _, err := decodeTOMLFile(path, cfg); err != nil {
		return fmt.Errorf("config %s: %w", path, err)
	}
	return nil
}

func loadForEdit(path string, loadCredentials, persistMigrations bool) *Config {
	cfg, err := loadForEditStrict(path, loadCredentials, persistMigrations)
	if err == nil {
		return cfg
	}
	slog.Warn("config: load for edit failed, using defaults", "path", path, "err", err)
	if loadCredentials {
		loadDotEnvForEditPath(path)
	}
	cfg = Default()
	normalizeConfigForEdit(cfg)
	return cfg
}

func LoadForEditWithoutCredentials(path string) *Config {
	return loadForEdit(path, false, true)
}

func loadForEditStrict(path string, loadCredentials, persistMigrations bool) (*Config, error) {
	if loadCredentials {
		loadDotEnvForEditPath(path)
	}
	cfg := Default()
	if persistMigrations {
		if _, err := os.Stat(path); err == nil {
			if err := migrateLegacyMCPTiersFile(path); err != nil {
				return nil, fmt.Errorf("config %s: %w", path, err)
			}
		}
	}
	if err := mergeFile(cfg, path); err != nil {
		return nil, err
	}
	changed := normalizeConfigForEdit(cfg)
	if persistMigrations && changed && strings.TrimSpace(path) != "" {
		if _, err := os.Stat(path); err == nil {
			if err := cfg.SaveTo(path); err != nil {
				return nil, err
			}
		}
	}
	return cfg, nil
}

func normalizeConfigForEdit(cfg *Config) bool {
	normalizePluginCommandLines(cfg)
	normalizeLegacyEffort(cfg)
	normalizeLegacyAgentStepLimits(cfg)
	normalizeLegacyMCPTiers(cfg)
	changed := normalizeLegacyStepFunBaseURLs(cfg)
	changed = normalizeLegacyMimoCustomProviders(cfg) || changed
	normalizeLegacyProviderModels(cfg)
	normalizeDesktopOfficialProviderAccess(cfg)
	applyDeepSeekOfficialDefaultPricing(cfg)
	backfillDeepSeekOfficialPrices(cfg)
	normalizeEffortConfig(cfg)
	return changed
}

func loadDotEnvForEditPath(path string) {
	path = strings.TrimSpace(path)
	if path == "" || isUserConfigPath(path) {
		loadDotEnv()
		return
	}
	loadDotEnvForRoot(filepath.Dir(path))
}

// mergeFile decodes a TOML file onto cfg if it exists. An absent file is not an error.
func mergeFile(cfg *Config, path string) error {
	if _, err := os.Stat(path); err != nil {
		return nil
	}
	if _, err := decodeTOMLFile(path, cfg); err != nil {
		return fmt.Errorf("config %s: %w", path, err)
	}
	return nil
}

func mergeRuntimeTOMLFile(cfg *Config, path string) error {
	if _, err := os.Stat(path); err == nil {
		if err := migrateLegacyMCPTiersFile(path); err != nil {
			slog.Warn("config: legacy mcp tier migration failed", "path", path, "err", err)
		}
	}
	return mergeFile(cfg, path)
}

// normalizeLegacyMCPTiers keeps loaded legacy config files on the new product
// behavior: enabled MCP servers connect in the background by default, and the
// retired per-server startup tier is no longer a user-facing setting.
func normalizeLegacyMCPTiers(c *Config) {
	if c == nil {
		return
	}
	for i := range c.Plugins {
		c.Plugins[i].Tier = ""
	}
}

// normalizeLegacyAgentStepLimits keeps old TOML readable without allowing a
// stale hidden value to override the adaptive progress policy. The fields stay
// in AgentConfig for decoder and cross-version desktop compatibility only.
func normalizeLegacyAgentStepLimits(c *Config) bool {
	if c == nil {
		return false
	}
	found := c.Agent.MaxSteps != 0 || c.Agent.PlannerMaxSteps != 0
	c.Agent.MaxSteps = 0
	c.Agent.PlannerMaxSteps = 0
	return found
}

// retiredConfigKeyMigrationMu serializes the independent one-time removals
// below. They may target the same user/project TOML files from concurrent
// desktop builds, so separate locks could race and restore a key another
// migration had just removed.
var retiredConfigKeyMigrationMu sync.Mutex

// MigrateLegacyAgentStepLimitsForRoot removes retired [agent] step-limit keys
// from the user and project config selected for root. Boot calls it immediately
// before LoadForRoot, so config-only/read-only commands never rewrite files and
// the runtime can surface exactly one migration notice.
func MigrateLegacyAgentStepLimitsForRoot(root string) (bool, error) {
	root = resolveRoot(root)
	paths := make([]string, 0, 2)
	if userPath := userConfigLoadPath(); userPath != "" {
		paths = append(paths, userPath)
	}
	projectPath := "reasonix.toml"
	if root != "." {
		projectPath = filepath.Join(root, "reasonix.toml")
	}
	paths = append(paths, projectPath)

	changedAny := false
	seen := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		clean := filepath.Clean(path)
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}
		changed, err := migrateLegacyAgentStepLimitsFile(path)
		if err != nil {
			return changedAny, fmt.Errorf("migrate deprecated agent step limits in %s: %w", path, err)
		}
		changedAny = changedAny || changed
	}
	return changedAny, nil
}

// migrateLegacyAgentStepLimitsFile removes retired [agent] step-limit keys
// before runtime decoding. A process-wide lock makes concurrent desktop tab
// builds observe a single migration; the atomic rewrite protects other readers.
func migrateLegacyAgentStepLimitsFile(path string) (bool, error) {
	retiredConfigKeyMigrationMu.Lock()
	defer retiredConfigKeyMigrationMu.Unlock()

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	raw, err := fileencoding.ReadFileUTF8(path)
	if err != nil {
		return false, err
	}
	next, changed := stripLegacyAgentStepLimitLines(string(raw))
	if !changed {
		return false, nil
	}
	if err := fileutil.AtomicWriteFile(path, []byte(next), info.Mode().Perm()); err != nil {
		return false, err
	}
	return true, nil
}

func stripLegacyAgentStepLimitLines(raw string) (string, bool) {
	return stripTOMLKeyLines(raw, "agent", "max_steps", "planner_max_steps")
}

// MigrateLegacyRedactToolOutputForRoot removes the retired
// [secrets].redact_tool_output setting from the user and project configs chosen
// for root. The setting no longer controls any runtime behavior; removing it
// avoids leaving an explicit `true` value on disk that falsely suggests live
// output or transcript redaction is still active.
func MigrateLegacyRedactToolOutputForRoot(root string) (bool, error) {
	root = resolveRoot(root)
	paths := make([]string, 0, 2)
	if userPath := userConfigLoadPath(); userPath != "" {
		paths = append(paths, userPath)
	}
	projectPath := "reasonix.toml"
	if root != "." {
		projectPath = filepath.Join(root, "reasonix.toml")
	}
	paths = append(paths, projectPath)

	changedAny := false
	seen := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		clean := filepath.Clean(path)
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}
		changed, err := migrateLegacyRedactToolOutputFile(path)
		if err != nil {
			return changedAny, fmt.Errorf("migrate deprecated redact_tool_output in %s: %w", path, err)
		}
		changedAny = changedAny || changed
	}
	return changedAny, nil
}

func migrateLegacyRedactToolOutputFile(path string) (bool, error) {
	retiredConfigKeyMigrationMu.Lock()
	defer retiredConfigKeyMigrationMu.Unlock()

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	raw, err := fileencoding.ReadFileUTF8(path)
	if err != nil {
		return false, err
	}
	next, changed := stripLegacyRedactToolOutputLines(string(raw))
	if !changed {
		return false, nil
	}
	if err := fileutil.AtomicWriteFile(path, []byte(next), info.Mode().Perm()); err != nil {
		return false, err
	}
	return true, nil
}

func stripLegacyRedactToolOutputLines(raw string) (string, bool) {
	return stripTOMLKeyLines(raw, "secrets", "redact_tool_output")
}

// MigrateLegacyMemoryCompilerForRoot removes the retired
// [agent].memory_compiler setting from the user and project configs chosen for
// root. The Memory v5 execution compiler was removed; stripping the key avoids
// leaving values on disk that falsely suggest compiler behavior (especially a
// stale verbosity = "compact") is still active.
func MigrateLegacyMemoryCompilerForRoot(root string) (bool, error) {
	root = resolveRoot(root)
	paths := make([]string, 0, 2)
	if userPath := userConfigLoadPath(); userPath != "" {
		paths = append(paths, userPath)
	}
	projectPath := "reasonix.toml"
	if root != "." {
		projectPath = filepath.Join(root, "reasonix.toml")
	}
	paths = append(paths, projectPath)

	changedAny := false
	seen := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		clean := filepath.Clean(path)
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}
		changed, err := migrateLegacyMemoryCompilerFile(path)
		if err != nil {
			return changedAny, fmt.Errorf("migrate deprecated memory_compiler in %s: %w", path, err)
		}
		changedAny = changedAny || changed
	}
	return changedAny, nil
}

func migrateLegacyMemoryCompilerFile(path string) (bool, error) {
	retiredConfigKeyMigrationMu.Lock()
	defer retiredConfigKeyMigrationMu.Unlock()

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	raw, err := fileencoding.ReadFileUTF8(path)
	if err != nil {
		return false, err
	}
	next, changed := stripLegacyMemoryCompilerLines(string(raw))
	if !changed {
		return false, nil
	}
	if err := fileutil.AtomicWriteFile(path, []byte(next), info.Mode().Perm()); err != nil {
		return false, err
	}
	return true, nil
}

func stripLegacyMemoryCompilerLines(raw string) (string, bool) {
	return stripTOMLKeyLines(raw, "agent", "memory_compiler")
}

func migrateLegacyMCPTiersFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	raw, err := fileencoding.ReadFileUTF8(path)
	if err != nil {
		return err
	}
	next, changed := stripLegacyMCPTierLines(string(raw))
	if !changed {
		return nil
	}
	return os.WriteFile(path, []byte(next), info.Mode().Perm())
}

func stripLegacyMCPTierLines(raw string) (string, bool) {
	return stripTOMLKeyLines(raw, "plugins", "tier")
}

// tomlStringState tracks whether a line-oriented scan is currently inside a
// TOML multiline string, so retired-key strippers never treat prose inside a
// `"""..."""` or `”'...”'` value (e.g. a config example quoted in a
// system_prompt) as a section header or key assignment.
type tomlStringState int

const (
	tomlOutside tomlStringState = iota
	tomlInMultilineBasic
	tomlInMultilineLiteral
)

// advanceTOMLStringState scans one raw line and returns the multiline-string
// state after it. Outside strings it honours single-line strings and `#`
// comments so quote delimiters inside them cannot open a multiline state.
// The scan is intentionally conservative: on malformed input it prefers
// staying/returning outside, which makes callers keep lines rather than
// delete them.
func advanceTOMLStringState(state tomlStringState, line string) tomlStringState {
	i := 0
	for i < len(line) {
		switch state {
		case tomlInMultilineBasic:
			if line[i] == '\\' {
				i += 2
				continue
			}
			if strings.HasPrefix(line[i:], `"""`) {
				state = tomlOutside
				i += 3
				continue
			}
			i++
		case tomlInMultilineLiteral:
			if strings.HasPrefix(line[i:], "'''") {
				state = tomlOutside
				i += 3
				continue
			}
			i++
		default: // tomlOutside
			switch {
			case line[i] == '#':
				return state // rest of the line is a comment
			case strings.HasPrefix(line[i:], `"""`):
				state = tomlInMultilineBasic
				i += 3
			case strings.HasPrefix(line[i:], "'''"):
				state = tomlInMultilineLiteral
				i += 3
			case line[i] == '"': // single-line basic string
				i++
				for i < len(line) && line[i] != '"' {
					if line[i] == '\\' {
						i++
					}
					i++
				}
				i++ // closing quote (or line end on malformed input)
			case line[i] == '\'': // single-line literal string
				i++
				for i < len(line) && line[i] != '\'' {
					i++
				}
				i++
			default:
				i++
			}
		}
	}
	return state
}

// stripTOMLKeyLines removes top-level `key = ...` assignment lines under the
// named section while leaving every line inside a TOML multiline string
// untouched. All retired-config-key migrations share it so none of them can
// corrupt a multiline value (such as a system_prompt quoting a config
// example). A dropped line is first checked to not itself open a multiline
// value; if it would, the line is kept — for these retired keys that never
// happens (their values are single-line), and keeping a stale line is always
// safer than truncating a string the user wrote.
func stripTOMLKeyLines(raw, section string, keys ...string) (string, bool) {
	lines := strings.Split(raw, "\n")
	current := ""
	state := tomlOutside
	changed := false
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if state != tomlOutside {
			// Inside a multiline string: never a section header or key line.
			out = append(out, line)
			state = advanceTOMLStringState(state, line)
			continue
		}
		if header := tomlSectionHeader(line); header != "" {
			current = header
		}
		next := advanceTOMLStringState(tomlOutside, line)
		if current == section && next == tomlOutside {
			dropped := false
			for _, key := range keys {
				if isTOMLKeyAssignment(line, key) {
					changed = true
					dropped = true
					break
				}
			}
			if dropped {
				continue
			}
		}
		out = append(out, line)
		state = next
	}
	return strings.Join(out, "\n"), changed
}

func tomlSectionHeader(line string) string {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "[") {
		return ""
	}
	if i := strings.Index(trimmed, "#"); i >= 0 {
		trimmed = strings.TrimSpace(trimmed[:i])
	}
	if strings.HasPrefix(trimmed, "[[") && strings.HasSuffix(trimmed, "]]") {
		return strings.TrimSpace(trimmed[2 : len(trimmed)-2])
	}
	if strings.HasSuffix(trimmed, "]") {
		return strings.TrimSpace(trimmed[1 : len(trimmed)-1])
	}
	return "other"
}

func isTOMLKeyAssignment(line, key string) bool {
	trimmed := strings.TrimSpace(line)
	if strings.HasPrefix(trimmed, "#") || !strings.HasPrefix(trimmed, key) {
		return false
	}
	rest := strings.TrimSpace(strings.TrimPrefix(trimmed, key))
	return strings.HasPrefix(rest, "=")
}

// normalizeLegacyProviderModels repairs provider entries written by older
// desktop builds that carried the official provider name/endpoint but omitted the
// model field. The repair is intentionally narrow: valid user-provided model
// lists are left untouched, while known official aliases get the model implied by
// their preset name so model pickers and provider validation have an option.
func normalizeLegacyProviderModels(c *Config) {
	if c == nil {
		return
	}
	for i := range c.Providers {
		p := &c.Providers[i]
		if providerHasAnyModel(*p) {
			continue
		}
		if model := legacyOfficialProviderModel(p.Name); model != "" {
			p.Model = model
		}
	}
}

const (
	legacyStepFunOpenAIBaseURL      = "https://api.stepfun.ai/step_plan/v1"
	officialStepFunOpenAIBaseURL    = "https://api.stepfun.com/step_plan/v1"
	legacyStepFunAnthropicBaseURL   = "https://api.stepfun.ai/step_plan"
	officialStepFunAnthropicBaseURL = "https://api.stepfun.com/step_plan"
)

func normalizeLegacyStepFunBaseURLs(c *Config) bool {
	if c == nil {
		return false
	}
	changed := false
	for i := range c.Providers {
		p := &c.Providers[i]
		switch {
		case isLegacyStepFunPresetProvider(*p, "stepfun", "openai") && normalizedBaseURLForMigration(p.BaseURL) == legacyStepFunOpenAIBaseURL:
			p.BaseURL = officialStepFunOpenAIBaseURL
			changed = true
		case isLegacyStepFunPresetProvider(*p, "stepfun-anthropic", "anthropic") && normalizedBaseURLForMigration(p.BaseURL) == legacyStepFunAnthropicBaseURL:
			p.BaseURL = officialStepFunAnthropicBaseURL
			changed = true
		}
	}
	return changed
}

func isLegacyStepFunPresetProvider(p ProviderEntry, id, kind string) bool {
	if !strings.EqualFold(strings.TrimSpace(p.Kind), kind) {
		return false
	}
	return strings.TrimSpace(p.Name) == id || strings.TrimSpace(p.PresetID) == id
}

func normalizedBaseURLForMigration(raw string) string {
	return strings.TrimRight(strings.TrimSpace(raw), "/")
}

func normalizeLegacyMimoProviderCatalogs(c *Config) bool {
	if c == nil {
		return false
	}
	changed := false
	for i := range c.Providers {
		p := &c.Providers[i]
		if legacyMimoProviderName(p.Name) == "" || len(p.Models) > 0 {
			continue
		}
		switch officialProviderHost(p.BaseURL) {
		case "api.xiaomimimo.com":
			if applyLegacyMimoCatalog(p, legacyMimoAPIModels(), []string{"mimo-v2.5", "mimo-v2-omni"}, "mimo-v2.5-pro") {
				changed = true
			}
		case "token-plan-cn.xiaomimimo.com":
			if applyLegacyMimoCatalog(p, legacyMimoTokenPlanModels(), []string{"mimo-v2.5"}, "mimo-v2.5-pro") {
				changed = true
			}
		}
	}
	return changed
}

func applyLegacyMimoCatalog(p *ProviderEntry, models, visionModels []string, fallbackDefault string) bool {
	if p == nil || len(models) == 0 {
		return false
	}
	beforeModels := append([]string(nil), p.Models...)
	beforeVision := append([]string(nil), p.VisionModels...)
	beforeDefault := p.Default
	beforeModel := p.Model
	beforeWindow := p.ContextWindow
	beforeNoProxy := p.NoProxy
	beforePricesLen := len(p.Prices)

	currentDefault := strings.TrimSpace(p.Default)
	if currentDefault == "" {
		currentDefault = strings.TrimSpace(p.Model)
	}
	p.Models = mergeModelLists(models, p.ModelList())
	p.Model = p.Models[0]
	p.Default = firstKnownModel(currentDefault, p.Models, fallbackDefault)
	p.VisionModels = mergeModelLists(visionModels, p.VisionModels)
	backfillOfficialContextWindow(p, 1_048_576)
	p.NoProxy = true
	if p.Prices == nil {
		p.Prices = mimoDomesticPrices(models)
	} else {
		for model, price := range mimoDomesticPrices(models) {
			if p.Prices[model] == nil {
				p.Prices[model] = price
			}
		}
	}

	return !stringSlicesEqual(beforeModels, p.Models) ||
		!stringSlicesEqual(beforeVision, p.VisionModels) ||
		beforeDefault != p.Default ||
		beforeModel != p.Model ||
		beforeWindow != p.ContextWindow ||
		beforeNoProxy != p.NoProxy ||
		beforePricesLen != len(p.Prices)
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func normalizeOfficialDeepSeekModels(c *Config) {
	if c == nil {
		return
	}
	for i := range c.Providers {
		p := &c.Providers[i]
		if officialProviderHost(p.BaseURL) != "api.deepseek.com" {
			continue
		}
		switch strings.TrimSpace(p.Name) {
		case "deepseek":
			ensureProviderModels(p, []string{"deepseek-v4-flash", "deepseek-v4-pro"}, "deepseek-v4-flash")
		case "deepseek-flash":
			ensureProviderModels(p, []string{"deepseek-v4-flash"}, "deepseek-v4-flash")
		case "deepseek-pro":
			ensureProviderModels(p, []string{"deepseek-v4-pro"}, "deepseek-v4-pro")
		}
	}
}

func officialProviderHost(baseURL string) string {
	u, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

func ensureProviderModels(p *ProviderEntry, required []string, fallbackDefault string) {
	if p == nil {
		return
	}
	// If the user has explicitly curated a model list (via Settings), respect
	// that choice and do not merge additional required models.
	if len(p.Models) > 0 {
		return
	}
	models := mergeModelLists(required, p.ModelList())
	if len(models) == 0 {
		return
	}
	p.Model = models[0]
	if len(models) > 1 {
		p.Models = models
		p.Default = firstKnownModel(p.Default, models, fallbackDefault)
		return
	}
	p.Models = nil
	p.Default = ""
}

func legacyOfficialProviderModel(name string) string {
	switch strings.TrimSpace(name) {
	case "deepseek-flash":
		return "deepseek-v4-flash"
	case "deepseek-pro":
		return "deepseek-v4-pro"
	case "mimo", "xiaomi-mimo", "xiaomi_mimo", "mimo-api", "mimo-token-plan", "mimo-pro":
		return "mimo-v2.5-pro"
	case "mimo-flash":
		return "mimo-v2.5"
	default:
		return ""
	}
}

func normalizeLegacyMimoCustomProviders(c *Config) bool {
	return normalizeLegacyMimoCustomProvidersForRefs(c, legacyMimoConfigRefs(c)...)
}

// NormalizeLegacyMimoCustomProvidersForRefs appends custom OpenAI-compatible
// MiMo providers needed by legacy refs that live outside reasonix.toml, such as
// restored desktop tab state.
func NormalizeLegacyMimoCustomProvidersForRefs(c *Config, refs ...string) bool {
	return normalizeLegacyMimoCustomProvidersForRefs(c, refs...)
}

func normalizeLegacyMimoCustomProvidersForRefs(c *Config, refs ...string) bool {
	if c == nil {
		return false
	}
	needed := map[string]bool{}
	addRef := func(ref string) {
		if name := legacyMimoProviderNameForRef(ref); name != "" {
			needed[name] = true
		}
	}
	for _, ref := range refs {
		addRef(ref)
	}
	changed := normalizeLegacyMimoProviderCatalogs(c)
	for name := range needed {
		if _, ok := c.Provider(name); ok {
			continue
		}
		c.Providers = append(c.Providers, legacyMimoCustomProvider(name))
		changed = true
	}
	if normalizeLegacyMimoProviderCatalogs(c) {
		changed = true
	}
	return changed
}

func legacyMimoConfigRefs(c *Config) []string {
	if c == nil {
		return nil
	}
	refs := []string{
		c.DefaultModel,
		c.Agent.PlannerModel,
		c.Agent.SubagentModel,
		c.Agent.AutoPlanClassifier,
		c.Bot.Model,
	}
	for _, ref := range c.Agent.SubagentModels {
		refs = append(refs, ref)
	}
	for _, conn := range c.Bot.Connections {
		refs = append(refs, conn.Model)
	}
	refs = append(refs, c.Desktop.ProviderAccess...)
	return refs
}

func legacyMimoProviderName(ref string) string {
	switch strings.TrimSpace(ref) {
	case "mimo", "xiaomi-mimo", "xiaomi_mimo", "mimo-api", "mimo-token-plan", "mimo-pro", "mimo-flash":
		return strings.TrimSpace(ref)
	default:
		return ""
	}
}

func legacyMimoProviderNameForRef(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	providerName, _, hasModel := strings.Cut(ref, "/")
	if name := legacyMimoProviderName(providerName); name != "" {
		return name
	}
	if hasModel {
		return ""
	}
	switch ref {
	case "mimo-v2.5-pro":
		return "mimo-pro"
	case "mimo-v2.5":
		return "mimo-flash"
	case "mimo-v2-omni":
		return "mimo-api"
	default:
		return ""
	}
}

func legacyMimoAPIModels() []string {
	return []string{"mimo-v2.5-pro", "mimo-v2.5", "mimo-v2-omni"}
}

func legacyMimoTokenPlanModels() []string {
	return []string{"mimo-v2.5-pro", "mimo-v2.5"}
}

func legacyMimoCustomProvider(name string) ProviderEntry {
	switch strings.TrimSpace(name) {
	case "mimo", "xiaomi-mimo", "xiaomi_mimo", "mimo-api":
		models := legacyMimoAPIModels()
		return ProviderEntry{
			Name:          strings.TrimSpace(name),
			Kind:          "openai",
			BaseURL:       "https://api.xiaomimimo.com/v1",
			Models:        models,
			VisionModels:  []string{"mimo-v2.5", "mimo-v2-omni"},
			Default:       "mimo-v2.5-pro",
			APIKeyEnv:     "MIMO_API_KEY",
			ContextWindow: 1_048_576,
			Prices:        mimoDomesticPrices(models),
			NoProxy:       true,
		}
	case "mimo-token-plan":
		models := legacyMimoTokenPlanModels()
		return ProviderEntry{
			Name:          "mimo-token-plan",
			Kind:          "openai",
			BaseURL:       "https://token-plan-cn.xiaomimimo.com/v1",
			Models:        models,
			VisionModels:  []string{"mimo-v2.5"},
			Default:       "mimo-v2.5-pro",
			APIKeyEnv:     "MIMO_API_KEY",
			ContextWindow: 1_048_576,
			Prices:        mimoDomesticPrices(models),
			NoProxy:       true,
		}
	case "mimo-flash":
		return ProviderEntry{Name: "mimo-flash", Kind: "openai", BaseURL: "https://token-plan-cn.xiaomimimo.com/v1", Model: "mimo-v2.5", APIKeyEnv: "MIMO_API_KEY", ContextWindow: 1_000_000, Price: mimoV25Price(), NoProxy: true}
	default:
		return ProviderEntry{Name: "mimo-pro", Kind: "openai", BaseURL: "https://token-plan-cn.xiaomimimo.com/v1", Model: "mimo-v2.5-pro", APIKeyEnv: "MIMO_API_KEY", ContextWindow: 1_000_000, Price: mimoV25ProPrice(), NoProxy: true}
	}
}

func normalizeDesktopOfficialProviderAccess(c *Config) {
	if c == nil || len(c.Desktop.ProviderAccess) == 0 {
		return
	}
	seen := desktopProviderAccessMap(nil)
	next := make([]string, 0, len(c.Desktop.ProviderAccess))
	for _, name := range c.Desktop.ProviderAccess {
		name = desktopProviderAccessNameForConfig(c, name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		next = append(next, name)
	}
	c.Desktop.ProviderAccess = next
	if seen["deepseek"] {
		ensureDeepSeekOfficialProvider(c)
	}
	normalizeLegacyMimoProviderCatalogs(c)
	retargetDesktopOfficialRefs(c, seen)
}

// NormalizeLegacyDesktopProviderAccess seeds the desktop provider-access list
// for configs written before Settings tracked explicit provider access. Callers
// should only use this when they know the TOML did not declare provider_access;
// an explicit empty list means the user removed all access entries.
func NormalizeLegacyDesktopProviderAccess(c *Config) {
	if c == nil || len(c.Desktop.ProviderAccess) > 0 {
		return
	}
	seen := desktopProviderAccessMap(nil)
	var access []string
	add := func(name string) {
		name = desktopProviderAccessNameForConfig(c, name)
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		access = append(access, name)
	}
	addRef := func(ref string) {
		if entry, ok := c.ResolveModel(ref); ok {
			if !entry.Configured() {
				return
			}
			add(entry.Name)
		}
	}
	addRef(c.DefaultModel)
	addRef(c.Agent.PlannerModel)
	addRef(c.Agent.SubagentModel)
	addRef(c.Agent.AutoPlanClassifier)
	for _, ref := range c.Agent.SubagentModels {
		addRef(ref)
	}
	addRef(c.Bot.Model)
	for _, conn := range c.Bot.Connections {
		addRef(conn.Model)
	}
	for i := range c.Providers {
		p := &c.Providers[i]
		if legacyMimoProviderName(p.Name) != "" && len(p.ModelList()) > 0 {
			add(p.Name)
			continue
		}
		if p.Configured() && len(p.ModelList()) > 0 {
			add(p.Name)
		}
	}
	if len(access) == 0 {
		return
	}
	c.Desktop.ProviderAccess = access
	normalizeDesktopOfficialProviderAccess(c)
}

func canonicalDesktopOfficialProviderName(name string) string {
	switch strings.TrimSpace(name) {
	case "deepseek-flash", "deepseek-pro":
		return "deepseek"
	default:
		return strings.TrimSpace(name)
	}
}

func desktopProviderAccessNameForConfig(c *Config, name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	canonical := canonicalDesktopOfficialProviderName(name)
	if canonical == name {
		return name
	}
	if c == nil {
		return canonical
	}
	if p, ok := c.Provider(name); ok && !providerEntryMatchesCanonicalOfficialAccess(p, canonical) {
		return name
	}
	return canonical
}

func providerEntryMatchesCanonicalOfficialAccess(p *ProviderEntry, canonical string) bool {
	if p == nil {
		return false
	}
	switch canonical {
	case "deepseek":
		return officialProviderKind(p) == "deepseek"
	default:
		return false
	}
}

// CanonicalDesktopOfficialProviderName returns the Settings Center provider ID
// for built-in official provider aliases.
func CanonicalDesktopOfficialProviderName(name string) string {
	return canonicalDesktopOfficialProviderName(name)
}

func desktopProviderAccessMap(names []string) map[string]bool {
	out := map[string]bool{}
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name != "" {
			out[name] = true
		}
	}
	return out
}

func ensureDeepSeekOfficialProvider(c *Config) {
	if p, ok := c.Provider("deepseek"); ok {
		if officialProviderKind(p) == "deepseek" {
			backfillOfficialContextWindow(p, 1_000_000)
		}
		return
	}
	entry := ProviderEntry{
		Name:          "deepseek",
		Kind:          "openai",
		BaseURL:       "https://api.deepseek.com",
		Models:        []string{"deepseek-v4-flash", "deepseek-v4-pro"},
		Default:       "deepseek-v4-flash",
		APIKeyEnv:     "DEEPSEEK_API_KEY",
		BalanceURL:    "https://api.deepseek.com/user/balance",
		ContextWindow: 1_000_000,
		Prices:        deepSeekV4PricesForConfig(c),
	}
	if old, ok := c.Provider("deepseek-flash"); ok {
		entry = officialProviderFromLegacy(entry, old)
		entry.Prices = deepSeekV4PricesForConfig(c)
		entry.Models = mergeModelLists([]string{"deepseek-v4-flash", "deepseek-v4-pro"}, old.ModelList())
		entry.Default = firstKnownModel(entry.Default, entry.Models, "deepseek-v4-flash")
	}
	backfillOfficialContextWindow(&entry, 1_000_000)
	c.Providers = append(c.Providers, entry)
}

func isOpenAIProviderKind(e *ProviderEntry) bool {
	return e != nil && strings.EqualFold(strings.TrimSpace(e.Kind), "openai")
}

func mergeCuratedModelsIntoProvider(e *ProviderEntry, models []string, fallback string) {
	// If the user has explicitly curated a model list (via Settings), respect
	// that choice and do not merge additional curated models.
	if len(e.Models) > 0 {
		return
	}
	currentDefault := e.Default
	if strings.TrimSpace(currentDefault) == "" {
		currentDefault = e.Model
	}
	e.Models = mergeModelLists(models, e.ModelList())
	e.Default = firstKnownModel(currentDefault, e.Models, fallback)
}

func backfillOfficialContextWindow(e *ProviderEntry, fallback int) {
	if e != nil && e.ContextWindow <= 0 {
		e.ContextWindow = fallback
	}
}

func officialProviderFromLegacy(entry ProviderEntry, old *ProviderEntry) ProviderEntry {
	entry.Kind = old.Kind
	entry.BaseURL = old.BaseURL
	entry.ModelsURL = old.ModelsURL
	entry.APIKeyEnv = old.APIKeyEnv
	entry.BalanceURL = old.BalanceURL
	entry.ContextWindow = old.ContextWindow
	entry.Price = old.Price
	entry.Thinking = old.Thinking
	entry.Effort = old.Effort
	entry.ReasoningProtocol = old.ReasoningProtocol
	entry.SupportedEfforts = append([]string(nil), old.SupportedEfforts...)
	entry.DefaultEffort = old.DefaultEffort
	entry.NoProxy = old.NoProxy
	return entry
}

func mergeModelLists(primary, extra []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(primary)+len(extra))
	for _, list := range [][]string{primary, extra} {
		for _, model := range list {
			model = strings.TrimSpace(model)
			if model == "" || seen[model] {
				continue
			}
			seen[model] = true
			out = append(out, model)
		}
	}
	return out
}

func firstKnownModel(current string, models []string, fallback string) string {
	current = strings.TrimSpace(current)
	for _, model := range models {
		if model == current {
			return current
		}
	}
	for _, model := range models {
		if model == fallback {
			return fallback
		}
	}
	if len(models) > 0 {
		return models[0]
	}
	return ""
}

func retargetDesktopOfficialRefs(c *Config, access map[string]bool) {
	c.DefaultModel = retargetDesktopOfficialRef(c.DefaultModel, access)
	c.Agent.PlannerModel = retargetDesktopOfficialRef(c.Agent.PlannerModel, access)
	c.Agent.SubagentModel = retargetDesktopOfficialRef(c.Agent.SubagentModel, access)
	c.Agent.AutoPlanClassifier = retargetDesktopOfficialRef(c.Agent.AutoPlanClassifier, access)
	for skill, ref := range c.Agent.SubagentModels {
		c.Agent.SubagentModels[skill] = retargetDesktopOfficialRef(ref, access)
	}
}

func retargetDesktopOfficialRef(ref string, access map[string]bool) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	provider, model, hasModel := strings.Cut(ref, "/")
	switch provider {
	case "deepseek-flash":
		if !access["deepseek"] {
			return ref
		}
		if !hasModel || strings.TrimSpace(model) == "" {
			model = "deepseek-v4-flash"
		}
		return "deepseek/" + model
	case "deepseek-pro":
		if !access["deepseek"] {
			return ref
		}
		if !hasModel || strings.TrimSpace(model) == "" {
			model = "deepseek-v4-pro"
		}
		return "deepseek/" + model
	default:
		return ref
	}
}
