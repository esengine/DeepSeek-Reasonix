package config

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"reflect"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
	"reasonix/internal/fileutil"
	fileencoding "reasonix/internal/fileutil/encoding"
	"reasonix/internal/provider"
)

const openCodeGoUpgradeVersion = 10

type openCodeGoAlias struct {
	Target   string `json:"target"`
	Identity string `json:"identity"`
}

// The journal is deliberately separate from TOML: older releases may save the
// configuration without understanding migration metadata. It stores identity
// digests, never a resolved API key.
type openCodeGoJournal struct {
	Version       int                        `json:"version"`
	Committed     bool                       `json:"committed"`
	ConfigHash    string                     `json:"config_hash"`
	CommitID      string                     `json:"commit_id,omitempty"`
	Aliases       map[string]openCodeGoAlias `json:"aliases"`
	SearchAliases map[string]openCodeGoAlias `json:"search_aliases"`
	Connections   []string                   `json:"connections,omitempty"`
	Skipped       []string                   `json:"skipped,omitempty"`
	Previous      *openCodeGoJournal         `json:"previous,omitempty"`
}

func openCodeGoDigest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

// Identity follows the credential reference and user transport settings, never
// a resolved secret or the mutable display name/model list/protocol default.
func openCodeGoIdentity(p ProviderEntry) string {
	b, _ := json.Marshal(struct {
		Key           string
		Headers       map[string]string
		Body          map[string]any
		Auth, NoProxy bool
	}{strings.TrimSpace(p.APIKeyEnv), normalizedProviderHeaders(p.Headers), p.ExtraBody, p.AuthHeader, p.NoProxy})
	return openCodeGoDigest(b)
}

func openCodeGoMigrationRoute(p ProviderEntry) (string, string) {
	route, ok := provider.OpenCodeGoRequestRoute(p.Kind, p.BaseURL, p.RequestURL, p.ChatURL)
	if !ok {
		return "", "effective request URL is not a standard OpenCode Go endpoint"
	}
	// All configured endpoints must be provably equivalent before redirecting.
	if _, ok := provider.OpenCodeGoRequestRoute(p.Kind, p.BaseURL, "", ""); !ok {
		return "", "custom base URL is preserved"
	}
	if p.ChatURL != "" {
		if _, ok := provider.OpenCodeGoRequestRoute("openai", p.BaseURL, p.ChatURL, ""); !ok {
			return "", "custom chat URL is preserved"
		}
	}
	if len(p.ExtraBody) != 0 {
		return "", "custom request body requires manual protocol review"
	}
	return route, ""
}

type openCodeGoGroup struct {
	source int
	entry  ProviderEntry
}

func planOpenCodeGoUpgrade(c *Config) (openCodeGoJournal, []openCodeGoGroup) {
	return planOpenCodeGoUpgradeFiltered(c, nil)
}

func planOpenCodeGoUpgradeFiltered(c *Config, eligible func(ProviderEntry) bool) (openCodeGoJournal, []openCodeGoGroup) {
	j := openCodeGoJournal{Version: openCodeGoUpgradeVersion, Aliases: map[string]openCodeGoAlias{}, SearchAliases: map[string]openCodeGoAlias{}}
	var additions []openCodeGoGroup
	count := len(c.Providers)
	bareOwners := map[string]string{}
	searchConnections := map[string]bool{}
	if c.openCodeGoJournal != nil {
		for _, a := range c.openCodeGoJournal.SearchAliases {
			name, _, _ := strings.Cut(a.Target, "/")
			searchConnections[name] = true
		}
	}
	for _, p := range c.Providers {
		for _, m := range p.ModelList() {
			if _, exists := bareOwners[m]; !exists {
				bareOwners[m] = p.Name
			}
		}
	}
	for i := range count {
		original := cloneProviderEntry(c.Providers[i])
		if eligible != nil && !eligible(original) {
			continue
		}
		if searchConnections[original.Name] {
			continue
		}
		current, reason := openCodeGoMigrationRoute(original)
		if current == "" {
			if strings.Contains(original.BaseURL, "opencode.ai/zen/go") || strings.Contains(original.RequestURL, "opencode.ai/zen/go") {
				j.Skipped = append(j.Skipped, original.Name+": "+reason)
			}
			continue
		}
		groups := map[string][]string{}
		for _, model := range original.ModelList() {
			route, known := provider.OpenCodeGoRecommendedRoute(model)
			if !known {
				route = current
			}
			groups[route] = append(groups[route], model)
		}
		if len(groups) == 0 {
			continue
		}
		keep := current
		if len(groups[keep]) == 0 {
			keep, _ = provider.OpenCodeGoRecommendedRoute(original.DefaultModel())
			if len(groups[keep]) == 0 {
				keep = firstOpenCodeGoGroup(groups)
			}
		}

		c.Providers[i] = makeOpenCodeGoGroup(original, keep, groups[keep])

		recordOpenCodeGoAliases(&j, original, c.Providers[i], groups[keep], bareOwners)
		changed := keep != current || len(groups) > 1
		for _, route := range []string{provider.OpenCodeGoRouteChat, provider.OpenCodeGoRouteAnthropic, provider.OpenCodeGoRouteResponses} {
			models := groups[route]
			if route == keep || len(models) == 0 {
				continue
			}
			p := makeOpenCodeGoGroup(original, route, models)
			p.Name = uniqueOpenCodeGoSiblingName(c, original.Name, route)
			p = reuseOpenCodeGoGroup(c, original, p)
			if _, exists := c.Provider(p.Name); !exists {
				c.Providers = append(c.Providers, p)
				additions = append(additions, openCodeGoGroup{i, p})
				if c.Desktop.ProviderAccess != nil && slices.Contains(c.Desktop.ProviderAccess, original.Name) {
					addOpenCodeGoAccess(c, p.Name)
				}
			}
			recordOpenCodeGoAliases(&j, original, p, models, bareOwners)
		}
		if preserveOpenCodeGoSearch(c, &j, &additions, original, current, groups, i) {
			changed = true
		}
		if changed {
			j.Connections = append(j.Connections, original.Name)
		}
	}
	placeOpenCodeGoSiblings(c, count, additions)
	rewrite := func(ref string) string {
		if a, ok := j.Aliases[ref]; ok {
			return a.Target
		}
		return ref
	}
	c.DefaultModel = rewrite(c.DefaultModel)
	c.Agent.PlannerModel = rewrite(c.Agent.PlannerModel)
	c.Agent.VisionModel = rewrite(c.Agent.VisionModel)
	c.Agent.GuardianModel = rewrite(c.Agent.GuardianModel)
	c.Agent.RecoveryModel = rewrite(c.Agent.RecoveryModel)
	c.Agent.SubagentModel = rewrite(c.Agent.SubagentModel)
	for name, ref := range c.Agent.SubagentModels {
		c.Agent.SubagentModels[name] = rewrite(ref)
	}
	c.Bot.Model = rewrite(c.Bot.Model)
	for i := range c.Bot.Connections {
		c.Bot.Connections[i].Model = rewrite(c.Bot.Connections[i].Model)
	}
	return j, additions
}

// placeOpenCodeGoSiblings moves each split group directly after the connection
// it came from. Bare model names resolve to their first owner, so a later
// account must not overtake a model the earlier account only moved routes for.
func placeOpenCodeGoSiblings(c *Config, count int, additions []openCodeGoGroup) {
	if len(additions) == 0 {
		return
	}
	ordered := make([]ProviderEntry, 0, len(c.Providers))
	for i := range count {
		ordered = append(ordered, c.Providers[i])
		for k, add := range additions {
			if add.source == i {
				ordered = append(ordered, c.Providers[count+k])
			}
		}
	}
	c.Providers = ordered
}

func allOpenCodeGoDeepSeek(models []string) bool {
	for _, m := range models {
		if !provider.OpenCodeGoDeepSeekModel(m) {
			return false
		}
	}
	return len(models) > 0
}

func legacyOpenCodeGoBinary(ids []string) bool {
	return len(ids) == 2 && slices.Contains(ids, "enabled") && slices.Contains(ids, "disabled")
}

func upgradeOpenCodeGoFileLocked(path string) (bool, error) {
	return upgradeOpenCodeGoFileWithWriterLocked(path, fileutil.AtomicWriteFile)
}

func upgradeOpenCodeGoFileWithWriterLocked(path string, write func(string, []byte, os.FileMode) error) (bool, error) {
	resolved, exists, err := statConfigPath(path)
	if err != nil || !exists {
		return false, err
	}
	raw, err := os.ReadFile(resolved)
	if err != nil {
		return false, err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return false, err
	}
	encoding, detected := fileencoding.Detect(raw)
	body := string(fileencoding.Decode(detected, encoding))
	// The lexical rewriters split on "\n" and re-parse value extents; a
	// trailing "\r" never parses alone, so edit LF text and restore CRLF after.
	crlf := strings.Contains(body, "\r\n")
	if crlf {
		body = strings.ReplaceAll(body, "\r\n", "\n")
	}
	var before Config
	if _, err := toml.Decode(body, &before); err != nil {
		return false, err
	}
	if before.ConfigVersion >= openCodeGoUpgradeVersion {
		return false, nil
	}
	if before.ConfigVersion < deepSeekOfficialChatUpgradeConfigVersion {
		body, _, err = rewriteDeepSeekProtocol(body, "openai", "https://api.deepseek.com", func(p *ProviderEntry, _ map[string]any) bool { return isOfficialDeepSeekChatUpgrade(p) })
		if err != nil {
			return false, err
		}
		if _, err := toml.Decode(body, &before); err != nil {
			return false, err
		}
	}
	var after Config
	_, _ = toml.Decode(body, &after)
	after.openCodeGoJournal = readOpenCodeGoJournal(resolved, raw)
	j, additions := planOpenCodeGoUpgrade(&after)
	next, err := rewriteOpenCodeGoConfig(body, &before, &after, additions)
	if err != nil {
		return false, fmt.Errorf("OpenCode Go upgrade: %w; original configuration retained", err)
	}
	if strings.EqualFold(strings.TrimSpace(before.Desktop.LayoutStyle), "classic") {
		next, err = rawTOMLSet(next, []string{"desktop", "layout_style"}, "workbench")
		if err != nil {
			return false, err
		}
	}
	// A committed older journal survives a downgrade/save/upgrade round trip.
	if previous := readOpenCodeGoJournal(resolved, raw); previous != nil {
		maps.Copy(j.Aliases, previous.Aliases)
		maps.Copy(j.SearchAliases, previous.SearchAliases)
		previous.Previous = nil
		previous.Committed = true
		j.Previous = previous
	}
	if len(j.Aliases) > 0 || len(j.SearchAliases) > 0 {
		j.CommitID = rand.Text()
		next, err = rawTOMLSet(next, []string{"opencode_go_migration_commit"}, j.CommitID)
		if err != nil {
			return false, err
		}
	}
	if err := verifyOpenCodeGoRewrite(next, &after); err != nil {
		return false, fmt.Errorf("OpenCode Go upgrade: %w; original configuration retained", err)
	}
	if crlf {
		next = strings.ReplaceAll(next, "\n", "\r\n")
	}
	encoded := fileencoding.Encode(next, encoding)
	j.ConfigHash = openCodeGoDigest(encoded)
	backup := resolved + ".opencode-go-v10.backup"
	if _, err := os.Stat(backup); os.IsNotExist(err) {
		if err := write(backup, raw, 0600); err != nil {
			return false, fmt.Errorf("save migration backup: %w", err)
		}
	} else if err != nil {
		return false, err
	}
	journalPath := resolved + ".opencode-go-v10.json"
	data, _ := json.MarshalIndent(j, "", "  ")
	if err := write(journalPath, data, 0600); err != nil {
		return false, fmt.Errorf("prepare migration journal: %w", err)
	}
	if err := write(resolved, encoded, info.Mode().Perm()); err != nil {
		return false, fmt.Errorf("commit migration: %w", err)
	}
	j.Committed = true
	j.Previous = nil
	data, _ = json.MarshalIndent(j, "", "  ")
	// If this last write fails, the exact committed config hash still activates
	// the prepared journal. No rollback can lose an already committed config.
	if err := write(journalPath, data, 0600); err != nil {
		slog.Warn("config: OpenCode Go migration journal acknowledgement deferred to next startup", "path", journalPath, "err", err)
	}
	return true, nil
}

func readOpenCodeGoJournal(path string, raw []byte) *openCodeGoJournal {
	data, err := os.ReadFile(path + ".opencode-go-v10.json")
	if err != nil {
		return nil
	}
	var j openCodeGoJournal
	if json.Unmarshal(data, &j) != nil || j.Version != openCodeGoUpgradeVersion {
		return nil
	}
	if !j.Committed && (len(raw) == 0 || j.ConfigHash != openCodeGoDigest(raw)) {
		var marker struct {
			CommitID string `toml:"opencode_go_migration_commit"`
		}
		if _, err := decodeTOMLBytes(raw, &marker); err != nil || j.CommitID == "" || marker.CommitID != j.CommitID {
			if j.Previous != nil && j.Previous.Committed && j.Previous.Version == openCodeGoUpgradeVersion {
				return j.Previous
			}
			return nil
		}
	}
	return &j
}

// Before a renderer can drop the unknown TOML commit marker, acknowledge any
// configuration commit whose final journal write was interrupted.
func finalizeOpenCodeGoJournal(path string) error {
	if _, err := os.Stat(path + ".opencode-go-v10.json"); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	j := readOpenCodeGoJournal(path, raw)
	if j == nil || j.Committed {
		return nil
	}
	j.Committed = true
	j.Previous = nil
	data, err := json.MarshalIndent(j, "", "  ")
	if err != nil {
		return err
	}
	if err := fileutil.AtomicWriteFile(path+".opencode-go-v10.json", data, 0600); err != nil {
		return fmt.Errorf("complete OpenCode Go migration journal before saving config: %w", err)
	}
	return nil
}

func makeOpenCodeGoGroup(original ProviderEntry, route string, models []string) ProviderEntry {
	p := cloneProviderEntry(original)
	setOpenCodeGoRoute(&p, route)
	p.RequestURL, p.ChatURL = "", ""
	applyOpenCodeGoModelGroup(&p, models)
	p.PresetID = openCodeGoPresetIDForRoute(route)
	p.PresetVersion = ProviderPresetVersion
	for _, model := range models {
		contract, ok := provider.OpenCodeGoContractForRoute(route, model)
		if !ok {
			continue
		}
		if p.ModelOverrides == nil {
			p.ModelOverrides = map[string]ProviderModelOverride{}
		}
		o := p.ModelOverrides[model]
		if o.ReasoningProtocol == "" || o.ReasoningProtocol == "auto" {
			o.ReasoningProtocol = contract.ReasoningProtocol
		}
		if len(o.SupportedEfforts) == 0 && len(p.SupportedEfforts) == 0 {
			o.SupportedEfforts = contract.Reasoning.IDs()
			o.DefaultEffort = contract.Reasoning.Default
		}
		// Only the old binary switch has a proven equivalent. Depths and
		// explicit custom vocabularies remain user-owned and get validated.
		if provider.OpenCodeGoDeepSeekModel(model) && ((len(o.SupportedEfforts) == 0 && (len(p.SupportedEfforts) == 0 || legacyOpenCodeGoBinary(p.SupportedEfforts))) || legacyOpenCodeGoBinary(o.SupportedEfforts)) {
			o.SupportedEfforts = contract.Reasoning.IDs()
			o.DefaultEffort = contract.Reasoning.Default
		}
		p.ModelOverrides[model] = o
	}
	if p.Effort == "enabled" && allOpenCodeGoDeepSeek(models) {
		p.Effort = "high"
	}
	if route != provider.OpenCodeGoRouteResponses && p.Effort == "none" && allOpenCodeGoDeepSeek(models) {
		p.Effort = "disabled"
	}
	return p
}

func recordOpenCodeGoAliases(j *openCodeGoJournal, original, p ProviderEntry, models []string, bareOwners map[string]string) {
	for _, model := range models {
		if _, known := provider.OpenCodeGoRecommendedRoute(model); !known {
			continue
		}
		alias := openCodeGoAlias{p.Name + "/" + model, openCodeGoIdentity(p)}
		j.Aliases[original.Name+"/"+model] = alias
		if model == original.DefaultModel() {
			j.Aliases[original.Name] = alias
		}
		if bareOwners[model] == original.Name {
			j.Aliases[model] = alias
		}
	}
}

func reuseOpenCodeGoGroup(c *Config, original, p ProviderEntry) ProviderEntry {
	// Reuse requires complete user settings to agree, including per-model
	// prices and overrides. A same-name different account never matches.
	for _, candidate := range c.Providers {
		if candidate.Name == original.Name || !reflect.DeepEqual(candidate.ModelList(), p.ModelList()) {
			continue
		}
		a, b := cloneProviderEntry(candidate), cloneProviderEntry(p)
		a.Name, b.Name, a.DisplayName, b.DisplayName = "", "", "", ""
		if reflect.DeepEqual(a, b) {
			p.Name = candidate.Name
			break
		}
	}
	return p
}

func preserveOpenCodeGoSearch(c *Config, j *openCodeGoJournal, additions *[]openCodeGoGroup, original ProviderEntry, current string, groups map[string][]string, i int) bool {
	// Preserve an enabled search on the original wire route. Search aliases
	// are purpose-specific: history continues through the recommended route.
	if original.WebSearch != nil && *original.WebSearch && current != provider.OpenCodeGoRouteChat && len(groups[provider.OpenCodeGoRouteChat]) > 0 {
		var models []string
		for _, m := range groups[provider.OpenCodeGoRouteChat] {
			if provider.OpenCodeGoDeepSeekModel(m) {
				models = append(models, m)
			}
		}
		if len(models) > 0 {
			p := makeOpenCodeGoGroup(original, current, models)
			p.Name = uniqueOpenCodeGoSiblingName(c, original.Name, "search")
			c.Providers = append(c.Providers, p)
			*additions = append(*additions, openCodeGoGroup{i, p})
			if c.Desktop.ProviderAccess != nil && slices.Contains(c.Desktop.ProviderAccess, original.Name) {
				addOpenCodeGoAccess(c, p.Name)
			}
			for _, model := range models {
				old := original.Name + "/" + model
				j.SearchAliases[old] = openCodeGoAlias{p.Name + "/" + model, openCodeGoIdentity(p)}
				if c.Agent.WebSearchModel == old {
					c.Agent.WebSearchModel = p.Name + "/" + model
				}
			}
			return true
		}
	}
	return false
}
