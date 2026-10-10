package installsource

import (
	"slices"

	"reasonix/internal/base/textutil"
	"reasonix/internal/ext/pluginpkg"
)

// Every string and list a plan shows the user passes through the projection
// below, so each frontend inherits the same bounds. Fields that name or run
// something are rendered literally (hidden characters become visible escapes);
// prose is stripped of what cannot be seen. The approval itself is bound to the
// planId, a digest of the complete unprojected plan, never to this text.
var (
	limitIdentity = textutil.PreviewIdentity
	limitLocator  = textutil.PreviewLocator
	limitProse    = textutil.PreviewProse
)

const (
	maxIdentityItems = textutil.MaxIdentityItems
	maxLocatorItems  = textutil.MaxLocatorItems
	maxProseItems    = textutil.MaxProseItems
)

// hostLiteral renders package-supplied text that a host-written sentence embeds.
// Nothing is deleted from it, so the sentence around it is never swallowed.
func hostLiteral(s string) string { return textutil.ShownLocator(s) }

// themesOnly holds the theme category to its name. It reads the unprojected
// plan: the bounded preview shows fewer steps than the plan installs.
func themesOnly(actions []action) bool {
	if len(actions) == 0 {
		return false
	}
	for _, a := range actions {
		others := a.SkillCount + a.AgentCount + a.CommandCount + a.HookCount + a.ToolCount + a.PromptCount
		if a.Kind != "plugin" || a.ThemeCount == 0 || others != 0 || a.Runtime != nil {
			return false
		}
	}
	return true
}

type previewer struct{ cut bool }

func (p *previewer) identity(s string) string { return p.text(s, limitIdentity, textutil.BoundLiteral) }
func (p *previewer) locator(s string) string  { return p.text(s, limitLocator, textutil.BoundLiteral) }
func (p *previewer) prose(s string) string    { return p.text(s, limitProse, textutil.BoundProse) }

func (p *previewer) text(s string, lim textutil.PreviewLimit, bound func(string, textutil.PreviewLimit) (string, bool)) string {
	out, cut := bound(s, lim)
	p.cut = p.cut || cut
	return out
}

func listOf[T any](p *previewer, in []T, limit int, one func(T) T) []T {
	if in == nil {
		return nil
	}
	if len(in) > limit {
		in = in[:limit]
		p.cut = true
	}
	out := make([]T, len(in))
	for i, v := range in {
		out[i] = one(v)
	}
	return out
}

func (p *previewer) identities(in []string) []string {
	return listOf(p, in, maxIdentityItems, p.identity)
}
func (p *previewer) locators(in []string) []string { return listOf(p, in, maxLocatorItems, p.locator) }
func (p *previewer) proses(in []string) []string   { return listOf(p, in, maxProseItems, p.prose) }

func (p *previewer) pairs(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	keys := make([]string, 0, len(in))
	for k := range in {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	if len(keys) > maxLocatorItems {
		keys = keys[:maxLocatorItems]
		p.cut = true
	}
	out := make(map[string]string, len(keys))
	for _, k := range keys {
		shown := p.locator(k)
		if _, dup := out[shown]; dup {
			p.cut = true
		}
		out[shown] = p.locator(in[k])
	}
	return out
}

func (p *previewer) skipped(in []pluginpkg.CompatibilityIssue) []pluginpkg.CompatibilityIssue {
	return listOf(p, in, maxProseItems, func(i pluginpkg.CompatibilityIssue) pluginpkg.CompatibilityIssue {
		return pluginpkg.CompatibilityIssue{
			Capability: p.identity(i.Capability),
			Path:       p.locator(i.Path),
			Reason:     p.prose(i.Reason),
		}
	})
}

func (p *previewer) runtime(in *RuntimePlanInfo) *RuntimePlanInfo {
	if in == nil {
		return nil
	}
	return &RuntimePlanInfo{
		Command:      p.locator(in.Command),
		Args:         p.locators(in.Args),
		Intercepts:   p.identities(in.Intercepts),
		Replaces:     p.identities(in.Replaces),
		Capabilities: p.identities(in.Capabilities),
		Tools:        p.identities(in.Tools),
		FullTrust:    in.FullTrust,
	}
}

// previewAction returns the user-facing form of a (redacted) action and sets
// PreviewTruncated when any field or list was cut short.
func previewAction(a action) action {
	var p previewer
	a.Kind, a.Action, a.Status = p.identity(a.Kind), p.identity(a.Action), p.identity(a.Status)
	a.Name, a.Scope, a.Mode = p.identity(a.Name), p.identity(a.Scope), p.identity(a.Mode)
	a.Transport, a.Layout, a.Commit = p.identity(a.Transport), p.identity(a.Layout), p.identity(a.Commit)
	a.Compatibility, a.ManifestKind, a.Version = p.identity(a.Compatibility), p.identity(a.ManifestKind), p.identity(a.Version)
	a.Source, a.Target, a.ConfigPath = p.locator(a.Source), p.locator(a.Target), p.locator(a.ConfigPath)
	a.URL, a.Command = p.locator(a.URL), p.locator(a.Command)
	a.InstallRoot, a.CanonicalPath = p.locator(a.InstallRoot), p.locator(a.CanonicalPath)
	a.Args, a.Env, a.Headers = p.locators(a.Args), p.pairs(a.Env), p.pairs(a.Headers)
	a.Skills, a.Agents, a.Commands = p.identities(a.Skills), p.identities(a.Agents), p.identities(a.Commands)
	a.MappedCapabilities = p.identities(a.MappedCapabilities)
	a.SkippedCapabilities = p.skipped(a.SkippedCapabilities)
	a.Runtime = p.runtime(a.Runtime)
	a.RiskReasons, a.Warnings = p.proses(a.RiskReasons), p.proses(a.Warnings)
	a.Error, a.Next = p.prose(a.Error), p.prose(a.Next)
	a.PreviewTruncated = p.cut
	return a
}

// capActions keeps every high-risk step and the first MaxActions of the rest,
// so the steps a person most needs to see are never the ones left out.
func capActions(in []action) ([]action, int) {
	out := make([]action, 0, min(len(in), textutil.MaxActions))
	rest, hidden := 0, 0
	for _, a := range in {
		switch {
		case a.RiskLevel == RiskHigh:
			out = append(out, a)
		case rest < textutil.MaxActions:
			rest++
			out = append(out, a)
		default:
			hidden++
		}
	}
	return out, hidden
}

// previewResponse bounds the plan-level fields and rolls the per-action flag
// up, so a consumer that reads only the envelope still learns of a cut.
func previewResponse(r response) response {
	var p previewer
	r.Op, r.Kind, r.Scope, r.Mode = p.identity(r.Op), p.identity(r.Kind), p.identity(r.Scope), p.identity(r.Mode)
	r.Name, r.Status = p.identity(r.Name), p.identity(r.Status)
	r.Source = p.locator(r.Source)
	r.Warnings = p.proses(r.Warnings)
	r.Error, r.Next = p.prose(r.Error), p.prose(r.Next)
	r.Actions, r.HiddenActions = capActions(r.Actions)
	for _, a := range r.Actions {
		p.cut = p.cut || a.PreviewTruncated
	}
	r.PreviewTruncated = p.cut
	return r
}
