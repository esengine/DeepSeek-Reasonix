package config

import (
	"os"
	"strconv"
	"strings"

	"reasonix/internal/fileutil"
	fileencoding "reasonix/internal/fileutil/encoding"
)

const deepSeekChatDefaultConfigVersion = 8
const deepSeekOfficialChatUpgradeConfigVersion = 9

// Official endpoint identity, rather than the editable display name, owns this
// one-time migration. Nonstandard paths and gateways remain user-owned.
func isOfficialDeepSeekChatUpgrade(p *ProviderEntry) bool {
	if p == nil || (p.Kind != "anthropic" && p.Kind != "responses" && p.Kind != "openai") {
		return false
	}
	for _, endpoint := range []string{p.BaseURL, p.RequestURL, p.ChatURL} {
		if endpoint == "" {
			continue
		}
		path, ok := deepSeekOpenAIEndpointPath(endpoint)
		if !ok {
			return false
		}
		switch path {
		case "", "/v1", "/anthropic", "/anthropic/v1", "/anthropic/v1/messages", "/responses", "/v1/responses", "/chat/completions", "/v1/chat/completions":
		default:
			return false
		}
	}
	return p.BaseURL != ""
}

func migrateOfficialDeepSeekChat(c *Config) {
	for i := range c.Providers {
		p := &c.Providers[i]
		if !isOfficialDeepSeekChatUpgrade(p) {
			continue
		}
		p.Kind, p.BaseURL = "openai", "https://api.deepseek.com"
		// Drop the standard override instead of pinning the canonical URL: the
		// derived endpoint is identical, and a non-empty override hides the
		// account from IsOfficialDeepSeekSearchEndpoint (independent search).
		p.RequestURL, p.ChatURL = "", ""
	}
}

// Only the historical built-in Messages routes are defaults to restore.
// Explicit alternate presets, custom models and transport settings remain owned
// by the user. Model selection, effort, pricing and the search switch survive.
func isLegacyDeepSeekMessagesDefault(p *ProviderEntry) bool {
	if p == nil || p.Kind != "anthropic" || !IsOfficialDeepSeekSearchEndpoint(p) ||
		p.PresetID != "" || len(p.Headers) != 0 || len(p.ExtraBody) != 0 || p.AuthHeader ||
		(p.ReasoningProtocol != "" && p.ReasoningProtocol != ReasoningProtocolDeepSeek) {
		return false
	}
	copy := *p
	copy.Kind = "openai"
	copy.BaseURL = "https://api.deepseek.com"
	if !CanUpgradeDeepSeekProviderProtocol(&copy) {
		return false
	}
	for _, override := range p.ModelOverrides {
		if override.ReasoningProtocol != "" && override.ReasoningProtocol != ReasoningProtocolDeepSeek {
			return false
		}
	}
	return true
}

func restoreDeepSeekChatDefaults(c *Config) {
	for i := range c.Providers {
		p := &c.Providers[i]
		if isLegacyDeepSeekMessagesDefault(p) {
			p.Kind = "openai"
			p.BaseURL = "https://api.deepseek.com"
		}
	}
}

// The caller holds the shared config-edit lock. Version 7 and 8 configurations
// need only this raw edit, avoiding a full render that could erase unknown data.
func upgradeDeepSeekChatDefaultFileLocked(path string) (bool, error) {
	resolved, exists, err := statConfigPath(path)
	if err != nil || !exists {
		return false, err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return false, err
	}
	raw, err := os.ReadFile(resolved)
	if err != nil {
		return false, err
	}
	encoding, detected := fileencoding.Detect(raw)
	body := string(fileencoding.Decode(detected, encoding))
	next, _, err := rewriteDeepSeekProtocol(body, "openai", "https://api.deepseek.com", func(p *ProviderEntry, _ map[string]any) bool { return isOfficialDeepSeekChatUpgrade(p) })
	if err != nil {
		return false, err
	}
	lines := strings.Split(next, "\n")
	state := tomlOutside
	found := false
	for i, line := range lines {
		if state == tomlOutside {
			if tomlSectionHeader(line) != "" {
				break
			}
			if isTOMLKeyAssignment(line, "config_version") {
				lines[i] = replaceTOMLScalarAssignment(line, strconv.Itoa(deepSeekOfficialChatUpgradeConfigVersion))
				found = true
				break
			}
		}
		state = advanceTOMLStringState(state, line)
	}
	next = strings.Join(lines, "\n")
	if !found {
		next = "config_version = 9\n" + next
	}
	if err := fileutil.AtomicWriteFile(resolved, fileencoding.Encode(next, encoding), info.Mode().Perm()); err != nil {
		return false, err
	}
	return true, nil
}
