package config

import (
	"fmt"
	"strings"

	"reasonix/internal/permissionpreset"
)

// normalizeLoadedConfig applies post-merge compatibility repairs and validates
// mode enums in their established order.
func normalizeLoadedConfig(cfg *Config) error {
	normalizePermissionPresetFields(cfg)
	normalizePluginCommandLines(cfg)
	normalizeLegacyEffort(cfg)
	cfg.ignoredLegacyStepLimits = normalizeLegacyAgentStepLimits(cfg)
	normalizeRetiredAutoPlan(cfg)
	if err := validateCompletionValidationModes(cfg.Agent.CompletionValidation); err != nil {
		return err
	}
	normalizeLegacyMCPTiers(cfg)
	normalizeLegacyStepFunBaseURLs(cfg)
	normalizeLegacyLongCatContextWindows(cfg)
	normalizeLegacyQwenContextWindows(cfg)
	normalizeLegacyKimiK3Catalog(cfg)
	normalizeOpenCodeGoRuntimeCompatibility(cfg)
	normalizeLegacyOpenCodeGoInstalls(cfg)
	normalizeLegacyMimoCustomProviders(cfg)
	normalizeLegacyProviderModels(cfg)
	normalizeDesktopOfficialProviderAccess(cfg)
	normalizeOfficialDeepSeekModels(cfg)
	migrateBillingDisplayCurrency(cfg)
	freezeProviderBillingCurrencies(cfg)
	applyDeepSeekOfficialDefaultPricing(cfg)
	backfillDeepSeekOfficialPrices(cfg)
	normalizeEffortConfig(cfg)
	backfillDeepSeekPro(cfg)
	return nil
}

// normalizePermissionPresetFields is the load-time compatibility boundary for
// user-facing execution presets. Known legacy values are migrated to canonical
// names. Unknown non-empty values fail closed and produce a visible load
// warning rather than silently inheriting a more permissive default.
func normalizePermissionPresetFields(cfg *Config) {
	if cfg == nil {
		return
	}
	normalize := func(path string, value *string, emptyDefault bool) {
		raw := strings.ToLower(strings.TrimSpace(*value))
		if raw == "" {
			if emptyDefault {
				*value = string(permissionpreset.WorkspaceWrite)
			}
			return
		}
		known := permissionpreset.Valid(raw)
		if !known {
			switch raw {
			case "ask", "auto", "yolo", "readonly", "read_only", "workspace", "workspace_write", "danger_full_access", "full", "full-access", "bypass":
				known = true
			}
		}
		*value = string(permissionpreset.Normalize(raw))
		if !known {
			cfg.addLoadWarning(fmt.Sprintf("%s has unknown permission preset %q; using read-only", path, raw))
		}
	}
	normalize("desktop.default_tool_approval_mode", &cfg.Desktop.DefaultToolApprovalMode, true)
	normalize("bot.tool_approval_mode", &cfg.Bot.ToolApprovalMode, true)
	normalize("bot.qq.tool_approval_mode", &cfg.Bot.QQ.ToolApprovalMode, false)
	normalize("bot.dingtalk.tool_approval_mode", &cfg.Bot.Dingtalk.ToolApprovalMode, false)
	for i := range cfg.Bot.Connections {
		normalize(fmt.Sprintf("bot.connections[%d].tool_approval_mode", i), &cfg.Bot.Connections[i].ToolApprovalMode, false)
	}
	for i := range cfg.Bot.Routes {
		normalize(fmt.Sprintf("bot.routes[%d].tool_approval_mode", i), &cfg.Bot.Routes[i].ToolApprovalMode, false)
	}
}
