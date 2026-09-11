package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/BurntSushi/toml"
)

func NewModelSettingsOfferID() (string, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(id[:]), nil
}

// ModelRuntimeSettings is an in-memory model resolver bundle. Desktop sends
// only tunnel tokens in Credentials; actual provider credentials remain local.
// It is never serialized into session history or a system prompt.
type ModelRuntimeSettings struct {
	SourceToken string                  `json:"sourceToken,omitempty"`
	OfferID     string                  `json:"offerID,omitempty"`
	ProxyURL    string                  `json:"proxyURL"`
	Revision    string                  `json:"revision"`
	Providers   []ProviderEntry         `json:"providers"`
	Credentials map[string]string       `json:"credentials"`
	References  map[string]string       `json:"references"`
	Preferences ModelRuntimePreferences `json:"preferences"`
}

// ModelSettingsSourceRequest is an authenticated, transient tunnel exchange.
// Offer ownership protects candidate routes until Serve publishes or rejects
// them. IDs are random correlation values and never enter user configuration.
type ModelSettingsSourceRequest struct {
	ModelSettingsOwnership
	Mode              string   `json:"mode"`
	OfferID           string   `json:"offerID"`
	PreviousOfferID   string   `json:"previousOfferID,omitempty"`
	Model             string   `json:"model,omitempty"`
	AppliedRevision   string   `json:"appliedRevision,omitempty"`
	RemotePort        int      `json:"remotePort,omitempty"`
	OwnedRevisions    []string `json:"ownedRevisions"`
	UnversionedOwners bool     `json:"unversionedOwners"`
}

// ModelSettingsOwnership orders complete owner snapshots within one Serve.
type ModelSettingsOwnership struct {
	OwnershipIncarnation string `json:"ownershipIncarnation"`
	OwnershipSeq         uint64 `json:"ownershipSeq"`
}

type ModelSettingsSourceResponse struct {
	Version  int                   `json:"version"`
	Revision string                `json:"revision"`
	Ref      string                `json:"ref,omitempty"`
	Settings *ModelRuntimeSettings `json:"settings,omitempty"`
}

type ModelRuntimePreferences struct {
	PlannerModel           string            `json:"plannerModel" toml:"planner_model"`
	VisionModel            string            `json:"visionModel" toml:"vision_model"`
	WebSearchModel         string            `json:"webSearchModel" toml:"web_search_model"`
	GuardianModel          string            `json:"guardianModel" toml:"guardian_model"`
	RecoveryModel          string            `json:"recoveryModel" toml:"recovery_model"`
	SubagentModel          string            `json:"subagentModel" toml:"subagent_model"`
	SubagentModels         map[string]string `json:"subagentModels" toml:"subagent_models"`
	SubagentEffort         string            `json:"subagentEffort" toml:"subagent_effort"`
	SubagentEfforts        map[string]string `json:"subagentEfforts" toml:"subagent_efforts"`
	MaxSubagentDepth       int               `json:"maxSubagentDepth" toml:"max_subagent_depth"`
	MaxSubagentConcurrency int               `json:"maxSubagentConcurrency" toml:"max_subagent_concurrency"`
	MaxParallelWriters     int               `json:"maxParallelWriters" toml:"max_parallel_writers"`
}

func (c *Config) RuntimeModelPreferences() ModelRuntimePreferences {
	var out ModelRuntimePreferences
	src, dst := reflect.ValueOf(c.Agent), reflect.ValueOf(&out).Elem()
	for i := range dst.NumField() {
		dst.Field(i).Set(src.FieldByName(dst.Type().Field(i).Name))
	}
	return out
}

// Apply overlays the desktop-managed resolver, retaining explicitly configured
// project models/preferences. The loaded Config is private to this boot; clone
// the input first so concurrent boot builders never share mutable maps.
func (settings *ModelRuntimeSettings) Apply(c *Config, root string) error {
	if settings == nil {
		return nil
	}
	raw, err := json.Marshal(settings)
	if err != nil {
		return fmt.Errorf("encode model snapshot: %w", err)
	}
	var frozen ModelRuntimeSettings
	if err := json.Unmarshal(raw, &frozen); err != nil {
		return err
	}
	var project map[string]any
	projectRaw, err := os.ReadFile(filepath.Join(root, "reasonix.toml"))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if len(projectRaw) > 0 {
		if _, err := toml.Decode(string(projectRaw), &project); err != nil {
			return err
		}
	}
	projectProviders := map[string]bool{}
	if entries, ok := project["providers"].([]map[string]any); ok {
		for _, entry := range entries {
			if name, ok := entry["name"].(string); ok {
				projectProviders[name] = true
			}
		}
	}
	retained := make([]ProviderEntry, 0, len(projectProviders))
	projectEntries := map[string]ProviderEntry{}
	for _, p := range c.Providers {
		if projectProviders[p.Name] {
			retained = append(retained, p)
			projectEntries[p.Name] = p
		}
	}
	// The Desktop expands multi-model providers into separately credentialed
	// routes. Preserve project overrides under those transport aliases too.
	sourceNames := map[string]string{}
	for source, target := range frozen.References {
		sourceName, _, _ := strings.Cut(source, "/")
		targetName, _, _ := strings.Cut(target, "/")
		sourceNames[targetName] = sourceName
	}
	c.Providers = nil
	seen := map[string]bool{}
	for _, p := range frozen.Providers {
		if strings.TrimSpace(p.Name) == "" || seen[p.Name] {
			return fmt.Errorf("model snapshot has an empty or duplicate provider")
		}
		seen[p.Name] = true
		if projectEntry, ok := projectEntries[sourceNames[p.Name]]; ok {
			alias := projectEntry
			alias.Name = p.Name
			c.Providers = append(c.Providers, alias)
			continue
		}
		if projectProviders[p.Name] {
			continue
		}
		p.resolvedAPIKey, p.credentialsFrozen = frozen.Credentials[p.Name], true
		p.credentialProxyURL = frozen.ProxyURL
		c.Providers = append(c.Providers, p)
	}
	c.Providers = append(c.Providers, retained...)
	c.Desktop.ProviderAccess = nil
	for _, p := range c.Providers {
		c.Desktop.ProviderAccess = append(c.Desktop.ProviderAccess, p.Name)
	}
	declared, _ := project["agent"].(map[string]any)
	src, dst := reflect.ValueOf(frozen.Preferences), reflect.ValueOf(&c.Agent).Elem()
	for i := range src.NumField() {
		field := src.Type().Field(i)
		if _, explicit := declared[field.Tag.Get("toml")]; !explicit {
			dst.FieldByName(field.Name).Set(src.Field(i))
		}
	}
	// Explicit project assignments still use the user's provider names. Route
	// them to the managed alias only when the project has no provider override.
	mapRef := func(ref string) string {
		name, _, _ := strings.Cut(ref, "/")
		if mapped := frozen.References[ref]; mapped != "" && !projectProviders[name] {
			return mapped
		}
		return ref
	}
	for _, ref := range []*string{&c.Agent.PlannerModel, &c.Agent.VisionModel, &c.Agent.WebSearchModel, &c.Agent.GuardianModel, &c.Agent.RecoveryModel, &c.Agent.SubagentModel} {
		*ref = mapRef(*ref)
	}
	for name, ref := range c.Agent.SubagentModels {
		c.Agent.SubagentModels[name] = mapRef(ref)
	}
	return nil
}

func (e *ProviderEntry) CredentialProxyURL() string { return e.credentialProxyURL }
