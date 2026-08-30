package main

import (
	"strings"

	"reasonix/internal/config"
)

func dingtalkConfigFromView(view DingtalkBotView, current config.DingtalkBotConfig) config.DingtalkBotConfig {
	return config.DingtalkBotConfig{
		Enabled:          view.Enabled,
		ClientID:         strings.TrimSpace(view.ClientID),
		ClientSecret:     current.ClientSecret,
		ClientIDEnv:      current.ClientIDEnv,
		SecretEnv:        strings.TrimSpace(view.ClientSecretEnv),
		BotName:          strings.TrimSpace(view.BotName),
		RequireMention:   view.RequireMention,
		Model:            strings.TrimSpace(view.Model),
		ToolApprovalMode: normalizeBotConnectionToolApprovalMode(view.ToolApprovalMode),
		WorkspaceRoot:    strings.TrimSpace(view.WorkspaceRoot),
		Access:           botAccessConfigFromView(view.Access),
		SessionMappings:  append([]config.BotConnectionSessionMapping(nil), current.SessionMappings...),
	}
}
