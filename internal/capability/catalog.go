package capability

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"reasonix/internal/config"
	"reasonix/internal/plugin"
	"reasonix/internal/skill"
	"reasonix/internal/tool"
)

// Profile filters which skills are eligible in a given runtime profile.
type Profile string

const (
	ProfileEconomy  Profile = "economy"
	ProfileBalanced Profile = "balanced"
	ProfileDelivery Profile = "delivery"
)

// Catalog is the unified capability inventory for one routing turn.
type Catalog struct {
	Entries     []Entry
	Fingerprint string
}

// CatalogOptions builds a catalog from live tools, skills, configured MCP
// servers (including auto_start=false), schema cache, and host failure state.
type CatalogOptions struct {
	Tools       []tool.ContractEntry
	Skills      []skill.Skill
	Plugins     []config.PluginEntry
	Profile     Profile
	Connected   map[string]bool // server name → connected
	Failed      map[string]string
	Disabled    map[string]bool
	CachedTools map[string][]plugin.CachedTool // server → tools
	CacheKeyOK  map[string]bool                // server → schema-cache key match
	// ProxyTools carries host-observed live tools of servers connected through
	// the Delivery proxy: they are absent from Tools (never registered) yet
	// must stay routable after the server turns ready.
	ProxyTools map[string][]plugin.CachedTool
}

// LoadCachedToolsForSpecs loads the persisted MCP schema caches for the given
// boot-converted specs, keyed by server name, plus the per-server cache-key
// match state. Mismatched caches are still returned (with
// CacheKeyOK=false) so MCPServerEntries can mark them stale instead of
// hiding them; servers without a usable cache are simply absent. Call once at
// session start and reuse — the cache lives on disk.
func LoadCachedToolsForSpecs(specs []plugin.Spec) (map[string][]plugin.CachedTool, map[string]bool) {
	cached := map[string][]plugin.CachedTool{}
	keyOK := map[string]bool{}
	for _, s := range specs {
		name := strings.TrimSpace(s.Name)
		if name == "" {
			continue
		}
		cs, ok, match := plugin.LoadCachedSchemaAny(name, plugin.SchemaCacheKey(s))
		if !ok || len(cs.Tools) == 0 {
			continue
		}
		cached[name] = cs.Tools
		keyOK[name] = match
	}
	return cached, keyOK
}

// BuildCatalog assembles the unified capability directory.
func BuildCatalog(opts CatalogOptions) Catalog {
	profile := opts.Profile
	if profile == "" {
		profile = ProfileBalanced
	}
	var entries []Entry
	entries = append(entries, ToolEntries(opts.Tools)...)
	entries = append(entries, SkillEntriesFiltered(opts.Skills, opts.Tools, profile)...)
	entries = append(entries, MCPServerEntries(opts)...)

	// Deduplicate by ID, preferring ready over configured.
	byID := map[string]Entry{}
	order := make([]string, 0, len(entries))
	for _, e := range entries {
		if prev, ok := byID[e.ID]; ok {
			if rankStatus(e.Status) > rankStatus(prev.Status) {
				byID[e.ID] = e
			}
			continue
		}
		byID[e.ID] = e
		order = append(order, e.ID)
	}
	out := make([]Entry, 0, len(order))
	for _, id := range order {
		out = append(out, byID[id])
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].ID < out[j].ID
	})
	return Catalog{Entries: out, Fingerprint: catalogFingerprint(out)}
}

// SkillEntriesFiltered applies profile eligibility and requires metadata.
func SkillEntriesFiltered(skills []skill.Skill, tools []tool.ContractEntry, profile Profile) []Entry {
	out := SkillEntries(skills, tools)
	filtered := make([]Entry, 0, len(out))
	for i, e := range out {
		sk := skills[i]
		if !skill.AllowedInProfile(sk, string(profile)) {
			continue
		}
		e.Requires = cleanList(sk.Requires)
		e.Profiles = normalizeProfiles(sk.Profiles)
		// Status stays ready when listed; callers re-check requires against the
		// live catalog at invoke time so routing can still recommend the skill.
		filtered = append(filtered, e)
	}
	return filtered
}

// MCPServerEntries includes every configured MCP, even when not auto-started.
func MCPServerEntries(opts CatalogOptions) []Entry {
	var out []Entry
	seen := map[string]bool{}
	for _, p := range opts.Plugins {
		name := strings.TrimSpace(p.Name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		status := StatusConfigured
		if opts.Disabled != nil && opts.Disabled[name] {
			status = StatusDisabled
		} else if opts.Failed != nil && opts.Failed[name] != "" {
			status = StatusFailed
		} else if opts.Connected != nil && opts.Connected[name] {
			status = StatusReady
		} else if opts.CacheKeyOK != nil && !opts.CacheKeyOK[name] && opts.CachedTools != nil && len(opts.CachedTools[name]) > 0 {
			status = StatusStale
		}
		e := Entry{
			ID:            "mcp-server:" + name,
			Kind:          KindMCPServer,
			Name:          name,
			Description:   "MCP server " + name,
			Source:        name,
			Status:        status,
			ConnectSource: "mcp",
			ConnectName:   name,
			AutoStart:     p.ShouldAutoStart(),
		}
		if reason, ok := opts.Failed[name]; ok && reason != "" {
			e.FailureReason = reason
		}
		out = append(out, e)

		// Surface concrete tools that are not on the provider-visible registry:
		// live proxy-observed tools once the server is connected (proxied
		// servers never register), cached schema before any connection exists.
		registryHasTools := false
		prefix := plugin.ToolPrefix(name)
		for _, te := range opts.Tools {
			if strings.HasPrefix(te.Name, prefix) {
				registryHasTools = true
				break
			}
		}
		var toolSrc []plugin.CachedTool
		toolStatus := StatusConfigured
		switch {
		case len(opts.ProxyTools[name]) > 0 && !registryHasTools:
			toolSrc = opts.ProxyTools[name]
			toolStatus = StatusReady
		case status != StatusReady:
			toolSrc = opts.CachedTools[name]
			// A schema-cache-key mismatch marked the server stale; its
			// tools carry the same staleness so routing prompts expose it.
			if status == StatusStale {
				toolStatus = StatusStale
			}
		}
		for _, ct := range toolSrc {
			raw := strings.TrimSpace(ct.Name)
			if raw == "" {
				continue
			}
			out = append(out, Entry{
				ID:            "mcp-tool:" + name + "/" + raw,
				Kind:          KindMCPTool,
				Name:          name + "/" + raw,
				Description:   strings.TrimSpace(ct.Description),
				Source:        name,
				Status:        toolStatus,
				ReadOnly:      ct.ReadOnly,
				Destructive:   ct.Destructive,
				ToolName:      plugin.ModelToolName(name, raw),
				ConnectSource: "mcp",
				ConnectName:   name,
				AutoStart:     p.ShouldAutoStart(),
			})
		}
	}
	return out
}

func normalizeProfiles(in []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, p := range in {
		p = strings.ToLower(strings.TrimSpace(p))
		switch p {
		case string(ProfileEconomy), string(ProfileBalanced), string(ProfileDelivery):
			if !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	return out
}

func rankStatus(s Status) int {
	switch s {
	case StatusReady:
		return 4
	case StatusConfigured:
		return 3
	case StatusStale:
		return 2
	case StatusFailed:
		return 1
	case StatusDisabled:
		return 0
	default:
		return 0
	}
}

func catalogFingerprint(entries []Entry) string {
	h := sha256.New()
	for _, e := range entries {
		fmt.Fprintf(h, "%s|%s|%s|%v\n", e.ID, e.Kind, e.Status, e.AutoUse)
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// Lookup returns the entry with the given capability ID.
func (c Catalog) Lookup(id string) (Entry, bool) {
	id = strings.TrimSpace(id)
	for _, e := range c.Entries {
		if e.ID == id {
			return e, true
		}
	}
	return Entry{}, false
}

// RequiresReady reports whether every required dependency is ready.
func (c Catalog) RequiresReady(requires []string) (ready bool, missing []string) {
	for _, dep := range requires {
		dep = strings.TrimSpace(dep)
		if dep == "" {
			continue
		}
		e, ok := c.Lookup(dep)
		if !ok || e.Status != StatusReady {
			missing = append(missing, dep)
		}
	}
	return len(missing) == 0, missing
}
