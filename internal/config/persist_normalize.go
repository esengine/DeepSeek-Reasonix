package config

import (
	"reflect"
	"strings"
)

// normalizePersistedConfig returns the semantic view of c as the load path
// would produce it: the built-in defaults with c's explicitly set values
// overlaid (mirroring Default + merged file), then accessor normalization so
// renderings (theme -> UITheme(), nil pointers -> accessor defaults, and so
// on) compare by semantic value instead of raw field value.
func normalizePersistedConfig(c *Config) *Config {
	if c == nil {
		return nil
	}
	cp := mergeConfigOverDefaults(c)
	cp.CredentialsStore = normalizeCredentialsStore(c.CredentialsStore)

	// [ui] — the renderer writes accessor-normalized values.
	cp.UI.Theme = c.UITheme()
	cp.UI.ThemeStyle = c.UIThemeStyle()
	cp.UI.ShortcutLayout = c.UIShortcutLayout()
	cp.UI.CursorShape = c.UICursorShape()
	cp.UI.CloseBehavior = c.DesktopCloseBehavior()

	// [cli]
	cp.CLI.UpdateChannel = c.CLIUpdateChannel()

	// [desktop]
	cp.Desktop.Language = c.DesktopLanguage()
	cp.Desktop.Currency = c.DesktopCurrency()
	cp.Desktop.LayoutStyle = c.DesktopLayoutStyle()
	cp.Desktop.Theme = c.DesktopTheme()
	cp.Desktop.ThemeStyle = c.DesktopThemeStyle()
	cp.Desktop.ExternalOpener = c.DesktopExternalOpener()
	cp.Desktop.CloseBehavior = c.DesktopCloseBehavior()
	cp.Desktop.DisplayMode = c.DesktopDisplayMode()
	cp.Desktop.StatusBarStyle = c.DesktopStatusBarStyle()
	cp.Desktop.ConversationWidth = c.DesktopConversationWidth()
	cp.Desktop.UpdateChannel = c.DesktopUpdateChannel()
	cp.Desktop.DefaultToolApprovalMode = c.DesktopDefaultToolApprovalMode()
	cp.Desktop.StatusBarItems = append([]string(nil), c.DesktopStatusBarItems()...)
	checkUpdates := c.DesktopCheckUpdates()
	cp.Desktop.CheckUpdates = &checkUpdates
	telemetry := c.DesktopTelemetry()
	cp.Desktop.Telemetry = &telemetry
	metrics := c.DesktopMetrics()
	cp.Desktop.Metrics = &metrics

	// [telemetry]
	cp.Telemetry.CLIMetrics = c.CLITelemetryMode()

	// [network]
	cp.Network.ProxyMode = c.NetworkProxyMode()
	if strings.TrimSpace(cp.Network.Proxy.Type) == "" {
		cp.Network.Proxy.Type = "socks5"
	}

	// [agent]
	coldResume := c.ColdResumePruneEnabled()
	cp.Agent.ColdResumePrune = &coldResume
	cp.Agent.ReasoningLanguage = c.ReasoningLanguage()
	// Fields accepted from older configs but no longer persisted by the
	// renderer must not register as drift between the intended config and the
	// decoded candidate.
	cp.Agent.RecoveryTemperature = 0
	cp.Agent.AutoPlan = "off"
	cp.Agent.AutoPlanClassifier = ""

	// [environment] — enabled is rendered with the nil=>true default.
	if cp.Environment.Enabled == nil {
		enabled := true
		cp.Environment.Enabled = &enabled
	}

	// [tools] — pointer fields render their accessor defaults.
	bashTimeout := c.BashTimeoutSeconds()
	cp.Tools.BashTimeoutSeconds = &bashTimeout
	mcpTimeout := c.MCPCallTimeoutSeconds()
	cp.Tools.MCPCallTimeoutSeconds = &mcpTimeout
	mcpStartupTimeout := c.MCPStartupTimeoutSeconds()
	cp.Tools.MCPStartupTimeoutSeconds = &mcpStartupTimeout
	stalled := c.BackgroundJobStalledWarningSeconds()
	cp.Tools.BackgroundJobs.StalledWarningSeconds = &stalled

	// [sandbox]
	cp.Sandbox.Bash = c.BashMode()

	// [[plugins]] — Tier is a legacy compatibility field the renderer omits.
	for i := range cp.Plugins {
		cp.Plugins[i].Tier = ""
	}

	// [skills]
	cp.Skills.DisabledSkills = append([]string(nil), c.DisabledSkillNames()...)
	cp.Skills.MaxDepth = c.SkillMaxDepth()

	// [[providers]] — Default's built-in providers are implicit: decoding a
	// TOML body appends them ahead of the file's entries, so the intended side
	// must carry the same implicit set for the comparison to be meaningful.
	cp.Providers = withImplicitDefaultProviders(cp.Providers)
	// The renderer writes `default` only when the provider lists `models`,
	// and falls back to `model` when the list is empty — the field that is
	// not persisted must not register as drift.
	for i := range cp.Providers {
		if len(cp.Providers[i].Models) > 0 {
			cp.Providers[i].Model = ""
		} else {
			cp.Providers[i].Default = ""
		}
	}

	return cp
}

// withImplicitDefaultProviders returns providers with the built-in defaults
// injected ahead of any same-named explicit entry, matching the load path
// (Default + merged file).
func withImplicitDefaultProviders(providers []ProviderEntry) []ProviderEntry {
	return mergeProvidersWithDefaults(providers)
}

// mergeProvidersWithDefaults returns the implicit built-in providers followed
// by the explicit entries; an explicit entry with a built-in name replaces
// the default, and every non-built-in entry is preserved in order. This
// mirrors how the load path assembles providers (Default plus the merged
// file's entries).
func mergeProvidersWithDefaults(providers []ProviderEntry) []ProviderEntry {
	defaults := Default().Providers
	if len(defaults) == 0 {
		return providers
	}
	builtin := make(map[string]bool, len(defaults))
	for _, d := range defaults {
		if strings.TrimSpace(d.Name) != "" {
			builtin[d.Name] = true
		}
	}
	explicit := make(map[string]int, len(providers))
	for i, p := range providers {
		if strings.TrimSpace(p.Name) != "" {
			explicit[p.Name] = i
		}
	}
	out := make([]ProviderEntry, 0, len(defaults)+len(providers))
	for _, d := range defaults {
		if idx, ok := explicit[d.Name]; ok {
			out = append(out, providers[idx])
			continue
		}
		out = append(out, d)
	}
	// Every non-built-in provider must survive; built-in names were already
	// consumed by the replacement loop above (exactly once, at the default's
	// position).
	for _, p := range providers {
		if !builtin[p.Name] {
			out = append(out, p)
		}
	}
	return out
}

// mergeConfigOverDefaults returns the built-in defaults with c's explicitly
// set values overlaid — the same assembly the load path performs (Default +
// merged file). Bool fields always overlay (an explicit false is meaningful);
// strings, numbers and pointers overlay when non-zero; slices, maps and
// pointers when non-nil. This makes the intended side comparable with a
// decoded candidate regardless of whether c came from a load or was built
// by hand.
func mergeConfigOverDefaults(c *Config) *Config {
	if c == nil {
		return nil
	}
	cpValue := cloneConfigValue(reflect.ValueOf(Default()).Elem())
	cp := cpValue.Interface().(Config)
	overlayConfigFields(reflect.ValueOf(c).Elem(), reflect.ValueOf(&cp).Elem())
	return &cp
}

func overlayConfigFields(src, dst reflect.Value) {
	t := src.Type()
	for i := 0; i < src.NumField(); i++ {
		sf := t.Field(i)
		if sf.PkgPath != "" {
			continue
		}
		sv := src.Field(i)
		dv := dst.Field(i)
		if !shouldOverlayConfigField(sv) {
			continue
		}
		if sv.Kind() == reflect.Struct {
			overlayConfigFields(sv, dv)
			continue
		}
		dv.Set(cloneConfigValue(sv))
	}
}

func shouldOverlayConfigField(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Bool:
		return true // explicit false must overlay the default
	case reflect.String, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return !v.IsZero()
	case reflect.Slice, reflect.Map, reflect.Pointer, reflect.Interface:
		return !v.IsNil()
	case reflect.Struct:
		return true
	default:
		return false
	}
}

// cloneConfigValue deep-copies a config field value so overlay mutations can
// never alias the source config.
func cloneConfigValue(v reflect.Value) reflect.Value {
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			return reflect.Zero(v.Type())
		}
		out := reflect.New(v.Type().Elem())
		out.Elem().Set(cloneConfigValue(v.Elem()))
		return out
	case reflect.Slice:
		if v.IsNil() {
			return reflect.Zero(v.Type())
		}
		out := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		for i := 0; i < v.Len(); i++ {
			out.Index(i).Set(cloneConfigValue(v.Index(i)))
		}
		return out
	case reflect.Map:
		if v.IsNil() {
			return reflect.Zero(v.Type())
		}
		out := reflect.MakeMapWithSize(v.Type(), v.Len())
		iter := v.MapRange()
		for iter.Next() {
			out.SetMapIndex(iter.Key(), cloneConfigValue(iter.Value()))
		}
		return out
	case reflect.Struct:
		out := reflect.New(v.Type()).Elem()
		overlayConfigFields(v, out)
		return out
	default:
		return v
	}
}
