package config

import "strings"

type accountRouteTemplate struct {
	RouteID   string
	BaseName  string
	Optional  bool
	MainOnly  bool
	ExtraOnly bool
	Entry     ProviderEntry
}

func accountGroupIDForPresetID(id string) string {
	id = strings.ToLower(strings.TrimSpace(id))
	if mapped, ok := presetAccountGroupIDs[id]; ok {
		return mapped
	}
	if id == "" {
		return ""
	}
	return id
}

func (p ProviderPreset) resolvedAccountGroupID() string {
	if id := strings.TrimSpace(p.AccountGroupID); id != "" {
		return id
	}
	return accountGroupIDForPresetID(p.ID)
}

var presetAccountGroupIDs = map[string]string{
	"deepseek-anthropic":                "deepseek",
	"deepseek-responses":                "deepseek",
	"opencode-go":                       "opencode-go",
	"opencode-go-recommended":           "opencode-go",
	"opencode-go-anthropic":             "opencode-go",
	"opencode-go-responses":             "opencode-go",
	"opencode-go-deepseek-anthropic":    "opencode-go",
	"opencode-go-deepseek-responses":    "opencode-go",
	"longcat-openai":                    "longcat",
	"longcat-anthropic":                 "longcat",
	"minimax-cn-api":                    "minimax-cn",
	"minimax-cn-anthropic":              "minimax-cn",
	"minimax-global-api":                "minimax-global",
	"minimax-global-anthropic":          "minimax-global",
	"glm-coding-plan-cn":                "glm-coding-plan-cn",
	"glm-coding-plan-cn-anthropic":      "glm-coding-plan-cn",
	"zai-coding-plan-global":            "zai-coding-plan-global",
	"zai-coding-plan-global-anthropic":  "zai-coding-plan-global",
	"mimo-api":                          "mimo-api",
	"mimo-anthropic":                    "mimo-api",
	"mimo-token-plan-cn":                "mimo-token-plan-cn",
	"mimo-token-plan-cn-anthropic":      "mimo-token-plan-cn",
	"mimo-token-plan-sgp":               "mimo-token-plan-sgp",
	"mimo-token-plan-sgp-anthropic":     "mimo-token-plan-sgp",
	"mimo-token-plan-ams":               "mimo-token-plan-ams",
	"mimo-token-plan-ams-anthropic":     "mimo-token-plan-ams",
	"qwen-coding-plan-cn":               "qwen-coding-plan-cn",
	"qwen-coding-plan-cn-anthropic":     "qwen-coding-plan-cn",
	"qwen-coding-plan-global":           "qwen-coding-plan-global",
	"qwen-coding-plan-global-anthropic": "qwen-coding-plan-global",
	"stepfun":                           "stepfun",
	"stepfun-anthropic":                 "stepfun",
	"stepfun-responses":                 "stepfun",
	"stepfun-api":                       "stepfun-api",
	"stepfun-api-anthropic":             "stepfun-api",
	"scnet":                             "scnet",
	"scnet-anthropic":                   "scnet",
}

func knownProviderIdentity(name string) (groupID, routeID, baseName string, ok bool) {
	switch strings.TrimSpace(name) {
	case "deepseek-flash", "deepseek-pro", "deepseek", "deepseek-anthropic", "deepseek-responses":
		base := strings.TrimSpace(name)
		if base == "deepseek-anthropic" {
			base = "deepseek"
		}
		return "deepseek", base, strings.TrimSpace(name), true
	}
	return "", "", "", false
}

func curatedProviderIdentity(e ProviderEntry) (groupID, routeID, baseName string, ok bool) {
	if group, route, ok := ProviderAccountIdentity(e); ok {
		base := strings.TrimSpace(e.Name)
		if route != "" {
			base = route
		}
		return group, route, base, true
	}
	if id := strings.TrimSpace(e.PresetID); id != "" {
		if group := accountGroupIDForPresetID(id); group != "" {
			route := strings.TrimSpace(e.Name)
			if preset, found := CuratedProviderPreset(id); found && len(preset.Entries) == 1 {
				route = strings.TrimSpace(preset.Entries[0].Name)
			}
			if group == "deepseek" && (route == "deepseek-anthropic" || route == "") {
				route = "deepseek"
			}
			return group, route, strings.TrimSpace(e.Name), true
		}
	}
	if group, route, base, ok := knownProviderIdentity(e.Name); ok {
		return group, route, base, true
	}
	for _, preset := range curatedProviderPresets {
		group := accountGroupIDForPresetID(preset.ID)
		for _, ent := range preset.Entries {
			if ent.Name == e.Name {
				route := ent.Name
				if group == "deepseek" && route == "deepseek-anthropic" {
					route = "deepseek"
				}
				return group, route, ent.Name, true
			}
		}
	}
	return "", "", "", false
}

func accountRouteTemplates(groupID string) []accountRouteTemplate {
	groupID = strings.TrimSpace(groupID)
	if groupID == "" {
		return nil
	}
	if groupID == "deepseek" {
		return deepSeekAccountRouteTemplates()
	}
	var out []accountRouteTemplate
	seen := map[string]bool{}
	addPreset := func(preset ProviderPreset) {
		optional := preset.Optional
		for _, e := range preset.Entries {
			name := strings.TrimSpace(e.Name)
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			entry := cloneProviderEntry(e)
			out = append(out, accountRouteTemplate{
				RouteID:  name,
				BaseName: name,
				Optional: optional,
				Entry:    entry,
			})
		}
	}
	if groupID == "opencode-go" {
		if preset, ok := CuratedProviderPreset("opencode-go-recommended"); ok {
			addPreset(preset)
		}
	}
	for _, preset := range curatedProviderPresets {
		if accountGroupIDForPresetID(preset.ID) != groupID {
			continue
		}
		if preset.ID == "opencode-go-recommended" {
			continue
		}
		addPreset(preset)
	}
	return out
}

func deepSeekAccountRouteTemplates() []accountRouteTemplate {
	var flash, pro ProviderEntry
	for _, p := range Default().Providers {
		switch p.Name {
		case "deepseek-flash":
			flash = cloneProviderEntry(p)
		case "deepseek-pro":
			pro = cloneProviderEntry(p)
		}
	}
	combined := ProviderEntry{}
	if preset, ok := CuratedProviderPreset("deepseek-anthropic"); ok && len(preset.Entries) > 0 {
		combined = cloneProviderEntry(preset.Entries[0])
		combined.Name = "deepseek"
	}
	responses := ProviderEntry{}
	if preset, ok := CuratedProviderPreset("deepseek-responses"); ok && len(preset.Entries) > 0 {
		responses = cloneProviderEntry(preset.Entries[0])
	}
	return []accountRouteTemplate{
		{RouteID: "deepseek-flash", BaseName: "deepseek-flash", MainOnly: true, Entry: flash},
		{RouteID: "deepseek-pro", BaseName: "deepseek-pro", MainOnly: true, Entry: pro},
		{RouteID: "deepseek", BaseName: "deepseek", ExtraOnly: true, Entry: combined},
		{RouteID: "deepseek-responses", BaseName: "deepseek-responses", Optional: true, Entry: responses},
	}
}

func baseAPIKeyEnvForGroup(groupID string) string {
	groupID = strings.TrimSpace(groupID)
	if groupID == "deepseek" {
		return "DEEPSEEK_API_KEY"
	}
	for _, preset := range curatedProviderPresets {
		if accountGroupIDForPresetID(preset.ID) == groupID {
			if env := strings.TrimSpace(preset.KeyEnv); env != "" {
				return env
			}
			for _, e := range preset.Entries {
				if env := strings.TrimSpace(e.APIKeyEnv); env != "" {
					return env
				}
			}
		}
	}
	return ""
}

func defaultAccountLabel(accountID string) string {
	switch accountID {
	case MainProviderAccountID:
		return "Main"
	case "backup":
		return "Backup"
	case "team":
		return "Team"
	case "personal":
		return "Personal"
	default:
		if strings.HasPrefix(accountID, legacyAccountIDPrefix) {
			return "Legacy"
		}
		return accountID
	}
}
