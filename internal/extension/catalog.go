package extension

import (
	"fmt"
	"sort"
)

// ContributionKind names one category of runtime capability. The wire names
// are stable for the same reason as Scope's.
type ContributionKind string

const (
	KindTool        ContributionKind = "tool"
	KindSkill       ContributionKind = "skill"
	KindCommand     ContributionKind = "command"
	KindPrompt      ContributionKind = "prompt"
	KindHook        ContributionKind = "hook"
	KindMCPServer   ContributionKind = "mcp_server"
	KindProvider    ContributionKind = "provider"
	KindTheme       ContributionKind = "theme"
	KindUIAction    ContributionKind = "ui_action"
	KindInterceptor ContributionKind = "interceptor"
	KindStrategy    ContributionKind = "strategy"
)

// knownKind reports whether k is a declared kind; the builder rejects unknown
// kinds so a misspelled manifest cannot silently drop a capability.
func knownKind(k ContributionKind) bool {
	switch k {
	case KindTool, KindSkill, KindCommand, KindPrompt, KindHook, KindMCPServer,
		KindProvider, KindTheme, KindUIAction, KindInterceptor, KindStrategy:
		return true
	default:
		return false
	}
}

// additiveKind reports whether every contribution of this kind survives
// resolution. Hooks and interceptors accumulate across sources by design —
// shadowing them would let one package silently disable another package's
// safety hook, which is the opposite of what hooks are for.
func additiveKind(k ContributionKind) bool {
	return k == KindHook || k == KindInterceptor
}

// Contribution is one capability offered to the kernel.
type Contribution struct {
	// Kind is the capability category.
	Kind ContributionKind
	// ID is the canonical identifier within the kind: the tool name, the
	// command's full (package-qualified) name, the skill's slash-or-bare name,
	// a hook's "event#n" sequence, the MCP server name, the provider ref
	// "provider/model", "plugin:<plugin>:<theme>" for themes, and so on.
	// Winner rules key on (Kind, ID).
	ID string
	// Source is the provenance; its Scope drives shadowing.
	Source ContributionSource
	// Priority orders interceptors (see SortInterceptors) and breaks catalog
	// ties within one tier for other kinds. Interceptor priorities must pass
	// ValidatePriority.
	Priority int
	// Payload is the existing concrete value — skill.Skill, command.Command,
	// plugin.Spec, provider.Descriptor, tool.Tool, ... — deliberately not
	// re-wrapped so downstream consumers keep working with the types they
	// already know.
	Payload any
	// Order is the per-contributor registration sequence: the index of this
	// contribution inside its contributor's returned slice, stamped by the
	// Builder. It makes otherwise-identical contributions from one source
	// deterministic (first registration wins) without depending on map
	// iteration or discovery order.
	Order int
}

// Catalog is the collected set of contributions. It is appended to during
// discovery and frozen into the snapshot at Freeze; adding to a frozen
// catalog panics, because mutating a published snapshot's contents would
// invalidate its CacheHash and every consumer's assumption of stability.
type Catalog struct {
	contribs []Contribution
	frozen   bool
}

// NewCatalog returns an empty, mutable catalog.
func NewCatalog() *Catalog { return &Catalog{} }

// Add appends contributions. It panics on a frozen catalog: freeze marks the
// point where the effective set became observable, and growth past that point
// is a wiring bug, not data.
func (c *Catalog) Add(contribs ...Contribution) {
	if c.frozen {
		panic("extension: Add on frozen catalog")
	}
	c.contribs = append(c.contribs, contribs...)
}

// Frozen reports whether the catalog has been sealed into a snapshot.
func (c *Catalog) Frozen() bool { return c.frozen }

// freeze seals the catalog. Only the Builder calls it.
func (c *Catalog) freeze() { c.frozen = true }

// Len returns the number of contributions.
func (c *Catalog) Len() int { return len(c.contribs) }

// All returns every contribution in deterministic order. The returned slice
// is a copy; mutating it cannot affect the catalog.
func (c *Catalog) All() []Contribution {
	out := make([]Contribution, len(c.contribs))
	copy(out, c.contribs)
	SortContributions(out)
	return out
}

// ByKind returns the contributions of one kind in deterministic order (a
// copy, like All).
func (c *Catalog) ByKind(kind ContributionKind) []Contribution {
	out := make([]Contribution, 0, len(c.contribs))
	for _, ct := range c.contribs {
		if ct.Kind == kind {
			out = append(out, ct)
		}
	}
	SortContributions(out)
	return out
}

// Get returns the contributions matching (kind, id) in deterministic order.
// Additive kinds can legitimately return several entries.
func (c *Catalog) Get(kind ContributionKind, id string) []Contribution {
	out := make([]Contribution, 0, 1)
	for _, ct := range c.contribs {
		if ct.Kind == kind && ct.ID == id {
			out = append(out, ct)
		}
	}
	SortContributions(out)
	return out
}

// SortContributions orders contributions deterministically for display and
// assembly: kind first so categories stay together, then shadowing tier
// (highest first, so the effective winner reads first), priority (higher
// first), then the identity fields. The final Source tiebreaks make the order
// total — without them, identical (Kind, ID) pairs from one contributor would
// inherit input order, which depends on contributor registration order.
func SortContributions(cs []Contribution) {
	sort.SliceStable(cs, func(i, j int) bool {
		a, b := cs[i], cs[j]
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if ra, rb := tierRank(a.Source.Scope), tierRank(b.Source.Scope); ra != rb {
			return ra > rb
		}
		if a.Priority != b.Priority {
			return a.Priority > b.Priority
		}
		if a.Source.PluginID != b.Source.PluginID {
			return a.Source.PluginID < b.Source.PluginID
		}
		if a.ID != b.ID {
			return a.ID < b.ID
		}
		if a.Order != b.Order {
			return a.Order < b.Order
		}
		if a.Source.Origin != b.Source.Origin {
			return a.Source.Origin < b.Source.Origin
		}
		return a.Source.Path < b.Source.Path
	})
}

// ConflictError reports same-tier duplicates of one canonical ID from
// different sources for a shadowed kind. The kernel makes these hard failures
// because today's alternative — a silent last-writer-wins — lets an installed
// package override another package's capability with no trace.
type ConflictError struct {
	Kind    ContributionKind
	ID      string
	Sources []ContributionSource
}

func (e *ConflictError) Error() string {
	labels := make([]string, 0, len(e.Sources))
	for _, s := range e.Sources {
		labels = append(labels, s.label())
	}
	return fmt.Sprintf("extension: conflicting %s %q claimed by %v", e.Kind, e.ID, labels)
}

// Conflicts reports every same-tier multi-source duplicate of a canonical ID
// among shadowed (non-additive) kinds. Hooks and interceptors are additive
// and never appear here. The result is deterministically ordered.
func (c *Catalog) Conflicts() []ConflictError {
	groups := map[ContributionKind]map[string][]Contribution{}
	for _, ct := range c.contribs {
		if additiveKind(ct.Kind) {
			continue
		}
		byID := groups[ct.Kind]
		if byID == nil {
			byID = map[string][]Contribution{}
			groups[ct.Kind] = byID
		}
		byID[ct.ID] = append(byID[ct.ID], ct)
	}
	var out []ConflictError
	for kind, byID := range groups {
		for id, cs := range byID {
			sources, ok := conflictingSources(cs)
			if !ok {
				continue
			}
			out = append(out, ConflictError{Kind: kind, ID: id, Sources: sources})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// conflictingSources returns the distinct sources tied at the highest tier
// among cs, when more than one source sits there. Sources are deduped by
// their identity key and returned in deterministic order.
func conflictingSources(cs []Contribution) ([]ContributionSource, bool) {
	best := -1
	for _, ct := range cs {
		if r := tierRank(ct.Source.Scope); r > best {
			best = r
		}
	}
	seen := map[string]bool{}
	var sources []ContributionSource
	for _, ct := range cs {
		if tierRank(ct.Source.Scope) != best {
			continue
		}
		key := ct.Source.key()
		if seen[key] {
			continue
		}
		seen[key] = true
		sources = append(sources, ct.Source)
	}
	sort.Slice(sources, func(i, j int) bool { return sources[i].key() < sources[j].key() })
	return sources, len(sources) > 1
}
