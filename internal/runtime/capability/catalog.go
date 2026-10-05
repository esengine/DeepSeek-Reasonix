package capability

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"sort"
	"strings"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/tool"
	"reasonix/internal/ext/plugin"
	"reasonix/internal/ext/skill"
)

// Profile labels a runtime for diagnostics. It selects nothing: the
// capability directory is the same under every role setting.
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
	Tools   []tool.ContractEntry
	Skills  []skill.Skill
	Plugins []config.PluginEntry
	// Profile is carried for diagnostics only; BuildCatalog reads nothing off it.
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
		cached[name] = s.EnabledCachedTools(cs.Tools)
		keyOK[name] = match
	}
	return cached, keyOK
}

// BuildCatalog assembles the unified capability directory.
func BuildCatalog(opts CatalogOptions) Catalog {
	var entries []Entry
	toolEntries := ToolEntries(opts.Tools)
	for i := range toolEntries {
		if toolEntries[i].Kind != KindMCPTool {
			continue
		}
		name := toolEntries[i].Source
		switch {
		case opts.Disabled != nil && opts.Disabled[name]:
			toolEntries[i].Status = StatusDisabled
		case opts.Failed != nil && opts.Failed[name] != "":
			toolEntries[i].Status = StatusFailed
			toolEntries[i].FailureReason = opts.Failed[name]
		}
	}
	entries = append(entries, toolEntries...)
	entries = append(entries, skillCatalogEntries(opts.Skills, opts.Tools)...)
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

// skillCatalogEntries is SkillEntries plus the frontmatter a catalog reader
// sees. A skill's declared profiles ride along for diagnostics and never hide
// it: role settings are host settings, not a visibility rule.
func skillCatalogEntries(skills []skill.Skill, tools []tool.ContractEntry) []Entry {
	out := SkillEntries(skills, tools)
	for i := range out {
		if i < len(skills) {
			out[i].Requires = cleanList(skills[i].Requires)
			out[i].Profiles = normalizeProfiles(skills[i].Profiles)
		}
	}
	return out
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
		case status == StatusReady && len(opts.ProxyTools[name]) > 0 && !registryHasTools:
			toolSrc = opts.ProxyTools[name]
			toolStatus = StatusReady
		case status != StatusReady:
			toolSrc = opts.CachedTools[name]
			// Cached tools share the server lifecycle. A failed or disabled
			// server cannot make a stale schema actionable, and a cache-key
			// mismatch keeps the same staleness on every cached tool.
			toolStatus = status
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

// Lookup returns the entry with the given capability ID or declared alias.
// Exact IDs win over aliases so a namespace can never shadow a real entry.
func (c Catalog) Lookup(id string) (Entry, bool) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Entry{}, false
	}
	for _, e := range c.Entries {
		if e.ID == id {
			return e, true
		}
	}
	for _, e := range c.Entries {
		if slices.Contains(e.Aliases, id) {
			return e, true
		}
	}
	// A tool's own name is what the model reads everywhere else: in another
	// tool's description, in a skill's prose, in its own error text. Refusing
	// it here answers "unknown capability_id" for a capability that is present
	// and callable. Two tools answering to one name is refused, not guessed.
	var byToolName Entry
	matches := 0
	for _, e := range c.Entries {
		if e.ToolName != "" && e.ToolName == id {
			byToolName, matches = e, matches+1
		}
	}
	if matches == 1 {
		return byToolName, true
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
