package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"

	"reasonix/internal/provider"
)

// ErrMigratedModelUnavailable marks a saved selection whose migrated OpenCode
// Go connection no longer matches; a new explicit selection resolves it.
var ErrMigratedModelUnavailable = errors.New("MIGRATED_MODEL_UNAVAILABLE")

func normalizeRuntimeConfigWithMigrationJournal(cfg *Config) error {
	cfg.loadOpenCodeGoJournal(userConfigLoadPath())
	return normalizeLoadedConfig(cfg)
}

func (c *Config) loadOpenCodeGoJournal(path string) {
	if c == nil || path == "" {
		return
	}
	resolved, exists, err := statConfigPath(path)
	if err != nil || !exists {
		return
	}
	raw, err := os.ReadFile(resolved)
	if err != nil {
		return
	}
	c.openCodeGoJournal = readOpenCodeGoJournal(resolved, raw)
}

// Project files are never rewritten by startup. Resolve their legacy routes in
// memory, while version-10 global connections retain later manual API choices.
func normalizeOpenCodeGoRuntimeCompatibility(c *Config) {
	if c == nil {
		return
	}
	previous := c.openCodeGoJournal
	j, _ := planOpenCodeGoUpgradeFiltered(c, func(p ProviderEntry) bool {
		return c.ConfigVersion < openCodeGoUpgradeVersion || c.providerSources[providerMergeKey(p)] == providerSourceProject
	})
	if previous != nil {
		maps.Copy(j.Aliases, previous.Aliases)
		maps.Copy(j.SearchAliases, previous.SearchAliases)
		j.Connections = append(j.Connections, previous.Connections...)
		j.Skipped = append(j.Skipped, previous.Skipped...)
	}
	if len(j.Aliases) > 0 || len(j.SearchAliases) > 0 || previous != nil {
		c.openCodeGoJournal = &j
	}
}

func (c *Config) resolveOpenCodeGoAlias(ref string, search bool) (string, error) {
	if c == nil || c.openCodeGoJournal == nil {
		return ref, nil
	}
	aliases := c.openCodeGoJournal.Aliases
	if search {
		aliases = c.openCodeGoJournal.SearchAliases
	}
	alias, ok := aliases[strings.TrimSpace(ref)]
	if !ok {
		return ref, nil
	}
	name, model, ok := strings.Cut(alias.Target, "/")
	p, found := c.Provider(name)
	if !ok || !found || !p.HasModel(model) {
		return "", fmt.Errorf("%w: %q moved to %q; restore that OpenCode Go connection in model settings", ErrMigratedModelUnavailable, ref, alias.Target)
	}
	if _, official := provider.OpenCodeGoRequestRoute(p.Kind, p.BaseURL, p.RequestURL, p.ChatURL); !official || openCodeGoIdentity(*p) != alias.Identity {
		return "", fmt.Errorf("%w: account or endpoint for %q has changed; restore the original connection for %q", ErrMigratedModelUnavailable, alias.Target, ref)
	}
	return alias.Target, nil
}

// ModelReferenceError checks alias fallback only when the reference is absent
// from the current catalog. Historical callers must use ResolveHistoricalModel.
func (c *Config) ModelReferenceError(ref string) error {
	if _, ok := c.resolveCurrentModel(ref); ok {
		return nil
	}
	_, err := c.resolveOpenCodeGoAlias(ref, false)
	return err
}

// ResolveHistoricalModel validates the original migration identity before
// resolving a saved selection. Unknown non-migrated refs are retained so the
// owning runtime can apply its existing plugin and stale-selection policy.
func (c *Config) ResolveHistoricalModel(ref string) (string, error) {
	target, err := c.resolveOpenCodeGoAlias(ref, false)
	if err != nil {
		return "", err
	}
	if entry, ok := c.resolveCurrentModel(target); ok {
		return entry.Name + "/" + entry.Model, nil
	}
	return target, nil
}

// ModelSelectionIdentity records only a digest, never resolved credentials.
// Current selections capture endpoint and model as well as the transport
// identity, so a later resume cannot silently adopt a different connection.
func (c *Config) ModelSelectionIdentity(ref string) string {
	entry, ok := c.resolveCurrentModel(ref)
	if !ok {
		return ""
	}
	tracked := isOpenCodeGoEntry(entry)
	if c.openCodeGoJournal != nil {
		for _, alias := range c.openCodeGoJournal.Aliases {
			if alias.Target == entry.Name+"/"+entry.Model {
				tracked = true
				break
			}
		}
	}
	if !tracked {
		return ""
	}
	b, _ := json.Marshal([]string{entry.Name, entry.Model, entry.Kind, entry.BaseURL, entry.RequestURL, entry.ChatURL, openCodeGoIdentity(*entry)})
	return openCodeGoDigest(b)
}

// ResolveSavedModel uses an explicitly persisted choice when present; legacy
// sidecars without that choice retain the original migration protection.
func (c *Config) ResolveSavedModel(ref, identity string) (string, error) {
	if identity == "" {
		return c.ResolveHistoricalModel(ref)
	}
	if current := c.ModelSelectionIdentity(ref); current == "" || current != identity {
		return "", fmt.Errorf("%w: saved connection for %q has changed; explicitly select a model to use the current connection", ErrMigratedModelUnavailable, ref)
	}
	entry, _ := c.resolveCurrentModel(ref)
	return entry.Name + "/" + entry.Model, nil
}

func (c *Config) resolveHistoricalWebSearchModel(ref string) (*ProviderEntry, error) {
	target, err := c.resolveOpenCodeGoAlias(ref, true)
	if err != nil {
		return nil, err
	}
	return c.ResolveWebSearchModel(target)
}

// OpenCodeGoUpgradeSummary is consumed by the existing startup notice channel.
func (c *Config) OpenCodeGoUpgradeSummary() string {
	if c == nil || c.openCodeGoJournal == nil {
		return ""
	}
	j := c.openCodeGoJournal
	var parts []string
	if len(j.Connections) > 0 {
		parts = append(parts, "OpenCode Go connections organized by model API: "+strings.Join(j.Connections, ", "))
	}
	if len(j.Skipped) > 0 {
		parts = append(parts, "Preserved custom connections: "+strings.Join(j.Skipped, "; "))
	}
	return strings.Join(parts, ". ")
}

func (c *Config) resolveOpenCodeGoAutomaticSearch(current *ProviderEntry, resolve func(*ProviderEntry) *ProviderEntry) *ProviderEntry {
	if isOpenCodeGoEntry(current) {
		// A migrated chat connection keeps its auxiliary search on the same
		// credential reference, even when another account appears earlier.
		if c.openCodeGoJournal != nil {
			for _, modelSpecific := range []bool{true, false} {
				refs := make([]string, 0, len(c.openCodeGoJournal.SearchAliases))
				for ref := range c.openCodeGoJournal.SearchAliases {
					refs = append(refs, ref)
				}
				slices.Sort(refs)
				for _, ref := range refs {
					alias := c.openCodeGoJournal.SearchAliases[ref]
					_, model, _ := strings.Cut(alias.Target, "/")
					if alias.Identity != openCodeGoIdentity(*current) || (modelSpecific && model != current.Model) {
						continue
					}
					if entry, err := c.resolveHistoricalWebSearchModel(ref); err == nil {
						if selected := resolve(entry); selected != nil {
							return selected
						}
					}
				}
			}
		}
		for i := range c.Providers {
			if openCodeGoIdentity(c.Providers[i]) != openCodeGoIdentity(*current) || !isOpenCodeGoEntry(&c.Providers[i]) {
				continue
			}
			entry, ok := c.ResolveModel(c.Providers[i].Name)
			if ok {
				if selected := resolve(entry); selected != nil {
					return selected
				}
			}
		}
	}
	return nil
}
