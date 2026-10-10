package pluginpkg

import "reasonix/internal/base/textutil"

// InventoryForDisplay is Inventory projected for a surface that shows it. The
// plain Inventory stays the package's own text, which callers that decide or
// fingerprint must read.
func (p Package) InventoryForDisplay() Inventory { return p.Inventory().bounded() }

// bounded projects every author-supplied string in the inventory through the
// shared preview limits: names as identities, paths and commands as locators,
// descriptions as prose. Renderers of an installed package read the inventory
// through here; Display and RuntimeTrustText cover the rest of its detail view.
func (inv Inventory) bounded() Inventory {
	id, loc, prose := textutil.ShownIdentity, textutil.ShownLocator, textutil.ShownProse
	for i := range inv.Skills {
		s := &inv.Skills[i]
		s.Name, s.Description, s.Path, s.Invocation, s.RunAs = id(s.Name), prose(s.Description), loc(s.Path), id(s.Invocation), id(s.RunAs)
	}
	for i := range inv.Agents {
		a := &inv.Agents[i]
		a.Name, a.Description, a.Path, a.Invocation, a.Model = id(a.Name), prose(a.Description), loc(a.Path), id(a.Invocation), id(a.Model)
		for j := range a.AllowedTools {
			a.AllowedTools[j] = id(a.AllowedTools[j])
		}
	}
	for i := range inv.Commands {
		c := &inv.Commands[i]
		c.Name, c.Description, c.ArgHint, c.Path, c.Invocation = id(c.Name), prose(c.Description), id(c.ArgHint), loc(c.Path), id(c.Invocation)
	}
	for i := range inv.Prompts {
		p := &inv.Prompts[i]
		p.Name, p.Description, p.ArgHint, p.Path = id(p.Name), prose(p.Description), id(p.ArgHint), loc(p.Path)
	}
	for i := range inv.Themes {
		inv.Themes[i].Name, inv.Themes[i].Path = id(inv.Themes[i].Name), loc(inv.Themes[i].Path)
	}
	for i := range inv.Hooks {
		h := &inv.Hooks[i]
		h.Event, h.Match, h.Command, h.ContextFile, h.Description = id(h.Event), loc(h.Match), loc(h.Command), loc(h.ContextFile), prose(h.Description)
	}
	for i := range inv.MCPServers {
		m := &inv.MCPServers[i]
		m.Name, m.DisplayName, m.Description = id(m.Name), id(m.DisplayName), prose(m.Description)
		m.Transport, m.Command, m.URL = id(m.Transport), loc(m.Command), loc(m.URL)
	}
	return inv
}

// Display returns the installed-package record as a detail view may print it.
// Root is left alone: it is a path this process resolves, not text it shows.
func (p InstalledPlugin) Display() InstalledPlugin {
	p.Name, p.Version, p.ManifestKind, p.Commit = textutil.ShownIdentity(p.Name), textutil.ShownIdentity(p.Version), textutil.ShownIdentity(p.ManifestKind), textutil.ShownIdentity(p.Commit)
	p.Source, p.Description = textutil.ShownLocator(p.Source), textutil.ShownProse(p.Description)
	p.Status, p.StatusReason = textutil.ShownIdentity(p.Status), textutil.ShownProse(p.StatusReason)
	return p
}

// DisplayLines bounds a list of warnings or diagnostics written from package content.
func DisplayLines(in []string) []string {
	if len(in) > textutil.MaxProseItems {
		in = in[:textutil.MaxProseItems]
	}
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = textutil.ShownProse(s)
	}
	return out
}

// DisplayIssues bounds compatibility findings, whose text comes from the package.
func DisplayIssues(in []CompatibilityIssue) []CompatibilityIssue {
	if len(in) > textutil.MaxProseItems {
		in = in[:textutil.MaxProseItems]
	}
	out := make([]CompatibilityIssue, len(in))
	for i, c := range in {
		out[i] = CompatibilityIssue{Capability: textutil.ShownIdentity(c.Capability), Path: textutil.ShownLocator(c.Path), Reason: textutil.ShownProse(c.Reason)}
	}
	return out
}

func shownLocators(in []string) []string {
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = textutil.ShownLocator(s)
	}
	return out
}

// Display returns the runtime declaration as a detail view may print it.
func (rt *RuntimeSpec) Display() *RuntimeSpec {
	if rt == nil {
		return nil
	}
	c := *rt
	c.Command, c.Args = textutil.ShownLocator(rt.Command), shownLocators(rt.Args)
	c.Intercepts, c.Replaces, c.Capabilities = DisplayIdentities(rt.Intercepts), DisplayIdentities(rt.Replaces), DisplayIdentities(rt.Capabilities)
	return &c
}

// DisplayIdentities bounds a list of names written from package content.
func DisplayIdentities(in []string) []string {
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = textutil.ShownIdentity(s)
	}
	return out
}
